package providers

import (
	"context"
	"fmt"
)

// NewEmbeddingProvider creates an EmbeddingProvider from a model config
func NewEmbeddingProvider(cfg ModelConfig) (EmbeddingProvider, error) {
	switch cfg.Provider {
	case "ollama":
		return NewOllamaEmbeddingProvider(cfg.BaseURL, cfg.ModelName, cfg.Dimensions), nil
	case "openai":
		return NewOpenAIEmbeddingProvider(cfg.APIKey, cfg.ModelName, cfg.Dimensions, cfg.BaseURL), nil
	case "gemini":
		return NewGeminiEmbeddingProvider(cfg.APIKey, cfg.ModelName, cfg.Dimensions), nil
	case "anthropic":
		return nil, fmt.Errorf("Anthropic does not support embedding models")
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
	case "anthropic":
		return NewAnthropicLLMProvider(cfg.APIKey, cfg.ModelName), nil
	case "gemini":
		return NewGeminiLLMProvider(cfg.APIKey, cfg.ModelName), nil
	default:
		return nil, fmt.Errorf("unsupported LLM provider: %s", cfg.Provider)
	}
}

// FetchAvailableModels queries the provider for available models
func FetchAvailableModels(ctx context.Context, provider, baseURL, apiKey string) ([]RemoteModel, error) {
	switch provider {
	case "ollama":
		return fetchOllamaModels(ctx, baseURL)
	case "openai":
		return fetchOpenAIModels(ctx, apiKey, baseURL)
	case "anthropic":
		return fetchAnthropicModels(), nil
	case "gemini":
		return fetchGeminiModels(ctx, apiKey)
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
	case "anthropic":
		return TestAnthropicConnectivity(ctx, cfg.APIKey)
	case "gemini":
		return TestGeminiConnectivity(ctx, cfg.APIKey)
	default:
		return fmt.Errorf("unsupported provider: %s", cfg.Provider)
	}
}
