// Migrate the legacy single-tenant `users` and `auth_*` collections into
// their per-audience tier copies (`operator_users`, `operator_sessions`,
// etc.) ahead of ADR-0003 PR-D's auth-path cutover.
//
// Why a one-shot script rather than a runtime migration:
//
//   - Every existing row in `users` was created by the operator-side
//     flows (the platform predates Tier-2 client onboarding), so the
//     migration is wholesale: all rows go to `operator_*`. There's no
//     row-by-row decision to make at runtime.
//   - The data shape is identical between the source and destination
//     collections — we just copy and stamp `tier="operator"`.
//   - Running this once during a maintenance window is simpler than
//     teaching every read path to dual-read for a transition window.
//
// Usage:
//
//	# Sandbox / dev (no extra flags needed):
//	go run ./scripts/migrate_user_split
//
//	# Production must explicitly opt in. The check is on ENV, not on
//	# the URI — so a production-credentialed run pointed at a staging
//	# DB still requires the flag, which is the desired conservative
//	# default.
//	go run ./scripts/migrate_user_split --confirm-prod=I-understand
//
// Environment:
//
//	MONGO_URI         (required) standard mongo connection string
//	MONGO_DATABASE    (required) target database name
//	ENV               (optional) "production" gates the run behind --confirm-prod
//
// Idempotency: a sentinel doc lives at
// `system_init.{key:"user_split_migration"}` carrying the timestamp and
// per-collection row counts of the run that wrote it. A second invocation
// finds the sentinel and exits without writing — re-running on the same DB
// is safe. To re-run after a deliberate reset, drop the sentinel manually
// and confirm the destination collections are empty:
//
//	db.system_init.deleteOne({key:"user_split_migration"})
//	db.operator_users.drop()  // and the other operator_* collections
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// sentinelKey is the system_init document key that gates re-runs. Once
// the sentinel exists, the script refuses to re-write any destination
// collection — operators must explicitly delete it to retry.
const sentinelKey = "user_split_migration"

// migration describes one source → destination copy. Every entry runs
// in the order listed; users first (so a partial run leaves the most
// important collection populated), auth-side rows next.
//
// `tierStamp` is "operator" for the destinations PR-B introduces in
// this migration. PR-D's eventual client-side onboarding will populate
// `client_*` collections from the new auth flows, not from this script,
// so no client entries appear here.
type migration struct {
	from      string
	to        string
	tierStamp string // bson field "tier" set to this value on every copied row when non-empty
	// systemOnly filters the source by `isSystem == true && tenantId == ""`.
	// Used for `authz_roles → operator_roles` so per-tenant custom roles
	// (which are tenant-scoped and stay on the legacy collection until
	// PR-D's auth-path split) are not duplicated.
	systemOnly bool
}

var plan = []migration{
	{from: "users", to: "operator_users", tierStamp: "operator"},
	{from: "auth_oauth_providers", to: "operator_oauth_providers"},
	{from: "auth_refresh_tokens", to: "operator_refresh_tokens"},
	{from: "auth_sessions", to: "operator_sessions"},
	{from: "auth_email_tokens", to: "operator_email_tokens"},
	{from: "auth_mfa_factors", to: "operator_mfa_factors"},
	{from: "authz_roles", to: "operator_roles", systemOnly: true},
}

func main() {
	confirmProd := flag.String("confirm-prod", "", `set to "I-understand" when ENV=production`)
	flag.Parse()

	if env := os.Getenv("ENV"); env == "production" && *confirmProd != "I-understand" {
		log.Fatalf("ENV=production requires --confirm-prod=I-understand")
	}

	uri := os.Getenv("MONGO_URI")
	if uri == "" {
		log.Fatalf("MONGO_URI is required")
	}
	dbName := os.Getenv("MONGO_DATABASE")
	if dbName == "" {
		log.Fatalf("MONGO_DATABASE is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		log.Fatalf("connect mongo: %v", err)
	}
	defer func() { _ = client.Disconnect(context.Background()) }()
	if err := client.Ping(ctx, nil); err != nil {
		log.Fatalf("ping mongo: %v", err)
	}

	db := client.Database(dbName)

	if claimed, err := isAlreadyMigrated(ctx, db); err != nil {
		log.Fatalf("read sentinel: %v", err)
	} else if claimed {
		log.Printf("sentinel %q already present — migration is a no-op; exiting", sentinelKey)
		return
	}

	results := make(map[string]int64, len(plan))
	for _, m := range plan {
		copied, err := runOne(ctx, db, m)
		if err != nil {
			log.Fatalf("migrate %s → %s: %v", m.from, m.to, err)
		}
		results[m.to] = copied
		log.Printf("copied %d rows: %s → %s", copied, m.from, m.to)
	}

	if err := writeSentinel(ctx, db, results); err != nil {
		log.Fatalf("write sentinel: %v", err)
	}
	log.Printf("done — sentinel %q written; re-running this script will be a no-op", sentinelKey)
}

// runOne copies every document from m.from into m.to, applying the
// tier stamp / systemOnly filter as configured. Returns the number of
// rows copied. The destination is wiped first so a half-finished prior
// attempt without a sentinel doesn't leave dangling rows; if the
// sentinel was successfully written, isAlreadyMigrated short-circuits
// before this runs.
func runOne(ctx context.Context, db *mongo.Database, m migration) (int64, error) {
	src := db.Collection(m.from)
	dst := db.Collection(m.to)

	// If the source is missing the destination must already be empty;
	// the registry creates the destinations on every boot but the
	// source is only present on long-lived deployments. Treat a missing
	// source as zero-row source rather than an error so fresh installs
	// (where `users` was never populated) can run the script as part
	// of provisioning automation without special-casing.
	srcCount, err := src.EstimatedDocumentCount(ctx)
	if err != nil {
		// `EstimatedDocumentCount` returns 0 on a missing collection in
		// modern Mongo; tolerate the legacy error path explicitly.
		log.Printf("source %s count error (treating as empty): %v", m.from, err)
		srcCount = 0
	}
	if srcCount == 0 {
		return 0, nil
	}

	// Wipe the destination first. The destination collection is created
	// empty by the registry on every boot — this guarantees a partial
	// prior attempt without a sentinel cannot leave the destination in
	// a half-populated state.
	if _, err := dst.DeleteMany(ctx, bson.M{}); err != nil {
		return 0, fmt.Errorf("wipe destination %s: %w", m.to, err)
	}

	filter := bson.M{}
	if m.systemOnly {
		filter = bson.M{"isSystem": true, "tenantId": ""}
	}

	cur, err := src.Find(ctx, filter)
	if err != nil {
		return 0, fmt.Errorf("read source %s: %w", m.from, err)
	}
	defer cur.Close(ctx)

	var (
		batch   []interface{}
		batchSz = 500
		total   int64
	)
	for cur.Next(ctx) {
		var doc bson.M
		if err := cur.Decode(&doc); err != nil {
			return total, fmt.Errorf("decode source %s: %w", m.from, err)
		}
		// Drop the legacy ObjectID so the destination assigns a fresh
		// `_id`. Reusing _id would prevent dual-reads against both
		// collections during validation since uniqueness is per-collection.
		delete(doc, "_id")
		if m.tierStamp != "" {
			doc["tier"] = m.tierStamp
		}
		batch = append(batch, doc)
		if len(batch) >= batchSz {
			if _, err := dst.InsertMany(ctx, batch); err != nil {
				return total, fmt.Errorf("insert batch into %s: %w", m.to, err)
			}
			total += int64(len(batch))
			batch = batch[:0]
		}
	}
	if err := cur.Err(); err != nil {
		return total, fmt.Errorf("iterate source %s: %w", m.from, err)
	}
	if len(batch) > 0 {
		if _, err := dst.InsertMany(ctx, batch); err != nil {
			return total, fmt.Errorf("insert tail into %s: %w", m.to, err)
		}
		total += int64(len(batch))
	}

	// Row-count assertion: the destination must have exactly the number
	// of rows we inserted. A mismatch means the source mutated mid-run
	// (a writer interleaved with the migration) — fail loudly so the
	// operator knows to re-run inside a maintenance window.
	gotDst, err := dst.CountDocuments(ctx, bson.M{})
	if err != nil {
		return total, fmt.Errorf("count destination %s: %w", m.to, err)
	}
	if gotDst != total {
		return total, fmt.Errorf("row-count mismatch: inserted=%d destination=%d (writer interleaved?)", total, gotDst)
	}
	return total, nil
}

func isAlreadyMigrated(ctx context.Context, db *mongo.Database) (bool, error) {
	n, err := db.Collection("system_init").CountDocuments(ctx,
		bson.M{"key": sentinelKey},
		options.Count().SetLimit(1),
	)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func writeSentinel(ctx context.Context, db *mongo.Database, counts map[string]int64) error {
	_, err := db.Collection("system_init").UpdateOne(ctx,
		bson.M{"key": sentinelKey},
		bson.M{"$setOnInsert": bson.M{
			"key":        sentinelKey,
			"counts":     counts,
			"migratedAt": time.Now().UTC(),
		}},
		options.Update().SetUpsert(true),
	)
	return err
}
