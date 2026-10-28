package repository

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/retrieval/models"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

var (
	obNow     = time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC)
	obLease   = 2 * time.Minute
	obCorpora = []string{"crm_contacts"}
)

func newOutbox(t *testing.T) (*Outbox, *mongo.Database) {
	t.Helper()
	db := newTestDB(t)
	ensureIndexes(t, db, CollOutbox, OutboxIndexes())
	return NewOutbox(db), db
}

func intent(src string, rev int64) iface.RetrievalIntent {
	return iface.RetrievalIntent{Corpus: "crm_contacts", SourceID: src, Revision: rev}
}

func row(t *testing.T, db *mongo.Database, tenant, corpus, src string) *models.OutboxEntry {
	t.Helper()
	var e models.OutboxEntry
	//tenantscope:allow system: test assertion reads a row directly
	err := db.Collection(CollOutbox).FindOne(context.Background(), bson.M{"tenantId": tenant, "corpus": corpus, "sourceId": src}).Decode(&e)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return &e
}

func mustEnqueue(t *testing.T, o *Outbox, tenant string, rev int64, src string) {
	t.Helper()
	if err := o.Enqueue(tctx(tenant), []iface.RetrievalIntent{intent(src, rev)}, obNow); err != nil {
		t.Fatal(err)
	}
}

func TestOutbox_EnqueueCoalescesRevisionsWithMax(t *testing.T) {
	o, db := newOutbox(t)
	for _, rev := range []int64{3, 2, 5} {
		mustEnqueue(t, o, "t1", rev, "c1")
	}
	//tenantscope:allow system: test assertion counts rows directly
	n, _ := db.Collection(CollOutbox).CountDocuments(context.Background(), bson.M{})
	if n != 1 {
		t.Fatalf("rows = %d", n)
	}
	e := row(t, db, "t1", "crm_contacts", "c1")
	if e.Revision != 5 || !e.Dirty || e.Epoch != 0 || e.Attempts != 0 {
		t.Fatalf("entry = %+v", e)
	}
}

func TestOutbox_EnqueueJoinsTheCallerTransaction(t *testing.T) {
	o, db := newOutbox(t)
	sess, err := db.Client().StartSession()
	if err != nil {
		t.Fatal(err)
	}
	defer sess.EndSession(context.Background())

	_, err = sess.WithTransaction(tctx("t1"), func(sc mongo.SessionContext) (any, error) {
		if _, err := db.Collection("src").InsertOne(sc, bson.M{"_id": "src"}); err != nil {
			return nil, err
		}
		return nil, o.Enqueue(sc, []iface.RetrievalIntent{intent("c1", 1)}, obNow)
	})
	if err != nil {
		t.Fatal(err)
	}
	if row(t, db, "t1", "crm_contacts", "c1") == nil {
		t.Fatal("committed row missing")
	}

	abort := errors.New("abort")
	_, err = sess.WithTransaction(tctx("t1"), func(sc mongo.SessionContext) (any, error) {
		if _, err := db.Collection("src").InsertOne(sc, bson.M{"_id": "src2"}); err != nil {
			return nil, err
		}
		if err := o.Enqueue(sc, []iface.RetrievalIntent{intent("c2", 1)}, obNow); err != nil {
			return nil, err
		}
		return nil, abort
	})
	if !errors.Is(err, abort) {
		t.Fatalf("err = %v", err)
	}
	if row(t, db, "t1", "crm_contacts", "c2") != nil {
		t.Fatal("aborted enqueue left a row")
	}
	//tenantscope:allow system: test assertion counts rows directly
	if n, _ := db.Collection("src").CountDocuments(context.Background(), bson.M{"_id": "src2"}); n != 0 {
		t.Fatal("aborted source write left a doc")
	}
}

func TestOutbox_ClaimIncrementsEpochAndClearsDirty(t *testing.T) {
	o, _ := newOutbox(t)
	mustEnqueue(t, o, "t1", 1, "c1")
	ctx := tctx("t1")
	e, err := o.Claim(ctx, obNow, obCorpora, "w1", obLease)
	if err != nil || e == nil {
		t.Fatalf("claim = %v, %v", e, err)
	}
	if e.Epoch != 1 || e.Dirty || e.LeaseOwner != "w1" || e.LeaseUntil == nil || !e.LeaseUntil.Equal(obNow.Add(obLease)) {
		t.Fatalf("entry = %+v", e)
	}
	if e2, err := o.Claim(ctx, obNow, obCorpora, "w2", obLease); err != nil || e2 != nil {
		t.Fatalf("second claim = %v, %v", e2, err)
	}
	e3, err := o.Claim(ctx, obNow.Add(obLease+time.Millisecond), obCorpora, "w2", obLease)
	if err != nil || e3 == nil || e3.Epoch != 2 || e3.LeaseOwner != "w2" {
		t.Fatalf("takeover = %+v, %v", e3, err)
	}
}

func TestOutbox_ClaimHonoursCorporaAndBackoff(t *testing.T) {
	o, db := newOutbox(t)
	if err := o.Enqueue(tctx("t1"), []iface.RetrievalIntent{{Corpus: "other", SourceID: "x", Revision: 1}}, obNow); err != nil {
		t.Fatal(err)
	}
	if e, err := o.Claim(tctx("t1"), obNow, obCorpora, "w1", obLease); err != nil || e != nil {
		t.Fatalf("other corpus claimed: %v, %v", e, err)
	}
	mustEnqueue(t, o, "t1", 1, "c1")
	//tenantscope:allow system: test fixture adjusts a row directly
	_, err := db.Collection(CollOutbox).UpdateOne(context.Background(), bson.M{"sourceId": "c1"},
		bson.M{"$set": bson.M{"nextAttemptAt": obNow.Add(time.Minute)}})
	if err != nil {
		t.Fatal(err)
	}
	if e, err := o.Claim(tctx("t1"), obNow, obCorpora, "w1", obLease); err != nil || e != nil {
		t.Fatalf("backoff claimed: %v, %v", e, err)
	}
	if e, err := o.Claim(tctx("t1"), obNow.Add(time.Minute), obCorpora, "w1", obLease); err != nil || e == nil {
		t.Fatalf("due claim: %v, %v", e, err)
	}
}

func TestOutbox_ReleaseIsFencedByEpoch(t *testing.T) {
	o, db := newOutbox(t)
	mustEnqueue(t, o, "t1", 1, "c1")
	ctx := tctx("t1")
	if _, err := o.Claim(ctx, obNow, obCorpora, "w1", obLease); err != nil {
		t.Fatal(err)
	}
	later := obNow.Add(obLease + time.Millisecond)
	if e, err := o.Claim(ctx, later, obCorpora, "w2", obLease); err != nil || e == nil || e.Epoch != 2 {
		t.Fatalf("takeover = %+v, %v", e, err)
	}
	if ok, err := o.HoldsLease(ctx, "crm_contacts", "c1", 1, "w1", later); err != nil || ok {
		t.Fatalf("stale holds = %v, %v", ok, err)
	}
	if ok, err := o.HoldsLease(ctx, "crm_contacts", "c1", 2, "w2", later); err != nil || !ok {
		t.Fatalf("current holds = %v, %v", ok, err)
	}
	if err := o.Release(ctx, "crm_contacts", "c1", 1, "w1", nil, later); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("release err = %v", err)
	}
	if err := o.Reschedule(ctx, "crm_contacts", "c1", 1, "w1", 1, later, "embed"); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("reschedule err = %v", err)
	}
	if e := row(t, db, "t1", "crm_contacts", "c1"); e.LeaseOwner != "w2" {
		t.Fatalf("owner = %q", e.LeaseOwner)
	}
}

func TestOutbox_EnqueueDuringLeaseKeepsKeyDirty(t *testing.T) {
	o, db := newOutbox(t)
	mustEnqueue(t, o, "t1", 1, "c1")
	ctx := tctx("t1")
	if _, err := o.Claim(ctx, obNow, obCorpora, "w1", obLease); err != nil {
		t.Fatal(err)
	}
	mustEnqueue(t, o, "t1", 2, "c1")
	applied := &models.AppliedState{Generation: 1, Revision: 1, ProfileHash: "p", IndexedHash: "h", Chunks: 3}
	if err := o.Release(ctx, "crm_contacts", "c1", 1, "w1", applied, obNow); err != nil {
		t.Fatal(err)
	}
	e := row(t, db, "t1", "crm_contacts", "c1")
	if !e.Dirty || e.Revision != 2 || e.LeaseOwner != "" || e.LeaseUntil != nil || e.Applied == nil || e.Applied.Revision != 1 {
		t.Fatalf("entry = %+v", e)
	}
	// nil applied clears the stored state
	if _, err := o.Claim(ctx, obNow, obCorpora, "w1", obLease); err != nil {
		t.Fatal(err)
	}
	if err := o.Release(ctx, "crm_contacts", "c1", 2, "w1", nil, obNow); err != nil {
		t.Fatal(err)
	}
	if e := row(t, db, "t1", "crm_contacts", "c1"); e.Applied != nil || e.Dirty {
		t.Fatalf("entry = %+v", e)
	}
}

func TestOutbox_RescheduleSetsBackoffAndDirty(t *testing.T) {
	o, db := newOutbox(t)
	mustEnqueue(t, o, "t1", 1, "c1")
	ctx := tctx("t1")
	if _, err := o.Claim(ctx, obNow, obCorpora, "w1", obLease); err != nil {
		t.Fatal(err)
	}
	next := obNow.Add(5 * time.Second)
	if err := o.Reschedule(ctx, "crm_contacts", "c1", 1, "w1", 1, next, "embed"); err != nil {
		t.Fatal(err)
	}
	e := row(t, db, "t1", "crm_contacts", "c1")
	if !e.Dirty || e.Attempts != 1 || !e.NextAttemptAt.Equal(next) || e.LastError != "embed" || e.LeaseUntil != nil || e.LeaseOwner != "" {
		t.Fatalf("entry = %+v", e)
	}
}

func TestOutbox_RequeueUpsertsDirty(t *testing.T) {
	o, db := newOutbox(t)
	ctx := tctx("t1")
	if err := o.Requeue(ctx, "crm_contacts", "c1", obNow); err != nil {
		t.Fatal(err)
	}
	e := row(t, db, "t1", "crm_contacts", "c1")
	if e == nil || !e.Dirty || e.Epoch != 0 {
		t.Fatalf("entry = %+v", e)
	}
	if _, err := o.Claim(ctx, obNow, obCorpora, "w1", obLease); err != nil {
		t.Fatal(err)
	}
	before := row(t, db, "t1", "crm_contacts", "c1")
	if err := o.Requeue(ctx, "crm_contacts", "c1", obNow.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	after := row(t, db, "t1", "crm_contacts", "c1")
	if !after.Dirty || after.LeaseOwner != "w1" || after.Epoch != before.Epoch || after.LeaseUntil == nil || !after.LeaseUntil.Equal(*before.LeaseUntil) {
		t.Fatalf("after = %+v", after)
	}
}

// DueTenants serves a round-robin cursor: tenants with a claimable key,
// strictly after the cursor, in tenant id order (not by age: a tenant with an
// old backlog must not win every pass), one entry per tenant.
func TestOutbox_DueTenantsOrdersByTenantAfterTheCursor(t *testing.T) {
	o, db := newOutbox(t)
	for _, k := range []struct{ tenant, src string }{{"t3", "c1"}, {"t1", "c1"}, {"t1", "c2"}, {"t2", "c1"}, {"t4", "c1"}} {
		mustEnqueue(t, o, k.tenant, 1, k.src)
	}
	set := func(tenant string, at time.Time) {
		//tenantscope:allow system: test fixture adjusts a row directly
		if _, err := db.Collection(CollOutbox).UpdateMany(context.Background(), bson.M{"tenantId": tenant},
			bson.M{"$set": bson.M{"nextAttemptAt": at}}); err != nil {
			t.Fatal(err)
		}
	}
	set("t3", obNow.Add(-2*time.Minute)) // oldest, still listed after t1 and t2
	set("t1", obNow.Add(-time.Minute))
	set("t4", obNow.Add(time.Minute)) // not due
	ctx := context.Background()
	for _, tc := range []struct {
		after string
		limit int
		want  []string
	}{
		{"", 64, []string{"t1", "t2", "t3"}},
		{"", 2, []string{"t1", "t2"}},
		{"t1", 64, []string{"t2", "t3"}},
		{"t2", 1, []string{"t3"}},
		{"t3", 64, nil},
		{"", 0, nil},
		{"", -1, nil},
	} {
		got, err := o.DueTenants(ctx, obNow, obCorpora, tc.after, tc.limit)
		if err != nil || !slices.Equal(got, tc.want) {
			t.Fatalf("DueTenants(after %q, limit %d) = %v, %v; want %v", tc.after, tc.limit, got, err, tc.want)
		}
	}
	// a leased key is not claimable until its lease expires
	if _, err := o.Claim(tctx("t2"), obNow, obCorpora, "w1", obLease); err != nil {
		t.Fatal(err)
	}
	if got, err := o.DueTenants(ctx, obNow, obCorpora, "t1", 64); err != nil || !slices.Equal(got, []string{"t3"}) {
		t.Fatalf("after claim = %v, %v", got, err)
	}
}

func TestOutbox_TenantScoped(t *testing.T) {
	o, _ := newOutbox(t)
	mustEnqueue(t, o, "t1", 1, "c1")
	if e, err := o.Claim(tctx("t2"), obNow, obCorpora, "w1", obLease); err != nil || e != nil {
		t.Fatalf("cross-tenant claim = %v, %v", e, err)
	}
	if ok, _ := o.HoldsLease(tctx("t2"), "crm_contacts", "c1", 0, "w1", obNow); ok {
		t.Fatal("cross-tenant lease")
	}
}
