package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra/backend/internal/core/compliance/retentionclass"
	"github.com/orkestra/backend/internal/core/compliance/services"
	"github.com/orkestra/backend/internal/shared/errcode"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// TenantPolicyHandler is the Tier-2 read-only summary (spec §7, D3): what a
// client can know about how its own data is logged and retained. No policy
// name, no platform-only class, no accountability metadata.
type TenantPolicyHandler struct {
	policies *services.PolicyService
}

func NewTenantPolicyHandler(policies *services.PolicyService) *TenantPolicyHandler {
	return &TenantPolicyHandler{policies: policies}
}

type ClientPolicyInput struct {
	TenantID string `path:"tenantId"`
}

type ClientPolicySummary struct {
	TenantID   string                       `json:"tenantId"`
	IPAddress  iface.IPAddressMode          `json:"ipAddress"`
	UserAgent  iface.UserAgentMode          `json:"userAgent"`
	SubjectIDs iface.SubjectIDMode          `json:"subjectIds"`
	Retention  map[iface.RetentionClass]int `json:"retention"`
	ValidFrom  time.Time                    `json:"validFrom"`
}

type ClientPolicyOutput struct {
	Body ClientPolicySummary
}

func (h *TenantPolicyHandler) Get(ctx context.Context, in *ClientPolicyInput) (*ClientPolicyOutput, error) {
	scoped, ok := ctxauth.GetTenantID(ctx)
	if !ok || scoped == "" || scoped != in.TenantID {
		return nil, errcode.NotFound(errcode.ComplianceTenantNotFound, "No compliance policy is available for this tenant.")
	}
	eff, ok := h.policies.Effective(in.TenantID)
	if !ok {
		return nil, errcode.ServiceUnavailable(errcode.CompliancePolicyUnavailable,
			"The compliance policies are not loaded yet; retry in a few seconds.")
	}
	lc := eff.Policy.LogContent
	s := ClientPolicySummary{TenantID: in.TenantID, IPAddress: lc.IPAddress, UserAgent: lc.UserAgent,
		SubjectIDs: lc.SubjectIDs, Retention: map[iface.RetentionClass]int{}, ValidFrom: eff.Policy.UpdatedAt}
	if eff.Assignment != nil && eff.Assignment.AssignedAt.After(s.ValidFrom) {
		s.ValidFrom = eff.Assignment.AssignedAt
	}
	for _, d := range eff.Retention {
		if c, ok := retentionclass.Get(d.Class); ok && c.TenantSettable {
			s.Retention[d.Class] = d.Days
		}
	}
	return &ClientPolicyOutput{Body: s}, nil
}

// RegisterTenantPolicyRoutes mounts the summary on the client surface;
// module.go requires tenant.read and the handler pins the path tenant to
// the caller's own.
func RegisterTenantPolicyRoutes(api huma.API, h *TenantPolicyHandler) {
	huma.Register(api, huma.Operation{OperationID: "tenant-compliance-policy", Method: http.MethodGet,
		Path: "/v1/tenants/{tenantId}/compliance/policy", Summary: "How the tenant's data is logged and retained",
		Tags: []string{"Compliance"}}, h.Get)
}
