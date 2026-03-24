package rag

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/chi/v5"

	"github.com/orkestra/backend/internal/rag/handlers"
)

// RegisterModelRoutes registers model management routes
func RegisterModelRoutes(api huma.API, handler *handlers.ModelHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "create-rag-model",
		Method:      http.MethodPost,
		Path:        "/v1/rag/models",
		Summary:     "Create AI model config",
		Description: "Creates a new AI model configuration for embedding or LLM provider.",
		Tags:        []string{"RAG Models"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.CreateModel)

	huma.Register(api, huma.Operation{
		OperationID: "list-rag-models",
		Method:      http.MethodGet,
		Path:        "/v1/rag/models",
		Summary:     "List AI models",
		Description: "Lists all configured AI models. Filter by type (embedding or llm).",
		Tags:        []string{"RAG Models"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.ListModels)

	huma.Register(api, huma.Operation{
		OperationID: "get-rag-model",
		Method:      http.MethodGet,
		Path:        "/v1/rag/models/{uuid}",
		Summary:     "Get AI model config",
		Description: "Returns a specific AI model configuration.",
		Tags:        []string{"RAG Models"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.GetModel)

	huma.Register(api, huma.Operation{
		OperationID: "update-rag-model",
		Method:      http.MethodPatch,
		Path:        "/v1/rag/models/{uuid}",
		Summary:     "Update AI model config",
		Description: "Updates an AI model configuration.",
		Tags:        []string{"RAG Models"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.UpdateModel)

	huma.Register(api, huma.Operation{
		OperationID: "delete-rag-model",
		Method:      http.MethodDelete,
		Path:        "/v1/rag/models/{uuid}",
		Summary:     "Delete AI model config",
		Description: "Deletes an AI model configuration.",
		Tags:        []string{"RAG Models"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.DeleteModel)

	huma.Register(api, huma.Operation{
		OperationID: "set-default-rag-model",
		Method:      http.MethodPost,
		Path:        "/v1/rag/models/{uuid}/default",
		Summary:     "Set default model",
		Description: "Sets this model as the default for its type (embedding or llm).",
		Tags:        []string{"RAG Models"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.SetDefault)

	huma.Register(api, huma.Operation{
		OperationID: "fetch-provider-models",
		Method:      http.MethodPost,
		Path:        "/v1/rag/models/fetch",
		Summary:     "Fetch available models from provider",
		Description: "Queries an AI provider endpoint and returns the list of available models.",
		Tags:        []string{"RAG Models"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.FetchAvailableModels)

	huma.Register(api, huma.Operation{
		OperationID: "test-rag-model",
		Method:      http.MethodPost,
		Path:        "/v1/rag/models/{uuid}/test",
		Summary:     "Test model connectivity",
		Description: "Tests connectivity to the AI provider and verifies the model is available.",
		Tags:        []string{"RAG Models"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.TestConnectivity)
}

// RegisterDocumentRoutes registers document ingestion routes
func RegisterDocumentRoutes(api huma.API, handler *handlers.DocumentHandler) {
	huma.Register(api, huma.Operation{
		OperationID:   "upload-rag-document",
		Method:        http.MethodPost,
		Path:          "/v1/rag/documents",
		Summary:       "Upload and ingest document",
		Description:   "Uploads a document (PDF/text), extracts text, chunks, embeds, and stores in the knowledge graph.",
		Tags:          []string{"RAG Documents"},
		Security:      []map[string][]string{{"bearerAuth": {}}},
		SkipValidateBody: true,
	}, handler.UploadDocument)

	huma.Register(api, huma.Operation{
		OperationID: "list-rag-documents",
		Method:      http.MethodGet,
		Path:        "/v1/rag/documents",
		Summary:     "List documents",
		Description: "Lists all ingested documents with status and metadata.",
		Tags:        []string{"RAG Documents"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.ListDocuments)

	huma.Register(api, huma.Operation{
		OperationID: "get-rag-document",
		Method:      http.MethodGet,
		Path:        "/v1/rag/documents/{uuid}",
		Summary:     "Get document details",
		Description: "Returns detailed information about an ingested document.",
		Tags:        []string{"RAG Documents"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.GetDocument)

	huma.Register(api, huma.Operation{
		OperationID: "update-rag-document",
		Method:      http.MethodPatch,
		Path:        "/v1/rag/documents/{uuid}",
		Summary:     "Update document metadata",
		Description: "Updates document metadata (title, ISO standard, version) in both MongoDB and the knowledge graph.",
		Tags:        []string{"RAG Documents"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.UpdateDocument)

	huma.Register(api, huma.Operation{
		OperationID: "get-rag-document-chunks",
		Method:      http.MethodGet,
		Path:        "/v1/rag/documents/{uuid}/chunks",
		Summary:     "Get document chunks",
		Description: "Returns all text chunks for a document, ordered by position.",
		Tags:        []string{"RAG Documents"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.GetDocumentChunks)

	huma.Register(api, huma.Operation{
		OperationID: "get-rag-document-sections",
		Method:      http.MethodGet,
		Path:        "/v1/rag/documents/{uuid}/sections",
		Summary:     "Get document sections",
		Description: "Returns the structural section tree for a document, ordered by position.",
		Tags:        []string{"RAG Documents"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.GetDocumentSections)

	huma.Register(api, huma.Operation{
		OperationID: "reprocess-rag-document",
		Method:      http.MethodPost,
		Path:        "/v1/rag/documents/{uuid}/reprocess",
		Summary:     "Reprocess document",
		Description: "Clears existing graph nodes for a document so it can be re-uploaded with the new pipeline.",
		Tags:        []string{"RAG Documents"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.ReprocessDocument)

	huma.Register(api, huma.Operation{
		OperationID: "delete-rag-document",
		Method:      http.MethodDelete,
		Path:        "/v1/rag/documents/{uuid}",
		Summary:     "Delete document",
		Description: "Deletes a document from both MongoDB and the knowledge graph.",
		Tags:        []string{"RAG Documents"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.DeleteDocument)

	huma.Register(api, huma.Operation{
		OperationID: "get-rag-document-relations",
		Method:      http.MethodGet,
		Path:        "/v1/rag/documents/{uuid}/relations",
		Summary:     "Get cross-document relations",
		Description: "Returns cross-document SIMILAR_TO relationships for a document, showing which chunks from other documents are related.",
		Tags:        []string{"RAG Documents"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.GetDocumentRelations)
}

// RegisterQueryRoutes registers RAG query routes
func RegisterQueryRoutes(api huma.API, handler *handlers.QueryHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "rag-query",
		Method:      http.MethodPost,
		Path:        "/v1/rag/query",
		Summary:     "Ask a question",
		Description: "Asks a question and returns an answer grounded in the ingested documents with source citations.",
		Tags:        []string{"RAG Query"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.Query)
}

// RegisterRelationshipTypeRoutes registers relationship type config routes
func RegisterRelationshipTypeRoutes(api huma.API, handler *handlers.RelationshipHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "list-rag-relationship-types",
		Method:      http.MethodGet,
		Path:        "/v1/rag/relationships",
		Summary:     "List relationship types",
		Description: "Lists all configured graph relationship types with category toggles.",
		Tags:        []string{"RAG Relationships"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.List)

	huma.Register(api, huma.Operation{
		OperationID: "create-rag-relationship-type",
		Method:      http.MethodPost,
		Path:        "/v1/rag/relationships",
		Summary:     "Create relationship type",
		Description: "Creates a new graph relationship type configuration.",
		Tags:        []string{"RAG Relationships"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.Create)

	huma.Register(api, huma.Operation{
		OperationID: "update-rag-relationship-type",
		Method:      http.MethodPatch,
		Path:        "/v1/rag/relationships/{uuid}",
		Summary:     "Update relationship type",
		Description: "Updates a relationship type's description, properties, or category toggles.",
		Tags:        []string{"RAG Relationships"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.Update)

	huma.Register(api, huma.Operation{
		OperationID: "delete-rag-relationship-type",
		Method:      http.MethodDelete,
		Path:        "/v1/rag/relationships/{uuid}",
		Summary:     "Delete relationship type",
		Description: "Deletes a non-system relationship type configuration.",
		Tags:        []string{"RAG Relationships"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.Delete)
}

// RegisterStreamRoute registers the SSE streaming query route on the Chi router directly
func RegisterStreamRoute(r chi.Router, handler *handlers.StreamHandler) {
	r.Post("/v1/rag/query/stream", handler.HandleQueryStream)
}
