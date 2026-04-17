package payments

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra/backend/internal/addons/payments/handlers"
)

func RegisterRoutes(api huma.API, h *handlers.TransactionHandler) {
	sec := []map[string][]string{{"bearerAuth": {}}}

	huma.Register(api, huma.Operation{
		OperationID: "payments-list-transactions",
		Method:      http.MethodGet, Path: "/v1/payments/transactions",
		Summary: "List payment transactions", Tags: []string{"Payments - Transactions"}, Security: sec,
	}, h.List)
	huma.Register(api, huma.Operation{
		OperationID: "payments-get-transaction",
		Method:      http.MethodGet, Path: "/v1/payments/transactions/{id}",
		Summary: "Get transaction", Tags: []string{"Payments - Transactions"}, Security: sec,
	}, h.Get)
	huma.Register(api, huma.Operation{
		OperationID: "payments-refund-transaction",
		Method:      http.MethodPost, Path: "/v1/payments/transactions/{id}/refund",
		Summary: "Refund transaction", Tags: []string{"Payments - Transactions"}, Security: sec,
	}, h.Refund)
	huma.Register(api, huma.Operation{
		OperationID: "payments-list-payment-methods",
		Method:      http.MethodGet, Path: "/v1/payments/methods",
		Summary: "List payment methods for client", Tags: []string{"Payments - Methods"}, Security: sec,
	}, h.ListPaymentMethods)
	huma.Register(api, huma.Operation{
		OperationID: "payments-list-webhook-events",
		Method:      http.MethodGet, Path: "/v1/payments/webhook-events",
		Summary: "List webhook events (audit)", Tags: []string{"Payments - Webhooks"}, Security: sec,
	}, h.ListWebhookEvents)
}
