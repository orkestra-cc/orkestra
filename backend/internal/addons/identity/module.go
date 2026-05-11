// Package identity is the Phase-3 BYO-IdP module.
//
// Two responsibilities:
//
//  1. BYO OpenID Connect login — per-tenant IdP configuration plus the
//     `/v1/identity/oidc/*` login surface. Tenants configure their own
//     IdP (Okta, Entra, Auth0, …) and users sign in against it; Orkestra
//     mints its own session after verifying the ID token.
//  2. SCIM 2.0 stubs — `/scim/v2/*` endpoints so IdPs can point their
//     provisioning clients at Orkestra without erroring. v1 ships full
//     metadata endpoints plus stubbed CRUD; persistence is a follow-up.
//
// The module is toggleable — operators can disable it entirely via the
// admin UI, which gates every route behind a 503. Per-tenant control
// for OIDC is via `IdPConfig.Enabled`; SCIM is gated by the presence of
// a live bearer token (rotated by an authenticated admin).
package identity

import (
	"log/slog"

	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"

	"github.com/orkestra/backend/internal/addons/identity/handlers"
	"github.com/orkestra/backend/internal/addons/identity/models"
	"github.com/orkestra/backend/internal/addons/identity/repository"
	"github.com/orkestra/backend/internal/addons/identity/scim"
	"github.com/orkestra/backend/internal/addons/identity/services"
	authServices "github.com/orkestra/backend/internal/core/auth/services"
	"github.com/orkestra/backend/internal/shared/config"
	"github.com/orkestra/backend/internal/shared/iface"
	"github.com/orkestra/backend/internal/shared/module"
)

// Module wires the identity module's handlers + service.
type Module struct {
	module.BaseModule
	admin     *handlers.AdminHandler
	public    *handlers.PublicHandler
	scimAdmin *handlers.ScimAdminHandler
	scim      *handlers.ScimHandler

	scimTokens *repository.ScimTokenRepository
	tenant     iface.TenantProvider
	logger     *slog.Logger
}

// NewModule constructs an empty module; Init wires collaborators.
func NewModule() *Module { return &Module{} }

func (m *Module) Name() string                    { return "identity" }
func (m *Module) DisplayName() string             { return "Identity (BYO IdP + SCIM)" }
func (m *Module) Description() string             { return "Per-tenant BYO OpenID Connect login and SCIM 2.0 provisioning stubs. Tenants configure their own IdP; users sign in against it and (future) are provisioned via SCIM." }
func (m *Module) Category() module.ModuleCategory { return module.CategoryToggleable }
func (m *Module) Enabled(_ *config.Config) bool   { return true }

// Dependencies: auth owns the token-issuance service we delegate to at
// the end of callback; tenant owns slug/UUID resolution. Both are core
// so this is defensive.
func (m *Module) Dependencies() []string { return []string{"auth", "tenant"} }

func (m *Module) RequiredServices() []module.ServiceKey {
	return []module.ServiceKey{
		module.ServicePasswordAuthService,
		module.ServiceTenantProvider,
		module.ServiceUserService,
	}
}

// NavItems surfaces the tenant-scoped Identity admin page in the sidebar.
// Gated by minRole=administrator at the nav layer and by tenant.update at
// the route layer (see RegisterRoutes below). Tenant-scoped, not platform-
// admin — backend endpoints operate on the caller's current tenant
// context (X-Tenant-ID), so operators switch tenants via the tenant
// switcher before configuring IdP / SCIM here.
func (m *Module) NavItems() []module.NavItemSpec {
	return []module.NavItemSpec{
		{Realm: "platform", Tier: "internal", Name: "Identity (IdP + SCIM)", Icon: "id-card", Path: "/identity", MinRole: "administrator", Active: true},
	}
}

func (m *Module) ProvidedServices() []module.ServiceKey {
	return []module.ServiceKey{
		module.ServiceIdentityOIDCService,
		module.ServiceIdentityAdminHandler,
		module.ServiceIdentityScimAdminHandler,
	}
}

// Collections declares the collections the module owns:
//   - identity_idp_configs (composite unique on (tenantId, protocol))
//   - identity_scim_tokens (one active row per tenant — enforced at the
//     service layer via the rotate-revokes-previous pattern)
func (m *Module) Collections() []module.CollectionSpec {
	return []module.CollectionSpec{
		{Name: models.IdPConfigsCollection, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"uuid": 1}, Unique: true},
			{OrderedKeys: []module.IndexKey{
				{Field: "tenantId", Direction: 1},
				{Field: "protocol", Direction: 1},
			}, Unique: true},
		}},
		{Name: models.ScimTokensCollection, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"uuid": 1}, Unique: true},
			// tokenHash is the login lookup key, indexed non-unique so
			// revoked rows for the same tenant can coexist for audit.
			{Keys: map[string]int{"tokenHash": 1}},
			{Keys: map[string]int{"tenantId": 1}},
		}},
	}
}

// Init wires the repositories, OIDC service, and handlers.
func (m *Module) Init(deps *module.Dependencies) error {
	passwordAuth := module.MustGetTyped[*authServices.PasswordAuthService](deps.Services, module.ServicePasswordAuthService)
	tenantProvider := module.MustGetTyped[iface.TenantProvider](deps.Services, module.ServiceTenantProvider)
	userProvider := module.MustGetTyped[iface.UserProvider](deps.Services, module.ServiceUserService)

	repo := repository.New(deps.DB)
	scimTokens := repository.NewScimTokenRepository(deps.DB)
	svc := services.New(services.Config{
		Repo:         repo,
		Users:        userProvider,
		Tenant:       tenantProvider,
		PasswordAuth: passwordAuth,
		State:        deps.RedisAdapter,
		Logger:       deps.Logger,
	})

	m.admin = handlers.NewAdminHandler(repo)
	m.public = handlers.NewPublicHandler(svc)
	m.scimAdmin = handlers.NewScimAdminHandler(scimTokens)
	m.scim = handlers.NewScimHandler()
	m.scimTokens = scimTokens
	m.tenant = tenantProvider
	m.logger = deps.Logger

	// Publish the concrete audit-emitter components so the compliance
	// module can drive their SetAuditSink setters. Keeping one key per
	// component makes the wiring explicit and each receiver independently
	// swappable. Compliance tolerates missing keys when identity is
	// disabled — the audit sink simply doesn't stitch into anything.
	deps.Services.Register(module.ServiceIdentityOIDCService, svc)
	deps.Services.Register(module.ServiceIdentityAdminHandler, m.admin)
	deps.Services.Register(module.ServiceIdentityScimAdminHandler, m.scimAdmin)

	deps.Logger.Info("Identity module initialized")
	return nil
}

// RegisterRoutes mounts three distinct route groups:
//
//   - Public OIDC flow (`/v1/identity/oidc/*`) on the public API.
//   - Admin CRUD (`/v1/identity/idp` + `/v1/identity/scim/*`) under the
//     protected router, gated by `tenant.update`.
//   - SCIM protocol surface (`/scim/v2/*`) on the root router behind the
//     identity module's own Bearer middleware (which resolves the tenant
//     directly from the token, without the JWT auth middleware).
func (m *Module) RegisterRoutes(ri *module.RouteInfo) {
	if m.public != nil {
		handlers.RegisterPublicRoutes(ri.Operator.PublicAPI, m.public)
	}
	if m.admin != nil {
		ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
			r.Use(ri.Operator.AuthMW.RequirePermission("tenant.update"))
			api := humachi.New(r, ri.APIConfig)
			handlers.RegisterAdminRoutes(api, m.admin)
			handlers.RegisterScimAdminRoutes(api, m.scimAdmin)
		})
	}
	if m.scim != nil && m.scimTokens != nil {
		ri.Router.Route("/scim/v2", func(r chi.Router) {
			r.Use(scim.BearerMiddleware(m.scimTokens, m.tenant))
			m.scim.Mount(r)
		})
	}
}
