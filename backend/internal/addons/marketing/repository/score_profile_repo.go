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

// ErrScoreProfileNotFound is returned when a score-profile lookup
// misses in the caller's tenant scope.
var ErrScoreProfileNotFound = errors.New("marketing: score profile not found")

// ScoreProfileRepository persists marketing_score_profiles rows.
type ScoreProfileRepository struct {
	coll *mongo.Collection
}

// NewScoreProfileRepository binds the repository to
// marketing_score_profiles.
func NewScoreProfileRepository(db *mongo.Database) *ScoreProfileRepository {
	return &ScoreProfileRepository{coll: db.Collection(models.ScoreProfilesCollection)}
}

// Create inserts a new profile. The caller mints UUID and Version
// (start at 1) and runs Validate() at the service layer. The
// repository stamps tenantId + timestamps.
func (r *ScoreProfileRepository) Create(ctx context.Context, p *models.ScoreProfile) error {
	if p == nil {
		return errors.New("marketing: nil score profile")
	}
	tenantID, err := tenantrepo.StampInsert(ctx)
	if err != nil {
		return err
	}
	p.TenantID = tenantID
	now := time.Now().UTC()
	p.CreatedAt = now
	p.UpdatedAt = now
	if p.Version == 0 {
		p.Version = 1
	}
	_, err = r.coll.InsertOne(ctx, p)
	return err
}

// GetByUUID returns the profile with the given UUID in the caller's
// tenant scope, or ErrScoreProfileNotFound when no document matches.
func (r *ScoreProfileRepository) GetByUUID(ctx context.Context, uuid string) (*models.ScoreProfile, error) {
	filter, err := tenantrepo.Scope(ctx, bson.M{"uuid": uuid})
	if err != nil {
		return nil, err
	}
	var out models.ScoreProfile
	if err := r.coll.FindOne(ctx, filter).Decode(&out); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrScoreProfileNotFound
		}
		return nil, err
	}
	return &out, nil
}

// GetByName looks up a profile by its tenant-unique name (slug-like
// identifier). Used by the seeder to detect whether the example
// "hot_lead" profile already exists on a tenant.
func (r *ScoreProfileRepository) GetByName(ctx context.Context, name string) (*models.ScoreProfile, error) {
	filter, err := tenantrepo.Scope(ctx, bson.M{"name": name})
	if err != nil {
		return nil, err
	}
	var out models.ScoreProfile
	if err := r.coll.FindOne(ctx, filter).Decode(&out); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrScoreProfileNotFound
		}
		return nil, err
	}
	return &out, nil
}

// ScoreProfileListFilter parameterises the list endpoint.
type ScoreProfileListFilter struct {
	ActiveOnly bool
	Limit      int64
	Skip       int64
}

// List returns profiles matching filter, ordered by Name ascending
// (deterministic UI sorting; profiles are administered, not browsed
// chronologically).
func (r *ScoreProfileRepository) List(ctx context.Context, f ScoreProfileListFilter) ([]models.ScoreProfile, error) {
	base := bson.M{}
	if f.ActiveOnly {
		base["active"] = true
	}
	filter, err := tenantrepo.Scope(ctx, base)
	if err != nil {
		return nil, err
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	opts := options.Find().
		SetSort(bson.D{{Key: "name", Value: 1}}).
		SetLimit(limit).
		SetSkip(f.Skip)
	cur, err := r.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	out := make([]models.ScoreProfile, 0)
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Replace persists the full profile document atomically. Used by the
// service layer on every Save — the service is responsible for
// calling BumpVersion(now) before invoking this, so Version and
// UpdatedAt are already monotonic by the time we write.
//
// The repository does not run a $set patch because rules / filters
// are full-replace semantics (operators editing rules expect the new
// list to be authoritative, not merged into the old one).
func (r *ScoreProfileRepository) Replace(ctx context.Context, p *models.ScoreProfile) error {
	if p == nil {
		return errors.New("marketing: nil score profile")
	}
	tenantID, ok := tenantrepo.CurrentTenantID(ctx)
	if !ok || tenantID == "" {
		return errors.New("marketing: missing tenant scope on Replace")
	}
	// Pin the tenantId on the document so a stray hand-edited body
	// can't be redirected to another tenant via Replace.
	p.TenantID = tenantID
	filter := bson.M{"tenantId": tenantID, "uuid": p.UUID}
	res, err := r.coll.ReplaceOne(ctx, filter, p)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrScoreProfileNotFound
	}
	return nil
}

// Delete removes the profile by UUID. The service layer is
// responsible for cascading marketing_score_snapshots (the snapshots
// repository exposes DeleteByProfileUUID).
func (r *ScoreProfileRepository) Delete(ctx context.Context, uuid string) error {
	filter, err := tenantrepo.Scope(ctx, bson.M{"uuid": uuid})
	if err != nil {
		return err
	}
	res, err := r.coll.DeleteOne(ctx, filter)
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return ErrScoreProfileNotFound
	}
	return nil
}
