package repository

import (
	"context"
	"errors"
	"time"

	"github.com/orkestra/backend/internal/core/tenant/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// CollEntitlements is the projection of active and historical capability
// entitlements held by tenants. See models/entitlement.go for the row shape.
const CollEntitlements = "tenant_entitlements"

// CreateEntitlement inserts a new entitlement row. The caller stamps UUID and
// GrantedAt; the repository does not touch those so replays (event-source
// replay) can land with the original timestamps.
func (r *Repository) CreateEntitlement(ctx context.Context, e *models.Entitlement) error {
	if e.GrantedAt.IsZero() {
		e.GrantedAt = time.Now()
	}
	_, err := r.db.Collection(CollEntitlements).InsertOne(ctx, e)
	return err
}

// GetActiveEntitlement returns the active (non-revoked, non-expired)
// entitlement of a tenant for a specific capability. Returns ErrNotFound if
// none exists.
func (r *Repository) GetActiveEntitlement(ctx context.Context, tenantUUID, capabilityID string) (*models.Entitlement, error) {
	now := time.Now()
	filter := bson.M{
		"tenantUUID":   tenantUUID,
		"capabilityId": capabilityID,
		"revokedAt":    nil,
		"$or": []bson.M{
			{"expiresAt": nil},
			{"expiresAt": bson.M{"$gt": now}},
		},
	}
	var e models.Entitlement
	err := r.db.Collection(CollEntitlements).FindOne(ctx, filter).Decode(&e)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// HasActiveEntitlement reports whether the tenant currently has any active
// entitlement to the capability. A read-optimized shortcut over
// GetActiveEntitlement that avoids decoding the full row.
func (r *Repository) HasActiveEntitlement(ctx context.Context, tenantUUID, capabilityID string) (bool, error) {
	now := time.Now()
	filter := bson.M{
		"tenantUUID":   tenantUUID,
		"capabilityId": capabilityID,
		"revokedAt":    nil,
		"$or": []bson.M{
			{"expiresAt": nil},
			{"expiresAt": bson.M{"$gt": now}},
		},
	}
	n, err := r.db.Collection(CollEntitlements).CountDocuments(ctx, filter, options.Count().SetLimit(1))
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// ListActiveByTenant returns every active entitlement held by a tenant,
// sorted by capability ID for deterministic output.
func (r *Repository) ListActiveByTenant(ctx context.Context, tenantUUID string) ([]models.Entitlement, error) {
	now := time.Now()
	filter := bson.M{
		"tenantUUID": tenantUUID,
		"revokedAt":  nil,
		"$or": []bson.M{
			{"expiresAt": nil},
			{"expiresAt": bson.M{"$gt": now}},
		},
	}
	cur, err := r.db.Collection(CollEntitlements).Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "capabilityId", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []models.Entitlement
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// RevokeActiveEntitlement marks the active entitlement for the
// (tenantUUID, capabilityID) pair as revoked. Idempotent: returns
// ErrNotFound if no active row exists. Never mutates already-revoked rows.
func (r *Repository) RevokeActiveEntitlement(ctx context.Context, tenantUUID, capabilityID string, at time.Time) error {
	filter := bson.M{
		"tenantUUID":   tenantUUID,
		"capabilityId": capabilityID,
		"revokedAt":    nil,
	}
	res, err := r.db.Collection(CollEntitlements).UpdateOne(ctx, filter, bson.M{"$set": bson.M{"revokedAt": at}})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrNotFound
	}
	return nil
}
