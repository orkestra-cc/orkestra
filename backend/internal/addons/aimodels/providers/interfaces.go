package providers

import (
	"github.com/orkestra-cc/orkestra-sdk/iface"
)

// EmbeddingProvider, LLMProvider, CompletionOptions, StreamChunk,
// CompletionResult, and LLMProviderWithUsage all live in shared/iface so the
// iface contract layer doesn't import this addon package and consumer
// modules (sales, rag) don't import this addon either. These thin type
// aliases keep `providers.LLMProvider` etc. working unchanged for the
// addon's own implementations — the underlying types are identical.
type (
	EmbeddingProvider    = iface.EmbeddingProvider
	LLMProvider          = iface.LLMProvider
	CompletionOptions    = iface.CompletionOptions
	StreamChunk          = iface.StreamChunk
	CompletionResult     = iface.CompletionResult
	LLMProviderWithUsage = iface.LLMProviderWithUsage
)
