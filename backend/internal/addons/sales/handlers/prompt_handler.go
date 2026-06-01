package handlers

import (
	"context"

	"github.com/danielgtaylor/huma/v2"

	"github.com/orkestra/backend/internal/addons/sales/models"
	"github.com/orkestra/backend/internal/addons/sales/repository"
	"github.com/orkestra/backend/internal/addons/sales/services"
)

// PromptHandler handles prompt management endpoints
type PromptHandler struct {
	repo         repository.PromptRepository
	promptLoader *services.PromptLoader
}

// NewPromptHandler creates a new PromptHandler
func NewPromptHandler(repo repository.PromptRepository, promptLoader *services.PromptLoader) *PromptHandler {
	return &PromptHandler{repo: repo, promptLoader: promptLoader}
}

// ListPrompts handles GET /v1/sales/prompts
func (h *PromptHandler) ListPrompts(ctx context.Context, req *models.ListPromptsRequest) (*models.ListPromptsResponse, error) {
	prompts, err := h.repo.List(ctx, req.Category)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to list prompts", err)
	}

	resp := &models.ListPromptsResponse{}
	resp.Body.Prompts = prompts
	return resp, nil
}

// GetPrompt handles GET /v1/sales/prompts/{uuid}
func (h *PromptHandler) GetPrompt(ctx context.Context, req *models.GetPromptRequest) (*models.GetPromptResponse, error) {
	prompt, err := h.repo.GetByUUID(ctx, req.UUID)
	if err != nil {
		return nil, huma.Error404NotFound("Prompt not found", err)
	}
	return &models.GetPromptResponse{Body: *prompt}, nil
}

// UpdatePrompt handles PATCH /v1/sales/prompts/{uuid}
func (h *PromptHandler) UpdatePrompt(ctx context.Context, req *models.UpdatePromptRequest) (*models.UpdatePromptResponse, error) {
	prompt, err := h.repo.GetByUUID(ctx, req.UUID)
	if err != nil {
		return nil, huma.Error404NotFound("Prompt not found", err)
	}

	prompt.Content = req.Body.Content
	prompt.IsCustom = true
	if req.Body.DisplayName != "" {
		prompt.DisplayName = req.Body.DisplayName
	}
	if req.Body.Description != "" {
		prompt.Description = req.Body.Description
	}

	if err := h.repo.Upsert(ctx, prompt); err != nil {
		return nil, huma.Error500InternalServerError("Failed to update prompt", err)
	}

	// Clear template cache so next execution uses updated content
	h.promptLoader.InvalidateCache()

	return &models.UpdatePromptResponse{Body: *prompt}, nil
}

// ResetPrompt handles POST /v1/sales/prompts/{uuid}/reset — restores embedded default
func (h *PromptHandler) ResetPrompt(ctx context.Context, req *models.ResetPromptRequest) (*models.ResetPromptResponse, error) {
	prompt, err := h.repo.GetByUUID(ctx, req.UUID)
	if err != nil {
		return nil, huma.Error404NotFound("Prompt not found", err)
	}

	defaultContent, err := h.promptLoader.GetDefaultContent(prompt.Category, prompt.Name)
	if err != nil {
		return nil, huma.Error500InternalServerError("Default prompt not found", err)
	}

	prompt.Content = defaultContent
	prompt.IsCustom = false

	if err := h.repo.Upsert(ctx, prompt); err != nil {
		return nil, huma.Error500InternalServerError("Failed to reset prompt", err)
	}

	h.promptLoader.InvalidateCache()

	return &models.ResetPromptResponse{Body: *prompt}, nil
}
