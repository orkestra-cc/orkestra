package repository

import (
	"context"
	"errors"
	"time"

	"github.com/orkestra/backend/internal/core/compliance/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// ErrLegalHoldNotFound is returned when releasing a hold that doesn't exist
// or is already released.
var ErrLegalHoldNotFound = errors.New("compliance: legal hold not found or already released")

// LegalHoldRepository persists litigation/investigation holds.
type LegalHoldRepository struct {
	coll *mongo.Collection
}

// NewLegalHoldRepo returns a repository bound to the legal-holds collection.
func NewLegalHoldRepo(db *mongo.Database) *LegalHoldRepository {
	return &LegalHoldRepository{coll: db.Collection(models.LegalHoldsCollection)}
}

// Insert appends a new hold.
func (r *LegalHoldRepository) Insert(ctx context.Context, h *models.LegalHold) error {
	_, err := r.coll.InsertOne(ctx, h)
	return err
}

// IsHeld reports whether the subject has any active hold. Platform-wide by
// design: a hold blocks erasure regardless of which tenant placed it.
func (r *LegalHoldRepository) IsHeld(ctx context.Context, userUUID string) (bool, error) {
	//tenantscope:allow a legal hold blocks erasure platform-wide; the query pins userUuid.
	n, err := r.coll.CountDocuments(ctx, bson.M{"userUuid": userUUID, "active": true})
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// ListActive lists active holds, optionally filtered to one subject.
func (r *LegalHoldRepository) ListActive(ctx context.Context, userUUID string) ([]models.LegalHold, error) {
	filter := bson.M{"active": true}
	if userUUID != "" {
		filter["userUuid"] = userUUID
	}
	//tenantscope:allow legal-hold admin read spans tenants by design.
	cur, err := r.coll.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []models.LegalHold
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Release marks a hold inactive. Returns ErrLegalHoldNotFound when no active
// hold with that UUID exists.
func (r *LegalHoldRepository) Release(ctx context.Context, uuid, releasedBy, reason string) error {
	now := time.Now().UTC()
	//tenantscope:allow releases a specific legal hold by its own UUID — cross-tenant by design.
	res, err := r.coll.UpdateOne(ctx,
		bson.M{"uuid": uuid, "active": true},
		bson.M{"$set": bson.M{
			"active":        false,
			"releasedAt":    now,
			"releasedBy":    releasedBy,
			"releaseReason": reason,
		}},
	)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrLegalHoldNotFound
	}
	return nil
}
