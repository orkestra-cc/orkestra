package repository

import (
	"context"
	"sort"
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

func TestGrants_ReplaceIsScopedAndConverges(t *testing.T) {
	db := newTestDB(t)
	// Unique (tenant, model, user): the index Replace relies on for races.
	if _, err := db.Collection(CollGrants).Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys:    bson.D{{Key: "tenantId", Value: 1}, {Key: "modelUuid", Value: 1}, {Key: "userUuid", Value: 1}},
		Options: options.Index().SetUnique(true),
	}); err != nil {
		t.Fatal(err)
	}
	repo := NewGrants(db)
	t1, t2 := ctxFor("t1"), ctxFor("t2")

	if err := repo.Replace(t1, "m1", "admin", []string{"u1", "u2", "u2"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Replace(t2, "m1", "admin", []string{"u9"}); err != nil {
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
	if err := repo.Replace(t1, "m1", "other-admin", []string{"u1", "u3"}); err != nil {
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
	if err := repo.Replace(t1, "m1", "admin", nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := repo.ListByModel(t1, "m1"); len(got) != 0 {
		t.Fatalf("expected cleared, got %v", users(got))
	}
}

// The bulk insert Replace uses is unordered: a duplicate (a grant that a
// racing PUT created between Replace's read and its insert) must neither
// stop the remaining rows nor be classified as a real failure.
func TestGrants_UnorderedInsertSurvivesDuplicate(t *testing.T) {
	db := newTestDB(t)
	if _, err := db.Collection(CollGrants).Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys:    bson.D{{Key: "tenantId", Value: 1}, {Key: "modelUuid", Value: 1}, {Key: "userUuid", Value: 1}},
		Options: options.Index().SetUnique(true),
	}); err != nil {
		t.Fatal(err)
	}
	repo := NewGrants(db)
	t1 := ctxFor("t1")
	// The racer already wrote u1; the stale writer then bulk-inserts [u1,u2].
	_, err := db.Collection(CollGrants).InsertOne(context.Background(), models.LLMGrant{UUID: "pre", TenantID: "t1", ModelUUID: "m1", UserUUID: "u1", GrantedBy: "racer"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Collection(CollGrants).InsertMany(context.Background(),
		[]any{models.LLMGrant{UUID: "x1", TenantID: "t1", ModelUUID: "m1", UserUUID: "u1"}, models.LLMGrant{UUID: "x2", TenantID: "t1", ModelUUID: "m1", UserUUID: "u2"}},
		options.InsertMany().SetOrdered(false))
	if err == nil || !onlyDuplicateKeyErrors(err) {
		t.Fatalf("expected duplicate-only bulk error, got %v", err)
	}
	got, _ := repo.ListByModel(t1, "m1")
	if !equalStrings(users(got), []string{"u1", "u2"}) {
		t.Fatalf("unordered insert must keep u2: %v", users(got))
	}
	if onlyDuplicateKeyErrors(context.DeadlineExceeded) {
		t.Fatal("non-bulk errors must not be swallowed")
	}
}

func TestGrants_DeleteByModelIsScoped(t *testing.T) {
	db := newTestDB(t)
	repo := NewGrants(db)
	_ = repo.Replace(ctxFor("t1"), "m1", "a", []string{"u1"})
	_ = repo.Replace(ctxFor("t2"), "m1", "a", []string{"u1"})
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
	repo := NewGrants(db)
	bg := context.Background()
	// subject is grantee in t1 and t2, and granter of "other" in t1 and t2.
	_ = repo.Replace(ctxFor("t1"), "m1", "admin", []string{"subject", "keep"})
	_ = repo.Replace(ctxFor("t2"), "m2", "admin", []string{"subject"})
	_ = repo.Replace(ctxFor("t1"), "m3", "subject", []string{"other"})
	_ = repo.Replace(ctxFor("t2"), "m4", "subject", []string{"other"})

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
