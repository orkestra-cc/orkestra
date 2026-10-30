package user

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra/backend/internal/core/user/handlers"
)

// RegisterReadRoutes registers the read-only user-management endpoints.
// Mounted behind `user.read` (global scope) — no step-up required since
// these can't mutate platform state.
func RegisterReadRoutes(api huma.API, userHandler *handlers.UserHandler) {
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
		OperationID: "list-users",
		Method:      http.MethodGet,
		Path:        "/v1/users",
		Summary:     "List users",
		Description: "Retrieves a paginated list of users with optional filtering",
		Tags:        []string{"Users"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, userHandler.ListUsers)

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

// RegisterCreateRoutes registers POST /v1/users, behind `user.create`.
// It creates a new row — the role-escalation guard in CreateUser rejects
// any attempt to seed a higher-tier role than the caller's, so no
// step-up is required at the route level.
func RegisterCreateRoutes(api huma.API, userHandler *handlers.UserHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "create-user",
		Method:      http.MethodPost,
		Path:        "/v1/users",
		Summary:     "Create a new user",
		Description: "Creates a new user with the provided information. Refuses to seed a role higher in the system tier than the caller's own (403 user.role_escalation_forbidden).",
		Tags:        []string{"Users"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, userHandler.CreateUser)
}

// RegisterUpdateRoutes registers PUT /v1/users/{id} (role change,
// deactivate, profile), behind `user.update` + `RequireStepUp(5m)` so a
// long-lived session can't be re-used hours later to grant or revoke
// privilege without a fresh MFA proof. The SPA's StepUpModal catches the
// 401 `step_up_required` and prompts the caller to re-verify. The handler
// adds the target-tier guard: a caller never edits a user whose current
// role is above their own (403 user.target_tier_forbidden).
func RegisterUpdateRoutes(api huma.API, userHandler *handlers.UserHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "update-user",
		Method:      http.MethodPut,
		Path:        "/v1/users/{id}",
		Summary:     "Update user",
		Description: "Updates a user's information. Refuses role escalation (403 user.role_escalation_forbidden), a target whose current role is above the caller's (403 user.target_tier_forbidden) and last-administrator removal (403 user.last_admin_forbidden). Requires a fresh MFA proof (RequireStepUp).",
		Tags:        []string{"Users"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, userHandler.UpdateUser)
}

// RegisterDeleteRoutes registers DELETE /v1/users/{id} (soft-alias),
// behind `user.delete` + `RequireStepUp(5m)`, with the same target-tier
// guard as the update.
func RegisterDeleteRoutes(api huma.API, userHandler *handlers.UserHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "delete-user",
		Method:      http.MethodDelete,
		Path:        "/v1/users/{id}",
		Summary:     "Delete user",
		Description: "Soft-deletes a user (alias-on-delete frees the email). Refuses self-delete (403 user.self_delete_forbidden), a target whose current role is above the caller's (403 user.target_tier_forbidden) and last-administrator removal (403 user.last_admin_forbidden). Requires a fresh MFA proof (RequireStepUp).",
		Tags:        []string{"Users"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, userHandler.DeleteUser)
}

// RegisterAdminClientReadRoutes mounts the read-only client-admin
// endpoints. Behind `user.read` only — no step-up.
func RegisterAdminClientReadRoutes(api huma.API, h *handlers.AdminClientUserHandler) {
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
}

// RegisterAdminClientCreateRoutes mounts the client-admin onboarding
// endpoints — create, invite, resend invite, resend verification — behind
// `user.create`. These insert fresh rows or re-send onboarding mail but
// never modify existing data, so no step-up is required.
func RegisterAdminClientCreateRoutes(api huma.API, h *handlers.AdminClientUserHandler) {
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
}

// RegisterAdminClientPasswordResetRoute mounts send-password-reset behind
// `user.update` without step-up: it acts on an EXISTING user (hence not
// create), but nothing changes until the recipient redeems the emailed,
// expiring link, so it does not need the fresh-proof gate of the PATCH.
func RegisterAdminClientPasswordResetRoute(api huma.API, h *handlers.AdminClientUserHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "send-password-reset-client-user-admin",
		Method:      http.MethodPost,
		Path:        "/v1/admin/client-users/{id}/send-password-reset",
		Summary:     "Trigger a password-reset email for a Tier-2 client user",
		Tags:        []string{"Users Admin"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, h.SendPasswordResetClientUserAdmin)
}

// RegisterAdminClientUpdateRoutes mounts PATCH /v1/admin/client-users/{id}
// (role/active flip, profile) behind `user.update` + `RequireStepUp(5m)`,
// for parity with the operator-tier update; same target-tier guard.
func RegisterAdminClientUpdateRoutes(api huma.API, h *handlers.AdminClientUserHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "update-client-user-admin",
		Method:      http.MethodPatch,
		Path:        "/v1/admin/client-users/{id}",
		Summary:     "Update profile, role, or active status of a Tier-2 client user",
		Description: "Patches name/username/email/phone/role/isActive. Refuses role escalation (403 user.role_escalation_forbidden) and a target whose current role is above the caller's (403 user.target_tier_forbidden). Requires a fresh MFA proof (RequireStepUp).",
		Tags:        []string{"Users Admin"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, h.UpdateClientUserAdmin)
}

// RegisterAdminClientDeleteRoutes mounts DELETE /v1/admin/client-users/{id}
// (soft-alias) behind `user.delete` + `RequireStepUp(5m)`; same
// target-tier guard as the operator-tier delete.
func RegisterAdminClientDeleteRoutes(api huma.API, h *handlers.AdminClientUserHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "delete-client-user-admin",
		Method:      http.MethodDelete,
		Path:        "/v1/admin/client-users/{id}",
		Summary:     "Soft-delete a Tier-2 client user and free its email",
		Description: "Calls SoftDeleteAndAliasEmail so the original address can be reused for a fresh signup. The original email is preserved on the user document for audit. Refuses a target whose current role is above the caller's (403 user.target_tier_forbidden). Requires a fresh MFA proof (RequireStepUp).",
		Tags:        []string{"Users Admin"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, h.DeleteClientUserAdmin)
}
