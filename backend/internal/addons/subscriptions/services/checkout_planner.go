package services

import (
	"context"
	"errors"
	"fmt"

	"github.com/orkestra/backend/internal/addons/subscriptions/models"
	"github.com/orkestra/backend/internal/addons/subscriptions/repository"
	"github.com/orkestra/backend/internal/shared/iface"
)

// CheckoutPlanner implements iface.SelfServiceCheckoutPlanner. It resolves a
// subscription UUID into the snapshot the payments client-surface handler
// needs to open a Stripe Checkout session: the owning tenant (for the
// ownership re-check that gates the route), the pending invoice (whose
// UUID is stamped into Stripe metadata so the existing webhook reconciler
// can match the resulting payment back to the right invoice without any
// new wiring), and the catalog tier price + label.
//
// The planner is read-only and idempotent — it never mutates state. It
// deliberately does NOT issue a fresh invoice when none is pending; that
// concern belongs to the renewal job (or a future explicit "trigger
// renewal" admin action). Returning iface.ErrCheckoutNoPendingInvoice
// keeps the contract small enough that a Tier-2 SPA can react with a
// helpful message without leaking renewal-job internals.
type CheckoutPlanner struct {
	subs     repository.SubscriptionRepository
	services repository.ServiceRepository
	invoices repository.InvoiceRepository
}

// NewCheckoutPlanner constructs the planner. Registered under
// module.ServiceSelfServiceCheckoutPlanner by SubscriptionsModule.Init.
func NewCheckoutPlanner(
	subs repository.SubscriptionRepository,
	services repository.ServiceRepository,
	invoices repository.InvoiceRepository,
) *CheckoutPlanner {
	return &CheckoutPlanner{subs: subs, services: services, invoices: invoices}
}

// PlanCheckoutSession looks up the subscription, its catalog service+tier,
// and its most recent pending invoice. Errors:
//   - ErrSubscriptionNotFound when the UUID is unknown
//   - iface.ErrCheckoutNoPendingInvoice when the subscription has no
//     invoice in `pending` status (handler returns 409)
//   - any underlying repository error otherwise
func (p *CheckoutPlanner) PlanCheckoutSession(ctx context.Context, subscriptionUUID string) (iface.CheckoutPlan, error) {
	if subscriptionUUID == "" {
		return iface.CheckoutPlan{}, errors.New("checkout planner: subscriptionUUID is required")
	}

	sub, err := p.subs.GetByUUID(ctx, subscriptionUUID)
	if err != nil {
		return iface.CheckoutPlan{}, err
	}

	svc, err := p.services.GetByUUID(ctx, sub.ServiceUUID)
	if err != nil {
		return iface.CheckoutPlan{}, fmt.Errorf("checkout planner: load service: %w", err)
	}
	tier := svc.FindTier(sub.TierCode)
	if tier == nil {
		return iface.CheckoutPlan{}, ErrSubscriptionTierNotFound
	}

	invoices, err := p.invoices.List(ctx, repository.InvoiceFilters{
		SubscriptionUUID: sub.UUID,
		Status:           models.InvoicePending,
	})
	if err != nil {
		return iface.CheckoutPlan{}, err
	}
	if len(invoices) == 0 {
		return iface.CheckoutPlan{}, iface.ErrCheckoutNoPendingInvoice
	}
	// invoiceRepository.List sorts by issuedAt desc — first row is newest.
	inv := invoices[0]

	return iface.CheckoutPlan{
		SubscriptionUUID: sub.UUID,
		TenantUUID:       sub.TenantUUID,
		ServiceUUID:      svc.UUID,
		ServiceName:      svc.Name,
		TierCode:         tier.Code,
		InvoiceUUID:      inv.UUID,
		InvoiceNumber:    inv.Number,
		AmountCents:      inv.TotalCents,
		Currency:         inv.Currency,
		Description:      fmt.Sprintf("%s — %s (%s)", svc.Name, tier.Code, inv.Number),
	}, nil
}
