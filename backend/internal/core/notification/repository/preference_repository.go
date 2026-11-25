package repository

import (
	"context"
	"errors"
	"time"

	"github.com/orkestra/backend/internal/core/notification/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// PreferenceRepository manages user notification preferences. The
// address-level suppression list has its own SuppressionRepository.
type PreferenceRepository interface {
	GetPreference(ctx context.Context, userUUID, category, channel string) (*models.PreferenceDoc, error)
	ListByUser(ctx context.Context, userUUID string) ([]*models.PreferenceDoc, error)
	UpsertPreference(ctx context.Context, doc *models.PreferenceDoc) error
}

type preferenceRepository struct {
	prefColl *mongo.Collection
}

func NewPreferenceRepository(db *mongo.Database) PreferenceRepository {
	return &preferenceRepository{
		prefColl: db.Collection(models.NotificationPreferencesCollection),
	}
}

func (r *preferenceRepository) GetPreference(ctx context.Context, userUUID, category, channel string) (*models.PreferenceDoc, error) {
	var doc models.PreferenceDoc
	err := r.prefColl.FindOne(ctx, bson.M{
		"userUuid": userUUID,
		"category": category,
		"channel":  channel,
	}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &doc, nil
}

func (r *preferenceRepository) ListByUser(ctx context.Context, userUUID string) ([]*models.PreferenceDoc, error) {
	cursor, err := r.prefColl.Find(ctx, bson.M{"userUuid": userUUID})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var out []*models.PreferenceDoc
	for cursor.Next(ctx) {
		var d models.PreferenceDoc
		if err := cursor.Decode(&d); err != nil {
			return nil, err
		}
		out = append(out, &d)
	}
	return out, cursor.Err()
}

func (r *preferenceRepository) UpsertPreference(ctx context.Context, doc *models.PreferenceDoc) error {
	doc.UpdatedAt = time.Now()
	_, err := r.prefColl.UpdateOne(ctx,
		bson.M{
			"userUuid": doc.UserUUID,
			"category": doc.Category,
			"channel":  doc.Channel,
		},
		bson.M{
			"$set": bson.M{
				"optedIn":   doc.OptedIn,
				"updatedAt": doc.UpdatedAt,
			},
			"$setOnInsert": bson.M{
				"userUuid": doc.UserUUID,
				"category": doc.Category,
				"channel":  doc.Channel,
			},
		},
		options.Update().SetUpsert(true),
	)
	return err
}
