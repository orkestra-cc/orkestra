package repository

import (
	"context"
	"time"

	"github.com/orkestra/backend/internal/core/retrieval/models"
	"github.com/orkestra/backend/pkg/sdk/module"
	"github.com/orkestra/backend/pkg/sdk/tenantrepo"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// CollSearchLog is the per-search audit log (tenant-scoped, ADR-0023 D12).
const CollSearchLog = "retrieval_search_log"

// SearchLogIndexes are the declared indexes of CollSearchLog; the module and
// the repository tests share them. The last four are system sweeps (not led
// by tenantId): subjects and userUuid serve the data-subject lookup and
// erasure, createdAt the retention purge, and the partial {unchecked,
// createdAt} (only rows with unchecked true) the sweep of provisional rows.
func SearchLogIndexes() []module.IndexSpec {
	key := func(fields ...string) []module.IndexKey {
		out := make([]module.IndexKey, 0, len(fields))
		for _, f := range fields {
			out = append(out, module.IndexKey{Field: f, Direction: 1})
		}
		return out
	}
	return []module.IndexSpec{
		{OrderedKeys: key("tenantId", "uuid"), Unique: true},
		{OrderedKeys: key("tenantId", "userUuid", "createdAt")},
		{OrderedKeys: key("subjects")},
		{OrderedKeys: key("createdAt")},
		{OrderedKeys: key("userUuid")},
		{OrderedKeys: key("unchecked", "createdAt"), PartialFilter: map[string]any{"unchecked": true}},
	}
}

// SearchLog owns retrieval_search_log.
type SearchLog struct{ coll *mongo.Collection }

func NewSearchLog(db *mongo.Database) *SearchLog {
	return &SearchLog{coll: db.Collection(CollSearchLog)}
}

// Insert writes row for the tenant in ctx; row.TenantID is overridden. A set
// CreatedAt is kept (truncated to ms UTC), otherwise it is now. FilterJSON is
// stored verbatim as the string field "filter" (absent when empty); the
// caller fills it with the canonical JSON of the request's filter.
func (r *SearchLog) Insert(ctx context.Context, row *models.SearchLog) error {
	tenantID, err := tenantrepo.StampInsert(ctx)
	if err != nil {
		return err
	}
	row.TenantID = tenantID
	if row.Principals == nil {
		row.Principals = []string{}
	}
	if row.SourceIDs == nil {
		row.SourceIDs = []string{}
	}
	if row.Subjects == nil {
		row.Subjects = []string{}
	}
	if row.CreatedAt.IsZero() {
		row.CreatedAt = time.Now()
	}
	row.CreatedAt = ms(row.CreatedAt)
	_, err = r.coll.InsertOne(ctx, row)
	return err
}

// searchLogPurgeBatch bounds one delete; a var so tests can shrink it.
var searchLogPurgeBatch int64 = 1000

// PurgeBefore deletes every tenant's rows created before cutoff, in bounded
// batches so a backlog never becomes one huge delete. On error it returns the
// total deleted so far.
func (r *SearchLog) PurgeBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	return deleteInBatches(ctx, r.coll, bson.M{"createdAt": bson.M{"$lt": ms(cutoff)}}, allowSystem)
}

// PurgeUncheckedBefore deletes every tenant's provisional rows (unchecked
// true) created before cutoff, in the same bounded batches: rows whose
// post-write erasure re-check or its follow-up write failed. It is
// fail-closed for privacy: such a row is dropped even when no erasure covered
// it. On error it returns the total deleted so far.
func (r *SearchLog) PurgeUncheckedBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	return deleteInBatches(ctx, r.coll, bson.M{"unchecked": true, "createdAt": bson.M{"$lt": ms(cutoff)}}, allowSystem)
}

// allowLabel is the //tenantscope:allow category a shared helper files its
// Mongo calls under. The analyzer reads only the comment right above a call,
// so a helper serving both categories has one call site per label.
type allowLabel int

const (
	allowSystem allowLabel = iota // retention, unchecked sweep, tenant purge, finalization sweep
	allowDSR                      // data-subject erasure (ADR-0023 D12, D13)
)

// deleteInBatches deletes the rows matching filter, searchLogPurgeBatch at a
// time (find the _ids, then delete them), until a short batch. On error it
// returns the total deleted so far. The caller owns the filter's scope and
// names the label its path belongs to.
func deleteInBatches(ctx context.Context, coll *mongo.Collection, filter bson.M, label allowLabel) (int64, error) {
	var total int64
	findOpts := options.Find().SetLimit(searchLogPurgeBatch).SetProjection(bson.M{"_id": 1})
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		var cur *mongo.Cursor
		var err error
		if label == allowDSR { //nolint:gocritic // dupBranchBody: the branches differ only in their //tenantscope:allow label, which the analyzer reads from the line above each call
			//tenantscope:allow dsr: data-subject search-log erasure; DeleteByUserAllTenants and DeleteBySubject pass the subject's explicit filter (ADR-0023 D12)
			cur, err = coll.Find(ctx, filter, findOpts)
		} else {
			//tenantscope:allow system: search-log retention purge, unchecked-row sweep and tenant purge; each caller passes an explicit, narrow filter
			cur, err = coll.Find(ctx, filter, findOpts)
		}
		if err != nil {
			return total, err
		}
		var ids []bson.M
		if err := cur.All(ctx, &ids); err != nil {
			return total, err
		}
		if len(ids) == 0 {
			return total, nil
		}
		in := make([]any, len(ids))
		for i, d := range ids {
			in[i] = d["_id"]
		}
		var res *mongo.DeleteResult
		if label == allowDSR { //nolint:gocritic // dupBranchBody: the branches differ only in their //tenantscope:allow label, which the analyzer reads from the line above each call
			//tenantscope:allow dsr: data-subject search-log erasure; deletes only the rows selected above
			res, err = coll.DeleteMany(ctx, bson.M{"_id": bson.M{"$in": in}})
		} else {
			//tenantscope:allow system: search-log retention purge, unchecked-row sweep and tenant purge; deletes only the rows selected above
			res, err = coll.DeleteMany(ctx, bson.M{"_id": bson.M{"$in": in}})
		}
		if res != nil {
			total += res.DeletedCount
		}
		if err != nil {
			return total, err
		}
		if int64(len(ids)) < searchLogPurgeBatch {
			return total, nil
		}
	}
}

// DeleteRow deletes the search-log row uuid of the tenant in ctx. A missing
// row, or a row of another tenant, is not an error.
func (r *SearchLog) DeleteRow(ctx context.Context, uuid string) error {
	filter, err := tenantrepo.Scope(ctx, bson.M{"uuid": uuid})
	if err != nil {
		return err
	}
	_, err = r.coll.DeleteOne(ctx, filter)
	return err
}

// MarkChecked clears the unchecked marker of the search-log row uuid of the
// tenant in ctx, confirming that no erasure covered it. A missing row, or a
// row of another tenant, is not an error.
func (r *SearchLog) MarkChecked(ctx context.Context, uuid string) error {
	filter, err := tenantrepo.Scope(ctx, bson.M{"uuid": uuid})
	if err != nil {
		return err
	}
	_, err = r.coll.UpdateOne(ctx, filter, bson.M{"$unset": bson.M{"unchecked": ""}})
	return err
}
