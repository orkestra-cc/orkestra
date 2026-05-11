package payments

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra/backend/internal/addons/payments/handlers"
)

// RegisterClientSelfServiceRoutes mounts the Tier-2 self-service payment
// endpoints on the ADR-0003 client API surface (api.*). The api parameter
// must be backed by a router that already applies RequireGlobal() — this
// function does not gate by RBAC permission; every handler re-checks
// tenant ownership against TenantProvider.ListUserMemberships internally.
func RegisterClientSelfServiceRoutes(api huma.API, h *handlers.ClientHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "me-list-transactions",
		Method:      http.MethodGet, Path: "/v1/me/transactions",
		Summary:  "List payment transactions across tenants the caller owns",
		Tags:     []string{"Payments - Self-service"},
		Security: paymentsSec,
	}, h.MeListTransactions)

	huma.Register(api, huma.Operation{
		OperationID: "me-list-payment-methods",
		Method:      http.MethodGet, Path: "/v1/me/payment-methods",
		Summary:  "List stored payment methods across tenants the caller owns",
		Tags:     []string{"Payments - Self-service"},
		Security: paymentsSec,
	}, h.MeListPaymentMethods)

	huma.Register(api, huma.Operation{
		OperationID: "me-create-checkout-session",
		Method:      http.MethodPost, Path: "/v1/me/payments/checkout-session",
		Summary:  "Open a Stripe Checkout session for a subscription's pending invoice",
		Tags:     []string{"Payments - Self-service"},
		Security: paymentsSec,
	}, h.MeCreateCheckoutSession)

	huma.Register(api, huma.Operation{
		OperationID: "me-create-setup-checkout-session",
		Method:      http.MethodPost, Path: "/v1/me/payments/setup-checkout-session",
		Summary:  "Open a Stripe Checkout session in setup mode to save a payment method",
		Tags:     []string{"Payments - Self-service"},
		Security: paymentsSec,
	}, h.MeCreateSetupCheckoutSession)
}
