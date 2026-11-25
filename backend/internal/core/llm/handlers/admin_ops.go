package handlers

import (
	"context"
	"log/slog"

	"github.com/orkestra/backend/internal/core/llm/models"
	"github.com/orkestra/backend/internal/core/llm/services"
)

// OpsHandler serves the operator's live checks: credential test, model
// discovery and the prompt test (services.AdminOps).
type OpsHandler struct {
	ops    *services.AdminOps
	logger *slog.Logger
}

func NewOpsHandler(ops *services.AdminOps, logger *slog.Logger) *OpsHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &OpsHandler{ops: ops, logger: logger}
}

type LLMCredentialTestResponse struct {
	Body models.LLMCredentialTestResult
}

// TestCredential checks the credential against its provider and records
// the outcome on it. A provider failure is a 200 with outcome error and an
// llm.* code; 429 llm.rate_limited past 10 calls a minute.
func (h *OpsHandler) TestCredential(ctx context.Context, req *LLMCredentialPath) (*LLMCredentialTestResponse, error) {
	res, err := h.ops.TestCredential(ctx, req.UUID)
	if err != nil {
		return nil, failure(ctx, h.logger, "test credential", err)
	}
	return &LLMCredentialTestResponse{Body: *res}, nil
}

type LLMDiscoveredModelsResponse struct {
	Body struct {
		Items []models.LLMDiscoveredModel `json:"items"`
	}
}

// DiscoverModels lists the model ids the credential's provider serves.
func (h *OpsHandler) DiscoverModels(ctx context.Context, req *LLMCredentialPath) (*LLMDiscoveredModelsResponse, error) {
	items, err := h.ops.DiscoverModels(ctx, req.UUID)
	if err != nil {
		return nil, failure(ctx, h.logger, "discover models", err)
	}
	resp := &LLMDiscoveredModelsResponse{}
	resp.Body.Items = items
	return resp, nil
}

type LLMPromptTestRequest struct {
	UUID string `path:"uuid" format:"uuid"`
	Body models.LLMPromptTestBody
}

type LLMPromptTestResponse struct {
	Body models.LLMPromptTestResult
}

// PromptTest sends one prompt to the model, ignoring the grants, and
// returns the collected answer.
func (h *OpsHandler) PromptTest(ctx context.Context, req *LLMPromptTestRequest) (*LLMPromptTestResponse, error) {
	res, err := h.ops.PromptTest(ctx, req.UUID, req.Body.Prompt)
	if err != nil {
		return nil, failure(ctx, h.logger, "prompt test", err)
	}
	return &LLMPromptTestResponse{Body: *res}, nil
}
