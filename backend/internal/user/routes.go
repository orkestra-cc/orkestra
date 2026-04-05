package user

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra/backend/internal/user/handlers"
)

// RegisterRoutes registers all user management routes on the given API.
func RegisterRoutes(api huma.API, userHandler *handlers.UserHandler) {
	// Core CRUD operations
	huma.Register(api, huma.Operation{
		OperationID: "create-user",
		Method:      http.MethodPost,
		Path:        "/v1/users",
		Summary:     "Create a new user",
		Description: "Creates a new user with the provided information",
		Tags:        []string{"Users"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, userHandler.CreateUser)

	huma.Register(api, huma.Operation{
		OperationID: "get-user",
		Method:      http.MethodGet,
		Path:        "/v1/users/{id}",
		Summary:     "Get user by ID",
		Description: "Retrieves a user by their UUID",
		Tags:        []string{"Users"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, userHandler.GetUser)

	huma.Register(api, huma.Operation{
		OperationID: "update-user",
		Method:      http.MethodPut,
		Path:        "/v1/users/{id}",
		Summary:     "Update user",
		Description: "Updates a user's information",
		Tags:        []string{"Users"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, userHandler.UpdateUser)

	huma.Register(api, huma.Operation{
		OperationID: "delete-user",
		Method:      http.MethodDelete,
		Path:        "/v1/users/{id}",
		Summary:     "Delete user",
		Description: "Soft deletes a user",
		Tags:        []string{"Users"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, userHandler.DeleteUser)

	huma.Register(api, huma.Operation{
		OperationID: "list-users",
		Method:      http.MethodGet,
		Path:        "/v1/users",
		Summary:     "List users",
		Description: "Retrieves a paginated list of users with optional filtering",
		Tags:        []string{"Users"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, userHandler.ListUsers)

	// Query operations
	huma.Register(api, huma.Operation{
		OperationID: "get-users-by-role",
		Method:      http.MethodGet,
		Path:        "/v1/users/role/{role}",
		Summary:     "Get users by role",
		Description: "Retrieves all users with a specific role",
		Tags:        []string{"Users"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, userHandler.GetUsersByRole)

	huma.Register(api, huma.Operation{
		OperationID: "get-user-by-email",
		Method:      http.MethodGet,
		Path:        "/v1/users/by-email",
		Summary:     "Get user by email",
		Description: "Retrieves a user by their email address",
		Tags:        []string{"Users"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, userHandler.GetUserByEmail)

	huma.Register(api, huma.Operation{
		OperationID: "get-user-count",
		Method:      http.MethodGet,
		Path:        "/v1/users/count",
		Summary:     "Get user count",
		Description: "Returns the total count of users with optional filtering",
		Tags:        []string{"Users"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, userHandler.GetUserCount)

	// Document management operations
	huma.Register(api, huma.Operation{
		OperationID: "get-users-with-expired-documents",
		Method:      http.MethodGet,
		Path:        "/v1/users/expired-documents",
		Summary:     "Get users with expired documents",
		Description: "Retrieves users who have expired driver documents",
		Tags:        []string{"Users", "Documents"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, userHandler.GetUsersWithExpiredDocuments)

	huma.Register(api, huma.Operation{
		OperationID: "get-users-with-expiring-soon-documents",
		Method:      http.MethodGet,
		Path:        "/v1/users/expiring-soon-documents",
		Summary:     "Get users with documents expiring soon",
		Description: "Retrieves users who have driver documents expiring within the specified number of days",
		Tags:        []string{"Users", "Documents"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, userHandler.GetUsersWithExpiringSoonDocuments)

	huma.Register(api, huma.Operation{
		OperationID: "update-user-documents",
		Method:      http.MethodPatch,
		Path:        "/v1/users/{id}/documents",
		Summary:     "Update user documents",
		Description: "Updates only the document-related fields for a user",
		Tags:        []string{"Users", "Documents"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, userHandler.UpdateUserDocuments)

	huma.Register(api, huma.Operation{
		OperationID: "check-user-document-expiry",
		Method:      http.MethodGet,
		Path:        "/v1/users/{id}/check-expiry",
		Summary:     "Check document expiry",
		Description: "Checks which documents are expired for a specific user",
		Tags:        []string{"Users", "Documents"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, userHandler.CheckDocumentExpiry)
}
