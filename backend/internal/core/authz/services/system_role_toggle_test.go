package services

// The platform-administrator gate on the one field a system role still
// exposes to an edit: IsActive.
//
// Every seeded catalog row is IsSystem=true with tenantId="" — the six
// platform roles AND the five org_* rows. Disabling one is the largest
// revocation the role editor can make: GetEffectivePermissions skips
// every binding whose role is inactive, so the patch drops every
// permission that role carries from every holder ON THE PLATFORM.
//
// Two existing guards deliberately do not cover it. The tenant-scope
// guard is skipped for a system row (it belongs to no tenant), and the
// immutability guard covers only name/description/permissions. So the
// {tenantId} in the path was decorative for this one field: any caller
// who could reach PATCH /v1/tenants/{their-own}/authz/roles/{id} with
// authz.role.update could disable `administrator` platform-wide. Both
// org_owner (22 perms) and org_admin (18) carry authz.role.update.
//
// A cascade check ("the actor must hold everything the role carries")
// cannot close it either — TestUpdateRole_SystemRoleDisableRefusedThoughCascadeWouldPass
// pins why. The gate is about who the caller IS, so it reads the actor's
// system role from the database through s.userRoles, never from the
// `srole` claim (D28: that claim can be a whole access-token lifetime
// stale, which is the window the role-change propagation exists to close).

import (
	"context"
	"errors"
	"testing"

	"github.com/orkestra/backend/internal/core/authz/models"
)

// orgOwnerShapedPermissions stands in for the tenant-level set an
// org_owner actually holds — every key here is non-system, so a
// tenant-scoped binding conveys all of them (D22 strips only platform
// keys and the wildcard). authz.role.update is in it because that is
// what gets the caller through the route's RequirePermission middleware
// in production.
var orgOwnerShapedPermissions = []string{
	"authz.role.read",
	"authz.role.create",
	"authz.role.update",
	"authz.role.delete",
	"authz.binding.create",
	"authz.binding.delete",
	"tenant.read",
	"tenant.update",
}

// An org_owner's SYSTEM role is an ordinary low platform role — their
// power comes from a tenant-scoped org_owner binding, not from the user
// row. Reading the system role is exactly what separates them from a
// platform administrator.
func TestUpdateRole_SystemRoleDisableRefusedForOrgOwner(t *testing.T) {
	svc, repo := newTier1Service(t, staticRoleLookup("operator"))
	repo.seedRole("role-admin", "administrator", true, []string{"*"}, "")
	// Scenery, and the point: even holding authz.role.update inside their
	// own tenant, the caller may not touch a global catalog row.
	grantActorPermissions(t, repo, "org-owner-1", "tenant-A", orgOwnerShapedPermissions...)

	off := false
	_, err := svc.UpdateRole(context.Background(), "tenant-A", "role-admin", "org-owner-1",
		models.UpdateRoleInput{IsActive: &off})
	if !errors.Is(err, ErrPlatformAdminRequired) {
		t.Fatalf("err = %v, want ErrPlatformAdminRequired", err)
	}
	stored := repo.roleByName("administrator")
	if stored == nil || !stored.IsActive {
		t.Fatalf("administrator must still be active after the refusal, got %+v", stored)
	}
}

// Why a cascade check is not a substitute: an org_owner holds EXACTLY
// the permissions the org_owner catalog row carries, so "the actor must
// hold everything the role carries" passes — and would have let an
// org_owner disable org_owner for every tenant on the platform.
func TestUpdateRole_SystemRoleDisableRefusedThoughCascadeWouldPass(t *testing.T) {
	svc, repo := newTier1Service(t, staticRoleLookup("operator"))
	repo.seedRole("role-org-owner", "org_owner", true, orgOwnerShapedPermissions, "")
	grantActorPermissions(t, repo, "org-owner-1", "tenant-A", orgOwnerShapedPermissions...)

	// Precondition, asserted rather than assumed: the cascade PASSES.
	held, err := svc.GetEffectivePermissions(context.Background(), "org-owner-1", "tenant-A")
	if err != nil {
		t.Fatalf("GetEffectivePermissions: %v", err)
	}
	if err := validateBindingCascade(&models.Role{Permissions: orgOwnerShapedPermissions}, held); err != nil {
		t.Fatalf("precondition: the cascade must pass here — that is the whole point: %v", err)
	}

	off := false
	_, err = svc.UpdateRole(context.Background(), "tenant-A", "role-org-owner", "org-owner-1",
		models.UpdateRoleInput{IsActive: &off})
	if !errors.Is(err, ErrPlatformAdminRequired) {
		t.Fatalf("err = %v, want ErrPlatformAdminRequired", err)
	}
	stored := repo.roleByName("org_owner")
	if stored == nil || !stored.IsActive {
		t.Fatalf("org_owner must still be active after the refusal, got %+v", stored)
	}
}

// The capability the console exposes and super_admin legitimately has is
// preserved: RolesTable.onToggleActive sends exactly this patch.
func TestUpdateRole_SystemRoleToggleAllowedForPlatformAdministrators(t *testing.T) {
	for _, systemRole := range []string{"super_admin", "administrator"} {
		t.Run(systemRole, func(t *testing.T) {
			svc, repo := newTier1Service(t, staticRoleLookup(systemRole))
			repo.seedRole("role-admin", "administrator", true, []string{"*"}, "")

			off := false
			updated, err := svc.UpdateRole(context.Background(), "tenant-A", "role-admin", "actor-1",
				models.UpdateRoleInput{IsActive: &off})
			if err != nil {
				t.Fatalf("%s must be able to disable a system role: %v", systemRole, err)
			}
			if updated.IsActive {
				t.Fatalf("expected IsActive=false after the toggle, got %+v", updated)
			}

			// And back on again — the gate is about the direction-neutral
			// act of changing the flag, not about disabling specifically.
			on := true
			updated, err = svc.UpdateRole(context.Background(), "tenant-A", "role-admin", "actor-1",
				models.UpdateRoleInput{IsActive: &on})
			if err != nil {
				t.Fatalf("%s must be able to re-enable a system role: %v", systemRole, err)
			}
			if !updated.IsActive {
				t.Fatalf("expected IsActive=true after re-enabling, got %+v", updated)
			}
		})
	}
}

// Every other system role is refused — including `developer`.
//
// developer is deliberately NOT a platform-role administrator: production
// seeds it read-only (D9, mirrored by the GetEffectivePermissions
// shortcut), so admitting it here would hand a production developer a
// platform-wide WRITE that the environment gate exists to deny. The
// allowed pair is the same one the rest of the platform already treats as
// its administrator quorum (user services CountActiveAdministrators,
// user handlers isPrivilegedSystemRole).
func TestUpdateRole_SystemRoleToggleRefusedForEveryOtherSystemRole(t *testing.T) {
	for _, systemRole := range []string{"", "guest", "operator", "manager", "developer"} {
		name := systemRole
		if name == "" {
			name = "no_role"
		}
		t.Run(name, func(t *testing.T) {
			svc, repo := newTier1Service(t, staticRoleLookup(systemRole))
			repo.seedRole("role-admin", "administrator", true, []string{"*"}, "")

			off := false
			_, err := svc.UpdateRole(context.Background(), "tenant-A", "role-admin", "actor-1",
				models.UpdateRoleInput{IsActive: &off})
			if !errors.Is(err, ErrPlatformAdminRequired) {
				t.Fatalf("system role %q: err = %v, want ErrPlatformAdminRequired", systemRole, err)
			}
			if stored := repo.roleByName("administrator"); stored == nil || !stored.IsActive {
				t.Fatalf("administrator must still be active after the refusal, got %+v", stored)
			}
		})
	}
}

// Fail closed: a caller whose system role cannot be read is not a proven
// platform administrator. Both shapes refuse — the lookup erroring (the
// actor's user row is gone) and no lookup wired at all.
func TestUpdateRole_SystemRoleToggleRefusedWhenTheActorRoleCannotBeRead(t *testing.T) {
	t.Run("lookup errors", func(t *testing.T) {
		svc, repo := newTier1Service(t, func(context.Context, string) (string, error) {
			return "", errors.New("user: no such row")
		})
		repo.seedRole("role-admin", "administrator", true, []string{"*"}, "")

		off := false
		_, err := svc.UpdateRole(context.Background(), "tenant-A", "role-admin", "actor-1",
			models.UpdateRoleInput{IsActive: &off})
		if !errors.Is(err, ErrPlatformAdminRequired) {
			t.Fatalf("err = %v, want ErrPlatformAdminRequired", err)
		}
		if stored := repo.roleByName("administrator"); stored == nil || !stored.IsActive {
			t.Fatalf("administrator must still be active after the refusal, got %+v", stored)
		}
	})

	t.Run("no lookup wired", func(t *testing.T) {
		svc, repo := newTier1Service(t, nil)
		repo.seedRole("role-admin", "administrator", true, []string{"*"}, "")

		off := false
		_, err := svc.UpdateRole(context.Background(), "tenant-A", "role-admin", "actor-1",
			models.UpdateRoleInput{IsActive: &off})
		if !errors.Is(err, ErrPlatformAdminRequired) {
			t.Fatalf("err = %v, want ErrPlatformAdminRequired", err)
		}
		if stored := repo.roleByName("administrator"); stored == nil || !stored.IsActive {
			t.Fatalf("administrator must still be active after the refusal, got %+v", stored)
		}
	})
}

// An unidentifiable caller is refused by name. The lookup here says
// super_admin for every UUID it is asked about, so this can only be the
// empty-actor branch: without an actor there is nobody to check.
//
// It also closes a gap the D21 validator left open on this path — the
// handler maps a request that spells the "system" sentinel to the empty
// actor expecting ErrGranterRequired, but an IsActive-only patch never
// reaches the validator that raises it.
func TestUpdateRole_SystemRoleToggleRequiresAnActor(t *testing.T) {
	svc, repo := newTier1Service(t, staticRoleLookup("super_admin"))
	repo.seedRole("role-admin", "administrator", true, []string{"*"}, "")

	off := false
	_, err := svc.UpdateRole(context.Background(), "tenant-A", "role-admin", "",
		models.UpdateRoleInput{IsActive: &off})
	if !errors.Is(err, ErrGranterRequired) {
		t.Fatalf("err = %v, want ErrGranterRequired", err)
	}
	if stored := repo.roleByName("administrator"); stored == nil || !stored.IsActive {
		t.Fatalf("administrator must still be active after the refusal, got %+v", stored)
	}
}

// Custom roles are untouched by the gate: their own tenant-scope guard
// already bounds who may edit them, and a tenant role is not platform
// configuration. An "operator" actor — refused above on a system row —
// disables one here without objection.
func TestUpdateRole_CustomRoleToggleUnaffectedByTheGate(t *testing.T) {
	svc, repo := newTier1Service(t, staticRoleLookup("operator"))
	repo.seedRole("role-c", "billing_reader", false, []string{"billing.invoice.read"}, "tenant-A")

	off := false
	updated, err := svc.UpdateRole(context.Background(), "tenant-A", "role-c", "org-owner-1",
		models.UpdateRoleInput{IsActive: &off})
	if err != nil {
		t.Fatalf("a custom role toggle must not be gated on the platform role: %v", err)
	}
	if updated.IsActive {
		t.Fatalf("expected IsActive=false after the toggle, got %+v", updated)
	}
}

// The immutability rule is unchanged and still comes first: it refuses
// name/description/permissions on a system row for EVERY caller, and a
// patch that mixes an immutable field with IsActive is refused as
// immutable rather than as a gate failure.
func TestUpdateRole_SystemRoleImmutabilityUnchangedByTheGate(t *testing.T) {
	rename := "renamed"
	description := "rewritten"
	off := false

	cases := []struct {
		name       string
		systemRole string
		input      models.UpdateRoleInput
	}{
		{"name, non-admin caller", "operator", models.UpdateRoleInput{Name: &rename}},
		{"name, super_admin caller", "super_admin", models.UpdateRoleInput{Name: &rename}},
		{"description, non-admin caller", "operator", models.UpdateRoleInput{Description: &description}},
		{"permissions, super_admin caller", "super_admin", models.UpdateRoleInput{Permissions: []string{"tenant.read"}}},
		{"name plus isActive, non-admin caller", "operator", models.UpdateRoleInput{Name: &rename, IsActive: &off}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo := newTier1Service(t, staticRoleLookup(tc.systemRole))
			repo.seedRole("role-admin", "administrator", true, []string{"*"}, "")

			_, err := svc.UpdateRole(context.Background(), "tenant-A", "role-admin", "actor-1", tc.input)
			if !errors.Is(err, ErrSystemRoleImmutable) {
				t.Fatalf("err = %v, want ErrSystemRoleImmutable", err)
			}
			stored := repo.roleByName("administrator")
			if stored == nil || !stored.IsActive || len(stored.Permissions) != 1 || stored.Permissions[0] != "*" {
				t.Fatalf("administrator must be untouched after the refusal, got %+v", stored)
			}
		})
	}
}

// The allowed set is a subset of the platform role names, so a typo in
// one map cannot silently admit a name the other does not know.
func TestPlatformAdminRoles_AreASubsetOfThePlatformSystemRoleNames(t *testing.T) {
	for name := range platformAdminRoles {
		if _, ok := platformSystemRoleNames[name]; !ok {
			t.Errorf("%q is not a platform system role name", name)
		}
	}
	if len(platformAdminRoles) == 0 {
		t.Fatal("the allowed set must not be empty — an empty set locks every operator out")
	}
}
