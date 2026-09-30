package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/services"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// The assertion gives RED a behavioral missing-contract failure before the
// registration method exists, rather than a compiler failure.
func registerInitialPasswordRoute(t *testing.T, h *PasswordAuthHandler, api huma.API) {
	t.Helper()
	registrar, ok := any(h).(interface{ RegisterInitialPasswordRoute(huma.API) })
	if !ok {
		t.Fatal("operator initial-password registration contract missing")
	}
	registrar.RegisterInitialPasswordRoute(api)
}

// Keep the service and Argon2 implementation real; substitute only storage.
type initialPasswordUsers struct {
	iface.UserProvider
	user      *iface.User
	setterErr error
}

func (u *initialPasswordUsers) GetUserByID(_ context.Context, id string) (*iface.User, error) {
	if id != u.user.UUID {
		return nil, iface.ErrUserNotFound
	}
	return u.user, nil
}

func (u *initialPasswordUsers) SetPasswordHashIfUnset(_ context.Context, id, hash string) error {
	if u.setterErr != nil {
		return u.setterErr
	}
	if id != u.user.UUID {
		return iface.ErrUserNotFound
	}
	if u.user.PasswordHash != "" {
		return iface.ErrPasswordAlreadySet
	}
	u.user.PasswordHash = hash
	return nil
}

func (u *initialPasswordUsers) ClearFailedLogins(context.Context, string) error { return nil }

type initialPasswordEvent struct {
	ctx       context.Context
	typ, user string
	fields    map[string]interface{}
}

func (e *initialPasswordEvent) RecordSelfAuthEvent(ctx context.Context, typ, user string, fields map[string]interface{}) {
	e.ctx, e.typ, e.user, e.fields = ctx, typ, user, fields
}

type initialPasswordHTTPEnv struct {
	users     *initialPasswordUsers
	passwords services.PasswordService
	policy    *services.AuthPolicyService
	event     *initialPasswordEvent
}

func newInitialPasswordHTTPEnv() *initialPasswordHTTPEnv {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return &initialPasswordHTTPEnv{
		users:     &initialPasswordUsers{user: &iface.User{UUID: "oauth-user", Email: "oauth@example.com", IsActive: true, EmailVerified: true}},
		passwords: services.NewPasswordService(logger, false),
		policy:    services.NewAuthPolicyServiceForTest(map[string]string{"passwordLoginEnabledAdmin": "true"}),
		event:     &initialPasswordEvent{},
	}
}

func (e *initialPasswordHTTPEnv) handler(setter iface.InitialPasswordSetter) *PasswordAuthHandler {
	svc := services.NewPasswordAuthService(services.PasswordAuthConfig{
		UserService: e.users, InitialPasswordSetter: setter, PasswordService: e.passwords,
		Policy: e.policy, Audience: services.PolicyAudienceOperator,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	svc.SetSecurityEventSink(e.event)
	return NewPasswordAuthHandler(svc, "", "", false)
}

func initialPasswordRequest(h http.Handler, body, user, sid string) (*httptest.ResponseRecorder, *http.Request) {
	r := httptest.NewRequest(http.MethodPost, "/v1/auth/operator/me/password", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	ctx := context.WithValue(r.Context(), "userUUID", user)
	if sid != "" {
		ctx = context.WithValue(ctx, "claims", &authModels.JWTClaims{SessionID: sid})
	}
	ctx = context.WithValue(ctx, "http_request", r)
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w, r
}

func TestSetInitialPassword_HTTPPreservesIdentityAndContext(t *testing.T) {
	for _, sid := range []string{"caller-sid", ""} {
		t.Run("sid="+sid, func(t *testing.T) {
			e := newInitialPasswordHTTPEnv()
			router := chi.NewRouter()
			api := humachi.New(router, huma.DefaultConfig("test", "1"))
			registerInitialPasswordRoute(t, e.handler(e.users), api)
			w, r := initialPasswordRequest(router, `{"newPassword":"fresh-passphrase-123"}`, "oauth-user", sid)
			if w.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			var body struct {
				Success bool   `json:"success"`
				Message string `json:"message"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || !body.Success || body.Message == "" {
				t.Fatalf("invalid success response: %s (%v)", w.Body.String(), err)
			}
			if ok, err := e.passwords.Verify("fresh-passphrase-123", e.users.user.PasswordHash); !ok || err != nil {
				t.Fatalf("submitted password not persisted: ok=%v err=%v", ok, err)
			}
			if e.event.ctx == nil || e.event.ctx.Value("http_request") != r.Context().Value("http_request") {
				t.Fatal("incoming HTTP context must reach the credential event sink intact")
			}
			if e.event.typ != "self_password_added" || e.event.user != "oauth-user" || e.event.fields["currentSessionId"] != sid {
				t.Fatalf("wrong event identity or SID: %+v", e.event)
			}
		})
	}
}

func TestSetInitialPassword_HandlerPreservesContext(t *testing.T) {
	e := newInitialPasswordHTTPEnv()
	r := httptest.NewRequest(http.MethodPost, "/v1/auth/operator/me/password", nil)
	ctx := context.WithValue(r.Context(), "http_request", r)
	ctx = context.WithValue(ctx, "userUUID", "oauth-user")
	ctx = context.WithValue(ctx, "claims", &authModels.JWTClaims{SessionID: "caller-sid"})
	req := &SetInitialPasswordRequest{}
	req.Body.NewPassword = "fresh-passphrase-123"
	resp, err := e.handler(e.users).SetInitialPassword(ctx, req)
	if err != nil || resp == nil || !resp.Body.Success {
		t.Fatalf("response=%+v error=%v", resp, err)
	}
	if e.event.ctx != ctx {
		t.Fatal("handler must forward the exact incoming context to the service")
	}
}

func TestSetInitialPassword_HTTPRefusals(t *testing.T) {
	for _, tc := range []struct {
		name          string
		setup         func(*initialPasswordHTTPEnv)
		user          string
		missingSetter bool
		status        int
		code          string
	}{
		{name: "unauthenticated", status: 401},
		{name: "already set", user: "oauth-user", setup: func(e *initialPasswordHTTPEnv) { e.users.user.PasswordHash = "existing" }, status: 409, code: "auth.password_already_set"},
		{name: "setter race", user: "oauth-user", setup: func(e *initialPasswordHTTPEnv) { e.users.setterErr = iface.ErrPasswordAlreadySet }, status: 409, code: "auth.password_already_set"},
		{name: "missing capability", user: "oauth-user", missingSetter: true, status: 503, code: "auth.unavailable"},
		{name: "unverified email", user: "oauth-user", setup: func(e *initialPasswordHTTPEnv) { e.users.user.EmailVerified = false }, status: 403, code: "auth.email_not_verified"},
		{name: "password method disabled", user: "oauth-user", setup: func(e *initialPasswordHTTPEnv) {
			e.policy = services.NewAuthPolicyServiceForTest(map[string]string{"passwordLoginEnabledAdmin": "false"})
		}, status: 403, code: "auth.password_login_disabled"},
		{name: "policy outage", user: "oauth-user", setup: func(e *initialPasswordHTTPEnv) {
			e.policy = services.NewAuthPolicyServiceForTestErr(errors.New("unavailable"))
		}, status: 503, code: "auth.policy_unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newInitialPasswordHTTPEnv()
			if tc.setup != nil {
				tc.setup(e)
			}
			before := e.users.user.PasswordHash
			var setter iface.InitialPasswordSetter = e.users
			if tc.missingSetter {
				setter = nil
			}
			router := chi.NewRouter()
			api := humachi.New(router, huma.DefaultConfig("test", "1"))
			registerInitialPasswordRoute(t, e.handler(setter), api)
			w, _ := initialPasswordRequest(router, `{"newPassword":"fresh-passphrase-123"}`, tc.user, "caller-sid")
			var body struct {
				Code string `json:"code"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if w.Code != tc.status || body.Code != tc.code {
				t.Fatalf("status=%d code=%q body=%s", w.Code, body.Code, w.Body.String())
			}
			if e.users.user.PasswordHash != before || e.event.ctx != nil {
				t.Fatal("refusal must not persist or announce a password")
			}
		})
	}
}

func TestSetInitialPassword_HTTPRejectsExtraAndMissingFields(t *testing.T) {
	for _, body := range []string{
		`{}`, `{"newPassword":"short"}`,
		`{"newPassword":"fresh-passphrase-123","email":"other@example.com"}`,
		`{"newPassword":"fresh-passphrase-123","currentPassword":"old"}`,
		`{"newPassword":"fresh-passphrase-123","confirmPassword":"fresh-passphrase-123"}`,
		`{"newPassword":"fresh-passphrase-123","userId":"other-user"}`,
		`{"newPassword":"fresh-passphrase-123","audience":"client"}`,
	} {
		t.Run(body, func(t *testing.T) {
			e := newInitialPasswordHTTPEnv()
			router := chi.NewRouter()
			api := humachi.New(router, huma.DefaultConfig("test", "1"))
			registerInitialPasswordRoute(t, e.handler(e.users), api)
			w, _ := initialPasswordRequest(router, body, "oauth-user", "caller-sid")
			want := http.StatusUnprocessableEntity
			if body == `{"newPassword":"short"}` {
				want = http.StatusBadRequest
			}
			if w.Code != want {
				t.Fatalf("status=%d want=%d body=%s", w.Code, want, w.Body.String())
			}
			if e.users.user.PasswordHash != "" || e.event.ctx != nil {
				t.Fatal("invalid input must not persist or announce a password")
			}
		})
	}
}
