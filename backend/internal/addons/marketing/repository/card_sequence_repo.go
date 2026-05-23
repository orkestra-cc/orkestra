package repository

import (
	"context"
	"errors"
	"time"

	"github.com/orkestra-cc/orkestra-addon-marketing/models"
	"github.com/orkestra-cc/orkestra-sdk/tenantrepo"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// CardSequenceRepository is the persistence boundary for
// marketing_card_sequences.
//
// This collection is rebuildable from MAX(card.code) if a recovery
// is ever needed — the sequences themselves do not carry business
// data, only the next-counter-to-issue value. Loss of a sequence
// row only causes a future code collision (caught by the
// (tenantId, code) unique fail-safe), not data loss.
type CardSequenceRepository struct {
	coll *mongo.Collection
}

// NewCardSequenceRepository binds a repository to the
// marketing_card_sequences collection.
func NewCardSequenceRepository(db *mongo.Database) *CardSequenceRepository {
	return &CardSequenceRepository{coll: db.Collection(models.CardSequencesCollection)}
}

// NextSequence atomically advances and returns the PRE-increment
// counter value for the (tenant, cardTypeUuid) pair. The first call
// returns 0 (caller's responsibility to zero-pad to the template's
// declared width); subsequent calls return 1, 2, 3, ...
//
// Implementation: findAndModify with $inc:{nextSeq:1} + upsert. The
// returned document carries the PRE-update value of nextSeq, which
// is exactly the counter the caller should render into the code.
//
// The operation is atomic; concurrent callers serialise on the
// underlying findAndModify so no two cards of the same type ever
// receive the same sequence value.
func (r *CardSequenceRepository) NextSequence(
	ctx context.Context, cardTypeUUID string,
) (int64, error) {
	if cardTypeUUID == "" {
		return 0, errors.New("marketing: empty cardTypeUuid")
	}
	tenantID, err := tenantrepo.StampInsert(ctx)
	if err != nil {
		return 0, err
	}

	filter := bson.M{
		"tenantId":     tenantID,
		"cardTypeUuid": cardTypeUUID,
	}
	now := time.Now().UTC()
	update := bson.M{
		"$inc": bson.M{"nextSeq": 1},
		"$setOnInsert": bson.M{
			// uuid is a stable identifier for the counter row; the
			// (tenantId, cardTypeUuid) unique index is the real key.
			"uuid":         "seq-" + cardTypeUUID,
			"tenantId":     tenantID,
			"cardTypeUuid": cardTypeUUID,
		},
		"$set": bson.M{"updatedAt": now},
	}
	opts := options.FindOneAndUpdate().
		SetUpsert(true).
		SetReturnDocument(options.Before)

	var got models.CardSequence
	err = r.coll.FindOneAndUpdate(ctx, filter, update, opts).Decode(&got)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			// First call for this (tenant, type) — Before returns no
			// document because nothing existed yet. The post-update
			// row has nextSeq=1; the value we want to return is 0.
			return 0, nil
		}
		return 0, err
	}
	return got.NextSeq, nil
}

// Reset removes the counter for the given cardTypeUuid in the
// caller's tenant scope. Used by the future "delete card type"
// flow when the operator confirms the deletion (the type has zero
// cards, so the counter is dead weight). Safe to no-op when the
// row does not exist.
func (r *CardSequenceRepository) Reset(ctx context.Context, cardTypeUUID string) error {
	filter, err := tenantrepo.Scope(ctx, bson.M{"cardTypeUuid": cardTypeUUID})
	if err != nil {
		return err
	}
	_, err = r.coll.DeleteOne(ctx, filter)
	return err
}
