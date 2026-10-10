// Package llm is the core module that manages LLM credentials, models and
// grants per internal organization and exposes iface.LLMGateway to the
// addons (ADR-0022). PR 1: catalog + access + envelope encryption; the
// provider adapters, routing and budget arrive in PR 2, Sign in with
// ChatGPT in PR 3.
package llm

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/orkestra/backend/internal/core/llm/handlers"
	"github.com/orkestra/backend/internal/core/llm/models"
	"github.com/orkestra/backend/internal/core/llm/providers"
	"github.com/orkestra/backend/internal/core/llm/repository"
	"github.com/orkestra/backend/internal/core/llm/services"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/module"
)

// secretKeyEnv holds the dedicated 32-byte hex key for API-key envelopes
// when compliance has not injected a KMS provider.
const secretKeyEnv = "LLM_SECRET_ENCRYPTION_KEY"

// stepUpMaxAge is the MFA freshness required on credential and grant writes.
const stepUpMaxAge = 5 * time.Minute

type Module struct {
	module.BaseModule
	logger   *slog.Logger
	vault    *services.Vault
	catalog  *services.CatalogService
	gateway  *services.Gateway
	registry *providers.Registry
	admin    *handlers.AdminHandler
	self     *handlers.SelfHandler
}

func NewModule() *Module { return &Module{} }

func (m *Module) Name() string        { return "llm" }
func (m *Module) DisplayName() string { return "LLM" }
func (m *Module) Description() string {
	return "Models, credentials and grants for language-model access; gateway for addons (ADR-0022)"
}
func (m *Module) Category() module.ModuleCategory { return module.CategoryCore }
func (m *Module) Dependencies() []string          { return []string{"user", "tenant", "notification"} }
func (m *Module) HotReloadConfig() bool           { return true }
func (m *Module) ProvidedServices() []module.ServiceKey {
	return []module.ServiceKey{module.ServiceLLMGateway}
}
func (m *Module) RequiredServices() []module.ServiceKey {
	return []module.ServiceKey{module.ServiceTenantDirectoryReader}
}

// Collections declares the three org-scoped collections. The unique
// (tenantId, name) indexes back the repository's ErrDuplicateName; the
// unique (tenantId, modelUuid, userUuid) index makes a racing grant insert
// a tolerated duplicate in Grants.Replace.
func (m *Module) Collections() []module.CollectionSpec {
	key := func(fields ...string) []module.IndexKey {
		out := make([]module.IndexKey, 0, len(fields))
		for _, f := range fields {
			out = append(out, module.IndexKey{Field: f, Direction: 1})
		}
		return out
	}
	return []module.CollectionSpec{
		{Name: repository.CollCredentials, Indexes: []module.IndexSpec{
			{OrderedKeys: key("tenantId", "name"), Unique: true},
			{OrderedKeys: key("tenantId", "uuid"), Unique: true},
			{OrderedKeys: key("tenantId", "provider")},
		}},
		{Name: repository.CollModels, Indexes: []module.IndexSpec{
			{OrderedKeys: key("tenantId", "name"), Unique: true},
			{OrderedKeys: key("tenantId", "uuid"), Unique: true},
			{OrderedKeys: key("tenantId", "status", "purposes.purpose")},
			{OrderedKeys: key("tenantId", "credentialRef.credentialUuid")},
		}},
		{Name: repository.CollGrants, Indexes: []module.IndexSpec{
			{OrderedKeys: key("tenantId", "modelUuid", "userUuid"), Unique: true},
			{OrderedKeys: key("tenantId", "userUuid")},
		}},
	}
}

func (m *Module) ConfigGroups() []module.ConfigGroup {
	return []module.ConfigGroup{
		{Key: "providers", Label: "Providers", Order: 1, Description: "Which kinds of provider this installation may call."},
		{Key: "limits", Label: "Limits & retention", Order: 2, Description: "Per-call deadline, usage retention and budget defaults."},
	}
}

func (m *Module) ConfigSchema() []module.ConfigField {
	one, maxReserve, maxDays, zero, hundred := 1, models.MaxOutputTokens, 3650, 0, 100
	return []module.ConfigField{
		{Key: "allow_hosted", Label: "Allow hosted providers", Group: "providers", Type: module.FieldBool, Default: "false", EnvVar: "LLM_ALLOW_HOSTED",
			Description: "Explicit privacy opt-in: prompts and outputs leave this installation (OpenAI, Anthropic, Gemini, OpenAI-compatible). Without it only ollama and mock are selectable. Document the data flow (RoPA, processor, transfer basis) before enabling."},
		{Key: "request_timeout", Label: "Request timeout", Group: "limits", Type: module.FieldDuration, Default: "60s", EnvVar: "LLM_REQUEST_TIMEOUT",
			Description: "Deadline per provider call. A consumer may only lower it."},
		{Key: "usage_retention_days", Label: "Usage retention (days)", Group: "limits", Type: module.FieldInt, Default: "90", EnvVar: "LLM_USAGE_RETENTION_DAYS", Min: &one, Max: &maxDays,
			Description: "Per-call usage rows older than this are purged daily. Monthly rollups are kept 24 months."},
		{Key: "budget_warn_pct", Label: "Budget warning threshold (%)", Group: "limits", Type: module.FieldInt, Default: "80", EnvVar: "LLM_BUDGET_WARN_PCT", Min: &zero, Max: &hundred,
			Description: "Default warning threshold for new budgets."},
		{Key: "budget_reserve_output_tokens", Label: "Reserved output tokens", Group: "limits", Type: module.FieldInt, Default: "4096", EnvVar: "LLM_BUDGET_RESERVE_OUTPUT_TOKENS", Min: &one, Max: &maxReserve,
			Description: "Estimated output reserved against the budget when a model declares no value of its own."},
	}
}

// Permissions declares only what PR 1 routes use; usage, budget and linked
// account permissions arrive with their routes in PR 2 and PR 3.
func (m *Module) Permissions() []iface.PermissionSpec {
	return []iface.PermissionSpec{
		{Key: "llm.admin.read", Module: "llm", Description: "Read LLM credentials (redacted), models and grants", System: true},
		{Key: "llm.credentials.admin", Module: "llm", Description: "Create, rotate, re-point and delete LLM credentials", System: true},
		{Key: "llm.models.admin", Module: "llm", Description: "Configure, update and delete LLM models", System: true},
		{Key: "llm.grants.admin", Module: "llm", Description: "Decide which users may use each LLM model", System: true},
		{Key: "llm.models.self", Module: "llm", Description: "List the LLM models you may use"},
	}
}

func (m *Module) NavItems() []module.NavItemSpec {
	return []module.NavItemSpec{
		{Realm: "platform", Tier: "internal", Name: "LLM", Icon: "robot", Path: "/admin/llm", MinRole: "administrator", Active: true},
	}
}

func (m *Module) Init(deps *module.Dependencies) error {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	m.logger = logger

	// A missing or malformed key never stops the boot: the module works,
	// only secret writes fail (llm.secret_key_missing) until a key or a KMS
	// provider exists. env-validate is the hard gate for production. The
	// value itself is never logged.
	vault, err := services.NewVault(os.Getenv(secretKeyEnv), logger)
	if err != nil {
		logger.Warn("llm: " + secretKeyEnv + " is not 64 hex characters and is ignored; API keys cannot be stored unless it is fixed or a KMS provider is injected")
		if vault, err = services.NewVault("", logger); err != nil {
			return err
		}
	} else if !vault.Available() {
		logger.Warn("llm: " + secretKeyEnv + " is not set; API keys cannot be stored unless a KMS provider is injected")
	}
	m.vault = vault

	dir, ok := module.GetTyped[iface.TenantDirectoryReader](deps.Services, module.ServiceTenantDirectoryReader)
	if !ok {
		return errors.New("llm: tenant directory reader not registered; llm depends on tenant")
	}

	creds := repository.NewCredentials(deps.DB)
	mods := repository.NewModels(deps.DB)
	grants := repository.NewGrants(deps.DB)

	productionLike := deps.Platform != nil && deps.Platform.IsProductionLike()
	cfg := func() services.CatalogConfig {
		return services.CatalogConfig{
			AllowHosted:    deps.GetConfigBool("llm", "allow_hosted", false),
			ProductionLike: productionLike,
		}
	}
	m.catalog = services.NewCatalogService(creds, mods, grants, vault, dir, cfg, logger)
	m.registry = providers.NewRegistry()
	m.gateway = services.NewGateway(services.NewAccessResolver(mods, grants, creds, cfg), m.catalog, m.registry, vault, logger)

	// Late wiring: compliance inits after us and pushes its KMS provider and
	// audit sink through the setters the gateway exposes
	// (iface.KMSProviderSetter, iface.AuditSinkSetter) by resolving
	// ServiceLLMGateway.
	deps.Services.Register(module.ServiceLLMGateway, m.gateway)

	if reg, ok := module.GetTyped[*iface.PIIProducerRegistry](deps.Services, module.ServicePIIProducerRegistry); ok {
		reg.Register(services.NewPIIProducer(creds, mods, grants, logger))
	}

	m.admin = handlers.NewAdminHandler(m.catalog, logger)
	m.self = handlers.NewSelfHandler(m.gateway, logger)
	return nil
}

// RegisterRoutes mounts every route on the operator surface (Tier-1 only)
// behind RequireInternalTenant and a permission: reads llm.admin.read,
// writes the matching .admin System permission, the self list the org
// permission llm.models.self. Every credential write (the API key or where
// it is sent) and every grant change also needs a fresh MFA proof.
func (m *Module) RegisterRoutes(ri *module.RouteInfo) {
	op := ri.Operator
	group := func(register func(r chi.Router), gates ...func(http.Handler) http.Handler) {
		op.ProtectedRouter.Group(func(r chi.Router) {
			r.Use(op.AuthMW.RequireInternalTenant())
			for _, g := range gates {
				r.Use(g)
			}
			register(r)
		})
	}
	api := func(r chi.Router) huma.API { return humachi.New(r, ri.APIConfig) }

	group(func(r chi.Router) { RegisterAdminReadRoutes(api(r), m.admin) },
		op.AuthMW.RequireSystemPermission("llm.admin.read"))
	group(func(r chi.Router) { RegisterCredentialWriteRoutes(api(r), m.admin) },
		op.AuthMW.RequireSystemPermission("llm.credentials.admin"), op.AuthMW.RequireStepUp(stepUpMaxAge))
	group(func(r chi.Router) { RegisterModelWriteRoutes(api(r), m.admin) },
		op.AuthMW.RequireSystemPermission("llm.models.admin"))
	group(func(r chi.Router) { RegisterGrantRoutes(api(r), m.admin) },
		op.AuthMW.RequireSystemPermission("llm.grants.admin"), op.AuthMW.RequireStepUp(stepUpMaxAge))
	group(func(r chi.Router) { RegisterSelfRoutes(api(r), m.self) },
		op.AuthMW.RequirePermission("llm.models.self"))
}

// HealthCheck reports healthy without a secret key or KMS: that state only
// disables secret writes (warned once at Init), the rest of the module works.
func (m *Module) HealthCheck(_ context.Context) error { return nil }
