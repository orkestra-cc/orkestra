package handlers

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra/backend/internal/core/compliance/services"
)

// SOC2Handler exposes the platform-admin SOC2 evidence read. No POST
// surface yet — snapshots are computed on demand and not persisted in
// v1; a later commit can add /generate + a history listing once the
// operational cadence is clear.
//
// The route is mounted unconditionally so the compliance.soc2_enabled toggle
// takes effect at runtime; enabled gates each request (404 when off) so the
// endpoint stays dormant unless the sub-feature is on.
type SOC2Handler struct {
	svc     *services.SOC2EvidenceService
	enabled func(context.Context) bool
}

// NewSOC2Handler wires the handler to the evidence service. enabled is read
// per request so an admin toggle of compliance.soc2_enabled needs no restart.
func NewSOC2Handler(svc *services.SOC2EvidenceService, enabled func(context.Context) bool) *SOC2Handler {
	return &SOC2Handler{svc: svc, enabled: enabled}
}

// EvidenceOutput wraps the evidence snapshot for Huma's JSON-body
// response convention.
type EvidenceOutput struct {
	Body *services.Evidence
}

// Evidence handles GET /v1/admin/compliance/soc2/evidence. Every
// invocation recomputes the snapshot from source — idempotent and
// repeatable, so two auditors hitting the endpoint a minute apart see
// the same answer when state hasn't changed.
func (h *SOC2Handler) Evidence(ctx context.Context, _ *struct{}) (*EvidenceOutput, error) {
	if h.enabled != nil && !h.enabled(ctx) {
		return nil, huma.Error404NotFound("SOC2 evidence is disabled (enable compliance.soc2_enabled)")
	}
	ev, err := h.svc.Generate(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("generate soc2 evidence", err)
	}
	return &EvidenceOutput{Body: ev}, nil
}

// RegisterSOC2Routes mounts the evidence endpoint on api. Gated
// (at the caller side) by the same system permission as the audit
// read so a single compliance role covers both surfaces.
func RegisterSOC2Routes(api huma.API, h *SOC2Handler) {
	huma.Register(api, huma.Operation{
		OperationID: "compliance-soc2-evidence",
		Method:      http.MethodGet,
		Path:        "/v1/admin/compliance/soc2/evidence",
		Summary:     "Generate a SOC2 evidence snapshot",
		Description: "Returns a point-in-time aggregate of CC-class controls — privileged user count, MFA coverage, failed-login trends, KMS lifecycle, audit-trail health. Idempotent; each call recomputes from source.",
		Tags:        []string{"Compliance", "SOC2"},
	}, h.Evidence)
}
