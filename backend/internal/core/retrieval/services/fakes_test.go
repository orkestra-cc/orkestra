package services

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/orkestra/backend/internal/core/retrieval/engine"
	"github.com/orkestra/backend/internal/core/retrieval/models"
	"github.com/orkestra/backend/internal/core/retrieval/repository"
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
type fakeModules struct{ started map[string]bool }

func (f *fakeModules) IsStarted(name string) bool { return f.started[name] }

// fakeProfiles serves inference profiles by endpoint name.
type fakeProfiles struct {
	profile *iface.InferenceProfile
	err     error
	calls   int
}

func (f *fakeProfiles) InferenceProfile(context.Context, string) (*iface.InferenceProfile, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	p := *f.profile
	return &p, nil
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

	beforeUpsert       func()
	beforeDeleteOthers func()
}

func newFakeEngine() *fakeEngine {
	return &fakeEngine{collections: map[string]map[string]engine.Point{}}
}

var _ engine.Engine = (*fakeEngine)(nil)

func (f *fakeEngine) log(m string) { f.calls = append(f.calls, m) }

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
	if h := f.beforeUpsert; h != nil {
		h()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log("Upsert")
	c, err := f.coll(collection)
	if err != nil {
		return err
	}
	for _, p := range points {
		if cur, ok := c[p.ID]; ok && cur.Runtime.SourceRevision > p.Runtime.SourceRevision {
			continue
		}
		c[p.ID] = p
	}
	return nil
}

func (f *fakeEngine) UpdatePayload(_ context.Context, collection string, points []engine.Point) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log("UpdatePayload")
	c, err := f.coll(collection)
	if err != nil {
		return err
	}
	for _, p := range points {
		cur, ok := c[p.ID]
		if !ok || cur.Runtime.ContentHash != p.Runtime.ContentHash || cur.Runtime.SourceRevision > p.Runtime.SourceRevision {
			continue
		}
		cur.Runtime = p.Runtime
		cur.Fields = p.Fields
		c[p.ID] = cur
	}
	return nil
}

func (f *fakeEngine) DeleteOtherRevisions(_ context.Context, collection, tenantID, sourceID string, keep int64) error {
	if h := f.beforeDeleteOthers; h != nil {
		h()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log("DeleteOtherRevisions")
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

// points returns a copy of a collection's points, for assertions.
func (f *fakeEngine) points(collection string) []engine.Point {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []engine.Point
	for _, p := range f.collections[collection] {
		out = append(out, p)
	}
	return out
}
