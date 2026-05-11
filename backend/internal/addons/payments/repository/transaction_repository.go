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

var ErrTransactionNotFound = errors.New("payments: transaction not found")

type TransactionFilters struct {
	SubscriptionUUID string
	InvoiceUUID      string
	TenantUUID       string
	Status           models.TransactionStatus
	Since            *time.Time
	Until            *time.Time
}

type TransactionRepository interface {
	Create(ctx context.Context, t *models.Transaction) error
	GetByUUID(ctx context.Context, uuid string) (*models.Transaction, error)
	GetByProviderTxID(ctx context.Context, provider models.ProviderName, providerTxID string) (*models.Transaction, error)
	List(ctx context.Context, f TransactionFilters) ([]models.Transaction, error)
	// FindByTenant lists transactions billed to the given tenant. Backs the
	// admin aggregator GET /v1/admin/tenants/{id}/payments and the self-
	// service /v1/me/transactions endpoint.
	FindByTenant(ctx context.Context, tenantUUID string) ([]models.Transaction, error)
	Update(ctx context.Context, t *models.Transaction) error
}

type transactionRepository struct {
	coll *mongo.Collection
}

func NewTransactionRepository(db *mongo.Database) TransactionRepository {
	return &transactionRepository{coll: db.Collection(models.TransactionsCollection)}
}

func (r *transactionRepository) Create(ctx context.Context, t *models.Transaction) error {
	now := time.Now().UTC()
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	t.UpdatedAt = now
	_, err := r.coll.InsertOne(ctx, t)
	return err
}

func (r *transactionRepository) GetByUUID(ctx context.Context, uuid string) (*models.Transaction, error) {
	var t models.Transaction
	err := r.coll.FindOne(ctx, bson.M{"uuid": uuid}).Decode(&t)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrTransactionNotFound
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *transactionRepository) GetByProviderTxID(ctx context.Context, provider models.ProviderName, providerTxID string) (*models.Transaction, error) {
	var t models.Transaction
	err := r.coll.FindOne(ctx, bson.M{"provider": provider, "providerTxID": providerTxID}).Decode(&t)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrTransactionNotFound
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *transactionRepository) List(ctx context.Context, f TransactionFilters) ([]models.Transaction, error) {
	filter := bson.M{}
	if f.SubscriptionUUID != "" {
		filter["subscriptionUUID"] = f.SubscriptionUUID
	}
	if f.InvoiceUUID != "" {
		filter["invoiceUUID"] = f.InvoiceUUID
	}
	if f.TenantUUID != "" {
		filter["tenantUUID"] = f.TenantUUID
	}
	if f.Status != "" {
		filter["status"] = f.Status
	}
	if f.Since != nil || f.Until != nil {
		rng := bson.M{}
		if f.Since != nil {
			rng["$gte"] = *f.Since
		}
		if f.Until != nil {
			rng["$lte"] = *f.Until
		}
		filter["createdAt"] = rng
	}
	opts := options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}})
	cur, err := r.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	out := make([]models.Transaction, 0)
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *transactionRepository) FindByTenant(ctx context.Context, tenantUUID string) ([]models.Transaction, error) {
	if tenantUUID == "" {
		return nil, nil
	}
	opts := options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}})
	filter := bson.M{"tenantUUID": tenantUUID}
	cur, err := r.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	out := make([]models.Transaction, 0)
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *transactionRepository) Update(ctx context.Context, t *models.Transaction) error {
	t.UpdatedAt = time.Now().UTC()
	res, err := r.coll.ReplaceOne(ctx, bson.M{"uuid": t.UUID}, t)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrTransactionNotFound
	}
	return nil
}
