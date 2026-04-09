package models

// --- Internal Service-to-Service DTOs ---
// Used by the monolith's RemoteAIModelProvider to call the AI service.

// EmbedRequest is the request for POST /v1/internal/ai/embed
type EmbedRequest struct {
	Body struct {
		Text      string `json:"text" doc:"Text to embed" required:"true"`
		ModelUUID string `json:"modelUuid,omitempty" doc:"Model UUID (uses default embedding model if empty)"`
	}
}

// EmbedResponse is the response for POST /v1/internal/ai/embed
type EmbedResponse struct {
	Body struct {
		Embedding  []float64 `json:"embedding" doc:"Embedding vector"`
		Dimensions int       `json:"dimensions" doc:"Vector dimensions"`
		ModelName  string    `json:"modelName" doc:"Model used"`
	}
}

// EmbedBatchRequest is the request for POST /v1/internal/ai/embed-batch
type EmbedBatchRequest struct {
	Body struct {
		Texts     []string `json:"texts" doc:"Texts to embed" required:"true"`
		ModelUUID string   `json:"modelUuid,omitempty" doc:"Model UUID (uses default embedding model if empty)"`
	}
}

// EmbedBatchResponse is the response for POST /v1/internal/ai/embed-batch
type EmbedBatchResponse struct {
	Body struct {
		Embeddings [][]float64 `json:"embeddings" doc:"Embedding vectors"`
		Dimensions int         `json:"dimensions" doc:"Vector dimensions"`
		ModelName  string      `json:"modelName" doc:"Model used"`
	}
}

// CompleteRequest is the request for POST /v1/internal/ai/complete
type CompleteRequest struct {
	Body struct {
		Prompt       string  `json:"prompt" doc:"Prompt text" required:"true"`
		ModelUUID    string  `json:"modelUuid,omitempty" doc:"Model UUID (uses default LLM if empty)"`
		SystemPrompt string  `json:"systemPrompt,omitempty" doc:"System prompt"`
		Temperature  float64 `json:"temperature,omitempty" doc:"Generation temperature"`
		MaxTokens    int     `json:"maxTokens,omitempty" doc:"Max output tokens"`
	}
}

// CompleteResponse is the response for POST /v1/internal/ai/complete
type CompleteResponse struct {
	Body struct {
		Text         string `json:"text" doc:"Generated text"`
		ModelName    string `json:"modelName" doc:"Model used"`
		InputTokens  int    `json:"inputTokens,omitempty" doc:"Input token count"`
		OutputTokens int    `json:"outputTokens,omitempty" doc:"Output token count"`
	}
}

// EmbeddingInfoRequest is the request for GET /v1/internal/ai/embedding-info
type EmbeddingInfoRequest struct {
	ModelUUID string `query:"modelUuid" doc:"Model UUID (uses default embedding model if empty)"`
}

// EmbeddingInfoResponse is the response for GET /v1/internal/ai/embedding-info
type EmbeddingInfoResponse struct {
	Body struct {
		Dimensions int    `json:"dimensions" doc:"Embedding vector dimensions"`
		ModelName  string `json:"modelName" doc:"Model identifier"`
	}
}

// LLMInfoRequest is the request for GET /v1/internal/ai/llm-info
type LLMInfoRequest struct {
	ModelUUID string `query:"modelUuid" doc:"Model UUID (uses default LLM if empty)"`
}

// LLMInfoResponse is the response for GET /v1/internal/ai/llm-info
type LLMInfoResponse struct {
	Body struct {
		ModelName string `json:"modelName" doc:"Model identifier"`
	}
}
