package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/orkestra/backend/internal/core/retrieval/models"
	"github.com/orkestra/backend/pkg/sdk/tenantrepo"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// AdminStore holds the reads and corpus-row deletes the admin surface adds on
// top of CorpusStates and TenantProgress. Absent rows are nil, nil elsewhere
// in this package; here every read returns a (possibly empty) aggregate.
type AdminStore struct{ outbox, progress, erasures, states, searchLog *mongo.Collection }

func NewAdminStore(db *mongo.Database) *AdminStore {
	return &AdminStore{
		outbox:    db.Collection(CollOutbox),
		progress:  db.Collection(CollTenantProgress),
		erasures:  db.Collection(CollErasures),
		states:    db.Collection(CollCorpusState),
		searchLog: db.Collection(CollSearchLog),
	}
}

// pendingOutbox is the outbox rows not yet applied (dirty) or being applied
// now (leased). OldestAt reads updatedAt: the entry has no per-intent enqueue
// time and every enqueue sets updatedAt.
var pendingOutbox = bson.M{"$or": []bson.M{{"dirty": true}, {"leaseUntil": bson.M{"$ne": nil}}}}

func backlogPipeline() []bson.M {
	return []bson.M{
		{"$match": pendingOutbox},
		{"$group": bson.M{"_id": "$corpus", "pending": bson.M{"$sum": 1}, "oldest": bson.M{"$min": "$updatedAt"}}},
	}
}

func decodeBacklog(ctx context.Context, cur *mongo.Cursor) (map[string]models.RetrievalBacklog, error) {
	var rows []struct {
		Corpus  string    `bson:"_id"`
		Pending int64     `bson:"pending"`
		Oldest  time.Time `bson:"oldest"`
	}
	if err := cur.All(ctx, &rows); err != nil {
		return nil, err
	}
	out := make(map[string]models.RetrievalBacklog, len(rows))
	for _, r := range rows {
		b := models.RetrievalBacklog{Pending: r.Pending}
		if !r.Oldest.IsZero() {
			t := r.Oldest.UTC()
			b.OldestAt = &t
		}
		out[r.Corpus] = b
	}
	return out, nil
}

// OutboxBacklogAll counts pending outbox rows per corpus across all tenants.
func (r *AdminStore) OutboxBacklogAll(ctx context.Context) (map[string]models.RetrievalBacklog, error) {
	//tenantscope:allow system: installation-wide outbox backlog per corpus for the internal-tenant admin page
	cur, err := r.outbox.Aggregate(ctx, backlogPipeline())
	if err != nil {
		return nil, err
	}
	return decodeBacklog(ctx, cur)
}

// OutboxBacklogTenant counts pending outbox rows per corpus for the tenant in ctx.
func (r *AdminStore) OutboxBacklogTenant(ctx context.Context) (map[string]models.RetrievalBacklog, error) {
	pipe, err := tenantrepo.ScopeAggregate(ctx, backlogPipeline())
	if err != nil {
		return nil, err
	}
	cur, err := r.outbox.Aggregate(ctx, pipe)
	if err != nil {
		return nil, err
	}
	return decodeBacklog(ctx, cur)
}

// ProgressCounts counts tenants by initial-build status per (corpus, generation).
func (r *AdminStore) ProgressCounts(ctx context.Context) (map[models.ProgressKey]models.ProgressCounts, error) {
	//tenantscope:allow system: installation-wide tenant build progress per corpus generation for the internal-tenant admin page
	cur, err := r.progress.Aggregate(ctx, []bson.M{{"$group": bson.M{
		"_id": bson.M{"corpus": "$corpus", "generation": "$generation", "status": "$status"},
		"n":   bson.M{"$sum": 1},
	}}})
	if err != nil {
		return nil, err
	}
	var rows []struct {
		ID struct {
			Corpus     string `bson:"corpus"`
			Generation int    `bson:"generation"`
			Status     string `bson:"status"`
		} `bson:"_id"`
		N int64 `bson:"n"`
	}
	if err := cur.All(ctx, &rows); err != nil {
		return nil, err
	}
	out := map[models.ProgressKey]models.ProgressCounts{}
	for _, row := range rows {
		k := models.ProgressKey{Corpus: row.ID.Corpus, Generation: row.ID.Generation}
		c := out[k]
		switch row.ID.Status {
		case models.ProgressDone:
			c.Done += row.N
		case models.ProgressBuilding:
			c.Building += row.N
		}
		out[k] = c
	}
	return out, nil
}

// ErasureCounts counts tombstones still being finalized. A missing status is
// a row written before finalization existed and counts as pending.
func (r *AdminStore) ErasureCounts(ctx context.Context) (models.RetrievalErasureCounts, error) {
	var out models.RetrievalErasureCounts
	//tenantscope:allow system: installation-wide pending erasure tombstones for the internal-tenant admin page
	pending, err := r.erasures.CountDocuments(ctx, bson.M{"$or": []bson.M{
		{"status": models.ErasureStatusPending}, {"status": bson.M{"$exists": false}},
	}})
	if err != nil {
		return out, err
	}
	//tenantscope:allow system: installation-wide fencing erasure tombstones for the internal-tenant admin page
	fencing, err := r.erasures.CountDocuments(ctx, bson.M{"status": models.ErasureStatusFencing})
	if err != nil {
		return out, err
	}
	out.Pending, out.Fencing = pending, fencing
	return out, nil
}

// TenantErasuresPending counts the tenant-scope tombstones of the tenant in
// ctx that are not done ($ne matches an absent status).
func (r *AdminStore) TenantErasuresPending(ctx context.Context) (int64, error) {
	f, err := tenantrepo.Scope(ctx, bson.M{"scope": models.ErasureScopeTenant, "status": bson.M{"$ne": models.ErasureStatusDone}})
	if err != nil {
		return 0, err
	}
	return r.erasures.CountDocuments(ctx, f)
}

// DeleteCorpusRows removes every tenant's outbox and tenant-progress rows of
// corpus and returns how many of each. Erasure tombstones and search-log rows
// are untouched.
func (r *AdminStore) DeleteCorpusRows(ctx context.Context, corpus string) (outbox, progress int64, err error) {
	//tenantscope:allow system: dropping an orphan corpus removes its outbox rows in every tenant
	res, err := r.outbox.DeleteMany(ctx, bson.M{"corpus": corpus})
	if err != nil {
		return 0, 0, err
	}
	outbox = res.DeletedCount
	//tenantscope:allow system: dropping an orphan corpus removes its tenant-progress rows in every tenant
	res, err = r.progress.DeleteMany(ctx, bson.M{"corpus": corpus})
	if err != nil {
		return outbox, 0, err
	}
	return outbox, res.DeletedCount, nil
}

// DeleteCorpusState deletes the state row of corpus if its Version still
// equals version; ErrStateConflict when it moved or is gone.
func (r *AdminStore) DeleteCorpusState(ctx context.Context, corpus string, version int64) error {
	//tenantscope:allow system: installation-wide corpus state (ADR-0023 D11), compare-and-delete on version
	res, err := r.states.DeleteOne(ctx, bson.M{"_id": corpus, "version": version})
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return ErrStateConflict
	}
	return nil
}

// SearchLog returns one page of the tenant's confirmed search-log rows,
// newest first, and the total matching rows. Unchecked (provisional) rows are
// hidden and not counted: until the post-write erasure re-check clears them
// they could show an erased subject.
func (r *AdminStore) SearchLog(ctx context.Context, q models.SearchLogQuery) ([]models.SearchLog, int64, error) {
	base := bson.M{"unchecked": bson.M{"$ne": true}}
	if q.UserUUID != "" {
		base["userUuid"] = q.UserUUID
	}
	bounds := bson.M{}
	if q.Since != nil {
		bounds["$gte"] = ms(*q.Since)
	}
	if q.Until != nil {
		bounds["$lte"] = ms(*q.Until)
	}
	if len(bounds) > 0 {
		base["createdAt"] = bounds
	}
	filter, err := tenantrepo.Scope(ctx, base)
	if err != nil {
		return nil, 0, err
	}
	total, err := r.searchLog.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("count search log: %w", err)
	}
	opts := options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}, {Key: "uuid", Value: -1}}).
		SetSkip(int64(q.Offset)).SetLimit(int64(q.Limit))
	cur, err := r.searchLog.Find(ctx, filter, opts)
	if err != nil {
		return nil, 0, err
	}
	rows := []models.SearchLog{}
	if err := cur.All(ctx, &rows); err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}
