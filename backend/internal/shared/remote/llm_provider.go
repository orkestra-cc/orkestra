package remote

import (
	"context"
	"fmt"
	"net/url"

	"github.com/orkestra/backend/internal/addons/aimodels/providers"
)

// RemoteLLMProvider implements providers.LLMProvider by calling
// the AI service's internal complete endpoint over HTTP.
type RemoteLLMProvider struct {
	client    *client
	modelUUID string // empty = default model
	model     string
}

// newRemoteLLMProvider fetches provider metadata and returns a ready provider.
func newRemoteLLMProvider(c *client, modelUUID string) (*RemoteLLMProvider, error) {
	p := &RemoteLLMProvider{client: c, modelUUID: modelUUID}

	path := "/v1/internal/ai/llm-info"
	if modelUUID != "" {
		path += "?modelUuid=" + url.QueryEscape(modelUUID)
	}

	var resp struct {
		ModelName string `json:"modelName"`
	}
	if err := c.get(context.Background(), path, &resp); err != nil {
		return nil, fmt.Errorf("remote: fetch llm info: %w", err)
	}

	p.model = resp.ModelName
	return p, nil
}

func (p *RemoteLLMProvider) Complete(ctx context.Context, prompt string, opts providers.CompletionOptions) (string, error) {
	reqBody := map[string]interface{}{
		"prompt":       prompt,
		"systemPrompt": opts.SystemPrompt,
		"temperature":  opts.Temperature,
		"maxTokens":    opts.MaxTokens,
	}
	if p.modelUUID != "" {
		reqBody["modelUuid"] = p.modelUUID
	}

	var resp struct {
		Text string `json:"text"`
	}
	if err := p.client.post(ctx, "/v1/internal/ai/complete", reqBody, &resp); err != nil {
		return "", err
	}
	return resp.Text, nil
}

func (p *RemoteLLMProvider) StreamComplete(ctx context.Context, prompt string, opts providers.CompletionOptions) (<-chan providers.StreamChunk, error) {
	// Streaming is handled directly by the frontend → AI service.
	// The monolith never needs to stream through this client.
	return nil, fmt.Errorf("remote: streaming not supported via service-to-service — use direct AI service connection")
}

func (p *RemoteLLMProvider) ModelName() string { return p.model }
