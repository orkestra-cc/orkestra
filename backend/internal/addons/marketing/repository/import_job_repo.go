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

// ErrImportJobNotFound is returned when an import-job lookup misses
// in the caller's tenant scope.
var ErrImportJobNotFound = errors.New("marketing: import job not found")

// ImportJobRepository persists import-job audit rows. Imports run
// synchronously inside the POST handler in Phase 1 — the repository
// is exposed so an async runner (Phase 2+) can take over without
// changing the read API.
type ImportJobRepository struct {
	coll *mongo.Collection
}

// NewImportJobRepository binds the repository to the
// marketing_import_jobs collection.
func NewImportJobRepository(db *mongo.Database) *ImportJobRepository {
	return &ImportJobRepository{coll: db.Collection(models.ImportJobsCollection)}
}

// Create persists a fresh job row. The caller mints the UUID and sets
// the initial status; the repository stamps tenantId + createdAt.
func (r *ImportJobRepository) Create(ctx context.Context, j *models.ImportJob) error {
	if j == nil {
		return errors.New("marketing: nil import job")
	}
	tenantID, err := tenantrepo.StampInsert(ctx)
	if err != nil {
		return err
	}
	j.TenantID = tenantID
	j.CreatedAt = time.Now().UTC()
	_, err = r.coll.InsertOne(ctx, j)
	return err
}

// GetByUUID returns one job by UUID.
func (r *ImportJobRepository) GetByUUID(ctx context.Context, uuid string) (*models.ImportJob, error) {
	filter, err := tenantrepo.Scope(ctx, bson.M{"uuid": uuid})
	if err != nil {
		return nil, err
	}
	var out models.ImportJob
	if err := r.coll.FindOne(ctx, filter).Decode(&out); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrImportJobNotFound
		}
		return nil, err
	}
	return &out, nil
}

// List returns recent jobs newest-first.
func (r *ImportJobRepository) List(ctx context.Context, limit, skip int64) ([]models.ImportJob, error) {
	filter, err := tenantrepo.Scope(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	opts := options.Find().
		SetSort(bson.D{{Key: "createdAt", Value: -1}}).
		SetLimit(limit).
		SetSkip(skip)
	cur, err := r.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	out := make([]models.ImportJob, 0)
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// UpdateStatus marks the job's transition and records the matching
// timestamp. Stats / error are written together so a single update
// captures the final state.
func (r *ImportJobRepository) UpdateStatus(ctx context.Context, uuid string, status models.ImportJobStatus, stats models.ImportJobStats, errMsg string) error {
	patch := bson.M{
		"status": status,
		"stats":  stats,
	}
	now := time.Now().UTC()
	if status == models.ImportJobStatusRunning {
		patch["startedAt"] = now
	}
	if status == models.ImportJobStatusDone || status == models.ImportJobStatusFailed {
		patch["completedAt"] = now
	}
	if errMsg != "" {
		patch["error"] = errMsg
	}
	filter, err := tenantrepo.Scope(ctx, bson.M{"uuid": uuid})
	if err != nil {
		return err
	}
	res, err := r.coll.UpdateOne(ctx, filter, bson.M{"$set": patch})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrImportJobNotFound
	}
	return nil
}
