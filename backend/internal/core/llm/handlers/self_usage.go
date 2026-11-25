package handlers

import (
	"context"

	"github.com/orkestra/backend/internal/core/llm/models"
)

type LLMMyUsageResponse struct {
	Body models.LLMSelfUsage
}

// MyUsage is the acting user's consumption in the current UTC month, split
// by who pays (org credential vs linked account).
func (h *UsageHandler) MyUsage(ctx context.Context, _ *struct{}) (*LLMMyUsageResponse, error) {
	u, err := h.usage.Self(ctx)
	if err != nil {
		return nil, failure(ctx, h.logger, "my usage", err)
	}
	return &LLMMyUsageResponse{Body: *u}, nil
}
