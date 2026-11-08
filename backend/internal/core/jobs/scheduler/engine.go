// Package scheduler is the per-replica loop of the core jobs module. Each
// replica polls the state collection for due jobs, claims one with a lease
// (an atomic findOneAndUpdate, so exactly one replica wins), runs it with a
// timeout while renewing the lease, and records the run and the next due
// time. Retries are durable (retryAt in the state document), missed slots
// coalesce into one catch-up run, and a run whose replica died is found
// through its expired lease and booked as abandoned.
//
// A run fences itself: it is cancelled once no renewal can land before the
// lease it last renewed expires (Until − (RenewInterval + the 10 s write
// timeout)), so it stops before another replica may claim the job. Lease
// expiry is judged on each replica's own wall clock: replicas must be
// NTP-synced, with a skew well below that margin (40 s with the defaults),
// or the at-most-one guarantee does not hold.
//
// A run whose module stops (StopModule at runtime, or StopAll at shutdown)
// is cancelled and booked interrupted without consuming an attempt: its due
// time is left as it was, so it runs again once the module starts.
package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/orkestra/backend/internal/core/jobs/models"
	"github.com/orkestra/backend/internal/core/jobs/repository"
	"github.com/orkestra/backend/pkg/sdk/jobs"
)

// Clock reads the current time. The engine reads time only through it, so
// tests drive due times, leases and retries; sleeping uses real timers.
type Clock interface{ Now() time.Time }

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

// Metrics receives run outcomes. d is zero for runs that never executed in
// this process (skipped, abandoned); implementations should not observe a
// duration for those.
type Metrics interface {
	RunFinished(key, status string, d time.Duration)
	LeaseLost(key string)
}

type nopMetrics struct{}

func (nopMetrics) RunFinished(string, string, time.Duration) {}
func (nopMetrics) LeaseLost(string)                          {}

// Defaults (spec J8, J9, J18).
const (
	DefaultPollInterval  = 10 * time.Second
	DefaultLeaseTTL      = 2 * time.Minute
	DefaultRenewInterval = 30 * time.Second
	DefaultStopGrace     = 30 * time.Second
	DefaultMaxConcurrent = 4
)

const (
	// A scheduled slot older than this when claimed runs as a catch-up.
	catchupAfter = time.Minute
	// Every write after a run gets its own timeout, detached from the run.
	writeTimeout = 10 * time.Second
	// Healthy fails once Due/Claim have failed continuously this long.
	unhealthyAfter = 5 * time.Minute
	maxErrorBytes  = 2048
	dueBatch       = 100
	// A failed outcome write is retried this many times in all, waiting
	// completeBackoff, then twice that, between attempts.
	completeAttempts = 3
	completeBackoff  = 100 * time.Millisecond
)

var (
	errStopping      = errors.New("scheduler stopping")
	errLeaseLost     = errors.New("lease lost")
	errModuleStopped = errors.New("module stopped")
)

// Options configure an Engine. Zero values take the defaults above.
type Options struct {
	Replica      string
	Clock        Clock
	Logger       *slog.Logger
	PollInterval time.Duration
	// LeaseTTL must exceed RenewInterval + 10 s (the write timeout): a run
	// is cancelled that long before the lease it last renewed expires.
	LeaseTTL      time.Duration
	RenewInterval time.Duration
	StopGrace     time.Duration
	MaxConcurrent int
	// DefaultLocation returns the module's timezone; it is read on every
	// poll so a config change applies live: when its name differs from the
	// one the last clean Reconcile used, Tick reconciles at once, so cron
	// jobs are rescheduled from the moment the change is seen. nil (or a nil
	// result) is UTC.
	DefaultLocation func() *time.Location
	// ModuleStarted gates claims: a job of a stopped module is never
	// claimed, not even on request. A run in progress whose module stops is
	// cancelled (checked on every Tick and every RenewInterval). nil means
	// every module is started.
	ModuleStarted func(module string) bool
	Metrics       Metrics
	// Jitter returns a value in [0,1) for the retry backoff.
	Jitter func() float64
}

func (o Options) withDefaults() Options {
	if o.Replica == "" {
		o.Replica, _ = os.Hostname()
	}
	if o.Clock == nil {
		o.Clock = systemClock{}
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	if o.PollInterval <= 0 {
		o.PollInterval = DefaultPollInterval
	}
	if o.LeaseTTL <= 0 {
		o.LeaseTTL = DefaultLeaseTTL
	}
	if o.RenewInterval <= 0 {
		o.RenewInterval = DefaultRenewInterval
	}
	if o.StopGrace <= 0 {
		o.StopGrace = DefaultStopGrace
	}
	if o.MaxConcurrent <= 0 {
		o.MaxConcurrent = DefaultMaxConcurrent
	}
	if o.ModuleStarted == nil {
		o.ModuleStarted = func(string) bool { return true }
	}
	if o.Metrics == nil {
		o.Metrics = nopMetrics{}
	}
	if o.Jitter == nil {
		o.Jitter = rand.Float64
	}
	return o
}

// Engine is one replica's scheduler.
type Engine struct {
	cat    *jobs.Catalog
	states repository.StateRepository
	runs   repository.RunRepository
	opt    Options
	log    *slog.Logger
	kick   chan struct{}

	tickMu sync.Mutex     // serialises Tick
	wg     sync.WaitGroup // run goroutines

	mu           sync.Mutex
	running      map[string]*runHandle
	started      bool
	stopping     bool
	loopExited   bool
	loopCancel   context.CancelFunc
	loopDone     chan struct{}
	failingSince time.Time
	wake         time.Time // earliest due time known locally
	reconciled   int       // catalog size at the last clean Reconcile; -1 = never
	// reconciledLoc is the default location name the last clean Reconcile
	// used; "" = never.
	reconciledLoc string
}

// New builds an engine; nothing runs until Start (or Tick in tests).
func New(cat *jobs.Catalog, states repository.StateRepository, runs repository.RunRepository, opt Options) *Engine {
	opt = opt.withDefaults()
	return &Engine{
		cat: cat, states: states, runs: runs, opt: opt,
		log:        opt.Logger,
		kick:       make(chan struct{}, 1),
		running:    map[string]*runHandle{},
		reconciled: -1,
	}
}

// plan is what one claim of a due job does.
type plan struct {
	trigger jobs.Trigger // "" when nothing executes: the claim only books state
	attempt int
	slot    *time.Time
	skip    bool // catch-up of a SkipMissed job: record skipped, reschedule
	// base is the claimed state after accounting for an abandoned run.
	base         models.State
	abandoned    *models.Lease
	clearRequest bool // the abandoned run was a manual one
}

// runHandle tracks one executing run.
type runHandle struct {
	entry     jobs.Entry
	eff       Effective
	plan      plan
	runID     string
	startedAt time.Time
	run       *jobs.Run
	log       *slog.Logger
	cancel    context.CancelCauseFunc

	finalized     atomic.Bool // whoever sets it writes the bookkeeping
	leaseLost     atomic.Bool // LeaseLost already counted
	moduleStopped atomic.Bool // cancelled because its module stopped
	stopOnce      sync.Once
	stopRenew     chan struct{}
	done          chan struct{} // closed once the bookkeeping is written
}

func (h *runHandle) endRenew() { h.stopOnce.Do(func() { close(h.stopRenew) }) }

func (e *Engine) now() time.Time { return e.opt.Clock.Now() }

func (e *Engine) defaultLocation() *time.Location {
	if e.opt.DefaultLocation == nil {
		return time.UTC
	}
	if l := e.opt.DefaultLocation(); l != nil {
		return l
	}
	return time.UTC
}

// Reconcile creates the state of every catalog entry that has none and
// reschedules every state whose fingerprint no longer matches its effective
// schedule (a code default changed, or the module timezone). States with an
// invalid override are logged and left alone.
//
// A fingerprint change reschedules from now: the next due time is computed
// strictly after now, so a slot that was already due under the old schedule
// is skipped, not run. Tick applies the same rule to a due state that
// drifted, and calls Reconcile as soon as it sees the module timezone change,
// so a cron job moves to the next slot of the new zone from that moment.
func (e *Engine) Reconcile(ctx context.Context) error {
	now := e.now()
	loc := e.defaultLocation()
	entries := e.cat.List()
	var errs []error
	for _, en := range entries {
		eff, err := EffectiveOf(en, nil, loc)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if err := e.states.EnsureState(ctx, en.Key, en.Module, eff.Fingerprint, NextRun(eff, now), now); err != nil {
			errs = append(errs, fmt.Errorf("jobs: ensure state of %s: %w", en.Key, err))
		}
	}
	all, err := e.states.List(ctx)
	if err != nil {
		return errors.Join(append(errs, fmt.Errorf("jobs: list states: %w", err))...)
	}
	byKey := make(map[string]models.State, len(all))
	for _, st := range all {
		byKey[st.Key] = st
	}
	for _, en := range entries {
		st, ok := byKey[en.Key]
		if !ok {
			continue
		}
		eff, err := EffectiveOf(en, &st, loc)
		if err != nil {
			e.log.Error("job override is invalid; job not scheduled", "job_key", en.Key, "error", err)
			continue
		}
		if st.Fingerprint != eff.Fingerprint {
			if err := e.reschedule(ctx, st, eff, now); err != nil {
				errs = append(errs, err)
			}
			continue
		}
		e.noteWake(st.NextRunAt, st.RetryAt, st.RunRequestedAt)
	}
	if len(errs) == 0 {
		e.mu.Lock()
		e.reconciled = len(entries)
		e.reconciledLoc = loc.String()
		e.mu.Unlock()
	}
	return errors.Join(errs...)
}

func (e *Engine) reschedule(ctx context.Context, st models.State, eff Effective, now time.Time) error {
	next := NextRun(eff, now)
	ok, err := e.states.Reschedule(ctx, st.Key, st.Revision, eff.Fingerprint, next, now)
	if err != nil {
		return fmt.Errorf("jobs: reschedule %s: %w", st.Key, err)
	}
	if ok {
		e.log.Info("job rescheduled", "job_key", st.Key, "from", st.Fingerprint, "to", eff.Fingerprint)
		e.noteWake(next)
	}
	return nil
}

// Tick polls once: it cancels the runs whose module stopped, reconciles if
// the module timezone changed, lists due jobs, claims what it can up to the
// free run slots and launches the runs. It returns how many runs it launched
// (claims that only book state, such as a skipped catch-up, do not count).
func (e *Engine) Tick(ctx context.Context) int {
	e.tickMu.Lock()
	defer e.tickMu.Unlock()
	e.mu.Lock()
	stopping := e.stopping
	e.mu.Unlock()
	if stopping {
		return 0
	}
	e.interruptStoppedModules()
	e.reconcileOnZoneChange(ctx)
	e.mu.Lock()
	free := e.opt.MaxConcurrent - len(e.running)
	e.mu.Unlock()
	if free <= 0 {
		return 0
	}
	keys := e.claimableKeys()
	if len(keys) == 0 {
		return 0
	}
	now := e.now()
	due, err := e.states.Due(ctx, now, keys, dueBatch)
	e.observe(err)
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			e.log.Error("jobs: listing due jobs failed", "error", err)
		}
		return 0
	}
	loc := e.defaultLocation()
	launched := 0
	for i := range due {
		if launched >= free {
			break
		}
		st := due[i]
		if e.isRunning(st.Key) {
			continue
		}
		entry, ok := e.cat.Get(st.Key)
		if !ok || !e.opt.ModuleStarted(entry.Module) {
			continue
		}
		eff, err := EffectiveOf(entry, &st, loc)
		if err != nil {
			e.log.Error("job override is invalid; job not scheduled", "job_key", st.Key, "error", err)
			continue
		}
		if st.Fingerprint != eff.Fingerprint {
			// The stored due time came from another schedule: recompute it
			// and decide on the next poll.
			if err := e.reschedule(ctx, st, eff, now); err != nil {
				e.log.Error("jobs: reschedule failed", "job_key", st.Key, "error", err)
			}
			continue
		}
		p, claim := e.decide(entry, eff, st, now)
		if !claim {
			if !eff.Enabled {
				// Disabled and due for its schedule or a retry: clear the
				// stale due time so Due stops returning it.
				if err := e.reschedule(ctx, st, eff, now); err != nil {
					e.log.Error("jobs: reschedule failed", "job_key", st.Key, "error", err)
				}
			}
			continue
		}
		if e.claimAndStart(ctx, entry, eff, st, p, now) {
			launched++
		}
	}
	return launched
}

// reconcileOnZoneChange runs Reconcile when the default location differs from
// the one the last clean Reconcile used. A failed Reconcile leaves the old
// name, so the next Tick tries again.
func (e *Engine) reconcileOnZoneChange(ctx context.Context) {
	name := e.defaultLocation().String()
	e.mu.Lock()
	last := e.reconciledLoc
	e.mu.Unlock()
	if last == "" || last == name {
		return
	}
	e.log.Info("jobs: module timezone changed; rescheduling", "from", last, "to", name)
	if err := e.Reconcile(ctx); err != nil && !errors.Is(err, context.Canceled) {
		e.log.Error("jobs: reconcile after a timezone change failed", "error", err)
	}
}

// interruptStoppedModules cancels every local run whose module is no longer
// started.
func (e *Engine) interruptStoppedModules() {
	e.mu.Lock()
	hs := make([]*runHandle, 0, len(e.running))
	for _, h := range e.running {
		hs = append(hs, h)
	}
	e.mu.Unlock()
	for _, h := range hs {
		e.interruptIfModuleStopped(h)
	}
}

// interruptIfModuleStopped cancels h once its module is no longer started.
// The run is booked interrupted without consuming an attempt (see requeue).
func (e *Engine) interruptIfModuleStopped(h *runHandle) {
	if e.opt.ModuleStarted(h.entry.Module) || !h.moduleStopped.CompareAndSwap(false, true) {
		return
	}
	h.log.Warn("jobs: the job's module stopped; cancelling the run", "module", h.entry.Module)
	h.cancel(errModuleStopped)
}

// claimableKeys lists the catalog keys this replica may claim: the job's
// module is started and the job is not running here. Due is restricted to
// them, so states nobody can claim (removed jobs, stopped modules) never
// crowd claimable ones out of the batch.
func (e *Engine) claimableKeys() []string {
	entries := e.cat.List()
	keys := make([]string, 0, len(entries))
	for _, en := range entries {
		if e.opt.ModuleStarted(en.Module) && !e.isRunning(en.Key) {
			keys = append(keys, en.Key)
		}
	}
	return keys
}

func (e *Engine) isRunning(key string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	_, ok := e.running[key]
	return ok
}

// observe tracks whether Due/Claim keep failing, for Healthy.
func (e *Engine) observe(err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	switch {
	case err == nil:
		e.failingSince = time.Time{}
	case errors.Is(err, context.Canceled):
	case e.failingSince.IsZero():
		e.failingSince = e.now()
	}
}

// decide picks what a claim of st does at now. Trigger precedence is manual >
// retry > schedule; a scheduled slot older than catchupAfter is a catch-up.
// An expired lease still carrying a run ID is a run whose replica died: it is
// accounted as a failed attempt first. claim is false when nothing is due and
// nothing needs booking.
func (e *Engine) decide(entry jobs.Entry, eff Effective, st models.State, now time.Time) (p plan, claim bool) {
	p.base = st
	p.base.Lease = nil
	if st.Lease != nil && st.Lease.RunID != "" {
		l := *st.Lease
		p.abandoned = &l
		// A lease without a trigger belonged to a claim that executed
		// nothing; there is no attempt to account for. The retry backoff
		// counts from l.Until, the earliest moment a takeover is legal (the
		// run was last known alive at its last renew, l.Until - LeaseTTL).
		if l.Trigger != "" {
			o := e.outcome(entry.Def, eff, p.base, jobs.Trigger(l.Trigger), l.Attempt, l.SlotAt, true, false, l.Until, now)
			if o.setNext {
				p.base.NextRunAt = o.next
			}
			p.base.RetryAt, p.base.RetrySlotAt, p.base.Attempt = o.retryAt, o.retrySlotAt, o.attempt
			if o.clearRequest {
				p.base.RunRequestedAt, p.base.RunRequestedBy = nil, ""
				p.clearRequest = true
			}
		}
	}
	if !eff.Enabled {
		p.base.NextRunAt, p.base.RetryAt, p.base.RetrySlotAt, p.base.Attempt = nil, nil, nil, 0
	}
	b := &p.base
	switch {
	case b.RunRequestedAt != nil:
		p.trigger, p.attempt = jobs.TriggerManual, 1
	case !eff.Enabled:
	case b.RetryAt != nil && !b.RetryAt.After(now):
		p.trigger, p.attempt, p.slot = jobs.TriggerRetry, b.Attempt+1, b.RetrySlotAt
	case b.NextRunAt != nil && !b.NextRunAt.After(now):
		slot := *b.NextRunAt
		p.trigger, p.attempt, p.slot = jobs.TriggerSchedule, 1, &slot
		if slot.Before(now.Add(-catchupAfter)) {
			p.trigger = jobs.TriggerCatchup
			p.skip = entry.Def.SkipMissed
		}
	}
	return p, p.trigger != "" || p.abandoned != nil
}

// outcome is the schedule fields after a run with the given trigger, attempt
// and slot finished. A retry waits Delay(attempt) from retryFrom.
type outcome struct {
	setNext      bool
	next         *time.Time
	retryAt      *time.Time
	retrySlotAt  *time.Time
	attempt      int
	clearRequest bool
}

func (e *Engine) outcome(def jobs.Definition, eff Effective, base models.State, trig jobs.Trigger, attempt int, slot *time.Time, failed, permanent bool, retryFrom, now time.Time) outcome {
	o := outcome{clearRequest: trig == jobs.TriggerManual}
	if trig == jobs.TriggerSchedule || trig == jobs.TriggerCatchup {
		o.setNext, o.next = true, NextRun(eff, now)
	}
	if !failed {
		return o // attempt 0, no retry
	}
	if !permanent && trig != jobs.TriggerManual && eff.Enabled && attempt < def.Retry.MaxAttempts {
		at := retryFrom.Add(def.Retry.Delay(attempt, e.opt.Jitter))
		o.retryAt, o.retrySlotAt, o.attempt = &at, slot, attempt
		return o
	}
	if trig == jobs.TriggerManual {
		// A failed manual run never retries and leaves a pending retry
		// of the schedule as it was.
		o.retryAt, o.retrySlotAt, o.attempt = base.RetryAt, base.RetrySlotAt, base.Attempt
	}
	return o
}

// requeue builds the state update after a run cut short because its module
// stopped: the claimed state is put back as it was (after any abandoned-run
// accounting), so the slot, retry or request is due again and the attempt
// is not consumed.
func requeue(eff Effective, p plan) repository.Completion {
	return repository.Completion{
		SetNext:           true,
		NextRunAt:         p.base.NextRunAt,
		RetryAt:           p.base.RetryAt,
		RetrySlotAt:       p.base.RetrySlotAt,
		Attempt:           p.base.Attempt,
		ClearRequest:      p.clearRequest,
		ExpectFingerprint: eff.Fingerprint,
	}
}

// completion builds the state update after an execution of p.
func (e *Engine) completion(entry jobs.Entry, eff Effective, p plan, failed, permanent bool, now time.Time) repository.Completion {
	o := e.outcome(entry.Def, eff, p.base, p.trigger, p.attempt, p.slot, failed, permanent, now, now)
	c := repository.Completion{
		SetNext:           o.setNext || p.abandoned != nil,
		NextRunAt:         o.next,
		RetryAt:           o.retryAt,
		RetrySlotAt:       o.retrySlotAt,
		Attempt:           o.attempt,
		ClearRequest:      o.clearRequest || p.clearRequest,
		ExpectFingerprint: eff.Fingerprint,
	}
	if !o.setNext {
		c.NextRunAt = p.base.NextRunAt // persists the abandoned-run accounting
	}
	return c
}

// claimAndStart claims st with a fresh lease and acts on p. It reports
// whether a run was launched.
func (e *Engine) claimAndStart(ctx context.Context, entry jobs.Entry, eff Effective, st models.State, p plan, now time.Time) bool {
	runID := uuid.NewString()
	executes := p.trigger != "" && !p.skip
	lease := models.Lease{Owner: e.opt.Replica, RunID: runID, Until: now.Add(e.opt.LeaseTTL), StartedAt: now}
	if executes {
		lease.Trigger, lease.Attempt, lease.SlotAt = string(p.trigger), p.attempt, p.slot
	}
	pre, err := e.states.Claim(ctx, entry.Key, st.Revision, lease, now)
	e.observe(err)
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			e.log.Error("jobs: claim failed", "job_key", entry.Key, "error", err)
		}
		return false
	}
	if pre == nil {
		return false // another replica won, or the state moved
	}
	log := e.log.With("job_key", entry.Key, "run_id", runID, "trigger", string(p.trigger), "attempt", p.attempt)
	// A leftover lease without a trigger ran nothing: release it quietly.
	deadRun := p.abandoned != nil && p.abandoned.Trigger != ""
	if deadRun {
		e.markAbandoned(entry.Key, p.abandoned, now)
	}
	switch {
	case p.trigger == "":
		c := e.completion(entry, eff, p, false, false, now)
		c.SetNext, c.NextRunAt = true, p.base.NextRunAt
		c.RetryAt, c.RetrySlotAt, c.Attempt = p.base.RetryAt, p.base.RetrySlotAt, p.base.Attempt
		if deadRun {
			c.LastRun = &models.LastRun{RunID: p.abandoned.RunID, Status: models.StatusAbandoned, StartedAt: p.abandoned.StartedAt, FinishedAt: now}
		}
		_, _ = e.complete(log, entry.Key, runID, c)
		return false
	case p.skip:
		e.skip(log, entry, eff, p, runID, now)
		return false
	}

	var scheduledFor *time.Time
	rec := &models.RunRecord{
		ID: runID, Key: entry.Key, Module: entry.Module, Trigger: string(p.trigger),
		Status: models.StatusRunning, Replica: e.opt.Replica, Attempt: p.attempt, StartedAt: now,
	}
	if p.trigger == jobs.TriggerManual {
		scheduledFor = p.base.RunRequestedAt
		rec.RequestedBy = p.base.RunRequestedBy
	} else {
		scheduledFor = p.slot
		rec.ScheduledFor = p.slot
	}
	wctx, cancelW := context.WithTimeout(context.Background(), writeTimeout)
	if err := e.runs.Insert(wctx, rec); err != nil {
		// The lease is ours: run anyway, the state still records the outcome.
		log.Error("jobs: recording the run start failed", "error", err)
	}
	cancelW()

	runCtx, stopTimeout := context.WithTimeout(context.WithoutCancel(ctx), entry.Def.Timeout)
	runCtx, cancel := context.WithCancelCause(runCtx)
	var sf time.Time
	if scheduledFor != nil {
		sf = *scheduledFor
	}
	h := &runHandle{
		entry: entry, eff: eff, plan: p, runID: runID, startedAt: now, log: log, cancel: cancel,
		run:       jobs.NewRun(entry.Key, runID, p.trigger, p.attempt, sf),
		stopRenew: make(chan struct{}),
		done:      make(chan struct{}),
	}
	e.mu.Lock()
	e.running[entry.Key] = h
	e.mu.Unlock()
	e.wg.Add(1)
	go e.execute(runCtx, h, stopTimeout)
	log.Info("job run started")
	return true
}

func (e *Engine) markAbandoned(key string, l *models.Lease, now time.Time) {
	wctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
	defer cancel()
	if err := e.runs.MarkIfRunning(wctx, l.RunID, models.StatusAbandoned, now); err != nil {
		e.log.Error("jobs: marking an abandoned run failed", "job_key", key, "run_id", l.RunID, "error", err)
	}
	e.opt.Metrics.RunFinished(key, models.StatusAbandoned, 0)
	e.log.Warn("job run abandoned: its lease expired", "job_key", key, "run_id", l.RunID,
		"trigger", l.Trigger, "attempt", l.Attempt, "owner", l.Owner)
}

// skip books a missed slot of a SkipMissed job without running it.
func (e *Engine) skip(log *slog.Logger, entry jobs.Entry, eff Effective, p plan, runID string, now time.Time) {
	rec := &models.RunRecord{
		ID: runID, Key: entry.Key, Module: entry.Module, Trigger: string(p.trigger),
		Status: models.StatusSkipped, Replica: e.opt.Replica, Attempt: p.attempt,
		ScheduledFor: p.slot, StartedAt: now, FinishedAt: &now,
	}
	wctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
	if err := e.runs.Insert(wctx, rec); err != nil {
		log.Error("jobs: recording a skipped run failed", "error", err)
	}
	cancel()
	c := e.completion(entry, eff, p, false, false, now)
	c.LastRun = &models.LastRun{RunID: runID, Status: models.StatusSkipped, StartedAt: now, FinishedAt: now}
	_, _ = e.complete(log, entry.Key, runID, c)
	e.opt.Metrics.RunFinished(entry.Key, models.StatusSkipped, 0)
	log.Info("missed job run skipped")
}

// complete writes c, each attempt with a fresh timeout. A write error is
// retried (the caller still holds, and keeps renewing, the lease); ok=false
// with a nil error means the lease is no longer ours. When every attempt
// fails, the error is returned and the lease is left to expire.
func (e *Engine) complete(log *slog.Logger, key, runID string, c repository.Completion) (bool, error) {
	var err error
	wait := completeBackoff
	for attempt := 1; attempt <= completeAttempts; attempt++ {
		if attempt > 1 {
			time.Sleep(wait)
			wait *= 2
		}
		wctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
		var ok bool
		ok, err = e.states.Complete(wctx, key, runID, c)
		cancel()
		if err != nil {
			log.Warn("jobs: writing the run outcome failed", "write_attempt", attempt, "error", err)
			continue
		}
		if !ok {
			log.Warn("jobs: lease lost before the run outcome was written", "reason", "lease_lost")
			return false, nil
		}
		if c.SetNext {
			e.noteWake(c.NextRunAt)
		}
		e.noteWake(c.RetryAt)
		return true, nil
	}
	log.Error("jobs: run outcome not written; the lease is left to expire", "reason", "outcome_write_failed", "error", err)
	return false, err
}

func (e *Engine) execute(ctx context.Context, h *runHandle, stopTimeout context.CancelFunc) {
	defer e.wg.Done()
	defer stopTimeout()
	go e.renew(h)
	defer h.endRenew() // the lease is renewed until the bookkeeping is done
	panicked, err := e.invoke(ctx, h)
	if !h.finalized.CompareAndSwap(false, true) {
		return // Stop already booked this run as interrupted
	}
	status, errText, permanent := classify(ctx, err, panicked)
	requeued := status == models.StatusInterrupted && errors.Is(context.Cause(ctx), errModuleStopped)
	e.book(h, status, errText, permanent, requeued)
}

func (e *Engine) invoke(ctx context.Context, h *runHandle) (panicked bool, err error) {
	defer func() {
		if v := recover(); v != nil {
			panicked, err = true, fmt.Errorf("panic: %v", v)
			h.log.Error("job panicked", "panic", fmt.Sprint(v), "stack", string(debug.Stack()))
		}
	}()
	return false, h.entry.Def.Run(ctx, h.run)
}

// classify maps a finished body to a run status.
func classify(ctx context.Context, err error, panicked bool) (status, errText string, permanent bool) {
	if err == nil {
		return models.StatusSucceeded, "", false
	}
	if panicked {
		return models.StatusFailed, truncate(err.Error()), false
	}
	text := err.Error()
	cause := context.Cause(ctx)
	switch {
	case errors.Is(cause, errStopping):
		return models.StatusInterrupted, truncate(errStopping.Error() + ": " + text), false
	case errors.Is(cause, errModuleStopped):
		return models.StatusInterrupted, truncate(errModuleStopped.Error() + ": " + text), false
	case errors.Is(cause, errLeaseLost):
		return models.StatusFailed, truncate(errLeaseLost.Error() + ": " + text), false
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return models.StatusTimedOut, truncate(text), false
	}
	return models.StatusFailed, truncate(text), jobs.IsPermanent(err)
}

// truncate cuts s to maxErrorBytes on a rune boundary.
func truncate(s string) string {
	if len(s) <= maxErrorBytes {
		return s
	}
	i := maxErrorBytes
	for i > 0 && !utf8.RuneStart(s[i]) {
		i--
	}
	return s[:i]
}

// book writes the outcome of h (run record, then state) and releases it.
// requeued puts the claimed state back instead of applying the outcome (a
// run cut short by its module stopping).
func (e *Engine) book(h *runHandle, status, errText string, permanent, requeued bool) {
	defer e.release(h)
	now := e.now()
	dur := max(now.Sub(h.startedAt), 0)
	details, dropped := h.run.Details()
	wctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
	if err := e.runs.Finish(wctx, h.runID, status, errText, now, dur.Milliseconds(), details, dropped); err != nil {
		h.log.Error("jobs: recording the run outcome failed", "error", err)
	}
	cancel()
	c := e.completion(h.entry, h.eff, h.plan, status != models.StatusSucceeded, permanent, now)
	if requeued {
		c = requeue(h.eff, h.plan)
	}
	c.LastRun = &models.LastRun{RunID: h.runID, Status: status, Error: errText, StartedAt: h.startedAt, FinishedAt: now, DurationMs: dur.Milliseconds()}
	ok, err := e.complete(h.log, h.entry.Key, h.runID, c)
	if !ok && err == nil && h.leaseLost.CompareAndSwap(false, true) {
		e.opt.Metrics.LeaseLost(h.entry.Key)
	}
	e.opt.Metrics.RunFinished(h.entry.Key, status, dur)
	if status == models.StatusSucceeded {
		h.log.Info("job run finished", "status", status, "duration", dur)
	} else {
		h.log.Warn("job run finished", "status", status, "duration", dur, "error", errText)
	}
}

func (e *Engine) release(h *runHandle) {
	e.mu.Lock()
	if e.running[h.entry.Key] == h {
		delete(e.running, h.entry.Key)
	}
	e.mu.Unlock()
	close(h.done)
	e.Wake() // capacity freed
}

// Wake makes the loop poll now instead of at its next wake time. The admin
// API calls it after a run request or an override change on this replica;
// other replicas see the change on their next poll. It never blocks.
func (e *Engine) Wake() {
	select {
	case e.kick <- struct{}{}:
	default:
	}
}

// renew extends the lease every RenewInterval until the run is booked
// (spec J8 as amended), and fences the run. A renew error is transient: the
// run goes on while a later renew can still land before the lease expires.
// The run is cancelled when Renew reports the lease is no longer ours, or at
// the fence, Until − (RenewInterval + writeTimeout) of the last successful
// renew, so it stops before another replica may claim (the margin covers the
// wait for the next renew tick plus one hung Renew call). The fence has its
// own timer, re-armed at least every RenewInterval so a clock jump is seen.
// Every renew tick also cancels the run if its module stopped.
func (e *Engine) renew(h *runHandle) {
	t := time.NewTicker(e.opt.RenewInterval)
	defer t.Stop()
	margin := e.opt.RenewInterval + writeTimeout
	until := h.startedAt.Add(e.opt.LeaseTTL) // the lease taken by the claim
	fenceAt := until.Add(-margin)
	fence := time.NewTimer(e.fenceWait(fenceAt))
	defer fence.Stop()
	for {
		var err error
		select {
		case <-h.stopRenew:
			return
		case <-fence.C:
			if e.now().Before(fenceAt) {
				fence.Reset(e.fenceWait(fenceAt))
				continue
			}
			err = errors.New("no renewal succeeded in time")
		case <-t.C:
			e.interruptIfModuleStopped(h)
			next := e.now().Add(e.opt.LeaseTTL)
			wctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
			var ok bool
			ok, err = e.states.Renew(wctx, h.entry.Key, h.runID, next)
			cancel()
			switch {
			case err == nil && ok:
				until, fenceAt = next, next.Add(-margin)
				fence.Reset(e.fenceWait(fenceAt))
				continue
			case err != nil && e.now().Before(fenceAt):
				h.log.Warn("jobs: lease renewal failed; retrying while the lease holds", "lease_until", until, "error", err)
				continue
			}
		}
		select {
		case <-h.stopRenew:
			return // already booked
		default:
		}
		if h.leaseLost.CompareAndSwap(false, true) {
			e.opt.Metrics.LeaseLost(h.entry.Key)
		}
		if err != nil {
			h.log.Warn("jobs: the lease is about to expire without a successful renewal; cancelling the run", "lease_until", until, "error", err)
		} else {
			h.log.Warn("jobs: lease taken over; cancelling the run")
		}
		h.cancel(errLeaseLost)
		return
	}
}

// fenceWait is how long the fence timer waits for fenceAt: never more than
// RenewInterval, so a jump of the clock is noticed, and zero once it passed.
func (e *Engine) fenceWait(fenceAt time.Time) time.Duration {
	return min(max(fenceAt.Sub(e.now()), 0), e.opt.RenewInterval)
}

func (e *Engine) noteWake(ts ...*time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, t := range ts {
		if t != nil && (e.wake.IsZero() || t.Before(e.wake)) {
			e.wake = *t
		}
	}
}

// sleepFor is how long the loop waits: PollInterval, or less when a due time
// known locally comes sooner.
func (e *Engine) sleepFor() time.Duration {
	e.mu.Lock()
	defer e.mu.Unlock()
	d := e.opt.PollInterval
	if !e.wake.IsZero() {
		until := e.wake.Sub(e.now())
		switch {
		case until <= 0:
			e.wake = time.Time{} // handled by the poll that just ran
		case until < d:
			d = until
		}
	}
	return d
}

// Start reconciles and then polls until ctx is cancelled or Stop is called.
// It blocks; run it in its own goroutine. Runs are detached from ctx: only
// Stop cancels them.
func (e *Engine) Start(ctx context.Context) {
	e.mu.Lock()
	if e.started || e.stopping {
		e.mu.Unlock()
		return
	}
	e.started = true
	loopCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	e.loopCancel, e.loopDone = cancel, done
	e.mu.Unlock()
	defer func() {
		cancel()
		e.mu.Lock()
		e.loopExited = true
		e.mu.Unlock()
		close(done)
	}()
	for {
		e.maybeReconcile(loopCtx)
		e.Tick(loopCtx)
		t := time.NewTimer(e.sleepFor())
		select {
		case <-loopCtx.Done():
			t.Stop()
			return
		case <-t.C:
		case <-e.kick:
			t.Stop()
		}
	}
}

// maybeReconcile runs Reconcile until it succeeds once, and again whenever
// the catalog grew (a module recovered by RetryInit registers its jobs late).
func (e *Engine) maybeReconcile(ctx context.Context) {
	n := len(e.cat.List())
	e.mu.Lock()
	need := e.reconciled != n
	e.mu.Unlock()
	if !need {
		return
	}
	if err := e.Reconcile(ctx); err != nil && !errors.Is(err, context.Canceled) {
		e.log.Error("jobs: reconcile failed", "error", err)
	}
}

// Stop stops polling, cancels the running jobs and waits up to StopGrace for
// them. A run that returns in time books its own outcome (interrupted if it
// returned an error); one still running after the grace is booked as
// interrupted on its behalf. Either way the lease is released and the
// interruption counts as a failed attempt, so the retry policy applies.
func (e *Engine) Stop(ctx context.Context) error {
	e.mu.Lock()
	if e.stopping {
		e.mu.Unlock()
		return nil
	}
	e.stopping = true
	cancelLoop, loopDone := e.loopCancel, e.loopDone
	e.mu.Unlock()
	if cancelLoop != nil {
		cancelLoop()
		select {
		case <-loopDone:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	// Wait out a Tick in flight; none launches anything after this.
	func() {
		e.tickMu.Lock()
		defer e.tickMu.Unlock()
	}()

	e.mu.Lock()
	handles := make([]*runHandle, 0, len(e.running))
	for _, h := range e.running {
		handles = append(handles, h)
	}
	e.mu.Unlock()
	for _, h := range handles {
		h.cancel(errStopping)
	}
	allDone := make(chan struct{})
	go func() { e.wg.Wait(); close(allDone) }()
	grace := time.NewTimer(e.opt.StopGrace)
	defer grace.Stop()
	var ctxErr error
	select {
	case <-allDone:
		return nil
	case <-grace.C:
	case <-ctx.Done():
		ctxErr = ctx.Err()
	}
	for _, h := range handles {
		if h.finalized.CompareAndSwap(false, true) {
			h.log.Warn("job did not stop within the grace period; booking it as interrupted")
			e.book(h, models.StatusInterrupted, errStopping.Error()+": the run did not return within the grace period", false, false)
			h.endRenew()
		}
	}
	for _, h := range handles {
		select {
		case <-h.done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return ctxErr
}

// Healthy reports why the scheduler is not working: the loop is not running,
// or listing and claiming due jobs has failed continuously for over 5 minutes.
func (e *Engine) Healthy() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	switch {
	case !e.started:
		return errors.New("jobs: scheduler not started")
	case e.loopExited:
		return errors.New("jobs: scheduler loop is not running")
	case !e.failingSince.IsZero() && e.now().Sub(e.failingSince) > unhealthyAfter:
		return fmt.Errorf("jobs: claiming due jobs has been failing since %s", e.failingSince.UTC().Format(time.RFC3339))
	}
	return nil
}

// drain waits for every launched run goroutine (tests).
func (e *Engine) drain() { e.wg.Wait() }
