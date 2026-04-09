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
