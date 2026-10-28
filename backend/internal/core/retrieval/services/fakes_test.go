package services

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"maps"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/orkestra/backend/internal/core/retrieval/engine"
	"github.com/orkestra/backend/internal/core/retrieval/models"
	"github.com/orkestra/backend/internal/core/retrieval/repository"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// clock is a settable fake clock. It starts at the real time, captured once
// (ruling R2): later tasks derive real context deadlines from it.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock() *clock { return &clock{t: time.Now().UTC().Truncate(time.Millisecond)} }

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// fakeModules reports the configured modules as started.
type fakeModules struct {
	mu      sync.Mutex
	started map[string]bool
}

func (f *fakeModules) IsStarted(name string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.started[name]
}

func (f *fakeModules) setStarted(name string, on bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.started[name] = on
}

// fakeProfiles serves inference profiles by endpoint name.
type fakeProfiles struct {
	mu      sync.Mutex
	profile *iface.InferenceProfile
	err     error
	calls   int
}

func (f *fakeProfiles) InferenceProfile(context.Context, string) (*iface.InferenceProfile, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	p := *f.profile
	return &p, nil
}

func (f *fakeProfiles) setErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

// update changes the served profile in place.
func (f *fakeProfiles) update(fn func(p *iface.InferenceProfile)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f.profile)
}

func (f *fakeProfiles) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// fakeStates is an in-memory CorpusStateStore with the repository's
// Insert/Save (compare-and-swap on Version) semantics.
type fakeStates struct {
	mu     sync.Mutex
	states map[string]models.CorpusState
	writes int
}

func newFakeStates() *fakeStates { return &fakeStates{states: map[string]models.CorpusState{}} }

func (f *fakeStates) Get(_ context.Context, corpus string) (*models.CorpusState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	st, ok := f.states[corpus]
	if !ok {
		return nil, nil
	}
	return &st, nil
}

func (f *fakeStates) Insert(_ context.Context, st *models.CorpusState) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.states[st.Corpus]; ok {
		return repository.ErrStateExists
	}
	doc := *st
	doc.Version = 1
	f.states[st.Corpus] = doc
	st.Version = 1
	f.writes++
	return nil
}

func (f *fakeStates) Save(_ context.Context, st *models.CorpusState) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cur, ok := f.states[st.Corpus]
	if !ok || cur.Version != st.Version {
		return repository.ErrStateConflict
	}
	doc := *st
	doc.Version = st.Version + 1
	f.states[st.Corpus] = doc
	st.Version = doc.Version
	f.writes++
	return nil
}

func (f *fakeStates) get(corpus string) models.CorpusState {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.states[corpus]
}

// fakeCatalog is a CorpusCatalog over a fixed list.
type fakeCatalog struct{ specs []iface.RetrievalCorpusSpec }

func (f *fakeCatalog) Corpus(name string) (iface.RetrievalCorpusSpec, bool) {
	for _, s := range f.specs {
		if s.Name == name {
			return s, true
		}
	}
	return iface.RetrievalCorpusSpec{}, false
}

func (f *fakeCatalog) Corpora() []iface.RetrievalCorpusSpec {
	out := append([]iface.RetrievalCorpusSpec(nil), f.specs...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// fakeEngine mirrors the engine.Engine contract in memory:
// collection -> point ID -> point.
type fakeEngine struct {
	mu          sync.Mutex
	collections map[string]map[string]engine.Point
	specs       []engine.CollectionSpec
	calls       []string // method names, in order
	healthErr   error
	ensureErr   error
	writeErr    error // returned by every write method (Upsert, UpdatePayload, Delete*) when set

	// Hooks run outside the lock, before the call takes effect.
	beforeUpsert       func()
	beforeDeleteOthers func()
}

func newFakeEngine() *fakeEngine {
	return &fakeEngine{collections: map[string]map[string]engine.Point{}}
}

var _ engine.Engine = (*fakeEngine)(nil)

func (f *fakeEngine) log(m string) { f.calls = append(f.calls, m) }

func (f *fakeEngine) setEnsureErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ensureErr = err
}

func (f *fakeEngine) setHealthErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.healthErr = err
}

func (f *fakeEngine) setWriteErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writeErr = err
}

func (f *fakeEngine) setBeforeUpsert(h func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.beforeUpsert = h
}

func (f *fakeEngine) setBeforeDeleteOthers(h func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.beforeDeleteOthers = h
}

func (f *fakeEngine) hook(h *func()) func() {
	f.mu.Lock()
	defer f.mu.Unlock()
	return *h
}

// callLog returns a copy of the call log.
func (f *fakeEngine) callLog() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// writes counts the calls that change points.
func (f *fakeEngine) writes() int {
	n := 0
	for _, c := range f.callLog() {
		switch c {
		case "Upsert", "UpdatePayload", "DeleteOtherRevisions", "DeleteByFilter":
			n++
		}
	}
	return n
}

// wipe removes every point of a collection, keeping the collection.
func (f *fakeEngine) wipe(collection string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.collections[collection] != nil {
		f.collections[collection] = map[string]engine.Point{}
	}
}

// copyPoint detaches a point from caller-owned maps and slices.
func copyPoint(p engine.Point) engine.Point {
	p.Dense = append([]float32(nil), p.Dense...)
	p.Runtime = copyRuntime(p.Runtime)
	if p.Fields != nil {
		m := make(map[string]any, len(p.Fields))
		for k, v := range p.Fields {
			if l, ok := v.([]string); ok {
				v = append([]string(nil), l...)
			}
			m[k] = v
		}
		p.Fields = m
	}
	return p
}

func copyRuntime(r engine.Runtime) engine.Runtime {
	r.VisibleTo = append([]string(nil), r.VisibleTo...)
	r.Subjects = append([]string(nil), r.Subjects...)
	return r
}

var errFakeNoVector = errors.New("fake engine: point without a dense vector")

func (f *fakeEngine) callCount(m string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if c == m {
			n++
		}
	}
	return n
}

func (f *fakeEngine) Health(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log("Health")
	return f.healthErr
}

func (f *fakeEngine) EnsureCollection(_ context.Context, spec engine.CollectionSpec) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log("EnsureCollection")
	if f.ensureErr != nil {
		return f.ensureErr
	}
	f.specs = append(f.specs, spec)
	if f.collections[spec.Name] == nil {
		f.collections[spec.Name] = map[string]engine.Point{}
	}
	return nil
}

func (f *fakeEngine) DropCollection(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log("DropCollection")
	delete(f.collections, name)
	return nil
}

func (f *fakeEngine) coll(name string) (map[string]engine.Point, error) {
	c, ok := f.collections[name]
	if !ok {
		return nil, engine.ErrNotFound
	}
	return c, nil
}

func (f *fakeEngine) Upsert(_ context.Context, collection string, points []engine.Point) error {
	if h := f.hook(&f.beforeUpsert); h != nil {
		h()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log("Upsert")
	if f.writeErr != nil {
		return f.writeErr
	}
	c, err := f.coll(collection)
	if err != nil {
		return err
	}
	for _, p := range points { // the whole request is refused, as by a driver
		if p.Dense == nil {
			return errFakeNoVector
		}
	}
	for _, p := range points {
		if cur, ok := c[p.ID]; ok && cur.Runtime.SourceRevision > p.Runtime.SourceRevision {
			continue
		}
		c[p.ID] = copyPoint(p)
	}
	return nil
}

func (f *fakeEngine) UpdatePayload(_ context.Context, collection string, points []engine.Point) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log("UpdatePayload")
	if f.writeErr != nil {
		return f.writeErr
	}
	c, err := f.coll(collection)
	if err != nil {
		return err
	}
	for _, p := range points {
		cur, ok := c[p.ID]
		if !ok || cur.Runtime.ContentHash != p.Runtime.ContentHash || cur.Runtime.SourceRevision > p.Runtime.SourceRevision {
			continue
		}
		np := copyPoint(p)
		cur.Runtime = np.Runtime
		cur.Fields = np.Fields
		c[p.ID] = cur
	}
	return nil
}

func (f *fakeEngine) DeleteOtherRevisions(_ context.Context, collection, tenantID, sourceID string, keep int64) error {
	if h := f.hook(&f.beforeDeleteOthers); h != nil {
		h()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log("DeleteOtherRevisions")
	if f.writeErr != nil {
		return f.writeErr
	}
	c, err := f.coll(collection)
	if err != nil {
		return err
	}
	for id, p := range c {
		if p.Runtime.TenantID == tenantID && p.Runtime.SourceID == sourceID && p.Runtime.SourceRevision != keep {
			delete(c, id)
		}
	}
	return nil
}

var errFakeSelector = errors.New("fake engine: selector refused")

func validSelector(sel engine.Selector) error {
	if sel.IsZero() && sel.SourceIDs == nil {
		return errFakeSelector
	}
	if sel.SourceIDs != nil {
		if len(sel.SourceIDs) == 0 || sel.TenantID == "" {
			return errFakeSelector
		}
		for _, id := range sel.SourceIDs {
			if id == "" {
				return errFakeSelector
			}
		}
	}
	return nil
}

func matches(p engine.Point, sel engine.Selector) bool {
	if sel.TenantID != "" && p.Runtime.TenantID != sel.TenantID {
		return false
	}
	if sel.SourceIDs != nil {
		found := false
		for _, id := range sel.SourceIDs {
			if id == p.Runtime.SourceID {
				found = true
			}
		}
		if !found {
			return false
		}
	}
	if sel.Subject != "" {
		found := false
		for _, s := range p.Runtime.Subjects {
			if s == sel.Subject {
				found = true
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func (f *fakeEngine) DeleteByFilter(_ context.Context, collection string, sel engine.Selector) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log("DeleteByFilter")
	if f.writeErr != nil {
		return f.writeErr
	}
	if err := validSelector(sel); err != nil {
		return err
	}
	c, err := f.coll(collection)
	if err != nil {
		return err
	}
	for id, p := range c {
		if matches(p, sel) {
			delete(c, id)
		}
	}
	return nil
}

func (f *fakeEngine) Count(_ context.Context, collection string, sel engine.Selector) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log("Count")
	if err := validSelector(sel); err != nil {
		return 0, err
	}
	c, err := f.coll(collection)
	if err != nil {
		return 0, err
	}
	var n int64
	for _, p := range c {
		if matches(p, sel) {
			n++
		}
	}
	return n, nil
}

func (f *fakeEngine) Query(context.Context, string, engine.Query) ([]engine.Candidate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log("Query")
	return nil, nil
}

// points returns a copy of a collection's points ordered by source and
// ordinal, for assertions.
func (f *fakeEngine) points(collection string) []engine.Point {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []engine.Point
	for _, p := range f.collections[collection] {
		out = append(out, copyPoint(p))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Runtime.SourceID != out[j].Runtime.SourceID {
			return out[i].Runtime.SourceID < out[j].Runtime.SourceID
		}
		return out[i].Runtime.Ordinal < out[j].Runtime.Ordinal
	})
	return out
}

// fakeErasures is an ErasureReader over a fixed list; it returns every
// stored tombstone and leaves exact matching to Tombstones.
type fakeErasures struct {
	mu    sync.Mutex
	docs  []models.Erasure
	err   error
	calls int
}

func (f *fakeErasures) Covering(context.Context, string, []string, []string) ([]models.Erasure, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return append([]models.Erasure(nil), f.docs...), nil
}

func (f *fakeErasures) add(e models.Erasure) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.docs = append(f.docs, e)
}

func (f *fakeErasures) setErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

func (f *fakeErasures) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// fakeOutbox is an in-memory OutboxStore with repository.Outbox's
// semantics: rows keyed by (tenant from ctx, corpus, source); Enqueue takes
// the $max revision; Claim increments the epoch and clears dirty; Release
// and Reschedule are fenced on epoch and owner (ErrLeaseLost otherwise);
// Release never touches dirty; Requeue never touches the lease.
type fakeOutbox struct {
	mu       sync.Mutex
	rows     map[outboxKey]*models.OutboxEntry
	releases []string // owner of each successful Release
	dueCalls []string // the cursor of each DueTenants call
}

type outboxKey struct{ tenant, corpus, source string }

var _ OutboxStore = (*fakeOutbox)(nil)

func newFakeOutbox() *fakeOutbox { return &fakeOutbox{rows: map[outboxKey]*models.OutboxEntry{}} }

var errFakeNoTenant = errors.New("fake outbox: no tenant in context")

func ctxTenant(ctx context.Context) (string, error) {
	t, ok := ctxauth.GetTenantID(ctx)
	if !ok {
		return "", errFakeNoTenant
	}
	return t, nil
}

func cloneEntry(e *models.OutboxEntry) *models.OutboxEntry {
	c := *e
	if e.LeaseUntil != nil {
		lu := *e.LeaseUntil
		c.LeaseUntil = &lu
	}
	if e.Applied != nil {
		a := *e.Applied
		c.Applied = &a
	}
	return &c
}

func (f *fakeOutbox) Enqueue(ctx context.Context, intents []iface.RetrievalIntent, now time.Time) error {
	t, err := ctxTenant(ctx)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, in := range intents {
		rev := in.Revision
		f.upsertDirty(outboxKey{t, in.Corpus, in.SourceID}, &rev, now)
	}
	return nil
}

func (f *fakeOutbox) Requeue(ctx context.Context, corpus, sourceID string, now time.Time) error {
	t, err := ctxTenant(ctx)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.upsertDirty(outboxKey{t, corpus, sourceID}, nil, now)
	return nil
}

func (f *fakeOutbox) upsertDirty(k outboxKey, revision *int64, now time.Time) {
	now = truncMS(now)
	r, ok := f.rows[k]
	if !ok {
		r = &models.OutboxEntry{TenantID: k.tenant, Corpus: k.corpus, SourceID: k.source, NextAttemptAt: now, CreatedAt: now}
		f.rows[k] = r
	}
	r.Dirty, r.UpdatedAt = true, now
	if revision != nil && *revision > r.Revision {
		r.Revision = *revision
	}
}

func truncMS(t time.Time) time.Time { return t.UTC().Truncate(time.Millisecond) }

func claimableRow(r *models.OutboxEntry, now time.Time, corpora []string) bool {
	now = truncMS(now)
	if !slices.Contains(corpora, r.Corpus) || r.NextAttemptAt.After(now) {
		return false
	}
	return (r.Dirty && r.LeaseUntil == nil) || (r.LeaseUntil != nil && r.LeaseUntil.Before(now))
}

func (f *fakeOutbox) DueTenants(_ context.Context, now time.Time, corpora []string, after string, limit int) ([]string, error) {
	if limit <= 0 {
		return nil, nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dueCalls = append(f.dueCalls, after)
	due := map[string]bool{}
	for k, r := range f.rows {
		if k.tenant > after && claimableRow(r, now, corpora) {
			due[k.tenant] = true
		}
	}
	out := slices.Sorted(maps.Keys(due))
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (f *fakeOutbox) Claim(ctx context.Context, now time.Time, corpora []string, owner string, lease time.Duration) (*models.OutboxEntry, error) {
	t, err := ctxTenant(ctx)
	if err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var best *models.OutboxEntry
	for k, r := range f.rows {
		if k.tenant != t || !claimableRow(r, now, corpora) {
			continue
		}
		if best == nil || r.NextAttemptAt.Before(best.NextAttemptAt) ||
			(r.NextAttemptAt.Equal(best.NextAttemptAt) && r.SourceID < best.SourceID) {
			best = r
		}
	}
	if best == nil {
		return nil, nil
	}
	until := truncMS(now.Add(lease))
	best.Epoch++
	best.Dirty, best.LeaseOwner, best.LeaseUntil, best.UpdatedAt = false, owner, &until, truncMS(now)
	return cloneEntry(best), nil
}

func (f *fakeOutbox) HoldsLease(ctx context.Context, corpus, sourceID string, epoch int64, owner string, now time.Time) (bool, error) {
	t, err := ctxTenant(ctx)
	if err != nil {
		return false, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.rows[outboxKey{t, corpus, sourceID}]
	return ok && r.Epoch == epoch && r.LeaseOwner == owner && r.LeaseUntil != nil && r.LeaseUntil.After(truncMS(now)), nil
}

// fenced returns the row leased by owner at epoch, or ErrLeaseLost.
func (f *fakeOutbox) fenced(ctx context.Context, corpus, sourceID string, epoch int64, owner string) (*models.OutboxEntry, error) {
	t, err := ctxTenant(ctx)
	if err != nil {
		return nil, err
	}
	r, ok := f.rows[outboxKey{t, corpus, sourceID}]
	if !ok || r.Epoch != epoch || r.LeaseOwner != owner {
		return nil, repository.ErrLeaseLost
	}
	return r, nil
}

func (f *fakeOutbox) Release(ctx context.Context, corpus, sourceID string, epoch int64, owner string, applied *models.AppliedState, now time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, err := f.fenced(ctx, corpus, sourceID, epoch, owner)
	if err != nil {
		return err
	}
	r.Attempts, r.UpdatedAt = 0, truncMS(now)
	r.LeaseOwner, r.LeaseUntil, r.LastError = "", nil, ""
	r.Applied = nil
	if applied != nil {
		a := *applied
		r.Applied = &a
	}
	f.releases = append(f.releases, owner)
	return nil
}

func (f *fakeOutbox) Reschedule(ctx context.Context, corpus, sourceID string, epoch int64, owner string, attempts int, next time.Time, code string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, err := f.fenced(ctx, corpus, sourceID, epoch, owner)
	if err != nil {
		return err
	}
	r.Dirty, r.Attempts, r.NextAttemptAt, r.LastError = true, attempts, truncMS(next), code
	r.LeaseOwner, r.LeaseUntil = "", nil
	return nil
}

// row returns a copy of one key's row (zero value when missing).
func (f *fakeOutbox) row(tenant, corpus, sourceID string) models.OutboxEntry {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.rows[outboxKey{tenant, corpus, sourceID}]
	if !ok {
		return models.OutboxEntry{}
	}
	return *cloneEntry(r)
}

// releasesBy counts the successful Release calls of owner.
func (f *fakeOutbox) releasesBy(owner string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, o := range f.releases {
		if o == owner {
			n++
		}
	}
	return n
}

// fakeSource is an owner's RetrievalSource over in-memory items per tenant.
type fakeSource struct {
	mu      sync.Mutex
	items   map[string]map[string]iface.RetrievalItem // tenant -> id -> item
	deleted map[string]map[string]bool
	err     error
	loads   int
	onLoad  func() // runs outside the lock, before the items are read

	enumErr   error
	enumCalls []enumCall
}

var _ iface.RetrievalSource = (*fakeSource)(nil)

func newFakeSource() *fakeSource {
	return &fakeSource{items: map[string]map[string]iface.RetrievalItem{}, deleted: map[string]map[string]bool{}}
}

func copyItem(it iface.RetrievalItem) iface.RetrievalItem {
	it.VisibleTo = append([]string(nil), it.VisibleTo...)
	it.Subjects = append([]string(nil), it.Subjects...)
	if it.Fields != nil {
		m := make(map[string]any, len(it.Fields))
		maps.Copy(m, it.Fields)
		it.Fields = m
	}
	return it
}

func (f *fakeSource) put(tenant string, it iface.RetrievalItem) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.items[tenant] == nil {
		f.items[tenant] = map[string]iface.RetrievalItem{}
	}
	f.items[tenant][it.SourceID] = copyItem(it)
	if f.deleted[tenant] != nil {
		delete(f.deleted[tenant], it.SourceID)
	}
}

func (f *fakeSource) remove(tenant, id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.items[tenant], id)
	if f.deleted[tenant] == nil {
		f.deleted[tenant] = map[string]bool{}
	}
	f.deleted[tenant][id] = true
}

func (f *fakeSource) setErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

func (f *fakeSource) setOnLoad(h func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.onLoad = h
}

func (f *fakeSource) loadCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.loads
}

func (f *fakeSource) Load(_ context.Context, tenantID string, ids []string) ([]iface.RetrievalLoaded, error) {
	f.mu.Lock()
	f.loads++
	h := f.onLoad
	f.mu.Unlock()
	if h != nil {
		h()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	var out []iface.RetrievalLoaded
	for _, id := range ids {
		if it, ok := f.items[tenantID][id]; ok {
			c := copyItem(it)
			out = append(out, iface.RetrievalLoaded{SourceID: id, Item: &c})
		} else if f.deleted[tenantID][id] {
			out = append(out, iface.RetrievalLoaded{SourceID: id, Deleted: true})
		}
	}
	return out, nil
}

// enumCall records one Enumerate call.
type enumCall struct {
	ctxTenant, ctxKind string
	tenant, cursor     string
	limit              int
	hasDeadline        bool
}

func (f *fakeSource) Enumerate(ctx context.Context, tenantID, cursor string, limit int) (iface.RetrievalPage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ct, _ := ctxauth.GetTenantID(ctx)
	ck := ctxauth.TenantKindFromContext(ctx)
	_, dl := ctx.Deadline()
	f.enumCalls = append(f.enumCalls, enumCall{ctxTenant: ct, ctxKind: ck, tenant: tenantID, cursor: cursor, limit: limit, hasDeadline: dl})
	if f.enumErr != nil {
		return iface.RetrievalPage{}, f.enumErr
	}
	ids := make([]string, 0, len(f.items[tenantID]))
	for id := range f.items[tenantID] {
		if id > cursor {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	more := len(ids) > limit
	if more {
		ids = ids[:limit]
	}
	page := iface.RetrievalPage{}
	for _, id := range ids {
		page.Items = append(page.Items, iface.RetrievalEnumerated{SourceID: id, Revision: f.items[tenantID][id].Revision})
	}
	if more {
		page.Next = ids[len(ids)-1]
	}
	return page, nil
}

func (f *fakeSource) setEnumErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.enumErr = err
}

func (f *fakeSource) enumerated() []enumCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]enumCall(nil), f.enumCalls...)
}

func (f *fakeSource) Visible(context.Context, string, iface.RetrievalPrincipalContext, []string) (map[string]iface.RetrievalVisibility, error) {
	return map[string]iface.RetrievalVisibility{}, nil
}

// fakeEmbedder is an LLMPinnedEmbedder returning deterministic vectors of
// dims (or vecLen when set) floats per input.
type fakeEmbedder struct {
	mu     sync.Mutex
	dims   int
	vecLen int // 0: dims
	err    error
	reqs   []iface.PinnedEmbedRequest
	ctxs   []context.Context
	hook   func(req iface.PinnedEmbedRequest) // runs outside the lock, before the result is built
}

var _ iface.LLMPinnedEmbedder = (*fakeEmbedder)(nil)

func (f *fakeEmbedder) EmbedWithProfile(ctx context.Context, req iface.PinnedEmbedRequest) (*iface.EmbedResult, error) {
	req.Inputs = append([]string(nil), req.Inputs...)
	f.mu.Lock()
	f.reqs = append(f.reqs, req)
	f.ctxs = append(f.ctxs, ctx)
	h := f.hook
	f.mu.Unlock()
	if h != nil {
		h(req)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	n := f.dims
	if f.vecLen != 0 {
		n = f.vecLen
	}
	res := &iface.EmbedResult{Dimensions: n}
	for _, in := range req.Inputs {
		sum := sha256.Sum256([]byte(in))
		v := make([]float32, n)
		for i := range v {
			v[i] = float32(sum[i%len(sum)]) / 255
		}
		res.Vectors = append(res.Vectors, v)
	}
	return res, nil
}

func (f *fakeEmbedder) setErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

func (f *fakeEmbedder) setVecLen(n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.vecLen = n
}

func (f *fakeEmbedder) setHook(h func(iface.PinnedEmbedRequest)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hook = h
}

func (f *fakeEmbedder) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.reqs)
}

func (f *fakeEmbedder) call(i int) (iface.PinnedEmbedRequest, context.Context) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reqs[i], f.ctxs[i]
}

// lockedBuffer is a log sink safe for concurrent writers.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// fakeProgress mirrors TenantProgress: ClaimBuild creates the row, returns nil
// when it is done or leased and bumps the epoch; Advance is fenced on epoch
// and owner (ErrLeaseLost), releases the lease and sets done.
type fakeProgress struct {
	mu   sync.Mutex
	rows map[progressKey]*models.TenantProgress
}

type progressKey struct {
	tenant, corpus string
	generation     int
}

var _ ProgressStore = (*fakeProgress)(nil)

func newFakeProgress() *fakeProgress {
	return &fakeProgress{rows: map[progressKey]*models.TenantProgress{}}
}

func (f *fakeProgress) ClaimBuild(ctx context.Context, corpus string, generation int, owner string, now time.Time, lease time.Duration) (*models.TenantProgress, error) {
	t, err := ctxTenant(ctx)
	if err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	k := progressKey{t, corpus, generation}
	r, ok := f.rows[k]
	if !ok {
		r = &models.TenantProgress{TenantID: t, Corpus: corpus, Generation: generation, Status: models.ProgressBuilding, StartedAt: truncMS(now)}
		f.rows[k] = r
	}
	if r.Status == models.ProgressDone || (r.LeaseUntil != nil && r.LeaseUntil.After(truncMS(now))) {
		return nil, nil
	}
	until := truncMS(now.Add(lease))
	r.Epoch++
	r.LeaseOwner, r.LeaseUntil, r.UpdatedAt = owner, &until, truncMS(now)
	c := *r
	return &c, nil
}

func (f *fakeProgress) Advance(ctx context.Context, corpus string, generation int, epoch int64, owner, cursor string, added int64, done bool, now time.Time) error {
	t, err := ctxTenant(ctx)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.rows[progressKey{t, corpus, generation}]
	if !ok || r.Epoch != epoch || r.LeaseOwner != owner {
		return repository.ErrLeaseLost
	}
	r.Cursor, r.UpdatedAt = cursor, truncMS(now)
	r.Enqueued += added
	r.LeaseOwner, r.LeaseUntil = "", nil
	if done {
		r.Status = models.ProgressDone
	}
	return nil
}

func (f *fakeProgress) get(tenant, corpus string, generation int) models.TenantProgress {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r, ok := f.rows[progressKey{tenant, corpus, generation}]; ok {
		return *r
	}
	return models.TenantProgress{}
}

// fakeTenants is an iface.TenantIDLister.
type fakeTenants struct {
	mu   sync.Mutex
	ids  []string
	kind string
}

var _ iface.TenantIDLister = (*fakeTenants)(nil)

func (f *fakeTenants) ListActiveTenantIDs(_ context.Context, kind string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.kind = kind
	return append([]string(nil), f.ids...), nil
}

// outboxCount counts the outbox rows of a tenant and corpus.
func (f *fakeOutbox) rowsOf(tenant, corpus string) []models.OutboxEntry {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []models.OutboxEntry
	for k, r := range f.rows {
		if k.tenant == tenant && k.corpus == corpus {
			out = append(out, *r)
		}
	}
	return out
}

// dueCallsCopy returns the cursor of every DueTenants call so far.
func (f *fakeOutbox) dueCallsCopy() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.dueCalls...)
}

// park moves a key's next attempt, as a backoff would.
func (f *fakeOutbox) park(tenant, corpus, sourceID string, next time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows[outboxKey{tenant, corpus, sourceID}].NextAttemptAt = truncMS(next)
}
