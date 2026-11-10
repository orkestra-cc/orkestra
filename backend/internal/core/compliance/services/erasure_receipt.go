package services

import (
	"context"
	"errors"
	"time"

	"github.com/orkestra/backend/internal/core/compliance/models"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// ErrErasureReceiptUnavailable is returned for a request that is not completed
// or closed_partial: only a closed erasure has a receipt.
var ErrErasureReceiptUnavailable = errors.New("compliance: only a completed or closed_partial erasure request has a receipt")

// ReceiptProducer is one producer's outcome on a receipt.
type ReceiptProducer struct {
	Key            string `json:"key"`
	Outcome        string `json:"outcome" enum:"done,pending,failed"`
	RowsDeleted    int    `json:"rowsDeleted"`
	RowsAnonymized int    `json:"rowsAnonymized"`
}

// ErasureReceipt is the evidence of one closed erasure request, built from the
// request row (which is kept for good). It names the subject and operators by
// UUID only; the approval notes and the close note are the masked texts the
// row stores.
type ErasureReceipt struct {
	RequestUUID     string     `json:"requestUuid"`
	SubjectUserUUID string     `json:"subjectUserUuid"`
	Source          string     `json:"source,omitempty"`
	RequestedBy     string     `json:"requestedBy,omitempty"`
	Mode            string     `json:"mode,omitempty"`
	ExecutedBy      string     `json:"executedBy,omitempty"`
	Status          string     `json:"status" enum:"completed,closed_partial"`
	CloseNote       string     `json:"closeNote,omitempty" doc:"closed_partial: the operator's masked note"`
	RequestedAt     time.Time  `json:"requestedAt"`
	CompletedAt     *time.Time `json:"completedAt,omitempty"`
	Attempts        int        `json:"attempts"`

	Approvals []models.ErasureApproval `json:"approvals"`
	Producers []ReceiptProducer        `json:"producers"`
	Abandoned []string                 `json:"abandoned"`

	TotalRowsDeleted    int       `json:"totalRowsDeleted"`
	TotalRowsAnonymized int       `json:"totalRowsAnonymized"`
	GeneratedAt         time.Time `json:"generatedAt"`
}

// BuildErasureReceipt builds the receipt of a completed or closed_partial
// request; any other status is ErrErasureReceiptUnavailable. Producers keep the
// row's progress order.
func BuildErasureReceipt(row *models.ErasureRequest, now time.Time) (*ErasureReceipt, error) {
	if row == nil || (row.Status != models.ErasureRequestCompleted && row.Status != models.ErasureRequestClosedPartial) {
		return nil, ErrErasureReceiptUnavailable
	}
	r := &ErasureReceipt{
		RequestUUID:     row.UUID,
		SubjectUserUUID: row.UserUUID,
		Source:          row.Source,
		RequestedBy:     row.RequestedBy,
		Mode:            row.Mode,
		ExecutedBy:      row.ResolvedBy,
		Status:          row.Status,
		RequestedAt:     row.RequestedAt,
		CompletedAt:     row.ResolvedAt,
		Attempts:        row.Attempts,
		Approvals:       append([]models.ErasureApproval{}, row.Approvals...),
		Producers:       make([]ReceiptProducer, 0, len(row.Progress)),
		Abandoned:       append([]string{}, row.Abandoned...),
		GeneratedAt:     now,
	}
	if row.Status == models.ErasureRequestClosedPartial {
		r.CloseNote = row.ResolutionNote
	}
	for _, p := range row.Progress {
		r.Producers = append(r.Producers, ReceiptProducer{Key: p.Key, Outcome: p.Outcome, RowsDeleted: p.RowsDeleted, RowsAnonymized: p.RowsAnonymized})
		r.TotalRowsDeleted += p.RowsDeleted
		r.TotalRowsAnonymized += p.RowsAnonymized
	}
	return r, nil
}

// auditMetadata is the receipt without its notes: UUIDs, counts and keys.
func (r *ErasureReceipt) auditMetadata() map[string]any {
	approvals := make([]map[string]any, 0, len(r.Approvals))
	for _, a := range r.Approvals {
		approvals = append(approvals, map[string]any{"by": a.By, "at": a.At})
	}
	producers := make([]map[string]any, 0, len(r.Producers))
	for _, p := range r.Producers {
		producers = append(producers, map[string]any{
			"key": p.Key, "outcome": p.Outcome, "rowsDeleted": p.RowsDeleted, "rowsAnonymized": p.RowsAnonymized,
		})
	}
	m := map[string]any{
		"requestUuid":         r.RequestUUID,
		"subjectUserUuid":     r.SubjectUserUUID,
		"source":              r.Source,
		"requestedBy":         r.RequestedBy,
		"mode":                r.Mode,
		"executedBy":          r.ExecutedBy,
		"status":              r.Status,
		"requestedAt":         r.RequestedAt,
		"attempts":            r.Attempts,
		"approvals":           approvals,
		"producers":           producers,
		"abandoned":           r.Abandoned,
		"totalRowsDeleted":    r.TotalRowsDeleted,
		"totalRowsAnonymized": r.TotalRowsAnonymized,
	}
	if r.CompletedAt != nil {
		m["completedAt"] = *r.CompletedAt
	}
	return m
}

// emitReceipt audits the receipt of a request that has just closed (nil-safe).
func (s *ErasureRequestService) emitReceipt(ctx context.Context, row *models.ErasureRequest, actor string) {
	if s.sink == nil {
		return
	}
	rc, err := BuildErasureReceipt(row, s.now())
	if err != nil {
		return
	}
	actorType := models.ActorTypeUser
	if actor == "" {
		actorType = models.ActorTypeSystem
	}
	s.sink.Emit(ctx, iface.AuditEvent{
		ActorUserID:  actor,
		ActorType:    actorType,
		Action:       "gdpr.dsr.erasure_receipt",
		ResourceType: "erasure_request",
		ResourceID:   row.UUID,
		Outcome:      "success",
		Metadata:     rc.auditMetadata(),
	})
}

// Receipt returns the receipt of a completed or closed_partial request; an
// unknown id is repository.ErrErasureRequestNotFound, any other status
// ErrErasureReceiptUnavailable.
func (s *ErasureRequestService) Receipt(ctx context.Context, requestUUID string) (*ErasureReceipt, error) {
	row, err := s.repo.Get(ctx, requestUUID)
	if err != nil {
		return nil, err
	}
	return BuildErasureReceipt(row, s.now())
}
