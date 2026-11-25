package handlers

import (
	"context"

	"github.com/orkestra/backend/internal/core/llm/models"
)

type LLMAccountsAdminListResponse struct {
	Body struct {
		Mode  string                           `json:"mode" enum:"disabled,loopback_local,hosted_paste"`
		Items []models.LLMUserAccountAdminView `json:"items"`
	}
}

// ListAccounts returns every linked ChatGPT registration of the current
// org with its owner, and the installation's mode (ruling 2.18): never a
// token, the issued client id or the OIDC subject.
func (h *SIWCHandler) ListAccounts(ctx context.Context, _ *struct{}) (*LLMAccountsAdminListResponse, error) {
	items, err := h.accounts.ListAdmin(ctx)
	if err != nil {
		return nil, failure(ctx, h.logger, "list linked accounts", err)
	}
	resp := &LLMAccountsAdminListResponse{}
	resp.Body.Mode = h.cfg().Mode
	resp.Body.Items = make([]models.LLMUserAccountAdminView, 0, len(items))
	for i := range items {
		resp.Body.Items = append(resp.Body.Items, items[i].AdminView())
	}
	return resp, nil
}

// ForceUnlink revokes (best-effort) and deletes any user's registration in
// the current org; audited with force=true.
func (h *SIWCHandler) ForceUnlink(ctx context.Context, req *LLMAccountByUUID) (*struct{}, error) {
	if err := h.accounts.Unlink(ctx, req.UUID, true); err != nil {
		return nil, failure(ctx, h.logger, "force unlink account", err)
	}
	return nil, nil
}
