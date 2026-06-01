// Package handlers exposes the platform-admin read surface over the audit
// events collection. Writes happen through the sink (side-channel from any
// module) — this handler is read-only by design.
package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra/backend/internal/addons/compliance/models"
	"github.com/orkestra/backend/internal/addons/compliance/repository"
)

// AdminHandler reads from the audit-events repository. It does not own the
// sink — consumers register audit events through iface.AuditSink, the sink
// persists them, and this handler queries the same collection.
type AdminHandler struct {
	repo *repository.AuditEventRepository
}

// New returns a handler bound to repo.
func New(repo *repository.AuditEventRepository) *AdminHandler {
	return &AdminHandler{repo: repo}
}

// ListAuditEventsInput captures the filter surface for the admin list.
// Offset-based pagination is intentional — platform admins scrub historic
// windows by date, not a cursor stream, so offsets read better.
type ListAuditEventsInput struct {
	TenantID     string `query:"tenantId" doc:"Filter to a specific tenant"`
	ActorUserID  string `query:"actorUserId"`
	Action       string `query:"action" doc:"Exact action match (e.g. auth.login.succeeded)"`
	ActionPrefix string `query:"actionPrefix" doc:"Action family prefix (e.g. auth. to match all auth.* events)"`
	ResourceType string `query:"resourceType"`
	ResourceID   string `query:"resourceId"`
	Outcome      string `query:"outcome" enum:"success,failure,denied"`
	Since        string `query:"since" doc:"RFC3339 lower bound (inclusive)"`
	Until        string `query:"until" doc:"RFC3339 upper bound (inclusive)"`
	Limit        int    `query:"limit" default:"50" minimum:"1" maximum:"500"`
	Offset       int    `query:"offset" default:"0" minimum:"0"`
}

// ListAuditEventsOutput is the admin list response. Total is the count of
// matching rows across the full filter — Items is the current page.
type ListAuditEventsOutput struct {
	Body struct {
		Items  []models.AuditEvent `json:"items"`
		Total  int64               `json:"total"`
		Limit  int                 `json:"limit"`
		Offset int                 `json:"offset"`
	}
}

// List handles GET /v1/admin/audit-events.
func (h *AdminHandler) List(ctx context.Context, in *ListAuditEventsInput) (*ListAuditEventsOutput, error) {
	f := repository.Filter{
		TenantID:     in.TenantID,
		ActorUserID:  in.ActorUserID,
		Action:       in.Action,
		ActionPrefix: in.ActionPrefix,
		ResourceType: in.ResourceType,
		ResourceID:   in.ResourceID,
		Outcome:      in.Outcome,
		Limit:        in.Limit,
		Skip:         in.Offset,
	}
	if in.Since != "" {
		t, err := time.Parse(time.RFC3339, in.Since)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid 'since' (expected RFC3339)")
		}
		f.Since = t
	}
	if in.Until != "" {
		t, err := time.Parse(time.RFC3339, in.Until)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid 'until' (expected RFC3339)")
		}
		f.Until = t
	}

	items, total, err := h.repo.List(ctx, f)
	if err != nil {
		return nil, huma.Error500InternalServerError("list audit events", err)
	}
	out := &ListAuditEventsOutput{}
	out.Body.Items = items
	out.Body.Total = total
	out.Body.Limit = in.Limit
	out.Body.Offset = in.Offset
	return out, nil
}

// Register mounts the admin list endpoint on api. The caller is responsible
// for middleware (RequireSystemPermission("system.compliance.audit.read")).
func Register(api huma.API, h *AdminHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "compliance-audit-events-list",
		Method:      http.MethodGet,
		Path:        "/v1/admin/audit-events",
		Summary:     "List platform audit events",
		Description: "Platform admin read over the compliance audit trail. Supports filtering by tenant, actor, action, resource, outcome, and time range.",
		Tags:        []string{"Compliance"},
	}, h.List)
}
