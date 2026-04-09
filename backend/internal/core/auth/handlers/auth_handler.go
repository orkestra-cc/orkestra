package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/repository"
	"github.com/orkestra/backend/internal/core/auth/services"
	"github.com/orkestra/backend/internal/shared/config"
	"github.com/orkestra/backend/internal/shared/middleware"
	"github.com/orkestra/backend/internal/shared/types"
	"github.com/orkestra/backend/internal/shared/utils"
	userModels "github.com/orkestra/backend/internal/core/user/models"
	"github.com/go-chi/chi/v5"
)

// AuthHandler handles authentication endpoints
type AuthHandler struct {
	authService       services.AuthService
	oauthFactory      services.OAuthProviderFactory
	oauthStateService services.OAuthStateService
	oauthProviderRepo repository.OAuthProviderRepository
	jwtService        services.JWTService
	config            *config.Config
}

// NewAuthHandler creates a new auth handler
func NewAuthHandler(
	authService services.AuthService,
	oauthFactory services.OAuthProviderFactory,
	oauthStateService services.OAuthStateService,
	oauthProviderRepo repository.OAuthProviderRepository,
	jwtService services.JWTService,
	config *config.Config,
) *AuthHandler {
	return &AuthHandler{
		authService:       authService,
		oauthFactory:      oauthFactory,
		oauthStateService: oauthStateService,
		oauthProviderRepo: oauthProviderRepo,
		jwtService:        jwtService,
		config:            config,
	}
}

// OAuth Login Request
type OAuthLoginRequest struct {
	Body struct {
		Provider models.OAuthProvider `json:"provider" enum:"google,apple,discord,github" doc:"OAuth provider name"`
	}
}

// OAuth Login Response
type OAuthLoginResponse struct {
	Body struct {
		AuthURL string `json:"authUrl" doc:"URL to redirect the user for OAuth authentication"`
		State   string `json:"state" doc:"OAuth state parameter for security"`
	}
}

// InitiateOAuthLogin handles the OAuth login initiation
func (h *AuthHandler) InitiateOAuthLogin(ctx context.Context, req *OAuthLoginRequest) (*OAuthLoginResponse, error) {
	logger := slog.Default()
	logger.Debug("InitiateOAuthLogin called", slog.String("provider", string(req.Body.Provider)))

	// Backend always determines frontend redirect URL automatically
	var frontendRedirectURL string
	if rawRequest, ok := ctx.Value("http_request").(*http.Request); ok {
		origin := rawRequest.Header.Get("Origin")
		if origin != "" {
			frontendRedirectURL = origin + "/auth/callback"
		} else {
			// Fallback to configured frontend URL
			frontendRedirectURL = h.config.Server.FrontendURL + "/auth/callback"
		}
	} else {
		// Fallback to configured frontend URL
		frontendRedirectURL = h.config.Server.FrontendURL + "/auth/callback"
	}

	// Extract device info from context (set by device middleware)
	var deviceInfo *models.DeviceInfo
	if di := ctx.Value("deviceInfo"); di != nil {
		if d, ok := di.(*types.DeviceInfo); ok {
			// Convert types.DeviceInfo to models.DeviceInfo
			deviceInfo = &models.DeviceInfo{
				DeviceID:    d.DeviceID,
				DeviceType:  d.DeviceType,
				Platform:    d.Platform,
				UserAgent:   d.UserAgent,
				Fingerprint: d.Fingerprint,
			}
		}
	}

	// Create OAuth state
	stateRequest := &services.StoreOAuthStateRequest{
		Provider:       req.Body.Provider,
		RedirectURI:    frontendRedirectURL,
		DeviceInfo:     deviceInfo,
		ExpiryDuration: 10 * time.Minute,
	}

	stateInfo, err := h.oauthStateService.StoreOAuthState(ctx, stateRequest)
	if err != nil {
		logger.Error("Failed to create OAuth state", slog.String("error", err.Error()))
		return nil, huma.Error400BadRequest("Failed to create OAuth state", err)
	}

	// Create OAuth provider
	provider, err := h.oauthFactory.CreateProvider(req.Body.Provider, nil)
	if err != nil {
		logger.Error("Failed to create OAuth provider", slog.String("error", err.Error()))
		return nil, huma.Error400BadRequest("Invalid OAuth provider", err)
	}

	// Get auth URL - use configured callback URL for OAuth provider
	var backendCallbackURL string
	switch req.Body.Provider {
	case models.OAuthProviderGoogle:
		backendCallbackURL = h.config.Auth.Google.RedirectURL
	case models.OAuthProviderApple:
		backendCallbackURL = h.config.Auth.Apple.RedirectURL
	case models.OAuthProviderDiscord:
		backendCallbackURL = h.config.Auth.Discord.RedirectURL
	case models.OAuthProviderGitHub:
		backendCallbackURL = h.config.Auth.GitHub.RedirectURL
	default:
		return nil, huma.Error400BadRequest("Unsupported OAuth provider", nil)
	}

	authURL := provider.GetAuthURL(stateInfo.State, "", backendCallbackURL)

	return &OAuthLoginResponse{
		Body: struct {
			AuthURL string `json:"authUrl" doc:"URL to redirect the user for OAuth authentication"`
			State   string `json:"state" doc:"OAuth state parameter for security"`
		}{
			AuthURL: authURL,
			State:   stateInfo.State,
		},
	}, nil
}

// OAuth Callback Request
type OAuthCallbackRequest struct {
	Code  string `query:"code" doc:"Authorization code from OAuth provider"`
	State string `query:"state" doc:"OAuth state parameter"`
}

// OAuth Callback Response with redirect
type OAuthCallbackResponse struct {
	Headers struct {
		Location string `header:"Location"`
	}
	Status int `status:"302"`
}

// Token Response (for non-redirect endpoints)
type TokenResponse struct {
	Body models.TokenResponse
}

// HandleGoogleCallback handles Google OAuth callback
// func (h *AuthHandler) HandleGoogleCallback(ctx context.Context, req *OAuthCallbackRequest) (*OAuthCallbackResponse, error) {
// 	// Validate state
// 	stateInfo, err := h.oauthStateService.ValidateOAuthState(ctx, req.State)
// 	if err != nil {
// 		return nil, huma.Error400BadRequest("Invalid OAuth state", err)
// 	}

// 	// Create Google OAuth service
// 	provider, err := h.oauthFactory.CreateProvider(models.OAuthProviderGoogle, nil)
// 	if err != nil {
// 		return nil, huma.Error500InternalServerError("Failed to get OAuth provider", err)
// 	}

// 	// Exchange code for tokens - must use same redirect URI as initial auth request
// 	backendBaseURL := "https://erpb.blacklab.cc" // TODO: Make this configurable
// 	backendCallbackURL := backendBaseURL + "/v1/auth/oauth/google/callback"
// 	tokenResp, err := provider.ExchangeCodeForToken(ctx, &services.CodeExchangeRequest{
// 		Code:        req.Code,
// 		RedirectURI: backendCallbackURL,
// 	})
// 	if err != nil {
// 		return nil, huma.Error400BadRequest("Failed to exchange code", err)
// 	}

// 	// Get user info from provider
// 	userInfo, err := provider.GetUserInfo(ctx, tokenResp.AccessToken)
// 	if err != nil {
// 		return nil, huma.Error500InternalServerError("Failed to get user info", err)
// 	}

// 	// Create or update user
// 	user := &models.User{
// 		UUID:          userInfo.ProviderID,
// 		Email:         userInfo.Email,
// 		Username:      userInfo.Email,
// 		FullName:      userInfo.Name,
// 		Avatar:        userInfo.Picture,
// 		EmailVerified: userInfo.EmailVerified,
// 		IsActive:      true,
// 		Role:          "viewer", // Default role
// 	}

// 	// Generate tokens
// 	tokenResponse, err := h.authService.GenerateEnhancedTokenPair(ctx, user, stateInfo.DeviceInfo, stateInfo.SecurityContext)
// 	if err != nil {
// 		return nil, huma.Error500InternalServerError("Failed to generate tokens", err)
// 	}

// 	// Redirect to frontend with tokens
// 	frontendURL := h.config.Server.FrontendURL
// 	redirectURL := fmt.Sprintf("%s/auth/callback?success=true&access_token=%s&token_type=Bearer&expires_in=%d&user_id=%s&email=%s&provider=google",
// 		frontendURL,
// 		url.QueryEscape(tokenResponse.AccessToken),
// 		tokenResponse.ExpiresIn,
// 		url.QueryEscape(user.UUID),
// 		url.QueryEscape(user.Email))

// 	resp := &OAuthCallbackResponse{
// 		Status: 302,
// 	}
// 	resp.Headers.Location = redirectURL

// 	return resp, nil
// }

// HandleGoogleCallbackHTTP handles Google OAuth callback with proper HTTP redirect
func (h *AuthHandler) HandleGoogleCallbackHTTP(w http.ResponseWriter, r *http.Request) {
	logger := slog.Default()
	ctx := r.Context()

	// Extract query parameters
	state := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")

	if state == "" || code == "" {
		logger.Warn("Missing state or code parameter in OAuth callback")
		http.Error(w, "Missing state or code parameter", http.StatusBadRequest)
		return
	}

	// Validate state
	stateInfo, err := h.oauthStateService.ValidateOAuthState(ctx, state)
	if err != nil {
		logger.Warn("Invalid OAuth state", slog.String("error", err.Error()))
		http.Error(w, "Invalid OAuth state", http.StatusBadRequest)
		return
	}

	// Create Google OAuth provider
	provider, err := h.oauthFactory.CreateProvider(models.OAuthProviderGoogle, nil)
	if err != nil {
		logger.Error("Failed to create OAuth provider", slog.String("error", err.Error()))
		http.Error(w, "Failed to get OAuth provider", http.StatusInternalServerError)
		return
	}

	// Exchange code for tokens
	backendCallbackURL := h.config.Auth.Google.RedirectURL

	tokenResp, err := provider.ExchangeCodeForToken(ctx, &services.CodeExchangeRequest{
		Code:        code,
		RedirectURI: backendCallbackURL,
	})
	if err != nil {
		logger.Error("Failed to exchange code for tokens", slog.String("error", err.Error()))
		http.Error(w, "Failed to exchange code", http.StatusBadRequest)
		return
	}

	// Get user info from provider
	userInfo, err := provider.GetUserInfo(ctx, tokenResp.AccessToken)
	if err != nil {
		logger.Error("Failed to get user info", slog.String("error", err.Error()))
		http.Error(w, "Failed to get user info", http.StatusInternalServerError)
		return
	}

	// Convert userInfo to map for enhanced auth service
	userInfoMap := map[string]interface{}{
		"email":          userInfo.Email,
		"name":           userInfo.Name,
		"picture":        userInfo.Picture,
		"provider_id":    userInfo.ProviderID,
		"email_verified": userInfo.EmailVerified,
	}

	// Prepare OAuth provider tokens for storage
	oauthTokens := &models.OAuthProviderTokens{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		TokenType:    tokenResp.TokenType,
		ExpiresIn:    int(tokenResp.ExpiresIn),
		Scopes:       tokenResp.Scope,
		IDToken:      tokenResp.IDToken,
	}

	// Use enhanced auth service for proper user creation and token management
	tokenResponse, err := h.authService.HandleOAuthCallbackWithLinking(ctx, models.OAuthProviderGoogle, userInfoMap, oauthTokens, stateInfo.SecurityContext, stateInfo.DeviceInfo)
	if err != nil {
		logger.Error("Failed to process OAuth callback", slog.String("error", err.Error()))
		http.Error(w, "Failed to process OAuth callback", http.StatusInternalServerError)
		return
	}
	// Set only refresh token as secure HttpOnly cookie
	// Use cookie configuration from environment
	cookieName := h.config.Auth.Cookie.Name     // Set from COOKIE_NAME env var
	cookieDomain := h.config.Auth.Cookie.Domain // Set from COOKIE_DOMAIN env var
	isSecure := h.config.Auth.Cookie.Secure     // Set from COOKIE_SECURE env var

	// Set only refresh token in cookie (7 days expiry)
	// Access token will be sent in the redirect URL for the client to store
	utils.SetRefreshTokenCookie(w, cookieName, tokenResponse.RefreshToken, 7*24*3600, cookieDomain, isSecure) // 7 days for refresh token

	// Redirect to frontend without access token (refresh token is in cookie, access token will be fetched via /auth/session)
	frontendURL := h.config.Server.FrontendURL
	redirectURL := fmt.Sprintf("%s/auth/callback?success=true&user_id=%s&email=%s&provider=google",
		frontendURL,
		url.QueryEscape(tokenResponse.User.ID),
		url.QueryEscape(tokenResponse.User.Email))

	http.Redirect(w, r, redirectURL, http.StatusFound)
}

// HandleDiscordCallbackHTTP handles Discord OAuth callback with proper HTTP redirect
func (h *AuthHandler) HandleDiscordCallbackHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract query parameters
	state := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")

	if state == "" || code == "" {
		http.Error(w, "Missing state or code parameter", http.StatusBadRequest)
		return
	}

	// Validate state
	stateInfo, err := h.oauthStateService.ValidateOAuthState(ctx, state)
	if err != nil {
		http.Error(w, "Invalid OAuth state", http.StatusBadRequest)
		return
	}

	// Create Discord OAuth provider
	provider, err := h.oauthFactory.CreateProvider(models.OAuthProviderDiscord, nil)
	if err != nil {
		http.Error(w, "Failed to get OAuth provider", http.StatusInternalServerError)
		return
	}

	// Exchange code for tokens
	backendCallbackURL := h.config.Auth.Discord.RedirectURL
	tokenResponse, err := provider.ExchangeCodeForToken(ctx, &services.CodeExchangeRequest{
		Code:        code,
		RedirectURI: backendCallbackURL,
	})
	if err != nil {
		http.Error(w, "Failed to exchange code for token", http.StatusInternalServerError)
		return
	}

	// Get user info
	userInfo, err := provider.GetUserInfo(ctx, tokenResponse.AccessToken)
	if err != nil {
		http.Error(w, "Failed to get user info", http.StatusInternalServerError)
		return
	}

	// Convert userInfo to map for enhanced auth service
	userInfoMap := map[string]interface{}{
		"email":          userInfo.Email,
		"name":           userInfo.Name,
		"picture":        userInfo.Picture,
		"provider_id":    userInfo.ProviderID,
		"email_verified": userInfo.EmailVerified,
	}

	// Prepare OAuth provider tokens for storage
	oauthTokens := &models.OAuthProviderTokens{
		AccessToken:  tokenResponse.AccessToken,
		RefreshToken: tokenResponse.RefreshToken,
		TokenType:    tokenResponse.TokenType,
		ExpiresIn:    int(tokenResponse.ExpiresIn),
		Scopes:       tokenResponse.Scope,
	}

	// Use enhanced auth service for proper user creation and token management
	authTokenResponse, err := h.authService.HandleOAuthCallbackWithLinking(ctx, models.OAuthProviderDiscord, userInfoMap, oauthTokens, stateInfo.SecurityContext, stateInfo.DeviceInfo)
	if err != nil {
		http.Error(w, "Failed to process OAuth callback", http.StatusInternalServerError)
		return
	}

	// Set only refresh token as secure HttpOnly cookie
	// Use cookie configuration from environment
	cookieName := h.config.Auth.Cookie.Name     // Set from COOKIE_NAME env var
	cookieDomain := h.config.Auth.Cookie.Domain // Set from COOKIE_DOMAIN env var
	isSecure := h.config.Auth.Cookie.Secure     // Set from COOKIE_SECURE env var

	// Set only refresh token in cookie (7 days expiry)
	// Access token will be sent in the redirect URL for the client to store
	utils.SetRefreshTokenCookie(w, cookieName, authTokenResponse.RefreshToken, 7*24*3600, cookieDomain, isSecure) // 7 days for refresh token

	// Redirect to frontend without access token (refresh token is in cookie, access token will be fetched via /auth/session)
	frontendURL := h.config.Server.FrontendURL
	redirectURL := fmt.Sprintf("%s/auth/callback?success=true&user_id=%s&email=%s&provider=discord",
		frontendURL,
		url.QueryEscape(authTokenResponse.User.ID),
		url.QueryEscape(authTokenResponse.User.Email))

	http.Redirect(w, r, redirectURL, http.StatusFound)
}

// HandleAppleCallbackHTTP handles Apple OAuth callback with proper HTTP redirect
func (h *AuthHandler) HandleAppleCallbackHTTP(w http.ResponseWriter, r *http.Request) {
	utils.AuthDebugFlow("HandleAppleCallbackHTTP")
	ctx := r.Context()

	// Parse form data (Apple uses POST with form data, not query parameters)
	if err := r.ParseForm(); err != nil {
		utils.AuthDebugError("parse_form", err)
		http.Error(w, "Failed to parse form data", http.StatusBadRequest)
		return
	}

	// Extract form parameters
	state := r.FormValue("state")
	code := r.FormValue("code")
	idToken := r.FormValue("id_token")

	// Debug logging with sensitive data protection
	utils.AuthDebugPresence("state", state)
	utils.AuthDebugPresence("code", code)
	utils.AuthDebugPresence("id_token", idToken)

	if code == "" {
		utils.AuthDebugError("validation", fmt.Errorf("missing authorization code"))
		http.Error(w, "Missing authorization code", http.StatusBadRequest)
		return
	}

	var stateInfo *services.OAuthStateInfo

	if state == "" {
		// SECURITY: In production, state parameter is REQUIRED to prevent CSRF attacks
		if h.config.IsProductionLike() {
			utils.AuthDebugError("security", fmt.Errorf("missing state parameter in production - possible CSRF attack"))
			http.Error(w, "Missing state parameter - authentication rejected for security", http.StatusBadRequest)
			return
		}

		// Development only: Allow fallback with warning (for testing Apple Sign In configuration issues)
		if idToken == "" {
			utils.AuthDebugError("security", fmt.Errorf("missing both state and id_token"))
			http.Error(w, "Missing security parameters", http.StatusBadRequest)
			return
		}

		// Log security warning for development
		utils.AuthDebug("SECURITY WARNING: State parameter missing - this would fail in production!")
		utils.AuthDebug("Fix Apple Service ID configuration to include state parameter")

		// Create minimal state info for development testing only
		stateInfo = &services.OAuthStateInfo{
			State:       "apple-dev-fallback",
			Provider:    models.OAuthProviderApple,
			RedirectURI: h.config.Server.FrontendURL + "/auth/callback",
			DeviceInfo:  nil,
			SecurityContext: &models.SecurityContext{
				IPAddress: utils.GetClientIP(r),
				Timestamp: time.Now(),
			},
		}
	} else {
		// Normal flow: Validate state parameter
		var err error
		stateInfo, err = h.oauthStateService.ValidateOAuthState(ctx, state)
		if err != nil {
			utils.AuthDebugError("state_validation", err)
			http.Error(w, "Invalid OAuth state", http.StatusBadRequest)
			return
		}
		utils.AuthDebug("OAuth state validated - Provider: %s", stateInfo.Provider)
	}

	// Create Apple OAuth provider
	utils.AuthDebug("Creating Apple OAuth provider")
	provider, err := h.oauthFactory.CreateProvider(models.OAuthProviderApple, nil)
	if err != nil {
		utils.AuthDebugError("create_provider", err)
		http.Error(w, "Failed to get OAuth provider", http.StatusInternalServerError)
		return
	}

	// Exchange code for tokens
	backendCallbackURL := h.config.Auth.Apple.RedirectURL
	utils.AuthDebug("Exchanging code for tokens")

	tokenResp, err := provider.ExchangeCodeForToken(ctx, &services.CodeExchangeRequest{
		Code:        code,
		RedirectURI: backendCallbackURL,
	})
	if err != nil {
		utils.AuthDebugError("exchange_code", err)
		http.Error(w, "Failed to exchange code", http.StatusBadRequest)
		return
	}
	utils.AuthDebug("Code exchanged successfully")

	// Get user info from Apple ID token (Apple doesn't provide a user info endpoint)
	utils.AuthDebug("Validating Apple ID token")
	userInfo, err := provider.ValidateIDToken(ctx, &services.IDTokenValidationRequest{
		IDToken:     tokenResp.IDToken,
		AccessToken: tokenResp.AccessToken,
		Audience:    provider.GetClientID(),
	})
	if err != nil {
		utils.AuthDebugError("validate_id_token", err)
		http.Error(w, "Failed to validate ID token", http.StatusInternalServerError)
		return
	}
	utils.AuthDebugPresence("user_email", userInfo.Email)

	// Convert userInfo to map for enhanced auth service
	userInfoMap := map[string]interface{}{
		"email":          userInfo.Email,
		"name":           userInfo.Name,
		"picture":        userInfo.Picture,
		"provider_id":    userInfo.ProviderID,
		"email_verified": userInfo.EmailVerified,
	}

	// Prepare OAuth provider tokens for storage
	oauthTokens := &models.OAuthProviderTokens{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		TokenType:    tokenResp.TokenType,
		ExpiresIn:    int(tokenResp.ExpiresIn),
		Scopes:       tokenResp.Scope,
		IDToken:      tokenResp.IDToken,
	}
	utils.AuthDebug("OAuth tokens prepared - has access: %v, has refresh: %v",
		tokenResp.AccessToken != "", tokenResp.RefreshToken != "")

	// Use enhanced auth service for proper user creation and token management
	utils.AuthDebug("Processing OAuth callback with linking")
	tokenResponse, err := h.authService.HandleOAuthCallbackWithLinking(ctx, models.OAuthProviderApple, userInfoMap, oauthTokens, stateInfo.SecurityContext, stateInfo.DeviceInfo)
	if err != nil {
		utils.AuthDebugError("oauth_callback", err)
		http.Error(w, "Failed to process OAuth callback", http.StatusInternalServerError)
		return
	}
	// Set only refresh token as secure HttpOnly cookie
	// Use cookie configuration from environment
	cookieName := h.config.Auth.Cookie.Name     // Set from COOKIE_NAME env var
	cookieDomain := h.config.Auth.Cookie.Domain // Set from COOKIE_DOMAIN env var
	isSecure := h.config.Auth.Cookie.Secure     // Set from COOKIE_SECURE env var

	// Set only refresh token in cookie (7 days expiry)
	// Access token will be sent in the redirect URL for the client to store
	utils.SetRefreshTokenCookie(w, cookieName, tokenResponse.RefreshToken, 7*24*3600, cookieDomain, isSecure) // 7 days for refresh token

	// Redirect to frontend without access token (refresh token is in cookie, access token will be fetched via /auth/session)
	frontendURL := h.config.Server.FrontendURL
	redirectURL := fmt.Sprintf("%s/auth/callback?success=true&user_id=%s&email=%s&provider=apple",
		frontendURL,
		url.QueryEscape(tokenResponse.User.ID),
		url.QueryEscape(tokenResponse.User.Email))

	http.Redirect(w, r, redirectURL, http.StatusFound)
}

// HandleAppleCallback handles Apple OAuth callback
func (h *AuthHandler) HandleAppleCallback(ctx context.Context, req *OAuthCallbackRequest) (*OAuthCallbackResponse, error) {
	// Similar to Google callback
	stateInfo, err := h.oauthStateService.ValidateOAuthState(ctx, req.State)
	if err != nil {
		return nil, huma.Error400BadRequest("Invalid OAuth state", err)
	}

	provider, err := h.oauthFactory.CreateProvider(models.OAuthProviderApple, nil)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to get OAuth provider", err)
	}

	// Exchange code for tokens - must use same redirect URI as initial auth request
	backendCallbackURL := h.config.Auth.Apple.RedirectURL
	tokenResp, err := provider.ExchangeCodeForToken(ctx, &services.CodeExchangeRequest{
		Code:        req.Code,
		RedirectURI: backendCallbackURL,
	})
	if err != nil {
		return nil, huma.Error400BadRequest("Failed to exchange code", err)
	}

	userInfo, err := provider.GetUserInfo(ctx, tokenResp.AccessToken)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to get user info", err)
	}

	// Convert userInfo to map for enhanced auth service
	userInfoMap := map[string]interface{}{
		"email":          userInfo.Email,
		"name":           userInfo.Name,
		"picture":        userInfo.Picture,
		"provider_id":    userInfo.ProviderID,
		"email_verified": userInfo.EmailVerified,
	}

	// Prepare OAuth provider tokens for storage
	oauthTokens := &models.OAuthProviderTokens{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		TokenType:    tokenResp.TokenType,
		ExpiresIn:    int(tokenResp.ExpiresIn),
		IDToken:      tokenResp.IDToken,
	}

	// Use enhanced auth service for proper user creation and token management
	tokenResponse, err := h.authService.HandleOAuthCallbackWithLinking(ctx, models.OAuthProviderApple, userInfoMap, oauthTokens, stateInfo.SecurityContext, stateInfo.DeviceInfo)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to process OAuth callback", err)
	}

	// Redirect to frontend with tokens (Note: Huma handlers can't set cookies directly)
	frontendURL := h.config.Server.FrontendURL
	redirectURL := fmt.Sprintf("%s/auth/callback?success=true&access_token=%s&token_type=Bearer&expires_in=%d&user_id=%s&email=%s&provider=apple",
		frontendURL,
		url.QueryEscape(tokenResponse.AccessToken),
		tokenResponse.ExpiresIn,
		url.QueryEscape(tokenResponse.User.ID),
		url.QueryEscape(tokenResponse.User.Email))

	resp := &OAuthCallbackResponse{
		Status: 302,
	}
	resp.Headers.Location = redirectURL

	return resp, nil
}

// HandleDiscordCallback handles Discord OAuth callback
// func (h *AuthHandler) HandleDiscordCallback(ctx context.Context, req *OAuthCallbackRequest) (*OAuthCallbackResponse, error) {
// 	stateInfo, err := h.oauthStateService.ValidateOAuthState(ctx, req.State)
// 	if err != nil {
// 		return nil, huma.Error400BadRequest("Invalid OAuth state", err)
// 	}

// 	provider, err := h.oauthFactory.CreateProvider(models.OAuthProviderDiscord, nil)
// 	if err != nil {
// 		return nil, huma.Error500InternalServerError("Failed to get OAuth provider", err)
// 	}

// 	// Exchange code for tokens - must use same redirect URI as initial auth request
// 	backendBaseURL := "https://erpb.blacklab.cc" // TODO: Make this configurable
// 	backendCallbackURL := backendBaseURL + "/v1/auth/oauth/discord/callback"
// 	tokenResp, err := provider.ExchangeCodeForToken(ctx, &services.CodeExchangeRequest{
// 		Code:        req.Code,
// 		RedirectURI: backendCallbackURL,
// 	})
// 	if err != nil {
// 		return nil, huma.Error400BadRequest("Failed to exchange code", err)
// 	}

// 	userInfo, err := provider.GetUserInfo(ctx, tokenResp.AccessToken)
// 	if err != nil {
// 		return nil, huma.Error500InternalServerError("Failed to get user info", err)
// 	}

// 	// Convert userInfo to map for enhanced auth service
// 	userInfoMap := map[string]interface{}{
// 		"email":          userInfo.Email,
// 		"name":           userInfo.Name,
// 		"picture":        userInfo.Picture,
// 		"provider_id":    userInfo.ProviderID,
// 		"email_verified": userInfo.EmailVerified,
// 	}

// 	// Use enhanced auth service for proper user creation and token management
// 	tokenResponse, err := h.authService.HandleOAuthCallbackWithLinking(ctx, models.OAuthProviderDiscord, userInfoMap, oauthTokens, stateInfo.SecurityContext, stateInfo.DeviceInfo)
// 	if err != nil {
// 		return nil, huma.Error500InternalServerError("Failed to process OAuth callback", err)
// 	}

// 	// Redirect to frontend with tokens (Note: Huma handlers can't set cookies directly)
// 	frontendURL := h.config.Server.FrontendURL
// 	redirectURL := fmt.Sprintf("%s/auth/callback?success=true&access_token=%s&token_type=Bearer&expires_in=%d&user_id=%s&email=%s&provider=discord",
// 		frontendURL,
// 		url.QueryEscape(tokenResponse.AccessToken),
// 		tokenResponse.ExpiresIn,
// 		url.QueryEscape(tokenResponse.User.ID),
// 		url.QueryEscape(tokenResponse.User.Email))

// 	resp := &OAuthCallbackResponse{
// 		Status: 302,
// 	}
// 	resp.Headers.Location = redirectURL

// 	return resp, nil
// }

// HandleGitHubCallback handles GitHub OAuth callback
func (h *AuthHandler) HandleGitHubCallback(ctx context.Context, req *OAuthCallbackRequest) (*OAuthCallbackResponse, error) {
	stateInfo, err := h.oauthStateService.ValidateOAuthState(ctx, req.State)
	if err != nil {
		return nil, huma.Error400BadRequest("Invalid OAuth state", err)
	}

	provider, err := h.oauthFactory.CreateProvider(models.OAuthProviderGitHub, nil)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to get OAuth provider", err)
	}

	// Exchange code for tokens - must use same redirect URI as initial auth request
	backendCallbackURL := h.config.Auth.GitHub.RedirectURL
	tokenResp, err := provider.ExchangeCodeForToken(ctx, &services.CodeExchangeRequest{
		Code:        req.Code,
		RedirectURI: backendCallbackURL,
	})
	if err != nil {
		return nil, huma.Error400BadRequest("Failed to exchange code", err)
	}

	userInfo, err := provider.GetUserInfo(ctx, tokenResp.AccessToken)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to get user info", err)
	}

	// Convert userInfo to map for enhanced auth service
	userInfoMap := map[string]interface{}{
		"email":          userInfo.Email,
		"name":           userInfo.Name,
		"picture":        userInfo.Picture,
		"provider_id":    userInfo.ProviderID,
		"email_verified": userInfo.EmailVerified,
	}

	// Prepare OAuth provider tokens for storage
	oauthTokens := &models.OAuthProviderTokens{
		AccessToken: tokenResp.AccessToken,
		TokenType:   tokenResp.TokenType,
		Scopes:      tokenResp.Scope,
	}

	// Use enhanced auth service for proper user creation and token management
	tokenResponse, err := h.authService.HandleOAuthCallbackWithLinking(ctx, models.OAuthProviderGitHub, userInfoMap, oauthTokens, stateInfo.SecurityContext, stateInfo.DeviceInfo)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to process OAuth callback", err)
	}

	// Redirect to frontend without access token (access token will be fetched via /auth/session)
	// Note: Huma handlers can't set cookies directly, so refresh token handling may need adjustment
	frontendURL := h.config.Server.FrontendURL
	redirectURL := fmt.Sprintf("%s/auth/callback?success=true&user_id=%s&email=%s&provider=github",
		frontendURL,
		url.QueryEscape(tokenResponse.User.ID),
		url.QueryEscape(tokenResponse.User.Email))

	resp := &OAuthCallbackResponse{
		Status: 302,
	}
	resp.Headers.Location = redirectURL

	return resp, nil
}

// Refresh Token Request
type RefreshTokenRequest struct {
	RefreshToken string `json:"refreshToken" doc:"Refresh token to exchange for new tokens"`
}

// RefreshTokensResponse simplified response with token in header
type RefreshTokensResponse struct {
	Headers struct {
		XNewAccessToken string `header:"X-New-Access-Token" doc:"New access token"`
	}
	Body struct {
		TokenType string `json:"tokenType"`
		ExpiresIn int64  `json:"expiresIn"`
		Success   bool   `json:"success"`
	}
}

// RefreshTokens handles token refresh
func (h *AuthHandler) RefreshTokens(ctx context.Context, req *RefreshTokenRequest) (*RefreshTokensResponse, error) {
	logger := slog.Default()

	// Extract device info and IP address from request context
	deviceInfo := middleware.GetDeviceInfo(ctx)
	var ipAddress string
	if deviceInfo != nil {
		ipAddress = deviceInfo.IP
	} else {
		ipAddress = "unknown" // Fallback if device info is not available
	}

	// Extract security context from request
	securityCtx := &models.SecurityContext{
		IPAddress: ipAddress,
		Timestamp: time.Now(),
	}

	var refreshToken string

	// First, try to get refresh token from cookie if available (Huma context doesn't have direct HTTP request access)
	// Check if we have access to the raw HTTP request from context
	if rawRequest, ok := ctx.Value("http_request").(*http.Request); ok {
		cookieName := h.config.Auth.Cookie.Name
		if cookieToken, err := utils.GetRefreshTokenFromCookieByName(rawRequest, cookieName); err == nil {
			refreshToken = cookieToken
		}
	}

	// If no token from cookie, use token from request body
	if refreshToken == "" && req.RefreshToken != "" {
		refreshToken = req.RefreshToken
	}

	// If no token found in either place
	if refreshToken == "" {
		return nil, huma.Error401Unauthorized("No refresh token provided", nil)
	}

	// Validate and refresh tokens with risk assessment
	tokenResponse, err := h.authService.RefreshTokensWithRiskAssessment(ctx, refreshToken, securityCtx)
	if err != nil {
		logger.Warn("Token refresh failed", slog.String("error", err.Error()))
		return nil, huma.Error401Unauthorized("Invalid refresh token", err)
	}

	return &RefreshTokensResponse{
		Headers: struct {
			XNewAccessToken string `header:"X-New-Access-Token" doc:"New access token"`
		}{
			XNewAccessToken: tokenResponse.AccessToken,
		},
		Body: struct {
			TokenType string `json:"tokenType"`
			ExpiresIn int64  `json:"expiresIn"`
			Success   bool   `json:"success"`
		}{
			TokenType: "Bearer",
			ExpiresIn: tokenResponse.ExpiresIn,
			Success:   true,
		},
	}, nil
}

// RefreshTokensWithHeaderHTTP handles token refresh with access token in X-New-Access-Token header
func (h *AuthHandler) RefreshTokensWithHeaderHTTP(w http.ResponseWriter, r *http.Request) {
	logger := slog.Default()
	ctx := r.Context()

	// Extract device info and IP address from request
	ipAddress := utils.GetClientIP(r)

	// Extract security context from request
	securityCtx := &models.SecurityContext{
		IPAddress: ipAddress,
		Timestamp: time.Now(),
	}

	// Extract refresh token from cookie or request body
	var refreshToken string
	var tokenSource string

	// First, try to get refresh token from cookie (using configured cookie name)
	cookieName := h.config.Auth.Cookie.Name
	if cookieToken, err := utils.GetRefreshTokenFromCookieByName(r, cookieName); err == nil {
		refreshToken = cookieToken
		tokenSource = "cookie"
	} else {
		// If no token from cookie, try parsing request body
		var req RefreshTokenRequest
		if r.Header.Get("Content-Type") == "application/json" {
			if err := json.NewDecoder(r.Body).Decode(&req); err == nil && req.RefreshToken != "" {
				refreshToken = req.RefreshToken
				tokenSource = "request_body"
			}
		}
	}

	// If no token found in either place
	if refreshToken == "" {
		http.Error(w, "No refresh token provided", http.StatusUnauthorized)
		return
	}

	// Validate and refresh tokens with risk assessment
	tokenResponse, err := h.authService.RefreshTokensWithRiskAssessment(ctx, refreshToken, securityCtx)
	if err != nil {
		logger.Warn("Token refresh failed", slog.String("error", err.Error()))
		http.Error(w, "Invalid refresh token", http.StatusUnauthorized)
		return
	}

	// Set new refresh token as cookie if we got the original from a cookie
	if tokenSource == "cookie" {
		cookieDomain := h.config.Auth.Cookie.Domain
		isSecure := h.config.Auth.Cookie.Secure
		utils.SetRefreshTokenCookie(w, cookieName, tokenResponse.RefreshToken, 7*24*3600, cookieDomain, isSecure) // 7 days
	}

	// Set the access token in the X-New-Access-Token header
	w.Header().Set("X-New-Access-Token", tokenResponse.AccessToken)

	// Return minimal JSON response
	w.Header().Set("Content-Type", "application/json")
	response := struct {
		TokenType string `json:"tokenType"`
		ExpiresIn int64  `json:"expiresIn"`
		Success   bool   `json:"success"`
	}{
		TokenType: "Bearer",
		ExpiresIn: tokenResponse.ExpiresIn,
		Success:   true,
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		logger.Error("Failed to encode response", slog.String("error", err.Error()))
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

// GetSessionHTTP handles session initialization for web clients after OAuth callback
// It uses the refresh token from cookie to generate a fresh access token
func (h *AuthHandler) GetSessionHTTP(w http.ResponseWriter, r *http.Request) {
	logger := slog.Default()
	ctx := r.Context()

	// Extract device info and IP address from request
	ipAddress := utils.GetClientIP(r)

	// Extract security context from request
	securityCtx := &models.SecurityContext{
		IPAddress: ipAddress,
		Timestamp: time.Now(),
	}

	// Extract refresh token from cookie
	cookieName := h.config.Auth.Cookie.Name
	refreshToken, err := utils.GetRefreshTokenFromCookieByName(r, cookieName)
	if err != nil {
		http.Error(w, "No refresh token found in cookie", http.StatusUnauthorized)
		return
	}

	// Validate and refresh tokens with risk assessment
	tokenResponse, err := h.authService.RefreshTokensWithRiskAssessment(ctx, refreshToken, securityCtx)
	if err != nil {
		logger.Warn("Token refresh failed", slog.String("error", err.Error()))
		http.Error(w, "Invalid refresh token", http.StatusUnauthorized)
		return
	}

	// Set new refresh token as cookie
	cookieDomain := h.config.Auth.Cookie.Domain
	isSecure := h.config.Auth.Cookie.Secure
	utils.SetRefreshTokenCookie(w, cookieName, tokenResponse.RefreshToken, 7*24*3600, cookieDomain, isSecure) // 7 days

	// Return the access token and user info in the response body for Redux storage
	w.Header().Set("Content-Type", "application/json")
	response := struct {
		AccessToken    string                             `json:"accessToken"`
		TokenType      string                             `json:"tokenType"`
		ExpiresIn      int64                              `json:"expiresIn"`
		User           *userModels.UserManagementResponse `json:"user"`
		OAuthProviders []models.OAuthProviderInfo         `json:"oauthProviders,omitempty"`
		Success        bool                               `json:"success"`
	}{
		AccessToken:    tokenResponse.AccessToken,
		TokenType:      "Bearer",
		ExpiresIn:      tokenResponse.ExpiresIn,
		User:           tokenResponse.User,
		OAuthProviders: tokenResponse.OAuthProviders,
		Success:        true,
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		logger.Error("Failed to encode response", slog.String("error", err.Error()))
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

// MobileGoogleAuthRequest represents the request from mobile app with Google tokens
type MobileGoogleAuthRequest struct {
	Body struct {
		IDToken     string `json:"id_token" form:"id_token" doc:"Google ID token from mobile app"`
		AccessToken string `json:"access_token,omitempty" form:"access_token" doc:"Google access token from mobile app"`
	}
}

// MobileGoogleAuthResponse represents the response to mobile app with JWT tokens
type MobileGoogleAuthResponse struct {
	Body struct {
		AccessToken  string `json:"access_token" doc:"JWT access token for API access"`
		RefreshToken string `json:"refresh_token" doc:"JWT refresh token for token renewal"`
		TokenType    string `json:"token_type" doc:"Token type (Bearer)"`
		ExpiresIn    int64  `json:"expires_in" doc:"Access token expiration time in seconds"`
		User         struct {
			ID            string `json:"id" doc:"User ID"`
			Email         string `json:"email" doc:"User email"`
			Name          string `json:"name" doc:"User full name"`
			Avatar        string `json:"avatar,omitempty" doc:"User avatar URL"`
			EmailVerified bool   `json:"email_verified" doc:"Email verification status"`
		} `json:"user" doc:"User information"`
	}
}

// HandleMobileGoogleAuth handles Google authentication from mobile apps
func (h *AuthHandler) HandleMobileGoogleAuth(ctx context.Context, req *MobileGoogleAuthRequest) (*MobileGoogleAuthResponse, error) {
	logger := slog.Default()

	// Extract device info from context
	var deviceInfo *models.DeviceInfo
	var ipAddress string = "unknown"
	if di := ctx.Value("deviceInfo"); di != nil {
		if d, ok := di.(*types.DeviceInfo); ok {
			deviceInfo = &models.DeviceInfo{
				DeviceID:    d.DeviceID,
				DeviceType:  d.DeviceType,
				Platform:    d.Platform,
				UserAgent:   d.UserAgent,
				Fingerprint: d.Fingerprint,
			}
			ipAddress = d.IP // Get IP from types.DeviceInfo
		}
	}
	securityCtx := &models.SecurityContext{
		IPAddress: ipAddress,
		Timestamp: time.Now(),
	}

	// Get Google OAuth provider
	provider, err := h.oauthFactory.CreateProvider(models.OAuthProviderGoogle, nil)
	if err != nil {
		logger.Error("Failed to create Google OAuth provider", slog.String("error", err.Error()))
		return nil, huma.Error500InternalServerError("Failed to initialize authentication provider", err)
	}

	// Validate ID token and get user info
	validationRequest := &services.IDTokenValidationRequest{
		IDToken:     req.Body.IDToken,
		AccessToken: req.Body.AccessToken,
		Audience:    h.config.Auth.Google.AndroidClientID, // Use Android client ID from environment
	}

	userInfo, err := provider.ValidateIDToken(ctx, validationRequest)
	if err != nil {
		logger.Warn("ID token validation failed", slog.String("error", err.Error()))
		return nil, huma.Error401Unauthorized("Invalid Google ID token", err)
	}

	// Convert userInfo to map for auth service
	userInfoMap := map[string]interface{}{
		"email":          userInfo.Email,
		"name":           userInfo.Name,
		"picture":        userInfo.Picture,
		"provider_id":    userInfo.ProviderID,
		"email_verified": userInfo.EmailVerified,
		"given_name":     userInfo.GivenName,
		"family_name":    userInfo.FamilyName,
	}

	// Store OAuth provider tokens if we have an access token
	var oauthTokens *models.OAuthProviderTokens
	if req.Body.AccessToken != "" {
		oauthTokens = &models.OAuthProviderTokens{
			AccessToken: req.Body.AccessToken,
			TokenType:   "Bearer",
		}
	}

	// Use auth service to handle user creation/update and generate JWT tokens
	tokenResponse, err := h.authService.HandleOAuthCallbackWithLinking(
		ctx,
		models.OAuthProviderGoogle,
		userInfoMap,
		oauthTokens,
		securityCtx,
		deviceInfo,
	)
	if err != nil {
		logger.Error("Failed to process OAuth callback", slog.String("error", err.Error()))
		return nil, huma.Error500InternalServerError("Failed to process authentication", err)
	}

	// Prepare response
	response := &MobileGoogleAuthResponse{}
	response.Body.AccessToken = tokenResponse.AccessToken
	response.Body.RefreshToken = tokenResponse.RefreshToken
	response.Body.TokenType = "Bearer"
	response.Body.ExpiresIn = tokenResponse.ExpiresIn
	response.Body.User.ID = tokenResponse.User.ID
	response.Body.User.Email = tokenResponse.User.Email
	response.Body.User.Name = tokenResponse.User.FullName // Use FullName instead of Name
	response.Body.User.Avatar = tokenResponse.User.Avatar
	response.Body.User.EmailVerified = tokenResponse.User.EmailVerified

	return response, nil
}

// MobileAppleAuthRequest represents the request from mobile app with Apple ID token
type MobileAppleAuthRequest struct {
	Body struct {
		IDToken     string `json:"id_token" form:"id_token" doc:"Apple ID token from mobile app"`
		AccessToken string `json:"access_token,omitempty" form:"access_token" doc:"Apple access token from mobile app (optional)"`
	}
}

// MobileAppleAuthResponse represents the response to mobile app with JWT tokens
// Reuses the same structure as Google for consistency
type MobileAppleAuthResponse = MobileGoogleAuthResponse

// HandleMobileAppleAuth handles Apple authentication from mobile apps
func (h *AuthHandler) HandleMobileAppleAuth(ctx context.Context, req *MobileAppleAuthRequest) (*MobileAppleAuthResponse, error) {
	logger := slog.Default()

	// Extract device info from context
	var deviceInfo *models.DeviceInfo
	var ipAddress string = "unknown"
	if di := ctx.Value("deviceInfo"); di != nil {
		if d, ok := di.(*types.DeviceInfo); ok {
			deviceInfo = &models.DeviceInfo{
				DeviceID:    d.DeviceID,
				DeviceType:  d.DeviceType,
				Platform:    d.Platform,
				UserAgent:   d.UserAgent,
				Fingerprint: d.Fingerprint,
			}
			ipAddress = d.IP // Get IP from types.DeviceInfo
		}
	}
	securityCtx := &models.SecurityContext{
		IPAddress: ipAddress,
		Timestamp: time.Now(),
	}

	// Get Apple OAuth provider
	provider, err := h.oauthFactory.CreateProvider(models.OAuthProviderApple, nil)
	if err != nil {
		logger.Error("Failed to create Apple OAuth provider", slog.String("error", err.Error()))
		return nil, huma.Error500InternalServerError("Failed to initialize authentication provider", err)
	}

	// Determine audience based on platform
	var audience string
	if deviceInfo != nil && deviceInfo.Platform == "ios" {
		audience = h.config.Auth.Apple.IOSClientID
	} else if deviceInfo != nil && deviceInfo.Platform == "android" {
		audience = h.config.Auth.Apple.AndroidClientID
	} else {
		// Fallback to general client ID if platform is not specified
		audience = h.config.Auth.Apple.ClientID
	}

	// Validate ID token and get user info
	validationRequest := &services.IDTokenValidationRequest{
		IDToken:     req.Body.IDToken,
		AccessToken: req.Body.AccessToken,
		Audience:    audience,
	}

	userInfo, err := provider.ValidateIDToken(ctx, validationRequest)
	if err != nil {
		logger.Warn("ID token validation failed", slog.String("error", err.Error()))
		return nil, huma.Error401Unauthorized("Invalid Apple ID token", err)
	}

	// Convert userInfo to map for auth service
	userInfoMap := map[string]interface{}{
		"email":          userInfo.Email,
		"name":           userInfo.Name,
		"picture":        userInfo.Picture,
		"provider_id":    userInfo.ProviderID,
		"email_verified": userInfo.EmailVerified,
		"given_name":     userInfo.GivenName,
		"family_name":    userInfo.FamilyName,
	}

	// Store OAuth provider tokens if we have an access token
	var oauthTokens *models.OAuthProviderTokens
	if req.Body.AccessToken != "" {
		oauthTokens = &models.OAuthProviderTokens{
			AccessToken: req.Body.AccessToken,
			TokenType:   "Bearer",
		}
	}

	// Use auth service to handle user creation/update and generate JWT tokens
	tokenResponse, err := h.authService.HandleOAuthCallbackWithLinking(
		ctx,
		models.OAuthProviderApple,
		userInfoMap,
		oauthTokens,
		securityCtx,
		deviceInfo,
	)
	if err != nil {
		logger.Error("Failed to process OAuth callback", slog.String("error", err.Error()))
		return nil, huma.Error500InternalServerError("Failed to process authentication", err)
	}

	// Prepare response
	response := &MobileAppleAuthResponse{}
	response.Body.AccessToken = tokenResponse.AccessToken
	response.Body.RefreshToken = tokenResponse.RefreshToken
	response.Body.TokenType = "Bearer"
	response.Body.ExpiresIn = tokenResponse.ExpiresIn
	response.Body.User.ID = tokenResponse.User.ID
	response.Body.User.Email = tokenResponse.User.Email
	response.Body.User.Name = tokenResponse.User.FullName // Use FullName instead of Name
	response.Body.User.Avatar = tokenResponse.User.Avatar
	response.Body.User.EmailVerified = tokenResponse.User.EmailVerified

	return response, nil
}

// RefreshTokensHTTP handles token refresh with cookie support (raw HTTP handler)
func (h *AuthHandler) RefreshTokensHTTP(w http.ResponseWriter, r *http.Request) {
	logger := slog.Default()
	ctx := r.Context()

	// Extract device info and IP address from request
	ipAddress := utils.GetClientIP(r)

	// Extract security context from request
	securityCtx := &models.SecurityContext{
		IPAddress: ipAddress,
		Timestamp: time.Now(),
	}

	// Extract refresh token from cookie or request body
	var refreshToken string
	var tokenSource string

	// First, try to get refresh token from cookie (using configured cookie name)
	cookieName := h.config.Auth.Cookie.Name
	if cookieToken, err := utils.GetRefreshTokenFromCookieByName(r, cookieName); err == nil {
		refreshToken = cookieToken
		tokenSource = "cookie"
	} else {
		// If no token from cookie, try parsing request body
		var req RefreshTokenRequest
		if r.Header.Get("Content-Type") == "application/json" {
			if err := json.NewDecoder(r.Body).Decode(&req); err == nil && req.RefreshToken != "" {
				refreshToken = req.RefreshToken
				tokenSource = "request_body"
			}
		}
	}

	// If no token found in either place
	if refreshToken == "" {
		http.Error(w, "No refresh token provided", http.StatusUnauthorized)
		return
	}

	// Validate and refresh tokens with risk assessment
	tokenResponse, err := h.authService.RefreshTokensWithRiskAssessment(ctx, refreshToken, securityCtx)
	if err != nil {
		logger.Warn("Token refresh failed", slog.String("error", err.Error()))
		http.Error(w, "Invalid refresh token", http.StatusUnauthorized)
		return
	}

	// Set new refresh token as cookie if we got the original from a cookie
	if tokenSource == "cookie" {
		cookieDomain := h.config.Auth.Cookie.Domain
		isSecure := h.config.Auth.Cookie.Secure
		utils.SetRefreshTokenCookie(w, cookieName, tokenResponse.RefreshToken, 7*24*3600, cookieDomain, isSecure) // 7 days
	}

	// Return JSON response
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(tokenResponse); err != nil {
		logger.Error("Failed to encode response", slog.String("error", err.Error()))
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

// Logout Request
type LogoutRequest struct {
	RefreshToken string `json:"refreshToken,omitempty" doc:"Refresh token to invalidate"`
	AllDevices   bool   `json:"allDevices" doc:"Logout from all devices"`
}

// Logout Response
type LogoutResponse struct {
	Body struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
}

// LogoutHTTP handles user logout with proper cookie clearing (raw HTTP handler)
func (h *AuthHandler) LogoutHTTP(w http.ResponseWriter, r *http.Request) {
	logger := slog.Default()
	ctx := r.Context()

	// Try to get user UUID from context first (if authenticated via middleware)
	userUUIDVal := ctx.Value("userUUID")
	if userUUIDVal == nil {
		// Fallback to userID for backward compatibility
		userUUIDVal = ctx.Value("userID")
	}

	var userUUID string
	var ok bool

	// If no user context (likely because auth middleware failed), try to extract from refresh token
	if userUUIDVal == nil {
		cookieName := h.config.Auth.Cookie.Name
		refreshToken, err := utils.GetRefreshTokenFromCookieByName(r, cookieName)
		if err != nil || refreshToken == "" {
			// Still clear the cookie even if we can't find it
			cookieDomain := h.config.Auth.Cookie.Domain
			isSecure := h.config.Auth.Cookie.Secure
			utils.ClearRefreshTokenCookie(w, cookieName, cookieDomain, isSecure)

			// Return success - user is effectively logged out
			w.Header().Set("Content-Type", "application/json")
			response := struct {
				Success bool   `json:"success"`
				Message string `json:"message"`
			}{
				Success: true,
				Message: "Successfully logged out",
			}
			json.NewEncoder(w).Encode(response)
			return
		}

		// Parse refresh token to get user UUID
		refreshClaims, err := h.jwtService.ParseUnverifiedClaims(refreshToken)
		if err != nil || refreshClaims.UserUUID == "" {
			// Still clear the cookie
			cookieDomain := h.config.Auth.Cookie.Domain
			isSecure := h.config.Auth.Cookie.Secure
			utils.ClearRefreshTokenCookie(w, cookieName, cookieDomain, isSecure)

			// Return success
			w.Header().Set("Content-Type", "application/json")
			response := struct {
				Success bool   `json:"success"`
				Message string `json:"message"`
			}{
				Success: true,
				Message: "Successfully logged out",
			}
			json.NewEncoder(w).Encode(response)
			return
		}

		userUUID = refreshClaims.UserUUID
	} else {
		userUUID, ok = userUUIDVal.(string)
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
	}

	// Parse request body for logout options
	var req LogoutRequest
	if r.Header.Get("Content-Type") == "application/json" {
		json.NewDecoder(r.Body).Decode(&req) // Ignore errors, use defaults
	}

	// Get refresh token from cookie to terminate specific session
	cookieName := h.config.Auth.Cookie.Name
	refreshToken, _ := utils.GetRefreshTokenFromCookieByName(r, cookieName)

	// Terminate sessions based on request
	if req.AllDevices {
		err := h.authService.TerminateAllSessionsByUUID(ctx, userUUID)
		if err != nil {
			logger.Error("Failed to terminate all sessions", slog.String("error", err.Error()))
			http.Error(w, "Failed to logout", http.StatusInternalServerError)
			return
		}
	} else {
		// Terminate current session based on refresh token or device ID
		if refreshToken != "" {
			// Parse refresh token to get device ID
			refreshClaims, err := h.jwtService.ParseUnverifiedClaims(refreshToken)
			if err == nil && refreshClaims.DeviceID != "" {
				h.authService.TerminateSessionByUUID(ctx, userUUID, refreshClaims.DeviceID)
			}
		} else if req.RefreshToken != "" {
			// Use refresh token from request body if provided
			refreshClaims, err := h.jwtService.ParseUnverifiedClaims(req.RefreshToken)
			if err == nil && refreshClaims.DeviceID != "" {
				h.authService.TerminateSessionByUUID(ctx, userUUID, refreshClaims.DeviceID)
			}
		}
	}

	// Clear the refresh token cookie
	cookieDomain := h.config.Auth.Cookie.Domain
	isSecure := h.config.Auth.Cookie.Secure
	utils.ClearRefreshTokenCookie(w, cookieName, cookieDomain, isSecure)

	// Return success response
	w.Header().Set("Content-Type", "application/json")
	response := struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}{
		Success: true,
		Message: "Successfully logged out",
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		logger.Error("Failed to encode response", slog.String("error", err.Error()))
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

// Logout handles user logout (Huma handler - deprecated, use LogoutHTTP instead)
func (h *AuthHandler) Logout(ctx context.Context, req *LogoutRequest) (*LogoutResponse, error) {
	// Get user from context
	userUUID := ctx.Value("userUUID").(string)

	if req.AllDevices {
		err := h.authService.TerminateAllSessionsByUUID(ctx, userUUID)
		if err != nil {
			return nil, huma.Error500InternalServerError("Failed to logout", err)
		}
	} else if req.RefreshToken != "" {
		// Terminate specific session
		claims, err := h.jwtService.ParseUnverifiedClaims(req.RefreshToken)
		if err == nil && claims.DeviceID != "" {
			err = h.authService.TerminateSessionByUUID(ctx, userUUID, claims.DeviceID)
			if err != nil {
				return nil, huma.Error500InternalServerError("Failed to logout", err)
			}
		}
	}

	return &LogoutResponse{
		Body: struct {
			Success bool   `json:"success"`
			Message string `json:"message"`
		}{
			Success: true,
			Message: "Successfully logged out",
		},
	}, nil
}

// GetCurrentUser Response
type GetCurrentUserResponse struct {
	Body CurrentUserResponse `json:"body"`
}

// CurrentUserResponse includes user data with OAuth providers
type CurrentUserResponse struct {
	userModels.UserManagementResponse
	OAuthProviders []models.OAuthProviderInfo `json:"oauthProviders,omitempty"`
}

// GetCurrentUser returns the current authenticated user
func (h *AuthHandler) GetCurrentUser(ctx context.Context, _ *struct{}) (*GetCurrentUserResponse, error) {
	userUUIDValue := ctx.Value("userUUID")
	if userUUIDValue == nil {
		return nil, huma.Error401Unauthorized("Authentication required", nil)
	}

	userUUID, ok := userUUIDValue.(string)
	if !ok {
		return nil, huma.Error401Unauthorized("Invalid authentication context", nil)
	}

	user, err := h.authService.GetUserByUUID(ctx, userUUID)
	if err != nil {
		return nil, huma.Error404NotFound("User not found", err)
	}

	// Fetch OAuth provider information
	oauthProviders, err := h.oauthProviderRepo.GetByUserUUID(ctx, userUUID)
	if err != nil {
		// OAuth providers are optional data - continue without them
		oauthProviders = []*models.OAuthProviderDoc{}
	}

	// Convert OAuth providers to response format
	oauthProvidersInfo := models.ConvertOAuthProvidersToInfo(oauthProviders)

	return &GetCurrentUserResponse{
		Body: CurrentUserResponse{
			UserManagementResponse: userModels.UserManagementResponse{
				ID:            user.UUID,
				Email:         user.Email,
				Username:      user.Username,
				FullName:      user.FullName,
				Avatar:        user.Avatar,
				Role:          user.Role,
				IsActive:      user.IsActive,
				EmailVerified: user.EmailVerified,
				LastLogin:     user.LastLogin,
				CreatedAt:     user.CreatedAt,
				UpdatedAt:     user.UpdatedAt,
			},
			OAuthProviders: oauthProvidersInfo,
		},
	}, nil
}

// RegisterRoutes registers all auth routes with the Huma API and Chi router
func (h *AuthHandler) RegisterRoutes(publicAPI huma.API, protectedAPI huma.API, router chi.Router, protectedRouter chi.Router) {
	// OAuth login initiation
	huma.Register(publicAPI, huma.Operation{
		OperationID: "initiate-oauth-login",
		Method:      http.MethodPost,
		Path:        "/v1/auth/oauth/login",
		Summary:     "Initiate OAuth login",
		Description: "Start the OAuth authentication flow for a specific provider",
		Tags:        []string{"Authentication"},
	}, h.InitiateOAuthLogin)

	// Mobile Google authentication endpoint
	huma.Register(publicAPI, huma.Operation{
		OperationID: "mobile-google-auth",
		Method:      http.MethodPost,
		Path:        "/v1/auth/google/mobile",
		Summary:     "Authenticate with Google from mobile app",
		Description: "Validate Google ID token from mobile app and return JWT tokens",
		Tags:        []string{"Authentication", "Mobile"},
	}, h.HandleMobileGoogleAuth)

	// Mobile Apple authentication endpoint
	huma.Register(publicAPI, huma.Operation{
		OperationID: "mobile-apple-auth",
		Method:      http.MethodPost,
		Path:        "/v1/auth/apple/mobile",
		Summary:     "Authenticate with Apple from mobile app",
		Description: "Validate Apple ID token from mobile app and return JWT tokens",
		Tags:        []string{"Authentication", "Mobile"},
	}, h.HandleMobileAppleAuth)

	// OAuth callbacks - use raw HTTP handlers for proper redirects
	router.Get("/v1/auth/oauth/google/callback", h.HandleGoogleCallbackHTTP)
	router.Get("/v1/auth/oauth/discord/callback", h.HandleDiscordCallbackHTTP)
	router.Post("/v1/auth/oauth/apple/callback", h.HandleAppleCallbackHTTP)

	// Token refresh - use raw HTTP handler for cookie support and custom headers
	router.Post("/v1/auth/refresh", h.RefreshTokensWithHeaderHTTP)
	router.Post("/v1/auth/refresh-cookie", h.RefreshTokensHTTP)

	// Session initialization for web clients after OAuth callback - use raw HTTP handler for cookies
	router.Get("/v1/auth/session", h.GetSessionHTTP)

	// OAuth callbacks (public) - GitHub still uses Huma for consistency with existing implementation

	huma.Register(publicAPI, huma.Operation{
		OperationID: "github-oauth-callback",
		Method:      http.MethodGet,
		Path:        "/v1/auth/oauth/github/callback",
		Summary:     "GitHub OAuth callback",
		Description: "Handle GitHub OAuth callback and exchange code for tokens",
		Tags:        []string{"Authentication"},
	}, h.HandleGitHubCallback)

	// Note: /v1/auth/refresh is now handled by raw HTTP handler above for custom headers

	// Logout - use raw HTTP handler for proper cookie clearing
	// Register on public router since logout can work with just refresh token cookie
	router.Post("/v1/auth/logout", h.LogoutHTTP)

	// Protected routes (require authentication)

	// Get current user
	huma.Register(protectedAPI, huma.Operation{
		OperationID: "get-current-user",
		Method:      http.MethodGet,
		Path:        "/v1/auth/me",
		Summary:     "Get current user",
		Description: "Get information about the currently authenticated user",
		Tags:        []string{"Authentication"},
		Security: []map[string][]string{
			{"bearerAuth": {}},
		},
	}, h.GetCurrentUser)
}
