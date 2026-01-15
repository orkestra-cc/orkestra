package services

import (
	"context"
	"fmt"

	"github.com/orkestra/backend/internal/auth/models"
)

// oauthProviderFactory implements OAuthProviderFactory
type oauthProviderFactory struct {
	configs     map[models.OAuthProvider]*OAuthProviderConfig
	redisClient RedisClient
}

// NewOAuthProviderFactory creates a new OAuth provider factory
func NewOAuthProviderFactory(configs map[models.OAuthProvider]*OAuthProviderConfig, redisClient RedisClient) OAuthProviderFactory {
	return &oauthProviderFactory{
		configs:     configs,
		redisClient: redisClient,
	}
}

func (f *oauthProviderFactory) CreateProvider(provider models.OAuthProvider, config *OAuthProviderConfig) (OAuthProviderInterface, error) {
	// Use provided config or fall back to factory config
	providerConfig := config
	if providerConfig == nil {
		if factoryConfig, exists := f.configs[provider]; exists {
			providerConfig = factoryConfig
		} else {
			return nil, fmt.Errorf("%w: %s", ErrProviderNotSupported, provider)
		}
	}

	switch provider {
	case models.OAuthProviderGoogle:
		return NewGoogleOAuthService(providerConfig), nil
	case models.OAuthProviderApple:
		provider, err := NewAppleOAuthService(providerConfig, f.redisClient)
		if err != nil {
			return nil, err
		}
		return provider, nil
	case models.OAuthProviderGitHub:
		return NewGitHubOAuthService(providerConfig), nil
	case models.OAuthProviderDiscord:
		return NewDiscordOAuthService(providerConfig), nil
	default:
		return nil, fmt.Errorf("%w: %s", ErrProviderNotSupported, provider)
	}
}

func (f *oauthProviderFactory) GetSupportedProviders() []models.OAuthProvider {
	return []models.OAuthProvider{
		models.OAuthProviderGoogle,
		models.OAuthProviderApple,
		models.OAuthProviderGitHub,
		models.OAuthProviderDiscord,
	}
}

// Helper function to create provider configs from environment
func CreateProviderConfigsFromEnv() map[models.OAuthProvider]*OAuthProviderConfig {
	// This would typically read from environment variables or config file
	// For now, returning empty map - should be implemented based on your config system
	return make(map[models.OAuthProvider]*OAuthProviderConfig)
}

// Provider manager for centralized OAuth operations
type OAuthProviderManager struct {
	factory   OAuthProviderFactory
	providers map[models.OAuthProvider]OAuthProviderInterface
}

func NewOAuthProviderManager(factory OAuthProviderFactory) *OAuthProviderManager {
	return &OAuthProviderManager{
		factory:   factory,
		providers: make(map[models.OAuthProvider]OAuthProviderInterface),
	}
}

func (m *OAuthProviderManager) GetProvider(provider models.OAuthProvider) (OAuthProviderInterface, error) {
	// Check if provider is already cached
	if p, exists := m.providers[provider]; exists {
		return p, nil
	}

	// Create new provider instance
	p, err := m.factory.CreateProvider(provider, nil)
	if err != nil {
		return nil, err
	}

	// Cache for future use
	m.providers[provider] = p
	return p, nil
}

func (m *OAuthProviderManager) GetAllProviders() []models.OAuthProvider {
	return m.factory.GetSupportedProviders()
}

func (m *OAuthProviderManager) SupportsProvider(provider models.OAuthProvider) bool {
	supportedProviders := m.factory.GetSupportedProviders()
	for _, supported := range supportedProviders {
		if supported == provider {
			return true
		}
	}
	return false
}

// OAuth flow utilities
func (m *OAuthProviderManager) GetWebAuthURL(provider models.OAuthProvider, state, codeChallenge, redirectURI string) (string, error) {
	p, err := m.GetProvider(provider)
	if err != nil {
		return "", err
	}
	return p.GetAuthURL(state, codeChallenge, redirectURI), nil
}

func (m *OAuthProviderManager) HandleWebCallback(provider models.OAuthProvider, request *CodeExchangeRequest) (*TokenResponse, *UserInfo, error) {
	p, err := m.GetProvider(provider)
	if err != nil {
		return nil, nil, err
	}

	// Exchange code for tokens
	tokens, err := p.ExchangeCodeForToken(ctx, request)
	if err != nil {
		return nil, nil, err
	}

	// Get user info
	userInfo, err := p.GetUserInfo(ctx, tokens.AccessToken)
	if err != nil {
		return tokens, nil, err // Return tokens even if user info fails
	}

	return tokens, userInfo, nil
}

func (m *OAuthProviderManager) HandleMobileAuth(provider models.OAuthProvider, request *IDTokenValidationRequest) (*UserInfo, error) {
	p, err := m.GetProvider(provider)
	if err != nil {
		return nil, err
	}

	if !p.SupportsMobileFlow() {
		return nil, NewProviderError(provider, "mobile_auth", ErrMobileFlowNotSupported)
	}

	return p.ValidateIDToken(ctx, request)
}

// Global context placeholder - in real implementation, this would come from the request context
var ctx = context.Background()
