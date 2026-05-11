package user

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra/backend/internal/core/user/handlers"
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

}

// RegisterAdminClientRoutes mounts the admin endpoints that operate on
// the client_users tier — list / get / create / update / delete behind
// the same system.users.admin gate as the legacy /v1/users surface.
func RegisterAdminClientRoutes(api huma.API, h *handlers.AdminClientUserHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "list-client-users-admin",
		Method:      http.MethodGet,
		Path:        "/v1/admin/client-users",
		Summary:     "List Tier-2 client users with their tenant memberships",
		Description: "Returns every client_users row joined with tenant memberships. Self-registered users with no memberships are included with an empty memberships array.",
		Tags:        []string{"Users Admin"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, h.ListClientUsersAdmin)

	huma.Register(api, huma.Operation{
		OperationID: "get-client-user-admin",
		Method:      http.MethodGet,
		Path:        "/v1/admin/client-users/{id}",
		Summary:     "Get a single Tier-2 client user with tenant memberships",
		Tags:        []string{"Users Admin"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, h.GetClientUserAdmin)

	huma.Register(api, huma.Operation{
		OperationID: "update-client-user-admin",
		Method:      http.MethodPatch,
		Path:        "/v1/admin/client-users/{id}",
		Summary:     "Update profile, role, or active status of a Tier-2 client user",
		Tags:        []string{"Users Admin"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, h.UpdateClientUserAdmin)

	huma.Register(api, huma.Operation{
		OperationID: "delete-client-user-admin",
		Method:      http.MethodDelete,
		Path:        "/v1/admin/client-users/{id}",
		Summary:     "Soft-delete a Tier-2 client user and free its email",
		Description: "Calls SoftDeleteAndAliasEmail so the original address can be reused for a fresh signup. The original email is preserved on the user document for audit.",
		Tags:        []string{"Users Admin"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, h.DeleteClientUserAdmin)

	huma.Register(api, huma.Operation{
		OperationID: "create-client-user-admin",
		Method:      http.MethodPost,
		Path:        "/v1/admin/client-users",
		Summary:     "Admin-direct create of a Tier-2 client user",
		Description: "Inserts a client_users row with the supplied password (validated against the live policy) and EmailVerified=true so the user can log in immediately. For an alternate flow that emails a token instead of needing a password, use POST /v1/admin/client-users/invite.",
		Tags:        []string{"Users Admin"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, h.CreateClientUserAdmin)

	huma.Register(api, huma.Operation{
		OperationID: "invite-client-user-admin",
		Method:      http.MethodPost,
		Path:        "/v1/admin/client-users/invite",
		Summary:     "Invite a new Tier-2 client user via email",
		Description: "Creates the client_users row with no password, emails an admin_invite token. The recipient redeems it via /v1/auth/client/accept-invite to set their password and verify the email in one step.",
		Tags:        []string{"Users Admin"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, h.InviteClientUserAdmin)

	huma.Register(api, huma.Operation{
		OperationID: "resend-invite-client-user-admin",
		Method:      http.MethodPost,
		Path:        "/v1/admin/client-users/{id}/invite/resend",
		Summary:     "Resend the admin invite email for an existing Tier-2 user",
		Tags:        []string{"Users Admin"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, h.ResendInviteClientUserAdmin)

	huma.Register(api, huma.Operation{
		OperationID: "resend-verification-client-user-admin",
		Method:      http.MethodPost,
		Path:        "/v1/admin/client-users/{id}/resend-verification",
		Summary:     "Resend the email-verification link for a Tier-2 client user",
		Tags:        []string{"Users Admin"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, h.ResendVerificationClientUserAdmin)

	huma.Register(api, huma.Operation{
		OperationID: "send-password-reset-client-user-admin",
		Method:      http.MethodPost,
		Path:        "/v1/admin/client-users/{id}/send-password-reset",
		Summary:     "Trigger a password-reset email for a Tier-2 client user",
		Tags:        []string{"Users Admin"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, h.SendPasswordResetClientUserAdmin)
}
