// Package compliance is the Phase-4.1 audit-log foundation. It owns the
// append-only audit_events collection, registers iface.AuditSink so every
// other module can emit events without importing this package, and serves
// the platform-admin read surface at /v1/admin/audit-events.
//
// Later Phase-4 commits build GDPR DSR pipelines and SOC2 evidence
// automation on top of the same collection. Keep the write path lean —
// consumers call Emit from hot paths.
package compliance

import (
	"context"
	"log/slog"
	"time"

	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"

	subscriptionServices "github.com/orkestra-cc/orkestra-addon-subscriptions/services"
	"github.com/orkestra-cc/orkestra-sdk/iface"
	"github.com/orkestra-cc/orkestra-sdk/module"
	"github.com/orkestra/backend/internal/addons/compliance/handlers"
	"github.com/orkestra/backend/internal/addons/compliance/models"
	"github.com/orkestra/backend/internal/addons/compliance/repository"
	"github.com/orkestra/backend/internal/addons/compliance/services"
	identityHandlers "github.com/orkestra/backend/internal/addons/identity/handlers"
	identityServices "github.com/orkestra/backend/internal/addons/identity/services"
	authServices "github.com/orkestra/backend/internal/core/auth/services"
	tenantServices "github.com/orkestra/backend/internal/core/tenant/services"
)

// Module wires the audit sink, DSR pipeline, SOC2 evidence, and
// admin/me handlers.
type Module struct {
	module.BaseModule
	sink       *services.AuditSink
	admin      *handlers.AdminHandler
	me         *handlers.MeHandler
	soc2       *handlers.SOC2Handler
	logger     *slog.Logger
	authPolicy *authServices.AuthPolicyService // optional — Phase 8 self-service deletion gate
}

// NewModule returns an unwired module; Init constructs the sink.
func NewModule() *Module { return &Module{} }

func (m *Module) Name() string        { return "compliance" }
func (m *Module) DisplayName() string { return "Compliance (Audit + DSR)" }
func (m *Module) Description() string {
	return "Platform compliance plane: append-only audit log consumed by every module, plus (later phases) GDPR DSR pipelines and SOC2 evidence automation."
}
func (m *Module) Category() module.ModuleCategory { return module.CategoryToggleable }

// Enabled defaults to true so compliance evidence is collected on every
// boot unless an operator explicitly opts out — SOC2 auditors expect
// uninterrupted audit trail coverage.
func (m *Module) Enabled() bool { return true }

// Dependencies: auth + tenant are core (guaranteed loaded), identity and
// subscriptions are addons compliance pushes sinks into — listing them
// forces the topological sort to run their Init before this module's so
// the registry already holds their concrete services when we resolve them.
// If an addon is absent at boot, the GetTyped lookup returns ok=false and
// the wiring is silently skipped.
func (m *Module) Dependencies() []string {
	return []string{"auth", "tenant", "identity", "subscriptions"}
}

// ProvidedServices publishes the sink under a stable key so every consumer
// can resolve it with module.GetTyped.
func (m *Module) ProvidedServices() []module.ServiceKey {
	return []module.ServiceKey{module.ServiceAuditSink}
}

// NavItems surfaces the admin-only compliance pages in the sidebar. Both
// entries are gated on administrator — the underlying APIs additionally
// require the system.compliance.audit.read permission, which super_admin /
// administrator / developer inherit via the system-role seed. The sub-items
// live under the same "System Administration" group that already hosts
// Tenant Management + Module Management so platform operators find them
// alongside the other admin surfaces.
func (m *Module) NavItems() []module.NavItemSpec {
	return []module.NavItemSpec{
		{Realm: "platform", Tier: "internal", Name: "Audit Events", Icon: "clipboard-list", Path: "/admin/audit-events", MinRole: "administrator", Active: true},
		{Realm: "platform", Tier: "internal", Name: "SOC2 Evidence", Icon: "shield-alt", Path: "/admin/compliance/soc2", MinRole: "administrator", Active: true},
	}
}

// Permissions contributes the system-level read gate used by the admin
// handler. Marked System:true so super_admin / administrator / developer
// inherit it automatically from authz role seeding.
func (m *Module) Permissions() []iface.PermissionSpec {
	return []iface.PermissionSpec{
		{
			Key:         "system.compliance.audit.read",
			Module:      "compliance",
			Description: "Read the platform audit event trail",
			System:      true,
		},
	}
}

// Collections declares the audit_events collection. Indexes are tuned for
// the admin list (tenant+timestamp desc, actor+timestamp desc, action
// prefix scans) plus a TTL on timestamp that enforces the default
// retention window (2 years).
func (m *Module) Collections() []module.CollectionSpec {
	return []module.CollectionSpec{
		{Name: models.AuditEventsCollection, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"uuid": 1}, Unique: true},
			{OrderedKeys: []module.IndexKey{
				{Field: "tenantId", Direction: 1},
				{Field: "timestamp", Direction: -1},
			}},
			{OrderedKeys: []module.IndexKey{
				{Field: "actorUserId", Direction: 1},
				{Field: "timestamp", Direction: -1},
			}},
			{OrderedKeys: []module.IndexKey{
				{Field: "action", Direction: 1},
				{Field: "timestamp", Direction: -1},
			}},
			{Keys: map[string]int{"resourceType": 1, "resourceId": 1}},
			// Retention: 2 years. SOC2 auditors typically require 1 year;
			// GDPR lets us keep audit logs as long as there is a legitimate
			// interest. Two years covers both without forcing per-tenant
			// retention config in this phase.
			{Keys: map[string]int{"timestamp": 1}, TTL: 2 * 365 * 24 * time.Hour},
		}},
		{Name: models.KMSKeysCollection, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"uuid": 1}, Unique: true},
			{Keys: map[string]int{"tenantUuid": 1}, Unique: true},
		}},
	}
}

// Init constructs the repository, the sink, the DSR service, the KMS
// provider, and the handlers, then registers everything in the service
// registry and pushes setters into consumer services that accept
// post-init wiring.
func (m *Module) Init(deps *module.Dependencies) error {
	repo := repository.New(deps.DB)
	m.sink = services.NewSink(repo, deps.Logger)
	m.admin = handlers.New(repo)
	m.logger = deps.Logger

	sink := iface.AuditSink(m.sink)
	deps.Services.Register(module.ServiceAuditSink, sink)

	// KMS provider — per-tenant envelope encryption + crypto-shred on
	// purge (Phase 4.3). Boots lazily: if the master key env is
	// missing the provider is absent and the tenant service runs
	// without crypto-shred (dev deployments that opt out). A future
	// AWS KMS provider swaps in here.
	kmsRepo := repository.NewKMSKeyRepo(deps.DB)
	if kms, err := services.NewLocalKMS(kmsRepo); err == nil {
		deps.Services.Register(module.ServiceKMSProvider, iface.KMSProvider(kms))
		if ts, ok := module.GetTyped[*tenantServices.Service](deps.Services, module.ServiceTenantService); ok {
			ts.SetKMSProvider(kms)
		}
	} else {
		deps.Logger.Warn("compliance: KMS provider disabled — ORKESTRA_KMS_MASTER_KEY missing or invalid",
			slog.String("error", err.Error()),
		)
	}

	// DSR pipeline — optional by design. If the main.go bootstrap never
	// created a producer registry (unusual) the handler is simply not
	// mounted and POST /v1/me/dsr/* returns 404.
	if reg, ok := module.GetTyped[*iface.PIIProducerRegistry](deps.Services, module.ServicePIIProducerRegistry); ok {
		dsrSvc := services.NewDSRService(reg, sink, deps.Logger)
		m.me = handlers.NewMeHandler(dsrSvc)
	}

	// SOC2 evidence service — reads from users, mfa factors, audit
	// events, kms keys. No cross-module contract; the service queries
	// collections directly since it's owned by compliance and the
	// shape is read-only aggregation.
	m.soc2 = handlers.NewSOC2Handler(services.NewSOC2EvidenceService(deps.DB))

	// Push the sink into known consumer services. Each receiver is optional
	// — missing services (module disabled, out of init order) are ignored so
	// compliance boots cleanly regardless of which optional modules are
	// active.
	// ADR-0003 PR-D D-8: wire the audit sink into both tier
	// PasswordAuthService instances so login/register/reset events from
	// either audience land on the same compliance audit trail. The
	// canonical ServicePasswordAuthService is operator-tier; the
	// ServiceClientPasswordAuthService key is the client-tier instance.
	if pa, ok := module.GetTyped[*authServices.PasswordAuthService](deps.Services, module.ServicePasswordAuthService); ok {
		pa.SetAuditSink(sink)
	}
	if pa, ok := module.GetTyped[*authServices.PasswordAuthService](deps.Services, module.ServiceClientPasswordAuthService); ok {
		pa.SetAuditSink(sink)
	}
	// Phase 8: pull the auth policy so the client-side DSR erase route
	// can gate on selfServiceAccountDeletionClient. Optional — when the
	// policy isn't published the client erase route stays unmounted (no
	// silent open path).
	if p, ok := module.GetTyped[*authServices.AuthPolicyService](deps.Services, module.ServiceAuthPolicy); ok {
		m.authPolicy = p
	}
	if ts, ok := module.GetTyped[*tenantServices.Service](deps.Services, module.ServiceTenantService); ok {
		ts.SetAuditSink(sink)
	}
	if os, ok := module.GetTyped[*identityServices.Service](deps.Services, module.ServiceIdentityOIDCService); ok {
		os.SetAuditSink(sink)
	}
	if ah, ok := module.GetTyped[*identityHandlers.AdminHandler](deps.Services, module.ServiceIdentityAdminHandler); ok {
		ah.SetAuditSink(sink)
	}
	if sh, ok := module.GetTyped[*identityHandlers.ScimAdminHandler](deps.Services, module.ServiceIdentityScimAdminHandler); ok {
		sh.SetAuditSink(sink)
	}
	if ss, ok := module.GetTyped[*subscriptionServices.SubscriptionService](deps.Services, module.ServiceSubscriptionService); ok {
		ss.SetAuditSink(sink)
	}

	deps.Logger.Info("Compliance module initialized — audit sink ready")
	return nil
}

// RegisterRoutes mounts three groups: audit + SOC2 admin reads behind
// the system-level permission, and DSR self-service endpoints behind
// RequireGlobal (any authenticated user can export / erase their own
// data).
func (m *Module) RegisterRoutes(ri *module.RouteInfo) {
	if m.admin != nil || m.soc2 != nil {
		ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
			r.Use(ri.Operator.AuthMW.RequireSystemPermission("system.compliance.audit.read"))
			api := humachi.New(r, ri.APIConfig)
			if m.admin != nil {
				handlers.Register(api, m.admin)
			}
			if m.soc2 != nil {
				handlers.RegisterSOC2Routes(api, m.soc2)
			}
		})
	}
	if m.me != nil {
		ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
			r.Use(ri.Operator.AuthMW.RequireGlobal())
			api := humachi.New(r, ri.APIConfig)
			handlers.RegisterMeRoutes(api, m.me)
		})
		// Phase 8: client-tier self-service. Export is unconditional
		// (read), erase is gated by the selfServiceAccountDeletionClient
		// policy toggle resolved live on every call. The route is only
		// mounted when both the client surface AND the auth policy are
		// available — a missing policy means we stay closed by default.
		if ri.Client != nil && m.authPolicy != nil {
			gate := handlers.SelfDeletionGate(func(ctx context.Context) bool {
				return m.authPolicy.SelfServiceAccountDeletionClient(ctx)
			})
			ri.Client.ProtectedRouter.Group(func(r chi.Router) {
				r.Use(ri.Client.AuthMW.RequireGlobal())
				api := humachi.New(r, ri.APIConfig)
				handlers.RegisterClientMeRoutes(api, m.me, gate)
			})
		}
	}
}

// Sink exposes the concrete sink for modules that inject it via a setter
// (onboarding, auth, tenant …) — avoids a second registry lookup when the
// compliance module and the consumer live in the same binary.
func (m *Module) Sink() *services.AuditSink { return m.sink }
