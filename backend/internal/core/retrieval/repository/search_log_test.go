package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/retrieval/models"
	"github.com/orkestra/backend/pkg/sdk/tenantrepo"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
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

func TestSearchLog_PurgeBeforeBatches(t *testing.T) {
	old := searchLogPurgeBatch
	searchLogPurgeBatch = 10
	t.Cleanup(func() { searchLogPurgeBatch = old })
	r, db := newSearchLog(t)
	now := time.Now().UTC()
	for i := 0; i < 25; i++ {
		if err := r.Insert(tctx("t-1"), &models.SearchLog{UUID: fmt.Sprintf("old%d", i), CreatedAt: now.Add(-100 * 24 * time.Hour)}); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 3; i++ {
		if err := r.Insert(tctx("t-2"), &models.SearchLog{UUID: fmt.Sprintf("new%d", i), CreatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	n, err := r.PurgeBefore(context.Background(), now.Add(-90*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if n != 25 {
		t.Fatalf("purged = %d, want 25", n)
	}
	if left := count(t, db, bson.M{}); left != 3 {
		t.Fatalf("remaining = %d, want 3", left)
	}
}

func TestSearchLog_UUIDUniquePerTenant(t *testing.T) {
	r, _ := newSearchLog(t)
	if err := r.Insert(tctx("t-1"), &models.SearchLog{UUID: "s1"}); err != nil {
		t.Fatal(err)
	}
	if err := r.Insert(tctx("t-1"), &models.SearchLog{UUID: "s1"}); !mongo.IsDuplicateKeyError(err) {
		t.Fatalf("same tenant+uuid err = %v, want duplicate key", err)
	}
	if err := r.Insert(tctx("t-2"), &models.SearchLog{UUID: "s1"}); err != nil {
		t.Fatalf("other tenant: %v", err)
	}
}

func TestSearchLog_InsertCreatedAtAndEmptyArrays(t *testing.T) {
	r, db := newSearchLog(t)
	set := time.Date(2026, 1, 2, 3, 4, 5, 678_901_234, time.FixedZone("x", 3600))
	if err := r.Insert(tctx("t-1"), &models.SearchLog{UUID: "set", CreatedAt: set}); err != nil {
		t.Fatal(err)
	}
	before := time.Now().Add(-time.Second)
	if err := r.Insert(tctx("t-1"), &models.SearchLog{UUID: "dflt"}); err != nil {
		t.Fatal(err)
	}
	get := func(u string) models.SearchLog {
		var d models.SearchLog
		//tenantscope:allow system: test assertion over every tenant
		if err := db.Collection(CollSearchLog).FindOne(context.Background(), bson.M{"uuid": u}).Decode(&d); err != nil {
			t.Fatal(err)
		}
		return d
	}
	if got := get("set").CreatedAt; !got.Equal(set.UTC().Truncate(time.Millisecond)) {
		t.Fatalf("createdAt = %v", got)
	}
	if got := get("dflt").CreatedAt; got.Before(before) || got.After(time.Now().Add(time.Second)) {
		t.Fatalf("default createdAt = %v", got)
	}
	var raw bson.M
	//tenantscope:allow system: test assertion over every tenant
	if err := db.Collection(CollSearchLog).FindOne(context.Background(), bson.M{"uuid": "set"}).Decode(&raw); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"principals", "sourceIds", "subjects"} {
		if _, ok := raw[f].(bson.A); !ok {
			t.Fatalf("%s = %#v, want empty array", f, raw[f])
		}
	}
}

func TestSearchLog_PurgeBeforeReturnsErrorOnDisconnectedClient(t *testing.T) {
	uri := os.Getenv("MONGO_TEST_URI")
	if uri == "" {
		t.Skip("MONGO_TEST_URI not set")
	}
	client, err := mongo.Connect(context.Background(), options.Client().ApplyURI(uri).SetServerSelectionTimeout(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Disconnect(context.Background()); err != nil {
		t.Fatal(err)
	}
	r := NewSearchLog(client.Database("retrieval_test_disconnected"))
	n, err := r.PurgeBefore(context.Background(), time.Now())
	if err == nil || n != 0 {
		t.Fatalf("n=%d err=%v, want 0 and an error", n, err)
	}
	cctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := r.PurgeBefore(cctx, time.Now()); err == nil {
		t.Fatal("cancelled ctx: want error")
	}
}
