// Package repository — security-event repository (Phase 2.1 of the
// core-completion epic).
//
// Owns the auth_security_events collection: an append-only audit log of
// security-relevant actions performed by or against a user account
// (admin OAuth unlink, self password reconfirm, session revoke, …).
// Rows are indexed by userUuid for the per-user activity timeline and
// by timestamp for retention queries.
package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/orkestra/backend/internal/core/auth/models"
)

// SecurityEventRepository persists and queries SecurityEvent rows.
type SecurityEventRepository interface {
	// Insert appends a new event. The repo stamps a UUID into ID when
	// the caller leaves it empty and fills Timestamp when zero, so
	// callers can omit boilerplate. Insert never overwrites — every
	// call is one row in the audit log.
	Insert(ctx context.Context, event *models.SecurityEvent) error

	// ListByUser returns the most-recent events for the user, sorted by
	// Timestamp desc and capped at limit. limit<=0 falls back to a
	// sensible default (100). Returns an empty slice when the user has
	// no events.
	ListByUser(ctx context.Context, userUUID string, limit int) ([]*models.SecurityEvent, error)

	// ListByUserPaged is the paginated variant the admin endpoint uses.
	// Returns (events, total). offset and limit are clamped to safe
	// values; passing a negative limit returns no rows. The total
	// count is the total matching the filter, not the page size — so
	// the UI can render "Showing 20 of 1,243".
	ListByUserPaged(ctx context.Context, userUUID string, offset, limit int, since *time.Time) (events []*models.SecurityEvent, total int64, err error)

	// DeleteAllByUser hard-deletes every event for the user. Used by
	// the GDPR DSR right-to-erasure pipeline — security events carry
	// PII (IP, UA, deviceID) tied to userUUID.
	DeleteAllByUser(ctx context.Context, userUUID string) (int64, error)
}

type securityEventRepository struct {
	collection *mongo.Collection
}

// NewSecurityEventRepository builds the repository.
func NewSecurityEventRepository(db *mongo.Database) SecurityEventRepository {
	return &securityEventRepository{
		collection: db.Collection(models.SecurityEventsCollection),
	}
}

const defaultSecurityEventLimit = 100

// maxSecurityEventLimit is the hard ceiling the admin endpoint enforces.
// A pathological query asking for 100k rows would page-fault every
// caller; the SPA paginates so the cap is a guardrail, not a feature.
const maxSecurityEventLimit = 500

func (r *securityEventRepository) Insert(ctx context.Context, event *models.SecurityEvent) error {
	if event == nil {
		return errors.New("security event: event is nil")
	}
	if event.UserUUID == "" {
		return errors.New("security event: userUUID is required")
	}
	if event.EventType == "" {
		return errors.New("security event: eventType is required")
	}
	if event.ID == "" {
		event.ID = uuid.NewString()
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	_, err := r.collection.InsertOne(ctx, event)
	return err
}

func (r *securityEventRepository) ListByUser(ctx context.Context, userUUID string, limit int) ([]*models.SecurityEvent, error) {
	if userUUID == "" {
		return nil, errors.New("security event: userUUID is required")
	}
	if limit <= 0 {
		limit = defaultSecurityEventLimit
	}
	if limit > maxSecurityEventLimit {
		limit = maxSecurityEventLimit
	}

	opts := options.Find().
		SetSort(bson.D{{Key: "timestamp", Value: -1}}).
		SetLimit(int64(limit))
	//tenantscope:allow auth_security_events is a non-tier-split audit log keyed on userUUID alone — see backend/internal/core/auth/AGENTS.md ("MongoDB collections" table marks auth_security_events as the single non-tier-split row).
	cursor, err := r.collection.Find(ctx, bson.M{"userUuid": userUUID}, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	out := make([]*models.SecurityEvent, 0, limit)
	for cursor.Next(ctx) {
		var doc models.SecurityEvent
		if err := cursor.Decode(&doc); err != nil {
			return nil, err
		}
		out = append(out, &doc)
	}
	return out, cursor.Err()
}

func (r *securityEventRepository) ListByUserPaged(ctx context.Context, userUUID string, offset, limit int, since *time.Time) ([]*models.SecurityEvent, int64, error) {
	if userUUID == "" {
		return nil, 0, errors.New("security event: userUUID is required")
	}
	if limit < 0 {
		return nil, 0, nil
	}
	if limit == 0 {
		limit = defaultSecurityEventLimit
	}
	if limit > maxSecurityEventLimit {
		limit = maxSecurityEventLimit
	}
	if offset < 0 {
		offset = 0
	}

	filter := bson.M{"userUuid": userUUID}
	if since != nil && !since.IsZero() {
		filter["timestamp"] = bson.M{"$gte": *since}
	}

	//tenantscope:allow auth_security_events is a non-tier-split audit log keyed on userUUID alone — pagination count for the admin per-user timeline.
	total, err := r.collection.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return []*models.SecurityEvent{}, 0, nil
	}

	opts := options.Find().
		SetSort(bson.D{{Key: "timestamp", Value: -1}}).
		SetSkip(int64(offset)).
		SetLimit(int64(limit))
	//tenantscope:allow auth_security_events is a non-tier-split audit log keyed on userUUID alone — see backend/internal/core/auth/AGENTS.md.
	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, total, err
	}
	defer cursor.Close(ctx)

	out := make([]*models.SecurityEvent, 0, limit)
	for cursor.Next(ctx) {
		var doc models.SecurityEvent
		if err := cursor.Decode(&doc); err != nil {
			return nil, total, err
		}
		out = append(out, &doc)
	}
	return out, total, cursor.Err()
}

func (r *securityEventRepository) DeleteAllByUser(ctx context.Context, userUUID string) (int64, error) {
	if userUUID == "" {
		return 0, errors.New("security event: userUUID is required")
	}
	//tenantscope:allow auth_security_events is a non-tier-split audit log keyed on userUUID alone — used by the GDPR DSR right-to-erasure pipeline; deletion follows the user record across all tenants they ever held a binding in.
	res, err := r.collection.DeleteMany(ctx, bson.M{"userUuid": userUUID})
	if err != nil {
		return 0, err
	}
	return res.DeletedCount, nil
}
