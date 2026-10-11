// Package llm is the core module that manages LLM credentials, models and
// grants per internal organization and exposes iface.LLMGateway to the
// addons (ADR-0022): catalog, access and envelope encryption, the six
// provider adapters, routing with budget reservation, fallback and the
// usage ledger, budget alerts, the operator's live checks, and Sign in with
// ChatGPT (users billing plan models to their own linked account).
package llm

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
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
	"github.com/orkestra/backend/pkg/sdk/metrics"
	"github.com/orkestra/backend/pkg/sdk/module"
)

// secretKeyEnv holds the dedicated 32-byte hex key for API-key envelopes
// when compliance has not injected a KMS provider.
const secretKeyEnv = "LLM_SECRET_ENCRYPTION_KEY"

// stepUpMaxAge is the MFA freshness required on credential, grant and budget
// writes.
const stepUpMaxAge = 5 * time.Minute

// Defaults the config accessors fall back to (and clamp into).
const (
	defaultRequestTimeout = 60 * time.Second
	defaultRetentionDays  = 90
	// minRetentionDays keeps every row of the current UTC month: the self
	// usage route sums the month from these rows, not from the rollup.
	minRetentionDays     = 31
	maxRetentionDays     = 3650
	defaultWarnPct       = 80
	defaultReserveOutput = 4096
	// reservationSlack is added to 2 × request_timeout: a call may run two
	// full attempts (a fallback) plus its finalization before the
	// reconciler may claim its reservation.
	reservationSlack = 60 * time.Second
	// streamIdleTimeout is the client default for the silence between two
	// reads of a stream (and the wait for its headers). Gateway calls
	// override it per request with max(30s, request_timeout) (the router's
	// streamIdle); it still bounds an unmarked stream.
	streamIdleTimeout = 30 * time.Second
	// rollupRetentionPeriods: monthly rollups are kept 24 months (spec).
	rollupRetentionPeriods = 24
	retentionEvery         = 24 * time.Hour
	retentionFirstRun      = time.Minute
)

type Module struct {
	module.BaseModule
	logger   *slog.Logger
	vault    *services.Vault
	catalog  *services.CatalogService
	gateway  *services.Gateway
	registry *providers.Registry
	// accounts owns the Sign in with ChatGPT registrations; the router
	// resolves user_account models through it.
	accounts *services.AccountsService
	admin    *handlers.AdminHandler
	self     *handlers.SelfHandler
	ops      *handlers.OpsHandler
	usageH   *handlers.UsageHandler
	siwc     *handlers.SIWCHandler

	// Background work (Start/Stop): the outbox worker, the reconciler and
	// the daily retention purge.
	outboxW    *services.OutboxWorker
	reconciler *services.Reconciler
	purge      func(ctx context.Context)
	// retention cadence; zero means the defaults (test seam).
	retentionFirst, retentionInterval time.Duration

	clientsMu sync.Mutex
	clients   map[string]*http.Client
	prodLike  bool

	// operatorPublicURL is OPERATOR_API_URL (cfg.Server.Operator.PublicURL),
	// injected by the catalog because module.Dependencies carries no config.
	operatorPublicURL string
	// siwcCfg reads the live Sign in with ChatGPT settings (set in Init).
	siwcCfg func() services.SIWCConfig

	lifecycleMu sync.Mutex
	cancel      context.CancelFunc
	done        chan struct{}
}

// Option configures a Module at construction.
type Option func(*Module)

// WithOperatorPublicURL sets the operator API origin (OPERATOR_API_URL),
// which Sign in with ChatGPT needs to derive its redirect URI and to judge
// whether the deployment supports the loopback flow.
func WithOperatorPublicURL(u string) Option {
	return func(m *Module) { m.operatorPublicURL = strings.TrimSpace(u) }
}

func NewModule(opts ...Option) *Module {
	m := &Module{}
	for _, o := range opts {
		o(m)
	}
	return m
}

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

// Collections declares the org-scoped collections. The unique
// (tenantId, name) indexes back the repository's ErrDuplicateName; the
// unique (tenantId, modelUuid, userUuid) index makes a racing grant insert
// a tolerated duplicate in Grants.Replace; the unique (tenantId, period)
// rollup is the document budget transactions serialize on, and the unique
// (tenantId, period, threshold) outbox index keeps one notification per
// threshold and month.
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
		{Name: repository.CollUserAccounts, Indexes: []module.IndexSpec{
			{OrderedKeys: key("tenantId", "userUuid", "clientId"), Unique: true},
			// At most one selected registration per (user, provider).
			{OrderedKeys: key("tenantId", "userUuid", "provider", "selected"), Unique: true, PartialFilter: map[string]any{"selected": true}},
			{OrderedKeys: key("tenantId", "status")},
			{OrderedKeys: key("tenantId", "uuid"), Unique: true},
			// DSR: the subject-scoped, cross-org queries of the PIIProducer
			// (export, delete) filter on userUuid alone.
			{OrderedKeys: key("userUuid"), PartialFilter: map[string]any{"userUuid": map[string]any{"$type": "string"}}},
		}},
		// The one collection without tenantId: a single document holding the
		// installation's ext_agent_host_id, addressed by _id.
		{Name: repository.CollInstallation},
		{Name: repository.CollUsage, Indexes: []module.IndexSpec{
			{OrderedKeys: key("tenantId", "uuid"), Unique: true},
			{OrderedKeys: []module.IndexKey{{Field: "tenantId", Direction: 1}, {Field: "createdAt", Direction: -1}}},
			{OrderedKeys: []module.IndexKey{{Field: "tenantId", Direction: 1}, {Field: "userUuid", Direction: 1}, {Field: "createdAt", Direction: -1}}},
			{OrderedKeys: []module.IndexKey{{Field: "tenantId", Direction: 1}, {Field: "modelUuid", Direction: 1}, {Field: "createdAt", Direction: -1}}},
			// System sweeps (the only indexes not led by tenantId; see
			// sweepIndexes in module_test.go): the reconciler's pending scan
			// and the daily retention purge, both cross-org by design.
			{OrderedKeys: key("outcome", "reservationExpiresAt"), PartialFilter: map[string]any{"outcome": "pending"}},
			{OrderedKeys: key("createdAt")},
			// DSR: the subject-scoped, cross-org usage queries of the
			// PIIProducer (export, delete, anonymize) filter on userUuid alone.
			{OrderedKeys: key("userUuid"), PartialFilter: map[string]any{"userUuid": map[string]any{"$type": "string"}}},
		}},
		{Name: repository.CollUsageMonthly, Indexes: []module.IndexSpec{
			{OrderedKeys: key("tenantId", "period"), Unique: true},
		}},
		{Name: repository.CollBudgets, Indexes: []module.IndexSpec{
			{OrderedKeys: key("tenantId"), Unique: true},
		}},
		{Name: repository.CollBudgetOutbox, Indexes: []module.IndexSpec{
			{OrderedKeys: key("tenantId", "period", "threshold"), Unique: true},
			{OrderedKeys: key("tenantId", "uuid"), Unique: true},
			// System sweep: the outbox worker's due-event lease.
			{OrderedKeys: key("status", "nextAttemptAt")},
		}},
	}
}

func (m *Module) ConfigGroups() []module.ConfigGroup {
	return []module.ConfigGroup{
		{Key: "providers", Label: "Providers", Order: 1, Description: "Which kinds of provider this installation may call."},
		{Key: "limits", Label: "Limits & retention", Order: 2, Description: "Per-call deadline, usage retention and budget defaults."},
		{Key: "siwc", Label: "Sign in with ChatGPT", Order: 3, Description: "Let users bill inference to their own ChatGPT plan. The callback is always on 127.0.0.1; hosted installations use the paste flow."},
	}
}

func (m *Module) ConfigSchema() []module.ConfigField {
	one, minDays, maxReserve, maxDays, zero, hundred := 1, minRetentionDays, models.MaxOutputTokens, 3650, 0, 100
	minPort, maxPort := 1024, 65535
	return []module.ConfigField{
		{Key: "allow_hosted", Label: "Allow hosted providers", Group: "providers", Type: module.FieldBool, Default: "false", EnvVar: "LLM_ALLOW_HOSTED",
			Description: "Explicit privacy opt-in: prompts and outputs leave this installation (OpenAI, Anthropic, Gemini, OpenAI-compatible). Without it only ollama and mock are selectable. Document the data flow (RoPA, processor, transfer basis) before enabling."},
		{Key: "request_timeout", Label: "Request timeout", Group: "limits", Type: module.FieldDuration, Default: "60s", EnvVar: "LLM_REQUEST_TIMEOUT",
			Description: "Deadline per provider call. A consumer may only lower it."},
		{Key: "usage_retention_days", Label: "Usage retention (days)", Group: "limits", Type: module.FieldInt, Default: "90", EnvVar: "LLM_USAGE_RETENTION_DAYS", Min: &minDays, Max: &maxDays,
			Description: "Per-call usage rows older than this are purged daily (at least 31, so the current month stays complete). Monthly rollups are kept 24 months."},
		{Key: "budget_warn_pct", Label: "Budget warning threshold (%)", Group: "limits", Type: module.FieldInt, Default: "80", EnvVar: "LLM_BUDGET_WARN_PCT", Min: &zero, Max: &hundred,
			Description: "Default warning threshold for new budgets."},
		{Key: "budget_reserve_output_tokens", Label: "Reserved output tokens", Group: "limits", Type: module.FieldInt, Default: "4096", EnvVar: "LLM_BUDGET_RESERVE_OUTPUT_TOKENS", Min: &one, Max: &maxReserve,
			Description: "Estimated output reserved against the budget when a model declares no value of its own."},
		{Key: services.SIWCFieldMode, Label: "Sign in with ChatGPT", Group: "siwc", Type: module.FieldEnum, Default: services.SIWCModeDisabled, EnvVar: "LLM_SIWC_MODE",
			Options:     []string{services.SIWCModeDisabled, services.SIWCModeLoopbackLocal, services.SIWCModeHostedPaste},
			Description: "disabled: off. loopback_local: browser and API on the same machine, API reached as http://127.0.0.1:<port> (OPERATOR_API_URL), automatic callback. hosted_paste: remote installation; the user pastes the callback URL the browser shows. Either mode needs Allow hosted providers. The loopback_local callback route is mounted at boot: switching to loopback_local takes effect after a restart, while hosted_paste and disabled apply at once. OpenAI documents the plan-usage flow for open-source and locally hosted apps and points remotely hosted apps to its interest form: enabling hosted_paste is an operator decision taken with that in mind (ADR-0022 D13)."},
		{Key: services.SIWCFieldPastePort, Label: "Paste-mode callback port", Group: "siwc", Type: module.FieldInt, Default: strconv.Itoa(services.SIWCDefaultPastePort), EnvVar: "LLM_SIWC_PASTE_PORT", Min: &minPort, Max: &maxPort, Advanced: true,
			DependsOn:   []module.FieldCondition{{Key: services.SIWCFieldMode, In: []string{services.SIWCModeHostedPaste}}},
			Description: "Port of the loopback redirect URI in hosted_paste. Nothing listens on it; change it only if a local process on users' machines occupies it (Codex CLI uses 1455)."},
	}
}

// Permissions declares only what the mounted routes use (the policy
// coverage baseline rejects a declared permission with no route).
func (m *Module) Permissions() []iface.PermissionSpec {
	return []iface.PermissionSpec{
		{Key: "llm.admin.read", Module: "llm", Description: "Read LLM credentials (redacted), models, grants and the budget", System: true},
		{Key: "llm.credentials.admin", Module: "llm", Description: "Create, rotate, re-point, test and delete LLM credentials", System: true},
		{Key: "llm.models.admin", Module: "llm", Description: "Configure, update, test and delete LLM models", System: true},
		{Key: "llm.grants.admin", Module: "llm", Description: "Decide which users may use each LLM model", System: true},
		{Key: "llm.usage.read", Module: "llm", Description: "Read the organization's LLM usage report", System: true},
		{Key: "llm.budgets.admin", Module: "llm", Description: "Set the organization's monthly LLM budget", System: true},
		{Key: "llm.accounts.admin", Module: "llm", Description: "Force-unlink any user's linked ChatGPT account in the organization", System: true},
		{Key: "llm.models.self", Module: "llm", Description: "List the LLM models you may use"},
		{Key: "llm.usage.self", Module: "llm", Description: "See your own LLM usage this month"},
		{Key: "llm.accounts.self", Module: "llm", Description: "Link, select and unlink your own ChatGPT accounts"},
	}
}

// NotificationTemplates declares the budget alert emails (en, it).
func (m *Module) NotificationTemplates() []module.NotificationTemplateSpec {
	return services.BudgetNotificationTemplates()
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
	m.prodLike = productionLike
	m.siwcCfg = func() services.SIWCConfig {
		return services.SIWCConfig{
			Mode:              siwcMode(deps.GetConfig("llm", services.SIWCFieldMode)),
			PastePort:         deps.GetConfigInt("llm", services.SIWCFieldPastePort, services.SIWCDefaultPastePort),
			OperatorPublicURL: m.operatorPublicURL,
			AllowHosted:       deps.GetConfigBool("llm", services.SIWCFieldAllowHost, false),
			ProductionLike:    productionLike,
		}
	}
	cfg := func() services.CatalogConfig {
		return services.CatalogConfig{
			AllowHosted:    deps.GetConfigBool("llm", "allow_hosted", false),
			ProductionLike: productionLike,
		}
	}
	m.catalog = services.NewCatalogService(creds, mods, grants, vault, dir, cfg, logger)
	m.registry = providers.NewRegistry()
	providers.RegisterAll(m.registry, m.clientFor)
	m.gateway = services.NewGateway(services.NewAccessResolver(mods, grants, creds, cfg), m.catalog, m.registry, vault, logger)

	// Ledger, rollup, budget and alerts.
	usage := repository.NewUsage(deps.DB)
	monthly := repository.NewMonthly(deps.DB)
	budgets := repository.NewBudgets(deps.DB)
	outbox := repository.NewOutbox(deps.DB)
	store := repository.NewBudgetStore(deps.DB, usage, monthly, budgets, outbox)
	settings := func() moduleSettings { return readSettings(deps) }
	budgetCfg := func() services.BudgetConfig { return settings().budget() }
	budgetSvc := services.NewBudgetService(store, budgetCfg, logger)
	// A typed-nil adapter would pass every "no Redis" check and then panic:
	// normalize it once (Task 5 carry).
	redis := liveRedis(deps.RedisAdapter)
	limiter := services.NewRateLimiter(redis, logger)

	// Sign in with ChatGPT: the linked registrations a user_account model
	// bills to. The plan catalog is read through the openai adapter (one
	// parser, ruling 2.10).
	accountsRepo := repository.NewUserAccounts(deps.DB)
	m.accounts = services.NewAccountsService(services.AccountsDeps{
		Repo: accountsRepo, Host: repository.NewInstallation(deps.DB),
		OIDC: services.NewOIDCClient(m.clientFor(models.ProviderOpenAI), logger), Plans: services.RegistryPlanCatalog{Registry: m.registry},
		State: services.NewSIWCStateStore(redis, vault), Vault: vault, Limiter: limiter, Redis: redis,
		Config: m.siwcCfg, Logger: logger,
	})
	m.gateway.SetAccounts(m.accounts)

	router := services.NewRouter(m.registry, services.NewCredentialResolver(m.catalog, m.accounts), budgetSvc,
		services.NewBreaker(3, 30*time.Second), m.clientFor,
		func() services.RouterConfig { return services.RouterConfig{RequestTimeout: settings().requestTimeout} },
		metrics.Default(), logger)
	router.AfterCall = m.afterCall
	m.gateway.SetRouter(router)

	// The notification module is a declared dependency and always loaded;
	// the sender and the operator user directory are still resolved
	// optionally, and a missing one makes each alert a logged no-op.
	sender, _ := module.GetTyped[iface.NotificationSender](deps.Services, module.ServiceNotificationSender)
	var users services.UserLookup
	if up, ok := module.GetTyped[iface.UserProvider](deps.Services, module.ServiceOperatorUserProvider); ok {
		users = up
	}
	notifier := services.NewBudgetNotifier(sender, dir, users, budgets, outbox, consoleOrigin(deps), logger)
	m.outboxW = services.NewOutboxWorker(outbox, notifier, logger)
	m.reconciler = services.NewReconciler(usage, budgetSvc, logger)
	m.purge = func(ctx context.Context) { runRetention(ctx, logger, settings().retentionDays, usage, monthly, outbox) }

	// Late wiring: compliance inits after us and pushes its KMS provider and
	// audit sink through the setters the gateway exposes
	// (iface.KMSProviderSetter, iface.AuditSinkSetter) by resolving
	// ServiceLLMGateway.
	deps.Services.Register(module.ServiceLLMGateway, m.gateway)

	if reg, ok := module.GetTyped[*iface.PIIProducerRegistry](deps.Services, module.ServicePIIProducerRegistry); ok {
		// Linked accounts: each refresh token is revoked under its own org,
		// then every registration deleted (ruling 2.15).
		reg.Register(services.NewPIIProducer(creds, mods, grants, usage, budgets, outbox, accountsRepo, m.accounts.RevokeRegistration, logger))
	}

	m.admin = handlers.NewAdminHandler(m.catalog, logger)
	m.self = handlers.NewSelfHandler(m.gateway, logger)
	m.ops = handlers.NewOpsHandler(services.NewAdminOps(m.catalog, m.registry, m.clientFor, limiter, services.NewPinnedRunner(m.gateway), logger), logger)
	m.usageH = handlers.NewUsageHandler(services.NewUsageService(usage, monthly, budgets, dir, m.catalog, budgetCfg, logger), logger)
	m.siwc = handlers.NewSIWCHandler(m.accounts, m.siwcCfg, limiter, metrics.Default(), consoleOrigin(deps), logger)
	return nil
}

// liveRedis returns nil for a nil or typed-nil client, so the "no Redis"
// paths (fail-open limiter, fail-closed SIWC state) see a plain nil.
func liveRedis(rc module.RedisClient) module.RedisClient {
	if rc == nil {
		return nil
	}
	if v := reflect.ValueOf(rc); (v.Kind() == reflect.Pointer || v.Kind() == reflect.Map || v.Kind() == reflect.Interface) && v.IsNil() {
		return nil
	}
	return rc
}

// afterCall is the router's per-attempt hook: a successful plan-billed call
// stamps the linked account's lastUsedAt (ruling 2.17). Failures change
// nothing here: a terminal refresh already flagged the account
// needs_reauth where it happened.
func (m *Module) afterCall(ctx context.Context, model models.Model, credentialUUID string, err error) {
	if err == nil && model.CredentialRef.Kind == models.CredentialKindUserAccount && credentialUUID != "" && m.accounts != nil {
		m.accounts.MarkUsed(ctx, credentialUUID)
	}
}

// clientFor returns the SSRF-safe client of a provider kind, built once
// per kind so connection pools are never shared across kinds. One client
// serves both shapes: non-stream responses are size-capped and their
// headers bounded by the transport's HeaderTimeout; requests marked
// providers.WithStreaming get the idle timeout instead. Timeout stays 0
// (it would cut healthy streams); the per-attempt deadline is the
// router's context.
func (m *Module) clientFor(kind string) *http.Client {
	m.clientsMu.Lock()
	defer m.clientsMu.Unlock()
	if c, ok := m.clients[kind]; ok {
		return c
	}
	if m.clients == nil {
		m.clients = map[string]*http.Client{}
	}
	c := providers.NewHTTPClient(providers.TransportConfig{ProductionLike: m.prodLike, Timeout: 0, IdleTimeout: streamIdleTimeout})
	m.clients[kind] = c
	return c
}

// consoleOrigin is the operator console's public origin for the links in
// budget alerts: OPERATOR_FRONTEND_URL, else the platform frontend URL.
func consoleOrigin(deps *module.Dependencies) string {
	if v := strings.TrimSpace(os.Getenv("OPERATOR_FRONTEND_URL")); v != "" {
		return v
	}
	if deps.Platform != nil {
		return deps.Platform.FrontendURL()
	}
	return ""
}

// moduleSettings is the hot-reloaded module config, read on every call and
// clamped into range: a stored value out of range (or unparsable) can never
// produce a zero deadline, a negative reservation or a reservation that
// expires before its call can end.
type moduleSettings struct {
	requestTimeout time.Duration
	retentionDays  int
	warnPct        int
	reserveOutput  int
}

func readSettings(deps *module.Dependencies) moduleSettings {
	return clampSettings(moduleSettings{
		requestTimeout: deps.GetConfigDuration("llm", "request_timeout", defaultRequestTimeout),
		retentionDays:  deps.GetConfigInt("llm", "usage_retention_days", defaultRetentionDays),
		warnPct:        deps.GetConfigInt("llm", "budget_warn_pct", defaultWarnPct),
		reserveOutput:  deps.GetConfigInt("llm", "budget_reserve_output_tokens", defaultReserveOutput),
	})
}

func clampSettings(s moduleSettings) moduleSettings {
	if s.requestTimeout <= 0 {
		s.requestTimeout = defaultRequestTimeout
	}
	s.retentionDays = min(max(s.retentionDays, minRetentionDays), maxRetentionDays)
	s.warnPct = min(max(s.warnPct, 0), 100)
	s.reserveOutput = min(max(s.reserveOutput, 1), models.MaxOutputTokens)
	return s
}

// budget derives the BudgetService config. ReservationTTL is
// 2 × request_timeout + 60s: always at least two full attempts, and always
// far above the router's finalization window, so a reservation never
// expires under a call that is still allowed to run.
func (s moduleSettings) budget() services.BudgetConfig {
	return services.BudgetConfig{
		ReserveOutputTokens: s.reserveOutput,
		ReservationTTL:      2*s.requestTimeout + reservationSlack,
		WarnPct:             s.warnPct,
	}
}

// runRetention is the daily purge: ledger rows older than
// usage_retention_days, rollups older than 24 months, and delivered or
// abandoned budget alerts older than the ledger cutoff. Each sweep is a
// system query across every org; a failure is logged and retried the next
// day.
func runRetention(ctx context.Context, logger *slog.Logger, days int, usage *repository.Usage, monthly *repository.Monthly, outbox *repository.Outbox) {
	cutoff := time.Now().UTC().AddDate(0, 0, -days)
	report := func(what string, n int64, err error) {
		if err != nil {
			logger.WarnContext(ctx, "llm: retention purge failed", slog.String("what", what), slog.String("error", err.Error()))
			return
		}
		if n > 0 {
			logger.InfoContext(ctx, "llm: retention purge", slog.String("what", what), slog.Int64("deleted", n))
		}
	}
	n, err := usage.PurgeBefore(ctx, cutoff)
	report("usage", n, err)
	n, err = monthly.PurgeOlderThan(ctx, rollupRetentionPeriods)
	report("usage_monthly", n, err)
	n, err = outbox.PurgeSettledBefore(ctx, cutoff)
	report("budget_outbox", n, err)
}

// Start launches the outbox worker, the reconciler and the daily retention
// purge. Idempotent: a second Start while running is a no-op. Each job
// handles every row under the row's own tenant (ruling 3.5/5.4).
func (m *Module) Start(ctx context.Context) error {
	if m.outboxW == nil {
		return nil // not initialized
	}
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()
	if m.cancel != nil {
		select {
		case <-m.done:
		default:
			return nil
		}
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(3)
	go func() { defer wg.Done(); m.outboxW.Run(runCtx) }()
	go func() { defer wg.Done(); m.reconciler.Run(runCtx) }()
	go func() { defer wg.Done(); m.retentionLoop(runCtx) }()
	go func() { wg.Wait(); close(done) }()
	m.cancel, m.done = cancel, done
	return nil
}

func (m *Module) retentionLoop(ctx context.Context) {
	first, every := m.retentionFirst, m.retentionInterval
	if first <= 0 {
		first = retentionFirstRun
	}
	if every <= 0 {
		every = retentionEvery
	}
	t := time.NewTimer(first)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.purge(ctx)
			t.Reset(every)
		}
	}
}

// Stop cancels the background jobs and waits for them (or for ctx).
func (m *Module) Stop(ctx context.Context) error {
	m.lifecycleMu.Lock()
	cancel, done := m.cancel, m.done
	m.lifecycleMu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		m.lifecycleMu.Lock()
		if m.done == done {
			m.cancel, m.done = nil, nil
		}
		m.lifecycleMu.Unlock()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// RegisterRoutes mounts every route on the operator surface (Tier-1 only)
// behind RequireInternalTenant and a permission: reads llm.admin.read,
// writes the matching .admin System permission, the self routes the org
// permissions llm.models.self, llm.usage.self and llm.accounts.self.
//
// Every write group also carries RequireMFA (the authz precedent). The
// .admin suffix makes Cedar demand an enrolled second factor, but with
// CEDAR_ENFORCE_ACTIONS unset the role table decides and Cedar's refusal is
// only logged, so without this gate a password-only administrator (one in
// the MFA enrollment grace period included) could change models. On the
// credential and grant groups RequireMFA sits before the step-up: step-up
// alone is satisfied by a password reconfirm for an operator with no
// enrolled factor whose role does not require one, so it does not prove a
// second factor; RequireMFA does, and the step-up then demands it be fresh
// for every credential write (the API key or where it is sent) and every
// grant change.
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

	group(func(r chi.Router) {
		a := api(r)
		RegisterAdminReadRoutes(a, m.admin)
		RegisterAccountsAdminReadRoutes(a, m.siwc)
	}, op.AuthMW.RequireSystemPermission("llm.admin.read"))
	group(func(r chi.Router) { RegisterCredentialWriteRoutes(api(r), m.admin) },
		op.AuthMW.RequireSystemPermission("llm.credentials.admin"), op.AuthMW.RequireMFA(), op.AuthMW.RequireStepUp(stepUpMaxAge))
	group(func(r chi.Router) { RegisterModelWriteRoutes(api(r), m.admin) },
		op.AuthMW.RequireSystemPermission("llm.models.admin"), op.AuthMW.RequireMFA())
	group(func(r chi.Router) { RegisterGrantRoutes(api(r), m.admin) },
		op.AuthMW.RequireSystemPermission("llm.grants.admin"), op.AuthMW.RequireMFA(), op.AuthMW.RequireStepUp(stepUpMaxAge))
	group(func(r chi.Router) { RegisterSelfRoutes(api(r), m.self) },
		op.AuthMW.RequirePermission("llm.models.self"))

	// Live checks: MFA like every write group (they send the stored key or
	// a prompt to a provider), no step-up (no configuration changes).
	group(func(r chi.Router) { RegisterCredentialOpsRoutes(api(r), m.ops) },
		op.AuthMW.RequireSystemPermission("llm.credentials.admin"), op.AuthMW.RequireMFA())
	group(func(r chi.Router) { RegisterPromptRoute(api(r), m.ops) },
		op.AuthMW.RequireSystemPermission("llm.models.admin"), op.AuthMW.RequireMFA())
	group(func(r chi.Router) { RegisterUsageReadRoutes(api(r), m.usageH) },
		op.AuthMW.RequireSystemPermission("llm.usage.read"))
	group(func(r chi.Router) { RegisterBudgetReadRoutes(api(r), m.usageH) },
		op.AuthMW.RequireSystemPermission("llm.admin.read"))
	// The budget write: MFA then a fresh step-up, like grants (relaxing
	// block to warn_only lifts the cap).
	group(func(r chi.Router) { RegisterBudgetWriteRoutes(api(r), m.usageH) },
		op.AuthMW.RequireSystemPermission("llm.budgets.admin"), op.AuthMW.RequireMFA(), op.AuthMW.RequireStepUp(stepUpMaxAge))
	group(func(r chi.Router) { RegisterSelfUsageRoutes(api(r), m.usageH) },
		op.AuthMW.RequirePermission("llm.usage.self"))

	// Sign in with ChatGPT. The user's own registrations: no MFA or step-up
	// (ruling 5.6: their own account, the same tier as me/models). The
	// forced unlink of someone else's: a System .admin write, so MFA then a
	// fresh step-up.
	group(func(r chi.Router) { RegisterAccountsSelfRoutes(api(r), m.siwc) },
		op.AuthMW.RequirePermission("llm.accounts.self"))
	group(func(r chi.Router) { RegisterAccountsAdminWriteRoutes(api(r), m.siwc) },
		op.AuthMW.RequireSystemPermission("llm.accounts.admin"), op.AuthMW.RequireMFA(), op.AuthMW.RequireStepUp(stepUpMaxAge))
	m.mountSIWCCallback(ri)
}

// mountSIWCCallback mounts the public loopback callback, only in
// loopback_local: the mode is read once here, so turning loopback_local on
// at runtime needs a restart (hosted_paste and disabled are hot; the
// handler re-reads the mode on every request and answers a closed
// redirect once it has changed). An invalid configuration still mounts
// the route, with a WARN: the handler refuses every callback until the
// settings are fixed.
func (m *Module) mountSIWCCallback(ri *module.RouteInfo) {
	if m.siwcCfg == nil || m.siwc == nil {
		return
	}
	cfg := m.siwcCfg()
	if !cfg.CallbackRouteMounted() {
		return
	}
	logger := m.logger
	if logger == nil {
		logger = slog.Default()
	}
	if ri.Router == nil {
		logger.Warn("llm siwc: loopback_local is set but no root router is available; the callback route is not mounted")
		return
	}
	if err := services.ValidateSIWCConfig(cfg); err != nil {
		logger.Warn("llm siwc: loopback_local configuration is invalid; the callback refuses every request until it is fixed", slog.String("error", err.Error()))
	}
	ri.Router.Get(services.SIWCCallbackPath, m.siwc.Callback)
}

// HealthCheck reports healthy without a secret key or KMS: that state only
// disables secret writes (warned once at Init), the rest of the module works.
func (m *Module) HealthCheck(_ context.Context) error { return nil }
