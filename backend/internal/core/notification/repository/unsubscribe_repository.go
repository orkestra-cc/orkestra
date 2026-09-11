package repository

import (
	"context"
	"errors"
	"time"

	"github.com/orkestra/backend/internal/core/notification/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// UnsubscribeRepository stores hashed unsubscribe tokens.
// Tokens are single-use: UsedAt is set on consume and the document
// TTLs after 30 days via a MongoDB index on ExpiresAt.
type UnsubscribeRepository interface {
	Create(ctx context.Context, doc *models.UnsubscribeTokenDoc) error
	GetByHash(ctx context.Context, hash string) (*models.UnsubscribeTokenDoc, error)
	MarkUsed(ctx context.Context, hash string) error

	// ClaimToken is the consuming read: it decides, atomically, which of two
	// concurrent clicks gets to consume the token, and marks the work that
	// consume still owes afterwards.
	ClaimToken(ctx context.Context, hash string, now time.Time, hasUser bool) (*models.UnsubscribeTokenDoc, error)

	// ClearSinkPending / ClearPrefPending record that work the claim marked
	// is now done, so the reconciler stops picking it up.
	ClearSinkPending(ctx context.Context, hash string) error
	ClearPrefPending(ctx context.Context, hash string) error

	// ListPending is the reconciler's scan: the claimed tokens that still owe
	// work and are due for another attempt, oldest first and bounded.
	ListPending(ctx context.Context, now time.Time, limit int) ([]models.UnsubscribeTokenDoc, error)

	// RecordFailedAttempt counts one failed replay against the retry budget
	// and defers the row until retryAt.
	RecordFailedAttempt(ctx context.Context, hash string, retryAt time.Time) error

	// MarkDeadLettered ends the retry loop for a row past its budget.
	MarkDeadLettered(ctx context.Context, hash string, now time.Time) error
}

// defaultPendingScanLimit bounds a reconciler pass when the caller names no
// limit. An unbounded scan over a backlog is how a background job turns a
// transient outage into a second one.
const defaultPendingScanLimit = 100

type unsubscribeRepository struct {
	coll *mongo.Collection
}

func NewUnsubscribeRepository(db *mongo.Database) UnsubscribeRepository {
	return &unsubscribeRepository{
		coll: db.Collection(models.NotificationUnsubscribeTokensCollect),
	}
}

func (r *unsubscribeRepository) Create(ctx context.Context, doc *models.UnsubscribeTokenDoc) error {
	if doc.CreatedAt.IsZero() {
		doc.CreatedAt = time.Now()
	}
	_, err := r.coll.InsertOne(ctx, doc)
	return err
}

func (r *unsubscribeRepository) GetByHash(ctx context.Context, hash string) (*models.UnsubscribeTokenDoc, error) {
	var doc models.UnsubscribeTokenDoc
	//tenantscope:allow system: an unsubscribe token is keyed by its own hash and arrives from an anonymous mail client following a link — there is no tenant in the request context, and scoping this read by one would make a valid unsubscribe look unknown
	err := r.coll.FindOne(ctx, bson.M{"tokenHash": hash}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &doc, nil
}

func (r *unsubscribeRepository) MarkUsed(ctx context.Context, hash string) error {
	now := time.Now()
	//tenantscope:allow system: same scope as GetByHash — the row is addressed by the token hash an anonymous recipient presented, and carries no tenant
	_, err := r.coll.UpdateOne(ctx,
		bson.M{"tokenHash": hash},
		bson.M{"$set": bson.M{"usedAt": now}},
	)
	return err
}

// ClaimToken consumes a token atomically. The filter is the whole guard: a
// token already used, expired, or unknown does not match, and the caller
// gets (nil, nil) — never an error it might be tempted to treat as a
// failure. Two concurrent one-click POSTs therefore produce exactly one
// consume, and the loser still answers the recipient generically.
//
// The claim raises the pending flags in the SAME write that stamps usedAt,
// which is the only way the work downstream of it survives a crash: a
// process that dies between the claim and the sink call — or between the
// claim and the preference write — has no failure to react to, so there is
// no later moment at which it could mark anything. sinkPending always goes
// up; prefPending goes up only when hasUser says the token names a user and
// there is therefore a preference row to write. Consume lowers each one as
// its work completes.
func (r *unsubscribeRepository) ClaimToken(ctx context.Context, hash string, now time.Time, hasUser bool) (*models.UnsubscribeTokenDoc, error) {
	if hash == "" {
		return nil, nil
	}
	set := bson.M{"usedAt": now, "sinkPending": true}
	if hasUser {
		set["prefPending"] = true
	}
	//tenantscope:allow system: an unsubscribe token is keyed by its own hash and arrives from an anonymous mail client following a link — there is no tenant in the request context, and scoping the claim by one would make a valid one-click unsubscribe fail
	res := r.coll.FindOneAndUpdate(ctx,
		bson.M{"tokenHash": hash, "usedAt": nil, "expiresAt": bson.M{"$gt": now}},
		bson.M{"$set": set},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	)
	var doc models.UnsubscribeTokenDoc
	if err := res.Decode(&doc); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return &doc, nil
}

// ClearSinkPending records that the sink accepted the opt-out. Leaving the
// flag up after a success would make the reconciler retry it forever, which
// is as much a bug as losing a failure.
func (r *unsubscribeRepository) ClearSinkPending(ctx context.Context, hash string) error {
	//tenantscope:allow system: same scope as ClaimToken — the row is addressed by the token hash an anonymous recipient presented, and carries no tenant
	_, err := r.coll.UpdateOne(ctx,
		bson.M{"tokenHash": hash},
		bson.M{"$unset": bson.M{"sinkPending": ""}},
	)
	return err
}

// ClearPrefPending records that the preference row was written. Same reason
// as its sibling: the claim raised the flag before the work ran, so only the
// success can lower it.
func (r *unsubscribeRepository) ClearPrefPending(ctx context.Context, hash string) error {
	//tenantscope:allow system: same scope as ClaimToken — the row is addressed by the token hash an anonymous recipient presented, and carries no tenant
	_, err := r.coll.UpdateOne(ctx,
		bson.M{"tokenHash": hash},
		bson.M{"$unset": bson.M{"prefPending": ""}},
	)
	return err
}

// ListPending returns the rows the reconciler has to replay: a token the
// claim stamped, still carrying sinkPending or prefPending, not already given
// up on, and due now.
//
// Two clauses match documents where the field is ABSENT, which is the normal
// shape here — both are omitempty:
//
//   - deadLetteredAt $exists false — a row that was given up on keeps its
//     history but must never be scanned again.
//   - nextAttemptAt $not $gt now — "not scheduled for later", which is true
//     both for a row that has never failed (no field) and for one whose
//     backoff has elapsed. Written this way rather than $lte so a row that
//     was never deferred is not silently excluded.
//
// The backoff therefore lives in the query: every row this returns is a row
// the reconciler will act on, so a row can never sit in the scan's output
// being skipped tick after tick.
//
// Deliberately unsorted. The $or plans as two index scans merged (the two
// partial indexes module.go declares), which no single index can order, so a
// sort would be an in-memory sort over the WHOLE pending set — and the moment
// that matters is the one this job exists for, a backlog after an outage.
// Each branch already yields its rows oldest-due-first, because that is the
// second key of its index.
func (r *unsubscribeRepository) ListPending(ctx context.Context, now time.Time, limit int) ([]models.UnsubscribeTokenDoc, error) {
	if limit <= 0 {
		limit = defaultPendingScanLimit
	}
	filter := bson.M{
		"deadLetteredAt": bson.M{"$exists": false},
		"$or": []bson.M{
			{"sinkPending": true},
			{"prefPending": true},
		},
		"nextAttemptAt": bson.M{"$not": bson.M{"$gt": now}},
	}
	//tenantscope:allow system: the reconciler replays unsubscribe side effects for tokens presented by anonymous mail clients — the rows carry no tenant, and this runs on a background ticker with no request context to scope by
	cur, err := r.coll.Find(ctx, filter, options.Find().SetLimit(int64(limit)))
	if err != nil {
		return nil, err
	}
	var docs []models.UnsubscribeTokenDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

// RecordFailedAttempt counts the failure and pushes the row out to retryAt.
// The count is an $inc rather than a value the caller computed: two hosts
// running the ticker must not lose an attempt between them, or the budget
// would never be reached.
func (r *unsubscribeRepository) RecordFailedAttempt(ctx context.Context, hash string, retryAt time.Time) error {
	//tenantscope:allow system: same scope as ClaimToken — the row is addressed by the token hash an anonymous recipient presented, and carries no tenant
	_, err := r.coll.UpdateOne(ctx,
		bson.M{"tokenHash": hash},
		bson.M{
			"$inc": bson.M{"attempts": 1},
			"$set": bson.M{"nextAttemptAt": retryAt},
		},
	)
	return err
}

// MarkDeadLettered gives up on a row past its retry budget. Clearing the
// pending flags is the load-bearing half: a dead-lettered row that stayed
// pending is a row the scan would return for ever. The attempt that exhausted
// the budget is counted here, so attempts reflects what was actually tried.
func (r *unsubscribeRepository) MarkDeadLettered(ctx context.Context, hash string, now time.Time) error {
	//tenantscope:allow system: same scope as ClaimToken — the row is addressed by the token hash an anonymous recipient presented, and carries no tenant
	_, err := r.coll.UpdateOne(ctx,
		bson.M{"tokenHash": hash},
		bson.M{
			"$inc":   bson.M{"attempts": 1},
			"$set":   bson.M{"deadLetteredAt": now},
			"$unset": bson.M{"sinkPending": "", "prefPending": "", "nextAttemptAt": ""},
		},
	)
	return err
}
