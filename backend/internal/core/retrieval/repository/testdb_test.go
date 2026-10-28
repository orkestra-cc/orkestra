package repository

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/testkit"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/module"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// newTestDB connects to MONGO_TEST_URI (replica set, directConnection=true
// in CI) and hands back an isolated database that is dropped on cleanup.
// Skips the calling test when the variable is unset, like the tenant
// repository harness, so a plain `go test ./...` never needs Mongo.
func newTestDB(t *testing.T) *mongo.Database {
	t.Helper()
	uri := os.Getenv("MONGO_TEST_URI")
	if uri == "" {
		t.Skip("MONGO_TEST_URI not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	db := client.Database(fmt.Sprintf("retrieval_test_%d", time.Now().UnixNano()))
	t.Cleanup(func() {
		_ = db.Drop(context.Background())
		_ = client.Disconnect(context.Background())
	})
	return db
}

// tctx is a request context for tenantID with tenant kind internal.
func tctx(tenantID string) context.Context {
	id := testkit.NewIdentity("u-admin", "admin@example.test", "administrator").WithTenant(tenantID, []string{"org_owner"}, true)
	return ctxauth.WithTenantKind(id.ContextFor(context.Background(), tenantID), "internal")
}

// ensureIndexes creates the indexes the module declares for coll, built from
// the same OutboxIndexes definitions, so the tests see the production keys.
func ensureIndexes(t *testing.T, db *mongo.Database, coll string, specs []module.IndexSpec) {
	t.Helper()
	var ms []mongo.IndexModel
	for _, s := range specs {
		var keys bson.D
		for _, k := range s.OrderedKeys {
			keys = append(keys, bson.E{Key: k.Field, Value: k.Direction})
		}
		opts := options.Index().SetUnique(s.Unique)
		if len(s.PartialFilter) > 0 {
			opts.SetPartialFilterExpression(s.PartialFilter)
		}
		ms = append(ms, mongo.IndexModel{Keys: keys, Options: opts})
	}
	if _, err := db.Collection(coll).Indexes().CreateMany(context.Background(), ms); err != nil {
		t.Fatal(err)
	}
}
