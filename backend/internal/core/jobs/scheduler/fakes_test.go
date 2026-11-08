package scheduler

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/orkestra/backend/internal/core/jobs/models"
	"github.com/orkestra/backend/internal/core/jobs/repository"
)

// fakeClock is a manually advanced clock, safe for concurrent use.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock(t time.Time) *fakeClock { return &fakeClock{t: t} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// fakeStates mirrors the semantics of the Mongo StateRepository in memory.
type fakeStates struct {
	mu       sync.Mutex
	docs     map[string]models.State
	dueErr   error
	claimErr error
	// renew, when set, decides the outcome of every Renew call.
	renew       func() (bool, error)
	reschedules int
}

func newFakeStates() *fakeStates { return &fakeStates{docs: map[string]models.State{}} }

func leaseFree(s models.State, now time.Time) bool {
	return s.Lease == nil || s.Lease.Until.Before(now)
}

func (f *fakeStates) put(s models.State) {
	f.mu.Lock()
	f.docs[s.Key] = s
	f.mu.Unlock()
}

func (f *fakeStates) get(key string) models.State {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.docs[key]
}

func (f *fakeStates) setDueErr(err error) {
	f.mu.Lock()
	f.dueErr = err
	f.mu.Unlock()
}

func (f *fakeStates) EnsureState(_ context.Context, key, module, fingerprint string, nextRunAt *time.Time, now time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.docs[key]; ok {
		return nil
	}
	f.docs[key] = models.State{Key: key, Module: module, Fingerprint: fingerprint, NextRunAt: nextRunAt, UpdatedAt: now}
	return nil
}

func (f *fakeStates) Get(_ context.Context, key string) (*models.State, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.docs[key]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return &s, nil
}

func (f *fakeStates) List(context.Context) ([]models.State, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]models.State, 0, len(f.docs))
	for _, s := range f.docs {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

func (f *fakeStates) Reschedule(_ context.Context, key string, revision int64, fingerprint string, nextRunAt *time.Time, now time.Time) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.docs[key]
	if !ok || s.Revision != revision {
		return false, nil
	}
	f.reschedules++
	s.Fingerprint, s.UpdatedAt, s.NextRunAt = fingerprint, now, nextRunAt
	if nextRunAt == nil {
		s.RetryAt, s.RetrySlotAt, s.Attempt = nil, nil, 0
	}
	s.Revision++
	f.docs[key] = s
	return true, nil
}

func (f *fakeStates) SetOverride(_ context.Context, key string, revision int64, o *models.Override, fingerprint string, nextRunAt *time.Time, by string, now time.Time) (*models.State, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.docs[key]
	if !ok {
		return nil, repository.ErrNotFound
	}
	if s.Revision != revision {
		return nil, repository.ErrRevisionConflict
	}
	s.Override, s.Fingerprint, s.UpdatedAt, s.UpdatedBy, s.NextRunAt = o, fingerprint, now, by, nextRunAt
	if nextRunAt == nil {
		s.RetryAt, s.RetrySlotAt, s.Attempt = nil, nil, 0
	}
	s.Revision++
	f.docs[key] = s
	return &s, nil
}

func (f *fakeStates) RequestRun(_ context.Context, key, by string, now time.Time) (*models.State, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.docs[key]
	if !ok {
		return nil, repository.ErrNotFound
	}
	if !leaseFree(s, now) {
		return nil, repository.ErrAlreadyRunning
	}
	if s.RunRequestedAt != nil {
		return nil, repository.ErrAlreadyRequested
	}
	s.RunRequestedAt, s.RunRequestedBy = &now, by
	s.Revision++
	f.docs[key] = s
	return &s, nil
}

func (f *fakeStates) Due(_ context.Context, now time.Time, limit int) ([]models.State, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.dueErr != nil {
		return nil, f.dueErr
	}
	var out []models.State
	for _, s := range f.docs {
		if !leaseFree(s, now) {
			continue
		}
		due := (s.NextRunAt != nil && !s.NextRunAt.After(now)) ||
			(s.RetryAt != nil && !s.RetryAt.After(now)) || s.RunRequestedAt != nil
		if due {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (f *fakeStates) Claim(_ context.Context, key string, revision int64, lease models.Lease, now time.Time) (*models.State, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.claimErr != nil {
		return nil, f.claimErr
	}
	s, ok := f.docs[key]
	if !ok || s.Revision != revision || !leaseFree(s, now) {
		return nil, nil
	}
	pre := s
	l := lease
	s.Lease = &l
	s.Revision++
	f.docs[key] = s
	return &pre, nil
}

func (f *fakeStates) Renew(_ context.Context, key, runID string, until time.Time) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.renew != nil {
		return f.renew()
	}
	s, ok := f.docs[key]
	if !ok || s.Lease == nil || s.Lease.RunID != runID {
		return false, nil
	}
	l := *s.Lease
	l.Until = until
	s.Lease = &l
	f.docs[key] = s
	return true, nil
}

func (f *fakeStates) Complete(_ context.Context, key, runID string, upd repository.Completion) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.docs[key]
	if !ok || s.Lease == nil || s.Lease.RunID != runID {
		return false, nil
	}
	lr := upd.LastRun
	s.LastRun = &lr
	s.Lease = nil
	if s.Fingerprint == upd.ExpectFingerprint {
		s.Attempt, s.RetryAt, s.RetrySlotAt = upd.Attempt, upd.RetryAt, upd.RetrySlotAt
		if upd.SetNext {
			s.NextRunAt = upd.NextRunAt
		}
	} else {
		s.Attempt, s.RetryAt, s.RetrySlotAt = 0, nil, nil
	}
	if upd.ClearRequest {
		s.RunRequestedAt, s.RunRequestedBy = nil, ""
	}
	s.Revision++
	f.docs[key] = s
	return true, nil
}

// fakeRuns mirrors the RunRepository in memory.
type fakeRuns struct {
	mu   sync.Mutex
	recs map[string]models.RunRecord
	seq  []string
}

func newFakeRuns() *fakeRuns { return &fakeRuns{recs: map[string]models.RunRecord{}} }

func (f *fakeRuns) Insert(_ context.Context, r *models.RunRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, dup := f.recs[r.ID]; dup {
		return errors.New("duplicate run id")
	}
	f.recs[r.ID] = *r
	f.seq = append(f.seq, r.ID)
	return nil
}

func (f *fakeRuns) Finish(_ context.Context, id, status, errText string, finishedAt time.Time, durationMs int64, details map[string]any, dropped int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.recs[id]
	if !ok || r.Status != models.StatusRunning {
		return nil
	}
	r.Status, r.Error, r.FinishedAt, r.DurationMs, r.Details, r.DroppedDetails = status, errText, &finishedAt, durationMs, details, dropped
	f.recs[id] = r
	return nil
}

func (f *fakeRuns) MarkIfRunning(_ context.Context, id, status string, finishedAt time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.recs[id]
	if !ok || r.Status != models.StatusRunning {
		return nil
	}
	r.Status, r.FinishedAt = status, &finishedAt
	f.recs[id] = r
	return nil
}

func (f *fakeRuns) List(_ context.Context, key, status string, _ *repository.RunCursor, _ int) ([]models.RunRecord, *repository.RunCursor, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []models.RunRecord
	for _, id := range f.seq {
		r := f.recs[id]
		if r.Key == key && (status == "" || r.Status == status) {
			out = append(out, r)
		}
	}
	return out, nil, nil
}

func (f *fakeRuns) DeleteOlderThan(context.Context, time.Time) (int64, error) { return 0, nil }

// all returns the records of key in insertion order.
func (f *fakeRuns) all(key string) []models.RunRecord {
	out, _, _ := f.List(context.Background(), key, "", nil, 0)
	return out
}

func (f *fakeRuns) byID(id string) models.RunRecord {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.recs[id]
}

// fakeMetrics counts metric calls.
type fakeMetrics struct {
	mu       sync.Mutex
	finished map[string]int // "key status"
	lost     map[string]int
}

func newFakeMetrics() *fakeMetrics {
	return &fakeMetrics{finished: map[string]int{}, lost: map[string]int{}}
}

func (m *fakeMetrics) RunFinished(key, status string, _ time.Duration) {
	m.mu.Lock()
	m.finished[key+" "+status]++
	m.mu.Unlock()
}

func (m *fakeMetrics) LeaseLost(key string) {
	m.mu.Lock()
	m.lost[key]++
	m.mu.Unlock()
}

func (m *fakeMetrics) lostCount(key string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lost[key]
}

func (m *fakeMetrics) finishedCount(key, status string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.finished[key+" "+status]
}
