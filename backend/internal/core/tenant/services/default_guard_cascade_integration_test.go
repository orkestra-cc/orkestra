package services

// These tests pin the ordering invariant a whole-branch review found
// violated: cascadeTenantData hard-deletes EVERY membership and EVERY
// closure row for a tenant, so it must never run until the platform-default
// guard has confirmed the target is not the default — and it must not run at
// all when the guarded write itself fails.
//
// Like default_tenant_integration_test.go they need a genuine replica-set
// MongoDB (RunDefaultGuarded is a multi-document transaction). See that
// file's header for the MONGO_TEST_URI recipe; newDefaultsTestDB skips the
// whole suite when the variable is unset.

import (
	"context"
	"errors"
	"testing"

	"github.com/orkestra/backend/internal/core/tenant/models"
	"github.com/orkestra/backend/internal/core/tenant/repository"
	"go.mongodb.org/mongo-driver/bson"
)

// cascadingLifecycleCase names one of the two lifecycle mutations that run
// cascadeTenantData. Suspend and archive only flip status, so they are out of
// scope here.
type cascadingLifecycleCase struct {
	name string
	call func(s *Service, ctx context.Context, uuid string) error
}

func cascadingLifecycleCases() []cascadingLifecycleCase {
	return []cascadingLifecycleCase{
		{
			name: "delete",
			call: func(s *Service, ctx context.Context, uuid string) error { return s.DeleteTenant(ctx, uuid) },
		},
		{
			name: "purge",
			call: func(s *Service, ctx context.Context, uuid string) error { return s.PurgeTenant(ctx, uuid) },
		},
	}
}

// seedCascadeFixture creates a tenant plus exactly the rows cascadeTenantData
// destroys: two memberships (owner + one plain member) and the self-ancestor
// closure row.
func seedCascadeFixture(t *testing.T, repo *repository.Repository) *models.Tenant {
	t.Helper()
	tn := seedDefaultsTenant(t, repo, nil)
	seedCascadeMemberships(t, repo, tn.UUID, tn.OwnerUserUUID)
	if err := repo.InsertSelfAncestor(context.Background(), tn.UUID); err != nil {
		t.Fatalf("seed self ancestor: %v", err)
	}
	return tn
}

// seedCascadeMemberships inserts an owner membership plus one plain member for
// tenantUUID. Split out of seedCascadeFixture so a test can attach memberships
// to a tenant UUID that has no tenant row at all.
func seedCascadeMemberships(t *testing.T, repo *repository.Repository, tenantUUID, ownerUUID string) {
	t.Helper()
	ctx := context.Background()
	for _, user := range []string{ownerUUID, "member-" + randSuffix(t)} {
		if err := repo.CreateMembership(ctx, &models.TenantMembership{
			UUID:       "membership-" + randSuffix(t),
			UserUUID:   user,
			TenantUUID: tenantUUID,
			TenantKind: models.TenantKindInternal,
			Roles:      []string{"org_owner"},
			IsOwner:    user == ownerUUID,
		}); err != nil {
			t.Fatalf("seed membership: %v", err)
		}
	}
}

// countMemberships reads the persisted membership rows back for tenantUUID.
// Every assertion in this file is on persisted state rather than on the
// returned error: the defect being guarded answered 409 ("nothing happened")
// while the rows were already gone.
func countMemberships(t *testing.T, repo *repository.Repository, tenantUUID string) int {
	t.Helper()
	rows, err := repo.ListMembershipsByTenant(context.Background(), tenantUUID)
	if err != nil {
		t.Fatalf("ListMembershipsByTenant(%s): %v", tenantUUID, err)
	}
	return len(rows)
}

// countAncestors reads the persisted closure rows back for tenantUUID.
func countAncestors(t *testing.T, repo *repository.Repository, tenantUUID string) int {
	t.Helper()
	rows, err := repo.ListAncestors(context.Background(), tenantUUID)
	if err != nil {
		t.Fatalf("ListAncestors(%s): %v", tenantUUID, err)
	}
	return len(rows)
}

// TestLifecycleCascade_DefaultLookupFailure_AbortsBeforeCascade covers the
// fail-open path: when DefaultTenantUUID returned a repository error the
// pre-check was skipped entirely and execution fell through to the cascade,
// hard-deleting every membership and closure row of a tenant nobody had
// proven was safe to touch. The lookup error must now propagate before
// anything is destroyed.
//
// The failure is injected by corrupting the platform-default pointer document
// so repository.GetDefault cannot decode it. That is deliberately surgical:
// every OTHER collection stays healthy, so a cascade still running ahead of
// the guard would succeed and the rows would really be gone.
func TestLifecycleCascade_DefaultLookupFailure_AbortsBeforeCascade(t *testing.T) {
	for _, tc := range cascadingLifecycleCases() {
		t.Run(tc.name, func(t *testing.T) {
			db, cleanup := newDefaultsTestDB(t)
			defer cleanup()
			repo := repository.New(db)
			svc := New(repo)
			ctx := context.Background()

			target := seedCascadeFixture(t, repo)

			//tenantscope:allow system: test corrupts the platform-global default pointer singleton so repository.GetDefault fails to decode it
			if _, err := db.Collection(repository.CollDefaults).InsertOne(ctx, bson.M{
				"kind":       string(models.TenantKindInternal),
				"tenantUUID": 42, // models.TenantDefault declares a string
				"revision":   int64(1),
			}); err != nil {
				t.Fatalf("insert corrupt pointer: %v", err)
			}
			if _, err := svc.DefaultTenantUUID(ctx); err == nil {
				t.Fatalf("DefaultTenantUUID = nil error, want the injected decode failure (fixture is not exercising the path)")
			}

			err := tc.call(svc, ctx, target.UUID)
			if err == nil {
				t.Fatalf("%s with an unreadable default pointer = nil, want the lookup error propagated", tc.name)
			}
			if errors.Is(err, ErrDefaultReassignmentRequired) {
				t.Fatalf("%s = ErrDefaultReassignmentRequired, want the lookup error propagated", tc.name)
			}
			if n := countMemberships(t, repo, target.UUID); n != 2 {
				t.Fatalf("membership count = %d, want 2 — the cascade must not run when the default pointer cannot be read", n)
			}
			if n := countAncestors(t, repo, target.UUID); n != 1 {
				t.Fatalf("closure-row count = %d, want 1 — the cascade must not run when the default pointer cannot be read", n)
			}
			row, gerr := repo.GetTenantByUUIDIncludingDeleted(ctx, target.UUID)
			if gerr != nil {
				t.Fatalf("GetTenantByUUIDIncludingDeleted: %v", gerr)
			}
			if row.Status != models.TenantStatusActive || row.DeletedAt != nil {
				t.Fatalf("tenant row mutated after an aborted %s: status=%q deletedAt=%v", tc.name, row.Status, row.DeletedAt)
			}
		})
	}
}

// TestLifecycleCascade_DefaultTarget_LeavesMembershipsIntact is the
// state-level companion to TestLifecycleGuards_BlockDefaultTarget, which
// asserts only on the tenant row: a denied lifecycle mutation must leave the
// platform default's memberships and closure rows untouched too. Without them
// the tenant stays active and stays the default while no operator is a member
// of it any more — JWT issuance stops selecting it as the tenant fallback and
// tenant resolution rejects X-Tenant-ID for it.
//
// The non-default control in the second half proves the cascade still runs
// exactly as before for a tenant the guard clears.
func TestLifecycleCascade_DefaultTarget_LeavesMembershipsIntact(t *testing.T) {
	for _, tc := range cascadingLifecycleCases() {
		t.Run(tc.name, func(t *testing.T) {
			db, cleanup := newDefaultsTestDB(t)
			defer cleanup()
			repo := repository.New(db)
			svc := New(repo)
			ctx := context.Background()

			defaultTenant := seedCascadeFixture(t, repo)
			otherTenant := seedCascadeFixture(t, repo)
			if err := svc.AssignDefaultTenant(ctx, defaultTenant.UUID, "", models.DefaultUpdateSourceSetup); err != nil {
				t.Fatalf("AssignDefaultTenant: %v", err)
			}

			if err := tc.call(svc, ctx, defaultTenant.UUID); !errors.Is(err, ErrDefaultReassignmentRequired) {
				t.Fatalf("%s(default) = %v, want ErrDefaultReassignmentRequired", tc.name, err)
			}
			if n := countMemberships(t, repo, defaultTenant.UUID); n != 2 {
				t.Fatalf("default tenant membership count = %d after denied %s, want 2", n, tc.name)
			}
			if n := countAncestors(t, repo, defaultTenant.UUID); n != 1 {
				t.Fatalf("default tenant closure-row count = %d after denied %s, want 1", n, tc.name)
			}

			// Control: a tenant the guard clears still cascades.
			if err := tc.call(svc, ctx, otherTenant.UUID); err != nil {
				t.Fatalf("%s(non-default) = %v, want nil", tc.name, err)
			}
			if n := countMemberships(t, repo, otherTenant.UUID); n != 0 {
				t.Fatalf("non-default tenant membership count = %d after %s, want 0 (cascade must still run)", n, tc.name)
			}
			if n := countAncestors(t, repo, otherTenant.UUID); n != 0 {
				t.Fatalf("non-default tenant closure-row count = %d after %s, want 0 (cascade must still run)", n, tc.name)
			}
		})
	}
}

// TestLifecycleCascade_NotRunWhenGuardedWriteFails proves the cascade now sits
// behind the guarded write rather than ahead of it. A tenant UUID that has
// memberships but no tenant row makes the guarded write fail deterministically
// with ErrNotFound — the deterministic stand-in for "the guarded write did not
// happen", which is exactly what a lost race against a concurrent
// TransferDefaultTenant produces via ErrDefaultGuard. Before the fix the
// cascade had already committed by the time the guard was consulted.
func TestLifecycleCascade_NotRunWhenGuardedWriteFails(t *testing.T) {
	for _, tc := range cascadingLifecycleCases() {
		t.Run(tc.name, func(t *testing.T) {
			db, cleanup := newDefaultsTestDB(t)
			defer cleanup()
			repo := repository.New(db)
			svc := New(repo)
			ctx := context.Background()

			// Memberships and a closure row pointing at a tenant UUID that has
			// no tenant document at all.
			missingUUID := "tenant-missing-" + randSuffix(t)
			seedCascadeMemberships(t, repo, missingUUID, "owner-"+randSuffix(t))
			if err := repo.InsertSelfAncestor(ctx, missingUUID); err != nil {
				t.Fatalf("seed self ancestor: %v", err)
			}

			if err := tc.call(svc, ctx, missingUUID); !errors.Is(err, repository.ErrNotFound) {
				t.Fatalf("%s(missing row) = %v, want repository.ErrNotFound", tc.name, err)
			}
			if n := countMemberships(t, repo, missingUUID); n != 2 {
				t.Fatalf("membership count = %d, want 2 — the cascade must not commit when the guarded write fails", n)
			}
			if n := countAncestors(t, repo, missingUUID); n != 1 {
				t.Fatalf("closure-row count = %d, want 1 — the cascade must not commit when the guarded write fails", n)
			}
		})
	}
}
