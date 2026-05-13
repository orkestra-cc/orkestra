package repository

import (
	"context"
	"errors"
	"time"

	"github.com/orkestra-cc/orkestra-addon-subscriptions/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// optionsFindSortCreatedAtDesc keeps every list/find call ordering consistent.
func optionsFindSortCreatedAtDesc() *options.FindOptions {
	return options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}})
}

var ErrSubscriptionNotFound = errors.New("subscriptions: subscription not found")

type SubscriptionFilters struct {
	TenantUUID  string
	ServiceUUID string
	Status      models.SubStatus
}

type SubscriptionRepository interface {
	Create(ctx context.Context, s *models.Subscription) error
	GetByUUID(ctx context.Context, uuid string) (*models.Subscription, error)
	List(ctx context.Context, f SubscriptionFilters) ([]models.Subscription, error)
	Update(ctx context.Context, s *models.Subscription) error
	// FindDue returns subscriptions in active/past_due whose NextBillingAt is
	// on or before `now`. Used by the renewal job every tick.
	FindDue(ctx context.Context, now time.Time) ([]models.Subscription, error)
	// FindByTenant returns every subscription bound to the given tenant.
	// Sorted by createdAt desc. Used by self-service handlers and the admin
	// aggregator GET /v1/admin/tenants/{id}/subscriptions.
	FindByTenant(ctx context.Context, tenantUUID string) ([]models.Subscription, error)
	UpdateStatus(ctx context.Context, uuid string, status models.SubStatus) error
	Delete(ctx context.Context, uuid string) error
}

type subscriptionRepository struct {
	coll *mongo.Collection
}

func NewSubscriptionRepository(db *mongo.Database) SubscriptionRepository {
	return &subscriptionRepository{coll: db.Collection(models.SubscriptionsCollection)}
}

func (r *subscriptionRepository) Create(ctx context.Context, s *models.Subscription) error {
	now := time.Now().UTC()
	if s.CreatedAt.IsZero() {
		s.CreatedAt = now
	}
	s.UpdatedAt = now
	_, err := r.coll.InsertOne(ctx, s)
	return err
}

func (r *subscriptionRepository) GetByUUID(ctx context.Context, uuid string) (*models.Subscription, error) {
	var s models.Subscription
	err := r.coll.FindOne(ctx, bson.M{"uuid": uuid}).Decode(&s)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrSubscriptionNotFound
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *subscriptionRepository) List(ctx context.Context, f SubscriptionFilters) ([]models.Subscription, error) {
	filter := bson.M{}
	if f.TenantUUID != "" {
		filter["tenantUUID"] = f.TenantUUID
	}
	if f.ServiceUUID != "" {
		filter["serviceUUID"] = f.ServiceUUID
	}
	if f.Status != "" {
		filter["status"] = f.Status
	}
	cur, err := r.coll.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	out := make([]models.Subscription, 0)
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *subscriptionRepository) FindByTenant(ctx context.Context, tenantUUID string) ([]models.Subscription, error) {
	if tenantUUID == "" {
		return nil, nil
	}
	opts := optionsFindSortCreatedAtDesc()
	filter := bson.M{"tenantUUID": tenantUUID}
	cur, err := r.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	out := make([]models.Subscription, 0)
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *subscriptionRepository) Update(ctx context.Context, s *models.Subscription) error {
	s.UpdatedAt = time.Now().UTC()
	res, err := r.coll.ReplaceOne(ctx, bson.M{"uuid": s.UUID}, s)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrSubscriptionNotFound
	}
	return nil
}

func (r *subscriptionRepository) FindDue(ctx context.Context, now time.Time) ([]models.Subscription, error) {
	filter := bson.M{
		"nextBillingAt": bson.M{"$lte": now},
		"status": bson.M{"$in": []models.SubStatus{
			models.SubActive,
			models.SubPastDue,
		}},
	}
	cur, err := r.coll.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	out := make([]models.Subscription, 0)
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *subscriptionRepository) UpdateStatus(ctx context.Context, uuid string, status models.SubStatus) error {
	_, err := r.coll.UpdateOne(ctx, bson.M{"uuid": uuid}, bson.M{
		"$set": bson.M{"status": status, "updatedAt": time.Now().UTC()},
	})
	return err
}

func (r *subscriptionRepository) Delete(ctx context.Context, uuid string) error {
	res, err := r.coll.DeleteOne(ctx, bson.M{"uuid": uuid})
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return ErrSubscriptionNotFound
	}
	return nil
}
