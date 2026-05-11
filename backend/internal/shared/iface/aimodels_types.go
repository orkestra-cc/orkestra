package iface

import "context"

// AI model contract types live here (rather than in addons/aimodels/providers)
// so that the iface package — the cross-module contract layer — does not
// import any addon package. The aimodels addon keeps thin type aliases in
// addons/aimodels/providers/interfaces.go so its own implementations and
// existing cross-module callers (sales, rag) continue to use the
// `providers.LLMProvider` / `providers.CompletionOptions` form unchanged.

// EmbeddingProvider abstracts embedding generation across providers.
type EmbeddingProvider interface {
	// Embed generates an embedding vector for a single text.
	Embed(ctx context.Context, text string) ([]float64, error)
	// EmbedBatch generates embedding vectors for multiple texts.
	EmbedBatch(ctx context.Context, texts []string) ([][]float64, error)
	// Dimensions returns the embedding vector dimension.
	Dimensions() int
	// ModelName returns the model identifier.
	ModelName() string
}

// CompletionOptions configures LLM completion behavior.
type CompletionOptions struct {
	Temperature  float64
	MaxTokens    int
	SystemPrompt string
}

// StreamChunk represents a single chunk from a streaming LLM response.
type StreamChunk struct {
	Text  string
	Done  bool
	Error error
}

// LLMProvider abstracts LLM completion across providers.
type LLMProvider interface {
	// Complete generates a full response for the given prompt.
	Complete(ctx context.Context, prompt string, opts CompletionOptions) (string, error)
	// StreamComplete generates a streaming response.
	StreamComplete(ctx context.Context, prompt string, opts CompletionOptions) (<-chan StreamChunk, error)
	// ModelName returns the model identifier.
	ModelName() string
}
