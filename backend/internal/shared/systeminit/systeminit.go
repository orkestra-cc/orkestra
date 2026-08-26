// Package systeminit owns the platform's two-phase bootstrap sequence.
//
// Two separate but related lifecycle events live in a single `system_init`
// MongoDB collection with different document keys:
//
// **first_admin lifecycle**: The "first user becomes super_admin" rule has
// two callers — the setup wizard (`POST /v1/setup/admin`) and ordinary
// signup (`POST /v1/auth/register`) — and both historically used a
// `GetUserCount() == 0` check immediately followed by a user create. That's
// a TOCTOU race: two concurrent signups on a fresh install can both see
// count==0 and both be granted super_admin. The patch is a single document
// with `key: first_admin` and unique-indexed `key`; the first caller to
// upsert it wins (CAS primitive). Value: *Repo implements
// auth/services.FirstAdminClaimer. Registered in the service registry as
// module.ServiceFirstAdminClaimer.
//
// **setup_finalization lifecycle**: A resumable setup saga that tracks
// default tenant provisioning and onboarding state across reboots. The
// document carries `key: setup_finalization` and records user/tenant
// creation during the setup flow so a retried `POST /v1/setup/complete`
// can idempotently finalize without recreating them. Consumers (setup
// service, tenant module Init for boot reconciliation) resolve the
// FinalizationStore interface from the service registry as
// module.ServiceSetupFinalizationStore. Value: *Repo implements
// FinalizationStore (CAS/lease primitives).
//
// The Repo is constructed by main.go before the module registry so both
// auth and setup can resolve it as a hard dependency. It is registered
// under two keys (ServiceFirstAdminClaimer, ServiceSetupFinalizationStore)
// so different consumers can discover it without cross-module imports.
package systeminit

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	// collection is the single MongoDB collection this package owns. Plural
	// or prefixed names would signal "more to come"; this really is one doc
	// for one platform invariant, so a plain name is correct.
	collection = "system_init"

	// keyFirstAdmin is the fixed document key for the first-admin seat. If
	// we ever need another platform-wide seat (e.g. "first tenant"), use a
	// new key rather than reusing this one.
	keyFirstAdmin = "first_admin"
)

// Repo owns the system_init collection.
type Repo struct {
	coll *mongo.Collection
}

// NewRepo returns a Repo and ensures the `key` unique index exists. The
// index-creation call is idempotent; safe to call on every boot. If the
// index creation fails the caller gets an error rather than a silently
// broken claim path, because without the unique index the race we're
// defending against returns.
func NewRepo(ctx context.Context, db *mongo.Database) (*Repo, error) {
	coll := db.Collection(collection)
	_, err := coll.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "key", Value: 1}},
		Options: options.Index().SetUnique(true).SetName("key_unique"),
	})
	if err != nil {
		return nil, fmt.Errorf("systeminit: ensure index: %w", err)
	}
	return &Repo{coll: coll}, nil
}
