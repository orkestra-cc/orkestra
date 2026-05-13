package services

import (
	"context"

	"github.com/orkestra-cc/orkestra-addon-subscriptions/repository"
	"github.com/orkestra-cc/orkestra-sdk/iface"
)

// TenantSubscriptionAdapter implements iface.TenantSubscriptionProvider by
// translating the rich subscriptions_subscriptions documents into the flat
// DTOs the core/tenant aggregator endpoint returns. It is intentionally
// read-only — mutations stay on the dedicated Subscription handlers.
type TenantSubscriptionAdapter struct {
	subs repository.SubscriptionRepository
}

// NewTenantSubscriptionAdapter constructs the adapter. Registered under
// module.ServiceTenantSubscriptionProvider by SubscriptionsModule.Init.
func NewTenantSubscriptionAdapter(subs repository.SubscriptionRepository) *TenantSubscriptionAdapter {
	return &TenantSubscriptionAdapter{subs: subs}
}

// ListByTenant returns every subscription owned by the tenant. The
// repository layer already sorts by createdAt desc.
func (a *TenantSubscriptionAdapter) ListByTenant(ctx context.Context, tenantUUID string) ([]iface.TenantSubscription, error) {
	if tenantUUID == "" {
		return []iface.TenantSubscription{}, nil
	}
	rows, err := a.subs.FindByTenant(ctx, tenantUUID)
	if err != nil {
		return nil, err
	}
	out := make([]iface.TenantSubscription, 0, len(rows))
	for i := range rows {
		r := rows[i]
		out = append(out, iface.TenantSubscription{
			UUID:               r.UUID,
			TenantUUID:         r.TenantUUID,
			ServiceUUID:        r.ServiceUUID,
			TierCode:           r.TierCode,
			Status:             string(r.Status),
			CurrentPeriodStart: r.CurrentPeriodStart,
			CurrentPeriodEnd:   r.CurrentPeriodEnd,
			NextBillingAt:      r.NextBillingAt,
			CreatedAt:          r.CreatedAt,
		})
	}
	return out, nil
}
