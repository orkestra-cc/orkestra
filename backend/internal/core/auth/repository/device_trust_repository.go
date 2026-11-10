// Package repository — device trust repository.
//
// Section C item #3 of the 2026-04-24 auth roadmap. Stores the
// "remember this device for 30 days" grants that let a returning user
// skip the MFA prompt on login when the same device + fingerprint
// returns within the trust window.
package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/orkestra/backend/internal/core/auth/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ErrDeviceTrustNotFound is returned when the caller asks for a trust
// row that doesn't exist or has been revoked. Not a hard error — most
// callers degrade to "device not trusted" and fall through to the
// standard MFA prompt.
var ErrDeviceTrustNotFound = errors.New("device trust: not found")

// DeviceTrustRepository handles the auth_device_trust collection.
type DeviceTrustRepository interface {
	// Upsert atomically replaces any existing active row for
	// (userUUID, deviceID) with the new grant — a fresh trust
	// supersedes the prior one. The replaced row, if any, is marked
	// revoked with reason "superseded_by_new_grant" so the history is
	// walkable on audit queries.
	Upsert(ctx context.Context, grant *models.DeviceTrustDoc) error

	// GetActive returns the single active, non-expired grant for
	// (userUUID, deviceID) — or (nil, nil) when no such row exists.
	// Callers that need to verify the device is still trusted should
	// also cross-check the fingerprint on the returned doc.
	GetActive(ctx context.Context, userUUID, deviceID string) (*models.DeviceTrustDoc, error)

	// ListActiveByUser returns every active, non-expired grant for the
	// user. Sorted by lastUsedAt desc so the most recently-used device
	// surfaces first in the UI.
	ListActiveByUser(ctx context.Context, userUUID string) ([]*models.DeviceTrustDoc, error)

	// MarkUsed bumps lastUsedAt on a trust row (called each time a
	// trusted-device login bypasses MFA). Best-effort — caller logs
	// on error and continues.
	MarkUsed(ctx context.Context, userUUID, deviceID string) error

	// RevokeByDevice flips isActive to false on the matching active row
	// with the supplied reason. Returns ErrDeviceTrustNotFound when no
	// active row exists.
	RevokeByDevice(ctx context.Context, userUUID, deviceID, reason string) error

	// RevokeAllByUser revokes every active grant for the user. Used on
	// password change, factor remove, admin MFA reset, and the user-
	// initiated "log out of all trusted devices" flow.
	RevokeAllByUser(ctx context.Context, userUUID, reason string) (int64, error)

	// DeleteAllByUser hard-deletes every grant for the user. Used by
	// the GDPR DSR right-to-erasure pipeline — trust rows carry PII
	// (fingerprint, IP, deviceName) tied to userUUID.
	DeleteAllByUser(ctx context.Context, userUUID string) (int64, error)
}

type deviceTrustRepository struct {
	collection *mongo.Collection
}

// NewDeviceTrustRepository builds the repository.
func NewDeviceTrustRepository(db *mongo.Database) DeviceTrustRepository {
	return &deviceTrustRepository{
		collection: db.Collection(models.DeviceTrustCollection),
	}
}

func (r *deviceTrustRepository) Upsert(ctx context.Context, grant *models.DeviceTrustDoc) error {
	if grant == nil {
		return errors.New("device trust: grant is nil")
	}
	if grant.UserUUID == "" || grant.DeviceID == "" {
		return errors.New("device trust: userUUID and deviceID are required")
	}
	now := time.Now()
	if grant.CreatedAt.IsZero() {
		grant.CreatedAt = now
	}
	grant.UpdatedAt = now
	grant.IsActive = true

	// Mark any existing active row as superseded before inserting the
	// replacement. Two-step rather than a single atomic replace because
	// we want the history walkable (the superseded row survives with
	// its revokedAt stamped).
	_, _ = r.collection.UpdateMany(ctx,
		bson.M{
			"userUuid": grant.UserUUID,
			"deviceId": grant.DeviceID,
			"isActive": true,
		},
		bson.M{"$set": bson.M{
			"isActive":      false,
			"revokedAt":     now,
			"revokedReason": models.DeviceTrustRevokedReplaced,
			"updatedAt":     now,
		}},
	)

	_, err := r.collection.InsertOne(ctx, grant)
	if err != nil {
		return fmt.Errorf("device trust: insert: %w", err)
	}
	return nil
}

func (r *deviceTrustRepository) GetActive(ctx context.Context, userUUID, deviceID string) (*models.DeviceTrustDoc, error) {
	if userUUID == "" || deviceID == "" {
		return nil, ErrDeviceTrustNotFound
	}
	filter := bson.M{
		"userUuid":     userUUID,
		"deviceId":     deviceID,
		"isActive":     true,
		"trustedUntil": bson.M{"$gt": time.Now()},
	}
	var doc models.DeviceTrustDoc
	if err := r.collection.FindOne(ctx, filter).Decode(&doc); err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, ErrDeviceTrustNotFound
		}
		return nil, fmt.Errorf("device trust: find: %w", err)
	}
	return &doc, nil
}

func (r *deviceTrustRepository) ListActiveByUser(ctx context.Context, userUUID string) ([]*models.DeviceTrustDoc, error) {
	if userUUID == "" {
		return nil, nil
	}
	filter := bson.M{
		"userUuid":     userUUID,
		"isActive":     true,
		"trustedUntil": bson.M{"$gt": time.Now()},
	}
	opts := options.Find().SetSort(bson.D{{Key: "lastUsedAt", Value: -1}, {Key: "trustedAt", Value: -1}})
	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("device trust: list: %w", err)
	}
	defer cursor.Close(ctx)
	var out []*models.DeviceTrustDoc
	for cursor.Next(ctx) {
		var doc models.DeviceTrustDoc
		if err := cursor.Decode(&doc); err != nil {
			return nil, fmt.Errorf("device trust: decode: %w", err)
		}
		out = append(out, &doc)
	}
	return out, nil
}

func (r *deviceTrustRepository) MarkUsed(ctx context.Context, userUUID, deviceID string) error {
	if userUUID == "" || deviceID == "" {
		return nil
	}
	now := time.Now()
	_, err := r.collection.UpdateOne(ctx,
		bson.M{
			"userUuid": userUUID,
			"deviceId": deviceID,
			"isActive": true,
		},
		bson.M{"$set": bson.M{"lastUsedAt": now, "updatedAt": now}},
	)
	if err != nil {
		return fmt.Errorf("device trust: mark used: %w", err)
	}
	return nil
}

func (r *deviceTrustRepository) RevokeByDevice(ctx context.Context, userUUID, deviceID, reason string) error {
	if userUUID == "" || deviceID == "" {
		return ErrDeviceTrustNotFound
	}
	if reason == "" {
		reason = models.DeviceTrustRevokedByUser
	}
	now := time.Now()
	res, err := r.collection.UpdateOne(ctx,
		bson.M{
			"userUuid": userUUID,
			"deviceId": deviceID,
			"isActive": true,
		},
		bson.M{"$set": bson.M{
			"isActive":      false,
			"revokedAt":     now,
			"revokedReason": reason,
			"updatedAt":     now,
		}},
	)
	if err != nil {
		return fmt.Errorf("device trust: revoke by device: %w", err)
	}
	if res.MatchedCount == 0 {
		return ErrDeviceTrustNotFound
	}
	return nil
}

func (r *deviceTrustRepository) RevokeAllByUser(ctx context.Context, userUUID, reason string) (int64, error) {
	if userUUID == "" {
		return 0, nil
	}
	if reason == "" {
		reason = models.DeviceTrustRevokedByUser
	}
	now := time.Now()
	res, err := r.collection.UpdateMany(ctx,
		bson.M{"userUuid": userUUID, "isActive": true},
		bson.M{"$set": bson.M{
			"isActive":      false,
			"revokedAt":     now,
			"revokedReason": reason,
			"updatedAt":     now,
		}},
	)
	if err != nil {
		return 0, fmt.Errorf("device trust: revoke all by user: %w", err)
	}
	return res.ModifiedCount, nil
}

func (r *deviceTrustRepository) DeleteAllByUser(ctx context.Context, userUUID string) (int64, error) {
	if userUUID == "" {
		return 0, nil
	}
	res, err := r.collection.DeleteMany(ctx, ByUserFilter(userUUID))
	if err != nil {
		return 0, fmt.Errorf("device trust: delete all by user: %w", err)
	}
	return res.DeletedCount, nil
}
