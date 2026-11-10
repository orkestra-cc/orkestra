package repository

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra/backend/internal/core/compliance/models"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"go.mongodb.org/mongo-driver/bson"
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

	held, err := r.IsHeld(ctx, "u-1", time.Now())
	if err != nil || !held {
		t.Fatalf("IsHeld after place = %v, %v; want true, nil", held, err)
	}

	active, err := r.ListActive(ctx, "u-1", time.Now())
	if err != nil || len(active) != 1 {
		t.Fatalf("ListActive = %d holds, %v; want 1", len(active), err)
	}

	if err := r.Release(ctx, "h-1", "admin-1", "case closed", time.Now()); err != nil {
		t.Fatalf("release: %v", err)
	}
	held, err = r.IsHeld(ctx, "u-1", time.Now())
	if err != nil || held {
		t.Fatalf("IsHeld after release = %v, %v; want false, nil", held, err)
	}

	// Releasing again finds nothing active.
	if err := r.Release(ctx, "h-1", "admin-1", "again", time.Now()); err != ErrLegalHoldNotFound {
		t.Fatalf("double release err = %v; want ErrLegalHoldNotFound", err)
	}
}

func TestLegalHoldExpiryAwareReads(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()
	r := NewLegalHoldRepo(db)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	placed := now.Add(-48 * time.Hour)
	mustInsert := func(h *models.LegalHold) {
		t.Helper()
		h.Active, h.PlacedAt = true, placed
		if err := r.Insert(ctx, h); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}
	mustInsert(&models.LegalHold{UUID: "past", UserUUID: "u-past", Reason: "r", ExpiresAt: tp(now.Add(-time.Minute))})
	mustInsert(&models.LegalHold{UUID: "future", UserUUID: "u-future", Reason: "r", ExpiresAt: tp(now.Add(time.Hour))})
	mustInsert(&models.LegalHold{UUID: "never", UserUUID: "u-never", Reason: "r"})

	for u, want := range map[string]bool{"u-past": false, "u-future": true, "u-never": true} {
		got, err := r.IsHeld(ctx, u, now)
		if err != nil || got != want {
			t.Fatalf("IsHeld(%s) = %v, %v; want %v", u, got, err, want)
		}
	}
	// Expired but not yet released: still active in the row, absent from ListActive.
	active, err := r.ListActive(ctx, "", now)
	if err != nil || len(active) != 2 {
		t.Fatalf("ListActive = %d, %v; want 2 (expired hold excluded)", len(active), err)
	}
	all, err := r.List(ctx, "", true, now)
	if err != nil || len(all) != 3 {
		t.Fatalf("List includeReleased = %d, %v; want 3", len(all), err)
	}
	inForce, err := r.List(ctx, "", false, now)
	if err != nil || len(inForce) != 2 {
		t.Fatalf("List in force = %d, %v; want 2", len(inForce), err)
	}
}

func TestLegalHoldExpireDueCAS(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()
	r := NewLegalHoldRepo(db)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	const due = 40
	for i := 0; i < due; i++ {
		id := "due-" + uuid.NewString()
		if err := r.Insert(ctx, &models.LegalHold{UUID: id, UserUUID: id, Reason: "r", Active: true,
			PlacedAt: now.Add(-time.Hour), ExpiresAt: tp(now.Add(-time.Minute))}); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Insert(ctx, &models.LegalHold{UUID: "keep", UserUUID: "keep", Reason: "r", Active: true,
		PlacedAt: now.Add(-time.Hour), ExpiresAt: tp(now.Add(time.Hour))}); err != nil {
		t.Fatal(err)
	}

	// Two replicas at once: every hold is released by exactly one of them.
	var wg sync.WaitGroup
	results := make([][]models.LegalHold, 2)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rel, err := r.ExpireDue(ctx, now, 500)
			if err != nil {
				t.Errorf("ExpireDue: %v", err)
			}
			results[i] = rel
		}(i)
	}
	wg.Wait()
	seen := map[string]int{}
	for _, rel := range results {
		for _, h := range rel {
			seen[h.UUID]++
			if h.Active || h.ReleasedBy != models.LegalHoldExpiryActor || h.ReleasedAt == nil {
				t.Fatalf("returned row not the released state: %+v", h)
			}
		}
	}
	if len(seen) != due {
		t.Fatalf("released %d distinct holds, want %d", len(seen), due)
	}
	for id, n := range seen {
		if n != 1 {
			t.Fatalf("hold %s released by %d callers", id, n)
		}
	}

	again, err := r.ExpireDue(ctx, now, 500)
	if err != nil || len(again) != 0 {
		t.Fatalf("second ExpireDue = %d, %v; want 0", len(again), err)
	}
	if held, _ := r.IsHeld(ctx, "keep", now); !held {
		t.Fatal("a hold with a future expiry must stay")
	}

	// The batch bound.
	for i := 0; i < 3; i++ {
		id := "b-" + uuid.NewString()
		_ = r.Insert(ctx, &models.LegalHold{UUID: id, UserUUID: id, Reason: "r", Active: true, PlacedAt: now, ExpiresAt: tp(now.Add(-time.Second))})
	}
	if got, _ := r.ExpireDue(ctx, now, 2); len(got) != 2 {
		t.Fatalf("batch of 2 released %d", len(got))
	}
}

func TestLegalHoldUpdateAndNotes(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()
	r := NewLegalHoldRepo(db)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	if err := r.Insert(ctx, &models.LegalHold{UUID: "h", UserUUID: "u", Reason: "r", Active: true, PlacedAt: now,
		ExpiresAt: tp(now.Add(time.Hour))}); err != nil {
		t.Fatal(err)
	}
	h, err := r.Update(ctx, "h", bson.M{"reviewAt": now.Add(time.Minute), "updatedAt": now}, []string{"expiresAt"}, now)
	if err != nil || h.ReviewAt == nil || h.ExpiresAt != nil || h.UpdatedAt == nil {
		t.Fatalf("Update = %+v, %v", h, err)
	}
	for _, txt := range []string{"one", "two"} {
		if _, err := r.AddNote(ctx, "h", models.LegalHoldNote{ID: uuid.NewString(), By: "op", At: now, Text: txt}, now); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := r.Get(ctx, "h")
	if len(got.Notes) != 2 || got.Notes[0].Text != "one" || got.Notes[1].Text != "two" {
		t.Fatalf("notes = %+v", got.Notes)
	}
	if err := r.Release(ctx, "h", "op", "done", now); err != nil {
		t.Fatal(err)
	}
	if _, err := r.AddNote(ctx, "h", models.LegalHoldNote{ID: "x", Text: "late"}, now); err != ErrLegalHoldNotFound {
		t.Fatalf("note on released = %v", err)
	}
	if _, err := r.Update(ctx, "h", bson.M{"updatedAt": now}, nil, now); err != ErrLegalHoldNotFound {
		t.Fatalf("update on released = %v", err)
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

const maxAttempts = 20 // services.ErasureRetryMaxAttempts

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
	got, err := r.Claim(ctx, "r-1", models.ErasureRequestPending, "anonymize", "admin-1", now, lease, nil)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if got.Status != models.ErasureRequestIncomplete || got.Mode != "anonymize" || got.ResolvedBy != "admin-1" || got.Attempts != 1 {
		t.Fatalf("claimed = %+v; want incomplete, anonymize, admin-1, attempts 1", got)
	}
	if got.LeaseUntil == nil || !got.LeaseUntil.Equal(lease.Truncate(time.Millisecond)) {
		t.Fatalf("LeaseUntil = %v; want %v", got.LeaseUntil, lease.Truncate(time.Millisecond))
	}
	if got.NextAttemptAt == nil || !got.NextAttemptAt.Equal(*got.LeaseUntil) {
		t.Fatalf("NextAttemptAt = %v; want the lease %v", got.NextAttemptAt, got.LeaseUntil)
	}
	if _, err := r.Claim(ctx, "r-1", models.ErasureRequestIncomplete, "", "admin-2", now, lease, nil); !errors.Is(err, ErrErasureRequestBusy) {
		t.Fatalf("second Claim err = %v; want ErrErasureRequestBusy", err)
	}
	if _, err := r.Claim(ctx, "r-1", models.ErasureRequestPending, "hard_delete", "admin-2", now, lease, nil); !errors.Is(err, ErrErasureRequestNotFound) {
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
	got, err := r.ClaimDue(ctx, now, lease, maxAttempts)
	if err != nil || got == nil || got.UUID != "a" {
		t.Fatalf("ClaimDue 1 = %+v, %v; want a", got, err)
	}
	got, err = r.ClaimDue(ctx, now, lease, maxAttempts)
	if err != nil || got != nil {
		t.Fatalf("ClaimDue 2 = %+v, %v; want nil", got, err)
	}
	later := now.Add(2 * time.Minute)
	got, err = r.ClaimDue(ctx, later, later.Add(5*time.Minute), maxAttempts)
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
	if _, err := r.Claim(ctx, "r-1", models.ErasureRequestPending, "anonymize", "admin-1", now, l1, nil); err != nil {
		t.Fatalf("Claim: %v", err)
	}
	later := l1.Add(time.Second)
	l2 := later.Add(5 * time.Minute).Truncate(time.Millisecond)
	taken, err := r.ClaimDue(ctx, later, l2, maxAttempts)
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
	if _, err := r.Claim(ctx, "r-1", models.ErasureRequestPending, "anonymize", "admin-1", now, lease, nil); err != nil {
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
	if got, err := r.ClaimDue(ctx, next.Add(time.Second), l2, maxAttempts); err != nil || got == nil {
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
	got, err := r.Claim(ctx, "r-1", models.ErasureRequestIncomplete, "ignored", "admin-2", now, now.Add(5*time.Minute), nil)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if got.Mode != "anonymize" || got.ResolvedBy != "admin-1" || got.Attempts != 1 || got.LastError != "" || got.Status != models.ErasureRequestIncomplete {
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

	if err := r.ClosePartial(ctx, "r-1", now, nil, []string{"gone"}, "admin-1", "module removed"); err != nil {
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
	if err := r.ClosePartial(ctx, "r-2", now, nil, []string{"gone"}, "admin-1", "n"); !errors.Is(err, ErrErasureRequestBusy) {
		t.Fatalf("leased err = %v; want ErrErasureRequestBusy", err)
	}
	if err := r.ClosePartial(ctx, "r-3", now, nil, []string{"gone"}, "admin-1", "n"); !errors.Is(err, ErrErasureRequestNotFound) {
		t.Fatalf("pending err = %v; want ErrErasureRequestNotFound", err)
	}
	if err := r.ClosePartial(ctx, "r-1", now, nil, []string{"gone"}, "admin-1", "n"); !errors.Is(err, ErrErasureRequestNotFound) {
		t.Fatalf("already closed err = %v; want ErrErasureRequestNotFound", err)
	}
}

func TestErasureClaimDueCountsAttemptsAndStopsAtMax(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()
	r := NewErasureRequestRepo(db)
	ctx := context.Background()
	now := time.Now().UTC()
	insertErasure(t, r, models.ErasureRequest{UUID: "r-1", UserUUID: "u-1", Status: models.ErasureRequestIncomplete, RequestedAt: now, NextAttemptAt: tp(now.Add(-time.Second))})

	// A run that dies before Settle still spends its attempt: every claim counts.
	for i := 1; i <= maxAttempts; i++ {
		got, err := r.ClaimDue(ctx, now, now.Add(5*time.Minute), maxAttempts)
		if err != nil || got == nil || got.Attempts != i {
			t.Fatalf("ClaimDue %d = %+v, %v; want attempts %d", i, got, err, i)
		}
		now = now.Add(5*time.Minute + time.Second) // the lease expires without a Settle
	}
	got, err := r.ClaimDue(ctx, now, now.Add(5*time.Minute), maxAttempts)
	if err != nil || got != nil {
		t.Fatalf("ClaimDue after %d unsettled claims = %+v, %v; want nil", maxAttempts, got, err)
	}
	row, err := r.Get(ctx, "r-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if row.Status != models.ErasureRequestIncomplete || row.Attempts != maxAttempts {
		t.Fatalf("exhausted row = %+v", row)
	}
}

func TestErasureClosePartialReleasesLeaseAndRefusesStaleSettle(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()
	r := NewErasureRequestRepo(db)
	ctx := context.Background()
	now := time.Now().UTC()
	insertErasure(t, r, models.ErasureRequest{UUID: "r-1", UserUUID: "u-1", Status: models.ErasureRequestPending, RequestedAt: now})
	lease := now.Add(5 * time.Minute)
	if _, err := r.Claim(ctx, "r-1", models.ErasureRequestPending, "anonymize", "admin-1", now, lease, nil); err != nil {
		t.Fatalf("Claim: %v", err)
	}
	later := lease.Add(time.Second) // the run died; its lease expired
	if err := r.ClosePartial(ctx, "r-1", later, nil, []string{"gone"}, "admin-1", "module removed"); err != nil {
		t.Fatalf("ClosePartial: %v", err)
	}
	row, err := r.Get(ctx, "r-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if row.Status != models.ErasureRequestClosedPartial || row.LeaseUntil != nil {
		t.Fatalf("closed row = %+v; want closed_partial without a lease", row)
	}
	stale := ErasureSettlement{Status: models.ErasureRequestCompleted, Attempts: 1, ResolvedAt: &later}
	if err := r.Settle(ctx, "r-1", lease, stale); !errors.Is(err, ErrErasureLeaseLost) {
		t.Fatalf("stale Settle err = %v; want ErrErasureLeaseLost", err)
	}
	row, err = r.Get(ctx, "r-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if row.Status != models.ErasureRequestClosedPartial {
		t.Fatalf("a stale Settle reopened the row: %+v", row)
	}
}

func TestErasureRecordProgressIsFencedByLeaseAndKeepsIt(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()
	r := NewErasureRequestRepo(db)
	ctx := context.Background()
	now := time.Now().UTC()
	insertErasure(t, r, models.ErasureRequest{UUID: "r-1", UserUUID: "u-1", Status: models.ErasureRequestPending, RequestedAt: now})
	plan := []models.ErasureProducerProgress{
		{Key: "user", Subject: "user", Outcome: models.ProducerOutcomePending},
		{Key: "crm", Subject: "crm", Outcome: models.ProducerOutcomePending},
	}

	l1 := now.Add(5 * time.Minute).Truncate(time.Millisecond)
	if err := r.RecordProgress(ctx, "r-1", l1, plan); !errors.Is(err, ErrErasureLeaseLost) {
		t.Fatalf("RecordProgress on a pending row err = %v; want ErrErasureLeaseLost", err)
	}
	if _, err := r.Claim(ctx, "r-1", models.ErasureRequestPending, "anonymize", "admin-1", now, l1, nil); err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if err := r.RecordProgress(ctx, "r-1", l1.Add(time.Second), plan); !errors.Is(err, ErrErasureLeaseLost) {
		t.Fatalf("RecordProgress with another lease err = %v; want ErrErasureLeaseLost", err)
	}
	if err := r.RecordProgress(ctx, "r-1", l1, plan); err != nil {
		t.Fatalf("RecordProgress: %v", err)
	}
	row, err := r.Get(ctx, "r-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if row.Status != models.ErasureRequestIncomplete || row.LeaseUntil == nil || !row.LeaseUntil.Equal(l1) ||
		len(row.Progress) != 2 || row.Progress[1].Key != "crm" || row.Progress[1].Outcome != models.ProducerOutcomePending {
		t.Fatalf("row = %+v; want both producers pending under the same lease", row)
	}
}

// TestErasureInsertOneActiveRetentionRequestPerSubject runs the partial unique
// index the module declares (models.ActiveRetentionErasurePartialFilter) on a
// real MongoDB: a second incomplete retention request for the same subject is
// refused with ErrErasureRequestActive, other sources and finished ones are not.
func TestErasureInsertOneActiveRetentionRequestPerSubject(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	if _, err := db.Collection(models.ErasureRequestsCollection).Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "userUuid", Value: 1}},
		Options: options.Index().SetUnique(true).SetPartialFilterExpression(models.ActiveRetentionErasurePartialFilter()),
	}); err != nil {
		t.Fatalf("create index: %v", err)
	}
	r := NewErasureRequestRepo(db)
	now := time.Now().UTC()
	retention := func(id string) *models.ErasureRequest {
		return &models.ErasureRequest{UUID: id, UserUUID: "u-1", Status: models.ErasureRequestIncomplete, Source: models.ErasureSourceRetention, RequestedAt: now}
	}
	if err := r.Insert(ctx, retention("a")); err != nil {
		t.Fatalf("Insert a: %v", err)
	}
	if err := r.Insert(ctx, retention("b")); !errors.Is(err, ErrErasureRequestActive) {
		t.Fatalf("second retention Insert err = %v; want ErrErasureRequestActive", err)
	}
	other := &models.ErasureRequest{UUID: "c", UserUUID: "u-1", Status: models.ErasureRequestIncomplete, Source: models.ErasureSourceSelfService, RequestedAt: now}
	if err := r.Insert(ctx, other); err != nil {
		t.Fatalf("self-service Insert next to a retention request: %v", err)
	}
	lease := now.Add(5 * time.Minute).Truncate(time.Millisecond)
	if _, err := r.Claim(ctx, "a", models.ErasureRequestIncomplete, "", "", now, lease, nil); err != nil {
		t.Fatalf("Claim a: %v", err)
	}
	if err := r.Settle(ctx, "a", lease, ErasureSettlement{Status: models.ErasureRequestCompleted, ResolvedAt: &now}); err != nil {
		t.Fatalf("Settle a: %v", err)
	}
	if err := r.Insert(ctx, retention("d")); err != nil {
		t.Fatalf("retention Insert after the first completed: %v", err)
	}
}

// TestErasureClosePartialIsFencedByProgress: the close is written only while
// the progress is still the one the caller checked. A run that settled in
// between (changed progress, lease released) makes it fail as busy, leaving
// the run's state; with the current progress it closes.
func TestErasureClosePartialIsFencedByProgress(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()
	r := NewErasureRequestRepo(db)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	checked := []models.ErasureProducerProgress{
		{Key: "user", Subject: "user", Outcome: models.ProducerOutcomeDone, Attempts: 1, UpdatedAt: now},
		{Key: "gone", Subject: "gone", Outcome: models.ProducerOutcomeFailed, Attempts: 1, UpdatedAt: now},
	}
	insertErasure(t, r, models.ErasureRequest{UUID: "r-1", UserUUID: "u-1", Status: models.ErasureRequestIncomplete, RequestedAt: now, Progress: checked})
	seen, err := r.Get(ctx, "r-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	// A concurrent run claims, records a new producer and settles.
	lease := now.Add(5 * time.Minute).Truncate(time.Millisecond)
	if _, err := r.Claim(ctx, "r-1", models.ErasureRequestIncomplete, "", "", now, lease, nil); err != nil {
		t.Fatalf("Claim: %v", err)
	}
	moved := append(append([]models.ErasureProducerProgress(nil), checked...),
		models.ErasureProducerProgress{Key: "crm", Subject: "crm", Outcome: models.ProducerOutcomePending, Attempts: 1, UpdatedAt: now})
	next := now.Add(time.Minute)
	if err := r.Settle(ctx, "r-1", lease, ErasureSettlement{Status: models.ErasureRequestIncomplete, Progress: moved, Attempts: 1, NextAttemptAt: &next}); err != nil {
		t.Fatalf("Settle: %v", err)
	}

	if err := r.ClosePartial(ctx, "r-1", now, seen.Progress, []string{"gone"}, "admin-1", "n"); !errors.Is(err, ErrErasureRequestBusy) {
		t.Fatalf("close on stale progress err = %v; want ErrErasureRequestBusy", err)
	}
	row, err := r.Get(ctx, "r-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if row.Status != models.ErasureRequestIncomplete || len(row.Progress) != 3 || row.NextAttemptAt == nil {
		t.Fatalf("row = %+v; want the run's state, not closed", row)
	}
	if err := r.ClosePartial(ctx, "r-1", now, row.Progress, []string{"gone", "crm"}, "admin-1", "n"); err != nil {
		t.Fatalf("close on current progress: %v", err)
	}
	if row, _ := r.Get(ctx, "r-1"); row.Status != models.ErasureRequestClosedPartial {
		t.Fatalf("row = %+v; want closed_partial", row)
	}
}

// --- admin-opened requests and approvals -----------------------------------

func TestErasureAddApprovalAppendsOnceAndLocksMode(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()
	r := NewErasureRequestRepo(db)
	ctx := context.Background()
	now := time.Now().UTC()
	insertErasure(t, r, models.ErasureRequest{UUID: "r-1", UserUUID: "u-1", Status: models.ErasureRequestPending, RequestedAt: now, RequiredApprovals: 2})

	got, err := r.AddApproval(ctx, "r-1", "anonymize", models.ErasureApproval{By: "op-a", At: now, Note: "ok"})
	if err != nil {
		t.Fatalf("AddApproval: %v", err)
	}
	if got.Mode != "anonymize" || len(got.Approvals) != 1 || got.Approvals[0].By != "op-a" {
		t.Fatalf("row = %+v; want mode anonymize and one approval by op-a", got)
	}
	if _, err := r.AddApproval(ctx, "r-1", "anonymize", models.ErasureApproval{By: "op-a", At: now}); !errors.Is(err, ErrErasureApprovalConflict) {
		t.Fatalf("same operator twice err = %v; want ErrErasureApprovalConflict", err)
	}
	if _, err := r.AddApproval(ctx, "r-1", "hard_delete", models.ErasureApproval{By: "op-b", At: now}); !errors.Is(err, ErrErasureApprovalConflict) {
		t.Fatalf("different mode err = %v; want ErrErasureApprovalConflict", err)
	}
	if got, err := r.AddApproval(ctx, "r-1", "anonymize", models.ErasureApproval{By: "op-b", At: now}); err != nil || len(got.Approvals) != 2 {
		t.Fatalf("second operator = %+v, %v; want two approvals", got, err)
	}
	if _, err := r.AddApproval(ctx, "nope", "anonymize", models.ErasureApproval{By: "op-c", At: now}); !errors.Is(err, ErrErasureRequestNotFound) {
		t.Fatalf("missing row err = %v; want ErrErasureRequestNotFound", err)
	}
}

func TestErasureAddApprovalRefusesNonPending(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()
	r := NewErasureRequestRepo(db)
	ctx := context.Background()
	now := time.Now().UTC()
	insertErasure(t, r, models.ErasureRequest{UUID: "r-1", UserUUID: "u-1", Status: models.ErasureRequestIncomplete, RequestedAt: now})
	if _, err := r.AddApproval(ctx, "r-1", "anonymize", models.ErasureApproval{By: "op-a", At: now}); !errors.Is(err, ErrErasureApprovalConflict) {
		t.Fatalf("incomplete row err = %v; want ErrErasureApprovalConflict", err)
	}
}

func TestErasureAddApprovalConcurrentOperatorsBothLand(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()
	r := NewErasureRequestRepo(db)
	ctx := context.Background()
	now := time.Now().UTC()
	insertErasure(t, r, models.ErasureRequest{UUID: "r-1", UserUUID: "u-1", Status: models.ErasureRequestPending, RequestedAt: now, RequiredApprovals: 2})

	start := make(chan struct{})
	errs := make(chan error, 6)
	for _, by := range []string{"op-a", "op-b", "op-a", "op-b", "op-a", "op-b"} {
		go func() {
			<-start
			_, err := r.AddApproval(ctx, "r-1", "anonymize", models.ErasureApproval{By: by, At: now})
			errs <- err
		}()
	}
	close(start)
	landed := 0
	for range 6 {
		switch err := <-errs; {
		case err == nil:
			landed++
		case errors.Is(err, ErrErasureApprovalConflict):
		default:
			t.Fatalf("AddApproval: %v", err)
		}
	}
	if landed != 2 {
		t.Fatalf("landed = %d; want exactly 2 (one per operator)", landed)
	}
	row, _ := r.Get(ctx, "r-1")
	if len(row.Approvals) != 2 {
		t.Fatalf("approvals = %+v; want two distinct", row.Approvals)
	}
}

func TestErasureClaimPendingAppendsExecutorApprovalAndFencesMode(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()
	r := NewErasureRequestRepo(db)
	ctx := context.Background()
	now := time.Now().UTC()
	insertErasure(t, r, models.ErasureRequest{UUID: "r-1", UserUUID: "u-1", Status: models.ErasureRequestPending, RequestedAt: now, Mode: "anonymize",
		Approvals: []models.ErasureApproval{{By: "op-a", At: now}}})
	lease := now.Add(5 * time.Minute)
	if _, err := r.Claim(ctx, "r-1", models.ErasureRequestPending, "hard_delete", "op-b", now, lease, &models.ErasureApproval{By: "op-b", At: now, Note: "executed"}); !errors.Is(err, ErrErasureApprovalConflict) {
		t.Fatalf("Claim with another mode err = %v; want ErrErasureApprovalConflict", err)
	}
	if _, err := r.Claim(ctx, "r-1", models.ErasureRequestPending, "anonymize", "op-a", now, lease, &models.ErasureApproval{By: "op-a", At: now, Note: "executed"}); !errors.Is(err, ErrErasureApprovalConflict) {
		t.Fatalf("Claim with an existing approver err = %v; want ErrErasureApprovalConflict", err)
	}
	got, err := r.Claim(ctx, "r-1", models.ErasureRequestPending, "anonymize", "op-b", now, lease, &models.ErasureApproval{By: "op-b", At: now, Note: "executed"})
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if got.Status != models.ErasureRequestIncomplete || len(got.Approvals) != 2 || got.Approvals[1].By != "op-b" || got.Approvals[1].Note != "executed" {
		t.Fatalf("claimed = %+v; want incomplete with approvals [op-a op-b]", got)
	}
}

func TestErasureHasOpen(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()
	r := NewErasureRequestRepo(db)
	ctx := context.Background()
	now := time.Now().UTC()
	insertErasure(t, r, models.ErasureRequest{UUID: "r-done", UserUUID: "u-1", Status: models.ErasureRequestCompleted, RequestedAt: now})
	insertErasure(t, r, models.ErasureRequest{UUID: "r-pend", UserUUID: "u-2", Status: models.ErasureRequestPending, RequestedAt: now})
	insertErasure(t, r, models.ErasureRequest{UUID: "r-inc", UserUUID: "u-3", Status: models.ErasureRequestIncomplete, RequestedAt: now})
	if id, ok, err := r.HasOpen(ctx, "u-1"); err != nil || ok || id != "" {
		t.Fatalf("completed only = %q, %v, %v; want none", id, ok, err)
	}
	if id, ok, err := r.HasOpen(ctx, "u-2"); err != nil || !ok || id != "r-pend" {
		t.Fatalf("pending = %q, %v, %v; want r-pend", id, ok, err)
	}
	if id, ok, err := r.HasOpen(ctx, "u-3"); err != nil || !ok || id != "r-inc" {
		t.Fatalf("incomplete = %q, %v, %v; want r-inc", id, ok, err)
	}
}

func TestErasureConcurrentAdminInsertOnePendingPerSubject(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	r := NewErasureRequestRepo(db)
	if _, err := db.Collection(models.ErasureRequestsCollection).Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "userUuid", Value: 1}, {Key: "source", Value: 1}},
		Options: options.Index().SetUnique(true).SetPartialFilterExpression(models.PendingAdminErasurePartialFilter()),
	}); err != nil {
		t.Fatalf("index: %v", err)
	}
	now := time.Now().UTC()
	start := make(chan struct{})
	errs := make(chan error, 4)
	for i := range 4 {
		go func() {
			<-start
			errs <- r.Insert(ctx, &models.ErasureRequest{UUID: "r-" + string(rune('a'+i)), UserUUID: "u-1", Status: models.ErasureRequestPending,
				Source: models.ErasureSourceAdmin, RequestedAt: now})
		}()
	}
	close(start)
	won, lost := 0, 0
	for range 4 {
		switch err := <-errs; {
		case err == nil:
			won++
		case errors.Is(err, ErrErasureOpenExists):
			lost++
		default:
			t.Fatalf("Insert: %v", err)
		}
	}
	if won != 1 || lost != 3 {
		t.Fatalf("won = %d, lost = %d; want 1 and 3", won, lost)
	}
	// A lodged request or a started (incomplete) admin one is not constrained.
	if err := r.Insert(ctx, &models.ErasureRequest{UUID: "r-lodged", UserUUID: "u-1", Status: models.ErasureRequestPending, RequestedAt: now}); err != nil {
		t.Fatalf("lodged insert: %v", err)
	}
	if id, ok, err := r.HasOpen(ctx, "u-1"); err != nil || !ok || id == "" {
		t.Fatalf("HasOpen = %q, %v, %v", id, ok, err)
	}
}

func TestErasureListFiltersAndCursorAcrossEqualRequestedAt(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()
	r := NewErasureRequestRepo(db)
	ctx := context.Background()
	t0 := time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond)
	for _, id := range []string{"a", "b", "c", "d", "e", "f", "g"} {
		insertErasure(t, r, models.ErasureRequest{UUID: id, UserUUID: "u-" + id, Status: models.ErasureRequestCompleted, RequestedAt: t0})
	}
	insertErasure(t, r, models.ErasureRequest{UUID: "n", UserUUID: "u-n", Status: models.ErasureRequestCompleted, RequestedAt: t0.Add(time.Minute)})
	insertErasure(t, r, models.ErasureRequest{UUID: "p", UserUUID: "u-p", Status: models.ErasureRequestPending, RequestedAt: t0})

	var got []string
	after := ""
	pages := 0
	for {
		rows, next, err := r.List(ctx, ErasureListFilter{Statuses: []string{models.ErasureRequestCompleted}, Limit: 3, After: after})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		pages++
		for _, x := range rows {
			got = append(got, x.UUID)
		}
		if next == "" {
			break
		}
		after = next
	}
	want := []string{"n", "g", "f", "e", "d", "c", "b", "a"}
	if pages != 3 || len(got) != len(want) {
		t.Fatalf("pages=%d got=%v; want %v", pages, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got=%v; want %v", got, want)
		}
	}
	all, next, err := r.List(ctx, ErasureListFilter{})
	if err != nil || len(all) != 9 || next != "" {
		t.Fatalf("unlimited all = %d, %q, %v", len(all), next, err)
	}
	one, _, err := r.List(ctx, ErasureListFilter{UserUUID: "u-c"})
	if err != nil || len(one) != 1 || one[0].UUID != "c" {
		t.Fatalf("userUuid filter = %+v, %v", one, err)
	}
	if _, _, err := r.List(ctx, ErasureListFilter{After: "%%"}); !errors.Is(err, ErrErasureCursorInvalid) {
		t.Fatalf("bad cursor err = %v", err)
	}
}

func TestLegalHoldLapsedCannotBeRevived(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()
	r := NewLegalHoldRepo(db)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	if err := r.Insert(ctx, &models.LegalHold{UUID: "lapsed", UserUUID: "u", Reason: "r", Active: true,
		PlacedAt: now.Add(-time.Hour), ExpiresAt: tp(now.Add(-time.Minute))}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Update(ctx, "lapsed", bson.M{"expiresAt": now.Add(time.Hour), "updatedAt": now}, nil, now); err != ErrLegalHoldNotFound {
		t.Fatalf("extend lapsed = %v; want ErrLegalHoldNotFound", err)
	}
	if _, err := r.Update(ctx, "lapsed", bson.M{"updatedAt": now}, []string{"expiresAt"}, now); err != ErrLegalHoldNotFound {
		t.Fatalf("clear expiry on lapsed = %v; want ErrLegalHoldNotFound", err)
	}
	if _, err := r.AddNote(ctx, "lapsed", models.LegalHoldNote{ID: "n", Text: "x"}, now); err != ErrLegalHoldNotFound {
		t.Fatalf("note on lapsed = %v; want ErrLegalHoldNotFound", err)
	}
	if held, _ := r.IsHeld(ctx, "u", now); held {
		t.Fatal("lapsed hold must stay unheld")
	}
	// An operator can still release it.
	if err := r.Release(ctx, "lapsed", "op", "done", now); err != nil {
		t.Fatalf("release lapsed: %v", err)
	}
}

// --- DSR exports -----------------------------------------------------------

func TestDSRExportRepoLifecycle(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	repo := NewDSRExportRepo(db)
	now := time.Now().UTC().Truncate(time.Millisecond)
	mk := func(id, user string, created, expires time.Time) *models.DSRExport {
		return &models.DSRExport{UUID: id, UserUUID: user, Format: "json", RequestedBy: "op", Reason: "r",
			CreatedAt: created, ExpiresAt: expires, SizeBytes: 5, Producers: []string{"user"}, ObjectKey: id + ".json"}
	}
	for _, e := range []*models.DSRExport{
		mk("e1", "u1", now.Add(-3*time.Hour), now.Add(-time.Hour)),
		mk("e2", "u1", now.Add(-2*time.Hour), now.Add(time.Hour)),
		mk("e3", "u2", now.Add(-time.Hour), now.Add(-time.Minute)),
	} {
		if err := repo.Insert(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	got, err := repo.Get(ctx, "e2")
	if err != nil || got.ObjectKey != "e2.json" || got.UserUUID != "u1" {
		t.Fatalf("get = %+v, %v", got, err)
	}
	if _, err := repo.Get(ctx, "nope"); !errors.Is(err, ErrDSRExportNotFound) {
		t.Fatalf("missing err = %v", err)
	}
	mine, _ := repo.ListByUser(ctx, "u1")
	if len(mine) != 2 || mine[0].UUID != "e2" {
		t.Fatalf("list by user (newest first) = %+v", mine)
	}
	exp, _ := repo.ListExpired(ctx, now, 10)
	if len(exp) != 2 || exp[0].UUID != "e1" {
		t.Fatalf("expired = %+v", exp)
	}
	if one, _ := repo.ListExpired(ctx, now, 1); len(one) != 1 {
		t.Fatalf("limit ignored: %+v", one)
	}
	if err := repo.Delete(ctx, "e1"); err != nil {
		t.Fatal(err)
	}
	if err := repo.Delete(ctx, "e1"); err != nil {
		t.Fatalf("delete must be idempotent: %v", err)
	}
}
