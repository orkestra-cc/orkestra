// Package handlers — client_handler.go hosts the Tier-2 self-service
// payment endpoints mounted on the ADR-0003 client API surface
// (api.orkestra.com / api.localhost). Each route is gated by
// RequireGlobal() at mount time AND re-checks ownership in the handler
// against the caller's identity (user) and TenantProvider memberships
// (tenant), mirroring the SubscriptionHandler pattern.
package handlers

import (
	"context"
	"errors"
	"fmt"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra/backend/internal/addons/payments/models"
	"github.com/orkestra/backend/internal/addons/payments/repository"
	"github.com/orkestra/backend/internal/addons/payments/services"
	"github.com/orkestra/backend/internal/shared/iface"
	"github.com/orkestra/backend/internal/shared/middleware"
)

// ClientHandler bundles the Tier-2 self-service payment routes. Constructed
// in payments/module.go's Init when the tenant module is wired so that
// tenant-owner flows can be reached. User-owner flows degrade gracefully
// when the tenant module is missing.
type ClientHandler struct {
	payment *services.PaymentService
	txRepo  repository.TransactionRepository
	pmRepo  repository.PaymentMethodRepository
	tenants iface.TenantProvider
	// userBilling is optional — when nil (clientbilling addon disabled)
	// user-owner checkout returns 503. Tenant-owner flows still work.
	userBilling iface.UserBillingCustomerProvider
	// planner is optional — the checkout-session route returns 503 when
	// the subscriptions module is disabled or has not registered the key.
	planner iface.SelfServiceCheckoutPlanner
	// lazyTenantProvisioning gates the Unified Client Aggregate Phase 2
	// behavior: callerOwnerSet calls TenantProvider.EnsureTenantForUser and
	// adds the resulting personal tenant to the caller's owner set. Default
	// ON since Phase 3; set UNIFIED_CLIENTS_LAZY_TENANT_ENABLED=false to
	// revert to the user-owner-only path while Phase 4/5 land.
	lazyTenantProvisioning bool
}

// NewClientHandler constructs the handler. tenants may be nil — tenant-
// owner routes return 503 in that case; user-owner routes still work
// when userBilling is wired. planner is optional in the same shape as the
// legacy contract.
func NewClientHandler(
	payment *services.PaymentService,
	txRepo repository.TransactionRepository,
	pmRepo repository.PaymentMethodRepository,
	tenants iface.TenantProvider,
	userBilling iface.UserBillingCustomerProvider,
	planner iface.SelfServiceCheckoutPlanner,
	lazyTenantProvisioning bool,
) *ClientHandler {
	return &ClientHandler{
		payment:                payment,
		txRepo:                 txRepo,
		pmRepo:                 pmRepo,
		tenants:                tenants,
		userBilling:            userBilling,
		planner:                planner,
		lazyTenantProvisioning: lazyTenantProvisioning,
	}
}

// callerOwnerSet returns the polymorphic owners the caller may act under:
// always the caller's own user identity, plus every tenant the caller
// owns. 401 when anonymous; the empty-tenant case is non-error — the user
// identity alone is a valid scope for self-service.
func (h *ClientHandler) callerOwnerSet(ctx context.Context) (map[iface.Owner]struct{}, error) {
	userUUID, ok := middleware.GetUserUUID(ctx)
	if !ok || userUUID == "" {
		return nil, huma.Error401Unauthorized("authentication required")
	}
	owned := map[iface.Owner]struct{}{
		iface.UserOwner(userUUID): {},
	}
	if h.tenants == nil {
		return owned, nil
	}
	// Unified Client Aggregate Phase 2 — see SubscriptionHandler.callerOwnerSet
	// for the rationale. Provisioning errors surface so that an unreachable
	// tenant module doesn't silently scope away the user's payment data.
	if h.lazyTenantProvisioning {
		personal, err := h.tenants.EnsureTenantForUser(ctx, userUUID)
		if err != nil {
			return nil, err
		}
		if personal != nil && personal.UUID != "" {
			owned[iface.TenantOwner(personal.UUID)] = struct{}{}
		}
	}
	memberships, err := h.tenants.ListUserMemberships(ctx, userUUID)
	if err != nil {
		return nil, err
	}
	for _, m := range memberships {
		if m.IsOwner {
			owned[iface.TenantOwner(m.TenantUUID)] = struct{}{}
		}
	}
	return owned, nil
}

// ensureCustomerForOwner returns the gateway customer reference for the
// owner, creating one on the gateway side if missing and persisting the
// id back onto the underlying record so subsequent calls skip the
// round-trip.
//
// Tenant-owner: reuses the existing tenant.StripeCustomerID seam.
// User-owner: requires the Phase 2 client_billing_customers projection.
// Until that lands, user-owner checkout returns 503 with a clear hint.
func (h *ClientHandler) ensureCustomerForOwner(ctx context.Context, owner iface.Owner) (iface.CustomerRef, string, error) {
	switch owner.Kind {
	case iface.OwnerKindTenant:
		if h.tenants == nil {
			return iface.CustomerRef{}, "", huma.Error503ServiceUnavailable("tenant provider not configured")
		}
		t, err := h.tenants.GetTenant(ctx, owner.UUID)
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
			Owner:     owner,
			Email:     t.Email,
			Name:      name,
			VATNumber: t.VATNumber,
			Country:   t.Country,
		})
		if err != nil {
			return iface.CustomerRef{}, "", err
		}
		// Persistence is best-effort — the customer exists on Stripe and
		// the renewal job's metadata-keyed lookup will reuse it.
		_ = h.tenants.SetTenantStripeCustomerID(ctx, t.UUID, ref.ID)
		return ref, t.Email, nil
	case iface.OwnerKindUser:
		if h.userBilling == nil {
			return iface.CustomerRef{}, "", huma.Error503ServiceUnavailable("user billing profile not configured")
		}
		prof, err := h.userBilling.Get(ctx, owner.UUID)
		if err != nil {
			return iface.CustomerRef{}, "", err
		}
		if prof == nil {
			// The client has not filled in their billing details yet —
			// surface a 409 so the SPA can route them to the billing-
			// profile form before retrying checkout.
			return iface.CustomerRef{}, "", huma.Error409Conflict("complete your billing profile before checkout")
		}
		if prof.StripeCustomerID != "" {
			return iface.CustomerRef{Provider: "stripe", ID: prof.StripeCustomerID}, prof.Email, nil
		}
		ref, err := h.payment.CreateCustomer(ctx, iface.CustomerInput{
			Owner:     owner,
			Email:     prof.Email,
			Name:      prof.DisplayName(),
			VATNumber: prof.VATNumber,
			Country:   prof.Country,
		})
		if err != nil {
			return iface.CustomerRef{}, "", err
		}
		// Best-effort persistence: the customer exists on Stripe and the
		// renewal job's metadata-keyed lookup will reuse it even if this
		// write fails.
		_ = h.userBilling.SetStripeCustomerID(ctx, owner.UUID, ref.ID)
		return ref, prof.Email, nil
	default:
		return iface.CustomerRef{}, "", fmt.Errorf("unknown owner kind %q", owner.Kind)
	}
}

// resolveOwnerFilter decodes the wire (ownerKind, ownerUUID, tenantUuid)
// triple into a single iface.Owner, validating it against the caller's
// owner set. tenantUuid is the legacy alias kept for transitional clients.
// Returns iface.Owner{} when no filter is requested. A filter naming a
// principal the caller does not own returns ok=false (handlers reply 0 rows).
func (h *ClientHandler) resolveOwnerFilter(owned map[iface.Owner]struct{}, kind, ownerUUID, tenantUUID string) (iface.Owner, bool, error) {
	if ownerUUID == "" && tenantUUID == "" {
		return iface.Owner{}, true, nil
	}
	var target iface.Owner
	if ownerUUID != "" {
		k := kind
		if k == "" {
			k = string(iface.OwnerKindTenant)
		}
		switch iface.OwnerKind(k) {
		case iface.OwnerKindUser, iface.OwnerKindTenant:
			target = iface.Owner{Kind: iface.OwnerKind(k), UUID: ownerUUID}
		default:
			return iface.Owner{}, false, huma.Error400BadRequest("invalid ownerKind")
		}
	} else {
		target = iface.TenantOwner(tenantUUID)
	}
	if _, ok := owned[target]; !ok {
		return target, false, nil
	}
	return target, true, nil
}

// --- Transactions ---

type MeListTransactionsRequest struct {
	OwnerKind        string `query:"ownerKind" enum:"user,tenant" doc:"Optional — restrict to one owner kind"`
	OwnerUUID        string `query:"ownerUuid" doc:"Optional — restrict to one owned principal"`
	TenantUUID       string `query:"tenantUuid" doc:"Deprecated alias for ownerKind=tenant + ownerUuid"`
	SubscriptionUUID string `query:"subscriptionUuid" doc:"Optional — restrict to one subscription"`
	Status           string `query:"status" enum:"pending,requires_action,succeeded,failed,refunded,partially_refunded"`
}
type MeListTransactionsResponse struct {
	Body struct {
		Items []models.Transaction `json:"items"`
		Total int                  `json:"total"`
	}
}

// MeListTransactions returns transactions across every owner the caller
// may act under. Per-owner fan-out — the repository's owner index is the
// only one available, and a Tier-2 user typically acts under one or two
// principals (themselves + one or two tenants) in the demo MVP shape.
func (h *ClientHandler) MeListTransactions(ctx context.Context, in *MeListTransactionsRequest) (*MeListTransactionsResponse, error) {
	owned, err := h.callerOwnerSet(ctx)
	if err != nil {
		return nil, err
	}
	resp := &MeListTransactionsResponse{}
	resp.Body.Items = []models.Transaction{}

	target, ok, err := h.resolveOwnerFilter(owned, in.OwnerKind, in.OwnerUUID, in.TenantUUID)
	if err != nil {
		return nil, err
	}
	if !target.IsZero() {
		if !ok {
			resp.Body.Total = 0
			return resp, nil
		}
		owned = map[iface.Owner]struct{}{target: {}}
	}

	for owner := range owned {
		items, err := h.txRepo.List(ctx, repository.TransactionFilters{
			Owner:            owner,
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
	OwnerKind  string `query:"ownerKind" enum:"user,tenant"`
	OwnerUUID  string `query:"ownerUuid"`
	TenantUUID string `query:"tenantUuid" doc:"Deprecated alias for ownerKind=tenant + ownerUuid"`
}
type MeListPaymentMethodsResponse struct {
	Body struct {
		Items []models.PaymentMethod `json:"items"`
		Total int                    `json:"total"`
	}
}

func (h *ClientHandler) MeListPaymentMethods(ctx context.Context, in *MeListPaymentMethodsRequest) (*MeListPaymentMethodsResponse, error) {
	owned, err := h.callerOwnerSet(ctx)
	if err != nil {
		return nil, err
	}
	resp := &MeListPaymentMethodsResponse{}
	resp.Body.Items = []models.PaymentMethod{}

	target, ok, err := h.resolveOwnerFilter(owned, in.OwnerKind, in.OwnerUUID, in.TenantUUID)
	if err != nil {
		return nil, err
	}
	if !target.IsZero() {
		if !ok {
			resp.Body.Total = 0
			return resp, nil
		}
		owned = map[iface.Owner]struct{}{target: {}}
	}

	for owner := range owned {
		items, err := h.pmRepo.ListByOwner(ctx, owner)
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
// (subscriptionUUID + invoiceUUID + ownerKind + ownerUUID) — the same path
// the renewal job's off-session charges follow.
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

	owned, err := h.callerOwnerSet(ctx)
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

	if _, ok := owned[plan.Owner]; !ok {
		// 404 (not 403) so the existence of out-of-scope subscriptions
		// does not leak to a fishing client.
		return nil, huma.Error404NotFound("not found")
	}

	customer, customerEmail, err := h.ensureCustomerForOwner(ctx, plan.Owner)
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
			"ownerKind":        string(plan.Owner.Kind),
			"ownerUUID":        plan.Owner.UUID,
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
		OwnerKind  string `json:"ownerKind,omitempty" enum:"user,tenant" doc:"Polymorphic owner kind (defaults to user)"`
		OwnerUUID  string `json:"ownerUuid,omitempty" doc:"Owner UUID — user UUID or tenant UUID per ownerKind. Defaults to the calling user when omitted."`
		TenantUUID string `json:"tenantUuid,omitempty" doc:"Deprecated alias for ownerKind=tenant + ownerUuid"`
		SuccessURL string `json:"successUrl" doc:"Absolute URL Stripe redirects to on success"`
		CancelURL  string `json:"cancelUrl" doc:"Absolute URL Stripe redirects to on cancel"`
	}
}

func (h *ClientHandler) MeCreateSetupCheckoutSession(ctx context.Context, in *MeCreateSetupCheckoutRequest) (*MeCheckoutSessionResponse, error) {
	if in.Body.SuccessURL == "" || in.Body.CancelURL == "" {
		return nil, huma.Error400BadRequest("successUrl and cancelUrl are required")
	}

	owned, err := h.callerOwnerSet(ctx)
	if err != nil {
		return nil, err
	}

	// Resolve the owner from the input or default to the calling user.
	var owner iface.Owner
	switch {
	case in.Body.OwnerUUID != "":
		k := in.Body.OwnerKind
		if k == "" {
			k = string(iface.OwnerKindUser)
		}
		switch iface.OwnerKind(k) {
		case iface.OwnerKindUser, iface.OwnerKindTenant:
			owner = iface.Owner{Kind: iface.OwnerKind(k), UUID: in.Body.OwnerUUID}
		default:
			return nil, huma.Error400BadRequest("invalid ownerKind")
		}
	case in.Body.TenantUUID != "":
		owner = iface.TenantOwner(in.Body.TenantUUID)
	default:
		// Default: the calling user.
		userUUID, ok := middleware.GetUserUUID(ctx)
		if !ok || userUUID == "" {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		owner = iface.UserOwner(userUUID)
	}

	if _, ok := owned[owner]; !ok {
		return nil, huma.Error404NotFound("not found")
	}

	customer, customerEmail, err := h.ensureCustomerForOwner(ctx, owner)
	if err != nil {
		return nil, err
	}

	res, err := h.payment.CreateSetupCheckoutSession(ctx, iface.SetupCheckoutInput{
		Customer:      customer,
		SuccessURL:    in.Body.SuccessURL,
		CancelURL:     in.Body.CancelURL,
		CustomerEmail: customerEmail,
		Metadata: map[string]string{
			"ownerKind": string(owner.Kind),
			"ownerUUID": owner.UUID,
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
