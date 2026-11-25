package repository

import (
	"context"
	"errors"
	"time"

	"github.com/orkestra/backend/internal/core/compliance/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ErrDSRExportNotFound is returned when an export row does not exist.
var ErrDSRExportNotFound = errors.New("compliance: dsr export not found")

// DSRExportRepository persists the index of stored DSR exports.
type DSRExportRepository struct {
	coll *mongo.Collection
}

// NewDSRExportRepo returns a repository bound to the exports collection.
func NewDSRExportRepo(db *mongo.Database) *DSRExportRepository {
	return &DSRExportRepository{coll: db.Collection(models.DSRExportsCollection)}
}

// Insert appends a new export row.
func (r *DSRExportRepository) Insert(ctx context.Context, e *models.DSRExport) error {
	_, err := r.coll.InsertOne(ctx, e)
	return err
}

// Get returns one export by uuid.
func (r *DSRExportRepository) Get(ctx context.Context, uuid string) (*models.DSRExport, error) {
	var e models.DSRExport
	//tenantscope:allow DSR exports are by data subject, not by tenant; the query pins uuid.
	err := r.coll.FindOne(ctx, bson.M{"uuid": uuid}).Decode(&e)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrDSRExportNotFound
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// ListByUser returns the subject's exports, newest first.
func (r *DSRExportRepository) ListByUser(ctx context.Context, userUUID string) ([]models.DSRExport, error) {
	//tenantscope:allow DSR exports are by data subject, not by tenant; the query pins userUuid.
	cur, err := r.coll.Find(ctx, bson.M{"userUuid": userUUID},
		options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}, {Key: "uuid", Value: -1}}))
	if err != nil {
		return nil, err
	}
	var out []models.DSRExport
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListExpired returns up to limit exports whose expiry is at or before now.
func (r *DSRExportRepository) ListExpired(ctx context.Context, now time.Time, limit int) ([]models.DSRExport, error) {
	//tenantscope:allow the cleanup job walks every subject's expired exports.
	cur, err := r.coll.Find(ctx, bson.M{"expiresAt": bson.M{"$lte": now}},
		options.Find().SetSort(bson.D{{Key: "expiresAt", Value: 1}}).SetLimit(int64(limit)))
	if err != nil {
		return nil, err
	}
	var out []models.DSRExport
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Delete removes the row; a missing row is not an error (idempotent).
func (r *DSRExportRepository) Delete(ctx context.Context, uuid string) error {
	//tenantscope:allow DSR exports are by data subject, not by tenant; the query pins uuid.
	_, err := r.coll.DeleteOne(ctx, bson.M{"uuid": uuid})
	return err
}

// DSRExportErasureRepository keeps the per-subject erasure markers.
type DSRExportErasureRepository struct {
	coll *mongo.Collection
}

// NewDSRExportErasureRepo returns a repository bound to the markers collection.
func NewDSRExportErasureRepo(db *mongo.Database) *DSRExportErasureRepository {
	return &DSRExportErasureRepository{coll: db.Collection(models.DSRExportErasuresCollection)}
}

// Mark records that an erasure of the subject's exports started at at; the
// marker only moves forward.
func (r *DSRExportErasureRepository) Mark(ctx context.Context, userUUID string, at time.Time) error {
	//tenantscope:allow the marker is by data subject, not by tenant; the query pins userUuid.
	_, err := r.coll.UpdateOne(ctx, bson.M{"userUuid": userUUID},
		bson.M{"$max": bson.M{"erasedAt": at}, "$setOnInsert": bson.M{"userUuid": userUUID}},
		options.Update().SetUpsert(true))
	return err
}

// ErasedAt returns the marker time, ok=false when the subject has none.
func (r *DSRExportErasureRepository) ErasedAt(ctx context.Context, userUUID string) (time.Time, bool, error) {
	var m models.DSRExportErasure
	//tenantscope:allow the marker is by data subject, not by tenant; the query pins userUuid.
	err := r.coll.FindOne(ctx, bson.M{"userUuid": userUUID}).Decode(&m)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	return m.ErasedAt, true, nil
}

// DeleteBefore removes the markers older than t and returns how many.
func (r *DSRExportErasureRepository) DeleteBefore(ctx context.Context, t time.Time) (int, error) {
	//tenantscope:allow the cleanup job walks every subject's markers.
	res, err := r.coll.DeleteMany(ctx, bson.M{"erasedAt": bson.M{"$lt": t}})
	if err != nil {
		return 0, err
	}
	return int(res.DeletedCount), nil
}
