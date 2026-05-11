package repository

import (
	"context"
	"errors"
	"time"

	"github.com/orkestra/backend/internal/addons/payments/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

var ErrPaymentMethodNotFound = errors.New("payments: payment method not found")

type PaymentMethodRepository interface {
	Create(ctx context.Context, pm *models.PaymentMethod) error
	GetByUUID(ctx context.Context, uuid string) (*models.PaymentMethod, error)
	ListByTenant(ctx context.Context, tenantUUID string) ([]models.PaymentMethod, error)
	Delete(ctx context.Context, uuid string) error
}

type paymentMethodRepository struct {
	coll *mongo.Collection
}

func NewPaymentMethodRepository(db *mongo.Database) PaymentMethodRepository {
	return &paymentMethodRepository{coll: db.Collection(models.PaymentMethodsCollection)}
}

func (r *paymentMethodRepository) Create(ctx context.Context, pm *models.PaymentMethod) error {
	now := time.Now().UTC()
	if pm.CreatedAt.IsZero() {
		pm.CreatedAt = now
	}
	pm.UpdatedAt = now
	_, err := r.coll.InsertOne(ctx, pm)
	return err
}

func (r *paymentMethodRepository) GetByUUID(ctx context.Context, uuid string) (*models.PaymentMethod, error) {
	var pm models.PaymentMethod
	err := r.coll.FindOne(ctx, bson.M{"uuid": uuid}).Decode(&pm)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrPaymentMethodNotFound
	}
	if err != nil {
		return nil, err
	}
	return &pm, nil
}

func (r *paymentMethodRepository) ListByTenant(ctx context.Context, tenantUUID string) ([]models.PaymentMethod, error) {
	if tenantUUID == "" {
		return nil, nil
	}
	filter := bson.M{"tenantUUID": tenantUUID}
	cur, err := r.coll.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	out := make([]models.PaymentMethod, 0)
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *paymentMethodRepository) Delete(ctx context.Context, uuid string) error {
	res, err := r.coll.DeleteOne(ctx, bson.M{"uuid": uuid})
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return ErrPaymentMethodNotFound
	}
	return nil
}
