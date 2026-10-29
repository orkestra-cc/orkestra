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

func mustInsert(t *testing.T, db *mongo.Database, coll string, docs ...any) {
	t.Helper()
	if _, err := db.Collection(coll).InsertMany(context.Background(), docs); err != nil {
		t.Fatal(err)
	}
}

func TestAdminStore_SearchLogIsTenantScoped(t *testing.T) {
	t.Setenv("ENVIRONMENT", "production") // tenantrepo panics on a missing tenant in development
	t.Setenv("ENV", "production")
	db := newTestDB(t)
	ensureIndexes(t, db, CollSearchLog, SearchLogIndexes())
	sl := NewSearchLog(db)
	r := NewAdminStore(db)
	day := func(n int) time.Time { return time.Date(2026, 3, n, 12, 0, 0, 0, time.UTC) }
	for _, x := range []struct {
		tenant, uuid, user string
		at                 time.Time
	}{
		{"tA", "a1", "u1", day(1)}, {"tA", "a2", "u1", day(2)}, {"tA", "a3", "u2", day(3)},
		{"tB", "b1", "u1", day(1)}, {"tB", "b2", "u2", day(2)},
	} {
		if err := sl.Insert(tctx(x.tenant), &models.SearchLog{UUID: x.uuid, UserUUID: x.user, CreatedAt: x.at}); err != nil {
			t.Fatal(err)
		}
	}
	ctx := tctx("tA")

	rows, total, err := r.SearchLog(ctx, models.SearchLogQuery{Limit: 50})
	if err != nil || total != 3 || len(rows) != 3 {
		t.Fatalf("all = %d rows total %d err %v", len(rows), total, err)
	}
	for i, want := range []string{"a3", "a2", "a1"} {
		if rows[i].UUID != want || rows[i].TenantID != "tA" {
			t.Errorf("row %d = %s/%s, want %s/tA", i, rows[i].UUID, rows[i].TenantID, want)
		}
	}
	if rows, total, _ = r.SearchLog(ctx, models.SearchLogQuery{UserUUID: "u1", Limit: 50}); len(rows) != 2 || total != 2 {
		t.Errorf("user filter = %d total %d", len(rows), total)
	}
	d2 := day(2)
	if rows, total, _ = r.SearchLog(ctx, models.SearchLogQuery{Since: &d2, Until: &d2, Limit: 50}); len(rows) != 1 || total != 1 || rows[0].UUID != "a2" {
		t.Errorf("inclusive bounds = %+v total %d", rows, total)
	}
	// A sub-millisecond Until still includes a row stored at that millisecond.
	d2n := d2.Add(500 * time.Microsecond)
	if rows, _, _ = r.SearchLog(ctx, models.SearchLogQuery{Since: &d2, Until: &d2n, Limit: 50}); len(rows) != 1 {
		t.Errorf("sub-ms Until = %d rows", len(rows))
	}
	if rows, total, _ = r.SearchLog(ctx, models.SearchLogQuery{Limit: 1, Offset: 1}); len(rows) != 1 || rows[0].UUID != "a2" || total != 3 {
		t.Errorf("paging = %+v total %d", rows, total)
	}
	if _, _, err = r.SearchLog(context.Background(), models.SearchLogQuery{Limit: 50}); !errors.Is(err, tenantrepo.ErrTenantScopeMissing) {
		t.Errorf("no tenant in ctx: err = %v, want ErrTenantScopeMissing", err)
	}
}

func TestAdminStore_SearchLogTieBreaksOnUUID(t *testing.T) {
	db := newTestDB(t)
	sl := NewSearchLog(db)
	at := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	for _, u := range []string{"a", "c", "b"} {
		if err := sl.Insert(tctx("tA"), &models.SearchLog{UUID: u, CreatedAt: at}); err != nil {
			t.Fatal(err)
		}
	}
	rows, _, err := NewAdminStore(db).SearchLog(tctx("tA"), models.SearchLogQuery{Limit: 10})
	if err != nil || len(rows) != 3 || rows[0].UUID != "c" || rows[1].UUID != "b" || rows[2].UUID != "a" {
		t.Fatalf("order = %+v err %v", rows, err)
	}
}

func TestAdminStore_SearchLogHidesUnchecked(t *testing.T) {
	db := newTestDB(t)
	mustInsert(t, db, CollSearchLog,
		bson.M{"uuid": "x1", "tenantId": "tA", "unchecked": true, "createdAt": time.Now()},
		bson.M{"uuid": "x2", "tenantId": "tA", "createdAt": time.Now()},
		bson.M{"uuid": "x3", "tenantId": "tA", "unchecked": false, "createdAt": time.Now()},
	)
	rows, total, err := NewAdminStore(db).SearchLog(tctx("tA"), models.SearchLogQuery{Limit: 50})
	if err != nil || len(rows) != 2 || total != 2 {
		t.Fatalf("rows %d total %d err %v", len(rows), total, err)
	}
	for _, r := range rows {
		if r.UUID == "x1" {
			t.Error("unchecked row listed")
		}
	}
}

func TestAdminStore_CountsAndBacklog(t *testing.T) {
	t.Setenv("ENVIRONMENT", "production") // tenantrepo panics on a missing tenant in development
	t.Setenv("ENV", "production")
	db := newTestDB(t)
	r := NewAdminStore(db)
	t0 := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	t1, t2 := t0.Add(time.Hour), t0.Add(2*time.Hour)
	lease := t2.Add(time.Hour)
	mustInsert(t, db, CollOutbox,
		bson.M{"tenantId": "tA", "corpus": "crm", "sourceId": "1", "dirty": true, "updatedAt": t0},
		bson.M{"tenantId": "tA", "corpus": "crm", "sourceId": "2", "dirty": false, "leaseUntil": lease, "updatedAt": t1}, // being applied
		bson.M{"tenantId": "tB", "corpus": "crm", "sourceId": "3", "dirty": true, "updatedAt": t2},
		bson.M{"tenantId": "tA", "corpus": "crm", "sourceId": "4", "dirty": false, "updatedAt": t0.Add(-time.Hour)}, // applied
	)
	all, err := r.OutboxBacklogAll(context.Background())
	if err != nil || len(all) != 1 || all["crm"].Pending != 3 || all["crm"].OldestAt == nil || !all["crm"].OldestAt.Equal(t0) {
		t.Fatalf("all = %+v err %v", all, err)
	}
	ten, err := r.OutboxBacklogTenant(tctx("tA"))
	if err != nil || ten["crm"].Pending != 2 || ten["crm"].OldestAt == nil || !ten["crm"].OldestAt.Equal(t0) {
		t.Fatalf("tenant = %+v err %v", ten, err)
	}
	if _, err := r.OutboxBacklogTenant(context.Background()); !errors.Is(err, tenantrepo.ErrTenantScopeMissing) {
		t.Errorf("no tenant in ctx: err = %v, want ErrTenantScopeMissing", err)
	}

	mustInsert(t, db, CollTenantProgress,
		bson.M{"tenantId": "tA", "corpus": "crm", "generation": 1, "status": models.ProgressDone},
		bson.M{"tenantId": "tB", "corpus": "crm", "generation": 1, "status": models.ProgressBuilding},
		bson.M{"tenantId": "tA", "corpus": "crm", "generation": 2, "status": models.ProgressBuilding},
	)
	pc, err := r.ProgressCounts(context.Background())
	if err != nil || len(pc) != 2 ||
		pc[models.ProgressKey{Corpus: "crm", Generation: 1}] != (models.ProgressCounts{Done: 1, Building: 1}) ||
		pc[models.ProgressKey{Corpus: "crm", Generation: 2}] != (models.ProgressCounts{Building: 1}) {
		t.Fatalf("progress = %+v err %v", pc, err)
	}

	mustInsert(t, db, CollErasures,
		bson.M{"uuid": "e1", "scope": "global", "kind": "user", "status": "pending"},
		bson.M{"uuid": "e2", "scope": "tenant", "tenantId": "tA", "kind": "item", "status": "fencing"},
		bson.M{"uuid": "e3", "scope": "tenant", "tenantId": "tA", "kind": "subject", "status": "done"},
		bson.M{"uuid": "e4", "scope": "global", "kind": "user"}, // legacy: no status, pending
		bson.M{"uuid": "e5", "scope": "tenant", "tenantId": "tB", "kind": "item", "status": "pending"},
	)
	ec, err := r.ErasureCounts(context.Background())
	if err != nil || ec != (models.RetrievalErasureCounts{Pending: 3, Fencing: 1}) {
		t.Fatalf("erasures = %+v err %v", ec, err)
	}
	n, err := r.TenantErasuresPending(tctx("tA"))
	if err != nil || n != 1 {
		t.Fatalf("tenant erasures = %d err %v", n, err)
	}
	if _, err := r.TenantErasuresPending(context.Background()); !errors.Is(err, tenantrepo.ErrTenantScopeMissing) {
		t.Errorf("no tenant in ctx: err = %v, want ErrTenantScopeMissing", err)
	}
}

func TestAdminStore_DeleteCorpusRows(t *testing.T) {
	db := newTestDB(t)
	r := NewAdminStore(db)
	states := NewCorpusStates(db)
	for _, c := range []string{"old", "crm"} {
		for _, tn := range []string{"tA", "tB"} {
			mustInsert(t, db, CollOutbox, bson.M{"tenantId": tn, "corpus": c, "sourceId": "1", "dirty": true})
			mustInsert(t, db, CollTenantProgress, bson.M{"tenantId": tn, "corpus": c, "generation": 1, "status": "done"})
		}
		if err := states.Insert(context.Background(), &models.CorpusState{Corpus: c, Status: "active"}); err != nil {
			t.Fatal(err)
		}
	}
	mustInsert(t, db, CollErasures, bson.M{"uuid": "e1", "scope": "tenant", "tenantId": "tA", "kind": "item", "corpus": "old"})

	ob, pr, err := r.DeleteCorpusRows(context.Background(), "old")
	if err != nil || ob != 2 || pr != 2 {
		t.Fatalf("deleted outbox %d progress %d err %v", ob, pr, err)
	}
	for _, coll := range []string{CollOutbox, CollTenantProgress} {
		//tenantscope:allow system: test assertion over every tenant
		if n, _ := db.Collection(coll).CountDocuments(context.Background(), bson.M{"corpus": "old"}); n != 0 {
			t.Errorf("%s still has %d old rows", coll, n)
		}
		//tenantscope:allow system: test assertion over every tenant
		if n, _ := db.Collection(coll).CountDocuments(context.Background(), bson.M{"corpus": "crm"}); n != 2 {
			t.Errorf("%s crm rows = %d, want 2", coll, n)
		}
	}
	//tenantscope:allow system: test assertion over every tenant
	if n, _ := db.Collection(CollErasures).CountDocuments(context.Background(), bson.M{}); n != 1 {
		t.Errorf("erasure rows = %d, want untouched 1", n)
	}

	// CAS: a stale version conflicts and keeps the row; the current one deletes it.
	if err := r.DeleteCorpusState(context.Background(), "old", 99); !errors.Is(err, ErrStateConflict) {
		t.Fatalf("stale version err = %v", err)
	}
	if st, _ := states.Get(context.Background(), "old"); st == nil {
		t.Fatal("state deleted despite conflict")
	}
	if err := r.DeleteCorpusState(context.Background(), "old", 1); err != nil {
		t.Fatal(err)
	}
	if st, err := states.Get(context.Background(), "old"); st != nil || err != nil {
		t.Fatalf("after delete Get = %v, %v; want nil, nil", st, err)
	}
	if st, _ := states.Get(context.Background(), "crm"); st == nil {
		t.Error("crm state deleted")
	}
}
