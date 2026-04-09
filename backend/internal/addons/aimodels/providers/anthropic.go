package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const anthropicAPIURL = "https://api.anthropic.com/v1"
const anthropicVersion = "2023-06-01"

// anthropicProvider implements LLMProvider for Anthropic Claude models.
// Anthropic does not offer embedding models.
type anthropicProvider struct {
	apiKey    string
	modelName string
	client    *http.Client
}

// NewAnthropicLLMProvider creates an LLMProvider backed by the Anthropic Messages API
func NewAnthropicLLMProvider(apiKey, modelName string) LLMProvider {
	return &anthropicProvider{
		apiKey:    apiKey,
		modelName: modelName,
		client:    &http.Client{Timeout: 5 * time.Minute},
	}
}

func (p *anthropicProvider) ModelName() string { return p.modelName }

// --- Anthropic Messages API types ---

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicRequest struct {
	Model     string             `json:"model"`
	Messages  []anthropicMessage `json:"messages"`
	System    string             `json:"system,omitempty"`
	MaxTokens int                `json:"max_tokens"`
	Stream    bool               `json:"stream,omitempty"`

	Temperature *float64 `json:"temperature,omitempty"`
}

type anthropicContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type anthropicResponse struct {
	Content []anthropicContentBlock `json:"content"`
	Usage   struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// --- SSE stream event types ---

type anthropicStreamEvent struct {
	Type  string          `json:"type"`
	Delta json.RawMessage `json:"delta,omitempty"`
}

type anthropicDelta struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

func (p *anthropicProvider) doRequest(ctx context.Context, reqBody anthropicRequest) (*http.Response, error) {
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("anthropic marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, anthropicAPIURL+"/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("anthropic request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", anthropicVersion)

	return p.client.Do(req)
}

func (p *anthropicProvider) Complete(ctx context.Context, prompt string, opts CompletionOptions) (string, error) {
	maxTokens := opts.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 4096
	}

	reqBody := anthropicRequest{
		Model:     p.modelName,
		Messages:  []anthropicMessage{{Role: "user", Content: prompt}},
		System:    opts.SystemPrompt,
		MaxTokens: maxTokens,
	}
	if opts.Temperature > 0 {
		reqBody.Temperature = &opts.Temperature
	}

	resp, err := p.doRequest(ctx, reqBody)
	if err != nil {
		return "", fmt.Errorf("anthropic call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("anthropic error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var result anthropicResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("anthropic decode: %w", err)
	}

	var sb strings.Builder
	for _, block := range result.Content {
		if block.Type == "text" {
			sb.WriteString(block.Text)
		}
	}
	return sb.String(), nil
}

func (p *anthropicProvider) CompleteWithUsage(ctx context.Context, prompt string, opts CompletionOptions) (*CompletionResult, error) {
	maxTokens := opts.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 4096
	}

	reqBody := anthropicRequest{
		Model:     p.modelName,
		Messages:  []anthropicMessage{{Role: "user", Content: prompt}},
		System:    opts.SystemPrompt,
		MaxTokens: maxTokens,
	}
	if opts.Temperature > 0 {
		reqBody.Temperature = &opts.Temperature
	}

	resp, err := p.doRequest(ctx, reqBody)
	if err != nil {
		return nil, fmt.Errorf("anthropic call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("anthropic error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var result anthropicResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("anthropic decode: %w", err)
	}

	var sb strings.Builder
	for _, block := range result.Content {
		if block.Type == "text" {
			sb.WriteString(block.Text)
		}
	}
	return &CompletionResult{
		Text:         sb.String(),
		InputTokens:  result.Usage.InputTokens,
		OutputTokens: result.Usage.OutputTokens,
	}, nil
}

func (p *anthropicProvider) StreamComplete(ctx context.Context, prompt string, opts CompletionOptions) (<-chan StreamChunk, error) {
	maxTokens := opts.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 4096
	}

	reqBody := anthropicRequest{
		Model:     p.modelName,
		Messages:  []anthropicMessage{{Role: "user", Content: prompt}},
		System:    opts.SystemPrompt,
		MaxTokens: maxTokens,
		Stream:    true,
	}
	if opts.Temperature > 0 {
		reqBody.Temperature = &opts.Temperature
	}

	resp, err := p.doRequest(ctx, reqBody)
	if err != nil {
		return nil, fmt.Errorf("anthropic stream call: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("anthropic stream error (status %d): %s", resp.StatusCode, string(respBody))
	}

	ch := make(chan StreamChunk, 32)
	go func() {
		defer resp.Body.Close()
		defer close(ch)

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()

			// SSE format: lines starting with "data: "
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")

			var event anthropicStreamEvent
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				continue
			}

			switch event.Type {
			case "content_block_delta":
				var delta anthropicDelta
				if err := json.Unmarshal(event.Delta, &delta); err == nil && delta.Text != "" {
					ch <- StreamChunk{Text: delta.Text}
				}
			case "message_stop":
				ch <- StreamChunk{Done: true}
				return
			case "error":
				ch <- StreamChunk{Error: fmt.Errorf("anthropic stream error: %s", string(event.Delta))}
				return
			}
		}
		if err := scanner.Err(); err != nil {
			ch <- StreamChunk{Error: err}
		}
	}()

	return ch, nil
}

// fetchAnthropicModels returns the known Anthropic model IDs.
// Anthropic does not expose a list-models API, so this is a hardcoded list.
func fetchAnthropicModels() []RemoteModel {
	return []RemoteModel{
		{ID: "claude-opus-4-20250514", OwnedBy: "anthropic"},
		{ID: "claude-sonnet-4-20250514", OwnedBy: "anthropic"},
		{ID: "claude-3-5-haiku-20241022", OwnedBy: "anthropic"},
	}
}

// TestAnthropicConnectivity verifies the API key is valid by sending a minimal request
func TestAnthropicConnectivity(ctx context.Context, apiKey string) error {
	provider := &anthropicProvider{
		apiKey:    apiKey,
		modelName: "claude-3-5-haiku-20241022",
		client:    &http.Client{Timeout: 15 * time.Second},
	}

	reqBody := anthropicRequest{
		Model:     provider.modelName,
		Messages:  []anthropicMessage{{Role: "user", Content: "Hi"}},
		MaxTokens: 1,
	}

	resp, err := provider.doRequest(ctx, reqBody)
	if err != nil {
		return fmt.Errorf("Anthropic API not reachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("Anthropic API key is invalid (status %d)", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Anthropic API error (status %d): %s", resp.StatusCode, string(body))
	}
	return nil
}
