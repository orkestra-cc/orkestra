package repository

import (
	"context"
	"time"

	"github.com/orkestra-cc/orkestra-addon-subscriptions/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type ActivityRepository interface {
	Create(ctx context.Context, a *models.ActivityLog) error
	ListBySubscription(ctx context.Context, subscriptionUUID string, limit int64) ([]models.ActivityLog, error)
}

type activityRepository struct {
	coll *mongo.Collection
}

func NewActivityRepository(db *mongo.Database) ActivityRepository {
	return &activityRepository{coll: db.Collection(models.ActivityCollection)}
}

func (r *activityRepository) Create(ctx context.Context, a *models.ActivityLog) error {
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now().UTC()
	}
	_, err := r.coll.InsertOne(ctx, a)
	return err
}

func (r *activityRepository) ListBySubscription(ctx context.Context, subscriptionUUID string, limit int64) ([]models.ActivityLog, error) {
	opts := options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}})
	if limit > 0 {
		opts = opts.SetLimit(limit)
	}
	cur, err := r.coll.Find(ctx, bson.M{"subscriptionUUID": subscriptionUUID}, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	out := make([]models.ActivityLog, 0)
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}
