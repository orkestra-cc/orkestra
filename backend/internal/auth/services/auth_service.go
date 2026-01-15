package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/orkestra/backend/internal/auth/models"
	"github.com/orkestra/backend/internal/auth/repository"
	"github.com/orkestra/backend/internal/shared/utils"
	userModels "github.com/orkestra/backend/internal/user/models"
	userServices "github.com/orkestra/backend/internal/user/services"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

var (
	ErrCannotRemoveLastOAuthLink = errors.New("cannot remove the last OAuth link")
	ErrOAuthLinkNotFound         = errors.New("OAuth link not found")
	ErrOAuthLinkAlreadyExists    = errors.New("OAuth link already exists")
	ErrUserMigrationRequired     = errors.New("user migration to UUID required")
	ErrInvalidRefreshToken       = errors.New("invalid refresh token")
)

// AuthService extends the basic auth service with UUID support and OAuth linking
type AuthService interface {
	// UUID-based user operations
	GetUserByUUID(ctx context.Context, uuid string) (*userModels.User, error)
	UpdateUserByUUID(ctx context.Context, uuid string, update *userModels.User) error
	UpdateLastLoginByUUID(ctx context.Context, uuid string) error
	DeleteUserByUUID(ctx context.Context, uuid string) error

	// OAuth Link Management
	AddOAuthLink(ctx context.Context, userUUID string, link models.LinkOAuthProviderInput) error
	RemoveOAuthLink(ctx context.Context, userUUID string, input models.UnlinkOAuthProviderInput) error
	SetPrimaryOAuthLink(ctx context.Context, userUUID string, input models.SetPrimaryOAuthProviderInput) error
	GetOAuthLinks(ctx context.Context, userUUID string) (*models.OAuthLinksResponse, error)

	// Account consolidation
	ConsolidateAccountsByEmail(ctx context.Context, email string) (*userModels.User, error)
	FindAccountsForConsolidation(ctx context.Context, email string) ([]*userModels.User, error)

	// Enhanced session management (UUID-based)
	GetUserSessionsByUUID(ctx context.Context, userUUID string) (*models.SessionsResponse, error)
	GetSessionCountByUUID(ctx context.Context, userUUID string) (int, error)
	RenameSessionByUUID(ctx context.Context, userUUID string, deviceID, deviceName string) error
	TerminateSessionByUUID(ctx context.Context, userUUID string, deviceID string) error
	TerminateAllSessionsByUUID(ctx context.Context, userUUID string) error

	// Enhanced token management with UUID and risk assessment
	GenerateEnhancedTokenPair(ctx context.Context, user *userModels.User, deviceInfo *models.DeviceInfo, securityCtx *models.SecurityContext) (*models.TokenResponse, error)
	RefreshTokensWithRiskAssessment(ctx context.Context, refreshToken string, securityCtx *models.SecurityContext) (*models.TokenResponse, error)
	ValidateTokenWithRiskAssessment(ctx context.Context, token string, securityCtx *models.SecurityContext) (*models.TokenValidationResult, error)

	// Risk assessment and security
	AssessLoginRisk(ctx context.Context, userUUID string, securityCtx *models.SecurityContext) (*models.RiskAssessment, error)
	RecordSecurityEvent(ctx context.Context, event *models.SecurityEvent) error
	GetSecurityEvents(ctx context.Context, userUUID string, limit int) ([]*models.SecurityEvent, error)

	// Migration utilities
	MigrateUserToUUID(ctx context.Context, userID primitive.ObjectID) (*userModels.User, error)
	MigrateAllUsersToUUID(ctx context.Context) (int, error)
	ConvertOAuthLinksToNewFormat(ctx context.Context, userUUID string) error

	// Enhanced OAuth callback handling with account linking
	HandleOAuthCallbackWithLinking(ctx context.Context, provider models.OAuthProvider, userInfo map[string]interface{}, oauthTokens *models.OAuthProviderTokens, securityCtx *models.SecurityContext, deviceInfo *models.DeviceInfo) (*models.TokenResponse, error)
}

// AuthConfig holds configuration for the auth service
type AuthConfig struct {
	AuthRepo          repository.AuthRepository
	UserService       userServices.UserService
	OAuthProviderRepo repository.OAuthProviderRepository
	RefreshTokenRepo  repository.RefreshTokenRepository
	AuthSessionRepo   repository.AuthSessionRepository
	JWTService        JWTService
}

type authService struct {
	authRepo          repository.AuthRepository
	userService       userServices.UserService
	oauthProviderRepo repository.OAuthProviderRepository
	refreshTokenRepo  repository.RefreshTokenRepository
	authSessionRepo   repository.AuthSessionRepository
	jwtService        JWTService
}

// NewAuthService creates a new auth service
func NewAuthService(config *AuthConfig) (AuthService, error) {
	return &authService{
		authRepo:          config.AuthRepo,
		userService:       config.UserService,
		oauthProviderRepo: config.OAuthProviderRepo,
		refreshTokenRepo:  config.RefreshTokenRepo,
		authSessionRepo:   config.AuthSessionRepo,
		jwtService:        config.JWTService,
	}, nil
}

// Placeholder implementations for now - to be fully implemented later

func (s *authService) GetUserByUUID(ctx context.Context, uuid string) (*userModels.User, error) {
	userModel, err := s.userService.GetUserByID(ctx, uuid)
	if err != nil {
		return nil, err
	}

	return userModel, nil
}

func (s *authService) UpdateUserByUUID(ctx context.Context, uuid string, update *userModels.User) error {
	// Convert user model to update input
	updateInput := &userModels.UpdateUserInput{
		FullName: update.FullName,
		Avatar:   update.Avatar,
		Role:     update.Role,
	}
	_, err := s.userService.UpdateUser(ctx, uuid, updateInput)
	return err
}

func (s *authService) UpdateLastLoginByUUID(ctx context.Context, uuid string) error {
	return s.userService.UpdateUserLastLogin(ctx, uuid)
}

func (s *authService) DeleteUserByUUID(ctx context.Context, uuid string) error {
	return s.userService.DeleteUser(ctx, uuid)
}

func (s *authService) AddOAuthLink(ctx context.Context, userUUID string, linkInput models.LinkOAuthProviderInput) error {
	// This method handles OAuth flow input to add a new link
	// TODO: Implement OAuth provider exchange to get provider ID and user info
	// For now, this is a placeholder that indicates the OAuth flow is not yet fully implemented
	return fmt.Errorf("OAuth link addition via OAuth flow not yet implemented - provider: %s", linkInput.Provider)
}

func (s *authService) RemoveOAuthLink(ctx context.Context, userUUID string, input models.UnlinkOAuthProviderInput) error {
	// Get user's OAuth links to find the one to remove
	links, err := s.userService.GetUserOAuthLinks(ctx, userUUID)
	if err != nil {
		return err
	}

	// Find the link for this provider
	var targetProviderID string
	for _, link := range links {
		if userModels.OAuthProvider(input.Provider) == link.Provider {
			targetProviderID = link.ProviderID
			break
		}
	}

	if targetProviderID == "" {
		return fmt.Errorf("OAuth link for provider %s not found", input.Provider)
	}

	return s.userService.RemoveOAuthLinkFromUser(ctx, userUUID, userModels.OAuthProvider(input.Provider), targetProviderID)
}

func (s *authService) SetPrimaryOAuthLink(ctx context.Context, userUUID string, input models.SetPrimaryOAuthProviderInput) error {
	// Get user's OAuth links to find the one to set as primary
	links, err := s.userService.GetUserOAuthLinks(ctx, userUUID)
	if err != nil {
		return err
	}

	// Find the link for this provider
	var targetProviderID string
	for _, link := range links {
		if userModels.OAuthProvider(input.Provider) == link.Provider {
			targetProviderID = link.ProviderID
			break
		}
	}

	if targetProviderID == "" {
		return fmt.Errorf("OAuth link for provider %s not found", input.Provider)
	}

	return s.userService.SetPrimaryOAuthLink(ctx, userUUID, userModels.OAuthProvider(input.Provider), targetProviderID)
}

func (s *authService) GetOAuthLinks(ctx context.Context, userUUID string) (*models.OAuthLinksResponse, error) {
	links, err := s.userService.GetUserOAuthLinks(ctx, userUUID)
	if err != nil {
		return nil, err
	}

	// Convert user OAuth links to auth OAuth links
	authLinks := make([]models.OAuthLink, len(links))
	for i, link := range links {
		authLinks[i] = models.OAuthLink{
			Provider:   models.OAuthProvider(link.Provider),
			ProviderID: link.ProviderID,
			Email:      link.Email,
			LinkedAt:   link.LinkedAt,
			IsActive:   link.IsActive,
			IsPrimary:  link.IsPrimary,
			OAuthData:  link.OAuthData,
			LastUsed:   link.LastUsed,
		}
	}

	return &models.OAuthLinksResponse{
		Links:       authLinks,
		CanUnlink:   len(authLinks) > 1, // Can only unlink if more than one link exists
		RequiresMFA: false,              // TODO: Implement MFA logic
	}, nil
}

func (s *authService) ConsolidateAccountsByEmail(ctx context.Context, email string) (*userModels.User, error) {
	userResponse, err := s.userService.GetUserByEmail(ctx, email)
	if err != nil {
		return nil, err
	}

	// Convert user response to auth model
	return convertUserResponseToAuthModel(userResponse), nil
}

func (s *authService) FindAccountsForConsolidation(ctx context.Context, email string) ([]*userModels.User, error) {
	userResponse, err := s.userService.GetUserByEmail(ctx, email)
	if err != nil {
		return []*userModels.User{}, nil
	}
	return []*userModels.User{convertUserResponseToAuthModel(userResponse)}, nil
}

func (s *authService) GetUserSessionsByUUID(ctx context.Context, userUUID string) (*models.SessionsResponse, error) {
	return &models.SessionsResponse{
		Sessions: []models.SessionInfo{},
	}, nil
}

func (s *authService) GetSessionCountByUUID(ctx context.Context, userUUID string) (int, error) {
	return 0, nil
}

func (s *authService) RenameSessionByUUID(ctx context.Context, userUUID string, deviceID, deviceName string) error {
	return fmt.Errorf("session management not yet implemented")
}

func (s *authService) TerminateSessionByUUID(ctx context.Context, userUUID string, deviceID string) error {
	return fmt.Errorf("session management not yet implemented")
}

func (s *authService) TerminateAllSessionsByUUID(ctx context.Context, userUUID string) error {
	return fmt.Errorf("session management not yet implemented")
}

func (s *authService) GenerateEnhancedTokenPair(ctx context.Context, user *userModels.User, deviceInfo *models.DeviceInfo, securityCtx *models.SecurityContext) (*models.TokenResponse, error) {
	fmt.Printf("[AUTH_DEBUG] ==> GenerateEnhancedTokenPair called for user: %s\n", user.UUID)
	fmt.Printf("[AUTH_DEBUG] Token creation source: OAuth Login Flow\n")
	fmt.Printf("[AUTH_DEBUG] Current active tokens for user before creation: querying database...\n")

	// Check current active tokens for debugging
	if existingTokens, err := s.refreshTokenRepo.GetActiveTokensByUser(ctx, user.UUID); err == nil {
		fmt.Printf("[AUTH_DEBUG] User currently has %d active refresh tokens\n", len(existingTokens))
		for i, token := range existingTokens {
			fmt.Printf("[AUTH_DEBUG]   Token %d: UUID=%s, DeviceID=%s, CreatedAt=%s\n",
				i+1, token.UUID, token.DeviceID, token.CreatedAt.Format("2006-01-02 15:04:05"))
		}
	} else {
		fmt.Printf("[AUTH_DEBUG] WARNING: Failed to query existing tokens: %v\n", err)
	}

	// Generate JWT tokens
	fmt.Printf("[AUTH_DEBUG] Generating JWT access token...\n")
	accessToken, err := s.jwtService.GenerateAccessToken(user)
	if err != nil {
		fmt.Printf("[AUTH_DEBUG] ERROR: Failed to generate access token: %v\n", err)
		return nil, err
	}
	fmt.Printf("[AUTH_DEBUG] Access token generated successfully (length: %d)\n", len(accessToken))

	fmt.Printf("[AUTH_DEBUG] Generating JWT refresh token...\n")
	refreshToken, err := s.jwtService.GenerateRefreshToken(user)
	if err != nil {
		fmt.Printf("[AUTH_DEBUG] ERROR: Failed to generate refresh token: %v\n", err)
		return nil, err
	}
	fmt.Printf("[AUTH_DEBUG] Refresh token generated successfully (length: %d)\n", len(refreshToken))

	sessionID := uuid.New().String()
	fmt.Printf("[AUTH_DEBUG] Generated session ID: %s\n", sessionID)

	// Create refresh token record in database for persistence and tracking
	now := time.Now()

	// Handle nil security context safely
	ipAddress := "unknown"
	if securityCtx != nil && securityCtx.IPAddress != "" {
		ipAddress = securityCtx.IPAddress
	}

	// Extract device information safely with fallbacks
	deviceID := "default-device"
	deviceType := "web"
	platform := "web"
	fingerprint := "default-fingerprint"

	if deviceInfo != nil {
		fmt.Printf("[AUTH_DEBUG] Device info found: ID=%s, Type=%s, Platform=%s, Fingerprint=%s\n",
			deviceInfo.DeviceID, deviceInfo.DeviceType, deviceInfo.Platform, deviceInfo.Fingerprint)

		// Use extracted device ID or fallback to default
		if deviceInfo.DeviceID != "" {
			deviceID = deviceInfo.DeviceID
		}

		// Use extracted device type or fallback to web
		if deviceInfo.DeviceType != "" {
			deviceType = deviceInfo.DeviceType
		}

		// Use extracted platform or fallback to web
		if deviceInfo.Platform != "" {
			platform = deviceInfo.Platform
		}

		// Use extracted fingerprint or fallback to default
		if deviceInfo.Fingerprint != "" {
			fingerprint = deviceInfo.Fingerprint
		}
	} else {
		fmt.Printf("[AUTH_DEBUG] No device info found, using defaults\n")
	}

	fmt.Printf("[AUTH_DEBUG] Final device values: ID=%s, Type=%s, Platform=%s, Fingerprint=%s\n",
		deviceID, deviceType, platform, fingerprint)

	refreshTokenRecord := &models.RefreshTokenDoc{
		UUID:         models.GenerateUUIDv7(),
		UserUUID:     user.UUID,
		Token:        refreshToken, // Repository layer will hash this for storage
		SessionUUID:  sessionID,
		DeviceID:     deviceID,
		DeviceType:   deviceType,
		Platform:     platform,
		Fingerprint:  fingerprint,
		IPAddress:    ipAddress,
		RiskScore:    0.1, // Low risk for OAuth
		IssuedAt:     now,
		ExpiresAt:    now.Add(7 * 24 * time.Hour), // 7 days
		LastActivity: now,
		IsRevoked:    false,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	// Store refresh token in database
	fmt.Printf("[AUTH_DEBUG] Storing refresh token in database...\n")
	err = s.refreshTokenRepo.CreateRefreshToken(ctx, refreshTokenRecord)
	if err != nil {
		fmt.Printf("[AUTH_DEBUG] ERROR: Failed to store refresh token: %v\n", err)
		return nil, fmt.Errorf("failed to store refresh token: %w", err)
	}
	fmt.Printf("[AUTH_DEBUG] Refresh token stored in database successfully\n")

	// Update user's last login timestamp
	fmt.Printf("[AUTH_DEBUG] Updating user's last login timestamp...\n")
	err = s.UpdateLastLoginByUUID(ctx, user.UUID)
	if err != nil {
		// Log error but don't fail the authentication
		fmt.Printf("[AUTH_DEBUG] WARNING: Failed to update last login timestamp: %v\n", err)
	} else {
		fmt.Printf("[AUTH_DEBUG] Last login timestamp updated successfully\n")
	}

	fmt.Printf("[AUTH_DEBUG] Creating token response...\n")

	// Fetch OAuth provider information for complete user data (similar to /auth/me endpoint)
	oauthProviders, err := s.oauthProviderRepo.GetByUserUUID(ctx, user.UUID)
	if err != nil {
		// Log the error but don't fail the request - OAuth providers are optional data
		fmt.Printf("[AUTH_DEBUG] Warning: Failed to fetch OAuth providers for user %s: %v\n", user.UUID, err)
		oauthProviders = []*models.OAuthProviderDoc{}
	}

	// Convert OAuth providers to response format
	oauthProvidersInfo := models.ConvertOAuthProvidersToInfo(oauthProviders)

	// Create user response
	userResponse := user.ToResponse()

	tokenResponse := &models.TokenResponse{
		AccessToken:    accessToken,
		RefreshToken:   refreshToken,
		TokenType:      "Bearer",
		ExpiresIn:      900, // 15 minutes
		SessionID:      sessionID,
		DeviceID:       deviceInfo.DeviceID,
		User:           userResponse,
		OAuthProviders: oauthProvidersInfo,
	}

	fmt.Printf("[AUTH_DEBUG] Token response created - Session ID: %s, Device ID: %s\n", sessionID, deviceInfo.DeviceID)
	fmt.Printf("[AUTH_DEBUG] <== GenerateEnhancedTokenPair completed successfully\n")
	return tokenResponse, nil
}

func (s *authService) RefreshTokensWithRiskAssessment(ctx context.Context, refreshToken string, securityCtx *models.SecurityContext) (*models.TokenResponse, error) {
	fmt.Printf("[AUTH_DEBUG] ==> RefreshTokensWithRiskAssessment called\n")
	fmt.Printf("[AUTH_DEBUG] Token creation source: Token Refresh Flow\n")

	// Validate JWT structure and extract claims
	claims, err := s.jwtService.ValidateRefreshToken(refreshToken)
	if err != nil {
		fmt.Printf("[AUTH_DEBUG] ERROR: Invalid refresh token: %v\n", err)
		return nil, fmt.Errorf("invalid refresh token: %w", err)
	}
	fmt.Printf("[AUTH_DEBUG] Refresh token validated for user: %s\n", claims.UserUUID)

	// Hash the incoming token to check against database (repository stores tokens hashed)
	hashedToken := utils.HashRefreshToken(refreshToken)

	// Validate token exists in database and is not revoked
	tokenDoc, err := s.refreshTokenRepo.GetByToken(ctx, hashedToken)
	if err != nil {
		return nil, fmt.Errorf("failed to validate refresh token: %w", err)
	}
	if tokenDoc == nil {
		return nil, fmt.Errorf("refresh token not found or expired")
	}

	// Get user information
	userModel, err := s.userService.GetUserByID(ctx, claims.UserUUID)
	if err != nil {
		return nil, fmt.Errorf("user not found: %w", err)
	}
	user := convertUserModelToAuthModel(userModel)

	// Update last activity
	err = s.refreshTokenRepo.UpdateLastActivity(ctx, tokenDoc.UUID)
	if err != nil {
		// Log warning but don't fail the refresh
		fmt.Printf("[AUTH_WARNING] Failed to update last activity: %v\n", err)
	}

	// Generate new token pair
	deviceInfo := &models.DeviceInfo{
		DeviceID:    tokenDoc.DeviceID,
		DeviceType:  tokenDoc.DeviceType,
		Platform:    tokenDoc.Platform,
		Fingerprint: tokenDoc.Fingerprint,
	}

	newTokenResponse, err := s.GenerateEnhancedTokenPair(ctx, user, deviceInfo, securityCtx)
	if err != nil {
		return nil, fmt.Errorf("failed to generate new token pair: %w", err)
	}

	// Implement token rotation: revoke the old token using hashed value (as stored in DB)
	err = s.refreshTokenRepo.RevokeToken(ctx, hashedToken, "token_rotation")
	if err != nil {
		// Log warning but don't fail the refresh since new tokens are already generated
		fmt.Printf("[AUTH_WARNING] Failed to revoke old refresh token during rotation: %v\n", err)
	}

	return newTokenResponse, nil
}

func (s *authService) ValidateTokenWithRiskAssessment(ctx context.Context, token string, securityCtx *models.SecurityContext) (*models.TokenValidationResult, error) {
	claims, err := s.jwtService.ValidateAccessToken(token)
	if err != nil {
		return &models.TokenValidationResult{
			Valid: false,
			Error: err,
		}, err
	}

	return &models.TokenValidationResult{
		Valid:     true,
		Claims:    claims,
		ExpiresAt: time.Unix(claims.ExpiresAt, 0),
		RiskScore: 0.0,
	}, nil
}

func (s *authService) AssessLoginRisk(ctx context.Context, userUUID string, securityCtx *models.SecurityContext) (*models.RiskAssessment, error) {
	return &models.RiskAssessment{
		Score:      0.0,
		Level:      "low",
		Factors:    []models.RiskFactor{},
		AssessedAt: time.Now(),
	}, nil
}

func (s *authService) RecordSecurityEvent(ctx context.Context, event *models.SecurityEvent) error {
	return nil // Placeholder
}

func (s *authService) GetSecurityEvents(ctx context.Context, userUUID string, limit int) ([]*models.SecurityEvent, error) {
	return []*models.SecurityEvent{}, nil
}

func (s *authService) MigrateUserToUUID(ctx context.Context, userID primitive.ObjectID) (*userModels.User, error) {
	return nil, fmt.Errorf("migration not yet implemented")
}

func (s *authService) MigrateAllUsersToUUID(ctx context.Context) (int, error) {
	return 0, fmt.Errorf("migration not yet implemented")
}

func (s *authService) ConvertOAuthLinksToNewFormat(ctx context.Context, userUUID string) error {
	return fmt.Errorf("OAuth link conversion not yet implemented")
}

func (s *authService) HandleOAuthCallbackWithLinking(ctx context.Context, provider models.OAuthProvider, userInfo map[string]interface{}, oauthTokens *models.OAuthProviderTokens, securityCtx *models.SecurityContext, deviceInfo *models.DeviceInfo) (*models.TokenResponse, error) {
	fmt.Printf("[AUTH_DEBUG] ==> HandleOAuthCallbackWithLinking called\n")
	fmt.Printf("[AUTH_DEBUG] Provider: %s\n", provider)

	// First extract email for validation and later use
	email, _ := userInfo["email"].(string)
	fmt.Printf("[AUTH_DEBUG] Extracted email from user info: %s\n", email)
	if email == "" {
		fmt.Printf("[AUTH_DEBUG] ERROR: Email is empty\n")
		return nil, errors.New("email required from OAuth provider")
	}

	// Extract provider-specific user ID FIRST to check for existing provider
	providerID, _ := userInfo["provider_id"].(string)
	if providerID == "" {
		// Fallback to sub claim for OpenID Connect providers
		providerID, _ = userInfo["sub"].(string)
	}
	fmt.Printf("[AUTH_DEBUG] Provider ID extracted: %s\n", providerID)

	// Check if this OAuth provider is already linked
	fmt.Printf("[AUTH_DEBUG] Checking for existing OAuth provider link...\n")
	fmt.Printf("[AUTH_DEBUG] Query: Provider=%s, ProviderID=%s\n", provider, providerID)

	existingProvider, err := s.oauthProviderRepo.GetByProviderAndID(ctx, provider, providerID)
	if err != nil && err.Error() != "failed to find OAuth provider: mongo: no documents in result" {
		fmt.Printf("[AUTH_DEBUG] ERROR: Failed to check existing provider: %v\n", err)
		// Don't fail the auth, just log the error
	}

	var user *userModels.User

	if existingProvider != nil {
		// Provider exists - fetch the user by UserUUID from the provider record
		fmt.Printf("[AUTH_DEBUG] OAuth provider already linked\n")
		fmt.Printf("[AUTH_DEBUG] Existing link - UUID: %s, UserUUID: %s, Primary: %v\n",
			existingProvider.UUID, existingProvider.UserUUID, existingProvider.IsPrimary)

		// CRITICAL FIX: Fetch the actual user using the provider's UserUUID
		fmt.Printf("[AUTH_DEBUG] Fetching user by UUID: %s\n", existingProvider.UserUUID)
		userModel, err := s.userService.GetUserByID(ctx, existingProvider.UserUUID)
		if err != nil {
			fmt.Printf("[AUTH_DEBUG] ERROR: Failed to get user for existing provider: %v\n", err)
			return nil, fmt.Errorf("failed to get user for existing provider: %w", err)
		}
		user = userModel
		fmt.Printf("[AUTH_DEBUG] User fetched successfully - UUID: %s, Email: %s\n", user.UUID, user.Email)
	} else {
		// No existing provider - check if user exists by email
		fmt.Printf("[AUTH_DEBUG] No existing provider link found, checking for user by email: %s\n", email)
		userResponse, err := s.userService.GetUserByEmail(ctx, email)
		if err != nil {
			fmt.Printf("[AUTH_DEBUG] User not found in database, creating new user\n")
			// Create new user via UserService
			newUUID := models.GenerateUUIDv7()
			fmt.Printf("[AUTH_DEBUG] Generated new UUID for user: %s\n", newUUID)

			createInput := &userModels.CreateUserInput{
				Email:    email,
				FullName: userInfo["name"].(string),
				Role:     "user",
			}
			fmt.Printf("[AUTH_DEBUG] Creating new user - Name: %s, Role: %s\n", createInput.FullName, createInput.Role)

			userModel, err := s.userService.CreateUserFromOAuth(ctx, createInput)
			if err != nil {
				fmt.Printf("[AUTH_DEBUG] ERROR: Failed to create user: %v\n", err)
				return nil, fmt.Errorf("failed to create user: %w", err)
			}
			user = convertUserModelToAuthModel(userModel)
			fmt.Printf("[AUTH_DEBUG] New user created successfully with UUID: %s\n", user.UUID)
		} else {
			user = convertUserResponseToAuthModel(userResponse)
			fmt.Printf("[AUTH_DEBUG] Existing user found with UUID: %s\n", user.UUID)
		}
	}

	// Link OAuth provider (if not already linked)
	fmt.Printf("[AUTH_DEBUG] ==> Starting OAuth provider linking process\n")

	if existingProvider != nil {
		fmt.Printf("[AUTH_DEBUG] OAuth provider already linked\n")
		fmt.Printf("[AUTH_DEBUG] Existing link - UUID: %s, UserUUID: %s, Primary: %v\n",
			existingProvider.UUID, existingProvider.UserUUID, existingProvider.IsPrimary)

		// Update OAuth tokens if provided
		if oauthTokens != nil {
			fmt.Printf("[AUTH_DEBUG] Updating OAuth tokens for existing provider\n")

			var encryptedAccessToken, encryptedRefreshToken string
			var accessTokenExpiresAt, refreshTokenExpiresAt *time.Time
			var tokenScopes []string

			if oauthTokens.AccessToken != "" {
				encryptedAccessToken, err = utils.EncryptOAuthToken(oauthTokens.AccessToken)
				if err != nil {
					fmt.Printf("[AUTH_DEBUG] WARNING: Failed to encrypt access token: %v\n", err)
				} else {
					if oauthTokens.ExpiresIn > 0 {
						expiresAt := time.Now().Add(time.Duration(oauthTokens.ExpiresIn) * time.Second)
						accessTokenExpiresAt = &expiresAt
					}
				}
			}

			if oauthTokens.RefreshToken != "" {
				encryptedRefreshToken, err = utils.EncryptOAuthToken(oauthTokens.RefreshToken)
				if err != nil {
					fmt.Printf("[AUTH_DEBUG] WARNING: Failed to encrypt refresh token: %v\n", err)
				} else {
					if oauthTokens.RefreshTokenExpiresIn > 0 {
						expiresAt := time.Now().Add(time.Duration(oauthTokens.RefreshTokenExpiresIn) * time.Second)
						refreshTokenExpiresAt = &expiresAt
					}
				}
			}

			tokenScopes = oauthTokens.Scopes

			// Update existing provider with new tokens
			existingProvider.AccessToken = encryptedAccessToken
			existingProvider.RefreshToken = encryptedRefreshToken
			existingProvider.AccessTokenExpiresAt = accessTokenExpiresAt
			existingProvider.RefreshTokenExpiresAt = refreshTokenExpiresAt
			existingProvider.TokenStatus = "active"
			existingProvider.LastTokenRefresh = &time.Time{}
			*existingProvider.LastTokenRefresh = time.Now()
			existingProvider.Scopes = tokenScopes
			existingProvider.UpdatedAt = time.Now()

			// Update tokens in database
			err = s.oauthProviderRepo.UpdateOAuthTokens(ctx, existingProvider.UUID,
				encryptedAccessToken, encryptedRefreshToken,
				accessTokenExpiresAt, refreshTokenExpiresAt, tokenScopes)
			if err != nil {
				fmt.Printf("[AUTH_DEBUG] WARNING: Failed to update OAuth tokens in database: %v\n", err)
			} else {
				fmt.Printf("[AUTH_DEBUG] OAuth tokens updated successfully - Access: %v, Refresh: %v\n",
					encryptedAccessToken != "", encryptedRefreshToken != "")
			}
		}

		// Update last used timestamp
		fmt.Printf("[AUTH_DEBUG] Updating last used timestamp for provider\n")
		err = s.oauthProviderRepo.UpdateLastUsed(ctx, existingProvider.UUID)
		if err != nil {
			fmt.Printf("[AUTH_DEBUG] WARNING: Failed to update last used: %v\n", err)
		}
	} else {
		fmt.Printf("[AUTH_DEBUG] No existing provider link found, creating new link\n")

		// Check if this is the first provider for the user
		userProviders, err := s.oauthProviderRepo.GetByUserUUID(ctx, user.UUID)
		isPrimary := len(userProviders) == 0
		fmt.Printf("[AUTH_DEBUG] This will be primary provider: %v (user has %d existing providers)\n", isPrimary, len(userProviders))

		// Extract additional provider metadata
		picture, _ := userInfo["picture"].(string)
		locale, _ := userInfo["locale"].(string)

		metadata := make(map[string]interface{})
		if picture != "" {
			metadata["picture"] = picture
		}
		if locale != "" {
			metadata["locale"] = locale
		}

		// Encrypt OAuth tokens if provided
		var encryptedAccessToken, encryptedRefreshToken string
		var accessTokenExpiresAt, refreshTokenExpiresAt *time.Time
		var tokenStatus string = "active"
		var tokenScopes []string

		if oauthTokens != nil {
			if oauthTokens.AccessToken != "" {
				var err error
				encryptedAccessToken, err = utils.EncryptOAuthToken(oauthTokens.AccessToken)
				if err != nil {
					fmt.Printf("[AUTH_DEBUG] WARNING: Failed to encrypt access token: %v\n", err)
					// Continue without storing access token rather than failing auth
				} else {
					fmt.Printf("[AUTH_DEBUG] Access token encrypted successfully\n")
					if oauthTokens.ExpiresIn > 0 {
						expiresAt := time.Now().Add(time.Duration(oauthTokens.ExpiresIn) * time.Second)
						accessTokenExpiresAt = &expiresAt
					}
				}
			}

			if oauthTokens.RefreshToken != "" {
				var err error
				encryptedRefreshToken, err = utils.EncryptOAuthToken(oauthTokens.RefreshToken)
				if err != nil {
					fmt.Printf("[AUTH_DEBUG] WARNING: Failed to encrypt refresh token: %v\n", err)
					// Continue without storing refresh token rather than failing auth
				} else {
					fmt.Printf("[AUTH_DEBUG] Refresh token encrypted successfully\n")
					if oauthTokens.RefreshTokenExpiresIn > 0 {
						expiresAt := time.Now().Add(time.Duration(oauthTokens.RefreshTokenExpiresIn) * time.Second)
						refreshTokenExpiresAt = &expiresAt
					}
				}
			}

			tokenScopes = oauthTokens.Scopes
			fmt.Printf("[AUTH_DEBUG] OAuth tokens prepared - Access: %v, Refresh: %v, Scopes: %v\n",
				encryptedAccessToken != "", encryptedRefreshToken != "", tokenScopes)
		}

		// Create new OAuth provider link with encrypted tokens
		newProvider := &models.OAuthProviderDoc{
			UUID:                  models.GenerateUUIDv7(),
			UserUUID:              user.UUID,
			Provider:              provider,
			ProviderID:            providerID,
			Email:                 email,
			IsPrimary:             isPrimary,
			LinkedAt:              time.Now(),
			AccessToken:           encryptedAccessToken,
			RefreshToken:          encryptedRefreshToken,
			AccessTokenExpiresAt:  accessTokenExpiresAt,
			RefreshTokenExpiresAt: refreshTokenExpiresAt,
			TokenStatus:           tokenStatus,
			Scopes:                tokenScopes,
			Metadata:              metadata,
			CreatedAt:             time.Now(),
			UpdatedAt:             time.Now(),
		}

		fmt.Printf("[AUTH_DEBUG] Creating OAuth provider link...\n")
		fmt.Printf("[AUTH_DEBUG] Provider details - UUID: %s, UserUUID: %s, Provider: %s, Primary: %v\n",
			newProvider.UUID, newProvider.UserUUID, newProvider.Provider, newProvider.IsPrimary)

		err = s.oauthProviderRepo.CreateOAuthProvider(ctx, newProvider)
		if err != nil {
			fmt.Printf("[AUTH_DEBUG] ERROR: Failed to create OAuth provider link: %v\n", err)
			// Don't fail the auth, continue without provider linking
		} else {
			fmt.Printf("[AUTH_DEBUG] OAuth provider linked successfully - UUID: %s\n", newProvider.UUID)
			fmt.Printf("[AUTH_DEBUG] Provider metadata saved: %v\n", metadata)
		}
	}

	fmt.Printf("[AUTH_DEBUG] <== OAuth provider linking process completed\n")

	// Use provided device info or create defaults
	if deviceInfo != nil {
		fmt.Printf("[AUTH_DEBUG] Using device info from OAuth state - DeviceID: %s, Fingerprint: %s\n",
			deviceInfo.DeviceID, deviceInfo.Fingerprint)
	} else {
		// Fallback to creating minimal device info with a new device ID
		deviceInfo = &models.DeviceInfo{
			DeviceID: uuid.New().String(),
		}
		fmt.Printf("[AUTH_DEBUG] No device info provided, generated device ID: %s\n", deviceInfo.DeviceID)
	}

	fmt.Printf("[AUTH_DEBUG] Calling GenerateEnhancedTokenPair to create JWT tokens\n")
	tokenResponse, err := s.GenerateEnhancedTokenPair(ctx, user, deviceInfo, securityCtx)
	if err != nil {
		fmt.Printf("[AUTH_DEBUG] ERROR: Failed to generate token pair: %v\n", err)
		return nil, err
	}
	fmt.Printf("[AUTH_DEBUG] <== HandleOAuthCallbackWithLinking completed successfully\n")
	return tokenResponse, nil
}

// Helper functions for type conversion between user and auth models

// convertUserModelToAuthModel is deprecated - we now use userModels.User directly
// This function just returns the same user model
func convertUserModelToAuthModel(userModel *userModels.User) *userModels.User {
	return userModel
}

// convertUserResponseToAuthModel converts a UserManagementResponse to user.User
func convertUserResponseToAuthModel(userResponse *userModels.UserManagementResponse) *userModels.User {
	if userResponse == nil {
		return nil
	}

	user := &userModels.User{
		UUID:          userResponse.ID,
		Email:         userResponse.Email,
		Username:      userResponse.Username,
		FullName:      userResponse.FullName,
		Avatar:        userResponse.Avatar,
		Role:          userResponse.Role,
		IsActive:      userResponse.IsActive,
		EmailVerified: userResponse.EmailVerified,
		LastLogin:     userResponse.LastLogin,
		CreatedAt:     userResponse.CreatedAt,
		UpdatedAt:     userResponse.UpdatedAt,
	}

	// OAuth links are not included in UserManagementResponse
	// They would need to be fetched separately if needed

	return user
}
