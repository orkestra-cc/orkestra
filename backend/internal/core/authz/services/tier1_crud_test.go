package services

// Phase 15: extends Tier-1 coverage with the CRUD methods that round
// out the role/binding lifecycle. All tests reuse the fakeRepo from
// repo_fake_test.go — no new infrastructure.

import (
	"context"
	"errors"
	"testing"

	"github.com/orkestra/backend/internal/core/authz/models"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// ===== CreateRole =====

func TestCreateRole_PersistsCustomRole(t *testing.T) {
	svc, repo := newTier1Service(t, staticRoleLookup(""))
	role, err := svc.CreateRole(context.Background(), "tenant-A", models.CreateRoleInput{
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

// ===== UpdateRole =====

func TestUpdateRole_SystemRoleNameImmutable(t *testing.T) {
	svc, repo := newTier1Service(t, staticRoleLookup(""))
	repo.seedRole("role-sys", "administrator", true, []string{"*"}, "")

	rename := "renamed"
	_, err := svc.UpdateRole(context.Background(), "role-sys", models.UpdateRoleInput{
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
	updated, err := svc.UpdateRole(context.Background(), "role-sys", models.UpdateRoleInput{
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
	repo.seedRole("role-c", "billing_reader", false, []string{"billing.invoice.read"}, "tenant-A")

	updated, err := svc.UpdateRole(context.Background(), "role-c", models.UpdateRoleInput{
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

	_, err := svc.UpdateRole(context.Background(), "role-c", models.UpdateRoleInput{
		Permissions: []string{},
	})
	if err == nil {
		t.Fatalf("empty permissions array must be rejected")
	}
}

func TestUpdateRole_EmptyNameRejected(t *testing.T) {
	svc, repo := newTier1Service(t, staticRoleLookup(""))
	repo.seedRole("role-c", "billing_reader", false, []string{"billing.invoice.read"}, "tenant-A")

	blank := "   "
	_, err := svc.UpdateRole(context.Background(), "role-c", models.UpdateRoleInput{
		Name: &blank,
	})
	if err == nil {
		t.Fatalf("whitespace-only name must be rejected")
	}
}

func TestUpdateRole_NoFieldsIsNoOp(t *testing.T) {
	svc, repo := newTier1Service(t, staticRoleLookup(""))
	repo.seedRole("role-c", "billing_reader", false, []string{"billing.invoice.read"}, "tenant-A")

	got, err := svc.UpdateRole(context.Background(), "role-c", models.UpdateRoleInput{})
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

	if err := svc.DeleteRole(context.Background(), "role-c"); err != nil {
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

	err := svc.DeleteRole(context.Background(), "role-sys")
	if !errors.Is(err, ErrSystemRoleImmutable) {
		t.Fatalf("expected ErrSystemRoleImmutable, got %v", err)
	}
	if _, ok := repo.bindings["bind-keep"]; !ok {
		t.Errorf("binding for system role must NOT be cascaded when delete refused")
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

func TestDeleteBinding_RemovesRowAndFlushesCache(t *testing.T) {
	svc, repo := newTier1Service(t, staticRoleLookup(""))
	repo.seedBinding("b-1", "u-1", "tenant-A", "role-X")

	if err := svc.DeleteBinding(context.Background(), "b-1"); err != nil {
		t.Fatalf("DeleteBinding: %v", err)
	}
	if _, ok := repo.bindings["b-1"]; ok {
		t.Errorf("binding b-1 must be removed from the fake")
	}
	// flushCache is a no-op when redis is nil — the test verifies the
	// happy path doesn't panic and the repo write happens.
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
	// Smoke test: when permissions on a role change, the service
	// flushes the effective-permission cache. With Redis nil the
	// flushCache call short-circuits — this test just confirms the
	// happy path doesn't panic and the role update lands.
	svc, repo := newTier1Service(t, staticRoleLookup(""))
	repo.seedRole("role-c", "billing_reader", false, []string{"billing.invoice.read"}, "tenant-A")
	updated, err := svc.UpdateRole(context.Background(), "role-c", models.UpdateRoleInput{
		Permissions: []string{"billing.invoice.read", "billing.invoice.refund"},
	})
	if err != nil {
		t.Fatalf("UpdateRole: %v", err)
	}
	if len(updated.Permissions) != 2 {
		t.Errorf("post-update permissions = %v, want 2 entries", updated.Permissions)
	}
}
