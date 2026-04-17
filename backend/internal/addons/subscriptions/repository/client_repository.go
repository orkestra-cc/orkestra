package repository

import (
	"context"
	"errors"
	"time"

	"github.com/orkestra/backend/internal/addons/subscriptions/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

var ErrClientNotFound = errors.New("subscriptions: client not found")

type ClientFilters struct {
	Status models.ClientStatus
	Search string
}

type ClientRepository interface {
	Create(ctx context.Context, c *models.Client) error
	GetByUUID(ctx context.Context, uuid string) (*models.Client, error)
	GetByEmail(ctx context.Context, email string) (*models.Client, error)
	List(ctx context.Context, f ClientFilters) ([]models.Client, error)
	Update(ctx context.Context, c *models.Client) error
	SetStripeCustomerID(ctx context.Context, uuid, stripeCustomerID string) error
	Delete(ctx context.Context, uuid string) error
}

type clientRepository struct {
	coll *mongo.Collection
}

func NewClientRepository(db *mongo.Database) ClientRepository {
	return &clientRepository{coll: db.Collection(models.ClientsCollection)}
}

func (r *clientRepository) Create(ctx context.Context, c *models.Client) error {
	now := time.Now().UTC()
	if c.CreatedAt.IsZero() {
		c.CreatedAt = now
	}
	c.UpdatedAt = now
	_, err := r.coll.InsertOne(ctx, c)
	return err
}

func (r *clientRepository) GetByUUID(ctx context.Context, uuid string) (*models.Client, error) {
	var c models.Client
	err := r.coll.FindOne(ctx, bson.M{"uuid": uuid}).Decode(&c)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrClientNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *clientRepository) GetByEmail(ctx context.Context, email string) (*models.Client, error) {
	var c models.Client
	err := r.coll.FindOne(ctx, bson.M{"email": email}).Decode(&c)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrClientNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *clientRepository) List(ctx context.Context, f ClientFilters) ([]models.Client, error) {
	filter := bson.M{}
	if f.Status != "" {
		filter["status"] = f.Status
	}
	if f.Search != "" {
		filter["$or"] = []bson.M{
			{"legalName": bson.M{"$regex": f.Search, "$options": "i"}},
			{"displayName": bson.M{"$regex": f.Search, "$options": "i"}},
			{"email": bson.M{"$regex": f.Search, "$options": "i"}},
			{"vatNumber": bson.M{"$regex": f.Search, "$options": "i"}},
		}
	}
	cur, err := r.coll.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	out := make([]models.Client, 0)
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *clientRepository) Update(ctx context.Context, c *models.Client) error {
	c.UpdatedAt = time.Now().UTC()
	res, err := r.coll.ReplaceOne(ctx, bson.M{"uuid": c.UUID}, c)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrClientNotFound
	}
	return nil
}

func (r *clientRepository) SetStripeCustomerID(ctx context.Context, uuid, stripeCustomerID string) error {
	_, err := r.coll.UpdateOne(ctx, bson.M{"uuid": uuid}, bson.M{
		"$set": bson.M{
			"stripeCustomerID": stripeCustomerID,
			"updatedAt":        time.Now().UTC(),
		},
	})
	return err
}

func (r *clientRepository) Delete(ctx context.Context, uuid string) error {
	res, err := r.coll.DeleteOne(ctx, bson.M{"uuid": uuid})
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return ErrClientNotFound
	}
	return nil
}
