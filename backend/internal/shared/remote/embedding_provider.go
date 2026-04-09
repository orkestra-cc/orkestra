package remote

import (
	"context"
	"fmt"
	"net/url"
)

// RemoteEmbeddingProvider implements providers.EmbeddingProvider by calling
// the AI service's internal embed endpoints over HTTP.
type RemoteEmbeddingProvider struct {
	client    *client
	modelUUID string // empty = default model
	dims      int
	model     string
}

// newRemoteEmbeddingProvider fetches provider metadata and returns a ready provider.
func newRemoteEmbeddingProvider(c *client, modelUUID string) (*RemoteEmbeddingProvider, error) {
	p := &RemoteEmbeddingProvider{client: c, modelUUID: modelUUID}

	// Fetch dimensions and model name from the AI service
	path := "/v1/internal/ai/embedding-info"
	if modelUUID != "" {
		path += "?modelUuid=" + url.QueryEscape(modelUUID)
	}

	var resp struct {
		Dimensions int    `json:"dimensions"`
		ModelName  string `json:"modelName"`
	}
	if err := c.get(context.Background(), path, &resp); err != nil {
		return nil, fmt.Errorf("remote: fetch embedding info: %w", err)
	}

	p.dims = resp.Dimensions
	p.model = resp.ModelName
	return p, nil
}

func (p *RemoteEmbeddingProvider) Embed(ctx context.Context, text string) ([]float64, error) {
	reqBody := map[string]interface{}{
		"text": text,
	}
	if p.modelUUID != "" {
		reqBody["modelUuid"] = p.modelUUID
	}

	var resp struct {
		Embedding []float64 `json:"embedding"`
	}
	if err := p.client.post(ctx, "/v1/internal/ai/embed", reqBody, &resp); err != nil {
		return nil, err
	}
	return resp.Embedding, nil
}

func (p *RemoteEmbeddingProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float64, error) {
	reqBody := map[string]interface{}{
		"texts": texts,
	}
	if p.modelUUID != "" {
		reqBody["modelUuid"] = p.modelUUID
	}

	var resp struct {
		Embeddings [][]float64 `json:"embeddings"`
	}
	if err := p.client.post(ctx, "/v1/internal/ai/embed-batch", reqBody, &resp); err != nil {
		return nil, err
	}
	return resp.Embeddings, nil
}

func (p *RemoteEmbeddingProvider) Dimensions() int  { return p.dims }
func (p *RemoteEmbeddingProvider) ModelName() string { return p.model }
