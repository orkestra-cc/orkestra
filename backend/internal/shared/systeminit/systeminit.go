// Package systeminit owns the platform's one-time bootstrap sentinel.
//
// The "first user becomes super_admin" rule has two callers — the setup
// wizard (`POST /v1/setup/admin`) and ordinary signup (`POST /v1/auth/
// register`) — and both historically used a `GetUserCount() == 0` check
// immediately followed by a user create. That's a TOCTOU race: two concurrent
// signups on a fresh install can both see count==0 and both be granted
// super_admin. The patch is a single document in `system_init` with a
// unique-indexed `key`; the first caller to upsert it wins.
//
// The package is deliberately outside any Module so main.go can construct it
// next to the ModuleConfigRepository and inject it into both the setup
// service and the password auth service. It carries no business logic — only
// the atomic CAS contract.
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
