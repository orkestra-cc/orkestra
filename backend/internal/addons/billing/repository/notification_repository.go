package repository

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/orkestra/backend/internal/addons/billing/models"
)

// Common errors
var (
	ErrNotificationNotFound = errors.New("notification not found")
)

// NotificationRepository defines the interface for SDI notification data access
type NotificationRepository interface {
	// Create operations
	Create(ctx context.Context, notification *models.SDINotification) error

	// Read operations
	GetByID(ctx context.Context, id string) (*models.SDINotification, error)
	GetByUUID(ctx context.Context, uuid string) (*models.SDINotification, error)
	GetByInvoiceUUID(ctx context.Context, invoiceUUID string) ([]models.SDINotification, error)
	List(ctx context.Context, filters *models.NotificationFilters, pagination models.PaginationParams) ([]models.SDINotification, int64, error)
	GetUnprocessed(ctx context.Context) ([]models.SDINotification, error)

	// Update operations
	MarkAsProcessed(ctx context.Context, uuid string, processedBy string) error

	// Statistics
	GetSummary(ctx context.Context, fromDate, toDate *time.Time) (*models.NotificationSummary, error)
	CountUnprocessed(ctx context.Context) (int64, error)

	// Polling state
	GetPollingState(ctx context.Context) (*models.PollingState, error)
	UpdatePollingState(ctx context.Context, state *models.PollingState) error
}

type notificationRepository struct {
	collection      *mongo.Collection
	pollingStateColl *mongo.Collection
}

// NewNotificationRepository creates a new NotificationRepository
func NewNotificationRepository(db *mongo.Database) NotificationRepository {
	repo := &notificationRepository{
		collection:      db.Collection("billing_notifications"),
		pollingStateColl: db.Collection("billing_polling_state"),
	}
	repo.createIndexes(context.Background())
	return repo
}

func (r *notificationRepository) createIndexes(ctx context.Context) {
	indexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "uuid", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{{Key: "invoiceUuid", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "openApiUuid", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "notificationType", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "processed", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "notificationDate", Value: -1}},
		},
		{
			Keys: bson.D{{Key: "createdAt", Value: -1}},
		},
	}

	_, _ = r.collection.Indexes().CreateMany(ctx, indexes)
}

func (r *notificationRepository) Create(ctx context.Context, notification *models.SDINotification) error {
	notification.CreatedAt = time.Now()
	notification.Processed = false

	result, err := r.collection.InsertOne(ctx, notification)
	if err != nil {
		return err
	}

	notification.ID = result.InsertedID.(primitive.ObjectID)
	return nil
}

func (r *notificationRepository) GetByID(ctx context.Context, id string) (*models.SDINotification, error) {
	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, ErrNotificationNotFound
	}

	var notification models.SDINotification
	err = r.collection.FindOne(ctx, bson.M{"_id": objectID}).Decode(&notification)

	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrNotificationNotFound
		}
		return nil, err
	}

	return &notification, nil
}

func (r *notificationRepository) GetByUUID(ctx context.Context, uuid string) (*models.SDINotification, error) {
	var notification models.SDINotification
	err := r.collection.FindOne(ctx, bson.M{"uuid": uuid}).Decode(&notification)

	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrNotificationNotFound
		}
		return nil, err
	}

	return &notification, nil
}

func (r *notificationRepository) GetByInvoiceUUID(ctx context.Context, invoiceUUID string) ([]models.SDINotification, error) {
	opts := options.Find().SetSort(bson.D{{Key: "notificationDate", Value: -1}})

	cursor, err := r.collection.Find(ctx, bson.M{"invoiceUuid": invoiceUUID}, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var notifications []models.SDINotification
	if err := cursor.All(ctx, &notifications); err != nil {
		return nil, err
	}

	return notifications, nil
}

func (r *notificationRepository) List(ctx context.Context, filters *models.NotificationFilters, pagination models.PaginationParams) ([]models.SDINotification, int64, error) {
	filter := bson.M{}

	if filters != nil {
		if filters.InvoiceUUID != "" {
			filter["invoiceUuid"] = filters.InvoiceUUID
		}
		if filters.NotificationType != "" {
			filter["notificationType"] = filters.NotificationType
		}
		if filters.Processed != nil {
			filter["processed"] = *filters.Processed
		}
		if filters.FromDate != nil || filters.ToDate != nil {
			dateFilter := bson.M{}
			if filters.FromDate != nil {
				dateFilter["$gte"] = *filters.FromDate
			}
			if filters.ToDate != nil {
				dateFilter["$lte"] = *filters.ToDate
			}
			filter["notificationDate"] = dateFilter
		}
	}

	// Count total
	total, err := r.collection.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, err
	}

	// Set pagination defaults
	if pagination.Page < 1 {
		pagination.Page = 1
	}
	if pagination.PageSize < 1 {
		pagination.PageSize = 20
	}
	if pagination.PageSize > 100 {
		pagination.PageSize = 100
	}

	skip := int64((pagination.Page - 1) * pagination.PageSize)
	limit := int64(pagination.PageSize)

	opts := options.Find().
		SetSort(bson.D{{Key: "notificationDate", Value: -1}}).
		SetSkip(skip).
		SetLimit(limit)

	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)

	var notifications []models.SDINotification
	if err := cursor.All(ctx, &notifications); err != nil {
		return nil, 0, err
	}

	return notifications, total, nil
}

func (r *notificationRepository) GetUnprocessed(ctx context.Context) ([]models.SDINotification, error) {
	opts := options.Find().SetSort(bson.D{{Key: "notificationDate", Value: 1}})

	cursor, err := r.collection.Find(ctx, bson.M{"processed": false}, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var notifications []models.SDINotification
	if err := cursor.All(ctx, &notifications); err != nil {
		return nil, err
	}

	return notifications, nil
}

func (r *notificationRepository) MarkAsProcessed(ctx context.Context, uuid string, processedBy string) error {
	now := time.Now()
	update := bson.M{
		"$set": bson.M{
			"processed":   true,
			"processedAt": now,
			"processedBy": processedBy,
		},
	}

	result, err := r.collection.UpdateOne(ctx, bson.M{"uuid": uuid}, update)
	if err != nil {
		return err
	}

	if result.MatchedCount == 0 {
		return ErrNotificationNotFound
	}

	return nil
}

func (r *notificationRepository) GetSummary(ctx context.Context, fromDate, toDate *time.Time) (*models.NotificationSummary, error) {
	summary := &models.NotificationSummary{}

	dateScope := bson.M{}
	if fromDate != nil || toDate != nil {
		bounds := bson.M{}
		if fromDate != nil {
			bounds["$gte"] = *fromDate
		}
		if toDate != nil {
			bounds["$lte"] = *toDate
		}
		dateScope["notificationDate"] = bounds
	}

	merge := func(extra bson.M) bson.M {
		out := bson.M{}
		for k, v := range dateScope {
			out[k] = v
		}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}

	// Total count
	summary.TotalCount, _ = r.collection.CountDocuments(ctx, merge(bson.M{}))

	// Unprocessed count
	summary.UnprocessedCount, _ = r.collection.CountDocuments(ctx, merge(bson.M{"processed": false}))

	// Positive notifications (RC, DT, NE with EC01)
	summary.PositiveCount, _ = r.collection.CountDocuments(ctx, merge(bson.M{
		"$or": []bson.M{
			{"notificationType": models.NotificationRC},
			{"notificationType": models.NotificationDT},
			{"notificationType": models.NotificationNE, "outcome": models.OutcomeAccepted},
		},
	}))

	// Negative notifications (NS, NE with EC02)
	summary.NegativeCount, _ = r.collection.CountDocuments(ctx, merge(bson.M{
		"$or": []bson.M{
			{"notificationType": models.NotificationNS},
			{"notificationType": models.NotificationNE, "outcome": models.OutcomeRejected},
		},
	}))

	// Pending action (unprocessed NS, MC, or rejected NE)
	summary.PendingAction, _ = r.collection.CountDocuments(ctx, merge(bson.M{
		"processed": false,
		"$or": []bson.M{
			{"notificationType": models.NotificationNS},
			{"notificationType": models.NotificationMC},
			{"notificationType": models.NotificationNE, "outcome": models.OutcomeRejected},
		},
	}))

	return summary, nil
}

func (r *notificationRepository) CountUnprocessed(ctx context.Context) (int64, error) {
	return r.collection.CountDocuments(ctx, bson.M{"processed": false})
}

func (r *notificationRepository) GetPollingState(ctx context.Context) (*models.PollingState, error) {
	var state models.PollingState
	err := r.pollingStateColl.FindOne(ctx, bson.M{}).Decode(&state)

	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			// Create initial state
			state = models.PollingState{
				LastPolledAt:      time.Now().Add(-24 * time.Hour), // Start from 24 hours ago
				TotalPolled:       0,
				ConsecutiveErrors: 0,
				UpdatedAt:         time.Now(),
			}
			_, err = r.pollingStateColl.InsertOne(ctx, state)
			if err != nil {
				return nil, err
			}
			return &state, nil
		}
		return nil, err
	}

	return &state, nil
}

func (r *notificationRepository) UpdatePollingState(ctx context.Context, state *models.PollingState) error {
	state.UpdatedAt = time.Now()

	opts := options.Update().SetUpsert(true)
	_, err := r.pollingStateColl.UpdateOne(
		ctx,
		bson.M{},
		bson.M{"$set": state},
		opts,
	)

	return err
}
