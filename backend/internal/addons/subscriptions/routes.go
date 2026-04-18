package subscriptions

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra/backend/internal/addons/subscriptions/handlers"
)

// Routes are split across permission buckets so mutating operations require
// the `.manage` grant rather than the broader `.view` grant. The module's
// RegisterRoutes wires each Register* function into a chi subgroup carrying
// the correct RequirePermission middleware.

var subscriptionsSec = []map[string][]string{{"bearerAuth": {}}}

// --- Services (catalog) ---

// RegisterServiceReadRoutes — gate with `subscriptions.service.view`.
func RegisterServiceReadRoutes(api huma.API, h *handlers.ServiceHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "subscriptions-list-services",
		Method:      http.MethodGet, Path: "/v1/subscriptions/services",
		Summary: "List catalog services", Tags: []string{"Subscriptions - Services"}, Security: subscriptionsSec,
	}, h.List)
	huma.Register(api, huma.Operation{
		OperationID: "subscriptions-get-service",
		Method:      http.MethodGet, Path: "/v1/subscriptions/services/{id}",
		Summary: "Get catalog service", Tags: []string{"Subscriptions - Services"}, Security: subscriptionsSec,
	}, h.Get)
}

// RegisterServiceWriteRoutes — gate with `subscriptions.service.manage`.
func RegisterServiceWriteRoutes(api huma.API, h *handlers.ServiceHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "subscriptions-create-service",
		Method:      http.MethodPost, Path: "/v1/subscriptions/services",
		Summary: "Create catalog service", Tags: []string{"Subscriptions - Services"}, Security: subscriptionsSec,
	}, h.Create)
	huma.Register(api, huma.Operation{
		OperationID: "subscriptions-update-service",
		Method:      http.MethodPatch, Path: "/v1/subscriptions/services/{id}",
		Summary: "Update catalog service", Tags: []string{"Subscriptions - Services"}, Security: subscriptionsSec,
	}, h.Update)
	huma.Register(api, huma.Operation{
		OperationID: "subscriptions-delete-service",
		Method:      http.MethodDelete, Path: "/v1/subscriptions/services/{id}",
		Summary: "Delete catalog service", Tags: []string{"Subscriptions - Services"}, Security: subscriptionsSec,
	}, h.Delete)
}

// --- Clients ---

// RegisterClientReadRoutes — gate with `subscriptions.client.view`.
func RegisterClientReadRoutes(api huma.API, h *handlers.ClientHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "subscriptions-list-clients",
		Method:      http.MethodGet, Path: "/v1/subscriptions/clients",
		Summary: "List clients", Tags: []string{"Subscriptions - Clients"}, Security: subscriptionsSec,
	}, h.List)
	huma.Register(api, huma.Operation{
		OperationID: "subscriptions-get-client",
		Method:      http.MethodGet, Path: "/v1/subscriptions/clients/{id}",
		Summary: "Get client", Tags: []string{"Subscriptions - Clients"}, Security: subscriptionsSec,
	}, h.Get)
}

// RegisterClientWriteRoutes — gate with `subscriptions.client.manage`.
func RegisterClientWriteRoutes(api huma.API, h *handlers.ClientHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "subscriptions-create-client",
		Method:      http.MethodPost, Path: "/v1/subscriptions/clients",
		Summary: "Create client", Tags: []string{"Subscriptions - Clients"}, Security: subscriptionsSec,
	}, h.Create)
	huma.Register(api, huma.Operation{
		OperationID: "subscriptions-update-client",
		Method:      http.MethodPatch, Path: "/v1/subscriptions/clients/{id}",
		Summary: "Update client", Tags: []string{"Subscriptions - Clients"}, Security: subscriptionsSec,
	}, h.Update)
	huma.Register(api, huma.Operation{
		OperationID: "subscriptions-archive-client",
		Method:      http.MethodDelete, Path: "/v1/subscriptions/clients/{id}",
		Summary: "Archive client", Tags: []string{"Subscriptions - Clients"}, Security: subscriptionsSec,
	}, h.Archive)
}

// --- Subscriptions ---

// RegisterSubscriptionReadRoutes — gate with `subscriptions.subscription.view`.
func RegisterSubscriptionReadRoutes(api huma.API, h *handlers.SubscriptionHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "subscriptions-list",
		Method:      http.MethodGet, Path: "/v1/subscriptions/subscriptions",
		Summary: "List subscriptions", Tags: []string{"Subscriptions"}, Security: subscriptionsSec,
	}, h.List)
	huma.Register(api, huma.Operation{
		OperationID: "subscriptions-get",
		Method:      http.MethodGet, Path: "/v1/subscriptions/subscriptions/{id}",
		Summary: "Get subscription", Tags: []string{"Subscriptions"}, Security: subscriptionsSec,
	}, h.Get)
}

// RegisterSubscriptionWriteRoutes — gate with `subscriptions.subscription.manage`.
func RegisterSubscriptionWriteRoutes(api huma.API, h *handlers.SubscriptionHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "subscriptions-create",
		Method:      http.MethodPost, Path: "/v1/subscriptions/subscriptions",
		Summary: "Create subscription", Tags: []string{"Subscriptions"}, Security: subscriptionsSec,
	}, h.Create)
	huma.Register(api, huma.Operation{
		OperationID: "subscriptions-cancel",
		Method:      http.MethodPost, Path: "/v1/subscriptions/subscriptions/{id}/cancel",
		Summary: "Cancel subscription", Tags: []string{"Subscriptions"}, Security: subscriptionsSec,
	}, h.Cancel)
	huma.Register(api, huma.Operation{
		OperationID: "subscriptions-reactivate",
		Method:      http.MethodPost, Path: "/v1/subscriptions/subscriptions/{id}/reactivate",
		Summary: "Reactivate subscription", Tags: []string{"Subscriptions"}, Security: subscriptionsSec,
	}, h.Reactivate)
	huma.Register(api, huma.Operation{
		OperationID: "subscriptions-retry-charge",
		Method:      http.MethodPost, Path: "/v1/subscriptions/subscriptions/{id}/retry-charge",
		Summary: "Retry charge", Tags: []string{"Subscriptions"}, Security: subscriptionsSec,
	}, h.RetryCharge)
}

// --- Nested reads ---

// RegisterInvoiceReadRoutes — gate with `subscriptions.invoice.view`.
func RegisterInvoiceReadRoutes(api huma.API, h *handlers.SubscriptionHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "subscriptions-list-invoices",
		Method:      http.MethodGet, Path: "/v1/subscriptions/subscriptions/{id}/invoices",
		Summary: "List invoices for subscription", Tags: []string{"Subscriptions - Invoices"}, Security: subscriptionsSec,
	}, h.ListInvoices)
}

// RegisterActivityReadRoutes — gate with `subscriptions.activity.view`.
func RegisterActivityReadRoutes(api huma.API, h *handlers.SubscriptionHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "subscriptions-list-activity",
		Method:      http.MethodGet, Path: "/v1/subscriptions/subscriptions/{id}/activity",
		Summary: "List activity log for subscription", Tags: []string{"Subscriptions - Activity"}, Security: subscriptionsSec,
	}, h.ListActivity)
}
