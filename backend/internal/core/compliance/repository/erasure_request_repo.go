package repository

import (
	"context"
	"errors"
	"time"

	"github.com/orkestra/backend/internal/core/compliance/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// ErrErasureRequestNotFound is returned when resolving a request that doesn't
// exist or is no longer pending.
var ErrErasureRequestNotFound = errors.New("compliance: erasure request not found or already resolved")

// ErasureRequestRepository persists right-to-erasure requests.
type ErasureRequestRepository struct {
	coll *mongo.Collection
}

// NewErasureRequestRepo binds the repository to its collection.
func NewErasureRequestRepo(db *mongo.Database) *ErasureRequestRepository {
	return &ErasureRequestRepository{coll: db.Collection(models.ErasureRequestsCollection)}
}

// Insert appends a new request.
func (r *ErasureRequestRepository) Insert(ctx context.Context, req *models.ErasureRequest) error {
	_, err := r.coll.InsertOne(ctx, req)
	return err
}

// Get returns one request by UUID.
func (r *ErasureRequestRepository) Get(ctx context.Context, uuid string) (*models.ErasureRequest, error) {
	var req models.ErasureRequest
	//tenantscope:allow compliance admin resolves any erasure request by its own UUID — cross-tenant by design.
	err := r.coll.FindOne(ctx, bson.M{"uuid": uuid}).Decode(&req)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrErasureRequestNotFound
	}
	if err != nil {
		return nil, err
	}
	return &req, nil
}

// ListByStatus lists requests in a given status, newest-first.
func (r *ErasureRequestRepository) ListByStatus(ctx context.Context, status string) ([]models.ErasureRequest, error) {
	filter := bson.M{}
	if status != "" {
		filter["status"] = status
	}
	//tenantscope:allow compliance admin reviews erasure requests platform-wide.
	cur, err := r.coll.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []models.ErasureRequest
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Resolve transitions a pending request to completed/rejected.
func (r *ErasureRequestRepository) Resolve(ctx context.Context, uuid, status, resolvedBy, mode, note string) error {
	now := time.Now().UTC()
	//tenantscope:allow resolves a specific erasure request by its own UUID — cross-tenant by design.
	res, err := r.coll.UpdateOne(ctx,
		bson.M{"uuid": uuid, "status": models.ErasureRequestPending},
		bson.M{"$set": bson.M{
			"status":         status,
			"resolvedAt":     now,
			"resolvedBy":     resolvedBy,
			"mode":           mode,
			"resolutionNote": note,
		}},
	)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrErasureRequestNotFound
	}
	return nil
}
