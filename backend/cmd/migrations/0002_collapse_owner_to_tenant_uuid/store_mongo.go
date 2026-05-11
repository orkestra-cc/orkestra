package main

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/orkestra/backend/cmd/migrations/0002_collapse_owner_to_tenant_uuid/migrator"
)

// sentinelCollection is the same collection 0001 stamped its per-row
// sentinels into. 0002 lands a single document keyed by migration name —
// no per-row sentinel because the rename is a whole-collection operation.
const sentinelCollection = "migrations_applied"

type mongoStore struct {
	db *mongo.Database
}

func newMongoStore(db *mongo.Database) *mongoStore { return &mongoStore{db: db} }

func (s *mongoStore) SentinelExists(ctx context.Context) (bool, error) {
	n, err := s.db.Collection(sentinelCollection).CountDocuments(ctx, bson.M{
		"migration": migrator.MigrationName,
	}, options.Count().SetLimit(1))
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// RenameOwnerToTenantUUID rewrites every doc in the collection that still
// carries the legacy `ownerUUID` field. The pipeline copies its value into
// `tenantUUID`, then drops both legacy fields. Filter is `ownerUUID:
// {$exists: true}` so a re-run after partial completion naturally skips
// already-renamed docs (zero matches, zero modified).
//
// We use an aggregation-pipeline update so $set and $unset can run in the
// same write — Mongo guarantees all the per-document operations land
// atomically.
func (s *mongoStore) RenameOwnerToTenantUUID(ctx context.Context, collection string) (int64, int64, error) {
	filter := bson.M{"ownerUUID": bson.M{"$exists": true}}
	pipeline := bson.A{
		bson.M{"$set": bson.M{"tenantUUID": "$ownerUUID"}},
		bson.M{"$unset": bson.A{"ownerKind", "ownerUUID"}},
	}
	res, err := s.db.Collection(collection).UpdateMany(ctx, filter, pipeline)
	if err != nil {
		return 0, 0, err
	}
	return res.MatchedCount, res.ModifiedCount, nil
}

// MarkSentinel inserts the completion record. The record carries every
// collection's matched/modified counts so an auditor can reconstruct what
// happened without replaying logs.
func (s *mongoStore) MarkSentinel(ctx context.Context, results []migrator.CollectionResult) error {
	collections := make([]bson.M, 0, len(results))
	for _, r := range results {
		collections = append(collections, bson.M{
			"name":     r.Collection,
			"matched":  r.Matched,
			"modified": r.Modified,
		})
	}
	_, err := s.db.Collection(sentinelCollection).InsertOne(ctx, bson.M{
		"migration":   migrator.MigrationName,
		"appliedAt":   time.Now().UTC(),
		"collections": collections,
	})
	return err
}
