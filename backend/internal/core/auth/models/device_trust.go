package models

import "time"

// DeviceTrustDuration is the default lifetime of a "remember this device"
// trust grant. Industry norm for remember-me-style flows; overridable at
// boot via the AUTH_DEVICE_TRUST_DURATION env var (Go duration string).
// Section C item #3 of the 2026-04-24 auth roadmap.
const DeviceTrustDuration = 30 * 24 * time.Hour

// DeviceTrustDoc is a single "this device may skip the MFA prompt on
// login" grant. One row per (userUUID, deviceID) pair — a fresh grant
// supersedes the prior row (the repo handles the upsert).
//
// trustedUntil carries a TTL index so Mongo reaps expired grants on
// its own. isActive / revokedAt tracks explicit revocation (password
// change, factor remove, admin reset, user-initiated DELETE).
// Fingerprint is captured at grant time and cross-checked on every
// IsTrusted read so a stolen deviceID alone can't bypass MFA — the
// attacker would also need the original device's fingerprint.
type DeviceTrustDoc struct {
	UUID         string    `bson:"uuid" json:"uuid"`
	UserUUID     string    `bson:"userUuid" json:"userUuid"`
	DeviceID     string    `bson:"deviceId" json:"deviceId"`
	Fingerprint  string    `bson:"fingerprint" json:"fingerprint"`
	DeviceName   string    `bson:"deviceName,omitempty" json:"deviceName,omitempty"`
	Platform     string    `bson:"platform,omitempty" json:"platform,omitempty"`
	IPAddress    string    `bson:"ipAddress,omitempty" json:"ipAddress,omitempty"`
	UserAgent    string    `bson:"userAgent,omitempty" json:"userAgent,omitempty"`
	TrustedAt    time.Time `bson:"trustedAt" json:"trustedAt"`
	TrustedUntil time.Time `bson:"trustedUntil" json:"trustedUntil"`
	LastUsedAt   time.Time `bson:"lastUsedAt,omitempty" json:"lastUsedAt,omitempty"`
	// GrantedAMR records which factor was verified when the trust row
	// was granted ("otp" | "webauthn"). Login-from-trusted-device
	// echoes this into the new token's amr claim so downstream
	// RequireMFA gates still see an MFA-marked session.
	GrantedAMR    string     `bson:"grantedAmr" json:"grantedAmr"`
	IsActive      bool       `bson:"isActive" json:"isActive"`
	RevokedAt     *time.Time `bson:"revokedAt,omitempty" json:"revokedAt,omitempty"`
	RevokedReason string     `bson:"revokedReason,omitempty" json:"revokedReason,omitempty"`
	CreatedAt     time.Time  `bson:"createdAt" json:"createdAt"`
	UpdatedAt     time.Time  `bson:"updatedAt" json:"updatedAt"`
}

// DeviceTrustRevokeReason values for the RevokedReason field. Kept as a
// small constant set so operators have a clean vocabulary on audit
// trails and PII exports.
const (
	DeviceTrustRevokedByUser           = "user_initiated"
	DeviceTrustRevokedOnPasswordChange = "password_changed"
	DeviceTrustRevokedOnMFARemove      = "mfa_factor_removed"
	DeviceTrustRevokedOnAdminReset     = "admin_mfa_reset"
	DeviceTrustRevokedReplaced         = "superseded_by_new_grant"
)

// DeviceTrustAMR is the annotation stamped on the new token's amr claim
// when a login skips the MFA prompt via device trust. Non-standard
// (RFC 8176 doesn't define it) — used alongside the prior factor value
// so downstream consumers can distinguish a fresh OTP from a trust
// passthrough. amrSatisfiesMFA does not match this string alone.
const DeviceTrustAMR = "device_trust"
