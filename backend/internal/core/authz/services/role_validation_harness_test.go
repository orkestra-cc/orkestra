package services

// Test harness for the D21 custom-role permission validator (H-4).
//
// Everything here wires the EXISTING pieces: the fakeRepo from
// repo_fake_test.go, through the EXISTING newTier1Service constructor in
// tier1_test.go. There is deliberately no second Service constructor and
// no second fake repo — a Service literal with a nil repo cannot be used
// here, because RegisterPermissions ends in repo.UpsertPermission and
// would nil-panic the moment a test declares a permission.
//
// The validator's own cases live in role_validation_test.go; the
// end-to-end H-4 probe and the administrator regression tests live in
// tier1_crud_test.go, which uses the free functions below against a bare
// newTier1Service pair rather than the wrapper.

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

// permSpecs is one group of permission declarations, so a test can read
// as `newValidationTestService(t, registered("tenant.read"),
// systemRegistered("system.users.admin"))`. Groups are flattened into a
// single RegisterPermissions call: that method REPLACES cachedPermSpecs
// rather than appending to it, so registering in several calls would
// leave ensureSeeded able to replay only the last group.
type permSpecs []iface.PermissionSpec

// moduleOf derives an owning module name from a dot-notation key, so the
// Permission rows the fake repo stores look like production ones without
// every test spelling the module out.
func moduleOf(key string) string {
	if i := strings.Index(key, "."); i > 0 {
		return key[:i]
	}
	return "test"
}

// registered declares ordinary, tenant-grantable permission keys.
func registered(keys ...string) permSpecs {
	out := make(permSpecs, 0, len(keys))
	for _, k := range keys {
		out = append(out, iface.PermissionSpec{Key: k, Module: moduleOf(k)})
	}
	return out
}

// systemRegistered declares platform-reserved (System:true) permission
// keys. A System key lands in BOTH systemPermissionSet and
// allPermissionSet — the two sets are ordered checks, not a partition.
func systemRegistered(keys ...string) permSpecs {
	out := make(permSpecs, 0, len(keys))
	for _, k := range keys {
		out = append(out, iface.PermissionSpec{Key: k, Module: moduleOf(k), System: true})
	}
	return out
}

// registerTestPermissions registers the flattened groups on svc. Usable
// with a bare `svc, repo := newTier1Service(t, staticRoleLookup(""))`
// pair as well as through the wrapper below.
func registerTestPermissions(t *testing.T, svc *Service, groups ...permSpecs) {
	t.Helper()
	specs := make([]iface.PermissionSpec, 0)
	for _, g := range groups {
		specs = append(specs, g...)
	}
	if len(specs) == 0 {
		return
	}
	if err := svc.RegisterPermissions(context.Background(), specs); err != nil {
		t.Fatalf("RegisterPermissions: %v", err)
	}
}

// validationTestSeq keeps the synthetic role/binding UUIDs unique, so a
// test may grant the same actor twice and get the union rather than a
// silent overwrite.
var validationTestSeq atomic.Int64

// grantActorPermissions gives actor the listed permissions inside
// tenantID, the way production does it: a custom role carrying them plus
// a binding onto the actor. GetEffectivePermissions therefore returns
// them, which is what the cascade check reads.
//
// Pass tenantID "" for a GLOBAL binding — and note that platform-reserved
// keys and "*" are conveyed ONLY that way. D22 strips both out of the
// effective set when the binding is tenant-scoped, so granting them at a
// tenantID silently leaves the actor holding nothing: to model a
// super_admin-equivalent wildcard holder, grant "*" globally (which is
// also what validateBindingScope requires of a platform role), or pin the
// caller's system role with newValidationTestServiceWithRole.
//
// Usable with a bare `svc, repo := newTier1Service(t, staticRoleLookup(""))`
// pair as well as through the wrapper below.
func grantActorPermissions(t *testing.T, repo *fakeRepo, actor, tenantID string, perms ...string) {
	t.Helper()
	if repo == nil {
		t.Fatal("grantActorPermissions needs the fakeRepo newTier1Service returns")
	}
	n := validationTestSeq.Add(1)
	roleUUID := fmt.Sprintf("granted-role-%d", n)
	repo.seedRole(roleUUID, fmt.Sprintf("granted_role_%d", n), false, perms, tenantID)
	repo.seedBinding(fmt.Sprintf("granted-binding-%d", n), actor, tenantID, roleUUID)
}

// validationTestService bundles the Service with the fakeRepo behind it
// so a validator test can both call the method under test and arrange
// what the actor holds, without threading the repo through every line.
// The embedded *Service means svc.validateCustomRolePermissions(…),
// svc.RegisterPermissions(…) and svc.allPermissionSet all read exactly
// as they would on a plain *Service.
// It deliberately does NOT store the *testing.T: a wrapper method that
// closed over the parent T would call Fatalf on it from a subtest's
// goroutine, which Go forbids (FailNow from the wrong goroutine hangs or
// mis-attributes the failure). Every helper takes the T of the test that
// is actually running.
type validationTestService struct {
	*Service
	repo *fakeRepo
}

// newValidationTestService stands up a Service over the in-memory repo
// with no Redis and no Cedar engine, and registers the supplied
// permission groups. The acting user has NO system role, so an actor's
// effective permissions come only from the bindings a test grants.
func newValidationTestService(t *testing.T, groups ...permSpecs) *validationTestService {
	t.Helper()
	return newValidationTestServiceWithRole(t, "", groups...)
}

// newValidationTestServiceWithRole is newValidationTestService with the
// caller's platform system role pinned — for the cases where the actor's
// privilege comes from the role shortcut in GetEffectivePermissions
// ("super_admin" ⇒ wildcard, "administrator" ⇒ every System key) rather
// than from a binding.
func newValidationTestServiceWithRole(t *testing.T, systemRole string, groups ...permSpecs) *validationTestService {
	t.Helper()
	svc, repo := newTier1Service(t, staticRoleLookup(systemRole))
	registerTestPermissions(t, svc, groups...)
	return &validationTestService{Service: svc, repo: repo}
}

// grantActor gives actor the listed permissions inside tenantID. Pass the
// T of the running test — including inside a t.Run subtest.
//
// There is deliberately no post-construction `register` counterpart:
// RegisterPermissions REPLACES cachedPermSpecs rather than appending, so
// a second call would leave ensureSeeded able to replay only the last
// group. Declare every group in the constructor instead.
func (v *validationTestService) grantActor(t *testing.T, actor, tenantID string, perms ...string) {
	t.Helper()
	grantActorPermissions(t, v.repo, actor, tenantID, perms...)
}

// TestValidationHarness_RegistersPermissionsAndGrantsAnActor pins the two
// things the validator's tests will lean on, so a harness fault reads as
// a harness failure rather than as a validator bug.
func TestValidationHarness_RegistersPermissionsAndGrantsAnActor(t *testing.T) {
	svc := newValidationTestService(t,
		registered("tenant.read", "tenant.delete"),
		systemRegistered("system.users.admin"))

	if _, ok := svc.allPermissionSet["tenant.read"]; !ok {
		t.Error("allPermissionSet must contain every registered key")
	}
	if _, ok := svc.systemPermissionSet["system.users.admin"]; !ok {
		t.Error("systemPermissionSet must contain every System:true key")
	}
	if _, ok := svc.allPermissionSet["system.users.admin"]; !ok {
		t.Error("a System key is ALSO in allPermissionSet — the sets are ordered checks, not a partition")
	}

	svc.grantActor(t, "actor-1", "tenant-1", "tenant.read")
	got, err := svc.GetEffectivePermissions(context.Background(), "actor-1", "tenant-1")
	if err != nil {
		t.Fatalf("GetEffectivePermissions: %v", err)
	}
	if len(got) != 1 || got[0] != "tenant.read" {
		t.Fatalf("effective permissions = %v, want exactly [tenant.read]", got)
	}

	// A second grant unions rather than overwriting the first.
	svc.grantActor(t, "actor-1", "tenant-1", "tenant.delete")
	got, err = svc.GetEffectivePermissions(context.Background(), "actor-1", "tenant-1")
	if err != nil {
		t.Fatalf("GetEffectivePermissions: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("effective permissions = %v, want both grants", got)
	}
}
