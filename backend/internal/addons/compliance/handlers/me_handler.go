// Package handlers hosts the compliance module's HTTP surface. me_handler
// exposes the GDPR data-subject endpoints — a user can request an export
// of their personal data (right of access + portability) or erase it
// (right to erasure).
package handlers

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra/backend/internal/addons/compliance/services"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// MeHandler exposes the caller-self DSR endpoints. No admin surface —
// these routes are only reachable by the authenticated subject, and the
// DSR service enforces userUUID-scoped access by construction (producers
// only accept a userUUID; they never receive an admin override). An
// admin tool for operator-initiated DSR lands later when CSR-side
// ticketing is wired.
type MeHandler struct {
	dsr *services.DSRService
}

// NewMeHandler constructs the handler bound to the DSR service.
func NewMeHandler(dsr *services.DSRService) *MeHandler {
	return &MeHandler{dsr: dsr}
}

// --- DTOs ---

// ExportOutput carries the personal data bundle plus a provenance
// section listing the producers that contributed. Errors are surfaced
// so a partial export is obvious to the caller.
type ExportOutput struct {
	Body struct {
		Bundle    iface.PersonalDataBundle `json:"bundle"`
		Producers []string                 `json:"producers"`
		Errors    map[string]string        `json:"errors,omitempty"`
	}
}

// EraseOutput summarizes what the erasure pipeline wiped. Callers can
// rely on the totalRows count as a rough completeness signal.
type EraseOutput struct {
	Body struct {
		Purged    map[string]iface.PurgeResult `json:"purged"`
		TotalRows int                          `json:"totalRows"`
		Errors    map[string]string            `json:"errors,omitempty"`
	}
}

// --- handlers ---

// Export handles POST /v1/me/dsr/export. The DSR service runs every
// registered producer for the authenticated userUUID and returns the
// bundled payload inline. v1 is synchronous — later phases can queue
// the job and return a job ID if the walk ever outgrows an HTTP
// timeout.
func (h *MeHandler) Export(ctx context.Context, _ *struct{}) (*ExportOutput, error) {
	userUUID, ok := ctxauth.GetUserUUID(ctx)
	if !ok || userUUID == "" {
		return nil, huma.Error401Unauthorized("authentication required")
	}
	res, err := h.dsr.Export(ctx, userUUID)
	if err != nil {
		return nil, huma.Error500InternalServerError("dsr export", err)
	}
	out := &ExportOutput{}
	out.Body.Bundle = res.Bundle
	out.Body.Producers = res.Producers
	if len(res.Errors) > 0 {
		out.Body.Errors = res.Errors
	}
	return out, nil
}

// Erase handles POST /v1/me/dsr/erase. Irreversible — the authenticated
// user's records are hard-deleted across every registered producer. The
// user's access token is already issued and will keep validating until
// it expires (15 min by default), but subsequent refresh attempts fail
// because the refresh-token rows are gone.
func (h *MeHandler) Erase(ctx context.Context, _ *struct{}) (*EraseOutput, error) {
	userUUID, ok := ctxauth.GetUserUUID(ctx)
	if !ok || userUUID == "" {
		return nil, huma.Error401Unauthorized("authentication required")
	}
	res, err := h.dsr.Erase(ctx, userUUID)
	if err != nil {
		return nil, huma.Error500InternalServerError("dsr erase", err)
	}
	out := &EraseOutput{}
	out.Body.Purged = res.Purged
	for _, r := range res.Purged {
		out.Body.TotalRows += r.RowsDeleted + r.RowsAnonymized
	}
	if len(res.Errors) > 0 {
		out.Body.Errors = res.Errors
	}
	return out, nil
}

// RegisterMeRoutes mounts the DSR endpoints on a protected API already
// gated by RequireAuth + RequireGlobal (caller is authenticated; no
// tenant-scoping needed because the flow operates on the caller's own
// userUUID).
func RegisterMeRoutes(api huma.API, h *MeHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "compliance-me-dsr-export",
		Method:      http.MethodPost,
		Path:        "/v1/me/dsr/export",
		Summary:     "Export the caller's personal data (GDPR right of access)",
		Description: "Synchronously collects the caller's personal data across every registered PII producer and returns it inline. Safe to retry — read-only.",
		Tags:        []string{"Compliance", "DSR"},
	}, h.Export)

	huma.Register(api, huma.Operation{
		OperationID: "compliance-me-dsr-erase",
		Method:      http.MethodPost,
		Path:        "/v1/me/dsr/erase",
		Summary:     "Erase the caller's personal data (GDPR right to erasure)",
		Description: "Irreversibly deletes the caller's personal data across every registered PII producer. After erasure the caller's access token keeps validating until it expires; subsequent refresh fails because the refresh-token rows are wiped.",
		Tags:        []string{"Compliance", "DSR"},
	}, h.Erase)
}

// SelfDeletionGate returns true when the caller's audience is allowed to
// trigger self-service erasure. Phase 8 of the auth-policy roadmap:
// operator surface is unconditionally allowed (admins control their
// own deletion); client surface is gated on the
// selfServiceAccountDeletionClient policy toggle.
type SelfDeletionGate func(ctx context.Context) bool

// EraseGated wraps Erase with a SelfDeletionGate check. Returns 403
// when the gate denies; otherwise delegates to Erase. Exported so the
// gate logic can be exercised by tests without a full Huma server.
func (h *MeHandler) EraseGated(ctx context.Context, gate SelfDeletionGate, req *struct{}) (*EraseOutput, error) {
	if gate != nil && !gate(ctx) {
		return nil, huma.NewError(http.StatusForbidden, "Self-service account deletion is disabled. Contact an administrator.")
	}
	return h.Erase(ctx, req)
}

// RegisterClientMeRoutes mounts the DSR endpoints on the client API
// surface. Export is unconditional — read access is always safe.
// Erase is wrapped in the gate: when SelfServiceAccountDeletionClient
// is off the route returns 403 self_service_deletion_disabled instead
// of running the erasure pipeline.
func RegisterClientMeRoutes(api huma.API, h *MeHandler, gate SelfDeletionGate) {
	huma.Register(api, huma.Operation{
		OperationID: "compliance-me-dsr-export-client",
		Method:      http.MethodPost,
		Path:        "/v1/me/dsr/export",
		Summary:     "Export the caller's personal data (GDPR right of access)",
		Description: "Synchronously collects the caller's personal data across every registered PII producer and returns it inline. Safe to retry — read-only.",
		Tags:        []string{"Compliance", "DSR"},
	}, h.Export)

	huma.Register(api, huma.Operation{
		OperationID: "compliance-me-dsr-erase-client",
		Method:      http.MethodPost,
		Path:        "/v1/me/dsr/erase",
		Summary:     "Erase the caller's personal data (GDPR right to erasure)",
		Description: "Irreversibly deletes the caller's personal data. Gated on the auth.selfServiceAccountDeletionClient admin toggle — disabled by default.",
		Tags:        []string{"Compliance", "DSR"},
	}, func(ctx context.Context, req *struct{}) (*EraseOutput, error) {
		return h.EraseGated(ctx, gate, req)
	})
}
