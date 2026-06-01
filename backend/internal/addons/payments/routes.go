package payments

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra/backend/internal/addons/payments/handlers"
)

// Routes are split across four permission groups so that mutating operations
// (refunds) require a dedicated permission rather than the broad transaction
// view grant. Each group is registered on a distinct humachi.API backed by a
// chi subrouter carrying its own RequirePermission middleware.

var paymentsSec = []map[string][]string{{"bearerAuth": {}}}

// RegisterTransactionReadRoutes exposes read-only transaction endpoints.
// Gate with `payments.transaction.view`.
func RegisterTransactionReadRoutes(api huma.API, h *handlers.TransactionHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "payments-list-transactions",
		Method:      http.MethodGet, Path: "/v1/payments/transactions",
		Summary: "List payment transactions", Tags: []string{"Payments - Transactions"}, Security: paymentsSec,
	}, h.List)
	huma.Register(api, huma.Operation{
		OperationID: "payments-get-transaction",
		Method:      http.MethodGet, Path: "/v1/payments/transactions/{id}",
		Summary: "Get transaction", Tags: []string{"Payments - Transactions"}, Security: paymentsSec,
	}, h.Get)
}

// RegisterTransactionRefundRoutes exposes the refund operation.
// Gate with `payments.transaction.refund`.
func RegisterTransactionRefundRoutes(api huma.API, h *handlers.TransactionHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "payments-refund-transaction",
		Method:      http.MethodPost, Path: "/v1/payments/transactions/{id}/refund",
		Summary: "Refund transaction", Tags: []string{"Payments - Transactions"}, Security: paymentsSec,
	}, h.Refund)
}

// RegisterPaymentMethodReadRoutes exposes read-only payment-method endpoints.
// Gate with `payments.method.view`.
func RegisterPaymentMethodReadRoutes(api huma.API, h *handlers.TransactionHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "payments-list-payment-methods",
		Method:      http.MethodGet, Path: "/v1/payments/methods",
		Summary: "List payment methods for client", Tags: []string{"Payments - Methods"}, Security: paymentsSec,
	}, h.ListPaymentMethods)
}

// RegisterWebhookEventReadRoutes exposes the webhook event audit log.
// Gate with `payments.webhook.view`.
func RegisterWebhookEventReadRoutes(api huma.API, h *handlers.TransactionHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "payments-list-webhook-events",
		Method:      http.MethodGet, Path: "/v1/payments/webhook-events",
		Summary: "List webhook events (audit)", Tags: []string{"Payments - Webhooks"}, Security: paymentsSec,
	}, h.ListWebhookEvents)
}
