package repository

// Mongo-gated test for InsertRole, the write CreateRole uses. Skips
// unless MONGO_TEST_URI (or MONGO_URI) is set, mirroring
// binding_ensure_integration_test.go: per-run random database name and
// cleanup that drops it.
//
// It needs a real MongoDB for the same reason the binding tests do —
// the whole contract is the unique (tenantId, name) index and the
// driver's mongo.IsDuplicateKeyError translation of the E11000 it
// raises. The in-memory fake in services/repo_fake_test.go can mirror
// the RULE but cannot prove the driver recognises the error shape.

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra/backend/internal/core/authz/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func liveRoleRepository(t *testing.T) (*Repository, func()) {
	t.Helper()
	uri := os.Getenv("MONGO_TEST_URI")
	if uri == "" {
		uri = os.Getenv("MONGO_URI")
	}
	if uri == "" {
		t.Skip("set MONGO_TEST_URI or MONGO_URI to run live authz role repository tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("mongo.Connect: %v", err)
	}
	if err := client.Ping(ctx, nil); err != nil {
		_ = client.Disconnect(context.Background())
		t.Fatalf("mongo.Ping: %v", err)
	}
	db := client.Database("authz_roles_insert_" + uuid.NewString())
	repo := New(db)
	// Mirror module.go's Collections() CollRoles indexes. Production
	// boot creates these via ensureCollections; the uniqueness of
	// (tenantId, name) is the entire contract under test.
	if _, err := db.Collection(CollRoles).Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "tenantId", Value: 1}, {Key: "name", Value: 1}},
		Options: options.Index().SetUnique(true).SetName("tenantId_1_name_1"),
	}); err != nil {
		t.Fatalf("create authz_roles unique index: %v", err)
	}
	cleanup := func() {
		_ = db.Drop(context.Background())
		_ = client.Disconnect(context.Background())
	}
	return repo, cleanup
}

// The regression the whole-branch review found: two CreateRole-shaped
// writes with the same (tenant, name) used to leave ONE row, carrying
// the second call's uuid and permissions. The first role's uuid stopped
// resolving, so every binding on it dangled permanently and its holders
// silently lost that access — with no invalidation, because CreateRole
// bumps nothing.
//
// InsertRole refuses the second write instead. The first row is
// untouched and still resolvable by its own uuid.
func TestInsertRole_NameCollisionIsRefusedNotRewritten(t *testing.T) {
	repo, cleanup := liveRoleRepository(t)
	defer cleanup()
	ctx := context.Background()

	first := &models.Role{
		UUID: "uuid-first", TenantID: "tenant-A", Name: "editors",
		Permissions: []string{"tenant.read", "tenant.delete"}, IsActive: true,
	}
	if err := repo.InsertRole(ctx, first); err != nil {
		t.Fatalf("first insert: %v", err)
	}

	second := &models.Role{
		UUID: "uuid-second", TenantID: "tenant-A", Name: "editors",
		Permissions: []string{"tenant.read"}, IsActive: true,
	}
	if err := repo.InsertRole(ctx, second); !errors.Is(err, ErrRoleExists) {
		t.Fatalf("second insert: err = %v, want ErrRoleExists", err)
	}

	got, err := repo.GetRoleByUUID(ctx, "uuid-first")
	if err != nil {
		t.Fatalf("the incumbent's uuid must still resolve, got %v", err)
	}
	if len(got.Permissions) != 2 {
		t.Errorf("permissions = %v, want the incumbent's own list", got.Permissions)
	}
	if _, err := repo.GetRoleByUUID(ctx, "uuid-second"); !errors.Is(err, ErrNotFound) {
		t.Errorf("the refused insert left a row behind: %v", err)
	}
	roles, err := repo.ListRoles(ctx, "tenant-A")
	if err != nil {
		t.Fatalf("ListRoles: %v", err)
	}
	if len(roles) != 1 {
		t.Errorf("roles = %d, want exactly the one that was inserted", len(roles))
	}

	// The name is reserved within its tenant only.
	other := &models.Role{
		UUID: "uuid-third", TenantID: "tenant-B", Name: "editors",
		Permissions: []string{"tenant.read"}, IsActive: true,
	}
	if err := repo.InsertRole(ctx, other); err != nil {
		t.Fatalf("another tenant must still be able to use the name: %v", err)
	}
}

// UpsertRole keeps its (tenantId, name) key — that is what makes the
// boot-time system-role seed idempotent, and why it must never serve
// role creation. Pinned so the two writes cannot be swapped back.
func TestUpsertRole_StillConvergesOnOneRowForTheSeeder(t *testing.T) {
	repo, cleanup := liveRoleRepository(t)
	defer cleanup()
	ctx := context.Background()

	seed := &models.Role{
		UUID: "uuid-sys", TenantID: "", Name: "administrator",
		Permissions: []string{"system.users.admin"}, IsSystem: true, IsActive: true,
	}
	if err := repo.UpsertRole(ctx, seed); err != nil {
		t.Fatalf("first seed: %v", err)
	}
	// SeedSystemRoles re-reads the existing row and reuses its uuid, so
	// a re-boot writes the SAME uuid back.
	if err := repo.UpsertRole(ctx, seed); err != nil {
		t.Fatalf("second seed: %v", err)
	}
	roles, err := repo.ListRoles(ctx, "")
	if err != nil {
		t.Fatalf("ListRoles: %v", err)
	}
	if len(roles) != 1 {
		t.Fatalf("roles = %d, want the seed to have converged on one row", len(roles))
	}
	if roles[0].UUID != "uuid-sys" {
		t.Errorf("uuid = %q, want the seeded one preserved", roles[0].UUID)
	}
}
