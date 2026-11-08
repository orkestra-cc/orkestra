// Package handlers exposes the Tier-1 admin endpoints of the jobs core module.
// Authorization is the route groups' (RequireSystemPermission on
// system.jobs.read / system.jobs.manage, see the module's routes.go); this
// layer translates between the HTTP envelope and the admin service.
package handlers

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/orkestra/backend/internal/core/jobs/services"
	"github.com/orkestra/backend/internal/shared/errcode"
)

// Handler serves /v1/admin/jobs.
type Handler struct {
	svc    *services.AdminService
	logger *slog.Logger
}

// New builds the handler.
func New(svc *services.AdminService, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{svc: svc, logger: logger}
}

// --- wire types ---------------------------------------------------------------

// JobsScheduleInput is an operator schedule.
type JobsScheduleInput struct {
	Kind         string `json:"kind" doc:"every or cron"`
	EverySeconds int64  `json:"everySeconds,omitempty" doc:"Interval in seconds for an every schedule (60 to 31622400)"`
	Cron         string `json:"cron,omitempty" doc:"5-field cron expression for a cron schedule, e.g. 0 2 * * *"`
}

func (s *JobsScheduleInput) input() *services.ScheduleInput {
	if s == nil {
		return nil
	}
	return &services.ScheduleInput{Kind: s.Kind, EverySeconds: s.EverySeconds, Cron: s.Cron}
}

// JobsListBody is the job list.
type JobsListBody struct {
	Jobs []services.JobsJobView `json:"jobs"`
}

// JobsRunsBody is one page of run history.
type JobsRunsBody struct {
	Runs       []services.JobsRunView `json:"runs"`
	NextCursor string                 `json:"nextCursor,omitempty" doc:"Pass as cursor to read the next page; absent on the last page"`
}

// JobsUpdateBody is a partial override change.
type JobsUpdateBody struct {
	Revision int64              `json:"revision" doc:"The job's revision as read; a stale one answers 409 jobs.revision_conflict"`
	Enabled  *bool              `json:"enabled,omitempty"`
	Schedule *JobsScheduleInput `json:"schedule,omitempty" doc:"Replaces the schedule; not allowed on a manual job"`
	Timezone *string            `json:"timezone,omitempty" doc:"IANA zone for cron schedules; an empty string removes the override"`
}

// JobsPreviewBody is a schedule to preview.
type JobsPreviewBody struct {
	Kind         string `json:"kind" doc:"every or cron"`
	EverySeconds int64  `json:"everySeconds,omitempty"`
	Cron         string `json:"cron,omitempty"`
	Timezone     string `json:"timezone,omitempty" doc:"IANA zone; defaults to the module timezone"`
}

// JobsPreviewResult lists the next due times of a schedule.
type JobsPreviewResult struct {
	Next        []time.Time `json:"next"`
	Description string      `json:"description"`
}

type ListRequest struct{}

type ListResponse struct{ Body JobsListBody }

type KeyRequest struct {
	Key string `path:"key" maxLength:"128" doc:"Job key, <module>.<name>"`
}

type JobResponse struct{ Body services.JobsJobView }

type RunsRequest struct {
	Key    string `path:"key" maxLength:"128"`
	Status string `query:"status" doc:"Filter by status: running, succeeded, failed, timed_out, abandoned, interrupted or skipped"`
	Cursor string `query:"cursor" maxLength:"512"`
	Limit  int    `query:"limit" minimum:"1" maximum:"100" default:"25"`
}

type RunsResponse struct{ Body JobsRunsBody }

type UpdateRequest struct {
	Key  string `path:"key" maxLength:"128"`
	Body JobsUpdateBody
}

type ClearRequest struct {
	Key      string `path:"key" maxLength:"128"`
	Revision int64  `query:"revision" required:"true" doc:"The job's revision as read"`
}

type PreviewRequest struct{ Body JobsPreviewBody }

type PreviewResponse struct{ Body JobsPreviewResult }

// --- operations ---------------------------------------------------------------

func (h *Handler) List(ctx context.Context, _ *ListRequest) (*ListResponse, error) {
	if h.svc == nil {
		return nil, errNotReady()
	}
	jobs, err := h.svc.List(ctx)
	if err != nil {
		return nil, h.fail(ctx, "list", err)
	}
	return &ListResponse{Body: JobsListBody{Jobs: jobs}}, nil
}

func (h *Handler) Get(ctx context.Context, req *KeyRequest) (*JobResponse, error) {
	if h.svc == nil {
		return nil, errNotReady()
	}
	v, err := h.svc.Get(ctx, req.Key)
	if err != nil {
		return nil, h.fail(ctx, "get", err)
	}
	return &JobResponse{Body: v}, nil
}

func (h *Handler) Runs(ctx context.Context, req *RunsRequest) (*RunsResponse, error) {
	if h.svc == nil {
		return nil, errNotReady()
	}
	runs, next, err := h.svc.Runs(ctx, req.Key, req.Status, req.Cursor, req.Limit)
	if err != nil {
		return nil, h.fail(ctx, "runs", err)
	}
	return &RunsResponse{Body: JobsRunsBody{Runs: runs, NextCursor: next}}, nil
}

func (h *Handler) Update(ctx context.Context, req *UpdateRequest) (*JobResponse, error) {
	if h.svc == nil {
		return nil, errNotReady()
	}
	v, err := h.svc.Update(ctx, req.Key, services.UpdateInput{
		Revision: req.Body.Revision,
		Enabled:  req.Body.Enabled,
		Schedule: req.Body.Schedule.input(),
		Timezone: req.Body.Timezone,
	})
	if err != nil {
		return nil, h.fail(ctx, "update", err)
	}
	return &JobResponse{Body: v}, nil
}

func (h *Handler) ClearOverride(ctx context.Context, req *ClearRequest) (*JobResponse, error) {
	if h.svc == nil {
		return nil, errNotReady()
	}
	v, err := h.svc.ClearOverride(ctx, req.Key, req.Revision)
	if err != nil {
		return nil, h.fail(ctx, "clear override", err)
	}
	return &JobResponse{Body: v}, nil
}

func (h *Handler) RequestRun(ctx context.Context, req *KeyRequest) (*JobResponse, error) {
	if h.svc == nil {
		return nil, errNotReady()
	}
	v, err := h.svc.RequestRun(ctx, req.Key)
	if err != nil {
		return nil, h.fail(ctx, "request run", err)
	}
	return &JobResponse{Body: v}, nil
}

func (h *Handler) Preview(ctx context.Context, req *PreviewRequest) (*PreviewResponse, error) {
	if h.svc == nil {
		return nil, errNotReady()
	}
	in := services.ScheduleInput{Kind: req.Body.Kind, EverySeconds: req.Body.EverySeconds, Cron: req.Body.Cron}
	next, desc, err := h.svc.Preview(ctx, in, req.Body.Timezone)
	if err != nil {
		return nil, h.fail(ctx, "preview", err)
	}
	return &PreviewResponse{Body: JobsPreviewResult{Next: next, Description: desc}}, nil
}

// errNotReady answers a request that reached the routes before Init built
// the service (never in a running server; OpenAPI generation and tests).
func errNotReady() error {
	return errcode.Internal(errcode.JobsPersistenceFailed, "The jobs module is not initialised; try again shortly")
}

// fail maps a service error to its error code. An error it cannot name is a
// persistence fault: logged here, answered 500 without its text.
func (h *Handler) fail(ctx context.Context, op string, err error) error {
	switch {
	case errors.Is(err, services.ErrNotFound):
		return errcode.NotFound(errcode.JobsNotFound, "No job with this key is registered")
	case errors.Is(err, services.ErrRevisionConflict):
		return errcode.Conflict(errcode.JobsRevisionConflict, "The job changed since it was loaded; reload it and try again")
	case errors.Is(err, services.ErrAlreadyRunning):
		return errcode.Conflict(errcode.JobsAlreadyRunning, "The job is running now; wait for the run to finish")
	case errors.Is(err, services.ErrAlreadyRequested):
		return errcode.Conflict(errcode.JobsAlreadyRequested, "A run of this job is already waiting for a free replica")
	case errors.Is(err, services.ErrModuleStopped):
		return errcode.Conflict(errcode.JobsModuleStopped, "The job's module is not started, so its jobs do not run")
	case errors.Is(err, services.ErrManualScheduleLocked):
		return errcode.UnprocessableEntity(errcode.JobsManualScheduleLocked, services.InputMessage(err, "This job runs only on request; its schedule cannot be changed"))
	case errors.Is(err, services.ErrInvalidSchedule):
		return errcode.UnprocessableEntity(errcode.JobsInvalidSchedule, services.InputMessage(err, "The schedule is not valid"))
	case errors.Is(err, services.ErrInvalidTimezone):
		return errcode.UnprocessableEntity(errcode.JobsInvalidTimezone, services.InputMessage(err, "The timezone is not an IANA zone name"))
	case errors.Is(err, services.ErrInvalidQuery):
		return errcode.BadRequest(errcode.JobsInvalidQuery, services.InputMessage(err, "The run history filter is not valid"))
	default:
		h.logger.ErrorContext(ctx, "jobs: admin operation failed", slog.String("operation", op), slog.String("error", err.Error()))
		return errcode.Internal(errcode.JobsPersistenceFailed, "The job state could not be read or written; try again")
	}
}
