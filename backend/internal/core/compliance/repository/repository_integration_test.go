package repository

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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
