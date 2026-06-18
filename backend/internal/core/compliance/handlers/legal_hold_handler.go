package handlers

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra/backend/internal/core/compliance/models"
	"github.com/orkestra/backend/internal/core/compliance/repository"
	"github.com/orkestra/backend/internal/core/compliance/services"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
)

// LegalHoldHandler exposes the admin surface to place / release / list the
// litigation holds that block GDPR erasure.
type LegalHoldHandler struct {
	svc *services.LegalHoldService
}

// NewLegalHoldHandler binds the handler to its service.
func NewLegalHoldHandler(svc *services.LegalHoldService) *LegalHoldHandler {
	return &LegalHoldHandler{svc: svc}
}

// --- list (read) ---

type ListLegalHoldsInput struct {
	UserUUID string `query:"userUuid" doc:"Filter to one subject"`
}
type ListLegalHoldsOutput struct {
	Body struct {
		Items []models.LegalHold `json:"items"`
	}
}

func (h *LegalHoldHandler) List(ctx context.Context, in *ListLegalHoldsInput) (*ListLegalHoldsOutput, error) {
	items, err := h.svc.ListActive(ctx, in.UserUUID)
	if err != nil {
		return nil, huma.Error500InternalServerError("list legal holds", err)
	}
	out := &ListLegalHoldsOutput{}
	out.Body.Items = items
	return out, nil
}

// --- place / release (write) ---

type PlaceLegalHoldInput struct {
	Body struct {
		UserUUID string `json:"userUuid" doc:"Subject the hold applies to"`
		Reason   string `json:"reason" doc:"Why the hold is placed (litigation, investigation, ...)"`
		CaseRef  string `json:"caseRef,omitempty" doc:"Optional external case reference"`
	}
}
type LegalHoldOutput struct {
	Body models.LegalHold
}

func (h *LegalHoldHandler) Place(ctx context.Context, in *PlaceLegalHoldInput) (*LegalHoldOutput, error) {
	if in.Body.UserUUID == "" || in.Body.Reason == "" {
		return nil, huma.Error422UnprocessableEntity("userUuid and reason are required")
	}
	actor, _ := ctxauth.GetUserUUID(ctx)
	tenantID, _ := ctxauth.GetTenantID(ctx)
	hold, err := h.svc.Place(ctx, services.PlaceInput{
		UserUUID: in.Body.UserUUID,
		TenantID: tenantID,
		Reason:   in.Body.Reason,
		CaseRef:  in.Body.CaseRef,
		PlacedBy: actor,
	})
	if err != nil {
		return nil, huma.Error500InternalServerError("place legal hold", err)
	}
	return &LegalHoldOutput{Body: *hold}, nil
}

type ReleaseLegalHoldInput struct {
	ID   string `path:"id"`
	Body struct {
		ReleaseReason string `json:"releaseReason" doc:"Why the hold is released"`
	}
}
type ReleaseLegalHoldOutput struct{}

func (h *LegalHoldHandler) Release(ctx context.Context, in *ReleaseLegalHoldInput) (*ReleaseLegalHoldOutput, error) {
	actor, _ := ctxauth.GetUserUUID(ctx)
	err := h.svc.Release(ctx, in.ID, actor, in.Body.ReleaseReason)
	if errors.Is(err, repository.ErrLegalHoldNotFound) {
		return nil, huma.Error404NotFound("legal hold not found or already released")
	}
	if err != nil {
		return nil, huma.Error500InternalServerError("release legal hold", err)
	}
	return &ReleaseLegalHoldOutput{}, nil
}

// RegisterLegalHoldReadRoutes mounts the read endpoint (gated by the audit
// read permission). RegisterLegalHoldWriteRoutes mounts place/release, which
// the caller wraps with the legalhold.manage permission + step-up.
func RegisterLegalHoldReadRoutes(api huma.API, h *LegalHoldHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "compliance-legal-hold-list",
		Method:      http.MethodGet,
		Path:        "/v1/admin/compliance/legal-holds",
		Summary:     "List active legal holds",
		Tags:        []string{"Compliance"},
	}, h.List)
}

func RegisterLegalHoldWriteRoutes(api huma.API, h *LegalHoldHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "compliance-legal-hold-place",
		Method:      http.MethodPost,
		Path:        "/v1/admin/compliance/legal-holds",
		Summary:     "Place a legal hold on a data subject (blocks GDPR erasure)",
		Tags:        []string{"Compliance"},
	}, h.Place)
	huma.Register(api, huma.Operation{
		OperationID: "compliance-legal-hold-release",
		Method:      http.MethodDelete,
		Path:        "/v1/admin/compliance/legal-holds/{id}",
		Summary:     "Release a legal hold",
		Tags:        []string{"Compliance"},
	}, h.Release)
}
