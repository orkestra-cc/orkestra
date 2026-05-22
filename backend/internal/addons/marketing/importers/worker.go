package importers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/orkestra-cc/orkestra-addon-marketing/models"
	"github.com/orkestra-cc/orkestra-addon-marketing/repository"
	"github.com/orkestra-cc/orkestra-sdk/ctxauth"
)

// ErrQueueFull is returned by Enqueue when the bounded queue would
// block. The HTTP layer maps this to 503 Service Unavailable so the
// caller knows to retry; we deliberately do not block on submission
// because that ties up an HTTP worker for the duration of a single
// busy import.
var ErrQueueFull = errors.New("marketing: import queue full")

// ErrShutdownTimeout is returned by Stop when in-flight jobs failed
// to drain within the graceful window.
var ErrShutdownTimeout = errors.New("marketing: import worker shutdown timed out")

// WorkerDeps wires the worker to its persistence + execution
// collaborators. The Catalog is the same adapter map ImportService
// uses; the worker resolves the importer by name when it dequeues a
// job. SpoolDir is where uploaded payloads are buffered to disk so a
// crash + restart can resume queued work without needing the
// original HTTP request to still be alive.
type WorkerDeps struct {
	Logger              *slog.Logger
	JobRepo             *repository.ImportJobRepository
	OrgRepo             *repository.OrganizationRepository
	PersonRepo          *repository.PersonRepository
	MshipRepo           *repository.MembershipRepository
	TagRepo             *repository.TagRepository
	Catalog             map[string]Importer
	SpoolDir            string
	Concurrency         int
	QueueBuffer         int
	GracefulStopTimeout time.Duration

	// Phase 3 — optional. ReviewParker is invoked by the pipeline on
	// blocking dedup conflicts; ActivityEmitter fires the auto-emission
	// hooks. Both may be nil during boot if the module's Init order
	// builds the worker before the services — in that case the
	// services should call Worker.WireServices once available.
	ReviewParker    ReviewParker
	ActivityEmitter ActivityEmitter
}

// WorkerJob is the unit of work the worker pulls off its queue. The
// row that holds the payload lives on disk under SpoolDir; PayloadPath
// is relative to it so the worker can be reconfigured to a different
// spool root between restarts (a Phase-4+ concern; today restarts
// preserve the path).
type WorkerJob struct {
	JobUUID     string
	TenantID    string
	Importer    string
	PayloadPath string
	MappingJSON []byte
}

// Worker is the long-lived background goroutine that drains queued
// import jobs. One Worker per backend instance — multiple replicas
// would each maintain their own queue; the JobRepo gating
// (ListAcrossTenantsByStatus + UpdateStatus's status guard) makes the
// "two workers race for the same job" case safe at the DB layer.
type Worker struct {
	deps  WorkerDeps
	queue chan WorkerJob
	wg    sync.WaitGroup
	once  sync.Once
	mu    sync.RWMutex // guards lateWiring fields
}

// WireServices is the late-wiring hook for the optional Phase-3
// collaborators (ReviewParker, ActivityEmitter). Module.Init has a
// boot-order dependency — the worker is built before the services
// because the services need the worker's catalog reference for tests.
// Calling WireServices once after both exist completes the wiring.
func (w *Worker) WireServices(rp ReviewParker, ae ActivityEmitter) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.deps.ReviewParker = rp
	w.deps.ActivityEmitter = ae
}

// NewWorker builds a Worker with the given deps. The queue channel is
// allocated immediately so Enqueue is safe to call before Start (the
// boot path in module.go is Init → admin enables module → Start, but
// tests can construct + Enqueue + Start in any order).
func NewWorker(deps WorkerDeps) *Worker {
	if deps.Concurrency <= 0 {
		deps.Concurrency = 2
	}
	if deps.QueueBuffer <= 0 {
		deps.QueueBuffer = 32
	}
	if deps.GracefulStopTimeout <= 0 {
		deps.GracefulStopTimeout = 30 * time.Second
	}
	if deps.Catalog == nil {
		deps.Catalog = map[string]Importer{}
	}
	return &Worker{
		deps:  deps,
		queue: make(chan WorkerJob, deps.QueueBuffer),
	}
}

// PersistPayload buffers the upload to disk under SpoolDir and returns
// the absolute path. The handler calls this before persisting the
// import-job row so that the row's PayloadPath reflects what's actually
// on disk.
func (w *Worker) PersistPayload(jobUUID string, body []byte) (string, error) {
	if err := os.MkdirAll(w.deps.SpoolDir, 0o755); err != nil {
		return "", fmt.Errorf("marketing: prepare spool dir: %w", err)
	}
	path := filepath.Join(w.deps.SpoolDir, jobUUID+".bin")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return "", fmt.Errorf("marketing: write spool file: %w", err)
	}
	return path, nil
}

// Enqueue hands the job to a worker goroutine. Returns ErrQueueFull
// when the bounded buffer is full — callers should treat that as a
// transient 503.
func (w *Worker) Enqueue(job WorkerJob) error {
	select {
	case w.queue <- job:
		return nil
	default:
		return ErrQueueFull
	}
}

// Start spawns the worker pool + runs the boot-recovery sweep. The
// passed context is the canonical "shutdown signal"; closing the
// queue from Stop is the secondary signal that runs each goroutine
// to completion.
func (w *Worker) Start(ctx context.Context) error {
	for i := 0; i < w.deps.Concurrency; i++ {
		w.wg.Add(1)
		go w.run(ctx)
	}
	return w.resumeQueuedJobsFromDB(ctx)
}

// Stop closes the queue producer side, waits for in-flight goroutines
// to drain up to GracefulStopTimeout, and returns. The boot-recovery
// sweep that runs on the next Start will recover any half-done work
// as runner_crash failures.
func (w *Worker) Stop(_ context.Context) error {
	w.once.Do(func() { close(w.queue) })
	done := make(chan struct{})
	go func() {
		w.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-time.After(w.deps.GracefulStopTimeout):
		return ErrShutdownTimeout
	}
}

// run is the goroutine body. It loops until the queue is closed AND
// the shutdown context cancels (either condition individually is
// enough to exit; both ensures we drain whatever's already in flight).
func (w *Worker) run(parentCtx context.Context) {
	defer w.wg.Done()
	for {
		select {
		case <-parentCtx.Done():
			return
		case job, ok := <-w.queue:
			if !ok {
				return
			}
			w.process(parentCtx, job)
		}
	}
}

// process executes a single job end-to-end. Errors are recorded on
// the job row + the spool payload is removed when the job terminates
// (success or failure).
func (w *Worker) process(parentCtx context.Context, job WorkerJob) {
	logger := w.deps.Logger
	if logger == nil {
		logger = slog.Default()
	}

	// Stamp tenantId onto a fresh ctx — the worker runs outside any
	// HTTP request, so the tenant scoping bus needs to be primed.
	ctx := context.WithValue(parentCtx, ctxauth.KeyTenantID, job.TenantID)

	defer func() {
		// Always clean the spool file. A failure to delete is logged
		// but never propagated — the worst case is a few stale .bin
		// files that ops can mop up.
		if job.PayloadPath != "" {
			if err := os.Remove(job.PayloadPath); err != nil && !os.IsNotExist(err) {
				logger.WarnContext(ctx, "marketing: failed to remove spool file",
					slog.String("jobUuid", job.JobUUID),
					slog.String("path", job.PayloadPath),
					slog.String("err", err.Error()),
				)
			}
		}
	}()

	stats, runErr := w.runPipeline(ctx, job)
	finalStatus := models.ImportJobStatusDone
	errMsg := ""
	if runErr != nil {
		finalStatus = models.ImportJobStatusFailed
		errMsg = runErr.Error()
	}
	if err := w.deps.JobRepo.UpdateStatus(ctx, job.JobUUID, finalStatus, stats, errMsg); err != nil {
		logger.ErrorContext(ctx, "marketing: stamp final job status failed",
			slog.String("jobUuid", job.JobUUID),
			slog.String("err", err.Error()),
		)
	}
}

// runPipeline is the inner loop that resolves the adapter, opens the
// spool file, and runs the existing pipeline against it. Separated
// from process() so the deferred cleanup runs no matter where the
// error occurred.
func (w *Worker) runPipeline(ctx context.Context, job WorkerJob) (models.ImportJobStats, error) {
	imp, ok := w.deps.Catalog[job.Importer]
	if !ok {
		return models.ImportJobStats{}, fmt.Errorf("marketing: no importer named %q", job.Importer)
	}
	if err := w.deps.JobRepo.UpdateStatus(ctx, job.JobUUID, models.ImportJobStatusRunning, models.ImportJobStats{}, ""); err != nil {
		return models.ImportJobStats{}, fmt.Errorf("marketing: mark running: %w", err)
	}

	reader, err := os.Open(job.PayloadPath)
	if err != nil {
		return models.ImportJobStats{}, fmt.Errorf("marketing: open spool: %w", err)
	}
	defer reader.Close()

	mapping, err := DecodeMapping(job.MappingJSON)
	if err != nil {
		return models.ImportJobStats{}, fmt.Errorf("marketing: decode mapping: %w", err)
	}

	src, err := imp.Parse(reader, mapping)
	if err != nil {
		return models.ImportJobStats{}, fmt.Errorf("marketing: adapter parse: %w", err)
	}
	defer src.Close()

	pipe := NewPipeline(job.JobUUID, job.Importer,
		w.deps.OrgRepo, w.deps.PersonRepo, w.deps.MshipRepo, w.deps.TagRepo)
	w.mu.RLock()
	parker := w.deps.ReviewParker
	emitter := w.deps.ActivityEmitter
	w.mu.RUnlock()
	if parker != nil {
		pipe = pipe.WithReviewParker(parker)
	}
	if emitter != nil {
		pipe = pipe.WithActivityEmitter(emitter)
	}
	stats, runErr := pipe.Run(ctx, src)
	return stats, runErr
}

// resumeQueuedJobsFromDB repopulates the in-process queue with jobs
// persisted as queued, and marks any stuck-in-running jobs as
// runner_crash failures. Idempotent — safe to call on every Start.
//
// Skipped jobs (paused_for_review) stay where they are; the resolve
// handler in PR-2 will re-enqueue them when the last review closes.
func (w *Worker) resumeQueuedJobsFromDB(ctx context.Context) error {
	logger := w.deps.Logger
	if logger == nil {
		logger = slog.Default()
	}

	// Mark previously-running jobs as crashed first — they cannot be
	// resumed (we have no reader for them mid-pipeline; sources[] on
	// committed rows tells ops what landed).
	running, err := w.deps.JobRepo.ListAcrossTenantsByStatus(ctx, models.ImportJobStatusRunning, 1000)
	if err != nil {
		return fmt.Errorf("marketing: list running jobs at boot: %w", err)
	}
	for _, j := range running {
		if err := w.deps.JobRepo.MarkRunnerCrash(ctx, j.UUID, j.TenantID); err != nil {
			logger.WarnContext(ctx, "marketing: mark runner_crash failed",
				slog.String("jobUuid", j.UUID),
				slog.String("err", err.Error()),
			)
		}
	}

	// Re-enqueue persisted-as-queued jobs. The spool file must still
	// be present — if a manual cleanup ran between processes, the job
	// transitions straight to failed with "spool_missing".
	queued, err := w.deps.JobRepo.ListAcrossTenantsByStatus(ctx, models.ImportJobStatusQueued, 1000)
	if err != nil {
		return fmt.Errorf("marketing: list queued jobs at boot: %w", err)
	}
	for _, j := range queued {
		path := filepath.Join(w.deps.SpoolDir, j.UUID+".bin")
		if _, err := os.Stat(path); err != nil {
			logger.WarnContext(ctx, "marketing: spool file missing for queued job",
				slog.String("jobUuid", j.UUID),
				slog.String("path", path),
				slog.String("err", err.Error()),
			)
			tCtx := context.WithValue(ctx, ctxauth.KeyTenantID, j.TenantID)
			if uerr := w.deps.JobRepo.UpdateStatus(tCtx, j.UUID, models.ImportJobStatusFailed, j.Stats, "spool_missing"); uerr != nil {
				logger.WarnContext(ctx, "marketing: failed to mark spool_missing",
					slog.String("jobUuid", j.UUID),
					slog.String("err", uerr.Error()),
				)
			}
			continue
		}
		// Best-effort enqueue. If the queue is already full at boot
		// (someone else enqueued before us), the job stays queued and
		// the next Start sweep picks it up.
		_ = w.Enqueue(WorkerJob{
			JobUUID:     j.UUID,
			TenantID:    j.TenantID,
			Importer:    j.Importer,
			PayloadPath: path,
			// Mapping is persisted alongside the spool file. See
			// DecodeMappingFile.
			MappingJSON: mappingJSONFromDisk(path),
		})
	}
	return nil
}

// mappingJSONFromDisk reads the sidecar mapping file next to the
// spool payload. Empty bytes on read failure — the pipeline parser
// surfaces that as an adapter_parse failure with a clear message.
func mappingJSONFromDisk(payloadPath string) []byte {
	mappingPath := payloadPath + ".mapping.json"
	b, err := os.ReadFile(mappingPath)
	if err != nil {
		return nil
	}
	return b
}

// PersistMapping writes the mapping sidecar next to the spool payload.
// Called by the handler at enqueue time so the boot-recovery sweep can
// reconstruct the WorkerJob without needing the original HTTP request.
func (w *Worker) PersistMapping(payloadPath string, mappingJSON []byte) error {
	return os.WriteFile(payloadPath+".mapping.json", mappingJSON, 0o600)
}
