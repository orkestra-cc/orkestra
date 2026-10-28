package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/orkestra/backend/internal/core/compliance/models"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// ErrLegalHoldActive is returned by Erase when the subject is under an active
// legal hold — erasure (and retention auto-cleanup) must not proceed.
var ErrLegalHoldActive = errors.New("compliance: data subject is under an active legal hold")

// errProducerPanicked is the fixed error recorded for a producer that
// panicked: the panic value and stack are logged, never returned.
var errProducerPanicked = errors.New("producer panicked")

// LegalHoldChecker reports whether a subject is under an active legal hold.
// Satisfied by *LegalHoldService; injected via SetLegalHoldChecker so the
// DSR service stays usable (and testable) without the legal-hold subsystem.
type LegalHoldChecker interface {
	IsHeld(ctx context.Context, userUUID string) (bool, error)
}

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
	registry    *iface.PIIProducerRegistry
	sink        iface.AuditSink
	logger      *slog.Logger
	holdChecker LegalHoldChecker
	warnedKeys  sync.Map // suffixed producer keys already warned about
}

// SetLegalHoldChecker wires the legal-hold gate. When set, Erase refuses to
// run if the subject is under an active hold.
func (s *DSRService) SetLegalHoldChecker(c LegalHoldChecker) { s.holdChecker = c }

// NewDSRService builds a DSR service. The registry is the shared,
// pre-boot producer catalog; sink is the compliance audit sink (may be
// nil if the caller wants DSRs without audit rows, though the plan of
// record always wires it).
func NewDSRService(registry *iface.PIIProducerRegistry, sink iface.AuditSink, logger *slog.Logger) *DSRService {
	return &DSRService{registry: registry, sink: sink, logger: logger}
}

// ExportResult pairs a bundle with diagnostics about the producer walk.
// Errors from individual producers are logged and returned in Errors so
// the caller can decide whether a partial bundle is acceptable. Bundle,
// Producers and Errors are keyed by producer key (see producerKeys).
type ExportResult struct {
	Bundle    iface.PersonalDataBundle
	Producers []string
	Errors    map[string]string
}

// bundleMetadataKey is the export bundle's own section; no producer may use it.
const bundleMetadataKey = "metadata"

// baseProducerKey is the producer's ProducerKey() when it implements
// iface.PIIProducerKeyer and returns a non-empty key, else its Subject().
func baseProducerKey(p iface.PIIProducer) string {
	if kp, ok := p.(iface.PIIProducerKeyer); ok {
		if k := strings.TrimSpace(kp.ProducerKey()); k != "" {
			return k
		}
	}
	return p.Subject()
}

// producerKeys returns one key per producer, in the same order: its
// baseProducerKey, with "#2", "#3"... appended when the key is already taken
// (by an earlier producer or by the reserved "metadata"). Erase and export
// both use it so a producer has one identity.
func producerKeys(ps []iface.PIIProducer) []string {
	keys := make([]string, len(ps))
	used := make(map[string]bool, len(ps)+1)
	used[bundleMetadataKey] = true
	for i, p := range ps {
		base := baseProducerKey(p)
		k := base
		for n := 2; used[k]; n++ {
			k = fmt.Sprintf("%s#%d", base, n)
		}
		used[k] = true
		keys[i] = k
	}
	return keys
}

// keysFor is producerKeys plus a one-time WARN per suffixed key: a "#n" key
// depends on registration order, so it is not stable across deploys.
func (s *DSRService) keysFor(ps []iface.PIIProducer) []string {
	keys := producerKeys(ps)
	for i, p := range ps {
		if keys[i] == baseProducerKey(p) {
			continue
		}
		if _, warned := s.warnedKeys.LoadOrStore(keys[i], true); warned {
			continue
		}
		s.logger.Warn("DSR: producer key taken; using a suffixed key that depends on registration order. Implement iface.PIIProducerKeyer with a unique key so erasure progress and export bundles stay stable across deploys",
			slog.String("subject", p.Subject()),
			slog.String("key", keys[i]),
		)
	}
	return keys
}

// exportOne calls the producer's export, turning a panic into errProducerPanicked.
func (s *DSRService) exportOne(ctx context.Context, p iface.PIIProducer, key, userUUID string) (data any, err error) {
	defer func() {
		if r := recover(); r != nil {
			s.logProducerPanic("export", p, key, userUUID, r)
			data, err = nil, errProducerPanicked
		}
	}()
	return p.ExportPersonalData(ctx, userUUID)
}

// purgeOne calls the producer's purge, turning a panic into errProducerPanicked
// so one broken producer cannot crash the replica or stop the others.
func (s *DSRService) purgeOne(ctx context.Context, p iface.PIIProducer, key, userUUID string, mode iface.EraseMode) (res iface.PurgeResult, err error) {
	defer func() {
		if r := recover(); r != nil {
			s.logProducerPanic("purge", p, key, userUUID, r)
			res, err = iface.PurgeResult{}, errProducerPanicked
		}
	}()
	return p.PurgePersonalData(ctx, userUUID, mode)
}

func (s *DSRService) logProducerPanic(op string, p iface.PIIProducer, key, userUUID string, r any) {
	s.logger.Error("DSR: "+op+" producer panicked",
		slog.String("subject", p.Subject()),
		slog.String("key", key),
		slog.String("userUuid", userUUID),
		slog.String("panic", fmt.Sprint(r)),
		slog.String("stack", string(debug.Stack())),
	)
}

// RegisteredKeys returns the producer keys of the current registry.
func (s *DSRService) RegisteredKeys() map[string]bool {
	out := map[string]bool{}
	for _, k := range s.keysFor(s.registry.List()) {
		out[k] = true
	}
	return out
}

// PlanProgress returns prior with every registered producer it lacks added as
// pending (registered producers in registration order, then prior keys no
// longer registered); added reports whether it added any. A run stores the
// plan before it calls a producer, so a request whose run dies before Settle
// still names every producer that held the subject's data, and stays
// incomplete (never completed) if one of them is later unregistered.
func (s *DSRService) PlanProgress(prior []models.ErasureProducerProgress) (plan []models.ErasureProducerProgress, added bool) {
	priorByKey := make(map[string]models.ErasureProducerProgress, len(prior))
	for _, p := range prior {
		priorByKey[p.Key] = p
	}
	producers := s.registry.List()
	keys := s.keysFor(producers)
	registered := make(map[string]bool, len(keys))
	now := time.Now().UTC()
	for i, p := range producers {
		registered[keys[i]] = true
		if pp, ok := priorByKey[keys[i]]; ok {
			plan = append(plan, pp)
			continue
		}
		added = true
		plan = append(plan, models.ErasureProducerProgress{
			Key: keys[i], Subject: p.Subject(), Outcome: models.ProducerOutcomePending, UpdatedAt: now,
		})
	}
	for _, pp := range prior {
		if !registered[pp.Key] {
			plan = append(plan, pp)
		}
	}
	return plan, added
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
				"version":    "2",
			},
		},
		Errors: map[string]string{},
	}

	producers := s.registry.List()
	keys := s.keysFor(producers)
	for i, p := range producers {
		key := keys[i]
		data, err := s.exportOne(ctx, p, key, userUUID)
		if err != nil {
			s.logger.Warn("DSR: export producer failed",
				slog.String("subject", p.Subject()),
				slog.String("key", key),
				slog.String("userUuid", userUUID),
				slog.String("error", err.Error()),
			)
			result.Errors[key] = err.Error()
			continue
		}
		result.Producers = append(result.Producers, key)
		if data != nil {
			result.Bundle[key] = data
		}
	}

	s.emitAudit(ctx, userUUID, "gdpr.dsr.exported", map[string]any{
		"producers": result.Producers,
		"errors":    len(result.Errors),
	})
	return result, nil
}

// EraseResult is the summary of one erase run. Purged and Errors hold this
// run only, keyed by producer key; Purged includes the partial result of a
// failed producer. Progress is merged with the prior progress: registered
// producers in registration order, then prior keys no longer registered.
type EraseResult struct {
	Purged   map[string]iface.PurgeResult
	Errors   map[string]string
	Progress []models.ErasureProducerProgress
}

// Complete reports whether every producer in Progress is done.
func (r *EraseResult) Complete() bool {
	for _, p := range r.Progress {
		if p.Outcome != models.ProducerOutcomeDone {
			return false
		}
	}
	return true
}

// CheckLegalHold returns ErrLegalHoldActive when the subject is held, the
// checker's error when it fails, or nil (also when no checker is wired).
func (s *DSRService) CheckLegalHold(ctx context.Context, userUUID string) error {
	if s.holdChecker == nil {
		return nil
	}
	held, err := s.holdChecker.IsHeld(ctx, userUUID)
	if err != nil {
		return err
	}
	if held {
		return ErrLegalHoldActive
	}
	return nil
}

// Erase runs every producer's PurgePersonalData once; see EraseRemaining.
func (s *DSRService) Erase(ctx context.Context, userUUID string, mode iface.EraseMode) (*EraseResult, error) {
	return s.EraseRemaining(ctx, userUUID, mode, nil)
}

// EraseRemaining runs the producers that are not yet done according to prior
// and merges the outcome. An active legal hold blocks it outright (no
// producer runs, no audit "erased" row). Producer errors are surfaced in the
// result; one failure (a panic included) does not abort the others — partial
// erasure still reduces personal data and the audit row makes it auditable.
func (s *DSRService) EraseRemaining(ctx context.Context, userUUID string, mode iface.EraseMode, prior []models.ErasureProducerProgress) (*EraseResult, error) {
	if err := s.CheckLegalHold(ctx, userUUID); err != nil {
		return nil, err
	}

	result := &EraseResult{
		Purged: map[string]iface.PurgeResult{},
		Errors: map[string]string{},
	}
	priorByKey := make(map[string]models.ErasureProducerProgress, len(prior))
	for _, p := range prior {
		priorByKey[p.Key] = p
	}

	producers := s.registry.List()
	keys := s.keysFor(producers)
	registered := make(map[string]bool, len(keys))
	for i, p := range producers {
		key := keys[i]
		registered[key] = true
		pp, hadPrior := priorByKey[key]
		if hadPrior && pp.Outcome == models.ProducerOutcomeDone {
			result.Progress = append(result.Progress, pp)
			continue
		}
		res, err := s.purgeOne(ctx, p, key, userUUID, mode)
		result.Purged[key] = res
		outcome := models.ProducerOutcomeDone
		switch {
		case err != nil:
			outcome = models.ProducerOutcomeFailed
			s.logger.Warn("DSR: purge producer failed",
				slog.String("subject", p.Subject()),
				slog.String("key", key),
				slog.String("userUuid", userUUID),
				slog.String("error", err.Error()),
			)
			result.Errors[key] = err.Error()
		case res.Pending:
			outcome = models.ProducerOutcomePending
		}
		result.Progress = append(result.Progress, models.ErasureProducerProgress{
			Key:            key,
			Subject:        p.Subject(),
			Outcome:        outcome,
			RowsDeleted:    pp.RowsDeleted + res.RowsDeleted,
			RowsAnonymized: pp.RowsAnonymized + res.RowsAnonymized,
			Attempts:       pp.Attempts + 1,
			UpdatedAt:      time.Now().UTC(),
		})
	}
	for _, pp := range prior {
		if !registered[pp.Key] {
			result.Progress = append(result.Progress, pp)
		}
	}

	meta := eraseMetadata(result)
	meta["mode"] = eraseModeLabel(mode)
	s.emitAudit(ctx, userUUID, "gdpr.dsr.erased", meta)
	return result, nil
}

// eraseModeLabel renders the EraseMode for the audit row.
func eraseModeLabel(mode iface.EraseMode) string {
	if mode == iface.EraseAnonymize {
		return "anonymize"
	}
	return "hard_delete"
}

// parseEraseMode is the inverse of eraseModeLabel: "anonymize" is
// EraseAnonymize, anything else EraseHardDelete.
func parseEraseMode(label string) iface.EraseMode {
	if label == "anonymize" {
		return iface.EraseAnonymize
	}
	return iface.EraseHardDelete
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
	var totalRows, pendingCount int
	for _, p := range r.Progress {
		res := r.Purged[p.Key]
		perSubject[p.Key] = map[string]any{
			"subject":        p.Subject,
			"outcome":        p.Outcome,
			"rowsDeleted":    res.RowsDeleted,
			"rowsAnonymized": res.RowsAnonymized,
			"collections":    res.Collections,
			"pending":        res.Pending,
		}
		totalRows += res.RowsDeleted + res.RowsAnonymized
		if p.Outcome == models.ProducerOutcomePending {
			pendingCount++
		}
	}
	out["perSubject"] = perSubject
	out["totalRows"] = totalRows
	out["errorCount"] = len(r.Errors)
	out["pendingCount"] = pendingCount
	out["complete"] = r.Complete()
	return out
}
