package repository

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"sync"
	"testing"

	"github.com/orkestra/backend/internal/core/llm/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func users(gs []models.LLMGrant) []string {
	out := []string{}
	for _, g := range gs {
		out = append(out, g.UserUUID)
	}
	sort.Strings(out)
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// seedModels inserts one model per (tenant, uuid) pair: Replace writes the
// model's access in the same transaction and refuses an unknown model.
func seedModels(t *testing.T, db *mongo.Database, pairs ...[2]string) {
	t.Helper()
	repo := NewModels(db)
	for _, p := range pairs {
		if err := repo.Insert(ctxFor(p[0]), newModel(p[1], p[1], "c1", models.ModelStatusActive, "u")); err != nil {
			t.Fatalf("seed model %s/%s: %v", p[0], p[1], err)
		}
	}
}

func ensureGrantIndex(t *testing.T, db *mongo.Database) {
	t.Helper()
	if _, err := db.Collection(CollGrants).Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys:    bson.D{{Key: "tenantId", Value: 1}, {Key: "modelUuid", Value: 1}, {Key: "userUuid", Value: 1}},
		Options: options.Index().SetUnique(true),
	}); err != nil {
		t.Fatal(err)
	}
}

func TestGrants_ReplaceIsScopedAndConverges(t *testing.T) {
	db := newTestDB(t)
	ensureGrantIndex(t, db)
	seedModels(t, db, [2]string{"t1", "m1"}, [2]string{"t2", "m1"})
	repo := NewGrants(db)
	t1, t2 := ctxFor("t1"), ctxFor("t2")

	if err := repo.Replace(t1, "m1", models.AccessGranted, "admin", []string{"u1", "u2", "u2"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Replace(t2, "m1", models.AccessGranted, "admin", []string{"u9"}); err != nil {
		t.Fatal(err)
	}
	got, err := repo.ListByModel(t1, "m1")
	if err != nil || !equalStrings(users(got), []string{"u1", "u2"}) {
		t.Fatalf("t1 grants = %v, %v", users(got), err)
	}
	// Replace again: u1 stays (not re-inserted), u2 dropped, u3 added.
	var u1UUID string
	for _, g := range got {
		if g.UserUUID == "u1" {
			u1UUID = g.UUID
		}
	}
	if err := repo.Replace(t1, "m1", models.AccessGranted, "other-admin", []string{"u1", "u3"}); err != nil {
		t.Fatal(err)
	}
	got, _ = repo.ListByModel(t1, "m1")
	if !equalStrings(users(got), []string{"u1", "u3"}) {
		t.Fatalf("after replace = %v", users(got))
	}
	for _, g := range got {
		if g.UserUUID == "u1" && (g.UUID != u1UUID || g.GrantedBy != "admin") {
			t.Fatalf("existing grant must be untouched: %+v", g)
		}
		if g.UserUUID == "u3" && g.GrantedBy != "other-admin" {
			t.Fatalf("new grant actor: %+v", g)
		}
	}
	// Other tenant untouched; empty set clears.
	if other, _ := repo.ListByModel(t2, "m1"); !equalStrings(users(other), []string{"u9"}) {
		t.Fatalf("t2 grants = %v", users(other))
	}
	if err := repo.Replace(t1, "m1", models.AccessGranted, "admin", nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := repo.ListByModel(t1, "m1"); len(got) != 0 {
		t.Fatalf("expected cleared, got %v", users(got))
	}
}

func TestGrants_DeleteByModelIsScoped(t *testing.T) {
	db := newTestDB(t)
	seedModels(t, db, [2]string{"t1", "m1"}, [2]string{"t2", "m1"})
	repo := NewGrants(db)
	if err := repo.Replace(ctxFor("t1"), "m1", models.AccessGranted, "a", []string{"u1"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Replace(ctxFor("t2"), "m1", models.AccessGranted, "a", []string{"u1"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteByModel(ctxFor("t1"), "m1"); err != nil {
		t.Fatal(err)
	}
	if g, _ := repo.ListByModel(ctxFor("t1"), "m1"); len(g) != 0 {
		t.Fatalf("t1 not cleared: %v", g)
	}
	if g, _ := repo.ListByModel(ctxFor("t2"), "m1"); len(g) != 1 {
		t.Fatalf("t2 must survive: %v", g)
	}
}

func TestGrants_DSRSubjectMethods(t *testing.T) {
	db := newTestDB(t)
	seedModels(t, db, [2]string{"t1", "m1"}, [2]string{"t2", "m2"}, [2]string{"t1", "m3"}, [2]string{"t2", "m4"})
	repo := NewGrants(db)
	bg := context.Background()
	// subject is grantee in t1 and t2, and granter of "other" in t1 and t2.
	for _, x := range []struct {
		tenant, model, actor string
		users                []string
	}{
		{"t1", "m1", "admin", []string{"subject", "keep"}},
		{"t2", "m2", "admin", []string{"subject"}},
		{"t1", "m3", "subject", []string{"other"}},
		{"t2", "m4", "subject", []string{"other"}},
	} {
		if err := repo.Replace(ctxFor(x.tenant), x.model, models.AccessGranted, x.actor, x.users); err != nil {
			t.Fatal(err)
		}
	}

	// By user (grantee).
	list, err := repo.ListByUserAllTenants(bg, "subject")
	if err != nil || len(list) != 2 {
		t.Fatalf("ListByUserAllTenants = %+v, %v", list, err)
	}
	n, err := repo.DeleteByUser(bg, "subject")
	if err != nil || n != 2 {
		t.Fatalf("DeleteByUser = %d, %v", n, err)
	}
	if g, _ := repo.ListByModel(ctxFor("t1"), "m1"); !equalStrings(users(g), []string{"keep"}) {
		t.Fatalf("keep must survive: %v", users(g))
	}

	// By granter (actor).
	gl, err := repo.ListByGranter(bg, "subject")
	if err != nil || len(gl) != 2 {
		t.Fatalf("ListByGranter = %+v, %v", gl, err)
	}
	n, err = repo.PseudonymizeGranter(bg, "subject")
	if err != nil || n != 2 {
		t.Fatalf("PseudonymizeGranter = %d, %v", n, err)
	}
	if left, _ := repo.ListByGranter(bg, "subject"); len(left) != 0 {
		t.Fatalf("subject still present as granter: %+v", left)
	}
	for _, x := range []struct{ tenant, model, wantBy string }{{"t1", "m3", models.ErasedActor}, {"t2", "m4", models.ErasedActor}, {"t1", "m1", "admin"}} {
		g, _ := repo.ListByModel(ctxFor(x.tenant), x.model)
		if len(g) != 1 || g[0].GrantedBy != x.wantBy {
			t.Fatalf("%s/%s grantedBy = %+v, want %q", x.tenant, x.model, g, x.wantBy)
		}
	}
}

// TestGrants_ReplaceWritesAccessAndOnlyItsModelFields: the transaction sets
// access, updatedAt and the grants revision on the model and nothing else;
// an unknown or foreign model is ErrNotFound and writes no grant.
func TestGrants_ReplaceWritesAccessAndOnlyItsModelFields(t *testing.T) {
	db := newTestDB(t)
	ensureGrantIndex(t, db)
	seedModels(t, db, [2]string{"t1", "m1"}, [2]string{"t2", "m2"})
	mods, repo := NewModels(db), NewGrants(db)
	t1, t2 := ctxFor("t1"), ctxFor("t2")
	before, err := mods.Get(t1, "m1")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Replace(t1, "m1", models.AccessEveryone, "admin", []string{"u1"}); err != nil {
		t.Fatal(err)
	}
	after, err := mods.Get(t1, "m1")
	if err != nil {
		t.Fatal(err)
	}
	if after.Access != models.AccessEveryone || after.GrantsRevision != 1 || !after.UpdatedAt.After(before.UpdatedAt) {
		t.Fatalf("access %q revision %d updatedAt %v (before %v)", after.Access, after.GrantsRevision, after.UpdatedAt, before.UpdatedAt)
	}
	restored := *after
	restored.Access, restored.UpdatedAt, restored.GrantsRevision = before.Access, before.UpdatedAt, before.GrantsRevision
	if !reflect.DeepEqual(restored, *before) {
		t.Fatalf("Replace changed more than access:\n before %+v\n after  %+v", before, after)
	}
	// Same value again is still written (the revision moves).
	if err := repo.Replace(t1, "m1", models.AccessEveryone, "admin", []string{"u1"}); err != nil {
		t.Fatal(err)
	}
	if again, _ := mods.Get(t1, "m1"); again.GrantsRevision != 2 {
		t.Fatalf("revision = %d, want 2", again.GrantsRevision)
	}

	for _, c := range []struct {
		ctx   context.Context
		model string
	}{{t2, "m1"}, {t1, "nope"}} {
		if err := repo.Replace(c.ctx, c.model, models.AccessEveryone, "admin", []string{"u7"}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Replace(%s) err = %v, want ErrNotFound", c.model, err)
		}
	}
	if g, _ := repo.ListByModel(t2, "m1"); len(g) != 0 {
		t.Fatalf("a refused Replace wrote grants: %v", users(g))
	}
	if other, _ := mods.Get(t2, "m2"); other.Access != models.AccessGranted || other.GrantsRevision != 0 {
		t.Fatalf("other org's model touched: %+v", other)
	}
}

// TestGrants_ConcurrentReplaceIsSerialized: two admins replace the grants
// of a model with no grants at the same time, with disjoint lists and
// different access. Without serialization both delete, both read an empty
// list and both insert, leaving the union. The transaction must end with
// exactly one caller's request — its list and its access together.
func TestGrants_ConcurrentReplaceIsSerialized(t *testing.T) {
	db := newTestDB(t)
	ensureGrantIndex(t, db)
	seedModels(t, db, [2]string{"t1", "m1"})
	mods, repo := NewModels(db), NewGrants(db)
	ctx := ctxFor("t1")
	type req struct {
		access string
		users  []string
	}
	reqs := []req{{models.AccessGranted, []string{"u1"}}, {models.AccessEveryone, []string{"u2"}}}
	for i := 0; i < 10; i++ {
		if err := repo.Replace(ctx, "m1", models.AccessGranted, "admin", nil); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		start := make(chan struct{})
		errs := make([]error, len(reqs))
		for k, r := range reqs {
			wg.Add(1)
			go func(k int, r req) {
				defer wg.Done()
				<-start
				errs[k] = repo.Replace(ctx, "m1", r.access, "admin", r.users)
			}(k, r)
		}
		close(start)
		wg.Wait()
		for k, err := range errs {
			if err != nil {
				t.Fatalf("iteration %d: Replace %d: %v", i, k, err)
			}
		}
		got, err := repo.ListByModel(ctx, "m1")
		if err != nil {
			t.Fatal(err)
		}
		m, err := mods.Get(ctx, "m1")
		if err != nil {
			t.Fatal(err)
		}
		gotUsers := users(got)
		ok := false
		for _, r := range reqs {
			if equalStrings(gotUsers, r.users) && m.Access == r.access {
				ok = true
			}
		}
		if !ok {
			t.Fatalf("iteration %d: ended with access %q grants %v, want exactly one request's state", i, m.Access, gotUsers)
		}
	}
}
