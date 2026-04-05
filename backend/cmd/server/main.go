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
	"strings"
	"syscall"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/orkestra/backend/internal/auth"
	"github.com/orkestra/backend/internal/auth/services"
	"github.com/orkestra/backend/internal/billing"
	"github.com/orkestra/backend/internal/company"
	"github.com/orkestra/backend/internal/graph"
	"github.com/orkestra/backend/internal/documents"
	"github.com/orkestra/backend/internal/aimodels"
	"github.com/orkestra/backend/internal/agents"
	"github.com/orkestra/backend/internal/sales"
	"github.com/orkestra/backend/internal/rag"
	"github.com/orkestra/backend/internal/dev"
	"github.com/orkestra/backend/internal/navigation"
	"github.com/orkestra/backend/internal/reporting"
	"github.com/orkestra/backend/internal/user"
	"github.com/orkestra/backend/internal/shared/config"
	"github.com/orkestra/backend/internal/shared/database"
	"github.com/orkestra/backend/internal/shared/errors"
	authMiddleware "github.com/orkestra/backend/internal/shared/middleware"
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

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	mongoConfig := database.MongoConfig{
		URI:             cfg.Database.MongoURI,
		Database:        cfg.Database.DatabaseName,
		MaxPoolSize:     cfg.Database.MaxPoolSize,
		MinPoolSize:     cfg.Database.MinPoolSize,
		MaxConnIdleTime: cfg.Database.MaxConnIdleTime,
		ConnectTimeout:  cfg.Database.ConnectTimeout,
	}

	db, err := database.NewMongoConnection(ctx, mongoConfig)
	if err != nil {
		log.Fatalf("Failed to connect to MongoDB: %v", err)
	}

	redisConfig := database.RedisConfig{
		URL:             cfg.Redis.URL,
		MaxRetries:      cfg.Redis.MaxRetries,
		MinIdleConns:    cfg.Redis.MinIdleConns,
		MaxIdleConns:    cfg.Redis.MaxIdleConns,
		ConnMaxLifetime: cfg.Redis.ConnMaxLifetime,
		ReadTimeout:     cfg.Redis.ReadTimeout,
		WriteTimeout:    cfg.Redis.WriteTimeout,
	}

	redisClient, err := database.NewRedisConnection(ctx, redisConfig)
	if err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}

	redisClientAdapter := database.NewRedisClientAdapter(redisClient)

	// Initialize module registry
	svcRegistry := module.NewServiceRegistry()
	modRegistry := module.NewModuleRegistry(logger)
	modDeps := &module.Dependencies{
		DB:           db,
		RedisAdapter: redisClientAdapter,
		Config:       cfg,
		Logger:       logger,
		Services:     svcRegistry,
	}

	// Register all modules (order matters: producers before consumers)
	modRegistry.Register(user.NewModule())      // produces UserService
	modRegistry.Register(auth.NewModule())       // consumes UserService, produces JWTService + AuthService
	modRegistry.Register(navigation.NewModule())
	modRegistry.Register(reporting.NewModule())
	modRegistry.Register(documents.NewModule())  // produces PDFService
	modRegistry.Register(aimodels.NewModule())   // produces AIModelProvider
	modRegistry.Register(company.NewModule())
	modRegistry.Register(billing.NewModule())    // consumes PDFService
	modRegistry.Register(graph.NewModule())      // produces GraphRepository
	modRegistry.Register(rag.NewModule())        // consumes Graph + AIModels, produces RAGQuery
	modRegistry.Register(sales.NewModule())      // consumes AIModels
	modRegistry.Register(agents.NewModule())     // consumes RAGQuery
	modRegistry.Register(dev.NewModule())        // consumes JWTService

	if err := modRegistry.InitAll(cfg, modDeps); err != nil {
		log.Fatalf("Failed to initialize modules: %v", err)
	}

	// Retrieve auth infrastructure from module registry for middleware setup
	jwtService := svcRegistry.MustGet(module.ServiceJWTService).(services.JWTService)
	authService := svcRegistry.MustGet(module.ServiceAuthService).(services.AuthService)

	// Initialize error management system
	isDevelopment := cfg.Server.Environment != "production"
	errorManager := errors.NewManager(logger, isDevelopment)
	defer errorManager.Close()

	// Initialize middleware with JWT service for consistent token handling
	authMiddlewareHandler := authMiddleware.NewAuthMiddlewareWithConfig(jwtService, errorManager, cfg)
	// Set auth service for auto-refresh functionality
	authMiddlewareHandler.SetAuthService(authService)
	deviceMiddlewareHandler := authMiddleware.NewDeviceMiddleware(errorManager)

	router := chi.NewRouter()

	// Security headers middleware
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Security headers to prevent common attacks
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("X-XSS-Protection", "1; mode=block")
			w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

			// HSTS header for production (force HTTPS)
			if cfg.IsProductionLike() {
				w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}

			next.ServeHTTP(w, r)
		})
	})

	// Request body size limit middleware
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

	// CORS middleware - must be before other middleware
	router.Use(cors.Handler(cors.Options{
		AllowedOrigins:   cfg.Server.CORSOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token", "X-Request-ID"},
		ExposedHeaders:   []string{"Link", "X-Total-Count", "X-Ratelimit-Limit", "X-Ratelimit-Remaining", "X-New-Access-Token", "X-Token-Refreshed"},
		AllowCredentials: true,
		MaxAge:           300, // 5 minutes
	}))

	router.Use(middleware.RequestID)
	router.Use(middleware.RealIP)
	// Custom logger that excludes /health endpoint
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/health" {
				middleware.Logger(next).ServeHTTP(w, r)
			} else {
				next.ServeHTTP(w, r)
			}
		})
	})

	// Add device information extraction middleware early in the chain
	router.Use(deviceMiddlewareHandler.ExtractDeviceInfo)

	// Add middleware to inject HTTP request into context for Huma handlers
	// Must be careful not to consume the request body as Huma needs to read it
	// Place after device middleware to avoid body consumption issues
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Create a new context with the HTTP request reference
			// Don't modify the request body - Huma will handle that
			ctx := context.WithValue(r.Context(), "http_request", r)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})

	// Add our error handling middleware
	router.Use(errorManager.GetErrorHandler().Middleware())
	router.Use(errorManager.GetValidator().Middleware())
	router.Use(errorManager.GetRateLimiter().Middleware("api:general"))

	// Keep the default recoverer as a fallback
	router.Use(middleware.Recoverer)
	// Timeout middleware with bypass for SSE streaming endpoints
	router.Use(func(next http.Handler) http.Handler {
		timeoutHandler := middleware.Timeout(60 * time.Second)(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/stream") {
				next.ServeHTTP(w, r)
				return
			}
			timeoutHandler.ServeHTTP(w, r)
		})
	})

	apiConfig := huma.DefaultConfig("Orkestra API", "1.0.0")
	apiConfig.DocsPath = ""
	apiConfig.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"bearerAuth": {
			Type:         "http",
			Scheme:       "bearer",
			BearerFormat: "JWT",
		},
	}

	// Register public routes first
	publicAPI := humachi.New(router, apiConfig)

	// Create protected routes with authentication middleware
	protectedRouter := chi.NewRouter()
	protectedRouter.Use(authMiddlewareHandler.RequireAuth)

	// Register routes for all modules
	modRegistry.RegisterAllRoutes(&module.RouteInfo{
		PublicAPI:        publicAPI,
		ProtectedRouter:  protectedRouter,
		Router:           router,
		AuthMW:           authMiddlewareHandler,
		APIConfig:        apiConfig,
	})

	// Mount the protected routes
	router.Mount("/", protectedRouter)

	huma.Register(publicAPI, huma.Operation{
		OperationID: "health-check",
		Method:      http.MethodGet,
		Path:        "/health",
		Summary:     "Health check",
		Description: "Returns the health status of the application",
		Tags:        []string{"Health"},
	}, func(ctx context.Context, _ *struct{}) (*struct {
		Body struct {
			Status  string            `json:"status"`
			Time    string            `json:"time"`
			Version string            `json:"version"`
			Checks  map[string]string `json:"checks"`
		}
	}, error) {
		checks := map[string]string{}

		ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()

		if err := db.Client().Ping(ctx, nil); err != nil {
			checks["mongodb"] = "down"
		} else {
			checks["mongodb"] = "up"
		}

		if err := redisClient.Ping(ctx).Err(); err != nil {
			checks["redis"] = "down"
		} else {
			checks["redis"] = "up"
		}

		status := "healthy"
		for _, check := range checks {
			if check == "down" {
				status = "unhealthy"
				break
			}
		}

		return &struct {
			Body struct {
				Status  string            `json:"status"`
				Time    string            `json:"time"`
				Version string            `json:"version"`
				Checks  map[string]string `json:"checks"`
			}
		}{
			Body: struct {
				Status  string            `json:"status"`
				Time    string            `json:"time"`
				Version string            `json:"version"`
				Checks  map[string]string `json:"checks"`
			}{
				Status:  status,
				Time:    time.Now().UTC().Format(time.RFC3339),
				Version: "1.0.0",
				Checks:  checks,
			},
		}, nil
	})

	huma.Register(publicAPI, huma.Operation{
		OperationID: "readiness-check",
		Method:      http.MethodGet,
		Path:        "/ready",
		Summary:     "Readiness check",
		Description: "Returns whether the application is ready to accept requests",
		Tags:        []string{"Health"},
	}, func(ctx context.Context, _ *struct{}) (*struct {
		Body struct {
			Ready bool `json:"ready"`
		}
	}, error) {
		ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()

		ready := true

		if err := db.Client().Ping(ctx, nil); err != nil {
			ready = false
		}

		if err := redisClient.Ping(ctx).Err(); err != nil {
			ready = false
		}

		return &struct {
			Body struct {
				Ready bool `json:"ready"`
			}
		}{
			Body: struct {
				Ready bool `json:"ready"`
			}{
				Ready: ready,
			},
		}, nil
	})

	// Scalar API Documentation
	router.Get("/docs", func(w http.ResponseWriter, r *http.Request) {
		// Override HAProxy CSP for documentation page to allow Scalar CDN
		// This CSP is more permissive to allow the documentation to work
		w.Header().Set("Content-Security-Policy", "default-src 'self' https://cdn.jsdelivr.net; script-src 'self' 'unsafe-inline' 'unsafe-eval' https://cdn.jsdelivr.net; style-src 'self' 'unsafe-inline' https://cdn.jsdelivr.net https://fonts.googleapis.com; connect-src 'self' http://localhost:* https://*.blacklab.cc; img-src 'self' data: https:; font-src 'self' data: https://cdn.jsdelivr.net https://fonts.gstatic.com https://fonts.googleapis.com;")
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<!doctype html>
<html>
<head>
    <title>Orkestra API Documentation</title>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <style>
        body { margin: 0; padding: 0; }
    </style>
</head>
<body>
    <script id="api-reference" data-url="/openapi.json"></script>
    <script src="https://cdn.jsdelivr.net/npm/@scalar/api-reference"></script>
</body>
</html>`))
	})

	// OpenAPI JSON endpoint
	router.Get("/openapi.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		spec := publicAPI.OpenAPI()
		if err := json.NewEncoder(w).Encode(spec); err != nil {
			http.Error(w, "Failed to generate OpenAPI spec", http.StatusInternalServerError)
			return
		}
	})

	srv := &http.Server{
		Addr:           fmt.Sprintf(":%s", cfg.Server.Port),
		Handler:        router,
		ReadTimeout:    15 * time.Second,
		WriteTimeout:   5 * time.Minute, // RAG queries with local LLM can take 2-3 minutes
		IdleTimeout:    60 * time.Second,
		MaxHeaderBytes: 1 << 20, // 1MB max header size
	}

	// Start background jobs for migrated modules
	if err := modRegistry.StartAll(context.Background()); err != nil {
		log.Fatalf("Failed to start modules: %v", err)
	}

	// Log development mode warning
	if !cfg.IsProduction() {
		utils.PrintDevelopmentWarning(cfg.Server.Environment)
	}

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

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("Shutting down server...")

	// Stop migrated modules
	modRegistry.StopAll(context.Background())

	ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("Failed to shutdown server gracefully", slog.String("error", err.Error()))
	}

	if err := database.DisconnectMongo(ctx, db); err != nil {
		logger.Error("Failed to disconnect from MongoDB", slog.String("error", err.Error()))
	}

	if err := database.DisconnectRedis(redisClient); err != nil {
		logger.Error("Failed to disconnect from Redis", slog.String("error", err.Error()))
	}

	logger.Info("Server stopped")
}



