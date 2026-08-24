package repository

// First-assignment counterpart to defaults_integration_test.go, whose
// harness helpers (newDefaultsTestDB, seedInternalActiveTenant) these tests
// reuse — including its replica-set requirement and MONGO_TEST_URI gate.
//
// The tests there all start from a kind that ALREADY has a pointer, which
// is the transfer path (SetDefault). These cover the path that has none
// yet: the setup/finalization first assignment (AssignDefault), where the
// guarded lifecycle side used to write nothing at all.

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/orkestra/backend/internal/core/tenant/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// TestRunDefaultGuarded_WritesSingletonWhenUnassigned is the deterministic
// half of the first-assignment serialization fix, and it pins the exact
// property RunDefaultGuarded's doc comment claims: that the guard's first
// mutation is a write to the tenant_defaults singleton for kind, so a
// concurrent AssignDefault has something to conflict with.
//
// The revision bump used to be a plain UpdateOne with no upsert. Against a
// kind with no pointer yet — precisely the first-assignment window — it
// matched zero documents and wrote NOTHING, so the write-conflict
// serialization that comment describes did not exist there: AssignDefault
// only READS the tenant row it validates, and MongoDB transactions are
// snapshot-isolated with no read-write conflict detection, so an assign and
// a lifecycle mutation of the same tenant could both commit.
//
// Reading the singleton from INSIDE the guarded write's own session is what
// makes this deterministic: it observes the transaction's own uncommitted
// write, which is exactly the state a racing transaction collides with. The
// row is a placeholder — it carries no tenantUUID — and must be gone once
// the transaction commits, so the collection keeps its invariant: a
// tenant_defaults row exists if and only if a default is assigned.
func TestRunDefaultGuarded_WritesSingletonWhenUnassigned(t *testing.T) {
	db, cleanup := newDefaultsTestDB(t)
	defer cleanup()
	r := New(db)
	ctx := context.Background()

	target := seedInternalActiveTenant(t, db)

	var seen bson.M
	err := r.RunDefaultGuarded(ctx, models.TenantKindInternal, target.UUID, func(sc mongo.SessionContext) error {
		//tenantscope:allow system: test reads the platform-global default pointer inside the guard's own session to observe its uncommitted write
		return db.Collection(CollDefaults).FindOne(sc, bson.M{"kind": string(models.TenantKindInternal)}).Decode(&seen)
	})
	if err != nil {
		t.Fatalf("RunDefaultGuarded on an unassigned kind: %v (the guard must still establish a write on the singleton)", err)
	}
	if seen == nil {
		t.Fatal("guard invoked write without having written the tenant_defaults singleton: a concurrent AssignDefault has nothing to conflict with")
	}
	if got, ok := seen["tenantUUID"]; ok {
		t.Fatalf("guard placeholder row carries tenantUUID=%v, want the field absent", got)
	}

	// No residue: the placeholder exists only to create a conflict domain.
	if _, gerr := r.GetDefault(ctx, models.TenantKindInternal); !errors.Is(gerr, ErrNotFound) {
		t.Fatalf("GetDefault after a guarded write on an unassigned kind = %v, want ErrNotFound (the guard leaked its placeholder row)", gerr)
	}
}

// TestAssignVersusLifecycle_Serialized is TestTransferVersusLifecycle_Serialized's
// missing twin: the same race, but on a kind with NO pointer yet.
//
// SetDefault always writes the singleton, so the transfer race was already
// serialized. AssignDefault writes only after its in-transaction conflict
// check, and the guarded lifecycle side used to write nothing at all when
// the pointer was absent — so both transactions could commit and leave the
// freshly assigned default naming a soft-deleted tenant. Either outcome
// (assigned, or deleted-and-unassigned) is acceptable; a pointer naming a
// non-operational tenant is not.
func TestAssignVersusLifecycle_Serialized(t *testing.T) {
	db, cleanup := newDefaultsTestDB(t)
	defer cleanup()
	r := New(db)
	ctx := context.Background()

	const iterations = 40
	for i := 0; i < iterations; i++ {
		target := seedInternalActiveTenant(t, db)

		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, _ = r.AssignDefault(ctx, models.TenantKindInternal, target.UUID, "", models.DefaultUpdateSourceSetup)
		}()
		go func() {
			defer wg.Done()
			<-start
			_ = r.RunDefaultGuarded(ctx, models.TenantKindInternal, target.UUID, func(sc mongo.SessionContext) error {
				return r.SoftDeleteTenant(sc, target.UUID, models.TenantDeleteReasonAdminAction)
			})
		}()
		close(start)
		wg.Wait()

		final, err := r.GetDefault(ctx, models.TenantKindInternal)
		switch {
		case errors.Is(err, ErrNotFound):
			// The lifecycle mutation won; nothing was assigned.
		case err != nil:
			t.Fatalf("iteration %d: GetDefault: %v", i, err)
		default:
			named, rerr := r.GetTenantByUUIDIncludingDeleted(ctx, final.TenantUUID)
			if rerr != nil {
				t.Fatalf("iteration %d: resolve pointer target %s: %v", i, final.TenantUUID, rerr)
			}
			if named.DeletedAt != nil || named.Status != models.TenantStatusActive {
				t.Fatalf("iteration %d: first assignment left the default pointing at a non-operational tenant %s (status=%s deletedAt=%v)",
					i, named.UUID, named.Status, named.DeletedAt)
			}
		}

		// Reset the singleton so the next iteration starts unassigned.
		//tenantscope:allow system: test teardown of the platform-global default pointer between iterations
		if _, derr := db.Collection(CollDefaults).DeleteMany(ctx, bson.M{"kind": string(models.TenantKindInternal)}); derr != nil {
			t.Fatalf("iteration %d: reset pointer: %v", i, derr)
		}
	}
}
