package services

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/retrieval/engine"
	"github.com/orkestra/backend/internal/core/retrieval/engine/qdrant"
	"github.com/orkestra/backend/internal/core/retrieval/models"
	"github.com/orkestra/backend/internal/core/retrieval/repository"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/module"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// pausingEngine wraps a real engine and blocks selected writes through hooks
// (the same deterministic block/signal pattern as the fake engine's hooks).
type pausingEngine struct {
	engine.Engine
	beforeUpsert, beforeDeleteOthers func()
}

func (p *pausingEngine) Upsert(ctx context.Context, collection string, points []engine.Point) error {
	if p.beforeUpsert != nil {
		p.beforeUpsert()
	}
	return p.Engine.Upsert(ctx, collection, points)
}

func (p *pausingEngine) DeleteOtherRevisions(ctx context.Context, collection, tenantID, sourceID string, keep int64) error {
	if p.beforeDeleteOthers != nil {
		p.beforeDeleteOthers()
	}
	return p.Engine.DeleteOtherRevisions(ctx, collection, tenantID, sourceID, keep)
}

// integrationEnv is a workerEnv whose stores and engine are the real MongoDB
// repositories and the real Qdrant driver. The fake source, embedder,
// profiles, modules, tenants and clock are shared with the unit tests.
type integrationEnv struct {
	*workerEnv
	db      *mongo.Database
	eng     engine.Engine
	pausing *pausingEngine
	erasure *repository.Erasures
	coll    string
}

func (e *integrationEnv) newPausingWorker() *Worker { return NewWorker(e.deps) }

// integrationStores skips unless MONGO_TEST_URI, QDRANT_TEST_URL and
// QDRANT_TEST_API_KEY are all set (a skip proves nothing about these paths),
// then connects to MongoDB (pinged, so an unreachable server fails clearly)
// and builds the Qdrant driver. The database is retrieval_<dbPrefix>_<ns> and
// the collection prefix <collPrefix><ns>. Cleanup drops the database.
func integrationStores(t *testing.T, dbPrefix, collPrefix string) (db *mongo.Database, drv *qdrant.Driver, prefix string) {
	t.Helper()
	var vals [3]string
	for i, name := range []string{"MONGO_TEST_URI", "QDRANT_TEST_URL", "QDRANT_TEST_API_KEY"} {
		if vals[i] = os.Getenv(name); vals[i] == "" {
			t.Skipf("%s not set", name)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(vals[0]))
	require.NoError(t, err)
	require.NoError(t, client.Ping(ctx, nil))
	stamp := time.Now().UnixNano()
	db = client.Database(fmt.Sprintf("retrieval_%s_%d", dbPrefix, stamp))
	prefix = fmt.Sprintf("%s%d", collPrefix, stamp)
	drv, err = qdrant.New(qdrant.Config{URL: vals[1], APIKey: vals[2]})
	require.NoError(t, err)
	t.Cleanup(func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer ccancel()
		if err := db.Drop(cctx); err != nil {
			t.Logf("cleanup: drop database: %v", err)
		}
		if err := client.Disconnect(cctx); err != nil {
			t.Logf("cleanup: disconnect: %v", err)
		}
	})
	return db, drv, prefix
}

// dropCollection is the cleanup of a test collection; it runs before the
// database drop (cleanups run last-in first-out).
func dropCollection(t *testing.T, drv engine.Engine, name string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := drv.DropCollection(ctx, name); err != nil {
		t.Logf("cleanup: drop collection %s: %v", name, err)
	}
}

// ensureIndexes creates the declared indexes of each collection.
func ensureIndexes(t *testing.T, db *mongo.Database, specs map[string][]module.IndexSpec) {
	t.Helper()
	for coll, list := range specs {
		var ms []mongo.IndexModel
		for _, s := range list {
			var keys bson.D
			for _, k := range s.OrderedKeys {
				keys = append(keys, bson.E{Key: k.Field, Value: k.Direction})
			}
			ms = append(ms, mongo.IndexModel{Keys: keys, Options: options.Index().SetUnique(s.Unique)})
		}
		_, err := db.Collection(coll).Indexes().CreateMany(context.Background(), ms)
		require.NoError(t, err)
	}
}

// newIntegrationEnv builds the worker integration env (see integrationStores).
func newIntegrationEnv(t *testing.T) *integrationEnv {
	t.Helper()
	db, drv, prefix := integrationStores(t, "it", "it")
	e := &integrationEnv{workerEnv: newTestWorker(t), db: db, eng: drv, coll: engine.CollectionName(prefix, testCorpus, 1)}
	t.Cleanup(func() { dropCollection(t, drv, e.coll) })

	ensureIndexes(t, db, map[string][]module.IndexSpec{
		repository.CollOutbox:         repository.OutboxIndexes(),
		repository.CollTenantProgress: repository.TenantProgressIndexes(),
		repository.CollErasures:       repository.ErasureIndexes(),
	})

	e.erasure = repository.NewErasures(db)
	e.pausing = &pausingEngine{Engine: drv}
	e.deps.Outbox = repository.NewOutbox(db)
	e.deps.Progress = repository.NewTenantProgress(db)
	e.deps.States = repository.NewCorpusStates(db)
	e.deps.Tombstones = NewTombstones(e.erasure)
	e.deps.Engine = e.pausing
	e.deps.Prefix = prefix
	e.w = NewWorker(e.deps)
	return e
}

func (e *integrationEnv) enqueueRev(t *testing.T, rev int64) {
	t.Helper()
	err := e.deps.Outbox.Enqueue(tenantCtx(context.Background(), "t1"),
		[]iface.RetrievalIntent{{Corpus: testCorpus, SourceID: "c1", Revision: rev}}, e.clk.Now())
	require.NoError(t, err)
}

func (e *integrationEnv) outboxRow(t *testing.T) models.OutboxEntry {
	t.Helper()
	var r models.OutboxEntry
	//tenantscope:allow system: test assertion reads a row directly
	err := e.db.Collection(repository.CollOutbox).FindOne(context.Background(),
		bson.M{"tenantId": "t1", "corpus": testCorpus, "sourceId": "c1"}).Decode(&r)
	require.NoError(t, err)
	return r
}

func (e *integrationEnv) count(t *testing.T) int64 {
	t.Helper()
	n, err := e.eng.Count(context.Background(), e.coll, engine.Selector{TenantID: "t1", SourceIDs: []string{"c1"}})
	require.NoError(t, err)
	return n
}

// chunk0Vector is the deterministic embedding of the first input the worker
// sent, i.e. of chunk 0 of the item it indexed first.
func (e *integrationEnv) chunk0Vector(t *testing.T) []float32 {
	t.Helper()
	req, _ := e.emb.call(0)
	res, err := e.emb.EmbedWithProfile(context.Background(), iface.PinnedEmbedRequest{Inputs: req.Inputs[:1]})
	require.NoError(t, err)
	return res.Vectors[0]
}

func (e *integrationEnv) query(t *testing.T, dense []float32) []engine.Candidate {
	t.Helper()
	got, err := e.eng.Query(context.Background(), e.coll, engine.Query{TenantID: "t1", Principals: []string{"tenant:*"},
		Dense: dense, Weights: engine.Weights{Dense: 1}, PerArm: 10, Limit: 10})
	require.NoError(t, err)
	return got
}

func TestIntegration_BootstrapIndexAndTombstone(t *testing.T) {
	e := newIntegrationEnv(t)
	e.tenants.ids = []string{"t1"}
	e.source.put("t1", item(1))

	e.pass(t, e.w)
	n, err := e.eng.Count(context.Background(), e.coll, engine.Selector{TenantID: "t1"})
	require.NoError(t, err) // the collection exists
	assert.Equal(t, int64(0), n)

	// the build pass enqueued c1 (this pass may already have indexed it); drain it
	e.pass(t, e.w)
	assert.Equal(t, int64(3), e.count(t))
	got := e.query(t, e.chunk0Vector(t))
	require.Len(t, got, 3)
	for _, c := range got {
		assert.Equal(t, int64(1), c.Runtime.SourceRevision)
	}

	err = e.erasure.Insert(tenantCtx(context.Background(), "t1"), &models.Erasure{
		Scope: models.ErasureScopeTenant, TenantID: "t1", Kind: models.ErasureKindItem, Corpus: testCorpus, SourceID: "c1"})
	require.NoError(t, err)
	err = e.deps.Outbox.Requeue(tenantCtx(context.Background(), "t1"), testCorpus, "c1", e.clk.Now())
	require.NoError(t, err)
	e.pass(t, e.w)
	assert.Equal(t, int64(0), e.count(t))
}

func TestIntegration_StaleHolderResumingAfterSuccessor(t *testing.T) {
	for _, name := range []string{"paused before upsert", "paused before delete"} {
		t.Run(name, func(t *testing.T) {
			e := newIntegrationEnv(t)
			w1, w2 := e.w, e.newPausingWorker()
			e.source.put("t1", item(1))
			e.enqueueRev(t, 1)

			paused, resume := make(chan struct{}), make(chan struct{})
			var unblockOnce sync.Once
			unblock := func() { unblockOnce.Do(func() { close(resume) }) }
			defer unblock() // never leave w1 blocked when an assertion stops the test
			if name == "paused before upsert" {
				e.pausing.beforeUpsert = pauseFirst(paused, resume)
			} else {
				e.pausing.beforeDeleteOthers = pauseFirst(paused, resume)
			}
			type result struct {
				rep *PassReport
				err error
			}
			done := make(chan result, 1)
			go func() {
				rep, err := e.runNext(w1)
				done <- result{rep, err}
			}()
			select {
			case <-paused:
			case <-time.After(10 * time.Second):
				t.Fatal("w1 never reached the engine write")
			}

			// while w1 is paused: the owner moves to rev 2, w1's lease expires, w2 takes over
			it := item(2)
			it.Text = "k l m"
			e.source.put("t1", it)
			e.enqueueRev(t, 2)
			e.clk.Advance(testLease + time.Second)
			rep2 := e.processNext(t, w2)
			require.Equal(t, PassReport{Claimed: 1, Indexed: 1}, *rep2)
			require.Equal(t, int64(1), e.count(t))

			unblock()
			var res result
			select {
			case res = <-done:
			case <-time.After(10 * time.Second):
				t.Fatal("w1 never finished")
			}
			require.NoError(t, res.err)
			assert.Equal(t, 1, res.rep.Requeued)
			assert.Equal(t, 0, res.rep.Indexed)
			r := e.outboxRow(t)
			assert.Equal(t, true, r.Dirty)
			assert.Equal(t, "", r.LeaseOwner)

			vec := e.chunk0Vector(t)
			if name == "paused before upsert" {
				// Qdrant's conditional upsert kept ordinal 0 at rev 2; ordinals 1-2 are w1's rev 1
				assert.Equal(t, int64(3), e.count(t))
				got := e.query(t, vec)
				require.Len(t, got, 3)
				revs := map[int]int64{}
				for _, c := range got {
					revs[c.Runtime.Ordinal] = c.Runtime.SourceRevision
				}
				assert.Equal(t, map[int]int64{0: 2, 1: 1, 2: 1}, revs)
			} else {
				// w1's DeleteOtherRevisions(keep 1) removed w2's rev-2 point (ruling R3)
				assert.Equal(t, int64(0), e.count(t))
			}

			// the re-queued key is processed again: exactly the newest revision remains
			rep3 := e.processNext(t, w2)
			assert.Equal(t, int64(1), e.count(t))
			got := e.query(t, vec)
			require.Len(t, got, 1)
			assert.Equal(t, int64(2), got[0].Runtime.SourceRevision)
			assert.Equal(t, "k l m", got[0].Text)
			r = e.outboxRow(t)
			assert.Equal(t, false, r.Dirty)
			require.NotNil(t, r.Applied)
			assert.Equal(t, int64(2), r.Applied.Revision)
			if name == "paused before upsert" {
				assert.Equal(t, PassReport{Claimed: 1, PayloadOnly: 1}, *rep3)
			} else {
				assert.Equal(t, PassReport{Claimed: 1, Indexed: 1}, *rep3) // Count mismatch -> full path
			}
		})
	}
}
