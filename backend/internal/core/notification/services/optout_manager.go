package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/orkestra/backend/internal/core/notification/models"
	"github.com/orkestra/backend/internal/core/notification/repository"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/module"
)

const (
	auditActionOptoutLifted   = "notification.optout.lifted"
	auditActionOptoutRecorded = "notification.optout.recorded"
	auditResourceOptout       = "marketing_optout"
)

// optoutManager is the audited lookup / lift / record of the durable
// marketing opt-out, for a consumer that collects consent itself.
//
// It has no suppression-list dependency by construction: a lift removes the
// opt-out and nothing else, so an address that bounced or complained stays
// suppressed. Record never fires the MarketingUnsubscribeSink either: the
// consumer that records an opt-out already knows about it.
type optoutManager struct {
	optouts  repository.MarketingOptoutRepository
	tokens   repository.UnsubscribeRepository
	services *module.ServiceRegistry
	logger   *slog.Logger
	now      func() time.Time
}

// NewOptoutManager builds the manager. The audit sink is resolved from
// services on every emission, never at construction: compliance registers it
// inside its own Init, after this module's (same reasoning as crm's
// LazyAuditEmitter). A nil registry or a missing sink makes auditing a no-op.
func NewOptoutManager(optouts repository.MarketingOptoutRepository, tokens repository.UnsubscribeRepository, services *module.ServiceRegistry, logger *slog.Logger) iface.MarketingOptoutManager {
	if logger == nil {
		logger = slog.Default()
	}
	return &optoutManager{optouts: optouts, tokens: tokens, services: services, logger: logger, now: time.Now}
}

func (m *optoutManager) LookupMarketingOptout(ctx context.Context, address string) (*iface.MarketingOptoutInfo, error) {
	doc, err := m.optouts.Get(ctx, repository.NormalizeOptoutAddress(address))
	if err != nil {
		return nil, fmt.Errorf("notification: reading the marketing opt-out: %w", err)
	}
	if doc == nil {
		return nil, nil
	}
	info := &iface.MarketingOptoutInfo{At: doc.At, TenantID: doc.TenantID, SourceContext: doc.SourceContext}
	if info.SourceContext == "" && doc.SourceTokenUUID != "" && m.tokens != nil {
		// Older opt-outs predate the copied context; the token may still exist.
		tok, terr := m.tokens.GetByUUID(ctx, doc.SourceTokenUUID)
		switch {
		case terr == nil:
			info.SourceContext = tok.Context
		case !errors.Is(terr, repository.ErrNotFound):
			// Attribution is best effort: the opt-out itself is the answer.
			m.logger.Warn("notification: opt-out source token not readable",
				slog.String("tokenUuid", doc.SourceTokenUUID), slog.String("error", scrubAddress(terr.Error(), address)))
		}
	}
	return info, nil
}

func (m *optoutManager) LiftMarketingOptout(ctx context.Context, address string, expectAt time.Time, ev iface.OptoutEvidence) (bool, error) {
	addr := repository.NormalizeOptoutAddress(address)
	if addr == "" {
		return false, nil
	}
	lifted, err := m.optouts.DeleteIfAt(ctx, addr, expectAt)
	if err != nil {
		return false, fmt.Errorf("notification: lifting the marketing opt-out: %w", err)
	}
	if lifted {
		m.audit(ctx, auditActionOptoutLifted, addr, ev)
	}
	return lifted, nil
}

func (m *optoutManager) RecordMarketingOptout(ctx context.Context, address string, ev iface.OptoutEvidence) error {
	addr := repository.NormalizeOptoutAddress(address)
	if addr == "" {
		return errors.New("notification: recording a marketing opt-out needs an address")
	}
	if err := m.optouts.Upsert(ctx, models.MarketingOptoutDoc{
		Address:  addr,
		Category: "",
		At:       m.now(),
		TenantID: ev.TenantID,
	}); err != nil {
		return fmt.Errorf("notification: recording the marketing opt-out: %w", err)
	}
	m.audit(ctx, auditActionOptoutRecorded, addr, ev)
	return nil
}

// audit emits to whatever sink is registered right now; the address is in the
// event metadata only, never in a log line.
func (m *optoutManager) audit(ctx context.Context, action, address string, ev iface.OptoutEvidence) {
	if m.services == nil {
		return
	}
	sink, ok := module.GetTyped[iface.AuditSink](m.services, module.ServiceAuditSink)
	if !ok || sink == nil {
		return
	}
	sink.Emit(ctx, iface.AuditEvent{
		TenantID:     ev.TenantID,
		ActorType:    "system",
		Action:       action,
		ResourceType: auditResourceOptout,
		Outcome:      "success",
		Metadata:     map[string]any{"source": ev.Source, "reference": ev.Reference, "address": address},
	})
}
