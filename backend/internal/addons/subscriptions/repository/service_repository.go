package repository

import (
	"context"
	"errors"
	"time"

	"github.com/orkestra/backend/internal/addons/subscriptions/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

var ErrServiceNotFound = errors.New("subscriptions: service not found")

type ServiceFilters struct {
	Active   *bool
	Category string
}

type ServiceRepository interface {
	Create(ctx context.Context, s *models.Service) error
	GetByUUID(ctx context.Context, uuid string) (*models.Service, error)
	GetByCode(ctx context.Context, code string) (*models.Service, error)
	List(ctx context.Context, f ServiceFilters) ([]models.Service, error)
	Update(ctx context.Context, s *models.Service) error
	Delete(ctx context.Context, uuid string) error
}

type serviceRepository struct {
	coll *mongo.Collection
}

func NewServiceRepository(db *mongo.Database) ServiceRepository {
	return &serviceRepository{coll: db.Collection(models.ServicesCollection)}
}

func (r *serviceRepository) Create(ctx context.Context, s *models.Service) error {
	now := time.Now().UTC()
	if s.CreatedAt.IsZero() {
		s.CreatedAt = now
	}
	s.UpdatedAt = now
	_, err := r.coll.InsertOne(ctx, s)
	return err
}

func (r *serviceRepository) GetByUUID(ctx context.Context, uuid string) (*models.Service, error) {
	var s models.Service
	err := r.coll.FindOne(ctx, bson.M{"uuid": uuid}).Decode(&s)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrServiceNotFound
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *serviceRepository) GetByCode(ctx context.Context, code string) (*models.Service, error) {
	var s models.Service
	err := r.coll.FindOne(ctx, bson.M{"code": code}).Decode(&s)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrServiceNotFound
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *serviceRepository) List(ctx context.Context, f ServiceFilters) ([]models.Service, error) {
	filter := bson.M{}
	if f.Active != nil {
		filter["active"] = *f.Active
	}
	if f.Category != "" {
		filter["category"] = f.Category
	}
	cur, err := r.coll.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	out := make([]models.Service, 0)
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *serviceRepository) Update(ctx context.Context, s *models.Service) error {
	s.UpdatedAt = time.Now().UTC()
	res, err := r.coll.ReplaceOne(ctx, bson.M{"uuid": s.UUID}, s)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrServiceNotFound
	}
	return nil
}

func (r *serviceRepository) Delete(ctx context.Context, uuid string) error {
	res, err := r.coll.DeleteOne(ctx, bson.M{"uuid": uuid})
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return ErrServiceNotFound
	}
	return nil
}
