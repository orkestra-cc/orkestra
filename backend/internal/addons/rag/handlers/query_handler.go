package handlers

import (
	"context"

	"github.com/danielgtaylor/huma/v2"

	"github.com/orkestra-cc/orkestra-sdk/iface"
	"github.com/orkestra/backend/internal/addons/rag/models"
	"github.com/orkestra/backend/internal/addons/rag/services"
)

// QueryHandler handles RAG query HTTP requests
type QueryHandler struct {
	queryService services.QueryService
}

// NewQueryHandler creates a new QueryHandler
func NewQueryHandler(querySvc services.QueryService) *QueryHandler {
	return &QueryHandler{queryService: querySvc}
}

func (h *QueryHandler) Query(ctx context.Context, req *models.RAGQueryRequest) (*iface.RAGQueryResponse, error) {
	resp, err := h.queryService.Query(ctx, req.Body.Question, req.Body.TopK, req.Body.MinScore, req.Body.ISOStandard, req.Body.ModelUUID, req.Body.RequirementLevel, req.Body.NodeType, req.Body.RetrievalMode, nil)
	if err != nil {
		return nil, huma.Error500InternalServerError("RAG query failed", err)
	}
	return resp, nil
}
