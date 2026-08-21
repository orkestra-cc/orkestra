// Package repository owns the Mongo persistence for the logging
// core module's single-document log_levels collection. ADR-0005
// Phase F.
package repository

import (
	"context"
	"errors"

	"github.com/orkestra/backend/internal/core/logging/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ErrNotFound is returned by Get when the single config document
// hasn't been seeded yet. Callers (the service) treat this as "use
// env-driven defaults".
var ErrNotFound = errors.New("log_levels: config not found")

// Repository abstracts the Mongo collection so the service can be
// unit-tested with a fake. The fake lives next to its consumer in
// services_test.go.
type Repository interface {
	Get(ctx context.Context) (*models.LogLevelDoc, error)
	CompareAndSwap(ctx context.Context, expectedRevision int64, doc *models.LogLevelDoc) (bool, error)
}

type mongoRepo struct {
	coll *mongo.Collection
}

// NewMongoRepository binds to the supplied collection. The caller
// (the module's Init) is responsible for declaring the collection
// in Collections() so the registry auto-creates it with the
// required unique index on _id.
func NewMongoRepository(coll *mongo.Collection) Repository {
	return &mongoRepo{coll: coll}
}

// Get returns the single document. Filters by the fixed sentinel
// _id so concurrent writers can't accidentally produce more than
// one document.
func (r *mongoRepo) Get(ctx context.Context) (*models.LogLevelDoc, error) {
	var doc models.LogLevelDoc
	//tenantscope:allow log_levels is a single global system-config document (_id="default"), not tenant-data — see backend/internal/core/logging/CLAUDE.md "What this module does NOT do".
	err := r.coll.FindOne(ctx, bson.M{"_id": models.DefaultConfigKey}).Decode(&doc)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &doc, nil
}

// CompareAndSwap replaces the entire document only when revision still
// matches the caller's authoritative read. A legacy document with no revision
// is treated as revision zero. The initial insert uses an upsert; a duplicate
// _id means another replica won that race and is reported as a clean CAS miss.
func (r *mongoRepo) CompareAndSwap(ctx context.Context, expectedRevision int64, doc *models.LogLevelDoc) (bool, error) {
	if doc.ConfigKey == "" {
		doc.ConfigKey = models.DefaultConfigKey
	}
	filter := bson.M{"_id": doc.ConfigKey}
	opts := options.Replace()
	if expectedRevision == 0 {
		filter["$or"] = bson.A{
			bson.M{"revision": int64(0)},
			bson.M{"revision": bson.M{"$exists": false}},
		}
		opts.SetUpsert(true)
	} else {
		filter["revision"] = expectedRevision
	}
	//tenantscope:allow log_levels is a single global system-config document (_id="default"), not tenant-data — see backend/internal/core/logging/CLAUDE.md "What this module does NOT do".
	result, err := r.coll.ReplaceOne(ctx, filter, doc, opts)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return false, nil
		}
		return false, err
	}
	return result.MatchedCount == 1 || result.UpsertedCount == 1, nil
}
