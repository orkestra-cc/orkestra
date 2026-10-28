package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/retrieval/models"
	"github.com/orkestra/backend/pkg/sdk/tenantrepo"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

func newSearchLog(t *testing.T) (*SearchLog, *mongo.Database) {
	t.Helper()
	db := newTestDB(t)
	ensureIndexes(t, db, CollSearchLog, SearchLogIndexes())
	return NewSearchLog(db), db
}

func count(t *testing.T, db *mongo.Database, filter bson.M) int64 {
	t.Helper()
	//tenantscope:allow system: test assertion over every tenant
	n, err := db.Collection(CollSearchLog).CountDocuments(context.Background(), filter)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestSearchLog_InsertStampsTenantFromContext(t *testing.T) {
	r, db := newSearchLog(t)
	row := &models.SearchLog{UUID: "s1", TenantID: "t-other", UserUUID: "u1", Corpus: "c", Outcome: models.SearchOutcomeOK}
	if err := r.Insert(tctx("t-1"), row); err != nil {
		t.Fatal(err)
	}
	var got bson.M
	//tenantscope:allow system: test assertion over every tenant
	if err := db.Collection(CollSearchLog).FindOne(context.Background(), bson.M{"uuid": "s1"}).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got["tenantId"] != "t-1" {
		t.Fatalf("tenantId = %v, want t-1", got["tenantId"])
	}
	if got["createdAt"] == nil {
		t.Fatal("createdAt missing")
	}
}

func TestSearchLog_InsertWithoutTenantFails(t *testing.T) {
	t.Setenv("ENVIRONMENT", "production")
	t.Setenv("ENV", "production")
	r, db := newSearchLog(t)
	err := r.Insert(context.Background(), &models.SearchLog{UUID: "s1"})
	if !errors.Is(err, tenantrepo.ErrTenantScopeMissing) {
		t.Fatalf("err = %v, want ErrTenantScopeMissing", err)
	}
	if n := count(t, db, bson.M{}); n != 0 {
		t.Fatalf("count = %d, want 0", n)
	}
}

func TestSearchLog_PurgeBeforeSweepsEveryTenant(t *testing.T) {
	r, db := newSearchLog(t)
	now := time.Now().UTC()
	rows := []struct {
		tenant, uuid string
		at           time.Time
	}{
		{"t-1", "old1", now.Add(-100 * 24 * time.Hour)},
		{"t-2", "old2", now.Add(-100 * 24 * time.Hour)},
		{"t-1", "new1", now.Add(-24 * time.Hour)},
	}
	for _, x := range rows {
		if err := r.Insert(tctx(x.tenant), &models.SearchLog{UUID: x.uuid, CreatedAt: x.at}); err != nil {
			t.Fatal(err)
		}
	}
	n, err := r.PurgeBefore(context.Background(), now.Add(-90*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("purged = %d, want 2", n)
	}
	if left := count(t, db, bson.M{}); left != 1 {
		t.Fatalf("remaining = %d, want 1", left)
	}
	if count(t, db, bson.M{"uuid": "new1"}) != 1 {
		t.Fatal("the recent row was purged")
	}
}
