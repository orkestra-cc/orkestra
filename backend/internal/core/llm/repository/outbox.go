package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra/backend/internal/core/llm/models"
	"github.com/orkestra/backend/pkg/sdk/tenantrepo"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const CollBudgetOutbox = "llm_budget_outbox"

// ErrLeaseLost: the outbox event is no longer leased by the caller (its
// lease expired and another worker took it over, or it was already
// settled); nothing was written.
var ErrLeaseLost = errors.New("llm repository: outbox lease lost")

func newUUID() string { return uuid.NewString() }

// Outbox owns llm_budget_outbox: budget threshold notifications, one per
// (org, period, threshold), delivered at-least-once by the outbox worker
// under a recoverable lease.
type Outbox struct{ coll *mongo.Collection }

func NewOutbox(db *mongo.Database) *Outbox { return &Outbox{coll: db.Collection(CollBudgetOutbox)} }

// InsertUnique inserts ev under the caller's org unless the org already
// has an event for (period, threshold); it reports whether it inserted.
// The existence check runs first because, inside a transaction, a
// duplicate-key error aborts the whole transaction; an event committed
// concurrently surfaces as a write conflict instead, which WithTxn
// retries, and the retry then sees it here.
func (r *Outbox) InsertUnique(ctx context.Context, ev *models.OutboxEvent) (bool, error) {
	tenantID, err := tenantrepo.StampInsert(ctx)
	if err != nil {
		return false, err
	}
	filter, err := tenantrepo.Scope(ctx, bson.M{"period": ev.Period, "threshold": ev.Threshold})
	if err != nil {
		return false, err
	}
	n, err := r.coll.CountDocuments(ctx, filter, options.Count().SetLimit(1))
	if err != nil {
		return false, err
	}
	if n > 0 {
		return false, nil
	}
	ev.TenantID = tenantID
	if _, err := r.coll.InsertOne(ctx, ev); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// LeaseDue claims the next due event of any org — pending, or sending
// with an expired lease (its worker died between leasing and marking) —
// by setting it sending until now+leaseFor. A held lease makes the event
// invisible to other workers. nil when nothing is due.
func (r *Outbox) LeaseDue(ctx context.Context, now time.Time, leaseFor time.Duration) (*models.OutboxEvent, error) {
	until := now.Add(leaseFor).Truncate(time.Millisecond) // BSON dates are ms; the fence compares this value
	var ev models.OutboxEvent
	//tenantscope:allow system: outbox worker drains budget notifications of every org; each leased event is then handled under its own tenantId (MarkSent/Reschedule/MarkFailed are scoped)
	err := r.coll.FindOneAndUpdate(ctx, bson.M{
		"status":        bson.M{"$in": []string{models.OutboxStatusPending, models.OutboxStatusSending}},
		"nextAttemptAt": bson.M{"$lte": now},
		"$or":           []bson.M{{"leaseUntil": nil}, {"leaseUntil": bson.M{"$lt": now}}},
	}, bson.M{"$set": bson.M{"status": models.OutboxStatusSending, "leaseUntil": until}},
		options.FindOneAndUpdate().SetSort(bson.D{{Key: "nextAttemptAt", Value: 1}}).SetReturnDocument(options.After)).Decode(&ev)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &ev, nil
}

// setStatus writes the outcome of one leased attempt. It is fenced by
// the lease: the filter requires the event to still be sending under
// exactly the leaseUntil this worker obtained, so a worker whose lease
// expired (and was taken over) cannot overwrite the new holder's state.
// A lost lease is ErrLeaseLost and writes nothing.
func (r *Outbox) setStatus(ctx context.Context, uuid string, leaseUntil time.Time, set bson.M) error {
	filter, err := tenantrepo.Scope(ctx, bson.M{"uuid": uuid, "status": models.OutboxStatusSending, "leaseUntil": leaseUntil})
	if err != nil {
		return err
	}
	set["leaseUntil"] = nil
	res, err := r.coll.UpdateOne(ctx, filter, bson.M{"$set": set})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrLeaseLost
	}
	return nil
}

// MarkSent records delivery under the lease that ended at leaseUntil;
// terminal.
func (r *Outbox) MarkSent(ctx context.Context, uuid string, leaseUntil time.Time) error {
	return r.setStatus(ctx, uuid, leaseUntil, bson.M{"status": models.OutboxStatusSent, "sentAt": time.Now().UTC()})
}

// Reschedule releases the lease and retries the event at next.
func (r *Outbox) Reschedule(ctx context.Context, uuid string, leaseUntil time.Time, attempts int, next time.Time) error {
	return r.setStatus(ctx, uuid, leaseUntil, bson.M{"status": models.OutboxStatusPending, "attempts": attempts, "nextAttemptAt": next})
}

// MarkFailed abandons the event after the bounded retries; terminal.
func (r *Outbox) MarkFailed(ctx context.Context, uuid string, leaseUntil time.Time, attempts int) error {
	return r.setStatus(ctx, uuid, leaseUntil, bson.M{"status": models.OutboxStatusFailed, "attempts": attempts})
}

// PurgeSettledBefore deletes the sent and failed events of every org
// created before cutoff (daily retention). Pending and sending events are
// never touched; the rollup's warnedAt/exhaustedAt marks, not these rows,
// keep a period from notifying twice.
func (r *Outbox) PurgeSettledBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	//tenantscope:allow system: retention purge of delivered or abandoned budget notifications for every org on the daily ticker; deletes only terminal rows older than the module-wide retention cutoff
	res, err := r.coll.DeleteMany(ctx, bson.M{"status": bson.M{"$in": []string{models.OutboxStatusSent, models.OutboxStatusFailed}}, "createdAt": bson.M{"$lt": cutoff}})
	if err != nil {
		return 0, err
	}
	return res.DeletedCount, nil
}
