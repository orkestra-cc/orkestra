package remote

import (
	"context"
	"errors"

	"github.com/orkestra/backend/internal/shared/iface"
)

// RemoteAIModelProvider implements iface.AIModelProvider by delegating to the
// AI service over HTTP. It returns RemoteEmbeddingProvider and RemoteLLMProvider
// instances that call the AI service's internal API for actual inference.
//
// Register in ServiceRegistry under module.ServiceAIModelProvider.
type RemoteAIModelProvider struct {
	client *client
}

// NewAIModelProvider creates a RemoteAIModelProvider connected to the given AI service URL.
func NewAIModelProvider(aiServiceURL string) *RemoteAIModelProvider {
	return &RemoteAIModelProvider{
		client: newClient(aiServiceURL),
	}
}

func (p *RemoteAIModelProvider) GetDefaultEmbeddingProvider(ctx context.Context) (iface.EmbeddingProvider, error) {
	return newRemoteEmbeddingProvider(p.client, "")
}

func (p *RemoteAIModelProvider) GetDefaultLLMProvider(ctx context.Context) (iface.LLMProvider, error) {
	return newRemoteLLMProvider(p.client, "")
}

func (p *RemoteAIModelProvider) GetLLMProvider(ctx context.Context, uuid string) (iface.LLMProvider, error) {
	return newRemoteLLMProvider(p.client, uuid)
}

func (p *RemoteAIModelProvider) GetEmbeddingProvider(ctx context.Context, uuid string) (iface.EmbeddingProvider, error) {
	return newRemoteEmbeddingProvider(p.client, uuid)
}

// GetDefaultLLMConfig is not supported across the split-service boundary.
// The agents module hard-depends on aimodels and must run in the same
// process (both modules live in the AI service in split mode, or both in
// the monolith otherwise), so this accessor is never reached in practice.
func (p *RemoteAIModelProvider) GetDefaultLLMConfig(ctx context.Context) (iface.LLMConfig, error) {
	return iface.LLMConfig{}, errors.New("GetDefaultLLMConfig not available via remote AI service — agents and aimodels must run in the same service")
}
