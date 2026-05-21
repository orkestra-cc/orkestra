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

// ErrScoreSnapshotNotFound is returned when a snapshot lookup misses
// in the caller's tenant scope.
var ErrScoreSnapshotNotFound = errors.New("marketing: score snapshot not found")

// ScoreSnapshotRepository persists marketing_score_snapshots rows.
// Snapshots are a rebuildable cache (decision D13): every row is
// reproducible from the activity log + the matching score profile,
// so DELETE-everything is a recovery option, not a destructive one.
type ScoreSnapshotRepository struct {
	coll *mongo.Collection
}

// NewScoreSnapshotRepository binds the repository to
// marketing_score_snapshots.
func NewScoreSnapshotRepository(db *mongo.Database) *ScoreSnapshotRepository {
	return &ScoreSnapshotRepository{coll: db.Collection(models.ScoreSnapshotsCollection)}
}

// Upsert writes (or replaces) the snapshot for the
// (tenant, person, profile) tuple. The unique index on those three
// fields guarantees there is at most one row per tuple; concurrent
// eager + nightly recomputers settle deterministically on the same
// document (latest write wins, which is correct because the value
// is reproducible).
//
// The caller is responsible for populating every field; the
// repository stamps tenantId from context, sets ComputedAt to now
// when not pre-populated, and Stale to false on every write.
func (r *ScoreSnapshotRepository) Upsert(ctx context.Context, s *models.ScoreSnapshot) error {
	if s == nil {
		return errors.New("marketing: nil score snapshot")
	}
	tenantID, err := tenantrepo.StampInsert(ctx)
	if err != nil {
		return err
	}
	s.TenantID = tenantID
	if s.ComputedAt.IsZero() {
		s.ComputedAt = time.Now().UTC()
	}
	// The Upsert path always lands a fresh snapshot — by construction
	// the writer has just recomputed against the current profile
	// version, so the row is no longer stale.
	s.Stale = false

	filter := bson.M{
		"tenantId":    tenantID,
		"personUuid":  s.PersonUUID,
		"profileUuid": s.ProfileUUID,
	}
	opts := options.Replace().SetUpsert(true)
	_, err = r.coll.ReplaceOne(ctx, filter, s, opts)
	return err
}

// GetByPersonProfile returns the snapshot for (person, profile) in
// the caller's tenant, or ErrScoreSnapshotNotFound when no row
// exists yet (first recompute has not run for this pair).
func (r *ScoreSnapshotRepository) GetByPersonProfile(ctx context.Context, personUUID, profileUUID string) (*models.ScoreSnapshot, error) {
	filter, err := tenantrepo.Scope(ctx, bson.M{
		"personUuid":  personUUID,
		"profileUuid": profileUUID,
	})
	if err != nil {
		return nil, err
	}
	var out models.ScoreSnapshot
	if err := r.coll.FindOne(ctx, filter).Decode(&out); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrScoreSnapshotNotFound
		}
		return nil, err
	}
	return &out, nil
}

// GetByUUID returns the snapshot by UUID. Used by the breakdown
// drawer in the admin UI, which links to a specific snapshot rather
// than re-resolving (person, profile).
func (r *ScoreSnapshotRepository) GetByUUID(ctx context.Context, uuid string) (*models.ScoreSnapshot, error) {
	filter, err := tenantrepo.Scope(ctx, bson.M{"uuid": uuid})
	if err != nil {
		return nil, err
	}
	var out models.ScoreSnapshot
	if err := r.coll.FindOne(ctx, filter).Decode(&out); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrScoreSnapshotNotFound
		}
		return nil, err
	}
	return &out, nil
}

// ListForPerson returns every snapshot for the person across all
// profiles. Used by the per-contact "Scores" tab.
func (r *ScoreSnapshotRepository) ListForPerson(ctx context.Context, personUUID string) ([]models.ScoreSnapshot, error) {
	filter, err := tenantrepo.Scope(ctx, bson.M{"personUuid": personUUID})
	if err != nil {
		return nil, err
	}
	cur, err := r.coll.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	out := make([]models.ScoreSnapshot, 0)
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// LeaderboardFilter parameterises the per-profile leaderboard read.
type LeaderboardFilter struct {
	ProfileUUID    string
	ApplicableOnly bool // skip rows where the person fails the profile filter
	Limit          int64
	Skip           int64
}

// Leaderboard returns snapshots for the profile ordered by Value
// descending. Default limit 50, capped at 500 — leaderboards beyond
// 500 are pagination work, not single-query work.
func (r *ScoreSnapshotRepository) Leaderboard(ctx context.Context, f LeaderboardFilter) ([]models.ScoreSnapshot, error) {
	base := bson.M{"profileUuid": f.ProfileUUID}
	if f.ApplicableOnly {
		base["applicable"] = true
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
		SetSort(bson.D{{Key: "value", Value: -1}}).
		SetLimit(limit).
		SetSkip(f.Skip)
	cur, err := r.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	out := make([]models.ScoreSnapshot, 0)
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListStale returns up to `limit` snapshots flagged Stale=true,
// across every profile. The nightly RecomputeJob iterates this in
// batches until it returns zero.
func (r *ScoreSnapshotRepository) ListStale(ctx context.Context, limit int64) ([]models.ScoreSnapshot, error) {
	filter, err := tenantrepo.Scope(ctx, bson.M{"stale": true})
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}
	opts := options.Find().SetLimit(limit)
	cur, err := r.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	out := make([]models.ScoreSnapshot, 0)
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// MarkStaleByProfile flips Stale=true on every snapshot for the
// profile. Called by ScoreProfileService.Save after BumpVersion so
// the nightly job + the next eager hit on each person settle the
// recompute.
func (r *ScoreSnapshotRepository) MarkStaleByProfile(ctx context.Context, profileUUID string) (int64, error) {
	filter, err := tenantrepo.Scope(ctx, bson.M{"profileUuid": profileUUID})
	if err != nil {
		return 0, err
	}
	res, err := r.coll.UpdateMany(ctx, filter, bson.M{"$set": bson.M{"stale": true}})
	if err != nil {
		return 0, err
	}
	return res.ModifiedCount, nil
}

// DeleteByPersonUUID removes every snapshot for the person. Used in
// the GDPR right-to-be-forgotten cascade and when the person is
// hard-deleted.
func (r *ScoreSnapshotRepository) DeleteByPersonUUID(ctx context.Context, personUUID string) (int64, error) {
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

// DeleteByProfileUUID removes every snapshot for the profile. Called
// by ScoreProfileService.Delete; if the profile is later recreated
// the snapshots regenerate from the activity log via the next eager
// or nightly recompute.
func (r *ScoreSnapshotRepository) DeleteByProfileUUID(ctx context.Context, profileUUID string) (int64, error) {
	filter, err := tenantrepo.Scope(ctx, bson.M{"profileUuid": profileUUID})
	if err != nil {
		return 0, err
	}
	res, err := r.coll.DeleteMany(ctx, filter)
	if err != nil {
		return 0, err
	}
	return res.DeletedCount, nil
}
