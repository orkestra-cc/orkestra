package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/retrieval/engine"
	"github.com/orkestra/backend/internal/core/retrieval/models"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

const (
	testCorpus = "crm_contacts"
	testColl   = "orkestra_crm_contacts_g1"
	testLease  = 2 * time.Minute
)

type workerEnv struct {
	w        *Worker
	deps     WorkerDeps
	clk      *clock
	eng      *fakeEngine
	outbox   *fakeOutbox
	source   *fakeSource
	emb      *fakeEmbedder
	erasures *fakeErasures
	progress *fakeProgress
	tenants  *fakeTenants
	logs     *lockedBuffer
	spec     iface.RetrievalCorpusSpec
	hash     string
}

func newTestWorker(t *testing.T) *workerEnv {
	t.Helper()
	ing := goldenProfile()
	ing.Chunking = iface.RetrievalChunking{Strategy: "fixed", MaxTokens: 4, Overlap: 1}
	e := &workerEnv{
		clk: newClock(), eng: newFakeEngine(), outbox: newFakeOutbox(), source: newFakeSource(),
		emb: &fakeEmbedder{dims: 8}, erasures: &fakeErasures{}, progress: newFakeProgress(), tenants: &fakeTenants{}, logs: &lockedBuffer{},
	}
	e.spec = iface.RetrievalCorpusSpec{Name: testCorpus, Owner: "crm",
		Filters:   []iface.RetrievalFilterField{{Name: "stage", Type: iface.RetrievalFieldKeyword}},
		Ingestion: ing, Source: e.source}
	e.hash = IngestionProfileHash(ing, "ip_a")
	e.deps = WorkerDeps{
		Outbox: e.outbox, Progress: e.progress, Tenants: e.tenants, States: newFakeStates(), Tombstones: NewTombstones(e.erasures),
		Catalog: &fakeCatalog{specs: []iface.RetrievalCorpusSpec{e.spec}}, Engine: e.eng,
		Profiles: &fakeProfiles{profile: &iface.InferenceProfile{ID: "ip_a", Kind: iface.InferenceKindEmbed,
			Boundary: iface.InferenceBoundaryLocal, Dimensions: 8}},
		Embedder: e.emb,
		Modules:  &fakeModules{started: map[string]bool{"crm": true}},
		Prefix:   "orkestra",
		Settings: func() WorkerSettings {
			return WorkerSettings{Lease: testLease, Batch: 32, PollInterval: 5 * time.Second}
		},
		Now:    e.clk.Now,
		Logger: slog.New(slog.NewTextHandler(e.logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}
	e.w = NewWorker(e.deps)
	return e
}

// newWorker is a second replica sharing every fake and the clock.
func (e *workerEnv) newWorker() *Worker { return NewWorker(e.deps) }

func item(rev int64) iface.RetrievalItem {
	return iface.RetrievalItem{SourceID: "c1", Revision: rev, Text: "a b c d e f g h i j",
		VisibleTo: []string{"tenant:*"}, Subjects: []string{"crm:1"}, Fields: map[string]any{"stage": "lead"}}
}

func (e *workerEnv) enqueue(t *testing.T, rev int64) {
	t.Helper()
	err := e.outbox.Enqueue(tenantCtx(context.Background(), "t1"),
		[]iface.RetrievalIntent{{Corpus: testCorpus, SourceID: "c1", Revision: rev}}, e.clk.Now())
	require.NoError(t, err)
}

func (e *workerEnv) row() models.OutboxEntry { return e.outbox.row("t1", testCorpus, "c1") }

// runNext prepares the corpus, claims one key of tenant t1 and processes it
// for tests that need a controlled interleaving. Safe to call off the test goroutine.
func (e *workerEnv) runNext(w *Worker) (*PassReport, error) {
	ctx := context.Background()
	g, problem, err := w.prepareCorpus(ctx, e.spec)
	if err != nil || g == nil {
		return nil, fmt.Errorf("prepareCorpus: gate=%v problem=%q err=%v", g, problem, err)
	}
	s := w.settings()
	rep := &PassReport{}
	entry, err := w.Outbox.Claim(tenantCtx(ctx, "t1"), w.Now(), []string{testCorpus}, w.owner, s.Lease)
	if err != nil {
		return nil, err
	}
	if entry == nil {
		return rep, nil
	}
	rep.Claimed++
	w.process(ctx, g, entry, s, rep)
	return rep, nil
}

func (e *workerEnv) processNext(t *testing.T, w *Worker) *PassReport {
	t.Helper()
	rep, err := e.runNext(w)
	require.NoError(t, err)
	return rep
}

// pass runs one full worker pass.
func (e *workerEnv) pass(t *testing.T, w *Worker) *PassReport {
	t.Helper()
	rep, err := w.RunOnce(context.Background())
	require.NoError(t, err)
	return &rep
}

func revisions(points []engine.Point) []int64 {
	out := make([]int64, len(points))
	for i, p := range points {
		out[i] = p.Runtime.SourceRevision
	}
	return out
}

func (e *workerEnv) indexRev1(t *testing.T) {
	t.Helper()
	e.source.put("t1", item(1))
	e.enqueue(t, 1)
	rep := e.pass(t, e.w)
	require.Equal(t, 1, rep.Indexed)
	require.Len(t, e.eng.points(testColl), 3)
}

func TestWorker_IndexesNewItem(t *testing.T) {
	e := newTestWorker(t)
	e.source.put("t1", item(1))
	e.enqueue(t, 1)
	claimedAt := e.clk.Now()
	rep := e.pass(t, e.w)
	assert.Equal(t, PassReport{Claimed: 1, Indexed: 1}, *rep)

	pts := e.eng.points(testColl)
	require.Len(t, pts, 3)
	for i, p := range pts {
		assert.Equal(t, engine.PointID("t1", testCorpus, "c1", i), p.ID)
		assert.Equal(t, int64(1), p.Runtime.SourceRevision)
		assert.Equal(t, e.hash, p.Runtime.IngestionProfile)
		assert.Equal(t, map[string]any{"stage": "lead"}, p.Fields)
		assert.Equal(t, []string{"tenant:*"}, p.Runtime.VisibleTo)
		assert.Len(t, p.Dense, 8)
	}

	require.Equal(t, 1, e.emb.callCount())
	req, ctx := e.emb.call(0)
	assert.Equal(t, "retrieval", req.Caller)
	assert.Equal(t, "t1", req.OnBehalfOf)
	assert.Equal(t, "ip_a", req.Profile)
	assert.Equal(t, iface.EmbedInputDocument, req.Kind)
	assert.Equal(t, []string{"a b c d", "d e f g", "g h i j"}, req.Inputs)
	tenant, _ := ctxauth.GetTenantID(ctx)
	assert.Equal(t, "t1", tenant)
	assert.Equal(t, "internal", ctxauth.TenantKindFromContext(ctx))
	dl, ok := ctx.Deadline()
	require.Equal(t, true, ok)
	assert.Equal(t, true, dl.Equal(claimedAt.Add(testLease).Add(-30*time.Second)), dl)

	r := e.row()
	assert.Equal(t, false, r.Dirty)
	assert.Equal(t, "", r.LeaseOwner)
	assert.Nil(t, r.LeaseUntil)
	require.NotNil(t, r.Applied)
	assert.Equal(t, models.AppliedState{Generation: 1, Revision: 1, Chunks: 3, ProfileHash: e.hash,
		IndexedHash: indexedHash(pts)}, *r.Applied)
}

func TestWorker_DeletedItemRemovesAllPoints(t *testing.T) {
	e := newTestWorker(t)
	e.indexRev1(t)
	e.source.remove("t1", "c1")
	e.enqueue(t, 2)
	rep := e.pass(t, e.w)
	assert.Equal(t, 1, rep.Removed)
	assert.Empty(t, e.eng.points(testColl))
	r := e.row()
	assert.Nil(t, r.Applied)
	assert.Equal(t, false, r.Dirty)
	assert.Equal(t, "", r.LeaseOwner)
}

func TestWorker_MissingItemRemovesAllPoints(t *testing.T) {
	e := newTestWorker(t)
	e.indexRev1(t)
	e.source.mu.Lock()
	delete(e.source.items["t1"], "c1") // the owner no longer reports the id at all
	e.source.mu.Unlock()
	e.enqueue(t, 2)
	assert.Equal(t, 1, e.pass(t, e.w).Removed)
	assert.Empty(t, e.eng.points(testColl))
}

func TestWorker_SourceBehindEnqueuedRevisionRetries(t *testing.T) {
	e := newTestWorker(t)
	e.source.put("t1", item(2))
	e.enqueue(t, 3)
	now := e.clk.Now()
	rep := e.pass(t, e.w)
	assert.Equal(t, 1, rep.Rescheduled)
	assert.Equal(t, 0, e.eng.writes())
	assert.Equal(t, 0, e.emb.callCount())
	r := e.row()
	assert.Equal(t, true, r.Dirty)
	assert.Equal(t, 1, r.Attempts)
	assert.Equal(t, codeSourceBehind, r.LastError)
	assert.Equal(t, now.Add(5*time.Second), r.NextAttemptAt)
	assert.Equal(t, "", r.LeaseOwner)
}

func TestWorker_TombstonedSubjectWritesNothingAndDeletesPoints(t *testing.T) {
	e := newTestWorker(t)
	e.indexRev1(t)
	e.erasures.add(models.Erasure{Scope: models.ErasureScopeTenant, TenantID: "t1", Kind: models.ErasureKindSubject, Subject: "crm:1"})
	it := item(2)
	it.Text = "a brand new text"
	e.source.put("t1", it)
	e.enqueue(t, 2)
	rep := e.pass(t, e.w)
	assert.Equal(t, 1, rep.Removed)
	assert.Equal(t, 1, e.emb.callCount())
	assert.Equal(t, 1, e.eng.callCount("Upsert"))
	assert.Equal(t, 0, e.eng.callCount("UpdatePayload"))
	assert.Empty(t, e.eng.points(testColl))
	r := e.row()
	assert.Equal(t, false, r.Dirty)
	assert.Equal(t, "", r.LeaseOwner)
	assert.Nil(t, r.Applied)
}

func TestWorker_ItemTombstoneSkipsLoad(t *testing.T) {
	e := newTestWorker(t)
	e.indexRev1(t)
	loads := e.source.loadCount()
	e.erasures.add(models.Erasure{Scope: models.ErasureScopeTenant, TenantID: "t1", Kind: models.ErasureKindItem, Corpus: testCorpus, SourceID: "c1"})
	e.enqueue(t, 2)
	assert.Equal(t, 1, e.pass(t, e.w).Removed)
	assert.Equal(t, loads, e.source.loadCount())
	assert.Empty(t, e.eng.points(testColl))
}

func TestWorker_SharingChangeUpdatesPayloadWithoutEmbedding(t *testing.T) {
	e := newTestWorker(t)
	e.indexRev1(t)
	it := item(2)
	it.VisibleTo = []string{"user:u1"}
	e.source.put("t1", it)
	e.enqueue(t, 2)
	rep := e.pass(t, e.w)
	assert.Equal(t, PassReport{Claimed: 1, PayloadOnly: 1}, *rep)
	assert.Equal(t, 1, e.emb.callCount())
	assert.Equal(t, 1, e.eng.callCount("Upsert"))
	pts := e.eng.points(testColl)
	require.Len(t, pts, 3)
	for _, p := range pts {
		assert.Equal(t, int64(2), p.Runtime.SourceRevision)
		assert.Equal(t, []string{"user:u1"}, p.Runtime.VisibleTo)
		assert.Len(t, p.Dense, 8)
	}
	r := e.row()
	require.NotNil(t, r.Applied)
	assert.Equal(t, int64(2), r.Applied.Revision)
	assert.Equal(t, false, r.Dirty)
}

func TestWorker_PayloadPathFallsBackWhenPointsMissing(t *testing.T) {
	e := newTestWorker(t)
	e.indexRev1(t)
	e.eng.wipe(testColl)
	it := item(2)
	it.VisibleTo = []string{"user:u1"}
	e.source.put("t1", it)
	e.enqueue(t, 2)
	rep := e.pass(t, e.w)
	assert.Equal(t, PassReport{Claimed: 1, Indexed: 1}, *rep)
	assert.Equal(t, 2, e.emb.callCount())
	pts := e.eng.points(testColl)
	require.Len(t, pts, 3)
	assert.Equal(t, []int64{2, 2, 2}, revisions(pts))
}

func TestWorker_EmbedFailureReschedulesWithBackoff(t *testing.T) {
	e := newTestWorker(t)
	e.source.put("t1", item(1))
	e.enqueue(t, 1)
	start := e.clk.Now()

	e.emb.setErr(fmt.Errorf("gateway: %w", iface.ErrLLMProviderUnavailable))
	assert.Equal(t, 1, e.pass(t, e.w).Rescheduled)
	r := e.row()
	assert.Equal(t, 1, r.Attempts)
	assert.Equal(t, codeEmbed, r.LastError)
	assert.Equal(t, start.Add(5*time.Second), r.NextAttemptAt)
	assert.Equal(t, true, r.Dirty)

	// not due yet
	assert.Equal(t, 0, e.pass(t, e.w).Claimed)

	e.clk.Advance(5 * time.Second)
	assert.Equal(t, 1, e.pass(t, e.w).Rescheduled)
	r = e.row()
	assert.Equal(t, 2, r.Attempts)
	assert.Equal(t, start.Add(15*time.Second), r.NextAttemptAt)

	e.clk.Advance(10 * time.Second)
	e.emb.setErr(iface.ErrLLMBudgetExceeded)
	e.pass(t, e.w)
	r = e.row()
	assert.Equal(t, codeBudget, r.LastError)
	assert.Equal(t, 3, r.Attempts)

	e.clk.Advance(20 * time.Second)
	e.emb.setErr(nil)
	e.emb.setVecLen(7)
	e.pass(t, e.w)
	r = e.row()
	assert.Equal(t, codeEmbed, r.LastError)
	assert.Equal(t, 4, r.Attempts)

	assert.Equal(t, 0, e.eng.writes())
	assert.Empty(t, e.eng.points(testColl))

	// success clears the failure
	e.clk.Advance(40 * time.Second)
	e.emb.setVecLen(0)
	assert.Equal(t, 1, e.pass(t, e.w).Indexed)
	r = e.row()
	assert.Equal(t, 0, r.Attempts)
	assert.Equal(t, "", r.LastError)
}

func TestWorker_EmbedsInBatchesOf32(t *testing.T) {
	e := newTestWorker(t)
	words := make([]string, 0, 200)
	for i := range 200 {
		words = append(words, fmt.Sprintf("w%d", i))
	}
	it := item(1)
	it.Text = strings.Join(words, " ") // 4-token windows, step 3: 67 chunks
	e.source.put("t1", it)
	e.enqueue(t, 1)
	assert.Equal(t, 1, e.pass(t, e.w).Indexed)
	require.Equal(t, 3, e.emb.callCount())
	for i, n := range []int{32, 32, 3} {
		req, _ := e.emb.call(i)
		assert.Len(t, req.Inputs, n)
	}
	assert.Len(t, e.eng.points(testColl), 67)
}

func TestWorker_InvalidItemParksForMaxBackoff(t *testing.T) {
	e := newTestWorker(t)
	it := item(1)
	it.Fields = map[string]any{"undeclared": 1}
	e.source.put("t1", it)
	e.enqueue(t, 1)
	now := e.clk.Now()
	assert.Equal(t, 1, e.pass(t, e.w).Rescheduled)
	r := e.row()
	assert.Equal(t, codeInvalidItem, r.LastError)
	assert.Equal(t, 1, r.Attempts)
	assert.Equal(t, now.Add(30*time.Minute), r.NextAttemptAt)
	assert.Equal(t, 0, e.eng.writes())
	assert.Equal(t, 0, e.emb.callCount())

	logs := e.logs.String()
	assert.Contains(t, logs, "level=WARN")
	assert.Contains(t, logs, "undeclared")
	assert.Contains(t, logs, "code=invalid_item")
	assert.Contains(t, logs, "source=c1")
	for _, secret := range []string{"a b c", "tenant:*", "crm:1", "lead"} {
		assert.Equal(t, false, strings.Contains(logs, secret), secret)
	}
}

func TestWorker_FailureCodes(t *testing.T) {
	cases := map[string]struct {
		arm  func(e *workerEnv)
		code string
	}{
		"tombstone reader": {func(e *workerEnv) { e.erasures.setErr(errors.New("mongo down")) }, codeTombstone},
		"source":           {func(e *workerEnv) { e.source.setErr(errors.New("owner down")) }, codeSource},
		"engine":           {func(e *workerEnv) { e.eng.setWriteErr(engine.ErrUnavailable) }, codeEngine},
		"engine deadline": {func(e *workerEnv) {
			e.eng.setWriteErr(fmt.Errorf("qdrant: %w", context.DeadlineExceeded))
		}, codeDeadline},
		"embed deadline": {func(e *workerEnv) {
			e.emb.setErr(fmt.Errorf("gateway: %w", context.DeadlineExceeded))
		}, codeDeadline},
		"source deadline": {func(e *workerEnv) { e.source.setErr(context.DeadlineExceeded) }, codeDeadline},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e := newTestWorker(t)
			e.source.put("t1", item(1))
			e.enqueue(t, 1)
			tc.arm(e)
			now := e.clk.Now()
			assert.Equal(t, 1, e.pass(t, e.w).Rescheduled)
			r := e.row()
			assert.Equal(t, tc.code, r.LastError)
			assert.Equal(t, 1, r.Attempts)
			assert.Equal(t, true, r.Dirty)
			assert.Equal(t, now.Add(5*time.Second), r.NextAttemptAt)
			assert.Equal(t, "", r.LeaseOwner)
			for _, leak := range []string{"mongo down", "owner down", "qdrant", "gateway"} {
				assert.Equal(t, false, strings.Contains(e.logs.String(), leak), leak)
			}
		})
	}
}

func TestWorker_LostLeaseBeforeAnyWriteRequeues(t *testing.T) {
	e := newTestWorker(t)
	e.source.put("t1", item(1))
	e.enqueue(t, 1)
	w2 := e.newWorker()
	// w1's lease expires while it loads; w2 claims the key before w1 writes.
	e.source.setOnLoad(func() {
		e.source.setOnLoad(nil)
		e.clk.Advance(testLease + time.Second)
		if _, err := e.outbox.Claim(tenantCtx(context.Background(), "t1"), e.clk.Now(), []string{testCorpus}, w2.owner, testLease); err != nil {
			t.Error(err)
		}
	})
	rep := e.pass(t, e.w)
	assert.Equal(t, 1, rep.Requeued)
	assert.Equal(t, 0, rep.Indexed)
	assert.Equal(t, 0, e.eng.writes())
	assert.Equal(t, true, e.row().Dirty)
	assert.Equal(t, w2.owner, e.row().LeaseOwner)
	assert.Equal(t, 0, e.outbox.releasesBy(e.w.owner))
}

func TestBackoff(t *testing.T) {
	for attempts, want := range map[int]time.Duration{
		0: 5 * time.Second, 1: 5 * time.Second, 2: 10 * time.Second, 3: 20 * time.Second,
		9: 21*time.Minute + 20*time.Second, 10: 30 * time.Minute, 50: 30 * time.Minute, 1 << 30: 30 * time.Minute,
	} {
		assert.Equal(t, want, backoff(attempts), attempts)
	}
}

func TestWorker_EnqueueWhileHeldIsAppliedNextPass(t *testing.T) {
	e := newTestWorker(t)
	e.source.put("t1", item(1))
	e.enqueue(t, 1)
	var once sync.Once
	e.emb.setHook(func(iface.PinnedEmbedRequest) {
		once.Do(func() {
			it := item(2)
			it.Text = "k l m"
			e.source.put("t1", it)
			e.enqueue(t, 2)
		})
	})
	assert.Equal(t, 1, e.processNext(t, e.w).Indexed)
	assert.Equal(t, []int64{1, 1, 1}, revisions(e.eng.points(testColl)))
	r := e.row()
	assert.Equal(t, true, r.Dirty)
	assert.Equal(t, int64(2), r.Revision)
	assert.Equal(t, "", r.LeaseOwner)

	assert.Equal(t, 1, e.processNext(t, e.w).Indexed)
	pts := e.eng.points(testColl)
	require.Len(t, pts, 1)
	assert.Equal(t, int64(2), pts[0].Runtime.SourceRevision)
	assert.Equal(t, "k l m", pts[0].Text)
	r = e.row()
	assert.Equal(t, false, r.Dirty)
	assert.Equal(t, int64(2), r.Applied.Revision)
}

// pauseFirst returns a hook that blocks its first caller until resume is
// closed, signalling paused when it starts waiting; later callers pass
// straight through (a sync.Once would make them wait for the first).
func pauseFirst(paused chan<- struct{}, resume <-chan struct{}) func() {
	var first atomic.Bool
	return func() {
		if first.CompareAndSwap(false, true) {
			close(paused)
			<-resume
		}
	}
}

func TestWorker_StaleHolderResumingAfterSuccessor(t *testing.T) {
	for _, name := range []string{"paused before upsert", "paused before delete"} {
		t.Run(name, func(t *testing.T) {
			e := newTestWorker(t)
			w1, w2 := e.w, e.newWorker()
			e.source.put("t1", item(1))
			e.enqueue(t, 1)

			paused, resume := make(chan struct{}), make(chan struct{})
			var unblockOnce sync.Once
			unblock := func() { unblockOnce.Do(func() { close(resume) }) }
			defer unblock() // never leave w1 blocked when an assertion stops the test
			if name == "paused before upsert" {
				e.eng.setBeforeUpsert(pauseFirst(paused, resume))
			} else {
				e.eng.setBeforeDeleteOthers(pauseFirst(paused, resume))
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
			e.enqueue(t, 2)
			e.clk.Advance(testLease + time.Second)
			rep2 := e.processNext(t, w2)
			require.Equal(t, PassReport{Claimed: 1, Indexed: 1}, *rep2)
			require.Equal(t, []int64{2}, revisions(e.eng.points(testColl)))

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
			assert.Equal(t, 0, e.outbox.releasesBy(w1.owner))
			r := e.row()
			assert.Equal(t, true, r.Dirty)
			assert.Equal(t, "", r.LeaseOwner)

			pts := e.eng.points(testColl)
			if name == "paused before upsert" {
				// w1's rev-1 upsert skipped ordinal 0 (rev 2 is newer) and wrote 1 and 2
				require.Len(t, pts, 3)
				assert.Equal(t, []int64{2, 1, 1}, revisions(pts))
				assert.Equal(t, []int{0, 1, 2}, []int{pts[0].Runtime.Ordinal, pts[1].Runtime.Ordinal, pts[2].Runtime.Ordinal})
			} else {
				// ruling R3: w1's DeleteOtherRevisions(keep 1) removed w2's rev-2 point
				assert.Empty(t, pts)
			}

			// the re-queued key is processed again: exactly the newest revision remains
			rep3 := e.processNext(t, w2)
			pts = e.eng.points(testColl)
			require.Len(t, pts, 1)
			assert.Equal(t, int64(2), pts[0].Runtime.SourceRevision)
			assert.Equal(t, "k l m", pts[0].Text)
			r = e.row()
			assert.Equal(t, false, r.Dirty)
			require.NotNil(t, r.Applied)
			assert.Equal(t, int64(2), r.Applied.Revision)
			if name == "paused before upsert" {
				assert.Equal(t, PassReport{Claimed: 1, PayloadOnly: 1}, *rep3)
				assert.Equal(t, 2, e.emb.callCount()) // w1 rev 1, w2 rev 2
			} else {
				assert.Equal(t, PassReport{Claimed: 1, Indexed: 1}, *rep3) // Count mismatch → full path
				assert.Equal(t, 3, e.emb.callCount())
			}
		})
	}
}
