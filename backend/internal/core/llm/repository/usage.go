package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/orkestra/backend/internal/core/llm/models"
	"github.com/orkestra/backend/pkg/sdk/tenantrepo"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	CollUsage        = "llm_usage"
	CollUsageMonthly = "llm_usage_monthly"
)

var (
	// ErrAlreadyFinalized: the ledger row is not pending (closed by the
	// router or the reconciler, or not this org's). Finalization is
	// idempotent through it: a late finalize never counts twice.
	ErrAlreadyFinalized = errors.New("llm: usage row already finalized")
	// ErrBudgetExceeded aborts a reservation transaction whose request
	// does not fit the remaining balance.
	ErrBudgetExceeded = errors.New("llm repository: budget exceeded")
	// ErrInvalidGroupBy: Usage.Query only groups by modelUuid, userUuid
	// or caller.
	ErrInvalidGroupBy = errors.New("llm repository: invalid usage grouping")
)

// WithTxn runs fn inside a MongoDB multi-document transaction (replica set
// required, as in every Orkestra environment). session.WithTransaction
// retries fn from scratch on a TransientTransactionError (a write
// conflict with a concurrent transaction), so fn must use only the
// session context it receives and must not leak side effects outside the
// transaction.
func WithTxn(ctx context.Context, db *mongo.Database, fn func(sc mongo.SessionContext) error) error {
	sess, err := db.Client().StartSession()
	if err != nil {
		return fmt.Errorf("llm: start session: %w", err)
	}
	defer sess.EndSession(ctx)
	_, err = sess.WithTransaction(ctx, func(sc mongo.SessionContext) (any, error) {
		return nil, fn(sc)
	})
	return err
}

// Usage owns llm_usage, the per-call ledger.
type Usage struct{ coll *mongo.Collection }

func NewUsage(db *mongo.Database) *Usage { return &Usage{coll: db.Collection(CollUsage)} }

// InsertPending inserts a ledger row under the caller's org (stamped from
// ctx, overriding whatever ev carried). Works with a plain context and,
// inside WithTxn, with the session context.
func (r *Usage) InsertPending(ctx context.Context, ev *models.UsageEvent) error {
	tenantID, err := tenantrepo.StampInsert(ctx)
	if err != nil {
		return err
	}
	ev.TenantID = tenantID
	_, err = r.coll.InsertOne(ctx, ev)
	return err
}

// Finalize closes a pending row of the caller's org and returns it as it
// was before the update (its reservation, kind and createdAt drive the
// rollup). A row that is not pending — or not this org's — is
// ErrAlreadyFinalized.
func (r *Usage) Finalize(ctx context.Context, uuid string, upd models.FinalizeUpdate) (*models.UsageEvent, error) {
	filter, err := tenantrepo.Scope(ctx, bson.M{"uuid": uuid, "outcome": models.OutcomePending})
	if err != nil {
		return nil, err
	}
	attempts := upd.Attempts
	if attempts == nil {
		attempts = []models.Attempt{}
	}
	// A pipeline update, so the credential reference can be conditioned on
	// the row's current state: a DSR anonymization may have rewritten
	// userUuid (and dropped credentialUuid) while the call was in flight,
	// and the finalization must not put the reference back. Literal values
	// go through $literal so no string is ever read as a field path.
	lit := func(v any) bson.M { return bson.M{"$literal": v} }
	set := bson.M{
		"outcome": lit(upd.Outcome), "fallback": lit(upd.Fallback), "inputTokens": lit(upd.InputTokens), "outputTokens": lit(upd.OutputTokens),
		"attempts": lit(attempts), "latencyMs": lit(upd.LatencyMs), "finalizedAt": lit(upd.FinalizedAt), "reservedTokens": lit(0),
	}
	if upd.ErrorCode != "" {
		set["errorCode"] = lit(upd.ErrorCode)
	}
	for field, v := range map[string]string{"provider": upd.Provider, "modelId": upd.ModelID, "modelUuid": upd.ModelUUID, "credentialKind": upd.CredentialKind} {
		if v != "" {
			set[field] = lit(v)
		}
	}
	if upd.CredentialUUID != "" {
		set["credentialUuid"] = bson.M{"$cond": bson.A{
			bson.M{"$regexMatch": bson.M{"input": bson.M{"$ifNull": bson.A{"$userUuid", ""}}, "regex": "^" + models.ErasedUsagePrefix}},
			"$$REMOVE",
			lit(upd.CredentialUUID),
		}}
	}
	pipeline := bson.A{bson.M{"$set": set}, bson.M{"$unset": bson.A{"reservationExpiresAt"}}}
	var before models.UsageEvent
	err = r.coll.FindOneAndUpdate(ctx, filter, pipeline,
		options.FindOneAndUpdate().SetReturnDocument(options.Before)).Decode(&before)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrAlreadyFinalized
	}
	if err != nil {
		return nil, err
	}
	return &before, nil
}

// ListPendingExpired returns pending rows of every org whose reservation
// expired before `before`, oldest first. Each carries the tenantId stamped
// at insert; the reconciler closes them through Finalize under that
// tenant's scope.
func (r *Usage) ListPendingExpired(ctx context.Context, before time.Time, limit int) ([]models.UsageEvent, error) {
	//tenantscope:allow system: reconciler sweeps pending reservations of every org on a ticker; rows are closed only through Finalize, scoped by the tenantId stamped at insert
	cur, err := r.coll.Find(ctx, bson.M{"outcome": models.OutcomePending, "reservationExpiresAt": bson.M{"$lt": before}},
		options.Find().SetSort(bson.D{{Key: "reservationExpiresAt", Value: 1}}).SetLimit(int64(limit)))
	if err != nil {
		return nil, err
	}
	out := []models.UsageEvent{}
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

var usageGroupFields = map[string]bool{"modelUuid": true, "userUuid": true, "caller": true}

// Query aggregates the caller's org usage in [from, to) by groupBy
// (modelUuid, userUuid or caller). It counts every finalized row that
// consumed tokens — error rows included, so the report agrees with the
// monthly rollup — and never pending or budget_blocked rows.
func (r *Usage) Query(ctx context.Context, from, to time.Time, groupBy string) ([]models.LLMUsageRow, error) {
	if !usageGroupFields[groupBy] {
		return nil, ErrInvalidGroupBy
	}
	pipeline, err := tenantrepo.ScopeAggregate(ctx, []bson.M{
		{"$match": bson.M{
			"createdAt": bson.M{"$gte": from, "$lt": to},
			"outcome":   bson.M{"$nin": []string{models.OutcomePending, models.OutcomeBudgetBlocked}},
			"$or":       []bson.M{{"inputTokens": bson.M{"$gt": 0}}, {"outputTokens": bson.M{"$gt": 0}}},
		}},
		{"$group": bson.M{
			"_id":          "$" + groupBy,
			"requests":     bson.M{"$sum": 1},
			"inputTokens":  bson.M{"$sum": "$inputTokens"},
			"outputTokens": bson.M{"$sum": "$outputTokens"},
		}},
		{"$project": bson.M{"_id": 0, "key": bson.M{"$ifNull": []any{"$_id", ""}}, "requests": 1, "inputTokens": 1, "outputTokens": 1}},
		{"$sort": bson.D{{Key: "inputTokens", Value: -1}, {Key: "key", Value: 1}}},
	})
	if err != nil {
		return nil, err
	}
	cur, err := r.coll.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	out := []models.LLMUsageRow{}
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// UserMonth sums a user's finalized usage in period, split by credential
// kind.
func (r *Usage) UserMonth(ctx context.Context, userUUID, period string) (*models.LLMSelfUsage, error) {
	start, end, err := models.PeriodBounds(period)
	if err != nil {
		return nil, fmt.Errorf("llm: period %q: %w", period, err)
	}
	pipeline, err := tenantrepo.ScopeAggregate(ctx, []bson.M{
		{"$match": bson.M{
			"userUuid":  userUUID,
			"createdAt": bson.M{"$gte": start, "$lt": end},
			"outcome":   bson.M{"$nin": []string{models.OutcomePending, models.OutcomeBudgetBlocked}},
		}},
		{"$group": bson.M{
			"_id":          "$credentialKind",
			"requests":     bson.M{"$sum": 1},
			"inputTokens":  bson.M{"$sum": "$inputTokens"},
			"outputTokens": bson.M{"$sum": "$outputTokens"},
		}},
	})
	if err != nil {
		return nil, err
	}
	cur, err := r.coll.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	var groups []struct {
		Kind         string `bson:"_id"`
		Requests     int64  `bson:"requests"`
		InputTokens  int64  `bson:"inputTokens"`
		OutputTokens int64  `bson:"outputTokens"`
	}
	if err := cur.All(ctx, &groups); err != nil {
		return nil, err
	}
	out := &models.LLMSelfUsage{Period: period}
	for _, g := range groups {
		out.Requests += g.Requests
		if g.Kind == models.CredentialKindUserAccount {
			out.UserAccountInputTokens += g.InputTokens
			out.UserAccountOutputTokens += g.OutputTokens
		} else {
			out.OrgInputTokens += g.InputTokens
			out.OrgOutputTokens += g.OutputTokens
		}
	}
	return out, nil
}

// PurgeBefore deletes finalized rows of every org created before cutoff
// (usage_retention_days). Pending rows are left to the reconciler.
func (r *Usage) PurgeBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	//tenantscope:allow system: retention purge runs for every org on the daily ticker; it deletes only finalized rows older than the module-wide retention cutoff
	res, err := r.coll.DeleteMany(ctx, bson.M{"createdAt": bson.M{"$lt": cutoff}, "outcome": bson.M{"$ne": models.OutcomePending}})
	if err != nil {
		return 0, err
	}
	return res.DeletedCount, nil
}

// ListByUserAllTenants exports every ledger row of a data subject.
func (r *Usage) ListByUserAllTenants(ctx context.Context, userUUID string) ([]models.UsageEvent, error) {
	//tenantscope:allow dsr: right-of-access export of the subject's usage rows across every org, invoked only by the compliance DSR pipeline through iface.PIIProducer
	cur, err := r.coll.Find(ctx, bson.M{"userUuid": userUUID})
	if err != nil {
		return nil, err
	}
	out := []models.UsageEvent{}
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// AnonymizeByUser replaces the subject on every ledger row (keeping the
// counters the org's rollup and reports rely on) and drops the
// credential reference that could point back to a linked account. Each
// row gets its own replacement from newID, so the subject's rows can no
// longer be linked to one another (a per-subject pseudonym would keep a
// behavioural profile: times, models, callers). Rows are rewritten in
// bulk batches; a row whose userUuid changed meanwhile is skipped.
func (r *Usage) AnonymizeByUser(ctx context.Context, userUUID string, newID func() string) (int64, error) {
	//tenantscope:allow dsr: right-to-erasure anonymization of the subject's usage rows across every org, invoked only by the compliance DSR pipeline through iface.PIIProducer
	cur, err := r.coll.Find(ctx, bson.M{"userUuid": userUUID}, options.Find().SetProjection(bson.M{"_id": 1}))
	if err != nil {
		return 0, err
	}
	defer cur.Close(ctx)
	var total int64
	batch := make([]mongo.WriteModel, 0, anonymizeBatch)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		// BulkWrite is outside tenantscope's method list: every filter below is
		// the subject-scoped _id + userUuid pair, under the dsr directive above.
		res, err := r.coll.BulkWrite(ctx, batch, options.BulkWrite().SetOrdered(false))
		if res != nil {
			total += res.ModifiedCount
		}
		batch = batch[:0]
		return err
	}
	for cur.Next(ctx) {
		var row struct {
			ID any `bson:"_id"`
		}
		if err := cur.Decode(&row); err != nil {
			return total, err
		}
		batch = append(batch, mongo.NewUpdateOneModel().
			SetFilter(bson.M{"_id": row.ID, "userUuid": userUUID}).
			SetUpdate(bson.M{"$set": bson.M{"userUuid": newID()}, "$unset": bson.M{"credentialUuid": ""}}))
		if len(batch) == anonymizeBatch {
			if err := flush(); err != nil {
				return total, err
			}
		}
	}
	if err := cur.Err(); err != nil {
		return total, err
	}
	return total, flush()
}

// anonymizeBatch bounds one BulkWrite of AnonymizeByUser.
const anonymizeBatch = 500

// DeleteByUser hard-deletes every finalized ledger row of a data
// subject. Pending rows are skipped: deleting one would strand its
// reservation in the rollup's reservedTokens until the month ends; the
// reconciler closes them within its TTL and a later erasure run removes
// them.
func (r *Usage) DeleteByUser(ctx context.Context, userUUID string) (int64, error) {
	//tenantscope:allow dsr: right-to-erasure hard delete of the subject's usage rows across every org, invoked only by the compliance DSR pipeline through iface.PIIProducer
	res, err := r.coll.DeleteMany(ctx, bson.M{"userUuid": userUUID, "outcome": bson.M{"$ne": models.OutcomePending}})
	if err != nil {
		return 0, err
	}
	return res.DeletedCount, nil
}

// Monthly owns llm_usage_monthly, one rollup per org and UTC month.
type Monthly struct{ coll *mongo.Collection }

func NewMonthly(db *mongo.Database) *Monthly { return &Monthly{coll: db.Collection(CollUsageMonthly)} }

// Get returns the org's rollup for period; an absent document is an
// empty rollup, not an error.
func (r *Monthly) Get(ctx context.Context, period string) (*models.UsageMonthly, error) {
	filter, err := tenantrepo.Scope(ctx, bson.M{"period": period})
	if err != nil {
		return nil, err
	}
	var m models.UsageMonthly
	err = r.coll.FindOne(ctx, filter).Decode(&m)
	if errors.Is(err, mongo.ErrNoDocuments) {
		tenantID, _ := tenantrepo.CurrentTenantID(ctx)
		return &models.UsageMonthly{TenantID: tenantID, Period: period}, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// GetForUpdate reads the rollup inside a transaction and writes it
// (updatedAt, upserting an empty document when absent), so the document
// is locked by this transaction from the read on: a concurrent
// transaction touching it conflicts and is retried after this one
// commits, and the admission check below the read is serialized.
func (r *Monthly) GetForUpdate(sc mongo.SessionContext, period string) (*models.UsageMonthly, error) {
	filter, err := tenantrepo.Scope(sc, bson.M{"period": period})
	if err != nil {
		return nil, err
	}
	var m models.UsageMonthly
	err = r.coll.FindOneAndUpdate(sc, filter, bson.M{
		"$set": bson.M{"updatedAt": time.Now().UTC()},
		"$setOnInsert": bson.M{
			"requests": int64(0), "orgInputTokens": int64(0), "orgOutputTokens": int64(0),
			"userAccountInputTokens": int64(0), "userAccountOutputTokens": int64(0), "reservedTokens": int64(0),
		},
	}, options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After)).Decode(&m)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// Reserve adds delta to the rollup's reservedTokens (upserting it).
func (r *Monthly) Reserve(ctx context.Context, period string, delta int64) error {
	filter, err := tenantrepo.Scope(ctx, bson.M{"period": period})
	if err != nil {
		return err
	}
	_, err = r.coll.UpdateOne(ctx, filter, bson.M{
		"$inc": bson.M{"reservedTokens": delta},
		"$set": bson.M{"updatedAt": time.Now().UTC()},
	}, options.Update().SetUpsert(true))
	return err
}

// Apply records one finalized call: one request, the released
// reservation and the real tokens on the payer's counters.
func (r *Monthly) Apply(ctx context.Context, period, credentialKind string, released, input, output int64) error {
	filter, err := tenantrepo.Scope(ctx, bson.M{"period": period})
	if err != nil {
		return err
	}
	inField, outField := "orgInputTokens", "orgOutputTokens"
	if credentialKind == models.CredentialKindUserAccount {
		inField, outField = "userAccountInputTokens", "userAccountOutputTokens"
	}
	_, err = r.coll.UpdateOne(ctx, filter, bson.M{
		"$inc": bson.M{"requests": int64(1), "reservedTokens": -released, inField: input, outField: output},
		"$set": bson.M{"updatedAt": time.Now().UTC()},
	}, options.Update().SetUpsert(true))
	return err
}

var thresholdFields = map[string]string{models.ThresholdWarn: "warnedAt", models.ThresholdExhausted: "exhaustedAt", models.ThresholdOversized: "oversizedAt"}

// MarkThreshold sets warnedAt, exhaustedAt or oversizedAt on the rollup if unset and
// reports whether this call set it (true exactly once per period).
func (r *Monthly) MarkThreshold(ctx context.Context, period, which string) (bool, error) {
	field, ok := thresholdFields[which]
	if !ok {
		return false, fmt.Errorf("llm: unknown threshold %q", which)
	}
	filter, err := tenantrepo.Scope(ctx, bson.M{"period": period, field: nil})
	if err != nil {
		return false, err
	}
	res, err := r.coll.UpdateOne(ctx, filter, bson.M{"$set": bson.M{field: time.Now().UTC()}})
	if err != nil {
		return false, err
	}
	return res.ModifiedCount == 1, nil
}

// PurgeOlderThan deletes rollups of every org older than the last
// `periods` months (the spec keeps 24).
func (r *Monthly) PurgeOlderThan(ctx context.Context, periods int) (int64, error) {
	now := time.Now().UTC()
	cutoff := models.PeriodOf(time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -periods, 0))
	//tenantscope:allow system: retention purge of monthly rollups for every org on the daily ticker; deletes only periods older than the fixed 24-month window
	res, err := r.coll.DeleteMany(ctx, bson.M{"period": bson.M{"$lt": cutoff}})
	if err != nil {
		return 0, err
	}
	return res.DeletedCount, nil
}
