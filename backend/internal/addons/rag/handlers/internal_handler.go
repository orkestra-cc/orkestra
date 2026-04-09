package handlers

import (
	"context"

	"github.com/danielgtaylor/huma/v2"

	"github.com/orkestra/backend/internal/addons/rag/models"
	"github.com/orkestra/backend/internal/addons/rag/services"
)

// InternalHandler handles service-to-service RAG query requests.
// Called by the monolith's RemoteRAGQueryProvider.
type InternalHandler struct {
	queryService services.QueryService
}

// NewInternalHandler creates a new InternalHandler
func NewInternalHandler(querySvc services.QueryService) *InternalHandler {
	return &InternalHandler{queryService: querySvc}
}

// Query executes a RAG query with full parameter support including document scoping.
func (h *InternalHandler) Query(ctx context.Context, req *models.InternalRAGQueryRequest) (*models.InternalRAGQueryResponse, error) {
	resp, err := h.queryService.Query(
		ctx,
		req.Body.Question,
		req.Body.TopK,
		req.Body.MinScore,
		req.Body.ISOStandard,
		req.Body.LLMOverrideUUID,
		req.Body.RequirementLevel,
		req.Body.NodeType,
		req.Body.RetrievalMode,
		req.Body.DocumentUUIDs,
	)
	if err != nil {
		return nil, huma.Error500InternalServerError("RAG query failed", err)
	}
	return resp, nil
}
