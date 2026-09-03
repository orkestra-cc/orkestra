package services

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/orkestra/backend/pkg/sdk/metrics"
)

const (
	// MailQueueCapacity bounds MEMORY: at most this many jobs wait.
	MailQueueCapacity = 256
	// MailWorkers bounds CONCURRENCY against the SMTP relay.
	MailWorkers = 16
	// mailDrainTimeout is how long Stop waits for queued work before
	// abandoning it. A restart may lose a queued or in-flight reset
	// mail; the user retries inside the caps of D2.
	mailDrainTimeout = 10 * time.Second
	// mailJobTimeout bounds one detached send.
	mailJobTimeout = 60 * time.Second
	// mailDropWarningInterval throttles the "mail dropped" WARN so a
	// flood — exactly the scenario the queue-full path exists to
	// survive — produces one log line a minute, not one per dropped
	// request. Mirrors attempt_counter.go's report()/attemptWarningInterval
	// shape already shipped in this package: the metric records every
	// occurrence, the log samples it.
	mailDropWarningInterval = time.Minute
)

// MailJob is one detached transactional send.
//
// Send closes over everything the delivery needs (recipient, template
// data, notifier) so the dispatcher stays free of notification types and
// of anything the caller would otherwise have to keep alive.
//
// TemplateID is the metric label for a drop and the only identifier that
// reaches a log line. RequestID correlates the drop with the request that
// caused it. The recipient's ADDRESS is never logged — a WARN naming who
// did not get their password-reset mail is an enumeration oracle in the
// log pipeline.
type MailJob struct {
	TemplateID string
	RequestID  string
	Send       func(ctx context.Context) error
}

// MailDispatcher detaches transactional auth mail from the request that
// triggered it, with hard bounds in three dimensions:
//
//   - memory: MailQueueCapacity queued jobs, no more;
//   - concurrency: MailWorkers goroutines, started once at module Start;
//   - request latency: enqueue is non-blocking, so the handler's
//     response time depends on neither the SMTP relay nor the backlog.
//
// That last bound is a security property, not just a performance one:
// ForgotPassword must cost the same whether or not the address exists,
// and a blocking acquire would have reintroduced the difference for
// known addresses exactly when the queue is contended.
//
// A full queue DROPS the job with a metric. A drop is a lost mail the
// user recovers by asking again inside the D2 caps;
// orkestra_auth_mail_dropped_total is the alerting signal that the
// queue or the worker count is undersized.
//
// # Concurrency and the running/stopped state machine
//
// mu is a RWMutex, not a plain Mutex, and that choice is load-bearing:
// Enqueue holds a READ lock across its entire check-then-send — reading
// d.started AND performing the `select` on d.jobs as one atomic section
// — while Start/Stop hold the WRITE lock across every mutation of
// d.started/d.jobs/d.wg. Go's RWMutex blocks new RLock acquisitions once
// a Lock is waiting, so a concurrent Stop cannot close d.jobs out from
// under an Enqueue that already observed "running": Stop's Lock() call
// blocks until every in-flight Enqueue's RLock has released, and any
// Enqueue that arrives after Stop starts waiting is itself blocked until
// Stop finishes and re-reads d.started as false. A plain Mutex released
// between the "is it running" check and the send (the shape this
// replaced) left exactly that gap open: unlock, Stop closes the channel,
// then the stale send panics on a closed channel.
//
// started is "true between a successful Start and the matching Stop",
// not "Start has ever been called". Stop resets it to false and Start
// checks only `if d.started`, so calling Start again after Stop launches
// a fresh generation (a new queue, a new worker pool, a new WaitGroup) —
// this is what lets the module's hot enable/disable cycle
// (StartModule/StopModule, pkg/sdk/module/registry.go) actually restart
// mail delivery instead of leaving it permanently off after the first
// disable. jobs and wg are read into local variables by Stop while
// holding the write lock, so a concurrent Start that immediately begins
// a new generation cannot corrupt the generation Stop is still draining
// — each Stop call owns its own snapshot of the channel and WaitGroup it
// is waiting on, independent of whatever the next Start assigns to the
// struct fields.
type MailDispatcher struct {
	log *slog.Logger

	mu      sync.RWMutex
	jobs    chan MailJob
	wg      *sync.WaitGroup
	started bool

	dropped atomic.Int64

	// dropWarnMu/lastDropWarn throttle the drop WARN; see
	// mailDropWarningInterval.
	dropWarnMu   sync.Mutex
	lastDropWarn time.Time
}

func NewMailDispatcher(log *slog.Logger) *MailDispatcher {
	if log == nil {
		log = slog.Default()
	}
	return &MailDispatcher{log: log}
}

// Start launches a fresh worker pool bound to a fresh queue. Idempotent
// while already running (a second Start on a live dispatcher is a
// no-op); after a Stop, Start relaunches a new generation — see the
// type's doc comment for why that matters to the module's hot
// enable/disable cycle.
func (d *MailDispatcher) Start() {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.started {
		return
	}
	jobs := make(chan MailJob, MailQueueCapacity)
	wg := &sync.WaitGroup{}
	d.jobs = jobs
	d.wg = wg
	d.started = true
	for i := 0; i < MailWorkers; i++ {
		wg.Add(1)
		go d.worker(jobs, wg)
	}
}

// Stop closes the current generation's queue and waits up to
// mailDrainTimeout (or until ctx is done, whichever comes first) for its
// workers to finish what is queued, then marks the dispatcher as not
// running. Idempotent — calling Stop while already stopped is a no-op.
// A later Start relaunches a fresh generation.
func (d *MailDispatcher) Stop(ctx context.Context) {
	if d == nil {
		return
	}
	d.mu.Lock()
	if !d.started {
		d.mu.Unlock()
		return
	}
	jobs := d.jobs
	wg := d.wg
	d.started = false
	close(jobs)
	d.mu.Unlock()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	timer := time.NewTimer(mailDrainTimeout)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		d.log.Warn("auth: mail dispatcher abandoned queued sends at shutdown",
			slog.Duration("drain_timeout", mailDrainTimeout))
	case <-ctx.Done():
		d.log.Warn("auth: mail dispatcher shutdown context expired; queued sends abandoned")
	}
}

// Enqueue hands a job to the pool and returns whether it was accepted.
// It NEVER blocks and NEVER creates a goroutine. The whole
// check-then-send runs under one RLock — see the type's doc comment for
// why that is what makes a concurrent Stop safe rather than merely
// usually-safe.
func (d *MailDispatcher) Enqueue(job MailJob) bool {
	if d == nil || job.Send == nil {
		return false
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	if !d.started {
		d.recordDrop(job, "dispatcher_stopped")
		return false
	}

	select {
	case d.jobs <- job:
		return true
	default:
		d.recordDrop(job, "queue_full")
		return false
	}
}

// recordDrop counts one dropped job unconditionally — the metric is the
// operational signal and must reflect every occurrence — and logs it,
// throttled to at most one WARN per mailDropWarningInterval so a flood
// of drops produces one line a minute, not one per request. reason
// distinguishes a full queue from a dispatcher that is not currently
// running; both are lost mail the user recovers inside the D2 caps.
func (d *MailDispatcher) recordDrop(job MailJob, reason string) {
	d.dropped.Add(1)
	metrics.Default().RecordAuthMailDropped(job.TemplateID)

	d.dropWarnMu.Lock()
	throttled := !d.lastDropWarn.IsZero() && time.Since(d.lastDropWarn) < mailDropWarningInterval
	if !throttled {
		d.lastDropWarn = time.Now()
	}
	d.dropWarnMu.Unlock()
	if throttled {
		return
	}

	// Template id and request id only — never the address.
	d.log.Warn("auth: mail dropped",
		slog.String("reason", reason),
		slog.String("template", job.TemplateID),
		slog.String("request_id", job.RequestID),
		slog.Int("queue_capacity", MailQueueCapacity),
	)
}

// Dropped reports the process-local drop count (tests and diagnostics;
// the metric is the operational signal).
func (d *MailDispatcher) Dropped() int64 {
	if d == nil {
		return 0
	}
	return d.dropped.Load()
}

// worker is bound to one generation's queue and WaitGroup, passed
// explicitly rather than read off the struct fields — those fields can
// be reassigned by a later Start once this generation's Stop has
// returned, and a worker reading them live would race that reassignment.
func (d *MailDispatcher) worker(jobs <-chan MailJob, wg *sync.WaitGroup) {
	defer wg.Done()
	for job := range jobs {
		d.run(job)
	}
}

func (d *MailDispatcher) run(job MailJob) {
	// WithoutCancel: the request that queued this job is long gone by
	// the time a worker picks it up, and a send tied to that context
	// would be cancelled before it ever left the process.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(context.Background()), mailJobTimeout)
	defer cancel()

	defer func() {
		if r := recover(); r != nil {
			d.log.Error("auth: mail send panicked",
				slog.String("template", job.TemplateID),
				slog.Any("panic", r))
		}
	}()

	if err := job.Send(ctx); err != nil {
		// Delivery failures were already swallowed on this path; they
		// keep going to the log and to the notification log row.
		d.log.Warn("auth: mail send failed",
			slog.String("template", job.TemplateID),
			slog.String("error", err.Error()))
	}
}
