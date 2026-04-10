package auth

import (
	"log/slog"
	"os"
	"time"

	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/orkestra/backend/internal/core/auth/handlers"
	"github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/repository"
	"github.com/orkestra/backend/internal/core/auth/services"
	sharederrors "github.com/orkestra/backend/internal/shared/errors"
	"github.com/orkestra/backend/internal/shared/iface"
	"github.com/orkestra/backend/internal/shared/module"
)

type AuthModule struct {
	module.BaseModule
	authHandler     *handlers.AuthHandler
	passwordHandler *handlers.PasswordAuthHandler
}

func NewModule() *AuthModule { return &AuthModule{} }

func (m *AuthModule) Name() string        { return "auth" }
func (m *AuthModule) DisplayName() string  { return "Authentication" }
func (m *AuthModule) Description() string  { return "OAuth 2.1, JWT, sessions, RBAC" }

func (m *AuthModule) Dependencies() []string { return []string{"user", "notification", "tenant", "authz"} }
func (m *AuthModule) RequiredServices() []module.ServiceKey {
	return []module.ServiceKey{module.ServiceUserService, module.ServiceTenantProvider}
}
func (m *AuthModule) OptionalServices() []module.ServiceKey {
	return []module.ServiceKey{module.ServiceNotificationSender}
}
func (m *AuthModule) ProvidedServices() []module.ServiceKey {
	return []module.ServiceKey{
		module.ServiceAuthService,
		module.ServiceJWTService,
		module.ServicePasswordService,
		module.ServicePasswordAuthService,
	}
}

func (m *AuthModule) Permissions() []iface.PermissionSpec {
	return []iface.PermissionSpec{
		{Key: "auth.self", Module: "auth", Description: "Edit your own password and sessions"},
	}
}

func (m *AuthModule) Collections() []module.CollectionSpec {
	return []module.CollectionSpec{
		{Name: models.OAuthProvidersCollection, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"userUuid": 1, "provider": 1}, Unique: true},
		}},
		{Name: models.RefreshTokensCollection, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"uuid": 1}, Unique: true},
			{Keys: map[string]int{"userUuid": 1}},
		}},
		{Name: models.AuthSessionsCollection, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"uuid": 1}, Unique: true},
		}},
		{Name: models.SecurityEventsCollection},
		{Name: models.EmailTokensCollection, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"uuid": 1}, Unique: true},
			{Keys: map[string]int{"tokenHash": 1}, Unique: true},
			{Keys: map[string]int{"userUuid": 1}},
			// TTL: documents are removed 24h after ExpiresAt (max TTL for both verify and reset).
			{Keys: map[string]int{"expiresAt": 1}, TTL: 24 * time.Hour},
		}},
	}
}

func (m *AuthModule) Init(deps *module.Dependencies) error {
	cfg := deps.Config
	logger := deps.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}

	// Repositories
	authRepo, err := repository.NewAuthRepository(deps.DB)
	if err != nil {
		return err
	}
	oauthProviderRepo := repository.NewOAuthProviderRepository(deps.DB)
	refreshTokenRepo := repository.NewRefreshTokenRepository(deps.DB)
	authSessionRepo := repository.NewAuthSessionRepository(deps.DB)
	emailTokenRepo := repository.NewEmailTokenRepository(deps.DB)

	// OAuth provider factory
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
			ClientSecret: "",
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

	providerFactory := services.NewOAuthProviderFactory(providerConfigs, deps.RedisAdapter)

	// JWT service. The tenant provider is wired in so that token issuance
	// can embed the user's current memberships in the JWT.
	jwtService := services.NewJWTService(cfg.Auth.JWT.PrivateKey, cfg.Auth.JWT.PublicKey)
	tenantProvider := module.MustGetTyped[iface.TenantProvider](deps.Services, module.ServiceTenantProvider)
	jwtService.SetTenantProvider(tenantProvider)

	// OAuth state service
	redisStore := services.NewRedisOAuthStateStore(deps.RedisAdapter)
	oauthStateService := services.NewOAuthStateService(redisStore)

	// Get user service from registry
	userService := module.MustGetTyped[iface.UserProvider](deps.Services, module.ServiceUserService)

	// Auth service
	authService, err := services.NewAuthService(&services.AuthConfig{
		AuthRepo:          authRepo,
		UserService:       userService,
		OAuthProviderRepo: oauthProviderRepo,
		RefreshTokenRepo:  refreshTokenRepo,
		AuthSessionRepo:   authSessionRepo,
		JWTService:        jwtService,
	})
	if err != nil {
		return err
	}

	// Auth handler
	m.authHandler = handlers.NewAuthHandler(
		authService,
		providerFactory,
		oauthStateService,
		oauthProviderRepo,
		jwtService,
		cfg,
	)

	// Password auth service — depends on notification module (optional).
	passwordSvc := services.NewPasswordService(logger, true)
	var notifier iface.NotificationSender
	if n, ok := module.GetTyped[iface.NotificationSender](deps.Services, module.ServiceNotificationSender); ok {
		notifier = n
	}

	rateLimiter := sharederrors.NewRateLimiter()
	passwordAuthSvc := services.NewPasswordAuthService(services.PasswordAuthConfig{
		UserService:              userService,
		PasswordService:          passwordSvc,
		JWTService:               jwtService,
		EmailTokenRepo:           emailTokenRepo,
		RefreshTokenRepo:         refreshTokenRepo,
		AuthSessionRepo:          authSessionRepo,
		Notifier:                 notifier,
		RateLimiter:              rateLimiter,
		FrontendURL:              cfg.Server.FrontendURL,
		RequireEmailVerification: getBoolEnv("AUTH_REQUIRE_EMAIL_VERIFICATION", cfg.IsProductionLike()),
		AppName:                  getEnvOrDefault("APP_NAME", "Orkestra"),
		SupportEmail:             os.Getenv("SUPPORT_EMAIL"),
		Logger:                   logger,
	})

	m.passwordHandler = handlers.NewPasswordAuthHandler(
		passwordAuthSvc,
		cfg.Auth.Cookie.Name,
		cfg.Auth.Cookie.Domain,
		cfg.Auth.Cookie.Secure,
	)

	// Register services for main.go middleware setup
	deps.Services.Register(module.ServiceAuthService, authService)
	deps.Services.Register(module.ServiceJWTService, jwtService)
	deps.Services.Register(module.ServicePasswordService, passwordSvc)
	deps.Services.Register(module.ServicePasswordAuthService, passwordAuthSvc)

	return nil
}

func getBoolEnv(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	return v == "true" || v == "1" || v == "yes"
}

func getEnvOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func (m *AuthModule) RegisterRoutes(ri *module.RouteInfo) {
	// Auth has both public and protected routes
	protectedAPI := humachi.New(ri.ProtectedRouter, ri.APIConfig)
	m.authHandler.RegisterRoutes(ri.PublicAPI, protectedAPI, ri.Router, ri.ProtectedRouter)

	// Password auth endpoints: register/login/verify/reset/forgot live on the
	// public API; change-password is protected and runs without an org
	// context (it's a user self-service flow).
	if m.passwordHandler != nil {
		m.passwordHandler.RegisterPublicRoutes(ri.PublicAPI)
		ri.ProtectedRouter.Group(func(r chi.Router) {
			r.Use(ri.AuthMW.RequireGlobal())
			api := humachi.New(r, ri.APIConfig)
			m.passwordHandler.RegisterProtectedRoutes(api)
		})
	}
}

