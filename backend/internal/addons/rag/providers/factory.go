package providers

import (
	"context"
	"fmt"
)

// ModelConfig contains the fields needed to create a provider.
// This mirrors the rag/models.ModelConfig but avoids circular imports.
type ModelConfig struct {
	Provider    string // "ollama" | "openai"
	ModelType   string // "embedding" | "llm"
	ModelName   string
	BaseURL     string
	APIKey      string
	Dimensions  int
	Temperature float64
	MaxTokens   int
}

// NewEmbeddingProvider creates an EmbeddingProvider from a model config
func NewEmbeddingProvider(cfg ModelConfig) (EmbeddingProvider, error) {
	switch cfg.Provider {
	case "ollama":
		return NewOllamaEmbeddingProvider(cfg.BaseURL, cfg.ModelName, cfg.Dimensions), nil
	case "openai":
		return NewOpenAIEmbeddingProvider(cfg.APIKey, cfg.ModelName, cfg.Dimensions, cfg.BaseURL), nil
	default:
		return nil, fmt.Errorf("unsupported embedding provider: %s", cfg.Provider)
	}
}

// NewLLMProvider creates an LLMProvider from a model config
func NewLLMProvider(cfg ModelConfig) (LLMProvider, error) {
	switch cfg.Provider {
	case "ollama":
		return NewOllamaLLMProvider(cfg.BaseURL, cfg.ModelName), nil
	case "openai":
		return NewOpenAILLMProvider(cfg.APIKey, cfg.ModelName, cfg.BaseURL), nil
	default:
		return nil, fmt.Errorf("unsupported LLM provider: %s", cfg.Provider)
	}
}

// RemoteModel represents a model available on a remote provider
type RemoteModel struct {
	ID      string
	OwnedBy string
}

// FetchAvailableModels queries the provider for available models
func FetchAvailableModels(ctx context.Context, provider, baseURL, apiKey string) ([]RemoteModel, error) {
	switch provider {
	case "ollama":
		return fetchOllamaModels(ctx, baseURL)
	case "openai":
		return fetchOpenAIModels(ctx, apiKey, baseURL)
	default:
		return nil, fmt.Errorf("unsupported provider: %s", provider)
	}
}

// TestConnectivity verifies a provider is reachable and the model exists
func TestConnectivity(ctx context.Context, cfg ModelConfig) error {
	switch cfg.Provider {
	case "ollama":
		return TestOllamaConnectivity(ctx, cfg.BaseURL, cfg.ModelName)
	case "openai":
		return TestOpenAIConnectivity(ctx, cfg.APIKey, cfg.ModelName, cfg.BaseURL)
	default:
		return fmt.Errorf("unsupported provider: %s", cfg.Provider)
	}
}
