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

// ErrCustomFieldSchemaNotFound is returned when the lookup for the
// caller's tenant + target finds no document.
var ErrCustomFieldSchemaNotFound = errors.New("marketing: custom field schema not found")

// CustomFieldSchemaRepository is the persistence boundary for
// marketing_custom_field_schemas. There is at most one document per
// (tenantId, targetCollection) — enforced via the compound unique
// index declared in module.go::Collections().
type CustomFieldSchemaRepository struct {
	coll *mongo.Collection
}

// NewCustomFieldSchemaRepository binds a repository to the
// marketing_custom_field_schemas collection.
func NewCustomFieldSchemaRepository(db *mongo.Database) *CustomFieldSchemaRepository {
	return &CustomFieldSchemaRepository{coll: db.Collection(models.CustomFieldSchemasCollection)}
}

// Upsert creates or replaces the schema for (tenantId, target). The
// Version field is incremented atomically — callers do not need to
// read-modify-write to advance it.
func (r *CustomFieldSchemaRepository) Upsert(ctx context.Context, s *models.CustomFieldSchema) error {
	if s == nil {
		return errors.New("marketing: nil custom field schema")
	}
	tenantID, err := tenantrepo.StampInsert(ctx)
	if err != nil {
		return err
	}
	s.TenantID = tenantID
	now := time.Now().UTC()
	s.UpdatedAt = now
	filter := bson.M{
		"tenantId":         tenantID,
		"targetCollection": s.TargetCollection,
	}
	update := bson.M{
		"$set": bson.M{
			"fields":             s.Fields,
			"allowUnknownFields": s.AllowUnknownFields,
			"updatedAt":          now,
			"updatedBy":          s.UpdatedBy,
		},
		"$setOnInsert": bson.M{
			"uuid":             s.UUID,
			"tenantId":         tenantID,
			"targetCollection": s.TargetCollection,
			"createdAt":        now,
		},
		"$inc": bson.M{"version": 1},
	}
	_, err = r.coll.UpdateOne(ctx, filter, update, options.Update().SetUpsert(true))
	return err
}

// GetForTarget returns the schema for the caller's tenant + the given
// target collection, or ErrCustomFieldSchemaNotFound when no row is
// configured yet.
func (r *CustomFieldSchemaRepository) GetForTarget(ctx context.Context, target models.CustomFieldTarget) (*models.CustomFieldSchema, error) {
	filter, err := tenantrepo.Scope(ctx, bson.M{"targetCollection": target})
	if err != nil {
		return nil, err
	}
	var out models.CustomFieldSchema
	if err := r.coll.FindOne(ctx, filter).Decode(&out); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrCustomFieldSchemaNotFound
		}
		return nil, err
	}
	return &out, nil
}

// List returns every schema document for the caller's tenant.
// Typically two rows max (persons + organizations).
func (r *CustomFieldSchemaRepository) List(ctx context.Context) ([]models.CustomFieldSchema, error) {
	filter, err := tenantrepo.Scope(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	cur, err := r.coll.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	out := make([]models.CustomFieldSchema, 0)
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Delete removes the schema for the caller's tenant + the given
// target. Existing Person/Organization records keep their
// custom_fields bag intact — they are no longer typed-validated
// after deletion.
func (r *CustomFieldSchemaRepository) Delete(ctx context.Context, target models.CustomFieldTarget) error {
	filter, err := tenantrepo.Scope(ctx, bson.M{"targetCollection": target})
	if err != nil {
		return err
	}
	res, err := r.coll.DeleteOne(ctx, filter)
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return ErrCustomFieldSchemaNotFound
	}
	return nil
}
