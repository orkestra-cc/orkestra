package services

// H-4: UpdateRole replaced `permissions` verbatim with no catalog check,
// no platform-key check and no cascade, so any caller who could edit a
// custom role could write ANY string into it — including a permission
// nobody holds and a platform key no tenant role may carry — and then
// bind themselves to it. The audit's probe is create → bind → update
// with tenant.delete; TestH4Probe_* in tier1_crud_test.go runs it
// end-to-end, and the cases here pin each of the four checks the gate
// applies on the way.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

func TestValidateCustomRolePermissions_RejectsAnUnknownKey(t *testing.T) {
	svc := newValidationTestService(t, registered("tenant.read", "tenant.update"))
	_, err := svc.validateCustomRolePermissions(context.Background(), "tenant-1", "actor-1",
		[]string{"tenant.read", "tenant.obliterate"})
	if !errors.Is(err, ErrUnknownPermission) {
		t.Fatalf("err = %v, want ErrUnknownPermission", err)
	}
	if !strings.Contains(err.Error(), "tenant.obliterate") {
		t.Errorf("the error must name the offending key, got %q", err)
	}
	if key, ok := OffendingPermissionKey(err); !ok || key != "tenant.obliterate" {
		t.Errorf("OffendingPermissionKey = %q,%v; want tenant.obliterate,true", key, ok)
	}
}

func TestValidateCustomRolePermissions_RejectsASystemKey(t *testing.T) {
	svc := newValidationTestService(t,
		registered("tenant.read"), systemRegistered("system.users.admin"))
	_, err := svc.validateCustomRolePermissions(context.Background(), "tenant-1", "actor-1",
		[]string{"tenant.read", "system.users.admin"})
	if !errors.Is(err, ErrSystemPermissionInCustomRole) {
		t.Fatalf("err = %v, want ErrSystemPermissionInCustomRole", err)
	}
	if key, ok := OffendingPermissionKey(err); !ok || key != "system.users.admin" {
		t.Errorf("OffendingPermissionKey = %q,%v; want system.users.admin,true", key, ok)
	}
}

func TestValidateCustomRolePermissions_RejectsWildcard(t *testing.T) {
	svc := newValidationTestService(t, registered("tenant.read"))
	_, err := svc.validateCustomRolePermissions(context.Background(), "tenant-1", "actor-1", []string{"*"})
	if !errors.Is(err, ErrSystemPermissionInCustomRole) {
		t.Fatalf("err = %v, want ErrSystemPermissionInCustomRole for the wildcard", err)
	}
}

func TestValidateCustomRolePermissions_RejectsAnEmptyList(t *testing.T) {
	svc := newValidationTestService(t, registered("tenant.read"))
	if _, err := svc.validateCustomRolePermissions(context.Background(), "tenant-1", "actor-1", []string{}); !errors.Is(err, ErrRolePermissionsRequired) {
		t.Fatalf("err = %v, want ErrRolePermissionsRequired", err)
	}
	// …and a list that is empty only after trimming.
	if _, err := svc.validateCustomRolePermissions(context.Background(), "tenant-1", "actor-1", []string{"  ", ""}); !errors.Is(err, ErrRolePermissionsRequired) {
		t.Fatalf("err = %v for a whitespace-only list, want ErrRolePermissionsRequired", err)
	}
}

func TestValidateCustomRolePermissions_TrimsAndDeduplicates(t *testing.T) {
	svc := newValidationTestService(t, registered("tenant.read", "tenant.update"))
	svc.grantActor(t, "actor-1", "tenant-1", "tenant.read", "tenant.update")

	got, err := svc.validateCustomRolePermissions(context.Background(), "tenant-1", "actor-1",
		[]string{" tenant.read ", "tenant.read", "tenant.update"})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %v, want 2 de-duplicated keys", got)
	}
	if got[0] != "tenant.read" || got[1] != "tenant.update" {
		t.Errorf("got %v, want the trimmed keys in first-seen order", got)
	}
}

// The cascade: a role can never carry more than its editor holds.
func TestValidateCustomRolePermissions_RefusesAKeyTheActorLacks(t *testing.T) {
	svc := newValidationTestService(t, registered("tenant.read", "tenant.delete"))
	svc.grantActor(t, "actor-1", "tenant-1", "tenant.read")

	_, err := svc.validateCustomRolePermissions(context.Background(), "tenant-1", "actor-1",
		[]string{"tenant.read", "tenant.delete"})
	if !errors.Is(err, ErrInsufficientPermissionsToGrant) {
		t.Fatalf("err = %v, want ErrInsufficientPermissionsToGrant", err)
	}
}

// Edge case 14: a super_admin's wildcard covers everything in the
// CASCADE, but the catalog and system-key checks still apply — a
// super_admin cannot put system.users.admin into a TENANT role either.
func TestValidateCustomRolePermissions_WildcardActorStillCannotAddASystemKey(t *testing.T) {
	svc := newValidationTestService(t, registered("tenant.read"), systemRegistered("system.users.admin"))
	svc.grantActor(t, "actor-1", "tenant-1", "*")

	if _, err := svc.validateCustomRolePermissions(context.Background(), "tenant-1", "actor-1",
		[]string{"system.users.admin"}); !errors.Is(err, ErrSystemPermissionInCustomRole) {
		t.Fatalf("err = %v, want ErrSystemPermissionInCustomRole even for a wildcard actor", err)
	}
}

func TestValidateCustomRolePermissions_EmptyActorIsRefused(t *testing.T) {
	svc := newValidationTestService(t, registered("tenant.read"))
	if _, err := svc.validateCustomRolePermissions(context.Background(), "tenant-1", "", []string{"tenant.read"}); !errors.Is(err, ErrGranterRequired) {
		t.Fatalf("err = %v, want ErrGranterRequired", err)
	}
}

// granterSystem bypasses the CASCADE only — seeding and internal callers
// still cannot write an unknown or platform key into a tenant role.
func TestValidateCustomRolePermissions_GranterSystemBypassesOnlyTheCascade(t *testing.T) {
	svc := newValidationTestService(t, registered("tenant.read", "tenant.delete"), systemRegistered("system.users.admin"))

	if _, err := svc.validateCustomRolePermissions(context.Background(), "tenant-1", granterSystem,
		[]string{"tenant.read", "tenant.delete"}); err != nil {
		t.Fatalf("granterSystem must bypass the cascade: %v", err)
	}
	if _, err := svc.validateCustomRolePermissions(context.Background(), "tenant-1", granterSystem,
		[]string{"system.users.admin"}); !errors.Is(err, ErrSystemPermissionInCustomRole) {
		t.Fatal("granterSystem must NOT bypass the system-key check")
	}
	if _, err := svc.validateCustomRolePermissions(context.Background(), "tenant-1", granterSystem,
		[]string{"tenant.nonexistent"}); !errors.Is(err, ErrUnknownPermission) {
		t.Fatal("granterSystem must NOT bypass the catalog check")
	}
}

// The key an error names is bounded: it is caller-supplied and the role
// schema puts no length limit on a permission string, so an oversized
// key must not travel back out through the handler's response detail.
func TestValidateCustomRolePermissions_BoundsTheKeyItEchoes(t *testing.T) {
	svc := newValidationTestService(t, registered("tenant.read"))
	huge := strings.Repeat("z", 5000)

	_, err := svc.validateCustomRolePermissions(context.Background(), "tenant-1", "actor-1", []string{huge})
	if !errors.Is(err, ErrUnknownPermission) {
		t.Fatalf("err = %v, want ErrUnknownPermission", err)
	}
	key, ok := OffendingPermissionKey(err)
	if !ok {
		t.Fatal("the error must still carry a key")
	}
	if limit := maxEchoedPermissionKey + len("…"); len(key) > limit {
		t.Errorf("echoed key is %d bytes; it must be truncated to at most %d", len(key), limit)
	}
}

// OffendingPermissionKey must not claim a key for errors that carry none
// — the handler branches on its second return value.
func TestOffendingPermissionKey_AbsentForOtherErrors(t *testing.T) {
	if key, ok := OffendingPermissionKey(ErrRolePermissionsRequired); ok {
		t.Errorf("got %q,true for a keyless sentinel; want \"\",false", key)
	}
	if key, ok := OffendingPermissionKey(nil); ok {
		t.Errorf("got %q,true for nil; want \"\",false", key)
	}
}

// allPermissionSet must actually be populated, or every key looks
// unknown and role editing breaks entirely. The registry calls
// RegisterPermissions once with the union of every module's
// Permissions(), and ensureSeeded replays the cached specs.
func TestAllPermissionSet_IsPopulatedAfterRegisterPermissions(t *testing.T) {
	svc := newValidationTestService(t)
	if err := svc.RegisterPermissions(context.Background(), []iface.PermissionSpec{
		{Key: "tenant.read", Module: "tenant"},
		{Key: "system.users.admin", Module: "user", System: true},
	}); err != nil {
		t.Fatalf("RegisterPermissions: %v", err)
	}
	if _, ok := svc.allPermissionSet["tenant.read"]; !ok {
		t.Error("allPermissionSet must contain every registered key")
	}
	if _, ok := svc.systemPermissionSet["system.users.admin"]; !ok {
		t.Error("systemPermissionSet must contain every System:true key")
	}
	if _, ok := svc.allPermissionSet["system.users.admin"]; !ok {
		t.Error("a System key is ALSO in allPermissionSet — the checks are ordered, not exclusive")
	}
}
