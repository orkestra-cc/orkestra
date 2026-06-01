package handlers

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra/backend/internal/addons/compliance/services"
)

// SOC2Handler exposes the platform-admin SOC2 evidence read. No POST
// surface yet — snapshots are computed on demand and not persisted in
// v1; a later commit can add /generate + a history listing once the
// operational cadence is clear.
type SOC2Handler struct {
	svc *services.SOC2EvidenceService
}

// NewSOC2Handler wires the handler to the evidence service.
func NewSOC2Handler(svc *services.SOC2EvidenceService) *SOC2Handler {
	return &SOC2Handler{svc: svc}
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
