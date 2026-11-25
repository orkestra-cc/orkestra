package handlers

import (
	"context"
	"log/slog"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

// SelfHandler serves the current user's view of the module through the
// same gateway addons use, so it shows exactly what a call may route to.
type SelfHandler struct {
	gw     iface.LLMGateway
	logger *slog.Logger
}

func NewSelfHandler(gw iface.LLMGateway, logger *slog.Logger) *SelfHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &SelfHandler{gw: gw, logger: logger}
}

type LLMMyModelsResponse struct {
	Body struct {
		Items []iface.LLMModelInfo `json:"items"`
	}
}

// MyModels lists the models the current user may use in the current org:
// a grant or access=everyone, never the fact of managing the model.
func (h *SelfHandler) MyModels(ctx context.Context, _ *struct{}) (*LLMMyModelsResponse, error) {
	items, err := h.gw.ListUsable(ctx)
	if err != nil {
		return nil, failure(ctx, h.logger, "list my models", err)
	}
	resp := &LLMMyModelsResponse{}
	resp.Body.Items = items
	return resp, nil
}
