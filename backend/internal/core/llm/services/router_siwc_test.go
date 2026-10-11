package services

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/llm/models"
	"github.com/orkestra/backend/internal/core/llm/providers"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// siwcEnv is the router over a real AccountsService (in-memory repo, fake
// OIDC) and the real openai adapter pointed at an httptest plan route; a
// scripted "anthropic" org candidate stands by to prove no cross-kind
// fallback ever happens.
type siwcEnv struct {
	h       *accountsHarness
	r       *Router
	store   *memBudgetStore
	acc     *models.UserAccount
	logs    *bytes.Buffer
	orgCall *int

	mu      sync.Mutex
	tokens  []string // Authorization of every inference request
	respond func(w http.ResponseWriter, token string)
}

func newSIWCEnv(t *testing.T) *siwcEnv {
	t.Helper()
	oidc := &fakeOIDC{scope: SIWCScope, sub: "sub-1", models: []string{"gpt-5.6-terra"}}
	e := &siwcEnv{h: newAccountsHarness(t, hostedPasteConfig(SIWCModeHostedPaste), oidc), store: newMemBudgetStore(0, ""), logs: &bytes.Buffer{}, orgCall: new(int)}
	e.store.budget = nil
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		e.mu.Lock()
		e.tokens = append(e.tokens, tok)
		respond := e.respond
		e.mu.Unlock()
		respond(w, tok)
	}))
	t.Cleanup(srv.Close)
	reg := providers.NewRegistry()
	reg.Register("openai", func(c providers.Credential, _ *http.Client) (providers.Provider, error) {
		c.BaseURL = srv.URL + "/v1"
		return providers.NewOpenAI(c, srv.Client())
	})
	reg.Register("anthropic", func(providers.Credential, *http.Client) (providers.Provider, error) {
		return &scriptedProvider{name: "anthropic", caps: allCaps, chat: ok("org", 1, 1), calls: e.orgCall}, nil
	})
	catalog, _, _, _, _ := newCatalog(t, CatalogConfig{AllowHosted: true})
	budget := NewBudgetService(e.store, func() BudgetConfig { return BudgetConfig{ReserveOutputTokens: 10, ReservationTTL: time.Minute} }, slog.Default())
	e.r = NewRouter(reg, NewCredentialResolver(catalog, e.h.svc), budget, NewBreaker(3, 30*time.Second), func(string) *http.Client { return srv.Client() },
		func() RouterConfig { return RouterConfig{RequestTimeout: 5 * time.Second} }, nil, slog.New(slog.NewTextHandler(e.logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	// The module's hook (module.afterCall), over the same accounts.
	e.r.AfterCall = func(ctx context.Context, m models.Model, id string, err error) {
		if err == nil && m.CredentialRef.Kind == models.CredentialKindUserAccount {
			e.h.svc.MarkUsed(ctx, id)
		}
	}
	e.h.svc.logger = slog.New(slog.NewTextHandler(e.logs, nil)) // token-free by contract; asserted below
	e.acc = e.h.linkAccount(t, ctxFor("t1"), "oaiapp_1", "c")
	return e
}

func (e *siwcEnv) setRespond(fn func(w http.ResponseWriter, token string)) {
	e.mu.Lock()
	e.respond = fn
	e.mu.Unlock()
}

func (e *siwcEnv) seen() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.tokens...)
}

func (e *siwcEnv) cands() []models.Model {
	org := model("org", "anthropic", "c2") // same purpose, cheaper never matters: another kind
	return []models.Model{userAccountModel("ua", "gpt-5.6-terra"), org}
}

func (e *siwcEnv) chat() (*iface.ChatResult, error) {
	return e.r.Chat(ctxFor("t1"), e.cands(), "u-admin", simpleReq, nil)
}

func (e *siwcEnv) account(t *testing.T) models.UserAccount {
	t.Helper()
	row, ok := e.h.repo.row(e.acc.UUID)
	if !ok {
		t.Fatal("account row missing")
	}
	return row
}

func planSSE(w http.ResponseWriter, events ...string) {
	w.Header().Set("Content-Type", "text/event-stream")
	for _, ev := range events {
		_, _ = w.Write([]byte(ev + "\n\n"))
	}
}

const planCompleted = "event: response.completed\ndata: " + `{"type":"response.completed","response":{"id":"r","status":"completed","output":[],"usage":{"input_tokens":7,"output_tokens":3,"input_tokens_details":{"cached_tokens":0},"output_tokens_details":{"reasoning_tokens":0}}}}`

func planDelta(text string) string {
	return "event: response.output_text.delta\ndata: " + `{"type":"response.output_text.delta","item_id":"m","output_index":0,"content_index":0,"delta":"` + text + `"}`
}

func planFailed(code string) string {
	return "event: response.failed\ndata: " + `{"type":"response.failed","response":{"id":"r","status":"failed","error":{"code":"` + code + `","message":"provider text user@example.test"}}}`
}

func planHTTP(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Request-Id", "req_siwc_1")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

// Success on the plan route: billed to the linked account (ledger kind and
// identity), the user-account rollup counts it, lastUsedAt is stamped.
func TestRouterSIWC_SuccessStampsLastUsedAndRollup(t *testing.T) {
	e := newSIWCEnv(t)
	e.setRespond(func(w http.ResponseWriter, _ string) { planSSE(w, planDelta("hi"), planCompleted) })
	if e.account(t).LastUsedAt != nil {
		t.Fatal("lastUsedAt set before any call")
	}
	res, err := e.chat()
	if err != nil || res.BillingScope != models.CredentialKindUserAccount || res.Usage.InputTokens != 7 || res.Fallback {
		t.Fatalf("chat = %+v, %v", res, err)
	}
	if got := e.seen(); len(got) != 1 || got[0] != "at-c" {
		t.Fatalf("inference tokens: %d requests, first is the linked token %v", len(got), len(got) == 1 && got[0] == "at-c")
	}
	ev := singleLedger(t, e.store)
	if ev.Outcome != models.OutcomeOK || ev.CredentialKind != models.CredentialKindUserAccount || ev.CredentialUUID != e.acc.UUID {
		t.Fatalf("ledger = %+v", ev)
	}
	for _, mo := range e.store.monthly {
		if mo.UserAccountInputTokens != 7 || mo.UserAccountOutputTokens != 3 || mo.OrgInputTokens != 0 || mo.OrgOutputTokens != 0 {
			t.Fatalf("rollup = %+v", mo)
		}
	}
	if acc := e.account(t); acc.LastUsedAt == nil || acc.Status != models.AccountStatusActive {
		t.Fatalf("account after success: lastUsedAt %v, status %q", acc.LastUsedAt, acc.Status)
	}
}

// Review Focus 5: the plan quota is exhausted inside an opened stream. The
// call is final: no fallback to the org candidate, an error ledger row with
// plan_quota_exhausted billed to user_account, the account stays active,
// lastUsedAt is not stamped.
func TestRouterSIWC_QuotaExhaustedInStreamIsFinal(t *testing.T) {
	e := newSIWCEnv(t)
	e.setRespond(func(w http.ResponseWriter, _ string) {
		planSSE(w, planFailed("subscription_sharing_usage_limit_exceeded"))
	})
	_, err := e.chat()
	if !errors.Is(err, iface.ErrLLMQuotaExhausted) {
		t.Fatalf("err = %v", err)
	}
	if *e.orgCall != 0 || len(e.seen()) != 1 {
		t.Fatalf("org calls %d, plan calls %d", *e.orgCall, len(e.seen()))
	}
	ev := singleLedger(t, e.store)
	if ev.Outcome != models.OutcomeError || ev.ErrorCode != "plan_quota_exhausted" || ev.CredentialKind != models.CredentialKindUserAccount || len(ev.Attempts) != 1 {
		t.Fatalf("ledger = %+v", ev)
	}
	if acc := e.account(t); acc.Status != models.AccountStatusActive || acc.LastUsedAt != nil {
		t.Fatalf("account: status %q, lastUsedAt %v", acc.Status, acc.LastUsedAt)
	}
	if strings.Contains(e.logs.String(), "provider text") || strings.Contains(e.logs.String(), "at-c") {
		t.Fatalf("provider text or token logged: %s", e.logs.String())
	}
}

// A fallbackable plan-route failure (503) still never falls back to an
// org-billed candidate (D12: a fallback never changes who pays), and the
// SIWC 503 is provider_unavailable, never credential_rejected.
func TestRouterSIWC_NeverFallsBackToOrgKey(t *testing.T) {
	e := newSIWCEnv(t)
	e.setRespond(func(w http.ResponseWriter, _ string) { planHTTP(w, 503, `{"error":{"message":"provider text"}}`) })
	_, err := e.chat()
	if !errors.Is(err, iface.ErrLLMProviderUnavailable) || *e.orgCall != 0 {
		t.Fatalf("err = %v, org calls = %d", err, *e.orgCall)
	}
	if ev := singleLedger(t, e.store); ev.ErrorCode != "provider_unavailable" || ev.CredentialKind != models.CredentialKindUserAccount || ev.Attempts[0].CredentialKind != models.CredentialKindUserAccount {
		t.Fatalf("ledger = %+v", ev)
	}
	if e.account(t).LastUsedAt != nil {
		t.Fatal("a failed call must not stamp lastUsedAt")
	}
}

// Ruling 5.2: an inference 401 before any output forces one refresh and
// retries the same candidate once with the rotated token.
func TestRouterSIWC_InferenceUnauthorizedRefreshesOnceAndRetries(t *testing.T) {
	e := newSIWCEnv(t)
	e.setRespond(func(w http.ResponseWriter, tok string) {
		if tok == "at-c" {
			planHTTP(w, 401, `{"error":{"code":"subscription_sharing_invalid_user","message":"provider text"}}`)
			return
		}
		planSSE(w, planDelta("ok"), planCompleted)
	})
	res, err := e.chat()
	if err != nil || res.Fallback || res.Model.UUID != "ua" {
		t.Fatalf("chat = %+v, %v", res, err)
	}
	if got := e.seen(); len(got) != 2 || got[0] != "at-c" || got[1] != "at-r1" {
		t.Fatalf("inference requests = %d (old then rotated token: %v)", len(got), len(got) == 2 && got[0] == "at-c" && got[1] == "at-r1")
	}
	e.h.oidc.locked(func() {
		if e.h.oidc.refreshes != 1 {
			t.Errorf("refreshes = %d", e.h.oidc.refreshes)
		}
	})
	ev := singleLedger(t, e.store)
	if ev.Outcome != models.OutcomeOK || len(ev.Attempts) != 2 || ev.Attempts[0].Outcome != attemptError || ev.Attempts[1].Outcome != attemptOK || ev.CredentialUUID != e.acc.UUID {
		t.Fatalf("ledger = %+v", ev)
	}
	logs := e.logs.String()
	if !strings.Contains(logs, "requestId=req_siwc_1") || !strings.Contains(logs, "status=401") || strings.Contains(logs, "provider text") || strings.Contains(logs, "at-c") {
		t.Fatalf("log line: %s", logs)
	}
	if acc := e.account(t); acc.LastUsedAt == nil || acc.Status != models.AccountStatusActive {
		t.Fatalf("account: %+v", acc.Status)
	}
	// The rejected attempt did not count toward the breaker.
	key := "openai||gpt-5.6-terra|" + e.acc.UUID
	for i := 0; i < 3; i++ {
		ticket, err := e.r.breaker.Allow(key)
		if err != nil {
			t.Fatalf("breaker opened after a refreshed 401: %v", err)
		}
		e.r.breaker.Release(key, ticket)
	}
}

// A terminal refresh after the 401 flags the account needs_reauth and
// answers ErrLLMUserAccountReauth; no retry, no fallback.
func TestRouterSIWC_InferenceUnauthorizedTerminalRefreshNeedsReauth(t *testing.T) {
	e := newSIWCEnv(t)
	e.setRespond(func(w http.ResponseWriter, _ string) { planHTTP(w, 401, `{}`) })
	e.h.oidc.locked(func() { e.h.oidc.refreshErr = providers.ClassifySIWCCode("refresh_token_reused") })
	_, err := e.chat()
	if !errors.Is(err, iface.ErrLLMUserAccountReauth) || len(e.seen()) != 1 || *e.orgCall != 0 {
		t.Fatalf("err = %v, plan calls %d, org calls %d", err, len(e.seen()), *e.orgCall)
	}
	if acc := e.account(t); acc.Status != models.AccountStatusNeedsReauth || acc.LastUsedAt != nil {
		t.Fatalf("account: status %q, lastUsedAt %v", acc.Status, acc.LastUsedAt)
	}
	if ev := singleLedger(t, e.store); ev.ErrorCode != "user_account_reauth_required" {
		t.Fatalf("ledger = %+v", ev)
	}
}

// One forced refresh per call: a second 401 with the rotated token is
// provider_unavailable (never credential_rejected), not another refresh.
func TestRouterSIWC_SecondUnauthorizedIsUnavailable(t *testing.T) {
	e := newSIWCEnv(t)
	e.setRespond(func(w http.ResponseWriter, _ string) { planHTTP(w, 401, `{}`) })
	_, err := e.chat()
	if !errors.Is(err, iface.ErrLLMProviderUnavailable) || errors.Is(err, providers.ErrUnauthorized) || errorCodeOf(err) != "provider_unavailable" {
		t.Fatalf("err = %v (%s)", err, errorCodeOf(err))
	}
	if len(e.seen()) != 2 || *e.orgCall != 0 {
		t.Fatalf("plan calls %d, org calls %d", len(e.seen()), *e.orgCall)
	}
	e.h.oidc.locked(func() {
		if e.h.oidc.refreshes != 1 {
			t.Errorf("refreshes = %d", e.h.oidc.refreshes)
		}
	})
	if acc := e.account(t); acc.Status != models.AccountStatusActive {
		t.Fatalf("a non-terminal outcome must keep the account active: %q", acc.Status)
	}
}

// Output already reached the caller: an invalid-user failure in the stream
// is partial output, never refreshed and retried.
func TestRouterSIWC_NoRefreshAfterOutput(t *testing.T) {
	e := newSIWCEnv(t)
	e.setRespond(func(w http.ResponseWriter, _ string) {
		planSSE(w, planDelta("partial"), planFailed("subscription_sharing_invalid_user"))
	})
	_, err := e.chat()
	if !errors.Is(err, iface.ErrLLMPartialOutput) || len(e.seen()) != 1 {
		t.Fatalf("err = %v, plan calls %d", err, len(e.seen()))
	}
	e.h.oidc.locked(func() {
		if e.h.oidc.refreshes != 0 {
			t.Errorf("refreshes = %d", e.h.oidc.refreshes)
		}
	})
}

// Embeddings on a user_account model are always a capability mismatch,
// decided before the linked account is touched.
func TestRouterSIWC_EmbedIsCapabilityMismatch(t *testing.T) {
	e := newSIWCEnv(t)
	m := userAccountModel("ua", "gpt-5.6-terra")
	m.Capabilities.Embeddings = true // even if a row claimed it
	var plansBefore int
	e.h.oidc.locked(func() { plansBefore = e.h.oidc.planCalls })
	_, err := e.r.Embed(ctxFor("t1"), []models.Model{m}, "u-admin", iface.EmbedRequest{Caller: "t", Inputs: []string{"x"}})
	if !errors.Is(err, iface.ErrLLMCapabilityMismatch) || len(e.seen()) != 0 {
		t.Fatalf("err = %v, calls %d", err, len(e.seen()))
	}
	e.h.oidc.locked(func() {
		if e.h.oidc.planCalls != plansBefore {
			t.Error("the linked account was resolved for an embedding")
		}
	})
	if ev := singleLedger(t, e.store); ev.ErrorCode != "capability_mismatch" || ev.Attempts[0].Outcome != attemptCapabilityMismatch {
		t.Fatalf("ledger = %+v", ev)
	}
}

// Important 1 (final review): a plan-route configuration rejection
// (route_not_supported, chatpass_v2_*) is final. The next user_account
// candidate bills the same account through the same issued client and
// grant, so there is exactly one provider call per logical call, no
// fallback, and the rejection never counts toward the breaker: a fourth
// call still reaches the provider after three rejections.
func TestRouterSIWC_RouteConfigurationRejectionIsFinalAndNotAFault(t *testing.T) {
	for _, tc := range []struct {
		name    string
		respond func(w http.ResponseWriter)
	}{
		{"route_not_supported in stream", func(w http.ResponseWriter) {
			planSSE(w, planFailed("subscription_sharing_route_not_supported"))
		}},
		{"chatpass_v2 scope over HTTP", func(w http.ResponseWriter) {
			planHTTP(w, 403, `{"error":{"code":"chatpass_v2_scope_not_authorized","message":"provider text"}}`)
		}},
		{"chatpass_v2 context over HTTP", func(w http.ResponseWriter) {
			planHTTP(w, 400, `{"error":{"code":"chatpass_v2_invalid_authorization_context","message":"provider text"}}`)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newSIWCEnv(t)
			e.setRespond(func(w http.ResponseWriter, _ string) { tc.respond(w) })
			cands := []models.Model{userAccountModel("ua", "gpt-5.6-terra"), userAccountModel("ua2", "gpt-5.6-terra"), model("org", "anthropic", "c2")}
			for call := 1; call <= 4; call++ {
				_, err := e.r.Chat(ctxFor("t1"), cands, "u-admin", simpleReq, nil)
				if !errors.Is(err, iface.ErrLLMProviderUnavailable) || !errors.Is(err, providers.ErrSIWCRouteNotSupported) || errorCodeOf(err) != "provider_unavailable" {
					t.Fatalf("call %d: err = %v (%s)", call, err, errorCodeOf(err))
				}
				if got := len(e.seen()); got != call {
					t.Fatalf("call %d: plan requests = %d (a fallback to the next same-kind model, or an open circuit)", call, got)
				}
			}
			if *e.orgCall != 0 {
				t.Fatalf("org calls = %d", *e.orgCall)
			}
			if len(e.store.ledger) != 4 {
				t.Fatalf("ledger rows = %d, want 4", len(e.store.ledger))
			}
			for _, ev := range e.store.ledger {
				if ev.ErrorCode != "provider_unavailable" || len(ev.Attempts) != 1 || ev.Fallback {
					t.Fatalf("ledger = %+v", ev)
				}
			}
			e.h.oidc.locked(func() {
				if e.h.oidc.refreshes != 0 {
					t.Errorf("a configuration rejection must not refresh (refreshes = %d)", e.h.oidc.refreshes)
				}
			})
			if acc := e.account(t); acc.Status != models.AccountStatusActive {
				t.Fatalf("a configuration rejection is not a reauth: status %q", acc.Status)
			}
		})
	}
}

// Minor 2 (final review): invalid_client from the refresh inside Resolve is
// final for fallback too, and does not count toward the breaker.
func TestRouterSIWC_InvalidClientOnRefreshIsFinal(t *testing.T) {
	e := newSIWCEnv(t)
	e.setRespond(func(w http.ResponseWriter, _ string) { planSSE(w, planDelta("x"), planCompleted) })
	e.h.expire(e.acc.UUID)
	e.h.oidc.locked(func() { e.h.oidc.refreshErr = providers.ClassifySIWCCode("invalid_client") })
	cands := []models.Model{userAccountModel("ua", "gpt-5.6-terra"), userAccountModel("ua2", "gpt-5.6-terra"), model("org", "anthropic", "c2")}
	_, err := e.r.Chat(ctxFor("t1"), cands, "u-admin", simpleReq, nil)
	if !errors.Is(err, providers.ErrSIWCInvalidClient) || errorCodeOf(err) != "provider_unavailable" {
		t.Fatalf("err = %v (%s)", err, errorCodeOf(err))
	}
	if len(e.seen()) != 0 || *e.orgCall != 0 {
		t.Fatalf("plan calls %d, org calls %d", len(e.seen()), *e.orgCall)
	}
	e.h.oidc.locked(func() {
		if e.h.oidc.refreshes != 1 {
			t.Errorf("refreshes = %d, want 1 (the second candidate must not re-hit the token endpoint)", e.h.oidc.refreshes)
		}
	})
	if ev := singleLedger(t, e.store); len(ev.Attempts) != 1 {
		t.Fatalf("ledger attempts = %+v", ev.Attempts)
	}
	if providerFault(providers.ClassifySIWCCode("invalid_client")) || providerFault(providers.ClassifySIWCCode("chatpass_v2_scope_not_authorized")) {
		t.Fatal("a client or grant configuration rejection must not be a provider fault")
	}
}
