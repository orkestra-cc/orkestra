package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra/backend/internal/addons/documents/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Package-level errors for document repository
var (
	ErrDocumentNotFound = errors.New("document not found")
)

// DocumentRepository defines the interface for generated document data access
type DocumentRepository interface {
	// Create operations
	Create(ctx context.Context, doc *models.GeneratedDocument) error

	// Read operations
	GetByUUID(ctx context.Context, uuid string) (*models.GeneratedDocument, error)
	GetByUUIDWithContent(ctx context.Context, uuid string) (*models.GeneratedDocument, error)
	GetBySource(ctx context.Context, sourceType models.SourceType, sourceUUID string) (*models.GeneratedDocument, error)
	List(ctx context.Context, pagination models.PaginationParams) ([]models.GeneratedDocument, int64, error)
	ListBySource(ctx context.Context, sourceType models.SourceType, sourceUUID string) ([]models.GeneratedDocument, error)

	// Delete operations
	SoftDelete(ctx context.Context, uuid string) error
	DeleteExpired(ctx context.Context) (int64, error)

	// Utility
	Count(ctx context.Context) (int64, error)
}

type documentRepository struct {
	collection *mongo.Collection
}

// NewDocumentRepository creates a new document repository instance
func NewDocumentRepository(db *mongo.Database) DocumentRepository {
	repo := &documentRepository{
		collection: db.Collection(models.DocumentOutputsCollection),
	}
	repo.createIndexes(context.Background())
	return repo
}

func (r *documentRepository) createIndexes(ctx context.Context) {
	indexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "uuid", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{{Key: "sourceType", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "sourceUuid", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "templateUuid", Value: 1}},
		},
		{
			Keys:    bson.D{{Key: "deletedAt", Value: 1}},
			Options: options.Index().SetSparse(true),
		},
		{
			Keys: bson.D{{Key: "generatedAt", Value: -1}},
		},
		{
			Keys: bson.D{{Key: "expiresAt", Value: 1}},
			Options: options.Index().SetSparse(true).SetExpireAfterSeconds(0),
		},
		// Compound index for source lookups
		{
			Keys: bson.D{
				{Key: "sourceType", Value: 1},
				{Key: "sourceUuid", Value: 1},
			},
		},
	}
	_, _ = r.collection.Indexes().CreateMany(ctx, indexes)
}

// Create creates a new generated document
func (r *documentRepository) Create(ctx context.Context, doc *models.GeneratedDocument) error {
	if doc.UUID == "" {
		doc.UUID = uuid.New().String()
	}
	now := time.Now()
	doc.CreatedAt = now
	doc.GeneratedAt = now

	_, err := r.collection.InsertOne(ctx, doc)
	return err
}

// GetByUUID retrieves a document by UUID (without the binary PDF content)
func (r *documentRepository) GetByUUID(ctx context.Context, uuid string) (*models.GeneratedDocument, error) {
	var doc models.GeneratedDocument
	// Exclude pdfContent to reduce memory usage
	opts := options.FindOne().SetProjection(bson.M{"pdfContent": 0})
	err := r.collection.FindOne(ctx, bson.M{
		"uuid":      uuid,
		"deletedAt": nil,
	}, opts).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return nil, ErrDocumentNotFound
	}
	return &doc, err
}

// GetByUUIDWithContent retrieves a document by UUID including the binary PDF content
func (r *documentRepository) GetByUUIDWithContent(ctx context.Context, uuid string) (*models.GeneratedDocument, error) {
	var doc models.GeneratedDocument
	err := r.collection.FindOne(ctx, bson.M{
		"uuid":      uuid,
		"deletedAt": nil,
	}).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return nil, ErrDocumentNotFound
	}
	return &doc, err
}

// GetBySource retrieves the most recent document for a specific source
func (r *documentRepository) GetBySource(ctx context.Context, sourceType models.SourceType, sourceUUID string) (*models.GeneratedDocument, error) {
	var doc models.GeneratedDocument
	opts := options.FindOne().
		SetProjection(bson.M{"pdfContent": 0}).
		SetSort(bson.D{{Key: "generatedAt", Value: -1}})

	err := r.collection.FindOne(ctx, bson.M{
		"sourceType": sourceType,
		"sourceUuid": sourceUUID,
		"deletedAt":  nil,
	}, opts).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return nil, ErrDocumentNotFound
	}
	return &doc, err
}

// List retrieves documents with pagination (without binary content)
func (r *documentRepository) List(ctx context.Context, pagination models.PaginationParams) ([]models.GeneratedDocument, int64, error) {
	filter := bson.M{"deletedAt": nil}

	// Count total matching documents
	total, err := r.collection.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, err
	}

	// Set up pagination
	skip := int64((pagination.Page - 1) * pagination.PageSize)
	limit := int64(pagination.PageSize)

	opts := options.Find().
		SetSkip(skip).
		SetLimit(limit).
		SetSort(bson.D{{Key: "generatedAt", Value: -1}}).
		SetProjection(bson.M{"pdfContent": 0})

	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)

	var docs []models.GeneratedDocument
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, 0, err
	}

	return docs, total, nil
}

// ListBySource retrieves all documents for a specific source
func (r *documentRepository) ListBySource(ctx context.Context, sourceType models.SourceType, sourceUUID string) ([]models.GeneratedDocument, error) {
	opts := options.Find().
		SetProjection(bson.M{"pdfContent": 0}).
		SetSort(bson.D{{Key: "generatedAt", Value: -1}})

	cursor, err := r.collection.Find(ctx, bson.M{
		"sourceType": sourceType,
		"sourceUuid": sourceUUID,
		"deletedAt":  nil,
	}, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []models.GeneratedDocument
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

// SoftDelete performs a soft delete on a document
func (r *documentRepository) SoftDelete(ctx context.Context, uuid string) error {
	now := time.Now()
	update := bson.M{
		"$set": bson.M{
			"deletedAt": now,
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
		return ErrDocumentNotFound
	}
	return nil
}

// DeleteExpired deletes documents that have expired
func (r *documentRepository) DeleteExpired(ctx context.Context) (int64, error) {
	now := time.Now()
	result, err := r.collection.DeleteMany(ctx, bson.M{
		"expiresAt": bson.M{"$lte": now},
	})
	if err != nil {
		return 0, err
	}
	return result.DeletedCount, nil
}

// Count returns the total number of documents
func (r *documentRepository) Count(ctx context.Context) (int64, error) {
	return r.collection.CountDocuments(ctx, bson.M{"deletedAt": nil})
}
