package handlers

import (
	"context"

	"github.com/orkestra/backend/internal/core/llm/models"
)

type LLMModelListResponse struct {
	Body struct {
		Items []models.LLMModelView `json:"items"`
	}
}

func (h *AdminHandler) ListModels(ctx context.Context, _ *struct{}) (*LLMModelListResponse, error) {
	list, err := h.catalog.ListModels(ctx)
	if err != nil {
		return nil, h.fail(ctx, "list models", err)
	}
	resp := &LLMModelListResponse{}
	resp.Body.Items = list
	return resp, nil
}

type LLMModelResponse struct {
	Body models.LLMModelView
}

type LLMModelCreateRequest struct {
	Body models.LLMModelBody
}

func (h *AdminHandler) CreateModel(ctx context.Context, req *LLMModelCreateRequest) (*LLMModelResponse, error) {
	m, err := h.catalog.CreateModel(ctx, req.Body.Input())
	if err != nil {
		return nil, h.fail(ctx, "create model", err)
	}
	return &LLMModelResponse{Body: models.LLMModelView{Model: *m, Grants: []models.LLMGrant{}}}, nil
}

type LLMModelPath struct {
	UUID string `path:"uuid" format:"uuid"`
}

func (h *AdminHandler) GetModel(ctx context.Context, req *LLMModelPath) (*LLMModelResponse, error) {
	v, err := h.catalog.GetModel(ctx, req.UUID)
	if err != nil {
		return nil, h.fail(ctx, "get model", err)
	}
	return &LLMModelResponse{Body: *v}, nil
}

type LLMModelPatchRequest struct {
	UUID string `path:"uuid" format:"uuid"`
	Body models.LLMModelPatchBody
}

// PatchModel changes only the fields present in the body; the merged model
// is validated as a whole.
func (h *AdminHandler) PatchModel(ctx context.Context, req *LLMModelPatchRequest) (*LLMModelResponse, error) {
	if _, err := h.catalog.PatchModel(ctx, req.UUID, req.Body); err != nil {
		return nil, h.fail(ctx, "patch model", err)
	}
	v, err := h.catalog.GetModel(ctx, req.UUID)
	if err != nil {
		return nil, h.fail(ctx, "get model", err)
	}
	return &LLMModelResponse{Body: *v}, nil
}

func (h *AdminHandler) DeleteModel(ctx context.Context, req *LLMModelPath) (*struct{}, error) {
	if err := h.catalog.DeleteModel(ctx, req.UUID); err != nil {
		return nil, h.fail(ctx, "delete model", err)
	}
	return nil, nil
}

type LLMGrantsPutRequest struct {
	UUID string `path:"uuid" format:"uuid"`
	Body models.LLMGrantsPutBody
}

// PutGrants sets who may use a model: access and the complete grant list,
// together. One non-member rejects the whole call (422) and nothing is
// written. The response is the model with its new access and grants.
func (h *AdminHandler) PutGrants(ctx context.Context, req *LLMGrantsPutRequest) (*LLMModelResponse, error) {
	v, err := h.catalog.ReplaceGrants(ctx, req.UUID, req.Body.Access, req.Body.UserUUIDs)
	if err != nil {
		return nil, h.fail(ctx, "replace grants", err)
	}
	return &LLMModelResponse{Body: *v}, nil
}
