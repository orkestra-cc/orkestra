package services

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/orkestra/backend/internal/core/retrieval/engine"
	"github.com/orkestra/backend/internal/core/retrieval/engine/enginefake"
	"github.com/orkestra/backend/internal/core/retrieval/models"
	"github.com/orkestra/backend/internal/core/retrieval/repository"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// memStore is an in-memory ErasureStore with the claim semantics of
// repository.ErasureFinalization: a claim pushes nextAttemptAt to now+hold
// (ms precision) and every transition is fenced on that value.
type memStore struct {
	mu       sync.Mutex
	rows     map[string]*models.Erasure
	fenceErr error // returned by MarkFencing when set
}

var _ ErasureStore = (*memStore)(nil)

func newMemStore() *memStore { return &memStore{rows: map[string]*models.Erasure{}} }

func msTime(t time.Time) time.Time { return t.UTC().Truncate(time.Millisecond) }

func timePtr(t time.Time) *time.Time { t = msTime(t); return &t }

func cloneErasure(e *models.Erasure) *models.Erasure {
	c := *e
	for _, p := range []**time.Time{&c.FirstPassAt, &c.FenceUntil, &c.DoneAt, &c.NextAttemptAt} {
		if *p != nil {
			v := **p
			*p = &v
		}
	}
	return &c
}

// put stores a row as given (item tombstones, legacy rows).
func (m *memStore) put(e models.Erasure) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows[e.UUID] = cloneErasure(&e)
}

func (m *memStore) ensure(match func(*models.Erasure) bool, row models.Erasure, now time.Time) (*models.Erasure, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.rows {
		if match(e) {
			return cloneErasure(e), false, nil
		}
	}
	row.UUID = uuid.NewString()
	row.Status = models.ErasureStatusPending
	row.CreatedAt = msTime(now)
	row.NextAttemptAt = timePtr(now)
	m.rows[row.UUID] = cloneErasure(&row)
	return cloneErasure(&row), true, nil
}

func (m *memStore) EnsureUser(_ context.Context, userUUID string, now time.Time) (*models.Erasure, bool, error) {
	subject := "user:" + userUUID
	return m.ensure(func(e *models.Erasure) bool {
		return e.Scope == models.ErasureScopeGlobal && e.Kind == models.ErasureKindUser && e.Subject == subject
	}, models.Erasure{Scope: models.ErasureScopeGlobal, Kind: models.ErasureKindUser, Subject: subject}, now)
}

func (m *memStore) EnsureSubject(_ context.Context, tenantID, subject string, now time.Time) (*models.Erasure, bool, error) {
	return m.ensure(func(e *models.Erasure) bool {
		return e.Scope == models.ErasureScopeTenant && e.Kind == models.ErasureKindSubject && e.TenantID == tenantID && e.Subject == subject
	}, models.Erasure{Scope: models.ErasureScopeTenant, Kind: models.ErasureKindSubject, TenantID: tenantID, Subject: subject}, now)
}

func (m *memStore) EnsureTenant(_ context.Context, tenantID string, now time.Time) (*models.Erasure, bool, error) {
	return m.ensure(func(e *models.Erasure) bool {
		return e.Scope == models.ErasureScopeTenant && e.Kind == models.ErasureKindTenant && e.TenantID == tenantID
	}, models.Erasure{Scope: models.ErasureScopeTenant, Kind: models.ErasureKindTenant, TenantID: tenantID}, now)
}

func (m *memStore) ListUser(_ context.Context, userUUID string) ([]models.Erasure, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []models.Erasure
	for _, e := range m.rows {
		if e.Scope == models.ErasureScopeGlobal && e.Kind == models.ErasureKindUser && e.Subject == "user:"+userUUID {
			out = append(out, *cloneErasure(e))
		}
	}
	return out, nil
}

func (m *memStore) Get(_ context.Context, id string) (*models.Erasure, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e, ok := m.rows[id]; ok {
		return cloneErasure(e), nil
	}
	return nil, nil
}

func memDue(e *models.Erasure, now time.Time) bool {
	switch e.Status {
	case "":
		return true
	case models.ErasureStatusPending:
		return e.NextAttemptAt == nil || !e.NextAttemptAt.After(now)
	case models.ErasureStatusFencing:
		return e.NextAttemptAt != nil && !e.NextAttemptAt.After(now)
	}
	return false
}

func (m *memStore) claimLocked(e *models.Erasure, now time.Time, hold time.Duration) *models.Erasure {
	if e.Status == "" {
		e.Status = models.ErasureStatusPending
	}
	e.NextAttemptAt = timePtr(now.Add(hold))
	return cloneErasure(e)
}

func (m *memStore) ClaimDue(_ context.Context, now time.Time, hold time.Duration) (*models.Erasure, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var due []*models.Erasure
	for _, e := range m.rows {
		if memDue(e, now) {
			due = append(due, e)
		}
	}
	if len(due) == 0 {
		return nil, nil
	}
	sort.Slice(due, func(i, j int) bool { // most overdue first; no nextAttemptAt sorts first
		a, b := due[i].NextAttemptAt, due[j].NextAttemptAt
		switch {
		case a == nil:
			return b != nil || due[i].UUID < due[j].UUID
		case b == nil:
			return false
		case !a.Equal(*b):
			return a.Before(*b)
		}
		return due[i].UUID < due[j].UUID
	})
	return m.claimLocked(due[0], now, hold), nil
}

func (m *memStore) ClaimOne(_ context.Context, id string, now time.Time, hold time.Duration) (*models.Erasure, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.rows[id]
	if !ok || !memDue(e, now) {
		return nil, nil
	}
	return m.claimLocked(e, now, hold), nil
}

func (m *memStore) transition(id string, claim time.Time, apply func(*models.Erasure)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.rows[id]
	if !ok || e.NextAttemptAt == nil || !e.NextAttemptAt.Equal(msTime(claim)) {
		return repository.ErrErasureClaimLost
	}
	apply(e)
	return nil
}

func (m *memStore) MarkFencing(_ context.Context, id string, claim, firstPassAt, fenceUntil time.Time) error {
	m.mu.Lock()
	err := m.fenceErr
	m.mu.Unlock()
	if err != nil {
		return err
	}
	return m.transition(id, claim, func(e *models.Erasure) {
		e.Status = models.ErasureStatusFencing
		e.FirstPassAt, e.FenceUntil, e.NextAttemptAt = timePtr(firstPassAt), timePtr(fenceUntil), timePtr(fenceUntil)
	})
}

func (m *memStore) MarkDone(_ context.Context, id string, claim, at time.Time) error {
	return m.transition(id, claim, func(e *models.Erasure) {
		e.Status = models.ErasureStatusDone
		e.DoneAt = timePtr(at)
		if e.FirstPassAt == nil {
			e.FirstPassAt = timePtr(at)
		}
		if e.FenceUntil == nil {
			e.FenceUntil = timePtr(at)
		}
		e.NextAttemptAt = nil
	})
}

func (m *memStore) MarkRetry(_ context.Context, id string, claim, next time.Time) error {
	return m.transition(id, claim, func(e *models.Erasure) {
		e.NextAttemptAt = timePtr(next)
		e.Attempts++
	})
}

func (m *memStore) TenantTombstoneIDs(_ context.Context, tenantIDs []string) (map[string]bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]bool{}
	for _, e := range m.rows {
		if e.Scope == models.ErasureScopeTenant && e.Kind == models.ErasureKindTenant && slices.Contains(tenantIDs, e.TenantID) {
			out[e.TenantID] = true
		}
	}
	return out, nil
}

// staticLister serves a settable collection map.
type staticLister struct {
	mu   sync.Mutex
	cols map[string][]string
	err  error
}

func (s *staticLister) set(cols map[string][]string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cols, s.err = cols, err
}

func (s *staticLister) LiveCollections(context.Context) (map[string][]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return nil, s.err
	}
	out := make(map[string][]string, len(s.cols))
	for k, v := range s.cols {
		out[k] = slices.Clone(v)
	}
	return out, nil
}

// fakeLogs records search-log DSR calls; fail holds failures left per method.
type fakeLogs struct {
	mu    sync.Mutex
	calls []string
	fail  map[string]int
}

var _ SearchLogDSRRepo = (*fakeLogs)(nil)

var errLogsDown = errors.New("fake logs: down")

func (f *fakeLogs) record(method string, args ...string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, method+"("+strings.Join(args, ",")+")")
	if f.fail[method] > 0 {
		f.fail[method]--
		return errLogsDown
	}
	return nil
}

func (f *fakeLogs) setFail(method string, n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail == nil {
		f.fail = map[string]int{}
	}
	f.fail[method] = n
}

func (f *fakeLogs) callLog() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func (f *fakeLogs) ListByUserAllTenants(_ context.Context, userUUID string) ([]models.SearchLog, error) {
	return nil, f.record("ListByUserAllTenants", userUUID)
}

func (f *fakeLogs) DeleteByUserAllTenants(_ context.Context, userUUID string) (int64, error) {
	return 0, f.record("DeleteByUserAllTenants", userUUID)
}

func (f *fakeLogs) DeleteBySubject(_ context.Context, tenantID, subject string) (int64, error) {
	return 0, f.record("DeleteBySubject", tenantID, subject)
}

func (f *fakeLogs) DeleteByTenant(_ context.Context, tenantID string) (int64, error) {
	return 0, f.record("DeleteByTenant", tenantID)
}

// recordingSink keeps every audit event.
type recordingSink struct {
	mu     sync.Mutex
	events []iface.AuditEvent
}

func (r *recordingSink) Emit(_ context.Context, ev iface.AuditEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, ev)
}

func (r *recordingSink) byAction(action string) []iface.AuditEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []iface.AuditEvent
	for _, ev := range r.events {
		if ev.Action == action {
			out = append(out, ev)
		}
	}
	return out
}

func finPoint(id, tenantID, sourceID string, subjects ...string) engine.Point {
	return engine.Point{
		ID: id, Dense: []float32{1, 0}, Text: id,
		Runtime: engine.Runtime{TenantID: tenantID, SourceID: sourceID, SourceRevision: 1, VisibleTo: []string{"tenant:*"}, Subjects: subjects},
	}
}

func testFinalizerConfig() FinalizerConfig {
	return FinalizerConfig{Lease: 2 * time.Minute, WriteTimeout: 30 * time.Second, Interval: 15 * time.Second, Batch: 50, Hold: 2 * time.Minute}
}

type finEnv struct {
	clk   *clock
	t0    time.Time
	store *memStore
	logs  *fakeLogs
	cols  *staticLister
	eng   *enginefake.Engine // nil when the finalizer has no engine
	sink  *recordingSink
	fin   *Finalizer
}

// newFinEnv builds a finalizer over in-memory fakes; withEngine false passes
// a nil engine.Engine (engine not configured).
func newFinEnv(t *testing.T, cols map[string][]string, withEngine bool) *finEnv {
	t.Helper()
	clk := newClock()
	env := &finEnv{clk: clk, t0: clk.Now(), store: newMemStore(), logs: &fakeLogs{}, cols: &staticLister{cols: cols}, sink: &recordingSink{}}
	relay := NewAuditRelay()
	relay.SetAuditSink(env.sink)
	var eng engine.Engine
	if withEngine {
		env.eng = enginefake.New()
		eng = env.eng
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	env.fin = NewFinalizer(env.store, env.logs, env.cols, eng, testFinalizerConfig, relay, clk.Now, logger)
	return env
}

func (e *finEnv) runOnce(t *testing.T, want int) {
	t.Helper()
	n, err := e.fin.RunOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, want, n, "processed")
}

func (e *finEnv) row(t *testing.T, id string) *models.Erasure {
	t.Helper()
	r, err := e.store.Get(context.Background(), id)
	require.NoError(t, err)
	require.NotNil(t, r)
	return r
}

func (e *finEnv) ensureUser(t *testing.T, u string) string {
	t.Helper()
	r, _, err := e.store.EnsureUser(context.Background(), u, e.clk.Now())
	require.NoError(t, err)
	return r.UUID
}

func (e *finEnv) count(t *testing.T, coll string, sel engine.Selector) int64 {
	t.Helper()
	n, err := e.eng.Count(context.Background(), coll, sel)
	require.NoError(t, err)
	return n
}

func TestFencingWindow(t *testing.T) {
	cases := []struct{ lease, write, want time.Duration }{
		{2 * time.Minute, 30 * time.Second, 3 * time.Minute},
		{5 * time.Second, 5 * time.Second, time.Minute},      // floor
		{2 * time.Hour, time.Minute, time.Hour},              // ceiling
		{0, -1, 3 * time.Minute},                             // fallbacks 2m + 30s
		{MaxWorkerLease, 30 * time.Second, 16 * time.Minute}, // production fence (P8)
	}
	for _, c := range cases {
		assert.Equal(t, c.want, FencingWindow(c.lease, c.write), fmt.Sprintf("FencingWindow(%v, %v)", c.lease, c.write))
	}
}

func TestErasureBackoff(t *testing.T) {
	for attempts, want := range map[int]time.Duration{
		0: 15 * time.Second, 1: 15 * time.Second, 2: 30 * time.Second, 4: 2 * time.Minute,
		10: 15 * time.Minute, 1 << 30: 15 * time.Minute, -3: 15 * time.Second,
	} {
		assert.Equal(t, want, ErasureBackoff(attempts), attempts)
	}
}

func TestSelectorFor(t *testing.T) {
	ok := []struct {
		e    models.Erasure
		want engine.Selector
	}{
		{models.Erasure{Kind: models.ErasureKindUser, Scope: models.ErasureScopeGlobal, Subject: "user:u1"}, engine.Selector{Subject: "user:u1"}},
		{models.Erasure{Kind: models.ErasureKindSubject, TenantID: "tA", Subject: "crm:42"}, engine.Selector{TenantID: "tA", Subject: "crm:42"}},
		{models.Erasure{Kind: models.ErasureKindItem, TenantID: "tA", Corpus: "people", SourceID: "s1"}, engine.Selector{TenantID: "tA", SourceIDs: []string{"s1"}}},
		{models.Erasure{Kind: models.ErasureKindTenant, TenantID: "tA"}, engine.Selector{TenantID: "tA"}},
	}
	for _, c := range ok {
		got, err := SelectorFor(c.e)
		require.NoError(t, err)
		assert.Equal(t, c.want, got, c.e.Kind)
	}
	for name, e := range map[string]models.Erasure{
		"unknown kind":        {Kind: "x", TenantID: "tA"},
		"subject, no subject": {Kind: models.ErasureKindSubject, TenantID: "tA"},
		"subject, no tenant":  {Kind: models.ErasureKindSubject, Subject: "crm:42"},
		"user, no subject":    {Kind: models.ErasureKindUser},
		"user, not a user":    {Kind: models.ErasureKindUser, Subject: "crm:42"},
		"user, empty uuid":    {Kind: models.ErasureKindUser, Subject: "user:"},
		"item, no source":     {Kind: models.ErasureKindItem, TenantID: "tA", Corpus: "people"},
		"item, no tenant":     {Kind: models.ErasureKindItem, Corpus: "people", SourceID: "s1"},
		"item, no corpus":     {Kind: models.ErasureKindItem, TenantID: "tA", SourceID: "s1"},
		"tenant, no tenant":   {Kind: models.ErasureKindTenant},
	} {
		if _, err := SelectorFor(e); err == nil {
			t.Errorf("SelectorFor(%s) = nil error", name)
		}
	}
}

func TestFinalizer_FirstPassThenFenceThenDone(t *testing.T) {
	env := newFinEnv(t, map[string][]string{"people": {"c_people_g1", "c_people_g2"}}, true)
	for _, c := range []string{"c_people_g1", "c_people_g2"} {
		env.eng.Seed(c, finPoint(c+"-u1", "tA", "s1", "user:u1"), finPoint(c+"-other", "tA", "s2", "user:u2"))
	}
	id := env.ensureUser(t, "u1")
	t0 := env.t0
	u1 := engine.Selector{Subject: "user:u1"}

	env.runOnce(t, 1)
	r := env.row(t, id)
	assert.Equal(t, models.ErasureStatusFencing, r.Status)
	assert.Equal(t, t0, *r.FirstPassAt)
	assert.Equal(t, t0.Add(3*time.Minute), *r.FenceUntil)
	for _, c := range []string{"c_people_g1", "c_people_g2"} {
		assert.Equal(t, int64(0), env.count(t, c, u1), c)
	}
	assert.Empty(t, env.sink.byAction(AuditErasureFinalized))

	env.clk.Advance(2 * time.Minute)
	env.runOnce(t, 0)

	env.clk.Advance(time.Minute)
	env.runOnce(t, 1)
	r = env.row(t, id)
	assert.Equal(t, models.ErasureStatusDone, r.Status)
	assert.Equal(t, t0.Add(3*time.Minute), *r.DoneAt)
	for _, c := range []string{"c_people_g1", "c_people_g2"} {
		assert.Equal(t, int64(1), env.count(t, c, engine.Selector{Subject: "user:u2"}), c)
	}

	evs := env.sink.byAction(AuditErasureFinalized)
	require.Len(t, evs, 1)
	ev := evs[0]
	assert.Equal(t, "retrieval_erasure", ev.ResourceType)
	assert.Equal(t, id, ev.ResourceID)
	assert.Equal(t, "system", ev.ActorType)
	assert.Equal(t, "success", ev.Outcome)
	assert.Equal(t, "", ev.ActorUserID)
	assert.Equal(t, "user", ev.Metadata["kind"])
	assert.Equal(t, "global", ev.Metadata["scope"])
	assert.Equal(t, 0, ev.Metadata["attempts"])
	assert.Equal(t, 2, ev.Metadata["collections"])
	for k, v := range ev.Metadata {
		if s, ok := v.(string); ok && strings.Contains(s, "u1") {
			t.Errorf("audit metadata %s carries the subject: %q", k, s)
		}
	}

	env.clk.Advance(time.Hour)
	env.runOnce(t, 0) // done is never claimed again
}

func TestFinalizer_StaleWriteDuringFenceRemovedBySecondPass(t *testing.T) {
	env := newFinEnv(t, map[string][]string{"people": {"c_people_g1", "c_people_g2"}}, true)
	env.eng.Seed("c_people_g1", finPoint("p1", "tA", "s1", "user:u1"))
	env.eng.Seed("c_people_g2")
	id := env.ensureUser(t, "u1")
	env.runOnce(t, 1)
	assert.Equal(t, models.ErasureStatusFencing, env.row(t, id).Status)

	env.clk.Advance(time.Minute)
	env.eng.Seed("c_people_g1", finPoint("p1", "tA", "s1", "user:u1")) // a stale writer

	env.clk.Advance(2 * time.Minute)
	env.runOnce(t, 1)
	assert.Equal(t, models.ErasureStatusDone, env.row(t, id).Status)
	assert.Equal(t, int64(0), env.count(t, "c_people_g1", engine.Selector{Subject: "user:u1"}))
}

func TestFinalizer_SecondPassRereadsCollections(t *testing.T) {
	env := newFinEnv(t, map[string][]string{"people": {"g1"}}, true)
	env.eng.Seed("g1", finPoint("p1", "tA", "s1", "user:u1"))
	id := env.ensureUser(t, "u1")
	env.runOnce(t, 1)

	env.cols.set(map[string][]string{"people": {"g1", "g2"}}, nil)
	env.eng.Seed("g2", finPoint("p2", "tA", "s1", "user:u1"))
	env.clk.Advance(3 * time.Minute)
	env.runOnce(t, 1)

	assert.Equal(t, models.ErasureStatusDone, env.row(t, id).Status)
	assert.Equal(t, int64(0), env.count(t, "g2", engine.Selector{Subject: "user:u1"}))
	assert.Equal(t, 2, env.sink.byAction(AuditErasureFinalized)[0].Metadata["collections"])
}

func TestFinalizer_PassOutcomes(t *testing.T) {
	cases := []struct {
		name        string
		setup       func(env *finEnv)
		wantStatus  string
		wantAttempt int
	}{
		{"delete unavailable", func(env *finEnv) { env.eng.FailDeletes(engine.ErrUnavailable) }, models.ErasureStatusPending, 1},
		{"count unavailable", func(env *finEnv) { env.eng.FailCounts(engine.ErrUnavailable) }, models.ErasureStatusPending, 1},
		{"delete not confirmed", func(env *finEnv) { env.eng.IgnoreDeletes(true) }, models.ErasureStatusPending, 1},
		{"collection missing in the engine", func(env *finEnv) {
			env.cols.set(map[string][]string{"people": {"c_people_g1", "c_dropped"}}, nil)
		}, models.ErasureStatusFencing, 0},
		{"lister error", func(env *finEnv) { env.cols.set(nil, errors.New("corpus state down")) }, models.ErasureStatusPending, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env := newFinEnv(t, map[string][]string{"people": {"c_people_g1"}}, true)
			env.eng.Seed("c_people_g1", finPoint("p1", "tA", "s1", "user:u1"))
			id := env.ensureUser(t, "u1")
			c.setup(env)
			env.runOnce(t, 1)

			r := env.row(t, id)
			assert.Equal(t, c.wantStatus, r.Status)
			assert.Equal(t, c.wantAttempt, r.Attempts)
			if c.wantStatus == models.ErasureStatusPending {
				assert.Equal(t, env.t0.Add(15*time.Second), *r.NextAttemptAt)
				assert.Nil(t, r.FirstPassAt)
			}
			assert.Empty(t, env.sink.events)
		})
	}
}

func TestFinalizer_RetryBacksOffThenSucceeds(t *testing.T) {
	env := newFinEnv(t, map[string][]string{"people": {"c_people_g1"}}, true)
	env.eng.Seed("c_people_g1", finPoint("p1", "tA", "s1", "user:u1"))
	id := env.ensureUser(t, "u1")
	env.eng.FailDeletes(engine.ErrUnavailable)
	env.runOnce(t, 1)
	env.clk.Advance(15 * time.Second)
	env.runOnce(t, 1)
	r := env.row(t, id)
	assert.Equal(t, 2, r.Attempts)
	assert.Equal(t, env.t0.Add(45*time.Second), *r.NextAttemptAt) // 15s + 30s

	env.eng.FailDeletes(nil)
	env.clk.Advance(30 * time.Second)
	env.runOnce(t, 1)
	assert.Equal(t, models.ErasureStatusFencing, env.row(t, id).Status)
	env.clk.Advance(3 * time.Minute)
	env.runOnce(t, 1)
	assert.Equal(t, models.ErasureStatusDone, env.row(t, id).Status)
	assert.Equal(t, 2, env.sink.byAction(AuditErasureFinalized)[0].Metadata["attempts"])
}

func TestFinalizer_ItemTombstoneOnlyTouchesItsCorpus(t *testing.T) {
	env := newFinEnv(t, map[string][]string{"people": {"p_g1"}, "notes": {"n_g1"}}, true)
	env.eng.Seed("p_g1", finPoint("p", "tA", "s1"))
	env.eng.Seed("n_g1", finPoint("n", "tA", "s1"))
	// An item tombstone as PR 4 writes it: no finalization fields yet.
	env.store.put(models.Erasure{UUID: "item-1", Scope: models.ErasureScopeTenant, TenantID: "tA", Kind: models.ErasureKindItem,
		Corpus: "people", SourceID: "s1", CreatedAt: env.t0})
	env.runOnce(t, 1)

	sel := engine.Selector{TenantID: "tA", SourceIDs: []string{"s1"}}
	assert.Equal(t, int64(0), env.count(t, "p_g1", sel))
	assert.Equal(t, int64(1), env.count(t, "n_g1", sel))
	assert.Equal(t, models.ErasureStatusFencing, env.row(t, "item-1").Status)
	assert.Empty(t, env.logs.callLog()) // item: no search-log stage

	env.clk.Advance(3 * time.Minute)
	env.runOnce(t, 1)
	assert.Equal(t, 1, env.sink.byAction(AuditErasureFinalized)[0].Metadata["collections"])
}

func TestFinalizer_NoEngineNoCollections_DoneAtOnce(t *testing.T) {
	env := newFinEnv(t, map[string][]string{}, false)
	id := env.ensureUser(t, "u1")
	env.runOnce(t, 1)

	r := env.row(t, id)
	assert.Equal(t, models.ErasureStatusDone, r.Status)
	assert.Equal(t, env.t0, *r.DoneAt)
	assert.Equal(t, env.t0, *r.FirstPassAt)
	assert.Equal(t, env.t0, *r.FenceUntil) // no fencing wait
	assert.Equal(t, []string{"DeleteByUserAllTenants(u1)"}, env.logs.callLog())
	evs := env.sink.byAction(AuditErasureFinalized)
	require.Len(t, evs, 1)
	assert.Equal(t, 0, evs[0].Metadata["collections"])
}

func TestFinalizer_NoEngineWithCollections_StaysPending(t *testing.T) {
	env := newFinEnv(t, map[string][]string{"people": {"c_people_g1"}}, false)
	id := env.ensureUser(t, "u1")
	env.runOnce(t, 1)
	r := env.row(t, id)
	assert.Equal(t, models.ErasureStatusPending, r.Status)
	assert.Equal(t, 1, r.Attempts)

	for i := 0; i < 3; i++ {
		env.clk.Advance(time.Hour)
		env.runOnce(t, 1)
	}
	r = env.row(t, id)
	assert.Equal(t, models.ErasureStatusPending, r.Status)
	assert.Equal(t, 4, r.Attempts)
	assert.Nil(t, r.DoneAt)
	assert.Empty(t, env.sink.events)
}

func TestFinalizer_AttemptRunsOneStepOnlyWhenDue(t *testing.T) {
	env := newFinEnv(t, map[string][]string{"people": {"c_people_g1"}}, true)
	env.eng.Seed("c_people_g1", finPoint("p1", "tA", "s1", "user:u1"))
	id := env.ensureUser(t, "u1")
	ctx := context.Background()

	r, err := env.fin.Attempt(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, r)
	assert.Equal(t, models.ErasureStatusFencing, r.Status)
	assert.Equal(t, int64(0), env.count(t, "c_people_g1", engine.Selector{Subject: "user:u1"}))

	env.eng.Seed("c_people_g1", finPoint("p1", "tA", "s1", "user:u1"))
	env.clk.Advance(time.Minute)
	again, err := env.fin.Attempt(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, r, again)                                                                   // unchanged
	assert.Equal(t, int64(1), env.count(t, "c_people_g1", engine.Selector{Subject: "user:u1"})) // no engine call
	assert.Len(t, env.logs.callLog(), 1)

	missing, err := env.fin.Attempt(ctx, "no-such-row")
	require.NoError(t, err)
	assert.Nil(t, missing)
}

func TestFinalizer_LostClaimIsNotAnError(t *testing.T) {
	env := newFinEnv(t, map[string][]string{}, true)
	env.store.fenceErr = repository.ErrErasureClaimLost
	env.ensureUser(t, "u1")
	env.runOnce(t, 1)
}

func TestFinalizer_LogStepPerKind(t *testing.T) {
	cases := []struct {
		name   string
		ensure func(t *testing.T, env *finEnv)
		want   []string
	}{
		{"user", func(t *testing.T, env *finEnv) { env.ensureUser(t, "u1") }, []string{"DeleteByUserAllTenants(u1)"}},
		{"subject", func(t *testing.T, env *finEnv) {
			_, _, err := env.store.EnsureSubject(context.Background(), "tA", "crm:42", env.t0)
			require.NoError(t, err)
		}, []string{"DeleteBySubject(tA,crm:42)"}},
		{"tenant", func(t *testing.T, env *finEnv) {
			_, _, err := env.store.EnsureTenant(context.Background(), "tA", env.t0)
			require.NoError(t, err)
		}, []string{"DeleteByTenant(tA)"}},
		{"item", func(t *testing.T, env *finEnv) {
			env.store.put(models.Erasure{UUID: "i1", Scope: models.ErasureScopeTenant, TenantID: "tA", Kind: models.ErasureKindItem,
				Corpus: "people", SourceID: "s1", Status: models.ErasureStatusPending, NextAttemptAt: timePtr(env.t0)})
		}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env := newFinEnv(t, map[string][]string{"people": {"c_people_g1"}}, true)
			c.ensure(t, env)
			env.runOnce(t, 1)
			assert.Equal(t, c.want, env.logs.callLog())
		})
	}
}

func TestFinalizer_LogDeleteFailureBlocksTheStep(t *testing.T) {
	env := newFinEnv(t, map[string][]string{"people": {"c_people_g1"}}, true)
	env.eng.Seed("c_people_g1", finPoint("p1", "tA", "s1"))
	r, _, err := env.store.EnsureTenant(context.Background(), "tA", env.t0)
	require.NoError(t, err)
	env.logs.setFail("DeleteByTenant", 1)

	env.runOnce(t, 1)
	row := env.row(t, r.UUID)
	assert.Equal(t, models.ErasureStatusPending, row.Status)
	assert.Equal(t, 1, row.Attempts)
	assert.Equal(t, int64(1), env.count(t, "c_people_g1", engine.Selector{TenantID: "tA"})) // no engine call
	assert.Empty(t, env.sink.events)

	env.clk.Advance(15 * time.Second)
	env.runOnce(t, 1)
	row = env.row(t, r.UUID)
	assert.Equal(t, models.ErasureStatusFencing, row.Status)
	assert.Equal(t, int64(0), env.count(t, "c_people_g1", engine.Selector{TenantID: "tA"}))

	env.clk.Advance(row.FenceUntil.Sub(env.clk.Now()))
	env.runOnce(t, 1)
	assert.Equal(t, models.ErasureStatusDone, env.row(t, r.UUID).Status)
	assert.Equal(t, []string{"DeleteByTenant(tA)", "DeleteByTenant(tA)", "DeleteByTenant(tA)"}, env.logs.callLog())
}

func TestFinalizer_SecondPassDeletesLateLogRows(t *testing.T) {
	env := newFinEnv(t, map[string][]string{"people": {"c_people_g1"}}, true)
	env.eng.Seed("c_people_g1")
	id := env.ensureUser(t, "u1")
	env.runOnce(t, 1)
	assert.Equal(t, []string{"DeleteByUserAllTenants(u1)"}, env.logs.callLog())

	env.clk.Advance(3 * time.Minute)
	env.runOnce(t, 1)
	assert.Equal(t, models.ErasureStatusDone, env.row(t, id).Status)
	assert.Equal(t, []string{"DeleteByUserAllTenants(u1)", "DeleteByUserAllTenants(u1)"}, env.logs.callLog())
}

func TestFinalizer_MalformedRowIsRetriedWithoutAnyDelete(t *testing.T) {
	env := newFinEnv(t, map[string][]string{"people": {"c_people_g1"}}, true)
	env.eng.Seed("c_people_g1", finPoint("p1", "tA", "s1", "user:"))
	// A user row without a uuid would widen the search-log delete to userUuid "".
	env.store.put(models.Erasure{UUID: "bad", Scope: models.ErasureScopeGlobal, Kind: models.ErasureKindUser, Subject: "user:",
		Status: models.ErasureStatusPending, NextAttemptAt: timePtr(env.t0)})
	env.runOnce(t, 1)
	r := env.row(t, "bad")
	assert.Equal(t, models.ErasureStatusPending, r.Status)
	assert.Equal(t, 1, r.Attempts)
	assert.Empty(t, env.logs.callLog())
	assert.Equal(t, 1, len(env.eng.Points("c_people_g1")))
}

func TestFinalizer_RunOnceHonoursBatch(t *testing.T) {
	env := newFinEnv(t, map[string][]string{}, false)
	cfg := testFinalizerConfig()
	cfg.Batch = 2
	env.fin = NewFinalizer(env.store, env.logs, env.cols, nil, func() FinalizerConfig { return cfg }, NewAuditRelay(), env.clk.Now, nil)
	for _, u := range []string{"u1", "u2", "u3"} {
		env.ensureUser(t, u)
	}
	env.runOnce(t, 2)
	env.runOnce(t, 1)
	env.runOnce(t, 0)
}

func TestFinalizer_RunOnceReturnsClaimError(t *testing.T) {
	env := newFinEnv(t, map[string][]string{}, false)
	boom := errors.New("mongo down")
	env.fin = NewFinalizer(claimFailStore{env.store, boom}, env.logs, env.cols, nil, testFinalizerConfig, NewAuditRelay(), env.clk.Now, nil)
	n, err := env.fin.RunOnce(context.Background())
	assert.Equal(t, 0, n)
	if !errors.Is(err, boom) {
		t.Fatalf("RunOnce error = %v, want %v", err, boom)
	}
}

type claimFailStore struct {
	*memStore
	err error
}

func (s claimFailStore) ClaimDue(context.Context, time.Time, time.Duration) (*models.Erasure, error) {
	return nil, s.err
}

func TestFinalizer_RunTicksUntilCanceled(t *testing.T) {
	env := newFinEnv(t, map[string][]string{}, false)
	cfg := testFinalizerConfig()
	cfg.Interval = 10 * time.Millisecond
	env.fin = NewFinalizer(env.store, env.logs, env.cols, nil, func() FinalizerConfig { return cfg }, NewAuditRelay(), env.clk.Now, nil)
	id := env.ensureUser(t, "u1")

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() { env.fin.Run(ctx); close(stopped) }()
	deadline := time.Now().Add(5 * time.Second)
	for env.row(t, id).Status != models.ErasureStatusDone {
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("Run never finalized the row")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}

// The finalizer against the real repository: claim values round-trip through
// Mongo (ms precision) and fence every transition.
func TestFinalizer_MongoStoreTwoPasses(t *testing.T) {
	db := newServicesMongoDB(t)
	ctx := context.Background()
	store := repository.NewErasureFinalization(db)
	logs := repository.NewSearchLogDSR(db)
	clk := newClock()
	t0 := clk.Now()
	eng := enginefake.New()
	eng.Seed("c_people_g1", finPoint("p1", "tA", "s1", "user:u1"))
	cols := &staticLister{cols: map[string][]string{"people": {"c_people_g1"}}}
	sink := &recordingSink{}
	relay := NewAuditRelay()
	relay.SetAuditSink(sink)
	fin := NewFinalizer(store, logs, cols, eng, testFinalizerConfig, relay, clk.Now, nil)

	//tenantscope:allow system: test fixture seeds a search-log row directly
	_, err := db.Collection(repository.CollSearchLog).InsertOne(ctx, models.SearchLog{UUID: "l1", TenantID: "tA", UserUUID: "u1", CreatedAt: t0})
	require.NoError(t, err)
	e, _, err := store.EnsureUser(ctx, "u1", t0)
	require.NoError(t, err)

	n, err := fin.RunOnce(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	r, err := store.Get(ctx, e.UUID)
	require.NoError(t, err)
	assert.Equal(t, models.ErasureStatusFencing, r.Status)
	assert.Equal(t, t0.Add(3*time.Minute), r.FenceUntil.UTC())
	left, err := logs.ListByUserAllTenants(ctx, "u1")
	require.NoError(t, err)
	assert.Empty(t, left)
	assert.Empty(t, eng.Points("c_people_g1"))

	clk.Advance(3 * time.Minute)
	n, err = fin.RunOnce(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	r, err = store.Get(ctx, e.UUID)
	require.NoError(t, err)
	assert.Equal(t, models.ErasureStatusDone, r.Status)
	assert.Len(t, sink.byAction(AuditErasureFinalized), 1)
}
