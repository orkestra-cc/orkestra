package services

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/retrieval/models"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

func (e *workerEnv) once(t *testing.T, w *Worker) PassReport { return *e.pass(t, w) }

func (e *workerEnv) enqueueFor(t *testing.T, tenant, id string, rev int64) {
	t.Helper()
	err := e.outbox.Enqueue(tenantCtx(context.Background(), tenant),
		[]iface.RetrievalIntent{{Corpus: testCorpus, SourceID: id, Revision: rev}}, e.clk.Now())
	require.NoError(t, err)
}

func (e *workerEnv) putFor(tenant, id string) {
	it := item(1)
	it.SourceID = id
	e.source.put(tenant, it)
}

func TestWorker_ClaimIsFairAcrossTenants(t *testing.T) {
	e := newTestWorker(t)
	e.tenants.ids = nil // no initial build: it would re-queue the indexed keys
	for i := range 5 {
		id := fmt.Sprintf("k%d", i)
		e.putFor("t1", id)
		e.enqueueFor(t, "t1", id, 1)
	}
	e.clk.Advance(time.Second)
	e.putFor("t2", "z")
	e.enqueueFor(t, "t2", "z", 1)
	e.deps.Settings = func() WorkerSettings { return WorkerSettings{Lease: testLease, Batch: 2} }
	w := e.newWorker()

	rep := e.once(t, w)
	assert.Equal(t, 2, rep.Claimed)
	assert.Equal(t, 2, rep.Indexed)
	clean := 0
	for _, r := range e.outbox.rowsOf("t1", testCorpus) {
		if !r.Dirty {
			clean++
		}
	}
	assert.Equal(t, 1, clean)
	assert.Equal(t, false, e.outbox.row("t2", testCorpus, "z").Dirty)
}

func TestWorker_KeepsIntentsWhileOwnerStopped(t *testing.T) {
	e := newTestWorker(t)
	mods := e.deps.Modules.(*fakeModules)
	e.tenants.ids = nil
	mods.setStarted("crm", false)
	e.source.put("t1", item(1))
	e.enqueue(t, 1)

	rep := e.once(t, e.w)
	assert.Equal(t, 0, rep.Claimed)
	assert.Equal(t, 0, len(e.eng.callLog()))
	assert.Equal(t, 0, e.deps.States.(*fakeStates).writes)
	r := e.row()
	assert.Equal(t, true, r.Dirty)
	assert.Equal(t, 0, r.Attempts)

	mods.setStarted("crm", true)
	rep = e.once(t, e.w)
	assert.Equal(t, 1, rep.Indexed)
	assert.Len(t, e.eng.points(testColl), 3)
	assert.Equal(t, false, e.row().Dirty)
}

func TestWorker_ProfileChangedKeepsIntents(t *testing.T) {
	e := newTestWorker(t)
	e.indexRev1(t)
	before := e.eng.points(testColl)

	cat := e.deps.Catalog.(*fakeCatalog)
	spec := cat.specs[0]
	spec.Ingestion.Chunking = iface.RetrievalChunking{Strategy: "fixed", MaxTokens: 2}
	cat.specs = []iface.RetrievalCorpusSpec{spec}
	e.putFor("t1", "c2")
	e.enqueueFor(t, "t1", "c2", 1)

	rep := e.once(t, e.w)
	assert.Equal(t, 0, rep.Claimed)
	r := e.outbox.row("t1", testCorpus, "c2")
	assert.Equal(t, true, r.Dirty)
	assert.Equal(t, 0, r.Attempts)
	assert.Equal(t, len(before), len(e.eng.points(testColl)))
	assert.Equal(t, []string{"corpus crm_contacts: ingestion profile changed; indexing paused until a new generation is built"}, e.w.Problems())
}

func TestWorker_LocalOnlyCorpusWithHostedEndpointWritesNothing(t *testing.T) {
	e := newTestWorker(t)
	e.deps.Profiles.(*fakeProfiles).update(func(p *iface.InferenceProfile) {
		p.Boundary = iface.InferenceBoundaryHosted
		p.Endpoint = "embed"
	})
	e.source.put("t1", item(1))
	e.enqueue(t, 1)

	rep := e.once(t, e.w)
	assert.Equal(t, 0, rep.Claimed)
	assert.Equal(t, 0, e.eng.callCount("EnsureCollection"))
	assert.Equal(t, 0, e.emb.callCount())
	assert.Equal(t, true, e.row().Dirty)
	require.Len(t, e.w.Problems(), 1)
	assert.Contains(t, e.w.Problems()[0], "local_only corpus uses hosted endpoint")
}

func (e *workerEnv) putMany(tenant string, n int) {
	for i := range n {
		e.putFor(tenant, fmt.Sprintf("i%03d", i))
	}
}

func TestBuild_EnqueuesEveryEnumeratedItemAndResumes(t *testing.T) {
	e := newTestWorker(t)
	e.tenants.ids = []string{"t1"}
	e.putMany("t1", 450)

	rep := e.once(t, e.w)
	assert.Equal(t, 200, rep.Enqueued)
	p := e.progress.get("t1", testCorpus, 1)
	assert.Equal(t, "i199", p.Cursor)
	assert.Equal(t, models.ProgressBuilding, p.Status)

	w2 := e.newWorker()
	rep = e.once(t, w2)
	assert.Equal(t, 200, rep.Enqueued)
	assert.Equal(t, int64(400), e.progress.get("t1", testCorpus, 1).Enqueued)

	rep = e.once(t, w2)
	assert.Equal(t, 50, rep.Enqueued)
	p = e.progress.get("t1", testCorpus, 1)
	assert.Equal(t, models.ProgressDone, p.Status)
	rows := e.outbox.rowsOf("t1", testCorpus)
	require.Len(t, rows, 450)
	for _, r := range rows {
		assert.Equal(t, int64(1), r.Revision)
	}

	n := len(e.source.enumerated())
	rep = e.once(t, w2)
	assert.Equal(t, 0, rep.Enqueued)
	assert.Equal(t, n, len(e.source.enumerated()))
}

func TestBuild_RunsPerInternalTenantUnderItsContext(t *testing.T) {
	e := newTestWorker(t)
	e.tenants.ids = []string{"t1", "t2"}
	e.putFor("t1", "a")
	e.putFor("t2", "b")
	e.once(t, e.w)

	assert.Equal(t, iface.TenantKindInternal, e.tenants.kind)
	calls := e.source.enumerated()
	require.Len(t, calls, 2)
	for i, tn := range []string{"t1", "t2"} {
		assert.Equal(t, tn, calls[i].tenant)
		assert.Equal(t, tn, calls[i].ctxTenant)
		assert.Equal(t, "internal", calls[i].ctxKind)
		assert.Equal(t, 200, calls[i].limit)
		assert.Equal(t, true, calls[i].hasDeadline)
	}
}

func TestBuild_SkipsInvalidEnumeratedIDs(t *testing.T) {
	e := newTestWorker(t)
	e.tenants.ids = []string{"t1"}
	for _, id := range []string{"", "a\x00b", "ok"} {
		e.putFor("t1", id)
	}
	rep := e.once(t, e.w)
	assert.Equal(t, 1, rep.Enqueued)
	rows := e.outbox.rowsOf("t1", testCorpus)
	require.Len(t, rows, 1)
	assert.Equal(t, "ok", rows[0].SourceID)
	p := e.progress.get("t1", testCorpus, 1)
	assert.Equal(t, models.ProgressDone, p.Status)
	assert.Equal(t, int64(1), p.Enqueued)
}

func TestBuild_EnumerateErrorReleasesTheLease(t *testing.T) {
	e := newTestWorker(t)
	e.tenants.ids = []string{"t1"}
	e.putFor("t1", "a")
	e.source.setEnumErr(fmt.Errorf("secret item text"))
	rep, err := e.w.RunOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, rep.Enqueued)
	p := e.progress.get("t1", testCorpus, 1)
	assert.Equal(t, "", p.Cursor)
	assert.Equal(t, models.ProgressBuilding, p.Status)
	assert.Equal(t, "", p.LeaseOwner)
	if s := e.logs.String(); strings.Contains(s, "secret item text") {
		t.Fatalf("log leaks source error: %s", s)
	}
	e.source.setEnumErr(nil)
	assert.Equal(t, 1, e.once(t, e.w).Enqueued)
}

func TestWorker_RunStopsOnCancel(t *testing.T) {
	e := newTestWorker(t)
	passes := make(chan struct{}, 8)
	e.w.afterPass = func() { passes <- struct{}{} }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { e.w.Run(ctx); close(done) }()
	select {
	case <-passes:
	case <-time.After(2 * time.Second):
		t.Fatal("no first pass")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}

// cleanTenants returns the tenants with at least one clean (indexed) key.
func (e *workerEnv) cleanTenants(tenants ...string) map[string]bool {
	out := map[string]bool{}
	for _, tn := range tenants {
		for _, r := range e.outbox.rowsOf(tn, testCorpus) {
			if !r.Dirty {
				out[tn] = true
			}
		}
	}
	return out
}

// With more due tenants than worker_batch, the round-robin cursor reaches
// every tenant within ceil(tenants/batch) passes, whatever the age of the
// others' backlogs (ADR-0023 D11 fairness).
func TestWorker_EveryDueTenantIsServedWithinCeilTenantsOverBatchPasses(t *testing.T) {
	e := newTestWorker(t)
	e.tenants.ids = nil
	tenants := []string{"t1", "t2", "t3", "t4", "t5"}
	for _, tn := range tenants[:4] {
		for i := range 5 {
			id := fmt.Sprintf("k%d", i)
			e.putFor(tn, id)
			e.enqueueFor(t, tn, id, 1)
		}
	}
	e.clk.Advance(time.Hour) // t5's single key is much fresher than every backlog
	e.putFor("t5", "fresh")
	e.enqueueFor(t, "t5", "fresh", 1)
	e.deps.Settings = func() WorkerSettings { return WorkerSettings{Lease: testLease, Batch: 2} }
	w := e.newWorker()

	// t1,t2 | t3,t4 | t5 (the tail of the list: the cursor wraps after it)
	for _, want := range []int{2, 2, 1} {
		assert.Equal(t, want, e.once(t, w).Claimed)
	}
	served := e.cleanTenants(tenants...)
	for _, tn := range tenants {
		assert.Equal(t, true, served[tn], tn)
	}
	assert.Equal(t, false, e.outbox.row("t5", testCorpus, "fresh").Dirty)
	// the cursor advanced to the last tenant served each pass, then wrapped
	// once the tail of the list was served
	e.once(t, w)
	assert.Equal(t, []string{"", "t2", "t4", ""}, e.outbox.dueCallsCopy())
}

// The cursor wraps within the pass when no tenant after it is due any more.
func TestWorker_EmptyTailWrapsTheCursorInTheSamePass(t *testing.T) {
	e := newTestWorker(t)
	e.tenants.ids = nil
	for _, k := range []struct{ tenant, id string }{{"t1", "a"}, {"t1", "b"}, {"t2", "a"}} {
		e.putFor(k.tenant, k.id)
		e.enqueueFor(t, k.tenant, k.id, 1)
	}
	e.deps.Settings = func() WorkerSettings { return WorkerSettings{Lease: testLease, Batch: 1} }
	w := e.newWorker()
	assert.Equal(t, 1, e.once(t, w).Claimed) // t1; the cursor stops at t1

	e.outbox.park("t2", testCorpus, "a", e.clk.Now().Add(time.Hour)) // t2 is no longer due
	assert.Equal(t, 1, e.once(t, w).Claimed)
	assert.Equal(t, []string{"", "t1", ""}, e.outbox.dueCallsCopy())
	for _, r := range e.outbox.rowsOf("t1", testCorpus) {
		assert.Equal(t, false, r.Dirty, r.SourceID)
	}
}
