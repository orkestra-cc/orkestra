package handlers

import (
	"context"

	"github.com/danielgtaylor/huma/v2"

	"github.com/orkestra/backend/internal/addons/rag/models"
	"github.com/orkestra/backend/internal/addons/rag/services"
)

// ModelHandler handles AI model configuration HTTP requests
type ModelHandler struct {
	modelService services.ModelService
}

// NewModelHandler creates a new ModelHandler
func NewModelHandler(modelSvc services.ModelService) *ModelHandler {
	return &ModelHandler{modelService: modelSvc}
}

func (h *ModelHandler) CreateModel(ctx context.Context, req *models.CreateModelRequest) (*models.CreateModelResponse, error) {
	model, err := h.modelService.CreateModel(ctx, req)
	if err != nil {
		return nil, huma.Error400BadRequest("Failed to create model", err)
	}
	return &models.CreateModelResponse{Body: *model}, nil
}

func (h *ModelHandler) ListModels(ctx context.Context, req *models.ListModelsRequest) (*models.ListModelsResponse, error) {
	list, err := h.modelService.ListModels(ctx, req.Type)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to list models", err)
	}
	resp := &models.ListModelsResponse{}
	resp.Body.Models = list
	return resp, nil
}

func (h *ModelHandler) GetModel(ctx context.Context, req *models.GetModelRequest) (*models.GetModelResponse, error) {
	model, err := h.modelService.GetModel(ctx, req.UUID)
	if err != nil {
		return nil, huma.Error404NotFound("Model not found", err)
	}
	return &models.GetModelResponse{Body: *model}, nil
}

func (h *ModelHandler) UpdateModel(ctx context.Context, req *models.UpdateModelRequest) (*models.UpdateModelResponse, error) {
	model, err := h.modelService.UpdateModel(ctx, req)
	if err != nil {
		return nil, huma.Error400BadRequest("Failed to update model", err)
	}
	return &models.UpdateModelResponse{Body: *model}, nil
}

func (h *ModelHandler) DeleteModel(ctx context.Context, req *models.DeleteModelRequest) (*models.DeleteModelResponse, error) {
	if err := h.modelService.DeleteModel(ctx, req.UUID); err != nil {
		return nil, huma.Error400BadRequest("Failed to delete model", err)
	}
	resp := &models.DeleteModelResponse{}
	resp.Body.Message = "Model deleted successfully"
	return resp, nil
}

func (h *ModelHandler) SetDefault(ctx context.Context, req *models.SetDefaultModelRequest) (*models.SetDefaultModelResponse, error) {
	if err := h.modelService.SetDefault(ctx, req.UUID); err != nil {
		return nil, huma.Error400BadRequest("Failed to set default model", err)
	}
	resp := &models.SetDefaultModelResponse{}
	resp.Body.Message = "Default model updated"
	return resp, nil
}

func (h *ModelHandler) FetchAvailableModels(ctx context.Context, req *models.FetchModelsRequest) (*models.FetchModelsResponse, error) {
	remoteModels, err := h.modelService.FetchAvailableModels(ctx, req.Body.Provider, req.Body.BaseURL, req.Body.APIKey)
	if err != nil {
		return nil, huma.Error400BadRequest("Failed to fetch models", err)
	}

	resp := &models.FetchModelsResponse{}
	for _, m := range remoteModels {
		resp.Body.Models = append(resp.Body.Models, models.AvailableModel{
			ID:      m.ID,
			OwnedBy: m.OwnedBy,
		})
	}
	return resp, nil
}

func (h *ModelHandler) TestConnectivity(ctx context.Context, req *models.TestModelRequest) (*models.TestModelResponse, error) {
	if err := h.modelService.TestConnectivity(ctx, req.UUID); err != nil {
		resp := &models.TestModelResponse{}
		resp.Body.Status = "error"
		resp.Body.Message = err.Error()
		return resp, nil
	}
	resp := &models.TestModelResponse{}
	resp.Body.Status = "ok"
	resp.Body.Message = "Connection successful"
	return resp, nil
}
