package services

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"
	"github.com/orkestra-cc/orkestra-sdk/iface"
	"github.com/orkestra-cc/orkestra-sdk/module"
	"github.com/orkestra/backend/internal/addons/payments/models"
	"github.com/orkestra/backend/internal/addons/payments/repository"
)

// Dispatcher is the webhook pipeline: persist → dedupe → reconcile.
// The reconciler is resolved lazily from the ServiceRegistry on each call
// so the subscriptions module can initialize in any order relative to this
// one (see plan doc § "Cycle-free wiring").
type Dispatcher struct {
	webhookRepo repository.WebhookEventRepository
	registry    *module.ServiceRegistry
	logger      *slog.Logger
}

func NewDispatcher(webhookRepo repository.WebhookEventRepository, registry *module.ServiceRegistry, logger *slog.Logger) *Dispatcher {
	return &Dispatcher{webhookRepo: webhookRepo, registry: registry, logger: logger}
}

// ErrAlreadyProcessed is returned when the webhook event was seen before.
// Callers should respond 200 OK so the provider stops retrying.
var ErrAlreadyProcessed = errors.New("payments: webhook already processed")

// Handle records the verified event, dedupes it via the unique index on
// (provider, providerEventID), and forwards it to the subscription
// reconciler. All steps are idempotent.
func (d *Dispatcher) Handle(ctx context.Context, evt iface.WebhookEvent) error {
	rec := &models.WebhookEvent{
		UUID:             uuid.NewString(),
		Provider:         models.ProviderName(evt.Provider),
		ProviderEventID:  evt.ProviderEventID,
		Type:             evt.Type,
		Normalized:       evt.Normalized,
		SubscriptionUUID: evt.SubscriptionUUID,
		InvoiceUUID:      evt.InvoiceUUID,
		RawPayload:       evt.RawPayload,
	}
	if err := d.webhookRepo.Insert(ctx, rec); err != nil {
		if errors.Is(err, repository.ErrDuplicateWebhookEvent) {
			d.logger.Info("payments: duplicate webhook event ignored",
				slog.String("provider", evt.Provider),
				slog.String("eventID", evt.ProviderEventID),
			)
			return ErrAlreadyProcessed
		}
		return err
	}

	reconciler, ok := module.GetTyped[iface.SubscriptionReconciler](d.registry, module.ServiceSubscriptionReconciler)
	if !ok {
		d.logger.Warn("payments: no subscription reconciler registered — event stored but not acted on",
			slog.String("eventID", evt.ProviderEventID),
			slog.String("normalized", evt.Normalized),
		)
		_ = d.webhookRepo.MarkProcessed(ctx, rec.UUID, "no reconciler registered")
		return nil
	}

	var processErr error
	switch evt.Normalized {
	case "charge.succeeded":
		if evt.InvoiceUUID != "" {
			processErr = reconciler.MarkInvoicePaid(ctx, evt.InvoiceUUID, evt.ProviderTxID, evt.OccurredAt)
		}
	case "charge.failed":
		if evt.InvoiceUUID != "" {
			processErr = reconciler.MarkInvoiceFailed(ctx, evt.InvoiceUUID, evt.FailureCode, evt.FailureMsg)
		}
	case "charge.refunded":
		if evt.InvoiceUUID != "" {
			processErr = reconciler.RecordRefund(ctx, evt.InvoiceUUID, evt.ProviderRefundID, evt.AmountCents, "")
		}
	default:
		// Unmapped event: stored for audit only.
	}

	var processErrMsg string
	if processErr != nil {
		processErrMsg = processErr.Error()
		d.logger.Error("payments: webhook reconcile failed",
			slog.String("provider", evt.Provider),
			slog.String("eventID", evt.ProviderEventID),
			slog.String("normalized", evt.Normalized),
			slog.String("error", processErrMsg),
		)
	}
	if err := d.webhookRepo.MarkProcessed(ctx, rec.UUID, processErrMsg); err != nil {
		d.logger.Warn("payments: mark processed failed",
			slog.String("uuid", rec.UUID),
			slog.String("error", err.Error()),
		)
	}
	return processErr
}
