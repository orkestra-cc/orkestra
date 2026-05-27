package navigation

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra/backend/internal/core/navigation/handlers"
)

// RegisterRoutes registers the navigation menu route on the given API.
func RegisterRoutes(api huma.API, navigationHandler *handlers.NavigationHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "get-navigation",
		Method:      http.MethodGet,
		Path:        "/v1/navigation",
		Summary:     "Get navigation menu",
		Description: "Returns role-filtered navigation menu for the current authenticated user",
		Tags:        []string{"Navigation"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, navigationHandler.GetNavigation)
}

// RegisterAdminRoutes mounts the operator-only navigation admin surface.
// Gated by `administrator` in the security scope — the operator-protected
// router enforces RequireRole(administrator) for any operation declaring
// it. Mirrors the convention used by /v1/admin/observability/log-levels.
func RegisterAdminRoutes(api huma.API, adminHandler *handlers.AdminNavigationHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "admin-navigation-get",
		Method:      http.MethodGet,
		Path:        "/v1/admin/navigation",
		Summary:     "Get the full unfiltered navigation tree",
		Description: "Returns every NavItem every module declared, regardless of the caller's role or tenant kind. Includes MinRole/Tier/ModuleEnabled metadata for the admin visibility matrix and reorder UI.",
		Tags:        []string{"Navigation"},
		Security:    []map[string][]string{{"bearerAuth": {"administrator"}}},
	}, adminHandler.GetAdminNavigation)

	huma.Register(api, huma.Operation{
		OperationID: "admin-navigation-patch-order",
		Method:      http.MethodPatch,
		Path:        "/v1/admin/navigation/order",
		Summary:     "Persist sibling ordering for one parent",
		Description: "Upserts the ordering override for one parent (either an item's ItemKey or the synthetic '__root.<realm>.<section-slug>' returned in the admin tree). Items present in orderedChildren take the listed position; items missing keep their declared order and append after. Self-heals stale ItemKeys on the next read.",
		Tags:        []string{"Navigation"},
		Security:    []map[string][]string{{"bearerAuth": {"administrator"}}},
	}, adminHandler.PatchOrder)

	huma.Register(api, huma.Operation{
		OperationID: "admin-navigation-delete-order",
		Method:      http.MethodDelete,
		Path:        "/v1/admin/navigation/order",
		Summary:     "Clear the ordering override for one parent",
		Description: "Drops the override document for the given parentKey. Missing-doc is a no-op (idempotent). After clearing, the parent's children render in declared order again.",
		Tags:        []string{"Navigation"},
		Security:    []map[string][]string{{"bearerAuth": {"administrator"}}},
	}, adminHandler.DeleteOrder)
}
