package systeminit

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ClaimFirstAdmin atomically tries to register userUUID as the platform's
// first super_admin. It returns claimed=true only if this call performed the
// insert; concurrent callers see claimed=false and should fall back to a
// non-super role.
//
// Rollback: if the subsequent user-creation step fails, the caller MUST call
// Release(userUUID). Leaving the sentinel behind while no user exists bricks
// future signups into "operator" forever — the setup wizard would silently
// stop minting super_admins even on a DB that otherwise looks fresh.
func (r *Repo) ClaimFirstAdmin(ctx context.Context, userUUID string) (claimed bool, err error) {
	if userUUID == "" {
		return false, errors.New("systeminit: userUUID is required")
	}
	// Upsert with $setOnInsert is the atomic CAS: either this call inserts
	// the document (UpsertedCount == 1) or it doesn't (the sentinel already
	// exists, somebody else won).
	//tenantscope:allow system: platform-global first-admin sentinel keyed by fixed key (claim CAS upsert)
	res, err := r.coll.UpdateOne(ctx,
		bson.M{"key": keyFirstAdmin},
		bson.M{"$setOnInsert": bson.M{
			"key":       keyFirstAdmin,
			"userUUID":  userUUID,
			"claimedAt": time.Now().UTC(),
		}},
		options.Update().SetUpsert(true),
	)
	if err != nil {
		return false, fmt.Errorf("systeminit: claim first admin: %w", err)
	}
	return res.UpsertedCount > 0, nil
}

// Release removes the first-admin sentinel if — and only if — it currently
// points at userUUID. Used for rollback when the user-creation step fails
// after a successful claim. The guarded delete ensures a slow rollback
// from one caller cannot clobber a fresh sentinel already claimed by another.
func (r *Repo) Release(ctx context.Context, userUUID string) error {
	if userUUID == "" {
		return errors.New("systeminit: userUUID is required")
	}
	//tenantscope:allow system: platform-global first-admin sentinel keyed by fixed key (guarded rollback delete)
	_, err := r.coll.DeleteOne(ctx, bson.M{
		"key":      keyFirstAdmin,
		"userUUID": userUUID,
	})
	if err != nil {
		return fmt.Errorf("systeminit: release first admin: %w", err)
	}
	return nil
}
