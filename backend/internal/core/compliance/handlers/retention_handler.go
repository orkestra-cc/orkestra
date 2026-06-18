package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra/backend/internal/core/compliance/services"
)

// RetentionHandler exposes the dry-run preview of retention auto-cleanup.
type RetentionHandler struct {
	svc *services.RetentionService
}

// NewRetentionHandler binds the handler to its service.
func NewRetentionHandler(svc *services.RetentionService) *RetentionHandler {
	return &RetentionHandler{svc: svc}
}

type RetentionPreviewOutput struct {
	Body struct {
		Cutoff    time.Time `json:"cutoff" doc:"Tombstones with deletedAt before this are eligible for cleanup"`
		Count     int       `json:"count"`
		UserUUIDs []string  `json:"userUuids"`
	}
}

// Preview handles GET /v1/admin/compliance/retention/preview.
func (h *RetentionHandler) Preview(ctx context.Context, _ *struct{}) (*RetentionPreviewOutput, error) {
	ids, cutoff, err := h.svc.Preview(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("retention preview", err)
	}
	out := &RetentionPreviewOutput{}
	out.Body.Cutoff = cutoff
	out.Body.Count = len(ids)
	out.Body.UserUUIDs = ids
	return out, nil
}

// RegisterRetentionRoutes mounts the read-only preview (gated by the audit
// read permission by the caller).
func RegisterRetentionRoutes(api huma.API, h *RetentionHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "compliance-retention-preview",
		Method:      http.MethodGet,
		Path:        "/v1/admin/compliance/retention/preview",
		Summary:     "Preview retention auto-cleanup candidates (dry run)",
		Description: "Lists anonymized user tombstones past the retention window that the auto-cleanup job would hard-delete. Mutates nothing; safe to retry.",
		Tags:        []string{"Compliance"},
	}, h.Preview)
}
