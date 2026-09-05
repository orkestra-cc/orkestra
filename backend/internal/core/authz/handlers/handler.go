package handlers

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra/backend/internal/core/authz/models"
	"github.com/orkestra/backend/internal/core/authz/repository"
	"github.com/orkestra/backend/internal/core/authz/services"
	"github.com/orkestra/backend/internal/shared/errcode"
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
	role, err := h.svc.CreateRole(ctx, in.TenantID, roleActor(ctx), in.Body)
	if err != nil {
		return nil, mapRoleWriteError(ctx, "create the role", err)
	}
	return &roleOutput{Body: role}, nil
}

func (h *Handler) updateRole(ctx context.Context, in *updateRoleInput) (*roleOutput, error) {
	if err := assertTenantScope(ctx, in.TenantID); err != nil {
		return nil, err
	}
	role, err := h.svc.UpdateRole(ctx, in.TenantID, in.Role, roleActor(ctx), in.Body)
	if err != nil {
		return nil, mapRoleWriteError(ctx, "update the role", err)
	}
	return &roleOutput{Body: role}, nil
}

// roleActor resolves the caller whose effective permissions bound a role
// write (D21). The authz service treats the literal platform sentinel as
// a waiver of the cascade, so a token subject that spelled it would
// inherit that waiver over HTTP; it is mapped to the empty actor, which
// the service refuses with ErrGranterRequired (400). Not reachable today
// — subjects are uuid.NewString() — but the waiver must only ever be
// chosen by in-process code, never named by a request.
func roleActor(ctx context.Context) string {
	actor, _ := ctxauth.GetUserUUID(ctx)
	if services.IsReservedActor(actor) {
		return ""
	}
	return actor
}

// mapRoleWriteError maps the failure modes createRole and updateRole
// share. Both run any supplied permission list through the same D21
// validator, so both answer with its sentinels; the not-found and
// system-role rows are reachable from the update path only, and so is
// the 503 — CreateRole is deliberately NOT wrapped in the cache gate
// (a role with no bindings changes nobody's effective permissions), so
// it can never produce ErrAuthzCacheUnavailable. Do not "fix" that by
// wrapping CreateRole.
func mapRoleWriteError(ctx context.Context, operation string, err error) error {
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return huma.Error404NotFound("role not found")
	case errors.Is(err, services.ErrSystemRoleImmutable):
		return huma.Error403Forbidden("system roles cannot be edited — only disabled")
	case errors.Is(err, services.ErrRoleNameRequired):
		return huma.Error400BadRequest("the role name cannot be empty")
	case errors.Is(err, services.ErrRolePermissionsRequired):
		return huma.Error400BadRequest("a role must carry at least one permission")
	case errors.Is(err, services.ErrGranterRequired):
		return huma.Error400BadRequest("the acting user is required")
	case errors.Is(err, services.ErrUnknownPermission):
		return errcode.UnprocessableEntity(errcode.AuthzPermissionUnknown,
			offendingPermission(err)+" has not been registered by any module, so no role may carry it.")
	case errors.Is(err, services.ErrSystemPermissionInCustomRole):
		return errcode.UnprocessableEntity(errcode.AuthzSystemPermissionForbidden,
			offendingPermission(err)+" is platform-reserved and cannot be granted through a tenant role.")
	case errors.Is(err, services.ErrInsufficientPermissionsToGrant):
		return huma.Error403Forbidden("you cannot give a role permissions you do not hold yourself")
	case errors.Is(err, services.ErrAuthzCacheUnavailable):
		return cacheUnavailableError()
	default:
		return authzInternalError(ctx, operation, err)
	}
}

// cacheUnavailableError renders the D27 gate's refusal. A GRANT retires
// the cached verdicts it invalidates BEFORE it writes; when that cannot
// be done the change is not applied at all. That is a transient
// server-side condition the caller should retry, not a request they can
// correct — so it is a 503 carrying a code the operator console can
// classify, never the codeless 500 the update path used to fall through
// to.
//
// The detail is deliberately fixed and carries no cause: the service
// logged the underlying store error and counted the refusal at the
// point where it happened (withGeneration), which is the only place the
// cause exists. Revocations never reach here — they write first and
// report through those same logs and metrics (ruling P22).
func cacheUnavailableError() error {
	return errcode.ServiceUnavailable(errcode.AuthzCacheUnavailable,
		"The permission cache could not be updated, so the change was not applied. Try again in a moment.")
}

// offendingPermission renders the sentence subject for the two 422s: the
// quoted key the validator refused, so the operator can see which entry
// of their list is wrong without diffing. strconv.Quote also escapes
// control characters — the key is caller-supplied, and the validator has
// already bounded its length. Falls back to a neutral subject when the
// error carries no key: every in-tree path wraps one, but a bare
// sentinel must not render an empty pair of quotes.
func offendingPermission(err error) string {
	if key, ok := services.OffendingPermissionKey(err); ok {
		return "The permission " + strconv.Quote(key)
	}
	return "That permission"
}

func (h *Handler) deleteRole(ctx context.Context, in *deleteRoleInput) (*struct{}, error) {
	if err := assertTenantScope(ctx, in.TenantID); err != nil {
		return nil, err
	}
	if err := h.svc.DeleteRole(ctx, in.TenantID, in.Role); err != nil {
		return nil, mapRoleDeleteError(ctx, err)
	}
	return &struct{}{}, nil
}

// mapRoleDeleteError is deleteRole's mapper, extracted so the wire
// contract is testable the way mapRoleWriteError's is.
//
// No cache row: a delete is a revocation, and P22 forbids refusing one
// for a cache reason. DeleteRole writes first and reports an
// invalidation failure through logs and metrics, so it never returns
// ErrAuthzCacheUnavailable and this mapper must never learn to answer
// 503 — that would mean the revocation had been refused.
func mapRoleDeleteError(ctx context.Context, err error) error {
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return huma.Error404NotFound("role not found")
	case errors.Is(err, services.ErrSystemRoleImmutable):
		return huma.Error403Forbidden("system roles cannot be deleted")
	default:
		return authzInternalError(ctx, "delete the role", err)
	}
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
	case errors.Is(err, services.ErrBindingExists):
		return huma.Error409Conflict("this role is already bound to the user in this tenant")
	case errors.Is(err, services.ErrAuthzCacheUnavailable):
		return cacheUnavailableError()
	default:
		return authzInternalError(ctx, "create the role binding", err)
	}
}

func (h *Handler) deleteBinding(ctx context.Context, in *deleteBindingInput) (*struct{}, error) {
	if err := assertTenantScope(ctx, in.TenantID); err != nil {
		return nil, err
	}
	if err := h.svc.DeleteBinding(ctx, in.TenantID, in.Binding); err != nil {
		return nil, mapBindingDeleteError(ctx, err)
	}
	return &struct{}{}, nil
}

// mapBindingDeleteError is deleteBinding's mapper, extracted for the
// same reason as mapRoleDeleteError — and carrying no cache row for the
// same reason: revoking a binding is never refused over a cache.
func mapBindingDeleteError(ctx context.Context, err error) error {
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return huma.Error404NotFound("binding not found")
	default:
		return authzInternalError(ctx, "delete the role binding", err)
	}
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
