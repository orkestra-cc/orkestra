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
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// ErasureRequestHandler exposes the mediated right-to-erasure workflow: the
// data subject lodges a request; operators list, execute, or reject it.
type ErasureRequestHandler struct {
	svc *services.ErasureRequestService
}

// NewErasureRequestHandler binds the handler to its service.
func NewErasureRequestHandler(svc *services.ErasureRequestService) *ErasureRequestHandler {
	return &ErasureRequestHandler{svc: svc}
}

// --- client ---

type LodgeErasureRequestInput struct {
	Body struct {
		Reason string `json:"reason,omitempty" doc:"Optional reason for the request"`
	}
}
type ErasureRequestOutput struct {
	Body models.ErasureRequest
}

func (h *ErasureRequestHandler) Lodge(ctx context.Context, in *LodgeErasureRequestInput) (*ErasureRequestOutput, error) {
	userUUID, ok := ctxauth.GetUserUUID(ctx)
	if !ok || userUUID == "" {
		return nil, huma.Error401Unauthorized("authentication required")
	}
	tenantID, _ := ctxauth.GetTenantID(ctx)
	req, err := h.svc.Lodge(ctx, userUUID, tenantID, in.Body.Reason)
	if err != nil {
		return nil, huma.Error500InternalServerError("lodge erasure request", err)
	}
	return &ErasureRequestOutput{Body: *req}, nil
}

// --- admin ---

type ListErasureRequestsOutput struct {
	Body struct {
		Items []models.ErasureRequest `json:"items"`
	}
}

func (h *ErasureRequestHandler) ListPending(ctx context.Context, _ *struct{}) (*ListErasureRequestsOutput, error) {
	items, err := h.svc.ListPending(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("list erasure requests", err)
	}
	out := &ListErasureRequestsOutput{}
	out.Body.Items = items
	return out, nil
}

type ExecuteErasureRequestInput struct {
	ID   string `path:"id"`
	Body struct {
		Mode string `json:"mode,omitempty" enum:"hard_delete,anonymize" default:"hard_delete" doc:"Erasure mode"`
	}
}
type ExecuteErasureRequestOutput struct {
	Body struct {
		Purged map[string]iface.PurgeResult `json:"purged"`
	}
}

func (h *ErasureRequestHandler) Execute(ctx context.Context, in *ExecuteErasureRequestInput) (*ExecuteErasureRequestOutput, error) {
	actor, _ := ctxauth.GetUserUUID(ctx)
	mode := iface.EraseHardDelete
	if in.Body.Mode == "anonymize" {
		mode = iface.EraseAnonymize
	}
	res, err := h.svc.Execute(ctx, in.ID, mode, actor)
	if errors.Is(err, services.ErrLegalHoldActive) {
		return nil, huma.Error409Conflict("erasure blocked: the subject is under an active legal hold")
	}
	if errors.Is(err, repository.ErrErasureRequestNotFound) {
		return nil, huma.Error404NotFound("erasure request not found or already resolved")
	}
	if err != nil {
		return nil, huma.Error500InternalServerError("execute erasure request", err)
	}
	out := &ExecuteErasureRequestOutput{}
	out.Body.Purged = res.Purged
	return out, nil
}

type RejectErasureRequestInput struct {
	ID   string `path:"id"`
	Body struct {
		Note string `json:"note,omitempty" doc:"Why the request is rejected"`
	}
}
type RejectErasureRequestOutput struct{}

func (h *ErasureRequestHandler) Reject(ctx context.Context, in *RejectErasureRequestInput) (*RejectErasureRequestOutput, error) {
	actor, _ := ctxauth.GetUserUUID(ctx)
	err := h.svc.Reject(ctx, in.ID, actor, in.Body.Note)
	if errors.Is(err, repository.ErrErasureRequestNotFound) {
		return nil, huma.Error404NotFound("erasure request not found or already resolved")
	}
	if err != nil {
		return nil, huma.Error500InternalServerError("reject erasure request", err)
	}
	return &RejectErasureRequestOutput{}, nil
}

// RegisterErasureRequestClientRoutes mounts the subject-facing lodge endpoint.
func RegisterErasureRequestClientRoutes(api huma.API, h *ErasureRequestHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "compliance-me-erasure-request",
		Method:      http.MethodPost,
		Path:        "/v1/me/dsr/erasure-request",
		Summary:     "Lodge a right-to-erasure request for operator review",
		Description: "Records a pending erasure request. Unlike /v1/me/dsr/erase (immediate), this is the mediated workflow — an operator reviews and executes it.",
		Tags:        []string{"Compliance", "DSR"},
	}, h.Lodge)
}

// RegisterErasureRequestAdminReadRoutes mounts the operator list (gated by the
// audit read permission). RegisterErasureRequestAdminWriteRoutes mounts
// execute/reject, which the caller wraps with the dsr.manage permission +
// step-up.
func RegisterErasureRequestAdminReadRoutes(api huma.API, h *ErasureRequestHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "compliance-erasure-requests-list",
		Method:      http.MethodGet,
		Path:        "/v1/admin/compliance/erasure-requests",
		Summary:     "List pending right-to-erasure requests",
		Tags:        []string{"Compliance"},
	}, h.ListPending)
}

func RegisterErasureRequestAdminWriteRoutes(api huma.API, h *ErasureRequestHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "compliance-erasure-request-execute",
		Method:      http.MethodPost,
		Path:        "/v1/admin/compliance/erasure-requests/{id}/execute",
		Summary:     "Execute a right-to-erasure request (runs the DSR erase)",
		Tags:        []string{"Compliance"},
	}, h.Execute)
	huma.Register(api, huma.Operation{
		OperationID: "compliance-erasure-request-reject",
		Method:      http.MethodPost,
		Path:        "/v1/admin/compliance/erasure-requests/{id}/reject",
		Summary:     "Reject a right-to-erasure request",
		Tags:        []string{"Compliance"},
	}, h.Reject)
}
