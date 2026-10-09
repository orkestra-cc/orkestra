package auth

// Live-Mongo proof that mongoIndexLister reads what the boot check needs.
// Gated on MONGO_TEST_URI (or MONGO_URI); skipped otherwise. Run with:
//
//	MONGO_TEST_URI='mongodb://<user>:<pw>@localhost:27018/?authSource=admin&directConnection=true' \
//	  go test ./internal/core/auth/ -run Integration -v

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/orkestra/backend/internal/core/auth/models"
)

func TestMongoIndexLister_Integration(t *testing.T) {
	uri := os.Getenv("MONGO_TEST_URI")
	if uri == "" {
		uri = os.Getenv("MONGO_URI")
	}
	if uri == "" {
		t.Skip("set MONGO_TEST_URI or MONGO_URI to run the live index-lister test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("mongo.Connect: %v", err)
	}
	defer client.Disconnect(ctx)
	db := client.Database(fmt.Sprintf("auth_index_check_test_%d", time.Now().UnixNano()))
	defer db.Drop(ctx)

	m := &AuthModule{indexLister: mongoIndexLister{db: db}}

	// Fresh database, collections absent: the index is missing.
	if err := m.verifyOAuthIdentityIndex(ctx); err == nil {
		t.Fatal("absent collections must read as a missing index")
	}

	// A collection that exists but carries only the legacy index: still missing.
	for _, c := range []string{models.OperatorOAuthProvidersCollection, models.ClientOAuthProvidersCollection} {
		if _, err := db.Collection(c).Indexes().CreateOne(ctx, mongo.IndexModel{
			Keys: bson.D{{Key: "userUuid", Value: 1}, {Key: "provider", Value: 1}}, Options: options.Index().SetUnique(true),
		}); err != nil {
			t.Fatalf("legacy index on %s: %v", c, err)
		}
	}
	if err := m.verifyOAuthIdentityIndex(ctx); err == nil {
		t.Fatal("the legacy (userUuid, provider) index alone must not satisfy the check")
	}

	// What migration 0010 creates — and what the module's own ordered spec
	// creates on a fresh install — satisfies it on both collections.
	for _, c := range []string{models.OperatorOAuthProvidersCollection, models.ClientOAuthProvidersCollection} {
		if _, err := db.Collection(c).Indexes().CreateOne(ctx, mongo.IndexModel{
			Keys: bson.D{{Key: "provider", Value: 1}, {Key: "providerId", Value: 1}}, Options: options.Index().SetUnique(true),
		}); err != nil {
			t.Fatalf("identity index on %s: %v", c, err)
		}
	}
	if err := m.verifyOAuthIdentityIndex(ctx); err != nil {
		t.Fatalf("both collections carry %s, got %v", oauthIdentityIndexName, err)
	}
}
