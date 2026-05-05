package handlers

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra/backend/internal/core/tenant/models"
	"github.com/orkestra/backend/internal/core/tenant/repository"
	"github.com/orkestra/backend/internal/core/tenant/services"
	"github.com/orkestra/backend/internal/shared/iface"
	"github.com/orkestra/backend/internal/shared/middleware"
	"github.com/orkestra/backend/internal/shared/module"
)

type Handler struct {
	svc *services.Service
	// registry is used at request time to resolve the optional aggregator
	// providers (subscriptions, payments). Looked up lazily because those
	// addons init after core/tenant; capturing the typed interfaces at
	// boot would freeze them as nil.
	registry *module.ServiceRegistry
}

// New wires a handler to the tenant service. The registry is optional;
// when nil the aggregator endpoints degrade to empty results.
func New(svc *services.Service, registry *module.ServiceRegistry) *Handler {
	return &Handler{svc: svc, registry: registry}
}

// --- Request/response envelopes ---

type listMyTenantsOutput struct {
	Body struct {
		Memberships []memberDTO `json:"memberships"`
	}
}

type memberDTO struct {
	TenantID   string   `json:"tenantId"`
	Name       string   `json:"name"`
	Slug       string   `json:"slug"`
	Plan       string   `json:"plan"`
	Kind       string   `json:"kind"`
	Roles      []string `json:"roles"`
	IsOwner    bool     `json:"isOwner"`
}

type createTenantInput struct {
	Body models.CreateTenantInput
}

type tenantOutput struct {
	Body *models.Tenant
}

type tenantIDPath struct {
	TenantID string `path:"tenantId"`
}

type updateTenantInput struct {
	TenantID string `path:"tenantId"`
	Body     models.UpdateTenantInput
}

type updatePlanInput struct {
	TenantID string `path:"tenantId"`
	Body     models.UpdatePlanInput
}

type membershipRow struct {
	models.TenantMembership
	Email string `json:"email,omitempty"`
}

type membershipListOutput struct {
	Body struct {
		Members []membershipRow `json:"members"`
	}
}

type inviteInput struct {
	TenantID string `path:"tenantId"`
	Body     models.InviteInput
}

type inviteOutput struct {
	Body *models.TenantInvite
}

type acceptInviteInput struct {
	Body models.AcceptInviteInput
}

// --- Route registration ---

// RegisterGlobalRoutes registers routes that do not require a tenant context
// (listing your tenants, creating a new tenant, accepting an invite).
func (h *Handler) RegisterGlobalRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "list-my-tenants",
		Method:      http.MethodGet,
		Path:        "/v1/tenants",
		Summary:     "List tenants the current user belongs to",
		Tags:        []string{"Tenants"},
	}, h.listMyTenants)

	huma.Register(api, huma.Operation{
		OperationID: "create-tenant",
		Method:      http.MethodPost,
		Path:        "/v1/tenants",
		Summary:     "Create a new tenant (caller becomes the owner)",
		Tags:        []string{"Tenants"},
	}, h.createTenant)

	huma.Register(api, huma.Operation{
		OperationID: "accept-invite",
		Method:      http.MethodPost,
		Path:        "/v1/tenants/accept-invite",
		Summary:     "Accept a pending tenant invitation",
		Tags:        []string{"Tenants"},
	}, h.acceptInvite)
}

// RegisterScopedReadRoutes registers read-only per-tenant routes. Safe to
// mount behind the tenant.read permission without MFA.
func (h *Handler) RegisterScopedReadRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "get-tenant",
		Method:      http.MethodGet,
		Path:        "/v1/tenants/{tenantId}",
		Summary:     "Get a tenant by id",
		Tags:        []string{"Tenants"},
	}, h.getTenant)

	huma.Register(api, huma.Operation{
		OperationID: "list-members",
		Method:      http.MethodGet,
		Path:        "/v1/tenants/{tenantId}/members",
		Summary:     "List tenant members",
		Tags:        []string{"Tenants"},
	}, h.listMembers)

	huma.Register(api, huma.Operation{
		OperationID: "list-divisions",
		Method:      http.MethodGet,
		Path:        "/v1/tenants/{tenantId}/divisions",
		Summary:     "List this tenant's divisions",
		Description: "Closure-table lookup of direct children (depth=1). Internal tenants never have divisions and always return an empty list.",
		Tags:        []string{"Tenants"},
	}, h.listDivisions)
}

// RegisterScopedMutationRoutes registers per-tenant mutations. MFA required
// per Block B — each can change permissions, plan, or destroy the tenant.
func (h *Handler) RegisterScopedMutationRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "update-tenant",
		Method:      http.MethodPatch,
		Path:        "/v1/tenants/{tenantId}",
		Summary:     "Update tenant name, slug or settings",
		Tags:        []string{"Tenants"},
	}, h.updateTenant)

	huma.Register(api, huma.Operation{
		OperationID: "delete-tenant",
		Method:      http.MethodDelete,
		Path:        "/v1/tenants/{tenantId}",
		Summary:     "Archive the tenant (owner only)",
		Tags:        []string{"Tenants"},
	}, h.deleteTenant)

	huma.Register(api, huma.Operation{
		OperationID: "update-plan",
		Method:      http.MethodPatch,
		Path:        "/v1/tenants/{tenantId}/plan",
		Summary:     "Change plan and features",
		Tags:        []string{"Tenants"},
	}, h.updatePlan)

	huma.Register(api, huma.Operation{
		OperationID: "remove-member",
		Method:      http.MethodDelete,
		Path:        "/v1/tenants/{tenantId}/members/{userUUID}",
		Summary:     "Remove a member from the tenant",
		Tags:        []string{"Tenants"},
	}, h.removeMember)

	huma.Register(api, huma.Operation{
		OperationID: "create-invite",
		Method:      http.MethodPost,
		Path:        "/v1/tenants/{tenantId}/invites",
		Summary:     "Invite a user to the tenant",
		Tags:        []string{"Tenants"},
	}, h.createInvite)

	huma.Register(api, huma.Operation{
		OperationID: "create-division",
		Method:      http.MethodPost,
		Path:        "/v1/tenants/{tenantId}/divisions",
		Summary:     "Create a division under this external tenant",
		Description: "Creates a sub-tenant (Kind=external, ParentTenantUUID=this). Refuses when the parent is internal.",
		Tags:        []string{"Tenants"},
	}, h.createDivision)
}

// --- Handler implementations ---

func (h *Handler) listMyTenants(ctx context.Context, _ *struct{}) (*listMyTenantsOutput, error) {
	userUUID, ok := middleware.GetUserUUID(ctx)
	if !ok {
		return nil, huma.Error401Unauthorized("not authenticated")
	}
	mbrs, err := h.svc.ListUserMemberships(ctx, userUUID)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to list memberships", err)
	}
	out := &listMyTenantsOutput{}
	for _, m := range mbrs {
		t, err := h.svc.GetTenantModel(ctx, m.TenantUUID)
		if err != nil {
			continue
		}
		out.Body.Memberships = append(out.Body.Memberships, memberDTO{
			TenantID: m.TenantUUID,
			Name:     m.TenantName,
			Slug:     m.TenantSlug,
			Plan:     t.Plan,
			Kind:     m.TenantKind,
			Roles:    m.Roles,
			IsOwner:  m.IsOwner,
		})
	}
	return out, nil
}

func (h *Handler) createTenant(ctx context.Context, in *createTenantInput) (*tenantOutput, error) {
	userUUID, ok := middleware.GetUserUUID(ctx)
	if !ok {
		return nil, huma.Error401Unauthorized("not authenticated")
	}
	t, err := h.svc.CreateTenant(ctx, userUUID, in.Body)
	if err != nil {
		return nil, huma.Error400BadRequest("failed to create tenant: " + err.Error())
	}
	return &tenantOutput{Body: t}, nil
}

func (h *Handler) getTenant(ctx context.Context, in *tenantIDPath) (*tenantOutput, error) {
	t, err := h.svc.GetTenantModel(ctx, in.TenantID)
	if err != nil {
		return nil, huma.Error404NotFound("tenant not found")
	}
	return &tenantOutput{Body: t}, nil
}

func (h *Handler) updateTenant(ctx context.Context, in *updateTenantInput) (*tenantOutput, error) {
	if err := h.svc.UpdateTenant(ctx, in.TenantID, in.Body); err != nil {
		return nil, huma.Error400BadRequest("update failed: " + err.Error())
	}
	t, err := h.svc.GetTenantModel(ctx, in.TenantID)
	if err != nil {
		return nil, huma.Error404NotFound("tenant not found")
	}
	return &tenantOutput{Body: t}, nil
}

func (h *Handler) deleteTenant(ctx context.Context, in *tenantIDPath) (*struct{}, error) {
	if err := h.svc.DeleteTenant(ctx, in.TenantID); err != nil {
		return nil, huma.Error400BadRequest("delete failed: " + err.Error())
	}
	return &struct{}{}, nil
}

// purgeTenant is the irreversible GDPR erasure path — flips status to
// purged AND crypto-shreds the tenant's KMS key. After this call every
// ciphertext wrapped with that key is unrecoverable. Gated (at the
// route level) by system.tenants.admin; future work adds a second MFA
// step-up + a 7-day delay between archive and purge to match SOC2
// expectations.
func (h *Handler) purgeTenant(ctx context.Context, in *tenantIDPath) (*struct{}, error) {
	if err := h.svc.PurgeTenant(ctx, in.TenantID); err != nil {
		return nil, huma.Error400BadRequest("purge failed: " + err.Error())
	}
	return &struct{}{}, nil
}

func (h *Handler) updatePlan(ctx context.Context, in *updatePlanInput) (*tenantOutput, error) {
	if err := h.svc.UpdatePlan(ctx, in.TenantID, in.Body); err != nil {
		return nil, huma.Error400BadRequest("plan update failed: " + err.Error())
	}
	t, err := h.svc.GetTenantModel(ctx, in.TenantID)
	if err != nil {
		return nil, huma.Error404NotFound("tenant not found")
	}
	return &tenantOutput{Body: t}, nil
}

func (h *Handler) listMembers(ctx context.Context, in *tenantIDPath) (*membershipListOutput, error) {
	members, err := h.svc.ListMembers(ctx, in.TenantID)
	if err != nil {
		return nil, huma.Error500InternalServerError("list failed", err)
	}
	out := &membershipListOutput{}
	out.Body.Members = make([]membershipRow, len(members))
	for i, m := range members {
		out.Body.Members[i] = membershipRow{TenantMembership: m}
	}
	h.enrichMemberEmails(ctx, in.TenantID, out.Body.Members)
	return out, nil
}

// enrichMemberEmails fills in the Email field of each row by looking up the
// user via the tier-appropriate UserProvider. Best-effort: any failure leaves
// Email empty so the table still renders.
func (h *Handler) enrichMemberEmails(ctx context.Context, tenantUUID string, rows []membershipRow) {
	if h.registry == nil || len(rows) == 0 {
		return
	}
	kind := iface.TenantKindInternal
	if t, err := h.svc.GetTenant(ctx, tenantUUID); err == nil && t != nil && t.Kind != "" {
		kind = t.Kind
	}
	providerKey := module.ServiceOperatorUserProvider
	if kind == iface.TenantKindExternal {
		providerKey = module.ServiceClientUserProvider
	}
	provider, ok := module.GetTyped[iface.UserProvider](h.registry, providerKey)
	if !ok || provider == nil {
		return
	}
	for i := range rows {
		u, err := provider.GetUserByID(ctx, rows[i].UserUUID)
		if err != nil || u == nil {
			continue
		}
		rows[i].Email = u.Email
	}
}

type removeMemberInput struct {
	TenantID string `path:"tenantId"`
	UserUUID string `path:"userUUID"`
}

func (h *Handler) removeMember(ctx context.Context, in *removeMemberInput) (*struct{}, error) {
	if err := h.svc.RemoveMember(ctx, in.TenantID, in.UserUUID); err != nil {
		return nil, huma.Error400BadRequest("remove failed: " + err.Error())
	}
	return &struct{}{}, nil
}

func (h *Handler) createInvite(ctx context.Context, in *inviteInput) (*inviteOutput, error) {
	userUUID, _ := middleware.GetUserUUID(ctx)
	inv, err := h.svc.CreateInvite(ctx, in.TenantID, userUUID, in.Body)
	if err != nil {
		return nil, huma.Error400BadRequest("invite failed: " + err.Error())
	}
	return &inviteOutput{Body: inv}, nil
}

func (h *Handler) acceptInvite(ctx context.Context, in *acceptInviteInput) (*tenantOutput, error) {
	userUUID, ok := middleware.GetUserUUID(ctx)
	if !ok {
		return nil, huma.Error401Unauthorized("not authenticated")
	}
	t, err := h.svc.AcceptInvite(ctx, userUUID, in.Body.Token)
	if err != nil {
		return nil, huma.Error400BadRequest("accept failed: " + err.Error())
	}
	return &tenantOutput{Body: t}, nil
}

// --- Platform admin routes ---

type adminTenantListItem struct {
	models.Tenant
	MemberCount int `json:"memberCount"`
}

type adminTenantListInput struct {
	IncludeDeleted bool `query:"includeDeleted"`
	// Kind narrows the list to one tier. Empty returns both.
	Kind string `query:"kind" enum:",internal,external"`
	// ParentTenantUUID narrows to direct children of the given parent. Mutually
	// exclusive with RootsOnly. Ignored when RootsOnly is true.
	ParentTenantUUID string `query:"parentTenantUUID"`
	// RootsOnly restricts to tenants that have no parent (used by the Clients
	// root list to exclude divisions).
	RootsOnly bool `query:"rootsOnly"`
}

type adminTenantListOutput struct {
	Body struct {
		Tenants []adminTenantListItem `json:"tenants"`
	}
}

type adminInviteListInput struct {
	TenantID        string `path:"tenantId"`
	IncludeAccepted bool   `query:"includeAccepted"`
}

type adminInviteListOutput struct {
	Body struct {
		Invites []models.TenantInvite `json:"invites"`
	}
}

type adminRevokeInviteInput struct {
	TenantID string `path:"tenantId"`
	InviteID string `path:"inviteId"`
}

// RegisterAdminRoutes registers platform-admin routes under /v1/admin/tenants.
// Gated by system.tenants.admin in module.go.
func (h *Handler) RegisterAdminRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "list-all-tenants-admin",
		Method:      http.MethodGet,
		Path:        "/v1/admin/tenants",
		Summary:     "List every tenant (platform admin)",
		Tags:        []string{"Tenants Admin"},
	}, h.listAllTenantsAdmin)

	huma.Register(api, huma.Operation{
		OperationID: "get-tenant-admin",
		Method:      http.MethodGet,
		Path:        "/v1/admin/tenants/{tenantId}",
		Summary:     "Get a tenant (platform admin)",
		Tags:        []string{"Tenants Admin"},
	}, h.getTenant)

	huma.Register(api, huma.Operation{
		OperationID: "update-tenant-admin",
		Method:      http.MethodPatch,
		Path:        "/v1/admin/tenants/{tenantId}",
		Summary:     "Update a tenant (platform admin)",
		Tags:        []string{"Tenants Admin"},
	}, h.updateTenant)

	huma.Register(api, huma.Operation{
		OperationID: "delete-tenant-admin",
		Method:      http.MethodDelete,
		Path:        "/v1/admin/tenants/{tenantId}",
		Summary:     "Archive a tenant (platform admin)",
		Tags:        []string{"Tenants Admin"},
	}, h.deleteTenant)

	huma.Register(api, huma.Operation{
		OperationID: "purge-tenant-admin",
		Method:      http.MethodPost,
		Path:        "/v1/admin/tenants/{tenantId}/purge",
		Summary:     "Purge a tenant (irreversible — crypto-shreds the KMS key)",
		Description: "GDPR right-to-erasure at the tenant level. Flips the tenant status to purged AND deletes the wrapped DEK; every ciphertext sealed with that key becomes cryptographically unrecoverable. There is no undo.",
		Tags:        []string{"Tenants Admin"},
	}, h.purgeTenant)

	huma.Register(api, huma.Operation{
		OperationID: "update-tenant-plan-admin",
		Method:      http.MethodPatch,
		Path:        "/v1/admin/tenants/{tenantId}/plan",
		Summary:     "Change a tenant's plan (platform admin)",
		Tags:        []string{"Tenants Admin"},
	}, h.updatePlan)

	huma.Register(api, huma.Operation{
		OperationID: "list-tenant-members-admin",
		Method:      http.MethodGet,
		Path:        "/v1/admin/tenants/{tenantId}/members",
		Summary:     "List tenant members (platform admin)",
		Tags:        []string{"Tenants Admin"},
	}, h.listMembers)

	huma.Register(api, huma.Operation{
		OperationID: "remove-tenant-member-admin",
		Method:      http.MethodDelete,
		Path:        "/v1/admin/tenants/{tenantId}/members/{userUUID}",
		Summary:     "Remove a member from a tenant (platform admin)",
		Tags:        []string{"Tenants Admin"},
	}, h.removeMember)

	huma.Register(api, huma.Operation{
		OperationID: "list-tenant-invites-admin",
		Method:      http.MethodGet,
		Path:        "/v1/admin/tenants/{tenantId}/invites",
		Summary:     "List pending tenant invites (platform admin)",
		Tags:        []string{"Tenants Admin"},
	}, h.listInvitesAdmin)

	huma.Register(api, huma.Operation{
		OperationID: "create-tenant-invite-admin",
		Method:      http.MethodPost,
		Path:        "/v1/admin/tenants/{tenantId}/invites",
		Summary:     "Create a tenant invite (platform admin)",
		Tags:        []string{"Tenants Admin"},
	}, h.createInvite)

	huma.Register(api, huma.Operation{
		OperationID: "revoke-tenant-invite-admin",
		Method:      http.MethodDelete,
		Path:        "/v1/admin/tenants/{tenantId}/invites/{inviteId}",
		Summary:     "Revoke a pending tenant invite (platform admin)",
		Tags:        []string{"Tenants Admin"},
	}, h.revokeInviteAdmin)

	// --- Divisions + cross-module aggregators (Phase 2) ---

	huma.Register(api, huma.Operation{
		OperationID: "list-tenant-divisions-admin",
		Method:      http.MethodGet,
		Path:        "/v1/admin/tenants/{tenantId}/divisions",
		Summary:     "List direct sub-tenants (divisions) of an external tenant",
		Description: "Returns tenants whose ParentTenantUUID equals the given tenant. Internal tenants never have divisions and always return an empty list.",
		Tags:        []string{"Tenants Admin"},
	}, h.listDivisionsAdmin)

	huma.Register(api, huma.Operation{
		OperationID: "create-tenant-division-admin",
		Method:      http.MethodPost,
		Path:        "/v1/admin/tenants/{tenantId}/divisions",
		Summary:     "Create a division under an external tenant (platform admin)",
		Description: "Creates a Tier-2 tenant with Kind=external and ParentTenantUUID set. Refuses if the parent is internal.",
		Tags:        []string{"Tenants Admin"},
	}, h.createDivisionAdmin)

	huma.Register(api, huma.Operation{
		OperationID: "list-tenant-subscriptions-admin",
		Method:      http.MethodGet,
		Path:        "/v1/admin/tenants/{tenantId}/subscriptions",
		Summary:     "List a tenant's subscriptions (platform admin)",
		Description: "Aggregator over the subscriptions module. Returns an empty list when the subscriptions addon is disabled.",
		Tags:        []string{"Tenants Admin"},
	}, h.listTenantSubscriptionsAdmin)

	huma.Register(api, huma.Operation{
		OperationID: "list-tenant-payments-admin",
		Method:      http.MethodGet,
		Path:        "/v1/admin/tenants/{tenantId}/payments",
		Summary:     "List a tenant's payment transactions (platform admin)",
		Description: "Aggregator over the payments module. Returns an empty list when the payments addon is disabled.",
		Tags:        []string{"Tenants Admin"},
	}, h.listTenantPaymentsAdmin)

	huma.Register(api, huma.Operation{
		OperationID: "get-tenant-billing-customer-admin",
		Method:      http.MethodGet,
		Path:        "/v1/admin/tenants/{tenantId}/billing-customer",
		Summary:     "Get a tenant's linked FatturaPA customer (platform admin)",
		Description: "Aggregator over the billing module. Returns 404 when no customer is linked or the billing addon is disabled. ADR-0001 PR-4.",
		Tags:        []string{"Tenants Admin"},
	}, h.getTenantBillingCustomerAdmin)

	huma.Register(api, huma.Operation{
		OperationID: "promote-tenant-to-billing-customer-admin",
		Method:      http.MethodPost,
		Path:        "/v1/admin/tenants/{tenantId}/billing-customer",
		Summary:     "Create a FatturaPA customer for the tenant (platform admin)",
		Description: "Idempotent — when a customer is already linked, returns the existing row. Otherwise creates a new Customer pre-filled from the tenant's iface.Tenant fields (LegalName, VATNumber, FiscalCode, Country, Email). Address fields are intentionally left empty: iface.Tenant doesn't carry the breakdown. ADR-0001 PR-4.",
		Tags:        []string{"Tenants Admin"},
	}, h.promoteTenantToBillingCustomerAdmin)
}

func (h *Handler) listAllTenantsAdmin(ctx context.Context, in *adminTenantListInput) (*adminTenantListOutput, error) {
	filter := repository.TenantListFilter{IncludeDeleted: in.IncludeDeleted, RootsOnly: in.RootsOnly}
	if in.Kind != "" {
		kind := models.TenantKind(in.Kind)
		if !kind.Valid() {
			return nil, huma.Error400BadRequest("invalid kind: must be internal or external")
		}
		filter.Kind = kind
	}
	if !in.RootsOnly && in.ParentTenantUUID != "" {
		p := in.ParentTenantUUID
		filter.ParentTenantUUID = &p
	}
	views, err := h.svc.ListAllTenantsFiltered(ctx, filter)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to list tenants", err)
	}
	out := &adminTenantListOutput{}
	out.Body.Tenants = make([]adminTenantListItem, 0, len(views))
	for _, v := range views {
		out.Body.Tenants = append(out.Body.Tenants, adminTenantListItem{Tenant: *v.Tenant, MemberCount: v.MemberCount})
	}
	return out, nil
}

func (h *Handler) listInvitesAdmin(ctx context.Context, in *adminInviteListInput) (*adminInviteListOutput, error) {
	onlyPending := !in.IncludeAccepted
	invs, err := h.svc.ListInvites(ctx, in.TenantID, onlyPending)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to list invites", err)
	}
	out := &adminInviteListOutput{}
	out.Body.Invites = invs
	return out, nil
}

func (h *Handler) revokeInviteAdmin(ctx context.Context, in *adminRevokeInviteInput) (*struct{}, error) {
	if err := h.svc.RevokeInvite(ctx, in.TenantID, in.InviteID); err != nil {
		return nil, huma.Error400BadRequest("revoke failed: " + err.Error())
	}
	return &struct{}{}, nil
}

// --- Divisions (Phase 2) ---

type divisionListOutput struct {
	Body struct {
		Divisions []models.Tenant `json:"divisions"`
	}
}

type createDivisionInput struct {
	TenantID string `path:"tenantId"`
	Body     struct {
		Name string `json:"name" validate:"required,min=1,max=120"`
		Slug string `json:"slug,omitempty"`
	}
}

// listDivisions serves the tenant-scoped list-divisions route. Auth gate is
// tenant.read on the parent — same as list-members.
func (h *Handler) listDivisions(ctx context.Context, in *tenantIDPath) (*divisionListOutput, error) {
	rows, err := h.svc.ListDivisions(ctx, in.TenantID)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to list divisions", err)
	}
	out := &divisionListOutput{}
	out.Body.Divisions = rows
	return out, nil
}

// listDivisionsAdmin is the platform-admin variant — same handler body,
// different auth group (system.tenants.admin). A tenant not found is
// represented as an empty list rather than 404 so admin dashboards can
// render cleanly when the parent id is a mis-type.
func (h *Handler) listDivisionsAdmin(ctx context.Context, in *tenantIDPath) (*divisionListOutput, error) {
	return h.listDivisions(ctx, in)
}

// createDivision is the tenant-scoped create path (tenant.read + MFA). The
// caller must be an authenticated user; the new division is owned by them
// and carries ParentTenantUUID=the current tenant.
func (h *Handler) createDivision(ctx context.Context, in *createDivisionInput) (*tenantOutput, error) {
	userUUID, ok := middleware.GetUserUUID(ctx)
	if !ok || userUUID == "" {
		return nil, huma.Error401Unauthorized("not authenticated")
	}
	t, err := h.svc.CreateDivision(ctx, in.TenantID, userUUID, in.Body.Name, in.Body.Slug)
	if err != nil {
		return nil, huma.Error400BadRequest("division create failed: " + err.Error())
	}
	return &tenantOutput{Body: t}, nil
}

// createDivisionAdmin is the platform-admin variant. Same mechanics; the
// admin acts as the initial owner of the new division until the client
// transfers ownership.
func (h *Handler) createDivisionAdmin(ctx context.Context, in *createDivisionInput) (*tenantOutput, error) {
	return h.createDivision(ctx, in)
}

// --- Cross-module aggregators (Phase 2) ---

type tenantSubscriptionsOutput struct {
	Body struct {
		Subscriptions []iface.TenantSubscription `json:"subscriptions"`
	}
}

type tenantPaymentsOutput struct {
	Body struct {
		Payments []iface.TenantPayment `json:"payments"`
	}
}

type tenantBillingCustomerOutput struct {
	Body *iface.TenantBillingCustomer
}

// listTenantSubscriptionsAdmin proxies to the subscriptions module via the
// TenantSubscriptionProvider iface. When the addon is disabled (nil
// registry lookup) the endpoint returns an empty list rather than 500 —
// the admin dashboard can render an empty "Subscriptions" tab cleanly.
func (h *Handler) listTenantSubscriptionsAdmin(ctx context.Context, in *tenantIDPath) (*tenantSubscriptionsOutput, error) {
	out := &tenantSubscriptionsOutput{}
	out.Body.Subscriptions = []iface.TenantSubscription{}
	if h.registry == nil {
		return out, nil
	}
	provider, ok := module.GetTyped[iface.TenantSubscriptionProvider](h.registry, module.ServiceTenantSubscriptionProvider)
	if !ok || provider == nil {
		return out, nil
	}
	rows, err := provider.ListByTenant(ctx, in.TenantID)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to list tenant subscriptions", err)
	}
	out.Body.Subscriptions = rows
	return out, nil
}

// listTenantPaymentsAdmin mirrors listTenantSubscriptionsAdmin for payments.
func (h *Handler) listTenantPaymentsAdmin(ctx context.Context, in *tenantIDPath) (*tenantPaymentsOutput, error) {
	out := &tenantPaymentsOutput{}
	out.Body.Payments = []iface.TenantPayment{}
	if h.registry == nil {
		return out, nil
	}
	provider, ok := module.GetTyped[iface.TenantPaymentProvider](h.registry, module.ServiceTenantPaymentProvider)
	if !ok || provider == nil {
		return out, nil
	}
	rows, err := provider.ListByTenant(ctx, in.TenantID)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to list tenant payments", err)
	}
	out.Body.Payments = rows
	return out, nil
}

// getTenantBillingCustomerAdmin returns the FatturaPA Customer linked to
// the given tenant. Diverges from the listing aggregators: rather than
// returning [] when no link exists, it returns 404 — the resource is
// singular, not a collection. Returns 404 (with a different reason) when
// the billing module is disabled, so the admin UI can degrade gracefully.
// ADR-0001 PR-4.
func (h *Handler) getTenantBillingCustomerAdmin(ctx context.Context, in *tenantIDPath) (*tenantBillingCustomerOutput, error) {
	if h.registry == nil {
		return nil, huma.Error404NotFound("billing module unavailable")
	}
	provider, ok := module.GetTyped[iface.TenantBillingCustomerProvider](h.registry, module.ServiceTenantBillingCustomerProvider)
	if !ok || provider == nil {
		return nil, huma.Error404NotFound("billing module unavailable")
	}
	customer, err := provider.GetByTenant(ctx, in.TenantID)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to load tenant billing customer", err)
	}
	if customer == nil {
		return nil, huma.Error404NotFound("no billing customer linked to this tenant")
	}
	return &tenantBillingCustomerOutput{Body: customer}, nil
}

// promoteTenantToBillingCustomerAdmin creates (or returns the existing)
// FatturaPA Customer for the given tenant. Idempotent — repeat calls
// return the existing row. Errors are mapped to specific HTTP statuses so
// the frontend can show a meaningful toast:
//
//   - 503 when the billing module is disabled (registry has no provider)
//   - 404 when the tenant doesn't exist
//   - 422 when the tenant is internal (only Tier-2 can have a billing profile)
//   - 422 when the tenant has no LegalName/Name to seed Denomination
//   - 500 for everything else
//
// ADR-0001 PR-4.
func (h *Handler) promoteTenantToBillingCustomerAdmin(ctx context.Context, in *tenantIDPath) (*tenantBillingCustomerOutput, error) {
	if h.registry == nil {
		return nil, huma.Error503ServiceUnavailable("billing module unavailable")
	}
	provider, ok := module.GetTyped[iface.TenantBillingCustomerProvider](h.registry, module.ServiceTenantBillingCustomerProvider)
	if !ok || provider == nil {
		return nil, huma.Error503ServiceUnavailable("billing module unavailable")
	}
	customer, err := provider.PromoteTenant(ctx, in.TenantID)
	if err != nil {
		return nil, mapPromoteCustomerError(err)
	}
	return &tenantBillingCustomerOutput{Body: customer}, nil
}

// mapPromoteCustomerError translates the billing service's sentinel errors
// into Huma HTTP errors. Kept as a small helper so the handler stays
// declarative. Imports from the billing services package would cause an
// import cycle (billing → iface → core/tenant via aggregator), so we
// match on the error message instead — the sentinels are documented on
// iface.TenantBillingCustomerProvider for that reason.
func mapPromoteCustomerError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	switch msg {
	case "tenant not found":
		return huma.Error404NotFound(msg)
	case "tenant is not an external (Tier-2) tenant":
		return huma.Error422UnprocessableEntity(msg)
	case "tenant has no name to derive customer denomination from":
		return huma.Error422UnprocessableEntity(msg)
	case "tenant provider unavailable":
		return huma.Error503ServiceUnavailable(msg)
	case "tenantUUID is required":
		return huma.Error400BadRequest(msg)
	default:
		return huma.Error500InternalServerError("failed to promote tenant", err)
	}
}
