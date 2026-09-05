package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
	_ "time/tzdata" // embed the IANA zone DB — the alpine base image ships none, and any module calling time.LoadLocation needs it

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"

	"github.com/orkestra/backend/internal/core/auth/services"
	authzServices "github.com/orkestra/backend/internal/core/authz/services"
	tenantServices "github.com/orkestra/backend/internal/core/tenant/services"
	"github.com/orkestra/backend/internal/shared/blob"
	"github.com/orkestra/backend/internal/shared/config"
	"github.com/orkestra/backend/internal/shared/container"
	"github.com/orkestra/backend/internal/shared/database"
	"github.com/orkestra/backend/internal/shared/devtoken"
	"github.com/orkestra/backend/internal/shared/errors"
	authMiddleware "github.com/orkestra/backend/internal/shared/middleware"
	"github.com/orkestra/backend/internal/shared/setup"
	"github.com/orkestra/backend/internal/shared/systeminit"
	"github.com/orkestra/backend/internal/shared/telemetry"
	"github.com/orkestra/backend/internal/shared/utils"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/metrics"
	"github.com/orkestra/backend/pkg/sdk/module"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// Version, BuildTime, GitCommit are set at build time via
//
//	go build -ldflags "-X main.Version=$TAG -X main.BuildTime=$TS -X main.GitCommit=$SHA"
//
// The Dockerfile sets these from ARGs; the release workflow forwards the
// git tag. Default values keep `go run` working without a build wrapper.
var (
	Version   = "dev"
	BuildTime = "unknown"
	GitCommit = "unknown"
)

func main() {
	logger := utils.SetupLogger()
	slog.SetDefault(logger)
	logger.Info("orkestra-backend starting",
		slog.String("version", Version),
		slog.String("build_time", BuildTime),
		slog.String("git_commit", GitCommit),
	)

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// OpenTelemetry tracer provider. Runs no-op when
	// OTEL_EXPORTER_OTLP_ENDPOINT is unset so local dev stays
	// frictionless; the per-request tenant baggage middleware still
	// runs uniformly. Shutdown is deferred so buffered spans flush on
	// SIGTERM.
	tracerShutdown := telemetry.Init("orkestra-backend", cfg.Server.Environment, logger)
	defer func() {
		sctx, scancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer scancel()
		if err := tracerShutdown(sctx); err != nil {
			logger.Warn("telemetry shutdown", slog.String("error", err.Error()))
		}
	}()

	// ADR-0005 Phase E — optional OTLP logs fanout. Disabled by
	// default (OTEL_LOGS_ENABLED=true to enable). When active, every
	// slog.Logger built via deps.Logger or slog.Default fans out to
	// both stdout AND the OTLP backend so vendor dashboards
	// (Honeycomb, Datadog, Grafana Cloud, Axiom) see the same lines
	// docker logs captures. Stdout remains the source of truth.
	logResult := telemetry.InitLogs("orkestra-backend", cfg.Server.Environment, logger)
	defer func() {
		sctx, scancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer scancel()
		if err := logResult.Shutdown(sctx); err != nil {
			logger.Warn("telemetry logs shutdown", slog.String("error", err.Error()))
		}
	}()
	if logResult.Handler != nil {
		logger = utils.SetupLogger(logResult.Handler)
		slog.SetDefault(logger)
	}

	// Connect infrastructure. 2-minute budget accommodates the
	// retry-with-backoff loops in NewMongoConnection and NewRedisConnection
	// (up to 20 attempts each) that wait out first-boot auth init races.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	db, err := database.NewMongoConnection(ctx, database.MongoConfig{
		URI:             cfg.Database.MongoURI,
		Database:        cfg.Database.DatabaseName,
		MaxPoolSize:     cfg.Database.MaxPoolSize,
		MinPoolSize:     cfg.Database.MinPoolSize,
		MaxConnIdleTime: cfg.Database.MaxConnIdleTime,
		ConnectTimeout:  cfg.Database.ConnectTimeout,
	})
	if err != nil {
		log.Fatalf("Failed to connect to MongoDB: %v", err)
	}

	redisClient, err := database.NewRedisConnection(ctx, database.RedisConfig{
		URL:             cfg.Redis.URL,
		MaxRetries:      cfg.Redis.MaxRetries,
		MinIdleConns:    cfg.Redis.MinIdleConns,
		MaxIdleConns:    cfg.Redis.MaxIdleConns,
		ConnMaxLifetime: cfg.Redis.ConnMaxLifetime,
		ReadTimeout:     cfg.Redis.ReadTimeout,
		WriteTimeout:    cfg.Redis.WriteTimeout,
	})
	if err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}

	redisAdapter := database.NewRedisClientAdapter(redisClient)

	// Module config infrastructure (DB-backed module management)
	configRepo := module.NewModuleConfigRepository(db)
	configService := module.NewModuleConfigService(configRepo, redisAdapter, logger)

	// Platform bootstrap sentinel: makes "first user becomes super_admin"
	// atomic instead of TOCTOU-racy. Constructed before the module registry
	// so the auth module's Init can pull it from the service registry.
	firstAdminClaimer, err := systeminit.NewRepo(ctx, db)
	if err != nil {
		log.Fatalf("Failed to initialize system-init repository: %v", err)
	}

	// Initialize module registry
	svcRegistry := module.NewServiceRegistry()
	svcRegistry.Register(module.ServiceFirstAdminClaimer, firstAdminClaimer)
	svcRegistry.Register(module.ServiceSetupFinalizationStore, firstAdminClaimer)
	// PII producer registry is pre-created here so producer modules can
	// register themselves during their own Init. ADR-0006: the core base
	// has no in-tree consumer (compliance/DSR left with the addons); the
	// registry is kept as the seam a fork's DSR module reads from. See
	// iface.PIIProducerRegistry.
	svcRegistry.Register(module.ServicePIIProducerRegistry, iface.NewPIIProducerRegistry())

	// Object storage (S3-compatible; defaults to RustFS in dev/staging).
	// Optional — when access key/secret are empty the backend boots
	// without ServiceBlobStore registered and the user module's avatar
	// upload endpoint degrades to 503 storage_unavailable. OAuth-source
	// and initials avatars still work.
	if cfg.Storage.AccessKey != "" && cfg.Storage.SecretKey != "" {
		provider := blob.NewProvider(blob.ProviderConfig{
			S3: blob.S3Config{
				Endpoint:       cfg.Storage.Endpoint,
				PublicEndpoint: cfg.Storage.PublicEndpoint,
				Region:         cfg.Storage.Region,
				AccessKey:      cfg.Storage.AccessKey,
				SecretKey:      cfg.Storage.SecretKey,
				ForcePathStyle: cfg.Storage.ForcePathStyle,
				EnsureBucket:   cfg.Storage.EnsureBucket,

				CORSAllowedOrigins: cfg.Storage.CORSAllowedOrigins,
			},
			BucketPrefix: cfg.Storage.BucketPrefix,
			Redis:        redisClient,
			Cache: blob.CachedConfig{
				SignedGetTTL: time.Hour,
				CacheBuffer:  10 * time.Minute,
				KeyPrefix:    "blob:url:",
			},
		})
		svcRegistry.Register(module.ServiceObjectStoreProvider, provider)

		// Back-compat: ServiceBlobStore is the "avatars" bucket, so existing
		// consumers (user avatars, auth DSR export bundles) resolve it
		// unchanged. For one transition release a CUSTOM STORAGE_BUCKET (not
		// <prefix>-avatars) is honored as-is so a deployment's existing
		// avatars aren't orphaned; the default and <prefix>-avatars-shaped
		// values go through the provider (which ensures the bucket on first use).
		var avatarStore blob.Store
		var avatarErr error
		if legacy := cfg.Storage.Bucket; legacy != "" && legacy != cfg.Storage.BucketPrefix+"-avatars" {
			logger.Warn("STORAGE_BUCKET is deprecated — honoring it as the avatars bucket for this release; migrate by renaming the bucket to <STORAGE_BUCKET_PREFIX>-avatars, or set STORAGE_BUCKET_PREFIX so <prefix>-avatars equals it",
				slog.String("legacy_bucket", legacy))
			sctx, scancel := context.WithTimeout(context.Background(), 30*time.Second)
			base, err := blob.NewS3(sctx, blob.S3Config{
				Endpoint:       cfg.Storage.Endpoint,
				PublicEndpoint: cfg.Storage.PublicEndpoint,
				Region:         cfg.Storage.Region,
				Bucket:         legacy,
				AccessKey:      cfg.Storage.AccessKey,
				SecretKey:      cfg.Storage.SecretKey,
				ForcePathStyle: cfg.Storage.ForcePathStyle,
				EnsureBucket:   cfg.Storage.EnsureBucket,

				CORSAllowedOrigins: cfg.Storage.CORSAllowedOrigins,
			})
			scancel()
			if err == nil {
				avatarStore = blob.NewCached(base, redisClient, blob.CachedConfig{
					SignedGetTTL: time.Hour, CacheBuffer: 10 * time.Minute, KeyPrefix: "blob:url:avatars:",
				})
			}
			avatarErr = err
		} else {
			avatarStore, avatarErr = provider.Bucket("avatars")
		}
		if avatarErr != nil {
			logger.Warn("blob storage unavailable — avatar uploads will return 503",
				slog.String("endpoint", cfg.Storage.Endpoint),
				slog.String("error", avatarErr.Error()))
		} else {
			svcRegistry.Register(module.ServiceBlobStore, avatarStore)
			logger.Info("blob storage ready",
				slog.String("endpoint", cfg.Storage.Endpoint),
				slog.String("bucket_prefix", cfg.Storage.BucketPrefix))
		}
	} else {
		logger.Info("blob storage not configured (STORAGE_ACCESS_KEY/SECRET empty) — avatar uploads disabled")
	}

	modRegistry := module.NewModuleRegistry(logger)
	modRegistry.SetConfigService(configService)
	modRegistry.SetContainerManager(container.NewManager(logger))
	modDeps := &module.Dependencies{
		DB:           db,
		RedisAdapter: redisAdapter,
		Platform:     cfg,
		Logger:       logger,
		Services:     svcRegistry,
	}

	// Core modules — always loaded (auth, users, navigation). The auth
	// factory captures cfg via the closure returned by coreModules so
	// auth's Init does not need Dependencies.Config (Phase 1c).
	for _, factory := range coreModules(cfg) {
		modRegistry.Register(factory())
	}

	// All optional modules are always instantiated and initialized so they
	// can be enabled/disabled at runtime without restart. Only enabled ones
	// are actually Start()ed after init.
	allOptNames := allOptionalModuleNames(logger)

	var optModules []module.Module
	for _, name := range allOptNames {
		if factory, ok := optionalModules[name]; ok {
			optModules = append(optModules, factory())
		}
	}
	if err := modRegistry.RegisterAll(optModules); err != nil {
		log.Fatalf("Failed to resolve module dependencies: %v", err)
	}

	// ADR-0005 Phase F — publish the catalog of module names before
	// InitAll so the logging core module can render a row per module
	// in its admin view. Read inside logging.Init via
	// module.ServiceLogLevelModuleNames.
	{
		all := modRegistry.AllModules()
		names := make([]string, 0, len(all))
		for _, m := range all {
			names = append(names, m.Name())
		}
		svcRegistry.Register(module.ServiceLogLevelModuleNames, names)
	}

	if err := modRegistry.InitAll(modDeps); err != nil {
		log.Fatalf("Failed to initialize modules: %v", err)
	}

	// Boot seeding has run inside InitAll. From here on the documents of
	// requiredPersistedModules must exist: GetConfig fails closed and the
	// module list shows a `missing` row instead of lazily re-seeding them.
	// This is also the boot gate for those modules: a missing document, or a
	// seeding/backfill failure SeedFromModules recorded, aborts here rather
	// than serving a strict policy reader an incomplete auth document.
	//
	// Its own short deadline, deliberately not the boot ctx above: that one
	// is the infrastructure-connect budget and may be nearly spent after a
	// slow first-boot Mongo race, and a healthy document must never be
	// refused because THIS read timed out.
	gateCtx, cancelGate := context.WithTimeout(context.Background(), 10*time.Second)
	err = configService.RequirePersistedConfig(gateCtx, requiredPersistedModules...)
	cancelGate()
	if err != nil {
		log.Fatalf("Required module config is not serviceable: %v", err)
	}

	// ADR-0005 Phase F — hot-swap the slog handler's resolver from
	// the boot env-driven static snapshot to the DB-backed live
	// resolver. Every existing module logger (clones produced by
	// deps.Logger.With during InitAll) picks up the new resolver
	// instantly via the shared resolverBox pointer; future records
	// gate on the persisted snapshot. No-op when the logging module
	// failed Init (would leave the env-driven resolver in place).
	if r, ok := module.GetTyped[utils.LevelResolver](svcRegistry, module.ServiceLogLevelResolver); ok {
		utils.SwapLevelResolver(r)
		logger.Info("logging: live level resolver active",
			slog.String("source", "logging core module"))
	}

	// Retrieve auth infrastructure for middleware setup
	jwtService := svcRegistry.MustGet(module.ServiceJWTService).(services.JWTService)

	// Error management
	errorManager := errors.NewManager(logger, cfg.Server.Environment != "production")
	defer errorManager.Close()

	// Auth middleware. The tenant and authz providers are wired after
	// InitAll so both are guaranteed registered in the ServiceRegistry.
	authMW := authMiddleware.NewAuthMiddleware(jwtService, errorManager)
	authMW.SetTenantProvider(module.MustGetTyped[iface.TenantProvider](svcRegistry, module.ServiceTenantProvider))
	authMW.SetAccessProvider(module.MustGetTyped[iface.AccessProvider](svcRegistry, module.ServiceAccessProvider))
	authMW.SetAuthzProvider(module.MustGetTyped[iface.AuthzProvider](svcRegistry, module.ServiceAuthzProvider))
	// ADR-0006: the compliance audit-sink probe was removed with the
	// addon. The SetAuditSink seam survives on the middleware and core
	// services, nil by default (usage is nil-guarded) — a fork that adds
	// an audit/compliance module re-wires it the same way compliance did.
	if rev, ok := module.GetTyped[services.SessionRevocationService](svcRegistry, module.ServiceSessionRevocation); ok {
		authMW.SetSessionRevocation(rev)
	}
	// Session-risk lookup: populated by the auth module once its
	// auth_sessions repo is constructed. Hand to both the HTTP
	// middleware (RequireLowRisk gate) and the authz Cedar shadow
	// evaluator (principal.risk_score attribute) so the same score
	// backs both enforcement surfaces. Missing lookup falls back to
	// zero risk on the Cedar principal and a pass-through on the gate.
	if lookup, ok := module.GetTyped[authMiddleware.SessionRiskLookup](svcRegistry, module.ServiceSessionRiskLookup); ok {
		authMW.SetSessionRiskLookup(lookup)
		if authzSvc, ok := module.GetTyped[*authzServices.Service](svcRegistry, module.ServiceAuthzService); ok && authzSvc != nil {
			// The authz service takes its own SessionRiskLookup type —
			// same signature, declared locally to avoid a cross-package
			// alias. Adapt via a thin wrapper.
			authzSvc.SetSessionRiskLookup(authzServices.SessionRiskLookup(lookup))
		}
	}
	// MFA-enrollment lookup + auth policy reader feed RequireStepUp's
	// no-factor branch. When the user has no factor: privileged roles
	// get 403 mfa_enrollment_required; everyone else gets 401
	// password_confirm_required — but only when the policy still accepts
	// the password for that audience — so the frontend can collect a
	// password reconfirm instead of asking for an MFA code that can't
	// exist. SetMFAEnrollmentLookup and SetUserProvider are optional and
	// degrade to the legacy "always emit step_up_required" path when
	// unwired. SetStepUpPolicy is NOT: since PR 3 the no-factor branch
	// must read passwordLoginEnabled{Admin,Client} before offering a
	// reconfirm, so an unwired policy answers 503 auth.policy_unavailable
	// there rather than guessing (an outage is never dressed up as a user
	// obligation). RequireMFA's master-switch read stays nil-tolerant.
	if lookup, ok := module.GetTyped[authMiddleware.MFAEnrollmentLookup](svcRegistry, module.ServiceMFAEnrollmentLookup); ok {
		authMW.SetMFAEnrollmentLookup(lookup)
	}
	// MFA-epoch lookup (spec §4.3 D16). Unlike the two setters below this is
	// tier-DISPATCHING, not tier-agnostic: the auth module builds it from
	// both tiers' user providers because this one middleware instance serves
	// both host muxes. SetUserProvider below is the operator provider only —
	// deliberately not reused here, since resolving a client UUID against
	// operator_users would miss, fail closed, and strip MFA authority from
	// every client-tier token.
	if lookup, ok := module.GetTyped[authMiddleware.MFAEpochLookup](svcRegistry, module.ServiceMFAEpochLookup); ok {
		authMW.SetMFAEpochLookup(lookup)
	} else {
		// ERROR, not WARN, and louder than resolveMFAEpochBumper's: that
		// one reports a fork's user provider predating the seam, which is
		// a supported configuration. This one can only mean the auth
		// module did not register the key it always registers — a wiring
		// regression. It is silent by construction otherwise: the gate
		// keeps passing, every test stays green, and the only symptom is
		// that a removed factor's authority survives.
		logger.Error("auth: MFA epoch lookup is not registered — MFA epoch not enforced; "+
			"a removed factor's authority will survive on tokens already issued until they expire",
			slog.String("service_key", string(module.ServiceMFAEpochLookup)))
	}
	if policy, ok := module.GetTyped[*services.AuthPolicyService](svcRegistry, module.ServiceAuthPolicy); ok && policy != nil {
		authMW.SetStepUpPolicy(policy)
	}
	if userProv, ok := module.GetTyped[iface.UserProvider](svcRegistry, module.ServiceUserService); ok {
		authMW.SetUserProvider(userProv)
	}
	deviceMW := authMiddleware.NewDeviceMiddleware(errorManager)

	// ADR-0003 PR-C: build two audience-scoped surfaces (operator + client),
	// each with its own chi.Mux + huma.API + protected sub-router. Both run
	// in the same process and share the same auth middleware, JWT service,
	// and module registry — only the host-mux dispatch and the per-audience
	// CORS / RequireAudience gates differ. PR-D will split the JWT issuance
	// path so client-side login mints aud=client tokens; until then every
	// monolith-issued token carries aud=operator and only the operator host
	// has registered routes.
	apiConfig := huma.DefaultConfig("Orkestra API", "1.0.0")
	apiConfig.DocsPath = ""
	apiConfig.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"bearerAuth": {
			Type:         "http",
			Scheme:       "bearer",
			BearerFormat: "JWT",
		},
	}

	// Trusted-proxy policy for client-IP resolution. Everything that
	// depends on knowing who is calling — the operator IP allow/blocklist
	// mounted just below, the login geo-block, the per-IP rate limiter,
	// and every audited IP — reads the address this policy produces.
	// A malformed value is fatal: booting with a policy we could not
	// parse would silently fall back to trusting nothing, and a
	// deployment behind a proxy would then attribute every request to
	// the proxy.
	trustedProxies, err := authMiddleware.NewTrustedProxyPolicy(
		cfg.Server.TrustedProxyCount, cfg.Server.TrustedProxyCIDRs)
	if err != nil {
		log.Fatalf("Invalid trusted proxy configuration: %v", err)
	}
	if cfg.IsProductionLike() && !trustedProxies.Configured() {
		logger.Warn("no trusted proxy configured — X-Forwarded-For is ignored and every request " +
			"is attributed to its direct peer. If this deployment sits behind a load balancer or CDN, " +
			"set TRUSTED_PROXY_CIDRS (preferred) or TRUSTED_PROXY_COUNT, otherwise the IP allowlist, " +
			"geo-block, and per-IP rate limits all operate on the proxy's address.")
	}

	operatorMux := chi.NewRouter()
	setupMiddleware(operatorMux, cfg, errorManager, deviceMW, []string{string(module.AudienceOperator), string(module.AudienceService)}, cfg.Server.Operator, logger, trustedProxies)
	// Phase 7: admin-managed IP allow/block gate on the operator host
	// only. Reads ipAllowlistAdmin / ipBlocklistAdmin live from
	// AuthPolicyService on every request — admin edits take effect
	// immediately. Skipped silently when the policy isn't wired.
	if policy, ok := module.GetTyped[*services.AuthPolicyService](svcRegistry, module.ServiceAuthPolicy); ok && policy != nil {
		gate := authMiddleware.NewIPGate(func() (allow []string, block []string) {
			ctx := context.Background()
			return policy.IPAllowlistOperator(ctx), policy.IPBlocklistOperator(ctx)
		})
		operatorMux.Use(gate.Middleware)
	}
	operatorAPI := humachi.New(operatorMux, apiConfig)
	operatorProtected := chi.NewRouter()
	operatorProtected.Use(authMW.RequireAuth)
	operatorProtected.Use(authMiddleware.TenantBaggage)

	clientMux := chi.NewRouter()
	setupMiddleware(clientMux, cfg, errorManager, deviceMW, []string{string(module.AudienceClient)}, cfg.Server.Client, logger, trustedProxies)
	clientAPI := humachi.New(clientMux, apiConfig)
	clientProtected := chi.NewRouter()
	clientProtected.Use(authMW.RequireAuth)
	clientProtected.Use(authMiddleware.TenantBaggage)

	// Module routes — operator-only modules (billing, documents, company,
	// graph, aimodels, rag, agents, sales, dev) register on the Operator
	// surface; the auth core module dual-registers per ADR-0003 PR-D D-4/D-5
	// for the operator and client login paths; PR-D D-7 moves onboarding's
	// public signup, subscriptions' Tier-2 self-service routes (public
	// catalog + /v1/me/subscriptions), and the payments Stripe webhook to
	// the Client surface. Operator-admin oversight of subscriptions /
	// payments stays on the Operator surface (RequireInternalTenant gates
	// preclude a clean client-surface mount).
	operatorSurface := &module.APISurface{
		Audience:        module.AudienceOperator,
		PublicAPI:       operatorAPI,
		ProtectedRouter: operatorProtected,
		AuthMW:          authMW,
	}
	clientSurface := &module.APISurface{
		Audience:        module.AudienceClient,
		PublicAPI:       clientAPI,
		ProtectedRouter: clientProtected,
		AuthMW:          authMW,
	}
	modRegistry.RegisterAllRoutes(&module.RouteInfo{
		Operator: operatorSurface,
		Client:   clientSurface,
		// Root chi.Router for special-case routes (dev/token, SSE that
		// bypasses Huma). Operator-only — dev tokens never need to land
		// on the client host.
		Router: operatorMux,
		// ADR-0003 PR-D D-5: client-side raw HTTP router so the auth
		// module can mount /v1/auth/client/{refresh,refresh-cookie,
		// logout} on the client host mux alongside its Huma routes.
		ClientRouter:  clientMux,
		APIConfig:     apiConfig,
		ConfigService: configService,
	})

	// First-install onboarding: public /v1/setup/status and /v1/setup/admin.
	// Reachable without auth — gated by the "no users exist" invariant
	// enforced inside setup.Service.CreateInitialAdmin. Operator-only:
	// the initial admin is a Tier-1 super_admin, so the wizard lives on
	// the operator host.
	//
	// Admin creation does not create a tenant. The initial Tier-1
	// organization is MANDATORY and is provisioned by the authenticated
	// finalization saga (POST /v1/setup/finalize) that the coordinator
	// record bound during admin creation authorizes — there is no skip.
	//
	// Every seam below is resolved with MustGetTyped: missing wiring must
	// fail module initialization loudly, not degrade a bootstrap endpoint
	// at runtime. The audit sink is the one exception — it belongs to the
	// compliance module and is nil-tolerated.
	setupSvc := setup.NewService(
		module.MustGetTyped[iface.UserProvider](svcRegistry, module.ServiceUserService),
		module.MustGetTyped[setup.AdminCreator](svcRegistry, module.ServicePasswordAuthService),
		module.MustGetTyped[systeminit.FinalizationStore](svcRegistry, module.ServiceSetupFinalizationStore),
		configService,
		newSetupTenantAdapter(module.MustGetTyped[*tenantServices.Service](svcRegistry, module.ServiceTenantService)),
		setupAuditSink(svcRegistry),
		logger,
	)
	setupHandler := setup.NewHandler(setupSvc, cfg.Auth.Cookie)
	setupHandler.RegisterPublicRoutes(operatorAPI)
	// Finalizer access + finalize are AUTHENTICATED operator routes. They
	// share the tenant-baggage-exempt /v1/setup/ prefix (no tenant exists
	// yet) but are mounted behind RequireAuth — never on the public setup
	// registrar. Operator audience is enforced mux-wide by setupMiddleware.
	operatorProtected.Group(func(r chi.Router) {
		api := humachi.New(r, apiConfig)
		setupHandler.RegisterProtectedRoutes(api)
	})

	// Dev-token endpoint (LOCAL DEVELOPMENT ONLY) — synthetic JWTs for
	// first login + local API testing, used by scripts/devtoken.sh and the
	// console's "Sign in with dev token" affordance. Re-provided in core
	// after ADR-0006 removed the dev addon. Mounted as a raw chi route on
	// the operator root mux (bypasses Huma, hidden from /docs); never on
	// the client host. No DB writes.
	//
	// The gate is IsProductionLike, not IsProduction: the endpoint hands a
	// signed super_admin token to any anonymous caller, so it must not
	// exist on staging either — staging is internet-reachable. Handler.
	// RegisterRoutes enforces the same rule independently.
	if !cfg.IsProductionLike() {
		// Resolver: an operator dev token that doesn't pin a tenant defaults
		// to the OPERATIONAL platform default (tenant_defaults pointer) — the
		// old newest-internal shortcut could select an archived tenant.
		// Nil-safe: no default assigned → tenant-less token (legacy behavior).
		var devTenantResolver devtoken.DefaultTenantResolver
		if dp, ok := module.GetTyped[iface.DefaultTenantProvider](svcRegistry, module.ServiceDefaultTenantProvider); ok && dp != nil {
			devTenantResolver = func(ctx context.Context) (string, string) {
				t, err := dp.GetDefaultTenant(ctx)
				if err != nil || t == nil {
					return "", ""
				}
				return t.UUID, "internal"
			}
		}
		devtoken.NewHandler(
			module.MustGetTyped[iface.JWTProvider](svcRegistry, module.ServiceOperatorJWTService),
			module.MustGetTyped[iface.JWTProvider](svcRegistry, module.ServiceClientJWTService),
			cfg,
			devTenantResolver,
			logger,
		).RegisterRoutes(operatorMux)
	}

	// Admin module management routes: platform-level, not per-org. Split
	// into reads and mutations so Block B can require MFA on the paths
	// that write secrets or flip module enablement. Both share the same
	// system permission; the MFA gate on the mutation group layers on top.
	// Operator-only — module enable/disable is a Tier-1 operator concern.
	moduleAdminHandler := module.NewModuleAdminHandler(configService, modRegistry)
	if err := wireModuleAdminAudit(moduleAdminHandler, svcRegistry); err != nil {
		log.Fatalf("Failed to wire module admin audit: %v", err)
	}
	operatorProtected.Group(func(r chi.Router) {
		r.Use(authMW.RequireSystemPermission("system.modules.admin"))
		adminAPI := humachi.New(r, apiConfig)
		module.RegisterAdminModuleReadRoutes(adminAPI, moduleAdminHandler)
	})
	operatorProtected.Group(func(r chi.Router) {
		r.Use(authMW.RequireSystemPermission("system.modules.admin"))
		r.Use(authMW.RequireMFA())
		// Section C item #2: also gate admin-module writes on low session
		// risk. A stolen MFA-stepped token from a high-risk session (new
		// device + new IP + rapid IP change) still can't flip module
		// enablement or write secrets. Threshold is env-tunable so ops
		// can widen the gate during staged rollouts.
		r.Use(authMW.RequireLowRisk(riskStepUpThreshold()))
		// The audit actor's User-Agent: Huma hands the handler a
		// context.Context, not the *http.Request, so the header has to be
		// stamped onto the context here.
		r.Use(authMiddleware.RequestMeta)
		adminAPI := humachi.New(r, apiConfig)
		module.RegisterAdminModuleMutationRoutes(adminAPI, moduleAdminHandler)
	})

	operatorMux.Mount("/", operatorProtected)
	clientMux.Mount("/", clientProtected)

	// Health, readiness, docs — served on both surfaces so orchestrator
	// probes (k8s liveness, ALB target health) can hit either host.
	// operatorAPI and clientAPI share a single OpenAPI document (both built
	// from apiConfig), and huma v2.39+ panics on a duplicate operation ID —
	// so the health/readiness operations are registered via Huma once, on the
	// operator API (which owns the shared document), and the client host
	// serves the same probes as raw routes. Both /openapi.json endpoints
	// still document /health + /ready via the operator registration.
	registerHealthEndpoints(operatorAPI, db, redisClient)
	registerHealthProbes(clientMux, db, redisClient)

	// /docs + /openapi.json are on by default in development and OFF in
	// production-like environments (API_DOCS_ENABLED). The document is a
	// complete route inventory, and the docs page runs a third-party bundle
	// on the API origin — the origin that holds the HttpOnly refresh cookie
	// — so an internet-reachable deployment must opt in explicitly and gate
	// both paths at the edge. OPENAPI_DUMP below is unaffected: it reads the
	// in-memory document, not the route.
	if cfg.Server.APIDocsEnabled {
		if cfg.IsProductionLike() {
			logger.Warn("API docs enabled on a production-like environment; gate /docs and /openapi.json at the edge",
				slog.String("env", cfg.Server.Environment))
		}
		registerDocsEndpoints(operatorMux, operatorAPI)
		registerDocsEndpoints(clientMux, clientAPI)
	} else {
		logger.Info("API docs disabled (/docs, /openapi.json); set API_DOCS_ENABLED=true to serve them",
			slog.String("env", cfg.Server.Environment))
	}

	// OPENAPI_DUMP mode (used by `make openapi-dump`): after every module
	// has been wired and its routes registered, serialize the OpenAPI
	// document to OPENAPI_DUMP_PATH and exit. We never bind a listener
	// in this mode, so the dump cost is dominated by module Init
	// (Mongo collection ensure, registry seed).
	//
	// Note: operatorAPI and clientAPI currently share a single in-memory
	// OpenAPI document because they're constructed from the same apiConfig.
	// The audience split lives at the mux/host level; both /openapi.json
	// endpoints serve identical content. If the surfaces are ever wired
	// with distinct huma.Config instances, this branch can dump per-
	// audience files via separate env vars.
	if os.Getenv("OPENAPI_DUMP") != "" {
		path := filepath.Clean(os.Getenv("OPENAPI_DUMP_PATH"))
		if path == "" || path == "." {
			log.Fatal("OPENAPI_DUMP=1 set but OPENAPI_DUMP_PATH is empty")
		}
		b, err := json.MarshalIndent(operatorAPI.OpenAPI(), "", "  ")
		if err != nil {
			log.Fatalf("openapi marshal: %v", err)
		}
		b = append(b, '\n')
		if err := os.WriteFile(path, b, 0o600); err != nil {
			log.Fatalf("openapi write: %v", err)
		}
		logger.Info("openapi dump",
			slog.String("path", path),
			slog.Int("bytes", len(b)))
		os.Exit(0)
	}

	// Phase 5.3: Prometheus /metrics endpoint. Mounted on the operator
	// mux for in-product browsing AND on the LAN ops handler so a
	// Prometheus scrape against orkestra-backend:3000/metrics works
	// without spoofing the operator Host header (Prometheus has no
	// per-scrape Host override). Kept off the client mux so a browser
	// hitting api.orkestra.com/metrics still 404s — the reverse proxy +
	// hostMux gate together preserve the "don't leak cardinality on the
	// public client surface" intent. METRICS_ENABLED is respected but
	// defaults to true because a scrape on a disabled handler just
	// yields 404 — cheap enough to leave on in every environment.
	var metricsHandler http.Handler
	if os.Getenv("METRICS_ENABLED") != "false" {
		mc := metrics.Default()
		if err := mc.Register(); err != nil {
			logger.Warn("metrics: registry init failed; /metrics will serve an empty body",
				slog.String("error", err.Error()))
		}
		stopLag := mc.Start(15 * time.Second)
		defer stopLag()
		metricsHandler = mc.Handler()
		operatorMux.Handle("/metrics", metricsHandler)
	}

	// Host mux dispatches by Host header. In dev (ENV=development) an
	// unmatched host falls through to operatorMux so curl
	// http://localhost:3000 still works without DNS gymnastics; in prod
	// an unmatched host gets 421 Misdirected Request.
	hostRoutes := map[string]http.Handler{}
	if cfg.Server.Operator.Host != "" {
		hostRoutes[cfg.Server.Operator.Host] = operatorMux
	}
	if cfg.Server.Client.Host != "" {
		hostRoutes[cfg.Server.Client.Host] = clientMux
	}
	var devFallthrough http.Handler
	if cfg.Server.Environment == "development" {
		devFallthrough = operatorMux
	}
	// LAN probe escape hatch: HAProxy / k8s liveness checks hit the pod
	// by IP, so their Host header never matches an audience. Carve out
	// /health and /ready (only) so those probes can answer 200 without
	// spoofing a Host header. Everything else on a non-matching host
	// still gets 421 — the host-header smuggling guard stays intact.
	root := newHostMux(hostRoutes, devFallthrough, lanOpsHandler(db, redisClient, metricsHandler))

	// HTTP server. The host mux is wrapped in otelhttp.NewHandler so every
	// request spawns a span the tenant-baggage middleware can enrich
	// with tenant.id / tenant.kind / user.id (ADR-0001 Phase 4.4). When
	// no OTLP endpoint is configured, span creation is still cheap and
	// the attribute writes are no-ops — there is no dev-vs-prod
	// divergence at the middleware level.
	handler := otelhttp.NewHandler(root, "http.request")
	srv := &http.Server{
		Addr:           fmt.Sprintf(":%s", cfg.Server.Port),
		Handler:        handler,
		ReadTimeout:    15 * time.Second,
		WriteTimeout:   7 * time.Minute,
		IdleTimeout:    60 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	// Build the start set: only modules that are currently enabled in the config
	// service (DB/env) get Start()ed. Others are initialized and routed but idle.
	startSet := make(map[string]bool)
	for _, name := range allOptNames {
		if configService.IsEnabled(context.Background(), name) {
			startSet[name] = true
		}
	}
	// Core modules are always started.
	for _, factory := range coreModules(cfg) {
		m := factory()
		startSet[m.Name()] = true
	}

	// Start module background jobs
	if err := modRegistry.StartAll(context.Background(), startSet); err != nil {
		log.Fatalf("Failed to start modules: %v", err)
	}

	if !cfg.IsProduction() {
		utils.PrintDevelopmentWarning(cfg.Server.Environment)
	}

	// Serve
	go func() {
		logger.Info("Starting server",
			slog.String("port", cfg.Server.Port),
		)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("Failed to start server", slog.String("error", err.Error()))
			os.Exit(1)
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("Shutting down server...")
	modRegistry.StopAll(context.Background())

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("Failed to shutdown server gracefully", slog.String("error", err.Error()))
	}
	if err := database.DisconnectMongo(shutdownCtx, db); err != nil {
		logger.Error("Failed to disconnect from MongoDB", slog.String("error", err.Error()))
	}
	if err := database.DisconnectRedis(redisClient); err != nil {
		logger.Error("Failed to disconnect from Redis", slog.String("error", err.Error()))
	}

	logger.Info("Server stopped")
}

// riskStepUpThreshold parses AUTH_RISK_STEP_UP_THRESHOLD as a float in
// [0, 1] and falls back to 0.5 on unset / malformed values. 0.5 is the
// "high" bucket boundary from auth/services.RiskLevelForScore — any
// single strong signal (new_device_fingerprint + new_ip + rapid_ip)
// pushes a login over this line. Operators can tighten to 0.3 (medium)
// for paranoid deployments or loosen toward 0.7 (critical) to keep the
// gate limited to near-certain attack signatures.
func riskStepUpThreshold() float64 {
	raw := os.Getenv("AUTH_RISK_STEP_UP_THRESHOLD")
	if raw == "" {
		return 0.5
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil || v < 0 || v > 1 {
		return 0.5
	}
	return v
}
