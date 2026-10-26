package repository

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra/backend/internal/core/compliance/models"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// --- Mongo integration tests ----------------------------------------------
//
// Every test below skips when MONGO_TEST_URI isn't set so `go test ./...`
// without the env var sees a clean PASS [skipped]. Set
// MONGO_TEST_URI=mongodb://admin:<pw>@localhost:27017 to run them.

const mongoTestURIEnv = "MONGO_TEST_URI"

func newTestDB(t *testing.T) (*mongo.Database, func()) {
	t.Helper()
	uri := os.Getenv(mongoTestURIEnv)
	if uri == "" {
		t.Skipf("skipping integration test: set %s to run (e.g. mongodb://admin:<pw>@localhost:27017)", mongoTestURIEnv)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("mongo connect: %v", err)
	}
	if err := client.Ping(ctx, nil); err != nil {
		t.Fatalf("mongo ping: %v", err)
	}
	suffix := make([]byte, 4)
	_, _ = rand.Read(suffix)
	db := client.Database("orkestra_test_compliance_" + hex.EncodeToString(suffix))
	cleanup := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = db.Drop(ctx)
		_ = client.Disconnect(ctx)
	}
	return db, cleanup
}

// --- audit events ----------------------------------------------------------

func TestAuditInsertAndListFilters(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()
	r := New(db)
	ctx := context.Background()

	now := time.Now().UTC()
	events := []*models.AuditEvent{
		{UUID: "e-old", TenantID: "t-1", Action: "auth.login.succeeded", Outcome: "success", Timestamp: now.Add(-2 * time.Hour)},
		{UUID: "e-mid", TenantID: "t-1", Action: "auth.login.failed", Outcome: "failure", Timestamp: now.Add(-1 * time.Hour)},
		{UUID: "e-new", TenantID: "t-2", Action: "tenant.updated", Outcome: "success", Timestamp: now},
	}
	for _, e := range events {
		if err := r.Insert(ctx, e); err != nil {
			t.Fatalf("insert %s: %v", e.UUID, err)
		}
	}

	// Tenant filter.
	items, total, err := r.List(ctx, Filter{TenantID: "t-1"})
	if err != nil {
		t.Fatalf("list by tenant: %v", err)
	}
	if total != 2 || len(items) != 2 {
		t.Fatalf("tenant filter: total=%d items=%d, want 2/2", total, len(items))
	}

	// Action-prefix filter matches the auth.* family.
	items, total, err = r.List(ctx, Filter{ActionPrefix: "auth."})
	if err != nil {
		t.Fatalf("list by prefix: %v", err)
	}
	if total != 2 {
		t.Fatalf("prefix filter total=%d, want 2", total)
	}

	// Newest-first ordering.
	items, _, err = r.List(ctx, Filter{})
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(items) != 3 || items[0].UUID != "e-new" || items[2].UUID != "e-old" {
		t.Fatalf("expected newest-first order, got %v", []string{items[0].UUID, items[1].UUID, items[2].UUID})
	}

	// Exact-action filter beats prefix.
	_, total, err = r.List(ctx, Filter{Action: "tenant.updated"})
	if err != nil {
		t.Fatalf("list by action: %v", err)
	}
	if total != 1 {
		t.Fatalf("action filter total=%d, want 1", total)
	}
}

// --- legal holds -----------------------------------------------------------

func TestLegalHoldLifecycle(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()
	r := NewLegalHoldRepo(db)
	ctx := context.Background()

	hold := &models.LegalHold{UUID: "h-1", UserUUID: "u-1", Reason: "litigation", Active: true, PlacedAt: time.Now().UTC()}
	if err := r.Insert(ctx, hold); err != nil {
		t.Fatalf("insert: %v", err)
	}

	held, err := r.IsHeld(ctx, "u-1")
	if err != nil || !held {
		t.Fatalf("IsHeld after place = %v, %v; want true, nil", held, err)
	}

	active, err := r.ListActive(ctx, "u-1")
	if err != nil || len(active) != 1 {
		t.Fatalf("ListActive = %d holds, %v; want 1", len(active), err)
	}

	if err := r.Release(ctx, "h-1", "admin-1", "case closed"); err != nil {
		t.Fatalf("release: %v", err)
	}
	held, err = r.IsHeld(ctx, "u-1")
	if err != nil || held {
		t.Fatalf("IsHeld after release = %v, %v; want false, nil", held, err)
	}

	// Releasing again finds nothing active.
	if err := r.Release(ctx, "h-1", "admin-1", "again"); err != ErrLegalHoldNotFound {
		t.Fatalf("double release err = %v; want ErrLegalHoldNotFound", err)
	}
}

// --- erasure requests ------------------------------------------------------

func TestErasureRequestLifecycle(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()
	r := NewErasureRequestRepo(db)
	ctx := context.Background()

	req := &models.ErasureRequest{UUID: "r-1", UserUUID: "u-1", Status: models.ErasureRequestPending, RequestedAt: time.Now().UTC()}
	if err := r.Insert(ctx, req); err != nil {
		t.Fatalf("insert: %v", err)
	}

	got, err := r.Get(ctx, "r-1")
	if err != nil || got.UserUUID != "u-1" {
		t.Fatalf("Get = %+v, %v", got, err)
	}

	pending, err := r.ListByStatus(ctx, models.ErasureRequestPending)
	if err != nil || len(pending) != 1 {
		t.Fatalf("ListByStatus pending = %d, %v; want 1", len(pending), err)
	}

	if err := r.Resolve(ctx, "r-1", models.ErasureRequestCompleted, "admin-1", "hard_delete", ""); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	// A resolved request is no longer pending → re-resolve misses.
	if err := r.Resolve(ctx, "r-1", models.ErasureRequestRejected, "admin-1", "", "x"); err != ErrErasureRequestNotFound {
		t.Fatalf("re-resolve err = %v; want ErrErasureRequestNotFound", err)
	}

	missing, err := r.Get(ctx, "nope")
	if err != ErrErasureRequestNotFound || missing != nil {
		t.Fatalf("Get missing = %+v, %v; want nil, ErrErasureRequestNotFound", missing, err)
	}
}

func tp(t time.Time) *time.Time { return &t }

func insertErasure(t *testing.T, r *ErasureRequestRepository, req models.ErasureRequest) {
	t.Helper()
	if err := r.Insert(context.Background(), &req); err != nil {
		t.Fatalf("insert %s: %v", req.UUID, err)
	}
}

func TestErasureListOpenNewestFirst(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()
	r := NewErasureRequestRepo(db)
	ctx := context.Background()
	t0 := time.Now().UTC().Add(-24 * time.Hour)
	insertErasure(t, r, models.ErasureRequest{UUID: "r-old", UserUUID: "u-1", Status: models.ErasureRequestPending, RequestedAt: t0})
	insertErasure(t, r, models.ErasureRequest{UUID: "r-new", UserUUID: "u-2", Status: models.ErasureRequestIncomplete, RequestedAt: t0.Add(time.Hour)})
	insertErasure(t, r, models.ErasureRequest{UUID: "r-done", UserUUID: "u-3", Status: models.ErasureRequestCompleted, RequestedAt: t0.Add(2 * time.Hour)})

	got, err := r.ListByStatus(ctx, models.ErasureRequestPending, models.ErasureRequestIncomplete)
	if err != nil {
		t.Fatalf("ListByStatus: %v", err)
	}
	if len(got) != 2 || got[0].UUID != "r-new" || got[1].UUID != "r-old" {
		t.Fatalf("ListByStatus = %+v; want [r-new r-old]", got)
	}
	all, err := r.ListByStatus(ctx)
	if err != nil || len(all) != 3 || all[0].UUID != "r-done" {
		t.Fatalf("ListByStatus() = %+v, %v; want 3 rows newest first", all, err)
	}
}

func TestErasureClaimFromPendingSetsModeAndLease(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()
	r := NewErasureRequestRepo(db)
	ctx := context.Background()
	now := time.Now().UTC()
	insertErasure(t, r, models.ErasureRequest{UUID: "r-1", UserUUID: "u-1", Status: models.ErasureRequestPending, RequestedAt: now})

	lease := now.Add(5 * time.Minute)
	got, err := r.Claim(ctx, "r-1", models.ErasureRequestPending, "anonymize", "admin-1", now, lease)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if got.Status != models.ErasureRequestIncomplete || got.Mode != "anonymize" || got.ResolvedBy != "admin-1" {
		t.Fatalf("claimed = %+v", got)
	}
	if got.LeaseUntil == nil || !got.LeaseUntil.Equal(lease.Truncate(time.Millisecond)) {
		t.Fatalf("LeaseUntil = %v; want %v", got.LeaseUntil, lease.Truncate(time.Millisecond))
	}
	if got.NextAttemptAt == nil || !got.NextAttemptAt.Equal(*got.LeaseUntil) {
		t.Fatalf("NextAttemptAt = %v; want the lease %v", got.NextAttemptAt, got.LeaseUntil)
	}
	if _, err := r.Claim(ctx, "r-1", models.ErasureRequestIncomplete, "", "admin-2", now, lease); !errors.Is(err, ErrErasureRequestBusy) {
		t.Fatalf("second Claim err = %v; want ErrErasureRequestBusy", err)
	}
	if _, err := r.Claim(ctx, "r-1", models.ErasureRequestPending, "hard_delete", "admin-2", now, lease); !errors.Is(err, ErrErasureRequestNotFound) {
		t.Fatalf("Claim pending on incomplete err = %v; want ErrErasureRequestNotFound", err)
	}
}

func TestErasureClaimDueSkipsLiveLeaseAndFutureAttempts(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()
	r := NewErasureRequestRepo(db)
	ctx := context.Background()
	now := time.Now().UTC()
	inc := models.ErasureRequestIncomplete
	insertErasure(t, r, models.ErasureRequest{UUID: "a", UserUUID: "u", Status: inc, RequestedAt: now, NextAttemptAt: tp(now.Add(-time.Minute))})
	insertErasure(t, r, models.ErasureRequest{UUID: "b", UserUUID: "u", Status: inc, RequestedAt: now, NextAttemptAt: tp(now.Add(-2 * time.Minute)), LeaseUntil: tp(now.Add(time.Minute))})
	insertErasure(t, r, models.ErasureRequest{UUID: "c", UserUUID: "u", Status: inc, RequestedAt: now, NextAttemptAt: tp(now.Add(time.Minute))})
	insertErasure(t, r, models.ErasureRequest{UUID: "d", UserUUID: "u", Status: inc, RequestedAt: now})

	lease := now.Add(5 * time.Minute)
	got, err := r.ClaimDue(ctx, now, lease)
	if err != nil || got == nil || got.UUID != "a" {
		t.Fatalf("ClaimDue 1 = %+v, %v; want a", got, err)
	}
	got, err = r.ClaimDue(ctx, now, lease)
	if err != nil || got != nil {
		t.Fatalf("ClaimDue 2 = %+v, %v; want nil", got, err)
	}
	later := now.Add(2 * time.Minute)
	got, err = r.ClaimDue(ctx, later, later.Add(5*time.Minute))
	if err != nil || got == nil || got.UUID != "b" {
		t.Fatalf("ClaimDue after lease expiry = %+v, %v; want b", got, err)
	}
}

func TestErasureSettleIsFencedByLease(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()
	r := NewErasureRequestRepo(db)
	ctx := context.Background()
	now := time.Now().UTC()
	insertErasure(t, r, models.ErasureRequest{UUID: "r-1", UserUUID: "u-1", Status: models.ErasureRequestPending, RequestedAt: now})

	l1 := now.Add(5 * time.Minute).Truncate(time.Millisecond)
	if _, err := r.Claim(ctx, "r-1", models.ErasureRequestPending, "anonymize", "admin-1", now, l1); err != nil {
		t.Fatalf("Claim: %v", err)
	}
	later := l1.Add(time.Second)
	l2 := later.Add(5 * time.Minute).Truncate(time.Millisecond)
	taken, err := r.ClaimDue(ctx, later, l2)
	if err != nil || taken == nil {
		t.Fatalf("ClaimDue = %+v, %v", taken, err)
	}

	done := ErasureSettlement{Status: models.ErasureRequestCompleted, ResolvedAt: &later}
	if err := r.Settle(ctx, "r-1", l1, done); !errors.Is(err, ErrErasureLeaseLost) {
		t.Fatalf("Settle with stale lease err = %v; want ErrErasureLeaseLost", err)
	}
	row, err := r.Get(ctx, "r-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if row.Status != models.ErasureRequestIncomplete || row.LeaseUntil == nil {
		t.Fatalf("row changed by a lost lease: %+v", row)
	}
	if err := r.Settle(ctx, "r-1", l2, done); err != nil {
		t.Fatalf("Settle: %v", err)
	}
	row, err = r.Get(ctx, "r-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if row.Status != models.ErasureRequestCompleted || row.LeaseUntil != nil || row.ResolvedAt == nil {
		t.Fatalf("settled row = %+v", row)
	}
}

func TestErasureHasIncomplete(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()
	r := NewErasureRequestRepo(db)
	ctx := context.Background()
	now := time.Now().UTC()
	insertErasure(t, r, models.ErasureRequest{UUID: "r-1", UserUUID: "u-1", Status: models.ErasureRequestIncomplete, RequestedAt: now})
	insertErasure(t, r, models.ErasureRequest{UUID: "r-2", UserUUID: "u-2", Status: models.ErasureRequestCompleted, RequestedAt: now})
	if ok, err := r.HasIncomplete(ctx, "u-1"); err != nil || !ok {
		t.Fatalf("HasIncomplete u-1 = %v, %v; want true", ok, err)
	}
	if ok, err := r.HasIncomplete(ctx, "u-2"); err != nil || ok {
		t.Fatalf("HasIncomplete u-2 = %v, %v; want false", ok, err)
	}
}

func TestErasureSettleIncompleteThenCompleted(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()
	r := NewErasureRequestRepo(db)
	ctx := context.Background()
	now := time.Now().UTC()
	insertErasure(t, r, models.ErasureRequest{UUID: "r-1", UserUUID: "u-1", Status: models.ErasureRequestPending, RequestedAt: now})
	lease := now.Add(5 * time.Minute)
	if _, err := r.Claim(ctx, "r-1", models.ErasureRequestPending, "anonymize", "admin-1", now, lease); err != nil {
		t.Fatalf("Claim: %v", err)
	}
	next := now.Add(time.Minute)
	prog := []models.ErasureProducerProgress{{Key: "auth", Subject: "auth", Outcome: models.ProducerOutcomePending, RowsDeleted: 3, Attempts: 1, UpdatedAt: now}}
	err := r.Settle(ctx, "r-1", lease, ErasureSettlement{Status: models.ErasureRequestIncomplete, Progress: prog, Attempts: 1, NextAttemptAt: &next, LastError: models.ErasureLastErrorRun})
	if err != nil {
		t.Fatalf("Settle incomplete: %v", err)
	}
	row, err := r.Get(ctx, "r-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if row.Status != models.ErasureRequestIncomplete || len(row.Progress) != 1 || row.Progress[0].RowsDeleted != 3 || row.Attempts != 1 ||
		row.NextAttemptAt == nil || !row.NextAttemptAt.Equal(next.Truncate(time.Millisecond)) || row.LastError != models.ErasureLastErrorRun || row.LeaseUntil != nil {
		t.Fatalf("incomplete row = %+v", row)
	}

	l2 := next.Add(5 * time.Minute)
	if got, err := r.ClaimDue(ctx, next.Add(time.Second), l2); err != nil || got == nil {
		t.Fatalf("ClaimDue = %+v, %v", got, err)
	}
	if err := r.Settle(ctx, "r-1", l2, ErasureSettlement{Status: models.ErasureRequestCompleted, Progress: prog, Attempts: 2, ResolvedAt: &next}); err != nil {
		t.Fatalf("Settle completed: %v", err)
	}
	row, err = r.Get(ctx, "r-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if row.Status != models.ErasureRequestCompleted || row.NextAttemptAt != nil || row.LastError != "" || row.ResolvedAt == nil || row.LeaseUntil != nil {
		t.Fatalf("completed row = %+v", row)
	}
}

func TestErasureClaimFromIncompleteKeepsModeAndResets(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()
	r := NewErasureRequestRepo(db)
	ctx := context.Background()
	now := time.Now().UTC()
	insertErasure(t, r, models.ErasureRequest{UUID: "r-1", UserUUID: "u-1", Status: models.ErasureRequestIncomplete, RequestedAt: now,
		Mode: "anonymize", ResolvedBy: "admin-1", Attempts: 20, LastError: models.ErasureLastErrorRun, LeaseUntil: tp(now.Add(-time.Minute))})
	got, err := r.Claim(ctx, "r-1", models.ErasureRequestIncomplete, "ignored", "admin-2", now, now.Add(5*time.Minute))
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if got.Mode != "anonymize" || got.ResolvedBy != "admin-1" || got.Attempts != 0 || got.LastError != "" || got.Status != models.ErasureRequestIncomplete {
		t.Fatalf("claimed = %+v", got)
	}
}

// --- kms keys --------------------------------------------------------------

func TestKMSKeyLifecycle(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()
	r := NewKMSKeyRepo(db)
	ctx := context.Background()

	key := &models.KMSKey{
		UUID:         uuid.NewString(),
		TenantUUID:   "t-1",
		Alias:        "tenant/t-1",
		EncryptedDEK: []byte{1, 2, 3},
		Nonce:        []byte{4, 5, 6},
		State:        models.KMSStateActive,
		CreatedAt:    time.Now().UTC(),
	}
	if err := r.Insert(ctx, key); err != nil {
		t.Fatalf("insert: %v", err)
	}

	byUUID, err := r.GetByUUID(ctx, key.UUID)
	if err != nil || byUUID.TenantUUID != "t-1" {
		t.Fatalf("GetByUUID = %+v, %v", byUUID, err)
	}
	byTenant, err := r.GetByTenant(ctx, "t-1")
	if err != nil || byTenant.UUID != key.UUID {
		t.Fatalf("GetByTenant = %+v, %v", byTenant, err)
	}

	if err := r.Shred(ctx, key.UUID); err != nil {
		t.Fatalf("shred: %v", err)
	}
	shredded, err := r.GetByUUID(ctx, key.UUID)
	if err != nil {
		t.Fatalf("get after shred: %v", err)
	}
	if shredded.State != models.KMSStatePendingDeletion || len(shredded.EncryptedDEK) != 0 {
		t.Fatalf("shred did not clear DEK / flip state: %+v", shredded)
	}

	if _, err := r.GetByTenant(ctx, "missing"); err != iface.ErrKMSKeyNotFound {
		t.Fatalf("GetByTenant missing err = %v; want ErrKMSKeyNotFound", err)
	}
}

func TestErasureClosePartialOnlyIncompleteAndLeaseFree(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()
	r := NewErasureRequestRepo(db)
	ctx := context.Background()
	now := time.Now().UTC()
	next := now.Add(time.Hour)
	insertErasure(t, r, models.ErasureRequest{UUID: "r-1", UserUUID: "u-1", Status: models.ErasureRequestIncomplete, RequestedAt: now, NextAttemptAt: &next})
	insertErasure(t, r, models.ErasureRequest{UUID: "r-2", UserUUID: "u-2", Status: models.ErasureRequestIncomplete, RequestedAt: now, LeaseUntil: tp(now.Add(time.Minute))})
	insertErasure(t, r, models.ErasureRequest{UUID: "r-3", UserUUID: "u-3", Status: models.ErasureRequestPending, RequestedAt: now})

	if err := r.ClosePartial(ctx, "r-1", now, []string{"gone"}, "admin-1", "module removed"); err != nil {
		t.Fatalf("ClosePartial: %v", err)
	}
	row, err := r.Get(ctx, "r-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if row.Status != models.ErasureRequestClosedPartial || len(row.Abandoned) != 1 || row.Abandoned[0] != "gone" ||
		row.ResolvedBy != "admin-1" || row.ResolutionNote != "module removed" || row.NextAttemptAt != nil || row.ResolvedAt == nil {
		t.Fatalf("closed row = %+v", row)
	}
	if err := r.ClosePartial(ctx, "r-2", now, []string{"gone"}, "admin-1", "n"); !errors.Is(err, ErrErasureRequestBusy) {
		t.Fatalf("leased err = %v; want ErrErasureRequestBusy", err)
	}
	if err := r.ClosePartial(ctx, "r-3", now, []string{"gone"}, "admin-1", "n"); !errors.Is(err, ErrErasureRequestNotFound) {
		t.Fatalf("pending err = %v; want ErrErasureRequestNotFound", err)
	}
	if err := r.ClosePartial(ctx, "r-1", now, []string{"gone"}, "admin-1", "n"); !errors.Is(err, ErrErasureRequestNotFound) {
		t.Fatalf("already closed err = %v; want ErrErasureRequestNotFound", err)
	}
}
