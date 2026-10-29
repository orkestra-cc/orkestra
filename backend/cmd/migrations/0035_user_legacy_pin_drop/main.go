// Binary 0035_user_legacy_pin_drop retires the reversible `pin` field of
// operator_users and client_users. Until the PIN sign-in method existed
// the user module stored an optional 4-digit PIN
// AES-GCM-encrypted with OAUTH_TOKEN_ENCRYPTION_KEY and never read it back.
// Those values were set with no strength rule and no step-up and were never
// a credential, so they are DROPPED, not converted: turning a `1234` typed
// into a create-user form into a live sign-in secret would be the wrong
// direction. Administrators set real PINs through the PIN routes. Rows that
// already carry a `pinHash` keep it. Idempotent: a re-run finds no `pin`.
//
// Logs counts only. MONGO_URI / MONGO_DATABASE; --dry-run counts the rows
// that would change without writing.
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"time"

	userrepo "github.com/orkestra/backend/internal/core/user/repository"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func main() {
	os.Exit(run())
}

func run() int {
	dryRun := flag.Bool("dry-run", false, "count the rows that would change, no writes")
	flag.Parse()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	uri, dbName := os.Getenv("MONGO_URI"), os.Getenv("MONGO_DATABASE")
	if uri == "" || dbName == "" {
		logger.Error("MONGO_URI and MONGO_DATABASE are required")
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		// NEVER log this error: a malformed MONGO_URI comes back verbatim,
		// credentials included.
		logger.Error("connect failed", "database", dbName)
		return 1
	}
	defer func() { _ = client.Disconnect(context.Background()) }()

	for _, coll := range []string{userrepo.OperatorUsersCollection, userrepo.ClientUsersCollection} {
		n, err := drop(ctx, client.Database(dbName).Collection(coll), *dryRun)
		if err != nil {
			logger.Error("migrate", "collection", coll, "error", err)
			return 1
		}
		logger.Info("done", "collection", coll, "dryRun", *dryRun, "legacyPinsDropped", n)
	}
	return 0
}

// drop unsets the legacy field on every row carrying it and returns how
// many rows did (or would, with dryRun).
func drop(ctx context.Context, coll *mongo.Collection, dryRun bool) (int64, error) {
	filter := bson.M{"pin": bson.M{"$exists": true}}
	if dryRun {
		//tenantscope:allow operator migration: user rows are global per tier, not tenant-scoped
		return coll.CountDocuments(ctx, filter)
	}
	//tenantscope:allow operator migration: user rows are global per tier, not tenant-scoped
	res, err := coll.UpdateMany(ctx, filter, bson.M{"$unset": bson.M{"pin": ""}})
	if err != nil {
		return 0, err
	}
	return res.ModifiedCount, nil
}
