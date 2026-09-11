package repository

// Mongo-gated tests for UnsubscribeRepository.ClaimToken. Same discipline as
// optout_repository_test.go: skip unless MONGO_TEST_URI is set, a per-run
// ephemeral database, a cleanup that drops only that database, and the same
// indexes module.go's Collections() declares in production.

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra/backend/internal/core/notification/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// newTestUnsubscribeRepo returns the concrete type, not the interface, for the
// same reason newTestOptoutRepo does: a test may need to reach past the
// interface at the raw collection.
func newTestUnsubscribeRepo(t *testing.T) (*unsubscribeRepository, func()) {
	t.Helper()
	uri := os.Getenv("MONGO_TEST_URI")
	if uri == "" {
		t.Skip("set MONGO_TEST_URI to run live notification unsubscribe repository tests")
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
	db := client.Database("orkestra_sdd_test_unsub_" + uuid.NewString())
	repo := &unsubscribeRepository{coll: db.Collection(models.NotificationUnsubscribeTokensCollect)}
	// The unique tokenHash index is what a claim races against in
	// production, so the test stands it up rather than proving atomicity
	// against a laxer schema than the real one.
	if _, err := repo.coll.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "uuid", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "tokenHash", Value: 1}}, Options: options.Index().SetUnique(true)},
	}); err != nil {
		t.Fatalf("create notification_unsubscribe_tokens indexes: %v", err)
	}
	cleanup := func() {
		_ = db.Drop(context.Background())
		_ = client.Disconnect(context.Background())
	}
	return repo, cleanup
}

// TestMongo_ClaimTokenIsAtomic releases enough contenders together that a
// read-then-write implementation could not pass by luck: a mail scanner
// prefetching the link while the recipient clicks it is exactly this race.
func TestMongo_ClaimTokenIsAtomic(t *testing.T) {
	repo, done := newTestUnsubscribeRepo(t)
	defer done()
	ctx := context.Background()
	if err := repo.Create(ctx, &models.UnsubscribeTokenDoc{
		UUID: "u1", TokenHash: "h1", Address: "ada@example.test",
		CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	const contenders = 16
	start := make(chan struct{})
	var wg sync.WaitGroup
	var mu sync.Mutex
	won := 0
	for i := 0; i < contenders; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			got, err := repo.ClaimToken(ctx, "h1", time.Now(), false)
			if err != nil {
				t.Errorf("claim: %v", err)
				return
			}
			if got != nil {
				mu.Lock()
				won++
				mu.Unlock()
			}
		}()
	}
	close(start)
	wg.Wait()
	if won != 1 {
		t.Fatalf("exactly one claim must win among %d contenders, %d did", contenders, won)
	}
}

func TestMongo_ClaimTokenRefusesUsedAndExpired(t *testing.T) {
	repo, done := newTestUnsubscribeRepo(t)
	defer done()
	ctx := context.Background()
	used := time.Now()
	seed := []*models.UnsubscribeTokenDoc{
		{UUID: "u1", TokenHash: "used", Address: "a@x.it", CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour), UsedAt: &used},
		{UUID: "u2", TokenHash: "expired", Address: "b@x.it", CreatedAt: time.Now().Add(-2 * time.Hour), ExpiresAt: time.Now().Add(-time.Hour)},
	}
	for _, d := range seed {
		if err := repo.Create(ctx, d); err != nil {
			t.Fatalf("create %s: %v", d.UUID, err)
		}
	}
	for _, hash := range []string{"used", "expired", "nonexistent"} {
		got, err := repo.ClaimToken(ctx, hash, time.Now(), false)
		if err != nil {
			t.Fatalf("claim %s: %v", hash, err)
		}
		if got != nil {
			t.Fatalf("claiming %q must not succeed", hash)
		}
	}
}

// TestMongo_ClaimTokenStampsTheDocument proves the claim is not just a
// filter: the winner's document comes back with usedAt set and sinkPending
// raised, which is what makes the sink recoverable after a crash between the
// claim and the sink call.
func TestMongo_ClaimTokenStampsTheDocument(t *testing.T) {
	repo, done := newTestUnsubscribeRepo(t)
	defer done()
	ctx := context.Background()
	if err := repo.Create(ctx, &models.UnsubscribeTokenDoc{
		UUID: "u1", TokenHash: "h1", Address: "ada@example.test", UserUUID: "user-1",
		CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := repo.ClaimToken(ctx, "h1", time.Now(), true)
	if err != nil || got == nil {
		t.Fatalf("claim: doc=%v err=%v", got, err)
	}
	if got.UsedAt == nil {
		t.Fatal("the claim must stamp usedAt")
	}
	if !got.SinkPending {
		t.Fatal("the claim must raise sinkPending: that is where the reconciler picks up")
	}
	// The claim returns the stored document, not a synthetic one — the
	// consume sequence reads the address and the user from it.
	if got.Address != "ada@example.test" || got.UserUUID != "user-1" {
		t.Fatalf("the claim must return the stored document, got %+v", got)
	}

	// And it is durable, not just on the returned copy.
	stored, err := repo.GetByHash(ctx, "h1")
	if err != nil {
		t.Fatalf("GetByHash: %v", err)
	}
	if stored.UsedAt == nil || !stored.SinkPending {
		t.Fatalf("stored document not stamped: %+v", stored)
	}
}

// TestMongo_ClaimTokenRaisesPrefPendingOnlyForAUserToken is the crash window
// the flag exists to close: the preference write happens AFTER the claim, so
// a process that dies in between has no failure to react to. Marking it up
// front is the only way that work survives; a token with no user has no
// preference to write and must not be marked.
func TestMongo_ClaimTokenRaisesPrefPendingOnlyForAUserToken(t *testing.T) {
	repo, done := newTestUnsubscribeRepo(t)
	defer done()
	ctx := context.Background()
	seed := []*models.UnsubscribeTokenDoc{
		{UUID: "u1", TokenHash: "with-user", Address: "a@x.it", UserUUID: "user-1", CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)},
		{UUID: "u2", TokenHash: "no-user", Address: "b@x.it", CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)},
	}
	for _, d := range seed {
		if err := repo.Create(ctx, d); err != nil {
			t.Fatalf("create %s: %v", d.UUID, err)
		}
	}
	for _, tc := range []struct {
		hash    string
		hasUser bool
		want    bool
	}{
		{"with-user", true, true},
		{"no-user", false, false},
	} {
		got, err := repo.ClaimToken(ctx, tc.hash, time.Now(), tc.hasUser)
		if err != nil || got == nil {
			t.Fatalf("claim %s: doc=%v err=%v", tc.hash, got, err)
		}
		if got.PrefPending != tc.want {
			t.Fatalf("claim %s: prefPending = %v, want %v", tc.hash, got.PrefPending, tc.want)
		}
		stored, err := repo.GetByHash(ctx, tc.hash)
		if err != nil {
			t.Fatalf("GetByHash %s: %v", tc.hash, err)
		}
		if stored.PrefPending != tc.want {
			t.Fatalf("stored %s: prefPending = %v, want %v", tc.hash, stored.PrefPending, tc.want)
		}
	}
}

// TestMongo_PendingFlagsRoundTrip covers the two writes the consume sequence
// uses to record that work the claim marked is now done.
func TestMongo_PendingFlagsRoundTrip(t *testing.T) {
	repo, done := newTestUnsubscribeRepo(t)
	defer done()
	ctx := context.Background()
	if err := repo.Create(ctx, &models.UnsubscribeTokenDoc{
		UUID: "u1", TokenHash: "h1", Address: "ada@example.test", UserUUID: "user-1",
		CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	claimed, err := repo.ClaimToken(ctx, "h1", time.Now(), true)
	if err != nil || claimed == nil {
		t.Fatalf("claim: doc=%v err=%v", claimed, err)
	}
	if !claimed.SinkPending || !claimed.PrefPending {
		t.Fatalf("both flags must start raised: %+v", claimed)
	}
	if err := repo.ClearSinkPending(ctx, "h1"); err != nil {
		t.Fatalf("ClearSinkPending: %v", err)
	}
	stored, err := repo.GetByHash(ctx, "h1")
	if err != nil {
		t.Fatalf("GetByHash: %v", err)
	}
	if stored.SinkPending {
		t.Fatal("sinkPending had to go down: the reconciler would retry it forever")
	}
	if !stored.PrefPending {
		t.Fatal("clearing one flag must not clear the other")
	}
	if err := repo.ClearPrefPending(ctx, "h1"); err != nil {
		t.Fatalf("ClearPrefPending: %v", err)
	}
	stored, err = repo.GetByHash(ctx, "h1")
	if err != nil {
		t.Fatalf("GetByHash: %v", err)
	}
	if stored.PrefPending {
		t.Fatal("prefPending had to go down once the preference was written")
	}
}
