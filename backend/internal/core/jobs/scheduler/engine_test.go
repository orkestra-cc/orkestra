package scheduler

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/jobs/models"
	"github.com/orkestra/backend/pkg/sdk/jobs"
)

var t0 = time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)

func tp(t time.Time) *time.Time { return &t }

type harness struct {
	e       *Engine
	states  *fakeStates
	runs    *fakeRuns
	clock   *fakeClock
	metrics *fakeMetrics
	started *atomic.Bool // module started
}

// newHarness registers defs under module "m", reconciles at t0 and returns
// the engine. opt fields left zero get test-friendly values.
func newHarness(t *testing.T, opt Options, defs ...jobs.Definition) *harness {
	t.Helper()
	cat := jobs.NewCatalog()
	if err := cat.Register("m", defs); err != nil {
		t.Fatal(err)
	}
	h := &harness{states: newFakeStates(), runs: newFakeRuns(), clock: newFakeClock(t0), metrics: newFakeMetrics(), started: &atomic.Bool{}}
	h.started.Store(true)
	opt.Clock = h.clock
	opt.Metrics = h.metrics
	if opt.ModuleStarted == nil {
		opt.ModuleStarted = func(string) bool { return h.started.Load() }
	}
	if opt.Jitter == nil {
		opt.Jitter = func() float64 { return 0.5 }
	}
	if opt.Replica == "" {
		opt.Replica = "test"
	}
	h.e = New(cat, h.states, h.runs, opt)
	if err := h.e.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { boundedDrain(t, h.e) })
	return h
}

// tick runs one poll and waits for every launched run to finish.
func (h *harness) tick(t *testing.T) int {
	t.Helper()
	n := h.e.Tick(context.Background())
	h.e.drain()
	return n
}

func def(name string, run func(context.Context, *jobs.Run) error) jobs.Definition {
	return jobs.Definition{Name: name, Title: name, Schedule: jobs.Every(time.Hour), Run: run}
}

func ok(context.Context, *jobs.Run) error { return nil }

func TestTickRunsDueJob(t *testing.T) {
	var calls atomic.Int32
	h := newHarness(t, Options{}, def("a", func(_ context.Context, r *jobs.Run) error {
		calls.Add(1)
		r.Detail("n", 3)
		return nil
	}))
	st := h.states.get("m.a")
	if st.NextRunAt == nil || !st.NextRunAt.Equal(t0.Add(time.Hour)) {
		t.Fatalf("reconcile nextRunAt = %v", st.NextRunAt)
	}
	if n := h.tick(t); n != 0 {
		t.Fatalf("not due yet: launched %d", n)
	}
	h.clock.Add(time.Hour)
	if n := h.tick(t); n != 1 {
		t.Fatalf("launched %d, want 1", n)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d", calls.Load())
	}
	recs := h.runs.all("m.a")
	if len(recs) != 1 {
		t.Fatalf("runs = %+v", recs)
	}
	r := recs[0]
	if r.Status != models.StatusSucceeded || r.Trigger != string(jobs.TriggerSchedule) || r.Attempt != 1 ||
		r.ScheduledFor == nil || !r.ScheduledFor.Equal(t0.Add(time.Hour)) || r.Replica != "test" || r.Details["n"] != int64(3) {
		t.Fatalf("run record: %+v", r)
	}
	st = h.states.get("m.a")
	if st.Lease != nil || st.LastRun == nil || st.LastRun.Status != models.StatusSucceeded || st.LastRun.RunID != r.ID {
		t.Fatalf("state after run: %+v", st)
	}
	if st.NextRunAt == nil || !st.NextRunAt.Equal(t0.Add(2*time.Hour)) {
		t.Fatalf("nextRunAt = %v", st.NextRunAt)
	}
	if n := h.tick(t); n != 0 {
		t.Fatalf("second tick launched %d", n)
	}
	if h.metrics.finishedCount("m.a", models.StatusSucceeded) != 1 {
		t.Fatal("metric RunFinished not recorded")
	}
}

func TestDisabledModuleNotClaimed(t *testing.T) {
	var calls atomic.Int32
	h := newHarness(t, Options{}, def("a", func(context.Context, *jobs.Run) error { calls.Add(1); return nil }))
	h.started.Store(false)
	h.clock.Add(time.Hour)
	if _, err := h.states.RequestRun(context.Background(), "m.a", "op", h.clock.Now()); err != nil {
		t.Fatal(err)
	}
	before := h.states.get("m.a")
	if n := h.tick(t); n != 0 || calls.Load() != 0 {
		t.Fatalf("stopped module ran: launched %d calls %d", n, calls.Load())
	}
	if after := h.states.get("m.a"); after.Revision != before.Revision || after.Lease != nil {
		t.Fatalf("state touched: %+v", after)
	}
	h.started.Store(true)
	if n := h.tick(t); n != 1 {
		t.Fatalf("after module start launched %d", n)
	}
}

// Ruling 2: a disabled job is not claimed for its schedule; its stale due
// time is cleared. A manual request still runs.
func TestDisabledJobNotClaimed(t *testing.T) {
	var calls atomic.Int32
	h := newHarness(t, Options{}, def("a", func(context.Context, *jobs.Run) error { calls.Add(1); return nil }))
	st := h.states.get("m.a")
	off := false
	eff, err := EffectiveOf(h.e.cat.List()[0], &models.State{Override: &models.Override{Enabled: &off}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// override written but nextRunAt left in place (e.g. by an older replica)
	if _, err := h.states.SetOverride(context.Background(), "m.a", st.Revision, &models.Override{Enabled: &off}, eff.Fingerprint, st.NextRunAt, "op", t0); err != nil {
		t.Fatal(err)
	}
	h.clock.Add(time.Hour)
	if n := h.tick(t); n != 0 || calls.Load() != 0 {
		t.Fatalf("disabled job ran: %d", n)
	}
	st = h.states.get("m.a")
	if st.NextRunAt != nil || st.Lease != nil {
		t.Fatalf("stale due time not cleared: %+v", st)
	}
	if _, err := h.states.RequestRun(context.Background(), "m.a", "op", h.clock.Now()); err != nil {
		t.Fatal(err)
	}
	if n := h.tick(t); n != 1 || calls.Load() != 1 {
		t.Fatalf("manual run on disabled job: launched %d calls %d", n, calls.Load())
	}
	st = h.states.get("m.a")
	if st.RunRequestedAt != nil || st.NextRunAt != nil {
		t.Fatalf("after manual run: %+v", st)
	}
	r := h.runs.all("m.a")[0]
	if r.Trigger != string(jobs.TriggerManual) || r.RequestedBy != "op" {
		t.Fatalf("manual run record: %+v", r)
	}
}

func TestRetryScheduledThenSucceeds(t *testing.T) {
	var calls atomic.Int32
	d := def("a", func(context.Context, *jobs.Run) error {
		if calls.Add(1) <= 2 {
			return errors.New("flaky")
		}
		return nil
	})
	d.Retry = jobs.RetryPolicy{MaxAttempts: 3, Backoff: time.Minute, MaxBackoff: time.Hour}
	h := newHarness(t, Options{}, d)
	slot := t0.Add(time.Hour)
	h.clock.Add(time.Hour)
	if n := h.tick(t); n != 1 {
		t.Fatalf("first: %d", n)
	}
	st := h.states.get("m.a")
	now := h.clock.Now()
	if st.Attempt != 1 || st.RetryAt == nil || !st.RetryAt.Equal(now.Add(time.Minute)) ||
		st.RetrySlotAt == nil || !st.RetrySlotAt.Equal(slot) || st.NextRunAt == nil || !st.NextRunAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("after first failure: attempt=%d retry=%v slot=%v next=%v", st.Attempt, st.RetryAt, st.RetrySlotAt, st.NextRunAt)
	}
	h.clock.Add(time.Minute)
	if n := h.tick(t); n != 1 {
		t.Fatalf("second: %d", n)
	}
	st = h.states.get("m.a")
	now = h.clock.Now()
	if st.Attempt != 2 || st.RetryAt == nil || !st.RetryAt.Equal(now.Add(2*time.Minute)) || !st.NextRunAt.Equal(t0.Add(2*time.Hour)) {
		t.Fatalf("after second failure: attempt=%d retry=%v next=%v", st.Attempt, st.RetryAt, st.NextRunAt)
	}
	h.clock.Add(2 * time.Minute)
	if n := h.tick(t); n != 1 {
		t.Fatalf("third: %d", n)
	}
	st = h.states.get("m.a")
	if st.Attempt != 0 || st.RetryAt != nil || st.RetrySlotAt != nil || st.LastRun.Status != models.StatusSucceeded {
		t.Fatalf("after success: %+v", st)
	}
	var got []string
	for _, r := range h.runs.all("m.a") {
		got = append(got, fmt.Sprintf("%s/%d/%s", r.Trigger, r.Attempt, r.Status))
		if r.ScheduledFor == nil || !r.ScheduledFor.Equal(slot) {
			t.Fatalf("scheduledFor of %s = %v", r.ID, r.ScheduledFor)
		}
	}
	want := "[schedule/1/failed retry/2/failed retry/3/succeeded]"
	if fmt.Sprint(got) != want {
		t.Fatalf("runs = %v, want %v", got, want)
	}
}

func TestRetriesExhausted(t *testing.T) {
	d := def("a", func(context.Context, *jobs.Run) error { return errors.New("down") })
	d.Retry = jobs.RetryPolicy{MaxAttempts: 2, Backoff: time.Minute, MaxBackoff: time.Hour}
	h := newHarness(t, Options{}, d)
	h.clock.Add(time.Hour)
	h.tick(t)
	h.clock.Add(time.Minute)
	if n := h.tick(t); n != 1 {
		t.Fatalf("retry: %d", n)
	}
	st := h.states.get("m.a")
	if st.Attempt != 0 || st.RetryAt != nil || st.NextRunAt == nil || !st.NextRunAt.Equal(t0.Add(2*time.Hour)) {
		t.Fatalf("exhausted: %+v", st)
	}
}

func TestPermanentErrorNoRetry(t *testing.T) {
	d := def("a", func(context.Context, *jobs.Run) error { return jobs.Permanent(errors.New("bad config")) })
	d.Retry = jobs.RetryPolicy{MaxAttempts: 3, Backoff: time.Minute, MaxBackoff: time.Hour}
	h := newHarness(t, Options{}, d)
	h.clock.Add(time.Hour)
	h.tick(t)
	st := h.states.get("m.a")
	if st.RetryAt != nil || st.Attempt != 0 || st.LastRun.Status != models.StatusFailed || st.LastRun.Error != "bad config" {
		t.Fatalf("permanent: %+v %+v", st, st.LastRun)
	}
	if !st.NextRunAt.Equal(h.clock.Now().Add(time.Hour)) {
		t.Fatalf("next slot not set: %v", st.NextRunAt)
	}
}

func TestManualFailureNoRetry(t *testing.T) {
	d := def("a", func(context.Context, *jobs.Run) error { return errors.New("nope") })
	d.Retry = jobs.RetryPolicy{MaxAttempts: 3, Backoff: time.Minute, MaxBackoff: time.Hour}
	h := newHarness(t, Options{}, d)
	h.clock.Add(10 * time.Minute)
	if _, err := h.states.RequestRun(context.Background(), "m.a", "op", h.clock.Now()); err != nil {
		t.Fatal(err)
	}
	if n := h.tick(t); n != 1 {
		t.Fatalf("launched %d", n)
	}
	st := h.states.get("m.a")
	if st.RetryAt != nil || st.Attempt != 0 || st.RunRequestedAt != nil {
		t.Fatalf("manual failure scheduled a retry or kept the request: %+v", st)
	}
	if st.NextRunAt == nil || !st.NextRunAt.Equal(t0.Add(time.Hour)) {
		t.Fatalf("manual run moved the schedule: %v", st.NextRunAt)
	}
	if r := h.runs.all("m.a")[0]; r.Trigger != string(jobs.TriggerManual) || r.Status != models.StatusFailed {
		t.Fatalf("record: %+v", r)
	}
}

func TestTimeoutStatus(t *testing.T) {
	d := def("a", func(ctx context.Context, _ *jobs.Run) error {
		<-ctx.Done()
		return ctx.Err()
	})
	d.Timeout = 20 * time.Millisecond
	h := newHarness(t, Options{}, d)
	h.clock.Add(time.Hour)
	h.tick(t)
	r := h.runs.all("m.a")[0]
	if r.Status != models.StatusTimedOut || r.Error == "" {
		t.Fatalf("record: %+v", r)
	}
	if st := h.states.get("m.a"); st.LastRun.Status != models.StatusTimedOut {
		t.Fatalf("lastRun: %+v", st.LastRun)
	}
}

func TestPanicRecovered(t *testing.T) {
	h := newHarness(t, Options{}, def("a", func(context.Context, *jobs.Run) error { panic("boom") }))
	h.clock.Add(time.Hour)
	if n := h.tick(t); n != 1 {
		t.Fatalf("launched %d", n)
	}
	r := h.runs.all("m.a")[0]
	if r.Status != models.StatusFailed || r.Error != "panic: boom" {
		t.Fatalf("record: %+v", r)
	}
	st := h.states.get("m.a")
	if st.Lease != nil || st.LastRun.Error != "panic: boom" {
		t.Fatalf("state: %+v", st)
	}
}

func TestErrorTruncated(t *testing.T) {
	long := strings.Repeat("é", 2000) // 4000 bytes
	h := newHarness(t, Options{}, def("a", func(context.Context, *jobs.Run) error { return errors.New(long) }))
	h.clock.Add(time.Hour)
	h.tick(t)
	got := h.runs.all("m.a")[0].Error
	if len(got) > 2048 || len(got) < 2046 || !strings.HasPrefix(long, got) {
		t.Fatalf("truncated to %d bytes", len(got))
	}
}

func TestCatchupCoalesced(t *testing.T) {
	var calls atomic.Int32
	h := newHarness(t, Options{}, def("a", func(context.Context, *jobs.Run) error { calls.Add(1); return nil }))
	h.clock.Add(72 * time.Hour)
	if n := h.tick(t); n != 1 {
		t.Fatalf("launched %d", n)
	}
	if n := h.tick(t); n != 0 {
		t.Fatalf("second tick launched %d", n)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want one coalesced run", calls.Load())
	}
	recs := h.runs.all("m.a")
	if len(recs) != 1 || recs[0].Trigger != string(jobs.TriggerCatchup) || !recs[0].ScheduledFor.Equal(t0.Add(time.Hour)) {
		t.Fatalf("records: %+v", recs)
	}
	if st := h.states.get("m.a"); !st.NextRunAt.Equal(h.clock.Now().Add(time.Hour)) {
		t.Fatalf("next = %v", st.NextRunAt)
	}
}

func TestOnTimeSlotIsSchedule(t *testing.T) {
	h := newHarness(t, Options{}, def("a", ok))
	h.clock.Add(time.Hour + 59*time.Second) // within the 1m catch-up threshold
	h.tick(t)
	if r := h.runs.all("m.a")[0]; r.Trigger != string(jobs.TriggerSchedule) {
		t.Fatalf("trigger = %s", r.Trigger)
	}
}

func TestSkipMissed(t *testing.T) {
	var calls atomic.Int32
	d := def("a", func(context.Context, *jobs.Run) error { calls.Add(1); return nil })
	d.SkipMissed = true
	h := newHarness(t, Options{}, d)
	h.clock.Add(72 * time.Hour)
	if n := h.tick(t); n != 0 {
		t.Fatalf("launched %d", n)
	}
	if calls.Load() != 0 {
		t.Fatal("skipped job executed")
	}
	recs := h.runs.all("m.a")
	if len(recs) != 1 || recs[0].Status != models.StatusSkipped || recs[0].Trigger != string(jobs.TriggerCatchup) || recs[0].FinishedAt == nil {
		t.Fatalf("records: %+v", recs)
	}
	st := h.states.get("m.a")
	if st.Lease != nil || st.LastRun == nil || st.LastRun.Status != models.StatusSkipped || !st.NextRunAt.Equal(h.clock.Now().Add(time.Hour)) {
		t.Fatalf("state: %+v", st)
	}
	if n := h.tick(t); n != 0 || len(h.runs.all("m.a")) != 1 {
		t.Fatal("skip repeated")
	}
}

func TestLeaseLossCancelsRun(t *testing.T) {
	cancelled := make(chan struct{})
	h := newHarness(t, Options{RenewInterval: 10 * time.Millisecond}, def("a", func(ctx context.Context, _ *jobs.Run) error {
		select {
		case <-ctx.Done():
			close(cancelled)
			return ctx.Err()
		case <-time.After(5 * time.Second):
			return nil
		}
	}))
	h.states.renew = func() (bool, error) { return false, nil }
	h.clock.Add(time.Hour)
	h.tick(t)
	select {
	case <-cancelled:
	default:
		t.Fatal("run ctx not cancelled on lease loss")
	}
	r := h.runs.all("m.a")[0]
	if r.Status != models.StatusFailed || !strings.HasPrefix(r.Error, "lease lost: ") {
		t.Fatalf("record: %+v", r)
	}
	if h.metrics.lostCount("m.a") != 1 {
		t.Fatalf("LeaseLost = %d, want 1", h.metrics.lostCount("m.a"))
	}
}

// Amended J8: a renew error is transient while the lease has time left.
func TestRenewTransientErrorContinues(t *testing.T) {
	release, closeRelease := releaser(t)
	var cancelled atomic.Bool
	h := newHarness(t, Options{RenewInterval: 5 * time.Millisecond}, def("a", func(ctx context.Context, _ *jobs.Run) error {
		select {
		case <-ctx.Done():
			cancelled.Store(true)
			return ctx.Err()
		case <-release:
			return nil
		}
	}))
	var renews atomic.Int32
	h.states.renew = func() (bool, error) {
		if renews.Add(1) == 1 {
			return false, errors.New("mongo blip")
		}
		return true, nil
	}
	h.clock.Add(time.Hour)
	h.e.Tick(context.Background())
	waitFor(t, func() bool { return renews.Load() >= 3 })
	closeRelease()
	h.e.drain()
	if cancelled.Load() || h.metrics.lostCount("m.a") != 0 || h.runs.all("m.a")[0].Status != models.StatusSucceeded {
		t.Fatalf("transient renew error cancelled the run: cancelled=%v lost=%d rec=%+v", cancelled.Load(), h.metrics.lostCount("m.a"), h.runs.all("m.a")[0])
	}
}

// Renew errors that last until the lease would have expired cancel the run.
func TestRenewErrorsUntilExpiryCancel(t *testing.T) {
	h := newHarness(t, Options{RenewInterval: 5 * time.Millisecond}, def("a", func(ctx context.Context, _ *jobs.Run) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
			return nil
		}
	}))
	var renews atomic.Int32
	h.states.renew = func() (bool, error) { renews.Add(1); return false, errors.New("mongo down") }
	h.clock.Add(time.Hour)
	h.e.Tick(context.Background())
	waitFor(t, func() bool { return renews.Load() >= 3 })
	if h.metrics.lostCount("m.a") != 0 {
		t.Fatal("cancelled while the lease still had time")
	}
	h.clock.Add(3 * time.Minute) // past the 2m lease
	h.e.drain()
	if h.metrics.lostCount("m.a") != 1 || h.runs.all("m.a")[0].Status != models.StatusFailed {
		t.Fatalf("renew errors until expiry: lost=%d rec=%+v", h.metrics.lostCount("m.a"), h.runs.all("m.a")[0])
	}
}

// I1: renew errors that persist cancel the run at the fence, Until −
// (RenewInterval + writeTimeout), while the lease still holds: no other
// replica can have claimed the job when the run stops.
func TestRenewErrorsFenceBeforeExpiry(t *testing.T) {
	const renewEvery = 5 * time.Millisecond
	var h *harness
	cancelled := make(chan struct{})
	var leaseHeldAtCancel atomic.Bool
	h = newHarness(t, Options{RenewInterval: renewEvery}, def("a", func(ctx context.Context, _ *jobs.Run) error {
		select {
		case <-ctx.Done():
			leaseHeldAtCancel.Store(!leaseFree(h.states.get("m.a"), h.clock.Now()))
			close(cancelled)
			return ctx.Err()
		case <-time.After(10 * time.Second):
			return nil
		}
	}))
	var renews atomic.Int32
	h.states.renew = func() (bool, error) { renews.Add(1); return false, errors.New("mongo partitioned") }
	h.clock.Add(time.Hour)
	if n := h.e.Tick(context.Background()); n != 1 {
		t.Fatalf("launched %d", n)
	}
	until := h.clock.Now().Add(DefaultLeaseTTL)
	fenceAt := until.Add(-(renewEvery + writeTimeout))

	h.clock.Set(fenceAt.Add(-time.Second))
	waitFor(t, func() bool { return renews.Load() >= 3 })
	select {
	case <-cancelled:
		t.Fatal("cancelled before the fence")
	case <-time.After(150 * time.Millisecond):
	}

	h.clock.Set(fenceAt.Add(time.Second)) // still 9 s before the lease expires
	select {
	case <-cancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("run not cancelled at the fence")
	}
	if !leaseHeldAtCancel.Load() {
		t.Fatal("the lease had already expired when the run was cancelled")
	}
	h.e.drain()
	if r := h.runs.all("m.a")[0]; r.Status != models.StatusFailed || !strings.HasPrefix(r.Error, "lease lost: ") {
		t.Fatalf("record: %+v", r)
	}
	if h.metrics.lostCount("m.a") != 1 {
		t.Fatalf("LeaseLost = %d, want 1", h.metrics.lostCount("m.a"))
	}
}

// I1 with the J8 amendment: renew errors up to just before the fence, then a
// successful renew, move the fence with the new lease; the run goes on past
// the old lease's expiry.
func TestRenewRecoveryMovesFence(t *testing.T) {
	const renewEvery = 5 * time.Millisecond
	release, closeRelease := releaser(t)
	var cancelled atomic.Bool
	h := newHarness(t, Options{RenewInterval: renewEvery}, def("a", func(ctx context.Context, _ *jobs.Run) error {
		select {
		case <-ctx.Done():
			cancelled.Store(true)
			return ctx.Err()
		case <-release:
			return nil
		}
	}))
	var failing atomic.Bool
	failing.Store(true)
	var renews atomic.Int32
	h.states.renew = func() (bool, error) {
		renews.Add(1)
		if failing.Load() {
			return false, errors.New("mongo blip")
		}
		return true, nil
	}
	h.clock.Add(time.Hour)
	h.e.Tick(context.Background())
	until := h.clock.Now().Add(DefaultLeaseTTL)
	fenceAt := until.Add(-(renewEvery + writeTimeout))

	h.clock.Set(fenceAt.Add(-time.Second))
	waitFor(t, func() bool { return renews.Load() >= 2 })
	failing.Store(false)
	n := renews.Load()
	waitFor(t, func() bool { return renews.Load() >= n+2 }) // at least one success
	h.clock.Set(until.Add(10 * time.Second))                // the first lease is long gone
	time.Sleep(150 * time.Millisecond)
	if cancelled.Load() {
		t.Fatal("a renewed run was cancelled at the old fence")
	}
	closeRelease()
	h.e.drain()
	if cancelled.Load() || h.metrics.lostCount("m.a") != 0 || h.runs.all("m.a")[0].Status != models.StatusSucceeded {
		t.Fatalf("cancelled=%v lost=%d rec=%+v", cancelled.Load(), h.metrics.lostCount("m.a"), h.runs.all("m.a")[0])
	}
}

// I2: a run whose module stops is cancelled on the next Tick and booked
// interrupted without consuming an attempt: the pending retry stays as it
// was and runs again, with the same attempt number, once the module starts.
func TestModuleStopInterruptsRunOnTick(t *testing.T) {
	var calls atomic.Int32
	d := def("a", func(ctx context.Context, _ *jobs.Run) error {
		if calls.Add(1) == 1 {
			<-ctx.Done()
			return ctx.Err()
		}
		return nil
	})
	d.Retry = jobs.RetryPolicy{MaxAttempts: 3, Backoff: time.Minute, MaxBackoff: time.Hour}
	h := newHarness(t, Options{}, d)
	st := h.states.get("m.a")
	st.Attempt, st.RetryAt, st.RetrySlotAt = 1, tp(t0.Add(time.Minute)), tp(t0)
	h.states.put(st)
	h.clock.Add(time.Minute)
	if n := h.e.Tick(context.Background()); n != 1 {
		t.Fatalf("launched %d", n)
	}
	waitFor(t, func() bool { return calls.Load() == 1 })

	h.started.Store(false)
	if n := h.e.Tick(context.Background()); n != 0 {
		t.Fatalf("launched %d for a stopped module", n)
	}
	h.e.drain()
	r := h.runs.all("m.a")[0]
	if r.Status != models.StatusInterrupted || !strings.HasPrefix(r.Error, "module stopped: ") || r.Attempt != 2 {
		t.Fatalf("record: %+v", r)
	}
	st = h.states.get("m.a")
	if st.Lease != nil || st.LastRun == nil || st.LastRun.Status != models.StatusInterrupted {
		t.Fatalf("state: %+v", st)
	}
	if st.Attempt != 1 || st.RetryAt == nil || !st.RetryAt.Equal(t0.Add(time.Minute)) || !st.NextRunAt.Equal(t0.Add(time.Hour)) {
		t.Fatalf("attempt consumed or due time moved: attempt=%d retryAt=%v next=%v", st.Attempt, st.RetryAt, st.NextRunAt)
	}
	if h.metrics.lostCount("m.a") != 0 {
		t.Fatal("module stop counted as a lost lease")
	}
	if n := h.tick(t); n != 0 {
		t.Fatalf("claimed while the module is stopped: %d", n)
	}

	h.started.Store(true)
	if n := h.tick(t); n != 1 {
		t.Fatalf("not re-run after the module started: %d", n)
	}
	recs := h.runs.all("m.a")
	if len(recs) != 2 || recs[1].Status != models.StatusSucceeded || recs[1].Trigger != string(jobs.TriggerRetry) || recs[1].Attempt != 2 {
		t.Fatalf("re-run: %+v", recs)
	}
}

// I2: without a Tick, the renew loop notices the stopped module within a
// RenewInterval; a scheduled slot stays due, attempt 0.
func TestModuleStopInterruptsRunOnRenew(t *testing.T) {
	cancelled := make(chan struct{})
	h := newHarness(t, Options{RenewInterval: 5 * time.Millisecond}, def("a", func(ctx context.Context, _ *jobs.Run) error {
		<-ctx.Done()
		close(cancelled)
		return ctx.Err()
	}))
	h.clock.Add(time.Hour)
	slot := h.clock.Now()
	if n := h.e.Tick(context.Background()); n != 1 {
		t.Fatalf("launched %d", n)
	}
	h.started.Store(false)
	select {
	case <-cancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("run of a stopped module not cancelled")
	}
	h.e.drain()
	if r := h.runs.all("m.a")[0]; r.Status != models.StatusInterrupted {
		t.Fatalf("record: %+v", r)
	}
	st := h.states.get("m.a")
	if st.Lease != nil || st.Attempt != 0 || st.RetryAt != nil || st.NextRunAt == nil || !st.NextRunAt.Equal(slot) {
		t.Fatalf("state: attempt=%d retryAt=%v next=%v lease=%+v", st.Attempt, st.RetryAt, st.NextRunAt, st.Lease)
	}
}

func TestRenewKeepsLease(t *testing.T) {
	release, closeRelease := releaser(t)
	h := newHarness(t, Options{RenewInterval: 5 * time.Millisecond}, def("a", func(context.Context, *jobs.Run) error {
		<-release
		return nil
	}))
	h.clock.Add(time.Hour)
	h.e.Tick(context.Background())
	first := h.states.get("m.a").Lease.Until
	h.clock.Add(time.Minute)
	deadline := time.Now().Add(2 * time.Second)
	for !h.states.get("m.a").Lease.Until.After(first) {
		if time.Now().After(deadline) {
			t.Fatal("lease never renewed")
		}
		time.Sleep(5 * time.Millisecond)
	}
	closeRelease()
	h.e.drain()
	if h.runs.all("m.a")[0].Status != models.StatusSucceeded {
		t.Fatal("renewed run did not succeed")
	}
}

func TestReconcileDefaultChange(t *testing.T) {
	h := newHarness(t, Options{}, def("a", ok), def("b", ok))
	// "a" was stored by an older build whose default was every 30m.
	a := h.states.get("m.a")
	a.Fingerprint = "on|every 30m0s"
	a.NextRunAt = tp(t0.Add(30 * time.Minute))
	h.states.put(a)
	// "b" carries an operator override, consistent with its stored fingerprint.
	b := h.states.get("m.b")
	b.Override = &models.Override{Schedule: &models.ScheduleSpec{Kind: "every", EverySeconds: 600}}
	b.Fingerprint = "on|every 10m0s"
	b.NextRunAt = tp(t0.Add(10 * time.Minute))
	h.states.put(b)

	h.clock.Add(5 * time.Minute)
	if err := h.e.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	a2 := h.states.get("m.a")
	if a2.Fingerprint != "on|every 1h0m0s" || !a2.NextRunAt.Equal(h.clock.Now().Add(time.Hour)) || a2.Revision != a.Revision+1 {
		t.Fatalf("drift not reconciled: %+v", a2)
	}
	b2 := h.states.get("m.b")
	if b2.Revision != b.Revision || !b2.NextRunAt.Equal(t0.Add(10*time.Minute)) {
		t.Fatalf("overridden job touched: %+v", b2)
	}
}

// A due state whose stored fingerprint came from another schedule (another
// build's code default) is drift: Tick reschedules it from now instead of
// running the old slot.
func TestTickReschedulesOnDrift(t *testing.T) {
	var calls atomic.Int32
	d := def("a", func(context.Context, *jobs.Run) error { calls.Add(1); return nil })
	d.Schedule = jobs.Cron("0 2 * * *")
	h := newHarness(t, Options{}, d)
	st := h.states.get("m.a")
	st.Fingerprint = "on|cron 0 3 * * *|UTC" // written by an older build
	h.states.put(st)
	h.clock.Add(14 * time.Hour) // 2026-11-09 02:00 UTC: due
	if n := h.tick(t); n != 0 || calls.Load() != 0 {
		t.Fatalf("launched %d on drift", n)
	}
	st = h.states.get("m.a")
	if st.Fingerprint != "on|cron 0 2 * * *|UTC" {
		t.Fatalf("fingerprint = %q", st.Fingerprint)
	}
	if want := time.Date(2026, 11, 10, 2, 0, 0, 0, time.UTC); !st.NextRunAt.Equal(want) {
		t.Fatalf("next = %v, want %v", st.NextRunAt, want)
	}
}

// A module timezone change is applied on the first Tick that sees it: the
// cron job moves at once to the next slot in the new zone, so no day is lost.
func TestTimezoneChangeReschedulesCronAtOnce(t *testing.T) {
	var loc atomic.Pointer[time.Location]
	loc.Store(time.UTC)
	rome, err := time.LoadLocation("Europe/Rome")
	if err != nil {
		t.Skip("no tzdata")
	}
	var calls atomic.Int32
	d := def("a", func(context.Context, *jobs.Run) error { calls.Add(1); return nil })
	d.Schedule = jobs.Cron("0 2 * * *")
	h := newHarness(t, Options{DefaultLocation: func() *time.Location { return loc.Load() }}, d)
	if st := h.states.get("m.a"); !st.NextRunAt.Equal(time.Date(2026, 11, 9, 2, 0, 0, 0, time.UTC)) {
		t.Fatalf("next in UTC = %v", st.NextRunAt)
	}
	loc.Store(rome)
	h.clock.Add(time.Minute) // the change is seen right away, not at the old due time
	if n := h.tick(t); n != 0 {
		t.Fatalf("launched %d", n)
	}
	st := h.states.get("m.a")
	want := time.Date(2026, 11, 9, 2, 0, 0, 0, rome) // 01:00 UTC, the same night
	if st.Fingerprint != "on|cron 0 2 * * *|Europe/Rome" || !st.NextRunAt.Equal(want) {
		t.Fatalf("after the zone change: fingerprint %q next %v, want next %v", st.Fingerprint, st.NextRunAt, want)
	}
	h.clock.Set(want)
	if n := h.tick(t); n != 1 || calls.Load() != 1 {
		t.Fatalf("the first slot in the new zone did not run: launched %d", n)
	}
	if r := h.runs.all("m.a")[0]; r.Trigger != string(jobs.TriggerSchedule) || !r.ScheduledFor.Equal(want) {
		t.Fatalf("run: %+v", r)
	}
}

// An interval job does not depend on the zone: a module timezone change
// leaves its due time and fingerprint alone.
func TestTimezoneChangeKeepsIntervalNextRun(t *testing.T) {
	var loc atomic.Pointer[time.Location]
	loc.Store(time.UTC)
	rome, err := time.LoadLocation("Europe/Rome")
	if err != nil {
		t.Skip("no tzdata")
	}
	h := newHarness(t, Options{DefaultLocation: func() *time.Location { return loc.Load() }}, def("a", ok))
	before := h.states.get("m.a")
	loc.Store(rome)
	h.clock.Add(time.Minute)
	if n := h.tick(t); n != 0 {
		t.Fatalf("launched %d", n)
	}
	after := h.states.get("m.a")
	if after.Fingerprint != before.Fingerprint || after.Revision != before.Revision || !after.NextRunAt.Equal(*before.NextRunAt) {
		t.Fatalf("interval job touched by a zone change: before %+v after %+v", before, after)
	}
	h.clock.Set(*before.NextRunAt)
	if n := h.tick(t); n != 1 {
		t.Fatalf("interval slot did not run: launched %d", n)
	}
}

func TestStopInterrupts(t *testing.T) {
	release, closeRelease := releaser(t)
	polite := def("polite", func(ctx context.Context, _ *jobs.Run) error {
		<-ctx.Done()
		return ctx.Err()
	})
	polite.Retry = jobs.RetryPolicy{MaxAttempts: 2, Backoff: time.Minute, MaxBackoff: time.Hour}
	stubborn := def("stubborn", func(context.Context, *jobs.Run) error {
		<-release
		return nil
	})
	h := newHarness(t, Options{StopGrace: 50 * time.Millisecond}, polite, stubborn)
	h.clock.Add(time.Hour)
	if n := h.e.Tick(context.Background()); n != 2 {
		t.Fatalf("launched %d", n)
	}
	if err := h.e.Stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}
	now := h.clock.Now()
	for _, key := range []string{"m.polite", "m.stubborn"} {
		r := h.runs.all(key)[0]
		if r.Status != models.StatusInterrupted {
			t.Fatalf("%s status = %s", key, r.Status)
		}
		st := h.states.get(key)
		if st.Lease != nil || st.LastRun == nil || st.LastRun.Status != models.StatusInterrupted {
			t.Fatalf("%s state: %+v", key, st)
		}
	}
	// interrupted counts as a failed attempt: the polite job gets its retry
	if st := h.states.get("m.polite"); st.Attempt != 1 || st.RetryAt == nil || !st.RetryAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("polite retry: %+v", st)
	}
	if st := h.states.get("m.stubborn"); st.RetryAt != nil || !st.NextRunAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("stubborn next: %+v", st)
	}
	// the stubborn body returning late changes nothing
	rev := h.states.get("m.stubborn").Revision
	closeRelease()
	h.e.drain()
	if r := h.runs.all("m.stubborn")[0]; r.Status != models.StatusInterrupted {
		t.Fatalf("late finisher overwrote status: %s", r.Status)
	}
	if h.states.get("m.stubborn").Revision != rev {
		t.Fatal("late finisher wrote state")
	}
	if n := h.e.Tick(context.Background()); n != 0 {
		t.Fatal("tick after stop launched runs")
	}
}

func TestMaxConcurrent(t *testing.T) {
	release, closeRelease := releaser(t)
	var calls atomic.Int32
	body := func(context.Context, *jobs.Run) error { calls.Add(1); <-release; return nil }
	var defs []jobs.Definition
	for i := 0; i < 6; i++ {
		defs = append(defs, def(fmt.Sprintf("j%d", i), body))
	}
	h := newHarness(t, Options{MaxConcurrent: 4}, defs...)
	h.clock.Add(time.Hour)
	if n := h.e.Tick(context.Background()); n != 4 {
		t.Fatalf("launched %d, want 4", n)
	}
	if n := h.e.Tick(context.Background()); n != 0 {
		t.Fatalf("over capacity launched %d", n)
	}
	closeRelease()
	h.e.drain()
	if n := h.tick(t); n != 2 {
		t.Fatalf("remaining launched %d, want 2", n)
	}
	if calls.Load() != 6 {
		t.Fatalf("calls = %d", calls.Load())
	}
}

// Ruling 1: an operator edit landing during a run survives its completion.
func TestOperatorEditDuringRunWins(t *testing.T) {
	release, closeRelease := releaser(t)
	d := def("a", func(context.Context, *jobs.Run) error { <-release; return errors.New("fail") })
	d.Retry = jobs.RetryPolicy{MaxAttempts: 3, Backoff: time.Minute, MaxBackoff: time.Hour}
	h := newHarness(t, Options{}, d)
	h.clock.Add(time.Hour)
	if n := h.e.Tick(context.Background()); n != 1 {
		t.Fatalf("launched %d", n)
	}
	st := h.states.get("m.a")
	off := false
	o := &models.Override{Enabled: &off}
	eff, _ := EffectiveOf(h.e.cat.List()[0], &models.State{Override: o}, nil)
	if _, err := h.states.SetOverride(context.Background(), "m.a", st.Revision, o, eff.Fingerprint, nil, "op", h.clock.Now()); err != nil {
		t.Fatal(err)
	}
	closeRelease()
	h.e.drain()
	st = h.states.get("m.a")
	if st.NextRunAt != nil || st.RetryAt != nil || st.Attempt != 0 {
		t.Fatalf("completion undid the operator edit: next=%v retry=%v attempt=%d", st.NextRunAt, st.RetryAt, st.Attempt)
	}
	if st.LastRun == nil || st.LastRun.Status != models.StatusFailed || st.Lease != nil {
		t.Fatalf("run fields: %+v", st)
	}
}

// An expired lease whose run never completed is marked abandoned and counts
// as a failed attempt; with no retry left, the claim only books it.
func TestAbandonedRunBooked(t *testing.T) {
	var calls atomic.Int32
	h := newHarness(t, Options{}, def("a", func(context.Context, *jobs.Run) error { calls.Add(1); return nil }))
	slot := t0.Add(time.Hour)
	st := h.states.get("m.a")
	st.Lease = &models.Lease{Owner: "dead", RunID: "old", Trigger: string(jobs.TriggerSchedule), Attempt: 1, SlotAt: &slot, StartedAt: slot, Until: slot.Add(2 * time.Minute)}
	h.states.put(st)
	_ = h.runs.Insert(context.Background(), &models.RunRecord{ID: "old", Key: "m.a", Status: models.StatusRunning, StartedAt: slot})
	h.clock.Add(time.Hour + 3*time.Minute)
	if n := h.tick(t); n != 0 || calls.Load() != 0 {
		t.Fatalf("launched %d", n)
	}
	if r := h.runs.byID("old"); r.Status != models.StatusAbandoned {
		t.Fatalf("old run: %+v", r)
	}
	st = h.states.get("m.a")
	if st.Lease != nil || st.LastRun == nil || st.LastRun.Status != models.StatusAbandoned || st.LastRun.RunID != "old" || !st.LastRun.StartedAt.Equal(slot) {
		t.Fatalf("state: %+v %+v", st, st.LastRun)
	}
	if !st.NextRunAt.Equal(h.clock.Now().Add(time.Hour)) {
		t.Fatalf("next = %v", st.NextRunAt)
	}
	if h.metrics.finishedCount("m.a", models.StatusAbandoned) != 1 {
		t.Fatal("abandoned metric")
	}
}

func TestUnknownKeyNotClaimed(t *testing.T) {
	h := newHarness(t, Options{}, def("a", ok))
	h.states.put(models.State{Key: "gone.job", Module: "gone", Fingerprint: "x", NextRunAt: tp(t0)})
	h.clock.Add(time.Hour)
	if n := h.tick(t); n != 1 {
		t.Fatalf("launched %d", n)
	}
	if st := h.states.get("gone.job"); st.Lease != nil || st.Revision != 0 {
		t.Fatalf("unknown key claimed: %+v", st)
	}
}

func TestHealthy(t *testing.T) {
	h := newHarness(t, Options{PollInterval: 5 * time.Millisecond}, def("a", ok))
	if h.e.Healthy() == nil {
		t.Fatal("healthy before Start")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); h.e.Start(ctx) }()
	waitFor(t, func() bool { return h.e.Healthy() == nil })

	h.states.setDueErr(errors.New("mongo down"))
	time.Sleep(20 * time.Millisecond) // a few failing polls
	if err := h.e.Healthy(); err != nil {
		t.Fatalf("unhealthy too early: %v", err)
	}
	h.clock.Add(6 * time.Minute)
	waitFor(t, func() bool { return h.e.Healthy() != nil })
	h.states.setDueErr(nil)
	waitFor(t, func() bool { return h.e.Healthy() == nil })

	if err := h.e.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	if h.e.Healthy() == nil {
		t.Fatal("healthy after loop exit")
	}
}

func TestStartRunsDueJobs(t *testing.T) {
	var calls atomic.Int32
	h := newHarness(t, Options{PollInterval: 5 * time.Millisecond}, def("a", func(context.Context, *jobs.Run) error { calls.Add(1); return nil }))
	h.clock.Add(time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { h.e.Start(ctx); close(done) }()
	waitFor(t, func() bool { return calls.Load() == 1 })
	cancel()
	<-done
}

// boundedDrain waits for the engine's runs, failing instead of hanging when a
// body never returns.
func boundedDrain(t *testing.T, e *Engine) {
	done := make(chan struct{})
	go func() { e.drain(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Error("job runs still going at test end")
	}
}

// releaser returns a channel blocking job bodies and its idempotent closer,
// also run on cleanup (before the harness drains) so a failing test does not
// hang on bodies it never released.
func releaser(t *testing.T) (chan struct{}, func()) {
	ch := make(chan struct{})
	var once sync.Once
	closeFn := func() { once.Do(func() { close(ch) }) }
	t.Cleanup(closeFn)
	return ch, closeFn
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met in time")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// An abandoned scheduled run with attempts left is retried in the same claim
// once its backoff (counted from the lease expiry) has elapsed; the next slot
// computed by the accounting is persisted.
func TestAbandonedRunRetried(t *testing.T) {
	var calls atomic.Int32
	d := def("a", func(context.Context, *jobs.Run) error { calls.Add(1); return nil })
	d.Retry = jobs.RetryPolicy{MaxAttempts: 2, Backoff: time.Minute, MaxBackoff: time.Hour}
	h := newHarness(t, Options{Jitter: func() float64 { return 0 }}, d)
	slot := t0.Add(time.Hour)
	st := h.states.get("m.a")
	st.Lease = &models.Lease{Owner: "dead", RunID: "old", Trigger: string(jobs.TriggerSchedule), Attempt: 1, SlotAt: &slot, StartedAt: slot, Until: slot.Add(2 * time.Minute)}
	h.states.put(st)
	_ = h.runs.Insert(context.Background(), &models.RunRecord{ID: "old", Key: "m.a", Status: models.StatusRunning, StartedAt: slot})
	h.clock.Add(time.Hour + 2*time.Minute + 30*time.Second) // retry due at expiry+48s: not yet
	if n := h.tick(t); n != 0 {
		t.Fatalf("retried before its backoff: %d", n)
	}
	st = h.states.get("m.a")
	if st.RetryAt == nil || !st.RetryAt.Equal(slot.Add(2*time.Minute+48*time.Second)) || st.Attempt != 1 {
		t.Fatalf("abandoned retry not booked: retry=%v attempt=%d", st.RetryAt, st.Attempt)
	}
	if h.runs.byID("old").Status != models.StatusAbandoned {
		t.Fatal("old run not abandoned")
	}
	h.clock.Add(30 * time.Second)
	if n := h.tick(t); n != 1 || calls.Load() != 1 {
		t.Fatalf("retry launched %d calls %d", n, calls.Load())
	}
	recs := h.runs.all("m.a")
	last := recs[len(recs)-1]
	if last.Trigger != string(jobs.TriggerRetry) || last.Attempt != 2 || last.Status != models.StatusSucceeded {
		t.Fatalf("retry record: %+v", last)
	}
	st = h.states.get("m.a")
	if st.Attempt != 0 || st.RetryAt != nil || st.NextRunAt == nil || !st.NextRunAt.After(h.clock.Now()) {
		t.Fatalf("after retry: %+v", st)
	}
}

// Same accounting when the backoff already elapsed: abandon and retry happen
// in one claim, and the next slot from the accounting is written.
func TestAbandonedRunRetriedInSameClaim(t *testing.T) {
	d := def("a", ok)
	d.Retry = jobs.RetryPolicy{MaxAttempts: 2, Backoff: time.Minute, MaxBackoff: time.Hour}
	h := newHarness(t, Options{Jitter: func() float64 { return 0 }}, d)
	slot := t0.Add(time.Hour)
	st := h.states.get("m.a")
	st.Lease = &models.Lease{Owner: "dead", RunID: "old", Trigger: string(jobs.TriggerSchedule), Attempt: 1, SlotAt: &slot, StartedAt: slot, Until: slot.Add(2 * time.Minute)}
	h.states.put(st)
	h.clock.Add(time.Hour + 5*time.Minute)
	if n := h.tick(t); n != 1 {
		t.Fatalf("launched %d", n)
	}
	st = h.states.get("m.a")
	if st.NextRunAt == nil || !st.NextRunAt.Equal(h.clock.Now().Add(time.Hour)) {
		t.Fatalf("next slot from the accounting lost: %v", st.NextRunAt)
	}
}

// A local run whose lease expired (but whose body is still going) is not
// launched a second time by the same replica.
func TestNoLocalDoubleRun(t *testing.T) {
	release, closeRelease := releaser(t)
	var calls atomic.Int32
	h := newHarness(t, Options{RenewInterval: time.Hour}, def("a", func(context.Context, *jobs.Run) error {
		calls.Add(1)
		<-release
		return nil
	}))
	h.clock.Add(time.Hour)
	if n := h.e.Tick(context.Background()); n != 1 {
		t.Fatalf("launched %d", n)
	}
	h.clock.Add(3 * time.Minute)
	if n := h.e.Tick(context.Background()); n != 0 {
		t.Fatalf("relaunched a run still going locally: %d", n)
	}
	closeRelease()
	h.e.drain()
	if calls.Load() != 1 {
		t.Fatalf("calls = %d", calls.Load())
	}
	// nor does it book its own live run as abandoned
	recs := h.runs.all("m.a")
	if len(recs) != 1 || recs[0].Status != models.StatusSucceeded {
		t.Fatalf("records: %+v", recs)
	}
	if st := h.states.get("m.a"); st.LastRun == nil || st.LastRun.Status != models.StatusSucceeded {
		t.Fatalf("lastRun: %+v", st.LastRun)
	}
}

// Fix 1: a dead replica's run on a job disabled meanwhile is still booked
// abandoned after the TTL, and nothing executes.
func TestAbandonedRunOfDisabledJobBooked(t *testing.T) {
	release, closeRelease := releaser(t)
	var calls atomic.Int32
	h := newHarness(t, Options{RenewInterval: time.Hour}, def("a", func(context.Context, *jobs.Run) error {
		calls.Add(1)
		<-release // replica A is dead: never renews, never completes
		return nil
	}))
	h.clock.Add(time.Hour)
	if n := h.e.Tick(context.Background()); n != 1 {
		t.Fatalf("A launched %d", n)
	}
	waitFor(t, func() bool { return calls.Load() == 1 })
	oldID := h.runs.all("m.a")[0].ID
	st := h.states.get("m.a")
	off := false
	o := &models.Override{Enabled: &off}
	eff, _ := EffectiveOf(h.e.cat.List()[0], &models.State{Override: o}, nil)
	if _, err := h.states.SetOverride(context.Background(), "m.a", st.Revision, o, eff.Fingerprint, nil, "op", h.clock.Now()); err != nil {
		t.Fatal(err)
	}
	b := New(h.e.cat, h.states, h.runs, Options{Replica: "b", Clock: h.clock, Metrics: h.metrics})
	h.clock.Add(3 * time.Minute)
	if n := b.Tick(context.Background()); n != 0 {
		t.Fatalf("B launched %d for a disabled job", n)
	}
	b.drain()
	if calls.Load() != 1 {
		t.Fatalf("calls = %d", calls.Load())
	}
	if r := h.runs.byID(oldID); r.Status != models.StatusAbandoned {
		t.Fatalf("dead run not booked: %+v", r)
	}
	st = h.states.get("m.a")
	if st.Lease != nil || st.NextRunAt != nil || st.RetryAt != nil || st.LastRun == nil || st.LastRun.Status != models.StatusAbandoned {
		t.Fatalf("state: %+v", st)
	}
	if n := b.Tick(context.Background()); n != 0 {
		t.Fatal("booked state still due")
	}
	closeRelease()
	h.e.drain()
}

// A leftover lease of a claim that executed nothing (no trigger) is released
// without booking a phantom abandoned run.
func TestBookkeepingLeaseReleasedQuietly(t *testing.T) {
	h := newHarness(t, Options{}, def("a", ok))
	st := h.states.get("m.a")
	prev := &models.LastRun{RunID: "prev", Status: models.StatusSucceeded}
	st.LastRun = prev
	st.Lease = &models.Lease{Owner: "dead", RunID: "bk", Until: t0.Add(time.Minute)}
	h.states.put(st)
	h.clock.Add(5 * time.Minute)
	if n := h.tick(t); n != 0 {
		t.Fatalf("launched %d", n)
	}
	st = h.states.get("m.a")
	if st.Lease != nil || st.LastRun == nil || st.LastRun.RunID != "prev" {
		t.Fatalf("state: lease=%+v lastRun=%+v", st.Lease, st.LastRun)
	}
	if h.metrics.finishedCount("m.a", models.StatusAbandoned) != 0 {
		t.Fatal("phantom abandoned run counted")
	}
}

// Fix 2: unclaimable due states never crowd out a claimable one.
func TestDueNotStarved(t *testing.T) {
	h := newHarness(t, Options{}, def("z", ok))
	for i := 0; i < 150; i++ {
		h.states.put(models.State{Key: fmt.Sprintf("gone.j%03d", i), Module: "gone", Fingerprint: "x", NextRunAt: tp(t0)})
	}
	cat := h.e.cat
	if err := cat.Register("stopped", []jobs.Definition{def("s", ok)}); err != nil {
		t.Fatal(err)
	}
	if err := h.e.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.e.opt.ModuleStarted = func(m string) bool { return m != "stopped" }
	h.clock.Add(time.Hour)
	if n := h.tick(t); n != 1 {
		t.Fatalf("launched %d, want the one claimable job", n)
	}
	if len(h.runs.all("m.z")) != 1 || len(h.runs.all("stopped.s")) != 0 {
		t.Fatal("wrong job ran")
	}
}

// Fix 3: a failing outcome write is retried while the lease is held; the run
// is booked as it ended and no lease loss is counted.
func TestCompleteRetried(t *testing.T) {
	h := newHarness(t, Options{}, def("a", ok))
	h.clock.Add(time.Hour)
	h.states.mu.Lock()
	h.states.completeErrs = 2
	h.states.mu.Unlock()
	if n := h.tick(t); n != 1 {
		t.Fatalf("launched %d", n)
	}
	st := h.states.get("m.a")
	if st.Lease != nil || st.LastRun == nil || st.LastRun.Status != models.StatusSucceeded || st.RetryAt != nil || st.Attempt != 0 {
		t.Fatalf("state: %+v", st)
	}
	if !st.NextRunAt.Equal(h.clock.Now().Add(time.Hour)) {
		t.Fatalf("next = %v", st.NextRunAt)
	}
	if h.metrics.lostCount("m.a") != 0 {
		t.Fatal("LeaseLost counted for a write failure")
	}
	if h.states.completes != 3 {
		t.Fatalf("Complete calls = %d, want 3", h.states.completes)
	}
}

// When every attempt fails, the lease is left to expire and no lease loss is
// counted.
func TestCompleteGivesUp(t *testing.T) {
	h := newHarness(t, Options{}, def("a", ok))
	h.clock.Add(time.Hour)
	h.states.mu.Lock()
	h.states.completeErrs = 10
	h.states.mu.Unlock()
	h.tick(t)
	st := h.states.get("m.a")
	if st.Lease == nil {
		t.Fatal("lease released although the outcome was not written")
	}
	if h.metrics.lostCount("m.a") != 0 {
		t.Fatal("LeaseLost counted for a write failure")
	}
	if h.runs.all("m.a")[0].Status != models.StatusSucceeded {
		t.Fatal("run record not finished")
	}
}

// Wake makes a sleeping loop poll at once: a run requested through the admin
// API starts without waiting for the poll interval.
func TestWakePollsNow(t *testing.T) {
	var calls atomic.Int32
	h := newHarness(t, Options{PollInterval: time.Hour}, def("a", func(context.Context, *jobs.Run) error { calls.Add(1); return nil }))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { h.e.Start(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	// The first poll finds nothing due; let the loop go to sleep.
	time.Sleep(20 * time.Millisecond)
	if _, err := h.states.RequestRun(context.Background(), "m.a", "u1", h.clock.Now()); err != nil {
		t.Fatal(err)
	}
	h.e.Wake()
	waitFor(t, func() bool { return calls.Load() == 1 })
}
