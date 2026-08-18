package models

import "time"

// ServiceAccountCredentialsCollection is a platform-global machine-credential
// store — deliberately not tier-split (see collections.go for the
// operator/client tier split most auth collections use). Service accounts
// are an operator-surface-only concept: they authenticate machine callers
// (CI jobs, integrations, agents) rather than a human sitting on either the
// operator or client tier, so there is no per-tier row to keep separate.
// Secrets are stored argon2id-hashed; the plaintext client secret is
// returned to the caller exactly once at creation/rotation time and never
// persisted.
const ServiceAccountCredentialsCollection = "service_account_credentials"

// ServiceAccountCredential is one machine credential bound to a service
// account. UUID identifies the credential row itself (a service account can
// hold more than one active credential across a rotation window); ClientID
// is the public identifier presented at auth time; SecretHash is the
// argon2id hash of the client secret.
type ServiceAccountCredential struct {
	UUID       string     `bson:"uuid"`
	UserUUID   string     `bson:"userUuid"`
	ClientID   string     `bson:"clientId"`
	SecretHash string     `bson:"secretHash"`
	Label      string     `bson:"label,omitempty"`
	CreatedAt  time.Time  `bson:"createdAt"`
	LastUsedAt *time.Time `bson:"lastUsedAt,omitempty"`
	RevokedAt  *time.Time `bson:"revokedAt,omitempty"`
}
