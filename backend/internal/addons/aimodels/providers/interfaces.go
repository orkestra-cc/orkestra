package providers

import "context"

// EmbeddingProvider abstracts embedding generation across providers
type EmbeddingProvider interface {
	// Embed generates an embedding vector for a single text
	Embed(ctx context.Context, text string) ([]float64, error)
	// EmbedBatch generates embedding vectors for multiple texts
	EmbedBatch(ctx context.Context, texts []string) ([][]float64, error)
	// Dimensions returns the embedding vector dimension
	Dimensions() int
	// ModelName returns the model identifier
	ModelName() string
}

// CompletionOptions configures LLM completion behavior
type CompletionOptions struct {
	Temperature  float64
	MaxTokens    int
	SystemPrompt string
}

// StreamChunk represents a single chunk from a streaming LLM response
type StreamChunk struct {
	Text  string
	Done  bool
	Error error
}

// LLMProvider abstracts LLM completion across providers
type LLMProvider interface {
	// Complete generates a full response for the given prompt
	Complete(ctx context.Context, prompt string, opts CompletionOptions) (string, error)
	// StreamComplete generates a streaming response
	StreamComplete(ctx context.Context, prompt string, opts CompletionOptions) (<-chan StreamChunk, error)
	// ModelName returns the model identifier
	ModelName() string
}

// CompletionResult holds the LLM response text along with token usage information
type CompletionResult struct {
	Text         string
	InputTokens  int
	OutputTokens int
}

// LLMProviderWithUsage extends LLMProvider with token usage tracking.
// Providers that support usage reporting (e.g. Anthropic) implement this interface.
type LLMProviderWithUsage interface {
	LLMProvider
	CompleteWithUsage(ctx context.Context, prompt string, opts CompletionOptions) (*CompletionResult, error)
}
