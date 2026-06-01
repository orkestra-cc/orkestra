package services

// Phase 12 Tier-1 integration tests for the security-load-bearing
// authz orchestration paths: HasPermission, CreateBinding,
// SeedSystemRoles, and RegisterPermissions. The Cedar/shadow-evaluate
// logic is already covered in service_test.go; these tests focus on
// the repo + cache + privilege-elevation glue around those primitives.

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/orkestra/backend/internal/core/authz/models"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

func testLogger(_ *testing.T) *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newTier1Service stands a Service up over an in-memory repo, no
// Redis (cache disabled), no Cedar engine (so the role-table verdict
// is authoritative). The Cedar shadow-mode tests live in
// service_test.go and stay green there.
func newTier1Service(t *testing.T, lookup UserSystemRoleLookup) (*Service, *fakeRepo) {
	t.Helper()
	repo := newFakeRepo()
	svc := &Service{
		repo:                repo,
		logger:              testLogger(t),
		userRoles:           lookup,
		systemPermissionSet: make(map[string]struct{}),
		allPermissionSet:    make(map[string]struct{}),
	}
	return svc, repo
}

func staticRoleLookup(role string) UserSystemRoleLookup {
	return func(_ context.Context, _ string) (string, error) { return role, nil }
}

// ===== HasPermission =====

func TestHasPermission_SuperAdminWildcardAllowsEverything(t *testing.T) {
	svc, _ := newTier1Service(t, staticRoleLookup("super_admin"))

	for _, perm := range []string{"system.users.admin", "tenant.delete", "anything.at.all"} {
		ok, err := svc.HasPermission(context.Background(), "u-1", "tenant-X", perm)
		if err != nil {
			t.Fatalf("HasPermission(%q): %v", perm, err)
		}
		if !ok {
			t.Errorf("super_admin must hold %q via wildcard", perm)
		}
	}
}

func TestHasPermission_AdministratorInheritsSystemPermissions(t *testing.T) {
	svc, _ := newTier1Service(t, staticRoleLookup("administrator"))
	// Seed the in-memory permission catalog so the administrator
	// shortcut has something to hand back.
	_ = svc.RegisterPermissions(context.Background(), []iface.PermissionSpec{
		{Key: "system.modules.admin", Module: "system", System: true},
		{Key: "tenant.update", Module: "tenant"}, // non-system
	})

	ok, err := svc.HasPermission(context.Background(), "u-2", "", "system.modules.admin")
	if err != nil || !ok {
		t.Errorf("administrator must inherit system.modules.admin, got ok=%v err=%v", ok, err)
	}
	// Non-system permission stays binding-driven — administrator
	// shortcut covers System=true keys only.
	ok2, _ := svc.HasPermission(context.Background(), "u-2", "tenant-X", "tenant.update")
	if ok2 {
		t.Errorf("administrator without binding must NOT hold non-system tenant.update")
	}
}

func TestHasPermission_UnprivilegedUser_RoleBindingMatch(t *testing.T) {
	svc, repo := newTier1Service(t, staticRoleLookup("operator"))
	// Seed a tenant role + binding granting one permission.
	repo.seedRole("role-A", "billing_reader", false, []string{"billing.invoice.read"}, "tenant-X")
	repo.seedBinding("bind-A", "u-3", "tenant-X", "role-A")

	ok, err := svc.HasPermission(context.Background(), "u-3", "tenant-X", "billing.invoice.read")
	if err != nil {
		t.Fatalf("HasPermission: %v", err)
	}
	if !ok {
		t.Errorf("user with binding granting billing.invoice.read must hold it")
	}
	// A different perm not granted by the role: deny.
	ok2, _ := svc.HasPermission(context.Background(), "u-3", "tenant-X", "billing.invoice.delete")
	if ok2 {
		t.Errorf("permission not in role bag must deny")
	}
}

func TestHasPermission_NoBinding_Denies(t *testing.T) {
	svc, _ := newTier1Service(t, staticRoleLookup("guest"))
	ok, err := svc.HasPermission(context.Background(), "u-4", "tenant-X", "billing.invoice.read")
	if err != nil {
		t.Fatalf("HasPermission: %v", err)
	}
	if ok {
		t.Errorf("user with no binding and unprivileged role must deny")
	}
}

func TestHasPermission_GlobalBindingAppliesAcrossTenants(t *testing.T) {
	svc, repo := newTier1Service(t, staticRoleLookup("operator"))
	// Global binding (tenantID="") — applies in every tenant context.
	repo.seedRole("role-G", "global_reader", false, []string{"docs.read"}, "")
	repo.seedBinding("bind-G", "u-5", "", "role-G")

	for _, tenant := range []string{"tenant-A", "tenant-B", ""} {
		ok, err := svc.HasPermission(context.Background(), "u-5", tenant, "docs.read")
		if err != nil {
			t.Fatalf("tenant=%q: %v", tenant, err)
		}
		if !ok {
			t.Errorf("global binding must apply for tenant=%q", tenant)
		}
	}
}

func TestHasPermission_InactiveRoleIsIgnored(t *testing.T) {
	svc, repo := newTier1Service(t, staticRoleLookup("operator"))
	repo.seedRole("role-I", "inactive_reader", false, []string{"billing.invoice.read"}, "tenant-X")
	repo.seedBinding("bind-I", "u-6", "tenant-X", "role-I")
	// Toggle the role inactive — its perms should drop out of the user's
	// effective set.
	role := repo.roleByName("inactive_reader")
	role.IsActive = false
	repo.roles[role.UUID] = *role

	ok, err := svc.HasPermission(context.Background(), "u-6", "tenant-X", "billing.invoice.read")
	if err != nil {
		t.Fatalf("HasPermission: %v", err)
	}
	if ok {
		t.Errorf("inactive role's perms must not apply")
	}
}

// ===== CreateBinding =====

func TestCreateBinding_HappyPath_TenantRoleInTenantScope(t *testing.T) {
	svc, repo := newTier1Service(t, staticRoleLookup("super_admin"))
	repo.seedRole("role-X", "billing_reader", false, []string{"billing.invoice.read"}, "tenant-A")

	b, err := svc.CreateBinding(context.Background(), "tenant-A", "granter-uuid", models.CreateBindingInput{
		UserUUID: "user-target",
		RoleUUID: "role-X",
	})
	if err != nil {
		t.Fatalf("CreateBinding: %v", err)
	}
	if b == nil || b.UUID == "" {
		t.Fatalf("expected populated binding, got %+v", b)
	}
	// Persisted in fake repo.
	if _, ok := repo.bindings[b.UUID]; !ok {
		t.Errorf("binding not persisted in fake repo")
	}
}

func TestCreateBinding_SystemRoleInTenantScope_Rejected(t *testing.T) {
	svc, repo := newTier1Service(t, staticRoleLookup("super_admin"))
	// Seed administrator system role (one of the 6 platform names).
	repo.seedRole("role-admin", "administrator", true, []string{"*"}, "")

	_, err := svc.CreateBinding(context.Background(), "tenant-A", "granter", models.CreateBindingInput{
		UserUUID: "user-target",
		RoleUUID: "role-admin",
	})
	if !errors.Is(err, ErrSystemRoleNotGrantableInTenant) {
		t.Fatalf("expected ErrSystemRoleNotGrantableInTenant, got %v", err)
	}
}

func TestCreateBinding_TenantRoleGlobally_Rejected(t *testing.T) {
	svc, repo := newTier1Service(t, staticRoleLookup("super_admin"))
	repo.seedRole("role-org", "org_owner", true, []string{"tenant.update"}, "")

	_, err := svc.CreateBinding(context.Background(), "" /* global */, "granter", models.CreateBindingInput{
		UserUUID: "user-target",
		RoleUUID: "role-org",
	})
	if !errors.Is(err, ErrTenantRoleNotGrantableGlobally) {
		t.Fatalf("expected ErrTenantRoleNotGrantableGlobally, got %v", err)
	}
}

func TestCreateBinding_Cascade_GranterMissingPerm_Rejected(t *testing.T) {
	// Granter is an "operator" with no useful perms. Target role asks
	// for billing.invoice.delete which the granter doesn't hold —
	// the cascade rule must refuse the grant.
	svc, repo := newTier1Service(t, staticRoleLookup("operator"))
	repo.seedRole("role-elevated", "billing_admin", false, []string{"billing.invoice.delete"}, "tenant-A")

	_, err := svc.CreateBinding(context.Background(), "tenant-A", "weak-granter", models.CreateBindingInput{
		UserUUID: "user-target",
		RoleUUID: "role-elevated",
	})
	if !errors.Is(err, ErrInsufficientPermissionsToGrant) {
		t.Fatalf("expected ErrInsufficientPermissionsToGrant, got %v", err)
	}
}

func TestCreateBinding_GranterRequired(t *testing.T) {
	svc, repo := newTier1Service(t, staticRoleLookup("super_admin"))
	repo.seedRole("role-X", "billing_reader", false, []string{"billing.invoice.read"}, "tenant-A")

	_, err := svc.CreateBinding(context.Background(), "tenant-A", "" /* missing granter */, models.CreateBindingInput{
		UserUUID: "user-target",
		RoleUUID: "role-X",
	})
	if !errors.Is(err, ErrGranterRequired) {
		t.Fatalf("expected ErrGranterRequired, got %v", err)
	}
}

func TestCreateBinding_SystemSentinelGranterBypassesCascade(t *testing.T) {
	// "system" is the platform-issued auto-grant sentinel — used by
	// the OwnerRoleBinder hook in tenant.CreateTenant. It must still
	// respect the separation rule (tier-discipline) but skip cascade.
	svc, repo := newTier1Service(t, staticRoleLookup("guest"))
	repo.seedRole("role-org-owner", "org_owner_v2", false, []string{"tenant.update"}, "tenant-A")

	_, err := svc.CreateBinding(context.Background(), "tenant-A", granterSystem, models.CreateBindingInput{
		UserUUID: "user-target",
		RoleUUID: "role-org-owner",
	})
	if err != nil {
		t.Fatalf("system sentinel must bypass cascade, got %v", err)
	}
}

func TestCreateBinding_PrivilegedRole_StartsMFAGrace(t *testing.T) {
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

	_, err := svc.CreateBinding(context.Background(), "tenant-A", "granter", models.CreateBindingInput{
		UserUUID: "user-target",
		RoleUUID: "role-admin-tenant",
	})
	if err != nil {
		t.Fatalf("CreateBinding: %v", err)
	}
	if graceCalls != 1 {
		t.Fatalf("expected exactly one MFA-grace call for org_admin grant, got %d", graceCalls)
	}
}

// ===== RegisterPermissions =====

func TestRegisterPermissions_Idempotent_PopulatesCatalog(t *testing.T) {
	svc, repo := newTier1Service(t, staticRoleLookup(""))
	specs := []iface.PermissionSpec{
		{Key: "billing.invoice.read", Module: "billing", System: false},
		{Key: "system.modules.admin", Module: "system", System: true},
	}
	if err := svc.RegisterPermissions(context.Background(), specs); err != nil {
		t.Fatalf("first register: %v", err)
	}
	if err := svc.RegisterPermissions(context.Background(), specs); err != nil {
		t.Fatalf("second register (re-seed must be idempotent): %v", err)
	}
	// allPermissionSet contains both keys; systemPermissionSet only the System=true one.
	svc.mu.RLock()
	defer svc.mu.RUnlock()
	if _, ok := svc.allPermissionSet["billing.invoice.read"]; !ok {
		t.Errorf("allPermissionSet missing billing.invoice.read")
	}
	if _, ok := svc.systemPermissionSet["system.modules.admin"]; !ok {
		t.Errorf("systemPermissionSet missing system.modules.admin")
	}
	if _, ok := svc.systemPermissionSet["billing.invoice.read"]; ok {
		t.Errorf("non-system perm leaked into systemPermissionSet")
	}
	// Persisted in repo too — the upsert is what /admin/permissions reads.
	persisted, _ := repo.ListPermissions(context.Background())
	if len(persisted) != 2 {
		t.Errorf("expected 2 persisted permissions, got %d", len(persisted))
	}
	// cachedPermSpecs should mirror the input for lazy reseed.
	if len(svc.cachedPermSpecs) != len(specs) {
		t.Errorf("cachedPermSpecs length = %d, want %d", len(svc.cachedPermSpecs), len(specs))
	}
}

func TestRegisterPermissions_EmptyInput_NoOp(t *testing.T) {
	svc, repo := newTier1Service(t, staticRoleLookup(""))
	if err := svc.RegisterPermissions(context.Background(), nil); err != nil {
		t.Fatalf("nil input must be a no-op, got %v", err)
	}
	if got, _ := repo.ListPermissions(context.Background()); len(got) != 0 {
		t.Errorf("no permissions should be persisted, got %d", len(got))
	}
}

// ===== SeedSystemRoles =====

func TestSeedSystemRoles_CreatesPlatformAndOrgRoles(t *testing.T) {
	svc, repo := newTier1Service(t, staticRoleLookup(""))
	// Catalog must exist before seeding so the role permission sets
	// can be derived from it.
	_ = svc.RegisterPermissions(context.Background(), []iface.PermissionSpec{
		{Key: "tenant.read", Module: "tenant"},
		{Key: "tenant.update", Module: "tenant"},
		{Key: "system.modules.admin", Module: "system", System: true},
	})
	if err := svc.SeedSystemRoles(context.Background()); err != nil {
		t.Fatalf("SeedSystemRoles: %v", err)
	}
	// Spot-check the 6 platform roles + a couple of tenant roles.
	mustHave := []string{
		"super_admin", "administrator", "developer", "manager", "operator", "guest",
		"org_owner", "org_admin", "org_member",
	}
	for _, name := range mustHave {
		if !repo.hasRoleNamed(name) {
			t.Errorf("seed missing role %q", name)
		}
	}
	// super_admin gets the wildcard.
	if r := repo.roleByName("super_admin"); r == nil || !permKeysContain(r.Permissions, "*") {
		t.Errorf("super_admin must hold wildcard, got %+v", r)
	}
}

func TestSeedSystemRoles_PreservesUUIDsAcrossReseed(t *testing.T) {
	svc, repo := newTier1Service(t, staticRoleLookup(""))
	_ = svc.RegisterPermissions(context.Background(), []iface.PermissionSpec{
		{Key: "tenant.read", Module: "tenant"},
	})
	if err := svc.SeedSystemRoles(context.Background()); err != nil {
		t.Fatalf("first seed: %v", err)
	}
	first := repo.roleByName("super_admin")
	if first == nil {
		t.Fatalf("super_admin not seeded")
	}
	firstUUID := first.UUID

	// Re-seed: the UUID must survive so existing bindings keep working.
	if err := svc.SeedSystemRoles(context.Background()); err != nil {
		t.Fatalf("second seed: %v", err)
	}
	second := repo.roleByName("super_admin")
	if second == nil {
		t.Fatalf("super_admin missing after re-seed")
	}
	if second.UUID != firstUUID {
		t.Errorf("super_admin UUID changed across re-seed: %q → %q", firstUUID, second.UUID)
	}
}

func TestSeedSystemRoles_DeveloperEnvGated_ProductionReadOnly(t *testing.T) {
	svc, repo := newTier1Service(t, staticRoleLookup(""))
	svc.production = true
	_ = svc.RegisterPermissions(context.Background(), []iface.PermissionSpec{
		{Key: "billing.invoice.read", Module: "billing"},
		{Key: "billing.invoice.create", Module: "billing"},
		{Key: "system.modules.admin", Module: "system", System: true},
	})
	if err := svc.SeedSystemRoles(context.Background()); err != nil {
		t.Fatalf("SeedSystemRoles: %v", err)
	}
	dev := repo.roleByName("developer")
	if dev == nil {
		t.Fatalf("developer role not seeded")
	}
	// In production developer should NOT carry the create perm.
	for _, p := range dev.Permissions {
		if p == "billing.invoice.create" {
			t.Errorf("production developer must not hold billing.invoice.create, got perms=%v", dev.Permissions)
		}
	}
}
