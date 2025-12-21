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
	"syscall"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/orkestra/backend/internal/auth/handlers"
	"github.com/orkestra/backend/internal/auth/models"
	"github.com/orkestra/backend/internal/auth/repository"
	"github.com/orkestra/backend/internal/auth/services"
	reportingHandlers "github.com/orkestra/backend/internal/reporting/handlers"
	reportingRepository "github.com/orkestra/backend/internal/reporting/repository"
	reportingServices "github.com/orkestra/backend/internal/reporting/services"
	"github.com/orkestra/backend/internal/shared/config"
	"github.com/orkestra/backend/internal/shared/database"
	"github.com/orkestra/backend/internal/shared/errors"
	authMiddleware "github.com/orkestra/backend/internal/shared/middleware"
	userHandlers "github.com/orkestra/backend/internal/user/handlers"
	userRepository "github.com/orkestra/backend/internal/user/repository"
	userServices "github.com/orkestra/backend/internal/user/services"
	navigationConfig "github.com/orkestra/backend/internal/navigation/config"
	navigationHandlers "github.com/orkestra/backend/internal/navigation/handlers"
	navigationServices "github.com/orkestra/backend/internal/navigation/services"
	"github.com/redis/go-redis/v9"
)

// redisOAuthStateStore implements services.OAuthStateStore interface
type redisOAuthStateStore struct {
	client *redis.Client
}

func (r *redisOAuthStateStore) Set(ctx context.Context, key string, value []byte, expiry time.Duration) error {
	return r.client.Set(ctx, key, value, expiry).Err()
}

func (r *redisOAuthStateStore) Get(ctx context.Context, key string) ([]byte, error) {
	result, err := r.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, fmt.Errorf("key not found")
	}
	return result, err
}

func (r *redisOAuthStateStore) Delete(ctx context.Context, key string) error {
	return r.client.Del(ctx, key).Err()
}

func (r *redisOAuthStateStore) DeleteByPattern(ctx context.Context, pattern string) error {
	iter := r.client.Scan(ctx, 0, pattern, 0).Iterator()
	var keys []string
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}
	if err := iter.Err(); err != nil {
		return err
	}
	if len(keys) > 0 {
		return r.client.Del(ctx, keys...).Err()
	}
	return nil
}

func main() {
	logger := setupLogger()
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

	// Initialize new enhanced repositories
	authRepo, err := repository.NewAuthRepository(db)
	if err != nil {
		log.Fatalf("Failed to create auth repository: %v", err)
	}
	oauthProviderRepo := repository.NewOAuthProviderRepository(db)

	refreshTokenRepo := repository.NewRefreshTokenRepository(db)
	authSessionRepo := repository.NewAuthSessionRepository(db)

	// Initialize OAuth provider factory with configurations
	providerConfigs := map[models.OAuthProvider]*services.OAuthProviderConfig{
		models.OAuthProviderGoogle: {
			ClientID:     cfg.Auth.Google.ClientID,
			ClientSecret: cfg.Auth.Google.ClientSecret,
			Scopes:       []string{"openid", "email", "profile"},
			AdditionalConfig: map[string]string{
				"android_client_id": cfg.Auth.Google.AndroidClientID,
				"ios_client_id":     cfg.Auth.Google.IOSClientID,
			},
		},
		models.OAuthProviderApple: {
			ClientID:     cfg.Auth.Apple.ClientID,
			ClientSecret: "", // JWT-based for Apple
			Scopes:       []string{"name", "email"},
			AdditionalConfig: map[string]string{
				"team_id":          cfg.Auth.Apple.TeamID,
				"key_id":           cfg.Auth.Apple.KeyID,
				"private_key":      cfg.Auth.Apple.PrivateKey,
				"private_key_path": cfg.Auth.Apple.PrivateKeyPath,
				"redirect_url":     cfg.Auth.Apple.RedirectURL,
			},
		},
		models.OAuthProviderGitHub: {
			ClientID:     cfg.Auth.GitHub.ClientID,
			ClientSecret: cfg.Auth.GitHub.ClientSecret,
			Scopes:       []string{"user:email", "read:user"},
			AdditionalConfig: map[string]string{
				"redirect_url": cfg.Auth.GitHub.RedirectURL,
			},
		},
		models.OAuthProviderDiscord: {
			ClientID:     cfg.Auth.Discord.ClientID,
			ClientSecret: cfg.Auth.Discord.ClientSecret,
			Scopes:       []string{"identify", "email"},
			AdditionalConfig: map[string]string{
				"redirect_url": cfg.Auth.Discord.RedirectURL,
			},
		},
	}

	redisClientAdapter := database.NewRedisClientAdapter(redisClient)
	providerFactory := services.NewOAuthProviderFactory(providerConfigs, redisClientAdapter)

	// Initialize OAuth provider manager
	providerManager := services.NewOAuthProviderManager(providerFactory)
	_ = providerManager // TODO: Use in auth handlers

	// Initialize JWT services
	jwtService := services.NewJWTService(cfg.Auth.JWT.PrivateKey, cfg.Auth.JWT.PublicKey)

	// Initialize OAuth state service with Redis adapter
	redisStore := &redisOAuthStateStore{client: redisClient}
	oauthStateService := services.NewOAuthStateService(redisStore)

	// Initialize user management module first (required by auth service)
	userRepo := userRepository.NewUserRepository(db)
	userService := userServices.NewUserService(userRepo, oauthProviderRepo)
	userHandler := userHandlers.NewUserHandler(userService)

	// Initialize reporting module
	reportingRepo := reportingRepository.NewDeadlineRepository(db)
	reportingService := reportingServices.NewDeadlineService(reportingRepo)
	reportingHandler := reportingHandlers.NewDeadlineHandler(reportingService)

	// Initialize navigation module
	menuConfig := navigationConfig.NewMenuConfig()
	navigationService := navigationServices.NewNavigationService(menuConfig)
	navigationHandler := navigationHandlers.NewNavigationHandler(navigationService)

	// Initialize auth service with all repositories
	authService, err := services.NewAuthService(&services.AuthConfig{
		AuthRepo:          authRepo,
		UserService:       userService,
		OAuthProviderRepo: oauthProviderRepo,
		RefreshTokenRepo:  refreshTokenRepo,
		AuthSessionRepo:   authSessionRepo,
		JWTService:        jwtService,
	})
	if err != nil {
		log.Fatalf("Failed to create enhanced auth service: %v", err)
	}

	// Initialize risk assessment service
	riskService := services.NewRiskAssessmentService(nil, nil, nil)
	_ = riskService // TODO: Use in auth handlers

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

	// CORS middleware - must be before other middleware
	router.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"http://localhost:8080", "http://192.168.88.53:8080", "http://localhost:5173"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token", "X-Request-ID"},
		ExposedHeaders:   []string{"Link", "X-Total-Count", "X-Ratelimit-Limit", "X-Ratelimit-Remaining"},
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
	router.Use(middleware.Timeout(60 * time.Second))

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
	protectedAPI := humachi.New(protectedRouter, apiConfig)

	// Initialize and register authentication handler
	authHandler := handlers.NewAuthHandler(
		authService,
		providerFactory,
		oauthStateService,
		oauthProviderRepo,
		jwtService,
		cfg,
	)
	authHandler.RegisterRoutes(publicAPI, protectedAPI, router, protectedRouter)

	// Create user management routes with role-based protection within protected router
	// Only administrator, ceo, and developer roles should have access
	protectedRouter.Group(func(r chi.Router) {
		r.Use(authMiddlewareHandler.RequireHierarchicalRole("administrator"))
		userAPI := humachi.New(r, apiConfig)
		// Register user management routes (protected with role restrictions)
		registerUserRoutes(userAPI, userHandler)
	})

	// Create reporting routes with role-based protection
	// Only manager, administrator, ceo, and developer roles should have access
	protectedRouter.Group(func(r chi.Router) {
		r.Use(authMiddlewareHandler.RequireHierarchicalRole("manager"))
		reportingAPI := humachi.New(r, apiConfig)
		// Register reporting routes (protected with role restrictions)
		registerReportRoutes(reportingAPI, reportingHandler)
	})

	// Navigation routes - accessible to all authenticated users
	// Role filtering happens inside the service based on user's role
	navigationAPI := humachi.New(protectedRouter, apiConfig)
	registerNavigationRoutes(navigationAPI, navigationHandler)

	// Mount the protected routes
	router.Mount("/", protectedRouter)

	// Then register other protected routes with middleware
	router.Group(func(r chi.Router) {
		r.Use(authMiddlewareHandler.RequireAuth)
		// TODO: Add other protected routes here as needed
	})

	router.Route("/protected", func(r chi.Router) {
		r.Use(authMiddlewareHandler.RequireAuth)
		r.Get("/", func(w http.ResponseWriter, req *http.Request) {
			userID := req.Context().Value("userID").(string)
			w.Write([]byte(fmt.Sprintf("Protected endpoint accessed by user: %s", userID)))
		})
	})

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
		Addr:         fmt.Sprintf(":%s", cfg.Server.Port),
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
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

func setupLogger() *slog.Logger {
	opts := slog.HandlerOptions{
		Level: slog.LevelInfo,
	}

	var handler slog.Handler
	env := os.Getenv("ENV")

	// Use JSON logging for production-like environments (production, staging)
	if env == "production" || env == "staging" {
		handler = slog.NewJSONHandler(os.Stdout, &opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, &opts)
	}

	logger := slog.New(handler)

	return logger.With(
		slog.String("service", "orkestra-backend"),
		slog.String("version", "1.0.0"),
		slog.String("environment", env),
	)
}

// registerUserRoutes registers all user management routes
func registerUserRoutes(api huma.API, userHandler *userHandlers.UserHandler) {
	// Core CRUD operations
	huma.Register(api, huma.Operation{
		OperationID: "create-user",
		Method:      http.MethodPost,
		Path:        "/api/v1/users",
		Summary:     "Create a new user",
		Description: "Creates a new user with the provided information",
		Tags:        []string{"Users"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, userHandler.CreateUser)

	huma.Register(api, huma.Operation{
		OperationID: "get-user",
		Method:      http.MethodGet,
		Path:        "/api/v1/users/{id}",
		Summary:     "Get user by ID",
		Description: "Retrieves a user by their UUID",
		Tags:        []string{"Users"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, userHandler.GetUser)

	huma.Register(api, huma.Operation{
		OperationID: "update-user",
		Method:      http.MethodPut,
		Path:        "/api/v1/users/{id}",
		Summary:     "Update user",
		Description: "Updates a user's information",
		Tags:        []string{"Users"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, userHandler.UpdateUser)

	huma.Register(api, huma.Operation{
		OperationID: "delete-user",
		Method:      http.MethodDelete,
		Path:        "/api/v1/users/{id}",
		Summary:     "Delete user",
		Description: "Soft deletes a user",
		Tags:        []string{"Users"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, userHandler.DeleteUser)

	huma.Register(api, huma.Operation{
		OperationID: "list-users",
		Method:      http.MethodGet,
		Path:        "/api/v1/users",
		Summary:     "List users",
		Description: "Retrieves a paginated list of users with optional filtering",
		Tags:        []string{"Users"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, userHandler.ListUsers)

	// Query operations
	huma.Register(api, huma.Operation{
		OperationID: "get-users-by-role",
		Method:      http.MethodGet,
		Path:        "/api/v1/users/role/{role}",
		Summary:     "Get users by role",
		Description: "Retrieves all users with a specific role",
		Tags:        []string{"Users"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, userHandler.GetUsersByRole)

	huma.Register(api, huma.Operation{
		OperationID: "get-user-by-email",
		Method:      http.MethodGet,
		Path:        "/api/v1/users/by-email",
		Summary:     "Get user by email",
		Description: "Retrieves a user by their email address",
		Tags:        []string{"Users"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, userHandler.GetUserByEmail)

	huma.Register(api, huma.Operation{
		OperationID: "get-user-count",
		Method:      http.MethodGet,
		Path:        "/api/v1/users/count",
		Summary:     "Get user count",
		Description: "Returns the total count of users with optional filtering",
		Tags:        []string{"Users"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, userHandler.GetUserCount)

	// Document management operations
	huma.Register(api, huma.Operation{
		OperationID: "get-users-with-expired-documents",
		Method:      http.MethodGet,
		Path:        "/api/v1/users/expired-documents",
		Summary:     "Get users with expired documents",
		Description: "Retrieves users who have expired driver documents",
		Tags:        []string{"Users", "Documents"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, userHandler.GetUsersWithExpiredDocuments)

	huma.Register(api, huma.Operation{
		OperationID: "get-users-with-expiring-soon-documents",
		Method:      http.MethodGet,
		Path:        "/api/v1/users/expiring-soon-documents",
		Summary:     "Get users with documents expiring soon",
		Description: "Retrieves users who have driver documents expiring within the specified number of days",
		Tags:        []string{"Users", "Documents"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, userHandler.GetUsersWithExpiringSoonDocuments)

	huma.Register(api, huma.Operation{
		OperationID: "update-user-documents",
		Method:      http.MethodPatch,
		Path:        "/api/v1/users/{id}/documents",
		Summary:     "Update user documents",
		Description: "Updates only the document-related fields for a user",
		Tags:        []string{"Users", "Documents"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, userHandler.UpdateUserDocuments)

	huma.Register(api, huma.Operation{
		OperationID: "check-user-document-expiry",
		Method:      http.MethodGet,
		Path:        "/api/v1/users/{id}/check-expiry",
		Summary:     "Check document expiry",
		Description: "Checks which documents are expired for a specific user",
		Tags:        []string{"Users", "Documents"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, userHandler.CheckDocumentExpiry)
}

// registerReportRoutes registers all reporting routes
func registerReportRoutes(api huma.API, deadlineHandler *reportingHandlers.DeadlineHandler) {
	// Deadline reports
	huma.Register(api, huma.Operation{
		OperationID: "get-deadline-report",
		Method:      http.MethodGet,
		Path:        "/api/v1/reports/deadlines",
		Summary:     "Get deadline report",
		Description: "Retrieves all items with expiry dates (users, certifications)",
		Tags:        []string{"Reports"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, deadlineHandler.GetDeadlines)
}

// registerNavigationRoutes registers the navigation menu route
func registerNavigationRoutes(api huma.API, navigationHandler *navigationHandlers.NavigationHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "get-navigation",
		Method:      http.MethodGet,
		Path:        "/api/v1/navigation",
		Summary:     "Get navigation menu",
		Description: "Returns role-filtered navigation menu for the current authenticated user",
		Tags:        []string{"Navigation"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, navigationHandler.GetNavigation)
}
