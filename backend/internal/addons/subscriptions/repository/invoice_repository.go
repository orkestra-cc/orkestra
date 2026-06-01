package repository

import (
	"context"
	"errors"
	"time"

	"github.com/orkestra/backend/internal/addons/subscriptions/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var ErrInvoiceNotFound = errors.New("subscriptions: invoice not found")

type InvoiceFilters struct {
	SubscriptionUUID string
	TenantUUID       string
	Status           models.InvoiceStatus
}

type InvoiceRepository interface {
	Create(ctx context.Context, inv *models.SubscriptionInvoice) error
	GetByUUID(ctx context.Context, uuid string) (*models.SubscriptionInvoice, error)
	GetByNumber(ctx context.Context, number string) (*models.SubscriptionInvoice, error)
	List(ctx context.Context, f InvoiceFilters) ([]models.SubscriptionInvoice, error)
	Update(ctx context.Context, inv *models.SubscriptionInvoice) error
	// CountInYear returns how many invoices were issued in the given calendar
	// year (UTC). Used to build sequential invoice numbers.
	CountInYear(ctx context.Context, year int) (int64, error)
}

type invoiceRepository struct {
	coll *mongo.Collection
}

func NewInvoiceRepository(db *mongo.Database) InvoiceRepository {
	return &invoiceRepository{coll: db.Collection(models.InvoicesCollection)}
}

func (r *invoiceRepository) Create(ctx context.Context, inv *models.SubscriptionInvoice) error {
	now := time.Now().UTC()
	if inv.CreatedAt.IsZero() {
		inv.CreatedAt = now
	}
	inv.UpdatedAt = now
	_, err := r.coll.InsertOne(ctx, inv)
	return err
}

func (r *invoiceRepository) GetByUUID(ctx context.Context, uuid string) (*models.SubscriptionInvoice, error) {
	var inv models.SubscriptionInvoice
	err := r.coll.FindOne(ctx, bson.M{"uuid": uuid}).Decode(&inv)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrInvoiceNotFound
	}
	if err != nil {
		return nil, err
	}
	return &inv, nil
}

func (r *invoiceRepository) GetByNumber(ctx context.Context, number string) (*models.SubscriptionInvoice, error) {
	var inv models.SubscriptionInvoice
	err := r.coll.FindOne(ctx, bson.M{"number": number}).Decode(&inv)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrInvoiceNotFound
	}
	if err != nil {
		return nil, err
	}
	return &inv, nil
}

func (r *invoiceRepository) List(ctx context.Context, f InvoiceFilters) ([]models.SubscriptionInvoice, error) {
	filter := bson.M{}
	if f.SubscriptionUUID != "" {
		filter["subscriptionUUID"] = f.SubscriptionUUID
	}
	if f.TenantUUID != "" {
		filter["tenantUUID"] = f.TenantUUID
	}
	if f.Status != "" {
		filter["status"] = f.Status
	}
	opts := options.Find().SetSort(bson.D{{Key: "issuedAt", Value: -1}})
	cur, err := r.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	out := make([]models.SubscriptionInvoice, 0)
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *invoiceRepository) Update(ctx context.Context, inv *models.SubscriptionInvoice) error {
	inv.UpdatedAt = time.Now().UTC()
	res, err := r.coll.ReplaceOne(ctx, bson.M{"uuid": inv.UUID}, inv)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrInvoiceNotFound
	}
	return nil
}

func (r *invoiceRepository) CountInYear(ctx context.Context, year int) (int64, error) {
	start := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(year+1, 1, 1, 0, 0, 0, 0, time.UTC)
	return r.coll.CountDocuments(ctx, bson.M{
		"issuedAt": bson.M{"$gte": start, "$lt": end},
	})
}
