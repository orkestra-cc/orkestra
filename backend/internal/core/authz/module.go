// Package authz is the authorization core module. It owns the permission
// catalog, roles (system + custom per tenant), role bindings with optional
// expiration, and the evaluator that the middleware calls on every request.
// It implements iface.AuthzProvider.
package authz

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/orkestra/backend/internal/core/authz/handlers"
	authzModels "github.com/orkestra/backend/internal/core/authz/models"
	"github.com/orkestra/backend/internal/core/authz/repository"
	"github.com/orkestra/backend/internal/core/authz/services"
	tenantServices "github.com/orkestra/backend/internal/core/tenant/services"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/module"
)

// validDevRoles is the set of system roles that synthetic dev-token users
// may carry. Must match the dev module's ValidRoles. Declared here to avoid
// an import dependency on the dev addon.
var validDevRoles = map[string]struct{}{
	"super_admin": {}, "administrator": {}, "developer": {},
	"manager": {}, "operator": {}, "guest": {},
}

type Module struct {
	module.BaseModule
	handler *handlers.Handler
	svc     *services.Service
}

func NewModule() *Module { return &Module{} }

func (m *Module) Name() string        { return "authz" }
func (m *Module) DisplayName() string { return "Authorization" }
func (m *Module) Description() string { return "Permissions catalog, roles, role bindings" }

func (m *Module) Dependencies() []string { return []string{"user", "tenant"} }

func (m *Module) RequiredServices() []module.ServiceKey {
	return []module.ServiceKey{module.ServiceUserService}
}

func (m *Module) ProvidedServices() []module.ServiceKey {
	return []module.ServiceKey{module.ServiceAuthzProvider, module.ServiceAuthzService}
}

func (m *Module) NavItems() []module.NavItemSpec {
	return []module.NavItemSpec{
		{Realm: "platform", Tier: "internal", Name: "Role Management", Icon: "shield-alt", Path: "/admin/roles", MinRole: "administrator", Active: true},
	}
}

func (m *Module) Collections() []module.CollectionSpec {
	return []module.CollectionSpec{
		{Name: repository.CollPermissions, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"key": 1}, Unique: true},
			{Keys: map[string]int{"module": 1}},
		}},
		{Name: repository.CollRoles, Indexes: []module.IndexSpec{
			{OrderedKeys: []module.IndexKey{
				{Field: "tenantId", Direction: 1},
				{Field: "name", Direction: 1},
			}, Unique: true},
			{Keys: map[string]int{"uuid": 1}, Unique: true},
		}},
		{Name: repository.CollBindings, Indexes: []module.IndexSpec{
			{OrderedKeys: []module.IndexKey{
				{Field: "userUUID", Direction: 1},
				{Field: "tenantId", Direction: 1},
			}},
			{Keys: map[string]int{"roleId": 1}},
			{Keys: map[string]int{"expiresAt": 1}},
			// First uniqueness constraint on this collection. NOT sparse,
			// NO partial filter: tenantId=="" rows are global/system-role
			// grants and are deliberately covered too — a user must not
			// hold the same system role twice globally any more than
			// they should hold the same tenant role twice in one tenant.
			// ensureCollections is create-only and non-fatal on failure,
			// so an installation with pre-existing duplicate bindings
			// needs the dedup migration run first — see
			// docs/migrations/0009_authz_bindings_unique.md and the
			// "Idempotent binding grants" section of this module's
			// CLAUDE.md.
			{OrderedKeys: []module.IndexKey{
				{Field: "tenantId", Direction: 1},
				{Field: "userUUID", Direction: 1},
				{Field: "roleId", Direction: 1},
			}, Unique: true},
		}},
		// ADR-0003 PR-B: tier-split role catalogs — same index shape
		// as authz_roles so the seed/lookup paths can be drop-in
		// replacements once the cutover lands.
		{Name: repository.CollOperatorRoles, Indexes: []module.IndexSpec{
			{OrderedKeys: []module.IndexKey{
				{Field: "tenantId", Direction: 1},
				{Field: "name", Direction: 1},
			}, Unique: true},
			{Keys: map[string]int{"uuid": 1}, Unique: true},
		}},
		{Name: repository.CollClientRoles, Indexes: []module.IndexSpec{
			{OrderedKeys: []module.IndexKey{
				{Field: "tenantId", Direction: 1},
				{Field: "name", Direction: 1},
			}, Unique: true},
			{Keys: map[string]int{"uuid": 1}, Unique: true},
		}},
	}
}

func (m *Module) Permissions() []iface.PermissionSpec {
	return []iface.PermissionSpec{
		{Key: "authz.role.read", Module: "authz", Description: "List roles"},
		{Key: "authz.role.create", Module: "authz", Description: "Create custom roles"},
		{Key: "authz.role.update", Module: "authz", Description: "Update custom roles and toggle any role's active state"},
		{Key: "authz.role.delete", Module: "authz", Description: "Delete custom roles"},
		{Key: "authz.binding.read", Module: "authz", Description: "List role bindings"},
		{Key: "authz.binding.create", Module: "authz", Description: "Grant roles to users"},
		{Key: "authz.binding.delete", Module: "authz", Description: "Revoke role bindings"},

		// Platform-level system permissions granted by the user's system role.
		{Key: "system.modules.admin", Module: "system", Description: "Manage module catalog and runtime state", System: true},
		{Key: "system.users.admin", Module: "system", Description: "Administer all user accounts", System: true},
	}
}

func (m *Module) Init(deps *module.Dependencies) error {
	repo := repository.New(deps.DB)

	// Register the authz PII producer with the DSR registry (created in
	// main.go before InitAll) so the compliance DSR pipeline exports / erases
	// a data subject's role bindings across tenants.
	if reg, ok := module.GetTyped[*iface.PIIProducerRegistry](deps.Services, module.ServicePIIProducerRegistry); ok {
		reg.Register(services.NewPIIProducer(repo))
	}

	// Optional TenantProvider handle: used by the Cedar shadow-mode
	// evaluator to stamp Principal.Capabilities so capability_grants.cedar
	// has something to reason about when a caller opts into capability
	// enforcement, and to stamp Resource.TenantStatus so tenant_scope.cedar's
	// inactive-tenant forbid rule sees the real lifecycle state.
	// The lookups are nil-safe — when the tenant module is not yet
	// registered (boot ordering) the Cedar principal simply has an empty
	// capability set and the resource falls back to status="active".
	tenantProvider, _ := module.GetTyped[iface.TenantProvider](deps.Services, module.ServiceTenantProvider)
	accessProvider, _ := module.GetTyped[iface.AccessProvider](deps.Services, module.ServiceAccessProvider)
	var lookupCaps services.TenantCapabilityLookup
	var lookupTenantStatus services.TenantStatusLookup
	if accessProvider != nil {
		lookupCaps = func(ctx context.Context, tenantUUID string) ([]string, error) {
			return accessProvider.ListCapabilityIDs(ctx, tenantUUID)
		}
	}
	if tenantProvider != nil {
		lookupTenantStatus = func(ctx context.Context, tenantUUID string) (string, error) {
			t, err := tenantProvider.GetTenant(ctx, tenantUUID)
			if err != nil {
				return "", err
			}
			return t.Status, nil
		}
	}

	// The evaluator needs to know each user's system role to honor the
	// developer/administrator shortcuts. We get it from UserProvider.
	userSvc := module.MustGetTyped[iface.UserProvider](deps.Services, module.ServiceUserService)
	lookup := func(ctx context.Context, userUUID string) (string, error) {
		u, err := userSvc.GetUserByID(ctx, userUUID)
		if err == nil {
			return u.Role, nil
		}
		// Dev-token fallback: synthetic users have no DB record.
		// Three guards: non-production + dev- UUID prefix + valid role in JWT.
		if !deps.Platform.IsProduction() && strings.HasPrefix(userUUID, "dev-") {
			if role, ok := ctxauth.GetSystemRole(ctx); ok {
				if _, valid := validDevRoles[role]; valid {
					return role, nil
				}
			}
		}
		return "", err
	}

	// Post-binding grace hook. Binding creates that land a privileged role
	// eagerly start the target user's MFA enrollment clock so the 7-day
	// window begins at promotion rather than at their next login. We pass
	// a thin closure so the authz service stays free of a direct user
	// module import.
	startMFAGrace := func(ctx context.Context, userUUID string) error {
		return userSvc.StartMFAGraceIfUnset(ctx, userUUID)
	}

	m.svc = services.New(services.Config{
		Repo:               repo,
		Redis:              deps.RedisAdapter,
		Logger:             deps.Logger,
		LookupUser:         lookup,
		LookupCaps:         lookupCaps,
		LookupTenantStatus: lookupTenantStatus,
		StartMFAGrace:      startMFAGrace,
		// Production flag restricts the developer system role to read-only
		// semantics (D9): dev/staging developers debug freely; prod
		// developers cannot mutate data or read secrets even if their
		// token is valid. Mirrors the role-seed output in SeedSystemRoles.
		Production: deps.Platform.IsProduction(),
		// Environment is fed to the Cedar shadow-mode engine so
		// platform.cedar can branch on prod vs non-prod developer rules.
		Environment: deps.Platform.GetEnvironment(),
		// EnforceActions opts a per-permission allowlist out of shadow
		// mode and into Cedar-authoritative mode (Section B item #1 of
		// the auth roadmap, 2026-04-24). Comma-separated env var so
		// operators can roll back without a deploy. Empty = pure shadow
		// mode (today's default). Recommended starter list:
		// "system.modules.admin,system.tenants.admin,system.users.admin,system.users.mfa_reset"
		// — those four are already explicitly named in tenant_scope.cedar.
		EnforceActions: parseCedarEnforceActions(os.Getenv("CEDAR_ENFORCE_ACTIONS")),
	})
	m.handler = handlers.New(m.svc)

	deps.Services.Register(module.ServiceAuthzProvider, iface.AuthzProvider(m.svc))
	// Also register the concrete service so main.go can wire post-InitAll
	// setters that the interface deliberately doesn't expose (e.g.
	// SetSessionRiskLookup which depends on the auth module's
	// auth_sessions repo — auth inits after authz).
	deps.Services.Register(module.ServiceAuthzService, m.svc)

	// Wire the OwnerRoleBinder so the tenant module can grant the
	// org_owner authz binding inside CreateTenant. Both modules are
	// initialized by the time post-Init wiring runs (registry calls
	// Init in topological order: tenant before authz). The binder
	// resolves the role by name from the global catalog and creates a
	// tenant-scoped binding pointing at the new tenant. "system" is
	// stamped as the granter to flag the binding as a platform-issued
	// auto-grant rather than a user action.
	if tenantConcrete, ok := module.GetTyped[*tenantServices.Service](deps.Services, module.ServiceTenantService); ok && tenantConcrete != nil {
		tenantConcrete.SetOwnerRoleBinder(func(ctx context.Context, ownerUUID, tenantUUID, roleName string) error {
			role, err := m.svc.GetRoleByName(ctx, "", roleName)
			if err != nil {
				return fmt.Errorf("authz: resolve owner role %q: %w", roleName, err)
			}
			// EnsureBinding (not CreateBinding): all three call sites of
			// this hook — CreateTenant, SetMemberRoles, AttachMember —
			// must be safe to replay after a lost response, a crashed
			// executor, or an expired lease (tier-1 setup-saga work).
			// EnsureBinding grants the (tenant, owner, role) tuple only
			// if it does not already exist and otherwise returns the
			// existing row untouched, backed by the unique compound
			// index on authz_bindings — see this module's CLAUDE.md.
			if _, err := m.svc.EnsureBinding(ctx, tenantUUID, "system", authzModels.CreateBindingInput{
				UserUUID: ownerUUID,
				RoleUUID: role.UUID,
			}); err != nil {
				return fmt.Errorf("authz: bind owner role %q on tenant %s: %w", roleName, tenantUUID, err)
			}
			return nil
		})
		// Cascade hook: when a tenant is deleted or purged, drop every
		// binding scoped to it. Without this, deleting a tenant leaves
		// org_owner / org_admin / custom-role rows pointing at a
		// nonexistent tenant — the next DB scan would surface them as
		// orphans and the evaluator would still consult them when
		// resolving permissions for a re-used tenant UUID.
		svc := m.svc
		tenantConcrete.RegisterPostDeleteHook(func(ctx context.Context, c tenantServices.TenantPostDeleteContext) error {
			if _, err := svc.RemoveBindingsByTenant(ctx, c.TenantUUID); err != nil {
				return fmt.Errorf("authz: remove tenant bindings: %w", err)
			}
			return nil
		})
		// Member-unbind hook: when a single member is removed from a tenant
		// or has their tenant role changed, drop that member's tenant-scoped
		// binding(s). Without it, attach→remove→re-attach (or a role change)
		// accumulates bindings and GetEffectivePermissions unions them — a
		// removed or downgraded member keeps the old role's permissions.
		// Distinct from the tenant-delete cascade above, which clears every
		// binding for a whole tenant.
		tenantConcrete.SetMemberUnbinder(func(ctx context.Context, userUUID, tenantUUID string) error {
			if _, err := svc.RemoveBindingsByUserAndTenant(ctx, userUUID, tenantUUID); err != nil {
				return fmt.Errorf("authz: unbind member %s on tenant %s: %w", userUUID, tenantUUID, err)
			}
			return nil
		})
	}
	return nil
}

func (m *Module) RegisterRoutes(ri *module.RouteInfo) {
	// Catalog is global (not per-org).
	ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(ri.Operator.AuthMW.RequireGlobal())
		api := humachi.New(r, ri.APIConfig)
		m.handler.RegisterGlobalRoutes(api)
	})

	// Scoped admin routes split by mutation vs read: reads need the base
	// permission, mutations additionally require a second factor (Block B)
	// so a stolen pwd-only token cannot silently grant a role or bump
	// permissions. The caller must step up via /v1/auth/mfa/verify first.
	ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(ri.Operator.AuthMW.RequirePermission("authz.role.read"))
		api := humachi.New(r, ri.APIConfig)
		m.handler.RegisterScopedReadRoutes(api)
	})
	// Per-org role/binding mutations are split per permission so the declared
	// fine-grained permissions are actually enforced — previously every mutation
	// only required authz.role.read (held by org_member/org_viewer). Each group
	// keeps the MFA step-up and the risk gate (Section C item #2: MFA alone can
	// be satisfied by a stolen stepped-up token; the risk gate catches a session
	// showing up in the scorer via new device / rapid IP change — fails open when
	// the lookup isn't wired). assertTenantScope + the service-side orgId check in
	// each handler close the cross-tenant tampering IDOR.
	// Literal permission strings per group (not a loop over a variable) so the
	// policycoverage CI gate can statically verify each route→permission wiring.
	riskThreshold := parseRiskStepUpThreshold(os.Getenv("AUTH_RISK_STEP_UP_THRESHOLD"))
	ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(ri.Operator.AuthMW.RequirePermission("authz.role.create"))
		r.Use(ri.Operator.AuthMW.RequireMFA())
		r.Use(ri.Operator.AuthMW.RequireLowRisk(riskThreshold))
		m.handler.RegisterScopedRoleCreateRoutes(humachi.New(r, ri.APIConfig))
	})
	ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(ri.Operator.AuthMW.RequirePermission("authz.role.update"))
		r.Use(ri.Operator.AuthMW.RequireMFA())
		r.Use(ri.Operator.AuthMW.RequireLowRisk(riskThreshold))
		m.handler.RegisterScopedRoleUpdateRoutes(humachi.New(r, ri.APIConfig))
	})
	ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(ri.Operator.AuthMW.RequirePermission("authz.role.delete"))
		r.Use(ri.Operator.AuthMW.RequireMFA())
		r.Use(ri.Operator.AuthMW.RequireLowRisk(riskThreshold))
		m.handler.RegisterScopedRoleDeleteRoutes(humachi.New(r, ri.APIConfig))
	})
	ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(ri.Operator.AuthMW.RequirePermission("authz.binding.create"))
		r.Use(ri.Operator.AuthMW.RequireMFA())
		r.Use(ri.Operator.AuthMW.RequireLowRisk(riskThreshold))
		m.handler.RegisterScopedBindingCreateRoutes(humachi.New(r, ri.APIConfig))
	})
	ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(ri.Operator.AuthMW.RequirePermission("authz.binding.delete"))
		r.Use(ri.Operator.AuthMW.RequireMFA())
		r.Use(ri.Operator.AuthMW.RequireLowRisk(riskThreshold))
		m.handler.RegisterScopedBindingDeleteRoutes(humachi.New(r, ri.APIConfig))
	})
}

// Service returns the authz service — exposed so the registry can trigger
// RegisterPermissions and SeedSystemRoles after all modules init.
func (m *Module) Service() *services.Service { return m.svc }

// parseRiskStepUpThreshold parses AUTH_RISK_STEP_UP_THRESHOLD as a float in
// [0, 1] and falls back to 0.5 on unset / malformed values. Mirrors the
// helper in cmd/server/main.go — duplicated rather than shared because
// the authz module can't import cmd/server. Keep the two in lock-step.
func parseRiskStepUpThreshold(raw string) float64 {
	if raw == "" {
		return 0.5
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil || v < 0 || v > 1 {
		return 0.5
	}
	return v
}

// parseCedarEnforceActions splits the comma-separated CEDAR_ENFORCE_ACTIONS
// env var into a list of permission keys, trimming whitespace and dropping
// empty fragments. Kept package-private — the env-var → []string conversion
// is wiring boilerplate the service shouldn't know about.
func parseCedarEnforceActions(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := parts[:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
