package aimodels

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/orkestra-cc/orkestra-addon-aimodels/handlers"
)

// RegisterInternalRoutes registers service-to-service AI model routes.
// These are called by the monolith's RemoteAIModelProvider when modules
// are split across services.
func RegisterInternalRoutes(api huma.API, handler *handlers.InternalHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "internal-ai-embed",
		Method:      http.MethodPost,
		Path:        "/v1/internal/ai/embed",
		Summary:     "Embed text (internal)",
		Description: "Generates an embedding vector for a single text. Service-to-service endpoint.",
		Tags:        []string{"Internal AI"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.Embed)

	huma.Register(api, huma.Operation{
		OperationID: "internal-ai-embed-batch",
		Method:      http.MethodPost,
		Path:        "/v1/internal/ai/embed-batch",
		Summary:     "Batch embed texts (internal)",
		Description: "Generates embedding vectors for multiple texts. Service-to-service endpoint.",
		Tags:        []string{"Internal AI"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.EmbedBatch)

	huma.Register(api, huma.Operation{
		OperationID: "internal-ai-complete",
		Method:      http.MethodPost,
		Path:        "/v1/internal/ai/complete",
		Summary:     "LLM completion (internal)",
		Description: "Generates an LLM completion. Service-to-service endpoint.",
		Tags:        []string{"Internal AI"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.Complete)

	huma.Register(api, huma.Operation{
		OperationID: "internal-ai-embedding-info",
		Method:      http.MethodGet,
		Path:        "/v1/internal/ai/embedding-info",
		Summary:     "Embedding provider info (internal)",
		Description: "Returns dimensions and model name for the embedding provider. Service-to-service endpoint.",
		Tags:        []string{"Internal AI"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.GetEmbeddingInfo)

	huma.Register(api, huma.Operation{
		OperationID: "internal-ai-llm-info",
		Method:      http.MethodGet,
		Path:        "/v1/internal/ai/llm-info",
		Summary:     "LLM provider info (internal)",
		Description: "Returns model name for the LLM provider. Service-to-service endpoint.",
		Tags:        []string{"Internal AI"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.GetLLMInfo)
}
