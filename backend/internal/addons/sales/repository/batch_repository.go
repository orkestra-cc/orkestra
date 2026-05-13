package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/orkestra-cc/orkestra-addon-sales/models"
)

// BatchRepository handles persistence of batch jobs
type BatchRepository interface {
	Create(ctx context.Context, batch *models.BatchJob) error
	GetByUUID(ctx context.Context, uuid string) (*models.BatchJob, error)
	GetByJobUUID(ctx context.Context, jobUUID string) (*models.BatchJob, error)
	ListPending(ctx context.Context) ([]models.BatchJob, error)
	UpdateStatus(ctx context.Context, uuid, status, errMsg string) error
	UpdateResults(ctx context.Context, uuid string, results []models.BatchResultEntry) error
}

type batchRepository struct {
	col *mongo.Collection
}

// NewBatchRepository creates a new BatchRepository backed by MongoDB
func NewBatchRepository(db *mongo.Database) BatchRepository {
	col := db.Collection("sales_batches")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	col.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "uuid", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "jobUuid", Value: 1}}},
		{Keys: bson.D{{Key: "status", Value: 1}}},
	})

	return &batchRepository{col: col}
}

func (r *batchRepository) Create(ctx context.Context, batch *models.BatchJob) error {
	if batch.UUID == "" {
		batch.UUID = uuid.New().String()
	}
	batch.CreatedAt = time.Now()
	batch.UpdatedAt = time.Now()
	_, err := r.col.InsertOne(ctx, batch)
	return err
}

func (r *batchRepository) GetByUUID(ctx context.Context, uuid string) (*models.BatchJob, error) {
	var batch models.BatchJob
	err := r.col.FindOne(ctx, bson.M{"uuid": uuid}).Decode(&batch)
	if err != nil {
		return nil, fmt.Errorf("batch not found: %w", err)
	}
	return &batch, nil
}

func (r *batchRepository) GetByJobUUID(ctx context.Context, jobUUID string) (*models.BatchJob, error) {
	var batch models.BatchJob
	err := r.col.FindOne(ctx, bson.M{"jobUuid": jobUUID}).Decode(&batch)
	if err != nil {
		return nil, fmt.Errorf("batch not found for job: %w", err)
	}
	return &batch, nil
}

func (r *batchRepository) ListPending(ctx context.Context) ([]models.BatchJob, error) {
	filter := bson.M{"status": bson.M{"$in": []string{"submitted", "processing"}}}
	cursor, err := r.col.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var batches []models.BatchJob
	if err := cursor.All(ctx, &batches); err != nil {
		return nil, err
	}
	return batches, nil
}

func (r *batchRepository) UpdateStatus(ctx context.Context, uuid, status, errMsg string) error {
	update := bson.M{
		"$set": bson.M{
			"status":    status,
			"updatedAt": time.Now(),
		},
	}
	if errMsg != "" {
		update["$set"].(bson.M)["error"] = errMsg
	}
	if status == "completed" || status == "failed" {
		now := time.Now()
		update["$set"].(bson.M)["completedAt"] = &now
	}
	_, err := r.col.UpdateOne(ctx, bson.M{"uuid": uuid}, update)
	return err
}

func (r *batchRepository) UpdateResults(ctx context.Context, uuid string, results []models.BatchResultEntry) error {
	now := time.Now()
	_, err := r.col.UpdateOne(ctx, bson.M{"uuid": uuid}, bson.M{
		"$set": bson.M{
			"results":     results,
			"status":      "completed",
			"completedAt": &now,
			"updatedAt":   now,
		},
	})
	return err
}
