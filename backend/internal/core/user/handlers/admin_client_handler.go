package handlers

import (
	"context"
	"errors"
	"log/slog"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra/backend/internal/core/user/services"
	"github.com/orkestra/backend/internal/shared/errcode"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/module"
)

// AdminClientUserHandler powers the admin "Clients" page — a list of
// client_users rows joined with each user's tenant memberships.
//
// Tenant memberships are fetched lazily via the service registry rather
// than injected at construction time because the user module initialises
// before tenant (tenant depends on user). At request time both modules
// are wired, so the lookup always succeeds in a real boot. When tenant is
// disabled at runtime the lookup returns nil and Memberships is reported
// as an empty array — the admin UI handles unattached users the same way.
type AdminClientUserHandler struct {
	clientUserService services.UserService
	services          *module.ServiceRegistry
	// platform classifies the deployment environment, for the caller-role
	// guard's synthetic dev-token exception. Optional; a nil platform is
	// treated as production-like, so the exception stays shut unless it is
	// deliberately wired.
	platform module.PlatformInfo
}

// SetPlatform wires the deployment's environment classification. Called
// from the user module's Init; unset (tests) disables the dev-token
// exception in callerRole, which is the fail-closed default.
func (h *AdminClientUserHandler) SetPlatform(p module.PlatformInfo) {
	h.platform = p
}

// NewAdminClientUserHandler wires the handler with the client-tier user
// service and a reference to the module service registry for the lazy
// tenant-provider lookup.
func NewAdminClientUserHandler(clientUserService services.UserService, services *module.ServiceRegistry) *AdminClientUserHandler {
	return &AdminClientUserHandler{
		clientUserService: clientUserService,
		services:          services,
	}
}

// clientTierNoPropagation is the value the client tier stamps into the
// `propagation` metadata key of a `user.role.changed` audit row.
//
// The operator tier carries two booleans on that action — cache_invalidated
// and sessions_terminated (M-13). One action name must not have two
// metadata contracts, so the client tier emits the same two keys. Both are
// literally false here: nothing is invalidated and no session is ended.
// This key says WHY, so a reader never mistakes a client-tier `false` for
// the operator tier's "we tried and it failed":
//
//   - No authz cache entry exists to retire. The authz module resolves its
//     system-role lookup from module.ServiceUserService, which is the
//     OPERATOR user service; client users live in a separate repository and
//     collection under ServiceClientUserProvider, so a client UUID never
//     resolves and a client user's Role never enters GetEffectivePermissions.
//     (Not because a client user cannot hold a platform role — they can, and
//     the value does reach their JWT `srole`. It is inert, not absent.)
//   - No session is ended, and that half is a genuine RESIDUAL rather than a
//     non-event: see the comment on the role branch in UpdateClientUserAdmin.
const clientTierNoPropagation = "not_applicable_client_tier"

// emitAudit mirrors UserHandler.emitAudit but probes the client-tier
// service. Nil sink, missing capability, or compliance addon disabled →
// silent no-op.
func (h *AdminClientUserHandler) emitAudit(ctx context.Context, event iface.AuditEvent) {
	emitter, ok := h.clientUserService.(auditEmitter)
	if !ok {
		return
	}
	sink := emitter.AuditSink()
	if sink == nil {
		return
	}
	sink.Emit(ctx, event)
}

// ListClientUsersAdminRequest mirrors the existing /v1/users filter set.
type ListClientUsersAdminRequest struct {
	Role          string `query:"role" doc:"Filter by user role"`
	IsActive      bool   `query:"isActive" doc:"Filter by active status"`
	EmailVerified bool   `query:"emailVerified" doc:"Filter by email verification status"`
	Search        string `query:"search" doc:"Search in name, email, username"`
	Page          int    `query:"page" default:"1" minimum:"1" doc:"Page number"`
	PageSize      int    `query:"pageSize" default:"50" minimum:"1" maximum:"200" doc:"Number of items per page"`
}

// ListClientUsersAdminResponse wraps the paginated payload in Huma's body
// envelope.
type ListClientUsersAdminResponse struct {
	Body iface.AdminClientUserListResponse `json:"body"`
}

// ListClientUsersAdmin handles GET /v1/admin/client-users.
func (h *AdminClientUserHandler) ListClientUsersAdmin(ctx context.Context, req *ListClientUsersAdminRequest) (*ListClientUsersAdminResponse, error) {
	filters := &iface.UserFilters{
		Role:   req.Role,
		Search: req.Search,
	}
	if req.IsActive {
		v := req.IsActive
		filters.IsActive = &v
	}
	if req.EmailVerified {
		v := req.EmailVerified
		filters.EmailVerified = &v
	}

	pagination := &iface.PaginationParams{Page: req.Page, PageSize: req.PageSize}

	page, err := h.clientUserService.ListUsers(ctx, filters, pagination)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to list client users", err)
	}

	tenantProv, _ := module.GetTyped[iface.TenantProvider](h.services, module.ServiceTenantProvider)

	out := make([]iface.AdminClientUserItem, 0, len(page.Users))
	for i := range page.Users {
		u := page.Users[i]
		item := iface.AdminClientUserItem{
			ID:            u.ID,
			Email:         u.Email,
			Username:      u.Username,
			FullName:      u.FullName,
			Avatar:        u.Avatar,
			Role:          u.Role,
			IsActive:      u.IsActive,
			EmailVerified: u.EmailVerified,
			LastLogin:     u.LastLogin,
			CreatedAt:     u.CreatedAt,
			Memberships:   []iface.AdminUserMembership{},
		}

		if tenantProv != nil {
			memberships, mErr := tenantProv.ListUserMemberships(ctx, u.ID)
			if mErr != nil {
				// Don't fail the whole list because one user's membership
				// fetch errored — log and continue with an empty array.
				slog.WarnContext(ctx, "admin client-users: list memberships failed",
					"userId", u.ID, "error", mErr)
			} else {
				item.Memberships = make([]iface.AdminUserMembership, 0, len(memberships))
				for _, m := range memberships {
					item.Memberships = append(item.Memberships, iface.AdminUserMembership{
						TenantUUID: m.TenantUUID,
						TenantName: m.TenantName,
						TenantSlug: m.TenantSlug,
						TenantKind: m.TenantKind,
						Roles:      m.Roles,
						IsOwner:    m.IsOwner,
					})
				}
			}
		}

		out = append(out, item)
	}

	return &ListClientUsersAdminResponse{
		Body: iface.AdminClientUserListResponse{
			Users:      out,
			Total:      page.Total,
			Page:       page.Page,
			PageSize:   page.PageSize,
			TotalPages: page.TotalPages,
		},
	}, nil
}

// buildAdminItem fetches a client user by id and joins its tenant
// memberships. Shared by GetClientUserAdmin and the create / update
// response paths so the detail page sees the same shape as the list.
func (h *AdminClientUserHandler) buildAdminItem(ctx context.Context, id string) (*iface.AdminClientUserItem, error) {
	resp, err := h.clientUserService.GetUser(ctx, id)
	if err != nil {
		return nil, err
	}
	item := iface.AdminClientUserItem{
		ID:            resp.ID,
		Email:         resp.Email,
		Username:      resp.Username,
		FullName:      resp.FullName,
		Avatar:        resp.Avatar,
		Role:          resp.Role,
		IsActive:      resp.IsActive,
		EmailVerified: resp.EmailVerified,
		LastLogin:     resp.LastLogin,
		CreatedAt:     resp.CreatedAt,
		Memberships:   []iface.AdminUserMembership{},
		Providers:     resp.Providers,
	}

	if tenantProv, ok := module.GetTyped[iface.TenantProvider](h.services, module.ServiceTenantProvider); ok && tenantProv != nil {
		memberships, mErr := tenantProv.ListUserMemberships(ctx, resp.ID)
		if mErr != nil {
			slog.WarnContext(ctx, "admin client-user: list memberships failed",
				"userId", resp.ID, "error", mErr)
		} else {
			item.Memberships = make([]iface.AdminUserMembership, 0, len(memberships))
			for _, m := range memberships {
				item.Memberships = append(item.Memberships, iface.AdminUserMembership{
					TenantUUID: m.TenantUUID,
					TenantName: m.TenantName,
					TenantSlug: m.TenantSlug,
					TenantKind: m.TenantKind,
					Roles:      m.Roles,
					IsOwner:    m.IsOwner,
				})
			}
		}
	}
	return &item, nil
}

// GetClientUserAdminRequest mirrors the path-only shape Huma expects.
type GetClientUserAdminRequest struct {
	ID string `path:"id" doc:"Client user UUID"`
}

// GetClientUserAdminResponse wraps a single AdminClientUserItem.
type GetClientUserAdminResponse struct {
	Body iface.AdminClientUserItem `json:"body"`
}

// GetClientUserAdmin handles GET /v1/admin/client-users/{id}.
func (h *AdminClientUserHandler) GetClientUserAdmin(ctx context.Context, req *GetClientUserAdminRequest) (*GetClientUserAdminResponse, error) {
	item, err := h.buildAdminItem(ctx, req.ID)
	if err != nil {
		if errors.Is(err, services.ErrUserNotFound) {
			return nil, huma.Error404NotFound("Client user not found", err)
		}
		if errors.Is(err, services.ErrInvalidInput) {
			return nil, huma.Error400BadRequest("Invalid user id", err)
		}
		return nil, huma.Error500InternalServerError("Failed to load client user", err)
	}
	return &GetClientUserAdminResponse{Body: *item}, nil
}

// UpdateClientUserAdminBody is the slim mutation payload — only the
// fields that an admin would reasonably change on a client user. Driver
// document fields are intentionally omitted; they are managed by the
// user themselves.
type UpdateClientUserAdminBody struct {
	FullName string `json:"fullName,omitempty" validate:"omitempty,min=1,max=100"`
	Username string `json:"username,omitempty" validate:"omitempty,min=3,max=50"`
	Email    string `json:"email,omitempty" validate:"omitempty,email"`
	Phone    string `json:"phone,omitempty" validate:"omitempty,e164"`
	Role     string `json:"role,omitempty" validate:"omitempty,oneof=super_admin administrator developer manager operator guest"`
	IsActive *bool  `json:"isActive,omitempty"`
}

// UpdateClientUserAdminRequest combines the path id and the patch body.
type UpdateClientUserAdminRequest struct {
	ID   string                    `path:"id" doc:"Client user UUID"`
	Body UpdateClientUserAdminBody `json:"body"`
}

// UpdateClientUserAdminResponse echoes the freshly joined item.
type UpdateClientUserAdminResponse struct {
	Body iface.AdminClientUserItem `json:"body"`
}

// UpdateClientUserAdmin handles PATCH /v1/admin/client-users/{id}.
func (h *AdminClientUserHandler) UpdateClientUserAdmin(ctx context.Context, req *UpdateClientUserAdminRequest) (*UpdateClientUserAdminResponse, error) {
	actorUUID, actorEmail := actorFromCtx(ctx)
	// Pre-change snapshot for lifecycle audit delta computation. Read
	// failure is non-fatal; the patch flow surfaces its own 404 below.
	previous, _ := h.clientUserService.GetUser(ctx, req.ID)

	// D29 / M-17. Role guards, mirroring the operator-tier PATCH. This
	// endpoint ran none at all, and its body accepts the whole platform
	// role enum (super_admin … guest) — a value that does reach the
	// user's JWT `srole`. Without these, an operator of any tier could
	// mint a client-tier super_admin, and could do it to themselves by
	// proxy.
	//
	// Deliberately NOT ported from the operator tier: the
	// last-administrator quorum. A client user is never a platform
	// administrator, so demoting one can never leave the platform
	// without one; running the quorum here would refuse legitimate
	// demotions for a condition that cannot arise.
	if req.Body.Role != "" {
		callerRole, err := h.callerRole(ctx)
		if err != nil {
			return nil, err
		}
		// The target's current role, for the refusal metadata. Empty when
		// the pre-read found nothing — the request then falls through to
		// UpdateUser's clean 404.
		previousRole := ""
		if previous != nil {
			previousRole = previous.Role
		}
		if !canAssignRole(callerRole, req.Body.Role) {
			h.emitAudit(ctx, iface.AuditEvent{
				ActorUserID:  actorUUID,
				ActorEmail:   actorEmail,
				ActorType:    "user",
				Action:       "user.update.refused",
				ResourceType: "client_user",
				ResourceID:   req.ID,
				Outcome:      "denied",
				Metadata: map[string]any{
					"code":      errcode.UserRoleEscalationForbidden,
					"attempted": "role_escalation",
					"from":      previousRole,
					"to":        req.Body.Role,
				},
			})
			return nil, errcode.Forbidden(errcode.UserRoleEscalationForbidden,
				"You cannot assign a role higher than your own")
		}
		// Privileged-role guard for machine principals, same fail-closed
		// shape as the operator tier: an unreadable target must not let a
		// privileged role land on a service account. Non-privileged
		// assignments with a nil pre-read fall through — UpdateUser
		// surfaces its own 404/500 below.
		if (previous == nil && isPrivilegedSystemRole(req.Body.Role)) || (previous != nil && !serviceAccountRoleAllowed(previous.Kind, req.Body.Role)) {
			h.emitAudit(ctx, iface.AuditEvent{
				ActorUserID:  actorUUID,
				ActorEmail:   actorEmail,
				ActorType:    "user",
				Action:       "user.update.refused",
				ResourceType: "client_user",
				ResourceID:   req.ID,
				Outcome:      "denied",
				Metadata: map[string]any{
					"code":      errcode.UserRoleEscalationForbidden,
					"attempted": "service_account_privileged_role",
					"to":        req.Body.Role,
				},
			})
			return nil, errcode.Forbidden(errcode.UserRoleEscalationForbidden,
				"Service accounts cannot hold privileged system roles")
		}
	}

	input := &iface.UpdateUserInput{
		FullName: req.Body.FullName,
		Username: req.Body.Username,
		Email:    req.Body.Email,
		Phone:    req.Body.Phone,
		Role:     req.Body.Role,
		IsActive: req.Body.IsActive,
	}
	if _, err := h.clientUserService.UpdateUser(ctx, req.ID, input); err != nil {
		switch {
		case errors.Is(err, services.ErrUserNotFound):
			return nil, huma.Error404NotFound("Client user not found", err)
		case errors.Is(err, services.ErrEmailNotUnique):
			return nil, errcode.Conflict(errcode.AuthEmailInUse, "Email already in use")
		case errors.Is(err, services.ErrInvalidInput):
			return nil, huma.Error400BadRequest("Invalid input", err)
		default:
			return nil, huma.Error500InternalServerError("Failed to update client user", err)
		}
	}

	item, err := h.buildAdminItem(ctx, req.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to reload client user", err)
	}

	// Lifecycle audit events on successful patches. Mirror the operator
	// handler's discrimination — isActive flip → activated/deactivated,
	// role change → role.changed with before/after metadata.
	if input.IsActive != nil && (previous == nil || previous.IsActive != *input.IsActive) {
		action := "user.activated"
		if !*input.IsActive {
			action = "user.deactivated"
			// Same rule as the operator tier: revoking the right to be
			// signed in has to end the sessions that right produced.
			h.terminateSessions(ctx, req.ID)
		}
		h.emitAudit(ctx, iface.AuditEvent{
			ActorUserID:  actorUUID,
			ActorEmail:   actorEmail,
			ActorType:    "user",
			Action:       action,
			ResourceType: "client_user",
			ResourceID:   req.ID,
			Outcome:      "success",
		})
	}
	if input.Role != "" && previous != nil && previous.Role != input.Role {
		// KNOWN RESIDUAL — a client role change ends NO session, so the
		// old `srole` stays live in every already-issued access token for
		// a full token lifetime. The operator tier terminates sessions
		// here (M-13); this tier deliberately does not, and the audit row
		// says so rather than implying otherwise.
		//
		// Latent, not exploitable today: a sweep of every `srole` reader
		// found none on a client-audience surface — internal/shared/setup
		// routes, the operator user handlers, navigation, the authz
		// handler are all on the operator-protected mux, and the
		// jwt_validator fallback fires only when no authz provider is
		// wired. The day a client-audience surface enforces on `srole`,
		// this becomes exploitable and the one-line fix is
		// h.terminateSessions(ctx, req.ID) right here — at the cost of
		// signing the user out on every role change.
		//
		// The cache half needs nothing: the authz module resolves its
		// system-role lookup from the OPERATOR user service, so a client
		// UUID never resolves and a client Role never enters
		// GetEffectivePermissions. Inert, not absent.
		h.emitAudit(ctx, iface.AuditEvent{
			ActorUserID:  actorUUID,
			ActorEmail:   actorEmail,
			ActorType:    "user",
			Action:       "user.role.changed",
			ResourceType: "client_user",
			ResourceID:   req.ID,
			Outcome:      "success",
			Metadata: map[string]any{
				"from": previous.Role,
				"to":   input.Role,
				// One action name, one metadata contract: the operator
				// tier stamps these two, so this tier stamps them too.
				// Both are literally accurate — neither happened — and a
				// consumer that reads only the booleans gets the
				// pessimistic answer, never an optimistic one.
				// `propagation` carries the reason; see the const.
				"cache_invalidated":   false,
				"sessions_terminated": false,
				"propagation":         clientTierNoPropagation,
			},
		})
	}

	return &UpdateClientUserAdminResponse{Body: *item}, nil
}

// DeleteClientUserAdminRequest is path-only.
type DeleteClientUserAdminRequest struct {
	ID string `path:"id" doc:"Client user UUID"`
}

// DeleteClientUserAdminResponse returns a confirmation message.
type DeleteClientUserAdminResponse struct {
	Body struct {
		Message string `json:"message"`
	}
}

// DeleteClientUserAdmin handles DELETE /v1/admin/client-users/{id}. Uses
// SoftDeleteAndAliasEmail so the freed email can be reused for a fresh
// signup — Tier-2 client emails are intentionally aliased, unlike
// operator-tier soft deletes which preserve the email for audit.
func (h *AdminClientUserHandler) DeleteClientUserAdmin(ctx context.Context, req *DeleteClientUserAdminRequest) (*DeleteClientUserAdminResponse, error) {
	actorUUID, actorEmail := actorFromCtx(ctx)
	if err := h.clientUserService.SoftDeleteAndAliasEmail(ctx, req.ID); err != nil {
		if errors.Is(err, services.ErrInvalidInput) {
			return nil, huma.Error400BadRequest("Invalid user id", err)
		}
		return nil, huma.Error500InternalServerError("Failed to delete client user", err)
	}
	h.terminateSessions(ctx, req.ID)
	h.emitAudit(ctx, iface.AuditEvent{
		ActorUserID:  actorUUID,
		ActorEmail:   actorEmail,
		ActorType:    "user",
		Action:       "user.deleted",
		ResourceType: "client_user",
		ResourceID:   req.ID,
		Outcome:      "success",
	})
	out := &DeleteClientUserAdminResponse{}
	out.Body.Message = "Client user deleted"
	return out, nil
}

// CreateClientUserAdminBody is the admin-direct create payload. The new
// user is pre-verified (admin vouched for the address) and active.
type CreateClientUserAdminBody struct {
	Email    string `json:"email" validate:"required,email"`
	FullName string `json:"fullName" validate:"required,min=1,max=100"`
	Username string `json:"username,omitempty" validate:"omitempty,min=3,max=50"`
	Phone    string `json:"phone,omitempty" validate:"omitempty,e164"`
	Role     string `json:"role" validate:"required,oneof=super_admin administrator developer manager operator guest"`
	Password string `json:"password" validate:"required,min=10,max=128" doc:"Initial password — admin should share securely; user can change after first login"`
}

// CreateClientUserAdminRequest carries the body.
type CreateClientUserAdminRequest struct {
	Body CreateClientUserAdminBody `json:"body"`
}

// CreateClientUserAdminResponse echoes the created item.
type CreateClientUserAdminResponse struct {
	Body iface.AdminClientUserItem `json:"body"`
}

// CreateClientUserAdmin handles POST /v1/admin/client-users. Pre-hashes
// the password against the live policy, then inserts the new client_users
// row with EmailVerified=true so the new user can log in immediately.
func (h *AdminClientUserHandler) CreateClientUserAdmin(ctx context.Context, req *CreateClientUserAdminRequest) (*CreateClientUserAdminResponse, error) {
	// Before the hasher lookup: authorisation must not depend on a
	// dependency being up, and an argon2id hash is deliberately expensive
	// — never pay for it on a request we are going to refuse.
	if err := h.guardCreateRole(ctx, req.Body.Role, req.Body.Email); err != nil {
		return nil, err
	}
	hasher, ok := module.GetTyped[iface.PasswordHasher](h.services, module.ServicePasswordService)
	if !ok || hasher == nil {
		return nil, huma.Error503ServiceUnavailable("Password service unavailable")
	}
	if err := hasher.ValidatePolicy(ctx, req.Body.Password, req.Body.Email); err != nil {
		return nil, huma.Error400BadRequest("Password does not meet policy", err)
	}
	hash, err := hasher.Hash(req.Body.Password)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to hash password", err)
	}

	input := &iface.CreateUserInput{
		Email:        req.Body.Email,
		FullName:     req.Body.FullName,
		Username:     req.Body.Username,
		Phone:        req.Body.Phone,
		Role:         req.Body.Role,
		PasswordHash: hash,
	}
	created, err := h.clientUserService.CreateUserWithPassword(ctx, input)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrEmailNotUnique):
			return nil, errcode.Conflict(errcode.AuthEmailInUse, "Email already in use")
		case errors.Is(err, services.ErrInvalidInput):
			return nil, huma.Error400BadRequest("Invalid input", err)
		default:
			return nil, huma.Error500InternalServerError("Failed to create client user", err)
		}
	}
	if mErr := h.clientUserService.MarkEmailVerified(ctx, created.UUID); mErr != nil {
		slog.WarnContext(ctx, "admin client-user: mark email verified failed",
			"userId", created.UUID, "error", mErr)
	}

	item, err := h.buildAdminItem(ctx, created.UUID)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to load created client user", err)
	}
	return &CreateClientUserAdminResponse{Body: *item}, nil
}

// inviteAuthOps fetches the client-tier admin auth surface lazily —
// auth depends on user, so this lookup must happen at request time.
// Returns (nil, false) when the auth module is disabled or its
// PasswordAuthService is missing; callers translate that to 503.
func (h *AdminClientUserHandler) inviteAuthOps() (iface.AdminAuthInviter, bool) {
	return module.GetTyped[iface.AdminAuthInviter](h.services, module.ServiceClientPasswordAuthService)
}

// InviteClientUserAdminBody powers POST /v1/admin/client-users/invite.
// Creates the user record with no password and emails an admin_invite
// token. The recipient redeems on the client SPA's /accept-invite page.
type InviteClientUserAdminBody struct {
	Email       string `json:"email" validate:"required,email"`
	FullName    string `json:"fullName" validate:"required,min=1,max=100"`
	Username    string `json:"username,omitempty" validate:"omitempty,min=3,max=50"`
	Phone       string `json:"phone,omitempty" validate:"omitempty,e164"`
	Role        string `json:"role" validate:"required,oneof=super_admin administrator developer manager operator guest"`
	InviterName string `json:"inviterName,omitempty" doc:"Free-text label rendered into the invite email — typically the operator's display name"`
}

// InviteClientUserAdminRequest carries the body.
type InviteClientUserAdminRequest struct {
	Body InviteClientUserAdminBody `json:"body"`
}

// InviteClientUserAdminResponse echoes the freshly-created item — the
// admin UI navigates to its detail page after success.
type InviteClientUserAdminResponse struct {
	Body iface.AdminClientUserItem `json:"body"`
}

// InviteClientUserAdmin handles POST /v1/admin/client-users/invite. The
// new client_users row carries an empty password hash and EmailVerified
// stays false — those fields are populated when the recipient redeems
// the invite via /v1/auth/client/accept-invite.
func (h *AdminClientUserHandler) InviteClientUserAdmin(ctx context.Context, req *InviteClientUserAdminRequest) (*InviteClientUserAdminResponse, error) {
	// Before the auth lookup, so a role escalation is refused as a 403
	// rather than masked by the 503 a degraded auth module would return.
	if err := h.guardCreateRole(ctx, req.Body.Role, req.Body.Email); err != nil {
		return nil, err
	}
	auth, ok := h.inviteAuthOps()
	if !ok || auth == nil {
		return nil, huma.Error503ServiceUnavailable("Auth service unavailable — cannot send invite")
	}

	input := &iface.CreateUserInput{
		Email:    req.Body.Email,
		FullName: req.Body.FullName,
		Username: req.Body.Username,
		Phone:    req.Body.Phone,
		Role:     req.Body.Role,
	}
	resp, err := h.clientUserService.CreateUser(ctx, input)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrEmailNotUnique):
			return nil, errcode.Conflict(errcode.AuthEmailInUse, "Email already in use")
		case errors.Is(err, services.ErrInvalidInput):
			return nil, huma.Error400BadRequest("Invalid input", err)
		default:
			return nil, huma.Error500InternalServerError("Failed to create client user", err)
		}
	}

	if err := auth.AdminSendInvite(ctx, resp.ID, req.Body.InviterName); err != nil {
		// Best-effort: the user row exists, the admin can resend the
		// invite from the detail page. Surface a 502 so the client
		// knows the email failed but doesn't roll the user back.
		slog.WarnContext(ctx, "admin invite: send failed",
			"userId", resp.ID, "error", err)
		return nil, huma.Error502BadGateway("User created but invite email failed to send", err)
	}

	item, err := h.buildAdminItem(ctx, resp.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to load created client user", err)
	}
	return &InviteClientUserAdminResponse{Body: *item}, nil
}

// ResendInviteClientUserAdminRequest re-emits an admin_invite token for
// an existing user. Path-only.
type ResendInviteClientUserAdminRequest struct {
	ID   string `path:"id" doc:"Client user UUID"`
	Body struct {
		InviterName string `json:"inviterName,omitempty"`
	} `json:"body"`
}

// AdminTriggerResponse is the no-body confirmation shape shared by the
// resend / reset / invite-resend admin actions.
type AdminTriggerResponse struct {
	Body struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
}

// ResendInviteClientUserAdmin handles POST /v1/admin/client-users/{id}/invite/resend.
func (h *AdminClientUserHandler) ResendInviteClientUserAdmin(ctx context.Context, req *ResendInviteClientUserAdminRequest) (*AdminTriggerResponse, error) {
	auth, ok := h.inviteAuthOps()
	if !ok || auth == nil {
		return nil, huma.Error503ServiceUnavailable("Auth service unavailable")
	}
	if err := auth.AdminSendInvite(ctx, req.ID, req.Body.InviterName); err != nil {
		return nil, mapInviteErr(err, "Failed to resend invite")
	}
	out := &AdminTriggerResponse{}
	out.Body.Success = true
	out.Body.Message = "Invite email re-sent"
	return out, nil
}

// ResendVerificationClientUserAdminRequest is path-only.
type ResendVerificationClientUserAdminRequest struct {
	ID string `path:"id" doc:"Client user UUID"`
}

// ResendVerificationClientUserAdmin handles
// POST /v1/admin/client-users/{id}/resend-verification. No-op if the
// user is already verified (returns 200 with success=true so the UI
// can flash a friendly toast).
func (h *AdminClientUserHandler) ResendVerificationClientUserAdmin(ctx context.Context, req *ResendVerificationClientUserAdminRequest) (*AdminTriggerResponse, error) {
	auth, ok := h.inviteAuthOps()
	if !ok || auth == nil {
		return nil, huma.Error503ServiceUnavailable("Auth service unavailable")
	}
	if err := auth.AdminResendVerification(ctx, req.ID); err != nil {
		return nil, mapInviteErr(err, "Failed to resend verification email")
	}
	out := &AdminTriggerResponse{}
	out.Body.Success = true
	out.Body.Message = "Verification email re-sent"
	return out, nil
}

// SendPasswordResetClientUserAdminRequest is path-only.
type SendPasswordResetClientUserAdminRequest struct {
	ID string `path:"id" doc:"Client user UUID"`
}

// SendPasswordResetClientUserAdmin handles
// POST /v1/admin/client-users/{id}/send-password-reset.
func (h *AdminClientUserHandler) SendPasswordResetClientUserAdmin(ctx context.Context, req *SendPasswordResetClientUserAdminRequest) (*AdminTriggerResponse, error) {
	auth, ok := h.inviteAuthOps()
	if !ok || auth == nil {
		return nil, huma.Error503ServiceUnavailable("Auth service unavailable")
	}
	if err := auth.AdminTriggerPasswordReset(ctx, req.ID); err != nil {
		return nil, mapInviteErr(err, "Failed to send password reset email")
	}
	out := &AdminTriggerResponse{}
	out.Body.Success = true
	out.Body.Message = "Password reset email sent"
	return out, nil
}

// mapInviteErr translates the auth service's sentinel errors into Huma
// HTTP responses. A bare error becomes 500 with the generic msg.
func mapInviteErr(err error, generic string) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, services.ErrUserNotFound):
		return huma.Error404NotFound("Client user not found", err)
	case errors.Is(err, services.ErrInvalidInput):
		return huma.Error400BadRequest("Invalid user id", err)
	}
	if errors.Is(err, iface.ErrPasswordLoginDisabled) {
		return errcode.Conflict(errcode.AuthPasswordLoginDisabled,
			"Email/password sign-in is disabled on the client surface; a reset link would mint a credential the surface refuses. Re-enable the method first.")
	}
	if errors.Is(err, iface.ErrAuthPolicyUnavailable) {
		return errcode.ServiceUnavailable(errcode.AuthPolicyUnavailable,
			"Sign-in policy is temporarily unavailable; try again shortly.")
	}
	// ErrNotificationDown lives in auth/services and we don't import it
	// from here — match by message so we still surface 503 cleanly.
	if msg := err.Error(); msg == "notifications disabled — cannot send email" {
		return huma.Error503ServiceUnavailable("Notifications disabled", err)
	}
	return huma.Error500InternalServerError(generic, err)
}

// callerRole resolves the CALLING OPERATOR's system role from the
// database — never from the `srole` JWT claim (spec §4.6 D28). The claim
// can be a whole access-token lifetime stale, which is exactly the window
// the role-change propagation exists to close.
//
// Which store answers is load-bearing, not cosmetic. The actor of this
// operator route is an OPERATOR; the target is a client user. They live
// in different collections behind different services, so the caller is
// resolved from module.ServiceOperatorUserProvider — the same lazy
// registry lookup terminateSessions uses — and never from
// h.clientUserService, which holds client-tier rows only. Looking the
// actor up there would miss on every real request, and D28's "a lookup
// failure is a 500" would then take the whole endpoint down.
//
// Three outcomes, all fail-closed:
//
//   - No authenticated principal on the context. There is no identity to
//     resolve, so the role is empty and canAssignRole's unknown tier (-1)
//     refuses every assignment. A degraded gate is a refusal of this
//     request, not a report of a broken database — same call the operator
//     handler makes.
//   - The operator provider is not registered. The user module registers
//     it unconditionally before it constructs this handler, so this is a
//     "cannot happen" — but a guard that cannot read its input must
//     refuse rather than sail past. 500.
//   - The lookup fails, or the row is absent. 500, NEVER a fallback to
//     the claim: falling back would make the claim authoritative again
//     exactly when the database cannot contradict it.
//
// The one exception is a synthetic dev-token principal in a
// non-production-like deployment, which has no row anywhere by design —
// see devTokenSystemRole for the three guards that keep it inert on
// staging and in production.
//
// Only called on a patch that names a role, so an ordinary profile patch
// costs no extra read.
func (h *AdminClientUserHandler) callerRole(ctx context.Context) (string, error) {
	actorUUID, _ := ctxauth.GetUserUUID(ctx)
	if actorUUID == "" {
		return "", nil
	}
	// A synthetic dev-token operator has no row in the operator store
	// either, and POST/PATCH /v1/admin/client-users are part of the same
	// documented local flow. Same three guards as the operator handler.
	if role, ok := devTokenSystemRole(ctx, h.platform, actorUUID); ok {
		return role, nil
	}
	if h.services == nil {
		slog.ErrorContext(ctx, "user: no service registry on the client-admin handler; refusing the client role assignment",
			slog.String("actor_uuid", actorUUID))
		return "", roleLookupUnavailable()
	}
	operators, ok := module.GetTyped[iface.UserProvider](h.services, module.ServiceOperatorUserProvider)
	if !ok || operators == nil {
		slog.ErrorContext(ctx, "user: operator user provider is not registered; refusing the client role assignment",
			slog.String("actor_uuid", actorUUID))
		return "", roleLookupUnavailable()
	}
	actor, err := operators.GetUserByID(ctx, actorUUID)
	if err != nil || actor == nil {
		slog.ErrorContext(ctx, "user: could not resolve the calling operator's system role; refusing the client role assignment",
			slog.String("actor_uuid", actorUUID),
			slog.Any("error", err))
		return "", roleLookupUnavailable()
	}
	return actor.Role, nil
}

// roleLookupUnavailable is the one 500 every failed caller-role lookup
// returns. Named rather than repeated so the three fail-closed branches
// above cannot drift apart, and deliberately carrying no detail about
// which of them fired — the operator log lines carry that, the client
// gets a retryable code and nothing about the platform's wiring.
func roleLookupUnavailable() error {
	return errcode.Internal(errcode.UserRoleLookupUnavailable,
		"Could not resolve the calling user's role. Retry shortly.")
}

// guardCreateRole is the role-assignment guard shared by the two client
// creation paths, CreateClientUserAdmin and InviteClientUserAdmin (ruling
// P32). Both bodies carry the same required
// `oneof=super_admin … guest` field, and a guard on the PATCH alone would
// be bypassable in one step: create — or invite — the user at the role
// you were refused, instead of patching them into it. M-17 would stay
// reachable while the finding was retired.
//
// Mirrors the operator tier's CreateUser exactly, including the
// `user.create.refused` audit row (with `resourceType="client_user"`) and
// the message. Shared between the two callers rather than copied so they
// cannot drift.
//
// serviceAccountRoleAllowed is deliberately absent, and structurally so
// rather than by oversight: iface.CreateUserInput.Kind is `json:"-"` and
// neither caller sets it, so a create can only ever produce a human
// principal — the guard could not fire and no test could falsify it. The
// operator tier makes the same call. TestClientAdminCreatePathsNeverMintAServiceAccount
// pins the premise, so if Kind ever becomes reachable from either body
// that test fails and this guard must grow the second half.
func (h *AdminClientUserHandler) guardCreateRole(ctx context.Context, role, email string) error {
	if role == "" {
		return nil
	}
	callerRole, err := h.callerRole(ctx)
	if err != nil {
		return err
	}
	if canAssignRole(callerRole, role) {
		return nil
	}
	actorUUID, actorEmail := actorFromCtx(ctx)
	h.emitAudit(ctx, iface.AuditEvent{
		ActorUserID:  actorUUID,
		ActorEmail:   actorEmail,
		ActorType:    "user",
		Action:       "user.create.refused",
		ResourceType: "client_user",
		Outcome:      "denied",
		Metadata: map[string]any{
			"code":      errcode.UserRoleEscalationForbidden,
			"attempted": "role_escalation",
			"to":        role,
			"email":     email,
		},
	})
	return errcode.Forbidden(errcode.UserRoleEscalationForbidden,
		"You cannot create a user with a role higher than your own")
}

// terminateSessions best-effort evicts every session of a client user
// whose access was just revoked (deactivate / delete). Resolves the
// client-tier auth service so an operator-tier session is never touched
// by a client-user lifecycle change. Silent on failure — see the
// operator handler's counterpart for the rationale.
func (h *AdminClientUserHandler) terminateSessions(ctx context.Context, userUUID string) {
	if h.services == nil || userUUID == "" {
		return
	}
	terminator, ok := module.GetTyped[iface.SessionTerminator](h.services, module.ServiceClientAuthService)
	if !ok || terminator == nil {
		return
	}
	if err := terminator.TerminateAllSessionsByUUID(ctx, userUUID); err != nil {
		slog.WarnContext(ctx, "user: could not terminate client sessions after access revocation",
			slog.String("user_uuid", userUUID),
			slog.String("error", err.Error()))
	}
}
