// AI Service — standalone sidecar for the AI module chain.
//
// Boots 4 modules (graph, aimodels, rag, agents) with their own MongoDB database.
// Validates JWTs using only the RS256 public key — no auth module dependency.
//
// Endpoints:
//   - Public-facing: all /v1/ai/*, /v1/rag/*, /v1/agents/*, /v1/graph/* routes
//   - Internal (service-to-service): /v1/internal/ai/*, /v1/internal/rag/*
//   - Health: /health, /ready
package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/orkestra/backend/internal/addons/agents"
	"github.com/orkestra/backend/internal/addons/aimodels"
	"github.com/orkestra/backend/internal/addons/graph"
	"github.com/orkestra/backend/internal/addons/rag"
	"github.com/orkestra/backend/internal/shared/config"
	"github.com/orkestra/backend/internal/shared/container"
	"github.com/orkestra/backend/internal/shared/database"
	"github.com/orkestra/backend/internal/shared/middleware"
	"github.com/orkestra/backend/internal/shared/module"
	"github.com/orkestra/backend/internal/shared/utils"
)

func main() {
	logger := utils.SetupLogger()
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Connect MongoDB. 2-minute budget accommodates retry-with-backoff
	// in NewMongoConnection/NewRedisConnection that waits out first-boot
	// auth init races.
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

	// Redis (used by ConfigService for caching)
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

	// Module config infrastructure
	configRepo := module.NewModuleConfigRepository(db)
	configService := module.NewModuleConfigService(configRepo, redisAdapter, logger)

	// Module registry — only AI chain modules
	svcRegistry := module.NewServiceRegistry()
	modRegistry := module.NewModuleRegistry(logger)
	modRegistry.SetConfigService(configService)
	modRegistry.SetContainerManager(container.NewManager(logger))
	modDeps := &module.Dependencies{
		DB:           db,
		RedisAdapter: redisAdapter,
		Config:       cfg,
		Logger:       logger,
		Services:     svcRegistry,
	}

	// Register AI modules (order matters: producers before consumers)
	modRegistry.Register(graph.NewModule())    // produces GraphRepository
	modRegistry.Register(aimodels.NewModule()) // produces AIModelProvider
	modRegistry.Register(rag.NewModule())      // consumes Graph + AIModels → produces RAGQuery
	modRegistry.Register(agents.NewModule())   // consumes RAGQuery

	if err := modRegistry.InitAll(cfg, modDeps); err != nil {
		log.Fatalf("Failed to initialize AI modules: %v", err)
	}

	// JWT validator — public key only, no auth module needed. Env stamp
	// matches what the monolith mints (orkestra.<env>); cross-env tokens
	// are rejected.
	jwtValidator, err := middleware.NewJWTValidator(cfg.Auth.JWT.PublicKeyPath, cfg.Server.Environment)
	if err != nil {
		log.Fatalf("Failed to create JWT validator: %v", err)
	}

	// Share the monolith's revoked-session set so a logout or admin-kill
	// invalidates tokens here too. Both binaries point at the same Redis
	// instance and agree on the auth:revoked:session:<sid> key namespace.
	jwtValidator.SetSessionRevocation(&sidecarSessionRevocationChecker{client: redisAdapter})

	// Router + middleware
	router := chi.NewRouter()
	setupAIMiddleware(router, cfg)

	// API config
	apiConfig := huma.DefaultConfig("Orkestra AI Service", "1.0.0")
	apiConfig.DocsPath = ""
	apiConfig.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"bearerAuth": {
			Type:         "http",
			Scheme:       "bearer",
			BearerFormat: "JWT",
		},
	}

	publicAPI := humachi.New(router, apiConfig)

	// Protected router — JWT validation only (no session refresh, no cookies)
	protectedRouter := chi.NewRouter()
	protectedRouter.Use(jwtValidator.RequireAuth)

	// Module routes — JWTValidator satisfies module.RoleMiddleware. The AI
	// sidecar serves only the `service` audience, but at PR-A the AI modules
	// (graph/aimodels/rag/agents) still register on the operator surface; PR-C
	// flips them onto a dedicated `service` APISurface with its own JWT
	// audience check. Until then both surfaces point at the same instances.
	aiSurface := &module.APISurface{
		Audience:        module.AudienceOperator,
		PublicAPI:       publicAPI,
		ProtectedRouter: protectedRouter,
		AuthMW:          jwtValidator,
	}
	modRegistry.RegisterAllRoutes(&module.RouteInfo{
		Operator:      aiSurface,
		Client:        aiSurface,
		Router:        router,
		APIConfig:     apiConfig,
		ConfigService: configService,
	})

	router.Mount("/", protectedRouter)

	// Health and readiness
	registerAIHealthEndpoints(publicAPI, db, redisClient)

	// API docs
	registerAIDocsEndpoints(router, publicAPI)

	// HTTP server
	port := cfg.Server.Port
	if aiPort := os.Getenv("AI_SERVICE_PORT"); aiPort != "" {
		port = aiPort
	}

	srv := &http.Server{
		Addr:           fmt.Sprintf(":%s", port),
		Handler:        router,
		ReadTimeout:    15 * time.Second,
		WriteTimeout:   5 * time.Minute, // Long timeout for LLM generation
		IdleTimeout:    60 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	// Start all AI module background jobs — in the AI service all registered modules are started.
	aiStartSet := make(map[string]bool)
	for _, m := range modRegistry.AllModules() {
		aiStartSet[m.Name()] = true
	}
	if err := modRegistry.StartAll(context.Background(), aiStartSet); err != nil {
		log.Fatalf("Failed to start AI modules: %v", err)
	}

	logger.Info("AI Service starting",
		slog.String("port", port),
		slog.String("environment", cfg.Server.Environment),
	)

	// Serve
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("Failed to start AI service", slog.String("error", err.Error()))
			os.Exit(1)
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("Shutting down AI service...")
	modRegistry.StopAll(context.Background())

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("Failed to shutdown AI service gracefully", slog.String("error", err.Error()))
	}
	if err := database.DisconnectMongo(shutdownCtx, db); err != nil {
		logger.Error("Failed to disconnect from MongoDB", slog.String("error", err.Error()))
	}
	if err := database.DisconnectRedis(redisClient); err != nil {
		logger.Error("Failed to disconnect from Redis", slog.String("error", err.Error()))
	}

	logger.Info("AI Service stopped")
}

// sidecarSessionRevocationChecker is a tiny middleware.SessionRevocationChecker
// for the sidecar. It talks to the same Redis as the monolith and agrees on
// the "auth:revoked:session:<sid>" key, so a logout or admin-kill on the
// monolith invalidates sidecar traffic too. Fails open on Redis errors to
// avoid coupling availability — same stance as the monolith's service.
type sidecarSessionRevocationChecker struct {
	client *database.RedisClientAdapter
}

func (c *sidecarSessionRevocationChecker) IsRevoked(ctx context.Context, sid string) (bool, error) {
	if sid == "" || c == nil || c.client == nil {
		return false, nil
	}
	_, err := c.client.Get(ctx, "auth:revoked:session:"+sid)
	if err == nil {
		return true, nil
	}
	// go-redis returns redis.Nil for missing keys; treat anything else as a
	// transient error and fail open. The monolith's service logs the error;
	// we rely on that surface rather than duplicating it here.
	if strings.Contains(err.Error(), "redis: nil") {
		return false, nil
	}
	return false, nil
}

// setupAIMiddleware configures global middleware for the AI service.
func setupAIMiddleware(router *chi.Mux, cfg *config.Config) {
	// Security headers
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("X-Frame-Options", "DENY")
			if cfg.IsProductionLike() {
				w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}
			next.ServeHTTP(w, r)
		})
	})

	// Body size limit
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > cfg.Server.MaxBodySize {
				http.Error(w, "Request body too large", http.StatusRequestEntityTooLarge)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, cfg.Server.MaxBodySize)
			next.ServeHTTP(w, r)
		})
	})

	// CORS
	router.Use(cors.Handler(cors.Options{
		AllowedOrigins:   cfg.Server.CORSOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Request-ID", "X-Tenant-ID"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	router.Use(chiMiddleware.RequestID)
	router.Use(chiMiddleware.RealIP)

	// Logger (exclude /health)
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/health" {
				chiMiddleware.Logger(next).ServeHTTP(w, r)
			} else {
				next.ServeHTTP(w, r)
			}
		})
	})

	// Inject HTTP request into context for Huma handlers
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), "http_request", r)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})

	router.Use(chiMiddleware.Recoverer)

	// Timeout with SSE bypass
	router.Use(func(next http.Handler) http.Handler {
		timeoutHandler := chiMiddleware.Timeout(60 * time.Second)(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/stream") {
				next.ServeHTTP(w, r)
				return
			}
			timeoutHandler.ServeHTTP(w, r)
		})
	})
}
