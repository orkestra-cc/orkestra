package repository

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/retrieval/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

var fin0 = time.Date(2026, 10, 28, 12, 0, 0, 123456789, time.UTC)

func newFinalization(t *testing.T) (*ErasureFinalization, *mongo.Database) {
	t.Helper()
	db := newTestDB(t)
	ensureIndexes(t, db, CollErasures, ErasureIndexes())
	return NewErasureFinalization(db), db
}

func countErasures(t *testing.T, db *mongo.Database, filter bson.M) int64 {
	t.Helper()
	//tenantscope:allow system: test assertion over every tenant
	n, err := db.Collection(CollErasures).CountDocuments(context.Background(), filter)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func claimOrFatal(t *testing.T, r *ErasureFinalization, now time.Time, hold time.Duration) *models.Erasure {
	t.Helper()
	e, err := r.ClaimDue(context.Background(), now, hold)
	if err != nil || e == nil {
		t.Fatalf("ClaimDue = %v, %v; want a row", e, err)
	}
	return e
}

func TestErasureFinalization_EnsureUserIdempotent(t *testing.T) {
	r, db := newFinalization(t)
	ctx := context.Background()
	e1, c1, err := r.EnsureUser(ctx, "u1", fin0)
	if err != nil {
		t.Fatal(err)
	}
	e2, c2, err := r.EnsureUser(ctx, "u1", fin0.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if !c1 || c2 {
		t.Fatalf("created = %v, %v; want true, false", c1, c2)
	}
	if e1.UUID == "" || e1.UUID != e2.UUID {
		t.Fatalf("uuids %q %q", e1.UUID, e2.UUID)
	}
	if e1.Scope != "global" || e1.TenantID != "" || e1.Kind != "user" || e1.Subject != "user:u1" {
		t.Fatalf("row = %+v", e1)
	}
	if e1.EffectiveStatus() != "pending" || e1.NextAttemptAt == nil || !e1.NextAttemptAt.Equal(ms(fin0)) {
		t.Fatalf("status/next = %s %v", e1.EffectiveStatus(), e1.NextAttemptAt)
	}
	if n := countErasures(t, db, bson.M{"subject": "user:u1"}); n != 1 {
		t.Fatalf("rows = %d, want 1", n)
	}
	list, err := r.ListUser(ctx, "u1")
	if err != nil || len(list) != 1 || list[0].UUID != e1.UUID {
		t.Fatalf("ListUser = %v, %v", list, err)
	}
}

func TestErasureFinalization_EnsureSubjectAndTenantKeyedPerTenant(t *testing.T) {
	r, db := newFinalization(t)
	ctx := context.Background()
	a, ca, err := r.EnsureSubject(ctx, "tA", "crm:42", fin0)
	if err != nil {
		t.Fatal(err)
	}
	b, cb, err := r.EnsureSubject(ctx, "tB", "crm:42", fin0)
	if err != nil {
		t.Fatal(err)
	}
	if !ca || !cb || a.UUID == b.UUID || a.TenantID != "tA" || b.TenantID != "tB" || a.Kind != "subject" {
		t.Fatalf("subject rows: %+v %v / %+v %v", a, ca, b, cb)
	}
	t1, c1, err := r.EnsureTenant(ctx, "tA", fin0)
	if err != nil {
		t.Fatal(err)
	}
	t2, c2, err := r.EnsureTenant(ctx, "tA", fin0)
	if err != nil {
		t.Fatal(err)
	}
	if !c1 || c2 || t1.UUID != t2.UUID || t1.Kind != "tenant" || t1.Scope != "tenant" || t1.TenantID != "tA" {
		t.Fatalf("tenant rows: %+v %v / %+v %v", t1, c1, t2, c2)
	}
	if n := countErasures(t, db, bson.M{"kind": "tenant"}); n != 1 {
		t.Fatalf("tenant rows = %d", n)
	}
}

func TestErasureFinalization_ClaimDue_RowWithoutStatus(t *testing.T) {
	r, db := newFinalization(t)
	//tenantscope:allow system: test seeds a PR-4-shaped row
	_, err := db.Collection(CollErasures).InsertOne(context.Background(), bson.M{
		"uuid": "legacy", "scope": "tenant", "tenantId": "tA", "kind": "item",
		"corpus": "people", "sourceId": "s1", "createdAt": ms(fin0),
	})
	if err != nil {
		t.Fatal(err)
	}
	e := claimOrFatal(t, r, fin0, 2*time.Minute)
	if e.EffectiveStatus() != "pending" || e.Attempts != 0 {
		t.Fatalf("row = %+v", e)
	}
	if e.NextAttemptAt == nil || !e.NextAttemptAt.Equal(ms(fin0.Add(2*time.Minute))) {
		t.Fatalf("nextAttemptAt = %v", e.NextAttemptAt)
	}
	if again, err := r.ClaimDue(context.Background(), fin0, 2*time.Minute); err != nil || again != nil {
		t.Fatalf("second claim = %v, %v; want nil (held)", again, err)
	}
}

func TestErasureFinalization_ClaimSkipsNotDueAndDone(t *testing.T) {
	r, db := newFinalization(t)
	ctx := context.Background()
	coll := db.Collection(CollErasures)
	//tenantscope:allow system: test seeds rows
	_, err := coll.InsertMany(ctx, []any{
		bson.M{"uuid": "fencing", "scope": "tenant", "tenantId": "tA", "kind": "tenant", "status": "fencing", "attempts": 0, "nextAttemptAt": ms(fin0.Add(5 * time.Minute)), "createdAt": ms(fin0)},
		bson.M{"uuid": "done", "scope": "tenant", "tenantId": "tB", "kind": "tenant", "status": "done", "attempts": 0, "createdAt": ms(fin0)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if e, err := r.ClaimDue(ctx, fin0, time.Minute); err != nil || e != nil {
		t.Fatalf("ClaimDue(t0) = %v, %v", e, err)
	}
	e := claimOrFatal(t, r, fin0.Add(5*time.Minute), time.Minute)
	if e.UUID != "fencing" {
		t.Fatalf("claimed %s", e.UUID)
	}
	if e, err := r.ClaimOne(ctx, "done", fin0.Add(time.Hour), time.Minute); err != nil || e != nil {
		t.Fatalf("ClaimOne(done) = %v, %v", e, err)
	}
	if e, err := r.ClaimOne(ctx, "fencing", fin0.Add(5*time.Minute), time.Minute); err != nil || e != nil {
		t.Fatalf("ClaimOne(held) = %v, %v", e, err)
	}
	if e, err := r.ClaimOne(ctx, "fencing", fin0.Add(7*time.Minute), time.Minute); err != nil || e == nil {
		t.Fatalf("ClaimOne(due) = %v, %v", e, err)
	}
}

func TestErasureFinalization_Transitions(t *testing.T) {
	r, _ := newFinalization(t)
	ctx := context.Background()
	if _, _, err := r.EnsureTenant(ctx, "tA", fin0); err != nil {
		t.Fatal(err)
	}
	t1 := fin0.Add(time.Minute)
	e := claimOrFatal(t, r, fin0, time.Minute)
	if err := r.MarkFencing(ctx, e.UUID, *e.NextAttemptAt, t1, t1.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	g, _ := r.Get(ctx, e.UUID)
	if g.Status != "fencing" || !g.FirstPassAt.Equal(ms(t1)) || !g.FenceUntil.Equal(ms(t1.Add(3*time.Minute))) || !g.NextAttemptAt.Equal(ms(t1.Add(3*time.Minute))) {
		t.Fatalf("after fencing: %+v", g)
	}
	e = claimOrFatal(t, r, t1.Add(3*time.Minute), time.Minute)
	if err := r.MarkRetry(ctx, e.UUID, *e.NextAttemptAt, t1.Add(4*time.Minute)); err != nil {
		t.Fatal(err)
	}
	g, _ = r.Get(ctx, e.UUID)
	if g.Attempts != 1 || g.Status != "fencing" {
		t.Fatalf("after retry: %+v", g)
	}
	e = claimOrFatal(t, r, t1.Add(4*time.Minute), time.Minute)
	t2 := t1.Add(5 * time.Minute)
	if err := r.MarkDone(ctx, e.UUID, *e.NextAttemptAt, t2); err != nil {
		t.Fatal(err)
	}
	g, _ = r.Get(ctx, e.UUID)
	if g.Status != "done" || !g.DoneAt.Equal(ms(t2)) || g.NextAttemptAt != nil || !g.FirstPassAt.Equal(ms(t1)) {
		t.Fatalf("after done: %+v", g)
	}
}

func TestErasureFinalization_MarkDoneFromPendingFillsPasses(t *testing.T) {
	r, _ := newFinalization(t)
	ctx := context.Background()
	if _, _, err := r.EnsureUser(ctx, "u1", fin0); err != nil {
		t.Fatal(err)
	}
	e := claimOrFatal(t, r, fin0, time.Minute)
	t1 := fin0.Add(time.Second)
	if err := r.MarkDone(ctx, e.UUID, *e.NextAttemptAt, t1); err != nil {
		t.Fatal(err)
	}
	g, _ := r.Get(ctx, e.UUID)
	if g.FirstPassAt == nil || g.FenceUntil == nil || g.DoneAt == nil ||
		!g.FirstPassAt.Equal(ms(t1)) || !g.FenceUntil.Equal(ms(t1)) || !g.DoneAt.Equal(ms(t1)) {
		t.Fatalf("row = %+v", g)
	}
}

func TestErasureFinalization_StaleClaimCannotTransition(t *testing.T) {
	r, _ := newFinalization(t)
	ctx := context.Background()
	if _, _, err := r.EnsureTenant(ctx, "tA", fin0); err != nil {
		t.Fatal(err)
	}
	hold := time.Minute
	a := claimOrFatal(t, r, fin0, hold)
	b := claimOrFatal(t, r, fin0.Add(hold), hold)
	t9 := fin0.Add(time.Hour)
	if err := r.MarkRetry(ctx, a.UUID, *a.NextAttemptAt, t9); !errors.Is(err, ErrErasureClaimLost) {
		t.Fatalf("stale MarkRetry = %v, want ErrErasureClaimLost", err)
	}
	if err := r.MarkRetry(ctx, b.UUID, *b.NextAttemptAt, t9); err != nil {
		t.Fatal(err)
	}
	if g, _ := r.Get(ctx, a.UUID); g.Attempts != 1 {
		t.Fatalf("attempts = %d, want 1", g.Attempts)
	}
	if g, err := r.Get(ctx, "missing"); err != nil || g != nil {
		t.Fatalf("Get(missing) = %v, %v", g, err)
	}
}

func TestErasureFinalization_TenantTombstoneIDs(t *testing.T) {
	r, _ := newFinalization(t)
	ctx := context.Background()
	if _, _, err := r.EnsureTenant(ctx, "tA", fin0); err != nil {
		t.Fatal(err)
	}
	// Other kinds for tB must not count as a tenant tombstone.
	if _, _, err := r.EnsureSubject(ctx, "tB", "crm:1", fin0); err != nil {
		t.Fatal(err)
	}
	got, err := r.TenantTombstoneIDs(ctx, []string{"tA", "tB"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !got["tA"] {
		t.Fatalf("got %v, want {tA: true}", got)
	}
	if got, err := r.TenantTombstoneIDs(ctx, nil); err != nil || len(got) != 0 {
		t.Fatalf("empty input = %v, %v", got, err)
	}
	// More ids than one chunk still resolve.
	ids := make([]string, 0, tenantTombstoneChunk+2)
	for i := 0; i < tenantTombstoneChunk+1; i++ {
		ids = append(ids, fmt.Sprintf("x%d", i))
	}
	ids = append(ids, "tA")
	if got, err := r.TenantTombstoneIDs(ctx, ids); err != nil || !got["tA"] || len(got) != 1 {
		t.Fatalf("chunked = %v, %v", got, err)
	}
}
