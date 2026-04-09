package handlers

import (
	"context"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/orkestra/backend/internal/addons/aimodels/models"
	"github.com/orkestra/backend/internal/addons/aimodels/services"
)

// ModelHandler handles AI model configuration HTTP requests
type ModelHandler struct {
	modelService services.AIModelService
}

// NewModelHandler creates a new ModelHandler
func NewModelHandler(modelSvc services.AIModelService) *ModelHandler {
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
	list, err := h.modelService.ListModels(ctx, req.Type, req.Provider, req.Category)
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
		return nil, huma.Error400BadRequest(err.Error())
	}

	resp := &models.FetchModelsResponse{}
	for _, m := range remoteModels {
		// Filter by modelType when capabilities are available
		if req.Body.ModelType != "" && m.Capabilities != "" {
			switch req.Body.ModelType {
			case "embedding":
				if !containsCap(m.Capabilities, "embedContent") {
					continue
				}
			case "llm":
				if !containsCap(m.Capabilities, "generateContent") {
					continue
				}
			}
		}
		resp.Body.Models = append(resp.Body.Models, models.AvailableModel{
			ID:           m.ID,
			OwnedBy:      m.OwnedBy,
			Capabilities: m.Capabilities,
		})
	}
	return resp, nil
}

func containsCap(capabilities, target string) bool {
	for _, c := range strings.Split(capabilities, ",") {
		if c == target {
			return true
		}
	}
	return false
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

func (h *ModelHandler) QuickPrompt(ctx context.Context, req *models.QuickPromptRequest) (*models.QuickPromptResponse, error) {
	response, timeMs, err := h.modelService.QuickPrompt(ctx, req.UUID, req.Body.Prompt)
	if err != nil {
		return nil, huma.Error400BadRequest("Quick prompt failed", err)
	}
	resp := &models.QuickPromptResponse{}
	resp.Body.Response = response
	resp.Body.TimeMs = timeMs
	return resp, nil
}
