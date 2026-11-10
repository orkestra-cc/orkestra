package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra/backend/internal/core/compliance/models"
	"github.com/orkestra/backend/internal/core/compliance/services"
)

// DSRPreviewHandler is the admin surface of the dry-run erasure preview.
type DSRPreviewHandler struct {
	svc *services.DSRPreviewService
}

// NewDSRPreviewHandler binds the handler to its service.
func NewDSRPreviewHandler(svc *services.DSRPreviewService) *DSRPreviewHandler {
	return &DSRPreviewHandler{svc: svc}
}

// ComplianceDSRPreviewProducer is one producer's line of the preview.
type ComplianceDSRPreviewProducer struct {
	Key         string   `json:"key" doc:"Producer key, the same one erasure progress and export bundles use"`
	Countable   bool     `json:"countable" doc:"False when the producer cannot count without deleting"`
	Rows        int      `json:"rows" doc:"Rows this producer would touch. Not additive across producers: two producers can report the same stored rows (the two auth instances share device-trust and security-event rows), so a sum of rows is an upper bound, never an exact count"`
	Collections []string `json:"collections" doc:"Collections the count covers"`
	Error       string   `json:"error,omitempty" doc:"Fixed text when counting failed or timed out; detail is only in the server log"`
}

// ComplianceDSRPreviewRetention is the obligation that blocks a hard delete.
type ComplianceDSRPreviewRetention struct {
	RetainUntil time.Time `json:"retainUntil"`
	Reasons     []string  `json:"reasons"`
}

// ComplianceDSRPreview is the dry-run of a subject's erasure.
type ComplianceDSRPreview struct {
	UserUUID          string                         `json:"userUuid"`
	LegalHolds        []models.LegalHold             `json:"legalHolds" doc:"Holds that block erasure now"`
	Retention         *ComplianceDSRPreviewRetention `json:"retention,omitempty" doc:"Obligation that blocks a hard delete; absent when none"`
	RetentionError    string                         `json:"retentionError,omitempty" doc:"Set when the retention gate could not be evaluated; a hard delete is then not allowed"`
	OpenRequests      []models.ErasureRequest        `json:"openRequests" doc:"Pending and incomplete erasure requests of the subject"`
	Producers         []ComplianceDSRPreviewProducer `json:"producers" doc:"Per-producer counts in registry order. They are not summed on purpose"`
	HardDeleteAllowed bool                           `json:"hardDeleteAllowed" doc:"No active hold, no retention obligation and the gate evaluated"`
	AnonymizeAllowed  bool                           `json:"anonymizeAllowed" doc:"No active hold"`
}

type DSRPreviewInput struct {
	UserUUID string `query:"userUuid" required:"true" minLength:"1" doc:"Data subject"`
}
type DSRPreviewOutput struct {
	Body ComplianceDSRPreview
}

// Preview handles GET /v1/admin/compliance/dsr/preview.
func (h *DSRPreviewHandler) Preview(ctx context.Context, in *DSRPreviewInput) (*DSRPreviewOutput, error) {
	p, err := h.svc.Preview(ctx, in.UserUUID)
	if err != nil {
		return nil, huma.Error500InternalServerError("dsr preview", err)
	}
	body := ComplianceDSRPreview{
		UserUUID: p.UserUUID, LegalHolds: p.LegalHolds, RetentionError: p.RetentionError,
		OpenRequests: p.OpenRequests, HardDeleteAllowed: p.HardDeleteAllowed, AnonymizeAllowed: p.AnonymizeAllowed,
		Producers: make([]ComplianceDSRPreviewProducer, 0, len(p.Producers)),
	}
	if p.Retention != nil {
		body.Retention = &ComplianceDSRPreviewRetention{RetainUntil: p.Retention.RetainUntil, Reasons: p.Retention.Reasons}
	}
	for _, c := range p.Producers {
		cols := c.Collections
		if cols == nil {
			cols = []string{}
		}
		body.Producers = append(body.Producers, ComplianceDSRPreviewProducer{
			Key: c.Key, Countable: c.Countable, Rows: c.Rows, Collections: cols, Error: c.Error,
		})
	}
	return &DSRPreviewOutput{Body: body}, nil
}

// RegisterDSRPreviewRoutes mounts the preview (audit.read).
func RegisterDSRPreviewRoutes(api huma.API, h *DSRPreviewHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "compliance-dsr-preview",
		Method:      http.MethodGet,
		Path:        "/v1/admin/compliance/dsr/preview",
		Summary:     "Dry-run of a data subject's erasure: holds, retention, open requests and per-producer counts",
		Tags:        []string{"Compliance"},
	}, h.Preview)
}
