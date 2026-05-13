package services

import (
	"context"

	"github.com/orkestra-cc/orkestra-sdk/iface"
	"github.com/orkestra/backend/internal/addons/payments/repository"
)

// TenantPaymentAdapter implements iface.TenantPaymentProvider by flattening
// Transaction rows into the shape the core/tenant aggregator endpoint
// returns. Read-only — refund and create paths stay on the Transaction
// handler.
type TenantPaymentAdapter struct {
	transactions repository.TransactionRepository
}

// NewTenantPaymentAdapter constructs the adapter. Registered under
// module.ServiceTenantPaymentProvider by PaymentsModule.Init.
func NewTenantPaymentAdapter(transactions repository.TransactionRepository) *TenantPaymentAdapter {
	return &TenantPaymentAdapter{transactions: transactions}
}

// ListByTenant returns every transaction billed to the tenant.
func (a *TenantPaymentAdapter) ListByTenant(ctx context.Context, tenantUUID string) ([]iface.TenantPayment, error) {
	if tenantUUID == "" {
		return []iface.TenantPayment{}, nil
	}
	rows, err := a.transactions.FindByTenant(ctx, tenantUUID)
	if err != nil {
		return nil, err
	}
	out := make([]iface.TenantPayment, 0, len(rows))
	for i := range rows {
		r := rows[i]
		out = append(out, iface.TenantPayment{
			UUID:             r.UUID,
			TenantUUID:       r.TenantUUID,
			SubscriptionUUID: r.SubscriptionUUID,
			InvoiceUUID:      r.InvoiceUUID,
			Provider:         string(r.Provider),
			ProviderTxID:     r.ProviderTxID,
			Status:           string(r.Status),
			AmountCents:      r.AmountCents,
			Currency:         r.Currency,
			RefundedCents:    r.RefundedCents,
			ChargedAt:        r.ChargedAt,
			RefundedAt:       r.RefundedAt,
			CreatedAt:        r.CreatedAt,
		})
	}
	return out, nil
}
