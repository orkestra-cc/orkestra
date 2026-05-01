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
	}
	for _, p := range want {
		if _, ok := spec.Paths[p]; !ok {
			t.Errorf("expected path %q missing from spec", p)
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
