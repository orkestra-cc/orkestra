package models

import (
	"time"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

// DeviceInfo contains device-specific information for authentication
type DeviceInfo struct {
	DeviceID     string `json:"deviceId" bson:"deviceId"`
	DeviceName   string `json:"deviceName,omitempty" bson:"deviceName,omitempty"`
	DeviceType   string `json:"deviceType" bson:"deviceType"` // mobile, tablet, desktop, watch
	Platform     string `json:"platform" bson:"platform"`     // ios, android, web, windows, macos
	AppVersion   string `json:"appVersion,omitempty" bson:"appVersion,omitempty"`
	OSVersion    string `json:"osVersion,omitempty" bson:"osVersion,omitempty"`
	Manufacturer string `json:"manufacturer,omitempty" bson:"manufacturer,omitempty"`
	Model        string `json:"model,omitempty" bson:"model,omitempty"`
	Fingerprint  string `json:"fingerprint" bson:"fingerprint"` // Device fingerprint hash
	UserAgent    string `json:"userAgent,omitempty" bson:"userAgent,omitempty"`
	PushToken    string `json:"pushToken,omitempty" bson:"pushToken,omitempty"` // For push notifications
	IsRooted     bool   `json:"isRooted,omitempty" bson:"isRooted,omitempty"`   // For security assessment
}

// SecurityContext contains security-related information for a request
type SecurityContext struct {
	SessionID   string    `json:"sessionId" bson:"sessionId"`
	IPAddress   string    `json:"ipAddress" bson:"ipAddress"`
	Location    *Location `json:"location,omitempty" bson:"location,omitempty"`
	RiskScore   float64   `json:"riskScore" bson:"riskScore"`
	RiskFactors []string  `json:"riskFactors,omitempty" bson:"riskFactors,omitempty"`
	IsVPN       bool      `json:"isVpn,omitempty" bson:"isVpn,omitempty"`
	IsTor       bool      `json:"isTor,omitempty" bson:"isTor,omitempty"`
	IsProxy     bool      `json:"isProxy,omitempty" bson:"isProxy,omitempty"`
	ThreatLevel string    `json:"threatLevel,omitempty" bson:"threatLevel,omitempty"` // low, medium, high, critical
	RequiresMFA bool      `json:"requiresMfa,omitempty" bson:"requiresMfa,omitempty"`
	RequestID   string    `json:"requestId,omitempty" bson:"requestId,omitempty"`
	Timestamp   time.Time `json:"timestamp" bson:"timestamp"`

	// AMR (RFC 8176) records which auth methods the caller completed for this
	// request. Populated at token issuance, mirrored into the JWT claim.
	AMR []string `json:"amr,omitempty" bson:"-"`
	// LastOTPAt is the unix timestamp of the last successful OTP step.
	// Drives step-up auth freshness checks (Block D).
	LastOTPAt int64 `json:"lastOtpAt,omitempty" bson:"-"`
	// Fingerprint is the device fingerprint for the current request.
	// Populated by the login/refresh path so the risk scorer can detect
	// new_device_fingerprint without a separate parameter on every
	// service method. Not persisted — the fingerprint lives on
	// AuthSessionDoc.DeviceInfo.Fingerprint once the session is created.
	Fingerprint string `json:"fingerprint,omitempty" bson:"-"`
}

// RiskAssessment contains detailed risk analysis results
type RiskAssessment struct {
	Score           float64                `json:"score" bson:"score"` // 0.0 (lowest) to 1.0 (highest)
	Level           string                 `json:"level" bson:"level"` // low, medium, high, critical
	Factors         []RiskFactor           `json:"factors" bson:"factors"`
	Recommendations []string               `json:"recommendations" bson:"recommendations"`
	RequiresMFA     bool                   `json:"requiresMfa" bson:"requiresMfa"`
	RequiresReauth  bool                   `json:"requiresReauth" bson:"requiresReauth"`
	BlockAccess     bool                   `json:"blockAccess" bson:"blockAccess"`
	AlertAdmin      bool                   `json:"alertAdmin" bson:"alertAdmin"`
	Metadata        map[string]interface{} `json:"metadata,omitempty" bson:"metadata,omitempty"`
	AssessedAt      time.Time              `json:"assessedAt" bson:"assessedAt"`
}

// RiskFactor represents an individual risk factor
type RiskFactor struct {
	Type        string                 `json:"type" bson:"type"` // location, device, behavior, time
	Description string                 `json:"description" bson:"description"`
	Weight      float64                `json:"weight" bson:"weight"`     // Factor's contribution to overall score
	Severity    string                 `json:"severity" bson:"severity"` // low, medium, high
	Details     map[string]interface{} `json:"details,omitempty" bson:"details,omitempty"`
}

// SecurityEvent represents a security-related event
type SecurityEvent struct {
	ID          string                 `json:"id" bson:"_id,omitempty"`
	UserUUID    string                 `json:"userUuid" bson:"userUuid"`
	EventType   string                 `json:"eventType" bson:"eventType"` // login, logout, failed_login, mfa_challenge, etc.
	Severity    string                 `json:"severity" bson:"severity"`   // info, warning, error, critical
	Description string                 `json:"description" bson:"description"`
	IPAddress   string                 `json:"ipAddress" bson:"ipAddress"`
	UserAgent   string                 `json:"userAgent" bson:"userAgent"`
	DeviceID    string                 `json:"deviceId,omitempty" bson:"deviceId,omitempty"`
	SessionID   string                 `json:"sessionId,omitempty" bson:"sessionId,omitempty"`
	Location    *Location              `json:"location,omitempty" bson:"location,omitempty"`
	Success     bool                   `json:"success" bson:"success"`
	RiskScore   float64                `json:"riskScore,omitempty" bson:"riskScore,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty" bson:"metadata,omitempty"`
	Timestamp   time.Time              `json:"timestamp" bson:"timestamp"`
}

// TokenResponse represents the response containing tokens and session info
type TokenResponse struct {
	AccessToken    string                        `json:"accessToken,omitempty"`
	RefreshToken   string                        `json:"refreshToken,omitempty"` // Not sent for mobile (stored in cookie)
	TokenType      string                        `json:"tokenType,omitempty"`
	ExpiresIn      int64                         `json:"expiresIn,omitempty"`
	User           *iface.UserManagementResponse `json:"user,omitempty"`
	OAuthProviders []OAuthProviderInfo           `json:"oauthProviders,omitempty"`
	SessionID      string                        `json:"sessionId,omitempty"`
	DeviceID       string                        `json:"deviceId,omitempty"`
	// RequiresMFA is set when the caller presented valid primary credentials
	// but must complete a second factor before receiving an access token.
	// AccessToken/RefreshToken are empty in this case; clients exchange
	// MFAToken via POST /v1/auth/mfa/login/verify.
	RequiresMFA bool   `json:"requiresMfa,omitempty"`
	MFAToken    string `json:"mfaToken,omitempty"`
	// WebAuthnAvailable hints to the client whether a passkey path exists
	// alongside the TOTP code prompt. The frontend uses it to decide whether
	// to render the "Use a passkey instead" button on /mfa/verify. Set only
	// when RequiresMFA=true and the user has at least one enrolled credential.
	WebAuthnAvailable bool `json:"webauthnAvailable,omitempty"`
	// MFAEnrollmentRequired indicates a privileged user logged in without
	// an enrolled factor. The token is issued (so they can enroll) but the
	// client should redirect to the enrollment flow.
	MFAEnrollmentRequired bool `json:"mfaEnrollmentRequired,omitempty"`
	// MFAGraceExpiresAt is the deadline after which logins without a factor
	// will fail with mfa_enrollment_required. Only set when a grace window
	// is active.
	MFAGraceExpiresAt *time.Time `json:"mfaGraceExpiresAt,omitempty"`
}

// SessionsResponse represents the response for listing user sessions
type SessionsResponse struct {
	Sessions      []SessionInfo `json:"sessions"`
	ActiveCount   int           `json:"activeCount"`
	MaxSessions   int           `json:"maxSessions"`
	CurrentDevice string        `json:"currentDevice,omitempty"`
}

// SessionInfo contains information about an active session
type SessionInfo struct {
	SessionID    string    `json:"sessionId"`
	DeviceID     string    `json:"deviceId"`
	DeviceName   string    `json:"deviceName"`
	DeviceType   string    `json:"deviceType"`
	Platform     string    `json:"platform"`
	Location     *Location `json:"location,omitempty"`
	IPAddress    string    `json:"ipAddress"`
	LastActivity time.Time `json:"lastActivity"`
	CreatedAt    time.Time `json:"createdAt"`
	ExpiresAt    time.Time `json:"expiresAt"`
	IsCurrent    bool      `json:"isCurrent"`
	RiskScore    float64   `json:"riskScore,omitempty"`
}

// OAuth Link Management Models

type LinkOAuthProviderInput struct {
	Provider     OAuthProvider          `json:"provider" validate:"required,oneof=google apple discord github"`
	Code         string                 `json:"code,omitempty"`        // For web flow
	IDToken      string                 `json:"idToken,omitempty"`     // For mobile flow
	AccessToken  string                 `json:"accessToken,omitempty"` // Optional
	RedirectURI  string                 `json:"redirectUri,omitempty"`
	CodeVerifier string                 `json:"codeVerifier,omitempty"` // For PKCE
	DeviceInfo   *DeviceInfo            `json:"deviceInfo,omitempty"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
}

type UnlinkOAuthProviderInput struct {
	Provider OAuthProvider `json:"provider" validate:"required,oneof=google apple discord github"`
	Reason   string        `json:"reason,omitempty"`
}

type SetPrimaryOAuthProviderInput struct {
	Provider OAuthProvider `json:"provider" validate:"required,oneof=google apple discord github"`
}

type OAuthLinksResponse struct {
	Links       []OAuthLink   `json:"links"`
	Primary     OAuthProvider `json:"primary,omitempty"`
	CanUnlink   bool          `json:"canUnlink"`   // False if only one link exists
	RequiresMFA bool          `json:"requiresMfa"` // True if unlinking requires MFA
}

// Mobile-specific models

type MobileAuthRequest struct {
	IDToken       string        `json:"idToken" validate:"required"`
	AccessToken   string        `json:"accessToken,omitempty"`
	Provider      OAuthProvider `json:"provider" validate:"required,oneof=google apple"`
	DeviceInfo    DeviceInfo    `json:"deviceInfo" validate:"required"`
	BiometricAuth bool          `json:"biometricAuth,omitempty"`
}

type MobileTokenRefreshRequest struct {
	RefreshToken  string `json:"refreshToken" validate:"required"`
	DeviceID      string `json:"deviceId" validate:"required"`
	Fingerprint   string `json:"fingerprint" validate:"required"`
	BiometricAuth bool   `json:"biometricAuth,omitempty"`
}

// Device Management Models

type DeviceRegistrationRequest struct {
	DeviceInfo DeviceInfo `json:"deviceInfo" validate:"required"`
	PushToken  string     `json:"pushToken,omitempty"`
	Timezone   string     `json:"timezone,omitempty"`
}

type DeviceUpdateRequest struct {
	DeviceName string `json:"deviceName,omitempty" validate:"omitempty,min=1,max=50"`
	PushToken  string `json:"pushToken,omitempty"`
}

type DeviceListResponse struct {
	Devices       []DeviceInfo `json:"devices"`
	CurrentDevice string       `json:"currentDevice"`
	MaxDevices    int          `json:"maxDevices"`
}
