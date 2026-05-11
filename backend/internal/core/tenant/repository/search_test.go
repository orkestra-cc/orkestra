package repository

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/tenant/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// TestUserCollectionForKind is a pure unit test for the helper that picks
// the tier-appropriate user collection for the SearchTenantsByQ join. Locks
// the kind→collection mapping so a future tier-rename can't silently route
// the lookup at the wrong collection.
func TestUserCollectionForKind(t *testing.T) {
	t.Parallel()
	cases := []struct {
		kind models.TenantKind
		want string
	}{
		{models.TenantKindInternal, "operator_users"},
		{models.TenantKindExternal, "client_users"},
		{"", ""},
		{models.TenantKind("nonsense"), ""},
	}
	for _, c := range cases {
		c := c
		t.Run(string(c.kind), func(t *testing.T) {
			t.Parallel()
			if got := userCollectionForKind(c.kind); got != c.want {
				t.Fatalf("userCollectionForKind(%q) = %q, want %q", c.kind, got, c.want)
			}
		})
	}
}

// --- Mongo integration tests ----------------------------------------------
//
// Every test below skips when MONGO_TEST_URI isn't set so a developer
// running `go test ./...` without the env var sees a clean PASS [skipped].
// CI sets MONGO_TEST_URI=mongodb://admin:<pw>@localhost:27017 and runs the
// integration suite.

const mongoTestURIEnv = "MONGO_TEST_URI"

// newTestDB spins up an isolated database for one test. Each test gets a
// unique name (random suffix) so parallel tests don't collide. The database
// is dropped on teardown.
func newTestDB(t *testing.T) (*mongo.Database, func()) {
	t.Helper()
	uri := os.Getenv(mongoTestURIEnv)
	if uri == "" {
		t.Skipf("skipping integration test: set %s to run (e.g. mongodb://admin:<pw>@localhost:27017)", mongoTestURIEnv)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("mongo connect: %v", err)
	}
	if err := client.Ping(ctx, nil); err != nil {
		t.Fatalf("mongo ping: %v", err)
	}
	suffix := make([]byte, 4)
	_, _ = rand.Read(suffix)
	dbName := "orkestra_test_search_" + hex.EncodeToString(suffix)
	db := client.Database(dbName)
	cleanup := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = db.Drop(ctx)
		_ = client.Disconnect(ctx)
	}
	return db, cleanup
}

// seedTenant inserts a Tenant row with reasonable defaults; overrides must
// be applied via the `with` callback.
func seedTenant(t *testing.T, db *mongo.Database, with func(*models.Tenant)) *models.Tenant {
	t.Helper()
	tn := &models.Tenant{
		UUID:      "tenant-" + randHex(t, 4),
		Kind:      models.TenantKindExternal,
		Status:    models.TenantStatusActive,
		Name:      "Tenant " + randHex(t, 2),
		Slug:      "slug-" + randHex(t, 2),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if with != nil {
		with(tn)
	}
	if _, err := db.Collection(CollTenants).InsertOne(context.Background(), tn); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	return tn
}

// seedMembership wires a (userUUID, tenantUUID) pair into tenant_memberships.
func seedMembership(t *testing.T, db *mongo.Database, userUUID, tenantUUID string) {
	t.Helper()
	doc := bson.M{
		"uuid":     "membership-" + randHex(t, 4),
		"userUUID": userUUID,
		"tenantId": tenantUUID,
		"roles":    []string{"org_member"},
		"joinedAt": time.Now(),
	}
	if _, err := db.Collection(CollMemberships).InsertOne(context.Background(), doc); err != nil {
		t.Fatalf("seed membership: %v", err)
	}
}

// seedUser inserts a user row into either client_users or operator_users.
// Uses bson.M directly so the test isn't coupled to the User struct's
// dozens of unrelated fields.
func seedUser(t *testing.T, db *mongo.Database, collection, userUUID, email, fullName, username string, deleted bool) {
	t.Helper()
	doc := bson.M{
		"uuid":      userUUID,
		"email":     email,
		"fullName":  fullName,
		"username":  username,
		"createdAt": time.Now(),
	}
	if deleted {
		doc["deletedAt"] = time.Now()
	}
	if _, err := db.Collection(collection).InsertOne(context.Background(), doc); err != nil {
		t.Fatalf("seed user (%s): %v", collection, err)
	}
}

func randHex(t *testing.T, n int) string {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return hex.EncodeToString(b)
}

// uuids extracts the tenant UUIDs from a search result, sorted, so equality
// assertions don't depend on ordering when the test isn't asserting sort.
func uuids(rs []TenantSearchResult) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.Tenant.UUID
	}
	sort.Strings(out)
	return out
}

// --- Tenant-side matching --------------------------------------------------

func TestSearchTenantsByQ_TenantNameMatch_CaseInsensitive(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()
	r := New(db)
	wanted := seedTenant(t, db, func(tn *models.Tenant) { tn.Name = "Acme S.r.l." })
	_ = seedTenant(t, db, func(tn *models.Tenant) { tn.Name = "Globex" })

	got, err := r.SearchTenantsByQ(context.Background(), TenantListFilter{Kind: models.TenantKindExternal, Q: "ACME"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(got) != 1 || got[0].Tenant.UUID != wanted.UUID {
		t.Fatalf("got %+v, want only %s", uuids(got), wanted.UUID)
	}
	if len(got[0].MatchedMembers) != 0 {
		t.Fatalf("MatchedMembers = %+v, want empty (tenant matched, not a member)", got[0].MatchedMembers)
	}
}

func TestSearchTenantsByQ_TenantSlugMatch(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()
	r := New(db)
	wanted := seedTenant(t, db, func(tn *models.Tenant) { tn.Slug = "rossi-srl" })
	_ = seedTenant(t, db, func(tn *models.Tenant) { tn.Slug = "globex" })

	got, err := r.SearchTenantsByQ(context.Background(), TenantListFilter{Kind: models.TenantKindExternal, Q: "rossi"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(got) != 1 || got[0].Tenant.UUID != wanted.UUID {
		t.Fatalf("got %+v, want only %s", uuids(got), wanted.UUID)
	}
}

// --- Member-side matching --------------------------------------------------

func TestSearchTenantsByQ_MemberEmailMatch_External(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()
	r := New(db)
	tn := seedTenant(t, db, nil)
	other := seedTenant(t, db, nil)
	seedUser(t, db, "client_users", "u-1", "alice@example.com", "Alice Bianchi", "alice", false)
	seedUser(t, db, "client_users", "u-2", "bob@example.com", "Bob Verdi", "bob", false)
	seedMembership(t, db, "u-1", tn.UUID)
	seedMembership(t, db, "u-2", other.UUID)

	got, err := r.SearchTenantsByQ(context.Background(), TenantListFilter{Kind: models.TenantKindExternal, Q: "alice@"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(got) != 1 || got[0].Tenant.UUID != tn.UUID {
		t.Fatalf("got %+v, want only %s", uuids(got), tn.UUID)
	}
	if len(got[0].MatchedMembers) != 1 || got[0].MatchedMembers[0].Email != "alice@example.com" {
		t.Fatalf("matchedMembers = %+v, want [alice@]", got[0].MatchedMembers)
	}
}

func TestSearchTenantsByQ_MemberFullNameMatch_Surname(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()
	r := New(db)
	tn := seedTenant(t, db, nil)
	seedUser(t, db, "client_users", "u-1", "mr@example.com", "Mario Rossi", "mario", false)
	seedMembership(t, db, "u-1", tn.UUID)

	got, err := r.SearchTenantsByQ(context.Background(), TenantListFilter{Kind: models.TenantKindExternal, Q: "rossi"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(got) != 1 || got[0].Tenant.UUID != tn.UUID {
		t.Fatalf("got %+v, want only %s", uuids(got), tn.UUID)
	}
	if got[0].MatchedMembers[0].FullName != "Mario Rossi" {
		t.Fatalf("matched member fullName = %q, want %q", got[0].MatchedMembers[0].FullName, "Mario Rossi")
	}
}

func TestSearchTenantsByQ_MemberUsernameMatch(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()
	r := New(db)
	tn := seedTenant(t, db, nil)
	seedUser(t, db, "client_users", "u-1", "user@example.com", "Some Person", "specific_handle", false)
	seedMembership(t, db, "u-1", tn.UUID)

	got, err := r.SearchTenantsByQ(context.Background(), TenantListFilter{Kind: models.TenantKindExternal, Q: "specific_handle"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(got) != 1 || got[0].MatchedMembers[0].Username != "specific_handle" {
		t.Fatalf("matchedMembers = %+v, want username match", got[0].MatchedMembers)
	}
}

func TestSearchTenantsByQ_KindInternal_UsesOperatorCollection(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()
	r := New(db)
	internal := seedTenant(t, db, func(tn *models.Tenant) { tn.Kind = models.TenantKindInternal })
	external := seedTenant(t, db, func(tn *models.Tenant) { tn.Kind = models.TenantKindExternal })

	// Same email seeded into BOTH user collections so we can prove the
	// kind filter steers the join at the right collection.
	seedUser(t, db, "operator_users", "u-op", "shared@example.com", "Operator Hand", "ophand", false)
	seedUser(t, db, "client_users", "u-cl", "shared@example.com", "Client Hand", "clhand", false)
	seedMembership(t, db, "u-op", internal.UUID)
	seedMembership(t, db, "u-cl", external.UUID)

	gotInternal, err := r.SearchTenantsByQ(context.Background(), TenantListFilter{Kind: models.TenantKindInternal, Q: "shared@"})
	if err != nil {
		t.Fatalf("search internal: %v", err)
	}
	if len(gotInternal) != 1 || gotInternal[0].Tenant.UUID != internal.UUID {
		t.Fatalf("internal search hit wrong tenant: %+v", uuids(gotInternal))
	}
	if gotInternal[0].MatchedMembers[0].FullName != "Operator Hand" {
		t.Fatalf("internal search returned client-side user: %+v", gotInternal[0].MatchedMembers)
	}

	gotExternal, err := r.SearchTenantsByQ(context.Background(), TenantListFilter{Kind: models.TenantKindExternal, Q: "shared@"})
	if err != nil {
		t.Fatalf("search external: %v", err)
	}
	if len(gotExternal) != 1 || gotExternal[0].Tenant.UUID != external.UUID {
		t.Fatalf("external search hit wrong tenant: %+v", uuids(gotExternal))
	}
	if gotExternal[0].MatchedMembers[0].FullName != "Client Hand" {
		t.Fatalf("external search returned operator-side user: %+v", gotExternal[0].MatchedMembers)
	}
}

func TestSearchTenantsByQ_NoMatch_EmptyResult(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()
	r := New(db)
	_ = seedTenant(t, db, func(tn *models.Tenant) { tn.Name = "Acme" })

	got, err := r.SearchTenantsByQ(context.Background(), TenantListFilter{Kind: models.TenantKindExternal, Q: "nothing-matches"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %d hits, want 0: %+v", len(got), uuids(got))
	}
}

// --- Soft-deletion handling ------------------------------------------------

func TestSearchTenantsByQ_IncludeDeleted_ExcludesByDefault(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()
	r := New(db)
	live := seedTenant(t, db, func(tn *models.Tenant) { tn.Name = "Acme Live" })
	now := time.Now()
	_ = seedTenant(t, db, func(tn *models.Tenant) {
		tn.Name = "Acme Dead"
		tn.DeletedAt = &now
	})

	got, err := r.SearchTenantsByQ(context.Background(), TenantListFilter{Kind: models.TenantKindExternal, Q: "Acme"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(got) != 1 || got[0].Tenant.UUID != live.UUID {
		t.Fatalf("got %+v, want only %s (live)", uuids(got), live.UUID)
	}
}

func TestSearchTenantsByQ_IncludeDeleted_True_ReturnsBoth(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()
	r := New(db)
	live := seedTenant(t, db, func(tn *models.Tenant) { tn.Name = "Acme Live" })
	now := time.Now()
	dead := seedTenant(t, db, func(tn *models.Tenant) {
		tn.Name = "Acme Dead"
		tn.DeletedAt = &now
	})

	got, err := r.SearchTenantsByQ(context.Background(), TenantListFilter{Kind: models.TenantKindExternal, Q: "Acme", IncludeDeleted: true})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d hits, want 2: %+v", len(got), uuids(got))
	}
	wantSet := map[string]bool{live.UUID: true, dead.UUID: true}
	for _, r := range got {
		if !wantSet[r.Tenant.UUID] {
			t.Fatalf("unexpected uuid %s", r.Tenant.UUID)
		}
	}
}

func TestSearchTenantsByQ_IncludeDeletedUsers_ExcludesByDefault(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()
	r := New(db)
	tn := seedTenant(t, db, nil)
	seedUser(t, db, "client_users", "u-dead", "dead@example.com", "Dead Person", "dead", true)
	seedMembership(t, db, "u-dead", tn.UUID)

	got, err := r.SearchTenantsByQ(context.Background(), TenantListFilter{Kind: models.TenantKindExternal, Q: "dead@"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %d hits, want 0 — soft-deleted user should not match by default", len(got))
	}
}

func TestSearchTenantsByQ_IncludeDeletedUsers_True_IncludesThem(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()
	r := New(db)
	tn := seedTenant(t, db, nil)
	seedUser(t, db, "client_users", "u-dead", "dead@example.com", "Dead Person", "dead", true)
	seedMembership(t, db, "u-dead", tn.UUID)

	got, err := r.SearchTenantsByQ(context.Background(), TenantListFilter{Kind: models.TenantKindExternal, Q: "dead@", IncludeDeletedUsers: true})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(got) != 1 || got[0].MatchedMembers[0].Email != "dead@example.com" {
		t.Fatalf("got %+v, want one hit on dead@example.com", got)
	}
}

// --- Hierarchy filters -----------------------------------------------------

func TestSearchTenantsByQ_RootsOnly_ExcludesChildren(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()
	r := New(db)
	root := seedTenant(t, db, func(tn *models.Tenant) { tn.Name = "Acme Holding" })
	parentRef := root.UUID
	_ = seedTenant(t, db, func(tn *models.Tenant) {
		tn.Name = "Acme Italia"
		tn.ParentTenantUUID = &parentRef
	})

	got, err := r.SearchTenantsByQ(context.Background(), TenantListFilter{Kind: models.TenantKindExternal, Q: "Acme", RootsOnly: true})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(got) != 1 || got[0].Tenant.UUID != root.UUID {
		t.Fatalf("got %+v, want only root %s", uuids(got), root.UUID)
	}
}

func TestSearchTenantsByQ_ParentTenantUUID_RestrictsToChildren(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()
	r := New(db)
	root := seedTenant(t, db, func(tn *models.Tenant) { tn.Name = "Acme Holding" })
	parentRef := root.UUID
	child := seedTenant(t, db, func(tn *models.Tenant) {
		tn.Name = "Acme Italia"
		tn.ParentTenantUUID = &parentRef
	})

	got, err := r.SearchTenantsByQ(context.Background(), TenantListFilter{
		Kind:             models.TenantKindExternal,
		Q:                "Acme",
		ParentTenantUUID: &parentRef,
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(got) != 1 || got[0].Tenant.UUID != child.UUID {
		t.Fatalf("got %+v, want only child %s", uuids(got), child.UUID)
	}
}

// --- matchedMembers contract ----------------------------------------------

func TestSearchTenantsByQ_MatchedMembers_BoundedByMax(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()
	r := New(db)
	tn := seedTenant(t, db, nil)
	// Seed twice the limit so a missing $slice would let the response grow
	// unbounded.
	for i := 0; i < MaxMatchedMembersPerTenant*2; i++ {
		uuid := "u-" + randHex(t, 4)
		seedUser(t, db, "client_users", uuid, "needle"+uuid+"@x.com", "Person", "user"+uuid, false)
		seedMembership(t, db, uuid, tn.UUID)
	}

	got, err := r.SearchTenantsByQ(context.Background(), TenantListFilter{Kind: models.TenantKindExternal, Q: "needle"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d hits, want 1", len(got))
	}
	if n := len(got[0].MatchedMembers); n != MaxMatchedMembersPerTenant {
		t.Fatalf("matchedMembers length = %d, want %d (bounded)", n, MaxMatchedMembersPerTenant)
	}
	if got[0].MemberCount != MaxMatchedMembersPerTenant*2 {
		t.Fatalf("MemberCount = %d, want %d (full membership count, not slice)", got[0].MemberCount, MaxMatchedMembersPerTenant*2)
	}
}

func TestSearchTenantsByQ_MatchedMembers_ProjectionFields(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()
	r := New(db)
	tn := seedTenant(t, db, nil)
	seedUser(t, db, "client_users", "u-1", "alice@x.com", "Alice Bianchi", "alice_b", false)
	seedMembership(t, db, "u-1", tn.UUID)

	got, err := r.SearchTenantsByQ(context.Background(), TenantListFilter{Kind: models.TenantKindExternal, Q: "alice"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(got) != 1 || len(got[0].MatchedMembers) != 1 {
		t.Fatalf("expected one hit with one matched member, got %+v", got)
	}
	m := got[0].MatchedMembers[0]
	if m.UserUUID != "u-1" || m.Email != "alice@x.com" || m.FullName != "Alice Bianchi" || m.Username != "alice_b" {
		t.Fatalf("matched member projection wrong: %+v", m)
	}
}

// --- Edge cases -----------------------------------------------------------

func TestSearchTenantsByQ_RegexSpecialCharsEscaped(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()
	r := New(db)
	dotty := seedTenant(t, db, func(tn *models.Tenant) { tn.Name = "Acme.Co" })
	dashed := seedTenant(t, db, func(tn *models.Tenant) { tn.Name = "AcmeXCo" })

	// Without QuoteMeta, "." in the query would match every "Acme?Co" via
	// the regex any-char wildcard. With QuoteMeta we require a literal dot.
	got, err := r.SearchTenantsByQ(context.Background(), TenantListFilter{Kind: models.TenantKindExternal, Q: "Acme.Co"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(got) != 1 || got[0].Tenant.UUID != dotty.UUID {
		t.Fatalf("got %+v, want only %s (literal dot)", uuids(got), dotty.UUID)
	}
	_ = dashed
}

func TestSearchTenantsByQ_TenantHitWithNoMembers(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()
	r := New(db)
	tn := seedTenant(t, db, func(tn *models.Tenant) { tn.Name = "Lonely Tenant" })

	got, err := r.SearchTenantsByQ(context.Background(), TenantListFilter{Kind: models.TenantKindExternal, Q: "Lonely"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(got) != 1 || got[0].Tenant.UUID != tn.UUID {
		t.Fatalf("got %+v, want only %s", uuids(got), tn.UUID)
	}
	if got[0].MemberCount != 0 || len(got[0].MatchedMembers) != 0 {
		t.Fatalf("expected zero members, got count=%d matched=%d", got[0].MemberCount, len(got[0].MatchedMembers))
	}
}

func TestSearchTenantsByQ_MemberHitOnTenantThatDoesntMatchByName(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()
	r := New(db)
	tn := seedTenant(t, db, func(tn *models.Tenant) { tn.Name = "Globex" })
	seedUser(t, db, "client_users", "u-1", "alice@example.com", "Alice", "alice", false)
	seedMembership(t, db, "u-1", tn.UUID)

	got, err := r.SearchTenantsByQ(context.Background(), TenantListFilter{Kind: models.TenantKindExternal, Q: "alice"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(got) != 1 || got[0].Tenant.UUID != tn.UUID {
		t.Fatalf("got %+v, want only %s (matched only via member)", uuids(got), tn.UUID)
	}
}

func TestSearchTenantsByQ_SortedByCreatedAtDesc(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()
	r := New(db)
	old := seedTenant(t, db, func(tn *models.Tenant) {
		tn.Name = "Acme Old"
		tn.CreatedAt = time.Now().Add(-2 * time.Hour)
	})
	mid := seedTenant(t, db, func(tn *models.Tenant) {
		tn.Name = "Acme Mid"
		tn.CreatedAt = time.Now().Add(-1 * time.Hour)
	})
	fresh := seedTenant(t, db, func(tn *models.Tenant) {
		tn.Name = "Acme Fresh"
		tn.CreatedAt = time.Now()
	})

	got, err := r.SearchTenantsByQ(context.Background(), TenantListFilter{Kind: models.TenantKindExternal, Q: "Acme"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d hits, want 3", len(got))
	}
	if got[0].Tenant.UUID != fresh.UUID || got[1].Tenant.UUID != mid.UUID || got[2].Tenant.UUID != old.UUID {
		t.Fatalf("sort wrong: %+v", []string{got[0].Tenant.UUID, got[1].Tenant.UUID, got[2].Tenant.UUID})
	}
}

// --- Tenant-only fallback (kind unset) ------------------------------------

func TestSearchTenantsByQ_TenantOnlyFallback_KindEmpty(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()
	r := New(db)
	hit := seedTenant(t, db, func(tn *models.Tenant) { tn.Name = "Acme Direct" })
	memberHit := seedTenant(t, db, func(tn *models.Tenant) { tn.Name = "Indirect Co" })
	seedUser(t, db, "client_users", "u-1", "Acme@indirect.example", "Person", "p", false)
	seedMembership(t, db, "u-1", memberHit.UUID)

	// Kind=="" routes to searchTenantsByQTenantOnly: matches tenant
	// name/slug only (no member-side join because we don't know which user
	// collection to use).
	got, err := r.SearchTenantsByQ(context.Background(), TenantListFilter{Q: "Acme"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(got) != 1 || got[0].Tenant.UUID != hit.UUID {
		t.Fatalf("got %+v, want only direct-name hit %s", uuids(got), hit.UUID)
	}
	if len(got[0].MatchedMembers) != 0 {
		t.Fatalf("matchedMembers should be empty in tenant-only fallback, got %+v", got[0].MatchedMembers)
	}
}

func TestSearchTenantsByQ_TenantOnlyFallback_RespectsRootsOnly(t *testing.T) {
	db, cleanup := newTestDB(t)
	defer cleanup()
	r := New(db)
	root := seedTenant(t, db, func(tn *models.Tenant) { tn.Name = "Acme Holding" })
	parentRef := root.UUID
	_ = seedTenant(t, db, func(tn *models.Tenant) {
		tn.Name = "Acme Italia"
		tn.ParentTenantUUID = &parentRef
	})

	got, err := r.SearchTenantsByQ(context.Background(), TenantListFilter{Q: "Acme", RootsOnly: true})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(got) != 1 || got[0].Tenant.UUID != root.UUID {
		t.Fatalf("got %+v, want only root %s", uuids(got), root.UUID)
	}
}
