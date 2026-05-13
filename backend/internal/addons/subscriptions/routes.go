package subscriptions

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra-cc/orkestra-addon-subscriptions/handlers"
)

// Routes are split across permission buckets so mutating operations require
// the `.manage` grant rather than the broader `.view` grant. The module's
// RegisterRoutes wires each Register* function into a chi subgroup carrying
// the correct RequirePermission middleware.

var subscriptionsSec = []map[string][]string{{"bearerAuth": {}}}

// --- Public catalog (no auth) ---

// RegisterPublicCatalogRoutes mounts the anonymous pricing endpoint on the
// public API so the signup UI can render plans before any credentials
// exist. The payload is the PublicCatalog* projection, not the full
// admin Service model, so metadata/timestamps never leak to anonymous
// callers.
func RegisterPublicCatalogRoutes(api huma.API, h *handlers.ServiceHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "public-catalog-list-services",
		Method:      http.MethodGet, Path: "/v1/public/catalog/services",
		Summary: "List active catalog services (anonymous)", Tags: []string{"Public Catalog"},
	}, h.PublicList)
}

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

// --- Self-service (tenant-owner, no per-route permission) ---

// RegisterSelfServiceRoutes mounts /v1/me/subscriptions on a router the
// caller has already gated with RequireGlobal(). No RBAC permission
// grant is required — every handler validates that the caller owns the
// target tenant via TenantProvider memberships (read live, not from JWT
// context, so revocations land immediately).
func RegisterSelfServiceRoutes(api huma.API, h *handlers.SubscriptionHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "subscriptions-self-subscribe",
		Method:      http.MethodPost, Path: "/v1/me/subscriptions",
		Summary:  "Self-service subscribe (tenant-owner only)",
		Tags:     []string{"Subscriptions - Self-service"},
		Security: subscriptionsSec,
	}, h.SelfSubscribe)

	huma.Register(api, huma.Operation{
		OperationID: "me-list-subscriptions",
		Method:      http.MethodGet, Path: "/v1/me/subscriptions",
		Summary:  "List subscriptions across tenants the caller owns",
		Tags:     []string{"Subscriptions - Self-service"},
		Security: subscriptionsSec,
	}, h.MeList)

	huma.Register(api, huma.Operation{
		OperationID: "me-get-subscription",
		Method:      http.MethodGet, Path: "/v1/me/subscriptions/{id}",
		Summary:  "Get a subscription owned by the caller",
		Tags:     []string{"Subscriptions - Self-service"},
		Security: subscriptionsSec,
	}, h.MeGet)

	huma.Register(api, huma.Operation{
		OperationID: "me-cancel-subscription",
		Method:      http.MethodPost, Path: "/v1/me/subscriptions/{id}/cancel",
		Summary:  "Cancel a subscription owned by the caller",
		Tags:     []string{"Subscriptions - Self-service"},
		Security: subscriptionsSec,
	}, h.MeCancel)

	huma.Register(api, huma.Operation{
		OperationID: "me-reactivate-subscription",
		Method:      http.MethodPost, Path: "/v1/me/subscriptions/{id}/reactivate",
		Summary:  "Reactivate a cancelled subscription owned by the caller",
		Tags:     []string{"Subscriptions - Self-service"},
		Security: subscriptionsSec,
	}, h.MeReactivate)

	huma.Register(api, huma.Operation{
		OperationID: "me-list-subscription-invoices",
		Method:      http.MethodGet, Path: "/v1/me/subscriptions/{id}/invoices",
		Summary:  "List invoices for a subscription owned by the caller",
		Tags:     []string{"Subscriptions - Self-service"},
		Security: subscriptionsSec,
	}, h.MeListInvoices)

	huma.Register(api, huma.Operation{
		OperationID: "me-list-subscription-activity",
		Method:      http.MethodGet, Path: "/v1/me/subscriptions/{id}/activity",
		Summary:  "List activity log for a subscription owned by the caller",
		Tags:     []string{"Subscriptions - Self-service"},
		Security: subscriptionsSec,
	}, h.MeListActivity)
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
