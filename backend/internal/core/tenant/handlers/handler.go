package handlers

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra-cc/orkestra-sdk/ctxauth"
	"github.com/orkestra-cc/orkestra-sdk/iface"
	"github.com/orkestra-cc/orkestra-sdk/module"
	"github.com/orkestra/backend/internal/core/tenant/models"
	"github.com/orkestra/backend/internal/core/tenant/repository"
	"github.com/orkestra/backend/internal/core/tenant/services"
)

// tenantSvc is the consumer-side view of *services.Service that the
// handler depends on. Defined here (not in services/) so the handler
// owns its narrow surface and tests can fake it without round-tripping
// through Mongo. *services.Service satisfies this via structural
// typing — the constructor still accepts the concrete pointer that
// module.go threads through.
type tenantSvc interface {
	GetTenant(ctx context.Context, tenantUUID string) (*iface.Tenant, error)
	GetTenantModel(ctx context.Context, tenantUUID string) (*models.Tenant, error)
	ListUserMemberships(ctx context.Context, userUUID string) ([]iface.TenantMembership, error)
	CreateTenant(ctx context.Context, ownerUUID string, input models.CreateTenantInput) (*models.Tenant, error)
	UpdateTenant(ctx context.Context, tenantUUID string, input models.UpdateTenantInput) error
	DeleteTenant(ctx context.Context, tenantUUID string) error
	PurgeTenant(ctx context.Context, tenantUUID string) error
	UpdatePlan(ctx context.Context, tenantUUID string, input models.UpdatePlanInput) error
	ListMembers(ctx context.Context, tenantUUID string) ([]models.TenantMembership, error)
	RemoveMember(ctx context.Context, tenantUUID, userUUID string) error
	AttachMember(ctx context.Context, tenantUUID, userUUID, roleName string, isOwner bool) (*models.TenantMembership, error)
	CreateInvite(ctx context.Context, tenantUUID, invitedBy string, input models.InviteInput) (*models.TenantInvite, error)
	ListInvites(ctx context.Context, tenantUUID string, onlyPending bool) ([]models.TenantInvite, error)
	RevokeInvite(ctx context.Context, tenantUUID, inviteUUID string) error
	AcceptInvite(ctx context.Context, userUUID, token string) (*models.Tenant, error)
	ListAllTenantsFiltered(ctx context.Context, filter repository.TenantListFilter) ([]services.TenantAdminView, error)
	ListDivisions(ctx context.Context, parentUUID string) ([]models.Tenant, error)
	CreateDivision(ctx context.Context, parentUUID, ownerUUID, name, slug string) (*models.Tenant, error)
	SetBillingIdentity(ctx context.Context, tenantUUID string, in services.SetBillingIdentityInput) error
	SetItalianBillable(ctx context.Context, tenantUUID string, on bool) error
	EnsureTenantForUser(ctx context.Context, userUUID string) (*iface.Tenant, error)
}

type Handler struct {
	svc tenantSvc
	// registry is used at request time to resolve the optional aggregator
	// providers (subscriptions, payments). Looked up lazily because those
	// addons init after core/tenant; capturing the typed interfaces at
	// boot would freeze them as nil.
	registry *module.ServiceRegistry
}

// New wires a handler to the tenant service. The registry is optional;
// when nil the aggregator endpoints degrade to empty results. The svc
// parameter is the concrete *services.Service at boot — declared as an
// interface in tests to swap in a fake without touching Mongo.
func New(svc tenantSvc, registry *module.ServiceRegistry) *Handler {
	return &Handler{svc: svc, registry: registry}
}

// --- Request/response envelopes ---

type listMyTenantsOutput struct {
	Body struct {
		Memberships []memberDTO `json:"memberships"`
	}
}

type memberDTO struct {
	TenantID string   `json:"tenantId"`
	Name     string   `json:"name"`
	Slug     string   `json:"slug"`
	Plan     string   `json:"plan"`
	Kind     string   `json:"kind"`
	Roles    []string `json:"roles"`
	IsOwner  bool     `json:"isOwner"`
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
	userUUID, ok := ctxauth.GetUserUUID(ctx)
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
	userUUID, ok := ctxauth.GetUserUUID(ctx)
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

// attachMemberAdminInput is the wire shape for the admin direct-grant flow.
// Either UserUUID or UserEmail must be supplied; UserUUID wins when both are
// provided. Role is the authz role name to grant (typically org_owner /
// org_admin / org_member); IsOwner stamps the denormalized owner flag on the
// membership row but does not change the tenant's primary owner.
type attachMemberAdminInput struct {
	TenantID string `path:"tenantId"`
	Body     struct {
		UserUUID  string `json:"userUuid,omitempty" doc:"UUID of the existing user to attach (preferred over userEmail when both are supplied)"`
		UserEmail string `json:"userEmail,omitempty" doc:"Email of the existing user; resolved against the tier-aware UserProvider"`
		Role      string `json:"role" doc:"authz role name to grant (e.g. org_owner, org_admin, org_member)"`
		IsOwner   bool   `json:"isOwner,omitempty" doc:"Stamp the denormalized owner flag on the membership row"`
	}
}

type attachMemberAdminOutput struct {
	Body struct {
		Member membershipRow `json:"member"`
	}
}

// attachMemberAdmin resolves the target user (by UUID or by email through
// the tier-appropriate UserProvider), validates the tenant exists, and
// delegates the membership write + authz binding to services.AttachMember.
// The handler maps service errors to HTTP status codes verbatim — the
// service layer is the single place that owns the validation taxonomy.
func (h *Handler) attachMemberAdmin(ctx context.Context, in *attachMemberAdminInput) (*attachMemberAdminOutput, error) {
	if in.Body.Role == "" {
		return nil, huma.Error400BadRequest("role is required")
	}
	if in.Body.UserUUID == "" && in.Body.UserEmail == "" {
		return nil, huma.Error400BadRequest("userUuid or userEmail is required")
	}

	t, err := h.svc.GetTenant(ctx, in.TenantID)
	if err != nil || t == nil {
		return nil, huma.Error404NotFound("tenant not found")
	}

	userUUID := strings.TrimSpace(in.Body.UserUUID)
	resolvedEmail := ""
	if userUUID == "" {
		// Resolve by email via the tier-matched UserProvider. Internal
		// tenants get operator users; external tenants get client users —
		// admin-attach must not silently mix audiences.
		providerKey := module.ServiceOperatorUserProvider
		if t.Kind == iface.TenantKindExternal {
			providerKey = module.ServiceClientUserProvider
		}
		provider, ok := module.GetTyped[iface.UserProvider](h.registry, providerKey)
		if !ok || provider == nil {
			return nil, huma.Error503ServiceUnavailable("user provider not configured for tenant tier")
		}
		u, err := provider.GetUserByEmail(ctx, strings.ToLower(strings.TrimSpace(in.Body.UserEmail)))
		if err != nil || u == nil {
			return nil, huma.Error404NotFound("user not found")
		}
		userUUID = u.ID
		resolvedEmail = u.Email
	}

	mbr, err := h.svc.AttachMember(ctx, in.TenantID, userUUID, in.Body.Role, in.Body.IsOwner)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrAttachInput):
			return nil, huma.Error400BadRequest(err.Error())
		case errors.Is(err, services.ErrMembershipExists):
			return nil, huma.Error409Conflict("user is already a member of this tenant")
		case errors.Is(err, repository.ErrNotFound):
			return nil, huma.Error404NotFound("tenant not found")
		default:
			return nil, huma.Error500InternalServerError("attach failed", err)
		}
	}

	out := &attachMemberAdminOutput{}
	out.Body.Member = membershipRow{TenantMembership: *mbr, Email: resolvedEmail}
	if out.Body.Member.Email == "" {
		// Email was not resolved upfront (UUID-only request) — best-effort
		// enrich for the response so the operator UI can render the row
		// without an extra round trip.
		h.enrichMemberEmails(ctx, in.TenantID, []membershipRow{out.Body.Member})
	}
	return out, nil
}

func (h *Handler) createInvite(ctx context.Context, in *inviteInput) (*inviteOutput, error) {
	userUUID, _ := ctxauth.GetUserUUID(ctx)
	inv, err := h.svc.CreateInvite(ctx, in.TenantID, userUUID, in.Body)
	if err != nil {
		return nil, huma.Error400BadRequest("invite failed: " + err.Error())
	}
	return &inviteOutput{Body: inv}, nil
}

func (h *Handler) acceptInvite(ctx context.Context, in *acceptInviteInput) (*tenantOutput, error) {
	userUUID, ok := ctxauth.GetUserUUID(ctx)
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

type adminMatchedMember struct {
	UserUUID string `json:"userUUID"`
	Email    string `json:"email"`
	FullName string `json:"fullName,omitempty"`
	Username string `json:"username,omitempty"`
}

type adminTenantListItem struct {
	models.Tenant
	MemberCount int `json:"memberCount"`
	// MatchedMembers carries the member-side hits when the request used the
	// `q` query param. Empty otherwise. Bounded by
	// repository.MaxMatchedMembersPerTenant.
	MatchedMembers []adminMatchedMember `json:"matchedMembers,omitempty"`
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
	// Q narrows results to tenants whose name/slug match (case-insensitive
	// substring) OR who have at least one member whose email / fullName /
	// username matches. Used by the search box on /admin/clients and
	// /admin/internal/tenants. Empty disables the search path.
	Q string `query:"q"`
	// IncludeDeletedUsers controls whether soft-deleted users count as
	// matches in Q's member-side join. Only meaningful when Q is set.
	IncludeDeletedUsers bool `query:"includeDeletedUsers"`
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
		OperationID: "attach-tenant-member-admin",
		Method:      http.MethodPost,
		Path:        "/v1/admin/tenants/{tenantId}/members",
		Summary:     "Attach an existing user to a tenant (platform admin direct grant)",
		Description: "Direct-grant alternative to the email invite flow. Looks up the target user by UUID or email (tier-aware), then inserts a membership row and creates the matching authz binding so the user can act in the tenant immediately. 404 when the tenant or user is missing, 409 when the user is already a member.",
		Tags:        []string{"Tenants Admin"},
	}, h.attachMemberAdmin)

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

	// --- Unified Client Aggregate (Phase 1) — billing-identity sub-document ---

	huma.Register(api, huma.Operation{
		OperationID: "set-tenant-billing-identity-admin",
		Method:      http.MethodPatch,
		Path:        "/v1/admin/clients/{tenantId}/billing-identity",
		Summary:     "Update a tenant's billing-identity sub-document (platform admin)",
		Description: "Sets IsCompany, LegalName, VAT/fiscal codes, billing address, and the FatturaPA routing sub-document on a Tier-2 tenant. Phase 1 of the Unified Client Aggregate refactor — the data this endpoint writes is what Phase 5 will resolve via BillingTenantProvider in place of the soon-to-be-deleted billing.Customer row. All body fields are optional; nil leaves the existing value.",
		Tags:        []string{"Tenants Admin"},
	}, h.setTenantBillingIdentityAdmin)

	huma.Register(api, huma.Operation{
		OperationID: "set-tenant-italian-billable-admin",
		Method:      http.MethodPost,
		Path:        "/v1/admin/clients/{tenantId}/italian-billable",
		Summary:     "Toggle a tenant's Italian-billable flag (platform admin)",
		Description: "Flips Tenant.IsItalianBillable. Enabling requires a FatturaPA profile with at least one routing handle (CodiceDestinatario or PECDestinatario) — 422 otherwise. Disabling is unconditional.",
		Tags:        []string{"Tenants Admin"},
	}, h.setTenantItalianBillableAdmin)
}

// --- Unified Client Aggregate (Phase 1) handlers ---

// setBillingIdentityInput is the wire shape for PATCH
// /v1/admin/clients/{tenantId}/billing-identity. All fields optional;
// missing fields leave the existing value untouched. FatturaPA is
// wholesale-replaced when present so the operator UI can post a complete
// sub-document without merge logic.
type setBillingIdentityInput struct {
	TenantID string `path:"tenantId"`
	Body     struct {
		IsCompany      *bool                    `json:"isCompany,omitempty" doc:"Legal-entity discriminator: false=natural person/sole-proprietor, true=corporation"`
		LegalName      *string                  `json:"legalName,omitempty"`
		VATNumber      *string                  `json:"vatNumber,omitempty"`
		FiscalCode     *string                  `json:"fiscalCode,omitempty"`
		BillingAddress *models.TenantAddress    `json:"billingAddress,omitempty"`
		FatturaPA      *models.FatturaPAProfile `json:"fatturaPA,omitempty" doc:"FatturaPA routing sub-document — required when IsItalianBillable is true"`
	}
}

type setItalianBillableInput struct {
	TenantID string `path:"tenantId"`
	Body     struct {
		Enabled bool `json:"enabled" doc:"true to enable Italian billable mode (requires FatturaPA routing); false to disable"`
	}
}

func (h *Handler) setTenantBillingIdentityAdmin(ctx context.Context, in *setBillingIdentityInput) (*tenantOutput, error) {
	svcInput := services.SetBillingIdentityInput{
		IsCompany:      in.Body.IsCompany,
		LegalName:      in.Body.LegalName,
		VATNumber:      in.Body.VATNumber,
		FiscalCode:     in.Body.FiscalCode,
		BillingAddress: in.Body.BillingAddress,
		FatturaPA:      in.Body.FatturaPA,
	}
	if err := h.svc.SetBillingIdentity(ctx, in.TenantID, svcInput); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, huma.Error404NotFound("tenant not found")
		}
		return nil, huma.Error400BadRequest("billing-identity update failed: " + err.Error())
	}
	t, err := h.svc.GetTenantModel(ctx, in.TenantID)
	if err != nil {
		return nil, huma.Error404NotFound("tenant not found")
	}
	return &tenantOutput{Body: t}, nil
}

func (h *Handler) setTenantItalianBillableAdmin(ctx context.Context, in *setItalianBillableInput) (*tenantOutput, error) {
	if err := h.svc.SetItalianBillable(ctx, in.TenantID, in.Body.Enabled); err != nil {
		switch {
		case errors.Is(err, repository.ErrNotFound):
			return nil, huma.Error404NotFound("tenant not found")
		case errors.Is(err, services.ErrItalianBillableMissingProfile):
			return nil, huma.Error422UnprocessableEntity(err.Error())
		default:
			return nil, huma.Error400BadRequest("italian-billable toggle failed: " + err.Error())
		}
	}
	t, err := h.svc.GetTenantModel(ctx, in.TenantID)
	if err != nil {
		return nil, huma.Error404NotFound("tenant not found")
	}
	return &tenantOutput{Body: t}, nil
}

func (h *Handler) listAllTenantsAdmin(ctx context.Context, in *adminTenantListInput) (*adminTenantListOutput, error) {
	filter := repository.TenantListFilter{
		IncludeDeleted:      in.IncludeDeleted,
		RootsOnly:           in.RootsOnly,
		Q:                   strings.TrimSpace(in.Q),
		IncludeDeletedUsers: in.IncludeDeletedUsers,
	}
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
		item := adminTenantListItem{Tenant: *v.Tenant, MemberCount: v.MemberCount}
		if len(v.MatchedMembers) > 0 {
			item.MatchedMembers = make([]adminMatchedMember, len(v.MatchedMembers))
			for i, m := range v.MatchedMembers {
				item.MatchedMembers[i] = adminMatchedMember{
					UserUUID: m.UserUUID,
					Email:    m.Email,
					FullName: m.FullName,
					Username: m.Username,
				}
			}
		}
		out.Body.Tenants = append(out.Body.Tenants, item)
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
	userUUID, ok := ctxauth.GetUserUUID(ctx)
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

// --- Unified Client Aggregate (Phase 6) — Tier-2 self-service ---

// BillingIdentityDTO is the focused projection returned by the self-service
// /v1/me/billing-identity endpoints. Trimmed from the full Tenant model so
// the client surface doesn't leak operator-only fields (KMSKeyID, Plan,
// IdPConfigUUID, etc.) and stays stable independently of the tenant aggregate.
type BillingIdentityDTO struct {
	TenantID          string                   `json:"tenantId"`
	IsCompany         bool                     `json:"isCompany"`
	IsItalianBillable bool                     `json:"isItalianBillable"`
	LegalName         string                   `json:"legalName,omitempty"`
	VATNumber         string                   `json:"vatNumber,omitempty"`
	FiscalCode        string                   `json:"fiscalCode,omitempty"`
	BillingAddress    models.TenantAddress     `json:"billingAddress,omitempty"`
	FatturaPA         *models.FatturaPAProfile `json:"fatturaPA,omitempty"`
}

type billingIdentityOutput struct {
	Body BillingIdentityDTO
}

type setMyBillingIdentityInput struct {
	Body struct {
		IsCompany      *bool                    `json:"isCompany,omitempty" doc:"Legal-entity discriminator: false=natural person/sole-proprietor, true=corporation"`
		LegalName      *string                  `json:"legalName,omitempty"`
		VATNumber      *string                  `json:"vatNumber,omitempty"`
		FiscalCode     *string                  `json:"fiscalCode,omitempty"`
		BillingAddress *models.TenantAddress    `json:"billingAddress,omitempty"`
		FatturaPA      *models.FatturaPAProfile `json:"fatturaPA,omitempty" doc:"FatturaPA routing sub-document — required when IsItalianBillable is true"`
	}
}

type setMyItalianBillableInput struct {
	Body struct {
		Enabled bool `json:"enabled"`
	}
}

// RegisterClientRoutes mounts the Tier-2 self-service billing-identity surface
// on the client audience. Each handler resolves the caller's personal tenant
// via EnsureTenantForUser (lazy provisioning), then delegates to the same
// service methods the admin endpoints call. Tier-2 users never see another
// tenant's data — the personal tenant is keyed by the authenticated userUUID.
func (h *Handler) RegisterClientRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "get-my-billing-identity",
		Method:      http.MethodGet,
		Path:        "/v1/me/billing-identity",
		Summary:     "Read the caller's billing identity",
		Description: "Returns the billing-identity sub-document of the caller's personal tenant. Lazy-provisions the personal tenant on first call.",
		Tags:        []string{"Tenants"},
	}, h.getMyBillingIdentity)

	huma.Register(api, huma.Operation{
		OperationID: "set-my-billing-identity",
		Method:      http.MethodPatch,
		Path:        "/v1/me/billing-identity",
		Summary:     "Update the caller's billing identity",
		Description: "Patches IsCompany, LegalName, VAT/fiscal codes, billing address, and the FatturaPA routing sub-document on the caller's personal tenant. All fields optional; nil leaves the existing value. FatturaPA is wholesale-replaced when present.",
		Tags:        []string{"Tenants"},
	}, h.setMyBillingIdentity)

	huma.Register(api, huma.Operation{
		OperationID: "set-my-italian-billable",
		Method:      http.MethodPost,
		Path:        "/v1/me/italian-billable",
		Summary:     "Toggle the caller's Italian-billable flag",
		Description: "Flips Tenant.IsItalianBillable on the caller's personal tenant. Enabling requires a FatturaPA profile with CodiceDestinatario or PECDestinatario (422 otherwise); disabling is unconditional.",
		Tags:        []string{"Tenants"},
	}, h.setMyItalianBillable)
}

func (h *Handler) resolveCallerTenant(ctx context.Context) (*models.Tenant, error) {
	userUUID, ok := ctxauth.GetUserUUID(ctx)
	if !ok {
		return nil, huma.Error401Unauthorized("not authenticated")
	}
	personal, err := h.svc.EnsureTenantForUser(ctx, userUUID)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to resolve personal tenant", err)
	}
	t, err := h.svc.GetTenantModel(ctx, personal.UUID)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to load personal tenant", err)
	}
	return t, nil
}

func tenantToBillingIdentityDTO(t *models.Tenant) BillingIdentityDTO {
	return BillingIdentityDTO{
		TenantID:          t.UUID,
		IsCompany:         t.IsCompany,
		IsItalianBillable: t.IsItalianBillable,
		LegalName:         t.LegalName,
		VATNumber:         t.VATNumber,
		FiscalCode:        t.FiscalCode,
		BillingAddress:    t.BillingAddress,
		FatturaPA:         t.FatturaPA,
	}
}

func (h *Handler) getMyBillingIdentity(ctx context.Context, _ *struct{}) (*billingIdentityOutput, error) {
	t, err := h.resolveCallerTenant(ctx)
	if err != nil {
		return nil, err
	}
	return &billingIdentityOutput{Body: tenantToBillingIdentityDTO(t)}, nil
}

func (h *Handler) setMyBillingIdentity(ctx context.Context, in *setMyBillingIdentityInput) (*billingIdentityOutput, error) {
	t, err := h.resolveCallerTenant(ctx)
	if err != nil {
		return nil, err
	}
	svcInput := services.SetBillingIdentityInput{
		IsCompany:      in.Body.IsCompany,
		LegalName:      in.Body.LegalName,
		VATNumber:      in.Body.VATNumber,
		FiscalCode:     in.Body.FiscalCode,
		BillingAddress: in.Body.BillingAddress,
		FatturaPA:      in.Body.FatturaPA,
	}
	if err := h.svc.SetBillingIdentity(ctx, t.UUID, svcInput); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, huma.Error404NotFound("tenant not found")
		}
		return nil, huma.Error400BadRequest("billing-identity update failed: " + err.Error())
	}
	updated, err := h.svc.GetTenantModel(ctx, t.UUID)
	if err != nil {
		return nil, huma.Error404NotFound("tenant not found")
	}
	return &billingIdentityOutput{Body: tenantToBillingIdentityDTO(updated)}, nil
}

func (h *Handler) setMyItalianBillable(ctx context.Context, in *setMyItalianBillableInput) (*billingIdentityOutput, error) {
	t, err := h.resolveCallerTenant(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.svc.SetItalianBillable(ctx, t.UUID, in.Body.Enabled); err != nil {
		switch {
		case errors.Is(err, repository.ErrNotFound):
			return nil, huma.Error404NotFound("tenant not found")
		case errors.Is(err, services.ErrItalianBillableMissingProfile):
			return nil, huma.Error422UnprocessableEntity(err.Error())
		default:
			return nil, huma.Error400BadRequest("italian-billable toggle failed: " + err.Error())
		}
	}
	updated, err := h.svc.GetTenantModel(ctx, t.UUID)
	if err != nil {
		return nil, huma.Error404NotFound("tenant not found")
	}
	return &billingIdentityOutput{Body: tenantToBillingIdentityDTO(updated)}, nil
}
