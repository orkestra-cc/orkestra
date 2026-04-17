package services

import (
	"context"
	"log/slog"
	"time"

	"github.com/orkestra/backend/internal/addons/subscriptions/models"
	"github.com/orkestra/backend/internal/addons/subscriptions/repository"
)

// Reconciler implements iface.SubscriptionReconciler. The payments module's
// webhook dispatcher calls these methods to update invoice + subscription
// state when Stripe fires asynchronous events (post-3DS confirmation,
// out-of-band refund, etc).
//
// Writes here are idempotent — receiving the same webhook twice is a no-op
// after the first successful reconcile.
type Reconciler struct {
	invoices      repository.InvoiceRepository
	subscriptions repository.SubscriptionRepository
	activity      *ActivityService
	logger        *slog.Logger
}

func NewReconciler(
	invoices repository.InvoiceRepository,
	subscriptions repository.SubscriptionRepository,
	activity *ActivityService,
	logger *slog.Logger,
) *Reconciler {
	return &Reconciler{
		invoices:      invoices,
		subscriptions: subscriptions,
		activity:      activity,
		logger:        logger,
	}
}

func (r *Reconciler) MarkInvoicePaid(ctx context.Context, invoiceUUID, providerTxID string, paidAt time.Time) error {
	inv, err := r.invoices.GetByUUID(ctx, invoiceUUID)
	if err != nil {
		return err
	}
	if inv.Status == models.InvoicePaid {
		return nil // idempotent
	}
	inv.Status = models.InvoicePaid
	inv.StripePaymentIntentID = providerTxID
	paid := paidAt
	inv.PaidAt = &paid
	inv.FailureCode = ""
	inv.FailureMsg = ""
	if err := r.invoices.Update(ctx, inv); err != nil {
		return err
	}

	sub, err := r.subscriptions.GetByUUID(ctx, inv.SubscriptionUUID)
	if err != nil {
		return err
	}
	if sub.Status == models.SubPastDue {
		sub.Status = models.SubActive
		sub.FailedChargeCount = 0
		if err := r.subscriptions.Update(ctx, sub); err != nil {
			return err
		}
	}
	r.activity.Log(ctx, sub, "system", models.ActivityCharged, "Invoice paid (webhook)", map[string]any{
		"invoiceUUID":  invoiceUUID,
		"providerTxID": providerTxID,
	})
	return nil
}

func (r *Reconciler) MarkInvoiceFailed(ctx context.Context, invoiceUUID, failureCode, failureMsg string) error {
	inv, err := r.invoices.GetByUUID(ctx, invoiceUUID)
	if err != nil {
		return err
	}
	if inv.Status == models.InvoiceFailed || inv.Status == models.InvoicePaid {
		return nil
	}
	now := time.Now().UTC()
	inv.Status = models.InvoiceFailed
	inv.FailedAt = &now
	inv.FailureCode = failureCode
	inv.FailureMsg = failureMsg
	if err := r.invoices.Update(ctx, inv); err != nil {
		return err
	}

	sub, err := r.subscriptions.GetByUUID(ctx, inv.SubscriptionUUID)
	if err != nil {
		return err
	}
	sub.FailedChargeCount++
	if sub.FailedChargeCount >= models.MaxChargeFailures {
		sub.Status = models.SubSuspended
	} else {
		sub.Status = models.SubPastDue
	}
	if err := r.subscriptions.Update(ctx, sub); err != nil {
		return err
	}
	r.activity.Log(ctx, sub, "system", models.ActivityChargeFailed, "Charge failed (webhook)", map[string]any{
		"invoiceUUID": invoiceUUID,
		"code":        failureCode,
		"message":     failureMsg,
		"attempts":    sub.FailedChargeCount,
	})
	return nil
}

func (r *Reconciler) RecordRefund(ctx context.Context, invoiceUUID, providerRefundID string, amountCents int64, reason string) error {
	inv, err := r.invoices.GetByUUID(ctx, invoiceUUID)
	if err != nil {
		return err
	}
	if inv.Status == models.InvoiceRefunded {
		return nil
	}
	now := time.Now().UTC()
	inv.Status = models.InvoiceRefunded
	inv.StripeRefundID = providerRefundID
	inv.RefundedAt = &now
	if err := r.invoices.Update(ctx, inv); err != nil {
		return err
	}

	sub, err := r.subscriptions.GetByUUID(ctx, inv.SubscriptionUUID)
	if err != nil {
		return err
	}
	r.activity.Log(ctx, sub, "system", models.ActivityRefunded, "Refund recorded", map[string]any{
		"invoiceUUID":      invoiceUUID,
		"providerRefundID": providerRefundID,
		"amountCents":      amountCents,
		"reason":           reason,
	})
	return nil
}
