package remote

import (
	"context"

	"github.com/orkestra/backend/internal/shared/iface"
)

// RemoteRAGQueryProvider implements iface.RAGQueryProvider by calling the AI
// service's internal RAG query endpoint over HTTP.
//
// Register in ServiceRegistry under module.ServiceRAGQuery.
type RemoteRAGQueryProvider struct {
	client *client
}

// NewRAGQueryProvider creates a RemoteRAGQueryProvider connected to the given AI service URL.
func NewRAGQueryProvider(aiServiceURL string) *RemoteRAGQueryProvider {
	return &RemoteRAGQueryProvider{
		client: newClient(aiServiceURL),
	}
}

func (p *RemoteRAGQueryProvider) Query(
	ctx context.Context,
	question string,
	topK int,
	minScore float64,
	isoStandard, llmOverrideUUID, requirementLevel, nodeType, retrievalMode string,
	documentUUIDs []string,
) (*iface.RAGQueryResponse, error) {
	reqBody := map[string]interface{}{
		"question":         question,
		"topK":             topK,
		"minScore":         minScore,
		"isoStandard":      isoStandard,
		"llmOverrideUuid":  llmOverrideUUID,
		"requirementLevel": requirementLevel,
		"nodeType":         nodeType,
		"retrievalMode":    retrievalMode,
		"documentUuids":    documentUUIDs,
	}

	var resp iface.RAGQueryResponse
	if err := p.client.post(ctx, "/v1/internal/rag/query", reqBody, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
