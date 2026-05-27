// Package repository owns Mongo persistence for the navigation module's
// ordering-override documents (Phase 2).
package repository

import (
	"context"
	"errors"
	"time"

	"github.com/orkestra/backend/internal/core/navigation/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ErrNotFound is returned by Get when no override doc exists for the
// requested parentKey. The service treats this as "no override — use
// declared order".
var ErrNotFound = errors.New("navigation_overrides: not found")

// OverrideRepository abstracts the navigation_overrides collection.
type OverrideRepository interface {
	ListAll(ctx context.Context) ([]models.NavOverride, error)
	Upsert(ctx context.Context, parentKey string, orderedChildren []string, updatedBy string) (*models.NavOverride, error)
	Delete(ctx context.Context, parentKey string) error
}

type mongoRepo struct {
	coll *mongo.Collection
}

// NewMongoRepository binds to the supplied collection. The caller (the
// navigation module's Init) is responsible for declaring the collection
// in Collections() so the registry auto-creates it.
func NewMongoRepository(coll *mongo.Collection) OverrideRepository {
	return &mongoRepo{coll: coll}
}

// ListAll returns every override document. The service builds an
// in-memory parentKey→ordering map from this slice; cheap because the
// collection is small (one doc per reordered parent, typically <50).
func (r *mongoRepo) ListAll(ctx context.Context) ([]models.NavOverride, error) {
	//tenantscope:allow navigation_overrides is global system config (one doc per parent menu node), not tenant-data — Phase 4 will introduce per-tenant scoping.
	cur, err := r.coll.Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []models.NavOverride
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Upsert replaces the override document for parentKey wholesale. The
// returned doc has UpdatedAt set to the persisted value so the handler
// can echo it back without a round-trip.
func (r *mongoRepo) Upsert(ctx context.Context, parentKey string, orderedChildren []string, updatedBy string) (*models.NavOverride, error) {
	doc := models.NavOverride{
		ParentKey:       parentKey,
		OrderedChildren: append([]string(nil), orderedChildren...), // copy so the caller's slice is not aliased
		UpdatedAt:       time.Now().UTC(),
		UpdatedBy:       updatedBy,
	}
	opts := options.Replace().SetUpsert(true)
	//tenantscope:allow navigation_overrides is global system config (one doc per parent menu node), not tenant-data — Phase 4 will introduce per-tenant scoping.
	if _, err := r.coll.ReplaceOne(ctx, bson.M{"_id": parentKey}, doc, opts); err != nil {
		return nil, err
	}
	return &doc, nil
}

// Delete removes the override for parentKey. A missing doc is not an
// error — the resulting state ("no override") is the same.
func (r *mongoRepo) Delete(ctx context.Context, parentKey string) error {
	//tenantscope:allow navigation_overrides is global system config (one doc per parent menu node), not tenant-data — Phase 4 will introduce per-tenant scoping.
	_, err := r.coll.DeleteOne(ctx, bson.M{"_id": parentKey})
	return err
}
