package repository

// TestListMembershipsByUser_DeterministicOrder pins Task 6.2's fix: before
// this task, ListMembershipsByUser issued a Find with no sort, so "the
// first owned membership" — how the JWT tenant fallback was picked — was at
// the mercy of MongoDB's natural order, which two tokens minted for the
// same user could disagree on. The repository now sorts joinedAt ascending,
// then the membership's tenant identifier (persisted field tenantId)
// ascending as a stable tie-break.
//
// Requires a genuine MongoDB (MONGO_TEST_URI) — see search_test.go's
// newTestDB for the skip-when-unset behaviour.

import (
	"context"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// seedMembershipAt inserts a (userUUID, tenantUUID) membership row with an
// explicit joinedAt, so a test can control ordering without sleeping
// between inserts. Mirrors seedMembership (search_test.go) but exposes the
// timestamp so ties and out-of-order inserts can be constructed on demand.
func seedMembershipAt(t *testing.T, db *mongo.Database, userUUID, tenantUUID string, joinedAt time.Time) {
	t.Helper()
	doc := bson.M{
		"uuid":     "membership-" + randHex(t, 4),
		"userUUID": userUUID,
		"tenantId": tenantUUID,
		"roles":    []string{"org_member"},
		"joinedAt": joinedAt,
	}
	if _, err := db.Collection(CollMemberships).InsertOne(context.Background(), doc); err != nil {
		t.Fatalf("seed membership: %v", err)
	}
}

// TestListMembershipsByUser_DeterministicOrder inserts three memberships
// out of Mongo natural-insertion order: two share the SAME joinedAt (tie),
// one joins later. The expected order is joinedAt asc, then tenantId asc
// for the tied pair.
func TestListMembershipsByUser_DeterministicOrder(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()
	r := New(db)
	ctx := context.Background()
	userUUID := "user-" + randHex(t, 4)

	base := time.Now().Add(-time.Hour).Truncate(time.Second)

	// Insertion order deliberately scrambled relative to the expected
	// output order: tenant-C (joins latest) is inserted FIRST, then the
	// tied pair in tenantId-descending order (B before A).
	seedMembershipAt(t, db, userUUID, "tenant-C", base.Add(time.Minute))
	seedMembershipAt(t, db, userUUID, "tenant-B", base) // tie with tenant-A
	seedMembershipAt(t, db, userUUID, "tenant-A", base) // tie with tenant-B, tenantId asc breaks it first

	got, err := r.ListMembershipsByUser(ctx, userUUID)
	if err != nil {
		t.Fatalf("ListMembershipsByUser: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d memberships, want 3: %+v", len(got), got)
	}
	wantOrder := []string{"tenant-A", "tenant-B", "tenant-C"}
	gotOrder := make([]string, len(got))
	for i, m := range got {
		gotOrder[i] = m.TenantUUID
	}
	for i, want := range wantOrder {
		if gotOrder[i] != want {
			t.Fatalf("ListMembershipsByUser order = %v, want %v (joinedAt asc, tenantId asc tie-break)", gotOrder, wantOrder)
		}
	}
}

// TestListMembershipsByUser_DeterministicOrder_RepeatedCallsAgree drives
// the same query twice against the same seeded data and asserts identical
// order both times. This is the property that actually mattered in
// production: two tokens minted back-to-back for the same user must pick
// the same tenant fallback, which requires the repository's order to be
// deterministic, not just "some order".
func TestListMembershipsByUser_DeterministicOrder_RepeatedCallsAgree(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()
	r := New(db)
	ctx := context.Background()
	userUUID := "user-" + randHex(t, 4)

	base := time.Now().Add(-time.Hour).Truncate(time.Second)
	seedMembershipAt(t, db, userUUID, "tenant-Z", base.Add(2*time.Minute))
	seedMembershipAt(t, db, userUUID, "tenant-M", base.Add(time.Minute))
	seedMembershipAt(t, db, userUUID, "tenant-A", base)

	first, err := r.ListMembershipsByUser(ctx, userUUID)
	if err != nil {
		t.Fatalf("ListMembershipsByUser (1st): %v", err)
	}
	second, err := r.ListMembershipsByUser(ctx, userUUID)
	if err != nil {
		t.Fatalf("ListMembershipsByUser (2nd): %v", err)
	}
	if len(first) != len(second) {
		t.Fatalf("call count mismatch: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i].TenantUUID != second[i].TenantUUID {
			t.Fatalf("order disagreement at index %d: %q vs %q", i, first[i].TenantUUID, second[i].TenantUUID)
		}
	}
	want := []string{"tenant-A", "tenant-M", "tenant-Z"}
	for i, w := range want {
		if first[i].TenantUUID != w {
			t.Fatalf("order = %v, want %v", first, want)
		}
	}
}
