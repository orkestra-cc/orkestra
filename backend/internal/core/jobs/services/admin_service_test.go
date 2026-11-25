package services

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/jobs/models"
	"github.com/orkestra/backend/internal/core/jobs/repository"
	"github.com/orkestra/backend/internal/core/jobs/scheduler"
	"github.com/orkestra/backend/internal/testkit"
	"github.com/orkestra/backend/pkg/sdk/iface"
	sdkjobs "github.com/orkestra/backend/pkg/sdk/jobs"
)

var t0 = time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)

// fakeStates implements the repository calls the admin service makes, with
// the same revision semantics as the Mongo repository.
type fakeStates struct {
	repository.StateRepository // unused methods panic
	mu                         sync.Mutex
	docs                       map[string]models.State
	listErr                    error
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
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := make([]models.State, 0, len(f.docs))
	for _, s := range f.docs {
		out = append(out, s)
	}
	return out, nil
}

func (f *fakeStates) SetOverride(_ context.Context, key string, revision int64, o *models.Override, fp string, next *time.Time, by string, now time.Time) (*models.State, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.docs[key]
	if !ok {
		return nil, repository.ErrNotFound
	}
	if s.Revision != revision {
		return nil, repository.ErrRevisionConflict
	}
	s.Override, s.Fingerprint, s.NextRunAt, s.UpdatedBy, s.UpdatedAt = o, fp, next, by, now
	if next == nil {
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
	if s.Lease != nil && !s.Lease.Until.Before(now) {
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

type fakeRuns struct {
	repository.RunRepository
	recs   []models.RunRecord
	status string
	before *repository.RunCursor
	limit  int
}

func (f *fakeRuns) List(_ context.Context, _ string, status string, before *repository.RunCursor, limit int) ([]models.RunRecord, *repository.RunCursor, error) {
	f.status, f.before, f.limit = status, before, limit
	return f.recs, &repository.RunCursor{StartedAt: t0, ID: "r-last"}, nil
}

type recordingSink struct {
	mu     sync.Mutex
	events []iface.AuditEvent
}

func (r *recordingSink) Emit(_ context.Context, e iface.AuditEvent) {
	r.mu.Lock()
	r.events = append(r.events, e)
	r.mu.Unlock()
}

type fixture struct {
	svc     *AdminService
	states  *fakeStates
	runs    *fakeRuns
	sink    *recordingSink
	started map[string]bool
	woken   int
}

func noop(context.Context, *sdkjobs.Run) error { return nil }

// newFixture registers m.hourly (every hour), m.nightly (cron 0 2 * * *) and
// m.manual (manual) and creates their states as the scheduler would.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	cat := sdkjobs.NewCatalog()
	if err := cat.Register("m", []sdkjobs.Definition{
		{Name: "hourly", Title: "Hourly", Schedule: sdkjobs.Every(time.Hour), Run: noop},
		{Name: "nightly", Title: "Nightly", Schedule: sdkjobs.Cron("0 2 * * *"), Run: noop, Retry: sdkjobs.RetryPolicy{MaxAttempts: 3}},
		{Name: "manual", Title: "Manual", Schedule: sdkjobs.Manual(), Run: noop},
	}); err != nil {
		t.Fatal(err)
	}
	f := &fixture{states: &fakeStates{docs: map[string]models.State{}}, runs: &fakeRuns{}, sink: &recordingSink{}, started: map[string]bool{"m": true}}
	for _, e := range cat.List() {
		eff, err := scheduler.EffectiveOf(e, nil, time.UTC)
		if err != nil {
			t.Fatal(err)
		}
		f.states.docs[e.Key] = models.State{Key: e.Key, Module: e.Module, Fingerprint: eff.Fingerprint, NextRunAt: scheduler.NextRun(eff, t0), Revision: 1}
	}
	f.svc = New(Options{
		Catalog: func() *sdkjobs.Catalog { return cat },
		States:  f.states, Runs: f.runs,
		ModuleStarted: func(m string) bool { return f.started[m] },
		AuditSink:     func() iface.AuditSink { return f.sink },
		Wake:          func() { f.woken++ },
		Now:           func() time.Time { return t0 },
	})
	return f
}

func operatorCtx() context.Context {
	return testkit.NewIdentity("u-admin", "admin@example.test", "administrator").WithTenant("t1", []string{"org_owner"}, true).ContextFor(context.Background(), "t1")
}

func boolp(b bool) *bool             { return &b }
func strp(s string) *string          { return &s }
func everyIn(s int64) *ScheduleInput { return &ScheduleInput{Kind: "every", EverySeconds: s} }

func TestListAndGet(t *testing.T) {
	f := newFixture(t)
	jobs, err := f.svc.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 3 || jobs[0].Key != "m.hourly" || jobs[1].Key != "m.manual" || jobs[2].Key != "m.nightly" {
		t.Fatalf("list = %+v", jobs)
	}
	n := jobs[2]
	if n.Schedule.Kind != "cron" || n.Schedule.Cron != "0 2 * * *" || n.Timezone != "UTC" || !n.Enabled ||
		!n.DefaultEnabled || n.Overridden || !n.ModuleStarted || n.MaxAttempts != 3 || n.TimeoutSeconds != 600 || n.Revision != 1 {
		t.Errorf("nightly view = %+v", n)
	}
	if h := jobs[0]; h.Schedule.EverySeconds != 3600 || h.NextRunAt == nil || !h.NextRunAt.Equal(t0.Add(time.Hour)) {
		t.Errorf("hourly view = %+v", h)
	}
	if _, err := f.svc.Get(context.Background(), "m.nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown key: %v", err)
	}
}

func TestUpdateStaleRevisionConflicts(t *testing.T) {
	f := newFixture(t)
	_, err := f.svc.Update(operatorCtx(), "m.hourly", UpdateInput{Revision: 0, Enabled: boolp(false)})
	if !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("err = %v, want revision conflict", err)
	}
	if len(f.sink.events) != 0 {
		t.Error("a rejected update was audited")
	}
}

func TestUpdateInvalidCronNamesTheField(t *testing.T) {
	f := newFixture(t)
	_, err := f.svc.Update(operatorCtx(), "m.nightly", UpdateInput{Revision: 1, Schedule: &ScheduleInput{Kind: "cron", Cron: "61 * * * *"}})
	if !errors.Is(err, ErrInvalidSchedule) {
		t.Fatalf("err = %v, want invalid schedule", err)
	}
	if msg := InputMessage(err, ""); !strings.Contains(msg, "minute") {
		t.Errorf("message %q does not name the minute field", msg)
	}
	for _, in := range []*ScheduleInput{everyIn(59), everyIn(int64(MaxEvery/time.Second) + 1), {Kind: "manual"}, {Kind: "hourly"}} {
		if _, err := f.svc.Update(operatorCtx(), "m.nightly", UpdateInput{Revision: 1, Schedule: in}); !errors.Is(err, ErrInvalidSchedule) {
			t.Errorf("%+v: err = %v, want invalid schedule", in, err)
		}
	}
}

func TestUpdateManualScheduleLocked(t *testing.T) {
	f := newFixture(t)
	_, err := f.svc.Update(operatorCtx(), "m.manual", UpdateInput{Revision: 1, Schedule: everyIn(600)})
	if !errors.Is(err, ErrManualScheduleLocked) {
		t.Fatalf("err = %v, want manual schedule locked", err)
	}
	// Enabling or disabling a manual job is still allowed.
	if _, err := f.svc.Update(operatorCtx(), "m.manual", UpdateInput{Revision: 1, Enabled: boolp(false)}); err != nil {
		t.Fatalf("disable manual: %v", err)
	}
}

func TestUpdateInvalidTimezone(t *testing.T) {
	f := newFixture(t)
	for _, tz := range []string{"Mars/Olympus", "Local"} {
		if _, err := f.svc.Update(operatorCtx(), "m.nightly", UpdateInput{Revision: 1, Timezone: strp(tz)}); !errors.Is(err, ErrInvalidTimezone) {
			t.Errorf("%q: err = %v, want invalid timezone", tz, err)
		}
	}
}

func TestDisablingClearsNextRunAt(t *testing.T) {
	f := newFixture(t)
	st := f.states.docs["m.hourly"]
	st.RetryAt, st.Attempt = &t0, 1
	f.states.docs["m.hourly"] = st

	v, err := f.svc.Update(operatorCtx(), "m.hourly", UpdateInput{Revision: 1, Enabled: boolp(false)})
	if err != nil {
		t.Fatal(err)
	}
	if v.Enabled || v.NextRunAt != nil || v.RetryAt != nil || v.Attempt != 0 || !v.Overridden || v.Revision != 2 {
		t.Fatalf("view after disable = %+v", v)
	}
	if got := f.states.docs["m.hourly"]; got.NextRunAt != nil || !strings.HasPrefix(got.Fingerprint, "off|") || got.UpdatedBy != "u-admin" {
		t.Errorf("state after disable = %+v", got)
	}
	if len(f.sink.events) != 1 {
		t.Fatalf("%d audit events, want 1", len(f.sink.events))
	}
	ev := f.sink.events[0]
	if ev.Action != AuditOverrideUpdated || ev.ResourceID != "m.hourly" || ev.ActorUserID != "u-admin" || ev.ActorEmail != "" {
		t.Errorf("audit event = %+v", ev)
	}
	if ev.Metadata["before"] != nil {
		t.Errorf("before = %#v, want nil", ev.Metadata["before"])
	}
	if after, _ := ev.Metadata["after"].(map[string]any); after["enabled"] != false {
		t.Errorf("after = %#v", ev.Metadata["after"])
	}
	if f.woken != 1 {
		t.Errorf("woken %d times, want 1", f.woken)
	}
}

// An edit that leaves the effective schedule unchanged keeps the due time; a
// schedule change reschedules from now.
func TestUpdateReschedulesOnlyOnFingerprintChange(t *testing.T) {
	f := newFixture(t)
	v, err := f.svc.Update(operatorCtx(), "m.hourly", UpdateInput{Revision: 1, Enabled: boolp(true)})
	if err != nil {
		t.Fatal(err)
	}
	if v.NextRunAt == nil || !v.NextRunAt.Equal(t0.Add(time.Hour)) {
		t.Errorf("unchanged schedule moved nextRunAt to %v", v.NextRunAt)
	}
	v, err = f.svc.Update(operatorCtx(), "m.hourly", UpdateInput{Revision: 2, Schedule: everyIn(600)})
	if err != nil {
		t.Fatal(err)
	}
	if v.Schedule.EverySeconds != 600 || v.DefaultSchedule.EverySeconds != 3600 || v.NextRunAt == nil || !v.NextRunAt.Equal(t0.Add(10*time.Minute)) {
		t.Errorf("after schedule change = %+v", v)
	}
	v, err = f.svc.Update(operatorCtx(), "m.nightly", UpdateInput{Revision: 1, Timezone: strp("Europe/Rome")})
	if err != nil {
		t.Fatal(err)
	}
	rome, _ := time.LoadLocation("Europe/Rome")
	if want := time.Date(2026, 11, 9, 2, 0, 0, 0, rome); v.Timezone != "Europe/Rome" || v.NextRunAt == nil || !v.NextRunAt.Equal(want) {
		t.Errorf("nightly in Rome = %v %v, want %v", v.Timezone, v.NextRunAt, want)
	}
	// Clearing the timezone with "" drops it from the override.
	v, err = f.svc.Update(operatorCtx(), "m.nightly", UpdateInput{Revision: 2, Timezone: strp("")})
	if err != nil {
		t.Fatal(err)
	}
	if v.Timezone != "UTC" || v.Overridden {
		t.Errorf("after clearing timezone = %+v", v)
	}
}

func TestClearOverride(t *testing.T) {
	f := newFixture(t)
	if _, err := f.svc.Update(operatorCtx(), "m.hourly", UpdateInput{Revision: 1, Schedule: everyIn(600), Enabled: boolp(false)}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.ClearOverride(operatorCtx(), "m.hourly", 1); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale clear: %v", err)
	}
	v, err := f.svc.ClearOverride(operatorCtx(), "m.hourly", 2)
	if err != nil {
		t.Fatal(err)
	}
	if v.Overridden || !v.Enabled || v.Schedule.EverySeconds != 3600 || v.NextRunAt == nil || !v.NextRunAt.Equal(t0.Add(time.Hour)) {
		t.Errorf("after clear = %+v", v)
	}
	last := f.sink.events[len(f.sink.events)-1]
	if last.Action != AuditOverrideCleared || last.Metadata["after"] != nil {
		t.Errorf("clear audit = %+v", last)
	}
	if before, _ := last.Metadata["before"].(map[string]any); before["enabled"] != false {
		t.Errorf("before = %#v", last.Metadata["before"])
	}
}

func TestRequestRun(t *testing.T) {
	f := newFixture(t)
	f.started["m"] = false
	if _, err := f.svc.RequestRun(operatorCtx(), "m.hourly"); !errors.Is(err, ErrModuleStopped) {
		t.Fatalf("stopped module: %v", err)
	}
	f.started["m"] = true
	v, err := f.svc.RequestRun(operatorCtx(), "m.hourly")
	if err != nil {
		t.Fatal(err)
	}
	if v.RunRequestedAt == nil || f.states.docs["m.hourly"].RunRequestedBy != "u-admin" || f.woken != 1 {
		t.Errorf("after request: %+v woken=%d", v, f.woken)
	}
	if _, err := f.svc.RequestRun(operatorCtx(), "m.hourly"); !errors.Is(err, ErrAlreadyRequested) {
		t.Errorf("second request: %v", err)
	}
	st := f.states.docs["m.nightly"]
	st.Lease = &models.Lease{Owner: "r1", RunID: "x", Trigger: "schedule", Until: t0.Add(time.Minute)}
	f.states.docs["m.nightly"] = st
	if _, err := f.svc.RequestRun(operatorCtx(), "m.nightly"); !errors.Is(err, ErrAlreadyRunning) {
		t.Errorf("running job: %v", err)
	}
	if v, _ := f.svc.Get(context.Background(), "m.nightly"); !v.Running {
		t.Error("job with a live lease not shown running")
	}
	// A disabled job may still be run on request.
	if _, err := f.svc.Update(operatorCtx(), "m.manual", UpdateInput{Revision: 1, Enabled: boolp(false)}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.RequestRun(operatorCtx(), "m.manual"); err != nil {
		t.Errorf("disabled job request: %v", err)
	}
	if n := len(f.sink.events); n != 3 || f.sink.events[0].Action != AuditRunRequested {
		t.Errorf("audit events = %+v", f.sink.events)
	}
}

func TestPreviewCronInRome(t *testing.T) {
	f := newFixture(t)
	next, desc, err := f.svc.Preview(context.Background(), ScheduleInput{Kind: "cron", Cron: "0 2 * * *"}, "Europe/Rome")
	if err != nil {
		t.Fatal(err)
	}
	if len(next) != PreviewCount {
		t.Fatalf("%d times, want %d", len(next), PreviewCount)
	}
	rome, _ := time.LoadLocation("Europe/Rome")
	for i, n := range next {
		if want := time.Date(2026, 11, 9+i, 2, 0, 0, 0, rome); !n.Equal(want) || n.Location().String() != "Europe/Rome" {
			t.Errorf("next[%d] = %v, want %v", i, n, want)
		}
		if i > 0 && !n.After(next[i-1]) {
			t.Errorf("next[%d] not ascending", i)
		}
	}
	if !strings.Contains(desc, "0 2 * * *") || !strings.Contains(desc, "Europe/Rome") {
		t.Errorf("description = %q", desc)
	}
	next, desc, err = f.svc.Preview(context.Background(), ScheduleInput{Kind: "every", EverySeconds: 5400}, "Europe/Rome")
	if err != nil || desc != "Every 90 minutes" || len(next) != PreviewCount {
		t.Fatalf("every preview: %q %v", desc, err)
	}
	if !next[0].Equal(t0.Add(90*time.Minute)) || next[0].Location().String() != "Europe/Rome" {
		t.Errorf("every next[0] = %v, want %v shown in Europe/Rome", next[0], t0.Add(90*time.Minute))
	}
	if _, _, err := f.svc.Preview(context.Background(), ScheduleInput{Kind: "cron", Cron: "0 2 * * *"}, "Nowhere/City"); !errors.Is(err, ErrInvalidTimezone) {
		t.Errorf("bad tz: %v", err)
	}
}

func TestRunsQuery(t *testing.T) {
	f := newFixture(t)
	f.runs.recs = []models.RunRecord{{ID: "r1", Key: "m.hourly", Status: "succeeded", StartedAt: t0, Details: map[string]any{"deleted": int64(3)}}}
	runs, next, err := f.svc.Runs(context.Background(), "m.hourly", "succeeded", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].Details["deleted"] != int64(3) || next == "" || f.runs.limit != DefaultRunsLimit || f.runs.status != "succeeded" {
		t.Fatalf("runs = %+v next=%q limit=%d", runs, next, f.runs.limit)
	}
	if _, _, err := f.svc.Runs(context.Background(), "m.hourly", "", next, 1000); err != nil || f.runs.before == nil || f.runs.before.ID != "r-last" || f.runs.limit != MaxRunsLimit {
		t.Errorf("second page: err=%v before=%+v limit=%d", err, f.runs.before, f.runs.limit)
	}
	if _, _, err := f.svc.Runs(context.Background(), "m.hourly", "exploded", "", 10); !errors.Is(err, ErrInvalidQuery) {
		t.Errorf("bad status: %v", err)
	}
	if _, _, err := f.svc.Runs(context.Background(), "m.hourly", "", "!!", 10); !errors.Is(err, ErrInvalidQuery) {
		t.Errorf("bad cursor: %v", err)
	}
	if _, _, err := f.svc.Runs(context.Background(), "m.gone", "", "", 10); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown key: %v", err)
	}
}

// I4: an input error quoting the operator's input stays within 200
// characters, whoever calls the service.
func TestInputMessagesClipped(t *testing.T) {
	long := strings.Repeat("Z", 1000)
	err := ValidateTimezone(long)
	if !errors.Is(err, ErrInvalidTimezone) {
		t.Fatalf("err = %v", err)
	}
	if msg := InputMessage(err, ""); len([]rune(msg)) > 200 || strings.Contains(msg, long) {
		t.Fatalf("timezone message is %d characters", len([]rune(msg)))
	}
	_, _, err = parseSchedule(ScheduleInput{Kind: "cron", Cron: strings.Repeat("9", 1000) + " * * * *"})
	if !errors.Is(err, ErrInvalidSchedule) {
		t.Fatalf("err = %v", err)
	}
	if msg := InputMessage(err, ""); len([]rune(msg)) > 200 || !strings.HasSuffix(msg, "…") {
		t.Fatalf("schedule message is %d characters: %q", len([]rune(msg)), msg)
	}
}
