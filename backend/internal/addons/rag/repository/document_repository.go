package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/orkestra/backend/internal/addons/rag/models"
)

const documentCollection = "rag_documents"

// DocumentRepository defines CRUD operations for RAG documents
type DocumentRepository interface {
	Create(ctx context.Context, doc *models.RagDocument) error
	GetByUUID(ctx context.Context, uuid string) (*models.RagDocument, error)
	List(ctx context.Context, status, isoStandard string) ([]models.RagDocument, error)
	UpdateStatus(ctx context.Context, uuid, status, errMsg string) error
	UpdateCompleted(ctx context.Context, uuid string, chunkCount int) error
	UpdateMetadata(ctx context.Context, uuid string, title, isoStandard, version *string) (*models.RagDocument, error)
	Delete(ctx context.Context, uuid string) error
}

type documentRepository struct {
	collection *mongo.Collection
}

// NewDocumentRepository creates a new DocumentRepository
func NewDocumentRepository(db *mongo.Database) DocumentRepository {
	coll := db.Collection(documentCollection)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	indexes := []mongo.IndexModel{
		{Keys: bson.D{{Key: "uuid", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "status", Value: 1}}},
		{Keys: bson.D{{Key: "isoStandard", Value: 1}}},
	}
	coll.Indexes().CreateMany(ctx, indexes) //nolint:errcheck

	return &documentRepository{collection: coll}
}

func (r *documentRepository) Create(ctx context.Context, doc *models.RagDocument) error {
	if doc.UUID == "" {
		doc.UUID = uuid.New().String()
	}
	now := time.Now()
	doc.CreatedAt = now
	doc.UpdatedAt = now

	_, err := r.collection.InsertOne(ctx, doc)
	if err != nil {
		return fmt.Errorf("insert document: %w", err)
	}
	return nil
}

func (r *documentRepository) GetByUUID(ctx context.Context, uuid string) (*models.RagDocument, error) {
	var doc models.RagDocument
	err := r.collection.FindOne(ctx, bson.M{"uuid": uuid}).Decode(&doc)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, fmt.Errorf("document not found: %s", uuid)
		}
		return nil, fmt.Errorf("get document: %w", err)
	}
	return &doc, nil
}

func (r *documentRepository) List(ctx context.Context, status, isoStandard string) ([]models.RagDocument, error) {
	filter := bson.M{}
	if status != "" {
		filter["status"] = status
	}
	if isoStandard != "" {
		filter["isoStandard"] = isoStandard
	}

	cursor, err := r.collection.Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}}))
	if err != nil {
		return nil, fmt.Errorf("list documents: %w", err)
	}
	defer cursor.Close(ctx)

	var docs []models.RagDocument
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, fmt.Errorf("decode documents: %w", err)
	}
	return docs, nil
}

func (r *documentRepository) UpdateStatus(ctx context.Context, uuid, status, errMsg string) error {
	update := bson.M{"status": status, "updatedAt": time.Now()}
	if errMsg != "" {
		update["error"] = errMsg
	}
	_, err := r.collection.UpdateOne(ctx, bson.M{"uuid": uuid}, bson.M{"$set": update})
	return err
}

func (r *documentRepository) UpdateCompleted(ctx context.Context, uuid string, chunkCount int) error {
	now := time.Now()
	_, err := r.collection.UpdateOne(ctx, bson.M{"uuid": uuid}, bson.M{"$set": bson.M{
		"status":      "completed",
		"chunkCount":  chunkCount,
		"completedAt": now,
		"updatedAt":   now,
	}})
	return err
}

func (r *documentRepository) UpdateMetadata(ctx context.Context, uuid string, title, isoStandard, version *string) (*models.RagDocument, error) {
	update := bson.M{"updatedAt": time.Now()}
	if title != nil {
		update["title"] = *title
	}
	if isoStandard != nil {
		update["isoStandard"] = *isoStandard
	}
	if version != nil {
		update["version"] = *version
	}

	res, err := r.collection.UpdateOne(ctx, bson.M{"uuid": uuid}, bson.M{"$set": update})
	if err != nil {
		return nil, fmt.Errorf("update document: %w", err)
	}
	if res.MatchedCount == 0 {
		return nil, fmt.Errorf("document not found: %s", uuid)
	}

	return r.GetByUUID(ctx, uuid)
}

func (r *documentRepository) Delete(ctx context.Context, uuid string) error {
	res, err := r.collection.DeleteOne(ctx, bson.M{"uuid": uuid})
	if err != nil {
		return fmt.Errorf("delete document: %w", err)
	}
	if res.DeletedCount == 0 {
		return fmt.Errorf("document not found: %s", uuid)
	}
	return nil
}
