// Package repository provides MongoDB access for the compliance module's
// audit-events collection. Writes are append-only — there is no Update or
// Delete surface by design.
package repository

import (
	"context"
	"time"

	"github.com/orkestra/backend/internal/addons/compliance/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// AuditEventRepository is the persistence boundary for audit events.
type AuditEventRepository struct {
	coll *mongo.Collection
}

// New returns a repository bound to the audit_events collection on db.
func New(db *mongo.Database) *AuditEventRepository {
	return &AuditEventRepository{coll: db.Collection(models.AuditEventsCollection)}
}

// Insert appends one event. The caller must have stamped UUID and Timestamp
// — this method does not patch them so the sink can guarantee the event's
// ordering identity without racing the clock.
//
//tenantscope:allow audit events are written with whatever tenantId the
// emitter supplies (or empty for platform-level events); the collection is
// explicitly cross-tenant and exempt from tenantrepo.StampInsert.
func (r *AuditEventRepository) Insert(ctx context.Context, e *models.AuditEvent) error {
	_, err := r.coll.InsertOne(ctx, e)
	return err
}

// Filter describes the read surface exposed to the admin handler. All
// fields are optional — empty Filter{} returns the most recent events
// across the platform.
type Filter struct {
	TenantID     string
	ActorUserID  string
	Action       string
	ActionPrefix string // e.g. "auth." — matches auth.login.*, auth.logout, etc.
	ResourceType string
	ResourceID   string
	Outcome      string
	Since        time.Time
	Until        time.Time
	Limit        int
	Skip         int
}

// List returns events matching filter, newest-first. The caller is expected
// to enforce RBAC before invoking — the repository itself does not gate
// visibility.
//
//tenantscope:allow audit admin read spans tenants by design — platform
// admins filter by tenantId via the optional Filter.TenantID field.
func (r *AuditEventRepository) List(ctx context.Context, f Filter) ([]models.AuditEvent, int64, error) {
	query := bson.M{}
	if f.TenantID != "" {
		query["tenantId"] = f.TenantID
	}
	if f.ActorUserID != "" {
		query["actorUserId"] = f.ActorUserID
	}
	if f.Action != "" {
		query["action"] = f.Action
	} else if f.ActionPrefix != "" {
		query["action"] = bson.M{"$regex": "^" + f.ActionPrefix}
	}
	if f.ResourceType != "" {
		query["resourceType"] = f.ResourceType
	}
	if f.ResourceID != "" {
		query["resourceId"] = f.ResourceID
	}
	if f.Outcome != "" {
		query["outcome"] = f.Outcome
	}
	if !f.Since.IsZero() || !f.Until.IsZero() {
		tsRange := bson.M{}
		if !f.Since.IsZero() {
			tsRange["$gte"] = f.Since
		}
		if !f.Until.IsZero() {
			tsRange["$lte"] = f.Until
		}
		query["timestamp"] = tsRange
	}

	//tenantscope:allow audit trail read spans tenants by design — admin RBAC gates visibility
	total, err := r.coll.CountDocuments(ctx, query)
	if err != nil {
		return nil, 0, err
	}

	limit := int64(f.Limit)
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	opts := options.Find().
		SetSort(bson.D{{Key: "timestamp", Value: -1}}).
		SetLimit(limit).
		SetSkip(int64(f.Skip))

	//tenantscope:allow audit trail read spans tenants by design — admin RBAC gates visibility
	cur, err := r.coll.Find(ctx, query, opts)
	if err != nil {
		return nil, 0, err
	}
	defer cur.Close(ctx)

	out := make([]models.AuditEvent, 0, limit)
	if err := cur.All(ctx, &out); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}
