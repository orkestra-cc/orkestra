package handlers

// Security regression tests for the logout identity fallback.
//
// POST /v1/auth/{tier}/logout is mounted on the PUBLIC router
// (module.go → ri.Router), so no auth middleware ever populates
// ctx["userUUID"]. The handler therefore falls back to the refresh
// cookie to learn whose sessions to terminate. That fallback MUST
// verify the token's signature: without it, anyone who knows a user's
// UUID can forge a syntactically-valid JWT, present it as the cookie,
// and terminate every session of that user (allDevices=true), with no
// credentials at all.

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/services"
	"github.com/orkestra/backend/internal/shared/config"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

const logoutTestCookieName = "orkestra_cookie"

func logoutTestKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	return key
}

// logoutTestHandler builds the minimal AuthHandler slice the identity
// resolver needs: a real JWT service (so signature verification is
// genuinely exercised) plus the cookie name from config.
func logoutTestHandler(t *testing.T, key *rsa.PrivateKey) *AuthHandler {
	t.Helper()
	jwtSvc, err := services.NewJWTServiceWithAudience(key, &key.PublicKey, "test", services.AudienceOperator, 0, 0)
	if err != nil {
		t.Fatalf("NewJWTServiceWithAudience: %v", err)
	}
	cfg := &config.Config{}
	cfg.Auth.Cookie.Name = logoutTestCookieName
	return &AuthHandler{jwtService: jwtSvc, config: cfg}
}

// signedRefreshCookie mints a genuine refresh token for the user/device.
func signedRefreshCookie(t *testing.T, h *AuthHandler, userUUID, deviceID string) string {
	t.Helper()
	token, err := h.jwtService.GenerateEnhancedRefreshToken(
		&iface.User{UUID: userUUID, Email: "victim@example.com"},
		&models.DeviceInfo{DeviceID: deviceID},
		&models.SecurityContext{SessionID: "sess-1"},
	)
	if err != nil {
		t.Fatalf("GenerateEnhancedRefreshToken: %v", err)
	}
	return token
}

// forgedToken builds a structurally valid JWT with an attacker-chosen
// payload and a garbage signature — exactly what an unauthenticated
// caller can produce with no access to the signing key.
func forgedToken(t *testing.T, claims map[string]any) string {
	t.Helper()
	enc := func(v any) string {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		return base64.RawURLEncoding.EncodeToString(raw)
	}
	header := enc(map[string]any{"alg": "RS256", "typ": "JWT"})
	payload := enc(claims)
	return header + "." + payload + "." + base64.RawURLEncoding.EncodeToString([]byte("not-a-real-signature"))
}

func logoutRequest(cookieValue string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/v1/auth/operator/logout", nil)
	if cookieValue != "" {
		r.AddCookie(&http.Cookie{Name: logoutTestCookieName, Value: cookieValue})
	}
	return r
}

func TestResolveLogoutIdentity_RejectsForgedCookie(t *testing.T) {
	key := logoutTestKey(t)
	h := logoutTestHandler(t, key)

	// The attack: no credentials, just a hand-rolled JWT naming the victim.
	forged := forgedToken(t, map[string]any{
		"sub":  "victim-uuid",
		"type": "refresh",
		"did":  "victim-device",
		"iss":  "orkestra.test",
		"aud":  services.AudienceOperator,
		"exp":  1 << 40,
	})

	r := logoutRequest(forged)
	userUUID, _, ok := h.resolveLogoutIdentity(r.Context(), r)

	if ok {
		t.Fatalf("forged cookie must not resolve an identity, got userUUID=%q", userUUID)
	}
	if userUUID != "" {
		t.Errorf("userUUID = %q, want empty", userUUID)
	}
}

func TestResolveLogoutIdentity_AcceptsSignedCookie(t *testing.T) {
	key := logoutTestKey(t)
	h := logoutTestHandler(t, key)
	token := signedRefreshCookie(t, h, "real-user", "real-device")

	r := logoutRequest(token)
	userUUID, deviceID, ok := h.resolveLogoutIdentity(r.Context(), r)

	if !ok {
		t.Fatal("a properly signed refresh cookie must resolve an identity")
	}
	if userUUID != "real-user" {
		t.Errorf("userUUID = %q, want real-user", userUUID)
	}
	if deviceID != "real-device" {
		t.Errorf("deviceID = %q, want real-device", deviceID)
	}
}

func TestResolveLogoutIdentity_RejectsCookieSignedByAnotherKey(t *testing.T) {
	victimKey := logoutTestKey(t)
	h := logoutTestHandler(t, victimKey)

	// Attacker mints a well-formed token with their own key pair.
	attackerKey := logoutTestKey(t)
	attackerHandler := logoutTestHandler(t, attackerKey)
	token := signedRefreshCookie(t, attackerHandler, "victim-uuid", "victim-device")

	r := logoutRequest(token)
	userUUID, _, ok := h.resolveLogoutIdentity(r.Context(), r)

	if ok {
		t.Fatalf("token signed by a foreign key must be rejected, got userUUID=%q", userUUID)
	}
}

func TestResolveLogoutIdentity_RejectsAccessTokenInCookie(t *testing.T) {
	key := logoutTestKey(t)
	h := logoutTestHandler(t, key)

	access, err := h.jwtService.GenerateAccessToken(&iface.User{UUID: "real-user", Email: "u@example.com"})
	if err != nil {
		t.Fatalf("GenerateAccessToken: %v", err)
	}

	r := logoutRequest(access)
	if _, _, ok := h.resolveLogoutIdentity(r.Context(), r); ok {
		t.Fatal("an access token presented as the refresh cookie must be rejected")
	}
}

func TestResolveLogoutIdentity_NoCookieResolvesNothing(t *testing.T) {
	key := logoutTestKey(t)
	h := logoutTestHandler(t, key)

	r := logoutRequest("")
	if _, _, ok := h.resolveLogoutIdentity(r.Context(), r); ok {
		t.Fatal("a request with no refresh cookie must not resolve an identity")
	}
}

func TestResolveLogoutIdentity_PrefersAuthenticatedContext(t *testing.T) {
	key := logoutTestKey(t)
	h := logoutTestHandler(t, key)
	// Cookie names a different user; the authenticated context must win.
	token := signedRefreshCookie(t, h, "cookie-user", "cookie-device")

	r := logoutRequest(token)
	ctx := context.WithValue(r.Context(), ctxauth.KeyUserUUID, "context-user")

	userUUID, _, ok := h.resolveLogoutIdentity(ctx, r)
	if !ok {
		t.Fatal("an authenticated context must resolve an identity")
	}
	if userUUID != "context-user" {
		t.Errorf("userUUID = %q, want context-user", userUUID)
	}
}
