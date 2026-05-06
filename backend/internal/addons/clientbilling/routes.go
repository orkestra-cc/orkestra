package clientbilling

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/orkestra/backend/internal/addons/clientbilling/handlers"
)

var clientbillingSec = []map[string][]string{{"bearerAuth": {}}}

// RegisterMeRoutes mounts /v1/me/billing-profile on a router the caller
// has already gated with RequireGlobal(). Both routes resolve the target
// user from the JWT, so no per-route RBAC permission is required (mirrors
// the subscriptions self-subscribe pattern).
func RegisterMeRoutes(api huma.API, h *handlers.MeHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "me-get-billing-profile",
		Method:      http.MethodGet, Path: "/v1/me/billing-profile",
		Summary:  "Get the caller's billing profile",
		Tags:     []string{"Client Billing"},
		Security: clientbillingSec,
	}, h.GetBillingProfile)

	huma.Register(api, huma.Operation{
		OperationID: "me-put-billing-profile",
		Method:      http.MethodPut, Path: "/v1/me/billing-profile",
		Summary:  "Create or update the caller's billing profile",
		Tags:     []string{"Client Billing"},
		Security: clientbillingSec,
	}, h.PutBillingProfile)
}
