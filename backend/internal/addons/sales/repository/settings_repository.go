package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra-cc/orkestra-addon-sales/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const settingsCollection = "sales_settings"

// SettingsRepository handles persistence for per-user sales settings
type SettingsRepository interface {
	GetByUser(ctx context.Context, userUUID string) (*models.SalesSettings, error)
	Upsert(ctx context.Context, settings *models.SalesSettings) error
}

type settingsRepository struct {
	collection *mongo.Collection
}

// NewSettingsRepository creates a new SettingsRepository backed by MongoDB
func NewSettingsRepository(db *mongo.Database) SettingsRepository {
	coll := db.Collection(settingsCollection)

	// Unique index on userUuid — one settings doc per user
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	coll.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "userUuid", Value: 1}},
		Options: options.Index().SetUnique(true),
	})

	return &settingsRepository{collection: coll}
}

func (r *settingsRepository) GetByUser(ctx context.Context, userUUID string) (*models.SalesSettings, error) {
	var s models.SalesSettings
	err := r.collection.FindOne(ctx, bson.M{"userUuid": userUUID}).Decode(&s)
	if err == mongo.ErrNoDocuments {
		// Return defaults
		return &models.SalesSettings{
			UUID:     uuid.New().String(),
			UserUUID: userUUID,
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get sales settings: %w", err)
	}
	return &s, nil
}

func (r *settingsRepository) Upsert(ctx context.Context, settings *models.SalesSettings) error {
	if settings.UUID == "" {
		settings.UUID = uuid.New().String()
	}
	settings.UpdatedAt = time.Now()
	if settings.CreatedAt.IsZero() {
		settings.CreatedAt = settings.UpdatedAt
	}

	opts := options.Update().SetUpsert(true)
	_, err := r.collection.UpdateOne(
		ctx,
		bson.M{"userUuid": settings.UserUUID},
		bson.M{"$set": settings},
		opts,
	)
	if err != nil {
		return fmt.Errorf("upsert sales settings: %w", err)
	}
	return nil
}
