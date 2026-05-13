package handlers

import (
	"context"

	"github.com/danielgtaylor/huma/v2"

	"github.com/orkestra-cc/orkestra-addon-aimodels/models"
	"github.com/orkestra-cc/orkestra-addon-aimodels/providers"
	"github.com/orkestra-cc/orkestra-addon-aimodels/services"
)

// InternalHandler handles service-to-service AI model requests.
// These endpoints are called by the monolith's RemoteAIModelProvider.
type InternalHandler struct {
	modelService services.AIModelService
}

// NewInternalHandler creates a new InternalHandler
func NewInternalHandler(modelSvc services.AIModelService) *InternalHandler {
	return &InternalHandler{modelService: modelSvc}
}

func (h *InternalHandler) getEmbeddingProvider(ctx context.Context, modelUUID string) (providers.EmbeddingProvider, error) {
	if modelUUID != "" {
		return h.modelService.GetEmbeddingProvider(ctx, modelUUID)
	}
	return h.modelService.GetDefaultEmbeddingProvider(ctx)
}

func (h *InternalHandler) getLLMProvider(ctx context.Context, modelUUID string) (providers.LLMProvider, error) {
	if modelUUID != "" {
		return h.modelService.GetLLMProvider(ctx, modelUUID)
	}
	return h.modelService.GetDefaultLLMProvider(ctx)
}

// Embed generates an embedding for a single text
func (h *InternalHandler) Embed(ctx context.Context, req *models.EmbedRequest) (*models.EmbedResponse, error) {
	provider, err := h.getEmbeddingProvider(ctx, req.Body.ModelUUID)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to get embedding provider", err)
	}

	embedding, err := provider.Embed(ctx, req.Body.Text)
	if err != nil {
		return nil, huma.Error500InternalServerError("Embedding failed", err)
	}

	resp := &models.EmbedResponse{}
	resp.Body.Embedding = embedding
	resp.Body.Dimensions = provider.Dimensions()
	resp.Body.ModelName = provider.ModelName()
	return resp, nil
}

// EmbedBatch generates embeddings for multiple texts
func (h *InternalHandler) EmbedBatch(ctx context.Context, req *models.EmbedBatchRequest) (*models.EmbedBatchResponse, error) {
	provider, err := h.getEmbeddingProvider(ctx, req.Body.ModelUUID)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to get embedding provider", err)
	}

	embeddings, err := provider.EmbedBatch(ctx, req.Body.Texts)
	if err != nil {
		return nil, huma.Error500InternalServerError("Batch embedding failed", err)
	}

	resp := &models.EmbedBatchResponse{}
	resp.Body.Embeddings = embeddings
	resp.Body.Dimensions = provider.Dimensions()
	resp.Body.ModelName = provider.ModelName()
	return resp, nil
}

// Complete generates an LLM completion
func (h *InternalHandler) Complete(ctx context.Context, req *models.CompleteRequest) (*models.CompleteResponse, error) {
	provider, err := h.getLLMProvider(ctx, req.Body.ModelUUID)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to get LLM provider", err)
	}

	opts := providers.CompletionOptions{
		Temperature:  req.Body.Temperature,
		MaxTokens:    req.Body.MaxTokens,
		SystemPrompt: req.Body.SystemPrompt,
	}

	resp := &models.CompleteResponse{}
	resp.Body.ModelName = provider.ModelName()

	// Use usage-tracking variant if available
	if usageProvider, ok := provider.(providers.LLMProviderWithUsage); ok {
		result, err := usageProvider.CompleteWithUsage(ctx, req.Body.Prompt, opts)
		if err != nil {
			return nil, huma.Error500InternalServerError("LLM completion failed", err)
		}
		resp.Body.Text = result.Text
		resp.Body.InputTokens = result.InputTokens
		resp.Body.OutputTokens = result.OutputTokens
	} else {
		text, err := provider.Complete(ctx, req.Body.Prompt, opts)
		if err != nil {
			return nil, huma.Error500InternalServerError("LLM completion failed", err)
		}
		resp.Body.Text = text
	}

	return resp, nil
}

// GetEmbeddingInfo returns metadata about the embedding provider
func (h *InternalHandler) GetEmbeddingInfo(ctx context.Context, req *models.EmbeddingInfoRequest) (*models.EmbeddingInfoResponse, error) {
	provider, err := h.getEmbeddingProvider(ctx, req.ModelUUID)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to get embedding provider", err)
	}

	resp := &models.EmbeddingInfoResponse{}
	resp.Body.Dimensions = provider.Dimensions()
	resp.Body.ModelName = provider.ModelName()
	return resp, nil
}

// GetLLMInfo returns metadata about the LLM provider
func (h *InternalHandler) GetLLMInfo(ctx context.Context, req *models.LLMInfoRequest) (*models.LLMInfoResponse, error) {
	provider, err := h.getLLMProvider(ctx, req.ModelUUID)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to get LLM provider", err)
	}

	resp := &models.LLMInfoResponse{}
	resp.Body.ModelName = provider.ModelName()
	return resp, nil
}
