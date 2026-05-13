package services

import (
	"context"
	"log/slog"
	"time"

	"github.com/orkestra-cc/orkestra-sdk/iface"
)

// DSRService orchestrates the GDPR data-subject-request pipelines:
// right-of-access (Export) and right-to-erasure (Erase). It reads from
// the pre-boot PIIProducerRegistry and walks every registered producer
// in registration order — determinism matters for reproducible export
// bundles and erasure audit trails.
//
// v1 is synchronous: both Export and Erase block the caller until every
// producer completes. The producer count is small (core user + auth, a
// handful of addons in later commits) and the surface is per-user so
// total latency stays in the low hundreds of milliseconds. Later phases
// can add a job table and run DSRs asynchronously if the round-trip
// becomes a UX issue.
type DSRService struct {
	registry *iface.PIIProducerRegistry
	sink     iface.AuditSink
	logger   *slog.Logger
}

// NewDSRService builds a DSR service. The registry is the shared,
// pre-boot producer catalog; sink is the compliance audit sink (may be
// nil if the caller wants DSRs without audit rows, though the plan of
// record always wires it).
func NewDSRService(registry *iface.PIIProducerRegistry, sink iface.AuditSink, logger *slog.Logger) *DSRService {
	return &DSRService{registry: registry, sink: sink, logger: logger}
}

// ExportResult pairs a bundle with diagnostics about the producer walk.
// Errors from individual producers are logged and returned in Errors so
// the caller can decide whether a partial bundle is acceptable.
type ExportResult struct {
	Bundle    iface.PersonalDataBundle
	Producers []string
	Errors    map[string]string
}

// Export runs every registered producer's ExportPersonalData for
// userUUID and assembles the bundle. Metadata section is always
// present; producer keys are added only when the producer returned
// non-nil data. An audit row is emitted on completion.
func (s *DSRService) Export(ctx context.Context, userUUID string) (*ExportResult, error) {
	result := &ExportResult{
		Bundle: iface.PersonalDataBundle{
			"metadata": map[string]any{
				"userUuid":   userUUID,
				"exportedAt": time.Now().UTC().Format(time.RFC3339),
				"version":    "1",
			},
		},
		Errors: map[string]string{},
	}

	for _, p := range s.registry.List() {
		subject := p.Subject()
		data, err := p.ExportPersonalData(ctx, userUUID)
		if err != nil {
			s.logger.Warn("DSR: export producer failed",
				slog.String("subject", subject),
				slog.String("userUuid", userUUID),
				slog.String("error", err.Error()),
			)
			result.Errors[subject] = err.Error()
			continue
		}
		result.Producers = append(result.Producers, subject)
		if data != nil {
			result.Bundle[subject] = data
		}
	}

	s.emitAudit(ctx, userUUID, "gdpr.dsr.exported", map[string]any{
		"producers": result.Producers,
		"errors":    len(result.Errors),
	})
	return result, nil
}

// EraseResult is the per-producer summary returned to the DSR caller.
// Mirrors ExportResult shape so the API surface is symmetrical.
type EraseResult struct {
	Purged map[string]iface.PurgeResult
	Errors map[string]string
}

// Erase runs every producer's PurgePersonalData. Emits an audit row
// with the aggregate erase summary. Producer errors are surfaced in the
// result; the service does not abort the whole pipeline on a single
// producer failure — partial erasure is still personally-data-reducing
// and the audit row makes it auditable.
func (s *DSRService) Erase(ctx context.Context, userUUID string) (*EraseResult, error) {
	result := &EraseResult{
		Purged: map[string]iface.PurgeResult{},
		Errors: map[string]string{},
	}

	for _, p := range s.registry.List() {
		subject := p.Subject()
		res, err := p.PurgePersonalData(ctx, userUUID)
		if err != nil {
			s.logger.Warn("DSR: purge producer failed",
				slog.String("subject", subject),
				slog.String("userUuid", userUUID),
				slog.String("error", err.Error()),
			)
			result.Errors[subject] = err.Error()
			continue
		}
		result.Purged[subject] = res
	}

	s.emitAudit(ctx, userUUID, "gdpr.dsr.erased", eraseMetadata(result))
	return result, nil
}

func (s *DSRService) emitAudit(ctx context.Context, userUUID, action string, metadata map[string]any) {
	if s.sink == nil {
		return
	}
	s.sink.Emit(ctx, iface.AuditEvent{
		ActorUserID:  userUUID,
		ActorType:    "user",
		Action:       action,
		ResourceType: "user",
		ResourceID:   userUUID,
		Metadata:     metadata,
	})
}

func eraseMetadata(r *EraseResult) map[string]any {
	out := map[string]any{}
	perSubject := map[string]any{}
	var totalRows int
	for subject, res := range r.Purged {
		perSubject[subject] = map[string]any{
			"rowsDeleted":    res.RowsDeleted,
			"rowsAnonymized": res.RowsAnonymized,
			"collections":    res.Collections,
		}
		totalRows += res.RowsDeleted + res.RowsAnonymized
	}
	out["perSubject"] = perSubject
	out["totalRows"] = totalRows
	out["errorCount"] = len(r.Errors)
	return out
}
