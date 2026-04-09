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
)

// githubOAuthService implements OAuthProviderInterface for GitHub OAuth 2.0
type githubOAuthService struct {
	config     *OAuthProviderConfig
	httpClient *http.Client
}

// NewGitHubOAuthService creates a new GitHub OAuth service with full OAuth 2.1 support
func NewGitHubOAuthService(config *OAuthProviderConfig) OAuthProviderInterface {
	return &githubOAuthService{
		config: config,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// Provider identification
func (s *githubOAuthService) GetProviderName() models.OAuthProvider {
	return models.OAuthProviderGitHub
}

func (s *githubOAuthService) GetClientID() string {
	return s.config.ClientID
}

// Web authentication flow (OAuth 2.1 with PKCE)
func (s *githubOAuthService) GetAuthURL(state string, codeChallenge string, redirectURI string) string {
	params := url.Values{}
	params.Set("client_id", s.config.ClientID)
	params.Set("redirect_uri", redirectURI)
	params.Set("response_type", "code")
	params.Set("scope", strings.Join(s.getDefaultScopes(), " "))
	params.Set("state", state)

	// OAuth 2.1 PKCE support
	if codeChallenge != "" {
		params.Set("code_challenge", codeChallenge)
		params.Set("code_challenge_method", "S256")
	}

	return fmt.Sprintf("https://github.com/login/oauth/authorize?%s", params.Encode())
}

func (s *githubOAuthService) ExchangeCodeForToken(ctx context.Context, request *CodeExchangeRequest) (*TokenResponse, error) {
	tokenURL := "https://github.com/login/oauth/access_token"

	data := url.Values{}
	data.Set("client_id", s.config.ClientID)
	data.Set("client_secret", s.config.ClientSecret)
	data.Set("code", request.Code)
	data.Set("redirect_uri", request.RedirectURI)

	// Add PKCE code verifier if provided
	if request.CodeVerifier != "" {
		data.Set("code_verifier", request.CodeVerifier)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, NewProviderError(models.OAuthProviderGitHub, "code_exchange", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, NewProviderError(models.OAuthProviderGitHub, "code_exchange", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, NewProviderError(models.OAuthProviderGitHub, "code_exchange", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, NewProviderError(models.OAuthProviderGitHub, "code_exchange", fmt.Errorf("HTTP %d", resp.StatusCode)).
			WithStatusCode(resp.StatusCode).
			WithProviderMessage(string(body))
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		Scope       string `json:"scope"`
		Error       string `json:"error,omitempty"`
		ErrorDesc   string `json:"error_description,omitempty"`
		ErrorURI    string `json:"error_uri,omitempty"`
	}

	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, NewProviderError(models.OAuthProviderGitHub, "code_exchange", err)
	}

	if tokenResp.Error != "" {
		return nil, NewProviderError(models.OAuthProviderGitHub, "code_exchange", fmt.Errorf("%s", tokenResp.Error)).
			WithProviderMessage(tokenResp.ErrorDesc)
	}

	scopes := []string{}
	if tokenResp.Scope != "" {
		scopes = strings.Split(tokenResp.Scope, ",")
	}

	return &TokenResponse{
		AccessToken: tokenResp.AccessToken,
		TokenType:   tokenResp.TokenType,
		Scope:       scopes,
		ExpiresIn:   0, // GitHub tokens don't expire
		IssuedAt:    time.Now(),
	}, nil
}

func (s *githubOAuthService) RefreshAccessToken(ctx context.Context, refreshToken string) (*TokenResponse, error) {
	// GitHub tokens don't expire, so refresh is not supported
	return nil, NewProviderError(models.OAuthProviderGitHub, "token_refresh", ErrRefreshNotSupported)
}

// Mobile authentication flow - GitHub doesn't support ID token validation
func (s *githubOAuthService) ValidateIDToken(ctx context.Context, request *IDTokenValidationRequest) (*UserInfo, error) {
	return nil, NewProviderError(models.OAuthProviderGitHub, "id_token_validation", ErrMobileFlowNotSupported)
}

// User information retrieval
func (s *githubOAuthService) GetUserInfo(ctx context.Context, accessToken string) (*UserInfo, error) {
	userURL := "https://api.github.com/user"

	// Get user profile
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, userURL, nil)
	if err != nil {
		return nil, NewProviderError(models.OAuthProviderGitHub, "user_info", err)
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "Orkestra-Backend/1.0")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, NewProviderError(models.OAuthProviderGitHub, "user_info", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, NewProviderError(models.OAuthProviderGitHub, "user_info", fmt.Errorf("HTTP %d", resp.StatusCode)).
			WithStatusCode(resp.StatusCode)
	}

	var user struct {
		ID        int    `json:"id"`
		Login     string `json:"login"`
		Name      string `json:"name"`
		Email     string `json:"email"`
		AvatarURL string `json:"avatar_url"`
		Location  string `json:"location"`
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, NewProviderError(models.OAuthProviderGitHub, "user_info", err)
	}

	if err := json.Unmarshal(body, &user); err != nil {
		return nil, NewProviderError(models.OAuthProviderGitHub, "user_info", err)
	}

	// Get primary email if not available in profile
	email := user.Email
	emailVerified := false
	if email == "" {
		email, emailVerified = s.getPrimaryEmail(ctx, accessToken)
	} else {
		emailVerified = true // Email from profile is considered verified
	}

	userInfo := &UserInfo{
		ProviderID:    fmt.Sprintf("%d", user.ID),
		Email:         email,
		EmailVerified: emailVerified,
		Name:          user.Name,
		Picture:       user.AvatarURL,
		AccessToken:   accessToken,
		ProviderData: map[string]interface{}{
			"login":      user.Login,
			"location":   user.Location,
			"avatar_url": user.AvatarURL,
		},
	}

	return userInfo, nil
}

func (s *githubOAuthService) getPrimaryEmail(ctx context.Context, accessToken string) (string, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/user/emails", nil)
	if err != nil {
		return "", false
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "Orkestra-Backend/1.0")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", false
	}

	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", false
	}

	if err := json.Unmarshal(body, &emails); err != nil {
		return "", false
	}

	// Find primary verified email
	for _, e := range emails {
		if e.Primary && e.Verified {
			return e.Email, true
		}
	}

	// Fallback to first verified email
	for _, e := range emails {
		if e.Verified {
			return e.Email, true
		}
	}

	return "", false
}

// Token management
func (s *githubOAuthService) RevokeToken(ctx context.Context, token string) error {
	revokeURL := fmt.Sprintf("https://api.github.com/applications/%s/token", s.config.ClientID)

	data := map[string]string{
		"access_token": token,
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return NewProviderError(models.OAuthProviderGitHub, "token_revocation", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, revokeURL, strings.NewReader(string(jsonData)))
	if err != nil {
		return NewProviderError(models.OAuthProviderGitHub, "token_revocation", err)
	}

	// GitHub requires basic auth for token revocation
	req.SetBasicAuth(s.config.ClientID, s.config.ClientSecret)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "Orkestra-Backend/1.0")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return NewProviderError(models.OAuthProviderGitHub, "token_revocation", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		return NewProviderError(models.OAuthProviderGitHub, "token_revocation", fmt.Errorf("HTTP %d", resp.StatusCode)).
			WithStatusCode(resp.StatusCode)
	}

	return nil
}

// Provider capabilities
func (s *githubOAuthService) GetSupportedScopes() []string {
	return s.getDefaultScopes()
}

func (s *githubOAuthService) getDefaultScopes() []string {
	if len(s.config.Scopes) > 0 {
		return s.config.Scopes
	}
	return []string{"read:user", "user:email"}
}

func (s *githubOAuthService) GetSupportedGrantTypes() []string {
	return []string{"authorization_code"}
}

func (s *githubOAuthService) SupportsRefreshTokens() bool {
	return false // GitHub tokens don't expire
}

func (s *githubOAuthService) SupportsMobileFlow() bool {
	return false // GitHub doesn't support ID token validation
}
