package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/orkestra/backend/internal/core/llm/models"
	"github.com/orkestra/backend/internal/core/llm/services"
	"github.com/orkestra/backend/internal/shared/errcode"
	"github.com/orkestra/backend/internal/testkit"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

const (
	siwcConsole       = "http://localhost:8080"
	siwcLoopbackHost  = "127.0.0.1:3007"
	siwcPasteRedirect = "http://127.0.0.1:14555" + services.SIWCCallbackPath
)

type siwcHarness struct {
	h        *SIWCHandler
	accounts *services.AccountsService
	repo     *fakeAccounts
	oidc     *fakeSIWCOIDC
	redis    *memRedis
	metrics  *recordingMetrics
	logs     *bytes.Buffer
	mu       sync.Mutex
	cfg      services.SIWCConfig
}

func (s *siwcHarness) setMode(mode string) {
	s.mu.Lock()
	s.cfg.Mode = mode
	s.mu.Unlock()
}

func (s *siwcHarness) config() services.SIWCConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg
}

// newSIWCHandler drives the real AccountsService over the fakes; the API is
// at http://127.0.0.1:3007 (loopback_local), the paste port is 14555.
func newSIWCHandler(t *testing.T, mode string) (*siwcHarness, context.Context) {
	t.Helper()
	logs := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(logs, nil))
	vault, err := services.NewVault(testKeyHex, logger)
	if err != nil {
		t.Fatal(err)
	}
	s := &siwcHarness{
		repo: newFakeAccounts(), oidc: &fakeSIWCOIDC{sub: "SUBJECT-SENTINEL", scope: services.SIWCScope}, redis: newMemRedis(),
		metrics: &recordingMetrics{}, logs: logs,
		cfg: services.SIWCConfig{Mode: mode, PastePort: services.SIWCDefaultPastePort, OperatorPublicURL: "http://" + siwcLoopbackHost, AllowHosted: true},
	}
	limiter := services.NewRateLimiter(s.redis, logger)
	accounts := services.NewAccountsService(services.AccountsDeps{
		Repo: s.repo, Host: fakeHost("urn:uuid:host-1"), OIDC: s.oidc, Plans: s.oidc,
		State: services.NewSIWCStateStore(s.redis, vault), Vault: vault, Limiter: limiter, Redis: s.redis,
		Config: s.config, Logger: logger,
	})
	s.accounts = accounts
	s.h = NewSIWCHandler(accounts, s.config, limiter, s.metrics, siwcConsole+"/", logger)
	return s, ctxForUser(t, "t1", "u-admin")
}

func ctxForUser(t *testing.T, tenant, user string) context.Context {
	t.Helper()
	id := testkit.NewIdentity(user, user+"@example.test", "operator").WithTenant(tenant, []string{"org_member"}, true)
	return ctxauth.WithTenantKind(id.ContextFor(context.Background(), tenant), "internal")
}

func stateOf(t *testing.T, authorizeURL string) string {
	t.Helper()
	u, err := url.Parse(authorizeURL)
	if err != nil || u.Query().Get("state") == "" {
		t.Fatalf("authorize URL without a state")
	}
	return u.Query().Get("state")
}

func (s *siwcHarness) link(t *testing.T, ctx context.Context, reauth string) string {
	t.Helper()
	req := &LLMAccountLinkRequest{}
	req.Body.ReauthAccountUUID = reauth
	res, err := s.h.Link(ctx, req)
	if err != nil {
		t.Fatalf("link: %v", err)
	}
	return stateOf(t, res.Body.AuthorizeURL)
}

func (s *siwcHarness) complete(ctx context.Context, callbackURL string) (*LLMAccountResponse, error) {
	req := &LLMAccountCompleteRequest{}
	req.Body.CallbackURL = callbackURL
	return s.h.Complete(ctx, req)
}

// callback serves the public loopback route; host defaults to the API's.
func (s *siwcHarness) callback(query, host string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, services.SIWCCallbackPath+"?"+query, nil)
	req.RemoteAddr = "192.0.2.10:5555"
	req.Host = host
	if host == "" {
		req.Host = siwcLoopbackHost
	}
	s.h.Callback(rec, req)
	return rec
}

// redirectOf asserts the closed return contract and returns its query.
func redirectOf(t *testing.T, rec *httptest.ResponseRecorder) url.Values {
	t.Helper()
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d", rec.Code)
	}
	if rec.Header().Get("Referrer-Policy") != "no-referrer" || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("headers = %v", rec.Header())
	}
	u, err := url.Parse(rec.Header().Get("Location"))
	if err != nil || u.Scheme+"://"+u.Host != siwcConsole || u.Path != "/user/security" {
		t.Fatalf("location = %q", rec.Header().Get("Location"))
	}
	q := u.Query()
	for k := range q {
		if k != "tab" && k != "linked" && k != "error" {
			t.Fatalf("redirect carries %q: %q", k, u.RawQuery)
		}
	}
	if q.Get("tab") != "ai" || (q.Get("linked") == "") == (q.Get("error") == "") {
		t.Fatalf("redirect query = %q", u.RawQuery)
	}
	if q.Get("linked") != "" && q.Get("linked") != "ok" {
		t.Fatalf("linked = %q", q.Get("linked"))
	}
	if e := q.Get("error"); e != "" && !siwcRedirectAllowlist[e] {
		t.Fatalf("error %q outside the allowlist", e)
	}
	return q
}

func assertNoSecrets(t *testing.T, what, s string) {
	t.Helper()
	for _, bad := range []string{"SENTINEL", "accessToken", "refreshToken", "idToken", "clientId", "oaiapp_", "subject"} {
		if strings.Contains(s, bad) {
			t.Fatalf("%s contains %q: %s", what, bad, s)
		}
	}
}

// --- Link ----------------------------------------------------------------------

func TestSIWC_LinkDisabledIs404(t *testing.T) {
	s, ctx := newSIWCHandler(t, services.SIWCModeDisabled)
	_, err := s.h.Link(ctx, &LLMAccountLinkRequest{})
	if status, code, _ := codeOf(t, err); status != 404 || code != errcode.LLMSIWCDisabled {
		t.Fatalf("link = %d %s", status, code)
	}
	if _, err := s.complete(ctx, siwcPasteRedirect+"?code=c&state=s"); err == nil {
		t.Fatal("complete must be refused while disabled")
	} else if status, code, _ := codeOf(t, err); status != 404 || code != errcode.LLMSIWCDisabled {
		t.Fatalf("complete = %d %s", status, code)
	}
}

// localhost is never a loopback_local deployment: refused before any attempt.
func TestSIWC_LinkRefusesALocalhostDeployment(t *testing.T) {
	s, ctx := newSIWCHandler(t, services.SIWCModeLoopbackLocal)
	s.mu.Lock()
	s.cfg.OperatorPublicURL = "http://localhost:3007"
	s.mu.Unlock()
	_, err := s.h.Link(ctx, &LLMAccountLinkRequest{})
	if status, code, _ := codeOf(t, err); status != 409 || code != errcode.LLMSIWCUnsupportedDeployment {
		t.Fatalf("link = %d %s", status, code)
	}
}

func TestSIWC_LinkIsRateLimited(t *testing.T) {
	s, ctx := newSIWCHandler(t, services.SIWCModeHostedPaste)
	for i := 0; i < services.SIWCLinkLimit; i++ {
		s.link(t, ctx, "")
	}
	_, err := s.h.Link(ctx, &LLMAccountLinkRequest{})
	if status, code, _ := codeOf(t, err); status != 429 || code != errcode.LLMRateLimited || headerOf(t, err, "Retry-After") == "" {
		t.Fatalf("6th link = %d %s", status, code)
	}
}

// --- paste completion -------------------------------------------------------------

func TestSIWC_PasteLinksAndAnswersAViewWithoutSecrets(t *testing.T) {
	s, ctx := newSIWCHandler(t, services.SIWCModeHostedPaste)
	state := s.link(t, ctx, "")
	resp, err := s.complete(ctx, siwcPasteRedirect+"?code=c1&state="+state+"&client_id=oaiapp_1")
	if err != nil {
		t.Fatal(err)
	}
	if resp.Body.Status != models.AccountStatusActive || !resp.Body.Selected || !resp.Body.PlanUsageGranted || resp.Body.Email != "plan@example.test" {
		t.Fatalf("account = %+v", resp.Body)
	}
	raw, _ := json.Marshal(resp.Body)
	assertNoSecrets(t, "complete response", string(raw))
	if s.metrics.last() != "linked" {
		t.Fatalf("metric = %q", s.metrics.last())
	}

	list, err := s.h.List(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if list.Body.Mode != services.SIWCModeHostedPaste || len(list.Body.Items) != 1 || list.Body.SelectedUUID != resp.Body.UUID {
		t.Fatalf("list = %+v", list.Body)
	}
	raw, _ = json.Marshal(list.Body)
	assertNoSecrets(t, "list response", string(raw))

	// Replayed state: the attempt was consumed.
	_, err = s.complete(ctx, siwcPasteRedirect+"?code=c1&state="+state+"&client_id=oaiapp_1")
	if status, code, _ := codeOf(t, err); status != 409 || code != errcode.LLMSIWCStateInvalid {
		t.Fatalf("replay = %d %s", status, code)
	}
	if s.metrics.last() != "state_invalid" {
		t.Fatalf("metric = %q", s.metrics.last())
	}
	assertNoSecrets(t, "logs", s.logs.String())
}

func TestSIWC_CompleteRejectsForeignState(t *testing.T) {
	s, ctx := newSIWCHandler(t, services.SIWCModeHostedPaste)
	state := s.link(t, ctx, "")
	other := ctxForUser(t, "t1", "u-other")
	_, err := s.complete(other, siwcPasteRedirect+"?code=c&state="+state+"&client_id=oaiapp_1")
	if status, code, _ := codeOf(t, err); status != 409 || code != errcode.LLMSIWCStateInvalid {
		t.Fatalf("foreign complete = %d %s", status, code)
	}
	if s.oidc.exchanged() != 0 || len(s.repo.all()) != 0 {
		t.Fatal("a foreign state must not exchange or store anything")
	}
}

// Spec "Test → Handler", paste side (ruling 5.15): the pasted URL is
// matched against the attempt's own redirect after the state is consumed
// (ruling 5.17).
func TestSIWC_CompleteTable(t *testing.T) {
	cases := []struct {
		name   string
		url    func(state string) string
		status int
		code   string
		metric string
	}{
		{"different host (localhost)", func(st string) string {
			return "http://localhost:14555" + services.SIWCCallbackPath + "?code=c&client_id=oaiapp_1&state=" + st
		}, 422, errcode.LLMSIWCCallbackInvalid, "state_invalid"},
		{"different port", func(st string) string {
			return "http://127.0.0.1:1455" + services.SIWCCallbackPath + "?code=c&client_id=oaiapp_1&state=" + st
		}, 422, errcode.LLMSIWCCallbackInvalid, "state_invalid"},
		{"different path", func(st string) string {
			return "http://127.0.0.1:14555/v1/llm/accounts/openai/other?code=c&client_id=oaiapp_1&state=" + st
		}, 422, errcode.LLMSIWCCallbackInvalid, "state_invalid"},
		{"denied", func(st string) string { return siwcPasteRedirect + "?error=access_denied&state=" + st }, 409, errcode.LLMSIWCDenied, "denied"},
		{"other error=", func(st string) string { return siwcPasteRedirect + "?error=server_error&state=" + st }, 502, errcode.LLMSIWCExchangeFailed, "exchange_failed"},
		{"no issued client id", func(st string) string { return siwcPasteRedirect + "?code=c&state=" + st }, 502, errcode.LLMSIWCExchangeFailed, "exchange_failed"},
		{"unknown state", func(string) string { return siwcPasteRedirect + "?code=c&client_id=oaiapp_1&state=bogus" }, 409, errcode.LLMSIWCStateInvalid, "state_invalid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, ctx := newSIWCHandler(t, services.SIWCModeHostedPaste)
			state := s.link(t, ctx, "")
			_, err := s.complete(ctx, tc.url(state))
			if status, code, _ := codeOf(t, err); status != tc.status || code != tc.code {
				t.Fatalf("complete = %d %s, want %d %s", status, code, tc.status, tc.code)
			}
			if s.metrics.last() != tc.metric {
				t.Fatalf("metric = %q, want %q", s.metrics.last(), tc.metric)
			}
			if s.oidc.exchanged() != 0 || len(s.repo.all()) != 0 {
				t.Fatal("nothing may be exchanged or stored")
			}
			if tc.code == errcode.LLMSIWCStateInvalid {
				return // another state: the attempt itself was never named
			}
			// Whatever the failure, the attempt named is gone.
			_, err = s.complete(ctx, siwcPasteRedirect+"?code=c&client_id=oaiapp_1&state="+state)
			if _, code, _ := codeOf(t, err); code != errcode.LLMSIWCStateInvalid {
				t.Fatalf("after failure: %s", code)
			}
		})
	}
}

func TestSIWC_CompleteRefusesAMalformedPasteWithoutConsuming(t *testing.T) {
	s, ctx := newSIWCHandler(t, services.SIWCModeHostedPaste)
	state := s.link(t, ctx, "")
	for _, bad := range []string{"not a url", siwcPasteRedirect + "?state=" + state, "http://u:p@127.0.0.1:14555" + services.SIWCCallbackPath + "?code=c&state=" + state} {
		_, err := s.complete(ctx, bad)
		if status, code, _ := codeOf(t, err); status != 422 || code != errcode.LLMSIWCCallbackInvalid {
			t.Fatalf("%q = %d %s", bad, status, code)
		}
	}
	if _, err := s.complete(ctx, siwcPasteRedirect+"?code=c&client_id=oaiapp_1&state="+state); err != nil {
		t.Fatalf("a malformed paste must not consume the attempt: %v", err)
	}
}

func TestSIWC_CompleteIsRateLimitedPerUser(t *testing.T) {
	s, ctx := newSIWCHandler(t, services.SIWCModeHostedPaste)
	for i := 0; i < services.SIWCCompleteLimit; i++ {
		_, _ = s.complete(ctx, siwcPasteRedirect+"?code=c&state=bogus")
	}
	_, err := s.complete(ctx, siwcPasteRedirect+"?code=c&state=bogus")
	if status, code, _ := codeOf(t, err); status != 429 || code != errcode.LLMRateLimited || headerOf(t, err, "Retry-After") == "" {
		t.Fatalf("11th complete = %d %s", status, code)
	}
	// Another user has their own budget.
	_, err = s.complete(ctx2(t), siwcPasteRedirect+"?code=c&state=bogus")
	if _, code, _ := codeOf(t, err); code != errcode.LLMSIWCStateInvalid {
		t.Fatalf("other user = %s", code)
	}
}

func ctx2(t *testing.T) context.Context { return ctxForUser(t, "t1", "u-second") }

// --- public loopback callback --------------------------------------------------------

func TestSIWC_CallbackRedirectIsClosed(t *testing.T) {
	s, _ := newSIWCHandler(t, services.SIWCModeLoopbackLocal)
	rec := s.callback("code=c&state=bogus", "")
	if q := redirectOf(t, rec); q.Get("error") != "state_invalid" {
		t.Fatalf("error = %q", q.Get("error"))
	}
	loc := rec.Header().Get("Location")
	if strings.Contains(loc, "code=") || strings.Contains(loc, "token") || strings.Contains(loc, "bogus") {
		t.Fatal("redirect must not carry code, state or tokens")
	}
}

func TestSIWC_CallbackLinksForTheAttemptsUser(t *testing.T) {
	s, ctx := newSIWCHandler(t, services.SIWCModeLoopbackLocal)
	state := s.link(t, ctx, "")
	rec := s.callback("code=c1&client_id=oaiapp_1&state="+url.QueryEscape(state), "")
	if q := redirectOf(t, rec); q.Get("linked") != "ok" {
		t.Fatalf("redirect = %v", q)
	}
	rows := s.repo.all()
	if len(rows) != 1 || rows[0].UserUUID != "u-admin" || rows[0].TenantID != "t1" || rows[0].ClientID != "oaiapp_1" {
		t.Fatalf("rows = %+v", rows)
	}
	if s.metrics.last() != "linked" {
		t.Fatalf("metric = %q", s.metrics.last())
	}
	assertNoSecrets(t, "logs", s.logs.String())
	if loc := rec.Header().Get("Location"); strings.Contains(loc, "code=") || strings.Contains(loc, "state=") {
		t.Fatalf("location = %q", loc)
	}
}

func TestSIWC_CallbackPlanDisabledIsLinkedAndCounted(t *testing.T) {
	s, ctx := newSIWCHandler(t, services.SIWCModeLoopbackLocal)
	s.oidc.scope = "openid profile email offline_access"
	state := s.link(t, ctx, "")
	if q := redirectOf(t, s.callback("code=c1&client_id=oaiapp_1&state="+url.QueryEscape(state), "")); q.Get("linked") != "ok" {
		t.Fatalf("redirect = %v", q)
	}
	if s.metrics.last() != "plan_disabled" {
		t.Fatalf("metric = %q", s.metrics.last())
	}
}

// Spec "Test → Handler", callback side (ruling 5.15).
func TestSIWC_CallbackTable(t *testing.T) {
	cases := []struct {
		name   string
		prep   func(t *testing.T, s *siwcHarness, ctx context.Context) (query, host string)
		want   string
		metric string
	}{
		{"expired state", func(t *testing.T, s *siwcHarness, ctx context.Context) (string, string) {
			st := s.link(t, ctx, "")
			s.redis.flushState()
			return "code=c&client_id=oaiapp_1&state=" + url.QueryEscape(st), ""
		}, "state_invalid", "state_invalid"},
		{"replayed state", func(t *testing.T, s *siwcHarness, ctx context.Context) (string, string) {
			st := s.link(t, ctx, "")
			q := "code=c&client_id=oaiapp_1&state=" + url.QueryEscape(st)
			redirectOf(t, s.callback(q, ""))
			return q, ""
		}, "state_invalid", "state_invalid"},
		{"state minted in paste mode (different redirect)", func(t *testing.T, s *siwcHarness, ctx context.Context) (string, string) {
			s.setMode(services.SIWCModeHostedPaste)
			st := s.link(t, ctx, "")
			s.setMode(services.SIWCModeLoopbackLocal)
			return "code=c&client_id=oaiapp_1&state=" + url.QueryEscape(st), ""
		}, "state_invalid", "state_invalid"},
		{"arrived on another port", func(t *testing.T, s *siwcHarness, ctx context.Context) (string, string) {
			st := s.link(t, ctx, "")
			return "code=c&client_id=oaiapp_1&state=" + url.QueryEscape(st), "127.0.0.1:9999"
		}, "state_invalid", "state_invalid"},
		{"denied", func(t *testing.T, s *siwcHarness, ctx context.Context) (string, string) {
			st := s.link(t, ctx, "")
			return "error=access_denied&state=" + url.QueryEscape(st), ""
		}, "denied", "denied"},
		{"no issued client id", func(t *testing.T, s *siwcHarness, ctx context.Context) (string, string) {
			st := s.link(t, ctx, "")
			return "code=c&state=" + url.QueryEscape(st), ""
		}, "token_exchange_failed", "exchange_failed"},
		{"id token does not validate", func(t *testing.T, s *siwcHarness, ctx context.Context) (string, string) {
			s.oidc.idErr = services.ErrSIWCIDTokenInvalid
			st := s.link(t, ctx, "")
			return "code=c&client_id=oaiapp_1&state=" + url.QueryEscape(st), ""
		}, "id_token_invalid", "id_token_invalid"},
		{"reauth back under an unexpected client id", func(t *testing.T, s *siwcHarness, ctx context.Context) (string, string) {
			first := s.link(t, ctx, "")
			redirectOf(t, s.callback("code=c0&client_id=oaiapp_1&state="+url.QueryEscape(first), ""))
			st := s.link(t, ctx, s.repo.all()[0].UUID)
			return "code=c&client_id=oaiapp_2&state=" + url.QueryEscape(st), ""
		}, "id_token_invalid", "id_token_invalid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, ctx := newSIWCHandler(t, services.SIWCModeLoopbackLocal)
			query, host := tc.prep(t, s, ctx)
			q := redirectOf(t, s.callback(query, host))
			if q.Get("error") != tc.want {
				t.Fatalf("error = %q, want %q", q.Get("error"), tc.want)
			}
			if s.metrics.last() != tc.metric {
				t.Fatalf("metric = %q, want %q", s.metrics.last(), tc.metric)
			}
			assertNoSecrets(t, "logs", s.logs.String())
		})
	}
}

// localhost (or any name) is refused before anything is consumed; the
// same state still completes on 127.0.0.1.
func TestSIWC_CallbackRefusesLocalhostWithoutConsuming(t *testing.T) {
	s, ctx := newSIWCHandler(t, services.SIWCModeLoopbackLocal)
	state := s.link(t, ctx, "")
	query := "code=c1&client_id=oaiapp_1&state=" + url.QueryEscape(state)
	for _, host := range []string{"localhost:3007", "localhost", "evil.example:3007"} {
		if q := redirectOf(t, s.callback(query, host)); q.Get("error") != "unsupported_deployment" {
			t.Fatalf("%s: error = %q", host, q.Get("error"))
		}
	}
	if s.redis.counted("llm:siwc:state:") {
		t.Fatal("a refused host consumed the state")
	}
	if q := redirectOf(t, s.callback(query, "")); q.Get("linked") != "ok" {
		t.Fatalf("redirect = %v", q)
	}
}

// The per-IP limit runs before the state is taken (Task 5 carry), keyed on
// the request's own address (ruling 3.T8.5).
func TestSIWC_CallbackRateLimitRunsBeforeTake(t *testing.T) {
	s, ctx := newSIWCHandler(t, services.SIWCModeLoopbackLocal)
	for i := 0; i < services.SIWCCallbackLimit; i++ {
		redirectOf(t, s.callback("code=c&state=bogus", ""))
	}
	s.redis.flushState()
	state := s.link(t, ctx, "")
	if q := redirectOf(t, s.callback("code=c1&client_id=oaiapp_1&state="+url.QueryEscape(state), "")); q.Get("error") != "state_invalid" {
		t.Fatalf("21st callback = %v", q)
	}
	if s.redis.counted("llm:siwc:state:") {
		t.Fatal("the limited callback consumed the state")
	}
	if !s.redis.counted("llm:rl:siwc_callback:192.0.2.10:") {
		t.Fatal("the limit must be keyed on the request's client IP")
	}
}

// The route is mounted at boot, the mode is re-read live: once the mode is
// no longer loopback_local every callback answers disabled.
func TestSIWC_CallbackAfterModeChangeIsDisabled(t *testing.T) {
	s, ctx := newSIWCHandler(t, services.SIWCModeLoopbackLocal)
	state := s.link(t, ctx, "")
	s.setMode(services.SIWCModeHostedPaste)
	if q := redirectOf(t, s.callback("code=c&client_id=oaiapp_1&state="+url.QueryEscape(state), "")); q.Get("error") != "disabled" {
		t.Fatalf("redirect = %v", q)
	}
	if s.oidc.exchanged() != 0 {
		t.Fatal("no exchange after the mode changed")
	}
}

func TestSIWC_CallbackRedirectBuilderIsTheOnlyOne(t *testing.T) {
	// Structural: no other file in this package may build /user/security.
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("glob: %v", err)
	}
	for _, f := range files {
		if f == "siwc.go" || strings.HasSuffix(f, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "/user/security") {
			t.Fatalf("%s builds a /user/security URL; only siwc.go may", f)
		}
	}
}

// --- select, unlink, admin ---------------------------------------------------------

func TestSIWC_SelectUnlinkAndAdmin(t *testing.T) {
	s, ctx := newSIWCHandler(t, services.SIWCModeHostedPaste)
	link := func(ctx context.Context, code, client string) string {
		resp, err := s.complete(ctx, siwcPasteRedirect+"?code="+code+"&client_id="+client+"&state="+s.link(t, ctx, ""))
		if err != nil {
			t.Fatal(err)
		}
		return resp.Body.UUID
	}
	a := link(ctx, "c1", "oaiapp_1")
	b := link(ctx, "c2", "oaiapp_2")
	other := link(ctx2(t), "c3", "oaiapp_3")

	if _, err := s.h.Select(ctx, &LLMAccountByUUID{UUID: b}); err != nil {
		t.Fatal(err)
	}
	list, _ := s.h.List(ctx, nil)
	if list.Body.SelectedUUID != b || len(list.Body.Items) != 2 {
		t.Fatalf("list = %+v", list.Body)
	}
	// Another user's registration is not found, neither to select nor to unlink.
	for _, err := range []error{
		func() error { _, err := s.h.Select(ctx, &LLMAccountByUUID{UUID: other}); return err }(),
		func() error { _, err := s.h.Unlink(ctx, &LLMAccountByUUID{UUID: other}); return err }(),
	} {
		if status, code, _ := codeOf(t, err); status != 404 || code != errcode.LLMNotFound {
			t.Fatalf("foreign = %d %s", status, code)
		}
	}
	if _, err := s.h.Unlink(ctx, &LLMAccountByUUID{UUID: a}); err != nil {
		t.Fatal(err)
	}

	admin, err := s.h.ListAccounts(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if admin.Body.Mode != services.SIWCModeHostedPaste || len(admin.Body.Items) != 2 {
		t.Fatalf("admin list = %+v", admin.Body)
	}
	owners := map[string]string{}
	for _, it := range admin.Body.Items {
		owners[it.UUID] = it.UserUUID
	}
	if owners[b] != "u-admin" || owners[other] != "u-second" {
		t.Fatalf("owners = %v", owners)
	}
	raw, _ := json.Marshal(admin.Body)
	assertNoSecrets(t, "admin list", string(raw))

	if _, err := s.h.ForceUnlink(ctx, &LLMAccountByUUID{UUID: other}); err != nil {
		t.Fatal(err)
	}
	if rows := s.repo.all(); len(rows) != 1 || rows[0].UUID != b {
		t.Fatalf("rows = %+v", rows)
	}
	// Both unlinks revoked their refresh token.
	s.oidc.mu.Lock()
	revoked := len(s.oidc.revoked)
	s.oidc.mu.Unlock()
	if revoked != 2 {
		t.Fatalf("revocations = %d", revoked)
	}
	// Another org sees none of them.
	empty, _ := s.h.ListAccounts(ctxForUser(t, "t2", "u-admin"), nil)
	if len(empty.Body.Items) != 0 {
		t.Fatalf("t2 sees %+v", empty.Body.Items)
	}
}

func TestMapError_SIWC(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{services.ErrSIWCDisabled, 404, errcode.LLMSIWCDisabled},
		{services.ErrSIWCUnsupportedDeployment, 409, errcode.LLMSIWCUnsupportedDeployment},
		{services.ValidateSIWCConfig(services.SIWCConfig{Mode: services.SIWCModeLoopbackLocal, AllowHosted: true, OperatorPublicURL: "http://localhost:3007"}), 409, errcode.LLMSIWCUnsupportedDeployment},
		{services.ValidateSIWCConfig(services.SIWCConfig{Mode: services.SIWCModeHostedPaste}), 503, errcode.LLMSIWCConfigInvalid},
		{services.ErrSIWCCallbackInvalid, 422, errcode.LLMSIWCCallbackInvalid},
		{services.ErrSIWCStateInvalid, 409, errcode.LLMSIWCStateInvalid},
		{services.ErrSIWCDenied, 409, errcode.LLMSIWCDenied},
		{services.ErrSIWCIDTokenInvalid, 409, errcode.LLMSIWCIDTokenInvalid},
		// The exchange failure wraps provider_unavailable: it must win.
		{fmt.Errorf("%w: %w", services.ErrSIWCExchangeFailed, iface.ErrLLMProviderUnavailable), 502, errcode.LLMSIWCExchangeFailed},
	}
	for _, tc := range cases {
		if status, code, detail := codeOf(t, MapError(tc.err)); status != tc.status || code != tc.code || detail == "" || strings.Contains(detail, "llm siwc") {
			t.Errorf("%v = %d %s %q", tc.err, status, code, detail)
		}
	}
}

// Minor 4 (final review): the paste completion re-validates the live
// configuration like Link and the callback do. An attempt minted before
// allow_hosted was switched off is refused before it is consumed or
// exchanged.
func TestSIWC_CompleteRevalidatesTheLiveConfiguration(t *testing.T) {
	s, ctx := newSIWCHandler(t, services.SIWCModeHostedPaste)
	state := s.link(t, ctx, "")
	s.mu.Lock()
	s.cfg.AllowHosted = false
	s.mu.Unlock()
	_, err := s.complete(ctx, siwcPasteRedirect+"?code=c&client_id=oaiapp_1&state="+state)
	if status, code, _ := codeOf(t, err); status != 503 || code != errcode.LLMSIWCConfigInvalid {
		t.Fatalf("complete = %d %s", status, code)
	}
	if s.oidc.exchanged() != 0 || len(s.repo.all()) != 0 {
		t.Fatal("nothing may be exchanged or stored")
	}
}

type ipAudit struct {
	mu     sync.Mutex
	events []iface.AuditEvent
}

func (a *ipAudit) Emit(_ context.Context, e iface.AuditEvent) {
	a.mu.Lock()
	a.events = append(a.events, e)
	a.mu.Unlock()
}

// Minor 7 (final review): the public callback has no auth middleware, so
// the handler stamps the RealIP-resolved address itself and the linked /
// link_denied audit events carry it.
func TestSIWC_CallbackAuditEventsCarryTheClientIP(t *testing.T) {
	s, ctx := newSIWCHandler(t, services.SIWCModeLoopbackLocal)
	audit := &ipAudit{}
	s.accounts.SetAuditSink(audit)
	linked := s.link(t, ctx, "")
	if q := redirectOf(t, s.callback("code=c1&client_id=oaiapp_1&state="+url.QueryEscape(linked), "")); q.Get("linked") != "ok" {
		t.Fatalf("redirect = %v", q)
	}
	denied := s.link(t, ctx, "")
	if q := redirectOf(t, s.callback("error=access_denied&state="+url.QueryEscape(denied), "")); q.Get("error") != "denied" {
		t.Fatalf("redirect = %v", q)
	}
	audit.mu.Lock()
	defer audit.mu.Unlock()
	seen := map[string]bool{}
	for _, e := range audit.events {
		if e.Action == "llm.account.linked" || e.Action == "llm.account.link_denied" {
			seen[e.Action] = true
			if e.IPAddress != "192.0.2.10" || e.ActorUserID != "u-admin" {
				t.Fatalf("%s: ip %q actor %q", e.Action, e.IPAddress, e.ActorUserID)
			}
		}
	}
	if !seen["llm.account.linked"] || !seen["llm.account.link_denied"] {
		t.Fatalf("events = %v", seen)
	}
}
