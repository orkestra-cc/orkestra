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
// the repository tests share them. The last three are system sweeps (not led
// by tenantId): subjects and userUuid serve the data-subject lookup and
// erasure, createdAt the retention purge.
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
	return deleteInBatches(ctx, r.coll, bson.M{"createdAt": bson.M{"$lt": ms(cutoff)}})
}

// deleteInBatches deletes the rows matching filter, searchLogPurgeBatch at a
// time (find the _ids, then delete them), until a short batch. On error it
// returns the total deleted so far. The caller owns the filter's scope.
func deleteInBatches(ctx context.Context, coll *mongo.Collection, filter bson.M) (int64, error) {
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		//tenantscope:allow system: bounded batched delete shared by the search-log retention purge and the DSR/tenant erasure; each caller passes an explicit, narrow filter
		cur, err := coll.Find(ctx, filter, options.Find().SetLimit(searchLogPurgeBatch).SetProjection(bson.M{"_id": 1}))
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
		//tenantscope:allow system: bounded batched delete shared by the search-log retention purge and the DSR/tenant erasure; it deletes only the rows selected above
		res, err := coll.DeleteMany(ctx, bson.M{"_id": bson.M{"$in": in}})
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
