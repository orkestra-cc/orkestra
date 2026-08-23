package handlers

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra/backend/internal/core/authz/models"
	"github.com/orkestra/backend/internal/core/authz/repository"
	"github.com/orkestra/backend/internal/core/authz/services"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
)

type Handler struct {
	svc *services.Service
}

func New(svc *services.Service) *Handler { return &Handler{svc: svc} }

// authzInternalError keeps evaluator, repository, and policy-engine details in
// logs. A handler that cannot classify a service error must not blame the
// caller with a 4xx or make the internal diagnostic part of the API contract.
func authzInternalError(ctx context.Context, operation string, err error) error {
	slog.ErrorContext(ctx, "authorization operation failed", "operation", operation, "error", err)
	return huma.Error500InternalServerError("Unable to " + operation + ". The failure has been logged.")
}

// --- Input/output envelopes ---

type permissionsOutput struct {
	Body models.PermissionCatalogResponse
}

type rolesOutput struct {
	Body models.RoleListResponse
}

type roleOutput struct {
	Body *models.Role
}

type bindingsOutput struct {
	Body models.BindingListResponse
}

type bindingOutput struct {
	Body *models.Binding
}

type effectiveOutput struct {
	Body models.EffectivePermissionsResponse
}

type listRolesInput struct {
	TenantID string `path:"tenantId"`
}

type createRoleInput struct {
	TenantID string `path:"tenantId"`
	Body     models.CreateRoleInput
}

type updateRoleInput struct {
	TenantID string `path:"tenantId"`
	Role     string `path:"roleId"`
	Body     models.UpdateRoleInput
}

type deleteRoleInput struct {
	TenantID string `path:"tenantId"`
	Role     string `path:"roleId"`
}

type createBindingInput struct {
	TenantID string `path:"tenantId"`
	Body     models.CreateBindingInput
}

type deleteBindingInput struct {
	TenantID string `path:"tenantId"`
	Binding  string `path:"bindingId"`
}

type listBindingsInput struct {
	TenantID string `path:"tenantId"`
}

type effectiveInput struct {
	TenantID string `path:"tenantId"`
}

// --- Routes ---

// RegisterGlobalRoutes registers permission-catalog routes that are read-only
// and don't need an org context (the catalog is shared across tenants).
func (h *Handler) RegisterGlobalRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "list-permissions",
		Method:      http.MethodGet,
		Path:        "/v1/authz/permissions",
		Summary:     "List the permission catalog (system-generated)",
		Tags:        []string{"Authorization"},
	}, h.listPermissions)
}

// RegisterScopedReadRoutes registers read-only per-org role and binding
// routes. Split from mutations so the module wiring can apply RequireMFA
// only to the paths that actually grant privilege.
func (h *Handler) RegisterScopedReadRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "list-roles",
		Method:      http.MethodGet,
		Path:        "/v1/tenants/{tenantId}/authz/roles",
		Summary:     "List roles (system + custom)",
		Tags:        []string{"Authorization"},
	}, h.listRoles)

	huma.Register(api, huma.Operation{
		OperationID: "list-bindings",
		Method:      http.MethodGet,
		Path:        "/v1/tenants/{tenantId}/authz/bindings",
		Summary:     "List role bindings in the org",
		Tags:        []string{"Authorization"},
	}, h.listBindings)

	huma.Register(api, huma.Operation{
		OperationID: "get-effective-permissions",
		Method:      http.MethodGet,
		Path:        "/v1/tenants/{tenantId}/authz/me",
		Summary:     "Get the current user's effective permissions in the org",
		Tags:        []string{"Authorization"},
	}, h.getEffective)
}

// The per-org role/binding mutations are split per permission (each mounted
// under its own permission + MFA + risk gate in module.go) so the declared
// fine-grained permissions (authz.role.create/update/delete,
// authz.binding.create/delete) are actually enforced — previously every
// mutation only required authz.role.read, which org_member/org_viewer hold.
// Every handler additionally asserts the {tenantId} path matches the caller's
// resolved tenant (assertTenantScope) and the service checks the role/binding's
// own orgId, closing the cross-tenant role/binding tampering IDOR.

// RegisterScopedRoleCreateRoutes — mounted under authz.role.create.
func (h *Handler) RegisterScopedRoleCreateRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "create-role",
		Method:      http.MethodPost,
		Path:        "/v1/tenants/{tenantId}/authz/roles",
		Summary:     "Create a custom role",
		Tags:        []string{"Authorization"},
	}, h.createRole)
}

// RegisterScopedRoleUpdateRoutes — mounted under authz.role.update.
func (h *Handler) RegisterScopedRoleUpdateRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "update-role",
		Method:      http.MethodPatch,
		Path:        "/v1/tenants/{tenantId}/authz/roles/{roleId}",
		Summary:     "Update a role (name/description/permissions for custom roles; isActive for any role)",
		Tags:        []string{"Authorization"},
	}, h.updateRole)
}

// RegisterScopedRoleDeleteRoutes — mounted under authz.role.delete.
func (h *Handler) RegisterScopedRoleDeleteRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "delete-role",
		Method:      http.MethodDelete,
		Path:        "/v1/tenants/{tenantId}/authz/roles/{roleId}",
		Summary:     "Delete a custom role (cascades bindings)",
		Tags:        []string{"Authorization"},
	}, h.deleteRole)
}

// RegisterScopedBindingCreateRoutes — mounted under authz.binding.create.
func (h *Handler) RegisterScopedBindingCreateRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "create-binding",
		Method:      http.MethodPost,
		Path:        "/v1/tenants/{tenantId}/authz/bindings",
		Summary:     "Grant a role to a user with optional expiration",
		Tags:        []string{"Authorization"},
	}, h.createBinding)
}

// RegisterScopedBindingDeleteRoutes — mounted under authz.binding.delete.
func (h *Handler) RegisterScopedBindingDeleteRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "delete-binding",
		Method:      http.MethodDelete,
		Path:        "/v1/tenants/{tenantId}/authz/bindings/{bindingId}",
		Summary:     "Revoke a role binding",
		Tags:        []string{"Authorization"},
	}, h.deleteBinding)
}

// --- Handler implementations ---

func (h *Handler) listPermissions(ctx context.Context, _ *struct{}) (*permissionsOutput, error) {
	perms, err := h.svc.ListPermissions(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("list permissions failed", err)
	}
	return &permissionsOutput{Body: models.PermissionCatalogResponse{Permissions: perms}}, nil
}

// assertTenantScope fails the request unless the {tenantId} path segment names
// the same tenant the auth middleware resolved for this request. The per-tenant
// authz routes gate a permission against the *resolved* tenant, but the handlers
// act on the *path* tenant (and on {roleId}/{bindingId} resolved by UUID), so
// without this any member of one tenant could read or tamper with another
// tenant's roles and bindings. 404 (not 403) to avoid a cross-tenant existence
// oracle. Platform-wide role administration is not exposed on this surface.
func assertTenantScope(ctx context.Context, pathTenantID string) error {
	scoped, ok := ctxauth.GetTenantID(ctx)
	if !ok || scoped == "" || pathTenantID != scoped {
		return huma.Error404NotFound("not found")
	}
	return nil
}

func (h *Handler) listRoles(ctx context.Context, in *listRolesInput) (*rolesOutput, error) {
	if err := assertTenantScope(ctx, in.TenantID); err != nil {
		return nil, err
	}
	roles, err := h.svc.ListRoles(ctx, in.TenantID)
	if err != nil {
		return nil, huma.Error500InternalServerError("list roles failed", err)
	}
	return &rolesOutput{Body: models.RoleListResponse{Roles: roles}}, nil
}

func (h *Handler) createRole(ctx context.Context, in *createRoleInput) (*roleOutput, error) {
	if err := assertTenantScope(ctx, in.TenantID); err != nil {
		return nil, err
	}
	role, err := h.svc.CreateRole(ctx, in.TenantID, in.Body)
	if err != nil {
		return nil, authzInternalError(ctx, "create the role", err)
	}
	return &roleOutput{Body: role}, nil
}

func (h *Handler) updateRole(ctx context.Context, in *updateRoleInput) (*roleOutput, error) {
	if err := assertTenantScope(ctx, in.TenantID); err != nil {
		return nil, err
	}
	role, err := h.svc.UpdateRole(ctx, in.TenantID, in.Role, in.Body)
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrNotFound):
			return nil, huma.Error404NotFound("role not found")
		case errors.Is(err, services.ErrSystemRoleImmutable):
			return nil, huma.Error403Forbidden("system roles cannot be edited — only disabled")
		default:
			return nil, authzInternalError(ctx, "update the role", err)
		}
	}
	return &roleOutput{Body: role}, nil
}

func (h *Handler) deleteRole(ctx context.Context, in *deleteRoleInput) (*struct{}, error) {
	if err := assertTenantScope(ctx, in.TenantID); err != nil {
		return nil, err
	}
	if err := h.svc.DeleteRole(ctx, in.TenantID, in.Role); err != nil {
		switch {
		case errors.Is(err, repository.ErrNotFound):
			return nil, huma.Error404NotFound("role not found")
		case errors.Is(err, services.ErrSystemRoleImmutable):
			return nil, huma.Error403Forbidden("system roles cannot be deleted")
		default:
			return nil, authzInternalError(ctx, "delete the role", err)
		}
	}
	return &struct{}{}, nil
}

func (h *Handler) listBindings(ctx context.Context, in *listBindingsInput) (*bindingsOutput, error) {
	if err := assertTenantScope(ctx, in.TenantID); err != nil {
		return nil, err
	}
	bindings, err := h.svc.ListBindings(ctx, in.TenantID)
	if err != nil {
		return nil, huma.Error500InternalServerError("list bindings failed", err)
	}
	return &bindingsOutput{Body: models.BindingListResponse{Bindings: bindings}}, nil
}

func (h *Handler) createBinding(ctx context.Context, in *createBindingInput) (*bindingOutput, error) {
	if err := assertTenantScope(ctx, in.TenantID); err != nil {
		return nil, err
	}
	grantedBy, _ := ctxauth.GetUserUUID(ctx)
	b, err := h.svc.CreateBinding(ctx, in.TenantID, grantedBy, in.Body)
	if err != nil {
		return nil, mapCreateBindingError(ctx, err)
	}
	return &bindingOutput{Body: b}, nil
}

func mapCreateBindingError(ctx context.Context, err error) error {
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return huma.Error404NotFound("role not found")
	case errors.Is(err, services.ErrRoleInactive):
		return huma.Error409Conflict("the selected role is inactive")
	case errors.Is(err, services.ErrSystemRoleNotGrantableInTenant):
		return huma.Error400BadRequest("platform roles must be granted globally")
	case errors.Is(err, services.ErrTenantRoleNotGrantableGlobally):
		return huma.Error400BadRequest("tenant roles must be granted within a tenant")
	case errors.Is(err, services.ErrInsufficientPermissionsToGrant):
		return huma.Error403Forbidden("you cannot grant a role with permissions you do not hold")
	case errors.Is(err, services.ErrGranterRequired):
		return huma.Error400BadRequest("the grantor is required")
	default:
		return authzInternalError(ctx, "create the role binding", err)
	}
}

func (h *Handler) deleteBinding(ctx context.Context, in *deleteBindingInput) (*struct{}, error) {
	if err := assertTenantScope(ctx, in.TenantID); err != nil {
		return nil, err
	}
	if err := h.svc.DeleteBinding(ctx, in.TenantID, in.Binding); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, huma.Error404NotFound("binding not found")
		}
		return nil, authzInternalError(ctx, "delete the role binding", err)
	}
	return &struct{}{}, nil
}

func (h *Handler) getEffective(ctx context.Context, in *effectiveInput) (*effectiveOutput, error) {
	if err := assertTenantScope(ctx, in.TenantID); err != nil {
		return nil, err
	}
	userUUID, ok := ctxauth.GetUserUUID(ctx)
	if !ok {
		return nil, huma.Error401Unauthorized("not authenticated")
	}
	perms, err := h.svc.GetEffectivePermissions(ctx, userUUID, in.TenantID)
	if err != nil {
		return nil, huma.Error500InternalServerError("effective permissions failed", err)
	}
	systemRole, _ := ctxauth.GetSystemRole(ctx)
	return &effectiveOutput{Body: models.EffectivePermissionsResponse{
		TenantID:    in.TenantID,
		Permissions: perms,
		SystemRole:  systemRole,
	}}, nil
}
