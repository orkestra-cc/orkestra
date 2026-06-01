package rag

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/orkestra/backend/internal/addons/rag/handlers"
)

// RegisterInternalRoutes registers service-to-service RAG routes.
// Called by the monolith's RemoteRAGQueryProvider when modules are split.
func RegisterInternalRoutes(api huma.API, handler *handlers.InternalHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "internal-rag-query",
		Method:      http.MethodPost,
		Path:        "/v1/internal/rag/query",
		Summary:     "RAG query (internal)",
		Description: "Full RAG query with document scoping. Service-to-service endpoint.",
		Tags:        []string{"Internal RAG"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.Query)
}
