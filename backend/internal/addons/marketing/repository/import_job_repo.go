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

// FindByIdempotencyKey returns the most recent job matching the key
// inside the lookback window. Empty key short-circuits to (nil, nil)
// because the caller MUST be able to compute one — surface that
// invariant violation as a not-found, not a panic.
func (r *ImportJobRepository) FindByIdempotencyKey(ctx context.Context, key string, lookback time.Duration) (*models.ImportJob, error) {
	if key == "" {
		return nil, nil
	}
	filter, err := tenantrepo.Scope(ctx, bson.M{
		"idempotencyKey": key,
		"createdAt":      bson.M{"$gte": time.Now().UTC().Add(-lookback)},
	})
	if err != nil {
		return nil, err
	}
	opts := options.FindOne().SetSort(bson.D{{Key: "createdAt", Value: -1}})
	var out models.ImportJob
	if err := r.coll.FindOne(ctx, filter, opts).Decode(&out); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return &out, nil
}

// AppendConflictReviewUUID adds the review UUID to the job's
// conflictReviewUuids array. Used by the pipeline whenever it parks
// a row in marketing_conflict_reviews; the worker reads
// CountPendingForJob on the review repo for the authoritative gate,
// but the array gives the UI a quick "which reviews came from this
// job" lookup without a second query.
func (r *ImportJobRepository) AppendConflictReviewUUID(ctx context.Context, jobUUID, reviewUUID string) error {
	filter, err := tenantrepo.Scope(ctx, bson.M{"uuid": jobUUID})
	if err != nil {
		return err
	}
	patch := bson.M{
		"$addToSet": bson.M{"conflictReviewUuids": reviewUUID},
		"$set":      bson.M{"updatedAt": time.Now().UTC()},
	}
	res, err := r.coll.UpdateOne(ctx, filter, patch)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrImportJobNotFound
	}
	return nil
}

// ListAcrossTenantsByStatus returns every job in the given status across
// all tenants. The ONE cross-tenant query in this repository — the worker
// calls it once at boot to repopulate the queue with persisted-as-queued
// jobs and to mark stuck-in-running jobs as runner_crash failures.
// Mirrors the bypass pattern documented on
// ScoreSnapshotRepository.ListStaleAcrossTenants.
//
// Caller is responsible for stamping each job's TenantID onto a fresh
// ctx (via ctxauth.KeyTenantID) before delegating downstream work to
// scope-aware methods.
func (r *ImportJobRepository) ListAcrossTenantsByStatus(ctx context.Context, status models.ImportJobStatus, limit int64) ([]models.ImportJob, error) {
	if !models.IsKnownImportJobStatus(status) {
		return nil, errors.New("marketing: unknown import job status")
	}
	if limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}
	opts := options.Find().
		SetSort(bson.D{{Key: "createdAt", Value: 1}}). // oldest first — preserve enqueue order
		SetLimit(limit)
	cur, err := r.coll.Find(ctx, bson.M{"status": status}, opts)
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

// MarkRunnerCrash transitions a job from running → failed with the
// reserved "runner_crash" error message. Used by the worker's boot
// recovery to salvage jobs the previous process couldn't finish.
func (r *ImportJobRepository) MarkRunnerCrash(ctx context.Context, jobUUID, tenantID string) error {
	if tenantID == "" {
		return errors.New("marketing: empty tenantId in MarkRunnerCrash")
	}
	now := time.Now().UTC()
	patch := bson.M{
		"status":      models.ImportJobStatusFailed,
		"error":       "runner_crash",
		"completedAt": now,
	}
	res, err := r.coll.UpdateOne(ctx,
		bson.M{"uuid": jobUUID, "tenantId": tenantID, "status": models.ImportJobStatusRunning},
		bson.M{"$set": patch},
	)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrImportJobNotFound
	}
	return nil
}
