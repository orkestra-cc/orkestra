package systeminit

import (
	"context"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// liveFinalizationRepo connects to a live MongoDB (skipping unless
// MONGO_TEST_URI or MONGO_URI is set), creates a per-run randomly named
// database, and returns a Repo plus a cleanup that drops the database. Modeled
// on internal/core/auth/repository/refresh_token_repository_concurrency_test.go's
// liveRefreshRepository helper.
func liveFinalizationRepo(t *testing.T) (*Repo, func()) {
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
	db := client.Database("systeminit_race_" + uuid.NewString())
	repo, err := NewRepo(ctx, db)
	if err != nil {
		_ = client.Disconnect(context.Background())
		t.Fatalf("NewRepo: %v", err)
	}
	cleanup := func() {
		_ = db.Drop(context.Background())
		_ = client.Disconnect(context.Background())
	}
	return repo, cleanup
}

// resetFinalization clears the collection between race iterations so each
// iteration starts from a clean slate under the same fixed "key".
func resetFinalization(ctx context.Context, t *testing.T, repo *Repo) {
	t.Helper()
	//tenantscope:allow system: test-only reset of the isolated per-run test database between race iterations
	if _, err := repo.coll.DeleteMany(ctx, bson.M{}); err != nil {
		t.Fatalf("reset collection: %v", err)
	}
}

// TestReserveRequest_SingleWinner races two ReserveRequest calls with
// different request hashes for the same adminUUID. Exactly one must win;
// the loser's hash must not appear in the persisted record.
func TestReserveRequest_SingleWinner(t *testing.T) {
	repo, cleanup := liveFinalizationRepo(t)
	defer cleanup()
	ctx := context.Background()

	const iterations = 20
	for i := 0; i < iterations; i++ {
		resetFinalization(ctx, t, repo)
		adminUUID := "admin-" + strconv.Itoa(i)
		if err := repo.InitializeFresh(ctx, adminUUID); err != nil {
			t.Fatalf("iteration %d InitializeFresh: %v", i, err)
		}

		hashA := "hash-a-" + strconv.Itoa(i)
		hashB := "hash-b-" + strconv.Itoa(i)

		start := make(chan struct{})
		var wg sync.WaitGroup
		results := make([]bool, 2)
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			ok, err := repo.ReserveRequest(ctx, adminUUID, "tenant-a-uuid", "Tenant A", "tenant-a", "manual", hashA)
			if err != nil {
				t.Errorf("iteration %d ReserveRequest A: %v", i, err)
				return
			}
			results[0] = ok
		}()
		go func() {
			defer wg.Done()
			<-start
			ok, err := repo.ReserveRequest(ctx, adminUUID, "tenant-b-uuid", "Tenant B", "tenant-b", "manual", hashB)
			if err != nil {
				t.Errorf("iteration %d ReserveRequest B: %v", i, err)
				return
			}
			results[1] = ok
		}()
		close(start)
		wg.Wait()

		if results[0] == results[1] {
			t.Fatalf("iteration %d: want exactly one reservation winner, got A=%v B=%v", i, results[0], results[1])
		}

		rec, err := repo.Get(ctx)
		if err != nil {
			t.Fatalf("iteration %d Get: %v", i, err)
		}
		if rec == nil {
			t.Fatalf("iteration %d: record missing after reservation race", i)
		}
		wantHash := hashA
		if results[1] {
			wantHash = hashB
		}
		if rec.RequestHash != wantHash {
			t.Fatalf("iteration %d: requestHash = %q, want winner's hash %q (loser must not have overwritten it)", i, rec.RequestHash, wantHash)
		}
	}
}

// TestClaimStage_OneLeaseWinner_ExpiredLeaseRecoverable races two ClaimStage
// calls for the same (hash, stage, revision): exactly one wins. It then
// backdates that winner's lease to simulate expiry and shows a fresh owner
// can reclaim the same (stage, revision), and that the original (now stale)
// owner can no longer AdvanceStage.
func TestClaimStage_OneLeaseWinner_ExpiredLeaseRecoverable(t *testing.T) {
	repo, cleanup := liveFinalizationRepo(t)
	defer cleanup()
	ctx := context.Background()

	const iterations = 20
	for i := 0; i < iterations; i++ {
		resetFinalization(ctx, t, repo)
		adminUUID := "admin-" + strconv.Itoa(i)
		hash := "hash-" + strconv.Itoa(i)
		if err := repo.InitializeFresh(ctx, adminUUID); err != nil {
			t.Fatalf("iteration %d InitializeFresh: %v", i, err)
		}
		if ok, err := repo.ReserveRequest(ctx, adminUUID, "tenant-uuid", "Tenant", "tenant", "manual", hash); err != nil || !ok {
			t.Fatalf("iteration %d ReserveRequest: ok=%v err=%v", i, ok, err)
		}
		rec, err := repo.Get(ctx)
		if err != nil || rec == nil {
			t.Fatalf("iteration %d Get after reserve: %v", i, err)
		}
		revision := rec.Revision

		start := make(chan struct{})
		var wg sync.WaitGroup
		results := make([]bool, 2)
		leaseFuture := time.Now().Add(time.Minute)
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			ok, err := repo.ClaimStage(ctx, hash, StageConfig, revision, "owner-A", leaseFuture)
			if err != nil {
				t.Errorf("iteration %d ClaimStage A: %v", i, err)
				return
			}
			results[0] = ok
		}()
		go func() {
			defer wg.Done()
			<-start
			ok, err := repo.ClaimStage(ctx, hash, StageConfig, revision, "owner-B", leaseFuture)
			if err != nil {
				t.Errorf("iteration %d ClaimStage B: %v", i, err)
				return
			}
			results[1] = ok
		}()
		close(start)
		wg.Wait()

		if results[0] == results[1] {
			t.Fatalf("iteration %d: want exactly one stage-lease winner, got A=%v B=%v", i, results[0], results[1])
		}
		winner := "owner-A"
		if results[1] {
			winner = "owner-B"
		}

		// Backdate the winner's lease directly (same-package test access to
		// r.coll) to simulate a lease that expired without renewal.
		past := time.Now().Add(-time.Minute)
		//tenantscope:allow system: test-only backdate of a lease in the isolated per-run test database to simulate expiry
		if _, err := repo.coll.UpdateOne(ctx,
			bson.M{"key": keySetupFinalization},
			bson.M{"$set": bson.M{"leaseUntil": past}},
		); err != nil {
			t.Fatalf("iteration %d backdate lease: %v", i, err)
		}

		ok, err := repo.ClaimStage(ctx, hash, StageConfig, revision, "owner-C", time.Now().Add(time.Minute))
		if err != nil {
			t.Fatalf("iteration %d reclaim expired lease: %v", i, err)
		}
		if !ok {
			t.Fatalf("iteration %d: expected expired lease to be reclaimable by a new owner", i)
		}
		rec, err = repo.Get(ctx)
		if err != nil || rec == nil {
			t.Fatalf("iteration %d Get after reclaim: %v", i, err)
		}
		if rec.LeaseOwner != "owner-C" {
			t.Fatalf("iteration %d: LeaseOwner = %q, want persisted reclaim owner-C", i, rec.LeaseOwner)
		}

		ok, err = repo.AdvanceStage(ctx, winner, StageConfig, revision)
		if err != nil {
			t.Fatalf("iteration %d AdvanceStage by stale owner: %v", i, err)
		}
		if ok {
			t.Fatalf("iteration %d: stale owner %q must not be able to advance the stage after losing the lease", i, winner)
		}
	}
}

// TestAdvanceStage_IncrementsRevisionAndClearsLease claims stage 1 and
// advances it, then asserts the persisted record shows Stage+1, Revision+1,
// no lease, and StageCompletedAt["1"] set.
func TestAdvanceStage_IncrementsRevisionAndClearsLease(t *testing.T) {
	repo, cleanup := liveFinalizationRepo(t)
	defer cleanup()
	ctx := context.Background()

	adminUUID := "admin-advance"
	hash := "hash-advance"
	if err := repo.InitializeFresh(ctx, adminUUID); err != nil {
		t.Fatalf("InitializeFresh: %v", err)
	}
	ok, err := repo.ReserveRequest(ctx, adminUUID, "tenant-uuid", "Tenant", "tenant", "manual", hash)
	if err != nil || !ok {
		t.Fatalf("ReserveRequest: ok=%v err=%v", ok, err)
	}
	rec, err := repo.Get(ctx)
	if err != nil || rec == nil {
		t.Fatalf("Get after reserve: %v", err)
	}
	revision := rec.Revision

	ok, err = repo.ClaimStage(ctx, hash, StageConfig, revision, "owner-1", time.Now().Add(time.Minute))
	if err != nil || !ok {
		t.Fatalf("ClaimStage: ok=%v err=%v", ok, err)
	}

	ok, err = repo.AdvanceStage(ctx, "owner-1", StageConfig, revision)
	if err != nil || !ok {
		t.Fatalf("AdvanceStage: ok=%v err=%v", ok, err)
	}

	rec, err = repo.Get(ctx)
	if err != nil || rec == nil {
		t.Fatalf("Get after advance: %v", err)
	}
	if rec.Stage != StageConfig+1 {
		t.Fatalf("Stage = %d, want %d", rec.Stage, StageConfig+1)
	}
	if rec.Revision != revision+1 {
		t.Fatalf("Revision = %d, want %d", rec.Revision, revision+1)
	}
	if rec.LeaseOwner != "" {
		t.Fatalf("LeaseOwner = %q, want cleared", rec.LeaseOwner)
	}
	if rec.LeaseUntil != nil {
		t.Fatalf("LeaseUntil = %v, want cleared", rec.LeaseUntil)
	}
	completedAt, ok2 := rec.StageCompletedAt[strconv.Itoa(StageConfig)]
	if !ok2 || completedAt.IsZero() {
		t.Fatalf("StageCompletedAt[%q] missing or zero: %v", strconv.Itoa(StageConfig), rec.StageCompletedAt)
	}
}

// TestComplete_WritesResultSnapshot advances through stages 1-3, claims
// stage 4 (StageFinish), and calls Complete — asserting Stage=StageDone,
// CompletedAt set, and Result equal to what was passed in.
func TestComplete_WritesResultSnapshot(t *testing.T) {
	repo, cleanup := liveFinalizationRepo(t)
	defer cleanup()
	ctx := context.Background()

	adminUUID := "admin-complete"
	hash := "hash-complete"
	if err := repo.InitializeFresh(ctx, adminUUID); err != nil {
		t.Fatalf("InitializeFresh: %v", err)
	}
	ok, err := repo.ReserveRequest(ctx, adminUUID, "tenant-uuid", "Tenant", "tenant", "manual", hash)
	if err != nil || !ok {
		t.Fatalf("ReserveRequest: ok=%v err=%v", ok, err)
	}

	owner := "owner-complete"
	for stage := StageConfig; stage <= StageFinish; stage++ {
		rec, err := repo.Get(ctx)
		if err != nil || rec == nil {
			t.Fatalf("Get before stage %d: %v", stage, err)
		}
		if rec.Stage != stage {
			t.Fatalf("expected stage %d before claim, got %d", stage, rec.Stage)
		}
		ok, err = repo.ClaimStage(ctx, hash, stage, rec.Revision, owner, time.Now().Add(time.Minute))
		if err != nil || !ok {
			t.Fatalf("ClaimStage stage %d: ok=%v err=%v", stage, ok, err)
		}
		if stage == StageFinish {
			break // leave StageFinish claimed-but-not-advanced; Complete does that
		}
		ok, err = repo.AdvanceStage(ctx, owner, stage, rec.Revision)
		if err != nil || !ok {
			t.Fatalf("AdvanceStage stage %d: ok=%v err=%v", stage, ok, err)
		}
	}

	rec, err := repo.Get(ctx)
	if err != nil || rec == nil {
		t.Fatalf("Get before complete: %v", err)
	}
	if rec.Stage != StageFinish {
		t.Fatalf("expected StageFinish(%d) before Complete, got %d", StageFinish, rec.Stage)
	}

	result := FinalizationResult{
		TenantUUID: "tenant-uuid", TenantName: "Tenant", TenantSlug: "tenant", Mode: "manual",
	}
	ok, err = repo.Complete(ctx, owner, rec.Revision, result)
	if err != nil || !ok {
		t.Fatalf("Complete: ok=%v err=%v", ok, err)
	}

	rec, err = repo.Get(ctx)
	if err != nil || rec == nil {
		t.Fatalf("Get after complete: %v", err)
	}
	if rec.Stage != StageDone {
		t.Fatalf("Stage = %d, want StageDone(%d)", rec.Stage, StageDone)
	}
	if rec.CompletedAt == nil {
		t.Fatalf("CompletedAt not set")
	}
	if rec.Result == nil || *rec.Result != result {
		t.Fatalf("Result = %+v, want %+v", rec.Result, result)
	}
}

// TestClaimRecovery_ObservedUUIDAndRevisionCAS races two ClaimRecovery calls
// observing the same (adminUUID, revision): exactly one must win.
func TestClaimRecovery_ObservedUUIDAndRevisionCAS(t *testing.T) {
	repo, cleanup := liveFinalizationRepo(t)
	defer cleanup()
	ctx := context.Background()

	const iterations = 20
	for i := 0; i < iterations; i++ {
		resetFinalization(ctx, t, repo)
		origAdmin := "orig-admin-" + strconv.Itoa(i)
		if err := repo.InitializeFresh(ctx, origAdmin); err != nil {
			t.Fatalf("iteration %d InitializeFresh: %v", i, err)
		}
		rec, err := repo.Get(ctx)
		if err != nil || rec == nil {
			t.Fatalf("iteration %d Get: %v", i, err)
		}

		start := make(chan struct{})
		var wg sync.WaitGroup
		results := make([]bool, 2)
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			ok, err := repo.ClaimRecovery(ctx, origAdmin, rec.Revision, "new-admin-x-"+strconv.Itoa(i))
			if err != nil {
				t.Errorf("iteration %d ClaimRecovery X: %v", i, err)
				return
			}
			results[0] = ok
		}()
		go func() {
			defer wg.Done()
			<-start
			ok, err := repo.ClaimRecovery(ctx, origAdmin, rec.Revision, "new-admin-y-"+strconv.Itoa(i))
			if err != nil {
				t.Errorf("iteration %d ClaimRecovery Y: %v", i, err)
				return
			}
			results[1] = ok
		}()
		close(start)
		wg.Wait()

		if results[0] == results[1] {
			t.Fatalf("iteration %d: want exactly one recovery winner, got X=%v Y=%v", i, results[0], results[1])
		}
		wantAdmin := "new-admin-x-" + strconv.Itoa(i)
		if results[1] {
			wantAdmin = "new-admin-y-" + strconv.Itoa(i)
		}
		after, err := repo.Get(ctx)
		if err != nil || after == nil {
			t.Fatalf("iteration %d Get after recovery race: %v", i, err)
		}
		if after.AdminUUID != wantAdmin {
			t.Fatalf("iteration %d: AdminUUID = %q, want winner's %q (loser must not have overwritten it)", i, after.AdminUUID, wantAdmin)
		}
		if after.Revision != rec.Revision+1 {
			t.Fatalf("iteration %d: Revision = %d, want %d (exactly one increment)", i, after.Revision, rec.Revision+1)
		}
	}
}

// TestFirstAdminRelease_CannotDeleteFinalizationRecord pins the load-bearing
// invariant that first_admin and setup_finalization have independent
// lifecycles: releasing the first-admin sentinel must never remove the
// persistent setup_finalization record.
func TestFirstAdminRelease_CannotDeleteFinalizationRecord(t *testing.T) {
	repo, cleanup := liveFinalizationRepo(t)
	defer cleanup()
	ctx := context.Background()

	adminUUID := "admin-release-guard"
	if err := repo.InitializeFresh(ctx, adminUUID); err != nil {
		t.Fatalf("InitializeFresh: %v", err)
	}
	claimed, err := repo.ClaimFirstAdmin(ctx, adminUUID)
	if err != nil {
		t.Fatalf("ClaimFirstAdmin: %v", err)
	}
	if !claimed {
		t.Fatalf("ClaimFirstAdmin: expected claim on empty first_admin sentinel")
	}

	if err := repo.Release(ctx, adminUUID); err != nil {
		t.Fatalf("Release: %v", err)
	}

	//tenantscope:allow system: test-only assertion counting documents in the isolated per-run test database
	firstAdminCount, err := repo.coll.CountDocuments(ctx, bson.M{"key": keyFirstAdmin})
	if err != nil {
		t.Fatalf("CountDocuments first_admin: %v", err)
	}
	if firstAdminCount != 0 {
		t.Fatalf("first_admin sentinel survived Release: count=%d", firstAdminCount)
	}

	rec, err := repo.Get(ctx)
	if err != nil {
		t.Fatalf("Get setup_finalization: %v", err)
	}
	if rec == nil {
		t.Fatalf("setup_finalization record was deleted by first-admin Release — the two lifecycles must stay separate")
	}
	if rec.AdminUUID != adminUUID {
		t.Fatalf("AdminUUID = %q, want %q", rec.AdminUUID, adminUUID)
	}
}

// TestInitializeFresh_DoesNotClobber shows InitializeFresh's $setOnInsert
// never overwrites an existing record created by EnsureRecord.
func TestInitializeFresh_DoesNotClobber(t *testing.T) {
	repo, cleanup := liveFinalizationRepo(t)
	defer cleanup()
	ctx := context.Background()

	rec, err := repo.EnsureRecord(ctx, SourceLegacyRecovery, nil)
	if err != nil {
		t.Fatalf("EnsureRecord: %v", err)
	}
	if rec == nil || rec.Source != SourceLegacyRecovery || rec.AdminUUID != "" {
		t.Fatalf("EnsureRecord result = %+v, want source=%q adminUUID=empty", rec, SourceLegacyRecovery)
	}

	if err := repo.InitializeFresh(ctx, "u1"); err != nil {
		t.Fatalf("InitializeFresh: %v", err)
	}

	rec, err = repo.Get(ctx)
	if err != nil || rec == nil {
		t.Fatalf("Get after InitializeFresh: %v", err)
	}
	if rec.Source != SourceLegacyRecovery {
		t.Fatalf("Source = %q, want unchanged %q (InitializeFresh must not clobber)", rec.Source, SourceLegacyRecovery)
	}
	if rec.AdminUUID != "" {
		t.Fatalf("AdminUUID = %q, want to stay empty (InitializeFresh must not clobber)", rec.AdminUUID)
	}
}

// TestReconcileLease_VersionedElection races two ClaimReconcileLease(1, ...)
// calls: exactly one wins. FinishReconcile(1, winner) then advances
// ReconciliationVersion to 1, and a subsequent ClaimReconcileLease(1, ...)
// must fail because the target version is no longer ahead of the record.
func TestReconcileLease_VersionedElection(t *testing.T) {
	repo, cleanup := liveFinalizationRepo(t)
	defer cleanup()
	ctx := context.Background()

	const iterations = 20
	for i := 0; i < iterations; i++ {
		resetFinalization(ctx, t, repo)
		adminUUID := "admin-reconcile-" + strconv.Itoa(i)
		// Consumer contract: a record must exist before ClaimReconcileLease
		// is ever called (boot reconciliation calls EnsureRecord first).
		if err := repo.InitializeFresh(ctx, adminUUID); err != nil {
			t.Fatalf("iteration %d InitializeFresh: %v", i, err)
		}

		start := make(chan struct{})
		var wg sync.WaitGroup
		results := make([]bool, 2)
		leaseFuture := time.Now().Add(time.Minute)
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			ok, err := repo.ClaimReconcileLease(ctx, 1, "reconciler-A", leaseFuture)
			if err != nil {
				t.Errorf("iteration %d ClaimReconcileLease A: %v", i, err)
				return
			}
			results[0] = ok
		}()
		go func() {
			defer wg.Done()
			<-start
			ok, err := repo.ClaimReconcileLease(ctx, 1, "reconciler-B", leaseFuture)
			if err != nil {
				t.Errorf("iteration %d ClaimReconcileLease B: %v", i, err)
				return
			}
			results[1] = ok
		}()
		close(start)
		wg.Wait()

		if results[0] == results[1] {
			t.Fatalf("iteration %d: want exactly one reconcile-lease winner, got A=%v B=%v", i, results[0], results[1])
		}
		winner := "reconciler-A"
		if results[1] {
			winner = "reconciler-B"
		}

		ok, err := repo.FinishReconcile(ctx, 1, winner)
		if err != nil || !ok {
			t.Fatalf("iteration %d FinishReconcile: ok=%v err=%v", i, ok, err)
		}

		rec, err := repo.Get(ctx)
		if err != nil || rec == nil {
			t.Fatalf("iteration %d Get after finish: %v", i, err)
		}
		if rec.ReconciliationVersion != 1 {
			t.Fatalf("iteration %d: ReconciliationVersion = %d, want 1", i, rec.ReconciliationVersion)
		}
		if rec.ReconcileLeaseOwner != "" || rec.ReconcileLeaseUntil != nil {
			t.Fatalf("iteration %d: reconcile lease not cleared: owner=%q until=%v", i, rec.ReconcileLeaseOwner, rec.ReconcileLeaseUntil)
		}

		ok, err = repo.ClaimReconcileLease(ctx, 1, "reconciler-C", leaseFuture)
		if err != nil {
			t.Fatalf("iteration %d ClaimReconcileLease after finish: %v", i, err)
		}
		if ok {
			t.Fatalf("iteration %d: ClaimReconcileLease(1, ...) must fail once reconciliationVersion already reached 1", i)
		}
	}
}

// TestRenewLease_UnchangedWriteStillReportsOwnership pins the one CAS in
// this file whose write can legitimately change nothing.
//
// MongoDB reports modifiedCount=0 for a matched document whose $set stores
// byte-identical values, so judging "do I still hold the lease?" by
// modifiedCount answers "no" to an executor that demonstrably does hold it.
// RenewLease is the only method here exposed to that: its $set is a pure
// refresh (leaseUntil + updatedAt), and setup's saga renews inside the same
// millisecond in which it claimed whenever the stage body and the
// intervening round trip fit in one — the common case on a fast host.
// Every other CAS here either $incs revision or writes a value its own
// filter proves is different.
//
// The construction manufactures that collision rather than waiting for it:
// the record is seeded in exactly the state ClaimStage leaves behind, with
// updatedAt pinned to a millisecond that has not arrived yet, and
// RenewLease is called at the start of that millisecond with the leaseUntil
// already stored. The assertion runs only once the record proves the write
// really did change nothing.
func TestRenewLease_UnchangedWriteStillReportsOwnership(t *testing.T) {
	repo, cleanup := liveFinalizationRepo(t)
	defer cleanup()
	ctx := context.Background()

	const owner = "executor-1"
	// Millisecond-aligned: the driver stores BSON datetimes at millisecond
	// resolution, and the comparison that decides modifiedCount is made on
	// the stored values.
	leaseUntil := time.UnixMilli(time.Now().UTC().Add(30 * time.Second).UnixMilli()).UTC()

	for attempt := 0; attempt < 64; attempt++ {
		resetFinalization(ctx, t, repo)

		// Far enough ahead that the insert's round trip lands before it.
		target := time.UnixMilli(time.Now().UTC().UnixMilli() + 12).UTC()
		//tenantscope:allow system: test-only seeding of the isolated per-run test database
		if _, err := repo.coll.InsertOne(ctx, bson.M{
			"key": keySetupFinalization, "adminUUID": "admin-1", "source": SourceFresh,
			"requestHash": "hash-1", "stage": StageConfig, "revision": int64(2),
			"reconciliationVersion": 0,
			"leaseOwner":            owner, "leaseUntil": leaseUntil,
			"createdAt": target, "updatedAt": target,
		}); err != nil {
			t.Fatalf("seed record: %v", err)
		}
		if time.Now().UTC().UnixMilli() >= target.UnixMilli() {
			continue // the round trip overshot the window; take a new target
		}
		for time.Now().UTC().UnixMilli() < target.UnixMilli() {
			// Spin: sleeping would overshoot a one-millisecond window.
		}

		ok, err := repo.RenewLease(ctx, owner, leaseUntil)
		if err != nil {
			t.Fatalf("RenewLease: %v", err)
		}
		rec, err := repo.Get(ctx)
		if err != nil || rec == nil {
			t.Fatalf("Get after renew: %v", err)
		}
		if rec.UpdatedAt.UnixMilli() != target.UnixMilli() {
			continue // updatedAt moved: this call did change the document
		}

		// The document is byte-identical to what it held before the call
		// and leaseOwner still names us: the lease was never lost.
		if rec.LeaseOwner != owner {
			t.Fatalf("seeded lease owner changed to %q", rec.LeaseOwner)
		}
		if !ok {
			t.Fatal("RenewLease reported a lost lease for a write that changed nothing — the caller still holds it")
		}
		return
	}
	t.Fatal("could not construct an unchanged RenewLease write in 64 attempts")
}
