package services

import (
	"context"
	"fmt"
	"time"

	"github.com/orkestra/backend/internal/core/auth/models"
)

// OAuthProviderInterface defines the standard interface for all OAuth providers
// Supporting both web (authorization code) and mobile (ID token) flows
type OAuthProviderInterface interface {
	// Provider identification
	GetProviderName() models.OAuthProvider
	GetClientID() string

	// Web authentication flow (OAuth 2.1 with PKCE)
	GetAuthURL(state string, codeChallenge string, redirectURI string) string
	ExchangeCodeForToken(ctx context.Context, request *CodeExchangeRequest) (*TokenResponse, error)
	RefreshAccessToken(ctx context.Context, refreshToken string) (*TokenResponse, error)

	// Mobile authentication flow (ID token validation)
	ValidateIDToken(ctx context.Context, request *IDTokenValidationRequest) (*UserInfo, error)

	// User information retrieval
	GetUserInfo(ctx context.Context, accessToken string) (*UserInfo, error)

	// Token management
	RevokeToken(ctx context.Context, token string) error

	// Provider capabilities
	GetSupportedScopes() []string
	GetSupportedGrantTypes() []string
	SupportsRefreshTokens() bool
	SupportsMobileFlow() bool
}

// CodeExchangeRequest represents a request to exchange authorization code for tokens (web flow)
type CodeExchangeRequest struct {
	Code         string `json:"code" validate:"required"`
	RedirectURI  string `json:"redirectUri" validate:"required"`
	CodeVerifier string `json:"codeVerifier,omitempty"` // For PKCE
	State        string `json:"state,omitempty"`
}

// IDTokenValidationRequest represents a request to validate ID token (mobile flow)
type IDTokenValidationRequest struct {
	IDToken     string                 `json:"idToken" validate:"required"`
	AccessToken string                 `json:"accessToken,omitempty"`
	Audience    string                 `json:"audience,omitempty"`
	DeviceInfo  *models.DeviceInfo     `json:"deviceInfo,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// TokenResponse represents the response from token exchange or refresh
type TokenResponse struct {
	AccessToken  string    `json:"accessToken"`
	RefreshToken string    `json:"refreshToken,omitempty"`
	IDToken      string    `json:"idToken,omitempty"`
	TokenType    string    `json:"tokenType"`
	ExpiresIn    int64     `json:"expiresIn"` // Seconds
	ExpiresAt    time.Time `json:"expiresAt"`
	Scope        []string  `json:"scope,omitempty"`
	IssuedAt     time.Time `json:"issuedAt"`
}

// UserInfo represents standardized user information from any OAuth provider
type UserInfo struct {
	// Standard fields (required)
	ProviderID    string `json:"providerId"` // Unique ID from the provider
	Email         string `json:"email"`
	EmailVerified bool   `json:"emailVerified"`

	// Profile information (optional)
	Name       string `json:"name,omitempty"`
	GivenName  string `json:"givenName,omitempty"`
	FamilyName string `json:"familyName,omitempty"`
	Picture    string `json:"picture,omitempty"`
	Locale     string `json:"locale,omitempty"`

	// Provider-specific data
	ProviderData map[string]interface{} `json:"providerData,omitempty"`

	// Token information
	AccessToken  string    `json:"accessToken,omitempty"`
	RefreshToken string    `json:"refreshToken,omitempty"`
	TokenExpiry  time.Time `json:"tokenExpiry,omitempty"`
	Scopes       []string  `json:"scopes,omitempty"`
}

// OAuthProviderFactory creates OAuth provider instances
type OAuthProviderFactory interface {
	CreateProvider(provider models.OAuthProvider, config *OAuthProviderConfig) (OAuthProviderInterface, error)
	GetSupportedProviders() []models.OAuthProvider
}

// OAuthProviderConfig contains configuration for OAuth providers
type OAuthProviderConfig struct {
	ClientID         string            `json:"clientId"`
	ClientSecret     string            `json:"clientSecret"`
	RedirectURI      string            `json:"redirectUri"`
	MobileConfig     *MobileConfig     `json:"mobileConfig,omitempty"`
	Scopes           []string          `json:"scopes,omitempty"`
	AdditionalConfig map[string]string `json:"additionalConfig,omitempty"`
}

// MobileConfig contains mobile-specific OAuth configuration
type MobileConfig struct {
	IOSClientID     string `json:"iosClientId,omitempty"`
	AndroidClientID string `json:"androidClientId,omitempty"`
	BundleID        string `json:"bundleId,omitempty"`    // iOS bundle identifier
	PackageName     string `json:"packageName,omitempty"` // Android package name
	AppScheme       string `json:"appScheme,omitempty"`   // Custom URL scheme
}

// OAuth provider errors
var (
	ErrProviderNotSupported   = fmt.Errorf("OAuth provider not supported")
	ErrInvalidAuthCode        = fmt.Errorf("invalid authorization code")
	ErrInvalidIDToken         = fmt.Errorf("invalid ID token")
	ErrInvalidTokenFormat     = fmt.Errorf("invalid token format")
	ErrProviderAPIError       = fmt.Errorf("provider API error")
	ErrUserInfoNotAvailable   = fmt.Errorf("user information not available")
	ErrRefreshNotSupported    = fmt.Errorf("refresh tokens not supported by provider")
	ErrMobileFlowNotSupported = fmt.Errorf("mobile flow not supported by provider")
)

// ProviderError wraps provider-specific errors with additional context
type ProviderError struct {
	Provider    models.OAuthProvider
	Operation   string
	Err         error
	StatusCode  int
	ProviderMsg string
	Details     map[string]interface{}
}

func (e *ProviderError) Error() string {
	return fmt.Sprintf("OAuth %s error in %s: %v", e.Provider, e.Operation, e.Err)
}

func (e *ProviderError) Unwrap() error {
	return e.Err
}

// NewProviderError creates a new provider error with context
func NewProviderError(provider models.OAuthProvider, operation string, err error) *ProviderError {
	return &ProviderError{
		Provider:  provider,
		Operation: operation,
		Err:       err,
	}
}

// WithStatusCode adds HTTP status code to the error
func (e *ProviderError) WithStatusCode(code int) *ProviderError {
	e.StatusCode = code
	return e
}

// WithProviderMessage adds provider-specific error message
func (e *ProviderError) WithProviderMessage(msg string) *ProviderError {
	e.ProviderMsg = msg
	return e
}

// WithDetails adds additional error details
func (e *ProviderError) WithDetails(details map[string]interface{}) *ProviderError {
	e.Details = details
	return e
}

// OAuthFlowType represents the type of OAuth flow being used
type OAuthFlowType string

const (
	WebFlow    OAuthFlowType = "web"
	MobileFlow OAuthFlowType = "mobile"
)

// FlowContext contains context information about the OAuth flow
type FlowContext struct {
	Type       OAuthFlowType          `json:"type"`
	DeviceInfo *models.DeviceInfo     `json:"deviceInfo,omitempty"`
	IPAddress  string                 `json:"ipAddress,omitempty"`
	UserAgent  string                 `json:"userAgent,omitempty"`
	RequestID  string                 `json:"requestId,omitempty"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}
