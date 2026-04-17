package handlers

import (
	"context"

	"github.com/orkestra/backend/internal/addons/payments/models"
	"github.com/orkestra/backend/internal/addons/payments/repository"
	"github.com/orkestra/backend/internal/addons/payments/services"
	"github.com/orkestra/backend/internal/shared/iface"
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
	return &TransactionHandler{txRepo: txRepo, pmRepo: pmRepo, whRepo: whRepo, payment: payment}
}

type ListTransactionsRequest struct {
	SubscriptionUUID string `query:"subscriptionUUID"`
	InvoiceUUID      string `query:"invoiceUUID"`
	ClientUUID       string `query:"clientUUID"`
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
		AmountCents int64  `json:"amountCents" doc:"Zero refunds the full amount"`
		Reason      string `json:"reason,omitempty"`
	}
}
type RefundResponse struct {
	Body struct {
		ProviderRefundID string `json:"providerRefundID"`
		Status           string `json:"status"`
	}
}

type ListPaymentMethodsRequest struct {
	ClientUUID string `query:"clientUUID" required:"true"`
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
		ClientUUID:       in.ClientUUID,
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
	return &TransactionResponse{Body: *t}, nil
}

func (h *TransactionHandler) Refund(ctx context.Context, in *RefundRequest) (*RefundResponse, error) {
	tx, err := h.txRepo.GetByUUID(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	res, err := h.payment.RefundCharge(ctx, tx.ProviderTxID, in.Body.AmountCents, in.Body.Reason)
	if err != nil {
		return nil, err
	}
	resp := &RefundResponse{}
	resp.Body.ProviderRefundID = res.ProviderRefundID
	resp.Body.Status = res.Status
	return resp, nil
}

func (h *TransactionHandler) ListPaymentMethods(ctx context.Context, in *ListPaymentMethodsRequest) (*ListPaymentMethodsResponse, error) {
	items, err := h.pmRepo.ListByClient(ctx, in.ClientUUID)
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

// Compile-time check that PaymentService satisfies iface.PaymentProvider.
var _ iface.PaymentProvider = (*services.PaymentService)(nil)
