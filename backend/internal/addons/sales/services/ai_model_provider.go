package services

import (
	"context"

	"github.com/orkestra/backend/internal/addons/aimodels/providers"
)

// AIModelProvider is the consumer-defined interface for accessing LLM providers.
// Satisfied by aimodels.AIModelService — the sales module depends only on this minimal interface.
type AIModelProvider interface {
	GetDefaultLLMProvider(ctx context.Context) (providers.LLMProvider, error)
	GetLLMProvider(ctx context.Context, uuid string) (providers.LLMProvider, error)
}
