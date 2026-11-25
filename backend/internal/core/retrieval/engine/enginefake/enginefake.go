// Package enginefake is an in-memory engine.Engine for tests outside the
// engine package. It mirrors the driver contract where a looser fake would let
// a test pass on behaviour Qdrant refuses: selectors fail closed, a missing
// collection is engine.ErrNotFound, Upsert never lowers a revision. Ranking is
// not modelled: Query ignores the filter and both arms and returns matches in
// insertion order with Score 1/(i+1).
package enginefake

import (
	"context"
	"errors"
	"slices"
	"sync"

	"github.com/orkestra/backend/internal/core/retrieval/engine"
)

var (
	errSelector = errors.New("enginefake: selector refused")
	errQuery    = errors.New("enginefake: query refused")
	errNoVector = errors.New("enginefake: point without a dense vector")
	errName     = errors.New("enginefake: empty collection name")
)

const maxQueryLimit = 500

type collection struct {
	order []string // point IDs in insertion order
	pts   map[string]engine.Point
}

func (c *collection) put(p engine.Point) {
	if _, ok := c.pts[p.ID]; !ok {
		c.order = append(c.order, p.ID)
	}
	c.pts[p.ID] = copyPoint(p)
}

func (c *collection) remove(match func(engine.Point) bool) {
	kept := c.order[:0]
	for _, id := range c.order {
		if match(c.pts[id]) {
			delete(c.pts, id)
			continue
		}
		kept = append(kept, id)
	}
	c.order = kept
}

// Engine is safe for concurrent use; points are copied on the way in and out.
type Engine struct {
	mu            sync.Mutex
	colls         map[string]*collection
	deleteErr     error
	countErr      error
	ignoreDeletes bool
}

var _ engine.Engine = (*Engine)(nil)

func New() *Engine { return &Engine{colls: map[string]*collection{}} }

// Seed creates the collection when absent and stores the points by ID
// unconditionally (no revision check, no vector check).
func (e *Engine) Seed(name string, pts ...engine.Point) {
	e.mu.Lock()
	defer e.mu.Unlock()
	c := e.ensure(name)
	for _, p := range pts {
		c.put(p)
	}
}

// FailDeletes makes DeleteByFilter return err; nil clears.
func (e *Engine) FailDeletes(err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.deleteErr = err
}

// FailCounts makes Count return err; nil clears.
func (e *Engine) FailCounts(err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.countErr = err
}

// IgnoreDeletes makes DeleteByFilter return nil and delete nothing.
func (e *Engine) IgnoreDeletes(on bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.ignoreDeletes = on
}

// Points returns copies of the collection's points in insertion order, or nil
// when the collection does not exist.
func (e *Engine) Points(name string) []engine.Point {
	e.mu.Lock()
	defer e.mu.Unlock()
	c, ok := e.colls[name]
	if !ok {
		return nil
	}
	out := make([]engine.Point, 0, len(c.order))
	for _, id := range c.order {
		out = append(out, copyPoint(c.pts[id]))
	}
	return out
}

func (e *Engine) ensure(name string) *collection {
	c, ok := e.colls[name]
	if !ok {
		c = &collection{pts: map[string]engine.Point{}}
		e.colls[name] = c
	}
	return c
}

func (e *Engine) coll(name string) (*collection, error) {
	c, ok := e.colls[name]
	if !ok {
		return nil, engine.ErrNotFound
	}
	return c, nil
}

func (e *Engine) Health(context.Context) error { return nil }

func (e *Engine) EnsureCollection(_ context.Context, spec engine.CollectionSpec) error {
	if spec.Name == "" {
		return errName
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.ensure(spec.Name)
	return nil
}

// DropCollection removes the collection; a missing one is not an error.
func (e *Engine) DropCollection(_ context.Context, name string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.colls, name)
	return nil
}

// Upsert refuses the whole request when a point has no vector, and leaves a
// stored point whose revision is higher than the incoming one unchanged.
func (e *Engine) Upsert(_ context.Context, name string, points []engine.Point) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	c, err := e.coll(name)
	if err != nil {
		return err
	}
	for _, p := range points {
		if p.Dense == nil {
			return errNoVector
		}
	}
	for _, p := range points {
		if cur, ok := c.pts[p.ID]; ok && cur.Runtime.SourceRevision > p.Runtime.SourceRevision {
			continue
		}
		c.put(p)
	}
	return nil
}

// UpdatePayload replaces Runtime and Fields of existing points with an equal
// ContentHash and a stored revision that is not higher; it never creates one.
func (e *Engine) UpdatePayload(_ context.Context, name string, points []engine.Point) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	c, err := e.coll(name)
	if err != nil {
		return err
	}
	for _, p := range points {
		cur, ok := c.pts[p.ID]
		if !ok || cur.Runtime.ContentHash != p.Runtime.ContentHash || cur.Runtime.SourceRevision > p.Runtime.SourceRevision {
			continue
		}
		np := copyPoint(p)
		cur.Runtime, cur.Fields = np.Runtime, np.Fields
		c.pts[p.ID] = cur
	}
	return nil
}

func (e *Engine) DeleteOtherRevisions(_ context.Context, name, tenantID, sourceID string, keep int64) error {
	if tenantID == "" || sourceID == "" {
		return errSelector
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	c, err := e.coll(name)
	if err != nil {
		return err
	}
	c.remove(func(p engine.Point) bool {
		return p.Runtime.TenantID == tenantID && p.Runtime.SourceID == sourceID && p.Runtime.SourceRevision != keep
	})
	return nil
}

// checkSelector applies the driver's refusals (engine.Selector doc), before
// the collection is looked up.
func checkSelector(s engine.Selector) error {
	if s.IsZero() {
		return errSelector
	}
	if s.SourceIDs != nil {
		if len(s.SourceIDs) == 0 || s.TenantID == "" || slices.Contains(s.SourceIDs, "") {
			return errSelector
		}
	}
	return nil
}

func matches(s engine.Selector, p engine.Point) bool {
	if s.TenantID != "" && p.Runtime.TenantID != s.TenantID {
		return false
	}
	if s.SourceIDs != nil && !slices.Contains(s.SourceIDs, p.Runtime.SourceID) {
		return false
	}
	if s.Subject != "" && !slices.Contains(p.Runtime.Subjects, s.Subject) {
		return false
	}
	return true
}

func (e *Engine) DeleteByFilter(_ context.Context, name string, sel engine.Selector) error {
	if err := checkSelector(sel); err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.deleteErr != nil {
		return e.deleteErr
	}
	c, err := e.coll(name)
	if err != nil {
		return err
	}
	if e.ignoreDeletes {
		return nil
	}
	c.remove(func(p engine.Point) bool { return matches(sel, p) })
	return nil
}

func (e *Engine) Count(_ context.Context, name string, sel engine.Selector) (int64, error) {
	if err := checkSelector(sel); err != nil {
		return 0, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.countErr != nil {
		return 0, e.countErr
	}
	c, err := e.coll(name)
	if err != nil {
		return 0, err
	}
	var n int64
	for _, id := range c.order {
		if matches(sel, c.pts[id]) {
			n++
		}
	}
	return n, nil
}

// Query keeps the tenant's points whose VisibleTo intersects Principals and
// returns at most Limit of them in insertion order; the filter is ignored.
func (e *Engine) Query(_ context.Context, name string, q engine.Query) ([]engine.Candidate, error) {
	if q.TenantID == "" || len(q.Principals) == 0 || slices.Contains(q.Principals, "") ||
		q.Limit < 1 || q.Limit > maxQueryLimit {
		return nil, errQuery
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	c, err := e.coll(name)
	if err != nil {
		return nil, err
	}
	var out []engine.Candidate
	for _, id := range c.order {
		p := c.pts[id]
		if p.Runtime.TenantID != q.TenantID || !intersects(p.Runtime.VisibleTo, q.Principals) {
			continue
		}
		cp := copyPoint(p)
		out = append(out, engine.Candidate{
			PointID: cp.ID, Runtime: cp.Runtime, Text: cp.Text, Fields: cp.Fields,
			Score: 1 / float32(len(out)+1),
		})
		if len(out) == q.Limit {
			break
		}
	}
	return out, nil
}

func intersects(a, b []string) bool {
	for _, x := range a {
		if slices.Contains(b, x) {
			return true
		}
	}
	return false
}

// copyPoint detaches a point from caller-owned maps and slices.
func copyPoint(p engine.Point) engine.Point {
	p.Dense = slices.Clone(p.Dense)
	p.Runtime.VisibleTo = slices.Clone(p.Runtime.VisibleTo)
	p.Runtime.Subjects = slices.Clone(p.Runtime.Subjects)
	if p.Fields != nil {
		m := make(map[string]any, len(p.Fields))
		for k, v := range p.Fields {
			if l, ok := v.([]string); ok {
				v = slices.Clone(l)
			}
			m[k] = v
		}
		p.Fields = m
	}
	return p
}
