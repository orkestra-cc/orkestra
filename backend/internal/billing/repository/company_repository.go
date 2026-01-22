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

// Company-specific errors
var (
	ErrCompanyNotFound      = errors.New("company not found")
	ErrCompanyAlreadyExists = errors.New("company with this fiscal ID already exists")
	ErrNoDefaultCompany     = errors.New("no default company configured")
)

// CompanyRepository defines the interface for company data access
type CompanyRepository interface {
	// Create operations
	Create(ctx context.Context, company *models.Company) error

	// Read operations
	GetByID(ctx context.Context, id string) (*models.Company, error)
	GetByUUID(ctx context.Context, uuid string) (*models.Company, error)
	GetByFiscalID(ctx context.Context, fiscalIDCode string) (*models.Company, error)
	GetDefault(ctx context.Context) (*models.Company, error)
	List(ctx context.Context, search string, pagination models.PaginationParams) ([]models.Company, int64, error)
	ListActive(ctx context.Context) ([]models.Company, error)

	// Update operations
	Update(ctx context.Context, company *models.Company) error
	SetDefault(ctx context.Context, uuid string) error
	ClearDefault(ctx context.Context) error

	// Delete operations
	SoftDelete(ctx context.Context, uuid string) error

	// Utility
	ExistsByFiscalID(ctx context.Context, fiscalIDCode string) (bool, error)
	Count(ctx context.Context) (int64, error)
}

type companyRepository struct {
	collection *mongo.Collection
}

// NewCompanyRepository creates a new CompanyRepository
func NewCompanyRepository(db *mongo.Database) CompanyRepository {
	repo := &companyRepository{
		collection: db.Collection("billing_companies"),
	}
	repo.createIndexes(context.Background())
	return repo
}

func (r *companyRepository) createIndexes(ctx context.Context) {
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
			Keys: bson.D{{Key: "isDefault", Value: 1}},
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

func (r *companyRepository) Create(ctx context.Context, company *models.Company) error {
	company.CreatedAt = time.Now()
	company.UpdatedAt = time.Now()
	company.IsActive = true

	result, err := r.collection.InsertOne(ctx, company)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return ErrCompanyAlreadyExists
		}
		return err
	}

	company.ID = result.InsertedID.(primitive.ObjectID)
	return nil
}

func (r *companyRepository) GetByID(ctx context.Context, id string) (*models.Company, error) {
	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, ErrCompanyNotFound
	}

	var company models.Company
	err = r.collection.FindOne(ctx, bson.M{
		"_id":       objectID,
		"deletedAt": nil,
	}).Decode(&company)

	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrCompanyNotFound
		}
		return nil, err
	}

	return &company, nil
}

func (r *companyRepository) GetByUUID(ctx context.Context, uuid string) (*models.Company, error) {
	var company models.Company
	err := r.collection.FindOne(ctx, bson.M{
		"uuid":      uuid,
		"deletedAt": nil,
	}).Decode(&company)

	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrCompanyNotFound
		}
		return nil, err
	}

	return &company, nil
}

func (r *companyRepository) GetByFiscalID(ctx context.Context, fiscalIDCode string) (*models.Company, error) {
	var company models.Company
	err := r.collection.FindOne(ctx, bson.M{
		"fiscalIdCode": fiscalIDCode,
		"deletedAt":    nil,
	}).Decode(&company)

	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrCompanyNotFound
		}
		return nil, err
	}

	return &company, nil
}

func (r *companyRepository) GetDefault(ctx context.Context) (*models.Company, error) {
	var company models.Company
	err := r.collection.FindOne(ctx, bson.M{
		"isDefault": true,
		"isActive":  true,
		"deletedAt": nil,
	}).Decode(&company)

	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrNoDefaultCompany
		}
		return nil, err
	}

	return &company, nil
}

func (r *companyRepository) List(ctx context.Context, search string, pagination models.PaginationParams) ([]models.Company, int64, error) {
	filter := bson.M{
		"deletedAt": nil,
		"isActive":  true,
	}

	if search != "" {
		// Escape special regex characters to prevent ReDoS attacks
		escapedSearch := regexp.QuoteMeta(search)
		filter["$or"] = []bson.M{
			{"denomination": bson.M{"$regex": escapedSearch, "$options": "i"}},
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

	// Sort by isDefault first (default company appears first), then by denomination
	opts := options.Find().
		SetSort(bson.D{{Key: "isDefault", Value: -1}, {Key: "denomination", Value: 1}}).
		SetSkip(skip).
		SetLimit(limit)

	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)

	var companies []models.Company
	if err := cursor.All(ctx, &companies); err != nil {
		return nil, 0, err
	}

	return companies, total, nil
}

func (r *companyRepository) ListActive(ctx context.Context) ([]models.Company, error) {
	filter := bson.M{
		"deletedAt": nil,
		"isActive":  true,
	}

	opts := options.Find().
		SetSort(bson.D{{Key: "isDefault", Value: -1}, {Key: "denomination", Value: 1}})

	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var companies []models.Company
	if err := cursor.All(ctx, &companies); err != nil {
		return nil, err
	}

	return companies, nil
}

func (r *companyRepository) Update(ctx context.Context, company *models.Company) error {
	company.UpdatedAt = time.Now()

	result, err := r.collection.ReplaceOne(ctx, bson.M{
		"uuid":      company.UUID,
		"deletedAt": nil,
	}, company)

	if err != nil {
		return err
	}

	if result.MatchedCount == 0 {
		return ErrCompanyNotFound
	}

	return nil
}

func (r *companyRepository) SetDefault(ctx context.Context, uuid string) error {
	// First, clear any existing default
	if err := r.ClearDefault(ctx); err != nil {
		return err
	}

	// Set the new default
	now := time.Now()
	result, err := r.collection.UpdateOne(ctx, bson.M{
		"uuid":      uuid,
		"deletedAt": nil,
	}, bson.M{
		"$set": bson.M{
			"isDefault": true,
			"updatedAt": now,
		},
	})

	if err != nil {
		return err
	}

	if result.MatchedCount == 0 {
		return ErrCompanyNotFound
	}

	return nil
}

func (r *companyRepository) ClearDefault(ctx context.Context) error {
	now := time.Now()
	_, err := r.collection.UpdateMany(ctx, bson.M{
		"isDefault": true,
		"deletedAt": nil,
	}, bson.M{
		"$set": bson.M{
			"isDefault": false,
			"updatedAt": now,
		},
	})

	return err
}

func (r *companyRepository) SoftDelete(ctx context.Context, uuid string) error {
	now := time.Now()
	update := bson.M{
		"$set": bson.M{
			"deletedAt": now,
			"updatedAt": now,
			"isActive":  false,
			"isDefault": false, // Clear default if deleting the default company
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
		return ErrCompanyNotFound
	}

	return nil
}

func (r *companyRepository) ExistsByFiscalID(ctx context.Context, fiscalIDCode string) (bool, error) {
	count, err := r.collection.CountDocuments(ctx, bson.M{
		"fiscalIdCode": fiscalIDCode,
		"deletedAt":    nil,
	})

	if err != nil {
		return false, err
	}

	return count > 0, nil
}

func (r *companyRepository) Count(ctx context.Context) (int64, error) {
	return r.collection.CountDocuments(ctx, bson.M{
		"deletedAt": nil,
		"isActive":  true,
	})
}
