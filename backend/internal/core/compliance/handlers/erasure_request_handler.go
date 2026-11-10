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

type ListErasureRequestsInput struct {
	Status   string `query:"status" enum:"open,completed,rejected,closed_partial,all" default:"open" doc:"open (pending and incomplete, the default), completed, rejected, closed_partial or all"`
	UserUUID string `query:"userUuid" doc:"Only the requests of this data subject"`
	Limit    int    `query:"limit" minimum:"1" maximum:"200" doc:"Page size (1-200). Omitted: 50, except for status open, which lists every open request"`
	After    string `query:"after" doc:"Opaque cursor: the next of the previous page"`
}
type ListErasureRequestsOutput struct {
	Body struct {
		Items []models.ErasureRequest `json:"items"`
		Next  string                  `json:"next,omitempty" doc:"Cursor of the next page; absent on the last"`
	}
}

const defaultErasureListLimit = 50

// List returns requests by status, newest first, paged by cursor.
func (h *ErasureRequestHandler) List(ctx context.Context, in *ListErasureRequestsInput) (*ListErasureRequestsOutput, error) {
	f := repository.ErasureListFilter{UserUUID: in.UserUUID, Limit: in.Limit, After: in.After}
	switch in.Status {
	case "", "open":
		f.Statuses = []string{models.ErasureRequestPending, models.ErasureRequestIncomplete}
	case "all":
	default:
		f.Statuses = []string{in.Status}
	}
	if f.Limit == 0 && len(f.Statuses) != 2 {
		f.Limit = defaultErasureListLimit
	}
	items, next, err := h.svc.List(ctx, f)
	if errors.Is(err, repository.ErrErasureCursorInvalid) {
		return nil, huma.Error422UnprocessableEntity("invalid cursor")
	}
	if err != nil {
		return nil, huma.Error500InternalServerError("list erasure requests", err)
	}
	if items == nil {
		items = []models.ErasureRequest{}
	}
	out := &ListErasureRequestsOutput{}
	out.Body.Items = items
	out.Body.Next = next
	return out, nil
}

type ErasureReceiptInput struct {
	ID string `path:"id"`
}
type ErasureReceiptOutput struct {
	Body services.ErasureReceipt
}

// Receipt returns the receipt of a completed or closed_partial request.
func (h *ErasureRequestHandler) Receipt(ctx context.Context, in *ErasureReceiptInput) (*ErasureReceiptOutput, error) {
	rc, err := h.svc.Receipt(ctx, in.ID)
	switch {
	case errors.Is(err, repository.ErrErasureRequestNotFound):
		return nil, huma.Error404NotFound("erasure request not found")
	case errors.Is(err, services.ErrErasureReceiptUnavailable):
		return nil, errcode.Conflict(errcode.ComplianceErasureReceiptUnavailable, "Only a completed or closed erasure request has a receipt.")
	case err != nil:
		return nil, huma.Error500InternalServerError("erasure receipt", err)
	}
	return &ErasureReceiptOutput{Body: *rc}, nil
}

type ExecuteErasureRequestInput struct {
	ID   string `path:"id"`
	Body struct {
		Mode string `json:"mode,omitempty" enum:"hard_delete,anonymize" doc:"Erasure mode. Omit to run in the mode the request locked (hard_delete when none); a different one is refused"`
	}
}
type ExecuteErasureRequestOutput struct {
	Body struct {
		Status   string                           `json:"status" enum:"completed,incomplete"`
		Purged   map[string]iface.PurgeResult     `json:"purged"`
		Progress []models.ErasureProducerProgress `json:"progress"`
	}
}

func (h *ErasureRequestHandler) Execute(ctx context.Context, in *ExecuteErasureRequestInput) (*ExecuteErasureRequestOutput, error) {
	actor, ok := ctxauth.GetUserUUID(ctx)
	if !ok || actor == "" {
		return nil, huma.Error401Unauthorized("authentication required")
	}
	out0, err := h.svc.ExecuteInMode(ctx, in.ID, in.Body.Mode, actor)
	if gateErr := erasureGateError(err, "the subject"); gateErr != nil {
		return nil, gateErr
	}
	if apprErr := erasureApprovalError(err); apprErr != nil {
		return nil, apprErr
	}
	if errors.Is(err, repository.ErrErasureRequestNotFound) {
		return nil, huma.Error404NotFound("erasure request not found or already resolved")
	}
	if errors.Is(err, repository.ErrErasureRequestBusy) {
		return nil, errcode.Conflict(errcode.ComplianceErasureRequestBusy, "Another erasure run holds this request; try again in a few minutes.")
	}
	if err != nil {
		return nil, huma.Error500InternalServerError("execute erasure request", err)
	}
	out := &ExecuteErasureRequestOutput{}
	out.Body.Status = out0.Request.Status
	out.Body.Purged = out0.Run.Purged
	out.Body.Progress = out0.Run.Progress
	return out, nil
}

// OpenErasureRequestInput is the body an operator sends to open an erasure
// request for a subject.
type OpenErasureRequestInput struct {
	Body struct {
		UserUUID string `json:"userUuid" minLength:"1" doc:"UUID of the data subject to erase"`
		Reason   string `json:"reason" minLength:"1" maxLength:"500" doc:"Why the subject is erased; also the opener's approval note"`
		Mode     string `json:"mode" enum:"hard_delete,anonymize" doc:"Erasure mode; the request is locked to it"`
	}
}
type OpenErasureRequestOutput struct {
	Body models.ErasureRequest
}

// Open records a pending erasure request an operator opens for a subject.
func (h *ErasureRequestHandler) Open(ctx context.Context, in *OpenErasureRequestInput) (*OpenErasureRequestOutput, error) {
	actor, ok := ctxauth.GetUserUUID(ctx)
	if !ok || actor == "" {
		return nil, huma.Error401Unauthorized("authentication required")
	}
	req, err := h.svc.Open(ctx, services.OpenInput{
		UserUUID: in.Body.UserUUID, Reason: in.Body.Reason, RequestedBy: actor, Mode: parseHandlerMode(in.Body.Mode),
	})
	if gateErr := erasureGateError(err, "the subject"); gateErr != nil {
		return nil, gateErr
	}
	if apprErr := erasureApprovalError(err); apprErr != nil {
		return nil, apprErr
	}
	if err != nil {
		return nil, huma.Error500InternalServerError("open erasure request", err)
	}
	return &OpenErasureRequestOutput{Body: *req}, nil
}

type ApproveErasureRequestInput struct {
	ID   string `path:"id"`
	Body struct {
		Mode string `json:"mode" enum:"hard_delete,anonymize" doc:"Erasure mode; the first approval locks it"`
		Note string `json:"note" minLength:"1" maxLength:"500" doc:"Why the operator approves"`
	}
}

// Approve records the calling operator's approval of a pending request.
func (h *ErasureRequestHandler) Approve(ctx context.Context, in *ApproveErasureRequestInput) (*ErasureRequestOutput, error) {
	actor, ok := ctxauth.GetUserUUID(ctx)
	if !ok || actor == "" {
		return nil, huma.Error401Unauthorized("authentication required")
	}
	req, err := h.svc.Approve(ctx, in.ID, parseHandlerMode(in.Body.Mode), actor, in.Body.Note)
	if apprErr := erasureApprovalError(err); apprErr != nil {
		return nil, apprErr
	}
	if errors.Is(err, repository.ErrErasureRequestNotFound) {
		return nil, huma.Error404NotFound("erasure request not found or already resolved")
	}
	if err != nil {
		return nil, huma.Error500InternalServerError("approve erasure request", err)
	}
	return &ErasureRequestOutput{Body: *req}, nil
}

func parseHandlerMode(mode string) iface.EraseMode {
	if mode == "anonymize" {
		return iface.EraseAnonymize
	}
	return iface.EraseHardDelete
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
	if errors.Is(err, services.ErrErasureRequestStarted) {
		return nil, errcode.Conflict(errcode.ComplianceErasureRequestStarted, "This erasure has started; it can be retried, not rejected.")
	}
	if errors.Is(err, repository.ErrErasureRequestNotFound) {
		return nil, huma.Error404NotFound("erasure request not found or already resolved")
	}
	if err != nil {
		return nil, huma.Error500InternalServerError("reject erasure request", err)
	}
	return &RejectErasureRequestOutput{}, nil
}

type CloseErasureRequestInput struct {
	ID   string `path:"id"`
	Body struct {
		Note string `json:"note" minLength:"1" maxLength:"500" doc:"Why the remaining erasure is abandoned"`
	}
}
type CloseErasureRequestOutput struct {
	Body models.ErasureRequest
}

// Close closes an incomplete request whose remaining producers are no longer
// installed, recording them as abandoned.
func (h *ErasureRequestHandler) Close(ctx context.Context, in *CloseErasureRequestInput) (*CloseErasureRequestOutput, error) {
	actor, _ := ctxauth.GetUserUUID(ctx)
	req, err := h.svc.ClosePartial(ctx, in.ID, actor, in.Body.Note)
	switch {
	case errors.Is(err, services.ErrErasureCloseNoteRequired):
		return nil, errcode.UnprocessableEntity(errcode.ComplianceErasureCloseNoteRequired, "A note of 1 to 500 characters is required to close an erasure request.")
	case errors.Is(err, services.ErrErasureRequestNotClosable):
		return nil, errcode.Conflict(errcode.ComplianceErasureRequestNotClosable, "Only an incomplete request whose remaining producers are no longer installed can be closed; retry it instead.")
	case errors.Is(err, repository.ErrErasureRequestBusy):
		return nil, errcode.Conflict(errcode.ComplianceErasureRequestBusy, "Another erasure run holds this request; try again in a few minutes.")
	case errors.Is(err, repository.ErrErasureRequestNotFound):
		return nil, huma.Error404NotFound("erasure request not found or already resolved")
	case err != nil:
		return nil, huma.Error500InternalServerError("close erasure request", err)
	}
	return &CloseErasureRequestOutput{Body: *req}, nil
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
		Summary:     "List right-to-erasure requests",
		Description: "Newest first, filtered by status (default open: pending and incomplete, with closable) and subject, paged by the opaque cursor in next.",
		Tags:        []string{"Compliance"},
	}, h.List)
	huma.Register(api, huma.Operation{
		OperationID: "compliance-erasure-request-receipt",
		Method:      http.MethodGet,
		Path:        "/v1/admin/compliance/erasure-requests/{id}/receipt",
		Summary:     "Receipt of a completed or closed erasure request",
		Description: "Built from the request row: subject and operators by UUID, approvals, per-producer outcomes and totals, abandoned keys and the close note. 409 for a request that is pending, incomplete or rejected.",
		Tags:        []string{"Compliance"},
	}, h.Receipt)
}

func RegisterErasureRequestAdminWriteRoutes(api huma.API, h *ErasureRequestHandler) {
	huma.Register(api, huma.Operation{
		OperationID:   "compliance-erasure-request-open",
		Method:        http.MethodPost,
		Path:          "/v1/admin/compliance/erasure-requests",
		Summary:       "Open a right-to-erasure request for a subject",
		Description:   "Records a pending request on behalf of a subject, with the opener's approval and the mode locked. Refused when the subject is under a legal hold, already has an open request, or (hard delete) is inside a minimum retention. The reason is mandatory.",
		Tags:          []string{"Compliance"},
		DefaultStatus: http.StatusCreated,
	}, h.Open)
	huma.Register(api, huma.Operation{
		OperationID: "compliance-erasure-request-approve",
		Method:      http.MethodPost,
		Path:        "/v1/admin/compliance/erasure-requests/{id}/approve",
		Summary:     "Approve a pending right-to-erasure request",
		Description: "Adds the calling operator's approval; the first approval locks the mode. The data subject cannot approve and an operator approves once. Execute needs the configured number of distinct approvals, the executor counting as one.",
		Tags:        []string{"Compliance"},
	}, h.Approve)
	huma.Register(api, huma.Operation{
		OperationID: "compliance-erasure-request-execute",
		Method:      http.MethodPost,
		Path:        "/v1/admin/compliance/erasure-requests/{id}/execute",
		Summary:     "Execute a right-to-erasure request (runs the DSR erase)",
		Description: "Runs the erasure of a pending request in the body's mode, or in the mode its approvals locked when the body has none (hard_delete if neither); a different mode is refused. The executor counts as an approver. An incomplete request is retried with its stored mode and the body's mode is ignored.",
		Tags:        []string{"Compliance"},
	}, h.Execute)
	huma.Register(api, huma.Operation{
		OperationID: "compliance-erasure-request-reject",
		Method:      http.MethodPost,
		Path:        "/v1/admin/compliance/erasure-requests/{id}/reject",
		Summary:     "Reject a right-to-erasure request",
		Tags:        []string{"Compliance"},
	}, h.Reject)
	huma.Register(api, huma.Operation{
		OperationID: "compliance-erasure-request-close",
		Method:      http.MethodPost,
		Path:        "/v1/admin/compliance/erasure-requests/{id}/close",
		Summary:     "Close an incomplete erasure request whose producers are gone",
		Description: "Closes an incomplete request whose remaining producers are no longer installed, recording them as abandoned. Requires a note.",
		Tags:        []string{"Compliance"},
	}, h.Close)
}

// erasureGateError maps the refusals that precede an erasure (legal hold,
// minimum retention, an unavailable retention check) to their coded responses;
// nil when err is none of them. subject reads "the subject" or "the account".
func erasureGateError(err error, subject string) error {
	var gerr *services.RetentionGateError
	switch {
	case errors.Is(err, services.ErrLegalHoldActive):
		return errcode.Conflict(errcode.ComplianceLegalHoldActive,
			"Erasure blocked: "+subject+" is under an active legal hold.")
	case errors.As(err, &gerr):
		return errcode.Conflict(errcode.ComplianceRetentionMinimumNotMet,
			"Hard delete refused: the data must be retained until "+gerr.RetainUntil.UTC().Format("2006-01-02")+".").
			WithExtension("retainUntil", gerr.RetainUntil.UTC().Format(time.RFC3339)).
			WithExtension("reasons", append([]string{}, gerr.Reasons...))
	case errors.Is(err, services.ErrRetentionCheckUnavailable):
		return errcode.ServiceUnavailable(errcode.ComplianceRetentionCheckUnavailable,
			"The retention check could not be completed; the hard delete was not run. Try again later.")
	}
	return nil
}

// erasureApprovalError maps the approval workflow's refusals to their coded
// responses; nil when err is none of them.
func erasureApprovalError(err error) error {
	var miss *services.ErasureApprovalsMissingError
	var exists *services.ErasureRequestExistsError
	switch {
	case errors.Is(err, services.ErrReasonRequired):
		return errcode.UnprocessableEntity(errcode.ComplianceReasonRequired, "A reason or note of 1 to 500 characters is required.")
	case errors.As(err, &exists):
		return errcode.Conflict(errcode.ComplianceErasureRequestExists, "The subject already has an open erasure request.").
			WithExtension("requestId", exists.UUID)
	case errors.As(err, &miss):
		return errcode.Conflict(errcode.ComplianceErasureApprovalsMissing, "The request needs more distinct operator approvals before it can run.").
			WithExtension("have", miss.Have).WithExtension("need", miss.Need)
	case errors.Is(err, services.ErrErasureSelfApproval):
		return errcode.Conflict(errcode.ComplianceErasureSelfApproval, "An operator cannot open, approve or execute the erasure of their own account.")
	case errors.Is(err, services.ErrErasureAlreadyApproved):
		return errcode.Conflict(errcode.ComplianceErasureAlreadyApproved, "You already approved this erasure request.")
	case errors.Is(err, services.ErrErasureModeMismatch):
		return errcode.Conflict(errcode.ComplianceErasureModeMismatch, "The request is locked to a different erasure mode.")
	case errors.Is(err, repository.ErrErasureApprovalConflict):
		return errcode.Conflict(errcode.ComplianceErasureRequestBusy, "The request changed while you acted on it; reload and try again.")
	}
	return nil
}
