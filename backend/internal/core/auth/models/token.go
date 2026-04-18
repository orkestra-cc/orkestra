package models

import (
	"time"
)

type TokenPair struct {
	AccessToken  string    `json:"accessToken"`
	RefreshToken string    `json:"refreshToken"`
	TokenType    string    `json:"tokenType"`
	ExpiresIn    int64     `json:"expiresIn"`
	SessionID    string    `json:"sessionId"`
	DeviceID     string    `json:"deviceId,omitempty"`
	Scope        []string  `json:"scope,omitempty"`
	IssuedAt     time.Time `json:"issuedAt"`
	RefreshCount int       `json:"refreshCount,omitempty"`
}

// OrgMembership is the subset of a tenant membership embedded in JWT claims.
// Only org id, tenant kind, and role names are embedded — permissions are
// resolved per-request by the authz module so revocation is instant.
//
// TenantKind lets middleware dispatch on tier ("is this request acting in an
// internal operator tenant or an external client tenant?") without a DB
// lookup. See ADR-0001.
type OrgMembership struct {
	OrgUUID    string   `json:"oid"`
	TenantKind string   `json:"k,omitempty"`
	Roles      []string `json:"r"`
}

type JWTClaims struct {
	// Standard JWT claims
	UserUUID   string `json:"sub"`           // User UUID (subject)
	Email      string `json:"email"`         // User email
	SystemRole string `json:"srole"`         // Global system role (developer/administrator/user)
	TokenType  string `json:"type"`          // "access" or "refresh"
	ExpiresAt  int64  `json:"exp"`           // Expiration timestamp
	IssuedAt   int64  `json:"iat"`           // Issued at timestamp
	NotBefore  int64  `json:"nbf,omitempty"` // Not before timestamp
	Issuer     string `json:"iss"`           // Token issuer
	Audience   string `json:"aud,omitempty"` // Token audience

	// Multi-tenancy claims
	Memberships  []OrgMembership `json:"mbr,omitempty"`  // orgs the user belongs to
	DefaultOrgID string          `json:"dorg,omitempty"` // selected when X-Org-ID header is absent

	// ActingTenantID is the tenant this specific token is minted for. When
	// set, middleware uses it directly instead of deriving the current tenant
	// from the X-Org-ID header. Phase 3 external-client flows always set
	// this so a client portal token can never accidentally act on an internal
	// tenant. See ADR-0001.
	ActingTenantID string `json:"acting_tenant_id,omitempty"`

	// ActingTenantKind is cached alongside ActingTenantID so tier-aware
	// middleware does not need to hit the DB. "internal" | "external".
	ActingTenantKind string `json:"acting_tenant_kind,omitempty"`

	// Security and session claims
	SessionID   string  `json:"sid"`            // Session identifier
	DeviceID    string  `json:"did"`            // Device identifier
	IPAddress   string  `json:"ip,omitempty"`   // IP address (for binding)
	Fingerprint string  `json:"fp,omitempty"`   // Device fingerprint
	RiskScore   float64 `json:"risk,omitempty"` // Risk assessment score

	// OAuth and provider claims
	OAuthProvider string `json:"provider,omitempty"` // Primary OAuth provider
	ProviderID    string `json:"pid,omitempty"`      // Provider user ID

	// OAuth scopes
	Scope []string `json:"scope,omitempty"`

	// MFA / authentication methods (RFC 8176). Examples: "pwd", "otp", "oauth",
	// "webauthn". Populated at token issuance, enforced by RequireMFA middleware.
	AMR []string `json:"amr,omitempty"`
	// LastOTPAt is the unix timestamp of the most recent successful OTP step.
	// Used by RequireStepUp (Block D) to detect stale MFA proofs.
	LastOTPAt int64 `json:"last_otp_at,omitempty"`
}

type RefreshTokenRequest struct {
	RefreshToken string `json:"refreshToken" validate:"required"`
}

type LogoutRequest struct {
	RefreshToken string `json:"refreshToken,omitempty"`
	SessionID    string `json:"sessionId,omitempty"`
	DeviceID     string `json:"deviceId,omitempty"`
	AllDevices   bool   `json:"allDevices"`
	Reason       string `json:"reason,omitempty"`
}

type TokenValidationResult struct {
	Valid            bool
	Claims           *JWTClaims
	Error            error
	ExpiresAt        time.Time
	RiskScore        float64
	RequiresMFA      bool
	RequiresReauth   bool
	SecurityWarnings []string
}

type OAuthState struct {
	State       string    `json:"state"`
	RedirectURL string    `json:"redirectUrl"`
	DeviceID    string    `json:"deviceId,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	ExpiresAt   time.Time `json:"expiresAt"`
	// PKCE fields for OAuth 2.1 compliance
	CodeChallenge       string `json:"codeChallenge,omitempty"`
	CodeChallengeMethod string `json:"codeChallengeMethod,omitempty"`
	Nonce               string `json:"nonce,omitempty"`
	// Enhanced security fields
	IPAddress   string  `json:"ipAddress,omitempty"`
	UserAgent   string  `json:"userAgent,omitempty"`
	Fingerprint string  `json:"fingerprint,omitempty"`
	RiskScore   float64 `json:"riskScore,omitempty"`
}

// Enhanced token request types
type TokenRequest struct {
	GrantType    string           `json:"grantType" validate:"required"`
	Code         string           `json:"code,omitempty"`
	RedirectURI  string           `json:"redirectUri,omitempty"`
	RefreshToken string           `json:"refreshToken,omitempty"`
	Scope        []string         `json:"scope,omitempty"`
	DeviceInfo   *DeviceInfo      `json:"deviceInfo,omitempty"`
	SecurityInfo *SecurityContext `json:"securityInfo,omitempty"`
	PKCE         *PKCEData        `json:"pkce,omitempty"`
}

type PKCEData struct {
	CodeVerifier  string `json:"codeVerifier" validate:"required"`
	CodeChallenge string `json:"codeChallenge" validate:"required"`
	Method        string `json:"method" validate:"required,oneof=S256 plain"`
}

// Risk assessment types
type RiskFactors struct {
	UnknownDevice    bool     `json:"unknownDevice"`
	UnknownLocation  bool     `json:"unknownLocation"`
	UnusualTime      bool     `json:"unusualTime"`
	HighRiskIP       bool     `json:"highRiskIP"`
	VPNDetected      bool     `json:"vpnDetected"`
	TorDetected      bool     `json:"torDetected"`
	BotDetected      bool     `json:"botDetected"`
	MultipleFailures bool     `json:"multipleFailures"`
	RiskScore        float64  `json:"riskScore"`
	RequiresMFA      bool     `json:"requiresMfa"`
	RequiresReauth   bool     `json:"requiresReauth"`
	Recommendations  []string `json:"recommendations"`
}
