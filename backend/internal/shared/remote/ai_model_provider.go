package remote

import (
	"context"

	"github.com/orkestra/backend/internal/addons/aimodels/providers"
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

func (p *RemoteAIModelProvider) GetDefaultEmbeddingProvider(ctx context.Context) (providers.EmbeddingProvider, error) {
	return newRemoteEmbeddingProvider(p.client, "")
}

func (p *RemoteAIModelProvider) GetDefaultLLMProvider(ctx context.Context) (providers.LLMProvider, error) {
	return newRemoteLLMProvider(p.client, "")
}

func (p *RemoteAIModelProvider) GetLLMProvider(ctx context.Context, uuid string) (providers.LLMProvider, error) {
	return newRemoteLLMProvider(p.client, uuid)
}

func (p *RemoteAIModelProvider) GetEmbeddingProvider(ctx context.Context, uuid string) (providers.EmbeddingProvider, error) {
	return newRemoteEmbeddingProvider(p.client, uuid)
}
