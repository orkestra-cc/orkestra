package subscriptions

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra/backend/internal/addons/subscriptions/handlers"
)

func RegisterRoutes(
	api huma.API,
	svcH *handlers.ServiceHandler,
	clientH *handlers.ClientHandler,
	subH *handlers.SubscriptionHandler,
) {
	sec := []map[string][]string{{"bearerAuth": {}}}

	// --- Services (catalog) ---
	huma.Register(api, huma.Operation{
		OperationID: "subscriptions-create-service",
		Method:      http.MethodPost, Path: "/v1/subscriptions/services",
		Summary: "Create catalog service", Tags: []string{"Subscriptions - Services"}, Security: sec,
	}, svcH.Create)
	huma.Register(api, huma.Operation{
		OperationID: "subscriptions-list-services",
		Method:      http.MethodGet, Path: "/v1/subscriptions/services",
		Summary: "List catalog services", Tags: []string{"Subscriptions - Services"}, Security: sec,
	}, svcH.List)
	huma.Register(api, huma.Operation{
		OperationID: "subscriptions-get-service",
		Method:      http.MethodGet, Path: "/v1/subscriptions/services/{id}",
		Summary: "Get catalog service", Tags: []string{"Subscriptions - Services"}, Security: sec,
	}, svcH.Get)
	huma.Register(api, huma.Operation{
		OperationID: "subscriptions-update-service",
		Method:      http.MethodPatch, Path: "/v1/subscriptions/services/{id}",
		Summary: "Update catalog service", Tags: []string{"Subscriptions - Services"}, Security: sec,
	}, svcH.Update)
	huma.Register(api, huma.Operation{
		OperationID: "subscriptions-delete-service",
		Method:      http.MethodDelete, Path: "/v1/subscriptions/services/{id}",
		Summary: "Delete catalog service", Tags: []string{"Subscriptions - Services"}, Security: sec,
	}, svcH.Delete)

	// --- Clients ---
	huma.Register(api, huma.Operation{
		OperationID: "subscriptions-create-client",
		Method:      http.MethodPost, Path: "/v1/subscriptions/clients",
		Summary: "Create client", Tags: []string{"Subscriptions - Clients"}, Security: sec,
	}, clientH.Create)
	huma.Register(api, huma.Operation{
		OperationID: "subscriptions-list-clients",
		Method:      http.MethodGet, Path: "/v1/subscriptions/clients",
		Summary: "List clients", Tags: []string{"Subscriptions - Clients"}, Security: sec,
	}, clientH.List)
	huma.Register(api, huma.Operation{
		OperationID: "subscriptions-get-client",
		Method:      http.MethodGet, Path: "/v1/subscriptions/clients/{id}",
		Summary: "Get client", Tags: []string{"Subscriptions - Clients"}, Security: sec,
	}, clientH.Get)
	huma.Register(api, huma.Operation{
		OperationID: "subscriptions-update-client",
		Method:      http.MethodPatch, Path: "/v1/subscriptions/clients/{id}",
		Summary: "Update client", Tags: []string{"Subscriptions - Clients"}, Security: sec,
	}, clientH.Update)
	huma.Register(api, huma.Operation{
		OperationID: "subscriptions-archive-client",
		Method:      http.MethodDelete, Path: "/v1/subscriptions/clients/{id}",
		Summary: "Archive client", Tags: []string{"Subscriptions - Clients"}, Security: sec,
	}, clientH.Archive)

	// --- Subscriptions ---
	huma.Register(api, huma.Operation{
		OperationID: "subscriptions-create",
		Method:      http.MethodPost, Path: "/v1/subscriptions/subscriptions",
		Summary: "Create subscription", Tags: []string{"Subscriptions"}, Security: sec,
	}, subH.Create)
	huma.Register(api, huma.Operation{
		OperationID: "subscriptions-list",
		Method:      http.MethodGet, Path: "/v1/subscriptions/subscriptions",
		Summary: "List subscriptions", Tags: []string{"Subscriptions"}, Security: sec,
	}, subH.List)
	huma.Register(api, huma.Operation{
		OperationID: "subscriptions-get",
		Method:      http.MethodGet, Path: "/v1/subscriptions/subscriptions/{id}",
		Summary: "Get subscription", Tags: []string{"Subscriptions"}, Security: sec,
	}, subH.Get)
	huma.Register(api, huma.Operation{
		OperationID: "subscriptions-cancel",
		Method:      http.MethodPost, Path: "/v1/subscriptions/subscriptions/{id}/cancel",
		Summary: "Cancel subscription", Tags: []string{"Subscriptions"}, Security: sec,
	}, subH.Cancel)
	huma.Register(api, huma.Operation{
		OperationID: "subscriptions-reactivate",
		Method:      http.MethodPost, Path: "/v1/subscriptions/subscriptions/{id}/reactivate",
		Summary: "Reactivate subscription", Tags: []string{"Subscriptions"}, Security: sec,
	}, subH.Reactivate)
	huma.Register(api, huma.Operation{
		OperationID: "subscriptions-retry-charge",
		Method:      http.MethodPost, Path: "/v1/subscriptions/subscriptions/{id}/retry-charge",
		Summary: "Retry charge", Tags: []string{"Subscriptions"}, Security: sec,
	}, subH.RetryCharge)

	// --- Nested reads ---
	huma.Register(api, huma.Operation{
		OperationID: "subscriptions-list-invoices",
		Method:      http.MethodGet, Path: "/v1/subscriptions/subscriptions/{id}/invoices",
		Summary: "List invoices for subscription", Tags: []string{"Subscriptions - Invoices"}, Security: sec,
	}, subH.ListInvoices)
	huma.Register(api, huma.Operation{
		OperationID: "subscriptions-list-activity",
		Method:      http.MethodGet, Path: "/v1/subscriptions/subscriptions/{id}/activity",
		Summary: "List activity log for subscription", Tags: []string{"Subscriptions - Activity"}, Security: sec,
	}, subH.ListActivity)
}
