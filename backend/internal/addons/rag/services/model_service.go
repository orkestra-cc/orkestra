package services

import (
	"context"
	"fmt"
	"log/slog"

	"go.mongodb.org/mongo-driver/bson"

	"github.com/orkestra-cc/orkestra-addon-aimodels/providers"
	"github.com/orkestra/backend/internal/addons/rag/models"
	"github.com/orkestra/backend/internal/addons/rag/repository"
	"github.com/orkestra/backend/internal/shared/config"
)

// ModelService manages AI model configurations
type ModelService interface {
	CreateModel(ctx context.Context, req *models.CreateModelRequest) (*models.ModelConfig, error)
	ListModels(ctx context.Context, modelType string) ([]models.ModelConfig, error)
	GetModel(ctx context.Context, uuid string) (*models.ModelConfig, error)
	UpdateModel(ctx context.Context, req *models.UpdateModelRequest) (*models.ModelConfig, error)
	DeleteModel(ctx context.Context, uuid string) error
	SetDefault(ctx context.Context, uuid string) error
	TestConnectivity(ctx context.Context, uuid string) error
	GetDefaultEmbeddingProvider(ctx context.Context) (providers.EmbeddingProvider, error)
	GetDefaultLLMProvider(ctx context.Context) (providers.LLMProvider, error)
	GetLLMProvider(ctx context.Context, uuid string) (providers.LLMProvider, error)
	FetchAvailableModels(ctx context.Context, provider, baseURL, apiKey string) ([]providers.RemoteModel, error)
	SeedDefaults(ctx context.Context) error
}

type modelService struct {
	repo   repository.ModelRepository
	cfg    config.RAGConfig
	logger *slog.Logger
}

// NewModelService creates a new ModelService
func NewModelService(repo repository.ModelRepository, cfg config.RAGConfig, logger *slog.Logger) ModelService {
	return &modelService{
		repo:   repo,
		cfg:    cfg,
		logger: logger.With(slog.String("module", "rag-models")),
	}
}

func (s *modelService) CreateModel(ctx context.Context, req *models.CreateModelRequest) (*models.ModelConfig, error) {
	cfg := &models.ModelConfig{
		Name:        req.Body.Name,
		Provider:    req.Body.Provider,
		ModelType:   req.Body.ModelType,
		ModelName:   req.Body.ModelName,
		BaseURL:     req.Body.BaseURL,
		APIKey:      req.Body.APIKey,
		Dimensions:  req.Body.Dimensions,
		Temperature: req.Body.Temperature,
		MaxTokens:   req.Body.MaxTokens,
		IsActive:    true,
	}

	// Use global config values as defaults for Ollama
	if cfg.Provider == "ollama" && cfg.BaseURL == "" {
		cfg.BaseURL = s.cfg.OllamaBaseURL
	}
	if cfg.Provider == "openai" && cfg.APIKey == "" {
		cfg.APIKey = s.cfg.OpenAIAPIKey
	}

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

func (s *modelService) ListModels(ctx context.Context, modelType string) ([]models.ModelConfig, error) {
	return s.repo.List(ctx, modelType)
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

	// Clear existing defaults for this model type
	if err := s.repo.ClearDefault(ctx, model.ModelType); err != nil {
		return err
	}

	// Set this one as default
	return s.repo.Update(ctx, uuid, bson.M{"isDefault": true})
}

func (s *modelService) TestConnectivity(ctx context.Context, uuid string) error {
	model, err := s.repo.GetByUUID(ctx, uuid)
	if err != nil {
		return err
	}

	cfg := s.toProviderConfig(model)
	return providers.TestConnectivity(ctx, cfg)
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

func (s *modelService) GetLLMProvider(ctx context.Context, uuid string) (providers.LLMProvider, error) {
	model, err := s.repo.GetByUUID(ctx, uuid)
	if err != nil {
		return nil, err
	}
	return providers.NewLLMProvider(s.toProviderConfig(model))
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
	// Apply global fallbacks
	if cfg.Provider == "ollama" && cfg.BaseURL == "" {
		cfg.BaseURL = s.cfg.OllamaBaseURL
	}
	if cfg.Provider == "openai" && cfg.APIKey == "" {
		cfg.APIKey = s.cfg.OpenAIAPIKey
	}
	return cfg
}

func (s *modelService) FetchAvailableModels(ctx context.Context, provider, baseURL, apiKey string) ([]providers.RemoteModel, error) {
	if provider == "openai" && apiKey == "" {
		apiKey = s.cfg.OpenAIAPIKey
	}
	return providers.FetchAvailableModels(ctx, provider, baseURL, apiKey)
}

// SeedDefaults seeds default model configurations if none exist
func (s *modelService) SeedDefaults(ctx context.Context) error {
	count, err := s.repo.Count(ctx)
	if err != nil {
		return fmt.Errorf("count models: %w", err)
	}
	if count > 0 {
		return nil // Already seeded
	}

	s.logger.Info("seeding default AI model configurations")

	// Default embedding model (llama.cpp OpenAI-compatible)
	embeddingModel := &models.ModelConfig{
		Name:       "Qwen3 Embedding 8B",
		Provider:   "openai",
		ModelType:  "embedding",
		ModelName:  "Qwen3-Embedding-8B-Q5_K_M.gguf",
		BaseURL:    "http://192.168.88.52:8082/v1",
		Dimensions: 4096,
		IsDefault:  true,
		IsActive:   true,
	}
	if err := s.repo.Create(ctx, embeddingModel); err != nil {
		return fmt.Errorf("seed embedding model: %w", err)
	}

	// Default LLM (llama.cpp OpenAI-compatible)
	llmModel := &models.ModelConfig{
		Name:        "Qwen3.5 9B",
		Provider:    "openai",
		ModelType:   "llm",
		ModelName:   "qwen3.5-9b-q8_0.gguf",
		BaseURL:     "http://192.168.88.52:8081/v1",
		Temperature: 0.1,
		MaxTokens:   2048,
		IsDefault:   true,
		IsActive:    true,
	}
	if err := s.repo.Create(ctx, llmModel); err != nil {
		return fmt.Errorf("seed llm model: %w", err)
	}

	s.logger.Info("default AI models seeded", slog.Int("count", 2))
	return nil
}
