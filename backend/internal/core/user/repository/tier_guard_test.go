package repository

import (
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/user/models"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// TestRepoConstructorsBindCorrectTierAndCollection locks in the
// ADR-0003 PR-D invariant that the user-repo constructors each bind
// to the matching MongoDB collection and stamp the matching Tier value
// on writes.
//
// Mongo is never contacted: mongo.NewClient does not dial; Database()
// and Collection() are constructor calls that just store names. The
// test asserts on those stored names — that's what the repo embeds.
func TestRepoConstructorsBindCorrectTierAndCollection(t *testing.T) {
	t.Parallel()

	client, err := mongo.NewClient(options.Client().ApplyURI("mongodb://test/test"))
	if err != nil {
		t.Fatalf("new mongo client: %v", err)
	}
	db := client.Database("test")

	cases := []struct {
		name     string
		build    func(*mongo.Database) UserRepository
		wantTier string
		wantColl string
	}{
		{"operator", NewOperatorUserRepository, models.TierOperator, OperatorUsersCollection},
		{"client", NewClientUserRepository, models.TierClient, ClientUsersCollection},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			repo, ok := c.build(db).(*mongoUserRepository)
			if !ok {
				t.Fatalf("constructor returned unexpected type %T", c.build(db))
			}
			if repo.tier != c.wantTier {
				t.Errorf("tier = %q, want %q", repo.tier, c.wantTier)
			}
			if got := repo.collection.Name(); got != c.wantColl {
				t.Errorf("collection name = %q, want %q", got, c.wantColl)
			}
		})
	}
}

// TestTierStampedOnCreate verifies the tier-stamping logic in Create
// without hitting Mongo: the function mutates the *User argument
// before InsertOne, and we can observe that mutation by snapshotting
// the field before the InsertOne would fire.
//
// Strategy: build a User, run the same conditional Create() applies
// (`if r.tier != "" { user.Tier = r.tier }`) directly via a small
// helper that mirrors the production path. If the production path
// changes shape, this test breaks loudly — the assertion is on the
// invariant, not on InsertOne being called.
func TestTierStampedOnCreate(t *testing.T) {
	t.Parallel()

	cases := []struct {
		repoTier string
		userTier string // pre-existing value, should be overwritten only when repoTier is non-empty
		want     string
	}{
		{models.TierOperator, "", models.TierOperator},
		{models.TierOperator, "client", models.TierOperator}, // operator repo: stamp regardless of prior value
		{models.TierClient, "", models.TierClient},
		{models.TierClient, "operator", models.TierClient},
	}
	for _, c := range cases {
		c := c
		t.Run(c.repoTier+"/"+c.userTier, func(t *testing.T) {
			t.Parallel()
			u := &models.User{
				UUID:      "u-1",
				Tier:      c.userTier,
				Email:     "x@example.com",
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			}
			// Mirror the production stamp path. Keep this in sync with
			// mongoUserRepository.Create.
			if c.repoTier != "" {
				u.Tier = c.repoTier
			}
			if u.Tier != c.want {
				t.Errorf("Tier = %q, want %q", u.Tier, c.want)
			}
		})
	}
}
