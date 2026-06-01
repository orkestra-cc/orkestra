// Package handlers — client_handler.go hosts the Tier-2 self-service
// payment endpoints mounted on the ADR-0003 client API surface
// (api.orkestra.com / api.localhost). Each route is gated by
// RequireGlobal() at mount time AND re-checks ownership in the handler
// against the caller's tenant memberships.
package handlers

import (
	"context"
	"errors"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra/backend/internal/addons/payments/models"
	"github.com/orkestra/backend/internal/addons/payments/repository"
	"github.com/orkestra/backend/internal/addons/payments/services"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// ClientHandler bundles the Tier-2 self-service payment routes. Constructed
// in payments/module.go's Init when the tenant module is wired so that
// EnsureTenantForUser can materialize the caller's personal tenant.
type ClientHandler struct {
	payment *services.PaymentService
	txRepo  repository.TransactionRepository
	pmRepo  repository.PaymentMethodRepository
	tenants iface.TenantProvider
	// planner is optional — the checkout-session route returns 503 when
	// the subscriptions module is disabled or has not registered the key.
	planner iface.SelfServiceCheckoutPlanner
}

// NewClientHandler constructs the handler. tenants is required for the
// self-service flows; routes return 503 when it is nil. planner is
// optional in the same shape as the legacy contract.
func NewClientHandler(
	payment *services.PaymentService,
	txRepo repository.TransactionRepository,
	pmRepo repository.PaymentMethodRepository,
	tenants iface.TenantProvider,
	planner iface.SelfServiceCheckoutPlanner,
) *ClientHandler {
	return &ClientHandler{
		payment: payment,
		txRepo:  txRepo,
		pmRepo:  pmRepo,
		tenants: tenants,
		planner: planner,
	}
}

// callerTenantSet returns the set of tenant UUIDs the caller may act
// under: the caller's personal tenant (materialized lazily) plus every
// tenant they own. 401 when anonymous, 503 when the tenant provider is
// missing.
func (h *ClientHandler) callerTenantSet(ctx context.Context) (map[string]struct{}, error) {
	userUUID, ok := ctxauth.GetUserUUID(ctx)
	if !ok || userUUID == "" {
		return nil, huma.Error401Unauthorized("authentication required")
	}
	if h.tenants == nil {
		return nil, huma.Error503ServiceUnavailable("tenant provider not configured")
	}
	owned := map[string]struct{}{}
	personal, err := h.tenants.EnsureTenantForUser(ctx, userUUID)
	if err != nil {
		return nil, err
	}
	if personal != nil && personal.UUID != "" {
		owned[personal.UUID] = struct{}{}
	}
	memberships, err := h.tenants.ListUserMemberships(ctx, userUUID)
	if err != nil {
		return nil, err
	}
	for _, m := range memberships {
		if m.IsOwner {
			owned[m.TenantUUID] = struct{}{}
		}
	}
	return owned, nil
}

// ensureCustomerForTenant returns the gateway customer reference for the
// tenant, creating one on the gateway side if missing and persisting the
// id back onto the tenant row so subsequent calls skip the round-trip.
func (h *ClientHandler) ensureCustomerForTenant(ctx context.Context, tenantUUID string) (iface.CustomerRef, string, error) {
	if h.tenants == nil {
		return iface.CustomerRef{}, "", huma.Error503ServiceUnavailable("tenant provider not configured")
	}
	t, err := h.tenants.GetTenant(ctx, tenantUUID)
	if err != nil {
		return iface.CustomerRef{}, "", err
	}
	if t == nil {
		return iface.CustomerRef{}, "", huma.Error404NotFound("tenant not found")
	}
	if t.StripeCustomerID != "" {
		return iface.CustomerRef{Provider: "stripe", ID: t.StripeCustomerID}, t.Email, nil
	}
	name := t.LegalName
	if name == "" {
		name = t.Name
	}
	ref, err := h.payment.CreateCustomer(ctx, iface.CustomerInput{
		TenantUUID: tenantUUID,
		Email:      t.Email,
		Name:       name,
		VATNumber:  t.VATNumber,
		Country:    t.Country,
	})
	if err != nil {
		return iface.CustomerRef{}, "", err
	}
	// Best-effort persistence: the customer exists on Stripe and the
	// renewal job's metadata-keyed lookup will reuse it even if this
	// write fails.
	_ = h.tenants.SetTenantStripeCustomerID(ctx, t.UUID, ref.ID)
	return ref, t.Email, nil
}

// resolveTenantFilter validates the optional `tenantUuid` query param
// against the caller's tenant set. Returns "" when no filter is requested.
// A filter naming a tenant the caller does not own returns ok=false
// (handlers reply 0 rows).
func (h *ClientHandler) resolveTenantFilter(owned map[string]struct{}, tenantUUID string) (string, bool) {
	if tenantUUID == "" {
		return "", true
	}
	if _, ok := owned[tenantUUID]; !ok {
		return tenantUUID, false
	}
	return tenantUUID, true
}

// --- Transactions ---

type MeListTransactionsRequest struct {
	TenantUUID       string `query:"tenantUuid" doc:"Optional — restrict to one owned tenant"`
	SubscriptionUUID string `query:"subscriptionUuid" doc:"Optional — restrict to one subscription"`
	Status           string `query:"status" enum:"pending,requires_action,succeeded,failed,refunded,partially_refunded"`
}
type MeListTransactionsResponse struct {
	Body struct {
		Items []models.Transaction `json:"items"`
		Total int                  `json:"total"`
	}
}

// MeListTransactions returns transactions across every tenant the caller
// may act under. Per-tenant fan-out — the repository's tenant index is the
// only one available, and a Tier-2 user typically acts under one or two
// tenants in the demo MVP shape.
func (h *ClientHandler) MeListTransactions(ctx context.Context, in *MeListTransactionsRequest) (*MeListTransactionsResponse, error) {
	owned, err := h.callerTenantSet(ctx)
	if err != nil {
		return nil, err
	}
	resp := &MeListTransactionsResponse{}
	resp.Body.Items = []models.Transaction{}

	target, ok := h.resolveTenantFilter(owned, in.TenantUUID)
	if target != "" {
		if !ok {
			resp.Body.Total = 0
			return resp, nil
		}
		owned = map[string]struct{}{target: {}}
	}

	for tenantUUID := range owned {
		items, err := h.txRepo.List(ctx, repository.TransactionFilters{
			TenantUUID:       tenantUUID,
			SubscriptionUUID: in.SubscriptionUUID,
			Status:           models.TransactionStatus(in.Status),
		})
		if err != nil {
			return nil, err
		}
		resp.Body.Items = append(resp.Body.Items, items...)
	}
	resp.Body.Total = len(resp.Body.Items)
	return resp, nil
}

// --- Payment methods ---

type MeListPaymentMethodsRequest struct {
	TenantUUID string `query:"tenantUuid" doc:"Optional — restrict to one owned tenant"`
}
type MeListPaymentMethodsResponse struct {
	Body struct {
		Items []models.PaymentMethod `json:"items"`
		Total int                    `json:"total"`
	}
}

func (h *ClientHandler) MeListPaymentMethods(ctx context.Context, in *MeListPaymentMethodsRequest) (*MeListPaymentMethodsResponse, error) {
	owned, err := h.callerTenantSet(ctx)
	if err != nil {
		return nil, err
	}
	resp := &MeListPaymentMethodsResponse{}
	resp.Body.Items = []models.PaymentMethod{}

	target, ok := h.resolveTenantFilter(owned, in.TenantUUID)
	if target != "" {
		if !ok {
			resp.Body.Total = 0
			return resp, nil
		}
		owned = map[string]struct{}{target: {}}
	}

	for tenantUUID := range owned {
		items, err := h.pmRepo.ListByTenant(ctx, tenantUUID)
		if err != nil {
			return nil, err
		}
		resp.Body.Items = append(resp.Body.Items, items...)
	}
	resp.Body.Total = len(resp.Body.Items)
	return resp, nil
}

// --- Stripe Checkout (payment mode) ---

type MeCreateCheckoutSessionRequest struct {
	Body struct {
		SubscriptionUUID string `json:"subscriptionUuid" doc:"UUID of a subscription owned by the caller"`
		SuccessURL       string `json:"successUrl" doc:"Absolute URL Stripe redirects to on success"`
		CancelURL        string `json:"cancelUrl" doc:"Absolute URL Stripe redirects to on cancel"`
	}
}

type MeCheckoutSessionResponse struct {
	Body struct {
		SessionID     string `json:"sessionId"`
		URL           string `json:"url"`
		AmountCents   int64  `json:"amountCents"`
		Currency      string `json:"currency"`
		InvoiceUUID   string `json:"invoiceUuid"`
		InvoiceNumber string `json:"invoiceNumber"`
	}
}

// MeCreateCheckoutSession opens a Stripe Checkout session in payment mode
// for the subscription's most recent pending invoice. The webhook
// reconciler picks up the resulting PaymentIntent via the metadata stamp
// (subscriptionUUID + invoiceUUID + tenantUUID) — the same path the
// renewal job's off-session charges follow.
//
// 409 when the subscription has no pending invoice — the SPA can prompt
// the user to wait for the next renewal tick (or, post-MVP, hit a
// "trigger renewal" admin action).
func (h *ClientHandler) MeCreateCheckoutSession(ctx context.Context, in *MeCreateCheckoutSessionRequest) (*MeCheckoutSessionResponse, error) {
	if h.planner == nil {
		return nil, huma.Error503ServiceUnavailable("subscription planner not configured")
	}
	if in.Body.SubscriptionUUID == "" {
		return nil, huma.Error400BadRequest("subscriptionUuid is required")
	}
	if in.Body.SuccessURL == "" || in.Body.CancelURL == "" {
		return nil, huma.Error400BadRequest("successUrl and cancelUrl are required")
	}

	owned, err := h.callerTenantSet(ctx)
	if err != nil {
		return nil, err
	}

	plan, err := h.planner.PlanCheckoutSession(ctx, in.Body.SubscriptionUUID)
	if err != nil {
		switch {
		case errors.Is(err, iface.ErrCheckoutNoPendingInvoice):
			return nil, huma.Error409Conflict("no pending invoice for subscription")
		default:
			return nil, err
		}
	}

	if _, ok := owned[plan.TenantUUID]; !ok {
		// 404 (not 403) so the existence of out-of-scope subscriptions
		// does not leak to a fishing client.
		return nil, huma.Error404NotFound("not found")
	}

	customer, customerEmail, err := h.ensureCustomerForTenant(ctx, plan.TenantUUID)
	if err != nil {
		return nil, err
	}

	res, err := h.payment.CreateCheckoutSession(ctx, iface.CheckoutSessionInput{
		Customer:      customer,
		AmountCents:   plan.AmountCents,
		Currency:      plan.Currency,
		Description:   plan.Description,
		SuccessURL:    in.Body.SuccessURL,
		CancelURL:     in.Body.CancelURL,
		CustomerEmail: customerEmail,
		Metadata: map[string]string{
			"subscriptionUUID": plan.SubscriptionUUID,
			"invoiceUUID":      plan.InvoiceUUID,
			"tenantUUID":       plan.TenantUUID,
		},
	})
	if err != nil {
		return nil, err
	}

	resp := &MeCheckoutSessionResponse{}
	resp.Body.SessionID = res.SessionID
	resp.Body.URL = res.URL
	resp.Body.AmountCents = plan.AmountCents
	resp.Body.Currency = plan.Currency
	resp.Body.InvoiceUUID = plan.InvoiceUUID
	resp.Body.InvoiceNumber = plan.InvoiceNumber
	return resp, nil
}

// --- Stripe Checkout (setup mode — save a card without charging) ---

type MeCreateSetupCheckoutRequest struct {
	Body struct {
		TenantUUID string `json:"tenantUuid,omitempty" doc:"Optional — UUID of a tenant the caller owns. Defaults to the caller's personal tenant."`
		SuccessURL string `json:"successUrl" doc:"Absolute URL Stripe redirects to on success"`
		CancelURL  string `json:"cancelUrl" doc:"Absolute URL Stripe redirects to on cancel"`
	}
}

func (h *ClientHandler) MeCreateSetupCheckoutSession(ctx context.Context, in *MeCreateSetupCheckoutRequest) (*MeCheckoutSessionResponse, error) {
	if in.Body.SuccessURL == "" || in.Body.CancelURL == "" {
		return nil, huma.Error400BadRequest("successUrl and cancelUrl are required")
	}

	owned, err := h.callerTenantSet(ctx)
	if err != nil {
		return nil, err
	}

	tenantUUID := in.Body.TenantUUID
	if tenantUUID == "" {
		// Default: the caller's personal tenant. callerTenantSet already
		// materialized it; pick the first matching entry.
		userUUID, _ := ctxauth.GetUserUUID(ctx)
		personal, err := h.tenants.EnsureTenantForUser(ctx, userUUID)
		if err != nil {
			return nil, err
		}
		if personal == nil || personal.UUID == "" {
			return nil, huma.Error500InternalServerError("failed to materialize personal tenant")
		}
		tenantUUID = personal.UUID
	} else if _, ok := owned[tenantUUID]; !ok {
		return nil, huma.Error404NotFound("not found")
	}

	customer, customerEmail, err := h.ensureCustomerForTenant(ctx, tenantUUID)
	if err != nil {
		return nil, err
	}

	res, err := h.payment.CreateSetupCheckoutSession(ctx, iface.SetupCheckoutInput{
		Customer:      customer,
		SuccessURL:    in.Body.SuccessURL,
		CancelURL:     in.Body.CancelURL,
		CustomerEmail: customerEmail,
		Metadata: map[string]string{
			"tenantUUID": tenantUUID,
		},
	})
	if err != nil {
		return nil, err
	}

	resp := &MeCheckoutSessionResponse{}
	resp.Body.SessionID = res.SessionID
	resp.Body.URL = res.URL
	return resp, nil
}
