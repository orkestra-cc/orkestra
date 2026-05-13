package handlers

import (
	"context"
	"fmt"

	"github.com/danielgtaylor/huma/v2"

	"github.com/orkestra-cc/orkestra-addon-sales/models"
	"github.com/orkestra-cc/orkestra-addon-sales/services"
)

// JobHandler handles job management endpoints
type JobHandler struct {
	orchestrator services.OrchestratorService
}

// NewJobHandler creates a new JobHandler
func NewJobHandler(orchestrator services.OrchestratorService) *JobHandler {
	return &JobHandler{orchestrator: orchestrator}
}

// ListJobs handles GET /v1/sales/jobs
func (h *JobHandler) ListJobs(ctx context.Context, req *models.JobListRequest) (*models.JobListResponse, error) {
	userUUID, _ := ctx.Value("userUUID").(string)

	jobs, total, err := h.orchestrator.ListJobs(ctx, userUUID, req.Status, req.Page, req.PageSize)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to list jobs", err)
	}

	resp := &models.JobListResponse{}
	resp.Body.Jobs = jobs
	resp.Body.Total = total
	resp.Body.Page = req.Page
	resp.Body.PageSize = req.PageSize
	return resp, nil
}

// GetJob handles GET /v1/sales/jobs/{uuid}
func (h *JobHandler) GetJob(ctx context.Context, req *models.JobDetailRequest) (*models.JobDetailResponse, error) {
	job, err := h.orchestrator.GetJob(ctx, req.UUID)
	if err != nil {
		return nil, huma.Error404NotFound("Job not found", err)
	}

	return &models.JobDetailResponse{Body: *job}, nil
}

// DeleteJob handles DELETE /v1/sales/jobs/{uuid}
func (h *JobHandler) DeleteJob(ctx context.Context, req *models.JobDeleteRequest) (*struct{}, error) {
	userUUID, _ := ctx.Value("userUUID").(string)

	if err := h.orchestrator.DeleteJob(ctx, req.UUID, userUUID); err != nil {
		return nil, huma.Error500InternalServerError("Failed to delete job", err)
	}

	return nil, nil
}

// RerunFailedAgents handles POST /v1/sales/jobs/{uuid}/rerun
func (h *JobHandler) RerunFailedAgents(ctx context.Context, req *models.JobRerunRequest) (*models.JobRerunResponse, error) {
	userUUID, _ := ctx.Value("userUUID").(string)

	job, err := h.orchestrator.RerunFailedAgents(ctx, req.UUID, userUUID)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to re-run failed agents", err)
	}

	return &models.JobRerunResponse{Body: *job}, nil
}

// RetryJob handles POST /v1/sales/jobs/{uuid}/retry
func (h *JobHandler) RetryJob(ctx context.Context, req *models.JobRetryRequest) (*models.ProspectResponse, error) {
	userUUID, _ := ctx.Value("userUUID").(string)

	job, err := h.orchestrator.RetryJob(ctx, req.UUID, userUUID)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to retry job", err)
	}

	resp := &models.ProspectResponse{}
	resp.Body.JobID = job.UUID
	resp.Body.StreamURL = fmt.Sprintf("/v1/sales/jobs/%s/stream", job.UUID)
	return resp, nil
}
