package handlers

// The refresh cookie is HttpOnly so that script on the SPA origin can never
// read a durable credential. POST /v1/auth/{tier}/refresh-cookie used to
// encode the whole TokenResponse — rotated refresh token included — which
// handed that credential to any script that could call the endpoint with
// credentials: 'include'. These tests pin the two shapes the handler now
// writes: a cookie-sourced refresh carries the access token and nothing
// durable; a body-sourced caller, who has no cookie, still receives the
// successor it needs.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/services"
	"github.com/orkestra/backend/internal/shared/config"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

type rotatingAuthService struct {
	services.AuthService // nil: anything not overridden panics
}

func (rotatingAuthService) PeekRefreshToken(context.Context, string) (*models.RefreshTokenDoc, error) {
	return &models.RefreshTokenDoc{ExpiresAt: time.Now().Add(time.Hour)}, nil
}

func (rotatingAuthService) RefreshTokensWithRiskAssessment(context.Context, string, *models.SecurityContext) (*models.TokenResponse, error) {
	return &models.TokenResponse{
		AccessToken:  "new-access",
		RefreshToken: "new-refresh",
		TokenType:    "Bearer",
		ExpiresIn:    900,
		SessionID:    "sid-1",
		DeviceID:     "did-1",
		User:         &iface.UserManagementResponse{ID: "user-1", Email: "u@example.com"},
	}, nil
}

func rotatingHandler(t *testing.T) *AuthHandler {
	t.Helper()
	cfg := &config.Config{}
	cfg.Auth.Cookie.Name = logoutTestCookieName
	return &AuthHandler{authService: rotatingAuthService{}, config: cfg, jwtService: newStepUpJWT(t)}
}

func TestRefreshCookie_CookieSourcedBodyCarriesNoRefreshToken(t *testing.T) {
	h := rotatingHandler(t)
	rec := httptest.NewRecorder()
	h.RefreshTokensHTTP(rec, withCookie(http.MethodPost, "/v1/auth/operator/refresh-cookie", "raw-cookie-token"))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	if _, leaked := body["refreshToken"]; leaked {
		t.Fatalf("refreshToken present in a cookie-sourced response body — HttpOnly is defeated: %s", rec.Body.String())
	}
	for _, durable := range []string{"sessionId", "deviceId", "mfaToken"} {
		if _, present := body[durable]; present {
			t.Errorf("%s present in the refresh body; the response carries the access token and nothing durable", durable)
		}
	}
	if body["accessToken"] != "new-access" {
		t.Fatalf("accessToken = %v, want new-access", body["accessToken"])
	}
	if body["expiresIn"] != float64(900) {
		t.Fatalf("expiresIn = %v, want 900", body["expiresIn"])
	}
	sc := rec.Header().Get("Set-Cookie")
	if !strings.Contains(sc, logoutTestCookieName+"=new-refresh") || !strings.Contains(sc, "HttpOnly") {
		t.Fatalf("Set-Cookie = %q, want the rotated token minted HttpOnly", sc)
	}
}

func TestRefreshCookie_BodySourcedCallerStillReceivesSuccessor(t *testing.T) {
	h := rotatingHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/operator/refresh-cookie", strings.NewReader(`{"refreshToken":"raw-body-token"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.RefreshTokensHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	if body["refreshToken"] != "new-refresh" {
		t.Fatalf("refreshToken = %v, want new-refresh — a body caller has no cookie and needs the successor", body["refreshToken"])
	}
	if sc := rec.Header().Get("Set-Cookie"); sc != "" {
		t.Fatalf("Set-Cookie = %q on a body-sourced refresh; no cookie was presented so none is minted", sc)
	}
}
