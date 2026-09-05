package setup

// Task 5.5: the finalization saga against a REAL Mongo-backed
// systeminit.Repo. finalize_test.go proves the decision logic with an
// in-memory store that imitates the CAS semantics; this file proves those
// semantics are the ones MongoDB actually gives us — the lease, the
// revision CAS, and the reservation filter — under genuine concurrency.
//
// Gated on MONGO_TEST_URI (or MONGO_URI); skipped otherwise. Run with:
//
//	MONGO_TEST_URI='mongodb://localhost:28017/?directConnection=true' \
//	  go test ./internal/shared/setup/... -run Integration -v

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/orkestra/backend/internal/shared/systeminit"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// liveFinalizationStore connects to a live MongoDB, creates a per-run
// randomly named database, and returns a real *systeminit.Repo plus a
// cleanup that drops it. Modeled on systeminit's own
// liveFinalizationRepo helper.
func liveFinalizationStore(t *testing.T) (*systeminit.Repo, func()) {
	t.Helper()
	uri := os.Getenv("MONGO_TEST_URI")
	if uri == "" {
		uri = os.Getenv("MONGO_URI")
	}
	if uri == "" {
		t.Skip("set MONGO_TEST_URI or MONGO_URI to run live setup-finalization tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("mongo.Connect: %v", err)
	}
	if err := client.Ping(ctx, nil); err != nil {
		_ = client.Disconnect(context.Background())
		t.Fatalf("mongo.Ping: %v", err)
	}
	db := client.Database("setup_finalize_" + uuid.NewString())
	repo, err := systeminit.NewRepo(ctx, db)
	if err != nil {
		_ = client.Disconnect(context.Background())
		t.Fatalf("systeminit.NewRepo: %v", err)
	}
	return repo, func() {
		_ = db.Drop(context.Background())
		_ = client.Disconnect(context.Background())
	}
}

// blockingTenants is fakeTenants with an opt-in hook that parks the FIRST
// executor inside stage 2 — the only reliable way to observe "somebody
// else holds the stage lease" without racing the scheduler. Blocking is
// off unless a test sets blocking before the saga starts; the lease-expiry
// test needs the stage to run straight through.
type blockingTenants struct {
	fakeTenants
	blocking bool
	entered  chan struct{}
	release  chan struct{}
	once     sync.Once
}

func (b *blockingTenants) EnsureSetupTenant(ctx context.Context, tenantUUID, ownerUUID, name, slug string, attested bool) error {
	if b.blocking {
		b.once.Do(func() {
			close(b.entered)
			<-b.release
		})
	}
	return b.fakeTenants.EnsureSetupTenant(ctx, tenantUUID, ownerUUID, name, slug, attested)
}

type liveFixture struct {
	svc     *Service
	store   *systeminit.Repo
	tenants *blockingTenants
	cfg     *fakeModuleConfig
	audit   *fakeAudit
	users   *fakeLifecycleUsers
	log     *callLog
}

func newLiveFixture(t *testing.T, store *systeminit.Repo, admins ...string) liveFixture {
	t.Helper()
	log := &callLog{}
	states := map[string]iface.UserLifecycleState{}
	roles := map[string]string{}
	for _, a := range admins {
		states[a] = iface.UserLifecycleActive
		// The recovery gate reads the caller's role from the database
		// (D28); seeded here rather than at the call sites because two
		// of these tests call Finalize from concurrent goroutines.
		roles[a] = roleSuperAdmin
	}
	tenants := &blockingTenants{
		fakeTenants: fakeTenants{log: log},
		entered:     make(chan struct{}),
		release:     make(chan struct{}),
	}
	cfg := &fakeModuleConfig{log: log}
	audit := &fakeAudit{}
	users := &fakeLifecycleUsers{count: 1, states: states, roles: roles}
	svc := NewService(users, &stubAdmin{}, store, cfg, tenants, audit, discardLogger())
	return liveFixture{svc: svc, store: store, tenants: tenants, cfg: cfg, audit: audit, users: users, log: log}
}

// TestFinalizeIntegration_ConcurrentIdenticalConvergesOnOneWinner races two
// identical finalizations. One executor holds the stage-2 lease; the other
// must observe it through Mongo and answer the typed 202 in-progress
// outcome without executing a stage. Exactly one tenant is ensured, one
// default assigned, one setup.completed audited.
func TestFinalizeIntegration_ConcurrentIdenticalConvergesOnOneWinner(t *testing.T) {
	store, cleanup := liveFinalizationStore(t)
	defer cleanup()
	ctx := context.Background()

	if err := store.InitializeFresh(ctx, "admin-1"); err != nil {
		t.Fatalf("InitializeFresh: %v", err)
	}
	fx := newLiveFixture(t, store, "admin-1")
	fx.tenants.blocking = true

	var (
		winner  *FinalizeResult
		winErr  error
		wgDone  sync.WaitGroup
		payload = testInput(true)
	)
	wgDone.Add(1)
	go func() {
		defer wgDone.Done()
		winner, winErr = fx.svc.Finalize(ctx, "admin-1", payload)
	}()

	// The winner is now parked inside stage 2 holding a live lease.
	select {
	case <-fx.tenants.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("winner never reached the tenant stage")
	}

	if _, err := fx.svc.Finalize(ctx, "admin-1", payload); !errors.Is(err, ErrFinalizationInProgress) {
		close(fx.tenants.release)
		wgDone.Wait()
		t.Fatalf("concurrent identical finalization = %v, want ErrFinalizationInProgress", err)
	}

	close(fx.tenants.release)
	wgDone.Wait()

	if winErr != nil {
		t.Fatalf("winner: %v", winErr)
	}
	if winner == nil || winner.TenantUUID == "" {
		t.Fatalf("winner produced no result: %+v", winner)
	}
	if n := len(fx.tenants.ensureArgs); n != 1 {
		t.Errorf("EnsureSetupTenant ran %d times, want 1", n)
	}
	if n := len(fx.tenants.assignArgs); n != 1 {
		t.Errorf("AssignDefaultTenant ran %d times, want 1", n)
	}
	if n := len(fx.audit.byAction(auditActionSetupCompleted)); n != 1 {
		t.Errorf("setup.completed emitted %d times, want exactly 1", n)
	}

	rec, err := store.Get(ctx)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if rec.CompletedAt == nil || rec.Result == nil {
		t.Fatalf("record not completed: %+v", rec)
	}
	if rec.Result.TenantUUID != winner.TenantUUID {
		t.Errorf("persisted tenant %q != winner %q", rec.Result.TenantUUID, winner.TenantUUID)
	}
	if rec.LeaseOwner != "" || rec.LeaseUntil != nil {
		t.Errorf("lease not released: owner=%q until=%v", rec.LeaseOwner, rec.LeaseUntil)
	}

	// The 202 loser can now replay and receive the persisted snapshot.
	replay, err := fx.svc.Finalize(ctx, "admin-1", payload)
	if err != nil {
		t.Fatalf("post-completion replay: %v", err)
	}
	if replay.TenantUUID != winner.TenantUUID {
		t.Errorf("replay tenant %q != winner %q", replay.TenantUUID, winner.TenantUUID)
	}
	if n := len(fx.tenants.ensureArgs); n != 1 {
		t.Errorf("replay executed a stage: EnsureSetupTenant ran %d times", n)
	}
}

// TestFinalizeIntegration_ConcurrentDifferentPayloadsOneStableConflict
// proves one reservation wins and the other payload gets a stable
// conflict — never a second reservation and never a side effect.
func TestFinalizeIntegration_ConcurrentDifferentPayloadsOneStableConflict(t *testing.T) {
	store, cleanup := liveFinalizationStore(t)
	defer cleanup()
	ctx := context.Background()

	if err := store.InitializeFresh(ctx, "admin-1"); err != nil {
		t.Fatalf("InitializeFresh: %v", err)
	}
	fx := newLiveFixture(t, store, "admin-1")
	fx.tenants.blocking = true

	reserved := testInput(true) // manual
	other := testInput(false)   // single — a DIFFERENT normalized request
	var (
		winErr error
		wgDone sync.WaitGroup
	)
	wgDone.Add(1)
	go func() {
		defer wgDone.Done()
		_, winErr = fx.svc.Finalize(ctx, "admin-1", reserved)
	}()

	select {
	case <-fx.tenants.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("winner never reached the tenant stage")
	}

	// While the first request is mid-saga, the different payload must be
	// refused as already-started — not reserved a second time.
	if _, err := fx.svc.Finalize(ctx, "admin-1", other); !errors.Is(err, ErrFinalizationAlreadyStarted) {
		close(fx.tenants.release)
		wgDone.Wait()
		t.Fatalf("different payload mid-saga = %v, want ErrFinalizationAlreadyStarted", err)
	}

	close(fx.tenants.release)
	wgDone.Wait()
	if winErr != nil {
		t.Fatalf("winner: %v", winErr)
	}

	rec, err := store.Get(ctx)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if rec.Mode != modeManual {
		t.Errorf("persisted mode = %q, want the winner's manual", rec.Mode)
	}
	if _, _, _, wantHash := normalizeFinalize(reserved.TenantName, reserved.TenantSlug, true); rec.RequestHash != wantHash {
		t.Errorf("persisted hash is not the winner's")
	}

	// Stability: the losing payload keeps getting the same conflict, twice
	// in a row, now that setup is complete.
	for i := 0; i < 2; i++ {
		if _, err := fx.svc.Finalize(ctx, "admin-1", other); !errors.Is(err, ErrFinalizationAlreadyCompleted) {
			t.Fatalf("attempt %d with the losing payload = %v, want ErrFinalizationAlreadyCompleted", i+1, err)
		}
	}
	if n := len(fx.tenants.ensureArgs); n != 1 {
		t.Errorf("EnsureSetupTenant ran %d times, want 1 — the losing payload must never reach a side effect", n)
	}
}

// TestFinalizeIntegration_ExpiredLeaseResumesSameStageSameRevision
// simulates a crashed executor: it claims a stage and dies holding the
// lease. While the lease is live no other request may execute; once it
// expires the next request resumes THE SAME stage at THE SAME revision,
// and the dead owner can no longer advance or overwrite it.
func TestFinalizeIntegration_ExpiredLeaseResumesSameStageSameRevision(t *testing.T) {
	store, cleanup := liveFinalizationStore(t)
	defer cleanup()
	ctx := context.Background()

	if err := store.InitializeFresh(ctx, "admin-1"); err != nil {
		t.Fatalf("InitializeFresh: %v", err)
	}
	fx := newLiveFixture(t, store, "admin-1")

	payload := testInput(true)
	name, slug, mode, hash := normalizeFinalize(payload.TenantName, payload.TenantSlug, payload.AllowAdditionalInternalTenants)
	reservedTenant := uuid.Must(uuid.NewV7()).String()
	ok, err := store.ReserveRequest(ctx, "admin-1", reservedTenant, name, slug, mode, hash)
	if err != nil || !ok {
		t.Fatalf("ReserveRequest ok=%v err=%v", ok, err)
	}

	before, err := store.Get(ctx)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	crashedRevision := before.Revision
	crashedStage := before.Stage

	// The doomed executor claims stage 1 with a deliberately short lease
	// and then "crashes" — nothing advances the stage.
	claimed, err := store.ClaimStage(ctx, hash, crashedStage, crashedRevision, "crashed-executor", time.Now().UTC().Add(250*time.Millisecond))
	if err != nil || !claimed {
		t.Fatalf("ClaimStage ok=%v err=%v", claimed, err)
	}

	// While the lease is live, a new request must not execute anything.
	if _, err := fx.svc.Finalize(ctx, "admin-1", payload); !errors.Is(err, ErrFinalizationInProgress) {
		t.Fatalf("request under a live foreign lease = %v, want ErrFinalizationInProgress", err)
	}
	if entries := fx.log.only("config.", "tenants."); len(entries) != 0 {
		t.Fatalf("a stage executed under a foreign live lease: %v", entries)
	}
	mid, err := store.Get(ctx)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if mid.Stage != crashedStage || mid.Revision != crashedRevision {
		t.Fatalf("record moved while the foreign lease was live: stage=%d revision=%d", mid.Stage, mid.Revision)
	}

	// Wait the lease out, then resume.
	time.Sleep(400 * time.Millisecond)

	res, err := fx.svc.Finalize(ctx, "admin-1", payload)
	if err != nil {
		t.Fatalf("resume after lease expiry: %v", err)
	}
	if res.TenantUUID != reservedTenant {
		t.Errorf("resumed on tenant %q, want the reserved %q", res.TenantUUID, reservedTenant)
	}
	// It resumed at the crashed stage — stage 1's effect ran — rather than
	// skipping it because "somebody held the lease once".
	if n := len(fx.cfg.updates); n != 1 {
		t.Errorf("config stage ran %d times, want 1 (the crashed stage must be redone)", n)
	}
	final, err := store.Get(ctx)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if final.CompletedAt == nil {
		t.Fatalf("saga did not complete after resume: %+v", final)
	}
	if _, done := final.StageCompletedAt["1"]; !done {
		t.Errorf("stage 1 has no completion stamp: %v", final.StageCompletedAt)
	}

	// The dead owner is stale for good: it can neither advance the stage
	// it once held nor complete the saga.
	if advanced, err := store.AdvanceStage(ctx, "crashed-executor", crashedStage, crashedRevision); err != nil || advanced {
		t.Errorf("stale owner advanced the stage: ok=%v err=%v", advanced, err)
	}
	if completed, err := store.Complete(ctx, "crashed-executor", crashedRevision, systeminit.FinalizationResult{}); err != nil || completed {
		t.Errorf("stale owner completed the saga: ok=%v err=%v", completed, err)
	}
	after, err := store.Get(ctx)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if after.Result == nil || after.Result.TenantUUID != reservedTenant {
		t.Errorf("stale owner overwrote the result snapshot: %+v", after.Result)
	}
}
