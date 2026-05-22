package handlers

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra-cc/orkestra-addon-marketing/models"
	"github.com/orkestra-cc/orkestra-addon-marketing/repository"
	"github.com/orkestra-cc/orkestra-addon-marketing/services"
)

// ConflictReviewHandler exposes the Phase 3 review-queue surface.
// Reads fold into marketing.contact.read (consistent with how the
// Phase 2 activity timeline reads fold in). Writes (resolve/dismiss)
// gate on the separate marketing.conflict.resolve permission so
// admins can grant queue-management without granting full contact
// write access.
type ConflictReviewHandler struct {
	svc *services.ConflictReviewService
}

// NewConflictReviewHandler binds the handler to its service.
func NewConflictReviewHandler(svc *services.ConflictReviewService) *ConflictReviewHandler {
	return &ConflictReviewHandler{svc: svc}
}

// --- DTOs ---

// ConflictFieldView mirrors models.ConflictField for the wire.
type ConflictFieldView struct {
	Field         string                  `json:"field"`
	ExistingValue any                     `json:"existingValue,omitempty"`
	IncomingValue any                     `json:"incomingValue,omitempty"`
	Severity      models.ConflictSeverity `json:"severity"`
}

func toConflictFieldView(c models.ConflictField) ConflictFieldView {
	return ConflictFieldView{
		Field:         c.Field,
		ExistingValue: c.ExistingValue,
		IncomingValue: c.IncomingValue,
		Severity:      c.Severity,
	}
}

// ConflictResolutionView mirrors models.ConflictResolution.
type ConflictResolutionView struct {
	Action         models.ConflictAction `json:"action"`
	FieldOverrides map[string]any        `json:"fieldOverrides,omitempty"`
}

// ConflictReviewView is the response shape for every read endpoint.
type ConflictReviewView struct {
	UUID               string                      `json:"uuid"`
	ImportJobUUID      string                      `json:"importJobUuid"`
	TargetKind         models.ConflictTargetKind   `json:"targetKind"`
	ExistingUUID       string                      `json:"existingUuid"`
	ExistingSnapshot   map[string]any              `json:"existingSnapshot,omitempty"`
	IncomingPayload    map[string]any              `json:"incomingPayload"`
	IncomingActivities []map[string]any            `json:"incomingActivities,omitempty"`
	Conflicts          []ConflictFieldView         `json:"conflicts"`
	Status             models.ConflictReviewStatus `json:"status"`
	Resolution         *ConflictResolutionView     `json:"resolution,omitempty"`
	ResolvedAt         *time.Time                  `json:"resolvedAt,omitempty"`
	ResolvedBy         string                      `json:"resolvedBy,omitempty"`
	ResolvedNotes      string                      `json:"resolvedNotes,omitempty"`
	CreatedAt          time.Time                   `json:"createdAt"`
	UpdatedAt          time.Time                   `json:"updatedAt"`
}

func toConflictReviewView(c *models.ConflictReview) ConflictReviewView {
	conflicts := make([]ConflictFieldView, 0, len(c.Conflicts))
	for _, cf := range c.Conflicts {
		conflicts = append(conflicts, toConflictFieldView(cf))
	}
	view := ConflictReviewView{
		UUID:               c.UUID,
		ImportJobUUID:      c.ImportJobUUID,
		TargetKind:         c.TargetKind,
		ExistingUUID:       c.ExistingUUID,
		ExistingSnapshot:   c.ExistingSnapshot,
		IncomingPayload:    c.IncomingPayload,
		IncomingActivities: c.IncomingActivities,
		Conflicts:          conflicts,
		Status:             c.Status,
		ResolvedAt:         c.ResolvedAt,
		ResolvedBy:         c.ResolvedBy,
		ResolvedNotes:      c.ResolvedNotes,
		CreatedAt:          c.CreatedAt,
		UpdatedAt:          c.UpdatedAt,
	}
	if c.Resolution != nil {
		view.Resolution = &ConflictResolutionView{
			Action:         c.Resolution.Action,
			FieldOverrides: c.Resolution.FieldOverrides,
		}
	}
	return view
}

// ResolveConflictPayload is the request body for
// POST /conflict-reviews/{id}/resolve.
type ResolveConflictPayload struct {
	Action         models.ConflictAction `json:"action" doc:"One of: keep_existing, take_incoming, manual_merge, dismiss"`
	FieldOverrides map[string]any        `json:"fieldOverrides,omitempty" doc:"Required when action=manual_merge. Per-field overrides applied to the existing record."`
	Notes          string                `json:"notes,omitempty"`
}

// DismissConflictPayload is the request body for
// POST /conflict-reviews/{id}/dismiss.
type DismissConflictPayload struct {
	Notes string `json:"notes,omitempty"`
}

// --- Request/response wrappers ---

type ListReviewsInput struct {
	Status        string `query:"status" doc:"Filter by review status: pending | resolved | dismissed"`
	TargetKind    string `query:"targetKind" doc:"Filter by target kind: person | organization"`
	ImportJobUUID string `query:"importJobUuid" doc:"Filter to reviews from a single import job"`
	ExistingUUID  string `query:"existingUuid" doc:"Filter to reviews attached to a single existing record"`
	Limit         int64  `query:"limit" doc:"Page size, default 50, capped at 500"`
	Skip          int64  `query:"skip"`
}

type ListReviewsResponse struct {
	Body struct {
		Items []ConflictReviewView `json:"items"`
		Meta  ListMeta             `json:"meta"`
	}
}

type GetReviewInput struct {
	ID string `path:"id"`
}

type GetReviewResponse struct {
	Body ConflictReviewView
}

type ResolveReviewInput struct {
	ID   string `path:"id"`
	Body ResolveConflictPayload
}

type ResolveReviewResponse struct {
	Body ConflictReviewView
}

type DismissReviewInput struct {
	ID   string `path:"id"`
	Body DismissConflictPayload
}

type DismissReviewResponse struct {
	Body ConflictReviewView
}

// --- Handler methods ---

// List serves GET /v1/marketing/conflict-reviews.
func (h *ConflictReviewHandler) List(ctx context.Context, in *ListReviewsInput) (*ListReviewsResponse, error) {
	f := repository.ConflictReviewListFilter{
		ImportJobUUID: in.ImportJobUUID,
		ExistingUUID:  in.ExistingUUID,
		Limit:         in.Limit,
		Skip:          in.Skip,
	}
	if in.Status != "" {
		s := models.ConflictReviewStatus(in.Status)
		if !models.IsKnownConflictReviewStatus(s) {
			return nil, huma.Error400BadRequest("unknown status: " + in.Status)
		}
		f.Status = s
	}
	if in.TargetKind != "" {
		k := models.ConflictTargetKind(in.TargetKind)
		if !models.IsKnownConflictTargetKind(k) {
			return nil, huma.Error400BadRequest("unknown targetKind: " + in.TargetKind)
		}
		f.TargetKind = k
	}
	got, err := h.svc.List(ctx, f)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	items := make([]ConflictReviewView, 0, len(got))
	for i := range got {
		items = append(items, toConflictReviewView(&got[i]))
	}
	resp := &ListReviewsResponse{}
	resp.Body.Items = items
	resp.Body.Meta = ListMeta{Limit: in.Limit, Skip: in.Skip, Count: len(items)}
	return resp, nil
}

// Get serves GET /v1/marketing/conflict-reviews/{id}.
func (h *ConflictReviewHandler) Get(ctx context.Context, in *GetReviewInput) (*GetReviewResponse, error) {
	got, err := h.svc.Get(ctx, in.ID)
	if err != nil {
		if errors.Is(err, repository.ErrConflictReviewNotFound) {
			return nil, huma.Error404NotFound("conflict review not found")
		}
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &GetReviewResponse{Body: toConflictReviewView(got)}, nil
}

// Resolve serves POST /v1/marketing/conflict-reviews/{id}/resolve.
func (h *ConflictReviewHandler) Resolve(ctx context.Context, in *ResolveReviewInput) (*ResolveReviewResponse, error) {
	if !models.IsKnownConflictAction(in.Body.Action) {
		return nil, huma.Error400BadRequest("unknown action: " + string(in.Body.Action))
	}
	err := h.svc.Resolve(ctx, in.ID, services.ResolveInput{
		Action:         in.Body.Action,
		FieldOverrides: in.Body.FieldOverrides,
		Notes:          in.Body.Notes,
	})
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrConflictReviewNotFound):
			return nil, huma.Error404NotFound("conflict review not found")
		case errors.Is(err, repository.ErrConflictReviewNotPending):
			return nil, huma.Error409Conflict("conflict review already resolved")
		case errors.Is(err, services.ErrConflictReviewInvalid):
			return nil, huma.Error400BadRequest(err.Error())
		case errors.Is(err, models.ErrInvalidConflictReview):
			return nil, huma.Error400BadRequest(err.Error())
		default:
			return nil, huma.Error500InternalServerError(err.Error())
		}
	}
	got, err := h.svc.Get(ctx, in.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &ResolveReviewResponse{Body: toConflictReviewView(got)}, nil
}

// Dismiss serves POST /v1/marketing/conflict-reviews/{id}/dismiss.
func (h *ConflictReviewHandler) Dismiss(ctx context.Context, in *DismissReviewInput) (*DismissReviewResponse, error) {
	err := h.svc.Dismiss(ctx, in.ID, services.DismissInput{Notes: in.Body.Notes})
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrConflictReviewNotFound):
			return nil, huma.Error404NotFound("conflict review not found")
		case errors.Is(err, repository.ErrConflictReviewNotPending):
			return nil, huma.Error409Conflict("conflict review already resolved")
		default:
			return nil, huma.Error500InternalServerError(err.Error())
		}
	}
	got, err := h.svc.Get(ctx, in.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &DismissReviewResponse{Body: toConflictReviewView(got)}, nil
}

// --- Route registration ---

// RegisterConflictReviewReadRoutes — folds into marketing.contact.read.
func RegisterConflictReviewReadRoutes(api huma.API, h *ConflictReviewHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "marketing-list-conflict-reviews",
		Method:      http.MethodGet, Path: "/v1/marketing/conflict-reviews",
		Summary: "List conflict reviews", Tags: []string{"Marketing - Conflict Reviews"},
	}, h.List)
	huma.Register(api, huma.Operation{
		OperationID: "marketing-get-conflict-review",
		Method:      http.MethodGet, Path: "/v1/marketing/conflict-reviews/{id}",
		Summary: "Get a conflict review", Tags: []string{"Marketing - Conflict Reviews"},
	}, h.Get)
}

// RegisterConflictReviewWriteRoutes — gate with
// marketing.conflict.resolve.
func RegisterConflictReviewWriteRoutes(api huma.API, h *ConflictReviewHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "marketing-resolve-conflict-review",
		Method:      http.MethodPost, Path: "/v1/marketing/conflict-reviews/{id}/resolve",
		Summary:       "Resolve a pending conflict review",
		Tags:          []string{"Marketing - Conflict Reviews"},
		DefaultStatus: http.StatusOK,
	}, h.Resolve)
	huma.Register(api, huma.Operation{
		OperationID: "marketing-dismiss-conflict-review",
		Method:      http.MethodPost, Path: "/v1/marketing/conflict-reviews/{id}/dismiss",
		Summary:       "Dismiss a pending conflict review (incoming row discarded)",
		Tags:          []string{"Marketing - Conflict Reviews"},
		DefaultStatus: http.StatusOK,
	}, h.Dismiss)
}
