package handlers

import (
	"context"
	"net/http"

	"github.com/orkestra/backend/internal/core/compliance/models"
	"github.com/orkestra/backend/internal/core/compliance/services"
)

type ListAssignmentsOutput struct {
	Body struct {
		Items []services.AssignmentView `json:"items"`
	}
}

func (h *PolicyHandler) ListAssignments(ctx context.Context, _ *struct{}) (*ListAssignmentsOutput, error) {
	items, err := h.admin.ListAssignments(ctx)
	if err != nil {
		return nil, mapPolicyError(ctx, err)
	}
	out := &ListAssignmentsOutput{}
	out.Body.Items = items
	return out, nil
}

type ValidateAssignmentInput struct {
	TenantID string `path:"tenantId"`
	Body     struct {
		PolicyID string `json:"policyId,omitempty" doc:"Policy to assign; empty previews moving the tenant back to the platform policy"`
	}
}

func (h *PolicyHandler) ValidateAssignment(ctx context.Context, in *ValidateAssignmentInput) (*ValidationOutput, error) {
	r, err := h.admin.ValidateAssignment(ctx, in.TenantID, in.Body.PolicyID)
	if err != nil {
		return nil, mapPolicyError(ctx, err)
	}
	return validationOutput(r), nil
}

type AssignPolicyInput struct {
	TenantID string `path:"tenantId"`
	Body     struct {
		PolicyID            string `json:"policyId"`
		Reason              string `json:"reason"`
		AcknowledgeWarnings bool   `json:"acknowledgeWarnings,omitempty"`
	}
}

func (h *PolicyHandler) Assign(ctx context.Context, in *AssignPolicyInput) (*WriteOutput, error) {
	res, err := h.admin.Assign(ctx, services.ActorFromContext(ctx), in.TenantID, in.Body.PolicyID, in.Body.Reason, in.Body.AcknowledgeWarnings)
	if err != nil {
		return nil, mapPolicyError(ctx, err)
	}
	return writeOutput(res, http.StatusOK), nil
}

type UnassignPolicyInput struct {
	TenantID string `path:"tenantId"`
	Body     struct {
		Reason              string `json:"reason"`
		AcknowledgeWarnings bool   `json:"acknowledgeWarnings,omitempty"`
	}
}

func (h *PolicyHandler) Unassign(ctx context.Context, in *UnassignPolicyInput) (*WriteOutput, error) {
	res, err := h.admin.Unassign(ctx, services.ActorFromContext(ctx), in.TenantID, in.Body.Reason, in.Body.AcknowledgeWarnings)
	if err != nil {
		return nil, mapPolicyError(ctx, err)
	}
	return writeOutput(res, http.StatusOK), nil
}

type ListChangeRequestsInput struct {
	Status string `query:"status" enum:"pending,approved,rejected,superseded,expired" required:"false" doc:"Filter by status; all when empty"`
}

type ListChangeRequestsOutput struct {
	Body struct {
		Items []models.PolicyChangeRequest `json:"items"`
	}
}

func (h *PolicyHandler) ListChangeRequests(ctx context.Context, in *ListChangeRequestsInput) (*ListChangeRequestsOutput, error) {
	items, err := h.admin.ListChangeRequests(ctx, in.Status)
	if err != nil {
		return nil, mapPolicyError(ctx, err)
	}
	out := &ListChangeRequestsOutput{}
	out.Body.Items = items
	return out, nil
}

type ChangeRequestIDInput struct {
	ID string `path:"id"`
}

type ChangeRequestOutput struct {
	Body models.PolicyChangeRequest
}

func (h *PolicyHandler) GetChangeRequest(ctx context.Context, in *ChangeRequestIDInput) (*ChangeRequestOutput, error) {
	cr, err := h.admin.GetChangeRequest(ctx, in.ID)
	if err != nil {
		return nil, mapPolicyError(ctx, err)
	}
	return &ChangeRequestOutput{Body: *cr}, nil
}

type DecideChangeRequestInput struct {
	ID   string `path:"id"`
	Body struct {
		Note string `json:"note" doc:"Why the request is approved or rejected (1-500 characters)"`
	}
}

func (h *PolicyHandler) Approve(ctx context.Context, in *DecideChangeRequestInput) (*WriteOutput, error) {
	res, err := h.admin.Approve(ctx, services.ActorFromContext(ctx), in.ID, in.Body.Note)
	if err != nil {
		return nil, mapPolicyError(ctx, err)
	}
	return writeOutput(res, http.StatusOK), nil
}

func (h *PolicyHandler) Reject(ctx context.Context, in *DecideChangeRequestInput) (*ChangeRequestOutput, error) {
	cr, err := h.admin.Reject(ctx, services.ActorFromContext(ctx), in.ID, in.Body.Note)
	if err != nil {
		return nil, mapPolicyError(ctx, err)
	}
	return &ChangeRequestOutput{Body: *cr}, nil
}
