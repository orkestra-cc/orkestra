package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
)

// A PIN session never reaches a destructive, administrative or system
// permission through the role table, whatever the user's grants say: the
// Cedar forbids that state the same rule are shadow-only by default.
func TestPermissionGates_RefusePINSessionsOnDestructiveKeys(t *testing.T) {
	run := func(gate func(*AuthMiddleware) func(http.Handler) http.Handler, amr []string) (bool, int) {
		authz := &fakeAuthz{allow: true}
		m := newTestMiddleware(authz, &fakeTenantProvider{}, nil)
		ctx := context.WithValue(context.Background(), ctxauth.KeyUserUUID, "u-1")
		ctx = context.WithValue(ctx, ctxauth.KeyTenantID, "t-1")
		ctx = context.WithValue(ctx, ctxClaims, &authModels.JWTClaims{AMR: amr})
		req := httptest.NewRequest(http.MethodDelete, "/v1/anything", nil).WithContext(ctx)
		rec := httptest.NewRecorder()
		called := false
		gate(m)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })).ServeHTTP(rec, req)
		return called, rec.Code
	}
	pin := []string{authModels.AMRPIN}
	for _, perm := range []string{"tenant.delete", "auth.service_accounts.manage", "tenant.settings.admin", "system.users.read"} {
		for name, gate := range map[string]func(*AuthMiddleware) func(http.Handler) http.Handler{
			"tenant": func(m *AuthMiddleware) func(http.Handler) http.Handler { return m.RequirePermission(perm) },
			"global": func(m *AuthMiddleware) func(http.Handler) http.Handler { return m.RequireSystemPermission(perm) },
		} {
			if called, code := run(gate, pin); called || code != http.StatusForbidden {
				t.Errorf("%s gate, PIN session, %s: called=%v status=%d, want 403", name, perm, called, code)
			}
			if called, _ := run(gate, []string{"pwd"}); !called {
				t.Errorf("%s gate, password session, %s: refused", name, perm)
			}
		}
	}
	for _, perm := range []string{"user.read", "auth.pin_login", "tenant.member.self"} {
		gate := func(m *AuthMiddleware) func(http.Handler) http.Handler { return m.RequirePermission(perm) }
		if called, _ := run(gate, pin); !called {
			t.Errorf("PIN session refused %s, which is neither destructive nor system", perm)
		}
	}
}
