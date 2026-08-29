package middleware

// Phase 14c: integration coverage for RequireAuth — the security
// perimeter on every protected route. Previously 0% tested at this
// layer; the JWT validator and session-revocation service have their
// own unit tests, but the middleware's integration of them
// (extract → validate → revocation check → context populate) was
// only exercised end-to-end in production.
//
// Setup is intentionally minimal: real *jwtService (so we exercise
// the actual validator), in-memory revocation stub, no auth-service —
// RequireAuth is bearer-only (ADR-0020), so there is no silent-refresh
// path to exercise; the three "NeverRotates" tests at the bottom pin
// that. httptest server captures the downstream handler's view of the
// request context.

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/repository"
	"github.com/orkestra/backend/internal/core/auth/services"
	sharederrors "github.com/orkestra/backend/internal/shared/errors"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// stubTenant satisfies iface.TenantProvider with the empty-membership
// default the JWTService tests use. The middleware doesn't drive
// tenant resolution from RequireAuth itself.
type stubTenant struct{}

func (stubTenant) GetTenant(context.Context, string) (*iface.Tenant, error) {
	return nil, errors.New("not used")
}
func (stubTenant) ListUserMemberships(context.Context, string) ([]iface.TenantMembership, error) {
	return nil, nil
}
func (stubTenant) IsMember(context.Context, string, string) (bool, error) {
	return false, errors.New("not used")
}
func (stubTenant) ActivateTenant(context.Context, string) error {
	return errors.New("not used")
}
func (stubTenant) SetTenantStripeCustomerID(context.Context, string, string) error {
	return errors.New("not used")
}
func (stubTenant) EnsureTenantForUser(context.Context, string) (*iface.Tenant, error) {
	return nil, errors.New("not used")
}

// fakeRevocation is an in-memory SessionRevocationService. Tests pre-
// populate `revoked` to flip a sid into the deny set. It also implements
// the optional SessionRevocationReasonReader, matching the production
// Redis service, so the middleware's reason-aware branch is exercised.
type fakeRevocation struct {
	mu      sync.Mutex
	revoked map[string]string
}

func newFakeRevocation() *fakeRevocation { return &fakeRevocation{revoked: map[string]string{}} }

func (f *fakeRevocation) Revoke(_ context.Context, sid, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if reason == "" {
		reason = "revoked"
	}
	f.revoked[sid] = reason
	return nil
}

func (f *fakeRevocation) IsRevoked(_ context.Context, sid string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.revoked[sid]
	return ok, nil
}

func (f *fakeRevocation) RevocationReason(_ context.Context, sid string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	reason, ok := f.revoked[sid]
	return reason, ok
}

// The production Redis service must keep satisfying the optional extension
// the middleware type-asserts on — if it stops, every capped-out session
// silently reverts to being reported as "revoked" with no test failing.
var _ services.SessionRevocationReasonReader = (*fakeRevocation)(nil)

// reasonBlindRevocation implements ONLY the base interface, standing in for
// a fork's own SessionRevocationService. The extension is optional, so this
// must keep working and simply fall back to the generic wording.
type reasonBlindRevocation struct{ revoked map[string]bool }

func (f *reasonBlindRevocation) Revoke(_ context.Context, sid, _ string) error {
	f.revoked[sid] = true
	return nil
}

func (f *reasonBlindRevocation) IsRevoked(_ context.Context, sid string) (bool, error) {
	return f.revoked[sid], nil
}

// requireAuthFixture bundles the constructed middleware + dependencies
// so each test stays a couple of lines.
type requireAuthFixture struct {
	t          *testing.T
	priv       *rsa.PrivateKey
	jwt        services.JWTService
	revocation *fakeRevocation
	mw         *AuthMiddleware
}

func newRequireAuthFixture(t *testing.T) *requireAuthFixture {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa: %v", err)
	}
	jwt, err := services.NewJWTServiceWithAudience(
		priv, &priv.PublicKey, "test", services.AudienceOperator,
		15*time.Minute, 7*24*time.Hour,
	)
	if err != nil {
		t.Fatalf("NewJWTServiceWithAudience: %v", err)
	}
	jwt.SetTenantProvider(stubTenant{})
	em := sharederrors.NewManager(silentTestLogger(), false)
	mw := NewAuthMiddleware(jwt, em)
	rev := newFakeRevocation()
	mw.SetSessionRevocation(rev)
	return &requireAuthFixture{t: t, priv: priv, jwt: jwt, revocation: rev, mw: mw}
}

// downstreamHandler reflects what the handler chain sees after
// RequireAuth fires — captures the user UUID + sid resolved from the
// context.
type downstreamHandler struct {
	called   bool
	userUUID string
	sid      string
}

func (h *downstreamHandler) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.called = true
		uid, _ := ctxauth.GetUserUUID(r.Context())
		h.userUUID = uid
		sid, _ := GetSessionID(r.Context())
		h.sid = sid
		w.WriteHeader(http.StatusOK)
	})
}

// issueTokenForUser is a small helper that mints an access token via
// the production GenerateAccessToken path. Mirrors what login flows
// produce so tests exercise the real validator on the way back in.
func (f *requireAuthFixture) issueTokenForUser(userUUID, role string) string {
	f.t.Helper()
	user := &iface.User{UUID: userUUID, Email: userUUID + "@example.com", Role: role}
	tok, err := f.jwt.GenerateAccessToken(user)
	if err != nil {
		f.t.Fatalf("GenerateAccessToken: %v", err)
	}
	return tok
}

// issueTokenWithSID mints a token whose sid claim matches the given
// value, so revocation tests can flip a known sid.
func (f *requireAuthFixture) issueTokenWithSID(userUUID, sid string) string {
	f.t.Helper()
	user := &iface.User{UUID: userUUID, Email: userUUID + "@example.com", Role: "operator"}
	// GenerateEnhancedAccessToken stamps SessionID from the security ctx.
	device := &authModels.DeviceInfo{DeviceID: "dev-A"}
	sec := &authModels.SecurityContext{SessionID: sid}
	tok, err := f.jwt.GenerateEnhancedAccessToken(user, device, sec)
	if err != nil {
		f.t.Fatalf("GenerateEnhancedAccessToken: %v", err)
	}
	return tok
}

// silentTestLogger swallows logs so test runs stay quiet.
func silentTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// ===== Cases =====

func TestRequireAuth_NoBearerToken_Returns401(t *testing.T) {
	f := newRequireAuthFixture(t)
	dh := &downstreamHandler{}
	srv := httptest.NewServer(f.mw.RequireAuth(dh.handler()))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/protected", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
	if dh.called {
		t.Errorf("downstream handler must NOT be reached without auth")
	}
}

func TestRequireAuth_MalformedToken_Returns401(t *testing.T) {
	f := newRequireAuthFixture(t)
	dh := &downstreamHandler{}
	srv := httptest.NewServer(f.mw.RequireAuth(dh.handler()))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/protected", nil)
	req.Header.Set("Authorization", "Bearer this-is-not-a-jwt")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
	if dh.called {
		t.Errorf("malformed token must not reach downstream")
	}
}

func TestRequireAuth_TamperedSignature_Returns401(t *testing.T) {
	f := newRequireAuthFixture(t)
	dh := &downstreamHandler{}
	srv := httptest.NewServer(f.mw.RequireAuth(dh.handler()))
	defer srv.Close()

	tok := f.issueTokenForUser("u-1", "operator")
	tampered := tok[:len(tok)-8] + "AAAAAAAA"

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/protected", nil)
	req.Header.Set("Authorization", "Bearer "+tampered)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
	if dh.called {
		t.Errorf("tampered signature must not reach downstream")
	}
}

func TestRequireAuth_ValidToken_PopulatesUserContext(t *testing.T) {
	f := newRequireAuthFixture(t)
	dh := &downstreamHandler{}
	srv := httptest.NewServer(f.mw.RequireAuth(dh.handler()))
	defer srv.Close()

	tok := f.issueTokenForUser("u-42", "operator")
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/protected", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if !dh.called {
		t.Fatalf("downstream handler must run for a valid token")
	}
	if dh.userUUID != "u-42" {
		t.Errorf("userUUID in context = %q, want u-42", dh.userUUID)
	}
}

func TestRequireAuth_RevokedSession_Returns401_SessionRevokedCode(t *testing.T) {
	f := newRequireAuthFixture(t)
	dh := &downstreamHandler{}
	srv := httptest.NewServer(f.mw.RequireAuth(dh.handler()))
	defer srv.Close()

	tok := f.issueTokenWithSID("u-7", "sess-doomed")
	// Mark this sid revoked before the request.
	_ = f.revocation.Revoke(context.Background(), "sess-doomed", "logout")

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/protected", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
	if dh.called {
		t.Errorf("revoked sid must NOT reach downstream")
	}
	// The middleware returns a distinct WWW-Authenticate code so the
	// SPA can render a different UX from "token expired".
	wa := resp.Header.Get("WWW-Authenticate")
	if wa == "" || wa != `Bearer error="session_revoked"` {
		t.Errorf("WWW-Authenticate = %q, want session_revoked", wa)
	}
}

// A session that reached its absolute cap must NOT be reported as "revoked".
//
// This is the path that actually reaches a user. ADR-0017 D4 gives the cap
// its own code, but the only emitters were /v1/auth/*/refresh-cookie (read by
// a raw fetch that discards the body on !res.ok) and /v1/auth/session
// (classified as an auth check, whose toast is suppressed). What the user saw
// instead was this middleware's session_revoked, emitted when their still-live
// access token hit the denylist — exactly the wording D4 calls inaccurate
// ("the distinction matters to whoever reads the support ticket").
func TestRequireAuth_SessionMaxAge_ReturnsDistinctCode(t *testing.T) {
	f := newRequireAuthFixture(t)
	dh := &downstreamHandler{}
	srv := httptest.NewServer(f.mw.RequireAuth(dh.handler()))
	defer srv.Close()

	tok := f.issueTokenWithSID("u-9", "sess-aged-out")
	// Exactly what expireSessionForMaxAge writes.
	_ = f.revocation.Revoke(context.Background(), "sess-aged-out", authModels.RevokeReasonSessionMaxAge)

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/protected", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
	if dh.called {
		t.Error("a capped-out sid must NOT reach downstream")
	}
	if wa := resp.Header.Get("WWW-Authenticate"); wa != `Bearer error="session_max_age_reached"` {
		t.Errorf("WWW-Authenticate = %q, want session_max_age_reached", wa)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"code":"session_max_age_reached"`) {
		t.Errorf("body = %s, want code session_max_age_reached — 'revoked' is inaccurate for a session that simply aged out", body)
	}
	if strings.Contains(string(body), "has been revoked") {
		t.Errorf("body = %s, still carries the revoked wording", body)
	}
}

// Any OTHER reason keeps the generic code — the new branch must be keyed on
// the cap's reason specifically, not on "a reason was stored".
func TestRequireAuth_OtherRevocationReason_KeepsGenericCode(t *testing.T) {
	f := newRequireAuthFixture(t)
	dh := &downstreamHandler{}
	srv := httptest.NewServer(f.mw.RequireAuth(dh.handler()))
	defer srv.Close()

	tok := f.issueTokenWithSID("u-10", "sess-pw-change")
	_ = f.revocation.Revoke(context.Background(), "sess-pw-change", "password_change")

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/protected", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"code":"session_revoked"`) {
		t.Errorf("body = %s, want the generic session_revoked code", body)
	}
}

// The reason reader is an OPTIONAL extension, so a fork's own
// SessionRevocationService — which implements only the two required methods —
// must still deny the request, just with the generic wording. Getting this
// wrong would either break forks at compile time or, worse, fail open.
func TestRequireAuth_RevocationWithoutReasonReader_StillDeniesGenerically(t *testing.T) {
	f := newRequireAuthFixture(t)
	blind := &reasonBlindRevocation{revoked: map[string]bool{}}
	f.mw.SetSessionRevocation(blind)

	dh := &downstreamHandler{}
	srv := httptest.NewServer(f.mw.RequireAuth(dh.handler()))
	defer srv.Close()

	tok := f.issueTokenWithSID("u-11", "sess-blind")
	// Even the cap's own reason cannot survive a service that discards it.
	_ = blind.Revoke(context.Background(), "sess-blind", authModels.RevokeReasonSessionMaxAge)

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/protected", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 — the optional extension must never gate the denial itself", resp.StatusCode)
	}
	if dh.called {
		t.Error("revoked sid reached downstream through the fallback path")
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"code":"session_revoked"`) {
		t.Errorf("body = %s, want the generic session_revoked fallback", body)
	}
}

func TestRequireAuth_DifferentSidNotRevoked_PassesThrough(t *testing.T) {
	// Sanity counter-test: the same fixture, a DIFFERENT sid, no
	// revocation entry → request passes. Guards against a regression
	// where IsRevoked() answers true for everything.
	f := newRequireAuthFixture(t)
	dh := &downstreamHandler{}
	srv := httptest.NewServer(f.mw.RequireAuth(dh.handler()))
	defer srv.Close()

	_ = f.revocation.Revoke(context.Background(), "some-other-sid", "logout")
	tok := f.issueTokenWithSID("u-8", "sess-fresh")

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/protected", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200 (different sid not revoked)", resp.StatusCode)
	}
	if !dh.called {
		t.Errorf("downstream must reach for non-revoked sid")
	}
	if dh.sid != "sess-fresh" {
		t.Errorf("sid in context = %q, want sess-fresh", dh.sid)
	}
}

type issuedSessionUsers struct{ iface.UserProvider }

func (issuedSessionUsers) UpdateUserLastLogin(context.Context, string) error { return nil }

type issuedSessionOAuthRepo struct {
	repository.OAuthProviderRepository
}

func (issuedSessionOAuthRepo) GetByUserUUID(context.Context, string) ([]*authModels.OAuthProviderDoc, error) {
	return nil, nil
}

type issuedSessionRefreshRepo struct {
	repository.RefreshTokenRepository
}

func (issuedSessionRefreshRepo) RevokeTokensByDevice(context.Context, string, string, string) error {
	return nil
}
func (issuedSessionRefreshRepo) CreateRefreshToken(context.Context, *authModels.RefreshTokenDoc) error {
	return nil
}
func (issuedSessionRefreshRepo) RevokeTokensBySession(context.Context, string, string) error {
	return nil
}

type issuedSessionRepo struct {
	repository.AuthSessionRepository
	mu   sync.Mutex
	docs map[string]*authModels.AuthSessionDoc
}

func (r *issuedSessionRepo) CreateSession(_ context.Context, doc *authModels.AuthSessionDoc) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	copy := *doc
	r.docs[doc.UUID] = &copy
	return nil
}

func (r *issuedSessionRepo) GetByUUID(_ context.Context, sessionID string) (*authModels.AuthSessionDoc, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	doc := r.docs[sessionID]
	if doc == nil {
		return nil, nil
	}
	copy := *doc
	return &copy, nil
}

func (r *issuedSessionRepo) TerminateSession(_ context.Context, sessionID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if doc := r.docs[sessionID]; doc != nil {
		doc.IsActive = false
	}
	return nil
}

func TestRequireAuth_SessionRevokedFromRealIssuedToken_DeniesOnlyThatSession(t *testing.T) {
	f := newRequireAuthFixture(t)
	sessions := &issuedSessionRepo{docs: map[string]*authModels.AuthSessionDoc{}}
	auth, err := services.NewAuthService(&services.AuthConfig{
		UserService:       issuedSessionUsers{},
		OAuthProviderRepo: issuedSessionOAuthRepo{},
		RefreshTokenRepo:  issuedSessionRefreshRepo{},
		AuthSessionRepo:   sessions,
		JWTService:        f.jwt,
	})
	if err != nil {
		t.Fatalf("NewAuthService: %v", err)
	}
	auth.SetSessionRevocation(f.revocation)
	user := &iface.User{UUID: "issued-user", Email: "issued@example.com", Role: "operator", IsActive: true}
	first, err := auth.GenerateEnhancedTokenPair(context.Background(), user, &authModels.DeviceInfo{DeviceID: "device-one"}, nil)
	if err != nil {
		t.Fatalf("issue first session: %v", err)
	}
	second, err := auth.GenerateEnhancedTokenPair(context.Background(), user, &authModels.DeviceInfo{DeviceID: "device-two"}, nil)
	if err != nil {
		t.Fatalf("issue second session: %v", err)
	}
	if err := auth.RevokeUserSession(context.Background(), user.UUID, first.SessionID, second.SessionID); err != nil {
		t.Fatalf("RevokeUserSession: %v", err)
	}

	for _, tc := range []struct {
		name       string
		token      string
		wantStatus int
		wantCalled bool
	}{
		{name: "revoked session", token: first.AccessToken, wantStatus: http.StatusUnauthorized},
		{name: "other session", token: second.AccessToken, wantStatus: http.StatusOK, wantCalled: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			downstream := &downstreamHandler{}
			req := httptest.NewRequest(http.MethodGet, "/protected", nil)
			req.Header.Set("Authorization", "Bearer "+tc.token)
			response := httptest.NewRecorder()
			f.mw.RequireAuth(downstream.handler()).ServeHTTP(response, req)
			if response.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", response.Code, tc.wantStatus, response.Body.String())
			}
			if downstream.called != tc.wantCalled {
				t.Fatalf("downstream called = %v, want %v", downstream.called, tc.wantCalled)
			}
			if !tc.wantCalled && !strings.Contains(response.Body.String(), `"code":"session_revoked"`) {
				t.Fatalf("body = %s, want session_revoked code", response.Body.String())
			}
		})
	}
}

// Compile-time guard that fakeRevocation satisfies the production
// SessionRevocationService interface. Drift surfaces immediately
// rather than as a confusing runtime error.
var _ services.SessionRevocationService = (*fakeRevocation)(nil)

// And that stubTenant satisfies iface.TenantProvider — same idea.
var _ iface.TenantProvider = stubTenant{}

// ===== Bearer-only perimeter (ADR-0020, #317) =====
//
// RequireAuth must never touch the refresh cookie. Rotation happens only
// through the explicit refresh endpoints — POST /v1/auth/{tier}/refresh-cookie
// and /refresh — and the read-only mint lives in GET /v1/auth/session. A
// middleware that rotated on any request lacking a valid bearer raced the
// SPA's own serialised refresh and signed operators out mid-session.

// testRefreshCookieName is the cookie name these tests present. It only
// has to match what the TEMP wiring below configures: after the fix the
// middleware has no notion of a cookie name at all (production reads its
// own from COOKIE_NAME_REFRESH, default "orkestra_cookie" — irrelevant here).
const testRefreshCookieName = "orkestra_cookie"

// refreshCookie mints a real refresh JWT and wraps it in the cookie.
func (f *requireAuthFixture) refreshCookie(userUUID string) *http.Cookie {
	f.t.Helper()
	user := &iface.User{UUID: userUUID, Email: userUUID + "@example.com", Role: "operator"}
	tok, err := f.jwt.GenerateRefreshToken(user)
	if err != nil {
		f.t.Fatalf("GenerateRefreshToken: %v", err)
	}
	return &http.Cookie{Name: testRefreshCookieName, Value: tok}
}

// mintExpiredAccessToken returns an access token signed by the fixture's
// key pair whose exp is already in the past: a sibling jwtService with a
// 1 ns access TTL. NOT a negative one — NewJWTService swaps any TTL <= 0
// for the 15-minute default and only clamps from above, so a negative
// value would mint a perfectly valid token. A positive nanosecond passes
// through untouched; exp is written as Unix seconds, so it truncates to
// the current second and jwt/v5 (no leeway) reports the token expired on
// the next validation. The fixture's validator then returns
// services.ErrTokenExpired — the exact production input of #317. The
// precondition below turns any drift in that reasoning into a loud
// failure rather than a test that passes for the wrong reason.
func (f *requireAuthFixture) mintExpiredAccessToken(userUUID string) string {
	f.t.Helper()
	expired, err := services.NewJWTServiceWithAudience(
		f.priv, &f.priv.PublicKey, "test", services.AudienceOperator,
		time.Nanosecond, 7*24*time.Hour,
	)
	if err != nil {
		f.t.Fatalf("NewJWTServiceWithAudience(expired): %v", err)
	}
	expired.SetTenantProvider(stubTenant{})
	user := &iface.User{UUID: userUUID, Email: userUUID + "@example.com", Role: "operator"}
	tok, err := expired.GenerateAccessToken(user)
	if err != nil {
		f.t.Fatalf("GenerateAccessToken(expired): %v", err)
	}
	if _, verr := f.jwt.ValidateAccessToken(tok); verr != services.ErrTokenExpired {
		f.t.Fatalf("precondition: want ErrTokenExpired from the fixture validator, got %v", verr)
	}
	return tok
}

// assertNoSilentRefresh is the whole contract in one place: 401, handler
// not reached, and — the part that matters — no cookie rotation and no
// minted token leaking out through headers.
func assertNoSilentRefresh(t *testing.T, resp *http.Response, dh *downstreamHandler) {
	t.Helper()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
	if dh.called {
		t.Errorf("downstream handler must NOT be reached on the strength of a refresh cookie")
	}
	if got := resp.Header.Get("Set-Cookie"); got != "" {
		t.Errorf("RequireAuth must never rotate the refresh cookie, got Set-Cookie=%q", got)
	}
	for _, h := range []string{"X-New-Access-Token", "X-Token-Refreshed"} {
		if got := resp.Header.Get(h); got != "" {
			t.Errorf("%s must not be emitted, got %q", h, got)
		}
	}
}

func TestRequireAuth_RefreshCookieWithoutBearer_Returns401_NeverRotates(t *testing.T) {
	f := newRequireAuthFixture(t)
	dh := &downstreamHandler{}
	srv := httptest.NewServer(f.mw.RequireAuth(dh.handler()))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/protected", nil)
	req.AddCookie(f.refreshCookie("u-cookie"))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	assertNoSilentRefresh(t, resp, dh)
}

func TestRequireAuth_ExpiredBearerWithRefreshCookie_Returns401_NeverRotates(t *testing.T) {
	f := newRequireAuthFixture(t)
	dh := &downstreamHandler{}
	srv := httptest.NewServer(f.mw.RequireAuth(dh.handler()))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/protected", nil)
	req.Header.Set("Authorization", "Bearer "+f.mintExpiredAccessToken("u-cookie"))
	req.AddCookie(f.refreshCookie("u-cookie"))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	assertNoSilentRefresh(t, resp, dh)
}

// The contract is "missing, expired OR invalid bearer": a tampered signature
// used to fall into the same cookie branch (ValidateAccessToken →
// ErrInvalidToken), so it gets the same guard. Same tampering idiom as
// TestRequireAuth_TamperedSignature_Returns401.
func TestRequireAuth_TamperedBearerWithRefreshCookie_Returns401_NeverRotates(t *testing.T) {
	f := newRequireAuthFixture(t)
	dh := &downstreamHandler{}
	srv := httptest.NewServer(f.mw.RequireAuth(dh.handler()))
	defer srv.Close()

	tok := f.issueTokenForUser("u-cookie", "operator")
	tampered := tok[:len(tok)-8] + "AAAAAAAA"

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/protected", nil)
	req.Header.Set("Authorization", "Bearer "+tampered)
	req.AddCookie(f.refreshCookie("u-cookie"))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	assertNoSilentRefresh(t, resp, dh)
}
