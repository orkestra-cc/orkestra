package handlers

import (
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
)

// TestRouteMountsRegisterDistinctPaths locks in the ADR-0003 PR-D
// invariant that operator and client mounts coexist on a single
// huma.API: paths and operation IDs must both distinguish them.
// Registering twice with the same mount, or two mounts that share a
// prefix, would panic inside huma. We touch every Register* method so
// each handler's mount-aware path/opID rewrite is exercised.
//
// Handlers are constructed with nil services because huma never invokes
// the bound function values during registration; only the operation
// metadata is read. A real request would NPE — that's fine, this test
// asserts wiring only.
func TestRouteMountsRegisterDistinctPaths(t *testing.T) {
	t.Parallel()

	router := chi.NewRouter()
	api := humachi.New(router, huma.DefaultConfig("test", "1.0.0"))

	pwd := &PasswordAuthHandler{}
	mfa := &MFAHandler{}
	wa := &WebAuthnHandler{}
	dt := &DeviceTrustHandler{}
	auth := &AuthHandler{}

	for _, mount := range []RouteMount{OperatorMount, ClientMount} {
		pwd.RegisterPublicRoutes(api, mount)
		pwd.RegisterProtectedRoutes(api, mount)
		mfa.RegisterPublicRoutes(api, mount)
		mfa.RegisterProtectedRoutes(api, mount)
		mfa.RegisterStepUpRoutes(api, mount)
		wa.RegisterPublicRoutes(api, mount)
		wa.RegisterProtectedRoutes(api, mount)
		wa.RegisterStepUpRoutes(api, mount)
		dt.RegisterRoutes(api, mount)
		auth.RegisterTierMountableRoutes(api, api, router, mount)
		auth.RegisterOAuthLinkRoute(api, mount)
	}

	spec := api.OpenAPI()
	if spec == nil || spec.Paths == nil {
		t.Fatal("OpenAPI spec or paths missing after registration")
	}

	// Spot-check the tier-split login paths land at the expected
	// prefixes — if the mount fields ever drift, this fails loudly
	// before module.go boots an unreachable surface.
	want := []string{
		"/v1/auth/operator/login",
		"/v1/auth/client/login",
		"/v1/auth/operator/me",
		"/v1/auth/client/me",
		"/v1/auth/operator/me/mfa",
		"/v1/auth/client/me/mfa/webauthn/credentials",
		"/v1/auth/operator/me/devices/trust",
		"/v1/auth/operator/me/mfa/backup-codes/regenerate",
		"/v1/auth/client/me/mfa/backup-codes/regenerate",
		"/v1/auth/operator/me/oauth/link/{provider}",
		"/v1/auth/client/me/oauth/link/{provider}",
	}
	for _, p := range want {
		if _, ok := spec.Paths[p]; !ok {
			t.Errorf("expected path %q missing from spec", p)
		}
	}

	// PATCH /me is the self-service preference surface (Phase 1 of the
	// i18n plan). Both verbs share the same path item, so verify the
	// operation lands as a sibling of GET.
	for _, p := range []string{"/v1/auth/operator/me", "/v1/auth/client/me"} {
		item, ok := spec.Paths[p]
		if !ok || item.Patch == nil {
			t.Errorf("expected PATCH on %q, got nothing", p)
		}
	}

	// /v1/admin/users/{userId}/mfa/reset is operator-only by design;
	// the admin-reset surface deliberately does not take a RouteMount
	// and must be present exactly once.
	mfa2 := &MFAHandler{}
	mfa2.RegisterAdminRoutes(api)
	if _, ok := spec.Paths["/v1/admin/users/{userId}/mfa/reset"]; !ok {
		t.Error("admin mfa reset path missing from spec")
	}
}

// TestSelfUserAuthHandlerMountsAllRoutes locks in that every
// Register* method on SelfUserAuthHandler lands its expected path on
// the spec. The handler is built with nil services because huma
// reads only metadata at registration time.
func TestSelfUserAuthHandlerMountsAllRoutes(t *testing.T) {
	t.Parallel()

	router := chi.NewRouter()
	api := humachi.New(router, huma.DefaultConfig("test", "1.0.0"))

	h := &SelfUserAuthHandler{}
	for _, mount := range []RouteMount{OperatorMount, ClientMount} {
		h.RegisterReadRoutes(api, mount)
		h.RegisterStepUpRoutes(api, mount)
	}

	spec := api.OpenAPI()
	if spec == nil || spec.Paths == nil {
		t.Fatal("OpenAPI spec missing after registration")
	}

	want := map[string]string{
		"/v1/auth/operator/me/auth-methods":         "GET",
		"/v1/auth/client/me/auth-methods":           "GET",
		"/v1/auth/operator/me/sessions":             "GET",
		"/v1/auth/client/me/sessions":               "GET",
		"/v1/auth/operator/me/oauth/{provider}":     "DELETE",
		"/v1/auth/client/me/oauth/{provider}":       "DELETE",
		"/v1/auth/operator/me/sessions/{sessionId}": "DELETE",
		"/v1/auth/client/me/sessions/{sessionId}":   "DELETE",
	}
	for path, method := range want {
		item, ok := spec.Paths[path]
		if !ok {
			t.Errorf("expected path %q missing from spec", path)
			continue
		}
		var op *huma.Operation
		switch method {
		case "GET":
			op = item.Get
		case "POST":
			op = item.Post
		case "DELETE":
			op = item.Delete
		}
		if op == nil {
			t.Errorf("expected %s on %q, got nothing", method, path)
		}
	}

	// DELETE /me/sessions (no path param) lives on the same item as
	// GET — verify both verbs landed.
	if item, ok := spec.Paths["/v1/auth/operator/me/sessions"]; !ok || item.Delete == nil {
		t.Error("expected DELETE /v1/auth/operator/me/sessions on spec")
	}
}

// TestAdminUserAuthHandlerMountsAllRoutes locks in that every Register*
// method on AdminUserAuthHandler lands its expected path on the spec.
// The handler is built with nil services because huma reads only
// metadata at registration time. If a future refactor splits a route
// or renames a path, this test fails before the mount drift reaches
// production.
func TestAdminUserAuthHandlerMountsAllRoutes(t *testing.T) {
	t.Parallel()

	router := chi.NewRouter()
	api := humachi.New(router, huma.DefaultConfig("test", "1.0.0"))

	h := &AdminUserAuthHandler{}
	h.RegisterReadAuthMethodsRoute(api)
	h.RegisterPasswordResetRoute(api)
	h.RegisterResendVerificationRoute(api)
	h.RegisterOAuthUnlinkRoute(api)

	spec := api.OpenAPI()
	if spec == nil || spec.Paths == nil {
		t.Fatal("OpenAPI spec missing after registration")
	}

	want := map[string]string{
		"/v1/admin/users/{userId}/auth-methods":        "GET",
		"/v1/admin/users/{userId}/send-password-reset": "POST",
		"/v1/admin/users/{userId}/resend-verification": "POST",
		"/v1/admin/users/{userId}/oauth/{provider}":    "DELETE",
	}
	for path, method := range want {
		item, ok := spec.Paths[path]
		if !ok {
			t.Errorf("expected path %q missing from spec", path)
			continue
		}
		var op *huma.Operation
		switch method {
		case "GET":
			op = item.Get
		case "POST":
			op = item.Post
		case "DELETE":
			op = item.Delete
		}
		if op == nil {
			t.Errorf("expected %s on %q, got nothing", method, path)
		}
	}
}
