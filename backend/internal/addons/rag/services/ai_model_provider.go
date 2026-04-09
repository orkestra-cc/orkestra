package services

import (
	"context"

	"github.com/orkestra/backend/internal/addons/aimodels/providers"
)

// AIModelProvider defines the minimal interface that RAG services need from the AI models module.
// This follows the Go idiom of consumer-defined interfaces to keep RAG decoupled from aimodels.
type AIModelProvider interface {
	GetDefaultEmbeddingProvider(ctx context.Context) (providers.EmbeddingProvider, error)
	GetDefaultLLMProvider(ctx context.Context) (providers.LLMProvider, error)
	GetLLMProvider(ctx context.Context, uuid string) (providers.LLMProvider, error)
}
