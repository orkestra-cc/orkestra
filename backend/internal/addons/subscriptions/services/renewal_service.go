package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra-cc/orkestra-sdk/iface"
	"github.com/orkestra-cc/orkestra-sdk/module"
	"github.com/orkestra/backend/internal/addons/subscriptions/models"
	"github.com/orkestra/backend/internal/addons/subscriptions/repository"
)

// DefaultVATRate is applied to every invoice subtotal in v1. Configurable
// later per tenant (e.g. B2B EU customers with valid VAT reverse-charge).
const DefaultVATRate = 0.22 // Italian IVA

// RenewalService owns the "a subscription is due, what happens next?" flow.
// It creates an invoice, asks the PaymentProvider to charge it, and
// transitions subscription state based on the outcome.
//
// The PaymentProvider is resolved lazily per call so the subscriptions
// module can initialize before payments without a cyclic dependency.
//
// Tenant ownership: every subscription resolves its billing identity through
// TenantProvider. Personal clients ride on their personal Tenant aggregate
// (created lazily by EnsureTenantForUser); admin-attached business clients
// ride on the business tenant.
type RenewalService struct {
	subs        repository.SubscriptionRepository
	services    repository.ServiceRepository
	invoices    repository.InvoiceRepository
	activity    *ActivityService
	entitlement *EntitlementSyncer
	tenants     iface.TenantProvider     // resolves tenant billing details + Stripe customer id
	notifier    iface.NotificationSender // may be nil
	registry    *module.ServiceRegistry
	logger      *slog.Logger
}

func NewRenewalService(
	subs repository.SubscriptionRepository,
	services repository.ServiceRepository,
	invoices repository.InvoiceRepository,
	activity *ActivityService,
	entitlement *EntitlementSyncer,
	tenants iface.TenantProvider,
	notifier iface.NotificationSender,
	registry *module.ServiceRegistry,
	logger *slog.Logger,
) *RenewalService {
	return &RenewalService{
		subs:        subs,
		services:    services,
		invoices:    invoices,
		activity:    activity,
		entitlement: entitlement,
		tenants:     tenants,
		notifier:    notifier,
		registry:    registry,
		logger:      logger,
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
	tenant, err := s.loadTenant(ctx, sub)
	if err != nil {
		return outcomeFailed, fmt.Errorf("load tenant: %w", err)
	}

	invoice, err := s.createInvoice(ctx, sub, svc, tier)
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
		s.sendNotification(ctx, tenant, "subscriptions.manual_payment", invoice, nil)
		// advance period anyway so we don't loop on the same cycle
		s.advancePeriod(ctx, sub, tier.Cycle)
		return outcomeManual, nil
	}

	return s.chargeInvoice(ctx, provider, sub, tenant, invoice, tier.Cycle)
}

func (s *RenewalService) createInvoice(ctx context.Context, sub *models.Subscription, svc *models.Service, tier *models.PricingTier) (*models.SubscriptionInvoice, error) {
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
		TenantUUID:       sub.TenantUUID,
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

func (s *RenewalService) chargeInvoice(ctx context.Context, provider iface.PaymentProvider, sub *models.Subscription, tenant *iface.Tenant, invoice *models.SubscriptionInvoice, cycle models.BillingCycle) (processOutcome, error) {
	stripeCustomerID := tenant.StripeCustomerID
	if stripeCustomerID == "" {
		// First charge for this tenant — create the Stripe customer on the fly.
		ref, err := provider.CreateCustomer(ctx, iface.CustomerInput{
			TenantUUID: sub.TenantUUID,
			Email:      tenant.Email,
			Name:       tenantBillingName(tenant),
			VATNumber:  tenant.VATNumber,
			Country:    tenant.Country,
		})
		if err != nil {
			return s.handleChargeFailure(ctx, sub, tenant, invoice, "", "create_customer_failed", err.Error())
		}
		stripeCustomerID = ref.ID
		tenant.StripeCustomerID = stripeCustomerID
		if err := s.persistStripeCustomerID(ctx, sub.TenantUUID, stripeCustomerID); err != nil {
			s.logger.Warn("renewal: persist stripe customer id failed",
				slog.String("tenantUUID", sub.TenantUUID),
				slog.String("error", err.Error()),
			)
		}
	}

	charge := iface.SubscriptionCharge{
		SubscriptionUUID: sub.UUID,
		InvoiceUUID:      invoice.UUID,
		TenantUUID:       sub.TenantUUID,
		Customer:         iface.CustomerRef{Provider: provider.Name(), ID: stripeCustomerID},
		AmountCents:      invoice.TotalCents,
		Currency:         invoice.Currency,
		Description:      fmt.Sprintf("%s — %s", tenantBillingName(tenant), invoice.Number),
		Metadata: map[string]string{
			"subscriptionUUID": sub.UUID,
			"invoiceUUID":      invoice.UUID,
			"tenantUUID":       sub.TenantUUID,
		},
	}
	result, err := provider.ChargeSubscription(ctx, charge)
	if err != nil {
		return s.handleChargeFailure(ctx, sub, tenant, invoice, "", "provider_error", err.Error())
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
		// Refresh entitlements: the grant's ExpiresAt is bumped to the new
		// period end so a tenant renewing on time never loses access, and a
		// tenant recovering from past_due is re-entitled. Idempotent for
		// within-period duplicate charges.
		s.entitlement.OnActivate(ctx, sub)
		s.sendNotification(ctx, tenant, "subscriptions.renewal.ok", invoice, nil)
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
		return s.handleChargeFailure(ctx, sub, tenant, invoice, result.ProviderTxID, result.FailureCode, result.FailureMsg)
	}
}

func (s *RenewalService) handleChargeFailure(ctx context.Context, sub *models.Subscription, tenant *iface.Tenant, invoice *models.SubscriptionInvoice, providerTxID, code, msg string) (processOutcome, error) {
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
	suspended := false
	if sub.FailedChargeCount >= models.MaxChargeFailures {
		sub.Status = models.SubSuspended
		suspended = true
	} else {
		sub.Status = models.SubPastDue
	}
	// Push NextBillingAt out by one day so we retry tomorrow, not every tick.
	sub.NextBillingAt = now.AddDate(0, 0, 1)
	if err := s.subs.Update(ctx, sub); err != nil {
		return outcomeFailed, err
	}
	// Past_due stays entitled during the dunning window (max_failures
	// attempts). Suspension is terminal for service consumption — revoke.
	if suspended {
		s.entitlement.OnDeactivate(ctx, sub)
	}
	s.activity.Log(ctx, sub, "system", models.ActivityChargeFailed, "Charge failed", map[string]any{
		"invoiceUUID":  invoice.UUID,
		"providerTxID": providerTxID,
		"code":         code,
		"message":      msg,
		"attempts":     sub.FailedChargeCount,
	})
	s.sendNotification(ctx, tenant, "subscriptions.renewal.failed", invoice, map[string]any{
		"failureCode": code,
		"failureMsg":  msg,
	})
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

// loadTenant resolves a tenant DTO carrying billing details for the
// subscription's tenant.
func (s *RenewalService) loadTenant(ctx context.Context, sub *models.Subscription) (*iface.Tenant, error) {
	if sub == nil || sub.TenantUUID == "" {
		return nil, errors.New("subscription has no tenant")
	}
	if s.tenants == nil {
		return nil, errors.New("tenant provider not configured")
	}
	t, err := s.tenants.GetTenant(ctx, sub.TenantUUID)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, errors.New("tenant not found")
	}
	return t, nil
}

// persistStripeCustomerID writes the lazy Stripe customer-id back onto the
// tenant. Best-effort by design — failures are surfaced to the caller for
// logging only; the customer already exists on Stripe and the renewal job's
// metadata-keyed lookup will reuse it next cycle.
func (s *RenewalService) persistStripeCustomerID(ctx context.Context, tenantUUID, stripeCustomerID string) error {
	if s.tenants == nil {
		return nil
	}
	return s.tenants.SetTenantStripeCustomerID(ctx, tenantUUID, stripeCustomerID)
}

// tenantBillingName picks the most descriptive label for the tenant when
// surfaced on Stripe descriptions and notifications. Falls back through
// LegalName → Name → Slug → empty string in turn.
func tenantBillingName(t *iface.Tenant) string {
	if t == nil {
		return ""
	}
	if t.LegalName != "" {
		return t.LegalName
	}
	if t.Name != "" {
		return t.Name
	}
	return t.Slug
}

func (s *RenewalService) sendNotification(ctx context.Context, tenant *iface.Tenant, category string, invoice *models.SubscriptionInvoice, extra map[string]any) {
	if s.notifier == nil || !s.notifier.IsConfigured(ctx) {
		return
	}
	if tenant == nil || tenant.Email == "" {
		return
	}
	name := tenantBillingName(tenant)
	data := map[string]any{
		"clientName":    name,
		"tenantName":    name,
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
		Recipients: []iface.Recipient{{Address: tenant.Email, Name: name}},
		Data:       data,
	})
	if err != nil {
		s.logger.Warn("renewal: notification send failed",
			slog.String("category", category),
			slog.String("tenantUUID", tenant.UUID),
			slog.String("error", err.Error()),
		)
	}
}
