package handlers

import (
	"context"
	"errors"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra/backend/internal/addons/subscriptions/models"
	"github.com/orkestra/backend/internal/addons/subscriptions/repository"
	"github.com/orkestra/backend/internal/addons/subscriptions/services"
	"github.com/orkestra/backend/internal/shared/iface"
	"github.com/orkestra/backend/internal/shared/middleware"
)

type SubscriptionHandler struct {
	subs     *services.SubscriptionService
	renewal  *services.RenewalService
	invoices repository.InvoiceRepository
	activity *services.ActivityService
	// tenants powers the self-subscribe ownership check. nil in setups that
	// run subscriptions without the tenant module — self-subscribe with a
	// tenant owner returns 503 in that case (the route refuses to guess
	// ownership from thin air). User-owner self-subscribe still works.
	tenants iface.TenantProvider
	// lazyTenantProvisioning flips on the Unified Client Aggregate Phase 2
	// behavior: callerOwnerSet calls TenantProvider.EnsureTenantForUser and
	// adds the resulting personal tenant to the owner set. Default ON since
	// Phase 3; set UNIFIED_CLIENTS_LAZY_TENANT_ENABLED=false to revert to
	// the user-owner-only path.
	lazyTenantProvisioning bool
}

func NewSubscriptionHandler(
	subs *services.SubscriptionService,
	renewal *services.RenewalService,
	invoices repository.InvoiceRepository,
	activity *services.ActivityService,
	tenants iface.TenantProvider,
	lazyTenantProvisioning bool,
) *SubscriptionHandler {
	return &SubscriptionHandler{
		subs:                   subs,
		renewal:                renewal,
		invoices:               invoices,
		activity:               activity,
		tenants:                tenants,
		lazyTenantProvisioning: lazyTenantProvisioning,
	}
}

// CreateSubscriptionInput is the operator-admin payload. Subscriptions are
// owned by a polymorphic principal (a user OR a tenant) — admins can bind
// either kind. OwnerKind defaults to "tenant" for backward-compatible admin
// flows; explicit "user" is required for personal client grants.
type CreateSubscriptionInput struct {
	OwnerKind   string `json:"ownerKind,omitempty" enum:"user,tenant" doc:"Polymorphic owner kind (defaults to tenant)"`
	OwnerUUID   string `json:"ownerUUID" doc:"UUID of the owner — user UUID or tenant UUID per OwnerKind"`
	ServiceUUID string `json:"serviceUUID"`
	TierCode    string `json:"tierCode"`
}

type CreateSubscriptionRequest struct {
	Body CreateSubscriptionInput
}
type SubscriptionResponse struct {
	Body models.Subscription
}
type GetSubscriptionRequest struct {
	ID string `path:"id"`
}
type ListSubscriptionsRequest struct {
	OwnerKind   string `query:"ownerKind" enum:"user,tenant"`
	OwnerUUID   string `query:"ownerUuid"`
	ServiceUUID string `query:"serviceUUID"`
	Status      string `query:"status" enum:"active,past_due,suspended,cancelled,expired"`
}
type ListSubscriptionsResponse struct {
	Body struct {
		Items []models.Subscription `json:"items"`
		Total int                   `json:"total"`
	}
}
type CancelSubscriptionRequest struct {
	ID   string `path:"id"`
	Body struct {
		AtPeriodEnd bool `json:"atPeriodEnd"`
	}
}
type ReactivateSubscriptionRequest struct {
	ID string `path:"id"`
}
type RetryChargeRequest struct {
	ID string `path:"id"`
}

func (h *SubscriptionHandler) Create(ctx context.Context, in *CreateSubscriptionRequest) (*SubscriptionResponse, error) {
	owner, err := parseOwnerInput(in.Body.OwnerKind, in.Body.OwnerUUID)
	if err != nil {
		return nil, err
	}
	if in.Body.ServiceUUID == "" || in.Body.TierCode == "" {
		return nil, huma.Error400BadRequest("serviceUUID and tierCode are required")
	}
	sub, err := h.subs.Create(ctx, owner, in.Body.ServiceUUID, in.Body.TierCode, actorFrom(ctx))
	if err != nil {
		return nil, err
	}
	return &SubscriptionResponse{Body: *sub}, nil
}

func (h *SubscriptionHandler) Get(ctx context.Context, in *GetSubscriptionRequest) (*SubscriptionResponse, error) {
	sub, err := h.subs.Get(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	if err := h.guardSubscriptionOwnership(ctx, sub); err != nil {
		return nil, err
	}
	return &SubscriptionResponse{Body: *sub}, nil
}

func (h *SubscriptionHandler) List(ctx context.Context, in *ListSubscriptionsRequest) (*ListSubscriptionsResponse, error) {
	filter := repository.SubscriptionFilters{
		ServiceUUID: in.ServiceUUID,
		Status:      models.SubStatus(in.Status),
	}
	if in.OwnerUUID != "" {
		owner, err := parseOwnerInput(in.OwnerKind, in.OwnerUUID)
		if err != nil {
			return nil, err
		}
		filter.Owner = owner
	}
	items, err := h.subs.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	resp := &ListSubscriptionsResponse{}
	resp.Body.Items = items
	resp.Body.Total = len(items)
	return resp, nil
}

func (h *SubscriptionHandler) Cancel(ctx context.Context, in *CancelSubscriptionRequest) (*SubscriptionResponse, error) {
	existing, err := h.subs.Get(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	if err := h.guardSubscriptionOwnership(ctx, existing); err != nil {
		return nil, err
	}
	sub, err := h.subs.Cancel(ctx, in.ID, in.Body.AtPeriodEnd, actorFrom(ctx))
	if err != nil {
		return nil, err
	}
	return &SubscriptionResponse{Body: *sub}, nil
}

func (h *SubscriptionHandler) Reactivate(ctx context.Context, in *ReactivateSubscriptionRequest) (*SubscriptionResponse, error) {
	existing, err := h.subs.Get(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	if err := h.guardSubscriptionOwnership(ctx, existing); err != nil {
		return nil, err
	}
	sub, err := h.subs.Reactivate(ctx, in.ID, actorFrom(ctx))
	if err != nil {
		return nil, err
	}
	return &SubscriptionResponse{Body: *sub}, nil
}

func (h *SubscriptionHandler) RetryCharge(ctx context.Context, in *RetryChargeRequest) (*SubscriptionResponse, error) {
	existing, err := h.subs.Get(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	if err := h.guardSubscriptionOwnership(ctx, existing); err != nil {
		return nil, err
	}
	sub, err := h.renewal.RetryNow(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	return &SubscriptionResponse{Body: *sub}, nil
}

type ListInvoicesRequest struct {
	ID string `path:"id"`
}
type ListInvoicesResponse struct {
	Body struct {
		Items []models.SubscriptionInvoice `json:"items"`
		Total int                          `json:"total"`
	}
}

func (h *SubscriptionHandler) ListInvoices(ctx context.Context, in *ListInvoicesRequest) (*ListInvoicesResponse, error) {
	sub, err := h.subs.Get(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	if err := h.guardSubscriptionOwnership(ctx, sub); err != nil {
		return nil, err
	}
	items, err := h.invoices.List(ctx, repository.InvoiceFilters{SubscriptionUUID: in.ID})
	if err != nil {
		return nil, err
	}
	resp := &ListInvoicesResponse{}
	resp.Body.Items = items
	resp.Body.Total = len(items)
	return resp, nil
}

type ListActivityRequest struct {
	ID    string `path:"id"`
	Limit int64  `query:"limit" default:"100" maximum:"500"`
}
type ListActivityResponse struct {
	Body struct {
		Items []models.ActivityLog `json:"items"`
		Total int                  `json:"total"`
	}
}

func (h *SubscriptionHandler) ListActivity(ctx context.Context, in *ListActivityRequest) (*ListActivityResponse, error) {
	sub, err := h.subs.Get(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	if err := h.guardSubscriptionOwnership(ctx, sub); err != nil {
		return nil, err
	}
	items, err := h.activity.List(ctx, in.ID, in.Limit)
	if err != nil {
		return nil, err
	}
	resp := &ListActivityResponse{}
	resp.Body.Items = items
	resp.Body.Total = len(items)
	return resp, nil
}

// SelfSubscribeInput is the wire-level payload for the self-service
// subscribe endpoint. Owner defaults to the calling user — self-registered
// clients subscribe as themselves. Admin-attached business members can pass
// `ownerKind: "tenant"` plus the tenant UUID to subscribe on the
// organization's behalf (ownership re-checked against the tenant module).
type SelfSubscribeInput struct {
	OwnerKind   string `json:"ownerKind,omitempty" enum:"user,tenant" doc:"Polymorphic owner kind (defaults to user)"`
	TenantUUID  string `json:"tenantUuid,omitempty" doc:"Required when ownerKind=tenant — UUID of the tenant the caller owns"`
	ServiceCode string `json:"serviceCode" doc:"Stable SKU code of a catalog service (e.g. 'pro')"`
	TierCode    string `json:"tierCode" doc:"Tier code within the service (e.g. 'monthly')"`
}

// SelfSubscribeRequest is the Huma wrapper. The body shape is concretely
// named so the OpenAPI schema registry keeps it distinct from the
// operator-facing CreateSubscriptionInput.
type SelfSubscribeRequest struct {
	Body SelfSubscribeInput
}

// SelfSubscribe creates a subscription owned by the caller's user identity
// or by a tenant the caller owns. Anonymous self-service checkout (public
// Stripe checkout session) is deferred — today we require an authenticated
// caller. The entitlement syncer grants capabilities on activation so the
// owner can hit RequireCapability-gated addons immediately; the first
// payment attempt still happens on the next renewal tick.
func (h *SubscriptionHandler) SelfSubscribe(ctx context.Context, req *SelfSubscribeRequest) (*SubscriptionResponse, error) {
	userUUID, ok := middleware.GetUserUUID(ctx)
	if !ok || userUUID == "" {
		return nil, huma.Error401Unauthorized("authentication required")
	}
	if req.Body.ServiceCode == "" || req.Body.TierCode == "" {
		return nil, huma.Error400BadRequest("serviceCode and tierCode are required")
	}

	kind := req.Body.OwnerKind
	if kind == "" {
		kind = string(iface.OwnerKindUser)
	}
	var owner iface.Owner
	switch iface.OwnerKind(kind) {
	case iface.OwnerKindUser:
		owner = iface.UserOwner(userUUID)
	case iface.OwnerKindTenant:
		if h.tenants == nil {
			return nil, huma.Error503ServiceUnavailable("tenant provider not configured")
		}
		if req.Body.TenantUUID == "" {
			return nil, huma.Error400BadRequest("tenantUuid is required when ownerKind=tenant")
		}
		memberships, err := h.tenants.ListUserMemberships(ctx, userUUID)
		if err != nil {
			return nil, err
		}
		ownsTenant := false
		for _, m := range memberships {
			if m.TenantUUID == req.Body.TenantUUID && m.IsOwner {
				ownsTenant = true
				break
			}
		}
		if !ownsTenant {
			return nil, huma.Error403Forbidden("caller is not owner of requested tenant")
		}
		owner = iface.TenantOwner(req.Body.TenantUUID)
	default:
		return nil, huma.Error400BadRequest("invalid ownerKind")
	}

	sub, err := h.subs.CreateForOwner(ctx, owner, req.Body.ServiceCode, req.Body.TierCode, userUUID)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrSubscriptionServiceCode):
			return nil, huma.Error404NotFound("service not found")
		case errors.Is(err, services.ErrSubscriptionTierNotFound):
			return nil, huma.Error404NotFound("tier not found on service")
		default:
			return nil, err
		}
	}
	return &SubscriptionResponse{Body: *sub}, nil
}

// guardSubscriptionOwnership enforces that the request's active tenant
// matches a tenant-owned subscription's owner. User-owned subscriptions are
// not gated by tenant context — they are visible to platform admins
// (whose tokens already bypass the X-Tenant-ID match check via the
// system.tenants.admin permission). Returns 404 on mismatch so existence
// of out-of-scope records is not leaked.
func (h *SubscriptionHandler) guardSubscriptionOwnership(ctx context.Context, sub *models.Subscription) error {
	if sub == nil {
		return nil
	}
	if sub.OwnerKind != iface.OwnerKindTenant || sub.OwnerUUID == "" {
		return nil
	}
	tenantID, hasTenant := middleware.GetTenantID(ctx)
	if !hasTenant {
		return nil
	}
	if sub.OwnerUUID != tenantID {
		return huma.Error404NotFound("not found", nil)
	}
	return nil
}

// callerOwnerSet returns the polymorphic owners the caller may act under:
// always the caller's own user identity, plus every tenant the caller owns.
// Returns 401 when anonymous, 503 when the tenant provider is missing for
// resolution. The empty-tenant case (caller has no memberships) is
// non-error — the user identity alone is a valid scope for self-service.
func (h *SubscriptionHandler) callerOwnerSet(ctx context.Context) (map[iface.Owner]struct{}, error) {
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
	// Unified Client Aggregate Phase 2: when the flag is on, materialize
	// (or fetch) the caller's personal tenant and treat it as an owned
	// principal alongside the legacy user owner. Errors here are surfaced
	// — the personal tenant is the canonical billing party once the flag
	// is on, so a silent fallback would hide subscriptions from the user.
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

// meGuardSubscription resolves the subscription by UUID and returns it iff
// the caller owns it (as user or via a tenant membership). Returns 404 (not
// 403) on ownership mismatch so the existence of out-of-scope subscriptions
// does not leak to a fishing client.
func (h *SubscriptionHandler) meGuardSubscription(ctx context.Context, subscriptionUUID string) (*models.Subscription, error) {
	owned, err := h.callerOwnerSet(ctx)
	if err != nil {
		return nil, err
	}
	sub, err := h.subs.Get(ctx, subscriptionUUID)
	if err != nil {
		return nil, err
	}
	if _, ok := owned[sub.Owner()]; !ok {
		return nil, huma.Error404NotFound("not found")
	}
	return sub, nil
}

// --- Self-service handlers ---

// MeListSubscriptionsRequest filters the caller's subscriptions to one
// owner when OwnerUUID is non-empty; otherwise returns subscriptions across
// every owner (the calling user plus every tenant they own).
type MeListSubscriptionsRequest struct {
	OwnerKind  string `query:"ownerKind" enum:"user,tenant" doc:"Optional — restrict to one owner kind"`
	OwnerUUID  string `query:"ownerUuid" doc:"Optional — restrict to one owned principal"`
	TenantUUID string `query:"tenantUuid" doc:"Deprecated alias for ownerKind=tenant + ownerUuid"`
	Status     string `query:"status" enum:"active,past_due,suspended,cancelled,expired"`
}

// MeList returns subscriptions across every owner the caller may act under.
// The repository is queried per-owner and the results are merged in memory;
// for the demo MVP a Tier-2 user is rarely an owner of more than a handful
// of principals, so a fan-out without an aggregate index is acceptable.
func (h *SubscriptionHandler) MeList(ctx context.Context, in *MeListSubscriptionsRequest) (*ListSubscriptionsResponse, error) {
	owned, err := h.callerOwnerSet(ctx)
	if err != nil {
		return nil, err
	}
	resp := &ListSubscriptionsResponse{}
	resp.Body.Items = []models.Subscription{}

	// Resolve the optional owner filter from either the explicit
	// (ownerKind, ownerUuid) pair or the legacy tenantUuid alias.
	var filterOwner iface.Owner
	if in.OwnerUUID != "" {
		owner, err := parseOwnerInput(in.OwnerKind, in.OwnerUUID)
		if err != nil {
			return nil, err
		}
		filterOwner = owner
	} else if in.TenantUUID != "" {
		filterOwner = iface.TenantOwner(in.TenantUUID)
	}

	if !filterOwner.IsZero() {
		if _, ok := owned[filterOwner]; !ok {
			// Pretend the principal does not exist rather than confirming
			// it does and refusing — same leak-prevention rationale as
			// the 404 on subscription mismatch.
			resp.Body.Total = 0
			return resp, nil
		}
		owned = map[iface.Owner]struct{}{filterOwner: {}}
	}

	for owner := range owned {
		items, err := h.subs.List(ctx, repository.SubscriptionFilters{
			Owner:  owner,
			Status: models.SubStatus(in.Status),
		})
		if err != nil {
			return nil, err
		}
		resp.Body.Items = append(resp.Body.Items, items...)
	}
	resp.Body.Total = len(resp.Body.Items)
	return resp, nil
}

type MeSubscriptionRequest struct {
	ID string `path:"id"`
}

func (h *SubscriptionHandler) MeGet(ctx context.Context, in *MeSubscriptionRequest) (*SubscriptionResponse, error) {
	sub, err := h.meGuardSubscription(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	return &SubscriptionResponse{Body: *sub}, nil
}

type MeCancelSubscriptionRequest struct {
	ID   string `path:"id"`
	Body struct {
		AtPeriodEnd bool `json:"atPeriodEnd"`
	}
}

func (h *SubscriptionHandler) MeCancel(ctx context.Context, in *MeCancelSubscriptionRequest) (*SubscriptionResponse, error) {
	if _, err := h.meGuardSubscription(ctx, in.ID); err != nil {
		return nil, err
	}
	sub, err := h.subs.Cancel(ctx, in.ID, in.Body.AtPeriodEnd, actorFrom(ctx))
	if err != nil {
		return nil, err
	}
	return &SubscriptionResponse{Body: *sub}, nil
}

func (h *SubscriptionHandler) MeReactivate(ctx context.Context, in *MeSubscriptionRequest) (*SubscriptionResponse, error) {
	if _, err := h.meGuardSubscription(ctx, in.ID); err != nil {
		return nil, err
	}
	sub, err := h.subs.Reactivate(ctx, in.ID, actorFrom(ctx))
	if err != nil {
		return nil, err
	}
	return &SubscriptionResponse{Body: *sub}, nil
}

func (h *SubscriptionHandler) MeListInvoices(ctx context.Context, in *MeSubscriptionRequest) (*ListInvoicesResponse, error) {
	if _, err := h.meGuardSubscription(ctx, in.ID); err != nil {
		return nil, err
	}
	items, err := h.invoices.List(ctx, repository.InvoiceFilters{SubscriptionUUID: in.ID})
	if err != nil {
		return nil, err
	}
	resp := &ListInvoicesResponse{}
	resp.Body.Items = items
	resp.Body.Total = len(items)
	return resp, nil
}

type MeListActivityRequest struct {
	ID    string `path:"id"`
	Limit int64  `query:"limit" default:"100" maximum:"500"`
}

func (h *SubscriptionHandler) MeListActivity(ctx context.Context, in *MeListActivityRequest) (*ListActivityResponse, error) {
	if _, err := h.meGuardSubscription(ctx, in.ID); err != nil {
		return nil, err
	}
	items, err := h.activity.List(ctx, in.ID, in.Limit)
	if err != nil {
		return nil, err
	}
	resp := &ListActivityResponse{}
	resp.Body.Items = items
	resp.Body.Total = len(items)
	return resp, nil
}

// parseOwnerInput validates and normalizes the wire (ownerKind, ownerUUID)
// pair into an iface.Owner. Empty kind defaults to "tenant" for backward
// compatibility with admin flows that pre-date the polymorphic-owner
// refactor.
func parseOwnerInput(kind, uuid string) (iface.Owner, error) {
	if uuid == "" {
		return iface.Owner{}, huma.Error400BadRequest("ownerUUID is required")
	}
	k := kind
	if k == "" {
		k = string(iface.OwnerKindTenant)
	}
	switch iface.OwnerKind(k) {
	case iface.OwnerKindUser, iface.OwnerKindTenant:
		return iface.Owner{Kind: iface.OwnerKind(k), UUID: uuid}, nil
	default:
		return iface.Owner{}, huma.Error400BadRequest("invalid ownerKind")
	}
}

// actorFrom returns the UUID of the authenticated user for audit-log
// attribution. Falls back to "system" when the middleware has not populated
// the context (background jobs, tests).
func actorFrom(ctx context.Context) string {
	if uuid, ok := middleware.GetUserUUID(ctx); ok && uuid != "" {
		return uuid
	}
	return "system"
}
