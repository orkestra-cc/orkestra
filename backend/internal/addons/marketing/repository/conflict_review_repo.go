// Package repository — conflict_review_repo.go is the persistence
// boundary for marketing_conflict_reviews. Rows are written once by
// the importer pipeline when it parks a blocking dedup conflict; they
// transition to resolved or dismissed exactly once via the resolver
// handler. The collection has no general Update — the only mutations
// are the two narrow MarkResolved / MarkDismissed transitions.
package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra-cc/orkestra-addon-marketing/models"
	"github.com/orkestra-cc/orkestra-sdk/tenantrepo"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ErrConflictReviewNotFound is returned when a lookup misses in the
// caller's tenant scope.
var ErrConflictReviewNotFound = errors.New("marketing: conflict review not found")

// ErrConflictReviewNotPending is returned when MarkResolved /
// MarkDismissed targets a row that has already been closed. The
// service surface translates this into a 409 so the operator sees a
// "someone else just resolved this" message instead of a silent no-op.
var ErrConflictReviewNotPending = errors.New("marketing: conflict review already resolved")

// ConflictReviewRepository persists marketing_conflict_reviews rows.
type ConflictReviewRepository struct {
	coll *mongo.Collection
}

// NewConflictReviewRepository binds the repository to the collection.
func NewConflictReviewRepository(db *mongo.Database) *ConflictReviewRepository {
	return &ConflictReviewRepository{coll: db.Collection(models.ConflictReviewsCollection)}
}

// Create inserts a fresh review row. The caller is responsible for
// every payload field; the repository mints UUID + tenantId + the two
// timestamps and forces Status to pending.
func (r *ConflictReviewRepository) Create(ctx context.Context, c *models.ConflictReview) error {
	if c == nil {
		return errors.New("marketing: nil conflict review")
	}
	tenantID, err := tenantrepo.StampInsert(ctx)
	if err != nil {
		return err
	}
	c.TenantID = tenantID
	if c.UUID == "" {
		c.UUID = uuid.New().String()
	}
	now := time.Now().UTC()
	if c.CreatedAt.IsZero() {
		c.CreatedAt = now
	}
	c.UpdatedAt = now
	c.Status = models.ConflictReviewStatusPending
	if err := c.Validate(); err != nil {
		return err
	}
	_, err = r.coll.InsertOne(ctx, c)
	return err
}

// GetByUUID returns one review by UUID in the caller's tenant.
func (r *ConflictReviewRepository) GetByUUID(ctx context.Context, uuid string) (*models.ConflictReview, error) {
	filter, err := tenantrepo.Scope(ctx, bson.M{"uuid": uuid})
	if err != nil {
		return nil, err
	}
	var out models.ConflictReview
	if err := r.coll.FindOne(ctx, filter).Decode(&out); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrConflictReviewNotFound
		}
		return nil, err
	}
	return &out, nil
}

// ConflictReviewListFilter parameterises the queue read surface. Empty
// values disable the corresponding filter.
type ConflictReviewListFilter struct {
	Status        models.ConflictReviewStatus
	TargetKind    models.ConflictTargetKind
	ImportJobUUID string
	ExistingUUID  string
	Limit         int64
	Skip          int64
}

// List returns reviews matching filter, newest-first by createdAt.
// Limit defaults to 50 (cap 500).
func (r *ConflictReviewRepository) List(ctx context.Context, f ConflictReviewListFilter) ([]models.ConflictReview, error) {
	base := bson.M{}
	if f.Status != "" {
		base["status"] = f.Status
	}
	if f.TargetKind != "" {
		base["targetKind"] = f.TargetKind
	}
	if f.ImportJobUUID != "" {
		base["importJobUuid"] = f.ImportJobUUID
	}
	if f.ExistingUUID != "" {
		base["existingUuid"] = f.ExistingUUID
	}
	filter, err := tenantrepo.Scope(ctx, base)
	if err != nil {
		return nil, err
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	opts := options.Find().
		SetSort(bson.D{{Key: "createdAt", Value: -1}}).
		SetLimit(limit).
		SetSkip(f.Skip)
	cur, err := r.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	out := make([]models.ConflictReview, 0)
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListPendingForJob returns every pending review attached to the
// given import job. No pagination — a single job is bounded by the
// per-import-row count, which is itself capped by ImportJobStats.RowsRead.
func (r *ConflictReviewRepository) ListPendingForJob(ctx context.Context, jobUUID string) ([]models.ConflictReview, error) {
	filter, err := tenantrepo.Scope(ctx, bson.M{
		"importJobUuid": jobUUID,
		"status":        models.ConflictReviewStatusPending,
	})
	if err != nil {
		return nil, err
	}
	opts := options.Find().SetSort(bson.D{{Key: "createdAt", Value: 1}})
	cur, err := r.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	out := make([]models.ConflictReview, 0)
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CountPendingForJob is the cheap "is the queue drained" check the
// worker calls after each resolve to decide whether the parent job
// can return to the running state.
func (r *ConflictReviewRepository) CountPendingForJob(ctx context.Context, jobUUID string) (int64, error) {
	filter, err := tenantrepo.Scope(ctx, bson.M{
		"importJobUuid": jobUUID,
		"status":        models.ConflictReviewStatusPending,
	})
	if err != nil {
		return 0, err
	}
	return r.coll.CountDocuments(ctx, filter)
}

// MarkResolved transitions a pending review to resolved, recording the
// resolution + the operator UUID + optional notes. Returns
// ErrConflictReviewNotPending if the row was already closed (the
// {"status":"pending"} clause in the filter is the atomic guard).
func (r *ConflictReviewRepository) MarkResolved(ctx context.Context, reviewUUID string, resolution models.ConflictResolution, userID, notes string) error {
	if err := resolution.Validate(); err != nil {
		return err
	}
	filter, err := tenantrepo.Scope(ctx, bson.M{
		"uuid":   reviewUUID,
		"status": models.ConflictReviewStatusPending,
	})
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	patch := bson.M{
		"status":        models.ConflictReviewStatusResolved,
		"resolution":    resolution,
		"resolvedAt":    now,
		"resolvedBy":    userID,
		"resolvedNotes": notes,
		"updatedAt":     now,
	}
	res, err := r.coll.UpdateOne(ctx, filter, bson.M{"$set": patch})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		// Determine which case: missing vs already-resolved.
		if _, err := r.GetByUUID(ctx, reviewUUID); err != nil {
			return err
		}
		return ErrConflictReviewNotPending
	}
	return nil
}

// MarkDismissed transitions a pending review to dismissed.
// Returns ErrConflictReviewNotPending if already closed.
func (r *ConflictReviewRepository) MarkDismissed(ctx context.Context, reviewUUID, userID, notes string) error {
	filter, err := tenantrepo.Scope(ctx, bson.M{
		"uuid":   reviewUUID,
		"status": models.ConflictReviewStatusPending,
	})
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	patch := bson.M{
		"status":        models.ConflictReviewStatusDismissed,
		"resolvedAt":    now,
		"resolvedBy":    userID,
		"resolvedNotes": notes,
		"updatedAt":     now,
	}
	res, err := r.coll.UpdateOne(ctx, filter, bson.M{"$set": patch})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		if _, err := r.GetByUUID(ctx, reviewUUID); err != nil {
			return err
		}
		return ErrConflictReviewNotPending
	}
	return nil
}
