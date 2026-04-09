package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra/backend/internal/addons/sales/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const promptsCollection = "sales_prompts"

// PromptRepository handles persistence for editable prompt templates
type PromptRepository interface {
	List(ctx context.Context, category string) ([]models.SalesPrompt, error)
	GetByUUID(ctx context.Context, uuid string) (*models.SalesPrompt, error)
	GetByCategoryAndName(ctx context.Context, category, name string) (*models.SalesPrompt, error)
	Upsert(ctx context.Context, prompt *models.SalesPrompt) error
	Count(ctx context.Context) (int64, error)
}

type promptRepository struct {
	collection *mongo.Collection
}

func NewPromptRepository(db *mongo.Database) PromptRepository {
	coll := db.Collection(promptsCollection)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	coll.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "uuid", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "category", Value: 1}, {Key: "name", Value: 1}}, Options: options.Index().SetUnique(true)},
	})

	return &promptRepository{collection: coll}
}

func (r *promptRepository) List(ctx context.Context, category string) ([]models.SalesPrompt, error) {
	filter := bson.M{}
	if category != "" {
		filter["category"] = category
	}

	opts := options.Find().SetSort(bson.D{{Key: "category", Value: 1}, {Key: "name", Value: 1}})
	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("list sales prompts: %w", err)
	}
	defer cursor.Close(ctx)

	var prompts []models.SalesPrompt
	if err := cursor.All(ctx, &prompts); err != nil {
		return nil, fmt.Errorf("decode sales prompts: %w", err)
	}
	return prompts, nil
}

func (r *promptRepository) GetByUUID(ctx context.Context, id string) (*models.SalesPrompt, error) {
	var p models.SalesPrompt
	err := r.collection.FindOne(ctx, bson.M{"uuid": id}).Decode(&p)
	if err != nil {
		return nil, fmt.Errorf("find sales prompt %s: %w", id, err)
	}
	return &p, nil
}

func (r *promptRepository) GetByCategoryAndName(ctx context.Context, category, name string) (*models.SalesPrompt, error) {
	var p models.SalesPrompt
	err := r.collection.FindOne(ctx, bson.M{"category": category, "name": name}).Decode(&p)
	if err != nil {
		return nil, fmt.Errorf("find sales prompt %s/%s: %w", category, name, err)
	}
	return &p, nil
}

func (r *promptRepository) Upsert(ctx context.Context, prompt *models.SalesPrompt) error {
	if prompt.UUID == "" {
		prompt.UUID = uuid.New().String()
	}
	prompt.UpdatedAt = time.Now()
	if prompt.CreatedAt.IsZero() {
		prompt.CreatedAt = prompt.UpdatedAt
	}

	opts := options.Update().SetUpsert(true)
	_, err := r.collection.UpdateOne(
		ctx,
		bson.M{"category": prompt.Category, "name": prompt.Name},
		bson.M{"$set": prompt},
		opts,
	)
	if err != nil {
		return fmt.Errorf("upsert sales prompt: %w", err)
	}
	return nil
}

func (r *promptRepository) Count(ctx context.Context) (int64, error) {
	return r.collection.CountDocuments(ctx, bson.M{})
}
