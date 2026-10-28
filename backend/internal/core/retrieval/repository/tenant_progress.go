package repository

import (
	"context"
	"errors"
	"time"

	"github.com/orkestra/backend/internal/core/retrieval/models"
	"github.com/orkestra/backend/pkg/sdk/module"
	"github.com/orkestra/backend/pkg/sdk/tenantrepo"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// CollTenantProgress is the per-tenant initial-build progress (tenant-scoped).
const CollTenantProgress = "retrieval_tenant_progress"

// TenantProgressIndexes are the declared indexes of CollTenantProgress; the
// module and the repository tests share them.
func TenantProgressIndexes() []module.IndexSpec {
	return []module.IndexSpec{
		{OrderedKeys: []module.IndexKey{{Field: "tenantId", Direction: 1}, {Field: "corpus", Direction: 1}, {Field: "generation", Direction: 1}}, Unique: true},
	}
}

// TenantProgress owns retrieval_tenant_progress: one row per (tenant, corpus,
// generation), leased and fenced by epoch like the outbox.
type TenantProgress struct{ coll *mongo.Collection }

func NewTenantProgress(db *mongo.Database) *TenantProgress {
	return &TenantProgress{coll: db.Collection(CollTenantProgress)}
}

// ClaimBuild leases the build row of the tenant in ctx, creating it on first
// use; nil, nil when the build is done or leased by another worker. It relies
// on the unique (tenantId, corpus, generation) index: an upsert that matches no
// claimable row while the key exists hits a duplicate key.
func (r *TenantProgress) ClaimBuild(ctx context.Context, corpus string, generation int, owner string, now time.Time, lease time.Duration) (*models.TenantProgress, error) {
	filter, err := tenantrepo.Scope(ctx, bson.M{
		"corpus": corpus, "generation": generation,
		"status": bson.M{"$ne": models.ProgressDone},
		"$or": []bson.M{
			{"leaseUntil": nil},
			{"leaseUntil": bson.M{"$lt": ms(now)}},
		},
	})
	if err != nil {
		return nil, err
	}
	update := bson.M{
		"$inc":         bson.M{"epoch": int64(1)},
		"$set":         bson.M{"leaseOwner": owner, "leaseUntil": ms(now.Add(lease)), "updatedAt": ms(now)},
		"$setOnInsert": bson.M{"status": models.ProgressBuilding, "cursor": "", "enqueued": int64(0), "startedAt": ms(now)},
	}
	var p models.TenantProgress
	err = r.coll.FindOneAndUpdate(ctx, filter, update,
		options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After)).Decode(&p)
	if mongo.IsDuplicateKeyError(err) || errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// Advance records the cursor and added count and releases the lease, fenced on
// (epoch, owner); done also marks the build finished. ErrLeaseLost when the
// lease was taken over.
func (r *TenantProgress) Advance(ctx context.Context, corpus string, generation int, epoch int64, owner, cursor string, added int64, done bool, now time.Time) error {
	filter, err := tenantrepo.Scope(ctx, bson.M{"corpus": corpus, "generation": generation, "epoch": epoch, "leaseOwner": owner})
	if err != nil {
		return err
	}
	set := bson.M{"cursor": cursor, "updatedAt": ms(now)}
	if done {
		set["status"] = models.ProgressDone
		set["doneAt"] = ms(now)
	}
	res, err := r.coll.UpdateOne(ctx, filter, bson.M{
		"$set":   set,
		"$inc":   bson.M{"enqueued": added},
		"$unset": bson.M{"leaseOwner": "", "leaseUntil": ""},
	})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrLeaseLost
	}
	return nil
}

// Get returns the build row of the tenant in ctx, or nil, nil.
func (r *TenantProgress) Get(ctx context.Context, corpus string, generation int) (*models.TenantProgress, error) {
	filter, err := tenantrepo.Scope(ctx, bson.M{"corpus": corpus, "generation": generation})
	if err != nil {
		return nil, err
	}
	var p models.TenantProgress
	err = r.coll.FindOne(ctx, filter).Decode(&p)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}
