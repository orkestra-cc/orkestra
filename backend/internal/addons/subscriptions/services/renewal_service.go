package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra/backend/internal/addons/subscriptions/models"
	"github.com/orkestra/backend/internal/addons/subscriptions/repository"
	"github.com/orkestra/backend/internal/shared/iface"
	"github.com/orkestra/backend/internal/shared/module"
)

// DefaultVATRate is applied to every invoice subtotal in v1. Configurable
// later per client (e.g. B2B EU customers with valid VAT reverse-charge).
const DefaultVATRate = 0.22 // Italian IVA

// RenewalService owns the "a subscription is due, what happens next?" flow.
// It creates an invoice, asks the PaymentProvider to charge it, and
// transitions subscription state based on the outcome.
//
// The PaymentProvider is resolved lazily per call so the subscriptions
// module can initialize before payments without a cyclic dependency.
type RenewalService struct {
	subs     repository.SubscriptionRepository
	clients  repository.ClientRepository
	services repository.ServiceRepository
	invoices repository.InvoiceRepository
	activity *ActivityService
	notifier iface.NotificationSender // may be nil
	registry *module.ServiceRegistry
	logger   *slog.Logger
}

func NewRenewalService(
	subs repository.SubscriptionRepository,
	clients repository.ClientRepository,
	services repository.ServiceRepository,
	invoices repository.InvoiceRepository,
	activity *ActivityService,
	notifier iface.NotificationSender,
	registry *module.ServiceRegistry,
	logger *slog.Logger,
) *RenewalService {
	return &RenewalService{
		subs:     subs,
		clients:  clients,
		services: services,
		invoices: invoices,
		activity: activity,
		notifier: notifier,
		registry: registry,
		logger:   logger,
	}
}

// ProcessDue is what the renewal job calls every tick: find due subs and
// try to charge each. Errors on individual subscriptions are logged but
// do not abort the batch.
func (s *RenewalService) ProcessDue(ctx context.Context, now time.Time) (processed, charged, failed int) {
	due, err := s.subs.FindDue(ctx, now)
	if err != nil {
		s.logger.Error("renewal: find due failed", slog.String("error", err.Error()))
		return 0, 0, 0
	}
	if len(due) == 0 {
		return 0, 0, 0
	}
	s.logger.Info("renewal: processing due subscriptions", slog.Int("count", len(due)))

	for i := range due {
		sub := &due[i]
		processed++
		outcome, err := s.ProcessOne(ctx, sub)
		if err != nil {
			s.logger.Error("renewal: subscription failed",
				slog.String("subscriptionUUID", sub.UUID),
				slog.String("error", err.Error()),
			)
			failed++
			continue
		}
		switch outcome {
		case outcomeCharged:
			charged++
		case outcomeFailed:
			failed++
		}
	}
	return
}

type processOutcome int

const (
	outcomeCharged processOutcome = iota
	outcomeFailed
	outcomeManual
	outcomeCancelled
)

// ProcessOne handles a single due subscription: check if it's actually
// still active, generate an invoice, dispatch to the payment provider,
// update state.
func (s *RenewalService) ProcessOne(ctx context.Context, sub *models.Subscription) (processOutcome, error) {
	// Scheduled cancellation — terminate instead of billing.
	if sub.CancelAtPeriodEnd && !sub.CurrentPeriodEnd.After(time.Now().UTC()) {
		now := time.Now().UTC()
		sub.Status = models.SubCancelled
		sub.CancelledAt = &now
		sub.EndsAt = &now
		if err := s.subs.Update(ctx, sub); err != nil {
			return outcomeFailed, err
		}
		s.activity.Log(ctx, sub, "system", models.ActivityCancelled, "Cancelled at period end", nil)
		return outcomeCancelled, nil
	}

	svc, err := s.services.GetByUUID(ctx, sub.ServiceUUID)
	if err != nil {
		return outcomeFailed, fmt.Errorf("load service: %w", err)
	}
	tier := svc.FindTier(sub.TierCode)
	if tier == nil {
		return outcomeFailed, ErrSubscriptionTierNotFound
	}
	client, err := s.clients.GetByUUID(ctx, sub.ClientUUID)
	if err != nil {
		return outcomeFailed, fmt.Errorf("load client: %w", err)
	}

	invoice, err := s.createInvoice(ctx, sub, svc, tier, client)
	if err != nil {
		return outcomeFailed, fmt.Errorf("create invoice: %w", err)
	}
	s.activity.Log(ctx, sub, "system", models.ActivityInvoiceIssued, "Invoice issued", map[string]any{
		"invoiceUUID": invoice.UUID,
		"number":      invoice.Number,
		"totalCents":  invoice.TotalCents,
	})

	provider, ok := s.paymentProvider()
	if !ok {
		invoice.Status = models.InvoiceAwaitingManualPayment
		if err := s.invoices.Update(ctx, invoice); err != nil {
			return outcomeFailed, err
		}
		s.activity.Log(ctx, sub, "system", models.ActivityManualPayment,
			"Payment provider not configured — invoice requires manual reconciliation", nil)
		s.sendNotification(ctx, client, "subscriptions.manual_payment", invoice, nil)
		// advance period anyway so we don't loop on the same cycle
		s.advancePeriod(ctx, sub, tier.Cycle)
		return outcomeManual, nil
	}

	return s.chargeInvoice(ctx, provider, sub, client, invoice, tier.Cycle)
}

func (s *RenewalService) createInvoice(ctx context.Context, sub *models.Subscription, svc *models.Service, tier *models.PricingTier, client *models.Client) (*models.SubscriptionInvoice, error) {
	now := time.Now().UTC()
	number, err := s.nextInvoiceNumber(ctx, now.Year())
	if err != nil {
		return nil, err
	}
	subtotal := tier.AmountCents
	vat := int64(float64(subtotal) * DefaultVATRate)
	total := subtotal + vat

	inv := &models.SubscriptionInvoice{
		UUID:             uuid.NewString(),
		Number:           number,
		SubscriptionUUID: sub.UUID,
		ClientUUID:       client.UUID,
		ServiceUUID:      svc.UUID,
		PeriodStart:      sub.CurrentPeriodStart,
		PeriodEnd:        sub.CurrentPeriodEnd,
		IssuedAt:         now,
		DueAt:            now.AddDate(0, 0, 7),
		SubtotalCents:    subtotal,
		VATCents:         vat,
		TotalCents:       total,
		Currency:         tier.Currency,
		Status:           models.InvoicePending,
	}
	if err := s.invoices.Create(ctx, inv); err != nil {
		return nil, err
	}
	return inv, nil
}

func (s *RenewalService) chargeInvoice(ctx context.Context, provider iface.PaymentProvider, sub *models.Subscription, client *models.Client, invoice *models.SubscriptionInvoice, cycle models.BillingCycle) (processOutcome, error) {
	if client.StripeCustomerID == "" {
		// First charge for this client — create the customer on the fly.
		ref, err := provider.CreateCustomer(ctx, iface.CustomerInput{
			ClientUUID: client.UUID,
			Email:      client.Email,
			Name:       client.LegalName,
			VATNumber:  client.VATNumber,
			Country:    client.BillingAddr.Country,
		})
		if err != nil {
			return s.handleChargeFailure(ctx, sub, invoice, "", "create_customer_failed", err.Error())
		}
		client.StripeCustomerID = ref.ID
		if err := s.clients.SetStripeCustomerID(ctx, client.UUID, ref.ID); err != nil {
			s.logger.Warn("renewal: persist stripe customer id failed",
				slog.String("clientUUID", client.UUID),
				slog.String("error", err.Error()),
			)
		}
	}

	charge := iface.SubscriptionCharge{
		SubscriptionUUID: sub.UUID,
		InvoiceUUID:      invoice.UUID,
		Customer:         iface.CustomerRef{Provider: provider.Name(), ID: client.StripeCustomerID},
		AmountCents:      invoice.TotalCents,
		Currency:         invoice.Currency,
		Description:      fmt.Sprintf("%s — %s", client.DisplayName, invoice.Number),
		Metadata: map[string]string{
			"subscriptionUUID": sub.UUID,
			"invoiceUUID":      invoice.UUID,
		},
	}
	result, err := provider.ChargeSubscription(ctx, charge)
	if err != nil {
		return s.handleChargeFailure(ctx, sub, invoice, "", "provider_error", err.Error())
	}

	switch result.Status {
	case "succeeded":
		now := time.Now().UTC()
		invoice.Status = models.InvoicePaid
		invoice.StripePaymentIntentID = result.ProviderTxID
		invoice.PaidAt = &now
		if err := s.invoices.Update(ctx, invoice); err != nil {
			return outcomeFailed, err
		}
		sub.Status = models.SubActive
		sub.FailedChargeCount = 0
		s.advancePeriod(ctx, sub, cycle)
		s.activity.Log(ctx, sub, "system", models.ActivityCharged, "Charge succeeded", map[string]any{
			"invoiceUUID":  invoice.UUID,
			"providerTxID": result.ProviderTxID,
			"amountCents":  invoice.TotalCents,
		})
		s.sendNotification(ctx, client, "subscriptions.renewal.ok", invoice, nil)
		return outcomeCharged, nil
	case "requires_action":
		// 3DS or similar — invoice stays pending; webhook will resolve.
		invoice.StripePaymentIntentID = result.ProviderTxID
		if err := s.invoices.Update(ctx, invoice); err != nil {
			return outcomeFailed, err
		}
		s.activity.Log(ctx, sub, "system", models.ActivityInvoiceIssued, "Charge requires additional action (awaiting webhook)", map[string]any{
			"invoiceUUID":  invoice.UUID,
			"providerTxID": result.ProviderTxID,
		})
		s.advancePeriod(ctx, sub, cycle)
		return outcomeCharged, nil
	default:
		return s.handleChargeFailure(ctx, sub, invoice, result.ProviderTxID, result.FailureCode, result.FailureMsg)
	}
}

func (s *RenewalService) handleChargeFailure(ctx context.Context, sub *models.Subscription, invoice *models.SubscriptionInvoice, providerTxID, code, msg string) (processOutcome, error) {
	now := time.Now().UTC()
	invoice.Status = models.InvoiceFailed
	invoice.StripePaymentIntentID = providerTxID
	invoice.FailedAt = &now
	invoice.FailureCode = code
	invoice.FailureMsg = msg
	if err := s.invoices.Update(ctx, invoice); err != nil {
		return outcomeFailed, err
	}
	sub.FailedChargeCount++
	if sub.FailedChargeCount >= models.MaxChargeFailures {
		sub.Status = models.SubSuspended
	} else {
		sub.Status = models.SubPastDue
	}
	// Push NextBillingAt out by one day so we retry tomorrow, not every tick.
	sub.NextBillingAt = now.AddDate(0, 0, 1)
	if err := s.subs.Update(ctx, sub); err != nil {
		return outcomeFailed, err
	}
	s.activity.Log(ctx, sub, "system", models.ActivityChargeFailed, "Charge failed", map[string]any{
		"invoiceUUID":  invoice.UUID,
		"providerTxID": providerTxID,
		"code":         code,
		"message":      msg,
		"attempts":     sub.FailedChargeCount,
	})
	// Load client for notification
	if client, err := s.clients.GetByUUID(ctx, sub.ClientUUID); err == nil {
		s.sendNotification(ctx, client, "subscriptions.renewal.failed", invoice, map[string]any{
			"failureCode": code,
			"failureMsg":  msg,
		})
	}
	return outcomeFailed, nil
}

func (s *RenewalService) advancePeriod(ctx context.Context, sub *models.Subscription, cycle models.BillingCycle) {
	sub.CurrentPeriodStart = sub.CurrentPeriodEnd
	sub.CurrentPeriodEnd = AdvancePeriod(sub.CurrentPeriodEnd, cycle)
	sub.NextBillingAt = sub.CurrentPeriodEnd
	if err := s.subs.Update(ctx, sub); err != nil {
		s.logger.Error("renewal: advance period failed",
			slog.String("subscriptionUUID", sub.UUID),
			slog.String("error", err.Error()),
		)
	}
}

// RetryNow forces a charge attempt for a single subscription (used by the
// admin "Retry charge" button). Bypasses the NextBillingAt check.
func (s *RenewalService) RetryNow(ctx context.Context, subscriptionUUID string) (*models.Subscription, error) {
	sub, err := s.subs.GetByUUID(ctx, subscriptionUUID)
	if err != nil {
		return nil, err
	}
	if sub.Status == models.SubCancelled || sub.Status == models.SubExpired {
		return nil, errors.New("subscription is terminal, cannot retry")
	}
	if _, err := s.ProcessOne(ctx, sub); err != nil {
		return nil, err
	}
	return s.subs.GetByUUID(ctx, subscriptionUUID)
}

func (s *RenewalService) nextInvoiceNumber(ctx context.Context, year int) (string, error) {
	count, err := s.invoices.CountInYear(ctx, year)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("SUB-%d-%06d", year, count+1), nil
}

// paymentProvider is resolved lazily on every call so the subscriptions
// module can be initialized before the payments module without either
// depending on the other. See plan doc § "Cycle-free wiring".
func (s *RenewalService) paymentProvider() (iface.PaymentProvider, bool) {
	if s.registry == nil {
		return nil, false
	}
	return module.GetTyped[iface.PaymentProvider](s.registry, module.ServicePaymentProvider)
}

func (s *RenewalService) sendNotification(ctx context.Context, client *models.Client, category string, invoice *models.SubscriptionInvoice, extra map[string]any) {
	if s.notifier == nil || !s.notifier.IsConfigured(ctx) {
		return
	}
	data := map[string]any{
		"clientName":    client.DisplayName,
		"invoiceNumber": invoice.Number,
		"totalCents":    invoice.TotalCents,
		"currency":      invoice.Currency,
		"dueAt":         invoice.DueAt,
	}
	for k, v := range extra {
		data[k] = v
	}
	_, err := s.notifier.SendTemplated(ctx, iface.TemplatedNotificationRequest{
		Channel:    "email",
		Type:       "transactional",
		Category:   category,
		TemplateID: category,
		Recipients: []iface.Recipient{{Address: client.Email, Name: client.DisplayName}},
		Data:       data,
	})
	if err != nil {
		s.logger.Warn("renewal: notification send failed",
			slog.String("category", category),
			slog.String("clientUUID", client.UUID),
			slog.String("error", err.Error()),
		)
	}
}
