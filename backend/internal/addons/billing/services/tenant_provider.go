package services

import (
	"context"
	"errors"

	"github.com/orkestra/backend/internal/addons/billing/models"
	"github.com/orkestra/backend/internal/addons/billing/repository"
	"github.com/orkestra/backend/internal/shared/iface"
)

// TenantBillingCustomerAdapter implements iface.TenantBillingCustomerProvider.
// Registered under module.ServiceTenantBillingCustomerProvider in
// BillingModule.Init so core/tenant can serve the
// /v1/admin/tenants/{id}/billing-customer aggregator endpoint without
// importing this addon. Read-only listing + idempotent promote — actual
// editing stays on the billing module's own /v1/billing/customers handlers.
//
// ADR-0001 PR-4.
type TenantBillingCustomerAdapter struct {
	customers repository.CustomerRepository
	service   CustomerService
}

// NewTenantBillingCustomerAdapter constructs the adapter.
func NewTenantBillingCustomerAdapter(customers repository.CustomerRepository, svc CustomerService) *TenantBillingCustomerAdapter {
	return &TenantBillingCustomerAdapter{customers: customers, service: svc}
}

// GetByTenant returns the customer linked to the given tenant.
//
// Contract: returns (nil, nil) when no customer is linked — the aggregator
// handler renders that as 404. Other repository errors propagate.
func (a *TenantBillingCustomerAdapter) GetByTenant(ctx context.Context, tenantUUID string) (*iface.TenantBillingCustomer, error) {
	if tenantUUID == "" {
		return nil, nil
	}
	c, err := a.customers.GetByTenantUUID(ctx, tenantUUID)
	if err != nil {
		if errors.Is(err, repository.ErrCustomerNotFound) {
			return nil, nil // not-linked → caller renders 404
		}
		return nil, err
	}
	return toIfaceCustomer(c), nil
}

// PromoteTenant is the idempotent create-from-tenant path. Returns an
// existing customer when one is already linked, otherwise builds a new
// one from the tenant's iface.Tenant fields.
func (a *TenantBillingCustomerAdapter) PromoteTenant(ctx context.Context, tenantUUID string) (*iface.TenantBillingCustomer, error) {
	c, err := a.service.PromoteTenantToCustomer(ctx, tenantUUID)
	if err != nil {
		return nil, err
	}
	return toIfaceCustomer(c), nil
}

// toIfaceCustomer flattens the rich Customer into the cross-module DTO.
// The aggregator endpoint intentionally exposes only the subset of fields
// the admin clients page needs to render — full editing happens through
// the billing module's own routes.
func toIfaceCustomer(c *models.Customer) *iface.TenantBillingCustomer {
	if c == nil {
		return nil
	}
	return &iface.TenantBillingCustomer{
		UUID:         c.UUID,
		TenantUUID:   c.TenantUUID,
		Denomination: c.Denomination,
		Name:         c.Name,
		Surname:      c.Surname,
		FiscalIDCode: c.FiscalIDCode,
		IsCompany:    c.IsCompany,
		Country:      c.Country,
		IsActive:     c.IsActive,
		CreatedAt:    c.CreatedAt,
	}
}
