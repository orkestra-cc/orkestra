package repository

// Mongo-gated tests for SuppressionRepository (same pattern as
// optout_repository_test.go): random database, the production unique index.

import (
	"context"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra/backend/internal/core/notification/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func newTestSuppressionRepo(t *testing.T) SuppressionRepository {
	t.Helper()
	uri := os.Getenv("MONGO_TEST_URI")
	if uri == "" {
		t.Skip("set MONGO_TEST_URI to run live notification suppression repository tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("mongo.Connect: %v", err)
	}
	db := client.Database("ork_test_suppr_" + uuid.NewString()[:8])
	if _, err := db.Collection(models.NotificationSuppressionsCollection).Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "address", Value: 1}},
		Options: options.Index().SetUnique(true),
	}); err != nil {
		t.Fatalf("create unique address index: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Drop(context.Background())
		_ = client.Disconnect(context.Background())
	})
	return NewSuppressionRepository(db)
}

func TestMongo_SuppressionInsertIfAbsentKeepsFirstRow(t *testing.T) {
	repo := newTestSuppressionRepo(t)
	ctx := context.Background()
	first := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	added, err := repo.InsertIfAbsent(ctx, []string{"Ada@Example.test", "bob@example.test", "ada@example.test", "  "},
		models.SuppressionDoc{Reason: "hard_bounce", CreatedAt: first, Source: "operator.import", ActorUUID: "op-1", TenantID: "t-1"})
	if err != nil {
		t.Fatalf("InsertIfAbsent: %v", err)
	}
	sort.Strings(added)
	if len(added) != 2 || added[0] != "ada@example.test" || added[1] != "bob@example.test" {
		t.Fatalf("added = %v", added)
	}

	added, err = repo.InsertIfAbsent(ctx, []string{"ada@example.test", "cy@example.test"},
		models.SuppressionDoc{Reason: "manual", CreatedAt: time.Now()})
	if err != nil {
		t.Fatalf("second InsertIfAbsent: %v", err)
	}
	if len(added) != 1 || added[0] != "cy@example.test" {
		t.Fatalf("second added = %v", added)
	}

	rows, err := repo.GetMany(ctx, []string{"ADA@example.test", "nobody@example.test"})
	if err != nil {
		t.Fatalf("GetMany: %v", err)
	}
	ada, ok := rows["ada@example.test"]
	if len(rows) != 1 || !ok {
		t.Fatalf("rows = %+v", rows)
	}
	if ada.Reason != "hard_bounce" || !ada.CreatedAt.Equal(first) || ada.Source != "operator.import" || ada.ActorUUID != "op-1" || ada.TenantID != "t-1" {
		t.Fatalf("first row was overwritten: %+v", ada)
	}

	for _, a := range []string{"Ada Lovelace <ADA@example.test>", " bob@example.test"} {
		if ok, err := repo.IsSuppressed(ctx, a); err != nil || !ok {
			t.Fatalf("IsSuppressed(%q) = %v, %v", a, ok, err)
		}
	}
}

func TestMongo_SuppressionDeleteManyReportsRemoved(t *testing.T) {
	repo := newTestSuppressionRepo(t)
	ctx := context.Background()
	if _, err := repo.InsertIfAbsent(ctx, []string{"a@example.test", "b@example.test"}, models.SuppressionDoc{Reason: "manual", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	//tenantscope:allow platform: the suppression list is address-keyed and platform-wide by design (tenantId is attribution only)
	removed, err := repo.DeleteMany(ctx, []string{"A@example.test", "zzz@example.test"})
	if err != nil {
		t.Fatalf("DeleteMany: %v", err)
	}
	if len(removed) != 1 || removed[0] != "a@example.test" {
		t.Fatalf("removed = %v", removed)
	}
	if ok, _ := repo.IsSuppressed(ctx, "a@example.test"); ok {
		t.Fatal("a@example.test still suppressed")
	}
	if ok, _ := repo.IsSuppressed(ctx, "b@example.test"); !ok {
		t.Fatal("b@example.test must stay suppressed")
	}
}
