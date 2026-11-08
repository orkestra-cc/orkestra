// Package repository owns the Mongo persistence of the jobs module. All
// time-dependent methods take `now` explicitly so the engine and tests
// control the clock.
package repository

import (
	"context"
	"errors"
	"time"

	"github.com/orkestra/backend/internal/core/jobs/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var (
	ErrNotFound         = errors.New("jobs: state not found")
	ErrRevisionConflict = errors.New("jobs: revision conflict")
	ErrAlreadyRunning   = errors.New("jobs: job is already running")
	ErrAlreadyRequested = errors.New("jobs: run already requested")
)

// Completion is the state update applied when a run finishes.
type Completion struct {
	LastRun      models.LastRun
	SetNext      bool
	NextRunAt    *time.Time
	RetryAt      *time.Time
	RetrySlotAt  *time.Time
	Attempt      int
	ClearRequest bool
}

// StateRepository persists per-job state and implements the atomic claim.
type StateRepository interface {
	EnsureState(ctx context.Context, key, module, fingerprint string, nextRunAt *time.Time, now time.Time) error
	Get(ctx context.Context, key string) (*models.State, error)
	List(ctx context.Context) ([]models.State, error)
	Reschedule(ctx context.Context, key string, revision int64, fingerprint string, nextRunAt *time.Time, now time.Time) (bool, error)
	SetOverride(ctx context.Context, key string, revision int64, o *models.Override, fingerprint string, nextRunAt *time.Time, by string, now time.Time) (*models.State, error)
	RequestRun(ctx context.Context, key, by string, now time.Time) (*models.State, error)
	Due(ctx context.Context, now time.Time, limit int) ([]models.State, error)
	Claim(ctx context.Context, key string, revision int64, lease models.Lease, now time.Time) (*models.State, error)
	Renew(ctx context.Context, key, runID string, until time.Time) (bool, error)
	Complete(ctx context.Context, key, runID string, upd Completion) (bool, error)
}

type stateRepo struct{ coll *mongo.Collection }

// NewStateRepository binds to the jobs_states collection.
func NewStateRepository(coll *mongo.Collection) StateRepository { return &stateRepo{coll: coll} }

// leaseFree matches a state whose lease is absent, null or expired.
func leaseFree(now time.Time) bson.M {
	return bson.M{"$or": bson.A{
		bson.M{"lease": nil}, // matches absent and null
		bson.M{"lease.until": bson.M{"$lt": now}},
	}}
}

func (r *stateRepo) EnsureState(ctx context.Context, key, module, fingerprint string, nextRunAt *time.Time, now time.Time) error {
	ins := bson.M{
		"module":      module,
		"fingerprint": fingerprint,
		"attempt":     0,
		"revision":    int64(0),
		"updatedAt":   now,
	}
	if nextRunAt != nil {
		ins["nextRunAt"] = *nextRunAt
	}
	//tenantscope:allow jobs_states is platform-level scheduler state, not tenant data.
	_, err := r.coll.UpdateOne(ctx, bson.M{"_id": key}, bson.M{"$setOnInsert": ins}, options.Update().SetUpsert(true))
	if mongo.IsDuplicateKeyError(err) {
		return nil // another replica inserted first
	}
	return err
}

func (r *stateRepo) Get(ctx context.Context, key string) (*models.State, error) {
	var s models.State
	//tenantscope:allow platform-level scheduler state.
	err := r.coll.FindOne(ctx, bson.M{"_id": key}).Decode(&s)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *stateRepo) List(ctx context.Context) ([]models.State, error) {
	//tenantscope:allow platform-level scheduler state.
	cur, err := r.coll.Find(ctx, bson.M{}, options.Find().SetSort(bson.D{{Key: "_id", Value: 1}}))
	if err != nil {
		return nil, err
	}
	var out []models.State
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *stateRepo) Reschedule(ctx context.Context, key string, revision int64, fingerprint string, nextRunAt *time.Time, now time.Time) (bool, error) {
	set := bson.M{"fingerprint": fingerprint, "updatedAt": now}
	unset := bson.M{}
	if nextRunAt != nil {
		set["nextRunAt"] = *nextRunAt
	} else {
		unset["nextRunAt"] = ""
		unset["retryAt"] = ""
		unset["retrySlotAt"] = ""
		set["attempt"] = 0
	}
	upd := bson.M{"$set": set, "$inc": bson.M{"revision": 1}}
	if len(unset) > 0 {
		upd["$unset"] = unset
	}
	//tenantscope:allow platform-level scheduler state.
	res, err := r.coll.UpdateOne(ctx, bson.M{"_id": key, "revision": revision}, upd)
	if err != nil {
		return false, err
	}
	return res.MatchedCount == 1, nil
}

func (r *stateRepo) SetOverride(ctx context.Context, key string, revision int64, o *models.Override, fingerprint string, nextRunAt *time.Time, by string, now time.Time) (*models.State, error) {
	set := bson.M{"fingerprint": fingerprint, "updatedAt": now, "updatedBy": by}
	unset := bson.M{}
	if o != nil {
		set["override"] = o
	} else {
		unset["override"] = ""
	}
	if nextRunAt != nil {
		set["nextRunAt"] = *nextRunAt
	} else {
		unset["nextRunAt"] = ""
		unset["retryAt"] = ""
		unset["retrySlotAt"] = ""
		set["attempt"] = 0
	}
	upd := bson.M{"$set": set, "$inc": bson.M{"revision": 1}}
	if len(unset) > 0 {
		upd["$unset"] = unset
	}
	var s models.State
	//tenantscope:allow platform-level scheduler state.
	err := r.coll.FindOneAndUpdate(ctx, bson.M{"_id": key, "revision": revision}, upd,
		options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&s)
	if errors.Is(err, mongo.ErrNoDocuments) {
		if _, gerr := r.Get(ctx, key); gerr != nil {
			return nil, gerr // ErrNotFound or transport error
		}
		return nil, ErrRevisionConflict
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *stateRepo) RequestRun(ctx context.Context, key, by string, now time.Time) (*models.State, error) {
	filter := bson.M{
		"_id":            key,
		"runRequestedAt": nil,
		"$or": bson.A{
			bson.M{"lease": nil},
			bson.M{"lease.until": bson.M{"$lt": now}},
		},
	}
	upd := bson.M{
		"$set": bson.M{"runRequestedAt": now, "runRequestedBy": by, "updatedAt": now, "updatedBy": by},
		"$inc": bson.M{"revision": 1},
	}
	var s models.State
	//tenantscope:allow platform-level scheduler state.
	err := r.coll.FindOneAndUpdate(ctx, filter, upd,
		options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&s)
	if err == nil {
		return &s, nil
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		return nil, err
	}
	cur, gerr := r.Get(ctx, key)
	if gerr != nil {
		return nil, gerr
	}
	if cur.Lease != nil && !cur.Lease.Until.Before(now) {
		return nil, ErrAlreadyRunning
	}
	if cur.RunRequestedAt != nil {
		return nil, ErrAlreadyRequested
	}
	return nil, ErrRevisionConflict // lost a race between filter and re-read
}

func (r *stateRepo) Due(ctx context.Context, now time.Time, limit int) ([]models.State, error) {
	filter := bson.M{"$and": bson.A{
		leaseFree(now),
		bson.M{"$or": bson.A{
			bson.M{"nextRunAt": bson.M{"$lte": now}},
			bson.M{"retryAt": bson.M{"$lte": now}},
			bson.M{"runRequestedAt": bson.M{"$ne": nil}},
		}},
	}}
	opts := options.Find().SetSort(bson.D{{Key: "_id", Value: 1}})
	if limit > 0 {
		opts.SetLimit(int64(limit))
	}
	//tenantscope:allow platform-level scheduler state.
	cur, err := r.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	var out []models.State
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *stateRepo) Claim(ctx context.Context, key string, revision int64, lease models.Lease, now time.Time) (*models.State, error) {
	filter := bson.M{"$and": bson.A{
		bson.M{"_id": key, "revision": revision},
		leaseFree(now),
	}}
	upd := bson.M{"$set": bson.M{"lease": lease}, "$inc": bson.M{"revision": 1}}
	var s models.State
	//tenantscope:allow platform-level scheduler state.
	err := r.coll.FindOneAndUpdate(ctx, filter, upd,
		options.FindOneAndUpdate().SetReturnDocument(options.Before)).Decode(&s)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *stateRepo) Renew(ctx context.Context, key, runID string, until time.Time) (bool, error) {
	//tenantscope:allow platform-level scheduler state.
	res, err := r.coll.UpdateOne(ctx, bson.M{"_id": key, "lease.runId": runID},
		bson.M{"$set": bson.M{"lease.until": until}})
	if err != nil {
		return false, err
	}
	return res.MatchedCount == 1, nil
}

func (r *stateRepo) Complete(ctx context.Context, key, runID string, upd Completion) (bool, error) {
	set := bson.M{"lastRun": upd.LastRun, "attempt": upd.Attempt}
	unset := bson.M{"lease": ""}
	put := func(field string, t *time.Time) {
		if t != nil {
			set[field] = *t
		} else {
			unset[field] = ""
		}
	}
	put("retryAt", upd.RetryAt)
	put("retrySlotAt", upd.RetrySlotAt)
	if upd.SetNext {
		put("nextRunAt", upd.NextRunAt)
	}
	if upd.ClearRequest {
		unset["runRequestedAt"] = ""
		unset["runRequestedBy"] = ""
	}
	//tenantscope:allow platform-level scheduler state.
	res, err := r.coll.UpdateOne(ctx, bson.M{"_id": key, "lease.runId": runID},
		bson.M{"$set": set, "$unset": unset, "$inc": bson.M{"revision": 1}})
	if err != nil {
		return false, err
	}
	return res.MatchedCount == 1, nil
}
