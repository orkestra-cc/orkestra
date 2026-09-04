package services

import (
	"context"
	"crypto/rsa"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/pkg/sdk/iface"
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

	GenerateAccessToken(user *iface.User) (string, error)
	GenerateRefreshToken(user *iface.User) (string, error)
	// GenerateAccessTokenWithAMR mints an access token with explicit authn
	// methods (RFC 8176) and an optional last-OTP timestamp. Used by the MFA
	// verify endpoint and password/OAuth login paths to record how the user
	// proved their identity on this request.
	GenerateAccessTokenWithAMR(user *iface.User, amr []string, lastOTPAt int64) (string, error)
	ValidateAccessToken(tokenString string) (*models.JWTClaims, error)
	ValidateRefreshToken(tokenString string) (*models.JWTClaims, error)
	ParseUnverifiedClaims(tokenString string) (*models.JWTClaims, error)

	GenerateEnhancedAccessToken(user *iface.User, deviceInfo *models.DeviceInfo, securityCtx *models.SecurityContext) (string, error)
	GenerateEnhancedRefreshToken(user *iface.User, deviceInfo *models.DeviceInfo, securityCtx *models.SecurityContext) (string, error)

	ValidateAccessTokenWithRisk(tokenString string) (*models.JWTClaims, error)
	ValidateRefreshTokenWithRisk(tokenString string) (*models.JWTClaims, error)

	GenerateTokenPair(user *iface.User, deviceInfo *models.DeviceInfo, securityCtx *models.SecurityContext) (*models.TokenPair, error)
	// GenerateTokenPairWithAMR is identical to GenerateTokenPair but stamps
	// the access token's amr + last_otp_at claims. Used by the MFA login
	// verify endpoint and anywhere else that needs to record which factors
	// were completed for a freshly minted session.
	GenerateTokenPairWithAMR(user *iface.User, deviceInfo *models.DeviceInfo, securityCtx *models.SecurityContext, amr []string, lastOTPAt int64) (*models.TokenPair, error)
	// GenerateAccessTokenForSessionWithAMR mints a replacement access token
	// for an existing canonical session without creating a new session id.
	GenerateAccessTokenForSessionWithAMR(user *iface.User, deviceInfo *models.DeviceInfo, securityCtx *models.SecurityContext, amr []string, lastOTPAt int64) (string, error)

	// SetTenantProvider allows late wiring of the tenant provider so that
	// token issuance can embed the user's current memberships in the JWT.
	// Called by the auth module after the tenant module has initialized.
	SetTenantProvider(tp iface.TenantProvider)

	// SetDefaultTenantProvider wires the platform-default Tier-1 tenant
	// resolver (iface.DefaultTenantProvider, tenant module PR 3). When
	// set, token issuance prefers the platform default as the tenant
	// fallback IF it names one of the user's valid memberships;
	// otherwise selection falls through to the owner-first rule
	// unchanged. Only the operator-audience JWT service is wired with
	// this (auth/module.go) — client-audience tokens never consult the
	// internal platform default (tier isolation). A nil provider, a
	// "no default assigned" response, or a provider error all fall
	// through the same way: fallback selection must never fail token
	// issuance.
	SetDefaultTenantProvider(dp iface.DefaultTenantProvider)

	// AccessTokenTTL is the lifetime a token minted right now would
	// carry: the admin-managed accessTokenTTL when a policy is wired,
	// otherwise the env-driven default. Callers report it to clients so
	// the SPA schedules its refresh against the real expiry rather than
	// a hardcoded guess.
	AccessTokenTTL(ctx context.Context) time.Duration
	// RefreshTokenTTL is the lifetime stamped on refresh tokens. The
	// persisted refresh row must use the same value, or the row and its
	// own JWT disagree about when the session ends.
	RefreshTokenTTL() time.Duration

	// SetPolicy wires the admin-managed AuthPolicyService so the access-
	// token TTL is read live on every mint. Nil keeps the env-driven
	// default. Phase 3.1 of the auth-policy roadmap.
	SetPolicy(p *AuthPolicyService)
}

type jwtService struct {
	privateKey    *rsa.PrivateKey
	publicKey     *rsa.PublicKey
	accessExpiry  time.Duration
	refreshExpiry time.Duration
	issuer        string
	audience      string
	tenant        iface.TenantProvider
	// defaultTenants resolves the platform-default Tier-1 tenant
	// (iface.DefaultTenantProvider). Nil unless wired via
	// SetDefaultTenantProvider — only the operator-audience service
	// gets it (auth/module.go). See the interface doc comment above for
	// the full fallback/failure semantics.
	defaultTenants iface.DefaultTenantProvider
	// policy is the optional admin-managed source for accessTokenTTL.
	// Nil falls back to accessExpiry (env-driven default). Set via
	// SetPolicy after construction. Phase 3.1 of the auth-policy
	// roadmap.
	policy *AuthPolicyService
}

// SetPolicy wires the admin-managed AuthPolicyService so accessTokenTTL
// is read live on every GenerateAccessToken. Nil keeps the legacy
// env-driven TTL (s.accessExpiry).
func (s *jwtService) SetPolicy(p *AuthPolicyService) { s.policy = p }

// accessTokenLifetime returns the lifetime to apply to a newly minted
// access token: the admin-policy value when wired, falling back to the
// env-driven accessExpiry. Centralised so every mint path uses the
// same resolution.
func (s *jwtService) accessTokenLifetime(ctx context.Context) time.Duration {
	if s.policy != nil {
		if d := s.policy.AccessTokenTTL(ctx); d > 0 {
			return d
		}
	}
	return s.accessExpiry
}

func (s *jwtService) AccessTokenTTL(ctx context.Context) time.Duration {
	return s.accessTokenLifetime(ctx)
}

func (s *jwtService) RefreshTokenTTL() time.Duration { return s.refreshExpiry }

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
// that doesn't care about TTL don't need to pass anything explicit. A
// positive accessTTL is additionally clamped into
// [MinAccessTokenTTL, MaxAccessTokenTTL]: a value over MaxAccessTokenTTL
// is capped rather than rejected (ADR-0017 D5), and a value under
// MinAccessTokenTTL is raised rather than rejected — below a minute, the
// SPA's proactive refresh (which fires inside a 30s skew of expiry) would
// find every freshly minted token already due for renewal and rotate it
// on every single request (ADR-0020 D3, #317).
func NewJWTService(privateKey *rsa.PrivateKey, publicKey *rsa.PublicKey, env string, accessTTL, refreshTTL time.Duration) JWTService {
	if accessTTL <= 0 {
		accessTTL = defaultAccessTokenTTL
	}
	if accessTTL > MaxAccessTokenTTL {
		// The Redis revocation denylist stores entries for
		// MaxAccessTokenTTL + 1m. A longer token would outlive its own
		// revocation entry and become valid again after logout. Clamp
		// rather than reject: this level is fed by JWT_ACCESS_TOKEN_EXPIRY
		// and by direct callers, neither of which can surface a 422.
		// ADR-0017 D5.
		slogDefault().Warn("auth: access-token lifetime above maximum, clamping",
			"value", accessTTL.String(),
			"using", MaxAccessTokenTTL.String())
		accessTTL = MaxAccessTokenTTL
	}
	if accessTTL < MinAccessTokenTTL {
		// Below a minute the SPA's proactive refresh — which fires
		// inside a 30s skew of the token's expiry — would find every
		// freshly minted token already inside its refresh window, so
		// every single request would rotate the token again. Clamp
		// rather than reject for the same reason as the ceiling above:
		// this level is fed by JWT_ACCESS_TOKEN_EXPIRY and by direct
		// callers, neither of which can surface a 422. ADR-0020 D3, #317.
		slogDefault().Warn("auth: access-token lifetime below minimum, clamping",
			"value", accessTTL.String(),
			"using", MinAccessTokenTTL.String())
		accessTTL = MinAccessTokenTTL
	}
	// Unreachable through configuration: getEnvAsDuration never returns
	// zero for JWT_REFRESH_TOKEN_EXPIRY (it falls back to the shipped
	// "7d"), so this branch fires only for callers constructing the
	// service with an explicit zero — in practice, tests. Kept as-is per
	// ADR-0017 decision F: changing the value would be a silent behaviour
	// change for direct callers with no benefit. The refresh-token TTL an
	// actual deployment runs is 7d, not the 30d this line suggests.
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

// isIssuedAudience reports whether aud is one of the values this
// platform stamps on a token. Exact match — no case folding, no
// trimming: every minter writes one of the constants verbatim, so
// anything else is either a forgery attempt or a bug, and both deserve
// a rejection.
func isIssuedAudience(aud string) bool {
	switch aud {
	case AudienceOperator, AudienceClient, AudienceService:
		return true
	default:
		return false
	}
}

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

func (s *jwtService) SetDefaultTenantProvider(dp iface.DefaultTenantProvider) {
	s.defaultTenants = dp
}

func (s *jwtService) IsEnabled() bool {
	return s.privateKey != nil && s.publicKey != nil
}

func (s *jwtService) GenerateEnhancedAccessToken(
	user *iface.User,
	deviceInfo *models.DeviceInfo,
	securityCtx *models.SecurityContext,
) (string, error) {
	if s.privateKey == nil {
		return "", ErrJWTKeysNotLoaded
	}
	now := time.Now()
	expiresAt := now.Add(s.accessTokenLifetime(context.Background()))

	primaryProvider := s.getPrimaryOAuthProvider(user)

	memberships, fallbackTenant, fallbackKind := s.loadMemberships(user.UUID)

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
		TenantFallbackID: fallbackTenant,
		ActingTenantID:   fallbackTenant,
		ActingTenantKind: fallbackKind,

		SessionID:     securityCtx.SessionID,
		DeviceID:      deviceInfo.DeviceID,
		IPAddress:     securityCtx.IPAddress,
		Fingerprint:   deviceInfo.Fingerprint,
		RiskScore:     securityCtx.RiskScore,
		OAuthProvider: string(primaryProvider),
		Scope:         []string{"profile", "email", "api"},

		AMR:       securityCtx.AMR,
		LastOTPAt: securityCtx.LastOTPAt,
		// auth_time comes from the caller: only a session-creating path
		// knows it just authenticated somebody. Stamping now() here
		// instead would give the machine mints below (the client-
		// credentials grant and the dev-token endpoint both reach the
		// signer through GenerateAccessToken) a rolling freshness window
		// no interactive presence ever backed.
		AuthTime: securityCtx.AuthTime,
		// mfae is a fact about the SUBJECT, so it is read off the user
		// every mint already holds, in this one place. It must be the
		// value that was current when the token was signed — never
		// re-read at validation time, or the claim would say nothing.
		MFAEpoch: user.MFAEpoch,
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
// during startup) or if the user belongs to no tenants.
//
// The embedded membership list is returned in the SAME order the provider
// (repository) delivers it — joinedAt ascending, then tenantUUID ascending,
// see tenant/repository.go::ListMembershipsByUser — and is never re-sorted
// here, so the claims' `mbr` array and the fallback selection below always
// agree on ordering.
//
// The tenant fallback is selected in this order:
//  1. The operational platform default (iface.DefaultTenantProvider), when
//     s.defaultTenants is wired AND it names one of the memberships just
//     loaded. This grants nothing by itself — membership validation already
//     happened above, and the operator X-Tenant-ID override still applies
//     downstream in middleware. A nil provider, a "no default assigned"
//     response, or a provider ERROR all fall straight through to rule 2 —
//     fallback selection must never fail token issuance.
//  2. The first OWNED membership, in the deterministic repo order.
//  3. The first membership, in the deterministic repo order.
//
// Also returns the tenant fallback kind so middleware can dispatch on tier
// without re-reading the provider. See ADR-0001.
func (s *jwtService) loadMemberships(userUUID string) (list []models.TenantMembership, fallbackTenant, fallbackKind string) {
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
		if fallbackTenant == "" {
			fallbackTenant = m.TenantUUID
			fallbackKind = kind
		}
	}
	if firstOwned != "" {
		fallbackTenant = firstOwned
		fallbackKind = firstOwnedKind
	}

	// 1. The operational platform default, when it appears among the user's
	// valid memberships. Selection grants nothing — membership validation
	// and the operator X-Tenant-ID override still apply downstream.
	if s.defaultTenants != nil {
		dt, derr := s.defaultTenants.GetDefaultTenant(ctx)
		if derr != nil {
			slogDefault().WarnContext(ctx, "auth: platform-default tenant lookup failed, falling back to owner-first selection",
				"error", derr.Error())
		} else if dt != nil {
			for _, m := range out {
				if m.TenantUUID == dt.UUID {
					return out, m.TenantUUID, m.TenantKind
				}
			}
		}
	}
	// 2. first owned membership (joinedAt asc, tenantUUID asc — repo sort);
	// 3. first membership. (existing loop above, unchanged)
	return out, fallbackTenant, fallbackKind
}

func (s *jwtService) GenerateEnhancedRefreshToken(
	user *iface.User,
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
		// The refresh token carries auth_time — unlike amr, which a
		// refresh deliberately does not re-assert. It is the only
		// durable record of the session's ORIGIN that the rotation and
		// /session paths can read: the refresh row has no such column,
		// and the auth-session row is only fetched when the absolute
		// session cap happens to be enabled. Without it every refreshed
		// token would report auth_time 0 and a user would be sent back
		// to the login form on a timer.
		AuthTime: securityCtx.AuthTime,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, s.claimsToMap(claims))

	tokenString, err := token.SignedString(s.privateKey)
	if err != nil {
		return "", fmt.Errorf("failed to sign enhanced refresh token: %w", err)
	}

	return tokenString, nil
}

func (s *jwtService) GenerateTokenPair(
	user *iface.User,
	deviceInfo *models.DeviceInfo,
	securityCtx *models.SecurityContext,
) (*models.TokenPair, error) {
	if err := requireSessionContext(securityCtx); err != nil {
		return nil, err
	}
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
		ExpiresIn:    int64(s.accessTokenLifetime(context.Background()).Seconds()),
		SessionID:    securityCtx.SessionID,
		DeviceID:     deviceInfo.DeviceID,
		Scope:        []string{"profile", "email", "api"},
		IssuedAt:     time.Now(),
		RefreshCount: 0,
	}, nil
}

func (s *jwtService) GenerateTokenPairWithAMR(
	user *iface.User,
	deviceInfo *models.DeviceInfo,
	securityCtx *models.SecurityContext,
	amr []string,
	lastOTPAt int64,
) (*models.TokenPair, error) {
	if err := requireSessionContext(securityCtx); err != nil {
		return nil, err
	}
	// Inject amr + last_otp_at into the context struct so the existing
	// access-token path picks them up; refresh tokens don't carry amr
	// (they're not presented to protected routes directly).
	ctx := *securityCtx
	ctx.AMR = amr
	ctx.LastOTPAt = lastOTPAt
	return s.GenerateTokenPair(user, deviceInfo, &ctx)
}

// GenerateAccessTokenForSessionWithAMR mints an access token for the
// supplied canonical session. It copies the context so callers keep their
// original authentication state intact.
func (s *jwtService) GenerateAccessTokenForSessionWithAMR(
	user *iface.User,
	deviceInfo *models.DeviceInfo,
	securityCtx *models.SecurityContext,
	amr []string,
	lastOTPAt int64,
) (string, error) {
	if err := requireSessionContext(securityCtx); err != nil {
		return "", err
	}
	ctx := *securityCtx
	ctx.AMR = amr
	ctx.LastOTPAt = lastOTPAt
	return s.GenerateEnhancedAccessToken(user, deviceInfo, &ctx)
}

func requireSessionContext(securityCtx *models.SecurityContext) error {
	if securityCtx == nil || securityCtx.SessionID == "" {
		return errors.New("session id is required for token issuance")
	}
	return nil
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
	// `aud` claim) are rejected here, and so is any audience this
	// platform does not issue — the check used to accept ANY non-empty
	// string, which made the "defense in depth" this comment promises
	// purely nominal.
	//
	// Note what is deliberately NOT done: pinning aud to s.audience.
	// A single AuthMiddleware holding a single JWT service guards BOTH
	// the operator and the client mux (cmd/server/main.go), so equality
	// with the minting audience would lock one entire tier out. Pinning
	// a request to its surface is the job of the mux-level
	// RequireAudience gate, which is non-skippable because it is mounted
	// with router.Use on each audience mux. This check is the narrower
	// one it can safely make: the token names an audience we actually
	// mint.
	aud, _ := mapClaims["aud"].(string)
	if aud == "" {
		return nil, ErrMissingAudience
	}
	if !isIssuedAudience(aud) {
		return nil, ErrInvalidToken
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
	if claims.TenantFallbackID != "" {
		m["dtid"] = claims.TenantFallbackID
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
	// Both omitted when zero: a pre-deploy token carries neither, and a
	// freshly minted one writing a literal 0 would be indistinguishable
	// from it in a log.
	if claims.AuthTime > 0 {
		m["auth_time"] = claims.AuthTime
	}
	if claims.MFAEpoch > 0 {
		m["mfae"] = claims.MFAEpoch
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
		TenantFallbackID: getStringClaim(m, "dtid"),
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
	// Absent reads as zero for both: an absent mfae matches a user
	// document that has no mfaEpoch (so the deploy downgrades nobody) and
	// an absent auth_time reads as stale.
	claims.AuthTime = int64(getFloatClaim(m, "auth_time"))
	claims.MFAEpoch = int(getFloatClaim(m, "mfae"))
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

func (s *jwtService) getPrimaryOAuthProvider(user *iface.User) models.OAuthProvider {
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

func (s *jwtService) GenerateAccessToken(user *iface.User) (string, error) {
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

// GenerateAccessTokenForTenant implements iface.TenantScopedTokenProvider. It
// mints an access token for a principal with NO database membership (the
// dev-token endpoint's synthetic user), carrying an explicit acting tenant plus
// a matching synthetic membership so tenant-scoped reads (billing/documents)
// resolve a tenant from request context. Self-contained — it does not run
// loadMemberships — so the normal login path is untouched.
func (s *jwtService) GenerateAccessTokenForTenant(user *iface.User, tenantUUID, tenantKind string, roles []string) (string, error) {
	if s.privateKey == nil {
		return "", ErrJWTKeysNotLoaded
	}
	claims := buildTenantScopedClaims(user, tenantUUID, tenantKind, roles, time.Now(),
		s.accessTokenLifetime(context.Background()), s.issuer, s.audience)
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, s.claimsToMap(claims))
	tokenString, err := token.SignedString(s.privateKey)
	if err != nil {
		return "", fmt.Errorf("failed to sign tenant-scoped access token: %w", err)
	}
	return tokenString, nil
}

// buildTenantScopedClaims is the pure claims constructor (no signing) for
// GenerateAccessTokenForTenant, factored out so the tenant injection is
// unit-testable without RSA keys. An empty kind defaults to internal.
func buildTenantScopedClaims(user *iface.User, tenantUUID, tenantKind string, roles []string, now time.Time, lifetime time.Duration, issuer, audience string) *models.JWTClaims {
	if tenantKind == "" {
		tenantKind = "internal"
	}
	return &models.JWTClaims{
		UserUUID:   user.UUID,
		Email:      user.Email,
		SystemRole: user.Role,
		TokenType:  "access",
		ExpiresAt:  now.Add(lifetime).Unix(),
		IssuedAt:   now.Unix(),
		NotBefore:  now.Unix(),
		Issuer:     issuer,
		Audience:   audience,

		Memberships: []models.TenantMembership{
			{TenantUUID: tenantUUID, TenantKind: tenantKind, Roles: roles},
		},
		TenantFallbackID: tenantUUID,
		ActingTenantID:   tenantUUID,
		ActingTenantKind: tenantKind,

		SessionID: fmt.Sprintf("session_%d", now.Unix()),
		DeviceID:  "default",
		Scope:     []string{"profile", "email", "api"},
	}
}

func (s *jwtService) GenerateAccessTokenWithAMR(user *iface.User, amr []string, lastOTPAt int64) (string, error) {
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

func (s *jwtService) GenerateRefreshToken(user *iface.User) (string, error) {
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
