package handlers

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/orkestra/backend/internal/core/llm/models"
	"github.com/orkestra/backend/internal/core/llm/services"
)

// defaultUsageWindow is the report window when from/to are absent.
const defaultUsageWindow = 30 * 24 * time.Hour

// UsageHandler serves the admin usage report, the budget and the user's
// own usage (services.UsageService).
type UsageHandler struct {
	usage  *services.UsageService
	logger *slog.Logger
}

func NewUsageHandler(usage *services.UsageService, logger *slog.Logger) *UsageHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &UsageHandler{usage: usage, logger: logger}
}

type LLMUsageQuery struct {
	From    time.Time `query:"from" doc:"Start (RFC 3339, inclusive); default: 30 days before to"`
	To      time.Time `query:"to" doc:"End (RFC 3339, exclusive); default: now"`
	GroupBy string    `query:"groupBy" enum:"model,user,caller" default:"model"`
}

type LLMUsageReportResponse struct {
	Body models.LLMUsageReport
}

// GetUsage is the org's usage in [from, to) grouped by model, user or
// caller, with the current month's rollup and the budget.
func (h *UsageHandler) GetUsage(ctx context.Context, req *LLMUsageQuery) (*LLMUsageReportResponse, error) {
	to := req.To
	if to.IsZero() {
		to = time.Now().UTC()
	}
	from := req.From
	if from.IsZero() {
		from = to.Add(-defaultUsageWindow)
	}
	groupBy := req.GroupBy
	if groupBy == "" {
		groupBy = "model"
	}
	rep, err := h.usage.Admin(ctx, from, to, groupBy)
	if err != nil {
		return nil, failure(ctx, h.logger, "usage report", err)
	}
	return &LLMUsageReportResponse{Body: *rep}, nil
}

type LLMBudgetGetResponse struct {
	Status int
	Body   *models.LLMBudgetView
}

// GetBudget answers 200 with the budget, or 204 when the org has none (no
// budget means no limit).
func (h *UsageHandler) GetBudget(ctx context.Context, _ *struct{}) (*LLMBudgetGetResponse, error) {
	b, err := h.usage.GetBudget(ctx)
	if errors.Is(err, services.ErrNotFound) {
		return &LLMBudgetGetResponse{Status: http.StatusNoContent}, nil
	}
	if err != nil {
		return nil, failure(ctx, h.logger, "get budget", err)
	}
	return &LLMBudgetGetResponse{Status: http.StatusOK, Body: b}, nil
}

type LLMBudgetPutRequest struct {
	Body models.LLMBudgetPutBody
}

type LLMBudgetResponse struct {
	Body models.LLMBudgetView
}

// PutBudget creates or replaces the org's budget.
func (h *UsageHandler) PutBudget(ctx context.Context, req *LLMBudgetPutRequest) (*LLMBudgetResponse, error) {
	b, err := h.usage.PutBudget(ctx, req.Body)
	if err != nil {
		return nil, failure(ctx, h.logger, "put budget", err)
	}
	return &LLMBudgetResponse{Body: *b}, nil
}
