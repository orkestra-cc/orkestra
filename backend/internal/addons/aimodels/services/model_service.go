package services

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.mongodb.org/mongo-driver/bson"

	"github.com/orkestra/backend/internal/addons/aimodels/models"
	"github.com/orkestra/backend/internal/addons/aimodels/providers"
	"github.com/orkestra/backend/internal/addons/aimodels/repository"
)

// AIModelService manages AI model configurations and provider creation
type AIModelService interface {
	// CRUD
	CreateModel(ctx context.Context, req *models.CreateModelRequest) (*models.ModelConfig, error)
	ListModels(ctx context.Context, modelType, provider, category string) ([]models.ModelConfig, error)
	GetModel(ctx context.Context, uuid string) (*models.ModelConfig, error)
	UpdateModel(ctx context.Context, req *models.UpdateModelRequest) (*models.ModelConfig, error)
	DeleteModel(ctx context.Context, uuid string) error

	// Defaults
	SetDefault(ctx context.Context, uuid string) error

	// Testing & discovery
	TestConnectivity(ctx context.Context, uuid string) error
	FetchAvailableModels(ctx context.Context, provider, baseURL, apiKey string) ([]providers.RemoteModel, error)
	QuickPrompt(ctx context.Context, uuid, prompt string) (string, int64, error)

	// Provider factory (consumed by RAG and other modules)
	GetDefaultEmbeddingProvider(ctx context.Context) (providers.EmbeddingProvider, error)
	GetDefaultLLMProvider(ctx context.Context) (providers.LLMProvider, error)
	GetEmbeddingProvider(ctx context.Context, uuid string) (providers.EmbeddingProvider, error)
	GetLLMProvider(ctx context.Context, uuid string) (providers.LLMProvider, error)

	// Seeding & migration
	SeedDefaults(ctx context.Context) error
	MigrateFromRAG(ctx context.Context, ragDB interface{ Collection(string, ...interface{}) interface{} }) error
}

// AIModelsConfig holds configuration for the AI models module
type AIModelsConfig struct {
	OllamaBaseURL string
	OpenAIAPIKey  string
	AnthropicKey  string
	GeminiKey     string
}

type modelService struct {
	repo   repository.ModelRepository
	cfg    AIModelsConfig
	logger *slog.Logger
}

// NewModelService creates a new AIModelService
func NewModelService(repo repository.ModelRepository, cfg AIModelsConfig, logger *slog.Logger) AIModelService {
	return &modelService{
		repo:   repo,
		cfg:    cfg,
		logger: logger.With(slog.String("module", "ai-models")),
	}
}

func (s *modelService) CreateModel(ctx context.Context, req *models.CreateModelRequest) (*models.ModelConfig, error) {
	cfg := &models.ModelConfig{
		Name:             req.Body.Name,
		Provider:         req.Body.Provider,
		ProviderCategory: models.ProviderCategoryFor(req.Body.Provider),
		ModelType:        req.Body.ModelType,
		ModelName:        req.Body.ModelName,
		BaseURL:          req.Body.BaseURL,
		APIKey:           req.Body.APIKey,
		Dimensions:       req.Body.Dimensions,
		Temperature:      req.Body.Temperature,
		MaxTokens:        req.Body.MaxTokens,
		IsActive:         true,
	}

	// Apply global fallbacks
	s.applyFallbacks(cfg)

	if err := s.repo.Create(ctx, cfg); err != nil {
		return nil, err
	}

	s.logger.Info("model created",
		slog.String("uuid", cfg.UUID),
		slog.String("name", cfg.Name),
		slog.String("provider", cfg.Provider),
		slog.String("type", cfg.ModelType),
	)
	return cfg, nil
}

func (s *modelService) ListModels(ctx context.Context, modelType, provider, category string) ([]models.ModelConfig, error) {
	return s.repo.List(ctx, modelType, provider, category)
}

func (s *modelService) GetModel(ctx context.Context, uuid string) (*models.ModelConfig, error) {
	return s.repo.GetByUUID(ctx, uuid)
}

func (s *modelService) UpdateModel(ctx context.Context, req *models.UpdateModelRequest) (*models.ModelConfig, error) {
	update := bson.M{}
	if req.Body.Name != nil {
		update["name"] = *req.Body.Name
	}
	if req.Body.BaseURL != nil {
		update["baseUrl"] = *req.Body.BaseURL
	}
	if req.Body.APIKey != nil {
		update["apiKey"] = *req.Body.APIKey
	}
	if req.Body.Dimensions != nil {
		update["dimensions"] = *req.Body.Dimensions
	}
	if req.Body.Temperature != nil {
		update["temperature"] = *req.Body.Temperature
	}
	if req.Body.MaxTokens != nil {
		update["maxTokens"] = *req.Body.MaxTokens
	}
	if req.Body.IsActive != nil {
		update["isActive"] = *req.Body.IsActive
	}

	if len(update) == 0 {
		return s.repo.GetByUUID(ctx, req.UUID)
	}

	if err := s.repo.Update(ctx, req.UUID, update); err != nil {
		return nil, err
	}
	return s.repo.GetByUUID(ctx, req.UUID)
}

func (s *modelService) DeleteModel(ctx context.Context, uuid string) error {
	return s.repo.Delete(ctx, uuid)
}

func (s *modelService) SetDefault(ctx context.Context, uuid string) error {
	model, err := s.repo.GetByUUID(ctx, uuid)
	if err != nil {
		return err
	}

	if err := s.repo.ClearDefault(ctx, model.ModelType); err != nil {
		return err
	}

	return s.repo.Update(ctx, uuid, bson.M{"isDefault": true})
}

func (s *modelService) TestConnectivity(ctx context.Context, uuid string) error {
	model, err := s.repo.GetByUUID(ctx, uuid)
	if err != nil {
		return err
	}

	cfg := s.toProviderConfig(model)
	testErr := providers.TestConnectivity(ctx, cfg)

	// Persist test result
	now := time.Now()
	status := "ok"
	if testErr != nil {
		status = "error"
	}
	_ = s.repo.Update(ctx, uuid, bson.M{
		"lastTestedAt":   now,
		"lastTestStatus": status,
	})

	return testErr
}

func (s *modelService) GetDefaultEmbeddingProvider(ctx context.Context) (providers.EmbeddingProvider, error) {
	model, err := s.repo.GetDefault(ctx, "embedding")
	if err != nil {
		return nil, err
	}
	return providers.NewEmbeddingProvider(s.toProviderConfig(model))
}

func (s *modelService) GetDefaultLLMProvider(ctx context.Context) (providers.LLMProvider, error) {
	model, err := s.repo.GetDefault(ctx, "llm")
	if err != nil {
		return nil, err
	}
	return providers.NewLLMProvider(s.toProviderConfig(model))
}

func (s *modelService) GetEmbeddingProvider(ctx context.Context, uuid string) (providers.EmbeddingProvider, error) {
	model, err := s.repo.GetByUUID(ctx, uuid)
	if err != nil {
		return nil, err
	}
	return providers.NewEmbeddingProvider(s.toProviderConfig(model))
}

func (s *modelService) GetLLMProvider(ctx context.Context, uuid string) (providers.LLMProvider, error) {
	model, err := s.repo.GetByUUID(ctx, uuid)
	if err != nil {
		return nil, err
	}
	return providers.NewLLMProvider(s.toProviderConfig(model))
}

func (s *modelService) FetchAvailableModels(ctx context.Context, provider, baseURL, apiKey string) ([]providers.RemoteModel, error) {
	// Apply global fallbacks for API keys
	switch provider {
	case "openai":
		if apiKey == "" {
			apiKey = s.cfg.OpenAIAPIKey
		}
	case "anthropic":
		if apiKey == "" {
			apiKey = s.cfg.AnthropicKey
		}
	case "gemini":
		if apiKey == "" {
			apiKey = s.cfg.GeminiKey
		}
	}

	// Validate required fields per provider
	switch provider {
	case "ollama":
		if baseURL == "" {
			baseURL = s.cfg.OllamaBaseURL
		}
		if baseURL == "" {
			return nil, fmt.Errorf("Ollama requires a base URL (e.g. http://host:11434)")
		}
	case "openai":
		if baseURL == "" && apiKey == "" {
			return nil, fmt.Errorf("OpenAI requires either a base URL (for local servers) or an API key (for OpenAI cloud)")
		}
	case "gemini":
		if apiKey == "" {
			return nil, fmt.Errorf("Gemini requires an API key — set it here or configure GEMINI_API_KEY on the server")
		}
	case "anthropic":
		// Anthropic returns a hardcoded list, no key needed for fetching
	}

	return providers.FetchAvailableModels(ctx, provider, baseURL, apiKey)
}

func (s *modelService) QuickPrompt(ctx context.Context, uuid, prompt string) (string, int64, error) {
	model, err := s.repo.GetByUUID(ctx, uuid)
	if err != nil {
		return "", 0, err
	}

	if model.ModelType != "llm" {
		return "", 0, fmt.Errorf("quick prompt is only available for LLM models")
	}

	llm, err := providers.NewLLMProvider(s.toProviderConfig(model))
	if err != nil {
		return "", 0, err
	}

	start := time.Now()
	response, err := llm.Complete(ctx, prompt, providers.CompletionOptions{
		Temperature: model.Temperature,
		MaxTokens:   model.MaxTokens,
	})
	elapsed := time.Since(start).Milliseconds()

	if err != nil {
		return "", elapsed, err
	}
	return response, elapsed, nil
}

func (s *modelService) toProviderConfig(model *models.ModelConfig) providers.ModelConfig {
	cfg := providers.ModelConfig{
		Provider:    model.Provider,
		ModelType:   model.ModelType,
		ModelName:   model.ModelName,
		BaseURL:     model.BaseURL,
		APIKey:      model.APIKey,
		Dimensions:  model.Dimensions,
		Temperature: model.Temperature,
		MaxTokens:   model.MaxTokens,
	}
	s.applyProviderFallbacks(&cfg)
	return cfg
}

// applyFallbacks applies global config fallbacks to a model being created
func (s *modelService) applyFallbacks(cfg *models.ModelConfig) {
	if cfg.Provider == "ollama" && cfg.BaseURL == "" {
		cfg.BaseURL = s.cfg.OllamaBaseURL
	}
	if cfg.Provider == "openai" && cfg.APIKey == "" {
		cfg.APIKey = s.cfg.OpenAIAPIKey
	}
	if cfg.Provider == "anthropic" && cfg.APIKey == "" {
		cfg.APIKey = s.cfg.AnthropicKey
	}
	if cfg.Provider == "gemini" && cfg.APIKey == "" {
		cfg.APIKey = s.cfg.GeminiKey
	}
}

// applyProviderFallbacks applies global config fallbacks to a provider config
func (s *modelService) applyProviderFallbacks(cfg *providers.ModelConfig) {
	if cfg.Provider == "ollama" && cfg.BaseURL == "" {
		cfg.BaseURL = s.cfg.OllamaBaseURL
	}
	if cfg.Provider == "openai" && cfg.APIKey == "" {
		cfg.APIKey = s.cfg.OpenAIAPIKey
	}
	if cfg.Provider == "anthropic" && cfg.APIKey == "" {
		cfg.APIKey = s.cfg.AnthropicKey
	}
	if cfg.Provider == "gemini" && cfg.APIKey == "" {
		cfg.APIKey = s.cfg.GeminiKey
	}
}

// SeedDefaults seeds default model configurations if none exist
func (s *modelService) SeedDefaults(ctx context.Context) error {
	count, err := s.repo.Count(ctx)
	if err != nil {
		return fmt.Errorf("count models: %w", err)
	}
	if count > 0 {
		return nil
	}

	s.logger.Info("seeding default AI model configurations")

	// Default embedding model (llama.cpp OpenAI-compatible)
	embeddingModel := &models.ModelConfig{
		Name:             "Qwen3 Embedding 8B",
		Provider:         "openai",
		ProviderCategory: "cloud",
		ModelType:        "embedding",
		ModelName:        "Qwen3-Embedding-8B-Q5_K_M.gguf",
		BaseURL:          "http://192.168.88.52:8082/v1",
		Dimensions:       4096,
		IsDefault:        true,
		IsActive:         true,
	}
	if err := s.repo.Create(ctx, embeddingModel); err != nil {
		return fmt.Errorf("seed embedding model: %w", err)
	}

	// Default LLM (llama.cpp OpenAI-compatible)
	llmModel := &models.ModelConfig{
		Name:             "Qwen3.5 9B",
		Provider:         "openai",
		ProviderCategory: "cloud",
		ModelType:        "llm",
		ModelName:        "qwen3.5-9b-q8_0.gguf",
		BaseURL:          "http://192.168.88.52:8081/v1",
		Temperature:      0.1,
		MaxTokens:        2048,
		IsDefault:        true,
		IsActive:         true,
	}
	if err := s.repo.Create(ctx, llmModel); err != nil {
		return fmt.Errorf("seed llm model: %w", err)
	}

	s.logger.Info("default AI models seeded", slog.Int("count", 2))
	return nil
}

// MigrateFromRAG migrates model configs from the old rag_models collection to ai_models.
// This is a no-op if ai_models already has data or rag_models doesn't exist/is empty.
func (s *modelService) MigrateFromRAG(ctx context.Context, _ interface{ Collection(string, ...interface{}) interface{} }) error {
	// Migration is handled at startup in main.go by reading rag_models directly
	// This method is intentionally a no-op — migration logic lives in the startup sequence
	return nil
}
