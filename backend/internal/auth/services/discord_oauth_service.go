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

	"github.com/orkestra/backend/internal/auth/models"
)

// discordOAuthService implements OAuthProviderInterface for Discord OAuth 2.0
type discordOAuthService struct {
	config     *OAuthProviderConfig
	httpClient *http.Client
}

// NewDiscordOAuthService creates a new Discord OAuth service with full OAuth 2.1 support
func NewDiscordOAuthService(config *OAuthProviderConfig) OAuthProviderInterface {
	return &discordOAuthService{
		config: config,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// Provider identification
func (s *discordOAuthService) GetProviderName() models.OAuthProvider {
	return models.OAuthProviderDiscord
}

func (s *discordOAuthService) GetClientID() string {
	return s.config.ClientID
}

// Web authentication flow (OAuth 2.1 with PKCE)
func (s *discordOAuthService) GetAuthURL(state string, codeChallenge string, redirectURI string) string {
	params := url.Values{}
	params.Set("client_id", s.config.ClientID)
	params.Set("redirect_uri", redirectURI)
	params.Set("response_type", "code")
	params.Set("scope", strings.Join(s.getDefaultScopes(), " "))
	params.Set("state", state)

	// Discord doesn't require PKCE but we can include it for consistency
	if codeChallenge != "" {
		params.Set("code_challenge", codeChallenge)
		params.Set("code_challenge_method", "S256")
	}

	return fmt.Sprintf("https://discord.com/api/oauth2/authorize?%s", params.Encode())
}

func (s *discordOAuthService) ExchangeCodeForToken(ctx context.Context, request *CodeExchangeRequest) (*TokenResponse, error) {
	data := url.Values{}
	data.Set("client_id", s.config.ClientID)
	data.Set("client_secret", s.config.ClientSecret)
	data.Set("grant_type", "authorization_code")
	data.Set("code", request.Code)
	data.Set("redirect_uri", request.RedirectURI)

	// Include PKCE verifier if present
	if request.CodeVerifier != "" {
		data.Set("code_verifier", request.CodeVerifier)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://discord.com/api/oauth2/token", strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create token request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to exchange code: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token exchange failed with status %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
		RefreshToken string `json:"refresh_token"`
		Scope        string `json:"scope"`
	}

	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("failed to parse token response: %w", err)
	}

	now := time.Now()
	expiresAt := now.Add(time.Duration(tokenResp.ExpiresIn) * time.Second)

	// Parse scope string into slice
	scopes := strings.Fields(tokenResp.Scope)
	if len(scopes) == 0 && tokenResp.Scope != "" {
		// Handle comma-separated scopes
		scopes = strings.Split(tokenResp.Scope, ",")
		for i, scope := range scopes {
			scopes[i] = strings.TrimSpace(scope)
		}
	}

	return &TokenResponse{
		AccessToken:  tokenResp.AccessToken,
		TokenType:    tokenResp.TokenType,
		ExpiresIn:    int64(tokenResp.ExpiresIn),
		ExpiresAt:    expiresAt,
		RefreshToken: tokenResp.RefreshToken,
		Scope:        scopes,
		IssuedAt:     now,
	}, nil
}

func (s *discordOAuthService) RefreshAccessToken(ctx context.Context, refreshToken string) (*TokenResponse, error) {
	data := url.Values{}
	data.Set("client_id", s.config.ClientID)
	data.Set("client_secret", s.config.ClientSecret)
	data.Set("grant_type", "refresh_token")
	data.Set("refresh_token", refreshToken)

	req, err := http.NewRequestWithContext(ctx, "POST", "https://discord.com/api/oauth2/token", strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create refresh request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to refresh token: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read refresh response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token refresh failed with status %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
		RefreshToken string `json:"refresh_token"`
		Scope        string `json:"scope"`
	}

	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("failed to parse refresh response: %w", err)
	}

	now := time.Now()
	expiresAt := now.Add(time.Duration(tokenResp.ExpiresIn) * time.Second)

	// Parse scope string into slice
	scopes := strings.Fields(tokenResp.Scope)
	if len(scopes) == 0 && tokenResp.Scope != "" {
		// Handle comma-separated scopes
		scopes = strings.Split(tokenResp.Scope, ",")
		for i, scope := range scopes {
			scopes[i] = strings.TrimSpace(scope)
		}
	}

	return &TokenResponse{
		AccessToken:  tokenResp.AccessToken,
		TokenType:    tokenResp.TokenType,
		ExpiresIn:    int64(tokenResp.ExpiresIn),
		ExpiresAt:    expiresAt,
		RefreshToken: tokenResp.RefreshToken,
		Scope:        scopes,
		IssuedAt:     now,
	}, nil
}

// Mobile authentication flow (ID token validation) - Discord doesn't use ID tokens
func (s *discordOAuthService) ValidateIDToken(ctx context.Context, request *IDTokenValidationRequest) (*UserInfo, error) {
	return nil, fmt.Errorf("Discord OAuth does not support ID token validation")
}

// User information retrieval
func (s *discordOAuthService) GetUserInfo(ctx context.Context, accessToken string) (*UserInfo, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://discord.com/api/v10/users/@me", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create user info request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get user info: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read user info response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("user info request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var discordUser struct {
		ID            string `json:"id"`
		Username      string `json:"username"`
		Discriminator string `json:"discriminator"`
		Email         string `json:"email"`
		Avatar        string `json:"avatar"`
		Verified      bool   `json:"verified"`
		GlobalName    string `json:"global_name"`
	}

	if err := json.Unmarshal(body, &discordUser); err != nil {
		return nil, fmt.Errorf("failed to parse user info: %w", err)
	}

	// Construct full name from available Discord fields
	fullName := discordUser.GlobalName
	if fullName == "" {
		if discordUser.Discriminator != "0" && discordUser.Discriminator != "" {
			fullName = fmt.Sprintf("%s#%s", discordUser.Username, discordUser.Discriminator)
		} else {
			fullName = discordUser.Username
		}
	}

	// Build avatar URL if available
	var avatarURL string
	if discordUser.Avatar != "" {
		avatarURL = fmt.Sprintf("https://cdn.discordapp.com/avatars/%s/%s.png", discordUser.ID, discordUser.Avatar)
	}

	return &UserInfo{
		ProviderID:    discordUser.ID,
		Email:         discordUser.Email,
		Name:          fullName,
		Picture:       avatarURL,
		EmailVerified: discordUser.Verified,
	}, nil
}

// Token management
func (s *discordOAuthService) RevokeToken(ctx context.Context, token string) error {
	data := url.Values{}
	data.Set("client_id", s.config.ClientID)
	data.Set("client_secret", s.config.ClientSecret)
	data.Set("token", token)

	req, err := http.NewRequestWithContext(ctx, "POST", "https://discord.com/api/oauth2/token/revoke", strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("failed to create revoke request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to revoke token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("token revocation failed with status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// Provider capabilities
func (s *discordOAuthService) GetSupportedScopes() []string {
	return s.getDefaultScopes()
}

func (s *discordOAuthService) GetSupportedGrantTypes() []string {
	return []string{"authorization_code", "refresh_token"}
}

func (s *discordOAuthService) SupportsRefreshTokens() bool {
	return true
}

func (s *discordOAuthService) SupportsMobileFlow() bool {
	return false // Discord doesn't use ID tokens like Google/Apple
}

// Helper methods
func (s *discordOAuthService) getDefaultScopes() []string {
	if len(s.config.Scopes) > 0 {
		return s.config.Scopes
	}
	return []string{"identify", "email"}
}
