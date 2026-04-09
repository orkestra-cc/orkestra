package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/orkestra/backend/internal/addons/rag/models"
)

const relationshipTypeCollection = "rag_relationship_types"

// RelationshipTypeRepository defines CRUD operations for relationship type configs.
type RelationshipTypeRepository interface {
	List(ctx context.Context) ([]models.RelationshipTypeConfig, error)
	GetByUUID(ctx context.Context, uuid string) (*models.RelationshipTypeConfig, error)
	GetByName(ctx context.Context, name string) (*models.RelationshipTypeConfig, error)
	Create(ctx context.Context, rt *models.RelationshipTypeConfig) error
	Update(ctx context.Context, uuid string, desc *string, props *[]string, cats *map[string]bool) (*models.RelationshipTypeConfig, error)
	Delete(ctx context.Context, uuid string) error
	Count(ctx context.Context) (int64, error)
	ListActiveForCategory(ctx context.Context, category string) ([]models.RelationshipTypeConfig, error)
}

type relationshipTypeRepository struct {
	collection *mongo.Collection
}

// NewRelationshipTypeRepository creates a new repository and seeds defaults if empty.
func NewRelationshipTypeRepository(db *mongo.Database) RelationshipTypeRepository {
	coll := db.Collection(relationshipTypeCollection)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	indexes := []mongo.IndexModel{
		{Keys: bson.D{{Key: "uuid", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "name", Value: 1}}, Options: options.Index().SetUnique(true)},
	}
	coll.Indexes().CreateMany(ctx, indexes) //nolint:errcheck

	repo := &relationshipTypeRepository{collection: coll}

	// Seed defaults if collection is empty
	count, _ := coll.CountDocuments(ctx, bson.M{})
	if count == 0 {
		repo.seed(ctx)
	}

	return repo
}

func (r *relationshipTypeRepository) seed(ctx context.Context) {
	defaults := models.DefaultRelationshipTypes()
	now := time.Now()
	docs := make([]interface{}, len(defaults))
	for i, rt := range defaults {
		rt.UUID = uuid.New().String()
		rt.CreatedAt = now
		rt.UpdatedAt = now
		docs[i] = rt
	}
	r.collection.InsertMany(ctx, docs) //nolint:errcheck
}

func (r *relationshipTypeRepository) List(ctx context.Context) ([]models.RelationshipTypeConfig, error) {
	cursor, err := r.collection.Find(ctx, bson.M{}, options.Find().SetSort(bson.D{{Key: "isSystem", Value: -1}, {Key: "name", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var results []models.RelationshipTypeConfig
	if err := cursor.All(ctx, &results); err != nil {
		return nil, err
	}
	return results, nil
}

func (r *relationshipTypeRepository) GetByUUID(ctx context.Context, uuid string) (*models.RelationshipTypeConfig, error) {
	var rt models.RelationshipTypeConfig
	err := r.collection.FindOne(ctx, bson.M{"uuid": uuid}).Decode(&rt)
	if err != nil {
		return nil, fmt.Errorf("relationship type not found: %w", err)
	}
	return &rt, nil
}

func (r *relationshipTypeRepository) GetByName(ctx context.Context, name string) (*models.RelationshipTypeConfig, error) {
	var rt models.RelationshipTypeConfig
	err := r.collection.FindOne(ctx, bson.M{"name": name}).Decode(&rt)
	if err != nil {
		return nil, err
	}
	return &rt, nil
}

func (r *relationshipTypeRepository) Create(ctx context.Context, rt *models.RelationshipTypeConfig) error {
	rt.UUID = uuid.New().String()
	rt.CreatedAt = time.Now()
	rt.UpdatedAt = time.Now()
	_, err := r.collection.InsertOne(ctx, rt)
	return err
}

func (r *relationshipTypeRepository) Update(ctx context.Context, uuid string, desc *string, props *[]string, cats *map[string]bool) (*models.RelationshipTypeConfig, error) {
	set := bson.M{"updatedAt": time.Now()}
	if desc != nil {
		set["description"] = *desc
	}
	if props != nil {
		set["properties"] = *props
	}
	if cats != nil {
		set["categories"] = *cats
	}

	_, err := r.collection.UpdateOne(ctx, bson.M{"uuid": uuid}, bson.M{"$set": set})
	if err != nil {
		return nil, err
	}
	return r.GetByUUID(ctx, uuid)
}

func (r *relationshipTypeRepository) Delete(ctx context.Context, uuid string) error {
	res, err := r.collection.DeleteOne(ctx, bson.M{"uuid": uuid, "isSystem": false})
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return fmt.Errorf("relationship type not found or is a system type")
	}
	return nil
}

func (r *relationshipTypeRepository) Count(ctx context.Context) (int64, error) {
	return r.collection.CountDocuments(ctx, bson.M{})
}

func (r *relationshipTypeRepository) ListActiveForCategory(ctx context.Context, category string) ([]models.RelationshipTypeConfig, error) {
	filter := bson.M{
		fmt.Sprintf("categories.%s", category): true,
	}
	cursor, err := r.collection.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var results []models.RelationshipTypeConfig
	if err := cursor.All(ctx, &results); err != nil {
		return nil, err
	}
	return results, nil
}
