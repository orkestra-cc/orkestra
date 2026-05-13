package testkit_test

import (
	"context"
	"testing"

	"github.com/orkestra-cc/orkestra-sdk/ctxauth"
	"github.com/orkestra-cc/orkestra-sdk/tenantrepo"
	"github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/shared/middleware"
	"github.com/orkestra/backend/internal/testkit"
)

// TestContextKeysRoundTrip is the load-bearing test for this package: it
// asserts that a context populated by testkit is readable by the exact
// public accessors production code uses. If anyone renames a context key on
// either side, this test fails immediately instead of every tenant-scoped
// integration test silently becoming a no-op.
func TestContextKeysRoundTrip(t *testing.T) {
	id := testkit.NewIdentity("user-1", "alice@example.com", "administrator").
		WithTenant("tenant-a", []string{"org_admin"}, true).
		WithTenant("tenant-b", []string{"org_member"}, false)

	ctx := id.ContextFor(context.Background(), "tenant-b")

	if got, ok := ctxauth.GetUserUUID(ctx); !ok || got != "user-1" {
		t.Errorf("GetUserUUID: got (%q, %v), want (\"user-1\", true)", got, ok)
	}
	if got, ok := ctxauth.GetUserEmail(ctx); !ok || got != "alice@example.com" {
		t.Errorf("GetUserEmail: got (%q, %v)", got, ok)
	}
	if got, ok := ctxauth.GetSystemRole(ctx); !ok || got != "administrator" {
		t.Errorf("GetSystemRole: got (%q, %v)", got, ok)
	}
	if got, ok := ctxauth.GetTenantID(ctx); !ok || got != "tenant-b" {
		t.Errorf("GetTenantID: got (%q, %v), want (\"tenant-b\", true)", got, ok)
	}
	roles, ok := ctxauth.GetTenantRoles(ctx)
	if !ok || len(roles) != 1 || roles[0] != "org_member" {
		t.Errorf("GetTenantRoles: got (%v, %v), want ([org_member], true)", roles, ok)
	}
	mbrs, ok := middleware.GetMemberships(ctx)
	if !ok || len(mbrs) != 2 {
		t.Errorf("GetMemberships: got %d memberships, want 2", len(mbrs))
	}
}

// TestDefaultTenantFallback: when activeTenantID is empty, the default
// tenant takes over. Mirrors the JWT's `dtid` claim being used when the
// X-Tenant-ID header is absent.
func TestDefaultTenantFallback(t *testing.T) {
	id := testkit.NewIdentity("user-1", "a@b", "manager").
		WithTenant("tenant-a", []string{"org_owner"}, true)
	ctx := id.ContextFor(context.Background(), "")
	got, ok := ctxauth.GetTenantID(ctx)
	if !ok || got != "tenant-a" {
		t.Fatalf("expected default tenant-a, got (%q, %v)", got, ok)
	}
}

// TestGlobalSentinel: passing "-" leaves the context tenant-less, matching
// RequireGlobal routes.
func TestGlobalSentinel(t *testing.T) {
	id := testkit.NewIdentity("user-1", "a@b", "super_admin").
		WithTenant("tenant-a", []string{"org_owner"}, true)
	ctx := id.ContextFor(context.Background(), "-")
	if got, ok := ctxauth.GetTenantID(ctx); ok {
		t.Fatalf("expected no tenantID for global sentinel, got %q", got)
	}
}

// TestInvalidActiveTenantPanics: passing a tenant the identity isn't a
// member of panics — the real middleware returns 403 in this case.
func TestInvalidActiveTenantPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for cross-tenant activeTenantID, got none")
		}
	}()
	id := testkit.NewIdentity("user-1", "a@b", "manager").
		WithTenant("tenant-a", []string{"org_member"}, true)
	id.ContextFor(context.Background(), "tenant-evil")
}

// TestClaimsSnapshot: the Claims helper returns a populated *JWTClaims that
// matches the identity.
func TestClaimsSnapshot(t *testing.T) {
	id := testkit.NewIdentity("user-1", "e@x", "developer").
		WithTenant("tenant-1", []string{"org_admin"}, true)
	claims := id.Claims()
	if claims.UserUUID != "user-1" || claims.SystemRole != "developer" {
		t.Errorf("identity mismatch: %+v", claims)
	}
	if claims.DefaultTenantID != "tenant-1" {
		t.Errorf("expected default tenant-1, got %q", claims.DefaultTenantID)
	}
	if len(claims.Memberships) != 1 || claims.Memberships[0].TenantUUID != "tenant-1" {
		t.Errorf("expected 1 membership for tenant-1, got %+v", claims.Memberships)
	}
}

// TestTenantRepoInterop: confirm tenantrepo.Scope can see the injected
// tenantID — the primary consumer of this testkit.
func TestTenantRepoInterop(t *testing.T) {
	id := testkit.NewIdentity("user-1", "a@b", "manager").
		WithTenant("tenant-7", []string{"org_member"}, true)
	ctx := id.ContextFor(context.Background(), "")

	got, ok := tenantrepo.CurrentTenantID(ctx)
	if !ok || got != "tenant-7" {
		t.Fatalf("tenantrepo.CurrentTenantID: got (%q, %v), want (\"tenant-7\", true)", got, ok)
	}
}

// Compile-time assurance the TenantMembership type we use matches the one
// the auth middleware actually reads — if the two diverge (e.g. auth adds
// a required field), the compiler catches it here.
var _ []models.TenantMembership = (&models.JWTClaims{}).Memberships
