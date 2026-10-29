package repository

import (
	"context"
	"errors"
	"time"

	"github.com/orkestra/backend/internal/core/auth/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ErrPINDeviceNotFound reports that the user has no such registered device.
var ErrPINDeviceNotFound = errors.New("pin device not found")

// PINDeviceRepository persists the per-user PIN device registry
// (operator_pin_devices / client_pin_devices). One row per (user, device):
// re-enrolling a device replaces its secret.
type PINDeviceRepository interface {
	// Upsert inserts or replaces the (UserUUID, DeviceID) row. The stored
	// UUID and CreatedAt are those of the new document.
	Upsert(ctx context.Context, doc *models.PINDeviceDoc) error
	// Find returns the user's row for deviceID, or ErrPINDeviceNotFound.
	Find(ctx context.Context, userUUID, deviceID string) (*models.PINDeviceDoc, error)
	// ListByUser returns the user's devices, newest first.
	ListByUser(ctx context.Context, userUUID string) ([]models.PINDeviceDoc, error)
	// TouchLastUsed stamps a successful PIN sign-in from the device.
	TouchLastUsed(ctx context.Context, userUUID, deviceID string, when time.Time) error
	// Delete removes one device; ErrPINDeviceNotFound when absent.
	Delete(ctx context.Context, userUUID, deviceID string) error
	// DeleteAllByUser removes every device of the user (PIN removal,
	// GDPR erasure) and returns the count.
	DeleteAllByUser(ctx context.Context, userUUID string) (int64, error)
}

type pinDeviceRepository struct {
	coll *mongo.Collection
	tier string
}

// NewOperatorPINDeviceRepository binds to operator_pin_devices.
func NewOperatorPINDeviceRepository(db *mongo.Database) PINDeviceRepository {
	return &pinDeviceRepository{coll: db.Collection(models.OperatorPINDevicesCollection), tier: models.TierOperator}
}

// NewClientPINDeviceRepository binds to client_pin_devices.
func NewClientPINDeviceRepository(db *mongo.Database) PINDeviceRepository {
	return &pinDeviceRepository{coll: db.Collection(models.ClientPINDevicesCollection), tier: models.TierClient}
}

func (r *pinDeviceRepository) Upsert(ctx context.Context, doc *models.PINDeviceDoc) error {
	if doc.CreatedAt.IsZero() {
		doc.CreatedAt = time.Now()
	}
	if r.tier != "" {
		doc.Tier = r.tier
	}
	//tenantscope:allow PIN devices are keyed on the user, like the user rows they belong to
	_, err := r.coll.ReplaceOne(ctx,
		bson.M{"userUuid": doc.UserUUID, "deviceId": doc.DeviceID},
		doc,
		options.Replace().SetUpsert(true))
	return err
}

func (r *pinDeviceRepository) Find(ctx context.Context, userUUID, deviceID string) (*models.PINDeviceDoc, error) {
	var doc models.PINDeviceDoc
	//tenantscope:allow PIN devices are keyed on the user, like the user rows they belong to
	err := r.coll.FindOne(ctx, bson.M{"userUuid": userUUID, "deviceId": deviceID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrPINDeviceNotFound
	}
	if err != nil {
		return nil, err
	}
	return &doc, nil
}

func (r *pinDeviceRepository) ListByUser(ctx context.Context, userUUID string) ([]models.PINDeviceDoc, error) {
	//tenantscope:allow PIN devices are keyed on the user, like the user rows they belong to
	cur, err := r.coll.Find(ctx, bson.M{"userUuid": userUUID}, options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}}))
	if err != nil {
		return nil, err
	}
	out := []models.PINDeviceDoc{}
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *pinDeviceRepository) TouchLastUsed(ctx context.Context, userUUID, deviceID string, when time.Time) error {
	//tenantscope:allow PIN devices are keyed on the user, like the user rows they belong to
	_, err := r.coll.UpdateOne(ctx,
		bson.M{"userUuid": userUUID, "deviceId": deviceID},
		bson.M{"$set": bson.M{"lastUsedAt": when}})
	return err
}

func (r *pinDeviceRepository) Delete(ctx context.Context, userUUID, deviceID string) error {
	//tenantscope:allow PIN devices are keyed on the user, like the user rows they belong to
	res, err := r.coll.DeleteOne(ctx, bson.M{"userUuid": userUUID, "deviceId": deviceID})
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return ErrPINDeviceNotFound
	}
	return nil
}

func (r *pinDeviceRepository) DeleteAllByUser(ctx context.Context, userUUID string) (int64, error) {
	//tenantscope:allow PIN devices are keyed on the user, like the user rows they belong to
	res, err := r.coll.DeleteMany(ctx, bson.M{"userUuid": userUUID})
	if err != nil {
		return 0, err
	}
	return res.DeletedCount, nil
}
