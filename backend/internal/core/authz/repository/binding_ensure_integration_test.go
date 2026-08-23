package repository

// Mongo-gated tests for EnsureBinding and the duplicate-key behavior it
// depends on. Skip unless MONGO_TEST_URI (or MONGO_URI) is set, mirroring
// the pattern in internal/core/auth/repository/refresh_token_repository_concurrency_test.go:
// per-run random database name, cleanup that drops the DB, and the
// start-channel barrier idiom for the concurrent test.
//
// The unique compound index on (tenantId, userUUID, roleId) that makes
// EnsureBinding's upsert — and CreateBinding's duplicate-key surfacing —
// meaningful is not something the fake repo in services/repo_fake_test.go
// can model: only a real MongoDB proves the driver's
// mongo.IsDuplicateKeyError translation actually recognizes the E11000
// shape these code paths depend on. The harness below builds that index
// itself, exactly as module.go's Collections() declares it (see
// authz/CLAUDE.md) — production boot goes through ensureCollections
// instead, which is create-only and non-fatal, hence the 0009 migration.

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra/backend/internal/core/authz/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func liveBindingRepository(t *testing.T) (*Repository, func()) {
	t.Helper()
	uri := os.Getenv("MONGO_TEST_URI")
	if uri == "" {
		uri = os.Getenv("MONGO_URI")
	}
	if uri == "" {
		t.Skip("set MONGO_TEST_URI or MONGO_URI to run live authz binding repository tests")
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
	db := client.Database("authz_bindings_ensure_" + uuid.NewString())
	repo := New(db)
	// Mirror module.go's Collections() CollBindings unique index — see
	// this package's parent module CLAUDE.md and
	// docs/migrations/0009_authz_bindings_unique.md. Production boot
	// creates this via ensureCollections; here the test stands it up
	// directly so EnsureBinding's upsert races the real constraint.
	if _, err := db.Collection(CollBindings).Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "tenantId", Value: 1}, {Key: "userUUID", Value: 1}, {Key: "roleId", Value: 1}},
		Options: options.Index().SetUnique(true).SetName("tenantId_1_userUUID_1_roleId_1"),
	}); err != nil {
		t.Fatalf("create authz_bindings unique index: %v", err)
	}
	cleanup := func() {
		_ = db.Drop(context.Background())
		_ = client.Disconnect(context.Background())
	}
	return repo, cleanup
}

func ensureRaceBinding(uuidStr, userUUID, tenantID, roleUUID, grantedBy string) *models.Binding {
	return &models.Binding{
		UUID:      uuidStr,
		UserUUID:  userUUID,
		TenantID:  tenantID,
		RoleUUID:  roleUUID,
		RoleName:  "org_owner",
		GrantedBy: grantedBy,
	}
}

// TestEnsureBinding_CreatesThenReturnsExisting pins the "preserve the
// winner" contract: the first call inserts, and a second call for the same
// tuple — carrying a DIFFERENT uuid, grantedBy, and a non-nil expiresAt —
// must return the ORIGINAL row untouched. Nothing about the loser's payload
// (including its would-be expiresAt) may leak onto the winner.
func TestEnsureBinding_CreatesThenReturnsExisting(t *testing.T) {
	repo, cleanup := liveBindingRepository(t)
	defer cleanup()
	ctx := context.Background()

	first := ensureRaceBinding(uuid.NewString(), "user-winner", "tenant-A", "role-org-owner", "system")
	out1, err := repo.EnsureBinding(ctx, first)
	if err != nil {
		t.Fatalf("EnsureBinding (first): %v", err)
	}
	if out1.UUID != first.UUID {
		t.Fatalf("first ensure returned UUID %q, want %q", out1.UUID, first.UUID)
	}
	if out1.GrantedBy != "system" {
		t.Fatalf("first ensure GrantedBy = %q, want %q", out1.GrantedBy, "system")
	}
	if out1.ExpiresAt != nil {
		t.Fatalf("first ensure ExpiresAt = %v, want nil", out1.ExpiresAt)
	}

	loserExpiry := time.Now().Add(24 * time.Hour)
	second := ensureRaceBinding(uuid.NewString(), "user-winner", "tenant-A", "role-org-owner", "someone-else")
	second.ExpiresAt = &loserExpiry
	out2, err := repo.EnsureBinding(ctx, second)
	if err != nil {
		t.Fatalf("EnsureBinding (second): %v", err)
	}
	if out2.UUID != out1.UUID {
		t.Fatalf("second ensure returned a different UUID: %q, want the winner's %q", out2.UUID, out1.UUID)
	}
	if out2.GrantedBy != out1.GrantedBy {
		t.Fatalf("second ensure overwrote GrantedBy: got %q, want the winner's %q", out2.GrantedBy, out1.GrantedBy)
	}
	if !out2.GrantedAt.Equal(out1.GrantedAt) {
		t.Fatalf("second ensure overwrote GrantedAt: got %v, want the winner's %v", out2.GrantedAt, out1.GrantedAt)
	}
	if out2.ExpiresAt != nil {
		t.Fatalf("second ensure leaked the loser's ExpiresAt onto the winner: got %v, want nil", out2.ExpiresAt)
	}

	//tenantscope:allow test assertion counts rows in one isolated per-run test database
	count, err := repo.db.Collection(CollBindings).CountDocuments(ctx, bson.M{
		"tenantId": "tenant-A", "userUUID": "user-winner", "roleId": "role-org-owner",
	})
	if err != nil {
		t.Fatalf("CountDocuments: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly one persisted binding for the tuple, got %d", count)
	}
}

// TestEnsureBinding_ConcurrentSingleRow races two goroutines calling
// EnsureBinding for the same tuple at once, looped enough iterations to
// reliably land both calls inside the race window. Both callers must
// converge on the same binding UUID, and exactly one document must ever
// land in Mongo for the tuple.
func TestEnsureBinding_ConcurrentSingleRow(t *testing.T) {
	repo, cleanup := liveBindingRepository(t)
	defer cleanup()
	ctx := context.Background()

	const iterations = 40
	for i := 0; i < iterations; i++ {
		tenantID := "tenant-race"
		userUUID := "user-race-" + uuid.NewString()
		roleUUID := "role-race"

		start := make(chan struct{})
		var wg sync.WaitGroup
		ids := make([]string, 2)
		errs := make([]error, 2)
		wg.Add(2)
		for g := 0; g < 2; g++ {
			go func(g int) {
				defer wg.Done()
				<-start
				b := ensureRaceBinding(uuid.NewString(), userUUID, tenantID, roleUUID, "system")
				out, err := repo.EnsureBinding(ctx, b)
				errs[g] = err
				if err == nil {
					ids[g] = out.UUID
				}
			}(g)
		}
		close(start)
		wg.Wait()

		if errs[0] != nil || errs[1] != nil {
			t.Fatalf("iteration %d: EnsureBinding errors: %v, %v", i, errs[0], errs[1])
		}
		if ids[0] == "" || ids[0] != ids[1] {
			t.Fatalf("iteration %d: diverged binding UUIDs %q != %q", i, ids[0], ids[1])
		}

		//tenantscope:allow test assertion counts rows in one isolated per-run test database
		count, err := repo.db.Collection(CollBindings).CountDocuments(ctx, bson.M{
			"tenantId": tenantID, "userUUID": userUUID, "roleId": roleUUID,
		})
		if err != nil {
			t.Fatalf("iteration %d: CountDocuments: %v", i, err)
		}
		if count != 1 {
			t.Fatalf("iteration %d: expected exactly one persisted binding, got %d", i, count)
		}
	}
}

// TestCreateBinding_DuplicateTupleRejected proves the unique index actually
// rejects a second plain CreateBinding on a tuple that already has a row —
// this is the E11000 the service layer's CreateBinding maps to
// services.ErrBindingExists (see
// services/binding_ensure_test.go:TestCreateBinding_DuplicateKeyMapsToErrBindingExists
// for that mapping, exercised there against a fake repo primed with the
// same mongo.WriteException shape a real duplicate key error takes).
func TestCreateBinding_DuplicateTupleRejected(t *testing.T) {
	repo, cleanup := liveBindingRepository(t)
	defer cleanup()
	ctx := context.Background()

	first := ensureRaceBinding(uuid.NewString(), "user-dup", "tenant-B", "role-dup", "granter-1")
	if err := repo.CreateBinding(ctx, first); err != nil {
		t.Fatalf("CreateBinding (first): %v", err)
	}

	second := ensureRaceBinding(uuid.NewString(), "user-dup", "tenant-B", "role-dup", "granter-2")
	err := repo.CreateBinding(ctx, second)
	if err == nil {
		t.Fatal("expected CreateBinding on an existing tuple to fail, got nil error")
	}
	if !mongo.IsDuplicateKeyError(err) {
		t.Fatalf("expected a duplicate-key error, got %v (%T)", err, err)
	}

	//tenantscope:allow test assertion counts rows in one isolated per-run test database
	count, err := repo.db.Collection(CollBindings).CountDocuments(ctx, bson.M{
		"tenantId": "tenant-B", "userUUID": "user-dup", "roleId": "role-dup",
	})
	if err != nil {
		t.Fatalf("CountDocuments: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected the rejected duplicate to leave exactly one row, got %d", count)
	}
}
