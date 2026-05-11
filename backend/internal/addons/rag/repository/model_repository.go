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

const modelCollection = "rag_models"

// ModelRepository defines CRUD operations for model configurations
type ModelRepository interface {
	Create(ctx context.Context, config *models.ModelConfig) error
	GetByUUID(ctx context.Context, uuid string) (*models.ModelConfig, error)
	List(ctx context.Context, modelType string) ([]models.ModelConfig, error)
	Update(ctx context.Context, uuid string, update bson.M) error
	Delete(ctx context.Context, uuid string) error
	GetDefault(ctx context.Context, modelType string) (*models.ModelConfig, error)
	ClearDefault(ctx context.Context, modelType string) error
	Count(ctx context.Context) (int64, error)
}

type modelRepository struct {
	collection *mongo.Collection
}

// NewModelRepository creates a new ModelRepository
func NewModelRepository(db *mongo.Database) ModelRepository {
	coll := db.Collection(modelCollection)

	// Ensure indexes
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	indexes := []mongo.IndexModel{
		{Keys: bson.D{{Key: "uuid", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "modelType", Value: 1}, {Key: "isDefault", Value: 1}}},
	}
	coll.Indexes().CreateMany(ctx, indexes) //nolint:errcheck // best-effort index ensure; subsequent reads will surface persistent failures

	return &modelRepository{collection: coll}
}

func (r *modelRepository) Create(ctx context.Context, config *models.ModelConfig) error {
	if config.UUID == "" {
		config.UUID = uuid.New().String()
	}
	now := time.Now()
	config.CreatedAt = now
	config.UpdatedAt = now

	_, err := r.collection.InsertOne(ctx, config)
	if err != nil {
		return fmt.Errorf("insert model config: %w", err)
	}
	return nil
}

func (r *modelRepository) GetByUUID(ctx context.Context, uuid string) (*models.ModelConfig, error) {
	var config models.ModelConfig
	err := r.collection.FindOne(ctx, bson.M{"uuid": uuid}).Decode(&config)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, fmt.Errorf("model not found: %s", uuid)
		}
		return nil, fmt.Errorf("get model config: %w", err)
	}
	return &config, nil
}

func (r *modelRepository) List(ctx context.Context, modelType string) ([]models.ModelConfig, error) {
	filter := bson.M{}
	if modelType != "" {
		filter["modelType"] = modelType
	}

	cursor, err := r.collection.Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "isDefault", Value: -1}, {Key: "name", Value: 1}}))
	if err != nil {
		return nil, fmt.Errorf("list model configs: %w", err)
	}
	defer cursor.Close(ctx)

	var configs []models.ModelConfig
	if err := cursor.All(ctx, &configs); err != nil {
		return nil, fmt.Errorf("decode model configs: %w", err)
	}
	return configs, nil
}

func (r *modelRepository) Update(ctx context.Context, uuid string, update bson.M) error {
	update["updatedAt"] = time.Now()
	_, err := r.collection.UpdateOne(ctx, bson.M{"uuid": uuid}, bson.M{"$set": update})
	if err != nil {
		return fmt.Errorf("update model config: %w", err)
	}
	return nil
}

func (r *modelRepository) Delete(ctx context.Context, uuid string) error {
	res, err := r.collection.DeleteOne(ctx, bson.M{"uuid": uuid})
	if err != nil {
		return fmt.Errorf("delete model config: %w", err)
	}
	if res.DeletedCount == 0 {
		return fmt.Errorf("model not found: %s", uuid)
	}
	return nil
}

func (r *modelRepository) GetDefault(ctx context.Context, modelType string) (*models.ModelConfig, error) {
	var config models.ModelConfig
	err := r.collection.FindOne(ctx, bson.M{"modelType": modelType, "isDefault": true}).Decode(&config)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, fmt.Errorf("no default %s model configured", modelType)
		}
		return nil, fmt.Errorf("get default model: %w", err)
	}
	return &config, nil
}

func (r *modelRepository) ClearDefault(ctx context.Context, modelType string) error {
	_, err := r.collection.UpdateMany(ctx,
		bson.M{"modelType": modelType, "isDefault": true},
		bson.M{"$set": bson.M{"isDefault": false, "updatedAt": time.Now()}},
	)
	return err
}

func (r *modelRepository) Count(ctx context.Context) (int64, error) {
	return r.collection.CountDocuments(ctx, bson.M{})
}
