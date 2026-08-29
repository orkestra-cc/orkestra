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
// the OBSERVABLE contract for that claim (no Set-Cookie, no minted-
// token header). The two structural tests further down —
// TestAuthMiddleware_Fields_CannotReintroduceCookieRotation and
// TestAuthGo_ContainsNoCookieRead — are the actual reintroduction
// guards; see their doc comments for what each one covers and what it
// doesn't. httptest server captures the downstream handler's view of
// the request context.

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
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
//
// What the three "*_NeverRotates" tests below do and do not cover: they pin
// the OBSERVABLE contract for a request that shows up with no valid bearer
// alongside a refresh cookie — 401, downstream handler not reached, no
// Set-Cookie, no minted-token header. They do NOT, by themselves, guard
// against the deleted cookie branch being reintroduced: they build the
// middleware via NewAuthMiddleware, which never wires an auth service or
// config, so — with that seam gone — they would pass unchanged against the
// pre-fix code too. TestAuthMiddleware_Fields_CannotReintroduceCookieRotation
// below is the actual reintroduction guard; see its doc comment for why a
// structural check is what that job needs.

// TestAuthMiddleware_Fields_CannotReintroduceCookieRotation is a structural
// tripwire, not a behavioural test. The behavioural tests above (and the
// three "*_NeverRotates" tests below) can only exercise code paths that
// exist; #317 deleted the cookie branch outright — along with the
// authService/cookieName/config fields, NewAuthMiddlewareWithConfig,
// SetAuthService, and the cookie helpers — so there is no path left to send
// a request down and observe. A behavioural regression test for "the
// deleted branch stays deleted" is therefore a contradiction: nothing built
// through the public constructor can wire it back. The only thing left to
// watch is the SHAPE of the type. This test reflects over AuthMiddleware's
// field names and diffs them, in both directions, against the explicit set
// below — so it fails the moment anyone adds a field back, before a single
// line of behaviour is written against it.
func TestAuthMiddleware_Fields_CannotReintroduceCookieRotation(t *testing.T) {
	// Keep in sync with the field list in auth.go's AuthMiddleware struct.
	expectedFields := map[string]struct{}{
		"jwtService":             {},
		"tenant":                 {},
		"access":                 {},
		"authz":                  {},
		"auditSink":              {},
		"sessionRevocation":      {},
		"sessionRiskLookup":      {},
		"mfaEnrollment":          {},
		"stepUpPolicy":           {},
		"users":                  {},
		"errorManager":           {},
		"impersonationDedupe":    {},
		"impersonationDedupeTTL": {},
	}

	typ := reflect.TypeOf(AuthMiddleware{})
	actualFields := make(map[string]struct{}, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		actualFields[typ.Field(i).Name] = struct{}{}
	}

	var unexpected, missing []string
	for name := range actualFields {
		if _, ok := expectedFields[name]; !ok {
			unexpected = append(unexpected, name)
		}
	}
	for name := range expectedFields {
		if _, ok := actualFields[name]; !ok {
			missing = append(missing, name)
		}
	}
	sort.Strings(unexpected)
	sort.Strings(missing)

	if len(unexpected) > 0 {
		t.Errorf(
			"AuthMiddleware gained field(s) not in the expected set: %v\n\n"+
				"RequireAuth is bearer-only per ADR-0020 / #317 (see the "+
				"\"RequireAuth is bearer-only\" bullet in "+
				"backend/internal/core/auth/CLAUDE.md). A field carrying an "+
				"auth service, a config struct, or a cookie name is exactly "+
				"the seam that let the old code silently rotate the refresh "+
				"cookie on any request with a missing/expired/invalid bearer "+
				"— do not add one to make this test pass.\n\n"+
				"If %v is a legitimately unrelated new field, first confirm "+
				"it gives RequireAuth no way to read or rotate a cookie or "+
				"mint credentials outside the explicit refresh endpoints, "+
				"THEN add its name to expectedFields in this test.",
			unexpected, unexpected,
		)
	}
	if len(missing) > 0 {
		t.Errorf(
			"AuthMiddleware lost expected field(s): %v — update expectedFields "+
				"in this test (and, if the removal is significant, the "+
				"\"RequireAuth is bearer-only\" bullet in "+
				"backend/internal/core/auth/CLAUDE.md) so this list keeps "+
				"tracking the real struct instead of silently going stale.",
			missing,
		)
	}
}

// TestAuthGo_ContainsNoCookieRead is the second structural guard, closing
// the gap the field-diff test above leaves open. #317's rejected
// alternative — ADR-0020's "Alternatives considered" section — needs no
// new struct field at all: services.JWTService is already wired in as
// m.jwtService, and it already exposes ValidateRefreshToken and
// GenerateAccessToken/GenerateAccessTokenWithAMR. So a "mint-only" rewrite
// of RequireAuth could read the refresh cookie, validate it with the
// already-injected jwtService, mint a fresh access token, and return it
// via X-New-Access-Token — reproducing exactly the variant ADR-0020
// rejected — without adding, removing or renaming a single field. The
// field-diff test would stay green throughout; this test is what catches
// that shape.
//
// It parses auth.go — the file RequireAuth and its helpers live in — with
// go/parser + go/ast, deliberately NOT a regex over the raw source: a
// comment that merely mentions ".Cookie(" must not trip it, and a real
// call must not be missed because of formatting. It walks every call
// expression for a selector call named Cookie or Cookies — the two
// *http.Request methods that read a cookie off an incoming request — and
// fails if either appears anywhere in the file.
//
// This is deliberately FILE-scoped, not package-scoped: device.go, in
// this same package, legitimately reads the device-id cookie
// (DeviceIDCookieName, "orkestra_did") via r.Cookie — a package-wide
// assertion would fail against correct code. The residual gap this
// leaves: a helper placed in a *different* file of this package (or one
// called indirectly through another package) could still read a cookie
// and hand the value into RequireAuth unnoticed. This narrows the
// reintroduction surface; it does not close it — reviewers still carry
// that residue.
func TestAuthGo_ContainsNoCookieRead(t *testing.T) {
	const path = "auth.go"
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	var offenders []string
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if sel.Sel.Name != "Cookie" && sel.Sel.Name != "Cookies" {
			return true
		}
		pos := fset.Position(call.Pos())
		var buf bytes.Buffer
		if err := printer.Fprint(&buf, fset, call); err != nil {
			buf.WriteString(sel.Sel.Name + "(...)")
		}
		offenders = append(offenders, fmt.Sprintf("%s:%d: %s", path, pos.Line, buf.String()))
		return true
	})

	if len(offenders) > 0 {
		t.Errorf(
			"auth.go contains a cookie read: %v\n\n"+
				"RequireAuth is bearer-only per ADR-0020 / #317. The alternative "+
				"ADR-0020's \"Alternatives considered\" section explicitly "+
				"rejected — a MINT-ONLY middleware — needs no new struct field: "+
				"services.JWTService is already wired in as m.jwtService, and it "+
				"already exposes ValidateRefreshToken and "+
				"GenerateAccessToken/GenerateAccessTokenWithAMR. Reading the "+
				"refresh cookie here, validating it with the already-injected "+
				"jwtService, and minting a fresh access token reproduces exactly "+
				"that rejected variant — and "+
				"TestAuthMiddleware_Fields_CannotReintroduceCookieRotation will "+
				"NOT catch it, because no field is added, removed or renamed. "+
				"The sanctioned client recovery for a missing/expired/invalid "+
				"bearer is 401 -> POST /v1/auth/{tier}/refresh-cookie -> retry, "+
				"not a silent mint here. See the \"RequireAuth is bearer-only\" "+
				"bullet in backend/internal/core/auth/CLAUDE.md.",
			offenders,
		)
	}
}

// testRefreshCookieName is the cookie name these tests present, so the
// tests can show the middleware ignores it: after the fix RequireAuth has
// no notion of a cookie name at all — no field to configure, no branch that
// reads one — so this constant need not match anything production uses
// (production reads its own from COOKIE_NAME_REFRESH, default
// "orkestra_cookie" — irrelevant here).
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

// mintExpiredAccessToken returns an access token signed directly with the
// fixture's private key whose exp is already in the past. It no longer
// goes through NewJWTService at all: since NewJWTService clamps every
// accessTTL into [MinAccessTokenTTL, MaxAccessTokenTTL] (ADR-0020 D3, #317),
// a 1ns TTL — the previous trick for minting an already-expired token
// without tripping the constructor's `<= 0` default — now clamps up to
// MinAccessTokenTTL (60s) and mints a perfectly valid token instead.
//
// Signing by hand sidesteps that clamp entirely. The claim set mirrors
// what jwtService.GenerateEnhancedAccessToken actually stamps (sub,
// email, srole, type, iat/nbf/exp, iss, aud, sid, did, scope) so this is
// a faithful stand-in for a production-issued token, just with exp
// already elapsed. jwt/v5's default Parser always validates exp when
// present (no parser options are needed to opt in — see
// (*jwt.Validator).Validate), so jwt.Parse fails with an error wrapping
// jwt.ErrTokenExpired before validateTokenEnhanced ever reaches its own
// type/issuer/audience checks — those checks, and their claim values
// here, are therefore irrelevant to *why* validation fails, but are kept
// accurate for shape fidelity. The fixture's validator maps that to
// services.ErrTokenExpired — the exact production input of #317. The
// precondition below turns any drift in that reasoning into a loud
// failure rather than a test that passes for the wrong reason.
func (f *requireAuthFixture) mintExpiredAccessToken(userUUID string) string {
	f.t.Helper()
	now := time.Now()
	issuedAt := now.Add(-2 * time.Hour)
	expiresAt := now.Add(-1 * time.Hour)
	claims := jwt.MapClaims{
		"sub":   userUUID,
		"email": userUUID + "@example.com",
		"srole": "operator",
		"type":  "access",
		"iat":   issuedAt.Unix(),
		"nbf":   issuedAt.Unix(),
		"exp":   expiresAt.Unix(),
		"iss":   "orkestra.test", // matches issuerFor("test"), the env newRequireAuthFixture builds f.jwt with
		"aud":   services.AudienceOperator,
		"sid":   "sess-expired",
		"did":   "default",
		"scope": []string{"profile", "email", "api"},
	}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(f.priv)
	if err != nil {
		f.t.Fatalf("sign expired access token: %v", err)
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
