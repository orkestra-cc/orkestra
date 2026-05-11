package iface

import (
	"context"
	"time"
)

// AI model usage- and batch-tracking contract types live here so that the
// sales addon — the cross-module caller that depends on these types via
// type assertion — does not need to import the aimodels addon directly.
// addons/aimodels/providers keeps thin type aliases for its own
// implementations (anthropic, gemini, openai) so the addon's source is
// untouched by this move.

// CompletionResult holds the LLM response text along with token usage
// information. Returned by LLMProviderWithUsage.CompleteWithUsage.
type CompletionResult struct {
	Text         string
	InputTokens  int
	OutputTokens int
}

// LLMProviderWithUsage extends LLMProvider with token usage tracking.
// Providers that report usage (Anthropic, Gemini, OpenAI) implement this;
// sales asserts to it for per-agent token accounting.
type LLMProviderWithUsage interface {
	LLMProvider
	CompleteWithUsage(ctx context.Context, prompt string, opts CompletionOptions) (*CompletionResult, error)
}

// BatchRequest represents one request in a batch submission.
type BatchRequest struct {
	CustomID string // Unique ID to correlate results (e.g., agent name)
	Prompt   string
	Options  CompletionOptions
}

// BatchSubmission is the handle returned when a batch is created.
type BatchSubmission struct {
	BatchID    string
	Provider   string
	RequestIDs []string
	CreatedAt  time.Time
}

// BatchResult is a single completed result within a batch.
type BatchResult struct {
	CustomID     string
	Text         string
	InputTokens  int
	OutputTokens int
	Error        string // non-empty if this specific request failed
}

// BatchStatus represents the current state of a provider batch.
type BatchStatus struct {
	BatchID string
	State   string        // "pending", "processing", "completed", "failed", "expired"
	Results []BatchResult // populated only when State == "completed"
	Error   string        // populated if the whole batch failed
}

// BatchLLMProvider extends LLMProvider with batch submission and polling.
// Cloud providers (Anthropic, Gemini, OpenAI) implement this; Ollama does
// not. Sales asserts to it to opt into batched agent runs.
type BatchLLMProvider interface {
	LLMProvider
	SubmitBatch(ctx context.Context, requests []BatchRequest) (*BatchSubmission, error)
	PollBatch(ctx context.Context, batchID string) (*BatchStatus, error)
	CancelBatch(ctx context.Context, batchID string) error
}
