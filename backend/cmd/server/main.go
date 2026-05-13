package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"

	"github.com/orkestra-cc/orkestra-sdk/iface"
	"github.com/orkestra-cc/orkestra-sdk/metrics"
	"github.com/orkestra-cc/orkestra-sdk/module"
	"github.com/orkestra/backend/internal/core/auth/services"
	authzServices "github.com/orkestra/backend/internal/core/authz/services"
	"github.com/orkestra/backend/internal/shared/config"
	"github.com/orkestra/backend/internal/shared/container"
	"github.com/orkestra/backend/internal/shared/database"
	"github.com/orkestra/backend/internal/shared/errors"
	authMiddleware "github.com/orkestra/backend/internal/shared/middleware"
	"github.com/orkestra/backend/internal/shared/remote"
	"github.com/orkestra/backend/internal/shared/setup"
	"github.com/orkestra/backend/internal/shared/systeminit"
	"github.com/orkestra/backend/internal/shared/telemetry"
	"github.com/orkestra/backend/internal/shared/utils"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

func main() {
	logger := utils.SetupLogger()
	slog.SetDefault(logger)

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
	// PII producer registry is pre-created here so producer modules can
	// register themselves during their own Init; compliance reads it when
	// servicing DSR requests. See iface.PIIProducerRegistry.
	svcRegistry.Register(module.ServicePIIProducerRegistry, iface.NewPIIProducerRegistry())
	modRegistry := module.NewModuleRegistry(logger)
	modRegistry.SetConfigService(configService)
	modRegistry.SetContainerManager(container.NewManager(logger))
	modDeps := &module.Dependencies{
		DB:           db,
		RedisAdapter: redisAdapter,
		Config:       cfg,
		Platform:     cfg,
		Logger:       logger,
		Services:     svcRegistry,
	}

	// Core modules — always loaded (auth, users, navigation)
	for _, factory := range coreModules {
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

	if err := modRegistry.InitAll(modDeps); err != nil {
		log.Fatalf("Failed to initialize modules: %v", err)
	}

	// AI service sidecar: register remote providers for AI modules
	// that failed Init locally (e.g. external infra not available).
	if cfg.Server.AIServiceURL != "" {
		remoteAI := remote.NewAIModelProvider(cfg.Server.AIServiceURL)
		remoteRAG := remote.NewRAGQueryProvider(cfg.Server.AIServiceURL)

		failedModules := modRegistry.FailedModules()
		if _, failed := failedModules["aimodels"]; failed {
			svcRegistry.Register(module.ServiceAIModelProvider, remoteAI)
		}
		if _, failed := failedModules["rag"]; failed {
			svcRegistry.Register(module.ServiceRAGQuery, remoteRAG)
		}
		logger.Info("AI service sidecar configured",
			slog.String("url", cfg.Server.AIServiceURL),
		)
	}

	// Retrieve auth infrastructure for middleware setup
	jwtService := svcRegistry.MustGet(module.ServiceJWTService).(services.JWTService)
	authService := svcRegistry.MustGet(module.ServiceAuthService).(services.AuthService)

	// Error management
	errorManager := errors.NewManager(logger, cfg.Server.Environment != "production")
	defer errorManager.Close()

	// Auth middleware. The tenant and authz providers are wired after
	// InitAll so both are guaranteed registered in the ServiceRegistry.
	authMW := authMiddleware.NewAuthMiddlewareWithConfig(jwtService, errorManager, cfg)
	authMW.SetAuthService(authService)
	authMW.SetTenantProvider(module.MustGetTyped[iface.TenantProvider](svcRegistry, module.ServiceTenantProvider))
	authMW.SetAccessProvider(module.MustGetTyped[iface.AccessProvider](svcRegistry, module.ServiceAccessProvider))
	authMW.SetAuthzProvider(module.MustGetTyped[iface.AuthzProvider](svcRegistry, module.ServiceAuthzProvider))
	if sink, ok := module.GetTyped[iface.AuditSink](svcRegistry, module.ServiceAuditSink); ok {
		authMW.SetAuditSink(sink)
	}
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
	// password_confirm_required so the frontend can collect a password
	// reconfirm instead of asking for an MFA code that can't exist. All
	// three setters are optional — when any is unwired, RequireStepUp
	// falls back to today's "always emit step_up_required" behaviour.
	if lookup, ok := module.GetTyped[authMiddleware.MFAEnrollmentLookup](svcRegistry, module.ServiceMFAEnrollmentLookup); ok {
		authMW.SetMFAEnrollmentLookup(lookup)
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

	operatorMux := chi.NewRouter()
	setupMiddleware(operatorMux, cfg, errorManager, deviceMW, string(module.AudienceOperator), cfg.Server.Operator)
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
	setupMiddleware(clientMux, cfg, errorManager, deviceMW, string(module.AudienceClient), cfg.Server.Client)
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
	setupSvc := setup.NewService(
		module.MustGetTyped[iface.UserProvider](svcRegistry, module.ServiceUserService),
		module.MustGetTyped[setup.AdminCreator](svcRegistry, module.ServicePasswordAuthService),
		configService,
		logger,
	)
	setup.NewHandler(setupSvc, cfg.Auth.Cookie).RegisterRoutes(operatorAPI)

	// Admin module management routes: platform-level, not per-org. Split
	// into reads and mutations so Block B can require MFA on the paths
	// that write secrets or flip module enablement. Both share the same
	// system permission; the MFA gate on the mutation group layers on top.
	// Operator-only — module enable/disable is a Tier-1 operator concern.
	moduleAdminHandler := module.NewModuleAdminHandler(configService, modRegistry)
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
		adminAPI := humachi.New(r, apiConfig)
		module.RegisterAdminModuleMutationRoutes(adminAPI, moduleAdminHandler)
	})

	operatorMux.Mount("/", operatorProtected)
	clientMux.Mount("/", clientProtected)

	// Health, readiness, docs — registered on both surfaces so
	// orchestrator probes (k8s liveness, ALB target health) can hit
	// either host. Each audience gets its own /openapi.json so SDK
	// generators see only that audience's surface.
	registerHealthEndpoints(operatorAPI, db, redisClient)
	registerDocsEndpoints(operatorMux, operatorAPI)
	registerHealthEndpoints(clientAPI, db, redisClient)
	registerDocsEndpoints(clientMux, clientAPI)

	// Phase 5.3: Prometheus /metrics endpoint. Operator-only — Prometheus
	// scrapes from inside the cluster against the operator host; exposing
	// metrics on the client host would leak internal cardinality to any
	// browser hitting api.orkestra.com/metrics. METRICS_ENABLED is
	// respected but defaults to true because a scrape on a disabled
	// handler just yields 404 — cheap enough to leave on in every
	// environment.
	if os.Getenv("METRICS_ENABLED") != "false" {
		mc := metrics.Default()
		if err := mc.Register(); err != nil {
			logger.Warn("metrics: registry init failed; /metrics will serve an empty body",
				slog.String("error", err.Error()))
		}
		stopLag := mc.Start(15 * time.Second)
		defer stopLag()
		operatorMux.Handle("/metrics", mc.Handler())
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
	root := newHostMux(hostRoutes, devFallthrough, lanOpsHandler(db, redisClient))

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
	for _, factory := range coreModules {
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
			slog.String("environment", cfg.Server.Environment),
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
