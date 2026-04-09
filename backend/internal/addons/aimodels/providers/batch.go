package providers

import (
	"context"
	"time"
)

// BatchRequest represents one request in a batch submission
type BatchRequest struct {
	CustomID string            // Unique ID to correlate results (e.g., agent name)
	Prompt   string
	Options  CompletionOptions
}

// BatchSubmission is the handle returned when a batch is created
type BatchSubmission struct {
	BatchID    string
	Provider   string
	RequestIDs []string
	CreatedAt  time.Time
}

// BatchResult is a single completed result within a batch
type BatchResult struct {
	CustomID     string
	Text         string
	InputTokens  int
	OutputTokens int
	Error        string // non-empty if this specific request failed
}

// BatchStatus represents the current state of a provider batch
type BatchStatus struct {
	BatchID string
	State   string        // "pending", "processing", "completed", "failed", "expired"
	Results []BatchResult // populated only when State == "completed"
	Error   string        // populated if the whole batch failed
}

// BatchLLMProvider extends LLMProvider with batch submission and polling.
// Cloud providers (Anthropic, OpenAI, Gemini) implement this. Ollama does not.
type BatchLLMProvider interface {
	LLMProvider
	SubmitBatch(ctx context.Context, requests []BatchRequest) (*BatchSubmission, error)
	PollBatch(ctx context.Context, batchID string) (*BatchStatus, error)
	CancelBatch(ctx context.Context, batchID string) error
}
