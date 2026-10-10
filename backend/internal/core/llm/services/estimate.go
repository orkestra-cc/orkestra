package services

import (
	"github.com/orkestra/backend/internal/core/llm/providers"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// EstimateInputTokens is the reservation's input estimate: ceil(UTF-8
// bytes / 3) over instructions, messages, tool schemas and the response
// schema — deliberately high, no tokenizer.
func EstimateInputTokens(req iface.ChatRequest) int { return providers.EstimateTokens(req) }

// EstimateEmbedTokens applies the same ceil(bytes / 3) rule to embedding
// inputs.
func EstimateEmbedTokens(inputs []string) int {
	n := 0
	for _, s := range inputs {
		n += len(s)
	}
	return (n + 2) / 3
}
