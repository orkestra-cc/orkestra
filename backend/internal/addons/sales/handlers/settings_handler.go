package handlers

import (
	"context"

	"github.com/danielgtaylor/huma/v2"

	"github.com/orkestra/backend/internal/addons/sales/models"
	"github.com/orkestra/backend/internal/addons/sales/repository"
)

// SettingsHandler handles per-user sales settings endpoints
type SettingsHandler struct {
	repo repository.SettingsRepository
}

// NewSettingsHandler creates a new SettingsHandler
func NewSettingsHandler(repo repository.SettingsRepository) *SettingsHandler {
	return &SettingsHandler{repo: repo}
}

// GetSettings handles GET /v1/sales/settings
func (h *SettingsHandler) GetSettings(ctx context.Context, _ *models.GetSalesSettingsRequest) (*models.GetSalesSettingsResponse, error) {
	userUUID, _ := ctx.Value("userUUID").(string)

	settings, err := h.repo.GetByUser(ctx, userUUID)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to get settings", err)
	}

	return &models.GetSalesSettingsResponse{Body: *settings}, nil
}

// UpdateSettings handles PATCH /v1/sales/settings
func (h *SettingsHandler) UpdateSettings(ctx context.Context, req *models.UpdateSalesSettingsRequest) (*models.UpdateSalesSettingsResponse, error) {
	userUUID, _ := ctx.Value("userUUID").(string)

	settings, err := h.repo.GetByUser(ctx, userUUID)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to get settings", err)
	}

	if req.Body.ModelUUID != nil {
		settings.ModelUUID = *req.Body.ModelUUID
	}
	if req.Body.Temperature != nil {
		settings.Temperature = *req.Body.Temperature
	}
	if req.Body.MaxTokens != nil {
		settings.MaxTokens = *req.Body.MaxTokens
	}
	if req.Body.Locale != nil {
		settings.Locale = *req.Body.Locale
	}
	if req.Body.BatchMode != nil {
		settings.BatchMode = *req.Body.BatchMode
	}

	if err := h.repo.Upsert(ctx, settings); err != nil {
		return nil, huma.Error500InternalServerError("Failed to save settings", err)
	}

	return &models.UpdateSalesSettingsResponse{Body: *settings}, nil
}
