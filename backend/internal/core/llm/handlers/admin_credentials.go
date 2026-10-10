package handlers

import (
	"context"
	"log/slog"

	"github.com/orkestra/backend/internal/core/llm/models"
	"github.com/orkestra/backend/internal/core/llm/services"
)

// AdminHandler serves the Tier-1 admin endpoints over the catalog of the
// current organization. Secrets are write-only: every response carries the
// redacted LLMCredentialView.
type AdminHandler struct {
	catalog *services.CatalogService
	logger  *slog.Logger
}

func NewAdminHandler(catalog *services.CatalogService, logger *slog.Logger) *AdminHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &AdminHandler{catalog: catalog, logger: logger}
}

func (h *AdminHandler) fail(ctx context.Context, op string, err error) error {
	return failure(ctx, h.logger, op, err)
}

type LLMCredentialListResponse struct {
	Body struct {
		Items []models.LLMCredentialView `json:"items"`
	}
}

func (h *AdminHandler) ListCredentials(ctx context.Context, _ *struct{}) (*LLMCredentialListResponse, error) {
	list, err := h.catalog.ListCredentials(ctx)
	if err != nil {
		return nil, h.fail(ctx, "list credentials", err)
	}
	resp := &LLMCredentialListResponse{}
	resp.Body.Items = make([]models.LLMCredentialView, 0, len(list))
	for _, c := range list {
		resp.Body.Items = append(resp.Body.Items, c.View())
	}
	return resp, nil
}

type LLMCredentialResponse struct {
	Body models.LLMCredentialView
}

type LLMCredentialCreateRequest struct {
	Body models.LLMCredentialCreateBody
}

func (h *AdminHandler) CreateCredential(ctx context.Context, req *LLMCredentialCreateRequest) (*LLMCredentialResponse, error) {
	c, err := h.catalog.CreateCredential(ctx, models.CredentialInput{
		Name: req.Body.Name, Provider: req.Body.Provider, BaseURL: req.Body.BaseURL, Secret: req.Body.Secret,
	})
	if err != nil {
		return nil, h.fail(ctx, "create credential", err)
	}
	return &LLMCredentialResponse{Body: c.View()}, nil
}

type LLMCredentialPath struct {
	UUID string `path:"uuid" format:"uuid"`
}

func (h *AdminHandler) GetCredential(ctx context.Context, req *LLMCredentialPath) (*LLMCredentialResponse, error) {
	c, err := h.catalog.GetCredential(ctx, req.UUID)
	if err != nil {
		return nil, h.fail(ctx, "get credential", err)
	}
	return &LLMCredentialResponse{Body: c.View()}, nil
}

type LLMCredentialPatchRequest struct {
	UUID string `path:"uuid" format:"uuid"`
	Body models.LLMCredentialPatchBody
}

func (h *AdminHandler) PatchCredential(ctx context.Context, req *LLMCredentialPatchRequest) (*LLMCredentialResponse, error) {
	c, err := h.catalog.PatchCredential(ctx, req.UUID, req.Body.Name, req.Body.BaseURL, req.Body.Status)
	if err != nil {
		return nil, h.fail(ctx, "patch credential", err)
	}
	return &LLMCredentialResponse{Body: c.View()}, nil
}

type LLMCredentialRotateRequest struct {
	UUID string `path:"uuid" format:"uuid"`
	Body models.LLMCredentialRotateBody
}

func (h *AdminHandler) RotateCredential(ctx context.Context, req *LLMCredentialRotateRequest) (*LLMCredentialResponse, error) {
	c, err := h.catalog.RotateCredential(ctx, req.UUID, req.Body.Secret)
	if err != nil {
		return nil, h.fail(ctx, "rotate credential", err)
	}
	return &LLMCredentialResponse{Body: c.View()}, nil
}

func (h *AdminHandler) DeleteCredential(ctx context.Context, req *LLMCredentialPath) (*struct{}, error) {
	if err := h.catalog.DeleteCredential(ctx, req.UUID); err != nil {
		return nil, h.fail(ctx, "delete credential", err)
	}
	return nil, nil
}
