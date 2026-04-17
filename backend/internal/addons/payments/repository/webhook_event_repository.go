package repository

import (
	"context"
	"errors"
	"time"

	"github.com/orkestra/backend/internal/addons/payments/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ErrDuplicateWebhookEvent is returned when Insert hits the unique index on
// (provider, providerEventID). This is the expected idempotency signal.
var ErrDuplicateWebhookEvent = errors.New("payments: duplicate webhook event")

type WebhookEventRepository interface {
	Insert(ctx context.Context, e *models.WebhookEvent) error
	MarkProcessed(ctx context.Context, uuid string, processErr string) error
	List(ctx context.Context, provider models.ProviderName, limit int64) ([]models.WebhookEvent, error)
}

type webhookEventRepository struct {
	coll *mongo.Collection
}

func NewWebhookEventRepository(db *mongo.Database) WebhookEventRepository {
	return &webhookEventRepository{coll: db.Collection(models.WebhookEventsCollection)}
}

func (r *webhookEventRepository) Insert(ctx context.Context, e *models.WebhookEvent) error {
	if e.ReceivedAt.IsZero() {
		e.ReceivedAt = time.Now().UTC()
	}
	_, err := r.coll.InsertOne(ctx, e)
	if mongo.IsDuplicateKeyError(err) {
		return ErrDuplicateWebhookEvent
	}
	return err
}

func (r *webhookEventRepository) MarkProcessed(ctx context.Context, uuid string, processErr string) error {
	now := time.Now().UTC()
	update := bson.M{
		"processed":   processErr == "",
		"processedAt": now,
	}
	if processErr != "" {
		update["processError"] = processErr
	}
	_, err := r.coll.UpdateOne(ctx, bson.M{"uuid": uuid}, bson.M{"$set": update})
	return err
}

func (r *webhookEventRepository) List(ctx context.Context, provider models.ProviderName, limit int64) ([]models.WebhookEvent, error) {
	filter := bson.M{}
	if provider != "" {
		filter["provider"] = provider
	}
	opts := options.Find().SetSort(bson.D{{Key: "receivedAt", Value: -1}})
	if limit > 0 {
		opts.SetLimit(limit)
	}
	cur, err := r.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	out := make([]models.WebhookEvent, 0)
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}
