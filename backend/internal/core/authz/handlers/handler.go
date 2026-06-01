package handlers

import (
	"context"
	"errors"
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

// RegisterScopedMutationRoutes registers per-org role and binding mutations.
// These are the operations Block B's MFA enforcement covers — every path
// below can grant or revoke effective permissions for another user.
func (h *Handler) RegisterScopedMutationRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "create-role",
		Method:      http.MethodPost,
		Path:        "/v1/tenants/{tenantId}/authz/roles",
		Summary:     "Create a custom role",
		Tags:        []string{"Authorization"},
	}, h.createRole)

	huma.Register(api, huma.Operation{
		OperationID: "update-role",
		Method:      http.MethodPatch,
		Path:        "/v1/tenants/{tenantId}/authz/roles/{roleId}",
		Summary:     "Update a role (name/description/permissions for custom roles; isActive for any role)",
		Tags:        []string{"Authorization"},
	}, h.updateRole)

	huma.Register(api, huma.Operation{
		OperationID: "delete-role",
		Method:      http.MethodDelete,
		Path:        "/v1/tenants/{tenantId}/authz/roles/{roleId}",
		Summary:     "Delete a custom role (cascades bindings)",
		Tags:        []string{"Authorization"},
	}, h.deleteRole)

	huma.Register(api, huma.Operation{
		OperationID: "create-binding",
		Method:      http.MethodPost,
		Path:        "/v1/tenants/{tenantId}/authz/bindings",
		Summary:     "Grant a role to a user with optional expiration",
		Tags:        []string{"Authorization"},
	}, h.createBinding)

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

func (h *Handler) listRoles(ctx context.Context, in *listRolesInput) (*rolesOutput, error) {
	roles, err := h.svc.ListRoles(ctx, in.TenantID)
	if err != nil {
		return nil, huma.Error500InternalServerError("list roles failed", err)
	}
	return &rolesOutput{Body: models.RoleListResponse{Roles: roles}}, nil
}

func (h *Handler) createRole(ctx context.Context, in *createRoleInput) (*roleOutput, error) {
	role, err := h.svc.CreateRole(ctx, in.TenantID, in.Body)
	if err != nil {
		return nil, huma.Error400BadRequest("create role failed: " + err.Error())
	}
	return &roleOutput{Body: role}, nil
}

func (h *Handler) updateRole(ctx context.Context, in *updateRoleInput) (*roleOutput, error) {
	role, err := h.svc.UpdateRole(ctx, in.Role, in.Body)
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrNotFound):
			return nil, huma.Error404NotFound("role not found")
		case errors.Is(err, services.ErrSystemRoleImmutable):
			return nil, huma.Error403Forbidden("system roles cannot be edited — only disabled")
		default:
			return nil, huma.Error400BadRequest("update role failed: " + err.Error())
		}
	}
	return &roleOutput{Body: role}, nil
}

func (h *Handler) deleteRole(ctx context.Context, in *deleteRoleInput) (*struct{}, error) {
	if err := h.svc.DeleteRole(ctx, in.Role); err != nil {
		switch {
		case errors.Is(err, repository.ErrNotFound):
			return nil, huma.Error404NotFound("role not found")
		case errors.Is(err, services.ErrSystemRoleImmutable):
			return nil, huma.Error403Forbidden("system roles cannot be deleted")
		default:
			return nil, huma.Error400BadRequest("delete role failed: " + err.Error())
		}
	}
	return &struct{}{}, nil
}

func (h *Handler) listBindings(ctx context.Context, in *listBindingsInput) (*bindingsOutput, error) {
	bindings, err := h.svc.ListBindings(ctx, in.TenantID)
	if err != nil {
		return nil, huma.Error500InternalServerError("list bindings failed", err)
	}
	return &bindingsOutput{Body: models.BindingListResponse{Bindings: bindings}}, nil
}

func (h *Handler) createBinding(ctx context.Context, in *createBindingInput) (*bindingOutput, error) {
	grantedBy, _ := ctxauth.GetUserUUID(ctx)
	b, err := h.svc.CreateBinding(ctx, in.TenantID, grantedBy, in.Body)
	if err != nil {
		return nil, huma.Error400BadRequest("create binding failed: " + err.Error())
	}
	return &bindingOutput{Body: b}, nil
}

func (h *Handler) deleteBinding(ctx context.Context, in *deleteBindingInput) (*struct{}, error) {
	if err := h.svc.DeleteBinding(ctx, in.Binding); err != nil {
		return nil, huma.Error400BadRequest("delete binding failed: " + err.Error())
	}
	return &struct{}{}, nil
}

func (h *Handler) getEffective(ctx context.Context, in *effectiveInput) (*effectiveOutput, error) {
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
