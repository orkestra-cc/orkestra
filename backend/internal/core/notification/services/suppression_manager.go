package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/orkestra/backend/internal/core/notification/models"
	"github.com/orkestra/backend/internal/core/notification/repository"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/module"
)

const (
	auditActionSuppressionAdded  = "notification.suppression.added"
	auditActionSuppressionLifted = "notification.suppression.lifted"
	auditResourceSuppression     = "email_suppression"
)

// ErrSuppressionBatchTooLarge: one call named more than
// iface.MaxSuppressionBatch addresses.
var ErrSuppressionBatchTooLarge = fmt.Errorf("notification: more than %d addresses in one suppression call", iface.MaxSuppressionBatch)

// ErrSuppressionReasonInvalid: the reason is not one of iface.SuppressionReason*.
var ErrSuppressionReasonInvalid = errors.New("notification: unknown suppression reason")

// ErrSuppressionOperationFailed wraps a repository failure of the manager. The
// message never quotes the store's error, which can carry an address.
var ErrSuppressionOperationFailed = errors.New("notification: suppression list operation failed")

// suppressionManager is the audited seam over the email suppression list
// (iface.EmailSuppressionManager). One audit event per changed address, with
// the address in the metadata only, never in a log line.
type suppressionManager struct {
	repo     repository.SuppressionRepository
	services *module.ServiceRegistry
	logger   *slog.Logger
	now      func() time.Time
}

// NewSuppressionManager builds the manager. The audit sink is resolved on
// every emission (compliance registers it after this module's Init); a nil
// registry or a missing sink makes auditing a no-op.
func NewSuppressionManager(repo repository.SuppressionRepository, services *module.ServiceRegistry, logger *slog.Logger) iface.EmailSuppressionManager {
	if logger == nil {
		logger = slog.Default()
	}
	return &suppressionManager{repo: repo, services: services, logger: logger, now: time.Now}
}

func (m *suppressionManager) LookupSuppressions(ctx context.Context, addresses []string) (map[string]iface.EmailSuppression, error) {
	if len(addresses) > iface.MaxSuppressionBatch {
		return nil, ErrSuppressionBatchTooLarge
	}
	docs, err := m.repo.GetMany(ctx, addresses)
	if err != nil {
		return nil, m.fail("reading the suppression list", err)
	}
	out := make(map[string]iface.EmailSuppression, len(docs))
	for addr, d := range docs {
		out[addr] = iface.EmailSuppression{
			Address: d.Address, Reason: d.Reason, At: d.CreatedAt,
			Source: d.Source, ActorUUID: d.ActorUUID, TenantID: d.TenantID,
		}
	}
	return out, nil
}

func (m *suppressionManager) SuppressAddresses(ctx context.Context, addresses []string, ev iface.SuppressionEvidence) ([]string, error) {
	if len(addresses) > iface.MaxSuppressionBatch {
		return nil, ErrSuppressionBatchTooLarge
	}
	ev = withContextAttribution(ctx, ev)
	switch ev.Reason {
	case "":
		ev.Reason = iface.SuppressionReasonManual
	case iface.SuppressionReasonHardBounce, iface.SuppressionReasonManual:
	default:
		return nil, ErrSuppressionReasonInvalid
	}
	added, err := m.repo.InsertIfAbsent(ctx, addresses, models.SuppressionDoc{
		Reason: ev.Reason, CreatedAt: m.now(), Source: ev.Source, ActorUUID: ev.ActorUUID, TenantID: ev.TenantID,
	})
	if err != nil {
		return nil, m.fail("adding to the suppression list", err)
	}
	for _, a := range added {
		m.audit(ctx, auditActionSuppressionAdded, a, ev)
	}
	return added, nil
}

func (m *suppressionManager) LiftSuppressions(ctx context.Context, addresses []string, ev iface.SuppressionEvidence) ([]string, error) {
	if len(addresses) > iface.MaxSuppressionBatch {
		return nil, ErrSuppressionBatchTooLarge
	}
	ev = withContextAttribution(ctx, ev)
	ev.Reason = ""
	//tenantscope:allow platform: the suppression list is address-keyed and platform-wide by design (tenantId is attribution only)
	lifted, err := m.repo.DeleteMany(ctx, addresses)
	// Rows deleted before a failure are gone: audit them either way.
	for _, a := range lifted {
		m.audit(ctx, auditActionSuppressionLifted, a, ev)
	}
	if err != nil {
		return lifted, m.fail("lifting from the suppression list", err)
	}
	return lifted, nil
}

func (m *suppressionManager) ListSuppressions(ctx context.Context, q iface.SuppressionListQuery) ([]iface.EmailSuppression, error) {
	limit := q.Limit
	if limit <= 0 || limit > iface.MaxSuppressionBatch {
		limit = iface.MaxSuppressionBatch
	}
	switch q.Reason {
	case "", iface.SuppressionReasonHardBounce, iface.SuppressionReasonManual:
	default:
		return nil, ErrSuppressionReasonInvalid
	}
	docs, err := m.repo.List(ctx, q.AfterAddress, limit, q.Contains, q.Reason)
	if err != nil {
		return nil, m.fail("listing the suppression list", err)
	}
	out := make([]iface.EmailSuppression, 0, len(docs))
	for _, d := range docs {
		out = append(out, iface.EmailSuppression{
			Address: d.Address, Reason: d.Reason, At: d.CreatedAt,
			Source: d.Source, ActorUUID: d.ActorUUID, TenantID: d.TenantID,
		})
	}
	return out, nil
}

func withContextAttribution(ctx context.Context, ev iface.SuppressionEvidence) iface.SuppressionEvidence {
	if ev.ActorUUID == "" {
		ev.ActorUUID, _ = ctxauth.GetUserUUID(ctx)
	}
	if ev.TenantID == "" {
		ev.TenantID, _ = ctxauth.GetTenantID(ctx)
	}
	return ev
}

func (m *suppressionManager) fail(what string, err error) error {
	// The store's text may quote an address; the log line keeps only the
	// operation, the error type stays for diagnosis.
	m.logger.Warn("notification: suppression list operation failed", slog.String("op", what), slog.String("errType", fmt.Sprintf("%T", err)))
	return fmt.Errorf("%w: %s", ErrSuppressionOperationFailed, what)
}

func (m *suppressionManager) audit(ctx context.Context, action, address string, ev iface.SuppressionEvidence) {
	if m.services == nil {
		return
	}
	sink, ok := module.GetTyped[iface.AuditSink](m.services, module.ServiceAuditSink)
	if !ok || sink == nil {
		return
	}
	actorType := "system"
	if ev.ActorUUID != "" {
		actorType = "user"
	}
	meta := map[string]any{"address": address, "source": ev.Source}
	if ev.Reason != "" {
		meta["reason"] = ev.Reason
	}
	sink.Emit(ctx, iface.AuditEvent{
		TenantID:     ev.TenantID,
		ActorUserID:  ev.ActorUUID,
		ActorType:    actorType,
		Action:       action,
		ResourceType: auditResourceSuppression,
		Outcome:      "success",
		Metadata:     meta,
	})
}
