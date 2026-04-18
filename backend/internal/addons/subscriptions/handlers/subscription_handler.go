package handlers

import (
	"context"

	"github.com/orkestra/backend/internal/addons/subscriptions/models"
	"github.com/orkestra/backend/internal/addons/subscriptions/repository"
	"github.com/orkestra/backend/internal/addons/subscriptions/services"
	"github.com/orkestra/backend/internal/shared/middleware"
)

type SubscriptionHandler struct {
	subs      *services.SubscriptionService
	renewal   *services.RenewalService
	invoices  repository.InvoiceRepository
	activity  *services.ActivityService
	ownership *services.ClientOwnershipService
}

func NewSubscriptionHandler(
	subs *services.SubscriptionService,
	renewal *services.RenewalService,
	invoices repository.InvoiceRepository,
	activity *services.ActivityService,
	ownership *services.ClientOwnershipService,
) *SubscriptionHandler {
	return &SubscriptionHandler{
		subs:      subs,
		renewal:   renewal,
		invoices:  invoices,
		activity:  activity,
		ownership: ownership,
	}
}

type CreateSubscriptionInput struct {
	ClientUUID  string `json:"clientUUID"`
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
	ClientUUID  string `query:"clientUUID"`
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
	sub, err := h.subs.Create(ctx, in.Body.ClientUUID, in.Body.ServiceUUID, in.Body.TierCode, actorFrom(ctx))
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
	items, err := h.subs.List(ctx, repository.SubscriptionFilters{
		ClientUUID:  in.ClientUUID,
		ServiceUUID: in.ServiceUUID,
		Status:      models.SubStatus(in.Status),
	})
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

// guardSubscriptionOwnership resolves the owning client for the subscription
// and enforces the request's active org matches. Clients without an org
// binding (operator-managed, v1 default) are always allowed.
func (h *SubscriptionHandler) guardSubscriptionOwnership(ctx context.Context, sub *models.Subscription) error {
	if sub == nil || h.ownership == nil {
		return nil
	}
	orgUUID, err := h.ownership.GetClientOrgUUID(ctx, sub.ClientUUID)
	if err != nil {
		// Treat missing clients as not-found so we never leak existence of
		// out-of-scope records.
		return nil
	}
	return assertOrgOwnsClient(ctx, orgUUID)
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
