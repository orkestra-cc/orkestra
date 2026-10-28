package repository

import (
	"context"
	"errors"
	"time"

	"github.com/orkestra/backend/internal/core/retrieval/models"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/tenantrepo"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ErrLeaseLost: the key is no longer leased by the caller at that epoch (its
// lease expired and another worker claimed it); nothing was written.
var ErrLeaseLost = errors.New("retrieval repository: lease lost")

// Outbox owns retrieval_outbox: one row per (tenant, corpus, source) key.
// Owners Enqueue inside their own transaction; a lease-fenced worker claims
// and releases keys. Epoch increments on every Claim and fences Release,
// Reschedule and HoldsLease.
type Outbox struct{ coll *mongo.Collection }

func NewOutbox(db *mongo.Database) *Outbox { return &Outbox{coll: db.Collection(CollOutbox)} }

func ms(t time.Time) time.Time { return t.UTC().Truncate(time.Millisecond) }

// claimable: corpus in corpora, due, and either dirty and unleased or with an
// expired lease (recovers a holder that died).
func claimable(now time.Time, corpora []string) bson.M {
	return bson.M{
		"corpus":        bson.M{"$in": corpora},
		"nextAttemptAt": bson.M{"$lte": ms(now)},
		"$or": []bson.M{
			{"dirty": true, "leaseUntil": nil},
			{"leaseUntil": bson.M{"$lt": ms(now)}},
		},
	}
}

func setOnInsert(now time.Time) bson.M {
	return bson.M{"epoch": int64(0), "attempts": 0, "nextAttemptAt": ms(now), "createdAt": ms(now)}
}

// Enqueue records intents for the tenant in ctx, one upsert per intent. ctx is
// passed through unchanged: a mongo.SessionContext makes the writes part of
// the caller's transaction. A key in backoff keeps its nextAttemptAt.
func (r *Outbox) Enqueue(ctx context.Context, intents []iface.RetrievalIntent, now time.Time) error {
	for _, in := range intents {
		if err := r.upsertDirty(ctx, in.Corpus, in.SourceID, &in.Revision, now); err != nil {
			return err
		}
	}
	return nil
}

// Requeue marks a key dirty (creating it if missing) without touching lease
// fields or the revision.
func (r *Outbox) Requeue(ctx context.Context, corpus, sourceID string, now time.Time) error {
	return r.upsertDirty(ctx, corpus, sourceID, nil, now)
}

func (r *Outbox) upsertDirty(ctx context.Context, corpus, sourceID string, revision *int64, now time.Time) error {
	// The upsert inherits tenantId from the scoped filter's equality match.
	filter, err := tenantrepo.Scope(ctx, bson.M{"corpus": corpus, "sourceId": sourceID})
	if err != nil {
		return err
	}
	update := bson.M{
		"$set":         bson.M{"dirty": true, "updatedAt": ms(now)},
		"$setOnInsert": setOnInsert(now),
	}
	if revision != nil {
		update["$max"] = bson.M{"revision": *revision}
	} else {
		update["$setOnInsert"].(bson.M)["revision"] = int64(0)
	}
	_, err = r.coll.UpdateOne(ctx, filter, update, options.Update().SetUpsert(true))
	return err
}

// DueTenants lists up to limit tenants with a claimable key whose tenant ID
// sorts strictly after after ("" starts from the first), in tenant ID order.
// The worker walks it as a round-robin cursor, so a tenant with an old
// backlog cannot win every pass. limit <= 0 returns nil without a query.
func (r *Outbox) DueTenants(ctx context.Context, now time.Time, corpora []string, after string, limit int) ([]string, error) {
	if limit <= 0 {
		return nil, nil
	}
	match := claimable(now, corpora)
	if after != "" {
		match["tenantId"] = bson.M{"$gt": after}
	}
	pipeline := []bson.M{
		{"$match": match},
		{"$group": bson.M{"_id": "$tenantId"}},
		{"$sort": bson.M{"_id": 1}},
		{"$limit": int64(limit)},
	}
	//tenantscope:allow system: the index worker finds tenants with due keys; each key is then claimed and processed under its own tenant
	cur, err := r.coll.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []string
	for cur.Next(ctx) {
		var doc struct {
			ID string `bson:"_id"`
		}
		if err := cur.Decode(&doc); err != nil {
			return nil, err
		}
		out = append(out, doc.ID)
	}
	return out, cur.Err()
}

// Claim leases one claimable key of the tenant in ctx, or returns nil.
func (r *Outbox) Claim(ctx context.Context, now time.Time, corpora []string, owner string, lease time.Duration) (*models.OutboxEntry, error) {
	filter, err := tenantrepo.Scope(ctx, claimable(now, corpora))
	if err != nil {
		return nil, err
	}
	update := bson.M{
		"$inc": bson.M{"epoch": 1},
		"$set": bson.M{"dirty": false, "leaseOwner": owner, "leaseUntil": ms(now.Add(lease)), "updatedAt": ms(now)},
	}
	var e models.OutboxEntry
	err = r.coll.FindOneAndUpdate(ctx, filter, update,
		options.FindOneAndUpdate().SetSort(bson.D{{Key: "nextAttemptAt", Value: 1}}).SetReturnDocument(options.After)).Decode(&e)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// HoldsLease reports whether owner still holds the lease at epoch.
func (r *Outbox) HoldsLease(ctx context.Context, corpus, sourceID string, epoch int64, owner string, now time.Time) (bool, error) {
	filter, err := tenantrepo.Scope(ctx, bson.M{
		"corpus": corpus, "sourceId": sourceID, "epoch": epoch, "leaseOwner": owner,
		"leaseUntil": bson.M{"$gt": ms(now)},
	})
	if err != nil {
		return false, err
	}
	n, err := r.coll.CountDocuments(ctx, filter, options.Count().SetLimit(1))
	return n == 1, err
}

// Release ends the lease and records the applied state (cleared when nil).
// dirty is untouched, so an intent enqueued during the lease survives.
func (r *Outbox) Release(ctx context.Context, corpus, sourceID string, epoch int64, owner string, applied *models.AppliedState, now time.Time) error {
	set := bson.M{"attempts": 0, "updatedAt": ms(now)}
	unset := bson.M{"leaseOwner": "", "leaseUntil": "", "lastError": ""}
	if applied != nil {
		set["applied"] = applied
	} else {
		unset["applied"] = ""
	}
	return r.fenced(ctx, corpus, sourceID, epoch, owner, bson.M{"$set": set, "$unset": unset})
}

// Reschedule ends the lease after a failure: dirty, with a backoff.
func (r *Outbox) Reschedule(ctx context.Context, corpus, sourceID string, epoch int64, owner string, attempts int, next time.Time, code string) error {
	return r.fenced(ctx, corpus, sourceID, epoch, owner, bson.M{
		"$set":   bson.M{"dirty": true, "attempts": attempts, "nextAttemptAt": ms(next), "lastError": code, "updatedAt": ms(time.Now())},
		"$unset": bson.M{"leaseOwner": "", "leaseUntil": ""},
	})
}

func (r *Outbox) fenced(ctx context.Context, corpus, sourceID string, epoch int64, owner string, update bson.M) error {
	filter, err := tenantrepo.Scope(ctx, bson.M{"corpus": corpus, "sourceId": sourceID, "epoch": epoch, "leaseOwner": owner})
	if err != nil {
		return err
	}
	res, err := r.coll.UpdateOne(ctx, filter, update)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrLeaseLost
	}
	return nil
}
