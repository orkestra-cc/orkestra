package services

import (
	"context"
	"errors"
	"fmt"
	"io"
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

// ImportService orchestrates one import-job lifecycle. The Phase-1
// flow runs synchronously inside the POST handler: create the row,
// stream the file through the pipeline, update the final state.
// Async runners (Phase 2+) wrap the same primitives behind a poller.
type ImportService struct {
	jobs    *repository.ImportJobRepository
	orgs    *repository.OrganizationRepository
	persons *repository.PersonRepository
	mships  *repository.MembershipRepository
	tags    *repository.TagRepository

	// importers indexed by Name(). Phase 1 ships only "csv"; future
	// adapters drop in here without changing the public service API.
	importers map[string]importers.Importer
}

// NewImportService binds the orchestrator with its collaborators
// and the adapter catalog. Pass at least one importer; an empty map
// makes the service inert (every Run call fails with
// ErrImportFailed wrapping "no importer registered").
func NewImportService(
	jobs *repository.ImportJobRepository,
	orgs *repository.OrganizationRepository,
	persons *repository.PersonRepository,
	mships *repository.MembershipRepository,
	tags *repository.TagRepository,
	adapters ...importers.Importer,
) *ImportService {
	cat := make(map[string]importers.Importer, len(adapters))
	for _, a := range adapters {
		cat[a.Name()] = a
	}
	return &ImportService{
		jobs:      jobs,
		orgs:      orgs,
		persons:   persons,
		mships:    mships,
		tags:      tags,
		importers: cat,
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

// Run is the Phase-1 sync entry point. The job row is created at
// status=running, the pipeline streams the reader, and the final
// status (done|failed) is stamped before returning. The returned
// ImportJob carries the final stats.
//
// The handler keeps the HTTP request open for the duration — Phase
// 1 imports are operator-driven and typically complete in seconds.
// Phase 2 will move long-running imports behind a background poller
// that reads queued jobs off the same collection.
func (s *ImportService) Run(
	ctx context.Context,
	importerName, sourceName string,
	reader io.Reader,
	mapping importers.ColumnMapping,
) (*models.ImportJob, error) {
	imp, ok := s.importers[importerName]
	if !ok {
		return nil, fmt.Errorf("%w: no importer named %q", ErrImportFailed, importerName)
	}

	job := &models.ImportJob{
		UUID:       uuid.New().String(),
		Importer:   importerName,
		SourceName: sourceName,
		Status:     models.ImportJobStatusRunning,
		CreatedBy:  actorUUID(ctx),
	}
	now := time.Now().UTC()
	job.StartedAt = &now
	if err := s.jobs.Create(ctx, job); err != nil {
		return nil, fmt.Errorf("%w: persist job: %v", ErrImportFailed, err)
	}

	src, err := imp.Parse(reader, mapping)
	if err != nil {
		_ = s.jobs.UpdateStatus(ctx, job.UUID, models.ImportJobStatusFailed, job.Stats, err.Error())
		return nil, fmt.Errorf("%w: parse: %v", ErrImportFailed, err)
	}
	defer src.Close()

	pipe := importers.NewPipeline(job.UUID, importerName, s.orgs, s.persons, s.mships, s.tags)
	stats, runErr := pipe.Run(ctx, src)
	job.Stats = stats

	finalStatus := models.ImportJobStatusDone
	errMsg := ""
	if runErr != nil {
		finalStatus = models.ImportJobStatusFailed
		errMsg = runErr.Error()
	}
	if err := s.jobs.UpdateStatus(ctx, job.UUID, finalStatus, stats, errMsg); err != nil {
		return nil, fmt.Errorf("%w: stamp final status: %v", ErrImportFailed, err)
	}
	final, err := s.jobs.GetByUUID(ctx, job.UUID)
	if err != nil {
		return nil, err
	}
	return final, nil
}

func actorUUID(ctx context.Context) string {
	if u, ok := ctxauth.GetUserUUID(ctx); ok {
		return u
	}
	return ""
}
