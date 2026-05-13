package handlers

import (
	"context"
	"fmt"

	"github.com/danielgtaylor/huma/v2"

	"github.com/orkestra-cc/orkestra-addon-sales/models"
	"github.com/orkestra-cc/orkestra-addon-sales/services"
)

// ProspectHandler handles prospect intelligence endpoints
type ProspectHandler struct {
	orchestrator services.OrchestratorService
}

// NewProspectHandler creates a new ProspectHandler
func NewProspectHandler(orchestrator services.OrchestratorService) *ProspectHandler {
	return &ProspectHandler{orchestrator: orchestrator}
}

// Prospect handles POST /v1/sales/prospect (async full analysis)
func (h *ProspectHandler) Prospect(ctx context.Context, req *models.ProspectRequest) (*models.ProspectResponse, error) {
	userUUID, _ := ctx.Value("userUUID").(string)

	job, err := h.orchestrator.CreateProspectJob(ctx, req.Body.URL, req.Body.Locale, userUUID)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to create prospect job", err)
	}

	resp := &models.ProspectResponse{}
	resp.Body.JobID = job.UUID
	resp.Body.StreamURL = fmt.Sprintf("/v1/sales/jobs/%s/stream", job.UUID)
	return resp, nil
}

// QuickProspect handles POST /v1/sales/prospect/quick (sync 60s snapshot)
func (h *ProspectHandler) QuickProspect(ctx context.Context, req *models.ProspectRequest) (*models.QuickProspectResponse, error) {
	userUUID, _ := ctx.Value("userUUID").(string)

	result, err := h.orchestrator.RunQuickProspect(ctx, req.Body.URL, req.Body.Locale, userUUID)
	if err != nil {
		return nil, huma.Error500InternalServerError("Quick prospect failed", err)
	}

	resp := &models.QuickProspectResponse{}
	resp.Body.Score = result.Score
	resp.Body.Grade = result.Grade
	resp.Body.CompanyName = result.CompanyName
	resp.Body.Summary = result.Summary
	resp.Body.Findings = result.Findings
	resp.Body.InputTokens = result.InputTokens
	resp.Body.OutputTokens = result.OutputTokens
	resp.Body.LatencyMs = result.LatencyMs
	return resp, nil
}
