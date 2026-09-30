package handlers

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra/backend/internal/core/compliance/models"
	"github.com/orkestra/backend/internal/core/compliance/repository"
	"github.com/orkestra/backend/internal/core/compliance/retentionclass"
	"github.com/orkestra/backend/internal/core/compliance/services"
	"github.com/orkestra/backend/internal/shared/errcode"
)

// PolicyHandler serves the Tier-1 operator surface of the policy engine
// (compliance spec §7). Reads need system.compliance.policy.read; writes
// need system.compliance.policy.manage and a fresh step-up (module.go).
type PolicyHandler struct {
	admin    *services.PolicyAdminService
	policies *services.PolicyService
}

func NewPolicyHandler(admin *services.PolicyAdminService, policies *services.PolicyService) *PolicyHandler {
	return &PolicyHandler{admin: admin, policies: policies}
}

// WriteResponse is the answer of every write: applied at once, or turned
// into a change request waiting for a second operator (202).
type WriteResponse struct {
	Applied       bool                        `json:"applied"`
	Policy        *models.Policy              `json:"policy,omitempty"`
	Assignment    *models.PolicyAssignment    `json:"assignment,omitempty"`
	ChangeRequest *models.PolicyChangeRequest `json:"changeRequest,omitempty"`
	Warnings      []services.PolicyIssue      `json:"warnings"`
}

type WriteOutput struct {
	Status int
	Body   WriteResponse
}

func writeOutput(res *services.WriteResult, appliedStatus int) *WriteOutput {
	out := &WriteOutput{Status: appliedStatus, Body: WriteResponse{
		Applied: res.Applied, Policy: res.Policy, Assignment: res.Assignment,
		ChangeRequest: res.ChangeRequest, Warnings: res.Warnings,
	}}
	if out.Body.Warnings == nil {
		out.Body.Warnings = []services.PolicyIssue{}
	}
	if !res.Applied {
		out.Status = http.StatusAccepted
	}
	return out
}

// --- reads ---

type ListPoliciesOutput struct {
	Body struct {
		Items []services.PolicyView `json:"items"`
	}
}

func (h *PolicyHandler) List(ctx context.Context, _ *struct{}) (*ListPoliciesOutput, error) {
	items, err := h.admin.ListPolicies(ctx)
	if err != nil {
		return nil, mapPolicyError(ctx, err)
	}
	out := &ListPoliciesOutput{}
	out.Body.Items = items
	return out, nil
}

type PolicyIDInput struct {
	ID string `path:"id" doc:"Policy UUID"`
}

type PolicyDetailOutput struct {
	Body services.PolicyDetail
}

func (h *PolicyHandler) Get(ctx context.Context, in *PolicyIDInput) (*PolicyDetailOutput, error) {
	d, err := h.admin.GetPolicy(ctx, in.ID)
	if err != nil {
		return nil, mapPolicyError(ctx, err)
	}
	return &PolicyDetailOutput{Body: *d}, nil
}

type ListVersionsOutput struct {
	Body struct {
		Items []models.PolicyVersion `json:"items"`
	}
}

func (h *PolicyHandler) Versions(ctx context.Context, in *PolicyIDInput) (*ListVersionsOutput, error) {
	items, err := h.admin.ListVersions(ctx, in.ID)
	if err != nil {
		return nil, mapPolicyError(ctx, err)
	}
	out := &ListVersionsOutput{}
	out.Body.Items = items
	return out, nil
}

type EffectiveInput struct {
	TenantID string `query:"tenantId" required:"true" doc:"Tenant UUID"`
}

type EffectiveOutput struct {
	Body services.EffectivePolicy
}

func (h *PolicyHandler) Effective(ctx context.Context, in *EffectiveInput) (*EffectiveOutput, error) {
	eff, ok := h.policies.Effective(in.TenantID)
	if !ok {
		return nil, mapPolicyError(ctx, services.ErrPolicyUnavailable)
	}
	return &EffectiveOutput{Body: eff}, nil
}

type ValidatePolicyInput struct {
	Body struct {
		PolicyID string             `json:"policyId,omitempty" doc:"UUID of the policy being edited; empty for a new tenant policy"`
		Policy   models.PolicyInput `json:"policy"`
	}
}

type ValidationOutput struct {
	Body services.ValidationResult
}

func validationOutput(r services.ValidationResult) *ValidationOutput {
	if r.Errors == nil {
		r.Errors = []services.PolicyIssue{}
	}
	if r.Warnings == nil {
		r.Warnings = []services.PolicyIssue{}
	}
	return &ValidationOutput{Body: r}
}

func (h *PolicyHandler) Validate(ctx context.Context, in *ValidatePolicyInput) (*ValidationOutput, error) {
	r, err := h.admin.ValidateDraft(ctx, in.Body.PolicyID, in.Body.Policy)
	if err != nil {
		return nil, mapPolicyError(ctx, err)
	}
	return validationOutput(r), nil
}

type RetentionClassesOutput struct {
	Body struct {
		Items []retentionclass.Class `json:"items"`
	}
}

func (h *PolicyHandler) RetentionClasses(_ context.Context, _ *struct{}) (*RetentionClassesOutput, error) {
	out := &RetentionClassesOutput{}
	out.Body.Items = retentionclass.All()
	return out, nil
}

// --- writes ---

type CreatePolicyInput struct {
	Body struct {
		Policy              models.PolicyInput `json:"policy"`
		Reason              string             `json:"reason" doc:"Why the policy is created (1-500 characters)"`
		AcknowledgeWarnings bool               `json:"acknowledgeWarnings,omitempty"`
	}
}

func (h *PolicyHandler) Create(ctx context.Context, in *CreatePolicyInput) (*WriteOutput, error) {
	res, err := h.admin.Create(ctx, services.ActorFromContext(ctx), in.Body.Policy, in.Body.Reason, in.Body.AcknowledgeWarnings)
	if err != nil {
		return nil, mapPolicyError(ctx, err)
	}
	return writeOutput(res, http.StatusCreated), nil
}

type UpdatePolicyInput struct {
	ID   string `path:"id"`
	Body struct {
		ExpectedVersion     int                `json:"expectedVersion" doc:"The version the editor started from"`
		Policy              models.PolicyInput `json:"policy"`
		Reason              string             `json:"reason"`
		AcknowledgeWarnings bool               `json:"acknowledgeWarnings,omitempty"`
	}
}

func (h *PolicyHandler) Update(ctx context.Context, in *UpdatePolicyInput) (*WriteOutput, error) {
	res, err := h.admin.Update(ctx, services.ActorFromContext(ctx), in.ID, in.Body.ExpectedVersion, in.Body.Policy, in.Body.Reason, in.Body.AcknowledgeWarnings)
	if err != nil {
		return nil, mapPolicyError(ctx, err)
	}
	return writeOutput(res, http.StatusOK), nil
}

type DeletePolicyInput struct {
	ID              string `path:"id"`
	ExpectedVersion int    `query:"expectedVersion" required:"true"`
	Body            struct {
		Reason string `json:"reason"`
	}
}

func (h *PolicyHandler) Delete(ctx context.Context, in *DeletePolicyInput) (*struct{}, error) {
	if err := h.admin.Delete(ctx, services.ActorFromContext(ctx), in.ID, in.ExpectedVersion, in.Body.Reason); err != nil {
		return nil, mapPolicyError(ctx, err)
	}
	return &struct{}{}, nil
}

// mapPolicyError turns a policy-engine error into the errcode envelope. An
// error it cannot name is a server fault (500), logged, never echoed.
func mapPolicyError(ctx context.Context, err error) error {
	var verr *services.ValidationError
	var werr *services.WarningsError
	switch {
	case errors.As(err, &verr) && verr.Result.HasError(services.IssueNameTaken),
		errors.Is(err, repository.ErrPolicyNameTaken):
		return errcode.Conflict(errcode.CompliancePolicyNameTaken, "Another compliance policy already has this name.")
	case errors.As(err, &verr):
		return errcode.UnprocessableEntity(errcode.CompliancePolicyInvalid,
			"The policy is not valid ("+strings.Join(verr.Result.ErrorCodes(), ", ")+"); the validate endpoint lists the fields.")
	case errors.As(err, &werr):
		return errcode.UnprocessableEntity(errcode.CompliancePolicyWarningsUnacknowledged,
			"The change has warnings to acknowledge: "+strings.Join(werr.Codes, ", ")+".")
	case errors.Is(err, services.ErrReasonRequired):
		return errcode.UnprocessableEntity(errcode.CompliancePolicyReasonRequired, "A reason of 1 to 500 characters is required.")
	case errors.Is(err, repository.ErrPolicyNotFound):
		return errcode.NotFound(errcode.CompliancePolicyNotFound, "The compliance policy does not exist.")
	case errors.Is(err, repository.ErrAssignmentNotFound):
		return errcode.NotFound(errcode.CompliancePolicyNotFound, "The tenant has no compliance policy assignment.")
	case errors.Is(err, repository.ErrChangeRequestNotFound):
		return errcode.NotFound(errcode.ComplianceChangeRequestNotFound, "The change request does not exist.")
	case errors.Is(err, services.ErrTenantNotFound):
		return errcode.NotFound(errcode.ComplianceTenantNotFound, "The tenant does not exist.")
	case errors.Is(err, repository.ErrPolicyVersionConflict), errors.Is(err, services.ErrChangeRequestSuperseded):
		return errcode.Conflict(errcode.CompliancePolicyVersionConflict,
			"The policy or the tenant's assignment changed in the meantime; reload and try again.")
	case errors.Is(err, services.ErrPlatformProtected):
		return errcode.Conflict(errcode.CompliancePolicyPlatformProtected, "The platform policy cannot be deleted or assigned to a tenant.")
	case errors.Is(err, services.ErrPolicyInUse):
		return errcode.Conflict(errcode.CompliancePolicyInUse, "The policy is assigned to at least one tenant; move those tenants first.")
	case errors.Is(err, repository.ErrChangeRequestNotPending):
		return errcode.Conflict(errcode.ComplianceChangeRequestNotPending, "The change request has already been decided.")
	case errors.Is(err, services.ErrSelfApproval):
		return errcode.Conflict(errcode.ComplianceChangeRequestSelfApproval,
			"A change request must be decided by an operator other than its author.")
	case errors.Is(err, services.ErrPolicyUnavailable):
		return errcode.ServiceUnavailable(errcode.CompliancePolicyUnavailable,
			"The compliance policies are not loaded yet; retry in a few seconds.")
	default:
		slog.ErrorContext(ctx, "compliance: policy engine request failed", slog.String("error", err.Error()))
		return errcode.Internal(errcode.CompliancePolicyPersistenceFailed, "The compliance policy request could not be completed.")
	}
}

// RegisterPolicyReadRoutes mounts the reads (and the two validate POSTs,
// which change nothing) behind system.compliance.policy.read.
func RegisterPolicyReadRoutes(api huma.API, h *PolicyHandler) {
	tags := []string{"Compliance"}
	huma.Register(api, huma.Operation{OperationID: "compliance-policy-list", Method: http.MethodGet,
		Path: "/v1/admin/compliance/policies", Summary: "List compliance policies", Tags: tags}, h.List)
	huma.Register(api, huma.Operation{OperationID: "compliance-policy-effective", Method: http.MethodGet,
		Path: "/v1/admin/compliance/policies/effective", Summary: "Effective compliance policy of a tenant", Tags: tags}, h.Effective)
	huma.Register(api, huma.Operation{OperationID: "compliance-policy-validate", Method: http.MethodPost,
		Path: "/v1/admin/compliance/policies/validate", Summary: "Validate a policy draft (errors and warnings)", Tags: tags}, h.Validate)
	huma.Register(api, huma.Operation{OperationID: "compliance-policy-get", Method: http.MethodGet,
		Path: "/v1/admin/compliance/policies/{id}", Summary: "Get a compliance policy and its tenants", Tags: tags}, h.Get)
	huma.Register(api, huma.Operation{OperationID: "compliance-policy-versions", Method: http.MethodGet,
		Path: "/v1/admin/compliance/policies/{id}/versions", Summary: "List the versions of a policy", Tags: tags}, h.Versions)
	huma.Register(api, huma.Operation{OperationID: "compliance-retention-classes", Method: http.MethodGet,
		Path: "/v1/admin/compliance/retention-classes", Summary: "Retention class catalog", Tags: tags}, h.RetentionClasses)
	huma.Register(api, huma.Operation{OperationID: "compliance-policy-assignment-list", Method: http.MethodGet,
		Path: "/v1/admin/compliance/policy-assignments", Summary: "List tenant policy assignments", Tags: tags}, h.ListAssignments)
	huma.Register(api, huma.Operation{OperationID: "compliance-policy-assignment-validate", Method: http.MethodPost,
		Path: "/v1/admin/compliance/policy-assignments/{tenantId}/validate", Summary: "Warnings of assigning a policy to a tenant (or removing it)", Tags: tags}, h.ValidateAssignment)
	huma.Register(api, huma.Operation{OperationID: "compliance-change-request-list", Method: http.MethodGet,
		Path: "/v1/admin/compliance/change-requests", Summary: "List policy change requests", Tags: tags}, h.ListChangeRequests)
	huma.Register(api, huma.Operation{OperationID: "compliance-change-request-get", Method: http.MethodGet,
		Path: "/v1/admin/compliance/change-requests/{id}", Summary: "Get a policy change request", Tags: tags}, h.GetChangeRequest)
}

// acceptedResponses documents, on a write that can wait for four eyes, the
// 202 it answers with the pending change request. Huma adds the applied
// status by itself but drops its default error response once an operation
// declares more than one response, so that one is declared here as well.
// A fresh map per operation: huma fills it in place.
func acceptedResponses() map[string]*huma.Response {
	return map[string]*huma.Response{
		"202": {
			Description: "Accepted: the change has warnings and four eyes is on, so it was stored as a pending " +
				"change request (changeRequest) for another operator to approve; nothing was applied.",
			Content: map[string]*huma.MediaType{"application/json": {Schema: &huma.Schema{Ref: "#/components/schemas/WriteResponse"}}},
		},
		"default": {
			Description: "Error",
			Content:     map[string]*huma.MediaType{"application/problem+json": {Schema: &huma.Schema{Ref: "#/components/schemas/ErrorModel"}}},
		},
	}
}

// RegisterPolicyWriteRoutes mounts the writes; module.go wraps them with
// system.compliance.policy.manage and a fresh step-up.
func RegisterPolicyWriteRoutes(api huma.API, h *PolicyHandler) {
	tags := []string{"Compliance"}
	huma.Register(api, huma.Operation{OperationID: "compliance-policy-create", Method: http.MethodPost,
		Path: "/v1/admin/compliance/policies", Summary: "Create a tenant compliance policy", Tags: tags,
		DefaultStatus: http.StatusCreated, Responses: acceptedResponses()}, h.Create)
	huma.Register(api, huma.Operation{OperationID: "compliance-policy-update", Method: http.MethodPut,
		Path: "/v1/admin/compliance/policies/{id}", Summary: "Change a compliance policy", Tags: tags,
		Responses: acceptedResponses()}, h.Update)
	huma.Register(api, huma.Operation{OperationID: "compliance-policy-delete", Method: http.MethodDelete,
		Path: "/v1/admin/compliance/policies/{id}", Summary: "Delete an unassigned tenant policy", Tags: tags,
		DefaultStatus: http.StatusNoContent}, h.Delete)
	huma.Register(api, huma.Operation{OperationID: "compliance-policy-assign", Method: http.MethodPut,
		Path: "/v1/admin/compliance/policy-assignments/{tenantId}", Summary: "Assign a policy to a tenant", Tags: tags,
		Responses: acceptedResponses()}, h.Assign)
	huma.Register(api, huma.Operation{OperationID: "compliance-policy-unassign", Method: http.MethodDelete,
		Path: "/v1/admin/compliance/policy-assignments/{tenantId}", Summary: "Move a tenant back to the platform policy", Tags: tags,
		Responses: acceptedResponses()}, h.Unassign)
	huma.Register(api, huma.Operation{OperationID: "compliance-change-request-approve", Method: http.MethodPost,
		Path: "/v1/admin/compliance/change-requests/{id}/approve", Summary: "Approve a policy change request", Tags: tags}, h.Approve)
	huma.Register(api, huma.Operation{OperationID: "compliance-change-request-reject", Method: http.MethodPost,
		Path: "/v1/admin/compliance/change-requests/{id}/reject", Summary: "Reject a policy change request", Tags: tags}, h.Reject)
}
