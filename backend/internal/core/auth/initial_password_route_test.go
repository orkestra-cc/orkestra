package auth

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/orkestra/backend/internal/core/auth/handlers"
	"github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/services"
	sharederrors "github.com/orkestra/backend/internal/shared/errors"
	authMiddleware "github.com/orkestra/backend/internal/shared/middleware"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/module"
)

// Observe the real gates at the registration boundary; their proof semantics
// remain covered by the middleware package's RequireEnrolmentProof tests.
type initialPasswordRouteMiddleware struct {
	*authMiddleware.AuthMiddleware
	trace *[]string
}

func (m initialPasswordRouteMiddleware) observe(name string, gate func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		gated := gate(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			*m.trace = append(*m.trace, name)
			gated.ServeHTTP(w, r)
		})
	}
}

func (m initialPasswordRouteMiddleware) RequireGlobal() func(http.Handler) http.Handler {
	return m.observe("global", m.AuthMiddleware.RequireGlobal())
}

func (m initialPasswordRouteMiddleware) RequireEnrolmentProof(maxAge time.Duration) func(http.Handler) http.Handler {
	return m.observe("enrollment", m.AuthMiddleware.RequireEnrolmentProof(maxAge))
}

type initialPasswordRouteUsers struct {
	iface.UserProvider
	user  *iface.User
	trace *[]string
}

func (u *initialPasswordRouteUsers) GetUserByID(_ context.Context, id string) (*iface.User, error) {
	*u.trace = append(*u.trace, "handler")
	if id != u.user.UUID {
		return nil, iface.ErrUserNotFound
	}
	return u.user, nil
}

func (u *initialPasswordRouteUsers) ClearFailedLogins(context.Context, string) error { return nil }

func (u *initialPasswordRouteUsers) SetPasswordHashIfUnset(_ context.Context, id, hash string) error {
	if id != u.user.UUID {
		return iface.ErrUserNotFound
	}
	if u.user.PasswordHash != "" {
		return iface.ErrPasswordAlreadySet
	}
	u.user.PasswordHash = hash
	return nil
}

func TestInitialPasswordRoute_OperatorGatesBeforeHandlerAndClientDoesNotMount(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	var trace []string
	users := &initialPasswordRouteUsers{
		user:  &iface.User{UUID: "operator-user", Email: "operator@example.com", IsActive: true, EmailVerified: true},
		trace: &trace,
	}
	passwords := services.NewPasswordService(logger, false)
	svc := services.NewPasswordAuthService(services.PasswordAuthConfig{
		UserService: users, InitialPasswordSetter: users, PasswordService: passwords,
		Policy:   services.NewAuthPolicyServiceForTest(map[string]string{"passwordLoginEnabledAdmin": "true"}),
		Audience: services.PolicyAudienceOperator, Logger: logger,
	})
	mw := authMiddleware.NewAuthMiddleware(nil, sharederrors.NewManager(logger, false))
	mw.SetMFAEnrollmentLookup(func(context.Context, string, string) (bool, error) { return false, nil })
	operator, client := chi.NewRouter(), chi.NewRouter()
	cfg := huma.DefaultConfig("route test", "1")
	m := &AuthModule{
		logger:                  logger,
		operatorPasswordHandler: handlers.NewPasswordAuthHandler(svc, "", "", false),
		operatorAuthHandler:     &handlers.AuthHandler{}, operatorMFAHandler: &handlers.MFAHandler{},
		operatorAdminUserAuthHandler: &handlers.AdminUserAuthHandler{}, operatorSelfUserAuthHandler: &handlers.SelfUserAuthHandler{},
		clientAuthHandler: &handlers.AuthHandler{}, clientPasswordHandler: &handlers.PasswordAuthHandler{},
		clientMFAHandler: &handlers.MFAHandler{}, deviceTrustHandler: &handlers.DeviceTrustHandler{},
		serviceTokenHandler: &handlers.ServiceTokenHandler{}, serviceAccountAdminHandler: &handlers.ServiceAccountAdminHandler{},
	}
	m.RegisterRoutes(&module.RouteInfo{
		Operator: &module.APISurface{PublicAPI: humachi.New(operator, cfg), ProtectedRouter: operator, AuthMW: initialPasswordRouteMiddleware{mw, &trace}},
		Client:   &module.APISurface{PublicAPI: humachi.New(client, cfg), ProtectedRouter: client, AuthMW: mw},
		Router:   operator, ClientRouter: client, APIConfig: cfg,
	})

	request := func(router http.Handler, path, user string, authTime int64) *httptest.ResponseRecorder {
		t.Helper()
		trace = nil
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"newPassword":"fresh-passphrase-123"}`))
		r.Header.Set("Content-Type", "application/json")
		ctx := context.WithValue(r.Context(), "claims", &models.JWTClaims{UserUUID: "operator-user", Audience: "operator", AMR: []string{"oauth"}, AuthTime: authTime})
		if user != "" {
			ctx = context.WithValue(ctx, "userUUID", user)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r.WithContext(ctx))
		return w
	}
	check := func(w *httptest.ResponseRecorder, status int, wantTrace []string) {
		t.Helper()
		if w.Code != status || !reflect.DeepEqual(trace, wantTrace) {
			t.Fatalf("status=%d trace=%v body=%s; want status=%d trace=%v", w.Code, trace, w.Body.String(), status, wantTrace)
		}
	}

	// Removing RequireGlobal would allow the enrollment gate to run first.
	w := request(operator, "/v1/auth/operator/me/password", "", time.Now().Unix())
	check(w, http.StatusUnauthorized, []string{"global"})
	// Removing enrollment proof would reach the credential mutation with stale proof.
	w = request(operator, "/v1/auth/operator/me/password", users.user.UUID, time.Now().Add(-6*time.Minute).Unix())
	check(w, http.StatusUnauthorized, []string{"global", "enrollment"})
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body["code"] != "reauthentication_required" || body["maxAgeSeconds"] != float64(300) {
		t.Fatalf("wrong enrollment refusal: %s (%v)", w.Body.String(), err)
	}
	if users.user.PasswordHash != "" {
		t.Fatal("refused requests changed the credential")
	}
	w = request(operator, "/v1/auth/operator/me/password", users.user.UUID, time.Now().Unix())
	check(w, http.StatusOK, []string{"global", "enrollment", "handler"})
	if ok, err := passwords.Verify("fresh-passphrase-123", users.user.PasswordHash); !ok || err != nil {
		t.Fatalf("initial password was not persisted: ok=%v err=%v", ok, err)
	}
	// Neither the client equivalent nor the operator path may leak onto the client host.
	for _, path := range []string{"/v1/auth/client/me/password", "/v1/auth/operator/me/password"} {
		w = request(client, path, users.user.UUID, time.Now().Unix())
		check(w, http.StatusNotFound, nil)
	}
}
