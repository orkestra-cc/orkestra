package repository

import (
	"context"
	"errors"
	"time"

	"github.com/orkestra/backend/internal/core/compliance/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
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

// heldFilter matches the holds that currently count: active, and either
// without an expiry or not yet expired at now. IsHeld and ListActive share it
// so an expired hold stops blocking at its instant, before the expiry job
// records the release.
func heldFilter(now time.Time) bson.M {
	return bson.M{
		"active": true,
		"$or": bson.A{
			bson.M{"expiresAt": nil}, // absent or null
			bson.M{"expiresAt": bson.M{"$gt": now}},
		},
	}
}

// IsHeld reports whether the subject has any hold in force at now.
// Platform-wide by design: a hold blocks erasure regardless of which tenant
// placed it.
func (r *LegalHoldRepository) IsHeld(ctx context.Context, userUUID string, now time.Time) (bool, error) {
	filter := heldFilter(now)
	filter["userUuid"] = userUUID
	//tenantscope:allow a legal hold blocks erasure platform-wide; the query pins userUuid.
	n, err := r.coll.CountDocuments(ctx, filter)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// ListActive lists the holds in force at now, optionally filtered to one subject.
func (r *LegalHoldRepository) ListActive(ctx context.Context, userUUID string, now time.Time) ([]models.LegalHold, error) {
	filter := heldFilter(now)
	if userUUID != "" {
		filter["userUuid"] = userUUID
	}
	return r.find(ctx, filter)
}

// List lists holds, newest first, optionally filtered to one subject. With
// includeReleased it returns released and expired holds too; otherwise the
// holds in force at now.
func (r *LegalHoldRepository) List(ctx context.Context, userUUID string, includeReleased bool, now time.Time) ([]models.LegalHold, error) {
	filter := bson.M{}
	if !includeReleased {
		filter = heldFilter(now)
	}
	if userUUID != "" {
		filter["userUuid"] = userUUID
	}
	return r.find(ctx, filter)
}

func (r *LegalHoldRepository) find(ctx context.Context, filter bson.M) ([]models.LegalHold, error) {
	//tenantscope:allow legal-hold admin read spans tenants by design.
	cur, err := r.coll.Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "placedAt", Value: -1}, {Key: "uuid", Value: -1}}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	out := []models.LegalHold{}
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Update applies set/unset to a hold in force and returns the updated row;
// ErrLegalHoldNotFound when no active hold has that UUID.
func (r *LegalHoldRepository) Update(ctx context.Context, uuid string, set bson.M, unset []string, now time.Time) (*models.LegalHold, error) {
	upd := bson.M{"$set": set}
	if len(unset) > 0 {
		u := bson.M{}
		for _, f := range unset {
			u[f] = ""
		}
		upd["$unset"] = u
	}
	return r.updateActive(ctx, uuid, upd, now)
}

// AddNote appends a note to an active hold and returns the updated row.
func (r *LegalHoldRepository) AddNote(ctx context.Context, uuid string, note models.LegalHoldNote, now time.Time) (*models.LegalHold, error) {
	return r.updateActive(ctx, uuid, bson.M{
		"$push": bson.M{"notes": note},
		"$set":  bson.M{"updatedAt": note.At},
	}, now)
}

// updateActive only touches a hold in force at now: a lapsed one (active in
// the row, past its expiry) cannot be revived by an edit or a note; it can
// still be released.
func (r *LegalHoldRepository) updateActive(ctx context.Context, uuid string, upd bson.M, now time.Time) (*models.LegalHold, error) {
	filter := heldFilter(now)
	filter["uuid"] = uuid
	var out models.LegalHold
	//tenantscope:allow updates a specific legal hold by its own UUID — cross-tenant by design.
	err := r.coll.FindOneAndUpdate(ctx, filter, upd,
		options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&out)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrLegalHoldNotFound
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// Get returns one hold, active or not; ErrLegalHoldNotFound when absent.
func (r *LegalHoldRepository) Get(ctx context.Context, uuid string) (*models.LegalHold, error) {
	var out models.LegalHold
	//tenantscope:allow reads a specific legal hold by its own UUID — cross-tenant by design.
	err := r.coll.FindOne(ctx, bson.M{"uuid": uuid}).Decode(&out)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrLegalHoldNotFound
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// ExpireDue releases up to limit active holds whose expiresAt has passed at
// now and returns exactly the rows this call released. Each hold is released
// by its own compare-and-set (active:true and expiresAt <= now), so two
// replicas running at once release and report every hold once between them.
func (r *LegalHoldRepository) ExpireDue(ctx context.Context, now time.Time, limit int) ([]models.LegalHold, error) {
	released := []models.LegalHold{}
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After).
		SetSort(bson.D{{Key: "expiresAt", Value: 1}})
	for len(released) < limit {
		if err := ctx.Err(); err != nil {
			return released, err
		}
		var h models.LegalHold
		//tenantscope:allow the expiry job releases due holds across every tenant by design.
		err := r.coll.FindOneAndUpdate(ctx,
			bson.M{"active": true, "expiresAt": bson.M{"$lte": now}},
			bson.M{"$set": bson.M{
				"active":        false,
				"releasedAt":    now,
				"releasedBy":    models.LegalHoldExpiryActor,
				"releaseReason": "expired",
			}}, opts).Decode(&h)
		if errors.Is(err, mongo.ErrNoDocuments) {
			return released, nil
		}
		if err != nil {
			return released, err
		}
		released = append(released, h)
	}
	return released, nil
}

// Release marks a hold inactive. Returns ErrLegalHoldNotFound when no active
// hold with that UUID exists.
func (r *LegalHoldRepository) Release(ctx context.Context, uuid, releasedBy, reason string, now time.Time) error {
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
