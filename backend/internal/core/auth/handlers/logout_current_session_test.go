package handlers

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/orkestra/backend/internal/core/auth/services"
	"github.com/orkestra/backend/internal/shared/config"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
)

type terminateRecorder struct {
	services.AuthService
	user, sid string
	calls     int
}

func (t *terminateRecorder) TerminateCurrentSession(_ context.Context, user, sid string) error {
	t.calls++
	t.user, t.sid = user, sid
	return nil
}

// A bearer-authenticated logout without a refresh cookie (mobile) must
// still end the bearer's own session doc, so push stops with the session.
func TestLogoutHTTP_BearerOnlyTerminatesCurrentSession(t *testing.T) {
	rec := &terminateRecorder{}
	cfg := &config.Config{}
	cfg.Auth.Cookie.Name = logoutTestCookieName
	h := &AuthHandler{authService: rec, config: cfg, jwtService: logoutTestHandler(t, logoutTestKey(t)).jwtService}

	r := logoutRequest("")
	ctx := context.WithValue(r.Context(), ctxauth.KeyUserUUID, "u-1")
	ctx = context.WithValue(ctx, ctxauth.KeySessionID, "sid-1")
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()
	h.LogoutHTTP(w, r)

	if rec.calls != 1 || rec.user != "u-1" || rec.sid != "sid-1" {
		t.Fatalf("TerminateCurrentSession calls=%d user=%q sid=%q", rec.calls, rec.user, rec.sid)
	}
}
