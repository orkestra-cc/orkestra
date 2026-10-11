package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/llm/models"
	"github.com/orkestra/backend/internal/core/llm/providers"
	"github.com/orkestra/backend/internal/core/llm/repository"
	"github.com/orkestra/backend/internal/shared/errcode"
	"github.com/orkestra/backend/internal/shared/utils"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/tenantrepo"
)

// --- fakes -------------------------------------------------------------------

// memAccounts mirrors repository.UserAccounts the way memCreds mirrors the
// credentials repository: rows are tenant-scoped, Insert stamps TenantID from
// the context, the three unique indexes answer repository.ErrDuplicateName,
// savedAt is stored at Mongo's millisecond precision and ReplaceTokens is a
// compare-and-swap on it. Mutex-guarded: the refresh tests run it from many
// goroutines under -race.
type memAccounts struct {
	mu   sync.Mutex
	rows map[string]*models.UserAccount
	// beforeInsert, when set, runs once inside the next Insert before the
	// uniqueness checks (a concurrent writer's window).
	beforeInsert func(rows map[string]*models.UserAccount)
	// beforeReplace, when set, runs once inside the next ReplaceTokens
	// before the compare-and-swap (another writer storing a set first).
	beforeReplace func(r *models.UserAccount)
	// listErr, when set, is what ListByUser answers (a Mongo blip).
	listErr error
	// beforeDelete, when set, runs once inside the next Delete before the
	// row goes (a refresh storing a newer set in the unlink's window).
	beforeDelete func(r *models.UserAccount)
}

func newMemAccounts() *memAccounts { return &memAccounts{rows: map[string]*models.UserAccount{}} }

func (m *memAccounts) Insert(ctx context.Context, a *models.UserAccount) error {
	tenant, err := tenantrepo.StampInsert(ctx)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if hook := m.beforeInsert; hook != nil {
		m.beforeInsert = nil
		hook(m.rows)
	}
	for _, r := range m.rows {
		if r.TenantID != tenant {
			continue
		}
		if r.UUID == a.UUID || (r.UserUUID == a.UserUUID && r.ClientID == a.ClientID) ||
			(a.Selected && r.Selected && r.UserUUID == a.UserUUID && r.Provider == a.Provider) {
			return repository.ErrDuplicateName
		}
	}
	a.TenantID = tenant
	cp := *a
	cp.SavedAt = cp.SavedAt.Truncate(time.Millisecond)
	m.rows[a.UUID] = &cp
	return nil
}

func (m *memAccounts) scoped(ctx context.Context, keep func(*models.UserAccount) bool) []models.UserAccount {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []models.UserAccount{}
	for _, r := range m.rows {
		if r.TenantID == tenantOf(ctx) && keep(r) {
			out = append(out, *r)
		}
	}
	return out
}

func (m *memAccounts) ListByUser(ctx context.Context, user string) ([]models.UserAccount, error) {
	m.mu.Lock()
	err := m.listErr
	m.mu.Unlock()
	if err != nil {
		return nil, err
	}
	out := m.scoped(ctx, func(r *models.UserAccount) bool { return r.UserUUID == user })
	sort.Slice(out, func(i, j int) bool { return out[i].LinkedAt.Before(out[j].LinkedAt) })
	return out, nil
}

func (m *memAccounts) ListAdmin(ctx context.Context) ([]models.UserAccount, error) {
	out := m.scoped(ctx, func(*models.UserAccount) bool { return true })
	sort.Slice(out, func(i, j int) bool { return out[i].LinkedAt.After(out[j].LinkedAt) })
	return out, nil
}

func (m *memAccounts) Get(ctx context.Context, id string) (*models.UserAccount, error) {
	out := m.scoped(ctx, func(r *models.UserAccount) bool { return r.UUID == id })
	if len(out) == 0 {
		return nil, repository.ErrNotFound
	}
	return &out[0], nil
}

func (m *memAccounts) GetSelected(ctx context.Context, user, provider string) (*models.UserAccount, error) {
	out := m.scoped(ctx, func(r *models.UserAccount) bool { return r.UserUUID == user && r.Provider == provider && r.Selected })
	if len(out) == 0 {
		return nil, repository.ErrNotFound
	}
	return &out[0], nil
}

func (m *memAccounts) Select(ctx context.Context, user, provider, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.rows[id]
	if !ok || t.TenantID != tenantOf(ctx) || t.UserUUID != user || t.Provider != provider {
		return repository.ErrNotFound
	}
	for _, r := range m.rows {
		if r.TenantID == t.TenantID && r.UserUUID == user && r.Provider == provider {
			r.Selected = r.UUID == id
		}
	}
	return nil
}

func (m *memAccounts) Delete(ctx context.Context, id string) (*models.UserAccount, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rows[id]
	if !ok || r.TenantID != tenantOf(ctx) {
		return nil, repository.ErrNotFound
	}
	if hook := m.beforeDelete; hook != nil {
		m.beforeDelete = nil
		hook(r)
	}
	delete(m.rows, id)
	cp := *r
	return &cp, nil
}

func (m *memAccounts) mutateScoped(ctx context.Context, id string, fn func(*models.UserAccount) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rows[id]
	if !ok || r.TenantID != tenantOf(ctx) {
		return repository.ErrNotFound
	}
	return fn(r)
}

func (m *memAccounts) SetLastUsed(ctx context.Context, id string, at time.Time) error {
	return m.mutateScoped(ctx, id, func(r *models.UserAccount) error { r.LastUsedAt = &at; return nil })
}

func (m *memAccounts) SetStatus(ctx context.Context, id, status, lastError string) error {
	return m.mutateScoped(ctx, id, func(r *models.UserAccount) error { r.Status, r.LastError = status, lastError; return nil })
}

func (m *memAccounts) SetStatusIf(ctx context.Context, id string, prev time.Time, status, lastError string) error {
	return m.mutateScoped(ctx, id, func(r *models.UserAccount) error {
		if !r.SavedAt.Equal(prev) {
			return repository.ErrStaleTokenSet
		}
		r.Status, r.LastError = status, lastError
		return nil
	})
}

func (m *memAccounts) ReplaceTokens(ctx context.Context, id string, prev time.Time, set models.AccountTokenSet) error {
	return m.mutateScoped(ctx, id, func(r *models.UserAccount) error {
		if hook := m.beforeReplace; hook != nil {
			m.beforeReplace = nil
			hook(r)
		}
		if !r.SavedAt.Equal(prev) {
			return repository.ErrStaleTokenSet
		}
		set.SavedAt = set.SavedAt.Truncate(time.Millisecond)
		applySet(r, set)
		return nil
	})
}

// row returns a copy of a stored row, whatever its tenant (test reads).
func (m *memAccounts) row(id string) (models.UserAccount, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rows[id]
	if !ok {
		return models.UserAccount{}, false
	}
	return *r, true
}

func (m *memAccounts) edit(id string, fn func(*models.UserAccount)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	fn(m.rows[id])
}

func (m *memAccounts) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.rows)
}

// fakeOIDC scripts the OpenAI endpoints. Synthetic tokens only. Refresh
// rotates: presenting an already-used refresh token answers
// refresh_token_reused, exactly what a lost single-flight would cause.
type fakeOIDC struct {
	mu sync.Mutex

	exchanges, refreshes, planCalls int
	scope                           string
	sub                             string
	refreshErr                      error
	revokeErr                       error
	refreshDelay                    time.Duration
	refreshOmitsOptional            bool // no refresh_token, scope, id_token, expires_in
	refreshEchoes                   bool // answers with the presented refresh token (no rotation)
	revoked                         []string
	models                          []string
	rejectAT                        string // ListPlanModels answers invalid_user for it
	beforeRefreshAnswer             func() // runs (unlocked) before Refresh answers
	beforeExchangeAnswer            func() // runs (unlocked) before Exchange answers
	usedRT                          map[string]bool
	n                               int

	lastAuthorize            AuthorizeParams
	lastVerifier, lastRedir  string
	lastClient               string
	lastAud, lastNonce       string
	lastRefreshClient        string
	lastPlanToken            string
	lastExchangeCodeVerified bool
}

func (f *fakeOIDC) locked(fn func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn()
}

func (f *fakeOIDC) AuthorizeURL(p AuthorizeParams) string {
	f.mu.Lock()
	f.lastAuthorize = p
	f.mu.Unlock()
	q := url.Values{"client_id": {p.ClientID}, "state": {p.State}, "nonce": {p.Nonce}, "redirect_uri": {p.RedirectURI}, "code_challenge": {p.CodeChallenge}}
	if p.IDTokenHint != "" {
		q.Set("id_token_hint", "present")
	}
	if p.AgentNameHint != "" {
		q.Set("agent_name_hint", p.AgentNameHint)
	}
	return "https://auth.example.test/authorize?" + q.Encode()
}

func (f *fakeOIDC) Exchange(ctx context.Context, clientID, code, verifier, redirect string) (*TokenResponse, error) {
	f.mu.Lock()
	hook := f.beforeExchangeAnswer
	f.beforeExchangeAnswer = nil
	f.mu.Unlock()
	if hook != nil {
		hook()
	}
	// The token request dies with its context, as the real client's does.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.exchanges++
	f.lastClient, f.lastVerifier, f.lastRedir = clientID, verifier, redirect
	f.lastExchangeCodeVerified = utils.GenerateCodeChallenge(verifier) == f.lastAuthorize.CodeChallenge
	return &TokenResponse{AccessToken: "at-" + code, RefreshToken: "rt-" + code, IDToken: "idt:" + f.sub + ":" + code, TokenType: "Bearer", ExpiresIn: 3600, Scope: f.scope}, nil
}

func (f *fakeOIDC) Refresh(_ context.Context, clientID, rt string) (*TokenResponse, error) {
	f.mu.Lock()
	f.refreshes++
	f.lastRefreshClient = clientID
	if hook := f.beforeRefreshAnswer; hook != nil {
		f.beforeRefreshAnswer = nil
		f.mu.Unlock()
		hook()
		f.mu.Lock()
	}
	if f.refreshErr != nil {
		err := f.refreshErr
		f.mu.Unlock()
		return nil, err
	}
	if f.usedRT == nil {
		f.usedRT = map[string]bool{}
	}
	if f.usedRT[rt] {
		f.mu.Unlock()
		return nil, providers.ClassifySIWCCode("refresh_token_reused")
	}
	if !f.refreshEchoes {
		f.usedRT[rt] = true
	}
	f.n++
	n, delay, omit, scope, sub := f.n, f.refreshDelay, f.refreshOmitsOptional, f.scope, f.sub
	nextRT := fmt.Sprintf("rt-r%d", n)
	if f.refreshEchoes {
		nextRT = rt
	}
	f.mu.Unlock()
	time.Sleep(delay)
	if omit {
		return &TokenResponse{AccessToken: fmt.Sprintf("at-r%d", n)}, nil
	}
	return &TokenResponse{AccessToken: fmt.Sprintf("at-r%d", n), RefreshToken: nextRT, IDToken: "idt:" + sub + ":r", TokenType: "Bearer", ExpiresIn: 3600, Scope: scope}, nil
}

func (f *fakeOIDC) Revoke(_ context.Context, _ string, rt string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.revoked = append(f.revoked, rt)
	return f.revokeErr
}

func (f *fakeOIDC) VerifyIDToken(_ context.Context, raw, aud, nonce string) (*IDClaims, error) {
	f.mu.Lock()
	f.lastAud, f.lastNonce = aud, nonce
	f.mu.Unlock()
	parts := strings.Split(raw, ":")
	if len(parts) != 3 || aud == "" || nonce == "" {
		return nil, ErrSIWCIDTokenInvalid
	}
	return &IDClaims{Issuer: SIWCIssuer, Subject: parts[1], Email: parts[1] + "@example.test", Nonce: nonce, Audience: []string{aud}, ExpiresAt: time.Now().Add(time.Hour)}, nil
}

func (f *fakeOIDC) ListPlanModels(_ context.Context, accessToken string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.planCalls++
	f.lastPlanToken = accessToken
	if f.rejectAT != "" && accessToken == f.rejectAT {
		return nil, fmt.Errorf("%w: %w", ErrSIWCInvalidUser, iface.ErrLLMProviderUnavailable)
	}
	return append([]string(nil), f.models...), nil
}

type fixedHost struct{}

func (fixedHost) GetOrCreateHostID(context.Context) (string, error) { return "urn:uuid:host-1", nil }

// switchHost is a host id source a test can change or break.
type switchHost struct {
	mu  sync.Mutex
	id  string
	err error
}

func (h *switchHost) GetOrCreateHostID(context.Context) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.id, h.err
}

func (h *switchHost) set(id string, err error) {
	h.mu.Lock()
	h.id, h.err = id, err
	h.mu.Unlock()
}

// pasteRedirect is the hosted_paste redirect of hostedPasteConfig.
const pasteRedirect = "http://127.0.0.1:14555" + SIWCCallbackPath

type syncAudit struct {
	mu     sync.Mutex
	events []iface.AuditEvent
}

func (a *syncAudit) Emit(_ context.Context, e iface.AuditEvent) {
	a.mu.Lock()
	a.events = append(a.events, e)
	a.mu.Unlock()
}

func (a *syncAudit) actions() []iface.AuditEvent {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]iface.AuditEvent(nil), a.events...)
}

// --- harness -----------------------------------------------------------------

type accountsHarness struct {
	svc   *AccountsService
	repo  *memAccounts
	oidc  *fakeOIDC
	redis *memRedis
	vault *Vault
	audit *syncAudit
	cfg   SIWCConfig
	host  *switchHost
}

func hostedPasteConfig(mode string) SIWCConfig {
	return SIWCConfig{Mode: mode, PastePort: 14555, OperatorPublicURL: "http://127.0.0.1:3007", AllowHosted: true}
}

func newAccountsHarness(t *testing.T, cfg SIWCConfig, oidc *fakeOIDC) *accountsHarness {
	t.Helper()
	v, err := NewVault(randomKeyHex(t), slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	h := &accountsHarness{repo: newMemAccounts(), oidc: oidc, redis: newMemRedis(), vault: v, audit: &syncAudit{}, cfg: cfg, host: &switchHost{id: "urn:uuid:host-1"}}
	h.svc = h.replica()
	return h
}

// replica builds another AccountsService over the same storage, Redis,
// vault and provider: a second backend replica.
func (h *accountsHarness) replica() *AccountsService {
	svc := NewAccountsService(AccountsDeps{
		Repo: h.repo, Host: h.host, OIDC: h.oidc, Plans: h.oidc, State: NewSIWCStateStore(h.redis, h.vault), Vault: h.vault,
		Limiter: NewRateLimiter(h.redis, slog.Default()), Redis: h.redis, Config: func() SIWCConfig { return h.cfg }, Logger: slog.Default(),
	})
	svc.pollEvery = 5 * time.Millisecond
	svc.sealRetryEvery = time.Millisecond
	svc.SetAuditSink(h.audit)
	return svc
}

func newAccounts(t *testing.T, mode string, oidc *fakeOIDC) (*AccountsService, *memAccounts) {
	t.Helper()
	h := newAccountsHarness(t, hostedPasteConfig(mode), oidc)
	return h.svc, h.repo
}

func stateFrom(t *testing.T, authorizeURL string) string {
	t.Helper()
	u, err := url.Parse(authorizeURL)
	if err != nil {
		t.Fatal(err)
	}
	return u.Query().Get("state")
}

// linkAccount runs a whole first link for the context's user.
func (h *accountsHarness) linkAccount(t *testing.T, ctx context.Context, clientID, code string) *models.UserAccount {
	t.Helper()
	link, err := h.svc.Link(ctx, "", "")
	if err != nil {
		t.Fatalf("link: %v", err)
	}
	acc, err := h.svc.Complete(ctx, CallbackInput{Code: code, State: stateFrom(t, link.AuthorizeURL), ClientID: clientID, RedirectURI: pasteRedirect}, true)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	return acc
}

func (h *accountsHarness) expire(id string) {
	h.repo.edit(id, func(r *models.UserAccount) { r.ExpiresAt = time.Now().Add(-time.Minute) })
}

// --- link and completion -------------------------------------------------------

func TestAccounts_PasteFlowLinksActiveAndSelected(t *testing.T) {
	oidc := &fakeOIDC{scope: SIWCScope, sub: "sub-1", models: []string{"gpt-5.6-terra"}}
	h := newAccountsHarness(t, hostedPasteConfig(SIWCModeHostedPaste), oidc)
	ctx := ctxFor("t1")
	link, err := h.svc.Link(ctx, "Work", "")
	if err != nil || link.Mode != SIWCModeHostedPaste {
		t.Fatalf("link mode = %v, err = %v", link != nil && link.Mode == SIWCModeHostedPaste, err)
	}
	var authorize AuthorizeParams
	oidc.locked(func() { authorize = oidc.lastAuthorize })
	if authorize.ClientID != SIWCDynamicClientID || authorize.AgentNameHint != SIWCAgentNameHint || authorize.HostID != "urn:uuid:host-1" ||
		authorize.IDTokenHint != "" || authorize.RedirectURI != "http://127.0.0.1:14555"+SIWCCallbackPath {
		t.Fatalf("authorize params: client=%q hint=%q host=%q redirect=%q", authorize.ClientID, authorize.AgentNameHint, authorize.HostID, authorize.RedirectURI)
	}
	if len(authorize.State) < 32 || len(authorize.Nonce) < 32 || authorize.CodeChallenge == "" || authorize.State == authorize.Nonce {
		t.Fatal("state, nonce and challenge must be fresh random values")
	}
	state := stateFrom(t, link.AuthorizeURL)
	pasted := "http://127.0.0.1:14555/v1/llm/accounts/openai/callback?code=c1&state=" + state + "&client_id=oaiapp_1"
	in, err := h.svc.ParsePastedCallback(pasted)
	if err != nil {
		t.Fatal(err)
	}
	acc, err := h.svc.Complete(ctx, in, true)
	if err != nil {
		t.Fatal(err)
	}
	if acc.Status != models.AccountStatusActive || !acc.Selected || !acc.PlanUsageGranted || acc.ClientID != "oaiapp_1" ||
		acc.Subject != "sub-1" || acc.Label != "Work" || acc.TenantID != "t1" || acc.UserUUID != "u-admin" || acc.ExtAgentHostID != "urn:uuid:host-1" {
		t.Fatalf("account: status=%s selected=%v plan=%v client=%s sub=%s label=%s", acc.Status, acc.Selected, acc.PlanUsageGranted, acc.ClientID, acc.Subject, acc.Label)
	}
	if acc.AccessToken.IsZero() || acc.RefreshToken.IsZero() || acc.IDToken.IsZero() {
		t.Fatal("tokens must be sealed")
	}
	if d := time.Until(acc.RefreshExpiresAt); d < models.RefreshTokenLifetime-time.Minute {
		t.Fatalf("refresh expiry in %s", d)
	}
	oidc.locked(func() {
		// The exchange used the issued client id, the attempt's verifier
		// (whose S256 is the challenge sent) and redirect; the ID token was
		// checked against the issued client id and the attempt's nonce.
		if oidc.lastClient != "oaiapp_1" || !oidc.lastExchangeCodeVerified || oidc.lastRedir != authorize.RedirectURI {
			t.Errorf("exchange: client=%q pkce=%v redirect=%q", oidc.lastClient, oidc.lastExchangeCodeVerified, oidc.lastRedir)
		}
		if oidc.lastAud != "oaiapp_1" || oidc.lastNonce != authorize.Nonce {
			t.Errorf("id token checked with aud=%q, nonce match=%v", oidc.lastAud, oidc.lastNonce == authorize.Nonce)
		}
	})
	if _, err := h.svc.Complete(ctx, in, true); !errors.Is(err, ErrSIWCStateInvalid) {
		t.Fatalf("replay err = %v", err)
	}
	cred, err := h.svc.CredentialFor(ctx, "u-admin")
	if err != nil || cred.AccessToken != "at-c1" {
		t.Fatalf("credential ok = %v, err = %v", cred != nil && cred.AccessToken == "at-c1", err)
	}
	if h.repo.count() != 1 {
		t.Fatalf("rows = %d", h.repo.count())
	}
}

// Review focus 1: a URL pasted by another user is state_invalid, and the
// owner's attempt is consumed by it.
func TestAccounts_PasteByAnotherUserIsRejectedAndConsumed(t *testing.T) {
	oidc := &fakeOIDC{scope: SIWCScope, sub: "sub-1"}
	svc, repo := newAccounts(t, SIWCModeHostedPaste, oidc)
	link, err := svc.Link(ctxFor("t1"), "", "")
	if err != nil {
		t.Fatal(err)
	}
	in := CallbackInput{Code: "c", State: stateFrom(t, link.AuthorizeURL), ClientID: "oaiapp_9", RedirectURI: pasteRedirect}
	if _, err := svc.Complete(ctxForUser("t1", "u-other"), in, true); !errors.Is(err, ErrSIWCStateInvalid) {
		t.Fatalf("other user err = %v", err)
	}
	if _, err := svc.Complete(ctxFor("t1"), in, true); !errors.Is(err, ErrSIWCStateInvalid) {
		t.Fatalf("owner after consumption err = %v", err)
	}
	// The same user in another org is not the attempt's owner either.
	link2, _ := svc.Link(ctxFor("t1"), "", "")
	in2 := CallbackInput{Code: "c", State: stateFrom(t, link2.AuthorizeURL), ClientID: "oaiapp_9", RedirectURI: pasteRedirect}
	if _, err := svc.Complete(ctxFor("t2"), in2, true); !errors.Is(err, ErrSIWCStateInvalid) {
		t.Fatalf("other org err = %v", err)
	}
	oidc.locked(func() {
		if oidc.exchanges != 0 {
			t.Error("no exchange must happen on a rejected state")
		}
	})
	if repo.count() != 0 {
		t.Fatal("nothing may be stored")
	}
}

func TestAccounts_PasteURLMustMatchRedirect(t *testing.T) {
	oidc := &fakeOIDC{scope: SIWCScope, sub: "sub-1"}
	svc, _ := newAccounts(t, SIWCModeHostedPaste, oidc)
	expected := "http://127.0.0.1:14555/v1/llm/accounts/openai/callback"
	// Refused by the parser itself: nothing is consumed.
	malformed := []string{
		"http://u:p@127.0.0.1:14555/v1/llm/accounts/openai/callback?code=c&state=s",
		"http://127.0.0.1:14555/v1/llm/accounts/openai/callback?state=s", // no code, no error
		"http://127.0.0.1:14555/v1/llm/accounts/openai/callback?code=c",  // no state
		"/v1/llm/accounts/openai/callback?code=c&state=s",                // not absolute
		"not a url",
		"",
		expected + "?code=c&state=s&pad=" + strings.Repeat("x", pastedCallbackMaxLen),
	}
	for _, raw := range malformed {
		if _, err := svc.ParsePastedCallback(raw); !errors.Is(err, ErrSIWCCallbackInvalid) {
			t.Errorf("%.80q accepted: %v", raw, err)
		}
	}
	in, err := svc.ParsePastedCallback("  " + expected + "?error=access_denied&state=s#frag  ")
	if err != nil || in.Error != "access_denied" || in.State != "s" || in.RedirectURI != expected {
		t.Fatalf("denied parse = %+v, %v", in, err)
	}
	// Parsed, then refused by Complete against the attempt's own redirect
	// (ruling 5.17): the attempt is consumed and nothing is exchanged.
	ctx := ctxFor("t1")
	for _, base := range []string{
		"http://localhost:14555/v1/llm/accounts/openai/callback",
		"http://127.0.0.1:1455/v1/llm/accounts/openai/callback",
		"https://127.0.0.1:14555/v1/llm/accounts/openai/callback",
		"http://127.0.0.1:14555/other",
		"http://127.0.0.1:14555/v1/llm/accounts/openai/callback/",
	} {
		link, err := svc.Link(ctx, "", "")
		if err != nil {
			t.Fatal(err)
		}
		state := stateFrom(t, link.AuthorizeURL)
		in, err := svc.ParsePastedCallback(base + "?code=c&client_id=oaiapp_1&state=" + state)
		if err != nil {
			t.Fatalf("%s: parse err = %v", base, err)
		}
		if _, err := svc.Complete(ctx, in, true); !errors.Is(err, ErrSIWCCallbackInvalid) {
			t.Fatalf("%s: complete err = %v", base, err)
		}
		in.RedirectURI = expected
		if _, err := svc.Complete(ctx, in, true); !errors.Is(err, ErrSIWCStateInvalid) {
			t.Fatalf("%s: the attempt must be consumed, err = %v", base, err)
		}
	}
	oidc.locked(func() {
		if oidc.exchanges != 0 {
			t.Error("exchanged on a mismatched redirect")
		}
	})
}

func TestAccounts_CompleteRefusesARedirectOtherThanTheAttempts(t *testing.T) {
	oidc := &fakeOIDC{scope: SIWCScope, sub: "sub-1"}
	svc, _ := newAccounts(t, SIWCModeHostedPaste, oidc)
	ctx := ctxFor("t1")
	// Mandatory: an unstated redirect fails closed like a wrong one.
	for _, redirect := range []string{"http://127.0.0.1:9999" + SIWCCallbackPath, ""} {
		link, _ := svc.Link(ctx, "", "")
		in := CallbackInput{Code: "c", State: stateFrom(t, link.AuthorizeURL), ClientID: "oaiapp_1", RedirectURI: redirect}
		if _, err := svc.Complete(ctx, in, true); !errors.Is(err, ErrSIWCCallbackInvalid) {
			t.Fatalf("redirect %q: err = %v", redirect, err)
		}
	}
	oidc.locked(func() {
		if oidc.exchanges != 0 {
			t.Error("exchanged on a mismatched redirect")
		}
	})
}

// The attempt is bound to the installation's host id.
func TestAccounts_CompleteRefusesAStateOfAnotherInstallation(t *testing.T) {
	oidc := &fakeOIDC{scope: SIWCScope, sub: "sub-1"}
	h := newAccountsHarness(t, hostedPasteConfig(SIWCModeHostedPaste), oidc)
	ctx := ctxFor("t1")
	for _, c := range []struct {
		id  string
		err error
	}{
		{"urn:uuid:host-2", nil},
		{"", errors.New("installation unreadable")},
	} {
		h.host.set("urn:uuid:host-1", nil)
		link, err := h.svc.Link(ctx, "", "")
		if err != nil {
			t.Fatal(err)
		}
		in := CallbackInput{Code: "c", State: stateFrom(t, link.AuthorizeURL), ClientID: "oaiapp_1", RedirectURI: pasteRedirect}
		h.host.set(c.id, c.err)
		if _, err := h.svc.Complete(ctx, in, true); !errors.Is(err, ErrSIWCStateInvalid) {
			t.Fatalf("host %q / %v: err = %v", c.id, c.err, err)
		}
		h.host.set("urn:uuid:host-1", nil)
		if _, err := h.svc.Complete(ctx, in, true); !errors.Is(err, ErrSIWCStateInvalid) {
			t.Fatalf("the rejected attempt must stay consumed: %v", err)
		}
	}
	oidc.locked(func() {
		if oidc.exchanges != 0 {
			t.Errorf("exchanges = %d", oidc.exchanges)
		}
	})
	if h.repo.count() != 0 {
		t.Fatal("nothing may be stored")
	}
}

// A new link is selected whenever the user has no selected registration,
// e.g. after unlinking the selected one while another remains.
func TestAccounts_LinkAfterUnlinkingTheSelectedIsSelected(t *testing.T) {
	oidc := &fakeOIDC{scope: SIWCScope, sub: "sub-1"}
	h := newAccountsHarness(t, hostedPasteConfig(SIWCModeHostedPaste), oidc)
	ctx := ctxFor("t1")
	a := h.linkAccount(t, ctx, "oaiapp_1", "a")
	b := h.linkAccount(t, ctx, "oaiapp_2", "b")
	if !a.Selected || b.Selected {
		t.Fatal("first link selected, second not")
	}
	if err := h.svc.Unlink(ctx, a.UUID, false); err != nil {
		t.Fatal(err)
	}
	c := h.linkAccount(t, ctx, "oaiapp_3", "c")
	if !c.Selected {
		t.Fatal("with no selected registration left, the new link is selected")
	}
	if cred, err := h.svc.CredentialFor(ctx, "u-admin"); err != nil || cred.Account.UUID != c.UUID {
		t.Fatalf("credential err = %v", err)
	}
}

func TestAccounts_DeniedAndMissingClientIDCloseWithoutExchange(t *testing.T) {
	oidc := &fakeOIDC{scope: SIWCScope, sub: "sub-1"}
	h := newAccountsHarness(t, hostedPasteConfig(SIWCModeHostedPaste), oidc)
	ctx := ctxFor("t1")
	for _, c := range []struct {
		in   CallbackInput
		want error
	}{
		{CallbackInput{Error: "access_denied", RedirectURI: pasteRedirect}, ErrSIWCDenied},
		{CallbackInput{Error: "server_error", RedirectURI: pasteRedirect}, ErrSIWCExchangeFailed},
		{CallbackInput{Code: "c", RedirectURI: pasteRedirect}, ErrSIWCExchangeFailed}, // first link without an issued client id
	} {
		link, err := h.svc.Link(ctx, "", "")
		if err != nil {
			t.Fatal(err)
		}
		c.in.State = stateFrom(t, link.AuthorizeURL)
		if _, err := h.svc.Complete(ctx, c.in, true); !errors.Is(err, c.want) {
			t.Errorf("%+v: err = %v", c.in, err)
		}
	}
	oidc.locked(func() {
		if oidc.exchanges != 0 {
			t.Errorf("exchanges = %d", oidc.exchanges)
		}
	})
	denied := false
	for _, e := range h.audit.actions() {
		denied = denied || e.Action == "llm.account.link_denied"
	}
	if !denied {
		t.Fatal("a denial is audited")
	}
}

// Review focus 4: a valid ID token without chatgpt.tokens.use.direct is a
// saved plan_disabled registration that is never eligible (no provider call).
func TestAccounts_PlanDisabledWithoutScope(t *testing.T) {
	oidc := &fakeOIDC{scope: "openid profile email offline_access", sub: "sub-1", models: []string{"gpt-x"}}
	h := newAccountsHarness(t, hostedPasteConfig(SIWCModeHostedPaste), oidc)
	ctx := ctxFor("t1")
	acc := h.linkAccount(t, ctx, "oaiapp_1", "c")
	if acc.Status != models.AccountStatusPlanDisabled || acc.PlanUsageGranted {
		t.Fatalf("status = %s plan = %v", acc.Status, acc.PlanUsageGranted)
	}
	if stored, ok := h.repo.row(acc.UUID); !ok || stored.Status != models.AccountStatusPlanDisabled {
		t.Fatal("the registration must be saved as plan_disabled")
	}
	if _, err := h.svc.CredentialFor(ctx, "u-admin"); !errors.Is(err, iface.ErrLLMPlanUsageDisabled) {
		t.Fatalf("credential err = %v", err)
	}
	h.expire(acc.UUID)
	if _, err := h.svc.CredentialFor(ctx, "u-admin"); !errors.Is(err, iface.ErrLLMPlanUsageDisabled) {
		t.Fatalf("expired credential err = %v", err)
	}
	oidc.locked(func() {
		if oidc.refreshes != 0 || oidc.planCalls != 0 {
			t.Errorf("provider called: refreshes=%d plan=%d", oidc.refreshes, oidc.planCalls)
		}
	})
}

// Review focus 2: a reauth that comes back under another issued client id
// (or another ChatGPT identity) is dropped; the registration is untouched.
func TestAccounts_ReauthRejectsDifferentClientOrSubject(t *testing.T) {
	oidc := &fakeOIDC{scope: SIWCScope, sub: "sub-1"}
	h := newAccountsHarness(t, hostedPasteConfig(SIWCModeHostedPaste), oidc)
	ctx := ctxFor("t1")
	acc := h.linkAccount(t, ctx, "oaiapp_1", "c")
	re, err := h.svc.Link(ctx, "", acc.UUID)
	if err != nil || !strings.Contains(re.AuthorizeURL, "id_token_hint=present") || !strings.Contains(re.AuthorizeURL, "client_id=oaiapp_1") ||
		strings.Contains(re.AuthorizeURL, "agent_name_hint") {
		t.Fatalf("reauth link err = %v", err)
	}
	var hint, login string
	oidc.locked(func() { hint, login = oidc.lastAuthorize.IDTokenHint, oidc.lastAuthorize.LoginHint })
	if hint != "idt:sub-1:c" || login != "sub-1@example.test" {
		t.Fatal("reauth must hint the stored ID token and email")
	}
	before, _ := h.repo.row(acc.UUID)
	var exchanges int
	oidc.locked(func() { exchanges = oidc.exchanges })
	if _, err := h.svc.Complete(ctx, CallbackInput{Code: "c2", State: stateFrom(t, re.AuthorizeURL), ClientID: "oaiapp_2", RedirectURI: pasteRedirect}, true); !errors.Is(err, ErrSIWCIDTokenInvalid) {
		t.Fatalf("different client err = %v", err)
	}
	oidc.locked(func() {
		if oidc.exchanges != exchanges {
			t.Error("a mismatched client id must not be exchanged")
		}
	})
	if got, _ := h.repo.row(acc.UUID); got.AccessToken.Ciphertext != before.AccessToken.Ciphertext || !got.SavedAt.Equal(before.SavedAt) {
		t.Fatal("rejected reauth must not touch the stored registration")
	}
	oidc.locked(func() { oidc.sub = "sub-2" })
	re2, _ := h.svc.Link(ctx, "", acc.UUID)
	if _, err := h.svc.Complete(ctx, CallbackInput{Code: "c3", State: stateFrom(t, re2.AuthorizeURL), RedirectURI: pasteRedirect}, true); !errors.Is(err, ErrSIWCIDTokenInvalid) {
		t.Fatalf("different subject err = %v", err)
	}
	if got, _ := h.repo.row(acc.UUID); got.AccessToken.Ciphertext != before.AccessToken.Ciphertext || got.Subject != "sub-1" {
		t.Fatal("a foreign identity must not touch the stored registration")
	}
	oidc.locked(func() {
		if len(oidc.revoked) != 1 || oidc.revoked[0] != "rt-c3" {
			t.Errorf("the dropped token set must be revoked (revocations = %d)", len(oidc.revoked))
		}
	})
	if h.repo.count() != 1 {
		t.Fatalf("rows = %d", h.repo.count())
	}
}

func TestAccounts_ReauthRestoresTheRegistration(t *testing.T) {
	oidc := &fakeOIDC{scope: SIWCScope, sub: "sub-1"}
	h := newAccountsHarness(t, hostedPasteConfig(SIWCModeHostedPaste), oidc)
	ctx := ctxFor("t1")
	acc := h.linkAccount(t, ctx, "oaiapp_1", "c")
	h.svc.MarkReauth(ctx, acc.UUID, "not a code")
	if got, _ := h.repo.row(acc.UUID); got.Status != models.AccountStatusNeedsReauth || got.LastError != errcode.LLMUserAccountReauthRequired {
		t.Fatalf("status = %s / %s", got.Status, got.LastError)
	}
	if _, err := h.svc.CredentialFor(ctx, "u-admin"); !errors.Is(err, iface.ErrLLMUserAccountReauth) {
		t.Fatalf("credential err = %v", err)
	}
	re, err := h.svc.Link(ctx, "", acc.UUID)
	if err != nil {
		t.Fatal(err)
	}
	// The issued client id may be omitted on a reauth callback.
	got, err := h.svc.Complete(ctx, CallbackInput{Code: "c2", State: stateFrom(t, re.AuthorizeURL), RedirectURI: pasteRedirect}, true)
	if err != nil || got.UUID != acc.UUID || got.Status != models.AccountStatusActive || got.LastError != "" || got.Label != acc.Label {
		t.Fatalf("reauth err = %v", err)
	}
	cred, err := h.svc.CredentialFor(ctx, "u-admin")
	if err != nil || cred.AccessToken != "at-c2" {
		t.Fatalf("credential after reauth ok = %v, err = %v", cred != nil && cred.AccessToken == "at-c2", err)
	}
	// Another user cannot start a reauth of this registration.
	if _, err := h.svc.Link(ctxForUser("t1", "u-other"), "", acc.UUID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign reauth err = %v", err)
	}
}

// The public loopback callback has no session: org and user come from the
// attempt, and the audit actor is the attempt's user.
func TestAccounts_LoopbackCallbackUsesTheAttemptsOrgAndUser(t *testing.T) {
	oidc := &fakeOIDC{scope: SIWCScope, sub: "sub-1"}
	h := newAccountsHarness(t, SIWCConfig{Mode: SIWCModeLoopbackLocal, OperatorPublicURL: "http://127.0.0.1:3007", AllowHosted: true}, oidc)
	link, err := h.svc.Link(ctxFor("t1"), "", "")
	if err != nil || link.Mode != SIWCModeLoopbackLocal {
		t.Fatalf("link err = %v", err)
	}
	if !strings.Contains(link.AuthorizeURL, url.QueryEscape("http://127.0.0.1:3007"+SIWCCallbackPath)) {
		t.Fatal("loopback_local redirects to the API's own port")
	}
	acc, err := h.svc.Complete(context.Background(), CallbackInput{Code: "c", State: stateFrom(t, link.AuthorizeURL), ClientID: "oaiapp_1", RedirectURI: "http://127.0.0.1:3007" + SIWCCallbackPath}, false)
	if err != nil || acc.TenantID != "t1" || acc.UserUUID != "u-admin" {
		t.Fatalf("callback err = %v", err)
	}
	var linked *iface.AuditEvent
	for _, e := range h.audit.actions() {
		if e.Action == "llm.account.linked" {
			linked = &e
		}
	}
	if linked == nil || linked.ActorUserID != "u-admin" || linked.TenantID != "t1" {
		t.Fatal("linked event must carry the attempt's user and org")
	}
}

func TestAccounts_SameClientIDRelinksInsteadOfDuplicating(t *testing.T) {
	oidc := &fakeOIDC{scope: SIWCScope, sub: "sub-1"}
	h := newAccountsHarness(t, hostedPasteConfig(SIWCModeHostedPaste), oidc)
	ctx := ctxFor("t1")
	first := h.linkAccount(t, ctx, "oaiapp_1", "c1")
	again := h.linkAccount(t, ctx, "oaiapp_1", "c2")
	if again.UUID != first.UUID || h.repo.count() != 1 {
		t.Fatalf("relink created a second row (rows = %d)", h.repo.count())
	}
	if cred, err := h.svc.CredentialFor(ctx, "u-admin"); err != nil || cred.AccessToken != "at-c2" {
		t.Fatalf("relink must store the new set (err = %v)", err)
	}
	// Same issued client id, another identity: refused (email equality
	// would not merge either).
	oidc.locked(func() { oidc.sub = "sub-2" })
	link, _ := h.svc.Link(ctx, "", "")
	if _, err := h.svc.Complete(ctx, CallbackInput{Code: "c3", State: stateFrom(t, link.AuthorizeURL), ClientID: "oaiapp_1", RedirectURI: pasteRedirect}, true); !errors.Is(err, ErrSIWCIDTokenInvalid) {
		t.Fatalf("foreign identity err = %v", err)
	}
	// A second registration (another issued client id) is linked but not
	// selected; Select switches.
	second := h.linkAccount(t, ctx, "oaiapp_2", "c4")
	if second.Selected || second.UUID == first.UUID {
		t.Fatal("a second registration must not take the selection")
	}
	if err := h.svc.Select(ctx, second.UUID); err != nil {
		t.Fatal(err)
	}
	if a, _ := h.repo.row(first.UUID); a.Selected {
		t.Fatal("previous selection kept")
	}
	if err := h.svc.Select(ctxForUser("t1", "u-other"), second.UUID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign select err = %v", err)
	}
	list, err := h.svc.List(ctx)
	if err != nil || len(list) != 2 {
		t.Fatalf("list = %d, %v", len(list), err)
	}
	if other, _ := h.svc.List(ctxForUser("t1", "u-other")); len(other) != 0 {
		t.Fatal("List returns the caller's own registrations only")
	}
	if all, _ := h.svc.ListAdmin(ctx); len(all) != 2 {
		t.Fatal("ListAdmin lists the org")
	}
	if all, _ := h.svc.ListAdmin(ctxFor("t2")); len(all) != 0 {
		t.Fatal("ListAdmin is org-scoped")
	}
}

// Ruling 2.4: two first links racing on the selected index — the loser is
// stored unselected instead of failing.
func TestAccounts_FirstLinkRaceOnSelectionInsertsUnselected(t *testing.T) {
	oidc := &fakeOIDC{scope: SIWCScope, sub: "sub-1"}
	h := newAccountsHarness(t, hostedPasteConfig(SIWCModeHostedPaste), oidc)
	ctx := ctxFor("t1")
	link, _ := h.svc.Link(ctx, "", "")
	h.repo.beforeInsert = func(rows map[string]*models.UserAccount) {
		rows["racer"] = &models.UserAccount{UUID: "racer", TenantID: "t1", UserUUID: "u-admin", Provider: models.ProviderOpenAI, ClientID: "oaiapp_race", Selected: true}
	}
	acc, err := h.svc.Complete(ctx, CallbackInput{Code: "c", State: stateFrom(t, link.AuthorizeURL), ClientID: "oaiapp_1", RedirectURI: pasteRedirect}, true)
	if err != nil || acc.Selected {
		t.Fatalf("race err = %v selected = %v", err, acc != nil && acc.Selected)
	}
	if h.repo.count() != 2 {
		t.Fatalf("rows = %d", h.repo.count())
	}
}

func TestAccounts_LinkDisabledUnsupportedOrMisconfigured(t *testing.T) {
	svc, _ := newAccounts(t, SIWCModeDisabled, &fakeOIDC{})
	if _, err := svc.Link(ctxFor("t1"), "", ""); !errors.Is(err, ErrSIWCDisabled) {
		t.Fatalf("disabled err = %v", err)
	}
	for _, c := range []struct {
		cfg  SIWCConfig
		want error
	}{
		{SIWCConfig{Mode: SIWCModeLoopbackLocal, OperatorPublicURL: "https://console.example.test", AllowHosted: true}, ErrSIWCUnsupportedDeployment},
		{SIWCConfig{Mode: SIWCModeLoopbackLocal, OperatorPublicURL: "http://localhost:3007", AllowHosted: true}, ErrSIWCUnsupportedDeployment},
		{SIWCConfig{Mode: SIWCModeHostedPaste, PastePort: 14555, OperatorPublicURL: "http://127.0.0.1:3007"}, ErrSIWCConfigInvalid}, // hosted providers off
		{SIWCConfig{Mode: SIWCModeHostedPaste, PastePort: 80, OperatorPublicURL: "http://127.0.0.1:3007", AllowHosted: true}, ErrSIWCConfigInvalid},
	} {
		h := newAccountsHarness(t, c.cfg, &fakeOIDC{})
		_, err := h.svc.Link(ctxFor("t1"), "", "")
		var typed *SIWCConfigError
		if !errors.Is(err, c.want) || !errors.As(err, &typed) {
			t.Errorf("%+v: err = %v", c.cfg, err)
		}
	}
	h := newAccountsHarness(t, hostedPasteConfig(SIWCModeHostedPaste), &fakeOIDC{})
	if _, err := h.svc.Link(ctxFor("t1"), strings.Repeat("x", accountLabelMaxLen+1), ""); !errors.Is(err, iface.ErrLLMInvalidRequest) {
		t.Fatalf("long label err = %v", err)
	}
}

func TestAccounts_LinkIsRateLimitedPerUser(t *testing.T) {
	svc, _ := newAccounts(t, SIWCModeHostedPaste, &fakeOIDC{})
	for i := 0; i < SIWCLinkLimit; i++ {
		if _, err := svc.Link(ctxFor("t1"), "", ""); err != nil {
			t.Fatalf("link %d: %v", i, err)
		}
	}
	_, err := svc.Link(ctxFor("t1"), "", "")
	var rl *RateLimitedError
	if !errors.As(err, &rl) || rl.RetryAfter <= 0 {
		t.Fatalf("over the limit err = %v", err)
	}
	if _, err := svc.Link(ctxForUser("t1", "u-other"), "", ""); err != nil {
		t.Fatalf("another user is not limited: %v", err)
	}
}

func TestAccounts_LinkWithoutSecretKeyIsSecretKeyMissing(t *testing.T) {
	v, err := NewVault("", slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	redis := newMemRedis()
	svc := NewAccountsService(AccountsDeps{
		Repo: newMemAccounts(), Host: fixedHost{}, OIDC: &fakeOIDC{}, State: NewSIWCStateStore(redis, v), Vault: v,
		Limiter: NewRateLimiter(redis, slog.Default()), Config: func() SIWCConfig { return hostedPasteConfig(SIWCModeHostedPaste) },
	})
	if _, err := svc.Link(ctxFor("t1"), "", ""); !errors.Is(err, ErrSecretKeyMissing) {
		t.Fatalf("err = %v", err)
	}
}

// Audit events carry {mode, reauth} only: never the authorize URL (on a
// reauth it embeds the ID token), the state, a code or a token.
func TestAccounts_AuditEventsCarryNoURLOrToken(t *testing.T) {
	oidc := &fakeOIDC{scope: SIWCScope, sub: "sub-1"}
	h := newAccountsHarness(t, hostedPasteConfig(SIWCModeHostedPaste), oidc)
	ctx := ctxFor("t1")
	acc := h.linkAccount(t, ctx, "oaiapp_1", "code-1")
	re, err := h.svc.Link(ctx, "", acc.UUID)
	if err != nil {
		t.Fatal(err)
	}
	state := stateFrom(t, re.AuthorizeURL)
	if _, err := h.svc.Complete(ctx, CallbackInput{Code: "code-2", State: state, ClientID: "oaiapp_1", RedirectURI: pasteRedirect}, true); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.Unlink(ctx, acc.UUID, false); err != nil {
		t.Fatal(err)
	}
	starts := 0
	for _, e := range h.audit.actions() {
		dump := fmt.Sprintf("%+v", e)
		for _, secret := range []string{"http", "authorize", state, "code-1", "code-2", "idt:", "at-", "rt-"} {
			if strings.Contains(dump, secret) {
				t.Errorf("%s event leaks %q-like content", e.Action, secret[:min(len(secret), 4)])
			}
		}
		if e.Action == "llm.account.link_started" {
			starts++
			if len(e.Metadata) != 2 || e.Metadata["mode"] != SIWCModeHostedPaste {
				t.Errorf("link_started metadata keys = %d", len(e.Metadata))
			}
			if _, ok := e.Metadata["reauth"].(bool); !ok {
				t.Error("link_started carries reauth")
			}
		}
	}
	if starts != 2 {
		t.Fatalf("link_started events = %d", starts)
	}
}

// --- refresh -----------------------------------------------------------------

// Review focus 3: concurrent refreshes of one account are one rotation; the
// others wait and use the rotated set (no refresh_token_reused).
func TestAccounts_RefreshSingleFlightAndTerminal(t *testing.T) {
	oidc := &fakeOIDC{scope: SIWCScope, sub: "sub-1", refreshDelay: 30 * time.Millisecond}
	h := newAccountsHarness(t, hostedPasteConfig(SIWCModeHostedPaste), oidc)
	ctx := ctxFor("t1")
	acc := h.linkAccount(t, ctx, "oaiapp_1", "c")
	before, _ := h.repo.row(acc.UUID)
	h.expire(acc.UUID)
	var wg sync.WaitGroup
	tokens := make([]string, 8)
	for i := range tokens {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cred, err := h.svc.CredentialFor(ctx, "u-admin")
			if err != nil {
				t.Errorf("credential: %v", err)
				return
			}
			tokens[i] = cred.AccessToken
		}(i)
	}
	wg.Wait()
	oidc.locked(func() {
		if oidc.refreshes != 1 || oidc.lastRefreshClient != "oaiapp_1" {
			t.Errorf("refreshes = %d (want 1, single-flight), issued client used = %v", oidc.refreshes, oidc.lastRefreshClient == "oaiapp_1")
		}
	})
	for i, tok := range tokens {
		if tok != "at-r1" {
			t.Errorf("caller %d did not get the rotated token", i)
		}
	}
	after, _ := h.repo.row(acc.UUID)
	if after.RefreshToken.Ciphertext == before.RefreshToken.Ciphertext || after.AccessToken.Ciphertext == before.AccessToken.Ciphertext ||
		!after.SavedAt.After(before.SavedAt) || time.Until(after.ExpiresAt) < 59*time.Minute {
		t.Fatal("the token set must rotate as one")
	}

	// Terminal: needs_reauth and ErrLLMUserAccountReauth; the terminal
	// sentinel itself never leaves the module.
	h.expire(acc.UUID)
	oidc.locked(func() { oidc.refreshErr = ErrSIWCRefreshTerminal })
	_, err := h.svc.CredentialFor(ctx, "u-admin")
	if !errors.Is(err, iface.ErrLLMUserAccountReauth) || errors.Is(err, ErrSIWCRefreshTerminal) {
		t.Fatalf("terminal refresh err = %v", err)
	}
	if got, _ := h.repo.row(acc.UUID); got.Status != models.AccountStatusNeedsReauth || got.LastError != errcode.LLMUserAccountReauthRequired {
		t.Fatalf("status = %s / %s", got.Status, got.LastError)
	}

	// Transient: unavailable, credentials and status kept.
	h.repo.edit(acc.UUID, func(r *models.UserAccount) { r.Status, r.LastError = models.AccountStatusActive, "" })
	stored, _ := h.repo.row(acc.UUID)
	for _, transient := range []error{
		fmt.Errorf("%w: token endpoint answered 503", iface.ErrLLMProviderUnavailable),
		providers.ClassifySIWCCode("invalid_client"), // configuration, not tokens: no reauth
	} {
		oidc.locked(func() { oidc.refreshErr = transient })
		if _, err := h.svc.CredentialFor(ctx, "u-admin"); !errors.Is(err, iface.ErrLLMProviderUnavailable) {
			t.Fatalf("transient refresh err = %v", err)
		}
		got, _ := h.repo.row(acc.UUID)
		if got.Status != models.AccountStatusActive || got.RefreshToken.Ciphertext != stored.RefreshToken.Ciphertext || !got.SavedAt.Equal(stored.SavedAt) {
			t.Fatalf("transient refresh changed the registration (status %s)", got.Status)
		}
	}
}

// Two replicas: the Redis lease lets one rotate while the other re-reads
// the stored set.
func TestAccounts_RefreshLeaseAcrossReplicas(t *testing.T) {
	oidc := &fakeOIDC{scope: SIWCScope, sub: "sub-1", refreshDelay: 60 * time.Millisecond}
	h := newAccountsHarness(t, hostedPasteConfig(SIWCModeHostedPaste), oidc)
	ctx := ctxFor("t1")
	acc := h.linkAccount(t, ctx, "oaiapp_1", "c")
	h.expire(acc.UUID)
	replicas := []*AccountsService{h.svc, h.replica(), h.replica()}
	var wg sync.WaitGroup
	got := make([]string, len(replicas))
	for i, svc := range replicas {
		wg.Add(1)
		go func(i int, svc *AccountsService) {
			defer wg.Done()
			cred, err := svc.CredentialFor(ctx, "u-admin")
			if err != nil {
				t.Errorf("replica %d: %v", i, err)
				return
			}
			got[i] = cred.AccessToken
		}(i, svc)
	}
	wg.Wait()
	oidc.locked(func() {
		if oidc.refreshes != 1 {
			t.Errorf("refreshes = %d, want 1 across replicas", oidc.refreshes)
		}
	})
	for i, tok := range got {
		if tok != "at-r1" {
			t.Errorf("replica %d did not get the rotated token", i)
		}
	}
	if keys, _ := h.redis.Keys(context.Background(), accountRefreshLeasePrefix+"*"); len(keys) != 0 {
		t.Fatalf("lease left behind: %d", len(keys))
	}
}

func TestAccounts_RefreshKeepsWhatTheResponseOmits(t *testing.T) {
	oidc := &fakeOIDC{scope: SIWCScope, sub: "sub-1", refreshOmitsOptional: true}
	h := newAccountsHarness(t, hostedPasteConfig(SIWCModeHostedPaste), oidc)
	ctx := ctxFor("t1")
	acc := h.linkAccount(t, ctx, "oaiapp_1", "c")
	h.expire(acc.UUID)
	before, _ := h.repo.row(acc.UUID)
	cred, err := h.svc.CredentialFor(ctx, "u-admin")
	if err != nil || cred.AccessToken != "at-r1" {
		t.Fatalf("refresh err = %v", err)
	}
	after, _ := h.repo.row(acc.UUID)
	if after.RefreshToken != before.RefreshToken || !after.RefreshExpiresAt.Equal(before.RefreshExpiresAt) {
		t.Fatal("an omitted refresh_token keeps the stored one")
	}
	if after.IDToken != before.IDToken || after.TokenType != "Bearer" {
		t.Fatal("an omitted id_token / token_type keeps the stored one")
	}
	if strings.Join(after.Scopes, " ") != SIWCScope || !after.PlanUsageGranted || after.Status != models.AccountStatusActive {
		t.Fatalf("an omitted scope keeps the stored scopes (plan %v)", after.PlanUsageGranted)
	}
	if d := time.Until(after.ExpiresAt); d < 59*time.Minute || d > 61*time.Minute {
		t.Fatalf("expires_in 0 means the default hour, got %s", d)
	}
}

func TestAccounts_EarliestRefreshAtIsRespected(t *testing.T) {
	oidc := &fakeOIDC{scope: SIWCScope, sub: "sub-1"}
	h := newAccountsHarness(t, hostedPasteConfig(SIWCModeHostedPaste), oidc)
	ctx := ctxFor("t1")
	acc := h.linkAccount(t, ctx, "oaiapp_1", "c")
	later := time.Now().Add(time.Hour)
	// Inside the skew but not expired, not yet refreshable: the current
	// token is still served.
	h.repo.edit(acc.UUID, func(r *models.UserAccount) { r.ExpiresAt, r.EarliestRefreshAt = time.Now().Add(30*time.Second), &later })
	if cred, err := h.svc.CredentialFor(ctx, "u-admin"); err != nil || cred.AccessToken != "at-c" {
		t.Fatalf("pre-expiry err = %v", err)
	}
	// Expired and not refreshable for an hour: unavailable, no refresh.
	h.expire(acc.UUID)
	if _, err := h.svc.CredentialFor(ctx, "u-admin"); !errors.Is(err, iface.ErrLLMProviderUnavailable) {
		t.Fatalf("expired err = %v", err)
	}
	oidc.locked(func() {
		if oidc.refreshes != 0 {
			t.Errorf("refreshed before earliest_refresh_at (%d)", oidc.refreshes)
		}
	})
	// Refreshable in a moment: waited for.
	soon := time.Now().Add(50 * time.Millisecond)
	h.repo.edit(acc.UUID, func(r *models.UserAccount) { r.EarliestRefreshAt = &soon })
	if cred, err := h.svc.CredentialFor(ctx, "u-admin"); err != nil || cred.AccessToken != "at-r1" {
		t.Fatalf("short wait err = %v", err)
	}
}

func TestAccounts_ForceRefreshUsesAnAlreadyRotatedSet(t *testing.T) {
	oidc := &fakeOIDC{scope: SIWCScope, sub: "sub-1"}
	h := newAccountsHarness(t, hostedPasteConfig(SIWCModeHostedPaste), oidc)
	ctx := ctxFor("t1")
	h.linkAccount(t, ctx, "oaiapp_1", "c")
	old, err := h.svc.CredentialFor(ctx, "u-admin")
	if err != nil {
		t.Fatal(err)
	}
	next, err := h.svc.ForceRefresh(ctx, old)
	if err != nil || next.AccessToken != "at-r1" {
		t.Fatalf("force err = %v", err)
	}
	// A second 401 on the old set: the stored set already moved on.
	again, err := h.svc.ForceRefresh(ctx, old)
	if err != nil || again.AccessToken != "at-r1" {
		t.Fatalf("second force err = %v", err)
	}
	oidc.locked(func() {
		if oidc.refreshes != 1 {
			t.Errorf("refreshes = %d", oidc.refreshes)
		}
	})
}

func TestAccounts_CredentialForFailsClosed(t *testing.T) {
	oidc := &fakeOIDC{scope: SIWCScope, sub: "sub-1"}
	h := newAccountsHarness(t, hostedPasteConfig(SIWCModeHostedPaste), oidc)
	ctx := ctxFor("t1")
	if _, err := h.svc.CredentialFor(ctx, ""); !errors.Is(err, iface.ErrLLMUserAccountRequired) {
		t.Fatalf("no user err = %v", err)
	}
	if _, err := h.svc.CredentialFor(ctx, "u-admin"); !errors.Is(err, iface.ErrLLMUserAccountRequired) {
		t.Fatalf("no account err = %v", err)
	}
	acc := h.linkAccount(t, ctx, "oaiapp_1", "c")
	if _, err := h.svc.CredentialFor(ctxFor("t2"), "u-admin"); !errors.Is(err, iface.ErrLLMUserAccountRequired) {
		t.Fatalf("other org err = %v", err)
	}
	h.repo.edit(acc.UUID, func(r *models.UserAccount) { r.Status = models.AccountStatusRevoked })
	if _, err := h.svc.CredentialFor(ctx, "u-admin"); !errors.Is(err, iface.ErrLLMUserAccountReauth) {
		t.Fatalf("revoked err = %v", err)
	}
	h.repo.edit(acc.UUID, func(r *models.UserAccount) { r.Status, r.PlanUsageGranted = models.AccountStatusActive, false })
	if _, err := h.svc.CredentialFor(ctx, "u-admin"); !errors.Is(err, iface.ErrLLMPlanUsageDisabled) {
		t.Fatalf("no plan grant err = %v", err)
	}
}

// --- plan catalog, use, unlink ---------------------------------------------------

func TestAccounts_PlanModelsCacheAndInvalidUserRetry(t *testing.T) {
	oidc := &fakeOIDC{scope: SIWCScope, sub: "sub-1", models: []string{"gpt-5.6-terra"}}
	h := newAccountsHarness(t, hostedPasteConfig(SIWCModeHostedPaste), oidc)
	ctx := ctxFor("t1")
	acc := h.linkAccount(t, ctx, "oaiapp_1", "c")
	cred, err := h.svc.CredentialFor(ctx, "u-admin")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		slugs, _, err := h.svc.PlanModels(ctx, cred)
		if err != nil || len(slugs) != 1 || slugs[0] != "gpt-5.6-terra" {
			t.Fatalf("plan models = %v, %v", slugs, err)
		}
		slugs[0] = "mutated" // callers get a copy
	}
	calls := func() (n int) { oidc.locked(func() { n = oidc.planCalls }); return }
	if calls() != 1 {
		t.Fatalf("plan calls = %d, want 1 (cached)", calls())
	}
	h.svc.now = func() time.Time { return time.Now().Add(accountPlanCacheTTL + time.Second) }
	if _, _, err := h.svc.PlanModels(ctx, cred); err != nil || calls() != 2 {
		t.Fatalf("expired cache: calls = %d, err = %v", calls(), err)
	}
	h.svc.now = time.Now
	if err := h.svc.Select(ctx, acc.UUID); err != nil {
		t.Fatal(err)
	}
	// 401 with the current token: one forced refresh and one retry with the
	// rotated token.
	oidc.locked(func() { oidc.rejectAT = "at-c" })
	slugs, used, err := h.svc.PlanModels(ctx, cred)
	if err != nil || len(slugs) != 1 {
		t.Fatalf("retry = %v, %v", slugs, err)
	}
	if used == nil || used.AccessToken != "at-r1" || !used.Account.SavedAt.After(cred.Account.SavedAt) {
		t.Fatal("PlanModels must hand back the refreshed credential, not the rejected one")
	}
	oidc.locked(func() {
		if oidc.refreshes != 1 || oidc.lastPlanToken != "at-r1" {
			t.Errorf("refreshes = %d, retried with rotated token = %v", oidc.refreshes, oidc.lastPlanToken == "at-r1")
		}
	})
	if calls() != 4 {
		t.Fatalf("plan calls = %d", calls())
	}
}

func TestRegistryPlanCatalog_ReadsTheSIWCCatalog(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/models") || r.Header.Get("Authorization") != "Bearer at-x" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"models":[{"slug":"gpt-a","display_name":"A","visibility":"list"},{"slug":"gpt-hidden","visibility":"hide"}]}`))
	}))
	defer srv.Close()
	reg := providers.NewRegistry()
	reg.Register(models.ProviderOpenAI, func(c providers.Credential, _ *http.Client) (providers.Provider, error) {
		if c.Kind != iface.LLMCredentialKindUserAccount {
			t.Errorf("credential kind = %q", c.Kind)
		}
		c.BaseURL = srv.URL
		return providers.NewOpenAI(c, srv.Client())
	})
	slugs, err := RegistryPlanCatalog{Registry: reg}.ListPlanModels(context.Background(), "at-x")
	if err != nil || len(slugs) != 1 || slugs[0] != "gpt-a" {
		t.Fatalf("slugs = %v, %v", slugs, err)
	}
	if _, err := (RegistryPlanCatalog{Registry: reg}).ListPlanModels(context.Background(), "at-wrong"); !errors.Is(err, ErrSIWCInvalidUser) {
		t.Fatalf("401 err = %v", err)
	}
}

func TestAccounts_MarkUsedIsATargetedWrite(t *testing.T) {
	oidc := &fakeOIDC{scope: SIWCScope, sub: "sub-1"}
	h := newAccountsHarness(t, hostedPasteConfig(SIWCModeHostedPaste), oidc)
	ctx := ctxFor("t1")
	acc := h.linkAccount(t, ctx, "oaiapp_1", "c")
	h.svc.MarkUsed(ctx, acc.UUID)
	got, _ := h.repo.row(acc.UUID)
	if got.LastUsedAt == nil || got.AccessToken != acc.AccessToken {
		t.Fatal("MarkUsed stamps lastUsedAt only")
	}
	h.svc.MarkUsed(ctxFor("t2"), acc.UUID) // other org: logged, no write
	h.svc.MarkReauth(ctx, acc.UUID, errcode.LLMUserAccountReauthRequired)
	if got, _ := h.repo.row(acc.UUID); got.Status != models.AccountStatusNeedsReauth || got.AccessToken != acc.AccessToken {
		t.Fatal("MarkReauth sets the status only")
	}
}

func TestAccounts_UnlinkRevokesAndDeletes(t *testing.T) {
	oidc := &fakeOIDC{scope: SIWCScope, sub: "sub-1"}
	h := newAccountsHarness(t, hostedPasteConfig(SIWCModeHostedPaste), oidc)
	ctx := ctxFor("t1")
	acc := h.linkAccount(t, ctx, "oaiapp_1", "c")
	if err := h.svc.Unlink(ctxForUser("t1", "u-other"), acc.UUID, false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other user unlink err = %v", err)
	}
	if err := h.svc.Unlink(ctx, acc.UUID, false); err != nil {
		t.Fatal(err)
	}
	oidc.locked(func() {
		if len(oidc.revoked) != 1 || oidc.revoked[0] != "rt-c" {
			t.Errorf("revocations = %d", len(oidc.revoked))
		}
	})
	if h.repo.count() != 0 {
		t.Fatalf("rows = %d", h.repo.count())
	}
	if err := h.svc.Unlink(ctx, acc.UUID, false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second unlink err = %v", err)
	}

	// A failed revocation still deletes locally; force reaches another
	// user's registration in the org (admin), never another org's.
	other := ctxForUser("t1", "u-other")
	acc2 := h.linkAccount(t, other, "oaiapp_2", "d")
	oidc.locked(func() {
		oidc.revokeErr = fmt.Errorf("%w: revocation endpoint answered 503", iface.ErrLLMProviderUnavailable)
	})
	if err := h.svc.Unlink(ctxFor("t2"), acc2.UUID, true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-org force err = %v", err)
	}
	if err := h.svc.Unlink(ctx, acc2.UUID, true); err != nil {
		t.Fatalf("force unlink err = %v", err)
	}
	if h.repo.count() != 0 {
		t.Fatal("unlink must delete even when the revocation fails")
	}
	forced := false
	for _, e := range h.audit.actions() {
		if e.Action == "llm.account.unlinked" && e.ResourceID == acc2.UUID {
			forced = e.Metadata["force"] == true && e.ActorUserID == "u-admin"
		}
	}
	if !forced {
		t.Fatal("the forced unlink is audited with the admin as actor")
	}
}

// The DSR purge has no org in its context: RevokeRegistration works per row
// under the row's own org.
func TestAccounts_RevokeRegistrationWorksWithoutATenantContext(t *testing.T) {
	oidc := &fakeOIDC{scope: SIWCScope, sub: "sub-1"}
	h := newAccountsHarness(t, hostedPasteConfig(SIWCModeHostedPaste), oidc)
	acc := h.linkAccount(t, ctxFor("t1"), "oaiapp_1", "c")
	stored, _ := h.repo.row(acc.UUID)
	h.svc.RevokeRegistration(context.Background(), stored)
	oidc.locked(func() {
		if len(oidc.revoked) != 1 || oidc.revoked[0] != "rt-c" {
			t.Errorf("revocations = %d", len(oidc.revoked))
		}
	})
	if h.repo.count() != 1 {
		t.Fatal("RevokeRegistration deletes nothing")
	}
}

// One replica without Redis: the in-process single-flight alone makes
// concurrent callers share one network refresh.
func TestAccounts_SingleReplicaWithoutRedisIsSingleFlight(t *testing.T) {
	oidc := &fakeOIDC{scope: SIWCScope, sub: "sub-1", refreshDelay: 30 * time.Millisecond}
	h := newAccountsHarness(t, hostedPasteConfig(SIWCModeHostedPaste), oidc)
	ctx := ctxFor("t1")
	acc := h.linkAccount(t, ctx, "oaiapp_1", "c")
	h.expire(acc.UUID)
	svc := NewAccountsService(AccountsDeps{
		Repo: h.repo, Host: h.host, OIDC: oidc, Plans: oidc, State: NewSIWCStateStore(h.redis, h.vault), Vault: h.vault,
		Limiter: NewRateLimiter(h.redis, slog.Default()), Redis: nil, Config: func() SIWCConfig { return h.cfg },
	})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if cred, err := svc.CredentialFor(ctx, "u-admin"); err != nil || cred.AccessToken != "at-r1" {
				t.Errorf("credential err = %v", err)
			}
		}()
	}
	wg.Wait()
	oidc.locked(func() {
		if oidc.refreshes != 1 {
			t.Errorf("refreshes = %d, want 1", oidc.refreshes)
		}
	})
}

// storeNewerSet plays another writer: it stores a newer, valid set whose
// access token is at.
func (h *accountsHarness) storeNewerSet(t *testing.T, r *models.UserAccount, at string) {
	t.Helper()
	env, err := h.vault.Seal(context.Background(), r.TenantID, r.UUID, accountFieldAccessToken, at)
	if err != nil {
		t.Fatal(err)
	}
	r.AccessToken, r.SavedAt, r.ExpiresAt = env, r.SavedAt.Add(time.Second), time.Now().Add(time.Hour)
	r.Status, r.LastError = models.AccountStatusActive, ""
}

// A rotation that loses the compare-and-swap serves the stored set and
// revokes the set it drops.
func TestAccounts_LostRotationIsRevokedAndTheStoredSetServed(t *testing.T) {
	oidc := &fakeOIDC{scope: SIWCScope, sub: "sub-1"}
	h := newAccountsHarness(t, hostedPasteConfig(SIWCModeHostedPaste), oidc)
	ctx := ctxFor("t1")
	acc := h.linkAccount(t, ctx, "oaiapp_1", "c")
	h.expire(acc.UUID)
	h.repo.mu.Lock()
	h.repo.beforeReplace = func(r *models.UserAccount) { h.storeNewerSet(t, r, "at-winner") }
	h.repo.mu.Unlock()
	cred, err := h.svc.CredentialFor(ctx, "u-admin")
	if err != nil || cred.AccessToken != "at-winner" {
		t.Fatalf("lost rotation err = %v", err)
	}
	oidc.locked(func() {
		if len(oidc.revoked) != 1 || oidc.revoked[0] != "rt-r1" {
			t.Errorf("the dropped rotation must be revoked (revocations = %d)", len(oidc.revoked))
		}
	})
}

// A stale presenter gets a terminal answer (refresh_token_reused) while the
// row already holds a newer valid set: no needs_reauth, the stored set is
// served.
func TestAccounts_TerminalOnAStaleSetServesTheNewerOne(t *testing.T) {
	oidc := &fakeOIDC{scope: SIWCScope, sub: "sub-1"}
	h := newAccountsHarness(t, hostedPasteConfig(SIWCModeHostedPaste), oidc)
	ctx := ctxFor("t1")
	acc := h.linkAccount(t, ctx, "oaiapp_1", "c")
	h.expire(acc.UUID)
	oidc.locked(func() {
		oidc.refreshErr = providers.ClassifySIWCCode("refresh_token_reused")
		oidc.beforeRefreshAnswer = func() { h.repo.edit(acc.UUID, func(r *models.UserAccount) { h.storeNewerSet(t, r, "at-winner") }) }
	})
	cred, err := h.svc.CredentialFor(ctx, "u-admin")
	if err != nil || cred.AccessToken != "at-winner" {
		t.Fatalf("stale terminal err = %v", err)
	}
	if got, _ := h.repo.row(acc.UUID); got.Status != models.AccountStatusActive || got.LastError != "" {
		t.Fatalf("a newer set must not be flagged (status %s)", got.Status)
	}
	// The fenced flag itself: written for the set that failed only.
	stored, _ := h.repo.row(acc.UUID)
	if err := h.repo.SetStatusIf(ctx, acc.UUID, acc.SavedAt, models.AccountStatusNeedsReauth, "llm.x"); !errors.Is(err, repository.ErrStaleTokenSet) {
		t.Fatalf("fenced flag on an old set err = %v", err)
	}
	if err := h.repo.SetStatusIf(ctx, acc.UUID, stored.SavedAt, models.AccountStatusNeedsReauth, "llm.x"); err != nil {
		t.Fatal(err)
	}
}

// An IdP that echoes the refresh token instead of rotating it: the losing
// rotation's "new" token is the one the winning stored set still uses, so
// it must not be revoked.
func TestAccounts_LostRotationDoesNotRevokeAnEchoedRefreshToken(t *testing.T) {
	oidc := &fakeOIDC{scope: SIWCScope, sub: "sub-1", refreshEchoes: true}
	h := newAccountsHarness(t, hostedPasteConfig(SIWCModeHostedPaste), oidc)
	ctx := ctxFor("t1")
	acc := h.linkAccount(t, ctx, "oaiapp_1", "c")
	h.expire(acc.UUID)
	h.repo.mu.Lock()
	h.repo.beforeReplace = func(r *models.UserAccount) { h.storeNewerSet(t, r, "at-winner") } // keeps rt-c
	h.repo.mu.Unlock()
	cred, err := h.svc.CredentialFor(ctx, "u-admin")
	if err != nil || cred.AccessToken != "at-winner" {
		t.Fatalf("lost rotation err = %v", err)
	}
	oidc.locked(func() {
		if len(oidc.revoked) != 0 {
			t.Errorf("an echoed refresh token still in use was revoked (revocations = %d)", len(oidc.revoked))
		}
	})
}

// --- final review fixes ----------------------------------------------------------

// Minor 1: once the code is being exchanged, a browser that hangs up must
// not lose the registration (the code is spent at OpenAI and the state
// consumed): exchange, verification and store run on a detached context.
func TestAccounts_CompleteSurvivesTheCallerHangingUp(t *testing.T) {
	oidc := &fakeOIDC{scope: SIWCScope, sub: "sub-1"}
	h := newAccountsHarness(t, hostedPasteConfig(SIWCModeHostedPaste), oidc)
	ctx, cancel := context.WithCancel(ctxFor("t1"))
	defer cancel()
	link, err := h.svc.Link(ctx, "", "")
	if err != nil {
		t.Fatal(err)
	}
	oidc.locked(func() { oidc.beforeExchangeAnswer = cancel })
	acc, err := h.svc.Complete(ctx, CallbackInput{Code: "c", State: stateFrom(t, link.AuthorizeURL), ClientID: "oaiapp_1", RedirectURI: pasteRedirect}, true)
	if err != nil || acc == nil {
		t.Fatalf("complete after hang-up err = %v", err)
	}
	if _, ok := h.repo.row(acc.UUID); !ok {
		t.Fatal("the registration must be stored")
	}
	oidc.locked(func() {
		if len(oidc.revoked) != 0 {
			t.Errorf("a stored registration must not be revoked (revocations = %d)", len(oidc.revoked))
		}
	})
}

// Minor 2: invalid_client keeps credentials and status (ruling: a
// configuration error), and the same set is not presented again for a
// short backoff; a new set (reauth, rotation) is never held back.
func TestAccounts_InvalidClientIsNotRepeatedForTheSameSet(t *testing.T) {
	oidc := &fakeOIDC{scope: SIWCScope, sub: "sub-1"}
	h := newAccountsHarness(t, hostedPasteConfig(SIWCModeHostedPaste), oidc)
	ctx := ctxFor("t1")
	acc := h.linkAccount(t, ctx, "oaiapp_1", "c")
	h.expire(acc.UUID)
	oidc.locked(func() { oidc.refreshErr = providers.ClassifySIWCCode("invalid_client") })
	for i := 0; i < 3; i++ {
		_, err := h.svc.CredentialFor(ctx, "u-admin")
		if !errors.Is(err, ErrSIWCInvalidClient) || !errors.Is(err, iface.ErrLLMProviderUnavailable) {
			t.Fatalf("call %d err = %v", i, err)
		}
	}
	refreshes := func() (n int) { oidc.locked(func() { n = oidc.refreshes }); return n }
	if n := refreshes(); n != 1 {
		t.Fatalf("token endpoint hit %d times, want 1", n)
	}
	if got, _ := h.repo.row(acc.UUID); got.Status != models.AccountStatusActive || got.LastError != "" {
		t.Fatalf("invalid_client is not a reauth: status %s", got.Status)
	}
	// A new set (here: savedAt moved, as a reauth or rotation would) is
	// presented at once.
	h.repo.edit(acc.UUID, func(r *models.UserAccount) { r.SavedAt = r.SavedAt.Add(time.Second) })
	_, _ = h.svc.CredentialFor(ctx, "u-admin")
	if n := refreshes(); n != 2 {
		t.Fatalf("a new set must not be held back (refreshes = %d)", n)
	}
	// After the backoff the same set is tried again.
	h.svc.now = func() time.Time { return time.Now().Add(accountInvalidClientBackoff + time.Second) }
	h.expire(acc.UUID)
	_, _ = h.svc.CredentialFor(ctx, "u-admin")
	if n := refreshes(); n != 3 {
		t.Fatalf("the backoff must expire (refreshes = %d)", n)
	}
}

// Minor 3: an unlink (or erasure) that races a refresh revoked only the
// token it read; the rotated one stored nowhere is revoked too.
func TestAccounts_RotationOfADeletedRegistrationIsRevoked(t *testing.T) {
	oidc := &fakeOIDC{scope: SIWCScope, sub: "sub-1"}
	h := newAccountsHarness(t, hostedPasteConfig(SIWCModeHostedPaste), oidc)
	ctx := ctxFor("t1")
	acc := h.linkAccount(t, ctx, "oaiapp_1", "c")
	h.expire(acc.UUID)
	oidc.locked(func() {
		oidc.beforeRefreshAnswer = func() {
			if err := h.svc.Unlink(ctx, acc.UUID, false); err != nil {
				t.Errorf("unlink: %v", err)
			}
		}
	})
	if _, err := h.svc.CredentialFor(ctx, "u-admin"); err == nil {
		t.Fatal("a deleted registration must not yield a credential")
	}
	oidc.locked(func() {
		if len(oidc.revoked) != 2 || oidc.revoked[0] != "rt-c" || oidc.revoked[1] != "rt-r1" {
			t.Errorf("revocations = %d (the unlink's, then the rotated token)", len(oidc.revoked))
		}
	})
}

// Minor 3 boundary: a token the IdP echoed is the one the unlink revoked.
func TestAccounts_EchoedTokenOfADeletedRegistrationIsNotRevokedTwice(t *testing.T) {
	oidc := &fakeOIDC{scope: SIWCScope, sub: "sub-1", refreshEchoes: true}
	h := newAccountsHarness(t, hostedPasteConfig(SIWCModeHostedPaste), oidc)
	ctx := ctxFor("t1")
	acc := h.linkAccount(t, ctx, "oaiapp_1", "c")
	h.expire(acc.UUID)
	oidc.locked(func() {
		oidc.beforeRefreshAnswer = func() { _ = h.svc.Unlink(ctx, acc.UUID, false) }
	})
	_, _ = h.svc.CredentialFor(ctx, "u-admin")
	oidc.locked(func() {
		if len(oidc.revoked) != 1 {
			t.Errorf("revocations = %d, want only the unlink's", len(oidc.revoked))
		}
	})
}

// Minor 8: the label limit counts characters, like the API schema.
func TestAccounts_LabelLimitCountsCharacters(t *testing.T) {
	h := newAccountsHarness(t, hostedPasteConfig(SIWCModeHostedPaste), &fakeOIDC{})
	if _, err := h.svc.Link(ctxFor("t1"), strings.Repeat("é", accountLabelMaxLen), ""); err != nil {
		t.Fatalf("%d two-byte characters err = %v", accountLabelMaxLen, err)
	}
	if _, err := h.svc.Link(ctxFor("t1"), strings.Repeat("é", accountLabelMaxLen+1), ""); !errors.Is(err, iface.ErrLLMInvalidRequest) {
		t.Fatalf("%d characters err = %v", accountLabelMaxLen+1, err)
	}
}

// Minor 10: a duplicate on the first link whose cause cannot be told (the
// re-read fails, or the same client id lands right after it) is a failed
// exchange, never llm.name_in_use, and the fresh set is revoked.
func TestAccounts_UnresolvableDuplicateIsAnExchangeFailure(t *testing.T) {
	for _, tc := range []struct {
		name string
		arm  func(h *accountsHarness)
	}{
		{"re-read fails", func(h *accountsHarness) {
			h.repo.beforeInsert = func(rows map[string]*models.UserAccount) {
				rows["racer"] = &models.UserAccount{UUID: "racer", TenantID: "t1", UserUUID: "u-admin", Provider: models.ProviderOpenAI, ClientID: "oaiapp_1"}
				h.repo.listErr = errors.New("mongo blip")
			}
		}},
		{"same client id after the re-read", func(h *accountsHarness) {
			h.repo.beforeInsert = func(rows map[string]*models.UserAccount) {
				rows["selected"] = &models.UserAccount{UUID: "selected", TenantID: "t1", UserUUID: "u-admin", Provider: models.ProviderOpenAI, ClientID: "oaiapp_other", Selected: true}
				h.repo.beforeInsert = func(rows map[string]*models.UserAccount) {
					rows["racer"] = &models.UserAccount{UUID: "racer", TenantID: "t1", UserUUID: "u-admin", Provider: models.ProviderOpenAI, ClientID: "oaiapp_1"}
				}
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			oidc := &fakeOIDC{scope: SIWCScope, sub: "sub-1"}
			h := newAccountsHarness(t, hostedPasteConfig(SIWCModeHostedPaste), oidc)
			ctx := ctxFor("t1")
			link, err := h.svc.Link(ctx, "", "")
			if err != nil {
				t.Fatal(err)
			}
			h.repo.mu.Lock()
			tc.arm(h)
			h.repo.mu.Unlock()
			_, err = h.svc.Complete(ctx, CallbackInput{Code: "c", State: stateFrom(t, link.AuthorizeURL), ClientID: "oaiapp_1", RedirectURI: pasteRedirect}, true)
			if !errors.Is(err, ErrSIWCExchangeFailed) || errors.Is(err, repository.ErrDuplicateName) || errors.Is(err, ErrNameInUse) {
				t.Fatalf("err = %v", err)
			}
			oidc.locked(func() {
				if len(oidc.revoked) != 1 || oidc.revoked[0] != "rt-c" {
					t.Errorf("the fresh set must be revoked (revocations = %d)", len(oidc.revoked))
				}
			})
		})
	}
}

// flakyKMS is a KMS provider whose Encrypt fails while fail is non-zero
// (fail < 0: always); Decrypt reverses a successful Encrypt.
type flakyKMS struct {
	mu   sync.Mutex
	fail int
}

func (k *flakyKMS) CreateKey(_ context.Context, tenantUUID string) (string, error) {
	return "k-" + tenantUUID, nil
}

func (k *flakyKMS) Encrypt(_ context.Context, keyID string, p []byte) ([]byte, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.fail != 0 {
		if k.fail > 0 {
			k.fail--
		}
		return nil, errors.New("kms: service unavailable")
	}
	return append([]byte(keyID+":"), p...), nil
}

func (k *flakyKMS) Decrypt(_ context.Context, keyID string, c []byte) ([]byte, error) {
	return []byte(strings.TrimPrefix(string(c), keyID+":")), nil
}

func (k *flakyKMS) DeleteKey(context.Context, string) error { return nil }

// Greptile 4241218091: the provider rotated the set but the vault failed to
// seal it once. The same returned set is sealed again and stored; the
// refresh is never repeated (the stored refresh token is spent).
func TestAccounts_SealFailureAfterRefreshRetriesTheSameSet(t *testing.T) {
	oidc := &fakeOIDC{scope: SIWCScope, sub: "sub-1"}
	h := newAccountsHarness(t, hostedPasteConfig(SIWCModeHostedPaste), oidc)
	ctx := ctxFor("t1")
	acc := h.linkAccount(t, ctx, "oaiapp_1", "c")
	before, _ := h.repo.row(acc.UUID)
	h.expire(acc.UUID)
	h.vault.SetKMSProvider(&flakyKMS{fail: 1})
	cred, err := h.svc.CredentialFor(ctx, "u-admin")
	if err != nil || cred.AccessToken != "at-r1" {
		t.Fatalf("credential err = %v (rotated token served: %v)", err, cred != nil && cred.AccessToken == "at-r1")
	}
	oidc.locked(func() {
		if oidc.refreshes != 1 || len(oidc.revoked) != 0 {
			t.Errorf("refreshes = %d, revocations = %d (want 1, 0)", oidc.refreshes, len(oidc.revoked))
		}
	})
	after, _ := h.repo.row(acc.UUID)
	if !after.SavedAt.After(before.SavedAt) || after.RefreshToken.Alg != models.EnvelopeAlgKMS || after.Status != models.AccountStatusActive {
		t.Fatalf("the rotated set was not stored (status %s)", after.Status)
	}
	if got, err := h.svc.vault.Open(ctx, after.TenantID, after.UUID, accountFieldRefreshToken, after.RefreshToken); err != nil || got != "rt-r1" {
		t.Fatal("the stored refresh token is not the rotated one")
	}
}

// When the vault keeps refusing, the rotated refresh token cannot be kept:
// it is revoked and the registration flagged needs_reauth (fenced on the
// set that was refreshed), instead of leaving the spent token stored.
func TestAccounts_PersistentSealFailureRevokesAndNeedsReauth(t *testing.T) {
	oidc := &fakeOIDC{scope: SIWCScope, sub: "sub-1"}
	h := newAccountsHarness(t, hostedPasteConfig(SIWCModeHostedPaste), oidc)
	ctx := ctxFor("t1")
	acc := h.linkAccount(t, ctx, "oaiapp_1", "c")
	h.expire(acc.UUID)
	before, _ := h.repo.row(acc.UUID)
	h.vault.SetKMSProvider(&flakyKMS{fail: -1})
	_, err := h.svc.CredentialFor(ctx, "u-admin")
	if !errors.Is(err, iface.ErrLLMUserAccountReauth) {
		t.Fatalf("err = %v, want ErrLLMUserAccountReauth", err)
	}
	oidc.locked(func() {
		if oidc.refreshes != 1 || len(oidc.revoked) != 1 || oidc.revoked[0] != "rt-r1" {
			t.Errorf("refreshes = %d, revocations = %d (want one refresh, the rotated token revoked)", oidc.refreshes, len(oidc.revoked))
		}
	})
	after, _ := h.repo.row(acc.UUID)
	if after.Status != models.AccountStatusNeedsReauth || after.LastError != errcode.LLMUserAccountReauthRequired || !after.SavedAt.Equal(before.SavedAt) {
		t.Fatalf("status = %s / %s", after.Status, after.LastError)
	}
}

// A refresh that kept the refresh token loses nothing when sealing fails:
// the stored set stays usable, the call is unavailable, nothing revoked.
func TestAccounts_SealFailureWithoutRotationKeepsTheStoredSet(t *testing.T) {
	oidc := &fakeOIDC{scope: SIWCScope, sub: "sub-1", refreshOmitsOptional: true}
	h := newAccountsHarness(t, hostedPasteConfig(SIWCModeHostedPaste), oidc)
	ctx := ctxFor("t1")
	acc := h.linkAccount(t, ctx, "oaiapp_1", "c")
	h.expire(acc.UUID)
	h.vault.SetKMSProvider(&flakyKMS{fail: -1})
	if _, err := h.svc.CredentialFor(ctx, "u-admin"); !errors.Is(err, iface.ErrLLMProviderUnavailable) || errors.Is(err, iface.ErrLLMUserAccountReauth) {
		t.Fatalf("err = %v", err)
	}
	oidc.locked(func() {
		if len(oidc.revoked) != 0 {
			t.Errorf("revocations = %d", len(oidc.revoked))
		}
	})
	if after, _ := h.repo.row(acc.UUID); after.Status != models.AccountStatusActive {
		t.Fatalf("status = %s", after.Status)
	}
}

// Greptile 4241218095: a refresh stores a newer set between the unlink's
// read and its delete. The unlink revokes the set the delete removed, so
// the newer refresh token does not outlive the registration.
func TestAccounts_UnlinkRevokesTheSetTheDeleteRemoved(t *testing.T) {
	oidc := &fakeOIDC{scope: SIWCScope, sub: "sub-1"}
	h := newAccountsHarness(t, hostedPasteConfig(SIWCModeHostedPaste), oidc)
	ctx := ctxFor("t1")
	acc := h.linkAccount(t, ctx, "oaiapp_1", "c")
	newer, err := h.vault.Seal(ctx, acc.TenantID, acc.UUID, accountFieldRefreshToken, "rt-newer")
	if err != nil {
		t.Fatal(err)
	}
	h.repo.mu.Lock()
	h.repo.beforeDelete = func(r *models.UserAccount) {
		r.RefreshToken, r.SavedAt = newer, r.SavedAt.Add(time.Second)
	}
	h.repo.mu.Unlock()
	if err := h.svc.Unlink(ctx, acc.UUID, false); err != nil {
		t.Fatal(err)
	}
	oidc.locked(func() {
		if len(oidc.revoked) != 1 || oidc.revoked[0] != "rt-newer" {
			t.Errorf("revocations = %d (the newer token revoked: %v)", len(oidc.revoked), len(oidc.revoked) == 1 && oidc.revoked[0] == "rt-newer")
		}
	})
	if h.repo.count() != 0 {
		t.Fatalf("rows = %d", h.repo.count())
	}
}

// Greptile 4241218111: the plan catalog cache is tied to the token set. A
// rotation stored by another replica (whose invalidation never reaches this
// one) makes the old entry a miss, and a read of the older set that ends
// late never replaces the newer entry.
func TestAccounts_PlanCacheIsTiedToTheTokenSet(t *testing.T) {
	oidc := &fakeOIDC{scope: SIWCScope, sub: "sub-1", models: []string{"m-old"}}
	h := newAccountsHarness(t, hostedPasteConfig(SIWCModeHostedPaste), oidc)
	ctx := ctxFor("t1")
	acc := h.linkAccount(t, ctx, "oaiapp_1", "c")
	old, err := h.svc.CredentialFor(ctx, "u-admin")
	if err != nil {
		t.Fatal(err)
	}
	if slugs, _, err := h.svc.PlanModels(ctx, old); err != nil || len(slugs) != 1 || slugs[0] != "m-old" {
		t.Fatalf("first read = %v, %v", slugs, err)
	}
	// Another replica refreshes the set; this replica's cache is not told.
	h.expire(acc.UUID)
	if _, err := h.replica().CredentialFor(ctx, "u-admin"); err != nil {
		t.Fatal(err)
	}
	oidc.locked(func() { oidc.models = []string{"m-new"} })
	cur, err := h.svc.CredentialFor(ctx, "u-admin")
	if err != nil || cur.Account.SavedAt.Equal(old.Account.SavedAt) {
		t.Fatalf("this replica did not read the rotated set: %v", err)
	}
	if slugs, _, err := h.svc.PlanModels(ctx, cur); err != nil || len(slugs) != 1 || slugs[0] != "m-new" {
		t.Fatalf("after another replica's rotation = %v, %v (stale catalog served)", slugs, err)
	}
	// A read with the older set answers its caller but is not cached over
	// the newer entry.
	oidc.locked(func() { oidc.models = []string{"m-stale"} })
	if _, _, err := h.svc.PlanModels(ctx, old); err != nil {
		t.Fatal(err)
	}
	var calls int
	oidc.locked(func() { calls = oidc.planCalls })
	if slugs, _, err := h.svc.PlanModels(ctx, cur); err != nil || len(slugs) != 1 || slugs[0] != "m-new" {
		t.Fatalf("newer entry replaced by an older read: %v, %v", slugs, err)
	}
	oidc.locked(func() {
		if oidc.planCalls != calls {
			t.Errorf("plan calls = %d, want %d (the newer entry served from cache)", oidc.planCalls, calls)
		}
	})
}
