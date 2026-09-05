package services

// Phase 15: extends Tier-1 coverage with the CRUD methods that round
// out the role/binding lifecycle. All tests reuse the fakeRepo from
// repo_fake_test.go — no new infrastructure.

import (
	"context"
	"errors"
	"testing"

	"github.com/orkestra/backend/internal/core/authz/models"
	"github.com/orkestra/backend/internal/core/authz/repository"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// ===== CreateRole =====

func TestCreateRole_PersistsCustomRole(t *testing.T) {
	svc, repo := newTier1Service(t, staticRoleLookup(""))
	// The keys this role carries must exist in the registered catalog:
	// the D21 validator refuses a key nothing ever declared, and the
	// granterSystem sentinel bypasses the cascade only, never the catalog.
	registerTestPermissions(t, svc, registered("billing.invoice.read"))
	role, err := svc.CreateRole(context.Background(), "tenant-A", granterSystem, models.CreateRoleInput{
		Name:        "billing_reader",
		Description: "read invoices",
		Permissions: []string{"billing.invoice.read"},
	})
	if err != nil {
		t.Fatalf("CreateRole: %v", err)
	}
	if role.UUID == "" {
		t.Fatalf("expected role UUID populated")
	}
	if role.IsSystem {
		t.Errorf("custom role must have IsSystem=false")
	}
	if !role.IsActive {
		t.Errorf("freshly created role must be active by default")
	}
	if role.TenantID != "tenant-A" {
		t.Errorf("TenantID = %q, want tenant-A", role.TenantID)
	}
	// Persisted under that UUID in the fake.
	if _, ok := repo.roles[role.UUID]; !ok {
		t.Errorf("role not persisted in fake repo")
	}
}

// CreateRole refuses a name already taken in the tenant, and leaves the
// incumbent exactly as it was.
//
// It used to write through UpsertRole, which is keyed on
// (tenantId, name): a create naming an existing role REWROTE it — the
// incumbent's uuid and permissions were replaced in place, so every
// binding pointing at the old uuid dangled permanently and its holders
// lost that access with no error and no cache invalidation. A caller
// holding only authz.role.create could therefore neuter or rewrite any
// role in the tenant by name, a power the catalog reserves to
// authz.role.update / authz.role.delete.
func TestCreateRole_DuplicateNameIsRefusedAndLeavesTheIncumbentIntact(t *testing.T) {
	svc, repo := newTier1Service(t, staticRoleLookup("super_admin"))
	registerTestPermissions(t, svc, registered("tenant.read", "tenant.update"))
	ctx := context.Background()

	first, err := svc.CreateRole(ctx, "tenant-1", "sa-1", models.CreateRoleInput{
		Name:        "editors",
		Permissions: []string{"tenant.read", "tenant.update"},
	})
	if err != nil {
		t.Fatalf("CreateRole: %v", err)
	}

	_, err = svc.CreateRole(ctx, "tenant-1", "sa-1", models.CreateRoleInput{
		Name:        "editors",
		Permissions: []string{"tenant.read"},
	})
	if !errors.Is(err, repository.ErrRoleExists) {
		t.Fatalf("err = %v, want repository.ErrRoleExists", err)
	}
	incumbent, ok := repo.roles[first.UUID]
	if !ok {
		t.Fatal("the incumbent's uuid no longer resolves — every binding on it now dangles")
	}
	if len(incumbent.Permissions) != 2 {
		t.Errorf("permissions = %v, want the incumbent's own list untouched", incumbent.Permissions)
	}

	// The name is reserved within its tenant only.
	if _, err := svc.CreateRole(ctx, "tenant-2", "sa-1", models.CreateRoleInput{
		Name:        "editors",
		Permissions: []string{"tenant.read"},
	}); err != nil {
		t.Fatalf("another tenant must still be able to use the name: %v", err)
	}
}

// ===== UpdateRole =====

func TestUpdateRole_SystemRoleNameImmutable(t *testing.T) {
	svc, repo := newTier1Service(t, staticRoleLookup(""))
	repo.seedRole("role-sys", "administrator", true, []string{"*"}, "")

	rename := "renamed"
	_, err := svc.UpdateRole(context.Background(), "tenant-A", "role-sys", granterSystem, models.UpdateRoleInput{
		Name: &rename,
	})
	if !errors.Is(err, ErrSystemRoleImmutable) {
		t.Fatalf("got %v, want ErrSystemRoleImmutable", err)
	}
	// Ensure the stored name didn't change.
	stored := repo.roleByName("administrator")
	if stored == nil {
		t.Errorf("administrator role disappeared after rejected update")
	}
}

func TestUpdateRole_SystemRoleIsActiveToggleAllowed(t *testing.T) {
	svc, repo := newTier1Service(t, staticRoleLookup(""))
	repo.seedRole("role-sys", "administrator", true, []string{"*"}, "")

	off := false
	updated, err := svc.UpdateRole(context.Background(), "tenant-A", "role-sys", granterSystem, models.UpdateRoleInput{
		IsActive: &off,
	})
	if err != nil {
		t.Fatalf("isActive toggle on system role must be allowed: %v", err)
	}
	if updated.IsActive {
		t.Errorf("expected IsActive=false after toggle, got %+v", updated)
	}
}

func TestUpdateRole_CustomRolePermissionsUpdate(t *testing.T) {
	svc, repo := newTier1Service(t, staticRoleLookup(""))
	// See TestCreateRole_PersistsCustomRole: the written keys have to be
	// in the catalog for the D21 validator to accept them.
	registerTestPermissions(t, svc, registered("billing.invoice.read", "billing.invoice.create"))
	repo.seedRole("role-c", "billing_reader", false, []string{"billing.invoice.read"}, "tenant-A")

	updated, err := svc.UpdateRole(context.Background(), "tenant-A", "role-c", granterSystem, models.UpdateRoleInput{
		Permissions: []string{"billing.invoice.read", "billing.invoice.create"},
	})
	if err != nil {
		t.Fatalf("UpdateRole: %v", err)
	}
	if len(updated.Permissions) != 2 {
		t.Errorf("permissions = %v, want 2 entries", updated.Permissions)
	}
}

func TestUpdateRole_EmptyPermissionsRejected(t *testing.T) {
	svc, repo := newTier1Service(t, staticRoleLookup(""))
	repo.seedRole("role-c", "billing_reader", false, []string{"billing.invoice.read"}, "tenant-A")

	_, err := svc.UpdateRole(context.Background(), "tenant-A", "role-c", granterSystem, models.UpdateRoleInput{
		Permissions: []string{},
	})
	// Named, not just non-nil: this used to be a bare errors.New, which the
	// handler could only answer as a 500 (D21).
	if !errors.Is(err, ErrRolePermissionsRequired) {
		t.Fatalf("err = %v, want ErrRolePermissionsRequired", err)
	}
}

func TestUpdateRole_EmptyNameRejected(t *testing.T) {
	svc, repo := newTier1Service(t, staticRoleLookup(""))
	repo.seedRole("role-c", "billing_reader", false, []string{"billing.invoice.read"}, "tenant-A")

	blank := "   "
	_, err := svc.UpdateRole(context.Background(), "tenant-A", "role-c", granterSystem, models.UpdateRoleInput{
		Name: &blank,
	})
	// Named, not just non-nil: the other bare errors.New that surfaced as a
	// 500 before D21.
	if !errors.Is(err, ErrRoleNameRequired) {
		t.Fatalf("err = %v, want ErrRoleNameRequired", err)
	}
}

func TestUpdateRole_NoFieldsIsNoOp(t *testing.T) {
	svc, repo := newTier1Service(t, staticRoleLookup(""))
	repo.seedRole("role-c", "billing_reader", false, []string{"billing.invoice.read"}, "tenant-A")

	got, err := svc.UpdateRole(context.Background(), "tenant-A", "role-c", granterSystem, models.UpdateRoleInput{})
	if err != nil {
		t.Fatalf("empty input must be a no-op, got %v", err)
	}
	if got == nil || got.UUID != "role-c" {
		t.Errorf("expected the existing role echoed back, got %+v", got)
	}
}

// ===== GetRoleByName / ListRoles =====

func TestGetRoleByName_GlobalAndTenantScoped(t *testing.T) {
	svc, repo := newTier1Service(t, staticRoleLookup(""))
	repo.seedRole("role-sys", "administrator", true, []string{"*"}, "")
	repo.seedRole("role-c", "billing_reader", false, []string{"billing.invoice.read"}, "tenant-A")

	// System role: tenantID="" lookup.
	got, err := svc.GetRoleByName(context.Background(), "", "administrator")
	if err != nil || got == nil || got.UUID != "role-sys" {
		t.Errorf("system role lookup failed: got=%+v err=%v", got, err)
	}
	// Tenant-scoped role.
	got2, err := svc.GetRoleByName(context.Background(), "tenant-A", "billing_reader")
	if err != nil || got2 == nil || got2.UUID != "role-c" {
		t.Errorf("tenant role lookup failed: got=%+v err=%v", got2, err)
	}
}

func TestListRoles_ReturnsSystemPlusTenantScoped(t *testing.T) {
	svc, repo := newTier1Service(t, staticRoleLookup(""))
	repo.seedRole("role-sys", "administrator", true, []string{"*"}, "")
	repo.seedRole("role-c-A", "billing_reader", false, []string{"billing.invoice.read"}, "tenant-A")
	repo.seedRole("role-c-B", "billing_writer", false, []string{"billing.invoice.create"}, "tenant-B")

	roles, err := svc.ListRoles(context.Background(), "tenant-A")
	if err != nil {
		t.Fatalf("ListRoles: %v", err)
	}
	// Should see administrator (system, global) + billing_reader (tenant-A scoped),
	// but NOT billing_writer (different tenant).
	names := map[string]bool{}
	for _, r := range roles {
		names[r.Name] = true
	}
	if !names["administrator"] {
		t.Errorf("listing must include the global system role")
	}
	if !names["billing_reader"] {
		t.Errorf("listing must include the tenant's own role")
	}
	if names["billing_writer"] {
		t.Errorf("listing must NOT include another tenant's role")
	}
}

// ===== ensureSeeded =====

func TestEnsureSeeded_SkipsWhenSystemRolesPresent(t *testing.T) {
	svc, repo := newTier1Service(t, staticRoleLookup(""))
	// Pre-seed an existing system role so the count check short-circuits.
	repo.seedRole("seeded", "administrator", true, []string{"*"}, "")
	// Cache some specs to prove they aren't reused.
	_ = svc.RegisterPermissions(context.Background(), []iface.PermissionSpec{
		{Key: "tenant.read", Module: "tenant"},
	})
	beforeCount := len(repo.roles)
	if err := svc.ensureSeeded(context.Background()); err != nil {
		t.Fatalf("ensureSeeded: %v", err)
	}
	if len(repo.roles) != beforeCount {
		t.Errorf("ensureSeeded must short-circuit when system roles exist; role count went %d → %d",
			beforeCount, len(repo.roles))
	}
}

func TestEnsureSeeded_LazyRebuildAfterDBWipe(t *testing.T) {
	// The lazy-heal contract: if the catalog/role collection is empty
	// at query time AND cachedPermSpecs is populated (RegisterPermissions
	// was called at boot), the service rebuilds both from the in-memory
	// spec cache. Models the dev "drop the DB and refresh" workflow.
	svc, repo := newTier1Service(t, staticRoleLookup(""))
	_ = svc.RegisterPermissions(context.Background(), []iface.PermissionSpec{
		{Key: "tenant.read", Module: "tenant"},
		{Key: "system.modules.admin", Module: "system", System: true},
	})
	// Wipe the seeded permissions to model a DB drop after RegisterPermissions
	// returned. cachedPermSpecs survives in memory.
	for k := range repo.permissions {
		delete(repo.permissions, k)
	}

	if err := svc.ensureSeeded(context.Background()); err != nil {
		t.Fatalf("ensureSeeded: %v", err)
	}
	if !repo.hasRoleNamed("super_admin") {
		t.Errorf("super_admin must be re-seeded after wipe + ensureSeeded")
	}
	persisted, _ := repo.ListPermissions(context.Background())
	if len(persisted) == 0 {
		t.Errorf("permission catalog must be re-populated after ensureSeeded")
	}
}

func TestEnsureSeeded_NoOpWhenSpecsCacheEmpty(t *testing.T) {
	// First-boot race: ensureSeeded fires before RegisterPermissions
	// has populated cachedPermSpecs. The service must NOT panic and
	// must NOT seed an empty catalog (which would later mismatch the
	// real one). It returns nil and lets the startup seed path catch
	// up.
	svc, repo := newTier1Service(t, staticRoleLookup(""))
	if err := svc.ensureSeeded(context.Background()); err != nil {
		t.Fatalf("ensureSeeded with empty cache must be a no-op, got %v", err)
	}
	if len(repo.roles) != 0 || len(repo.permissions) != 0 {
		t.Errorf("ensureSeeded must NOT seed when cache is empty")
	}
}

// ===== DeleteRole =====

func TestDeleteRole_CustomRoleCascadesBindings(t *testing.T) {
	svc, repo := newTier1Service(t, staticRoleLookup(""))
	repo.seedRole("role-c", "billing_reader", false, []string{"billing.invoice.read"}, "tenant-A")
	repo.seedBinding("bind-1", "u-1", "tenant-A", "role-c")
	repo.seedBinding("bind-2", "u-2", "tenant-A", "role-c")
	// Unrelated binding pointing at a different role — must survive.
	repo.seedRole("role-other", "other", false, []string{"other.perm"}, "tenant-A")
	repo.seedBinding("bind-3", "u-3", "tenant-A", "role-other")

	if err := svc.DeleteRole(context.Background(), "tenant-A", "role-c"); err != nil {
		t.Fatalf("DeleteRole: %v", err)
	}
	if _, ok := repo.roles["role-c"]; ok {
		t.Errorf("role row must be gone")
	}
	if _, ok := repo.bindings["bind-1"]; ok {
		t.Errorf("cascading binding bind-1 must be removed")
	}
	if _, ok := repo.bindings["bind-2"]; ok {
		t.Errorf("cascading binding bind-2 must be removed")
	}
	if _, ok := repo.bindings["bind-3"]; !ok {
		t.Errorf("unrelated binding bind-3 must survive")
	}
}

func TestDeleteRole_SystemRoleRefused(t *testing.T) {
	svc, repo := newTier1Service(t, staticRoleLookup(""))
	repo.seedRole("role-sys", "administrator", true, []string{"*"}, "")
	repo.seedBinding("bind-keep", "u-1", "", "role-sys")

	err := svc.DeleteRole(context.Background(), "tenant-A", "role-sys")
	if !errors.Is(err, ErrSystemRoleImmutable) {
		t.Fatalf("expected ErrSystemRoleImmutable, got %v", err)
	}
	if _, ok := repo.bindings["bind-keep"]; !ok {
		t.Errorf("binding for system role must NOT be cascaded when delete refused")
	}
}

// ===== Cross-tenant guards (audit C-2) =====
//
// A per-tenant route resolves the {roleId}/{bindingId} by UUID. Without the
// service-side orgId check a member of tenant B could pass tenant A's role or
// binding UUID and mutate it. These assert the guard rejects the cross-tenant
// case with ErrNotFound and leaves the row intact.

func TestUpdateRole_CrossTenantRefused(t *testing.T) {
	svc, repo := newTier1Service(t, staticRoleLookup(""))
	repo.seedRole("role-c", "billing_reader", false, []string{"billing.invoice.read"}, "tenant-A")

	newName := "hijacked"
	_, err := svc.UpdateRole(context.Background(), "tenant-B", "role-c", granterSystem, models.UpdateRoleInput{Name: &newName})
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-tenant update, got %v", err)
	}
	if got := repo.roles["role-c"]; got.Name != "billing_reader" {
		t.Errorf("role name must be unchanged, got %q", got.Name)
	}
}

func TestDeleteRole_CrossTenantRefused(t *testing.T) {
	svc, repo := newTier1Service(t, staticRoleLookup(""))
	repo.seedRole("role-c", "billing_reader", false, []string{"billing.invoice.read"}, "tenant-A")
	repo.seedBinding("bind-1", "u-1", "tenant-A", "role-c")

	err := svc.DeleteRole(context.Background(), "tenant-B", "role-c")
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-tenant delete, got %v", err)
	}
	if _, ok := repo.roles["role-c"]; !ok {
		t.Errorf("role must survive a cross-tenant delete attempt")
	}
	if _, ok := repo.bindings["bind-1"]; !ok {
		t.Errorf("bindings must NOT be cascaded on a refused cross-tenant delete")
	}
}

func TestDeleteBinding_CrossTenantRefused(t *testing.T) {
	svc, repo := newTier1Service(t, staticRoleLookup(""))
	repo.seedBinding("b-1", "u-1", "tenant-A", "role-c")

	err := svc.DeleteBinding(context.Background(), "tenant-B", "b-1")
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-tenant binding delete, got %v", err)
	}
	if _, ok := repo.bindings["b-1"]; !ok {
		t.Errorf("binding must survive a cross-tenant delete attempt")
	}
}

// ===== ListPermissions / ListBindings =====

func TestListPermissions_PassesThroughRepo(t *testing.T) {
	svc, _ := newTier1Service(t, staticRoleLookup(""))
	_ = svc.RegisterPermissions(context.Background(), []iface.PermissionSpec{
		{Key: "tenant.read", Module: "tenant"},
		{Key: "billing.invoice.read", Module: "billing"},
	})
	got, err := svc.ListPermissions(context.Background())
	if err != nil {
		t.Fatalf("ListPermissions: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("got %d permissions, want 2", len(got))
	}
}

func TestListBindings_FilteredByTenant(t *testing.T) {
	svc, repo := newTier1Service(t, staticRoleLookup(""))
	repo.seedRole("role", "x", false, []string{"x.read"}, "tenant-A")
	repo.seedBinding("b-A", "u-1", "tenant-A", "role")
	repo.seedBinding("b-B", "u-2", "tenant-B", "role")

	got, err := svc.ListBindings(context.Background(), "tenant-A")
	if err != nil {
		t.Fatalf("ListBindings: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d bindings, want 1", len(got))
	}
	if got[0].UUID != "b-A" {
		t.Errorf("got binding %q, want b-A", got[0].UUID)
	}
}

// ===== DeleteBinding =====

func TestDeleteBinding_RemovesRow(t *testing.T) {
	svc, repo := newTier1Service(t, staticRoleLookup(""))
	repo.seedBinding("b-1", "u-1", "tenant-A", "role-X")

	if err := svc.DeleteBinding(context.Background(), "tenant-A", "b-1"); err != nil {
		t.Fatalf("DeleteBinding: %v", err)
	}
	if _, ok := repo.bindings["b-1"]; ok {
		t.Errorf("binding b-1 must be removed from the fake")
	}
	// Repo-level contract only: this harness leaves Redis nil, so the
	// invalidation is a no-op here by construction. The cache half is
	// asserted for real by TestDeleteBinding_FlushesEveryAuthzCache in
	// cache_test.go — this name no longer promises it.
}

// ===== RemoveBindingsByTenant =====

func TestRemoveBindingsByTenant_DropsOnlyMatchingRows(t *testing.T) {
	svc, repo := newTier1Service(t, staticRoleLookup(""))
	repo.seedBinding("b-A1", "u-1", "tenant-A", "role")
	repo.seedBinding("b-A2", "u-2", "tenant-A", "role")
	repo.seedBinding("b-B1", "u-3", "tenant-B", "role")
	repo.seedBinding("b-G", "u-4", "" /* global */, "role-sys")

	n, err := svc.RemoveBindingsByTenant(context.Background(), "tenant-A")
	if err != nil {
		t.Fatalf("RemoveBindingsByTenant: %v", err)
	}
	if n != 2 {
		t.Errorf("removed %d bindings, want 2", n)
	}
	for _, k := range []string{"b-A1", "b-A2"} {
		if _, ok := repo.bindings[k]; ok {
			t.Errorf("binding %q must be removed", k)
		}
	}
	for _, k := range []string{"b-B1", "b-G"} {
		if _, ok := repo.bindings[k]; !ok {
			t.Errorf("binding %q from a different scope must survive", k)
		}
	}
}

func TestRemoveBindingsByTenant_NoMatches_ReturnsZero(t *testing.T) {
	svc, repo := newTier1Service(t, staticRoleLookup(""))
	repo.seedBinding("b-other", "u-1", "tenant-X", "role")

	n, err := svc.RemoveBindingsByTenant(context.Background(), "tenant-NONEXISTENT")
	if err != nil {
		t.Fatalf("RemoveBindingsByTenant: %v", err)
	}
	if n != 0 {
		t.Errorf("got %d removed, want 0 for unknown tenant", n)
	}
	if _, ok := repo.bindings["b-other"]; !ok {
		t.Errorf("unrelated binding must survive")
	}
}

// ===== UpdateRole flushes the perm cache =====

func TestUpdateRole_FlushesPermissionCache(t *testing.T) {
	// A permission change on a role must retire every cached verdict:
	// the role is reachable through any binding, so the affected user
	// set is not enumerable and the whole cache goes.
	//
	// This used to run on newTier1Service, which leaves Redis nil — the
	// invalidation short-circuited and the body asserted only that the
	// role was written, so the name promised a flush the test never
	// looked for. It now runs against miniredis and reads the cache.
	svc, repo, _ := newCacheTestService(t, staticRoleLookup(""))
	ctx := context.Background()
	// See TestCreateRole_PersistsCustomRole: the written keys have to be
	// in the catalog for the D21 validator to accept them.
	registerTestPermissions(t, svc, registered("billing.invoice.read", "billing.invoice.refund"))
	repo.seedRole("role-c", "billing_reader", false, []string{"billing.invoice.read"}, "tenant-A")
	svc.cacheSet(ctx, "u-1", "tenant-A", []string{"billing.invoice.read"})
	if _, ok := svc.cacheGet(ctx, "u-1", "tenant-A"); !ok {
		t.Fatal("precondition: the cached verdict must be readable before the update")
	}

	updated, err := svc.UpdateRole(ctx, "tenant-A", "role-c", granterSystem, models.UpdateRoleInput{
		Permissions: []string{"billing.invoice.read", "billing.invoice.refund"},
	})
	if err != nil {
		t.Fatalf("UpdateRole: %v", err)
	}
	if len(updated.Permissions) != 2 {
		t.Errorf("post-update permissions = %v, want 2 entries", updated.Permissions)
	}
	if _, ok := svc.cacheGet(ctx, "u-1", "tenant-A"); ok {
		t.Error("the cached verdict must be retired by the role update")
	}
}

// ===== D21: custom-role permission validation (H-4) =====

// TestH4Probe_CannotEscalateACustomRoleAfterBinding is the audit's H-4
// probe, inverted. The original ran create → bind → update-with-a-key-the
// -actor-lacks and succeeded, because UpdateRole wrote `permissions`
// verbatim: bindings had a cascade check, roles did not, so the role
// editor was the way around it. It must now fail at the update.
func TestH4Probe_CannotEscalateACustomRoleAfterBinding(t *testing.T) {
	svc, repo := newTier1Service(t, staticRoleLookup(""))
	registerTestPermissions(t, svc, registered("tenant.read", "tenant.delete"))
	grantActorPermissions(t, repo, "actor-1", "tenant-1", "tenant.read")
	ctx := context.Background()

	role, err := svc.CreateRole(ctx, "tenant-1", "actor-1", models.CreateRoleInput{
		Name:        "harmless",
		Permissions: []string{"tenant.read"},
	})
	if err != nil {
		t.Fatalf("CreateRole: %v", err)
	}
	if _, err := svc.CreateBinding(ctx, "tenant-1", "actor-1", models.CreateBindingInput{
		UserUUID: "actor-1",
		RoleUUID: role.UUID,
	}); err != nil {
		t.Fatalf("CreateBinding: %v", err)
	}

	_, err = svc.UpdateRole(ctx, "tenant-1", role.UUID, "actor-1", models.UpdateRoleInput{
		Permissions: []string{"tenant.read", "tenant.delete"},
	})
	if !errors.Is(err, ErrInsufficientPermissionsToGrant) {
		t.Fatalf("the probe must fail at the update, got %v", err)
	}
	if got := repo.roles[role.UUID]; len(got.Permissions) != 1 {
		t.Errorf("the refused update must not have been persisted, got %v", got.Permissions)
	}
}

// TestUpdateRole_IsActiveOnlyPatchSkipsValidation is edge case 13: the
// validator runs only when a permission list is actually supplied, so a
// role that already carries a key no module declares any more can still
// be disabled. Without this, a stale key would make the role
// un-deactivatable — the opposite of what an operator needs.
func TestUpdateRole_IsActiveOnlyPatchSkipsValidation(t *testing.T) {
	svc, repo := newTier1Service(t, staticRoleLookup(""))
	// Deliberately NOT registered: the residue of a removed module.
	repo.seedRole("role-stale", "legacy", false, []string{"tenant.obliterate"}, "tenant-1")

	active := false
	updated, err := svc.UpdateRole(context.Background(), "tenant-1", "role-stale", "actor-1",
		models.UpdateRoleInput{IsActive: &active})
	if err != nil {
		t.Fatalf("disabling a role with a stale key must still work: %v", err)
	}
	if updated.IsActive {
		t.Error("the role must be disabled")
	}
}

// CreateRole gained the same empty-name guard UpdateRole has. The schema's
// min=1 stops "" at the edge but not "   ".
func TestCreateRole_EmptyNameIsARoleNameError(t *testing.T) {
	svc, _ := newTier1Service(t, staticRoleLookup(""))
	registerTestPermissions(t, svc, registered("tenant.read"))

	_, err := svc.CreateRole(context.Background(), "tenant-1", granterSystem, models.CreateRoleInput{
		Name:        "   ",
		Permissions: []string{"tenant.read"},
	})
	if !errors.Is(err, ErrRoleNameRequired) {
		t.Fatalf("err = %v, want ErrRoleNameRequired", err)
	}
}

// ===== D21 must NOT close the role editor to platform administrators =====
//
// Ruling P3'. A pre-flight finding claimed the D21 cascade would lock a
// platform administrator out of the custom-role editor. It does not: the
// editor is reached through RequirePermission("authz.role.create"), which
// resolves through the SAME GetEffectivePermissions the cascade consults,
// and the seeded `administrator` role carries every registered key. The
// three tests below exist to keep that door open. If one of them goes
// red, the fix is NOT to weaken the validator — it means the H-4 fix and
// the administrator's reach genuinely conflict and someone has to rule.

// seedAdministratorShapedActor gives actor the permission set a platform
// administrator actually holds on a live install: the `srole` shortcut
// PLUS a GLOBAL binding (tenantId "") to the seeded `administrator` role,
// whose permission list is allKeys — every registered key, ordinary and
// System:true alike. The global binding is the half that carries the
// ordinary keys; the shortcut alone carries only the System ones.
func seedAdministratorShapedActor(t *testing.T, repo *fakeRepo, actor string, allKeys ...string) {
	t.Helper()
	repo.seedRole("role-administrator", "administrator", true, allKeys, "")
	repo.seedBinding("bind-administrator", actor, "", "role-administrator")
}

func newAdministratorFixture(t *testing.T) (*Service, *fakeRepo) {
	t.Helper()
	svc, repo := newTier1Service(t, staticRoleLookup("administrator"))
	registerTestPermissions(t, svc,
		registered("tenant.read", "tenant.update", "authz.role.create", "authz.role.update"),
		systemRegistered("system.users.admin"))
	seedAdministratorShapedActor(t, repo, "admin-1",
		"tenant.read", "tenant.update", "authz.role.create", "authz.role.update", "system.users.admin")
	return svc, repo
}

// The architect's requirement, pinned: an administrator-shaped actor can
// still author AND edit a custom role carrying ordinary tenant
// permissions after D21.
func TestCreateAndUpdateRole_AdministratorKeepsTheRoleEditorOpen(t *testing.T) {
	svc, _ := newAdministratorFixture(t)
	ctx := context.Background()

	role, err := svc.CreateRole(ctx, "tenant-1", "admin-1", models.CreateRoleInput{
		Name:        "support",
		Permissions: []string{"tenant.read", "tenant.update"},
	})
	if err != nil {
		t.Fatalf("an administrator must still be able to author a custom role: %v", err)
	}
	if len(role.Permissions) != 2 {
		t.Errorf("permissions = %v, want both keys", role.Permissions)
	}

	updated, err := svc.UpdateRole(ctx, "tenant-1", role.UUID, "admin-1", models.UpdateRoleInput{
		Permissions: []string{"tenant.read"},
	})
	if err != nil {
		t.Fatalf("an administrator must still be able to edit a custom role: %v", err)
	}
	if len(updated.Permissions) != 1 {
		t.Errorf("permissions = %v, want the narrowed list", updated.Permissions)
	}
}

// …and the door stays shut where it must: check 3 binds everyone. The
// actor genuinely HOLDS system.users.admin here (both through the srole
// shortcut and through the global binding), so this refusal is the
// platform-key rule, not the cascade.
func TestCreateRole_AdministratorStillCannotPutAPlatformKeyInATenantRole(t *testing.T) {
	svc, _ := newAdministratorFixture(t)
	ctx := context.Background()

	if _, err := svc.CreateRole(ctx, "tenant-1", "admin-1", models.CreateRoleInput{
		Name:        "escalator",
		Permissions: []string{"tenant.read", "system.users.admin"},
	}); !errors.Is(err, ErrSystemPermissionInCustomRole) {
		t.Fatalf("err = %v, want ErrSystemPermissionInCustomRole", err)
	}

	if _, err := svc.CreateRole(ctx, "tenant-1", "admin-1", models.CreateRoleInput{
		Name:        "wildcard",
		Permissions: []string{"*"},
	}); !errors.Is(err, ErrSystemPermissionInCustomRole) {
		t.Fatalf("err = %v, want ErrSystemPermissionInCustomRole for the wildcard", err)
	}
}

// A super_admin is unaffected: the wildcard covers the cascade, so every
// ordinary key is authorable — and the platform-key rule still binds them
// too, because it is about what a TENANT role may carry, not about what
// the actor may grant.
func TestCreateRole_SuperAdminWildcardIsUnaffected(t *testing.T) {
	svc, _ := newTier1Service(t, staticRoleLookup("super_admin"))
	registerTestPermissions(t, svc,
		registered("tenant.read", "tenant.update"),
		systemRegistered("system.users.admin"))
	ctx := context.Background()

	role, err := svc.CreateRole(ctx, "tenant-1", "sa-1", models.CreateRoleInput{
		Name:        "anything",
		Permissions: []string{"tenant.read", "tenant.update"},
	})
	if err != nil {
		t.Fatalf("a super_admin must be able to author any ordinary custom role: %v", err)
	}
	if _, err := svc.UpdateRole(ctx, "tenant-1", role.UUID, "sa-1", models.UpdateRoleInput{
		Permissions: []string{"tenant.read"},
	}); err != nil {
		t.Fatalf("a super_admin must be able to edit it: %v", err)
	}

	if _, err := svc.CreateRole(ctx, "tenant-1", "sa-1", models.CreateRoleInput{
		Name:        "escalator",
		Permissions: []string{"system.users.admin"},
	}); !errors.Is(err, ErrSystemPermissionInCustomRole) {
		t.Fatalf("err = %v, want ErrSystemPermissionInCustomRole even for a super_admin", err)
	}
}
