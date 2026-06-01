package handlers

import (
	"context"
	"errors"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra/backend/internal/addons/subscriptions/models"
	"github.com/orkestra/backend/internal/addons/subscriptions/repository"
	"github.com/orkestra/backend/internal/addons/subscriptions/services"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

type SubscriptionHandler struct {
	subs     *services.SubscriptionService
	renewal  *services.RenewalService
	invoices repository.InvoiceRepository
	activity *services.ActivityService
	// tenants is required for all self-service flows: every caller acts under
	// at least one tenant (their personal tenant, materialized lazily by
	// EnsureTenantForUser). Tenant-owner self-subscribe also enforces
	// ownership via ListUserMemberships.
	tenants iface.TenantProvider
}

func NewSubscriptionHandler(
	subs *services.SubscriptionService,
	renewal *services.RenewalService,
	invoices repository.InvoiceRepository,
	activity *services.ActivityService,
	tenants iface.TenantProvider,
) *SubscriptionHandler {
	return &SubscriptionHandler{
		subs:     subs,
		renewal:  renewal,
		invoices: invoices,
		activity: activity,
		tenants:  tenants,
	}
}

// CreateSubscriptionInput is the operator-admin payload. Subscriptions are
// owned by a Tenant aggregate.
type CreateSubscriptionInput struct {
	TenantUUID  string `json:"tenantUUID" doc:"UUID of the tenant the subscription belongs to"`
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
	TenantUUID  string `query:"tenantUUID"`
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
	if in.Body.TenantUUID == "" {
		return nil, huma.Error400BadRequest("tenantUUID is required")
	}
	if in.Body.ServiceUUID == "" || in.Body.TierCode == "" {
		return nil, huma.Error400BadRequest("serviceUUID and tierCode are required")
	}
	sub, err := h.subs.Create(ctx, in.Body.TenantUUID, in.Body.ServiceUUID, in.Body.TierCode, actorFrom(ctx))
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
	if err := h.guardSubscriptionTenantScope(ctx, sub); err != nil {
		return nil, err
	}
	return &SubscriptionResponse{Body: *sub}, nil
}

func (h *SubscriptionHandler) List(ctx context.Context, in *ListSubscriptionsRequest) (*ListSubscriptionsResponse, error) {
	filter := repository.SubscriptionFilters{
		TenantUUID:  in.TenantUUID,
		ServiceUUID: in.ServiceUUID,
		Status:      models.SubStatus(in.Status),
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
	if err := h.guardSubscriptionTenantScope(ctx, existing); err != nil {
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
	if err := h.guardSubscriptionTenantScope(ctx, existing); err != nil {
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
	if err := h.guardSubscriptionTenantScope(ctx, existing); err != nil {
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
	if err := h.guardSubscriptionTenantScope(ctx, sub); err != nil {
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
	if err := h.guardSubscriptionTenantScope(ctx, sub); err != nil {
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
// subscribe endpoint. The default scope is the caller's personal tenant
// (created lazily); pass `tenantUuid` to subscribe under a tenant the
// caller owns.
type SelfSubscribeInput struct {
	TenantUUID  string `json:"tenantUuid,omitempty" doc:"Optional — UUID of a tenant the caller owns. Defaults to the caller's personal tenant."`
	ServiceCode string `json:"serviceCode" doc:"Stable SKU code of a catalog service (e.g. 'pro')"`
	TierCode    string `json:"tierCode" doc:"Tier code within the service (e.g. 'monthly')"`
}

type SelfSubscribeRequest struct {
	Body SelfSubscribeInput
}

// SelfSubscribe creates a subscription owned by the caller's personal
// tenant or by a tenant the caller owns. The entitlement syncer grants
// capabilities on activation so the owner can hit RequireCapability-gated
// addons immediately; the first payment attempt still happens on the next
// renewal tick.
func (h *SubscriptionHandler) SelfSubscribe(ctx context.Context, req *SelfSubscribeRequest) (*SubscriptionResponse, error) {
	userUUID, ok := ctxauth.GetUserUUID(ctx)
	if !ok || userUUID == "" {
		return nil, huma.Error401Unauthorized("authentication required")
	}
	if req.Body.ServiceCode == "" || req.Body.TierCode == "" {
		return nil, huma.Error400BadRequest("serviceCode and tierCode are required")
	}
	if h.tenants == nil {
		return nil, huma.Error503ServiceUnavailable("tenant provider not configured")
	}

	tenantUUID := req.Body.TenantUUID
	if tenantUUID == "" {
		// Default scope: the caller's personal tenant (created lazily).
		personal, err := h.tenants.EnsureTenantForUser(ctx, userUUID)
		if err != nil {
			return nil, err
		}
		if personal == nil || personal.UUID == "" {
			return nil, huma.Error500InternalServerError("failed to materialize personal tenant")
		}
		tenantUUID = personal.UUID
	} else {
		memberships, err := h.tenants.ListUserMemberships(ctx, userUUID)
		if err != nil {
			return nil, err
		}
		owns := false
		for _, m := range memberships {
			if m.TenantUUID == tenantUUID && m.IsOwner {
				owns = true
				break
			}
		}
		if !owns {
			return nil, huma.Error403Forbidden("caller is not owner of requested tenant")
		}
	}

	sub, err := h.subs.CreateForTenant(ctx, tenantUUID, req.Body.ServiceCode, req.Body.TierCode, userUUID)
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

// guardSubscriptionTenantScope enforces that the request's active tenant
// matches the subscription's tenant. Returns 404 on mismatch so existence
// of out-of-scope records is not leaked.
func (h *SubscriptionHandler) guardSubscriptionTenantScope(ctx context.Context, sub *models.Subscription) error {
	if sub == nil || sub.TenantUUID == "" {
		return nil
	}
	tenantID, hasTenant := ctxauth.GetTenantID(ctx)
	if !hasTenant {
		return nil
	}
	if sub.TenantUUID != tenantID {
		return huma.Error404NotFound("not found", nil)
	}
	return nil
}

// callerTenantSet returns the set of tenant UUIDs the caller may act under:
// the caller's personal tenant (materialized lazily) plus every tenant they
// own. Returns 401 when anonymous, 503 when the tenant provider is missing.
func (h *SubscriptionHandler) callerTenantSet(ctx context.Context) (map[string]struct{}, error) {
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

// meGuardSubscription resolves the subscription by UUID and returns it iff
// the caller owns it (via the personal tenant or a tenant membership).
// Returns 404 (not 403) on ownership mismatch so the existence of
// out-of-scope subscriptions does not leak to a fishing client.
func (h *SubscriptionHandler) meGuardSubscription(ctx context.Context, subscriptionUUID string) (*models.Subscription, error) {
	owned, err := h.callerTenantSet(ctx)
	if err != nil {
		return nil, err
	}
	sub, err := h.subs.Get(ctx, subscriptionUUID)
	if err != nil {
		return nil, err
	}
	if _, ok := owned[sub.TenantUUID]; !ok {
		return nil, huma.Error404NotFound("not found")
	}
	return sub, nil
}

// --- Self-service handlers ---

// MeListSubscriptionsRequest filters the caller's subscriptions to one
// tenant when TenantUUID is non-empty; otherwise returns subscriptions
// across every tenant the caller may act under.
type MeListSubscriptionsRequest struct {
	TenantUUID string `query:"tenantUuid" doc:"Optional — restrict to one owned tenant"`
	Status     string `query:"status" enum:"active,past_due,suspended,cancelled,expired"`
}

// MeList returns subscriptions across every tenant the caller may act
// under. The repository is queried per-tenant and the results merged in
// memory; for the demo MVP a Tier-2 user is rarely an owner of more than
// a handful of tenants, so a fan-out without an aggregate index is fine.
func (h *SubscriptionHandler) MeList(ctx context.Context, in *MeListSubscriptionsRequest) (*ListSubscriptionsResponse, error) {
	owned, err := h.callerTenantSet(ctx)
	if err != nil {
		return nil, err
	}
	resp := &ListSubscriptionsResponse{}
	resp.Body.Items = []models.Subscription{}

	if in.TenantUUID != "" {
		if _, ok := owned[in.TenantUUID]; !ok {
			resp.Body.Total = 0
			return resp, nil
		}
		owned = map[string]struct{}{in.TenantUUID: {}}
	}

	for tenantUUID := range owned {
		items, err := h.subs.List(ctx, repository.SubscriptionFilters{
			TenantUUID: tenantUUID,
			Status:     models.SubStatus(in.Status),
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

// actorFrom returns the UUID of the authenticated user for audit-log
// attribution. Falls back to "system" when the middleware has not populated
// the context (background jobs, tests).
func actorFrom(ctx context.Context) string {
	if uuid, ok := ctxauth.GetUserUUID(ctx); ok && uuid != "" {
		return uuid
	}
	return "system"
}
