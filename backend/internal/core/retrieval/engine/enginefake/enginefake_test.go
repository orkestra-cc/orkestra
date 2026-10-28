package enginefake

import (
	"context"
	"errors"
	"testing"

	"github.com/orkestra/backend/internal/core/retrieval/engine"
)

func pt(id, tenantID, sourceID string, rev int64, subjects ...string) engine.Point {
	return engine.Point{
		ID:    id,
		Dense: []float32{1, 0},
		Text:  id,
		Runtime: engine.Runtime{
			TenantID: tenantID, SourceID: sourceID, SourceRevision: rev,
			VisibleTo: []string{"tenant:*"}, Subjects: subjects,
		},
	}
}

func ids(pts []engine.Point) []string {
	out := make([]string, 0, len(pts))
	for _, p := range pts {
		out = append(out, p.ID)
	}
	return out
}

func TestEngineFake_DeleteAndCountBySelector(t *testing.T) {
	ctx := context.Background()
	e := New()
	e.Seed("c1",
		pt("p1", "tA", "s1", 1, "user:u1"),
		pt("p2", "tB", "s2", 1, "user:u1", "crm:9"),
		pt("p3", "tA", "s3", 1, "crm:9"),
	)

	if err := e.DeleteByFilter(ctx, "c1", engine.Selector{Subject: "user:u1"}); err != nil {
		t.Fatalf("DeleteByFilter(user:u1) = %v", err)
	}
	if got := ids(e.Points("c1")); len(got) != 1 || got[0] != "p3" {
		t.Fatalf("after delete, points = %v, want [p3]", got)
	}
	if n, err := e.Count(ctx, "c1", engine.Selector{TenantID: "tA"}); err != nil || n != 1 {
		t.Fatalf("Count(tA) = %d, %v; want 1", n, err)
	}
	if n, err := e.Count(ctx, "c1", engine.Selector{TenantID: "tB"}); err != nil || n != 0 {
		t.Fatalf("Count(tB) = %d, %v; want 0", n, err)
	}

	if err := e.DeleteByFilter(ctx, "missing", engine.Selector{TenantID: "tA"}); !errors.Is(err, engine.ErrNotFound) {
		t.Fatalf("DeleteByFilter(missing) = %v, want engine.ErrNotFound", err)
	}
	if _, err := e.Count(ctx, "missing", engine.Selector{TenantID: "tA"}); !errors.Is(err, engine.ErrNotFound) {
		t.Fatalf("Count(missing) = %v, want engine.ErrNotFound", err)
	}
	if err := e.DeleteByFilter(ctx, "c1", engine.Selector{}); err == nil || errors.Is(err, engine.ErrNotFound) {
		t.Fatalf("DeleteByFilter(Selector{}) = %v, want a refusal", err)
	}
	if _, err := e.Count(ctx, "c1", engine.Selector{}); err == nil || errors.Is(err, engine.ErrNotFound) {
		t.Fatalf("Count(Selector{}) = %v, want a refusal", err)
	}
	if got := ids(e.Points("c1")); len(got) != 1 {
		t.Fatalf("a refused delete changed the collection: %v", got)
	}
}

func TestEngineFake_SelectorFailsClosed(t *testing.T) {
	ctx := context.Background()
	e := New()
	e.Seed("c1", pt("p1", "tA", "s1", 1, "user:x"), pt("p2", "tB", "s1", 1, "user:x"))
	cases := map[string]engine.Selector{
		"zero":                     {},
		"empty non-nil SourceIDs":  {TenantID: "tA", SourceIDs: []string{}},
		"empty source id":          {TenantID: "tA", SourceIDs: []string{""}},
		"SourceIDs without tenant": {SourceIDs: []string{"s1"}, Subject: "user:x"},
	}
	for name, sel := range cases {
		t.Run(name, func(t *testing.T) {
			if err := e.DeleteByFilter(ctx, "c1", sel); err == nil || errors.Is(err, engine.ErrNotFound) {
				t.Errorf("DeleteByFilter = %v, want a refusal", err)
			}
			if _, err := e.Count(ctx, "c1", sel); err == nil || errors.Is(err, engine.ErrNotFound) {
				t.Errorf("Count = %v, want a refusal", err)
			}
			// Refused before the collection is looked up.
			if err := e.DeleteByFilter(ctx, "missing", sel); err == nil || errors.Is(err, engine.ErrNotFound) {
				t.Errorf("DeleteByFilter(missing) = %v, want a refusal", err)
			}
		})
	}
	if n := len(e.Points("c1")); n != 2 {
		t.Fatalf("refused selectors deleted points: %d left, want 2", n)
	}
}

func TestEngineFake_UpsertKeepsHigherRevision(t *testing.T) {
	ctx := context.Background()
	e := New()
	if err := e.EnsureCollection(ctx, engine.CollectionSpec{Name: "c1", Dimensions: 2}); err != nil {
		t.Fatal(err)
	}
	p := pt("p1", "tA", "s1", 2)
	p.Text = "rev2"
	if err := e.Upsert(ctx, "c1", []engine.Point{p}); err != nil {
		t.Fatal(err)
	}
	old := pt("p1", "tA", "s1", 1)
	old.Text = "rev1"
	if err := e.Upsert(ctx, "c1", []engine.Point{old}); err != nil {
		t.Fatal(err)
	}
	if got := e.Points("c1"); len(got) != 1 || got[0].Text != "rev2" {
		t.Fatalf("after a lower revision, points = %+v, want rev2 kept", got)
	}
	newer := pt("p1", "tA", "s1", 3)
	newer.Text = "rev3"
	if err := e.Upsert(ctx, "c1", []engine.Point{newer}); err != nil {
		t.Fatal(err)
	}
	if got := e.Points("c1"); len(got) != 1 || got[0].Text != "rev3" {
		t.Fatalf("after a higher revision, points = %+v, want rev3", got)
	}
	if err := e.Upsert(ctx, "c1", []engine.Point{{ID: "nv", Runtime: engine.Runtime{TenantID: "tA"}}}); err == nil {
		t.Fatal("Upsert of a point without a vector = nil, want an error")
	}
	if err := e.Upsert(ctx, "missing", []engine.Point{p}); !errors.Is(err, engine.ErrNotFound) {
		t.Fatalf("Upsert(missing) = %v, want engine.ErrNotFound", err)
	}
}

func TestEngineFake_SeedCopiesAndFailureSwitches(t *testing.T) {
	ctx := context.Background()
	e := New()
	p := pt("p1", "tA", "s1", 5, "user:u1")
	e.Seed("c1", p)
	p.Runtime.Subjects[0] = "mutated"
	if got := e.Points("c1")[0].Runtime.Subjects[0]; got != "user:u1" {
		t.Fatalf("Seed kept a caller-owned slice: %q", got)
	}
	// Seed stores unconditionally, even a lower revision.
	e.Seed("c1", pt("p1", "tA", "s1", 1))
	if got := e.Points("c1")[0].Runtime.SourceRevision; got != 1 {
		t.Fatalf("Seed revision = %d, want 1", got)
	}

	boom := errors.New("boom")
	sel := engine.Selector{TenantID: "tA"}
	e.FailDeletes(boom)
	if err := e.DeleteByFilter(ctx, "c1", sel); !errors.Is(err, boom) {
		t.Fatalf("DeleteByFilter with FailDeletes = %v", err)
	}
	e.FailDeletes(nil)
	e.FailCounts(boom)
	if _, err := e.Count(ctx, "c1", sel); !errors.Is(err, boom) {
		t.Fatalf("Count with FailCounts = %v", err)
	}
	e.FailCounts(nil)
	e.IgnoreDeletes(true)
	if err := e.DeleteByFilter(ctx, "c1", sel); err != nil {
		t.Fatalf("DeleteByFilter with IgnoreDeletes = %v", err)
	}
	if n, _ := e.Count(ctx, "c1", sel); n != 1 {
		t.Fatalf("IgnoreDeletes deleted: count %d", n)
	}
	e.IgnoreDeletes(false)
	if err := e.DeleteByFilter(ctx, "c1", sel); err != nil {
		t.Fatal(err)
	}
	if n, _ := e.Count(ctx, "c1", sel); n != 0 {
		t.Fatalf("count after delete = %d, want 0", n)
	}
}

func TestEngineFake_QueryKeepsTenantAndPrincipals(t *testing.T) {
	ctx := context.Background()
	e := New()
	a1 := pt("a1", "tA", "s1", 1)
	a2 := pt("a2", "tA", "s2", 1)
	a2.Runtime.VisibleTo = []string{"user:bob"}
	a3 := pt("a3", "tA", "s3", 1)
	b1 := pt("b1", "tB", "s1", 1)
	e.Seed("c1", a1, a2, a3, b1)

	q := engine.Query{TenantID: "tA", Principals: []string{"tenant:*"}, Keyword: "x", Weights: engine.Weights{Keyword: 1}, Limit: 10}
	got, err := e.Query(ctx, "c1", q)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].PointID != "a1" || got[1].PointID != "a3" {
		t.Fatalf("Query = %+v, want [a1 a3]", got)
	}
	if got[0].Score != 1 || got[1].Score != 0.5 {
		t.Fatalf("scores = %v, %v; want 1, 0.5", got[0].Score, got[1].Score)
	}
	q.Limit = 1
	if got, _ := e.Query(ctx, "c1", q); len(got) != 1 {
		t.Fatalf("Limit 1 returned %d", len(got))
	}
	for name, bad := range map[string]engine.Query{
		"no tenant":       {Principals: []string{"tenant:*"}, Limit: 1},
		"no principals":   {TenantID: "tA", Limit: 1},
		"empty principal": {TenantID: "tA", Principals: []string{""}, Limit: 1},
	} {
		if _, err := e.Query(ctx, "c1", bad); err == nil {
			t.Errorf("Query(%s) = nil error, want a refusal", name)
		}
	}
	if _, err := e.Query(ctx, "missing", q); !errors.Is(err, engine.ErrNotFound) {
		t.Fatalf("Query(missing) = %v, want engine.ErrNotFound", err)
	}
}

func TestEngineFake_PayloadAndRevisionsAndCollections(t *testing.T) {
	ctx := context.Background()
	e := New()
	p := pt("p1", "tA", "s1", 2)
	p.Runtime.ContentHash = "h"
	e.Seed("c1", p, pt("old", "tA", "s1", 1), pt("other", "tA", "s2", 1))

	upd := pt("p1", "tA", "s1", 2)
	upd.Runtime.ContentHash = "h"
	upd.Runtime.VisibleTo = []string{"user:new"}
	stale := pt("p1", "tA", "s1", 1)
	stale.Runtime.ContentHash = "h"
	stale.Runtime.VisibleTo = []string{"user:stale"}
	ghost := pt("ghost", "tA", "s9", 1)
	if err := e.UpdatePayload(ctx, "c1", []engine.Point{upd, stale, ghost}); err != nil {
		t.Fatal(err)
	}
	pts := e.Points("c1")
	if len(pts) != 3 || pts[0].Runtime.VisibleTo[0] != "user:new" {
		t.Fatalf("UpdatePayload: %+v", pts)
	}

	if err := e.DeleteOtherRevisions(ctx, "c1", "tA", "s1", 2); err != nil {
		t.Fatal(err)
	}
	if got := ids(e.Points("c1")); len(got) != 2 || got[0] != "p1" || got[1] != "other" {
		t.Fatalf("DeleteOtherRevisions left %v, want [p1 other]", got)
	}

	if err := e.Health(ctx); err != nil {
		t.Fatal(err)
	}
	if err := e.DropCollection(ctx, "c1"); err != nil {
		t.Fatal(err)
	}
	if err := e.DropCollection(ctx, "c1"); err != nil {
		t.Fatalf("DropCollection(missing) = %v, want nil", err)
	}
	if _, err := e.Count(ctx, "c1", engine.Selector{TenantID: "tA"}); !errors.Is(err, engine.ErrNotFound) {
		t.Fatalf("Count after drop = %v, want engine.ErrNotFound", err)
	}
	if e.Points("c1") != nil {
		t.Fatal("Points of a dropped collection is not nil")
	}
}
