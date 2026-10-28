package main

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	complianceModels "github.com/orkestra/backend/internal/core/compliance/models"
	complianceRepo "github.com/orkestra/backend/internal/core/compliance/repository"
	complianceServices "github.com/orkestra/backend/internal/core/compliance/services"
	"github.com/orkestra/backend/internal/core/retrieval/engine"
	"github.com/orkestra/backend/internal/core/retrieval/engine/enginefake"
	retrievalRepo "github.com/orkestra/backend/internal/core/retrieval/repository"
	retrievalServices "github.com/orkestra/backend/internal/core/retrieval/services"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// e2eDB connects to MONGO_TEST_URI and hands back an isolated database that is
// dropped on cleanup; the test skips when the variable is unset.
func e2eDB(t *testing.T) *mongo.Database {
	t.Helper()
	uri := os.Getenv("MONGO_TEST_URI")
	if uri == "" {
		t.Skip("MONGO_TEST_URI not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	db := client.Database(fmt.Sprintf("e2e_retrieval_erasure_%d", time.Now().UnixNano()))
	t.Cleanup(func() {
		_ = db.Drop(context.Background())
		_ = client.Disconnect(context.Background())
	})
	// The production indexes. None is unique on a tombstone's natural key, so
	// concurrent writers can create a duplicate tombstone (at-least-once); the
	// readers tolerate duplicates.
	var models []mongo.IndexModel
	for _, s := range retrievalRepo.ErasureIndexes() {
		var keys bson.D
		for _, k := range s.OrderedKeys {
			keys = append(keys, bson.E{Key: k.Field, Value: k.Direction})
		}
		models = append(models, mongo.IndexModel{Keys: keys, Options: options.Index().SetUnique(s.Unique)})
	}
	if _, err = db.Collection(retrievalRepo.CollErasures).Indexes().CreateMany(context.Background(), models); err != nil {
		t.Fatalf("create indexes: %v", err)
	}
	return db
}

// e2eClock is a settable clock shared by the finalizer and the PII producer.
type e2eClock struct {
	mu sync.Mutex
	t  time.Time
}

func newE2EClock() *e2eClock { return &e2eClock{t: time.Now().UTC()} }

func (c *e2eClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *e2eClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func check(t *testing.T, ok bool, msg string) {
	t.Helper()
	if !ok {
		t.Fatalf("check failed: %s", msg)
	}
}

func eq[T comparable](t *testing.T, want, got T, msg ...string) {
	t.Helper()
	if want != got {
		t.Fatalf("want %v, got %v %v", want, got, msg)
	}
}

type staticLister struct{ cols map[string][]string }

func (s staticLister) LiveCollections(context.Context) (map[string][]string, error) {
	return s.cols, nil
}

type erasureE2E struct {
	clk *e2eClock
	fin *retrievalServices.Finalizer
	ers *complianceServices.ErasureRequestService
	cfg retrievalServices.FinalizerConfig
}

// newErasureE2E wires retrieval's PII producer into compliance's erasure-request
// workflow over one database. eng may be nil (no overlay).
func newErasureE2E(t *testing.T, eng engine.Engine, cols map[string][]string) *erasureE2E {
	t.Helper()
	db := e2eDB(t)
	logger := discardLogger()
	clk := newE2EClock()
	cfg := retrievalServices.FinalizerConfig{
		Lease: 2 * time.Minute, WriteTimeout: 30 * time.Second,
		Interval: 15 * time.Second, Batch: 50, Hold: 2 * time.Minute,
	}
	store := retrievalRepo.NewErasureFinalization(db)
	logs := retrievalRepo.NewSearchLogDSR(db)
	fin := retrievalServices.NewFinalizer(store, logs, staticLister{cols: cols}, eng,
		func() retrievalServices.FinalizerConfig { return cfg },
		retrievalServices.NewAuditRelay(), clk.Now, logger)
	producers := iface.NewPIIProducerRegistry()
	producers.Register(retrievalServices.NewPIIProducer(store, logs, fin, clk.Now, logger))
	dsr := complianceServices.NewDSRService(producers, nil, logger)
	ers := complianceServices.NewErasureRequestService(complianceRepo.NewErasureRequestRepo(db), dsr, nil, logger)
	return &erasureE2E{clk: clk, fin: fin, ers: ers, cfg: cfg}
}

func e2ePoint(id, tenantID, sourceID string, subjects ...string) engine.Point {
	return engine.Point{
		ID: id, Dense: []float32{1, 0}, Text: id,
		Runtime: engine.Runtime{TenantID: tenantID, SourceID: sourceID, SourceRevision: 1,
			VisibleTo: []string{"tenant:*"}, Subjects: subjects},
	}
}

func hasSubject(pts []engine.Point, subject string) bool {
	for _, p := range pts {
		for _, s := range p.Runtime.Subjects {
			if s == subject {
				return true
			}
		}
	}
	return false
}

func TestRetrievalErasure_RequestIncompleteUntilFinalized(t *testing.T) {
	ctx := context.Background()
	const coll = "orkestra_people_g1"
	eng := enginefake.New()
	eng.Seed(coll, e2ePoint("p1", "tA", "s1", "user:u1"))
	e := newErasureE2E(t, eng, map[string][]string{"people": {coll}})

	req, err := e.ers.Lodge(ctx, "u1", "tA", "test")
	must(t, err)

	out, err := e.ers.Execute(ctx, req.UUID, iface.EraseHardDelete, "op")
	must(t, err)
	eq(t, complianceModels.ErasureRequestIncomplete, out.Request.Status)
	check(t, !hasSubject(eng.Points(coll), "user:u1"), "inline first pass deletes the point")

	// A stale writer lands a point inside the fence.
	eng.Seed(coll, e2ePoint("p2", "tA", "s2", "user:u1"))
	e.clk.Advance(time.Minute)
	out, err = e.ers.Execute(ctx, req.UUID, iface.EraseHardDelete, "op")
	must(t, err)
	eq(t, complianceModels.ErasureRequestIncomplete, out.Request.Status, "fence not over")

	e.clk.Advance(retrievalServices.FencingWindow(e.cfg.Lease, e.cfg.WriteTimeout) + e.cfg.Hold)
	n, err := e.fin.RunOnce(ctx)
	must(t, err)
	eq(t, 1, n)
	check(t, !hasSubject(eng.Points(coll), "user:u1"), "second pass removes the stale point")

	out, err = e.ers.Execute(ctx, req.UUID, iface.EraseHardDelete, "op")
	must(t, err)
	eq(t, complianceModels.ErasureRequestCompleted, out.Request.Status)
}

func TestRetrievalErasure_NoOverlay_RequestCompletesAtOnce(t *testing.T) {
	ctx := context.Background()
	e := newErasureE2E(t, nil, map[string][]string{})

	req, err := e.ers.Lodge(ctx, "u1", "tA", "test")
	must(t, err)
	out, err := e.ers.Execute(ctx, req.UUID, iface.EraseHardDelete, "op")
	must(t, err)
	eq(t, complianceModels.ErasureRequestCompleted, out.Request.Status)
}
