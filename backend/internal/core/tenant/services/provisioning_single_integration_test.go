package services

// Concurrency tests for the `single` Tier-1 provisioning invariant: at most
// one tenant of a tier may occupy a provisioning slot. Reuses
// setup_tenant_integration_test.go's harness (newSetupTenantTestDB,
// newSetupTenantService, singleModeResolver, randSuffix) including its
// MONGO_TEST_URI gate and the provisioning-lock unique index it builds.
//
// The invariant used to be a count followed by a separate insert. That is a
// TOCTOU gap, and — importantly — one that wrapping the pair in a
// transaction does NOT close on its own: MongoDB transactions are
// snapshot-isolated with no read-write conflict detection, so two creations
// each counting zero and each inserting a DIFFERENT tenant document collide
// on nothing and both commit. Only a shared per-kind lock row makes them
// conflict. See repository/provisioning_locks.go.

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/tenant/models"
)

// TestCreateTenant_SingleMode_ConcurrentCreatesExactlyOneWins is the
// create/create race. Every caller sees an empty tier at the moment it
// checks; exactly one may end up with a slot.
func TestCreateTenant_SingleMode_ConcurrentCreatesExactlyOneWins(t *testing.T) {
	db, cleanup := newSetupTenantTestDB(t)
	defer cleanup()
	repo, svc, _, _ := newSetupTenantService(db)
	svc.SetProvisioningModeResolver(singleModeResolver)
	ctx := context.Background()

	const racers = 6
	suffix := randSuffix(t)

	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make([]error, racers)
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, errs[i] = svc.CreateTenant(ctx, fmt.Sprintf("owner-%s-%d", suffix, i), models.CreateTenantInput{
				Name: fmt.Sprintf("Racer %s %d", suffix, i),
				Slug: fmt.Sprintf("racer-%s-%d", suffix, i),
				Kind: models.TenantKindInternal,
			})
		}(i)
	}
	close(start)
	wg.Wait()

	var won int
	for i, err := range errs {
		switch {
		case err == nil:
			won++
		case errors.Is(err, ErrProvisioningLocked):
			// The correct refusal.
		default:
			t.Errorf("racer %d: unexpected error %v (want nil or ErrProvisioningLocked)", i, err)
		}
	}
	if won != 1 {
		t.Errorf("%d racers created a tenant, want exactly 1", won)
	}

	n, err := repo.CountProvisioningSlotsByKind(ctx, models.TenantKindInternal)
	if err != nil {
		t.Fatalf("CountProvisioningSlotsByKind: %v", err)
	}
	if n != 1 {
		t.Fatalf("tier holds %d provisioning-slot occupants, want exactly 1 — `single` mode was breached", n)
	}
}

// TestSetupTenant_SingleMode_ConcurrentCreateAndRestore is the create/restore
// race. The restore branch re-occupies a slot just as a creation does, so it
// must contend for the same lock — otherwise a saga retry restoring its
// reserved row and an operator creating a fresh Tier-1 tenant can both
// succeed and leave the tier at two occupants.
func TestSetupTenant_SingleMode_ConcurrentCreateAndRestore(t *testing.T) {
	db, cleanup := newSetupTenantTestDB(t)
	defer cleanup()
	repo, svc, _, binder := newSetupTenantService(db)
	svc.SetProvisioningModeResolver(singleModeResolver)
	ctx := context.Background()

	suffix := randSuffix(t)
	reservedUUID := "setup-" + suffix
	ownerUUID := "owner-" + suffix
	name := "Setup Tenant " + suffix
	slug := "setup-tenant-" + suffix

	// Produce a genuine rollback row: the first attempt fails at bindOwner,
	// so createTenantWithUUID unwinds and stamps provisioning_rollback.
	binder.failFirst = 1
	if err := svc.EnsureSetupTenant(ctx, reservedUUID, ownerUUID, name, slug, true); err == nil {
		t.Fatal("seeding EnsureSetupTenant with a failing bindOwner = nil, want an error")
	}
	rolled, err := repo.GetTenantByUUIDIncludingDeleted(ctx, reservedUUID)
	if err != nil {
		t.Fatalf("GetTenantByUUIDIncludingDeleted: %v", err)
	}
	if rolled.DeletedAt == nil || rolled.DeletedReason != models.TenantDeleteReasonProvisioningRollback {
		t.Fatalf("seed row is not a rollback candidate: deletedAt=%v reason=%q", rolled.DeletedAt, rolled.DeletedReason)
	}
	if n, cerr := repo.CountProvisioningSlotsByKind(ctx, models.TenantKindInternal); cerr != nil || n != 0 {
		t.Fatalf("tier holds %d occupants (err=%v) before the race, want 0", n, cerr)
	}

	var (
		wg          sync.WaitGroup
		start       = make(chan struct{})
		restoreErr  error
		createErr   error
		createOwner = "other-owner-" + suffix
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		restoreErr = svc.EnsureSetupTenant(ctx, reservedUUID, ownerUUID, name, slug, true)
	}()
	go func() {
		defer wg.Done()
		<-start
		_, createErr = svc.CreateTenant(ctx, createOwner, models.CreateTenantInput{
			Name: "Rival " + suffix,
			Slug: "rival-" + suffix,
			Kind: models.TenantKindInternal,
		})
	}()
	close(start)
	wg.Wait()

	for what, err := range map[string]error{"restore": restoreErr, "create": createErr} {
		if err != nil && !errors.Is(err, ErrProvisioningLocked) {
			t.Errorf("%s: unexpected error %v (want nil or ErrProvisioningLocked)", what, err)
		}
	}
	if restoreErr == nil && createErr == nil {
		t.Error("both the restore and the rival creation succeeded: `single` mode was breached")
	}
	if restoreErr != nil && createErr != nil {
		t.Errorf("neither side succeeded (restore=%v create=%v): the loser's refusal is correct, but one had to win", restoreErr, createErr)
	}

	n, err := repo.CountProvisioningSlotsByKind(ctx, models.TenantKindInternal)
	if err != nil {
		t.Fatalf("CountProvisioningSlotsByKind: %v", err)
	}
	if n != 1 {
		t.Fatalf("tier holds %d provisioning-slot occupants after the race, want exactly 1", n)
	}
}

// TestCreateTenant_SingleMode_SlugConflictStillMaps guards a regression the
// guard could easily have introduced: moving the insert inside a
// transaction must not change how a duplicate slug surfaces.
// tenantWriteError matches on the driver's E11000 shape, so the error has
// to reach it unwrapped through session.WithTransaction — otherwise a slug
// clash would degrade from 409 tenant.slug_already_in_use to an opaque 500.
func TestCreateTenant_SingleMode_SlugConflictStillMaps(t *testing.T) {
	db, cleanup := newSetupTenantTestDB(t)
	defer cleanup()
	_, svc, _, _ := newSetupTenantService(db)
	ctx := context.Background()
	suffix := randSuffix(t)

	// An external tenant takes the slug without occupying an INTERNAL
	// provisioning slot, so the internal tier is still empty and the
	// guarded insert is what hits the unique index.
	svc.SetProvisioningModeResolver(func(_ context.Context, k models.TenantKind) string {
		if k == models.TenantKindInternal {
			return models.ProvisioningModeSingle
		}
		return models.ProvisioningModeManual
	})
	if _, err := svc.CreateTenant(ctx, "ext-owner-"+suffix, models.CreateTenantInput{
		Name: "Ext " + suffix,
		Slug: "clash-" + suffix,
		Kind: models.TenantKindExternal,
	}); err != nil {
		t.Fatalf("seed external tenant: %v", err)
	}

	_, err := svc.CreateTenant(ctx, "int-owner-"+suffix, models.CreateTenantInput{
		Name: "Int " + suffix,
		Slug: "clash-" + suffix,
		Kind: models.TenantKindInternal,
	})
	if !errors.Is(err, ErrSlugAlreadyInUse) {
		t.Fatalf("guarded single-mode create with a taken slug = %v, want ErrSlugAlreadyInUse", err)
	}
}

// TestCreateTenant_ManualMode_ConcurrentCreatesAllSucceed is the boundary:
// the guard must constrain `single` only. `manual` has no cardinality rule,
// so concurrent creations all land — and keep taking the plain, non
// transactional insert path, which is what lets ordinary tenant creation
// work on a standalone mongod.
func TestCreateTenant_ManualMode_ConcurrentCreatesAllSucceed(t *testing.T) {
	db, cleanup := newSetupTenantTestDB(t)
	defer cleanup()
	repo, svc, _, _ := newSetupTenantService(db)
	svc.SetProvisioningModeResolver(func(context.Context, models.TenantKind) string {
		return models.ProvisioningModeManual
	})
	ctx := context.Background()

	const racers = 4
	suffix := randSuffix(t)
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make([]error, racers)
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, errs[i] = svc.CreateTenant(ctx, fmt.Sprintf("owner-%s-%d", suffix, i), models.CreateTenantInput{
				Name: fmt.Sprintf("Manual %s %d", suffix, i),
				Slug: fmt.Sprintf("manual-%s-%d", suffix, i),
				Kind: models.TenantKindInternal,
			})
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("racer %d: %v (manual mode imposes no cardinality limit)", i, err)
		}
	}
	ctxTimeout, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	n, err := repo.CountProvisioningSlotsByKind(ctxTimeout, models.TenantKindInternal)
	if err != nil {
		t.Fatalf("CountProvisioningSlotsByKind: %v", err)
	}
	if n != racers {
		t.Fatalf("tier holds %d occupants, want %d", n, racers)
	}
}
