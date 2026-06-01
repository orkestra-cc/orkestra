package dev

import (
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/orkestra/backend/internal/addons/dev/handlers"
)

// isProductionEnvironment checks if the current environment is production
// This provides a safety check even if the caller forgets to check
func isProductionEnvironment() bool {
	env := strings.ToLower(os.Getenv("APP_ENV"))
	return env == "production" || env == "prod"
}

// RegisterRoutes registers development-only routes via Huma
// IMPORTANT: Only call this in non-production environments
// This function will refuse to register routes in production as a safety measure
func RegisterRoutes(api huma.API, devTokenHandler *handlers.DevTokenHandler) {
	// CRITICAL: Safety check to prevent accidental registration in production
	if isProductionEnvironment() {
		slog.Error("SECURITY: Attempted to register dev routes in production environment - request denied")
		return // Do not register any routes
	}
	// Token generation endpoint (hidden from Scalar/OpenAPI docs)
	huma.Register(api, huma.Operation{
		OperationID: "generate-dev-token",
		Method:      http.MethodPost,
		Path:        "/dev/token",
		Summary:     "Generate development JWT token",
		Description: "Generates a JWT token for testing purposes. Only available in development and staging environments. Creates a synthetic user (no database writes).",
		Tags:        []string{"Development"},
		Hidden:      true,
	}, devTokenHandler.GenerateToken)

	// List available roles (hidden from Scalar/OpenAPI docs)
	huma.Register(api, huma.Operation{
		OperationID: "list-dev-roles",
		Method:      http.MethodGet,
		Path:        "/dev/token/roles",
		Summary:     "List available roles for token generation",
		Description: "Returns the list of valid roles that can be used when generating development tokens.",
		Tags:        []string{"Development"},
		Hidden:      true,
	}, devTokenHandler.ListRoles)
}
