package models

import "time"

// AuthMethodsView is the aggregator response for the admin user-auth
// endpoint. It collapses three sources — the user document, the user's
// MFA factor row(s), and their linked OAuth providers — into one shape
// the admin profile card consumes. Mirrors the operator-facing card so
// every method shown in the UI maps to a concrete field here.
type AuthMethodsView struct {
	// HasUsablePassword is true iff the user has a non-empty password
	// hash. "Usable" is the right framing because OAuth-only accounts
	// have no password — they cannot log in via email+password until
	// one is set (typically via the password-reset flow).
	HasUsablePassword bool       `json:"hasUsablePassword"`
	PasswordUpdatedAt *time.Time `json:"passwordUpdatedAt,omitempty"`

	EmailVerified bool       `json:"emailVerified"`
	LastLoginAt   *time.Time `json:"lastLoginAt,omitempty"`

	// MFARequired reflects whether the target user's role currently
	// triggers RoleRequiresMFA. The card uses this to label the MFA
	// section "Required for this role" vs "Optional".
	MFARequired       bool       `json:"mfaRequired"`
	MFAGraceStartedAt *time.Time `json:"mfaGraceStartedAt,omitempty"`
	MFAGraceExpiresAt *time.Time `json:"mfaGraceExpiresAt,omitempty"`

	MFAFactors     []MFAFactorView     `json:"mfaFactors"`
	OAuthProviders []OAuthProviderView `json:"oauthProviders"`
}

// MFAFactorView is one row in AuthMethodsView.MFAFactors. The shape
// covers both TOTP and WebAuthn factors — for TOTP only
// BackupCodesRemaining is populated; for WebAuthn only Credentials is.
type MFAFactorView struct {
	Type                 string                     `json:"type" enum:"totp,webauthn"`
	EnrolledAt           *time.Time                 `json:"enrolledAt,omitempty"`
	LastUsedAt           *time.Time                 `json:"lastUsedAt,omitempty"`
	BackupCodesRemaining int                        `json:"backupCodesRemaining,omitempty"`
	Credentials          []WebAuthnCredentialSummary `json:"credentials,omitempty"`
}

// WebAuthnCredentialSummary is the admin-facing slice of a single
// passkey: enough to identify it in the UI without exposing the public
// key bytes. CredentialID is base64url-encoded at the API boundary.
type WebAuthnCredentialSummary struct {
	CredentialID string     `json:"credentialId"`
	Name         string     `json:"name"`
	CreatedAt    time.Time  `json:"createdAt"`
	LastUsedAt   *time.Time `json:"lastUsedAt,omitempty"`
}

// OAuthProviderView is one linked identity in AuthMethodsView.OAuthProviders.
type OAuthProviderView struct {
	Provider   string     `json:"provider" enum:"google,apple,github,discord"`
	Email      string     `json:"email"`
	LinkedAt   time.Time  `json:"linkedAt"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
	IsPrimary  bool       `json:"isPrimary"`
}
