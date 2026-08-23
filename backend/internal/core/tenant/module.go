// Package tenant is the multi-tenancy core module. It owns the unified
// Tenant aggregate (two-tier: internal | external, hierarchical via
// ParentTenantUUID), per-user memberships, and plan-based entitlements.
// Implements iface.TenantProvider so the middleware and other modules can
// resolve the current tenant, check membership, and gate routes.
//
// See docs/adr/0001-unified-tenant-model.md for the two-tier design.
package tenant

import (
	"context"
	"fmt"

	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/orkestra/backend/internal/core/tenant/handlers"
	"github.com/orkestra/backend/internal/core/tenant/models"
	"github.com/orkestra/backend/internal/core/tenant/repository"
	"github.com/orkestra/backend/internal/core/tenant/services"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/module"
)

type Module struct {
	module.BaseModule
	handler *handlers.Handler
	svc     *services.Service
}

func NewModule() *Module { return &Module{} }

func (m *Module) Name() string        { return "tenant" }
func (m *Module) DisplayName() string { return "Tenants" }
func (m *Module) Description() string {
	return "Unified two-tier tenancy: internal operator tenants + external client tenants, memberships, plan entitlements"
}

func (m *Module) Dependencies() []string { return []string{"user"} }

// ConfigSchema exposes the per-tier tenant-provisioning policy as admin-managed
// config. Read at request time by the service's ProvisioningModeResolver — edits
// at /admin/modules/tenant take effect on the next creation with no restart.
//
// Both default to "manual" (admin-only creation): a fresh install starts with
// zero tenants and expects an operator to create them deliberately — the first
// internal tenant is created from the setup wizard's OrgStep or the admin UI.
// External clients are never auto-provisioned and cannot self-create a tenant —
// only a platform admin creates a client tenant and assigns it to a Tier-2 user.
func (m *Module) ConfigSchema() []module.ConfigField {
	return []module.ConfigField{
		{
			Key: "provisioning.internal.mode", Label: "Internal tenant creation", Group: "provisioning.internal",
			Description: "Who may create internal (operator-tier) tenants. manual (default): only platform administrators (system.tenants.admin). single: additionally lock the platform to one occupied Tier-1 provisioning slot. Every Tier-1 creation requires system.tenants.admin regardless of mode.",
			Type:        module.FieldEnum, Default: models.ProvisioningModeManual,
			Options: []string{models.ProvisioningModeManual, models.ProvisioningModeSingle},
			EnvVar:  "TENANT_PROVISIONING_INTERNAL_MODE",
		},
		{
			Key: "provisioning.external.mode", Label: "External tenant creation", Group: "provisioning.external",
			Description: "Who may create external (client-tier) tenants. open: self-serve clients are provisioned automatically. manual (default): only platform administrators create client tenants and assign them to a Tier-2 user — self-serve signup never auto-provisions a tenant and external users cannot create one themselves.",
			Type:        module.FieldEnum, Default: models.ProvisioningModeManual,
			Options: []string{models.ProvisioningModeOpen, models.ProvisioningModeManual},
			EnvVar:  "TENANT_PROVISIONING_EXTERNAL_MODE",
		},
	}
}

// ConfigGroups splits the two provisioning policies onto the full-page rail by
// tenant tier — the internal (operator) vs external (client) distinction that
// governs data isolation everywhere else in the platform. Two top-level groups
// (one field each) are the minimum that promotes the page to the rail layout.
func (m *Module) ConfigGroups() []module.ConfigGroup {
	return []module.ConfigGroup{
		{Key: "provisioning.internal", Label: "Internal provisioning (Tier-1)", Order: 1,
			Description: "Who may create internal, operator-tier tenants."},
		{Key: "provisioning.external", Label: "External provisioning (Tier-2)", Order: 2,
			Description: "Who may create external, client-tier tenants."},
	}
}

func (m *Module) ProvidedServices() []module.ServiceKey {
	return []module.ServiceKey{
		module.ServiceTenantProvider,
		module.ServiceAccessProvider,
		module.ServiceTenantService,
		module.ServiceBillingTenantProvider,
	}
}

func (m *Module) Collections() []module.CollectionSpec {
	return []module.CollectionSpec{
		{Name: repository.CollTenants, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"uuid": 1}, Unique: true},
			{Keys: map[string]int{"slug": 1}, Unique: true, Sparse: true},
			{Keys: map[string]int{"ownerUserUUID": 1}},
			{Keys: map[string]int{"kind": 1}},
			{Keys: map[string]int{"status": 1}},
			{Keys: map[string]int{"parentTenantUUID": 1}, Sparse: true},
		}},
		{Name: repository.CollMemberships, Indexes: []module.IndexSpec{
			{OrderedKeys: []module.IndexKey{
				{Field: "userUUID", Direction: 1},
				{Field: "tenantId", Direction: 1},
			}, Unique: true},
			{Keys: map[string]int{"tenantId": 1}},
		}},
		{Name: repository.CollInvites, Indexes: []module.IndexSpec{
			// tokenHash is the lookup key on accept; unique stops two invites
			// from colliding on the same random token. Sparse so the index
			// build succeeds across transitional docs.
			{Keys: map[string]int{"tokenHash": 1}, Unique: true, Sparse: true},
			{Keys: map[string]int{"tenantId": 1}},
			{Keys: map[string]int{"expiresAt": 1}, ExpireAt: true},
		}},
		// Closure table for the tenant hierarchy (ADR-0001). Supports external
		// multi-tenant clients (clients that are themselves multi-tenant with
		// sub-workspaces). Indexed both ways so ancestor-of-X and descendants-
		// of-X are O(1)/O(depth) respectively.
		{Name: repository.CollAncestors, Indexes: []module.IndexSpec{
			{OrderedKeys: []module.IndexKey{
				{Field: "descendantUUID", Direction: 1},
				{Field: "ancestorUUID", Direction: 1},
			}, Unique: true},
			{Keys: map[string]int{"ancestorUUID": 1}},
		}},
		// Capability entitlements projection. Tenants may hold many
		// historical rows per capability (revoked + re-granted, or
		// expired); at most one is active at a time — that invariant is
		// enforced in the service (Grant revokes any existing active row
		// before inserting). Indexes here accelerate the common reads.
		{Name: repository.CollEntitlements, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"uuid": 1}, Unique: true, Sparse: true},
			{OrderedKeys: []module.IndexKey{
				{Field: "tenantUUID", Direction: 1},
				{Field: "capabilityId", Direction: 1},
			}},
			{Keys: map[string]int{"capabilityId": 1}},
			{Keys: map[string]int{"expiresAt": 1}, Sparse: true},
		}},
	}
}

func (m *Module) Permissions() []iface.PermissionSpec {
	return []iface.PermissionSpec{
		{Key: "tenant.read", Module: "tenant", Description: "Read tenant details"},
		{Key: "tenant.update", Module: "tenant", Description: "Update tenant name, slug, settings"},
		{Key: "tenant.delete", Module: "tenant", Description: "Archive the tenant"},
		{Key: "tenant.plan.update", Module: "tenant", Description: "Change plan and features"},
		{Key: "tenant.member.read", Module: "tenant", Description: "List tenant members"},
		{Key: "tenant.member.invite", Module: "tenant", Description: "Invite new members"},
		{Key: "tenant.member.remove", Module: "tenant", Description: "Remove members from the tenant"},
		{Key: "system.tenants.admin", Module: "tenant", Description: "Administer all tenants platform-wide", System: true},
	}
}

func (m *Module) NavItems() []module.NavItemSpec {
	// Two-tier split (ADR-0001 Phase 3): operator admins see "our own
	// companies" (internal) and "customers on the platform" (external
	// clients) as two adjacent entries under the Administration (platform)
	// realm — Internal Tenants first, External Tenants directly below it.
	// Both entries require the administrator role and the system.tenants.admin
	// permission (enforced at the route layer).
	return []module.NavItemSpec{
		{Realm: "platform", Tier: "internal", Name: "Internal Tenants", Icon: "building", Path: "/admin/internal/tenants", MinRole: "administrator", Active: true},
		{Realm: "platform", Tier: "internal", Name: "External Tenants", Icon: "users", Path: "/admin/clients", MinRole: "administrator", Active: true},
	}
}

func (m *Module) Init(deps *module.Dependencies) error {
	repo := repository.New(deps.DB)
	m.svc = services.New(repo)
	m.handler = handlers.New(m.svc, deps.Services)

	// Wire the per-tier provisioning policy reader. Reads the admin-managed
	// `provisioning.{internal,external}.mode` keys from this module's config
	// (ModuleConfigService, 30s Redis cache) on every call so an edit at
	// /admin/modules/tenant takes effect without a restart. Nil ConfigService
	// (tests) leaves the resolver returning "" → the service fails closed to
	// manual for internal, and stays open for external (see ProvisioningMode).
	m.svc.SetProvisioningModeResolver(func(ctx context.Context, kind models.TenantKind) string {
		if deps.ConfigService == nil {
			return ""
		}
		key := "provisioning.internal.mode"
		if kind == models.TenantKindExternal {
			key = "provisioning.external.mode"
		}
		return deps.ConfigService.GetValue(ctx, "tenant", key)
	})

	// Register the tenant PII producer with the DSR registry (created in
	// main.go before InitAll). The compliance module's DSR pipeline runs it to
	// export / erase a data subject's tenant memberships.
	if reg, ok := module.GetTyped[*iface.PIIProducerRegistry](deps.Services, module.ServicePIIProducerRegistry); ok {
		reg.Register(services.NewPIIProducer(repo))
	}

	deps.Services.Register(module.ServiceTenantProvider, iface.TenantProvider(m.svc))
	// Polymorphic-owner capability surface lives on the same concrete
	// Service so the entitlement projection has one writer.
	deps.Services.Register(module.ServiceAccessProvider, iface.AccessProvider(m.svc))
	// Also publish the concrete service so addon modules (compliance) that
	// need to drive post-init setters can resolve it without importing the
	// tenant package from a second location.
	deps.Services.Register(module.ServiceTenantService, m.svc)
	// Billing-party resolver for the unified-clients refactor (Phase 1):
	// walks up Tenant.ParentTenantUUID until it finds a tenant with FatturaPA
	// fields and returns the snapshot the billing send path needs. Same
	// concrete Service so the resolver shares the tenant repository.
	deps.Services.Register(module.ServiceBillingTenantProvider, iface.BillingTenantProvider(m.svc))

	// Cascade hook: evict the owner's client_users row when an external
	// Tier-2 tenant is deleted and the owner has no other live
	// memberships. Without this, the per-collection unique email index
	// would block a fresh self-serve signup with the same address even
	// though the only tenant the account ever belonged to is gone.
	// Internal tenants are intentionally skipped — operator users
	// outlive single workspaces (one human can run several internal
	// admin sessions). The user module loads before tenant in the
	// topological order, so the client provider is already in the
	// registry by the time this Init runs.
	if clientUsers, ok := module.GetTyped[iface.ClientUserProvider](deps.Services, module.ServiceClientUserProvider); ok && clientUsers != nil {
		m.svc.RegisterPostDeleteHook(func(ctx context.Context, c services.TenantPostDeleteContext) error {
			if c.Kind != iface.TenantKindExternal {
				return nil
			}
			if c.OwnerUserUUID == "" || c.OwnerHasOtherTenants {
				return nil
			}
			if err := clientUsers.SoftDeleteAndAliasEmail(ctx, c.OwnerUserUUID); err != nil {
				return fmt.Errorf("tenant: evict orphaned client owner: %w", err)
			}
			return nil
		})
		// Wire the user-display resolver consumed by EnsureTenantForUser
		// (seeds the personal tenant's Name from the User's FullName) and
		// by ResolveBillingParty (renders the FatturaPA party name for
		// sole-proprietor tenants). Bound to the ClientUserProvider because
		// only Tier-2 self-registered users need this path; operator users
		// never trigger lazy provisioning.
		m.svc.SetUserDisplayResolver(func(ctx context.Context, userUUID string) (string, string, error) {
			u, err := clientUsers.GetUserByID(ctx, userUUID)
			if err != nil || u == nil {
				return "", "", err
			}
			return u.FullName, u.Email, nil
		})
	}
	return nil
}

func (m *Module) RegisterRoutes(ri *module.RouteInfo) {
	// Global routes: list/create tenants, accept invites. These need auth
	// but intentionally do not require a current-tenant context.
	ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(ri.Operator.AuthMW.RequireGlobal())
		api := humachi.New(r, ri.APIConfig)
		m.handler.RegisterGlobalRoutes(api)
	})

	// Tenant-scoped routes: need the caller to have the tenant.read permission
	// in X-Tenant-ID. tenant.* permissions are granted by the system
	// administrator role seeded by the authz module. Reads pass through
	// with just the permission; mutations additionally require an MFA
	// step-up (Block B) because they can transfer ownership data, change
	// plan entitlements, or destroy the tenant.
	ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(ri.Operator.AuthMW.RequirePermission("tenant.read"))
		api := humachi.New(r, ri.APIConfig)
		m.handler.RegisterScopedReadRoutes(api)
	})
	// Per-tenant mutations are split per permission so the declared fine-grained
	// permissions (tenant.update / .delete / .plan.update / .member.*) are
	// actually enforced — previously every mutation only required tenant.read,
	// which org_viewer/org_member hold. Every group additionally requires an MFA
	// step-up (Block B) and every handler asserts the {tenantId} path matches the
	// caller's resolved tenant (assertTenantScope), closing the cross-tenant IDOR.
	ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(ri.Operator.AuthMW.RequirePermission("tenant.update"))
		r.Use(ri.Operator.AuthMW.RequireMFA())
		api := humachi.New(r, ri.APIConfig)
		m.handler.RegisterScopedUpdateRoutes(api)
	})
	ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(ri.Operator.AuthMW.RequirePermission("tenant.plan.update"))
		r.Use(ri.Operator.AuthMW.RequireMFA())
		api := humachi.New(r, ri.APIConfig)
		m.handler.RegisterScopedPlanRoutes(api)
	})
	ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(ri.Operator.AuthMW.RequirePermission("tenant.delete"))
		r.Use(ri.Operator.AuthMW.RequireMFA())
		api := humachi.New(r, ri.APIConfig)
		m.handler.RegisterScopedDeleteRoutes(api)
	})
	ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(ri.Operator.AuthMW.RequirePermission("tenant.member.invite"))
		r.Use(ri.Operator.AuthMW.RequireMFA())
		api := humachi.New(r, ri.APIConfig)
		m.handler.RegisterScopedInviteRoutes(api)
	})
	ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(ri.Operator.AuthMW.RequirePermission("tenant.member.remove"))
		r.Use(ri.Operator.AuthMW.RequireMFA())
		api := humachi.New(r, ri.APIConfig)
		m.handler.RegisterScopedMemberRoutes(api)
	})

	// Platform-admin routes: visible to super_admin / administrator /
	// developer via the system.tenants.admin permission. These bypass
	// per-tenant membership so a platform operator can manage every tenant
	// without joining each one.
	ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(ri.Operator.AuthMW.RequireSystemPermission("system.tenants.admin"))
		api := humachi.New(r, ri.APIConfig)
		m.handler.RegisterAdminRoutes(api)
	})

	// Tier-2 self-service: /v1/me/billing-identity. Mounted on the client
	// audience surface so frontend-client tokens (aud=client) can call it
	// to manage their own tenant's billing identity. The handler resolves
	// the personal tenant via EnsureTenantForUser — Tier-2 users never
	// touch another tenant's row.
	if ri.Client != nil {
		ri.Client.ProtectedRouter.Group(func(r chi.Router) {
			r.Use(ri.Client.AuthMW.RequireGlobal())
			api := humachi.New(r, ri.APIConfig)
			m.handler.RegisterClientRoutes(api)
		})
	}
}

// Service returns the tenant service — exposed so the authz module can
// also consume it during its own initialization.
func (m *Module) Service() *services.Service { return m.svc }
