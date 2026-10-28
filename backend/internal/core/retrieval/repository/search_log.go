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
// the repository tests share them. The last two are system sweeps (not led by
// tenantId): subjects serves the data-subject lookup, createdAt the retention
// purge.
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
	}
}

// SearchLog owns retrieval_search_log.
type SearchLog struct{ coll *mongo.Collection }

func NewSearchLog(db *mongo.Database) *SearchLog {
	return &SearchLog{coll: db.Collection(CollSearchLog)}
}

// Insert writes row for the tenant in ctx; row.TenantID is overridden. A set
// CreatedAt is kept (truncated to ms UTC), otherwise it is now.
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
	var total int64
	filter := bson.M{"createdAt": bson.M{"$lt": ms(cutoff)}}
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		//tenantscope:allow system: search-log retention runs for every tenant on the daily ticker; it selects only rows older than search_log_retention_days
		cur, err := r.coll.Find(ctx, filter, options.Find().SetLimit(searchLogPurgeBatch).SetProjection(bson.M{"_id": 1}))
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
		//tenantscope:allow system: search-log retention runs for every tenant on the daily ticker; it deletes only the rows selected above (older than search_log_retention_days)
		res, err := r.coll.DeleteMany(ctx, bson.M{"_id": bson.M{"$in": in}})
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
