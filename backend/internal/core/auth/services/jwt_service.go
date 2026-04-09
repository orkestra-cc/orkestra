package services

import (
	"crypto/rsa"
	"errors"
	"fmt"
	"time"

	"github.com/orkestra/backend/internal/core/auth/models"
	userModels "github.com/orkestra/backend/internal/core/user/models"
	"github.com/golang-jwt/jwt/v5"
)

var (
	ErrInvalidToken      = errors.New("invalid token")
	ErrTokenExpired      = errors.New("token has expired")
	ErrInvalidTokenType  = errors.New("invalid token type")
	ErrInvalidSigningKey = errors.New("invalid signing key")
	ErrJWTKeysNotLoaded  = errors.New("JWT keys not loaded - authentication is disabled")
)

type JWTService interface {
	// Check if JWT keys are loaded and service is functional
	IsEnabled() bool

	// Basic JWT interface compatibility
	GenerateAccessToken(user *userModels.User) (string, error)
	GenerateRefreshToken(user *userModels.User) (string, error)
	ValidateAccessToken(tokenString string) (*models.JWTClaims, error)
	ValidateRefreshToken(tokenString string) (*models.JWTClaims, error)
	ParseUnverifiedClaims(tokenString string) (*models.JWTClaims, error)

	// Token generation with enhanced security claims
	GenerateEnhancedAccessToken(user *userModels.User, deviceInfo *models.DeviceInfo, securityCtx *models.SecurityContext) (string, error)
	GenerateEnhancedRefreshToken(user *userModels.User, deviceInfo *models.DeviceInfo, securityCtx *models.SecurityContext) (string, error)

	// Token validation with risk assessment
	ValidateAccessTokenWithRisk(tokenString string) (*models.JWTClaims, error)
	ValidateRefreshTokenWithRisk(tokenString string) (*models.JWTClaims, error)

	// Token pair generation for complete authentication flow
	GenerateTokenPair(user *userModels.User, deviceInfo *models.DeviceInfo, securityCtx *models.SecurityContext) (*models.TokenPair, error)

	// Token refresh with rotation support
	RefreshTokensWithRotation(refreshToken string, deviceInfo *models.DeviceInfo) (*models.TokenPair, error)
}

type jwtService struct {
	privateKey    *rsa.PrivateKey
	publicKey     *rsa.PublicKey
	accessExpiry  time.Duration
	refreshExpiry time.Duration
	issuer        string
	audience      string
}

func NewJWTService(privateKey *rsa.PrivateKey, publicKey *rsa.PublicKey) JWTService {
	return &jwtService{
		privateKey:    privateKey,
		publicKey:     publicKey,
		accessExpiry:  15 * time.Minute,    // Short-lived access tokens
		refreshExpiry: 30 * 24 * time.Hour, // Long-lived refresh tokens
		issuer:        "orkestra",
		audience:      "orkestra-api",
	}
}

// IsEnabled returns true if JWT keys are loaded and the service can sign/verify tokens
func (s *jwtService) IsEnabled() bool {
	return s.privateKey != nil && s.publicKey != nil
}

func (s *jwtService) GenerateEnhancedAccessToken(
	user *userModels.User,
	deviceInfo *models.DeviceInfo,
	securityCtx *models.SecurityContext,
) (string, error) {
	if s.privateKey == nil {
		return "", ErrJWTKeysNotLoaded
	}
	now := time.Now()
	expiresAt := now.Add(s.accessExpiry)

	// Get primary OAuth provider if available
	primaryProvider := s.getPrimaryOAuthProvider(user)

	claims := &models.JWTClaims{
		// Standard JWT claims - Using UUID as subject
		UserUUID:  user.UUID,
		Email:     user.Email,
		Role:      user.Role,
		TokenType: "access",
		ExpiresAt: expiresAt.Unix(),
		IssuedAt:  now.Unix(),
		NotBefore: now.Unix(),
		Issuer:    s.issuer,
		Audience:  s.audience,

		// Security and session claims
		SessionID:   securityCtx.SessionID,
		DeviceID:    deviceInfo.DeviceID,
		IPAddress:   securityCtx.IPAddress,
		Fingerprint: deviceInfo.Fingerprint,
		RiskScore:   securityCtx.RiskScore,

		// OAuth provider information
		OAuthProvider: string(primaryProvider),

		// Enhanced capabilities
		Scope:        []string{"profile", "email", "api"},
		Capabilities: s.getCapabilitiesForRole(user.Role),
	}

	// Add permissions based on role
	claims.Permissions = s.getPermissionsForRole(user.Role)

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, s.claimsToMap(claims))

	tokenString, err := token.SignedString(s.privateKey)
	if err != nil {
		return "", fmt.Errorf("failed to sign enhanced access token: %w", err)
	}

	return tokenString, nil
}

func (s *jwtService) GenerateEnhancedRefreshToken(
	user *userModels.User,
	deviceInfo *models.DeviceInfo,
	securityCtx *models.SecurityContext,
) (string, error) {
	if s.privateKey == nil {
		return "", ErrJWTKeysNotLoaded
	}
	now := time.Now()
	expiresAt := now.Add(s.refreshExpiry)

	// Refresh tokens have minimal claims for security
	claims := &models.JWTClaims{
		UserUUID:    user.UUID,
		Email:       user.Email,
		TokenType:   "refresh",
		ExpiresAt:   expiresAt.Unix(),
		IssuedAt:    now.Unix(),
		Issuer:      s.issuer,
		SessionID:   securityCtx.SessionID,
		DeviceID:    deviceInfo.DeviceID,
		Fingerprint: deviceInfo.Fingerprint,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, s.claimsToMap(claims))

	tokenString, err := token.SignedString(s.privateKey)
	if err != nil {
		return "", fmt.Errorf("failed to sign enhanced refresh token: %w", err)
	}

	return tokenString, nil
}

func (s *jwtService) GenerateTokenPair(
	user *userModels.User,
	deviceInfo *models.DeviceInfo,
	securityCtx *models.SecurityContext,
) (*models.TokenPair, error) {
	accessToken, err := s.GenerateEnhancedAccessToken(user, deviceInfo, securityCtx)
	if err != nil {
		return nil, err
	}

	refreshToken, err := s.GenerateEnhancedRefreshToken(user, deviceInfo, securityCtx)
	if err != nil {
		return nil, err
	}

	return &models.TokenPair{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		TokenType:    "Bearer",
		ExpiresIn:    int64(s.accessExpiry.Seconds()),
		SessionID:    securityCtx.SessionID,
		DeviceID:     deviceInfo.DeviceID,
		Scope:        []string{"profile", "email", "api"},
		IssuedAt:     time.Now(),
		RefreshCount: 0,
	}, nil
}

func (s *jwtService) ValidateAccessTokenWithRisk(tokenString string) (*models.JWTClaims, error) {
	return s.validateTokenEnhanced(tokenString, "access")
}

func (s *jwtService) ValidateRefreshTokenWithRisk(tokenString string) (*models.JWTClaims, error) {
	return s.validateTokenEnhanced(tokenString, "refresh")
}

func (s *jwtService) validateTokenEnhanced(tokenString string, expectedType string) (*models.JWTClaims, error) {
	if s.publicKey == nil {
		return nil, ErrJWTKeysNotLoaded
	}
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return s.publicKey, nil
	})

	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrTokenExpired
		}
		return nil, ErrInvalidToken
	}

	if !token.Valid {
		return nil, ErrInvalidToken
	}

	mapClaims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, ErrInvalidToken
	}

	tokenType, _ := mapClaims["type"].(string)
	if tokenType != expectedType {
		return nil, ErrInvalidTokenType
	}

	issuer, _ := mapClaims["iss"].(string)
	if issuer != s.issuer {
		return nil, ErrInvalidToken
	}

	claims := s.mapToClaims(mapClaims)
	return claims, nil
}

func (s *jwtService) RefreshTokensWithRotation(refreshToken string, deviceInfo *models.DeviceInfo) (*models.TokenPair, error) {
	// This would be implemented with the repository to validate and rotate tokens
	// For now, returning an error to indicate it needs full implementation
	return nil, errors.New("refresh token rotation not yet implemented - requires repository integration")
}

func (s *jwtService) ParseUnverifiedClaims(tokenString string) (*models.JWTClaims, error) {
	token, _, err := jwt.NewParser().ParseUnverified(tokenString, jwt.MapClaims{})
	if err != nil {
		return nil, err
	}

	mapClaims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, ErrInvalidToken
	}

	return s.mapToClaims(mapClaims), nil
}

// Helper methods

func (s *jwtService) claimsToMap(claims *models.JWTClaims) jwt.MapClaims {
	m := jwt.MapClaims{
		"sub":   claims.UserUUID, // Using UUID as subject
		"email": claims.Email,
		"role":  claims.Role,
		"type":  claims.TokenType,
		"exp":   claims.ExpiresAt,
		"iat":   claims.IssuedAt,
		"iss":   claims.Issuer,
	}

	// Add optional fields only if they have values
	if claims.NotBefore > 0 {
		m["nbf"] = claims.NotBefore
	}
	if claims.Audience != "" {
		m["aud"] = claims.Audience
	}
	if claims.SessionID != "" {
		m["sid"] = claims.SessionID
	}
	if claims.DeviceID != "" {
		m["did"] = claims.DeviceID
	}
	if claims.IPAddress != "" {
		m["ip"] = claims.IPAddress
	}
	if claims.Fingerprint != "" {
		m["fp"] = claims.Fingerprint
	}
	if claims.RiskScore > 0 {
		m["risk"] = claims.RiskScore
	}
	if claims.OAuthProvider != "" {
		m["provider"] = claims.OAuthProvider
	}
	if len(claims.Scope) > 0 {
		m["scope"] = claims.Scope
	}
	if len(claims.Capabilities) > 0 {
		m["caps"] = claims.Capabilities
	}
	if len(claims.Permissions) > 0 {
		m["perms"] = claims.Permissions
	}
	if len(claims.Groups) > 0 {
		m["groups"] = claims.Groups
	}

	return m
}

func (s *jwtService) mapToClaims(m jwt.MapClaims) *models.JWTClaims {
	claims := &models.JWTClaims{
		UserUUID:      getStringClaim(m, "sub"),
		Email:         getStringClaim(m, "email"),
		Role:          getStringClaim(m, "role"),
		TokenType:     getStringClaim(m, "type"),
		ExpiresAt:     int64(getFloatClaim(m, "exp")),
		IssuedAt:      int64(getFloatClaim(m, "iat")),
		NotBefore:     int64(getFloatClaim(m, "nbf")),
		Issuer:        getStringClaim(m, "iss"),
		Audience:      getStringClaim(m, "aud"),
		SessionID:     getStringClaim(m, "sid"),
		DeviceID:      getStringClaim(m, "did"),
		IPAddress:     getStringClaim(m, "ip"),
		Fingerprint:   getStringClaim(m, "fp"),
		RiskScore:     getFloatClaim(m, "risk"),
		OAuthProvider: getStringClaim(m, "provider"),
	}

	// Handle array claims
	if scope, ok := m["scope"].([]interface{}); ok {
		claims.Scope = interfaceSliceToStringSlice(scope)
	}
	if caps, ok := m["caps"].([]interface{}); ok {
		claims.Capabilities = interfaceSliceToStringSlice(caps)
	}
	if perms, ok := m["perms"].([]interface{}); ok {
		claims.Permissions = interfaceSliceToStringSlice(perms)
	}
	if groups, ok := m["groups"].([]interface{}); ok {
		claims.Groups = interfaceSliceToStringSlice(groups)
	}

	return claims
}

func (s *jwtService) getPrimaryOAuthProvider(user *userModels.User) models.OAuthProvider {
	// Check new OAuth links structure first
	for _, link := range user.OAuthLinks {
		if link.IsPrimary && link.IsActive {
			return models.OAuthProvider(link.Provider)
		}
	}
	// Fall back to legacy provider field
	if user.OAuthProvider != "" {
		return models.OAuthProvider(user.OAuthProvider)
	}
	// Default to first active link if no primary is set
	for _, link := range user.OAuthLinks {
		if link.IsActive {
			return models.OAuthProvider(link.Provider)
		}
	}
	return ""
}

func (s *jwtService) getCapabilitiesForRole(role string) []string {
	capabilities := map[string][]string{
		"admin": {
			"users:manage",
			"system:configure",
			"reports:full",
			"tracking:manage",
			"tasks:manage",
			"operators:manage",
		},
		"manager": {
			"reports:full",
			"tracking:view",
			"tasks:manage",
			"operators:manage",
		},
		"operator": {
			"tracking:update",
			"tasks:update",
			"reports:self",
		},
		"viewer": {
			"tracking:view",
			"tasks:view",
			"reports:view",
		},
	}

	if caps, ok := capabilities[role]; ok {
		return caps
	}
	return []string{}
}

func (s *jwtService) getPermissionsForRole(role string) []string {
	permissions := map[string][]string{
		"admin": {
			"create", "read", "update", "delete", "manage",
		},
		"manager": {
			"create", "read", "update",
		},
		"operator": {
			"read", "update:self",
		},
		"viewer": {
			"read",
		},
	}

	if perms, ok := permissions[role]; ok {
		return perms
	}
	return []string{"read"}
}

func interfaceSliceToStringSlice(slice []interface{}) []string {
	result := make([]string, 0, len(slice))
	for _, v := range slice {
		if str, ok := v.(string); ok {
			result = append(result, str)
		}
	}
	return result
}

// Basic JWT interface implementation for compatibility
func (s *jwtService) GenerateAccessToken(user *userModels.User) (string, error) {
	// Use basic device info and security context for simple interface
	deviceInfo := &models.DeviceInfo{
		DeviceID:   "default",
		DeviceType: "unknown",
		Platform:   "api",
	}
	securityCtx := &models.SecurityContext{
		SessionID: fmt.Sprintf("session_%d", time.Now().Unix()),
		Timestamp: time.Now(),
	}
	return s.GenerateEnhancedAccessToken(user, deviceInfo, securityCtx)
}

func (s *jwtService) GenerateRefreshToken(user *userModels.User) (string, error) {
	// Use basic device info and security context for simple interface
	deviceInfo := &models.DeviceInfo{
		DeviceID:   "default",
		DeviceType: "unknown",
		Platform:   "api",
	}
	securityCtx := &models.SecurityContext{
		SessionID: fmt.Sprintf("session_%d", time.Now().Unix()),
		Timestamp: time.Now(),
	}
	return s.GenerateEnhancedRefreshToken(user, deviceInfo, securityCtx)
}

func (s *jwtService) ValidateAccessToken(tokenString string) (*models.JWTClaims, error) {
	return s.ValidateAccessTokenWithRisk(tokenString)
}

func (s *jwtService) ValidateRefreshToken(tokenString string) (*models.JWTClaims, error) {
	return s.ValidateRefreshTokenWithRisk(tokenString)
}

// Helper functions for claim extraction
func getStringClaim(claims jwt.MapClaims, key string) string {
	if val, ok := claims[key]; ok {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return ""
}

func getFloatClaim(claims jwt.MapClaims, key string) float64 {
	if val, ok := claims[key]; ok {
		if f, ok := val.(float64); ok {
			return f
		}
	}
	return 0
}
