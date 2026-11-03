package repository

// Mongo-gated tests for MarketingOptoutRepository. Skip unless
// MONGO_TEST_URI is set, mirroring the pattern in
// internal/core/authz/repository/binding_ensure_integration_test.go:
// per-run random database name, a cleanup that drops it, and the same
// unique index module.go's Collections() declares in production.

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra/backend/internal/core/notification/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// newTestOptoutRepo returns the concrete type, not the interface, because
// the tests below reach into the unexported col() to assert on the raw
// document count.
func newTestOptoutRepo(t *testing.T) (*marketingOptoutRepo, func()) {
	t.Helper()
	uri := os.Getenv("MONGO_TEST_URI")
	if uri == "" {
		t.Skip("set MONGO_TEST_URI to run live notification optout repository tests")
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
	db := client.Database("orkestra_sdd_test_optout_" + uuid.NewString())
	repo := &marketingOptoutRepo{db: db}
	// Mirror module.go's Collections() unique index on address — production
	// boot creates this via the registry's ensureCollections; here the test
	// stands it up directly so Upsert's idempotency is proven against the
	// real constraint, not just against the fake in services tests.
	if _, err := db.Collection(models.NotificationMarketingOptoutsCollection).Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "address", Value: 1}},
		Options: options.Index().SetUnique(true),
	}); err != nil {
		t.Fatalf("create notification_marketing_optouts unique address index: %v", err)
	}
	cleanup := func() {
		_ = db.Drop(context.Background())
		_ = client.Disconnect(context.Background())
	}
	return repo, cleanup
}

func TestMongo_OptoutUpsertIsIdempotent(t *testing.T) {
	repo, done := newTestOptoutRepo(t)
	defer done()
	ctx := context.Background()

	doc := models.MarketingOptoutDoc{Address: "ada@example.test", Category: "marketing", At: time.Unix(100, 0), SourceTokenUUID: "t1"}
	if err := repo.Upsert(ctx, doc); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	// A second call is what happens after a crash between step 1 and step 2:
	// it must not fail, and it must not create a second document.
	doc.SourceTokenUUID = "t2"
	doc.At = time.Unix(200, 0)
	if err := repo.Upsert(ctx, doc); err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	//tenantscope:allow system: test assertion against the platform-global opt-out collection, same scope as the repository it exercises
	n, err := repo.col().CountDocuments(ctx, map[string]any{"address": "ada@example.test"})
	if err != nil || n != 1 {
		t.Fatalf("expected exactly one document per address, n=%d err=%v", n, err)
	}
}

func TestMongo_IsOptedOut(t *testing.T) {
	repo, done := newTestOptoutRepo(t)
	defer done()
	ctx := context.Background()
	if err := repo.Upsert(ctx, models.MarketingOptoutDoc{Address: "ada@example.test", Category: "marketing", At: time.Now()}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	for _, tc := range []struct {
		name string
		addr string
		want bool
	}{
		{"opted-out address", "ada@example.test", true},
		{"different address", "grace@example.test", false},
		{"uppercase and spacing: same address", "  Ada@Example.TEST  ", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := repo.IsOptedOut(ctx, tc.addr, "marketing")
			if err != nil {
				t.Fatalf("IsOptedOut: %v", err)
			}
			if got != tc.want {
				t.Fatalf("IsOptedOut(%q) = %v, want %v", tc.addr, got, tc.want)
			}
		})
	}
}

func TestMongo_OptoutGetAndDeleteIfAt(t *testing.T) {
	repo, done := newTestOptoutRepo(t)
	defer done()
	ctx := context.Background()

	if d, err := repo.Get(ctx, "ada@example.test"); d != nil || err != nil {
		t.Fatalf("Get on an empty collection = %v, %v; want nil, nil", d, err)
	}
	at := time.UnixMilli(1_700_000_000_000)
	if err := repo.Upsert(ctx, models.MarketingOptoutDoc{Address: "Ada@example.test", At: at, TenantID: "t1", SourceContext: "ctx-1"}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	got, err := repo.Get(ctx, "  ADA@example.test ")
	if err != nil || got == nil {
		t.Fatalf("Get: %v %v", got, err)
	}
	if !got.At.Equal(at) || got.TenantID != "t1" || got.SourceContext != "ctx-1" {
		t.Fatalf("unexpected doc: %+v", got)
	}

	if ok, err := repo.DeleteIfAt(ctx, "ada@example.test", at.Add(time.Millisecond)); err != nil || ok {
		t.Fatalf("wrong instant: ok=%v err=%v", ok, err)
	}
	if d, _ := repo.Get(ctx, "ada@example.test"); d == nil {
		t.Fatal("the row must be kept when the instant differs")
	}
	if ok, err := repo.DeleteIfAt(ctx, "ADA@example.test", got.At); err != nil || !ok {
		t.Fatalf("right instant: ok=%v err=%v", ok, err)
	}
	if d, _ := repo.Get(ctx, "ada@example.test"); d != nil {
		t.Fatal("the row must be gone")
	}
}
