package handlers

// HTTP-level logout tests through a tier mux built the way main.go builds
// it: RequireAudience at the mux level, the logout route mounted on the raw
// mux by RegisterTierMountableRoutes, and NO auth middleware in front of it.
// Production never injects an identity into the context of this route, so
// these tests do not either — the session must be found from the bearer or
// the refresh cookie alone.

import (
	"context"
	"crypto/rsa"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/repository"
	"github.com/orkestra/backend/internal/core/auth/services"
	"github.com/orkestra/backend/internal/shared/config"
	"github.com/orkestra/backend/internal/shared/middleware"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

type memRevocation struct {
	mu  sync.Mutex
	set map[string]bool
}

func (m *memRevocation) Revoke(_ context.Context, sid, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.set == nil {
		m.set = map[string]bool{}
	}
	m.set[sid] = true
	return nil
}

func (m *memRevocation) IsRevoked(_ context.Context, sid string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.set[sid], nil
}

type logoutMuxHarness struct {
	mux       *chi.Mux
	repo      repository.AuthSessionRepository
	svc       services.AuthService
	rev       *memRevocation
	key       *rsa.PrivateKey
	operator  services.JWTService
	clientJWT services.JWTService
}

func newLogoutMuxHarness(t *testing.T) *logoutMuxHarness {
	t.Helper()
	uri := os.Getenv("MONGO_TEST_URI")
	if uri == "" {
		t.Skip("set MONGO_TEST_URI to run the logout HTTP test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	db := client.Database(fmt.Sprintf("auth_logout_bearer_test_%d", time.Now().UnixNano()))
	t.Cleanup(func() {
		_ = db.Drop(context.Background())
		_ = client.Disconnect(context.Background())
	})

	// Both tiers sign with the same key pair in production; only the
	// audience tells their tokens apart.
	key := logoutTestKey(t)
	operatorJWT, err := services.NewJWTServiceWithAudience(key, &key.PublicKey, "test", services.AudienceOperator, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	clientJWT, err := services.NewJWTServiceWithAudience(key, &key.PublicKey, "test", services.AudienceClient, 0, 0)
	if err != nil {
		t.Fatal(err)
	}

	repo := repository.NewOperatorAuthSessionRepository(db)
	rev := &memRevocation{}
	svc, err := services.NewAuthService(&services.AuthConfig{
		AuthSessionRepo:  repo,
		RefreshTokenRepo: repository.NewOperatorRefreshTokenRepository(db),
		JWTService:       operatorJWT,
	})
	if err != nil {
		t.Fatal(err)
	}
	svc.SetSessionRevocation(rev)

	cfg := &config.Config{}
	cfg.Auth.Cookie.Name = logoutTestCookieName
	h := NewAuthHandler(svc, nil, nil, nil, nil, operatorJWT, cfg, "")
	h.SetSessionRevocation(rev)
	h.SetTier(services.AudienceOperator)

	mux := chi.NewRouter()
	mux.Use(middleware.RequireAudience(services.AudienceOperator, "service"))
	api := humachi.New(mux, huma.DefaultConfig("test", "1"))
	protected := humachi.New(chi.NewRouter(), huma.DefaultConfig("test", "1"))
	h.RegisterTierMountableRoutes(api, protected, mux, OperatorMount)

	return &logoutMuxHarness{mux: mux, repo: repo, svc: svc, rev: rev, key: key, operator: operatorJWT, clientJWT: clientJWT}
}

func (hh *logoutMuxHarness) seedSession(t *testing.T, sid, user, device string) {
	t.Helper()
	if err := hh.repo.CreateSession(context.Background(), &models.AuthSessionDoc{
		UUID: sid, UserUUID: user, DeviceID: device, ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("create session: %v", err)
	}
}

func accessFor(t *testing.T, j services.JWTService, user, device, sid string) string {
	t.Helper()
	tok, err := j.GenerateEnhancedAccessToken(&iface.User{UUID: user, Email: user + "@example.com"},
		&models.DeviceInfo{DeviceID: device}, &models.SecurityContext{SessionID: sid})
	if err != nil {
		t.Fatalf("mint access token: %v", err)
	}
	return tok
}

func (hh *logoutMuxHarness) logout(t *testing.T, bearer, cookie string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/v1/auth/operator/logout", nil)
	if bearer != "" {
		r.Header.Set("Authorization", "Bearer "+bearer)
	}
	if cookie != "" {
		r.AddCookie(&http.Cookie{Name: logoutTestCookieName, Value: cookie})
	}
	w := httptest.NewRecorder()
	hh.mux.ServeHTTP(w, r)
	return w
}

func (hh *logoutMuxHarness) active(t *testing.T, sid string) bool {
	t.Helper()
	reader, ok := hh.svc.(iface.SessionActivityReader)
	if !ok {
		t.Fatal("auth service must satisfy iface.SessionActivityReader")
	}
	got, err := reader.ActiveSessions(context.Background(), []string{sid})
	if err != nil {
		t.Fatal(err)
	}
	return got[sid]
}

func (hh *logoutMuxHarness) docActive(t *testing.T, sid string) bool {
	t.Helper()
	d, err := hh.repo.GetByUUID(context.Background(), sid)
	if err != nil || d == nil {
		t.Fatalf("get session %s: %v", sid, err)
	}
	return d.IsActive
}

func TestLogoutMux_BearerOnlyEndsTheSession(t *testing.T) {
	hh := newLogoutMuxHarness(t)
	hh.seedSession(t, "sid-mobile", "u-1", "dev-1")
	hh.seedSession(t, "sid-other", "u-1", "dev-2")

	w := hh.logout(t, accessFor(t, hh.operator, "u-1", "dev-1", "sid-mobile"), "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", w.Code, w.Body.String())
	}
	if hh.docActive(t, "sid-mobile") {
		t.Fatal("bearer-only logout must flip the session doc to inactive")
	}
	if hh.active(t, "sid-mobile") {
		t.Fatal("ActiveSessions must report the logged-out session as ended")
	}
	if ok, _ := hh.rev.IsRevoked(context.Background(), "sid-mobile"); !ok {
		t.Fatal("the bearer's sid must be in the revocation set")
	}
	if !hh.docActive(t, "sid-other") || !hh.active(t, "sid-other") {
		t.Fatal("the user's other session must stay active")
	}
}

func TestLogoutMux_UnverifiableBearerTerminatesNothing(t *testing.T) {
	hh := newLogoutMuxHarness(t)
	hh.seedSession(t, "sid-victim", "victim", "dev-v")

	otherKey := logoutTestKey(t)
	foreignJWT, err := services.NewJWTServiceWithAudience(otherKey, &otherKey.PublicKey, "test", services.AudienceOperator, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	expired, err := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"sub": "victim", "type": "access", "iss": "orkestra.test", "aud": services.AudienceOperator,
		"sid": "sid-victim", "exp": time.Now().Add(-time.Minute).Unix(), "iat": time.Now().Add(-time.Hour).Unix(),
	}).SignedString(hh.key)
	if err != nil {
		t.Fatal(err)
	}

	cases := map[string]struct {
		token      string
		wantStatus int
	}{
		"bad signature": {accessFor(t, foreignJWT, "victim", "dev-v", "sid-victim"), http.StatusOK},
		"forged":        {forgedToken(t, map[string]any{"sub": "victim", "type": "access", "iss": "orkestra.test", "aud": services.AudienceOperator, "sid": "sid-victim", "exp": 1 << 40}), http.StatusOK},
		"expired":       {expired, http.StatusOK},
		// The mux gate refuses a client-audience token on the operator host
		// before the handler runs.
		"client token on operator mux": {accessFor(t, hh.clientJWT, "victim", "dev-v", "sid-victim"), http.StatusUnauthorized},
		"refresh token as bearer": {func() string {
			tok, err := hh.operator.GenerateEnhancedRefreshToken(&iface.User{UUID: "victim"}, &models.DeviceInfo{DeviceID: "dev-v"}, &models.SecurityContext{SessionID: "sid-victim"})
			if err != nil {
				t.Fatal(err)
			}
			return tok
		}(), http.StatusOK},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			w := hh.logout(t, tc.token, "")
			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)", w.Code, tc.wantStatus, w.Body.String())
			}
			if !hh.docActive(t, "sid-victim") || !hh.active(t, "sid-victim") {
				t.Fatal("an unverifiable bearer must terminate nothing")
			}
			if ok, _ := hh.rev.IsRevoked(context.Background(), "sid-victim"); ok {
				t.Fatal("an unverifiable bearer must revoke nothing")
			}
		})
	}
}

func TestLogoutMux_CookieLogoutUnchanged(t *testing.T) {
	hh := newLogoutMuxHarness(t)
	hh.seedSession(t, "sid-web", "u-web", "dev-web")
	hh.seedSession(t, "sid-phone", "u-web", "dev-phone")

	cookie, err := hh.operator.GenerateEnhancedRefreshToken(&iface.User{UUID: "u-web", Email: "w@example.com"},
		&models.DeviceInfo{DeviceID: "dev-web"}, &models.SecurityContext{SessionID: "sid-web"})
	if err != nil {
		t.Fatal(err)
	}
	w := hh.logout(t, "", cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if hh.docActive(t, "sid-web") || hh.active(t, "sid-web") {
		t.Fatal("cookie logout must end the cookie device's session")
	}
	if !hh.docActive(t, "sid-phone") {
		t.Fatal("cookie logout must leave other devices alone")
	}
	cleared := false
	for _, c := range w.Result().Cookies() {
		if c.Name == logoutTestCookieName && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("logout must clear the refresh cookie")
	}
}

// Defence in depth below the mux gate: the operator mux also admits the
// service audience, and the JWT service does not pin the audience, so the
// handler itself refuses a token minted for another tier.
func TestBearerLogoutIdentity_RefusesAnotherTiersAudience(t *testing.T) {
	key := logoutTestKey(t)
	h := logoutTestHandler(t, key)
	h.SetTier(services.AudienceOperator)
	clientJWT, err := services.NewJWTServiceWithAudience(key, &key.PublicKey, "test", services.AudienceClient, 0, 0)
	if err != nil {
		t.Fatal(err)
	}

	r := logoutRequest("")
	r.Header.Set("Authorization", "Bearer "+accessFor(t, clientJWT, "u", "d", "sid-c"))
	if _, _, ok := h.bearerLogoutIdentity(r.Context(), r); ok {
		t.Fatal("a client-audience bearer must not identify an operator logout")
	}

	r = logoutRequest("")
	r.Header.Set("Authorization", "Bearer "+accessFor(t, h.jwtService, "u", "d", "sid-o"))
	user, sid, ok := h.bearerLogoutIdentity(r.Context(), r)
	if !ok || user != "u" || sid != "sid-o" {
		t.Fatalf("own-tier bearer: user=%q sid=%q ok=%v", user, sid, ok)
	}

	rev := &memRevocation{}
	_ = rev.Revoke(context.Background(), "sid-o", "logout")
	h.SetSessionRevocation(rev)
	if _, _, ok := h.bearerLogoutIdentity(r.Context(), r); ok {
		t.Fatal("a bearer whose sid is already revoked must be refused, as RequireAuth refuses it")
	}
}
