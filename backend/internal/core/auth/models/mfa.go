package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// MFAFactorType enumerates the kinds of second-factor authentication supported.
// A user may enroll one factor of each type; the unique (userUuid, type) index
// on the collection enforces that. WebAuthn factors hold zero-or-many
// credentials inside the doc — the doc's presence is the "factor exists"
// signal, the credential array carries the per-authenticator state.
type MFAFactorType string

const (
	MFAFactorTOTP     MFAFactorType = "totp"
	MFAFactorWebAuthn MFAFactorType = "webauthn"
)

// MFAStatus communicates the per-user enrollment state to the frontend.
// The "required" variants are populated by Block B once role-based MFA
// enforcement is wired in; Block A only reports enrolled vs not_required.
type MFAStatus string

const (
	MFAStatusNotRequired               MFAStatus = "not_required"
	MFAStatusRequiredPendingEnrollment MFAStatus = "required_pending_enrollment"
	MFAStatusEnrolled                  MFAStatus = "enrolled"
	MFAStatusGrace                     MFAStatus = "grace"
)

// MFAFactorDoc represents a stored second-factor binding for a user.
// Secrets are AES-256-GCM encrypted and backup codes are argon2id hashed,
// so a database dump does not expose live credentials. WebAuthn credentials
// store only public keys + counters, so no encryption is required for them.
type MFAFactorDoc struct {
	ID   primitive.ObjectID `bson:"_id,omitempty" json:"-"`
	UUID string             `bson:"uuid" json:"id"`
	// Tier is "operator" / "client" / "" — see auth/models.collections.go
	// OAuthProviderDoc.Tier comment (ADR-0003 PR-D).
	Tier              string        `bson:"tier,omitempty" json:"-"`
	UserUUID          string        `bson:"userUuid" json:"userUuid"`
	Type              MFAFactorType `bson:"type" json:"type"`
	SecretEnc         string        `bson:"secretEnc,omitempty" json:"-"`
	VerifiedAt        *time.Time    `bson:"verifiedAt,omitempty" json:"verifiedAt,omitempty"`
	CreatedAt         time.Time     `bson:"createdAt" json:"createdAt"`
	LastUsedAt        *time.Time    `bson:"lastUsedAt,omitempty" json:"lastUsedAt,omitempty"`
	BackupCodesHashed []string      `bson:"backupCodesHashed,omitempty" json:"-"`
	// LastUsedStep is the most recent TOTP step index (unix / period)
	// accepted for this factor. Verify refuses any candidate whose step
	// is ≤ LastUsedStep, preventing replay of a just-captured code within
	// its 30s window.
	LastUsedStep int64 `bson:"lastUsedStep,omitempty" json:"-"`
	// WebAuthnCredentials is the list of registered passkeys for this user
	// when Type == webauthn. Each entry tracks its own sign counter and
	// last-used timestamp so cloned-authenticator detection works per
	// credential. Only populated for webauthn factors; empty for TOTP.
	WebAuthnCredentials []WebAuthnCredential `bson:"webauthnCredentials,omitempty" json:"-"`
}

// WebAuthnCredential is a single registered passkey. Field shapes mirror the
// go-webauthn library's webauthn.Credential so we can hand them straight to
// the library at login time. The credential ID and public key are the public
// half of an asymmetric keypair — the user's authenticator owns the private
// key — so neither needs encryption at rest.
type WebAuthnCredential struct {
	// CredentialID is the raw bytes returned by the authenticator. Stored
	// as a byte slice (BSON binary) — base64url-encoded only at the API
	// boundary so the wire format stays compatible with the W3C JSON.
	CredentialID    []byte   `bson:"credentialId" json:"credentialId"`
	PublicKey       []byte   `bson:"publicKey" json:"publicKey"`
	AttestationType string   `bson:"attestationType,omitempty" json:"attestationType,omitempty"`
	AAGUID          []byte   `bson:"aaguid,omitempty" json:"aaguid,omitempty"`
	SignCount       uint32   `bson:"signCount" json:"signCount"`
	CloneWarning    bool     `bson:"cloneWarning,omitempty" json:"cloneWarning,omitempty"`
	Transports      []string `bson:"transports,omitempty" json:"transports,omitempty"`
	UserVerified    bool     `bson:"userVerified,omitempty" json:"userVerified,omitempty"`
	BackupEligible  bool     `bson:"backupEligible,omitempty" json:"backupEligible,omitempty"`
	BackupState     bool     `bson:"backupState,omitempty" json:"backupState,omitempty"`
	// Name is the user-supplied label so the settings UI can show
	// "Yubikey 5C" or "iPhone Touch ID" rather than a hex blob.
	Name       string     `bson:"name" json:"name"`
	CreatedAt  time.Time  `bson:"createdAt" json:"createdAt"`
	LastUsedAt *time.Time `bson:"lastUsedAt,omitempty" json:"lastUsedAt,omitempty"`
}
