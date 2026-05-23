package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra-cc/orkestra-addon-marketing/importers"
	"github.com/orkestra-cc/orkestra-addon-marketing/models"
	"github.com/orkestra-cc/orkestra-addon-marketing/repository"
	"github.com/orkestra-cc/orkestra-sdk/ctxauth"
)

// ErrImportFailed wraps unrecoverable importer errors. Per-row
// failures (a malformed row, a violated invariant) increment the
// stats and continue — the whole-job-failed path is reserved for
// systemic issues (malformed CSV header, repository outage).
var ErrImportFailed = errors.New("marketing: import failed")

// ErrUnknownImporter is returned by Enqueue when the requested adapter
// name doesn't exist in the catalog. Surfaces as 400 at the handler.
var ErrUnknownImporter = errors.New("marketing: unknown importer")

// IdempotencyLookbackWindow caps how far back FindByIdempotencyKey
// scans. 24h is the canonical Phase 3 value — wide enough that a
// reasonable retry budget is covered, narrow enough that a deliberate
// re-import (operator wants to re-run last week's import after fixing
// a mapping) isn't silently deduped.
const IdempotencyLookbackWindow = 24 * time.Hour

// ImportService orchestrates import-job lifecycle. Phase 3 moved
// execution behind a background Worker — Enqueue persists the job
// row in `queued`, buffers the upload to disk, hands the job to the
// worker, and returns immediately. The worker drains the queue and
// transitions the job through running → done|failed (or →
// paused_for_review when the pipeline parks a conflict).
type ImportService struct {
	jobs   *repository.ImportJobRepository
	worker *importers.Worker
	// catalog is held for the unknown-importer 400 — the worker has its
	// own copy, but the handler needs to fail fast before persisting.
	catalog map[string]importers.Importer
}

// NewImportService binds the orchestrator. The worker keeps its own
// references to the data repos + adapter catalog; this service only
// needs the catalog (for validation) and the worker (for enqueue).
func NewImportService(
	jobs *repository.ImportJobRepository,
	worker *importers.Worker,
	adapters ...importers.Importer,
) *ImportService {
	cat := make(map[string]importers.Importer, len(adapters))
	for _, a := range adapters {
		cat[a.Name()] = a
	}
	return &ImportService{
		jobs:    jobs,
		worker:  worker,
		catalog: cat,
	}
}

// ListImports proxies to the repository.
func (s *ImportService) ListImports(ctx context.Context, limit, skip int64) ([]models.ImportJob, error) {
	return s.jobs.List(ctx, limit, skip)
}

// GetImport returns one job by UUID.
func (s *ImportService) GetImport(ctx context.Context, jobUUID string) (*models.ImportJob, error) {
	return s.jobs.GetByUUID(ctx, jobUUID)
}

// Enqueue persists a queued import job + spools the payload + hands
// the job to the background worker. Returns the persisted job (status
// queued) so the handler can echo it back with the jobUuid + Location
// header.
//
// Idempotency: when the caller supplies an explicit key (operator
// retry header) or the auto-derived sha256(body || mapping) matches a
// recent job, this method returns the existing row instead of creating
// a new one. The existing row may be in ANY status — queued / running
// / done / failed / paused_for_review.
func (s *ImportService) Enqueue(
	ctx context.Context,
	importerName, sourceName string,
	body []byte,
	mapping importers.ColumnMapping,
	mappingJSON []byte,
	explicitIdempotencyKey string,
) (*models.ImportJob, error) {
	if _, ok := s.catalog[importerName]; !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownImporter, importerName)
	}

	tenantID, ok := ctxauth.GetTenantID(ctx)
	if !ok || tenantID == "" {
		return nil, fmt.Errorf("%w: missing tenant on context", ErrImportFailed)
	}

	idempotencyKey := explicitIdempotencyKey
	if idempotencyKey == "" {
		idempotencyKey = importers.ComputeIdempotencyKey(body, mapping)
	}

	if existing, err := s.jobs.FindByIdempotencyKey(ctx, idempotencyKey, IdempotencyLookbackWindow); err != nil {
		return nil, fmt.Errorf("%w: idempotency lookup: %v", ErrImportFailed, err)
	} else if existing != nil {
		return existing, nil
	}

	job := &models.ImportJob{
		UUID:           uuid.New().String(),
		Importer:       importerName,
		SourceName:     sourceName,
		Status:         models.ImportJobStatusQueued,
		IdempotencyKey: idempotencyKey,
		CreatedBy:      actorUUID(ctx),
	}
	if err := s.jobs.Create(ctx, job); err != nil {
		return nil, fmt.Errorf("%w: persist job: %v", ErrImportFailed, err)
	}

	// Buffer payload + mapping sidecar on disk. The worker uses these
	// to resume after a backend restart without needing the original
	// HTTP request to still be alive.
	payloadPath, err := s.worker.PersistPayload(job.UUID, body)
	if err != nil {
		_ = s.jobs.UpdateStatus(ctx, job.UUID, models.ImportJobStatusFailed, job.Stats, "spool_write_failed")
		return nil, fmt.Errorf("%w: %v", ErrImportFailed, err)
	}
	if err := s.worker.PersistMapping(payloadPath, mappingJSON); err != nil {
		_ = s.jobs.UpdateStatus(ctx, job.UUID, models.ImportJobStatusFailed, job.Stats, "mapping_write_failed")
		return nil, fmt.Errorf("%w: %v", ErrImportFailed, err)
	}

	enqErr := s.worker.Enqueue(importers.WorkerJob{
		JobUUID:     job.UUID,
		TenantID:    tenantID,
		Importer:    importerName,
		PayloadPath: payloadPath,
		MappingJSON: mappingJSON,
	})
	if enqErr != nil {
		// Queue full: leave the job in `queued` so the next worker
		// drain / boot-recovery sweep picks it up. We do NOT mark it
		// failed — `queued` is the right state and the worker's
		// resumeQueuedJobsFromDB handles backfill.
		if !errors.Is(enqErr, importers.ErrQueueFull) {
			return nil, fmt.Errorf("%w: enqueue: %v", ErrImportFailed, enqErr)
		}
	}

	return job, nil
}

func actorUUID(ctx context.Context) string {
	if u, ok := ctxauth.GetUserUUID(ctx); ok {
		return u
	}
	return ""
}
