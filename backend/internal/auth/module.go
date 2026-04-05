package auth

import (
	"context"

	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/orkestra/backend/internal/auth/handlers"
	"github.com/orkestra/backend/internal/auth/models"
	"github.com/orkestra/backend/internal/auth/repository"
	"github.com/orkestra/backend/internal/auth/services"
	"github.com/orkestra/backend/internal/shared/config"
	"github.com/orkestra/backend/internal/shared/module"
	userServices "github.com/orkestra/backend/internal/user/services"
)

type AuthModule struct {
	authHandler *handlers.AuthHandler
}

func NewModule() *AuthModule {
	return &AuthModule{}
}

func (m *AuthModule) Name() string { return "auth" }

func (m *AuthModule) Enabled(_ *config.Config) bool { return true }

func (m *AuthModule) Init(deps *module.Dependencies) error {
	cfg := deps.Config

	// Repositories
	authRepo, err := repository.NewAuthRepository(deps.DB)
	if err != nil {
		return err
	}
	oauthProviderRepo := repository.NewOAuthProviderRepository(deps.DB)
	refreshTokenRepo := repository.NewRefreshTokenRepository(deps.DB)
	authSessionRepo := repository.NewAuthSessionRepository(deps.DB)

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

	// JWT service
	jwtService := services.NewJWTService(cfg.Auth.JWT.PrivateKey, cfg.Auth.JWT.PublicKey)

	// OAuth state service
	redisStore := services.NewRedisOAuthStateStore(deps.RedisAdapter)
	oauthStateService := services.NewOAuthStateService(redisStore)

	// Get user service from registry
	userService := deps.Services.MustGet(module.ServiceUserService).(userServices.UserService)

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

	// Register services for main.go middleware setup
	deps.Services.Register(module.ServiceAuthService, authService)
	deps.Services.Register(module.ServiceJWTService, jwtService)

	return nil
}

func (m *AuthModule) RegisterRoutes(ri *module.RouteInfo) {
	// Auth has both public and protected routes
	protectedAPI := humachi.New(ri.ProtectedRouter, ri.APIConfig)
	m.authHandler.RegisterRoutes(ri.PublicAPI, protectedAPI, ri.Router, ri.ProtectedRouter)
}

func (m *AuthModule) Start(_ context.Context) error      { return nil }
func (m *AuthModule) Stop(_ context.Context) error       { return nil }
func (m *AuthModule) HealthCheck(_ context.Context) error { return nil }
