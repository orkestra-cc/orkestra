package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra/backend/internal/core/retrieval/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ErrErasureClaimLost: the row no longer carries the claim the caller took,
// because another sweep re-claimed it after the hold expired.
var ErrErasureClaimLost = errors.New("retrieval: erasure claim lost")

// tenantTombstoneChunk bounds the $in list of TenantTombstoneIDs.
const tenantTombstoneChunk = 500

// ErasureFinalization owns the finalization state of retrieval_erasures:
// idempotent creation of the user/subject/tenant tombstones, and the claim and
// transitions the finalizer runs (ADR-0023 D12). A claim pushes nextAttemptAt
// to now+hold and is identified by that value; every transition is fenced on it.
type ErasureFinalization struct{ coll *mongo.Collection }

func NewErasureFinalization(db *mongo.Database) *ErasureFinalization {
	return &ErasureFinalization{coll: db.Collection(CollErasures)}
}

// ensure upserts the row for key, inserting a pending row due at now; created
// is true only for the call that inserted it.
func (r *ErasureFinalization) ensure(ctx context.Context, key bson.M, extra bson.M, now time.Time) (*models.Erasure, bool, error) {
	at := ms(now)
	id := uuid.NewString()
	set := bson.M{
		"uuid":          id,
		"status":        models.ErasureStatusPending,
		"attempts":      0,
		"nextAttemptAt": at,
		"createdAt":     at,
	}
	for k, v := range extra {
		set[k] = v
	}
	var e models.Erasure
	//tenantscope:allow system: tombstone upsert keyed by an explicit natural key supplied by EnsureUser, EnsureSubject and EnsureTenant (ADR-0023 D12)
	err := r.coll.FindOneAndUpdate(ctx, key, bson.M{"$setOnInsert": set},
		options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After)).Decode(&e)
	if err != nil {
		return nil, false, err
	}
	return &e, e.UUID == id, nil
}

// EnsureUser records the global tombstone of a data subject.
func (r *ErasureFinalization) EnsureUser(ctx context.Context, userUUID string, now time.Time) (*models.Erasure, bool, error) {
	subject := userSubjectPrefix + userUUID
	//tenantscope:allow dsr: global user tombstone of a data-subject erasure (ADR-0023 D12)
	return r.ensure(ctx,
		bson.M{"scope": models.ErasureScopeGlobal, "kind": models.ErasureKindUser, "subject": subject},
		bson.M{}, now)
}

// EnsureSubject records the tombstone of a subject in an explicit tenant.
func (r *ErasureFinalization) EnsureSubject(ctx context.Context, tenantID, subject string, now time.Time) (*models.Erasure, bool, error) {
	//tenantscope:allow dsr: owner-module subject erasure for an explicit tenant
	return r.ensure(ctx,
		bson.M{"scope": models.ErasureScopeTenant, "tenantId": tenantID, "kind": models.ErasureKindSubject, "subject": subject},
		bson.M{}, now)
}

// EnsureTenant records the purge tombstone of an explicit tenant.
func (r *ErasureFinalization) EnsureTenant(ctx context.Context, tenantID string, now time.Time) (*models.Erasure, bool, error) {
	//tenantscope:allow system: tenant purge tombstone for an explicit tenant
	return r.ensure(ctx,
		bson.M{"scope": models.ErasureScopeTenant, "tenantId": tenantID, "kind": models.ErasureKindTenant},
		bson.M{}, now)
}

// ListUser returns the global tombstones of a data subject (normally one).
func (r *ErasureFinalization) ListUser(ctx context.Context, userUUID string) ([]models.Erasure, error) {
	//tenantscope:allow dsr: global user tombstone of a data-subject erasure (ADR-0023 D12)
	cur, err := r.coll.Find(ctx, bson.M{"scope": models.ErasureScopeGlobal, "kind": models.ErasureKindUser, "subject": userSubjectPrefix + userUUID})
	if err != nil {
		return nil, err
	}
	var out []models.Erasure
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Get returns the row, or nil, nil when absent.
func (r *ErasureFinalization) Get(ctx context.Context, uuid string) (*models.Erasure, error) {
	var e models.Erasure
	//tenantscope:allow system: erasure finalization sweep across tenants and global tombstones
	err := r.coll.FindOne(ctx, bson.M{"uuid": uuid}).Decode(&e)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// dueFilter selects rows a sweep may claim: legacy rows without a status,
// pending or fencing rows whose nextAttemptAt has passed, and pending rows
// that never got one.
func dueFilter(now time.Time) bson.M {
	return bson.M{"$or": []bson.M{
		{"status": bson.M{"$exists": false}},
		{"status": bson.M{"$in": []string{models.ErasureStatusPending, models.ErasureStatusFencing}}, "nextAttemptAt": bson.M{"$lte": ms(now)}},
		{"status": models.ErasureStatusPending, "nextAttemptAt": bson.M{"$exists": false}},
	}}
}

func (r *ErasureFinalization) claim(ctx context.Context, filter bson.M, now time.Time, hold time.Duration) (*models.Erasure, error) {
	update := bson.A{bson.M{"$set": bson.M{
		"status":        bson.M{"$ifNull": bson.A{"$status", models.ErasureStatusPending}},
		"attempts":      bson.M{"$ifNull": bson.A{"$attempts", 0}},
		"nextAttemptAt": ms(now.Add(hold)),
	}}}
	var e models.Erasure
	//tenantscope:allow system: erasure finalization sweep across tenants and global tombstones
	err := r.coll.FindOneAndUpdate(ctx, filter, update,
		options.FindOneAndUpdate().SetSort(bson.D{{Key: "nextAttemptAt", Value: 1}}).SetReturnDocument(options.After)).Decode(&e)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// ClaimDue claims the most overdue row for hold, or returns nil, nil.
func (r *ErasureFinalization) ClaimDue(ctx context.Context, now time.Time, hold time.Duration) (*models.Erasure, error) {
	return r.claim(ctx, dueFilter(now), now, hold)
}

// ClaimOne claims one row when it is due, or returns nil, nil.
func (r *ErasureFinalization) ClaimOne(ctx context.Context, uuid string, now time.Time, hold time.Duration) (*models.Erasure, error) {
	return r.claim(ctx, bson.M{"$and": []bson.M{{"uuid": uuid}, dueFilter(now)}}, now, hold)
}

func (r *ErasureFinalization) transition(ctx context.Context, uuid string, claim time.Time, update any) error {
	//tenantscope:allow system: erasure finalization sweep across tenants and global tombstones
	res, err := r.coll.UpdateOne(ctx, bson.M{"uuid": uuid, "nextAttemptAt": ms(claim)}, update)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrErasureClaimLost
	}
	return nil
}

// MarkFencing records the first engine pass and the fence the second must
// wait out; the row is next due at fenceUntil.
func (r *ErasureFinalization) MarkFencing(ctx context.Context, uuid string, claim, firstPassAt, fenceUntil time.Time) error {
	return r.transition(ctx, uuid, claim, bson.M{"$set": bson.M{
		"status":        models.ErasureStatusFencing,
		"firstPassAt":   ms(firstPassAt),
		"fenceUntil":    ms(fenceUntil),
		"nextAttemptAt": ms(fenceUntil),
	}})
}

// MarkDone closes the row; a row never fenced gets firstPassAt and fenceUntil
// equal to at. The tombstone keeps covering after done.
func (r *ErasureFinalization) MarkDone(ctx context.Context, uuid string, claim, at time.Time) error {
	a := ms(at)
	return r.transition(ctx, uuid, claim, bson.A{
		bson.M{"$set": bson.M{
			"status":      models.ErasureStatusDone,
			"doneAt":      a,
			"firstPassAt": bson.M{"$ifNull": bson.A{"$firstPassAt", a}},
			"fenceUntil":  bson.M{"$ifNull": bson.A{"$fenceUntil", a}},
		}},
		bson.M{"$unset": "nextAttemptAt"},
	})
}

// MarkRetry counts a failed attempt and schedules the next one.
func (r *ErasureFinalization) MarkRetry(ctx context.Context, uuid string, claim, next time.Time) error {
	return r.transition(ctx, uuid, claim, bson.A{bson.M{"$set": bson.M{
		"nextAttemptAt": ms(next),
		"attempts":      bson.M{"$add": bson.A{bson.M{"$ifNull": bson.A{"$attempts", 0}}, 1}},
	}}})
}

// TenantTombstoneIDs returns, among tenantIDs, those that already have a
// tenant tombstone.
func (r *ErasureFinalization) TenantTombstoneIDs(ctx context.Context, tenantIDs []string) (map[string]bool, error) {
	out := make(map[string]bool)
	for start := 0; start < len(tenantIDs); start += tenantTombstoneChunk {
		end := min(start+tenantTombstoneChunk, len(tenantIDs))
		//tenantscope:allow system: tenant-purge reconciliation reads the tenant tombstones of an explicit list of purged tenants (ADR-0023 D12)
		cur, err := r.coll.Find(ctx,
			bson.M{"scope": models.ErasureScopeTenant, "kind": models.ErasureKindTenant, "tenantId": bson.M{"$in": tenantIDs[start:end]}},
			options.Find().SetProjection(bson.M{"tenantId": 1}))
		if err != nil {
			return nil, err
		}
		var rows []models.Erasure
		if err := cur.All(ctx, &rows); err != nil {
			return nil, err
		}
		for _, e := range rows {
			out[e.TenantID] = true
		}
	}
	return out, nil
}
