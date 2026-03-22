package models

// --- Model DTOs ---

type CreateModelRequest struct {
	Body struct {
		Name        string  `json:"name" doc:"Display name" required:"true"`
		Provider    string  `json:"provider" doc:"Provider" required:"true" enum:"ollama,openai,anthropic,gemini"`
		ModelType   string  `json:"modelType" doc:"Model type" required:"true" enum:"embedding,llm"`
		ModelName   string  `json:"modelName" doc:"Model identifier (e.g. nomic-embed-text, gpt-4o, claude-sonnet-4-20250514)" required:"true"`
		BaseURL     string  `json:"baseUrl,omitempty" doc:"Provider base URL (required for Ollama, optional for OpenAI-compatible)"`
		APIKey      string  `json:"apiKey,omitempty" doc:"API key (required for cloud providers)"`
		Dimensions  int     `json:"dimensions,omitempty" doc:"Embedding dimensions (embedding models only)"`
		Temperature float64 `json:"temperature,omitempty" doc:"Generation temperature (LLM models only)"`
		MaxTokens   int     `json:"maxTokens,omitempty" doc:"Max tokens (LLM models only)"`
	}
}

type CreateModelResponse struct {
	Body ModelConfig
}

type ListModelsRequest struct {
	Type     string `query:"type" doc:"Filter by model type: embedding or llm"`
	Provider string `query:"provider" doc:"Filter by provider: ollama, openai, anthropic, gemini"`
	Category string `query:"category" doc:"Filter by category: local or cloud"`
}

type ListModelsResponse struct {
	Body struct {
		Models []ModelConfig `json:"models" doc:"List of model configurations"`
	}
}

type GetModelRequest struct {
	UUID string `path:"uuid" doc:"Model UUID"`
}

type GetModelResponse struct {
	Body ModelConfig
}

type UpdateModelRequest struct {
	UUID string `path:"uuid" doc:"Model UUID"`
	Body struct {
		Name        *string  `json:"name,omitempty" doc:"Display name"`
		BaseURL     *string  `json:"baseUrl,omitempty" doc:"Provider base URL"`
		APIKey      *string  `json:"apiKey,omitempty" doc:"API key"`
		Dimensions  *int     `json:"dimensions,omitempty" doc:"Embedding dimensions"`
		Temperature *float64 `json:"temperature,omitempty" doc:"Generation temperature"`
		MaxTokens   *int     `json:"maxTokens,omitempty" doc:"Max tokens"`
		IsActive    *bool    `json:"isActive,omitempty" doc:"Active status"`
	}
}

type UpdateModelResponse struct {
	Body ModelConfig
}

type DeleteModelRequest struct {
	UUID string `path:"uuid" doc:"Model UUID"`
}

type DeleteModelResponse struct {
	Body struct {
		Message string `json:"message" doc:"Confirmation message"`
	}
}

type SetDefaultModelRequest struct {
	UUID string `path:"uuid" doc:"Model UUID"`
}

type SetDefaultModelResponse struct {
	Body struct {
		Message string `json:"message" doc:"Confirmation message"`
	}
}

type TestModelRequest struct {
	UUID string `path:"uuid" doc:"Model UUID"`
}

type TestModelResponse struct {
	Body struct {
		Status  string `json:"status" doc:"Connection status: ok or error"`
		Message string `json:"message" doc:"Status message"`
	}
}

type QuickPromptRequest struct {
	UUID string `path:"uuid" doc:"Model UUID"`
	Body struct {
		Prompt string `json:"prompt" doc:"Prompt to send to the model" required:"true"`
	}
}

type QuickPromptResponse struct {
	Body struct {
		Response string `json:"response" doc:"Model response"`
		TimeMs   int64  `json:"timeMs" doc:"Response time in milliseconds"`
	}
}

// AvailableModel represents a model available on a provider
type AvailableModel struct {
	ID           string `json:"id" doc:"Model identifier"`
	OwnedBy      string `json:"ownedBy,omitempty" doc:"Model owner"`
	Capabilities string `json:"capabilities,omitempty" doc:"Comma-separated capabilities (e.g. embedContent,generateContent)"`
}

type FetchModelsRequest struct {
	Body struct {
		Provider  string `json:"provider" doc:"Provider" required:"true" enum:"ollama,openai,anthropic,gemini"`
		BaseURL   string `json:"baseUrl,omitempty" doc:"Provider base URL (required for Ollama and OpenAI-compatible)"`
		APIKey    string `json:"apiKey,omitempty" doc:"API key (required for cloud providers)"`
		ModelType string `json:"modelType,omitempty" doc:"Filter by model type: embedding or llm" enum:"embedding,llm,"`
	}
}

type FetchModelsResponse struct {
	Body struct {
		Models []AvailableModel `json:"models" doc:"Available models on the provider"`
	}
}
