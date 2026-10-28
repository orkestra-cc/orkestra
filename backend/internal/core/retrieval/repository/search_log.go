package repository

import (
	"context"
	"time"

	"github.com/orkestra/backend/internal/core/retrieval/models"
	"github.com/orkestra/backend/pkg/sdk/module"
	"github.com/orkestra/backend/pkg/sdk/tenantrepo"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
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
	if row.CreatedAt.IsZero() {
		row.CreatedAt = time.Now()
	}
	row.CreatedAt = ms(row.CreatedAt)
	_, err = r.coll.InsertOne(ctx, row)
	return err
}

// PurgeBefore deletes every tenant's rows created before cutoff.
func (r *SearchLog) PurgeBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	//tenantscope:allow system: search-log retention runs for every tenant on the daily ticker; it deletes only rows older than search_log_retention_days
	res, err := r.coll.DeleteMany(ctx, bson.M{"createdAt": bson.M{"$lt": ms(cutoff)}})
	if err != nil {
		return 0, err
	}
	return res.DeletedCount, nil
}
