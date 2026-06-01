package handlers

import (
	"context"

	"github.com/danielgtaylor/huma/v2"

	"github.com/orkestra/backend/internal/addons/rag/models"
	"github.com/orkestra/backend/internal/addons/rag/services"
)

// RelationshipHandler handles relationship type config HTTP requests.
type RelationshipHandler struct {
	service services.RelationshipTypeService
}

// NewRelationshipHandler creates a new RelationshipHandler.
func NewRelationshipHandler(svc services.RelationshipTypeService) *RelationshipHandler {
	return &RelationshipHandler{service: svc}
}

func (h *RelationshipHandler) List(ctx context.Context, _ *models.ListRelationshipTypesRequest) (*models.ListRelationshipTypesResponse, error) {
	rels, err := h.service.List(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to list relationship types", err)
	}
	resp := &models.ListRelationshipTypesResponse{}
	resp.Body.RelationshipTypes = rels
	return resp, nil
}

func (h *RelationshipHandler) Create(ctx context.Context, req *models.CreateRelationshipTypeRequest) (*models.CreateRelationshipTypeResponse, error) {
	rt, err := h.service.Create(ctx, req.Body.Name, req.Body.Description, req.Body.FromNode, req.Body.ToNode, req.Body.Properties, req.Body.Categories)
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	return &models.CreateRelationshipTypeResponse{Body: *rt}, nil
}

func (h *RelationshipHandler) Update(ctx context.Context, req *models.UpdateRelationshipTypeRequest) (*models.UpdateRelationshipTypeResponse, error) {
	rt, err := h.service.Update(ctx, req.UUID, req.Body.Description, req.Body.Properties, req.Body.Categories)
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	return &models.UpdateRelationshipTypeResponse{Body: *rt}, nil
}

func (h *RelationshipHandler) Delete(ctx context.Context, req *models.DeleteRelationshipTypeRequest) (*models.DeleteRelationshipTypeResponse, error) {
	if err := h.service.Delete(ctx, req.UUID); err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	resp := &models.DeleteRelationshipTypeResponse{}
	resp.Body.Message = "Relationship type deleted"
	return resp, nil
}
