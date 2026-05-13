package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/orkestra-cc/orkestra-addon-sales/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const jobsCollection = "sales_jobs"

// JobRepository handles persistence for sales intelligence jobs
type JobRepository interface {
	Create(ctx context.Context, job *models.Job) error
	GetByUUID(ctx context.Context, uuid string) (*models.Job, error)
	UpdateStatus(ctx context.Context, uuid string, status models.JobStatus, errorMsg string) error
	UpdateFull(ctx context.Context, job *models.Job) error
	ListByUser(ctx context.Context, userID string, status string, page, pageSize int) ([]models.Job, int64, error)
	UpdatePhases(ctx context.Context, uuid string, phases []models.JobPhase) error
	Delete(ctx context.Context, uuid string) error
	MarkStaleJobsFailed(ctx context.Context) error
}

type jobRepository struct {
	collection *mongo.Collection
}

// NewJobRepository creates a new JobRepository backed by MongoDB
func NewJobRepository(db *mongo.Database) JobRepository {
	coll := db.Collection(jobsCollection)

	// Create indexes
	indexModels := []mongo.IndexModel{
		{Keys: bson.D{{Key: "uuid", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "createdBy", Value: 1}, {Key: "createdAt", Value: -1}}},
		{Keys: bson.D{{Key: "status", Value: 1}}},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	coll.Indexes().CreateMany(ctx, indexModels)

	return &jobRepository{collection: coll}
}

func (r *jobRepository) Create(ctx context.Context, job *models.Job) error {
	_, err := r.collection.InsertOne(ctx, job)
	if err != nil {
		return fmt.Errorf("insert sales job: %w", err)
	}
	return nil
}

func (r *jobRepository) GetByUUID(ctx context.Context, uuid string) (*models.Job, error) {
	var job models.Job
	err := r.collection.FindOne(ctx, bson.M{"uuid": uuid}).Decode(&job)
	if err != nil {
		return nil, fmt.Errorf("find sales job %s: %w", uuid, err)
	}
	return &job, nil
}

func (r *jobRepository) UpdateStatus(ctx context.Context, uuid string, status models.JobStatus, errorMsg string) error {
	update := bson.M{
		"$set": bson.M{
			"status":    status,
			"updatedAt": time.Now(),
		},
	}
	if errorMsg != "" {
		update["$set"].(bson.M)["errorMessage"] = errorMsg
	}
	if status == models.JobStatusCompleted || status == models.JobStatusFailed || status == models.JobStatusCancelled {
		now := time.Now()
		update["$set"].(bson.M)["completedAt"] = now
	}

	_, err := r.collection.UpdateOne(ctx, bson.M{"uuid": uuid}, update)
	if err != nil {
		return fmt.Errorf("update sales job status: %w", err)
	}
	return nil
}

func (r *jobRepository) UpdateFull(ctx context.Context, job *models.Job) error {
	job.UpdatedAt = time.Now()
	update := bson.M{
		"$set": bson.M{
			"status":       job.Status,
			"phases":       job.Phases,
			"agentResults": job.AgentResults,
			"reportUuid":   job.ReportUUID,
			"totalScore":   job.TotalScore,
			"grade":        job.Grade,
			"errorMessage": job.ErrorMessage,
			"updatedAt":    job.UpdatedAt,
			"completedAt":  job.CompletedAt,
		},
	}
	_, err := r.collection.UpdateOne(ctx, bson.M{"uuid": job.UUID}, update)
	if err != nil {
		return fmt.Errorf("update full sales job: %w", err)
	}
	return nil
}

func (r *jobRepository) ListByUser(ctx context.Context, userID string, status string, page, pageSize int) ([]models.Job, int64, error) {
	filter := bson.M{"createdBy": userID}
	if status != "" {
		filter["status"] = status
	}

	total, err := r.collection.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("count sales jobs: %w", err)
	}

	skip := int64((page - 1) * pageSize)
	opts := options.Find().
		SetSort(bson.D{{Key: "createdAt", Value: -1}}).
		SetSkip(skip).
		SetLimit(int64(pageSize))

	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, 0, fmt.Errorf("find sales jobs: %w", err)
	}
	defer cursor.Close(ctx)

	var jobs []models.Job
	if err := cursor.All(ctx, &jobs); err != nil {
		return nil, 0, fmt.Errorf("decode sales jobs: %w", err)
	}
	return jobs, total, nil
}

func (r *jobRepository) Delete(ctx context.Context, uuid string) error {
	_, err := r.collection.DeleteOne(ctx, bson.M{"uuid": uuid})
	if err != nil {
		return fmt.Errorf("delete sales job: %w", err)
	}
	return nil
}

func (r *jobRepository) UpdatePhases(ctx context.Context, uuid string, phases []models.JobPhase) error {
	_, err := r.collection.UpdateOne(ctx, bson.M{"uuid": uuid}, bson.M{
		"$set": bson.M{"phases": phases, "updatedAt": time.Now()},
	})
	if err != nil {
		return fmt.Errorf("update phases: %w", err)
	}
	return nil
}

func (r *jobRepository) MarkStaleJobsFailed(ctx context.Context) error {
	filter := bson.M{
		"status": bson.M{"$in": []models.JobStatus{
			models.JobStatusQueued,
			models.JobStatusDiscovery,
			models.JobStatusAnalysis,
			models.JobStatusSynthesis,
		}},
	}
	update := bson.M{
		"$set": bson.M{
			"status":       models.JobStatusFailed,
			"errorMessage": "server restarted while job was in progress",
			"updatedAt":    time.Now(),
			"completedAt":  time.Now(),
		},
	}
	result, err := r.collection.UpdateMany(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("mark stale jobs failed: %w", err)
	}
	if result.ModifiedCount > 0 {
		// Caller will log this
	}
	return nil
}
