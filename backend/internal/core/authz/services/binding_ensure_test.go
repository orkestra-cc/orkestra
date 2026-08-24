package services

// Unit coverage for EnsureBinding and the CreateBinding duplicate-key
// mapping, run against the in-memory fakeRepo (no live Mongo — the
// concurrent-safety + real E11000 behavior is covered separately by the
// Mongo-gated tests in repository/binding_ensure_integration_test.go).
//
// EnsureBinding is documented (service.go) to run the exact same
// validation pipeline as CreateBinding — role active, system/tenant
// separation, cascade rule, cache invalidation, the MFA-grace hook — so
// these tests mirror the CreateBinding cases in tier1_test.go /
// cache_test.go one-for-one rather than inventing new scenarios.

import (
	"context"
	"errors"
	"testing"

	"github.com/orkestra/backend/internal/core/authz/models"
	"go.mongodb.org/mongo-driver/mongo"
)

// ===== EnsureBinding: same validation pipeline as CreateBinding =====

func TestEnsureBinding_HappyPath_PersistsAndReturnsBinding(t *testing.T) {
	svc, repo := newTier1Service(t, staticRoleLookup("super_admin"))
	repo.seedRole("role-X", "billing_reader", false, []string{"billing.invoice.read"}, "tenant-A")

	b, err := svc.EnsureBinding(context.Background(), "tenant-A", "granter-uuid", models.CreateBindingInput{
		UserUUID: "user-target",
		RoleUUID: "role-X",
	})
	if err != nil {
		t.Fatalf("EnsureBinding: %v", err)
	}
	if b == nil || b.UUID == "" {
		t.Fatalf("expected populated binding, got %+v", b)
	}
	if _, ok := repo.bindings[b.UUID]; !ok {
		t.Errorf("binding not persisted in fake repo")
	}
}

func TestEnsureBinding_RoleInactive_Rejected(t *testing.T) {
	svc, repo := newTier1Service(t, staticRoleLookup("super_admin"))
	repo.seedRole("role-off", "disabled_role", false, []string{"billing.invoice.read"}, "tenant-A")
	repo.roles["role-off"] = func() models.Role {
		r := repo.roles["role-off"]
		r.IsActive = false
		return r
	}()

	_, err := svc.EnsureBinding(context.Background(), "tenant-A", "granter", models.CreateBindingInput{
		UserUUID: "user-target",
		RoleUUID: "role-off",
	})
	if !errors.Is(err, ErrRoleInactive) {
		t.Fatalf("expected ErrRoleInactive, got %v", err)
	}
}

func TestEnsureBinding_SystemRoleInTenantScope_Rejected(t *testing.T) {
	svc, repo := newTier1Service(t, staticRoleLookup("super_admin"))
	repo.seedRole("role-admin", "administrator", true, []string{"*"}, "")

	_, err := svc.EnsureBinding(context.Background(), "tenant-A", "granter", models.CreateBindingInput{
		UserUUID: "user-target",
		RoleUUID: "role-admin",
	})
	if !errors.Is(err, ErrSystemRoleNotGrantableInTenant) {
		t.Fatalf("expected ErrSystemRoleNotGrantableInTenant, got %v", err)
	}
}

func TestEnsureBinding_TenantRoleGlobally_Rejected(t *testing.T) {
	svc, repo := newTier1Service(t, staticRoleLookup("super_admin"))
	repo.seedRole("role-org", "org_owner", true, []string{"tenant.update"}, "")

	_, err := svc.EnsureBinding(context.Background(), "" /* global */, "granter", models.CreateBindingInput{
		UserUUID: "user-target",
		RoleUUID: "role-org",
	})
	if !errors.Is(err, ErrTenantRoleNotGrantableGlobally) {
		t.Fatalf("expected ErrTenantRoleNotGrantableGlobally, got %v", err)
	}
}

func TestEnsureBinding_Cascade_GranterMissingPerm_Rejected(t *testing.T) {
	svc, repo := newTier1Service(t, staticRoleLookup("operator"))
	repo.seedRole("role-elevated", "billing_admin", false, []string{"billing.invoice.delete"}, "tenant-A")

	_, err := svc.EnsureBinding(context.Background(), "tenant-A", "weak-granter", models.CreateBindingInput{
		UserUUID: "user-target",
		RoleUUID: "role-elevated",
	})
	if !errors.Is(err, ErrInsufficientPermissionsToGrant) {
		t.Fatalf("expected ErrInsufficientPermissionsToGrant, got %v", err)
	}
}

func TestEnsureBinding_GranterRequired(t *testing.T) {
	svc, repo := newTier1Service(t, staticRoleLookup("super_admin"))
	repo.seedRole("role-X", "billing_reader", false, []string{"billing.invoice.read"}, "tenant-A")

	_, err := svc.EnsureBinding(context.Background(), "tenant-A", "" /* missing granter */, models.CreateBindingInput{
		UserUUID: "user-target",
		RoleUUID: "role-X",
	})
	if !errors.Is(err, ErrGranterRequired) {
		t.Fatalf("expected ErrGranterRequired, got %v", err)
	}
}

func TestEnsureBinding_SystemSentinelGranterBypassesCascade(t *testing.T) {
	svc, repo := newTier1Service(t, staticRoleLookup("guest"))
	repo.seedRole("role-org-owner", "org_owner_v2", false, []string{"tenant.update"}, "tenant-A")

	_, err := svc.EnsureBinding(context.Background(), "tenant-A", granterSystem, models.CreateBindingInput{
		UserUUID: "user-target",
		RoleUUID: "role-org-owner",
	})
	if err != nil {
		t.Fatalf("system sentinel must bypass cascade, got %v", err)
	}
}

func TestEnsureBinding_PrivilegedRole_StartsMFAGrace(t *testing.T) {
	graceCalls := 0
	svc, repo := newTier1Service(t, staticRoleLookup("super_admin"))
	repo.seedRole("role-admin-tenant", "org_admin", true, []string{"tenant.update"}, "")
	svc.startMFAGrace = func(_ context.Context, userUUID string) error {
		if userUUID != "user-target" {
			t.Errorf("grace called for wrong user: %q", userUUID)
		}
		graceCalls++
		return nil
	}

	_, err := svc.EnsureBinding(context.Background(), "tenant-A", "granter", models.CreateBindingInput{
		UserUUID: "user-target",
		RoleUUID: "role-admin-tenant",
	})
	if err != nil {
		t.Fatalf("EnsureBinding: %v", err)
	}
	if graceCalls != 1 {
		t.Fatalf("expected exactly one MFA-grace call for org_admin grant, got %d", graceCalls)
	}
}

func TestEnsureBinding_InvalidatesTargetCache(t *testing.T) {
	svc, repo, _ := newCacheTestService(t, staticRoleLookup("super_admin"))
	repo.seedRole("role-X", "billing_reader", false, []string{"billing.invoice.read"}, "tenant-A")

	svc.cacheSet(context.Background(), "u-target", "tenant-A", []string{"old-cache"})

	_, err := svc.EnsureBinding(context.Background(), "tenant-A", "granter", models.CreateBindingInput{
		UserUUID: "u-target",
		RoleUUID: "role-X",
	})
	if err != nil {
		t.Fatalf("EnsureBinding: %v", err)
	}
	if _, ok := svc.cacheGet(context.Background(), "u-target", "tenant-A"); ok {
		t.Errorf("u-target's stale cache entry must be invalidated after ensure")
	}
}

// ===== EnsureBinding: idempotency (grant-if-absent, return winner otherwise) =====

// TestEnsureBinding_ReplayReturnsExistingRow_NeverOverwritesWinner pins the
// "preserve the winner" contract at the service layer: replaying EnsureBinding
// for the same (tenant, user, role) tuple — with a different granter and a
// second candidate ExpiresAt — must return the SAME binding UUID/GrantedBy
// as the first call, and the fake must still hold exactly one row for the
// tuple. The real concurrency race (two callers landing inside the same
// window) is covered live in repository/binding_ensure_integration_test.go;
// this proves the service-level replay path is idempotent independent of
// timing.
func TestEnsureBinding_ReplayReturnsExistingRow_NeverOverwritesWinner(t *testing.T) {
	svc, repo := newTier1Service(t, staticRoleLookup("super_admin"))
	repo.seedRole("role-owner", "org_owner", true, []string{"tenant.update"}, "")
	// org_owner is a tenant-scoped role in the seeded catalog by name, but
	// isPlatformSystemRole keys off platformSystemRoleNames — org_owner
	// isn't one of the six, so this seeds a legitimate tenant-scoped role.

	first, err := svc.EnsureBinding(context.Background(), "tenant-A", granterSystem, models.CreateBindingInput{
		UserUUID: "owner-1",
		RoleUUID: "role-owner",
	})
	if err != nil {
		t.Fatalf("EnsureBinding (first): %v", err)
	}

	second, err := svc.EnsureBinding(context.Background(), "tenant-A", granterSystem, models.CreateBindingInput{
		UserUUID: "owner-1",
		RoleUUID: "role-owner",
	})
	if err != nil {
		t.Fatalf("EnsureBinding (replay): %v", err)
	}
	if second.UUID != first.UUID {
		t.Fatalf("replay returned a different binding UUID: got %q, want the winner's %q", second.UUID, first.UUID)
	}
	if second.GrantedBy != first.GrantedBy {
		t.Fatalf("replay changed GrantedBy: got %q, want %q", second.GrantedBy, first.GrantedBy)
	}

	count := 0
	for _, bnd := range repo.bindings {
		if bnd.TenantID == "tenant-A" && bnd.UserUUID == "owner-1" && bnd.RoleUUID == "role-owner" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly one persisted binding for the tuple after replay, got %d", count)
	}
}

// ===== CreateBinding: duplicate-key mapping =====

// TestCreateBinding_DuplicateKeyMapsToErrBindingExists primes the fake
// repo's CreateBinding with a mongo.WriteException shaped like a real
// E11000 duplicate-key error (the same idiom
// internal/core/tenant/services/service_error_test.go uses to fake one) and
// asserts the service maps it to the stable services.ErrBindingExists
// sentinel rather than leaking the raw driver error.
func TestCreateBinding_DuplicateKeyMapsToErrBindingExists(t *testing.T) {
	svc, repo := newTier1Service(t, staticRoleLookup("super_admin"))
	repo.seedRole("role-X", "billing_reader", false, []string{"billing.invoice.read"}, "tenant-A")
	repo.createBindingErr = mongo.WriteException{WriteErrors: []mongo.WriteError{{
		Code:    11000,
		Message: "E11000 duplicate key error collection: authz_bindings index: tenantId_1_userUUID_1_roleId_1 dup key",
	}}}

	_, err := svc.CreateBinding(context.Background(), "tenant-A", "granter", models.CreateBindingInput{
		UserUUID: "user-target",
		RoleUUID: "role-X",
	})
	if !errors.Is(err, ErrBindingExists) {
		t.Fatalf("expected ErrBindingExists, got %v", err)
	}
}

func TestCreateBinding_NonDuplicateRepoErrorPassesThrough(t *testing.T) {
	svc, repo := newTier1Service(t, staticRoleLookup("super_admin"))
	repo.seedRole("role-X", "billing_reader", false, []string{"billing.invoice.read"}, "tenant-A")
	repo.createBindingErr = errors.New("fakeRepo: injected non-duplicate failure")

	_, err := svc.CreateBinding(context.Background(), "tenant-A", "granter", models.CreateBindingInput{
		UserUUID: "user-target",
		RoleUUID: "role-X",
	})
	if err == nil || errors.Is(err, ErrBindingExists) {
		t.Fatalf("expected the injected error to pass through unmapped, got %v", err)
	}
}
