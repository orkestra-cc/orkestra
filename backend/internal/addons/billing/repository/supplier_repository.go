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

	"github.com/orkestra/backend/internal/addons/billing/models"
)

// Common errors
var (
	ErrSupplierNotFound      = errors.New("supplier not found")
	ErrSupplierAlreadyExists = errors.New("supplier with this fiscal ID already exists")
)

// SupplierRepository defines the interface for supplier data access
type SupplierRepository interface {
	// Create operations
	Create(ctx context.Context, supplier *models.Supplier) error

	// Read operations
	GetByID(ctx context.Context, id string) (*models.Supplier, error)
	GetByUUID(ctx context.Context, uuid string) (*models.Supplier, error)
	GetByFiscalID(ctx context.Context, fiscalIDCode string) (*models.Supplier, error)
	List(ctx context.Context, search string, pagination models.PaginationParams) ([]models.Supplier, int64, error)
	ListActive(ctx context.Context) ([]models.Supplier, error)

	// Update operations
	Update(ctx context.Context, supplier *models.Supplier) error

	// Delete operations
	SoftDelete(ctx context.Context, uuid string) error

	// Utility
	ExistsByFiscalID(ctx context.Context, fiscalIDCode string) (bool, error)
}

type supplierRepository struct {
	collection *mongo.Collection
}

// NewSupplierRepository creates a new SupplierRepository
func NewSupplierRepository(db *mongo.Database) SupplierRepository {
	repo := &supplierRepository{
		collection: db.Collection("billing_suppliers"),
	}
	repo.createIndexes(context.Background())
	return repo
}

func (r *supplierRepository) createIndexes(ctx context.Context) {
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
			Keys:    bson.D{{Key: "deletedAt", Value: 1}},
			Options: options.Index().SetSparse(true),
		},
		{
			Keys: bson.D{{Key: "createdAt", Value: -1}},
		},
	}

	_, _ = r.collection.Indexes().CreateMany(ctx, indexes)
}

func (r *supplierRepository) Create(ctx context.Context, supplier *models.Supplier) error {
	supplier.CreatedAt = time.Now()
	supplier.UpdatedAt = time.Now()
	supplier.IsActive = true

	result, err := r.collection.InsertOne(ctx, supplier)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return ErrSupplierAlreadyExists
		}
		return err
	}

	supplier.ID = result.InsertedID.(primitive.ObjectID)
	return nil
}

func (r *supplierRepository) GetByID(ctx context.Context, id string) (*models.Supplier, error) {
	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, ErrSupplierNotFound
	}

	var supplier models.Supplier
	err = r.collection.FindOne(ctx, bson.M{
		"_id":       objectID,
		"deletedAt": nil,
	}).Decode(&supplier)

	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrSupplierNotFound
		}
		return nil, err
	}

	return &supplier, nil
}

func (r *supplierRepository) GetByUUID(ctx context.Context, uuid string) (*models.Supplier, error) {
	var supplier models.Supplier
	err := r.collection.FindOne(ctx, bson.M{
		"uuid":      uuid,
		"deletedAt": nil,
	}).Decode(&supplier)

	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrSupplierNotFound
		}
		return nil, err
	}

	return &supplier, nil
}

func (r *supplierRepository) GetByFiscalID(ctx context.Context, fiscalIDCode string) (*models.Supplier, error) {
	var supplier models.Supplier
	err := r.collection.FindOne(ctx, bson.M{
		"fiscalIdCode": fiscalIDCode,
		"deletedAt":    nil,
	}).Decode(&supplier)

	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrSupplierNotFound
		}
		return nil, err
	}

	return &supplier, nil
}

func (r *supplierRepository) List(ctx context.Context, search string, pagination models.PaginationParams) ([]models.Supplier, int64, error) {
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

	var suppliers []models.Supplier
	if err := cursor.All(ctx, &suppliers); err != nil {
		return nil, 0, err
	}

	return suppliers, total, nil
}

func (r *supplierRepository) ListActive(ctx context.Context) ([]models.Supplier, error) {
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

	var suppliers []models.Supplier
	if err := cursor.All(ctx, &suppliers); err != nil {
		return nil, err
	}

	return suppliers, nil
}

func (r *supplierRepository) Update(ctx context.Context, supplier *models.Supplier) error {
	supplier.UpdatedAt = time.Now()

	result, err := r.collection.ReplaceOne(ctx, bson.M{
		"uuid":      supplier.UUID,
		"deletedAt": nil,
	}, supplier)

	if err != nil {
		return err
	}

	if result.MatchedCount == 0 {
		return ErrSupplierNotFound
	}

	return nil
}

func (r *supplierRepository) SoftDelete(ctx context.Context, uuid string) error {
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
		return ErrSupplierNotFound
	}

	return nil
}

func (r *supplierRepository) ExistsByFiscalID(ctx context.Context, fiscalIDCode string) (bool, error) {
	count, err := r.collection.CountDocuments(ctx, bson.M{
		"fiscalIdCode": fiscalIDCode,
		"deletedAt":    nil,
	})

	if err != nil {
		return false, err
	}

	return count > 0, nil
}
