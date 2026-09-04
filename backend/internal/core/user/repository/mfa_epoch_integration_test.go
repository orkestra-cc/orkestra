package repository

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// liveUserRepository connects to a real MongoDB (skips when neither
// MONGO_TEST_URI nor MONGO_URI is set) and returns an isolated
// operator-tier repository plus a cleanup func. Same shape as
// internal/core/auth/repository/refresh_token_repository_concurrency_test.go's
// liveRefreshRepository.
//
// BumpMFAEpoch's whole reason to exist is the atomic $inc — two
// concurrent factor removals must converge on distinct values, never
// observing the other's write as its own. The services package's
// in-memory fakeUserRepo (a mutex-guarded u.MFAEpoch++) is monotone BY
// CONSTRUCTION regardless of what the Mongo code does underneath, so it
// cannot catch a read-then-write regression here. Only a real server
// can.
func liveUserRepository(t *testing.T) (*mongoUserRepository, func()) {
	t.Helper()
	uri := os.Getenv("MONGO_TEST_URI")
	if uri == "" {
		uri = os.Getenv("MONGO_URI")
	}
	if uri == "" {
		t.Skip("set MONGO_TEST_URI or MONGO_URI to run live user repository tests")
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
	db := client.Database("user_mfa_epoch_" + uuid.NewString())
	repo := NewOperatorUserRepository(db).(*mongoUserRepository)
	cleanup := func() {
		_ = db.Drop(context.Background())
		_ = client.Disconnect(context.Background())
	}
	return repo, cleanup
}

// TestBumpMFAEpoch_ConcurrentCallersConverge is the property this whole
// PR rests on: N concurrent removals must each observe a DISTINCT epoch
// (the N returned values are exactly {1..N}, no duplicates) and the
// document must land on exactly N. A read-then-write implementation
// would let two callers both read the same starting value and both
// write "their" increment on top of it, losing one bump and leaving one
// removal's caller still believing its own old token is dead when it
// isn't.
func TestBumpMFAEpoch_ConcurrentCallersConverge(t *testing.T) {
	repo, cleanup := liveUserRepository(t)
	defer cleanup()
	ctx := context.Background()

	const n = 20
	//tenantscope:allow Live repository test seeds one isolated test database directly.
	if _, err := repo.collection.InsertOne(ctx, bson.M{
		"uuid":     "race-user",
		"email":    "race@example.com",
		"isActive": true,
	}); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	results := make([]int, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			got, err := repo.BumpMFAEpoch(ctx, "race-user")
			if err != nil {
				t.Errorf("goroutine %d: BumpMFAEpoch: %v", i, err)
				return
			}
			results[i] = got
		}()
	}
	close(start)
	wg.Wait()

	seen := make(map[int]bool, n)
	for i, v := range results {
		if v < 1 || v > n {
			t.Fatalf("goroutine %d: bump returned %d, want in [1,%d]", i, v, n)
		}
		if seen[v] {
			t.Fatalf("bump value %d observed twice — two concurrent callers converged on the same value", v)
		}
		seen[v] = true
	}

	var stored struct {
		MFAEpoch int `bson:"mfaEpoch"`
	}
	//tenantscope:allow Live repository test inspects one isolated test database directly.
	if err := repo.collection.FindOne(ctx, bson.M{"uuid": "race-user"}).Decode(&stored); err != nil {
		t.Fatalf("FindOne: %v", err)
	}
	if stored.MFAEpoch != n {
		t.Fatalf("stored mfaEpoch = %d, want %d", stored.MFAEpoch, n)
	}
}

// TestBumpMFAEpoch_NoExistingFieldFirstBumpIsOne seeds a document with
// no mfaEpoch key at all — a real pre-deploy row — and asserts the
// $inc's implicit-zero behaviour actually holds against a real server
// (Mongo's $inc treats a missing field as 0 and creates it).
func TestBumpMFAEpoch_NoExistingFieldFirstBumpIsOne(t *testing.T) {
	repo, cleanup := liveUserRepository(t)
	defer cleanup()
	ctx := context.Background()

	//tenantscope:allow Live repository test seeds one isolated test database directly.
	if _, err := repo.collection.InsertOne(ctx, bson.M{
		"uuid":     "legacy-user",
		"email":    "legacy@example.com",
		"isActive": true,
		// Deliberately no mfaEpoch key — models a document written
		// before the field existed on the schema.
	}); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	got, err := repo.BumpMFAEpoch(ctx, "legacy-user")
	if err != nil {
		t.Fatalf("BumpMFAEpoch: %v", err)
	}
	if got != 1 {
		t.Fatalf("first bump on a document with no mfaEpoch key = %d, want 1", got)
	}
}

// TestBumpMFAEpoch_SoftDeletedUserIsNotFound exercises the deletedAt
// predicate: a soft-deleted row must not be bumpable, and the failure
// must be ErrUserNotFound (the mongo.ErrNoDocuments -> ErrUserNotFound
// mapping), not a generic error.
func TestBumpMFAEpoch_SoftDeletedUserIsNotFound(t *testing.T) {
	repo, cleanup := liveUserRepository(t)
	defer cleanup()
	ctx := context.Background()

	//tenantscope:allow Live repository test seeds one isolated test database directly.
	if _, err := repo.collection.InsertOne(ctx, bson.M{
		"uuid":      "deleted-user",
		"email":     "deleted@example.com",
		"isActive":  true,
		"deletedAt": time.Now(),
	}); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	if _, err := repo.BumpMFAEpoch(ctx, "deleted-user"); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("BumpMFAEpoch on a soft-deleted user = %v, want ErrUserNotFound", err)
	}
}
