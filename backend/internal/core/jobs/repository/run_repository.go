package repository

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/orkestra/backend/internal/core/jobs/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// RunCursor is the keyset position of a run listing (startedAt desc, _id desc).
type RunCursor struct {
	StartedAt time.Time
	ID        string
}

type cursorWire struct {
	T  string `json:"t"`
	ID string `json:"id"`
}

// EncodeRunCursor renders the opaque wire form (base64url, no padding).
func EncodeRunCursor(c *RunCursor) string {
	if c == nil {
		return ""
	}
	b, _ := json.Marshal(cursorWire{T: c.StartedAt.UTC().Format(time.RFC3339Nano), ID: c.ID})
	return base64.RawURLEncoding.EncodeToString(b)
}

// DecodeRunCursor parses EncodeRunCursor's output.
func DecodeRunCursor(s string) (*RunCursor, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("invalid cursor: %w", err)
	}
	var w cursorWire
	if err := json.Unmarshal(b, &w); err != nil {
		return nil, fmt.Errorf("invalid cursor: %w", err)
	}
	t, err := time.Parse(time.RFC3339Nano, w.T)
	if err != nil || w.ID == "" {
		return nil, errors.New("invalid cursor")
	}
	return &RunCursor{StartedAt: t, ID: w.ID}, nil
}

// RunRepository persists run records.
type RunRepository interface {
	Insert(ctx context.Context, r *models.RunRecord) error
	// Finish writes the outcome of a run that is still running; a record
	// already in a terminal status is left unchanged.
	Finish(ctx context.Context, id, status, errText string, finishedAt time.Time, durationMs int64, details map[string]any, dropped int) error
	MarkIfRunning(ctx context.Context, id, status string, finishedAt time.Time) error
	List(ctx context.Context, key, status string, before *RunCursor, limit int) ([]models.RunRecord, *RunCursor, error)
	DeleteOlderThan(ctx context.Context, cutoff time.Time) (int64, error)
}

type runRepo struct{ coll *mongo.Collection }

// NewRunRepository binds to the jobs_runs collection.
func NewRunRepository(coll *mongo.Collection) RunRepository { return &runRepo{coll: coll} }

func (r *runRepo) Insert(ctx context.Context, rec *models.RunRecord) error {
	//tenantscope:allow platform-level scheduler run history.
	_, err := r.coll.InsertOne(ctx, rec)
	return err
}

func (r *runRepo) Finish(ctx context.Context, id, status, errText string, finishedAt time.Time, durationMs int64, details map[string]any, dropped int) error {
	set := bson.M{"status": status, "finishedAt": finishedAt, "durationMs": durationMs}
	unset := bson.M{}
	if errText != "" {
		set["error"] = errText
	} else {
		unset["error"] = ""
	}
	if len(details) > 0 {
		set["details"] = details
	}
	if dropped > 0 {
		set["droppedDetails"] = dropped
	}
	upd := bson.M{"$set": set}
	if len(unset) > 0 {
		upd["$unset"] = unset
	}
	// Only a running record is finished: a terminal status written first
	// (abandoned by another replica, interrupted by Stop) wins.
	//tenantscope:allow platform-level scheduler run history.
	_, err := r.coll.UpdateOne(ctx, bson.M{"_id": id, "status": models.StatusRunning}, upd)
	return err
}

func (r *runRepo) MarkIfRunning(ctx context.Context, id, status string, finishedAt time.Time) error {
	//tenantscope:allow platform-level scheduler run history.
	_, err := r.coll.UpdateOne(ctx, bson.M{"_id": id, "status": models.StatusRunning},
		bson.M{"$set": bson.M{"status": status, "finishedAt": finishedAt}})
	return err
}

func (r *runRepo) List(ctx context.Context, key, status string, before *RunCursor, limit int) ([]models.RunRecord, *RunCursor, error) {
	if limit < 1 {
		limit = 1
	}
	if limit > 100 {
		limit = 100
	}
	filter := bson.M{"key": key}
	if status != "" {
		filter["status"] = status
	}
	if before != nil {
		filter["$or"] = bson.A{
			bson.M{"startedAt": bson.M{"$lt": before.StartedAt}},
			bson.M{"startedAt": before.StartedAt, "_id": bson.M{"$lt": before.ID}},
		}
	}
	opts := options.Find().
		SetSort(bson.D{{Key: "startedAt", Value: -1}, {Key: "_id", Value: -1}}).
		SetLimit(int64(limit + 1))
	//tenantscope:allow platform-level scheduler run history.
	cur, err := r.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, nil, err
	}
	var out []models.RunRecord
	if err := cur.All(ctx, &out); err != nil {
		return nil, nil, err
	}
	var next *RunCursor
	if len(out) > limit {
		out = out[:limit]
		last := out[len(out)-1]
		next = &RunCursor{StartedAt: last.StartedAt, ID: last.ID}
	}
	return out, next, nil
}

func (r *runRepo) DeleteOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	//tenantscope:allow platform-level scheduler run history.
	res, err := r.coll.DeleteMany(ctx, bson.M{"startedAt": bson.M{"$lt": cutoff}})
	if err != nil {
		return 0, err
	}
	return res.DeletedCount, nil
}
