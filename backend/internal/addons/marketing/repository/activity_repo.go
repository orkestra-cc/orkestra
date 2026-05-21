// Package repository — activity_repo.go is the persistence boundary
// for marketing_activities. The collection is append-only by contract
// (decision D22): the repository deliberately does NOT expose
// UpdateOne / DeleteOne / FindOneAndUpdate. Corrections happen via a
// new activity of Kind = KindCorrectedBy.
//
// The one DELETE escape hatch (HardDeleteByPersonUUID) exists for the
// GDPR right-to-be-forgotten cascade and is gated by the
// marketing.contact.delete permission at the handler layer.
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

// ErrActivityNotFound is returned when an activity lookup misses in
// the caller's tenant scope.
var ErrActivityNotFound = errors.New("marketing: activity not found")

// ActivityRepository persists rows of the append-only event log.
type ActivityRepository struct {
	coll *mongo.Collection
}

// NewActivityRepository binds the repository to marketing_activities.
func NewActivityRepository(db *mongo.Database) *ActivityRepository {
	return &ActivityRepository{coll: db.Collection(models.ActivitiesCollection)}
}

// Create inserts the activity, stamping tenantId + recordedAt. The
// caller is responsible for UUID, DedupKey, Kind/Source/PersonUUID,
// OccurredAt, and OrgUUID (derived from the person's primary+active
// membership at the service layer).
//
// Idempotence (decision D21): a duplicate dedupKey raises E11000 in
// Mongo; this method swallows the error and returns nil (no-op
// success). That is the contract that makes re-imports safe.
func (r *ActivityRepository) Create(ctx context.Context, a *models.Activity) error {
	if a == nil {
		return errors.New("marketing: nil activity")
	}
	tenantID, err := tenantrepo.StampInsert(ctx)
	if err != nil {
		return err
	}
	a.TenantID = tenantID
	if a.RecordedAt.IsZero() {
		a.RecordedAt = time.Now().UTC()
	}
	if _, err := r.coll.InsertOne(ctx, a); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return nil
		}
		return err
	}
	return nil
}

// GetByUUID returns one activity by UUID in the caller's tenant.
func (r *ActivityRepository) GetByUUID(ctx context.Context, uuid string) (*models.Activity, error) {
	filter, err := tenantrepo.Scope(ctx, bson.M{"uuid": uuid})
	if err != nil {
		return nil, err
	}
	var out models.Activity
	if err := r.coll.FindOne(ctx, filter).Decode(&out); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrActivityNotFound
		}
		return nil, err
	}
	return &out, nil
}

// ActivityListFilter parameterises the timeline read surface. Empty
// values disable the corresponding filter.
type ActivityListFilter struct {
	PersonUUID string
	OrgUUID    string
	Kinds      []models.ActivityKind
	Source     models.ActivitySource
	Since      time.Time // inclusive — activities with OccurredAt >= Since
	Until      time.Time // exclusive — activities with OccurredAt < Until
	Limit      int64
	Skip       int64
}

// List returns activities matching filter, ordered by OccurredAt
// descending. Limit defaults to 100 (cap 1000) — timelines tend to
// be deep, so the cap is higher than the contact-list 500 cap.
func (r *ActivityRepository) List(ctx context.Context, f ActivityListFilter) ([]models.Activity, error) {
	base := bson.M{}
	if f.PersonUUID != "" {
		base["personUuid"] = f.PersonUUID
	}
	if f.OrgUUID != "" {
		base["orgUuid"] = f.OrgUUID
	}
	if len(f.Kinds) > 0 {
		base["kind"] = bson.M{"$in": f.Kinds}
	}
	if f.Source != "" {
		base["source"] = f.Source
	}
	if !f.Since.IsZero() || !f.Until.IsZero() {
		occ := bson.M{}
		if !f.Since.IsZero() {
			occ["$gte"] = f.Since
		}
		if !f.Until.IsZero() {
			occ["$lt"] = f.Until
		}
		base["occurredAt"] = occ
	}
	filter, err := tenantrepo.Scope(ctx, base)
	if err != nil {
		return nil, err
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	opts := options.Find().
		SetSort(bson.D{{Key: "occurredAt", Value: -1}}).
		SetLimit(limit).
		SetSkip(f.Skip)
	cur, err := r.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	out := make([]models.Activity, 0)
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListAllForPerson returns every activity for the person, ordered by
// OccurredAt ascending. Used by ScoreService.recomputeOne for a
// from-scratch recompute against the full timeline. No limit — the
// score engine needs every row, and per-person volume is bounded
// (~hundreds per active contact even at five years of history).
func (r *ActivityRepository) ListAllForPerson(ctx context.Context, personUUID string) ([]models.Activity, error) {
	filter, err := tenantrepo.Scope(ctx, bson.M{"personUuid": personUUID})
	if err != nil {
		return nil, err
	}
	opts := options.Find().SetSort(bson.D{{Key: "occurredAt", Value: 1}})
	cur, err := r.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	out := make([]models.Activity, 0)
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// HardDeleteByPersonUUID removes every activity for the person. This
// is the GDPR right-to-be-forgotten cascade and the only DELETE path
// the repository exposes. Bound to the marketing.contact.delete
// permission at the handler layer; the service layer logs an audit
// trail before invoking this.
func (r *ActivityRepository) HardDeleteByPersonUUID(ctx context.Context, personUUID string) (int64, error) {
	filter, err := tenantrepo.Scope(ctx, bson.M{"personUuid": personUUID})
	if err != nil {
		return 0, err
	}
	res, err := r.coll.DeleteMany(ctx, filter)
	if err != nil {
		return 0, err
	}
	return res.DeletedCount, nil
}
