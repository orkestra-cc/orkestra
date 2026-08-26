package handlers

import (
	"context"
	"encoding/json"
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
	"github.com/orkestra/backend/internal/core/tenant/repository"
	"github.com/orkestra/backend/internal/core/tenant/services"
	sharedErrors "github.com/orkestra/backend/internal/shared/errors"
	"github.com/orkestra/backend/internal/shared/middleware"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// routeAuthzAllowSystemTenantsAdmin is a minimal iface.AuthzProvider stub
// that grants exactly the permission the tenant admin surface declares.
// Only HasPermission is exercised by RequireSystemPermission; the other two
// methods are never called on this path.
type routeAuthzAllowSystemTenantsAdmin struct{}

func (routeAuthzAllowSystemTenantsAdmin) HasPermission(_ context.Context, _, _, permission string) (bool, error) {
	return permission == "system.tenants.admin", nil
}
func (routeAuthzAllowSystemTenantsAdmin) GetEffectivePermissions(context.Context, string, string) ([]string, error) {
	return nil, nil
}
func (routeAuthzAllowSystemTenantsAdmin) RegisterPermissions(context.Context, []iface.PermissionSpec) error {
	return nil
}

// mfaMasterSwitchOnPolicy always reports the platform MFA master switch ON.
//
// This is the trap called out in the task brief: RequireMFA deliberately
// passes every request through, regardless of amr, when the step-up
// policy's MFAEnabled reports false — that escape hatch exists so a
// never-enrolled operator is never locked out of the very admin writes
// needed to configure the platform (see AuthMiddleware.RequireMFA's doc
// comment). A route-authorization test that omits this stub, or that wires
// one whose MFAEnabled returns false, would "pass" the destructive routes
// through unconditionally and prove nothing about the MFA gate.
type mfaMasterSwitchOnPolicy struct{}

func (mfaMasterSwitchOnPolicy) MFARequired(*iface.User, []authModels.OrgMembership) bool {
	return false
}
func (mfaMasterSwitchOnPolicy) MFAEnabled(context.Context) bool { return true }

// newAdminMFARouter wires the exact two-group split module.go applies to
// the platform-admin tenant surface: RegisterAdminRoutes behind
// RequireSystemPermission alone, RegisterAdminDestructiveRoutes behind
// RequireSystemPermission + RequireMFA. The underlying tenant service is a
// fake that trivially succeeds every call reachable through these tests —
// the point of this test is HTTP-layer gating, not business logic.
func newAdminMFARouter(t *testing.T) *chi.Mux {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	authMW := middleware.NewAuthMiddleware(nil, sharedErrors.NewManager(logger, true))
	authMW.SetAuthzProvider(routeAuthzAllowSystemTenantsAdmin{})
	authMW.SetStepUpPolicy(mfaMasterSwitchOnPolicy{})

	svc := &fakeTenantSvc{
		listAllTenantsFilteredFn: func(context.Context, repository.TenantListFilter) ([]services.TenantAdminView, error) {
			return nil, nil
		},
		deleteTenantFn:          func(context.Context, string) error { return nil },
		purgeTenantFn:           func(context.Context, string) error { return nil },
		transferDefaultTenantFn: func(context.Context, string, string) error { return nil },
	}
	h := New(svc, nil)

	router := chi.NewRouter()
	router.Group(func(r chi.Router) {
		r.Use(authMW.RequireSystemPermission("system.tenants.admin"))
		api := humachi.New(r, huma.DefaultConfig("test", "1.0.0"))
		h.RegisterAdminRoutes(api)
	})
	router.Group(func(r chi.Router) {
		r.Use(authMW.RequireSystemPermission("system.tenants.admin"))
		r.Use(authMW.RequireMFA())
		api := humachi.New(r, huma.DefaultConfig("test", "1.0.0"))
		h.RegisterAdminDestructiveRoutes(api)
	})
	return router
}

// TestAdminRoutes_MFABoundary is the route-authorization test: proves that
// transfer, archive/delete, and purge all demand a stepped-up token (401
// step_up_required without an MFA amr, pass with amr=[otp]) while the read
// route requires only the permission grant, MFA or not.
func TestAdminRoutes_MFABoundary(t *testing.T) {
	router := newAdminMFARouter(t)

	noMFA := []string{"pwd"}
	withMFA := []string{"pwd", "otp"}

	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		amr        []string
		wantStatus int
		wantCode   string
	}{
		{name: "read: list tenants, no MFA amr, passes (no MFA required)", method: http.MethodGet, path: "/v1/admin/tenants", amr: noMFA, wantStatus: http.StatusOK},
		{name: "read: list tenants, with MFA amr, still passes", method: http.MethodGet, path: "/v1/admin/tenants", amr: withMFA, wantStatus: http.StatusOK},
		{name: "destructive: delete tenant, no MFA amr, blocked", method: http.MethodDelete, path: "/v1/admin/tenants/t-1", amr: noMFA, wantStatus: http.StatusUnauthorized, wantCode: "step_up_required"},
		{name: "destructive: delete tenant, with MFA amr, passes", method: http.MethodDelete, path: "/v1/admin/tenants/t-1", amr: withMFA, wantStatus: http.StatusNoContent},
		{name: "destructive: purge tenant, no MFA amr, blocked", method: http.MethodPost, path: "/v1/admin/tenants/t-1/purge", amr: noMFA, wantStatus: http.StatusUnauthorized, wantCode: "step_up_required"},
		{name: "destructive: purge tenant, with MFA amr, passes", method: http.MethodPost, path: "/v1/admin/tenants/t-1/purge", amr: withMFA, wantStatus: http.StatusNoContent},
		{name: "destructive: transfer default, no MFA amr, blocked", method: http.MethodPut, path: "/v1/admin/tenants/default", body: `{"tenantId":"t-2"}`, amr: noMFA, wantStatus: http.StatusUnauthorized, wantCode: "step_up_required"},
		{name: "destructive: transfer default, with MFA amr, passes", method: http.MethodPut, path: "/v1/admin/tenants/default", body: `{"tenantId":"t-2"}`, amr: withMFA, wantStatus: http.StatusNoContent},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var bodyReader io.Reader
			if tt.body != "" {
				bodyReader = strings.NewReader(tt.body)
			}
			req := httptest.NewRequest(tt.method, tt.path, bodyReader)
			if tt.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			ctx := context.WithValue(req.Context(), ctxauth.KeyUserUUID, "admin-1")
			ctx = middleware.WithAMR(ctx, tt.amr)
			req = req.WithContext(ctx)

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if tt.wantCode != "" {
				var respBody map[string]any
				if err := json.Unmarshal(rec.Body.Bytes(), &respBody); err != nil {
					t.Fatalf("decode response: %v; body = %s", err, rec.Body.String())
				}
				if code, _ := respBody["code"].(string); code != tt.wantCode {
					t.Errorf("code = %q, want %q; body = %s", code, tt.wantCode, rec.Body.String())
				}
			}
		})
	}
}
