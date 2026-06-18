// Package compliance is the platform audit-log + DSR foundation. It owns the
// append-only audit_events collection, registers iface.AuditSink so every
// other module can emit events without importing this package, drives the
// GDPR DSR pipeline over the iface.PIIProducerRegistry, mints per-tenant KMS
// keys for crypto-shred, and serves the platform-admin + self-service
// surfaces.
//
// ADR-0009: re-homed from the (ADR-0006-removed) optional addon to a CORE
// module — the personal data DSR acts on is core-owned (user/auth/tenant),
// so the compliance plane ships always-on. Keep the audit write path lean —
// consumers call Emit from hot paths.
package compliance

import (
	"context"
	"log/slog"
	"time"

	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"

	authServices "github.com/orkestra/backend/internal/core/auth/services"
	"github.com/orkestra/backend/internal/core/compliance/handlers"
	"github.com/orkestra/backend/internal/core/compliance/models"
	"github.com/orkestra/backend/internal/core/compliance/repository"
	"github.com/orkestra/backend/internal/core/compliance/services"
	tenantServices "github.com/orkestra/backend/internal/core/tenant/services"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/module"
)

// Module wires the audit sink, DSR pipeline, SOC2 evidence, and
// admin/me handlers.
type Module struct {
	module.BaseModule
	sink         *services.AuditSink
	admin        *handlers.AdminHandler
	me           *handlers.MeHandler
	soc2         *handlers.SOC2Handler
	legalHold    *handlers.LegalHoldHandler
	retention    *handlers.RetentionHandler
	retentionSvc *services.RetentionService
	erasureReq   *handlers.ErasureRequestHandler
	stopCh       chan struct{}
	logger       *slog.Logger
}

// NewModule returns an unwired module; Init constructs the sink.
func NewModule() *Module { return &Module{} }

func (m *Module) Name() string        { return "compliance" }
func (m *Module) DisplayName() string { return "Compliance (Audit + DSR)" }
func (m *Module) Description() string {
	return "Platform compliance plane: append-only audit log consumed by every module, GDPR DSR pipeline (right-of-access export + right-to-erasure), per-tenant KMS crypto-shred, and SOC2 evidence."
}

// ConfigSchema gates optional sub-features. SOC2 evidence is off by default —
// most deployments don't pursue SOC2, so the evidence page + API stay dormant
// unless explicitly enabled. The audit log + GDPR DSR pipeline are always on
// (the core reason this module is core).
func (m *Module) ConfigSchema() []module.ConfigField {
	return []module.ConfigField{
		{Key: "soc2_enabled", Label: "SOC2 evidence", Description: "Expose the SOC2 evidence admin page and API. Off by default — enable only for deployments pursuing SOC2 certification.", Type: module.FieldBool, Default: "false", EnvVar: "COMPLIANCE_SOC2_ENABLED"},
		{Key: "auto_cleanup_enabled", Label: "Retention auto-cleanup", Description: "Run a daily job that hard-deletes anonymized user tombstones past the retention window. OFF by default — this is irreversible bulk deletion.", Type: module.FieldBool, Default: "false", EnvVar: "COMPLIANCE_AUTO_CLEANUP_ENABLED"},
		{Key: "retention_years", Label: "Retention years", Description: "Years an anonymized tombstone is kept before auto-cleanup may hard-delete it.", Type: module.FieldInt, Default: "5", EnvVar: "COMPLIANCE_RETENTION_YEARS"},
		{Key: "export_retention_days", Label: "Export retention (days)", Description: "Days a generated DSR export stays downloadable before it expires.", Type: module.FieldInt, Default: "30", EnvVar: "COMPLIANCE_EXPORT_RETENTION_DAYS"},
	}
}

// Category is inherited from BaseModule (CategoryCore): the compliance plane
// is always-on per ADR-0009 — no enable/disable, Init failure is fatal.

// Dependencies: user/auth/tenant are core and hold the personal data the DSR
// pipeline erases/exports; listing them forces the topological sort to run
// their Init (which registers the PII producers + concrete services) before
// this module resolves the registry and pushes the audit sink in.
func (m *Module) Dependencies() []string {
	return []string{"user", "auth", "tenant"}
}

// ProvidedServices publishes the sink under a stable key so every consumer
// can resolve it with module.GetTyped.
func (m *Module) ProvidedServices() []module.ServiceKey {
	return []module.ServiceKey{module.ServiceAuditSink}
}

// NavItems surfaces the admin-only compliance pages in the sidebar. Both
// entries are gated on administrator — the underlying APIs additionally
// require the system.compliance.audit.read permission, which super_admin /
// administrator / developer inherit via the system-role seed.
func (m *Module) NavItems() []module.NavItemSpec {
	return []module.NavItemSpec{
		{Realm: "platform", Tier: "internal", Section: "System Administration", Name: "Compliance", Icon: "shield-halved", Path: "/admin/compliance", MinRole: "administrator", Active: true},
		// SOC2 page is emitted unconditionally but gated on the
		// compliance.soc2_enabled config flag, evaluated per request by the
		// navigation filter — so toggling it at /admin/modules surfaces (or
		// hides) the link on the next nav fetch without a restart. NavItems()
		// runs before Init, so it can't read the resolved flag itself.
		{Realm: "platform", Tier: "internal", Section: "System Administration", Name: "SOC2 Evidence", Icon: "shield-alt", Path: "/admin/compliance/soc2", MinRole: "administrator", Active: true, RequiresConfig: "soc2_enabled"},
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
			Description: "Read the platform audit event trail, DSR status, and legal holds",
			System:      true,
		},
		{
			Key:         "system.compliance.legalhold.manage",
			Module:      "compliance",
			Description: "Place and release legal holds that block GDPR erasure",
			System:      true,
		},
		{
			Key:         "system.compliance.dsr.manage",
			Module:      "compliance",
			Description: "Execute or reject right-to-erasure requests on behalf of data subjects",
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
		{Name: models.LegalHoldsCollection, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"uuid": 1}, Unique: true},
			{OrderedKeys: []module.IndexKey{
				{Field: "userUuid", Direction: 1},
				{Field: "active", Direction: 1},
			}},
		}},
		{Name: models.ErasureRequestsCollection, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"uuid": 1}, Unique: true},
			{OrderedKeys: []module.IndexKey{
				{Field: "status", Direction: 1},
				{Field: "requestedAt", Direction: -1},
			}},
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
	// purge. Boots lazily: if the master key env is missing the provider
	// is absent and the tenant service runs without crypto-shred (dev
	// deployments that opt out). A future AWS KMS provider swaps in here.
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

	// Legal-hold subsystem — gates DSR erasure (and, later, retention
	// auto-cleanup) for subjects under an active litigation/investigation hold.
	legalHoldSvc := services.NewLegalHoldService(repository.NewLegalHoldRepo(deps.DB))
	m.legalHold = handlers.NewLegalHoldHandler(legalHoldSvc)

	// DSR pipeline — drives every registered PII producer. The registry is
	// pre-created in main.go before InitAll, so by the time this core module
	// inits (after user/auth/tenant) the producers are registered.
	if reg, ok := module.GetTyped[*iface.PIIProducerRegistry](deps.Services, module.ServicePIIProducerRegistry); ok {
		dsrSvc := services.NewDSRService(reg, sink, deps.Logger)
		dsrSvc.SetLegalHoldChecker(legalHoldSvc)
		m.me = handlers.NewMeHandler(dsrSvc)

		// Retention auto-cleanup — reaps anonymized user tombstones past the
		// window via the DSR pipeline (so the legal-hold gate applies). Off by
		// default; the daily ticker is launched by Start() only when enabled.
		retentionCfg := func() services.RetentionConfig {
			return services.RetentionConfig{
				Enabled: deps.GetConfigBool("compliance", "auto_cleanup_enabled", false),
				Years:   deps.GetConfigInt("compliance", "retention_years", 5),
			}
		}
		m.retentionSvc = services.NewRetentionService(deps.DB, dsrSvc, deps.Logger, retentionCfg)
		m.retention = handlers.NewRetentionHandler(m.retentionSvc)

		// Mediated right-to-erasure workflow — subjects lodge a request, an
		// operator reviews and executes (running the DSR erase) or rejects it.
		erasureReqSvc := services.NewErasureRequestService(repository.NewErasureRequestRepo(deps.DB), dsrSvc)
		m.erasureReq = handlers.NewErasureRequestHandler(erasureReqSvc)
	}

	// SOC2 evidence — gated behind compliance.soc2_enabled (default false):
	// SOC2 isn't pursued by every deployment. The handler + route are wired
	// unconditionally so the toggle works at runtime (no restart); the gate is
	// enforced per request via the soc2Enabled closure (and the nav link is
	// gated the same way via NavItemSpec.RequiresConfig). Reads from users,
	// mfa factors, audit events, kms keys (read-only aggregation).
	soc2Enabled := func(context.Context) bool {
		return deps.GetConfigBool("compliance", "soc2_enabled", false)
	}
	m.soc2 = handlers.NewSOC2Handler(services.NewSOC2EvidenceService(deps.DB), soc2Enabled)

	// Push the sink into known core consumer services. Each receiver is
	// optional — missing services (out of init order) are ignored so
	// compliance boots cleanly. (A later pass adopts iface.AuditSinkSetter
	// to drop the concrete-type coupling.)
	if pa, ok := module.GetTyped[*authServices.PasswordAuthService](deps.Services, module.ServicePasswordAuthService); ok {
		pa.SetAuditSink(sink)
	}
	if ts, ok := module.GetTyped[*tenantServices.Service](deps.Services, module.ServiceTenantService); ok {
		ts.SetAuditSink(sink)
	}

	deps.Logger.Info("Compliance module initialized — audit sink ready")
	return nil
}

// Start launches the retention auto-cleanup ticker. The loop itself re-reads
// the enable flag every run, so it's safe to start unconditionally — it
// no-ops while auto_cleanup_enabled is false.
func (m *Module) Start(ctx context.Context) error {
	if m.retentionSvc == nil {
		return nil
	}
	m.stopCh = make(chan struct{})
	go m.retentionSvc.Loop(ctx, m.stopCh)
	return nil
}

// Stop halts the retention ticker on module disable / host shutdown.
func (m *Module) Stop(_ context.Context) error {
	if m.stopCh != nil {
		close(m.stopCh)
		m.stopCh = nil
	}
	return nil
}

// RegisterRoutes mounts three groups on the operator (Tier-1) surface:
// audit + SOC2 admin reads behind the system-level permission, and DSR
// self-service endpoints behind RequireGlobal (any authenticated user can
// export / erase their own data).
func (m *Module) RegisterRoutes(ri *module.RouteInfo) {
	// Admin reads — audit events, SOC2 (when enabled), legal-hold list —
	// gated by the audit read permission.
	if m.admin != nil || m.soc2 != nil || m.legalHold != nil || m.retention != nil || m.erasureReq != nil {
		ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
			r.Use(ri.Operator.AuthMW.RequireSystemPermission("system.compliance.audit.read"))
			api := humachi.New(r, ri.APIConfig)
			if m.admin != nil {
				handlers.Register(api, m.admin)
			}
			if m.soc2 != nil {
				handlers.RegisterSOC2Routes(api, m.soc2)
			}
			if m.legalHold != nil {
				handlers.RegisterLegalHoldReadRoutes(api, m.legalHold)
			}
			if m.retention != nil {
				handlers.RegisterRetentionRoutes(api, m.retention)
			}
			if m.erasureReq != nil {
				handlers.RegisterErasureRequestAdminReadRoutes(api, m.erasureReq)
			}
		})
	}
	// Legal-hold writes change erasure eligibility — gated by the manage
	// permission + a fresh step-up.
	if m.legalHold != nil {
		ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
			r.Use(ri.Operator.AuthMW.RequireSystemPermission("system.compliance.legalhold.manage"))
			r.Use(ri.Operator.AuthMW.RequireStepUp(5 * time.Minute))
			api := humachi.New(r, ri.APIConfig)
			handlers.RegisterLegalHoldWriteRoutes(api, m.legalHold)
		})
	}
	// Erasure-request execute/reject change/destroy data — gated by the
	// dsr.manage permission + a fresh step-up.
	if m.erasureReq != nil {
		ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
			r.Use(ri.Operator.AuthMW.RequireSystemPermission("system.compliance.dsr.manage"))
			r.Use(ri.Operator.AuthMW.RequireStepUp(5 * time.Minute))
			api := humachi.New(r, ri.APIConfig)
			handlers.RegisterErasureRequestAdminWriteRoutes(api, m.erasureReq)
		})
	}
	if m.me != nil {
		ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
			r.Use(ri.Operator.AuthMW.RequireGlobal())
			api := humachi.New(r, ri.APIConfig)
			handlers.RegisterMeRoutes(api, m.me)
			if m.erasureReq != nil {
				handlers.RegisterErasureRequestClientRoutes(api, m.erasureReq)
			}
		})
	}
}

// Sink exposes the concrete sink for modules that inject it via a setter
// (onboarding, auth, tenant …) — avoids a second registry lookup when the
// compliance module and the consumer live in the same binary.
func (m *Module) Sink() *services.AuditSink { return m.sink }
