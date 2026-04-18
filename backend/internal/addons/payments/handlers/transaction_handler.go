package handlers

import (
	"context"
	"errors"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra/backend/internal/addons/payments/models"
	"github.com/orkestra/backend/internal/addons/payments/repository"
	"github.com/orkestra/backend/internal/addons/payments/services"
	"github.com/orkestra/backend/internal/shared/iface"
	"github.com/orkestra/backend/internal/shared/module"
)

type TransactionHandler struct {
	txRepo  repository.TransactionRepository
	pmRepo  repository.PaymentMethodRepository
	whRepo  repository.WebhookEventRepository
	payment *services.PaymentService
	// svcReg is consulted on every request so toggling subscriptions on/off
	// via the admin UI immediately enables/disables the org-ownership guard.
	svcReg *module.ServiceRegistry
}

func NewTransactionHandler(
	txRepo repository.TransactionRepository,
	pmRepo repository.PaymentMethodRepository,
	whRepo repository.WebhookEventRepository,
	payment *services.PaymentService,
	svcReg *module.ServiceRegistry,
) *TransactionHandler {
	return &TransactionHandler{
		txRepo:  txRepo,
		pmRepo:  pmRepo,
		whRepo:  whRepo,
		payment: payment,
		svcReg:  svcReg,
	}
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
	if err := assertOrgOwnsClient(ctx, h.svcReg, t.ClientUUID); err != nil {
		return nil, err
	}
	return &TransactionResponse{Body: *t}, nil
}

func (h *TransactionHandler) Refund(ctx context.Context, in *RefundRequest) (*RefundResponse, error) {
	tx, err := h.txRepo.GetByUUID(ctx, in.ID)
	if err != nil {
		return nil, huma.Error404NotFound("transaction not found", err)
	}
	if err := assertOrgOwnsClient(ctx, h.svcReg, tx.ClientUUID); err != nil {
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
	if err := assertOrgOwnsClient(ctx, h.svcReg, in.ClientUUID); err != nil {
		return nil, err
	}
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
