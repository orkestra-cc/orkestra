package services

import (
	"context"
	"crypto/rsa"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/orkestra/backend/internal/core/auth/models"
	userModels "github.com/orkestra/backend/internal/core/user/models"
	"github.com/orkestra/backend/internal/shared/iface"
)

var (
	ErrInvalidToken      = errors.New("invalid token")
	ErrTokenExpired      = errors.New("token has expired")
	ErrInvalidTokenType  = errors.New("invalid token type")
	ErrInvalidSigningKey = errors.New("invalid signing key")
	ErrJWTKeysNotLoaded  = errors.New("JWT keys not loaded - authentication is disabled")
	// ErrMissingAudience signals a JWT v1 token (no `aud` claim). After
	// ADR-0003 PR-D D-3's hard cutover the audience claim is mandatory
	// at issuance, so any v1 token presented to the validator is rejected
	// here. Surfaces to the boundary the same way as ErrInvalidToken.
	ErrMissingAudience = errors.New("token is missing aud claim")
)

type JWTService interface {
	IsEnabled() bool

	GenerateAccessToken(user *userModels.User) (string, error)
	GenerateRefreshToken(user *userModels.User) (string, error)
	// GenerateAccessTokenWithAMR mints an access token with explicit authn
	// methods (RFC 8176) and an optional last-OTP timestamp. Used by the MFA
	// verify endpoint and password/OAuth login paths to record how the user
	// proved their identity on this request.
	GenerateAccessTokenWithAMR(user *userModels.User, amr []string, lastOTPAt int64) (string, error)
	ValidateAccessToken(tokenString string) (*models.JWTClaims, error)
	ValidateRefreshToken(tokenString string) (*models.JWTClaims, error)
	ParseUnverifiedClaims(tokenString string) (*models.JWTClaims, error)

	GenerateEnhancedAccessToken(user *userModels.User, deviceInfo *models.DeviceInfo, securityCtx *models.SecurityContext) (string, error)
	GenerateEnhancedRefreshToken(user *userModels.User, deviceInfo *models.DeviceInfo, securityCtx *models.SecurityContext) (string, error)

	ValidateAccessTokenWithRisk(tokenString string) (*models.JWTClaims, error)
	ValidateRefreshTokenWithRisk(tokenString string) (*models.JWTClaims, error)

	GenerateTokenPair(user *userModels.User, deviceInfo *models.DeviceInfo, securityCtx *models.SecurityContext) (*models.TokenPair, error)
	// GenerateTokenPairWithAMR is identical to GenerateTokenPair but stamps
	// the access token's amr + last_otp_at claims. Used by the MFA login
	// verify endpoint and anywhere else that needs to record which factors
	// were completed for a freshly minted session.
	GenerateTokenPairWithAMR(user *userModels.User, deviceInfo *models.DeviceInfo, securityCtx *models.SecurityContext, amr []string, lastOTPAt int64) (*models.TokenPair, error)

	// SetTenantProvider allows late wiring of the tenant provider so that
	// token issuance can embed the user's current memberships in the JWT.
	// Called by the auth module after the tenant module has initialized.
	SetTenantProvider(tp iface.TenantProvider)
}

type jwtService struct {
	privateKey    *rsa.PrivateKey
	publicKey     *rsa.PublicKey
	accessExpiry  time.Duration
	refreshExpiry time.Duration
	issuer        string
	audience      string
	tenant        iface.TenantProvider
}

// NewJWTService builds a JWT issuer/validator with an environment-stamped
// issuer claim. `env` is the deployment environment (e.g. "production",
// "staging", "development"); tokens are minted with iss=`orkestra.<env>` and
// validation rejects any other value. This prevents a token signed in one
// environment from being accepted by another even if the signing keys were
// accidentally shared (or leaked across deployments).
//
// accessTTL/refreshTTL are sourced from cfg.Auth.JWT (env vars
// JWT_ACCESS_TOKEN_EXPIRY / JWT_REFRESH_TOKEN_EXPIRY). Zero or negative
// values fall back to safe defaults so unit tests and any future caller
// that doesn't care about TTL don't need to pass anything explicit.
func NewJWTService(privateKey *rsa.PrivateKey, publicKey *rsa.PublicKey, env string, accessTTL, refreshTTL time.Duration) JWTService {
	if accessTTL <= 0 {
		accessTTL = 15 * time.Minute
	}
	if refreshTTL <= 0 {
		refreshTTL = 30 * 24 * time.Hour
	}
	return &jwtService{
		privateKey:    privateKey,
		publicKey:     publicKey,
		accessExpiry:  accessTTL,
		refreshExpiry: refreshTTL,
		issuer:        issuerFor(env),
		// ADR-0003 PR-D D-3: every monolith-issued token carries an
		// audience claim (mandatory after the v2 cutover). The default
		// here is operator; D-4/D-5 will let per-tier login paths
		// override via NewJWTServiceWithAudience so client tokens
		// are stamped aud=client.
		audience: AudienceOperator,
	}
}

// NewJWTServiceWithAudience constructs a JWT service bound to a specific
// audience. ADR-0003 PR-D's auth-path split (D-4 operator, D-5 client)
// uses this so the matching login flow stamps the right `aud` value on
// every minted access + refresh token. Empty audience is rejected — the
// post-cutover invariant is that no monolith-issued token leaves the
// boundary without a known audience.
func NewJWTServiceWithAudience(privateKey *rsa.PrivateKey, publicKey *rsa.PublicKey, env, audience string, accessTTL, refreshTTL time.Duration) (JWTService, error) {
	if audience == "" {
		return nil, fmt.Errorf("jwt service: audience is required (ADR-0003 PR-D)")
	}
	svc := NewJWTService(privateKey, publicKey, env, accessTTL, refreshTTL).(*jwtService)
	svc.audience = audience
	return svc, nil
}

// Audience constants stamped on monolith-issued tokens. Mirror
// shared/module.Audience{Operator,Client,Service}; defined locally to
// avoid the auth services package depending on the module registry
// (which depends on iface, which the auth services already implement —
// circular import).
const (
	AudienceOperator = "operator"
	AudienceClient   = "client"
	AudienceService  = "service"
)

// LegacyAudienceOperator preserves the pre-PR-D constant name for any
// remaining external callers; new code should use AudienceOperator.
//
// Deprecated: use AudienceOperator. Kept as an alias so the PR-D
// commit's diff stays scoped to the cutover.
const LegacyAudienceOperator = AudienceOperator

// issuerFor produces the canonical iss claim value for a given environment.
// An empty env is normalised to "development" so local dev and test code
// paths produce consistent tokens.
func issuerFor(env string) string {
	if env == "" {
		env = "development"
	}
	return "orkestra." + env
}

func (s *jwtService) SetTenantProvider(tp iface.TenantProvider) {
	s.tenant = tp
}

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

	primaryProvider := s.getPrimaryOAuthProvider(user)

	memberships, defaultTenant, defaultKind := s.loadMemberships(user.UUID)

	claims := &models.JWTClaims{
		UserUUID:   user.UUID,
		Email:      user.Email,
		SystemRole: user.Role,
		TokenType:  "access",
		ExpiresAt:  expiresAt.Unix(),
		IssuedAt:   now.Unix(),
		NotBefore:  now.Unix(),
		Issuer:     s.issuer,
		Audience:   s.audience,

		Memberships:      memberships,
		DefaultTenantID:  defaultTenant,
		ActingTenantID:   defaultTenant,
		ActingTenantKind: defaultKind,

		SessionID:     securityCtx.SessionID,
		DeviceID:      deviceInfo.DeviceID,
		IPAddress:     securityCtx.IPAddress,
		Fingerprint:   deviceInfo.Fingerprint,
		RiskScore:     securityCtx.RiskScore,
		OAuthProvider: string(primaryProvider),
		Scope:         []string{"profile", "email", "api"},

		AMR:       securityCtx.AMR,
		LastOTPAt: securityCtx.LastOTPAt,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, s.claimsToMap(claims))

	tokenString, err := token.SignedString(s.privateKey)
	if err != nil {
		return "", fmt.Errorf("failed to sign enhanced access token: %w", err)
	}

	return tokenString, nil
}

// loadMemberships fetches the user's tenant memberships via the tenant
// provider. Returns an empty list if the provider isn't wired yet (edge case
// during startup) or if the user belongs to no tenants. DefaultTenantID
// picks the first owned tenant, otherwise the first membership. Also returns
// the default tenant kind so middleware can dispatch on tier without
// re-reading the provider. See ADR-0001.
func (s *jwtService) loadMemberships(userUUID string) (list []models.TenantMembership, defaultTenant, defaultKind string) {
	if s.tenant == nil {
		return nil, "", ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	mbrs, err := s.tenant.ListUserMemberships(ctx, userUUID)
	if err != nil || len(mbrs) == 0 {
		return nil, "", ""
	}
	out := make([]models.TenantMembership, 0, len(mbrs))
	var firstOwned, firstOwnedKind string
	for _, m := range mbrs {
		kind := m.TenantKind
		if kind == "" {
			kind = "internal"
		}
		out = append(out, models.TenantMembership{
			TenantUUID: m.TenantUUID,
			TenantKind: kind,
			Roles:      m.Roles,
		})
		if m.IsOwner && firstOwned == "" {
			firstOwned = m.TenantUUID
			firstOwnedKind = kind
		}
		if defaultTenant == "" {
			defaultTenant = m.TenantUUID
			defaultKind = kind
		}
	}
	if firstOwned != "" {
		defaultTenant = firstOwned
		defaultKind = firstOwnedKind
	}
	return out, defaultTenant, defaultKind
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

	claims := &models.JWTClaims{
		UserUUID:  user.UUID,
		Email:     user.Email,
		TokenType: "refresh",
		ExpiresAt: expiresAt.Unix(),
		IssuedAt:  now.Unix(),
		Issuer:    s.issuer,
		// ADR-0003 PR-D D-3: refresh tokens carry the same audience as
		// the access token they pair with. Cross-audience refresh is
		// blocked at the host-mux's RequireAudience middleware.
		Audience:    s.audience,
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

func (s *jwtService) GenerateTokenPairWithAMR(
	user *userModels.User,
	deviceInfo *models.DeviceInfo,
	securityCtx *models.SecurityContext,
	amr []string,
	lastOTPAt int64,
) (*models.TokenPair, error) {
	// Inject amr + last_otp_at into the context struct so the existing
	// access-token path picks them up; refresh tokens don't carry amr
	// (they're not presented to protected routes directly).
	ctx := *securityCtx
	ctx.AMR = amr
	ctx.LastOTPAt = lastOTPAt
	return s.GenerateTokenPair(user, deviceInfo, &ctx)
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

	// ADR-0003 PR-D D-3: aud is mandatory post-cutover. v1 tokens (no
	// `aud` claim) are rejected here; the host-mux RequireAudience
	// middleware also blocks them at an earlier stage but defense in
	// depth at validation time keeps the contract explicit for any
	// caller that bypasses the audience MW (sidecar internal calls,
	// future test paths).
	aud, _ := mapClaims["aud"].(string)
	if aud == "" {
		return nil, ErrMissingAudience
	}

	claims := s.mapToClaims(mapClaims)
	return claims, nil
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
		"sub":   claims.UserUUID,
		"email": claims.Email,
		"srole": claims.SystemRole,
		"type":  claims.TokenType,
		"exp":   claims.ExpiresAt,
		"iat":   claims.IssuedAt,
		"iss":   claims.Issuer,
	}

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
	if claims.DefaultTenantID != "" {
		m["dtid"] = claims.DefaultTenantID
	}
	if claims.ActingTenantID != "" {
		m["acting_tenant_id"] = claims.ActingTenantID
	}
	if claims.ActingTenantKind != "" {
		m["acting_tenant_kind"] = claims.ActingTenantKind
	}
	if len(claims.Memberships) > 0 {
		mbrs := make([]map[string]any, 0, len(claims.Memberships))
		for _, mb := range claims.Memberships {
			entry := map[string]any{
				"tid": mb.TenantUUID,
				"r":   mb.Roles,
			}
			if mb.TenantKind != "" {
				entry["k"] = mb.TenantKind
			}
			mbrs = append(mbrs, entry)
		}
		m["mbr"] = mbrs
	}
	if len(claims.AMR) > 0 {
		m["amr"] = claims.AMR
	}
	if claims.LastOTPAt > 0 {
		m["last_otp_at"] = claims.LastOTPAt
	}

	return m
}

func (s *jwtService) mapToClaims(m jwt.MapClaims) *models.JWTClaims {
	claims := &models.JWTClaims{
		UserUUID:         getStringClaim(m, "sub"),
		Email:            getStringClaim(m, "email"),
		SystemRole:       getStringClaim(m, "srole"),
		TokenType:        getStringClaim(m, "type"),
		ExpiresAt:        int64(getFloatClaim(m, "exp")),
		IssuedAt:         int64(getFloatClaim(m, "iat")),
		NotBefore:        int64(getFloatClaim(m, "nbf")),
		Issuer:           getStringClaim(m, "iss"),
		Audience:         getStringClaim(m, "aud"),
		SessionID:        getStringClaim(m, "sid"),
		DeviceID:         getStringClaim(m, "did"),
		IPAddress:        getStringClaim(m, "ip"),
		Fingerprint:      getStringClaim(m, "fp"),
		RiskScore:        getFloatClaim(m, "risk"),
		OAuthProvider:    getStringClaim(m, "provider"),
		DefaultTenantID:  getStringClaim(m, "dtid"),
		ActingTenantID:   getStringClaim(m, "acting_tenant_id"),
		ActingTenantKind: getStringClaim(m, "acting_tenant_kind"),
	}

	if scope, ok := m["scope"].([]interface{}); ok {
		claims.Scope = interfaceSliceToStringSlice(scope)
	}
	if amr, ok := m["amr"].([]interface{}); ok {
		claims.AMR = interfaceSliceToStringSlice(amr)
	}
	claims.LastOTPAt = int64(getFloatClaim(m, "last_otp_at"))
	if mbrs, ok := m["mbr"].([]interface{}); ok {
		claims.Memberships = make([]models.TenantMembership, 0, len(mbrs))
		for _, raw := range mbrs {
			obj, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			mb := models.TenantMembership{
				TenantUUID: getStr(obj, "tid"),
				TenantKind: getStr(obj, "k"),
			}
			if roles, ok := obj["r"].([]interface{}); ok {
				mb.Roles = interfaceSliceToStringSlice(roles)
			}
			claims.Memberships = append(claims.Memberships, mb)
		}
	}

	return claims
}

func getStr(m map[string]any, k string) string {
	v, _ := m[k].(string)
	return v
}

func (s *jwtService) getPrimaryOAuthProvider(user *userModels.User) models.OAuthProvider {
	for _, link := range user.OAuthLinks {
		if link.IsPrimary && link.IsActive {
			return models.OAuthProvider(link.Provider)
		}
	}
	if user.OAuthProvider != "" {
		return models.OAuthProvider(user.OAuthProvider)
	}
	for _, link := range user.OAuthLinks {
		if link.IsActive {
			return models.OAuthProvider(link.Provider)
		}
	}
	return ""
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

func (s *jwtService) GenerateAccessTokenWithAMR(user *userModels.User, amr []string, lastOTPAt int64) (string, error) {
	deviceInfo := &models.DeviceInfo{
		DeviceID:   "default",
		DeviceType: "unknown",
		Platform:   "api",
	}
	securityCtx := &models.SecurityContext{
		SessionID: fmt.Sprintf("session_%d", time.Now().Unix()),
		Timestamp: time.Now(),
		AMR:       amr,
		LastOTPAt: lastOTPAt,
	}
	return s.GenerateEnhancedAccessToken(user, deviceInfo, securityCtx)
}

func (s *jwtService) GenerateRefreshToken(user *userModels.User) (string, error) {
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
