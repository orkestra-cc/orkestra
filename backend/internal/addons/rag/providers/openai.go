package providers

import (
	"context"
	"fmt"
	"net/http"
	"time"

	openai "github.com/sashabaranov/go-openai"
)

// openaiProvider implements both EmbeddingProvider and LLMProvider for OpenAI and
// OpenAI-compatible APIs (llama.cpp, vLLM, LocalAI, etc.)
type openaiProvider struct {
	client     *openai.Client
	modelName  string
	dimensions int
}

// newOpenAIClient creates an OpenAI client, optionally with a custom base URL
func newOpenAIClient(apiKey, baseURL string) *openai.Client {
	config := openai.DefaultConfig(apiKey)
	config.HTTPClient = &http.Client{Timeout: 5 * time.Minute}
	if baseURL != "" {
		config.BaseURL = baseURL
	}
	return openai.NewClientWithConfig(config)
}

// NewOpenAIEmbeddingProvider creates an EmbeddingProvider backed by OpenAI or compatible API
func NewOpenAIEmbeddingProvider(apiKey, modelName string, dimensions int, baseURL string) EmbeddingProvider {
	return &openaiProvider{
		client:     newOpenAIClient(apiKey, baseURL),
		modelName:  modelName,
		dimensions: dimensions,
	}
}

// NewOpenAILLMProvider creates an LLMProvider backed by OpenAI or compatible API
func NewOpenAILLMProvider(apiKey, modelName string, baseURL string) LLMProvider {
	return &openaiProvider{
		client:    newOpenAIClient(apiKey, baseURL),
		modelName: modelName,
	}
}

func (p *openaiProvider) ModelName() string { return p.modelName }
func (p *openaiProvider) Dimensions() int   { return p.dimensions }

// --- Embedding ---

func (p *openaiProvider) Embed(ctx context.Context, text string) ([]float64, error) {
	resp, err := p.client.CreateEmbeddings(ctx, openai.EmbeddingRequest{
		Model: openai.EmbeddingModel(p.modelName),
		Input: []string{text},
	})
	if err != nil {
		return nil, fmt.Errorf("openai embed: %w", err)
	}
	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("openai embed: empty response")
	}
	return float32sToFloat64s(resp.Data[0].Embedding), nil
}

func (p *openaiProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float64, error) {
	resp, err := p.client.CreateEmbeddings(ctx, openai.EmbeddingRequest{
		Model: openai.EmbeddingModel(p.modelName),
		Input: texts,
	})
	if err != nil {
		return nil, fmt.Errorf("openai embed batch: %w", err)
	}
	results := make([][]float64, len(resp.Data))
	for i, d := range resp.Data {
		results[i] = float32sToFloat64s(d.Embedding)
	}
	return results, nil
}

// --- LLM ---

func (p *openaiProvider) Complete(ctx context.Context, prompt string, opts CompletionOptions) (string, error) {
	messages := []openai.ChatCompletionMessage{}
	if opts.SystemPrompt != "" {
		messages = append(messages, openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleSystem,
			Content: opts.SystemPrompt,
		})
	}
	messages = append(messages, openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleUser,
		Content: prompt,
	})

	req := openai.ChatCompletionRequest{
		Model:    p.modelName,
		Messages: messages,
	}
	if opts.Temperature > 0 {
		req.Temperature = float32(opts.Temperature)
	}
	if opts.MaxTokens > 0 {
		req.MaxTokens = opts.MaxTokens
	}

	resp, err := p.client.CreateChatCompletion(ctx, req)
	if err != nil {
		return "", fmt.Errorf("openai complete: %w", err)
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("openai complete: empty response")
	}
	return resp.Choices[0].Message.Content, nil
}

func (p *openaiProvider) StreamComplete(ctx context.Context, prompt string, opts CompletionOptions) (<-chan StreamChunk, error) {
	messages := []openai.ChatCompletionMessage{}
	if opts.SystemPrompt != "" {
		messages = append(messages, openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleSystem,
			Content: opts.SystemPrompt,
		})
	}
	messages = append(messages, openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleUser,
		Content: prompt,
	})

	req := openai.ChatCompletionRequest{
		Model:    p.modelName,
		Messages: messages,
		Stream:   true,
	}
	if opts.Temperature > 0 {
		req.Temperature = float32(opts.Temperature)
	}
	if opts.MaxTokens > 0 {
		req.MaxTokens = opts.MaxTokens
	}

	stream, err := p.client.CreateChatCompletionStream(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("openai stream: %w", err)
	}

	ch := make(chan StreamChunk, 32)
	go func() {
		defer stream.Close()
		defer close(ch)

		for {
			resp, err := stream.Recv()
			if err != nil {
				if err.Error() == "EOF" {
					ch <- StreamChunk{Done: true}
					return
				}
				ch <- StreamChunk{Error: err}
				return
			}
			if len(resp.Choices) > 0 {
				ch <- StreamChunk{Text: resp.Choices[0].Delta.Content}
			}
		}
	}()

	return ch, nil
}

// fetchOpenAIModels lists models available on an OpenAI-compatible API
func fetchOpenAIModels(ctx context.Context, apiKey, baseURL string) ([]RemoteModel, error) {
	client := newOpenAIClient(apiKey, baseURL)
	resp, err := client.ListModels(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list models: %w", err)
	}

	models := make([]RemoteModel, len(resp.Models))
	for i, m := range resp.Models {
		models[i] = RemoteModel{ID: m.ID, OwnedBy: m.OwnedBy}
	}
	return models, nil
}

// TestOpenAIConnectivity checks if the API is reachable
func TestOpenAIConnectivity(ctx context.Context, apiKey, modelName, baseURL string) error {
	client := newOpenAIClient(apiKey, baseURL)
	_, err := client.ListModels(ctx)
	if err != nil {
		return fmt.Errorf("OpenAI-compatible API error: %w", err)
	}
	return nil
}

func float32sToFloat64s(f32s []float32) []float64 {
	f64s := make([]float64, len(f32s))
	for i, v := range f32s {
		f64s[i] = float64(v)
	}
	return f64s
}
