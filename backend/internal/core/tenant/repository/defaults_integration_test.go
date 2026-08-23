package repository

// These tests require a genuine replica-set MongoDB (or sharded cluster) —
// SetDefault and RunDefaultGuarded run inside multi-document transactions,
// which a standalone mongod does not support at all (StartSession /
// WithTransaction fail outright). Point MONGO_TEST_URI at the replica-set
// instance, e.g.:
//
//	MONGO_TEST_URI='mongodb://localhost:28017/?directConnection=true' \
//	  go test ./internal/core/tenant/repository/... -run TestSetDefault -v
//
// directConnection=true is mandatory against the CI mongod (replica set
// rs0) — without it the driver's replica-set discovery can resolve to a
// different default port on the host. newTestDB (search_test.go) skips
// the whole suite when MONGO_TEST_URI is unset, so a plain `go test ./...`
// run reports these as skipped rather than failing.

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/tenant/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

func seedInternalActiveTenant(t *testing.T, db *mongo.Database) *models.Tenant {
	t.Helper()
	return seedTenant(t, db, func(tn *models.Tenant) {
		tn.Kind = models.TenantKindInternal
		tn.Status = models.TenantStatusActive
	})
}

// TestSetDefault_ValidatesOperationalInternalTarget covers the "operational
// tenant" predicate SetDefault enforces INSIDE its transaction: kind must
// match, status must be active, and the tenant must not be soft-deleted. A
// Tier-2 (external) tenant is rejected even when active — it can never
// become the platform default. On success the pointer row carries
// updatedBy only when the actor UUID is non-empty.
func TestSetDefault_ValidatesOperationalInternalTarget(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()
	r := New(db)
	ctx := context.Background()

	notOperational := []struct {
		name string
		mut  func(*models.Tenant)
	}{
		{"suspended", func(tn *models.Tenant) { tn.Status = models.TenantStatusSuspended }},
		{"archived", func(tn *models.Tenant) { tn.Status = models.TenantStatusArchived }},
		{"soft-deleted", func(tn *models.Tenant) {
			now := time.Now()
			tn.DeletedAt = &now
		}},
		{"external", func(tn *models.Tenant) { tn.Kind = models.TenantKindExternal }},
	}
	for _, tc := range notOperational {
		t.Run(tc.name, func(t *testing.T) {
			tn := seedTenant(t, db, func(x *models.Tenant) {
				x.Kind = models.TenantKindInternal
				x.Status = models.TenantStatusActive
				tc.mut(x)
			})
			_, err := r.SetDefault(ctx, models.TenantKindInternal, tn.UUID, "", models.DefaultUpdateSourceSetup, false)
			if !errors.Is(err, ErrDefaultTargetNotOperational) {
				t.Fatalf("SetDefault(%s) = %v, want ErrDefaultTargetNotOperational", tc.name, err)
			}
			if _, gerr := r.GetDefault(ctx, models.TenantKindInternal); !errors.Is(gerr, ErrNotFound) {
				t.Fatalf("GetDefault after rejected %s target = %v, want ErrNotFound (no pointer should have been created)", tc.name, gerr)
			}
		})
	}

	operational := seedInternalActiveTenant(t, db)
	actor := "223e4567-e89b-12d3-a456-426614174000"
	prev, err := r.SetDefault(ctx, models.TenantKindInternal, operational.UUID, actor, models.DefaultUpdateSourceSetup, false)
	if err != nil {
		t.Fatalf("SetDefault(operational): %v", err)
	}
	if prev != "" {
		t.Fatalf("prev = %q, want empty (no prior pointer)", prev)
	}
	got, err := r.GetDefault(ctx, models.TenantKindInternal)
	if err != nil {
		t.Fatalf("GetDefault: %v", err)
	}
	if got.Kind != models.TenantKindInternal {
		t.Fatalf("Kind = %q, want internal", got.Kind)
	}
	if got.TenantUUID != operational.UUID {
		t.Fatalf("TenantUUID = %q, want %q", got.TenantUUID, operational.UUID)
	}
	if got.UpdateSource != models.DefaultUpdateSourceSetup {
		t.Fatalf("UpdateSource = %q, want %q", got.UpdateSource, models.DefaultUpdateSourceSetup)
	}
	if got.UpdatedBy != actor {
		t.Fatalf("UpdatedBy = %q, want %q", got.UpdatedBy, actor)
	}

	// Migration-sourced write: actorUUID empty. updatedBy must be ABSENT
	// from the raw document, never stored as an empty-string sentinel.
	migrated := seedInternalActiveTenant(t, db)
	if _, err := r.SetDefault(ctx, models.TenantKindInternal, migrated.UUID, "", models.DefaultUpdateSourceMigration, false); err != nil {
		t.Fatalf("SetDefault(migration): %v", err)
	}
	var raw bson.M
	//tenantscope:allow system: test asserts the raw pointer document shape directly (updatedBy must be absent, not empty, for a migration-sourced write)
	if err := db.Collection(CollDefaults).FindOne(ctx, bson.M{"kind": string(models.TenantKindInternal)}).Decode(&raw); err != nil {
		t.Fatalf("decode raw pointer doc: %v", err)
	}
	if v, exists := raw["updatedBy"]; exists {
		t.Fatalf("updatedBy present in raw doc after migration write: %v (want field entirely absent)", v)
	}
	if raw["updateSource"] != models.DefaultUpdateSourceMigration {
		t.Fatalf("updateSource = %v, want %q", raw["updateSource"], models.DefaultUpdateSourceMigration)
	}
}

// TestSetDefault_UniqueKindReplacesAtomically verifies a second SetDefault
// call replaces the pointer row in place (still exactly one document for
// kind=internal), returns the previous target's UUID, and bumps Revision.
func TestSetDefault_UniqueKindReplacesAtomically(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()
	r := New(db)
	ctx := context.Background()

	t1 := seedInternalActiveTenant(t, db)
	t2 := seedInternalActiveTenant(t, db)

	if _, err := r.SetDefault(ctx, models.TenantKindInternal, t1.UUID, "", models.DefaultUpdateSourceSetup, false); err != nil {
		t.Fatalf("SetDefault(t1): %v", err)
	}
	prev, err := r.SetDefault(ctx, models.TenantKindInternal, t2.UUID, "", models.DefaultUpdateSourceTransfer, true)
	if err != nil {
		t.Fatalf("SetDefault(t2): %v", err)
	}
	if prev != t1.UUID {
		t.Fatalf("prev = %q, want %q", prev, t1.UUID)
	}

	//tenantscope:allow system: test asserts the pointer singleton for kind stays a single document after replacement
	n, err := db.Collection(CollDefaults).CountDocuments(ctx, bson.M{"kind": string(models.TenantKindInternal)})
	if err != nil {
		t.Fatalf("CountDocuments: %v", err)
	}
	if n != 1 {
		t.Fatalf("tenant_defaults document count for kind=internal = %d, want 1", n)
	}

	got, err := r.GetDefault(ctx, models.TenantKindInternal)
	if err != nil {
		t.Fatalf("GetDefault: %v", err)
	}
	if got.TenantUUID != t2.UUID {
		t.Fatalf("TenantUUID = %q, want %q", got.TenantUUID, t2.UUID)
	}
	if got.Revision != 2 {
		t.Fatalf("Revision = %d, want 2", got.Revision)
	}
}

// TestSetDefault_RequireExisting verifies requireExisting=true fails with
// ErrNotFound when no pointer row exists yet, and that the aborted
// transaction leaves no partial pointer behind.
func TestSetDefault_RequireExisting(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()
	r := New(db)
	ctx := context.Background()

	tn := seedInternalActiveTenant(t, db)

	_, err := r.SetDefault(ctx, models.TenantKindInternal, tn.UUID, "", models.DefaultUpdateSourceTransfer, true)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("SetDefault(requireExisting=true, no row) = %v, want ErrNotFound", err)
	}

	if _, err := r.GetDefault(ctx, models.TenantKindInternal); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetDefault after aborted SetDefault = %v, want ErrNotFound (transaction must not leave a partial pointer)", err)
	}
}

// TestRunDefaultGuarded_BlocksDefaultTarget verifies the guard fires only
// for the tenant currently named by the pointer — a write targeting the
// current default aborts with ErrDefaultGuard and the write callback is
// never invoked, while a write targeting an unrelated tenant runs the
// callback inside the session.
func TestRunDefaultGuarded_BlocksDefaultTarget(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()
	r := New(db)
	ctx := context.Background()

	t1 := seedInternalActiveTenant(t, db)
	t2 := seedInternalActiveTenant(t, db)

	if _, err := r.SetDefault(ctx, models.TenantKindInternal, t1.UUID, "", models.DefaultUpdateSourceSetup, false); err != nil {
		t.Fatalf("SetDefault(t1): %v", err)
	}

	called := false
	err := r.RunDefaultGuarded(ctx, models.TenantKindInternal, t1.UUID, func(sc mongo.SessionContext) error {
		called = true
		return nil
	})
	if !errors.Is(err, ErrDefaultGuard) {
		t.Fatalf("RunDefaultGuarded(t1, is-default) = %v, want ErrDefaultGuard", err)
	}
	if called {
		t.Fatalf("write callback invoked despite ErrDefaultGuard")
	}

	called = false
	err = r.RunDefaultGuarded(ctx, models.TenantKindInternal, t2.UUID, func(sc mongo.SessionContext) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("RunDefaultGuarded(t2, unrelated): %v", err)
	}
	if !called {
		t.Fatalf("write callback not invoked for unrelated (non-default) target")
	}
}

// TestTransferVersusLifecycle_Serialized is the transactional-design
// centerpiece: it races SetDefault(..., T2, ...) against
// RunDefaultGuarded(..., T2, soft-delete T2) across 40 iterations. Because
// both write the same tenant_defaults singleton as their first mutation,
// MongoDB's write-conflict detection forces one transaction to retry
// against the other's committed outcome — the retry re-validates from
// scratch, so the two possible outcomes are both safe: either the transfer
// commits first and the guarded delete aborts with ErrDefaultGuard, or the
// delete commits first and the transfer's re-validated target-operational
// check fails with ErrDefaultTargetNotOperational. Either way, the pointer
// must never end up naming a non-operational tenant.
func TestTransferVersusLifecycle_Serialized(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()
	r := New(db)
	ctx := context.Background()

	const iterations = 40
	for i := 0; i < iterations; i++ {
		t1 := seedInternalActiveTenant(t, db)
		t2 := seedInternalActiveTenant(t, db)

		if _, err := r.SetDefault(ctx, models.TenantKindInternal, t1.UUID, "", models.DefaultUpdateSourceSetup, false); err != nil {
			t.Fatalf("iteration %d: seed SetDefault(t1): %v", i, err)
		}

		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, _ = r.SetDefault(ctx, models.TenantKindInternal, t2.UUID, "", models.DefaultUpdateSourceTransfer, true)
		}()
		go func() {
			defer wg.Done()
			<-start
			_ = r.RunDefaultGuarded(ctx, models.TenantKindInternal, t2.UUID, func(sc mongo.SessionContext) error {
				return r.SoftDeleteTenant(sc, t2.UUID)
			})
		}()
		close(start)
		wg.Wait()

		final, err := r.GetDefault(ctx, models.TenantKindInternal)
		if err != nil {
			t.Fatalf("iteration %d: GetDefault: %v", i, err)
		}
		named, err := r.GetTenantByUUIDIncludingDeleted(ctx, final.TenantUUID)
		if err != nil {
			t.Fatalf("iteration %d: resolve pointer target %s: %v", i, final.TenantUUID, err)
		}
		if named.DeletedAt != nil || named.Status != models.TenantStatusActive {
			t.Fatalf("iteration %d: default pointer names non-operational tenant %s (status=%s deletedAt=%v)",
				i, named.UUID, named.Status, named.DeletedAt)
		}
	}
}
