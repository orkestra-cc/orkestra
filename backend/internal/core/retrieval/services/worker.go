package services

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/orkestra/backend/internal/core/retrieval/chunk"
	"github.com/orkestra/backend/internal/core/retrieval/engine"
	"github.com/orkestra/backend/internal/core/retrieval/models"
	"github.com/orkestra/backend/internal/core/retrieval/repository"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/module"
)

// CorpusStateStore is the corpus state persistence the worker needs;
// satisfied by *repository.CorpusStates.
type CorpusStateStore interface {
	Get(ctx context.Context, corpus string) (*models.CorpusState, error)
	Insert(ctx context.Context, st *models.CorpusState) error
	Save(ctx context.Context, st *models.CorpusState) error
}

// OutboxStore is the outbox the worker drains; satisfied by *repository.Outbox.
type OutboxStore interface {
	OutboxWriter
	DueTenants(ctx context.Context, now time.Time, corpora []string, after string, limit int) ([]string, error)
	Claim(ctx context.Context, now time.Time, corpora []string, owner string, lease time.Duration) (*models.OutboxEntry, error)
	HoldsLease(ctx context.Context, corpus, sourceID string, epoch int64, owner string, now time.Time) (bool, error)
	Release(ctx context.Context, corpus, sourceID string, epoch int64, owner string, applied *models.AppliedState, now time.Time) error
	Reschedule(ctx context.Context, corpus, sourceID string, epoch int64, owner string, attempts int, next time.Time, code string) error
	Requeue(ctx context.Context, corpus, sourceID string, now time.Time) error
}

// ProgressStore is the per-tenant build progress; satisfied by
// *repository.TenantProgress.
type ProgressStore interface {
	ClaimBuild(ctx context.Context, corpus string, generation int, owner string, now time.Time, lease time.Duration) (*models.TenantProgress, error)
	Advance(ctx context.Context, corpus string, generation int, epoch int64, owner, cursor string, added int64, done bool, now time.Time) error
	Get(ctx context.Context, corpus string, generation int) (*models.TenantProgress, error)
}

// TombstoneChecker answers which probed items an erasure covers; satisfied
// by *Tombstones.
type TombstoneChecker interface {
	Covered(ctx context.Context, tenantID, corpus string, probes []Probe) (map[string]bool, error)
}

var (
	_ OutboxStore      = (*repository.Outbox)(nil)
	_ ProgressStore    = (*repository.TenantProgress)(nil)
	_ CorpusStateStore = (*repository.CorpusStates)(nil)
	_ TombstoneChecker = (*Tombstones)(nil)
)

// WorkerSettings are the installation's indexing knobs, clamped on every read.
type WorkerSettings struct {
	Lease        time.Duration
	Batch        int
	PollInterval time.Duration
}

const (
	MinWorkerLease = 30 * time.Second
	MaxWorkerLease = 15 * time.Minute
	// IndexingFencingWindow is how long a stale worker may still write after
	// losing its lease (consumed by erasure, PR 6).
	IndexingFencingWindow = 2 * MaxWorkerLease

	defaultWorkerLease = 2 * time.Minute
	defaultWorkerBatch = 32
	maxWorkerBatch     = 256
	defaultPollEvery   = 5 * time.Second
	minPollEvery       = time.Second
	maxPollEvery       = 5 * time.Minute
)

// ClampWorkerSettings applies defaults to unset values and clamps the rest.
func ClampWorkerSettings(s WorkerSettings) WorkerSettings {
	if s.Lease == 0 {
		s.Lease = defaultWorkerLease
	}
	s.Lease = min(max(s.Lease, MinWorkerLease), MaxWorkerLease)
	if s.Batch <= 0 {
		s.Batch = defaultWorkerBatch
	}
	s.Batch = min(s.Batch, maxWorkerBatch)
	if s.PollInterval == 0 {
		s.PollInterval = defaultPollEvery
	}
	s.PollInterval = min(max(s.PollInterval, minPollEvery), maxPollEvery)
	return s
}

// WorkerDeps are the index worker's collaborators.
type WorkerDeps struct {
	Outbox     OutboxStore
	States     CorpusStateStore
	Progress   ProgressStore
	Tombstones TombstoneChecker
	Catalog    CorpusCatalog
	Engine     engine.Engine
	Profiles   iface.LLMInferenceProfiles
	Embedder   iface.LLMPinnedEmbedder
	Modules    module.ModuleStatusReader
	Tenants    iface.TenantIDLister
	Prefix     string
	Settings   func() WorkerSettings // read every pass (hot reload), clamped
	Now        func() time.Time
	Logger     *slog.Logger
}

// Worker drains the outbox into the vector engine and builds generations.
type Worker struct {
	WorkerDeps
	owner string

	mu       sync.Mutex
	problems []string
	ensured  map[ensureKey]bool // collections ensured by this process
	built    map[buildKey]bool  // initial builds known to be done
	// buildOffset is where the next build pass starts in the sorted
	// (corpus, tenant) list.
	buildOffset int
	// tenantCursor is the round-robin position of the index pass: the last
	// tenant served ("" = start). Per process; replicas keep their own.
	tenantCursor string
	afterPass    func() // test hook: runs after every pass of Run
}

// NewWorker builds a Worker with a fresh owner ID; it performs no I/O.
func NewWorker(d WorkerDeps) *Worker {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	if d.Settings == nil {
		d.Settings = func() WorkerSettings { return WorkerSettings{} }
	}
	return &Worker{WorkerDeps: d, owner: uuid.NewString(), ensured: map[ensureKey]bool{}, built: map[buildKey]bool{}}
}

func (w *Worker) settings() WorkerSettings { return ClampWorkerSettings(w.Settings()) }

// Closed error codes: the only values stored in an outbox row's lastError
// and logged for a failed key.
const (
	codeSource       = "source"        // the owner's Load failed
	codeSourceBehind = "source_behind" // the owner returned an older revision than enqueued
	codeInvalidItem  = "invalid_item"  // the item breaks the module's rules
	codeEmbed        = "embed"         // embedding failed or returned unusable vectors
	codeBudget       = "budget"        // the tenant's LLM budget is exhausted
	codeEngine       = "engine"        // a vector engine call failed
	codeDeadline     = "deadline"      // the write deadline passed
	codeTombstone    = "tombstone"     // the erasure check failed
)

const (
	embedBatch     = 32
	backoffBase    = 5 * time.Second
	backoffMax     = 30 * time.Minute
	maxBackoffStep = 20 // 5s<<20 is far beyond the cap; bounds the shift
)

// backoff is the delay before retry number attempts (≥ 1): 5s·2^(attempts−1),
// capped at 30m.
func backoff(attempts int) time.Duration {
	shift := min(max(attempts-1, 0), maxBackoffStep)
	return min(backoffBase<<shift, backoffMax)
}

// tenantCtx scopes a system job's context to one internal tenant, the way the
// auth middleware does for a request (every retrieval tenant is internal).
// There is no SDK constructor for it, so it sets ctxauth.KeyTenantID directly.
func tenantCtx(ctx context.Context, tenantID string) context.Context {
	return ctxauth.WithTenantKind(context.WithValue(ctx, ctxauth.KeyTenantID, tenantID), "internal")
}

// PassReport counts what one worker pass did.
type PassReport struct {
	Claimed, Indexed, PayloadOnly, Removed, Rescheduled, Requeued, Enqueued int
}

// keyRun is the processing of one claimed outbox key.
type keyRun struct {
	w     *Worker
	g     *corpusGate
	e     *models.OutboxEntry
	rep   *PassReport
	tctx  context.Context // tenant scope: lease guard (bookkeeping derives from it, see bctx)
	wctx  context.Context // tctx bounded by the write deadline: Load, Embed, engine calls
	wrote bool            // an engine write was issued
}

// process applies one claimed key to the active generation under its lease.
// Every engine write is preceded by a lease check at the claim's epoch, and
// whenever the lease turns out lost the key is re-queued, so a stale worker
// that wrote after its successor leaves the key dirty for another pass.
func (w *Worker) process(ctx context.Context, g *corpusGate, e *models.OutboxEntry, s WorkerSettings, rep *PassReport) {
	tctx := tenantCtx(ctx, e.TenantID)
	leaseUntil := w.Now().Add(s.Lease)
	if e.LeaseUntil != nil {
		leaseUntil = *e.LeaseUntil
	}
	wctx, cancel := context.WithDeadline(tctx, leaseUntil.Add(-s.Lease/4))
	defer cancel()
	k := &keyRun{w: w, g: g, e: e, rep: rep, tctx: tctx, wctx: wctx}
	if !k.guard() {
		return
	}
	k.run()
}

func (k *keyRun) run() {
	T, C, id := k.e.TenantID, k.g.Spec.Name, k.e.SourceID

	// 1. an item tombstone covers the key whatever the owner reports
	if covered, ok := k.covered(nil); !ok {
		return
	} else if covered {
		k.remove()
		return
	}

	// 2. load the current item
	loaded, err := k.g.Spec.Source.Load(k.wctx, T, []string{id})
	if err != nil {
		k.reschedule(codeSource, err)
		return
	}
	item, valid := loadedItem(loaded, id)
	if !valid {
		// The reply cannot be trusted for this id; the log names the key only.
		k.reschedule(codeInvalidItem, nil)
		return
	}
	if item == nil {
		k.remove()
		return
	}
	if item.Revision < k.e.Revision {
		k.reschedule(codeSourceBehind, nil)
		return
	}
	it := *item
	it.SourceID = id // the key is authoritative for point identity

	// 3. a subject tombstone covers the item's current subjects
	if covered, ok := k.covered(it.Subjects); !ok {
		return
	} else if covered {
		k.remove()
		return
	}

	// 4. validate
	fields, err := normalizeFields(k.g.Spec.Filters, it.Fields)
	if err == nil {
		err = validateItem(it)
	}
	if err != nil {
		// err names a field key at most, never a value (errInvalidItem).
		k.reschedule(codeInvalidItem, err)
		return
	}

	// 5. chunk and build the points
	chunks, truncated, err := chunk.Split(it.Text, k.g.Spec.Ingestion.Chunking)
	if err != nil {
		k.reschedule(codeInvalidItem, err)
		return
	}
	if truncated {
		k.w.Logger.Warn("retrieval: item text truncated by the chunk bounds",
			"corpus", C, "tenant", T, "source", id, "chunks", len(chunks))
	}
	points := buildPoints(T, C, k.g.ProfileHash, it, chunks, fields)
	if len(points) == 0 {
		k.remove()
		return
	}
	applied := &models.AppliedState{
		Generation: k.g.State.ActiveGeneration, Revision: it.Revision, ProfileHash: k.g.ProfileHash,
		IndexedHash: indexedHash(points), Chunks: len(points),
	}

	// 6. same indexed text as last applied: payload-only update
	if a := k.e.Applied; a != nil && a.Generation == applied.Generation && a.ProfileHash == applied.ProfileHash &&
		a.IndexedHash == applied.IndexedHash && a.Chunks == applied.Chunks {
		done, ok := k.payloadOnly(points, it.Revision)
		if !ok {
			return
		}
		if done {
			if k.release(applied) {
				k.rep.PayloadOnly++
			}
			return
		}
	}

	// 7. embed
	if !k.embed(points) {
		return
	}

	// 8. write, then drop every other revision of the item
	coll := k.g.State.Collection
	if !k.guard() {
		return
	}
	k.wrote = true
	if err := k.w.Engine.Upsert(k.wctx, coll, points); err != nil {
		k.reschedule(codeEngine, err)
		return
	}
	if !k.guard() {
		return
	}
	if err := k.w.Engine.DeleteOtherRevisions(k.wctx, coll, T, id, it.Revision); err != nil {
		k.reschedule(codeEngine, err)
		return
	}
	if k.release(applied) {
		k.rep.Indexed++
	}
}

// loadedItem picks the claimed id's entry from a Load reply: nil when the id
// is absent or Deleted. valid is false when the reply cannot be trusted for
// the id: more than one entry, an entry neither deleted nor carrying an
// item, or an item that names another source.
func loadedItem(loaded []iface.RetrievalLoaded, id string) (item *iface.RetrievalItem, valid bool) {
	var entry *iface.RetrievalLoaded
	for i := range loaded {
		if loaded[i].SourceID != id {
			continue
		}
		if entry != nil {
			return nil, false
		}
		entry = &loaded[i]
	}
	switch {
	case entry == nil || entry.Deleted:
		return nil, true
	case entry.Item == nil:
		return nil, false
	case entry.Item.SourceID != "" && entry.Item.SourceID != id:
		return nil, false
	}
	return entry.Item, true
}

// covered runs the tombstone check; ok is false when the key was rescheduled.
func (k *keyRun) covered(subjects []string) (covered, ok bool) {
	id := k.e.SourceID
	got, err := k.w.Tombstones.Covered(k.wctx, k.e.TenantID, k.g.Spec.Name, []Probe{{SourceID: id, Subjects: subjects}})
	if err != nil {
		k.reschedule(codeTombstone, err)
		return false, false
	}
	return got[id], true
}

// payloadOnly replaces the payload of the stored points. done reports that
// the item now has exactly these points; ok is false when the key was
// rescheduled or re-queued.
func (k *keyRun) payloadOnly(points []engine.Point, revision int64) (done, ok bool) {
	coll, T, id := k.g.State.Collection, k.e.TenantID, k.e.SourceID
	if !k.guard() {
		return false, false
	}
	k.wrote = true
	if err := k.w.Engine.UpdatePayload(k.wctx, coll, points); err != nil {
		k.reschedule(codeEngine, err)
		return false, false
	}
	if !k.guard() {
		return false, false
	}
	if err := k.w.Engine.DeleteOtherRevisions(k.wctx, coll, T, id, revision); err != nil {
		k.reschedule(codeEngine, err)
		return false, false
	}
	n, err := k.w.Engine.Count(k.wctx, coll, engine.Selector{TenantID: T, SourceIDs: []string{id}})
	if err != nil {
		k.reschedule(codeEngine, err)
		return false, false
	}
	return n == int64(len(points)), true
}

// embed fills every point's Dense vector, embedBatch inputs per call; false
// when the key was rescheduled.
func (k *keyRun) embed(points []engine.Point) bool {
	for start := 0; start < len(points); start += embedBatch {
		batch := points[start:min(start+embedBatch, len(points))]
		inputs := make([]string, len(batch))
		for i, p := range batch {
			inputs[i] = p.Text
		}
		res, err := k.w.Embedder.EmbedWithProfile(k.wctx, iface.PinnedEmbedRequest{
			Caller: "retrieval", OnBehalfOf: k.e.TenantID, Profile: k.g.State.EmbeddingProfileID,
			Kind: iface.EmbedInputDocument, Inputs: inputs,
		})
		if err != nil {
			code := codeEmbed
			if errors.Is(err, iface.ErrLLMBudgetExceeded) {
				code = codeBudget
			}
			k.reschedule(code, err)
			return false
		}
		if res == nil || len(res.Vectors) != len(batch) {
			k.reschedule(codeEmbed, nil)
			return false
		}
		for i, v := range res.Vectors {
			if len(v) != k.g.State.Dimensions {
				k.reschedule(codeEmbed, nil)
				return false
			}
			batch[i].Dense = v
		}
	}
	return true
}

// remove deletes every point of the item and releases the key with no
// applied state.
func (k *keyRun) remove() {
	if !k.guard() {
		return
	}
	k.wrote = true
	sel := engine.Selector{TenantID: k.e.TenantID, SourceIDs: []string{k.e.SourceID}}
	if err := k.w.Engine.DeleteByFilter(k.wctx, k.g.State.Collection, sel); err != nil {
		k.reschedule(codeEngine, err)
		return
	}
	if k.release(nil) {
		k.rep.Removed++
	}
}

// bookkeepingTimeout bounds Release, Reschedule and Requeue once the run
// context is done (Stop), so the key's lease still ends cleanly.
const bookkeepingTimeout = 5 * time.Second

// bctx is the context of the bookkeeping calls: the tenant scope without the
// run's cancellation, bounded by bookkeepingTimeout. Stopping the worker
// mid-key then releases or reschedules the key instead of leaving it leased
// until expiry.
func (k *keyRun) bctx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(k.tctx), bookkeepingTimeout)
}

// guard reports whether the worker still holds the key's lease at the
// claim's epoch; otherwise the key is re-queued and processing stops.
func (k *keyRun) guard() bool {
	held, err := k.w.Outbox.HoldsLease(k.tctx, k.g.Spec.Name, k.e.SourceID, k.e.Epoch, k.w.owner, k.w.Now())
	if err == nil && held {
		return true
	}
	if err != nil {
		k.w.Logger.Warn("retrieval: lease check failed", "corpus", k.g.Spec.Name, "tenant", k.e.TenantID, "source", k.e.SourceID)
	}
	k.requeue()
	return false
}

// release ends the lease recording applied; a lost lease re-queues the key.
func (k *keyRun) release(applied *models.AppliedState) bool {
	ctx, cancel := k.bctx()
	defer cancel()
	err := k.w.Outbox.Release(ctx, k.g.Spec.Name, k.e.SourceID, k.e.Epoch, k.w.owner, applied, k.w.Now())
	switch {
	case err == nil:
		return true
	case errors.Is(err, repository.ErrLeaseLost):
		k.requeue()
	default:
		// The lease expires and the key is claimed again.
		k.w.Logger.Warn("retrieval: release failed", "corpus", k.g.Spec.Name, "tenant", k.e.TenantID, "source", k.e.SourceID)
	}
	return false
}

// reschedule ends the lease after a failure, with backoff. err, when set,
// only refines the code; it is never stored or logged.
func (k *keyRun) reschedule(code string, err error) {
	if err != nil && errors.Is(err, context.DeadlineExceeded) {
		code = codeDeadline
	}
	attempts := k.e.Attempts + 1
	delay := backoff(attempts)
	if code == codeInvalidItem {
		delay = backoffMax
	}
	now := k.w.Now()
	args := []any{"corpus", k.g.Spec.Name, "tenant", k.e.TenantID, "source", k.e.SourceID, "code", code, "attempts", attempts}
	if code == codeInvalidItem && err != nil {
		args = append(args, "reason", err.Error()) // names a field key at most (errInvalidItem)
	}
	k.w.Logger.Warn("retrieval: index key rescheduled", args...)
	ctx, cancel := k.bctx()
	defer cancel()
	rerr := k.w.Outbox.Reschedule(ctx, k.g.Spec.Name, k.e.SourceID, k.e.Epoch, k.w.owner, attempts, now.Add(delay), code)
	switch {
	case rerr == nil:
		k.rep.Rescheduled++
	case errors.Is(rerr, repository.ErrLeaseLost):
		// The successor owns the key. If this run already wrote to the engine
		// the write may have landed after the successor's: re-queue so the
		// key is processed again.
		if k.wrote {
			k.requeue()
		}
	default:
		k.w.Logger.Warn("retrieval: reschedule failed", "corpus", k.g.Spec.Name, "tenant", k.e.TenantID, "source", k.e.SourceID, "code", code)
	}
}

// requeue marks the key dirty without touching its lease.
func (k *keyRun) requeue() {
	ctx, cancel := k.bctx()
	defer cancel()
	if err := k.w.Outbox.Requeue(ctx, k.g.Spec.Name, k.e.SourceID, k.w.Now()); err != nil {
		k.w.Logger.Warn("retrieval: requeue failed", "corpus", k.g.Spec.Name, "tenant", k.e.TenantID, "source", k.e.SourceID)
		return
	}
	k.rep.Requeued++
}
