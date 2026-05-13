package aimodels

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/orkestra-cc/orkestra-addon-aimodels/handlers"
)

// RegisterRoutes registers all AI model management routes
func RegisterRoutes(api huma.API, handler *handlers.ModelHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "create-ai-model",
		Method:      http.MethodPost,
		Path:        "/v1/ai/models",
		Summary:     "Create AI model config",
		Description: "Creates a new AI model configuration for embedding or LLM provider.",
		Tags:        []string{"AI Models"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.CreateModel)

	huma.Register(api, huma.Operation{
		OperationID: "list-ai-models",
		Method:      http.MethodGet,
		Path:        "/v1/ai/models",
		Summary:     "List AI models",
		Description: "Lists all configured AI models. Filter by type, provider, or category.",
		Tags:        []string{"AI Models"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.ListModels)

	huma.Register(api, huma.Operation{
		OperationID: "get-ai-model",
		Method:      http.MethodGet,
		Path:        "/v1/ai/models/{uuid}",
		Summary:     "Get AI model config",
		Description: "Returns a specific AI model configuration.",
		Tags:        []string{"AI Models"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.GetModel)

	huma.Register(api, huma.Operation{
		OperationID: "update-ai-model",
		Method:      http.MethodPatch,
		Path:        "/v1/ai/models/{uuid}",
		Summary:     "Update AI model config",
		Description: "Updates an AI model configuration.",
		Tags:        []string{"AI Models"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.UpdateModel)

	huma.Register(api, huma.Operation{
		OperationID: "delete-ai-model",
		Method:      http.MethodDelete,
		Path:        "/v1/ai/models/{uuid}",
		Summary:     "Delete AI model config",
		Description: "Deletes an AI model configuration.",
		Tags:        []string{"AI Models"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.DeleteModel)

	huma.Register(api, huma.Operation{
		OperationID: "set-default-ai-model",
		Method:      http.MethodPost,
		Path:        "/v1/ai/models/{uuid}/default",
		Summary:     "Set default model",
		Description: "Sets this model as the default for its type (embedding or llm).",
		Tags:        []string{"AI Models"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.SetDefault)

	huma.Register(api, huma.Operation{
		OperationID: "fetch-ai-provider-models",
		Method:      http.MethodPost,
		Path:        "/v1/ai/models/fetch",
		Summary:     "Fetch available models from provider",
		Description: "Queries an AI provider endpoint and returns the list of available models.",
		Tags:        []string{"AI Models"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.FetchAvailableModels)

	huma.Register(api, huma.Operation{
		OperationID: "test-ai-model",
		Method:      http.MethodPost,
		Path:        "/v1/ai/models/{uuid}/test",
		Summary:     "Test model connectivity",
		Description: "Tests connectivity to the AI provider and verifies the model is available.",
		Tags:        []string{"AI Models"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.TestConnectivity)

	huma.Register(api, huma.Operation{
		OperationID: "quick-prompt-ai-model",
		Method:      http.MethodPost,
		Path:        "/v1/ai/models/{uuid}/prompt",
		Summary:     "Quick prompt test",
		Description: "Sends a quick prompt to an LLM model and returns the response with timing info.",
		Tags:        []string{"AI Models"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.QuickPrompt)
}
