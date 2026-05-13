package middleware

// Phase 14c: integration coverage for RequireAuth — the security
// perimeter on every protected route. Previously 0% tested at this
// layer; the JWT validator and session-revocation service have their
// own unit tests, but the middleware's integration of them
// (extract → validate → revocation check → context populate) was
// only exercised end-to-end in production.
//
// Setup is intentionally minimal: real *jwtService (so we exercise
// the actual validator), in-memory revocation stub, no auth-service
// (silent-refresh path is exercised separately by the existing
// silent-refresh tests if any). httptest server captures the
// downstream handler's view of the request context.

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/orkestra-cc/orkestra-sdk/ctxauth"
	"github.com/orkestra-cc/orkestra-sdk/iface"
	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/services"
	userModels "github.com/orkestra/backend/internal/core/user/models"
	sharederrors "github.com/orkestra/backend/internal/shared/errors"
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
// populate `revoked` to flip a sid into the deny set.
type fakeRevocation struct {
	mu      sync.Mutex
	revoked map[string]bool
}

func newFakeRevocation() *fakeRevocation { return &fakeRevocation{revoked: map[string]bool{}} }

func (f *fakeRevocation) Revoke(_ context.Context, sid, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.revoked[sid] = true
	return nil
}

func (f *fakeRevocation) IsRevoked(_ context.Context, sid string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.revoked[sid], nil
}

// requireAuthFixture bundles the constructed middleware + dependencies
// so each test stays a couple of lines.
type requireAuthFixture struct {
	t          *testing.T
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
	return &requireAuthFixture{t: t, jwt: jwt, revocation: rev, mw: mw}
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
	user := &userModels.User{UUID: userUUID, Email: userUUID + "@example.com", Role: role}
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
	user := &userModels.User{UUID: userUUID, Email: userUUID + "@example.com", Role: "operator"}
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

// Compile-time guard that fakeRevocation satisfies the production
// SessionRevocationService interface. Drift surfaces immediately
// rather than as a confusing runtime error.
var _ services.SessionRevocationService = (*fakeRevocation)(nil)

// And that stubTenant satisfies iface.TenantProvider — same idea.
var _ iface.TenantProvider = stubTenant{}
