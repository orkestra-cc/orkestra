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

const reportsCollection = "sales_reports"

// ReportRepository handles persistence for generated prospect reports
type ReportRepository interface {
	Create(ctx context.Context, report *models.Report) error
	GetByUUID(ctx context.Context, uuid string) (*models.Report, error)
	GetByJobUUID(ctx context.Context, jobUUID string) (*models.Report, error)
	Delete(ctx context.Context, uuid string) error
	DeleteByJobUUID(ctx context.Context, jobUUID string) error
	ListByUser(ctx context.Context, userID string, page, pageSize int) ([]models.Report, int64, error)
}

type reportRepository struct {
	collection *mongo.Collection
}

func NewReportRepository(db *mongo.Database) ReportRepository {
	coll := db.Collection(reportsCollection)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	coll.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "uuid", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "jobUuid", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "createdBy", Value: 1}, {Key: "createdAt", Value: -1}}},
	})

	return &reportRepository{collection: coll}
}

func (r *reportRepository) Create(ctx context.Context, report *models.Report) error {
	_, err := r.collection.InsertOne(ctx, report)
	if err != nil {
		return fmt.Errorf("insert sales report: %w", err)
	}
	return nil
}

func (r *reportRepository) GetByUUID(ctx context.Context, uuid string) (*models.Report, error) {
	var report models.Report
	err := r.collection.FindOne(ctx, bson.M{"uuid": uuid}).Decode(&report)
	if err != nil {
		return nil, fmt.Errorf("find sales report %s: %w", uuid, err)
	}
	return &report, nil
}

func (r *reportRepository) GetByJobUUID(ctx context.Context, jobUUID string) (*models.Report, error) {
	var report models.Report
	err := r.collection.FindOne(ctx, bson.M{"jobUuid": jobUUID}).Decode(&report)
	if err != nil {
		return nil, fmt.Errorf("find report for job %s: %w", jobUUID, err)
	}
	return &report, nil
}

func (r *reportRepository) Delete(ctx context.Context, uuid string) error {
	_, err := r.collection.DeleteOne(ctx, bson.M{"uuid": uuid})
	if err != nil {
		return fmt.Errorf("delete sales report: %w", err)
	}
	return nil
}

func (r *reportRepository) DeleteByJobUUID(ctx context.Context, jobUUID string) error {
	_, err := r.collection.DeleteMany(ctx, bson.M{"jobUuid": jobUUID})
	if err != nil {
		return fmt.Errorf("delete sales reports by job: %w", err)
	}
	return nil
}

func (r *reportRepository) ListByUser(ctx context.Context, userID string, page, pageSize int) ([]models.Report, int64, error) {
	filter := bson.M{"createdBy": userID}

	total, err := r.collection.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("count sales reports: %w", err)
	}

	skip := int64((page - 1) * pageSize)
	opts := options.Find().
		SetSort(bson.D{{Key: "createdAt", Value: -1}}).
		SetSkip(skip).
		SetLimit(int64(pageSize)).
		SetProjection(bson.M{"contentMd": 0, "agentData": 0}) // exclude large fields from list

	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, 0, fmt.Errorf("find sales reports: %w", err)
	}
	defer cursor.Close(ctx)

	var reports []models.Report
	if err := cursor.All(ctx, &reports); err != nil {
		return nil, 0, fmt.Errorf("decode sales reports: %w", err)
	}
	return reports, total, nil
}
