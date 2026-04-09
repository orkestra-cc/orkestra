package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/orkestra/backend/internal/core/auth/models"
	"github.com/golang-jwt/jwt/v5"
)

// googleOAuthService implements OAuthProviderInterface for Google OAuth 2.1
type googleOAuthService struct {
	config      *OAuthProviderConfig
	httpClient  *http.Client
	keysService GoogleKeysService
}

// NewGoogleOAuthService creates a new Google OAuth service with full OAuth 2.1 support
func NewGoogleOAuthService(config *OAuthProviderConfig) OAuthProviderInterface {
	return &googleOAuthService{
		config: config,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
		keysService: NewGoogleKeysService(),
	}
}

// Provider identification
func (s *googleOAuthService) GetProviderName() models.OAuthProvider {
	return models.OAuthProviderGoogle
}

func (s *googleOAuthService) GetClientID() string {
	return s.config.ClientID
}

// Web authentication flow (OAuth 2.1 with PKCE)
func (s *googleOAuthService) GetAuthURL(state string, codeChallenge string, redirectURI string) string {
	params := url.Values{}
	params.Set("client_id", s.config.ClientID)
	params.Set("redirect_uri", redirectURI)
	params.Set("response_type", "code")
	params.Set("scope", strings.Join(s.getDefaultScopes(), " "))
	params.Set("state", state)
	params.Set("access_type", "offline")
	params.Set("prompt", "consent")

	// OAuth 2.1 PKCE support
	if codeChallenge != "" {
		params.Set("code_challenge", codeChallenge)
		params.Set("code_challenge_method", "S256")
	}

	return fmt.Sprintf("https://accounts.google.com/o/oauth2/v2/auth?%s", params.Encode())
}

func (s *googleOAuthService) ExchangeCodeForToken(ctx context.Context, request *CodeExchangeRequest) (*TokenResponse, error) {
	tokenURL := "https://oauth2.googleapis.com/token"

	data := url.Values{}
	data.Set("code", request.Code)
	data.Set("client_id", s.config.ClientID)
	data.Set("client_secret", s.config.ClientSecret)
	data.Set("redirect_uri", request.RedirectURI)
	data.Set("grant_type", "authorization_code")

	// Add PKCE code verifier if provided
	if request.CodeVerifier != "" {
		data.Set("code_verifier", request.CodeVerifier)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, NewProviderError(models.OAuthProviderGoogle, "code_exchange", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, NewProviderError(models.OAuthProviderGoogle, "code_exchange", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, NewProviderError(models.OAuthProviderGoogle, "code_exchange", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, NewProviderError(models.OAuthProviderGoogle, "code_exchange", fmt.Errorf("HTTP %d", resp.StatusCode)).
			WithStatusCode(resp.StatusCode).
			WithProviderMessage(string(body))
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int64  `json:"expires_in"`
		Scope        string `json:"scope"`
	}

	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, NewProviderError(models.OAuthProviderGoogle, "code_exchange", err)
	}

	expiresAt := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	scopes := strings.Split(tokenResp.Scope, " ")

	return &TokenResponse{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		IDToken:      tokenResp.IDToken,
		TokenType:    tokenResp.TokenType,
		ExpiresIn:    tokenResp.ExpiresIn,
		ExpiresAt:    expiresAt,
		Scope:        scopes,
		IssuedAt:     time.Now(),
	}, nil
}

func (s *googleOAuthService) RefreshAccessToken(ctx context.Context, refreshToken string) (*TokenResponse, error) {
	tokenURL := "https://oauth2.googleapis.com/token"

	data := url.Values{}
	data.Set("refresh_token", refreshToken)
	data.Set("client_id", s.config.ClientID)
	data.Set("client_secret", s.config.ClientSecret)
	data.Set("grant_type", "refresh_token")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, NewProviderError(models.OAuthProviderGoogle, "token_refresh", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, NewProviderError(models.OAuthProviderGoogle, "token_refresh", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, NewProviderError(models.OAuthProviderGoogle, "token_refresh", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, NewProviderError(models.OAuthProviderGoogle, "token_refresh", fmt.Errorf("HTTP %d", resp.StatusCode)).
			WithStatusCode(resp.StatusCode).
			WithProviderMessage(string(body))
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int64  `json:"expires_in"`
		Scope       string `json:"scope"`
	}

	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, NewProviderError(models.OAuthProviderGoogle, "token_refresh", err)
	}

	expiresAt := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	scopes := strings.Split(tokenResp.Scope, " ")

	return &TokenResponse{
		AccessToken: tokenResp.AccessToken,
		TokenType:   tokenResp.TokenType,
		ExpiresIn:   tokenResp.ExpiresIn,
		ExpiresAt:   expiresAt,
		Scope:       scopes,
		IssuedAt:    time.Now(),
	}, nil
}

// Mobile authentication flow (ID token validation)
func (s *googleOAuthService) ValidateIDToken(ctx context.Context, request *IDTokenValidationRequest) (*UserInfo, error) {
	// Parse the JWT without verification to get the header
	token, _, err := jwt.NewParser().ParseUnverified(request.IDToken, jwt.MapClaims{})
	if err != nil {
		return nil, NewProviderError(models.OAuthProviderGoogle, "id_token_validation", ErrInvalidTokenFormat)
	}

	// Get the key ID from the header
	kid, ok := token.Header["kid"].(string)
	if !ok {
		return nil, NewProviderError(models.OAuthProviderGoogle, "id_token_validation", fmt.Errorf("missing kid in token header"))
	}

	// Get Google's public keys
	publicKey, err := s.getGooglePublicKey(ctx, kid)
	if err != nil {
		return nil, NewProviderError(models.OAuthProviderGoogle, "id_token_validation", err)
	}

	// Verify the token
	parsedToken, err := jwt.Parse(request.IDToken, func(token *jwt.Token) (interface{}, error) {
		// Verify signing method
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return publicKey, nil
	})

	if err != nil {
		// Token validation error - could be expired or malformed
		return nil, NewProviderError(models.OAuthProviderGoogle, "id_token_validation", fmt.Errorf("token validation failed: %w", err))
	}

	if !parsedToken.Valid {
		return nil, NewProviderError(models.OAuthProviderGoogle, "id_token_validation", ErrInvalidIDToken)
	}

	claims, ok := parsedToken.Claims.(jwt.MapClaims)
	if !ok {
		return nil, NewProviderError(models.OAuthProviderGoogle, "id_token_validation", fmt.Errorf("invalid claims format"))
	}

	// Validate audience if specified
	if request.Audience != "" {
		aud, _ := claims["aud"].(string)
		if aud != request.Audience {
			return nil, NewProviderError(models.OAuthProviderGoogle, "id_token_validation", fmt.Errorf("invalid audience"))
		}
	}

	// Extract user information
	userInfo := &UserInfo{
		ProviderID:    getStringClaim(claims, "sub"),
		Email:         getStringClaim(claims, "email"),
		EmailVerified: getBoolClaim(claims, "email_verified"),
		Name:          getStringClaim(claims, "name"),
		GivenName:     getStringClaim(claims, "given_name"),
		FamilyName:    getStringClaim(claims, "family_name"),
		Picture:       getStringClaim(claims, "picture"),
		Locale:        getStringClaim(claims, "locale"),
		AccessToken:   request.AccessToken,
		ProviderData:  make(map[string]interface{}),
	}

	// Store additional claims
	for key, value := range claims {
		if !isStandardClaim(key) {
			userInfo.ProviderData[key] = value
		}
	}

	return userInfo, nil
}

// User information retrieval
func (s *googleOAuthService) GetUserInfo(ctx context.Context, accessToken string) (*UserInfo, error) {
	userInfoURL := "https://www.googleapis.com/oauth2/v2/userinfo"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, userInfoURL, nil)
	if err != nil {
		return nil, NewProviderError(models.OAuthProviderGoogle, "user_info", err)
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, NewProviderError(models.OAuthProviderGoogle, "user_info", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, NewProviderError(models.OAuthProviderGoogle, "user_info", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, NewProviderError(models.OAuthProviderGoogle, "user_info", fmt.Errorf("HTTP %d", resp.StatusCode)).
			WithStatusCode(resp.StatusCode).
			WithProviderMessage(string(body))
	}

	var googleUser struct {
		ID            string `json:"id"`
		Email         string `json:"email"`
		VerifiedEmail bool   `json:"verified_email"`
		Name          string `json:"name"`
		GivenName     string `json:"given_name"`
		FamilyName    string `json:"family_name"`
		Picture       string `json:"picture"`
		Locale        string `json:"locale"`
	}

	if err := json.Unmarshal(body, &googleUser); err != nil {
		return nil, NewProviderError(models.OAuthProviderGoogle, "user_info", err)
	}

	return &UserInfo{
		ProviderID:    googleUser.ID,
		Email:         googleUser.Email,
		EmailVerified: googleUser.VerifiedEmail,
		Name:          googleUser.Name,
		GivenName:     googleUser.GivenName,
		FamilyName:    googleUser.FamilyName,
		Picture:       googleUser.Picture,
		Locale:        googleUser.Locale,
		AccessToken:   accessToken,
	}, nil
}

// Token management
func (s *googleOAuthService) RevokeToken(ctx context.Context, token string) error {
	revokeURL := "https://oauth2.googleapis.com/revoke"

	data := url.Values{}
	data.Set("token", token)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, revokeURL, strings.NewReader(data.Encode()))
	if err != nil {
		return NewProviderError(models.OAuthProviderGoogle, "token_revoke", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return NewProviderError(models.OAuthProviderGoogle, "token_revoke", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return NewProviderError(models.OAuthProviderGoogle, "token_revoke", fmt.Errorf("HTTP %d", resp.StatusCode)).
			WithStatusCode(resp.StatusCode).
			WithProviderMessage(string(body))
	}

	return nil
}

// Provider capabilities
func (s *googleOAuthService) GetSupportedScopes() []string {
	return []string{
		"openid",
		"email",
		"profile",
		"https://www.googleapis.com/auth/userinfo.email",
		"https://www.googleapis.com/auth/userinfo.profile",
	}
}

func (s *googleOAuthService) GetSupportedGrantTypes() []string {
	return []string{
		"authorization_code",
		"refresh_token",
	}
}

func (s *googleOAuthService) SupportsRefreshTokens() bool {
	return true
}

func (s *googleOAuthService) SupportsMobileFlow() bool {
	return true
}

// Helper methods

func (s *googleOAuthService) getDefaultScopes() []string {
	if len(s.config.Scopes) > 0 {
		return s.config.Scopes
	}
	return []string{"openid", "email", "profile"}
}

func (s *googleOAuthService) getGooglePublicKey(ctx context.Context, kid string) (interface{}, error) {
	// Use the Google keys service to fetch and cache public keys
	return s.keysService.GetPublicKey(ctx, kid)
}

// Helper functions for JWT claims
func getBoolClaim(claims jwt.MapClaims, key string) bool {
	if val, ok := claims[key].(bool); ok {
		return val
	}
	return false
}

func isStandardClaim(key string) bool {
	standardClaims := map[string]bool{
		"iss": true, "sub": true, "aud": true, "exp": true, "nbf": true, "iat": true, "jti": true,
		"email": true, "email_verified": true, "name": true, "given_name": true, "family_name": true,
		"picture": true, "locale": true,
	}
	return standardClaims[key]
}

// Note: PKCE generation moved to utils package and is properly implemented with SHA256
