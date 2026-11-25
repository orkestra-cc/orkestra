package services

import (
	"context"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

// DSRExportProducerKey is the producer's key in export bundles and erasure progress.
const DSRExportProducerKey = "compliance.dsr_exports"

// DSRExportProducer registers the stored exports in the PII registry: the
// subject's right of access lists them, and an erasure deletes them. Without
// it a stored export would outlive the erasure of the person it describes.
type DSRExportProducer struct {
	svc *DSRExportService
}

// NewDSRExportProducer binds the producer to the export service.
func NewDSRExportProducer(svc *DSRExportService) *DSRExportProducer {
	return &DSRExportProducer{svc: svc}
}

func (p *DSRExportProducer) Subject() string     { return "compliance" }
func (p *DSRExportProducer) ProducerKey() string { return DSRExportProducerKey }

// ExportPersonalData lists the subject's stored exports (never their content).
func (p *DSRExportProducer) ExportPersonalData(ctx context.Context, userUUID string) (any, error) {
	rows, err := p.svc.List(ctx, userUUID)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	out := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		out = append(out, map[string]any{
			"uuid":      r.UUID,
			"format":    r.Format,
			"createdAt": r.CreatedAt,
			"expiresAt": r.ExpiresAt,
		})
	}
	return out, nil
}

// PurgePersonalData deletes the subject's export objects, then their rows. Both
// modes delete: a stored export is a copy of the data, not an identity row.
// Idempotent. With no storage and exports on record it fails, so the request
// stays incomplete rather than leaving the objects behind.
func (p *DSRExportProducer) PurgePersonalData(ctx context.Context, userUUID string, _ iface.EraseMode) (iface.PurgeResult, error) {
	n, err := p.svc.PurgeUser(ctx, userUUID)
	return iface.PurgeResult{RowsDeleted: n, Collections: []string{"compliance_dsr_exports"}}, err
}

// CountPersonalData counts the subject's stored exports.
func (p *DSRExportProducer) CountPersonalData(ctx context.Context, userUUID string) (iface.PersonalDataCount, error) {
	rows, err := p.svc.List(ctx, userUUID)
	if err != nil {
		return iface.PersonalDataCount{}, err
	}
	return iface.PersonalDataCount{Rows: len(rows), Collections: []string{"compliance_dsr_exports"}}, nil
}

var (
	_ iface.PIIProducer        = (*DSRExportProducer)(nil)
	_ iface.PIIProducerKeyer   = (*DSRExportProducer)(nil)
	_ iface.PIIProducerCounter = (*DSRExportProducer)(nil)
)
