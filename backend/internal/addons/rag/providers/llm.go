package providers

import "context"

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
