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

var ErrNotFound = errors.New("notification: not found")

// NotificationRepository persists delivery log entries and performs
// idempotency lookups.
type NotificationRepository interface {
	Create(ctx context.Context, doc *models.NotificationDoc) error
	FindByIdempotencyKey(ctx context.Context, key string, since time.Time) (*models.NotificationDoc, error)
	List(ctx context.Context, filter Filter, limit int64) ([]*models.NotificationDoc, error)
	GetByUUID(ctx context.Context, uuid string) (*models.NotificationDoc, error)
}

// Filter narrows a notification list query.
type Filter struct {
	UserUUID string
	Category string
	Status   string
	Channel  string
}

type notificationRepository struct {
	coll *mongo.Collection
}

func NewNotificationRepository(db *mongo.Database) NotificationRepository {
	return &notificationRepository{
		coll: db.Collection(models.NotificationMessagesCollection),
	}
}

func (r *notificationRepository) Create(ctx context.Context, doc *models.NotificationDoc) error {
	if doc.CreatedAt.IsZero() {
		doc.CreatedAt = time.Now()
	}
	_, err := r.coll.InsertOne(ctx, doc)
	return err
}

func (r *notificationRepository) FindByIdempotencyKey(ctx context.Context, key string, since time.Time) (*models.NotificationDoc, error) {
	if key == "" {
		return nil, nil
	}
	var doc models.NotificationDoc
	err := r.coll.FindOne(ctx, bson.M{
		"idempotencyKey": key,
		"createdAt":      bson.M{"$gte": since},
	}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &doc, nil
}

func (r *notificationRepository) List(ctx context.Context, filter Filter, limit int64) ([]*models.NotificationDoc, error) {
	q := bson.M{}
	if filter.UserUUID != "" {
		q["recipientUserUuid"] = filter.UserUUID
	}
	if filter.Category != "" {
		q["category"] = filter.Category
	}
	if filter.Status != "" {
		q["status"] = filter.Status
	}
	if filter.Channel != "" {
		q["channel"] = filter.Channel
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	cursor, err := r.coll.Find(ctx, q,
		options.Find().SetSort(bson.M{"createdAt": -1}).SetLimit(limit),
	)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var out []*models.NotificationDoc
	for cursor.Next(ctx) {
		var d models.NotificationDoc
		if err := cursor.Decode(&d); err != nil {
			return nil, err
		}
		out = append(out, &d)
	}
	return out, cursor.Err()
}

func (r *notificationRepository) GetByUUID(ctx context.Context, uuid string) (*models.NotificationDoc, error) {
	var doc models.NotificationDoc
	err := r.coll.FindOne(ctx, bson.M{"uuid": uuid}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &doc, nil
}
