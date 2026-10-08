package handlers

// Wire shape for a refresh whose session has been terminated: 401 with the
// `session_revoked` code the middleware already uses for the same fact, and
// the refresh cookie expired, because the session is durably gone and a
// browser that keeps presenting the cookie would keep hitting the same 401.

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
)

type inactiveSessionAuthService struct {
	services.AuthService // nil: anything not overridden panics
}

func (inactiveSessionAuthService) PeekRefreshToken(context.Context, string) (*models.RefreshTokenDoc, error) {
	return &models.RefreshTokenDoc{ExpiresAt: time.Now().Add(time.Hour)}, nil
}

func (inactiveSessionAuthService) RefreshTokensWithRiskAssessment(context.Context, string, *models.SecurityContext) (*models.TokenResponse, error) {
	return nil, services.ErrRefreshSessionInactive
}

func (inactiveSessionAuthService) MintAccessTokenFromRefresh(context.Context, string, *models.SecurityContext) (*models.TokenResponse, error) {
	return nil, services.ErrRefreshSessionInactive
}

func inactiveSessionHandler() *AuthHandler {
	cfg := &config.Config{}
	cfg.Auth.Cookie.Name = logoutTestCookieName
	return &AuthHandler{authService: inactiveSessionAuthService{}, config: cfg}
}

func assertSessionRevoked401(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (%s)", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	if body["code"] != "session_revoked" {
		t.Fatalf("code = %v, want session_revoked", body["code"])
	}
	sc := rec.Header().Get("Set-Cookie")
	if !strings.Contains(sc, logoutTestCookieName+"=") || !strings.Contains(sc, "Max-Age=0") {
		t.Fatalf("Set-Cookie = %q, want the refresh cookie expired — the session is durably gone", sc)
	}
}

func TestRefreshCookie_TerminatedSessionIs401SessionRevokedAndClearsTheCookie(t *testing.T) {
	rec := httptest.NewRecorder()
	inactiveSessionHandler().RefreshTokensHTTP(rec, withCookie(http.MethodPost, "/v1/auth/operator/refresh-cookie"))
	assertSessionRevoked401(t, rec)
}

func TestSessionBootstrap_TerminatedSessionIs401SessionRevokedAndClearsTheCookie(t *testing.T) {
	rec := httptest.NewRecorder()
	inactiveSessionHandler().GetSessionHTTP(rec, withCookie(http.MethodGet, "/v1/auth/session"))
	assertSessionRevoked401(t, rec)
}
