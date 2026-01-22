package repository

import (
	"context"
	"errors"
	"regexp"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/orkestra/backend/internal/billing/models"
)

// Common errors
var (
	ErrCustomerNotFound     = errors.New("customer not found")
	ErrCustomerAlreadyExists = errors.New("customer with this fiscal ID already exists")
)

// CustomerRepository defines the interface for customer data access
type CustomerRepository interface {
	// Create operations
	Create(ctx context.Context, customer *models.Customer) error

	// Read operations
	GetByID(ctx context.Context, id string) (*models.Customer, error)
	GetByUUID(ctx context.Context, uuid string) (*models.Customer, error)
	GetByFiscalID(ctx context.Context, fiscalIDCode string) (*models.Customer, error)
	List(ctx context.Context, search string, pagination models.PaginationParams) ([]models.Customer, int64, error)
	ListActive(ctx context.Context) ([]models.Customer, error)

	// Update operations
	Update(ctx context.Context, customer *models.Customer) error

	// Delete operations
	SoftDelete(ctx context.Context, uuid string) error

	// Utility
	ExistsByFiscalID(ctx context.Context, fiscalIDCode string) (bool, error)
}

type customerRepository struct {
	collection *mongo.Collection
}

// NewCustomerRepository creates a new CustomerRepository
func NewCustomerRepository(db *mongo.Database) CustomerRepository {
	repo := &customerRepository{
		collection: db.Collection("billing_customers"),
	}
	repo.createIndexes(context.Background())
	return repo
}

func (r *customerRepository) createIndexes(ctx context.Context) {
	indexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "uuid", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys:    bson.D{{Key: "fiscalIdCode", Value: 1}},
			Options: options.Index().SetUnique(true).SetSparse(true),
		},
		{
			Keys: bson.D{{Key: "denomination", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "isActive", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "isPA", Value: 1}},
		},
		{
			Keys:    bson.D{{Key: "deletedAt", Value: 1}},
			Options: options.Index().SetSparse(true),
		},
		{
			Keys: bson.D{{Key: "createdAt", Value: -1}},
		},
	}

	_, _ = r.collection.Indexes().CreateMany(ctx, indexes)
}

func (r *customerRepository) Create(ctx context.Context, customer *models.Customer) error {
	customer.CreatedAt = time.Now()
	customer.UpdatedAt = time.Now()
	customer.IsActive = true

	result, err := r.collection.InsertOne(ctx, customer)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return ErrCustomerAlreadyExists
		}
		return err
	}

	customer.ID = result.InsertedID.(primitive.ObjectID)
	return nil
}

func (r *customerRepository) GetByID(ctx context.Context, id string) (*models.Customer, error) {
	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, ErrCustomerNotFound
	}

	var customer models.Customer
	err = r.collection.FindOne(ctx, bson.M{
		"_id":       objectID,
		"deletedAt": nil,
	}).Decode(&customer)

	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrCustomerNotFound
		}
		return nil, err
	}

	return &customer, nil
}

func (r *customerRepository) GetByUUID(ctx context.Context, uuid string) (*models.Customer, error) {
	var customer models.Customer
	err := r.collection.FindOne(ctx, bson.M{
		"uuid":      uuid,
		"deletedAt": nil,
	}).Decode(&customer)

	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrCustomerNotFound
		}
		return nil, err
	}

	return &customer, nil
}

func (r *customerRepository) GetByFiscalID(ctx context.Context, fiscalIDCode string) (*models.Customer, error) {
	var customer models.Customer
	err := r.collection.FindOne(ctx, bson.M{
		"fiscalIdCode": fiscalIDCode,
		"deletedAt":    nil,
	}).Decode(&customer)

	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrCustomerNotFound
		}
		return nil, err
	}

	return &customer, nil
}

func (r *customerRepository) List(ctx context.Context, search string, pagination models.PaginationParams) ([]models.Customer, int64, error) {
	filter := bson.M{
		"deletedAt": nil,
		"isActive":  true,
	}

	if search != "" {
		// Escape special regex characters to prevent ReDoS attacks
		escapedSearch := regexp.QuoteMeta(search)
		filter["$or"] = []bson.M{
			{"denomination": bson.M{"$regex": escapedSearch, "$options": "i"}},
			{"name": bson.M{"$regex": escapedSearch, "$options": "i"}},
			{"surname": bson.M{"$regex": escapedSearch, "$options": "i"}},
			{"fiscalIdCode": bson.M{"$regex": escapedSearch, "$options": "i"}},
			{"email": bson.M{"$regex": escapedSearch, "$options": "i"}},
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
		SetSort(bson.D{{Key: "denomination", Value: 1}, {Key: "surname", Value: 1}, {Key: "name", Value: 1}}).
		SetSkip(skip).
		SetLimit(limit)

	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)

	var customers []models.Customer
	if err := cursor.All(ctx, &customers); err != nil {
		return nil, 0, err
	}

	return customers, total, nil
}

func (r *customerRepository) ListActive(ctx context.Context) ([]models.Customer, error) {
	filter := bson.M{
		"deletedAt": nil,
		"isActive":  true,
	}

	opts := options.Find().
		SetSort(bson.D{{Key: "denomination", Value: 1}, {Key: "surname", Value: 1}, {Key: "name", Value: 1}})

	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var customers []models.Customer
	if err := cursor.All(ctx, &customers); err != nil {
		return nil, err
	}

	return customers, nil
}

func (r *customerRepository) Update(ctx context.Context, customer *models.Customer) error {
	customer.UpdatedAt = time.Now()

	result, err := r.collection.ReplaceOne(ctx, bson.M{
		"uuid":      customer.UUID,
		"deletedAt": nil,
	}, customer)

	if err != nil {
		return err
	}

	if result.MatchedCount == 0 {
		return ErrCustomerNotFound
	}

	return nil
}

func (r *customerRepository) SoftDelete(ctx context.Context, uuid string) error {
	now := time.Now()
	update := bson.M{
		"$set": bson.M{
			"deletedAt": now,
			"updatedAt": now,
			"isActive":  false,
		},
	}

	result, err := r.collection.UpdateOne(ctx, bson.M{
		"uuid":      uuid,
		"deletedAt": nil,
	}, update)

	if err != nil {
		return err
	}

	if result.MatchedCount == 0 {
		return ErrCustomerNotFound
	}

	return nil
}

func (r *customerRepository) ExistsByFiscalID(ctx context.Context, fiscalIDCode string) (bool, error) {
	count, err := r.collection.CountDocuments(ctx, bson.M{
		"fiscalIdCode": fiscalIDCode,
		"deletedAt":    nil,
	})

	if err != nil {
		return false, err
	}

	return count > 0, nil
}
