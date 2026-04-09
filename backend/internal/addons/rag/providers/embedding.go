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
