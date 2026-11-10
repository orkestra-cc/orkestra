package handlers

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra/backend/internal/core/compliance/models"
	"github.com/orkestra/backend/internal/core/compliance/repository"
	"github.com/orkestra/backend/internal/core/compliance/services"
	"github.com/orkestra/backend/internal/shared/errcode"
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
	UserUUID        string `query:"userUuid" doc:"Filter to one subject"`
	IncludeReleased bool   `query:"includeReleased" doc:"Include released and expired holds"`
}
type ListLegalHoldsOutput struct {
	Body struct {
		Items []models.LegalHold `json:"items"`
	}
}

func (h *LegalHoldHandler) List(ctx context.Context, in *ListLegalHoldsInput) (*ListLegalHoldsOutput, error) {
	items, err := h.svc.List(ctx, in.UserUUID, in.IncludeReleased)
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
		// ReviewAt is when the hold is due for a review; it never releases the hold.
		ReviewAt *time.Time `json:"reviewAt,omitempty" doc:"Optional review date"`
		// ExpiresAt releases the hold on its own; must be in the future.
		ExpiresAt *time.Time `json:"expiresAt,omitempty" doc:"Optional expiry; the hold releases itself then"`
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
		UserUUID:  in.Body.UserUUID,
		TenantID:  tenantID,
		Reason:    in.Body.Reason,
		CaseRef:   in.Body.CaseRef,
		PlacedBy:  actor,
		ReviewAt:  in.Body.ReviewAt,
		ExpiresAt: in.Body.ExpiresAt,
	})
	if err != nil {
		return nil, mapLegalHoldError(err, "place legal hold")
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
	if err := h.svc.Release(ctx, in.ID, actor, in.Body.ReleaseReason); err != nil {
		return nil, mapLegalHoldError(err, "release legal hold")
	}
	return &ReleaseLegalHoldOutput{}, nil
}

func mapLegalHoldError(err error, op string) error {
	switch {
	case errors.Is(err, repository.ErrLegalHoldNotFound):
		return huma.Error404NotFound("legal hold not found or already released")
	case errors.Is(err, services.ErrReasonRequired):
		return errcode.UnprocessableEntity(errcode.ComplianceReasonRequired, "A reason or note of 1 to 500 characters is required (notes up to 1000).")
	case errors.Is(err, services.ErrLegalHoldExpiryPast):
		return errcode.UnprocessableEntity(errcode.ComplianceLegalHoldExpiryPast, "The expiry must be in the future.")
	case errors.Is(err, services.ErrLegalHoldUpdateInvalid):
		return huma.Error422UnprocessableEntity("set or clear a review date or an expiry, not both and not nothing")
	}
	return huma.Error500InternalServerError(op, err)
}

type UpdateLegalHoldInput struct {
	ID   string `path:"id"`
	Body struct {
		ReviewAt       *time.Time `json:"reviewAt,omitempty" doc:"New review date"`
		ExpiresAt      *time.Time `json:"expiresAt,omitempty" doc:"New expiry; must be in the future"`
		ClearReviewAt  bool       `json:"clearReviewAt,omitempty" doc:"Remove the review date"`
		ClearExpiresAt bool       `json:"clearExpiresAt,omitempty" doc:"Remove the expiry (the hold no longer expires)"`
	}
}

func (h *LegalHoldHandler) Update(ctx context.Context, in *UpdateLegalHoldInput) (*LegalHoldOutput, error) {
	actor, _ := ctxauth.GetUserUUID(ctx)
	hold, err := h.svc.Update(ctx, in.ID, actor, in.Body.ReviewAt, in.Body.ExpiresAt, in.Body.ClearReviewAt, in.Body.ClearExpiresAt)
	if err != nil {
		return nil, mapLegalHoldError(err, "update legal hold")
	}
	return &LegalHoldOutput{Body: *hold}, nil
}

type AddLegalHoldNoteInput struct {
	ID   string `path:"id"`
	Body struct {
		Text string `json:"text" doc:"Note text, 1 to 1000 characters"`
	}
}

func (h *LegalHoldHandler) AddNote(ctx context.Context, in *AddLegalHoldNoteInput) (*LegalHoldOutput, error) {
	actor, _ := ctxauth.GetUserUUID(ctx)
	hold, err := h.svc.AddNote(ctx, in.ID, actor, in.Body.Text)
	if err != nil {
		return nil, mapLegalHoldError(err, "add legal hold note")
	}
	return &LegalHoldOutput{Body: *hold}, nil
}

// RegisterLegalHoldReadRoutes mounts the read endpoint (gated by the audit
// read permission). RegisterLegalHoldWriteRoutes mounts place/release, which
// the caller wraps with the legalhold.manage permission + step-up.
func RegisterLegalHoldReadRoutes(api huma.API, h *LegalHoldHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "compliance-legal-hold-list",
		Method:      http.MethodGet,
		Path:        "/v1/admin/compliance/legal-holds",
		Summary:     "List legal holds",
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
	huma.Register(api, huma.Operation{
		OperationID: "compliance-legal-hold-update",
		Method:      http.MethodPatch,
		Path:        "/v1/admin/compliance/legal-holds/{id}",
		Summary:     "Change the review date or expiry of a legal hold",
		Tags:        []string{"Compliance"},
	}, h.Update)
	huma.Register(api, huma.Operation{
		OperationID: "compliance-legal-hold-add-note",
		Method:      http.MethodPost,
		Path:        "/v1/admin/compliance/legal-holds/{id}/notes",
		Summary:     "Add a note to a legal hold",
		Tags:        []string{"Compliance"},
	}, h.AddNote)
}
