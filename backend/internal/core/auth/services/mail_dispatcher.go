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
type MailDispatcher struct {
	log  *slog.Logger
	jobs chan MailJob
	wg   sync.WaitGroup

	mu      sync.Mutex
	started bool
	stopped bool

	dropped atomic.Int64
}

func NewMailDispatcher(log *slog.Logger) *MailDispatcher {
	if log == nil {
		log = slog.Default()
	}
	return &MailDispatcher{
		log:  log,
		jobs: make(chan MailJob, MailQueueCapacity),
	}
}

// Start launches the worker pool. Idempotent.
func (d *MailDispatcher) Start() {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.started || d.stopped {
		return
	}
	d.started = true
	for i := 0; i < MailWorkers; i++ {
		d.wg.Add(1)
		go d.worker()
	}
}

// Stop closes the queue and waits up to mailDrainTimeout (or until ctx
// is done, whichever comes first) for the workers to finish what is
// queued. Idempotent.
func (d *MailDispatcher) Stop(ctx context.Context) {
	if d == nil {
		return
	}
	d.mu.Lock()
	if d.stopped || !d.started {
		d.stopped = true
		d.mu.Unlock()
		return
	}
	d.stopped = true
	close(d.jobs)
	d.mu.Unlock()

	done := make(chan struct{})
	go func() {
		d.wg.Wait()
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
// It NEVER blocks and NEVER creates a goroutine.
func (d *MailDispatcher) Enqueue(job MailJob) bool {
	if d == nil || job.Send == nil {
		return false
	}
	d.mu.Lock()
	stopped := d.stopped || !d.started
	d.mu.Unlock()
	if stopped {
		return false
	}

	select {
	case d.jobs <- job:
		return true
	default:
		d.dropped.Add(1)
		metrics.Default().RecordAuthMailDropped(job.TemplateID)
		// Template id and request id only — never the address.
		d.log.Warn("auth: mail dropped, dispatcher queue full",
			slog.String("template", job.TemplateID),
			slog.String("request_id", job.RequestID),
			slog.Int("queue_capacity", MailQueueCapacity),
		)
		return false
	}
}

// Dropped reports the process-local drop count (tests and diagnostics;
// the metric is the operational signal).
func (d *MailDispatcher) Dropped() int64 {
	if d == nil {
		return 0
	}
	return d.dropped.Load()
}

func (d *MailDispatcher) worker() {
	defer d.wg.Done()
	for job := range d.jobs {
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
