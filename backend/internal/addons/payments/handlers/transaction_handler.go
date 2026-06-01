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

type TransactionHandler struct {
	txRepo  repository.TransactionRepository
	pmRepo  repository.PaymentMethodRepository
	whRepo  repository.WebhookEventRepository
	payment *services.PaymentService
}

func NewTransactionHandler(
	txRepo repository.TransactionRepository,
	pmRepo repository.PaymentMethodRepository,
	whRepo repository.WebhookEventRepository,
	payment *services.PaymentService,
) *TransactionHandler {
	return &TransactionHandler{
		txRepo:  txRepo,
		pmRepo:  pmRepo,
		whRepo:  whRepo,
		payment: payment,
	}
}

type ListTransactionsRequest struct {
	SubscriptionUUID string `query:"subscriptionUUID"`
	InvoiceUUID      string `query:"invoiceUUID"`
	TenantUUID       string `query:"tenantUUID"`
	Status           string `query:"status" enum:"pending,requires_action,succeeded,failed,refunded,partially_refunded"`
}
type ListTransactionsResponse struct {
	Body struct {
		Items []models.Transaction `json:"items"`
		Total int                  `json:"total"`
	}
}
type GetTransactionRequest struct {
	ID string `path:"id"`
}
type TransactionResponse struct {
	Body models.Transaction
}

type RefundRequest struct {
	ID   string `path:"id"`
	Body struct {
		AmountCents int64  `json:"amountCents" minimum:"0" doc:"Zero refunds the full remaining amount"`
		Reason      string `json:"reason,omitempty" maxLength:"500"`
	}
}
type RefundResponse struct {
	Body struct {
		ProviderRefundID string `json:"providerRefundID"`
		Status           string `json:"status"`
	}
}

type ListPaymentMethodsRequest struct {
	TenantUUID string `query:"tenantUUID"`
}
type ListPaymentMethodsResponse struct {
	Body struct {
		Items []models.PaymentMethod `json:"items"`
		Total int                    `json:"total"`
	}
}

type ListWebhookEventsRequest struct {
	Provider string `query:"provider" enum:"stripe,paypal"`
	Limit    int64  `query:"limit" default:"100" maximum:"500"`
}
type ListWebhookEventsResponse struct {
	Body struct {
		Items []models.WebhookEvent `json:"items"`
		Total int                   `json:"total"`
	}
}

func (h *TransactionHandler) List(ctx context.Context, in *ListTransactionsRequest) (*ListTransactionsResponse, error) {
	items, err := h.txRepo.List(ctx, repository.TransactionFilters{
		SubscriptionUUID: in.SubscriptionUUID,
		InvoiceUUID:      in.InvoiceUUID,
		TenantUUID:       in.TenantUUID,
		Status:           models.TransactionStatus(in.Status),
	})
	if err != nil {
		return nil, err
	}
	resp := &ListTransactionsResponse{}
	resp.Body.Items = items
	resp.Body.Total = len(items)
	return resp, nil
}

func (h *TransactionHandler) Get(ctx context.Context, in *GetTransactionRequest) (*TransactionResponse, error) {
	t, err := h.txRepo.GetByUUID(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	if err := assertTenantScope(ctx, t.TenantUUID); err != nil {
		return nil, err
	}
	return &TransactionResponse{Body: *t}, nil
}

func (h *TransactionHandler) Refund(ctx context.Context, in *RefundRequest) (*RefundResponse, error) {
	tx, err := h.txRepo.GetByUUID(ctx, in.ID)
	if err != nil {
		return nil, huma.Error404NotFound("transaction not found", err)
	}
	if err := assertTenantScope(ctx, tx.TenantUUID); err != nil {
		return nil, err
	}
	if in.Body.AmountCents < 0 {
		return nil, huma.Error400BadRequest("refund amount cannot be negative", nil)
	}
	if tx.Status == models.TxRefunded {
		return nil, huma.Error409Conflict("transaction is already fully refunded", nil)
	}
	remaining := tx.AmountCents - tx.RefundedCents
	if remaining <= 0 {
		return nil, huma.Error409Conflict("transaction has no remaining balance to refund", nil)
	}
	if in.Body.AmountCents > remaining {
		return nil, huma.Error400BadRequest("refund amount exceeds remaining balance", nil)
	}
	res, err := h.payment.RefundCharge(ctx, tx.ProviderTxID, in.Body.AmountCents, in.Body.Reason)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrRefundAmountNegative),
			errors.Is(err, services.ErrRefundExceedsBalance):
			return nil, huma.Error400BadRequest(err.Error(), nil)
		case errors.Is(err, services.ErrAlreadyRefunded):
			return nil, huma.Error409Conflict(err.Error(), nil)
		}
		return nil, err
	}
	resp := &RefundResponse{}
	resp.Body.ProviderRefundID = res.ProviderRefundID
	resp.Body.Status = res.Status
	return resp, nil
}

func (h *TransactionHandler) ListPaymentMethods(ctx context.Context, in *ListPaymentMethodsRequest) (*ListPaymentMethodsResponse, error) {
	if in.TenantUUID == "" {
		return nil, huma.Error400BadRequest("tenantUUID is required", nil)
	}
	if err := assertTenantScope(ctx, in.TenantUUID); err != nil {
		return nil, err
	}
	items, err := h.pmRepo.ListByTenant(ctx, in.TenantUUID)
	if err != nil {
		return nil, err
	}
	resp := &ListPaymentMethodsResponse{}
	resp.Body.Items = items
	resp.Body.Total = len(items)
	return resp, nil
}

func (h *TransactionHandler) ListWebhookEvents(ctx context.Context, in *ListWebhookEventsRequest) (*ListWebhookEventsResponse, error) {
	items, err := h.whRepo.List(ctx, models.ProviderName(in.Provider), in.Limit)
	if err != nil {
		return nil, err
	}
	resp := &ListWebhookEventsResponse{}
	resp.Body.Items = items
	resp.Body.Total = len(items)
	return resp, nil
}

// assertTenantScope enforces that the request's active tenant matches the
// row's tenant. Returns 404 on mismatch so existence of out-of-scope records
// is not leaked.
func assertTenantScope(ctx context.Context, tenantUUID string) error {
	if tenantUUID == "" {
		return nil
	}
	requestTenant, hasTenant := ctxauth.GetTenantID(ctx)
	if !hasTenant {
		return nil
	}
	if tenantUUID != requestTenant {
		return huma.Error404NotFound("not found", nil)
	}
	return nil
}

// Compile-time check that PaymentService satisfies iface.PaymentProvider.
var _ iface.PaymentProvider = (*services.PaymentService)(nil)
