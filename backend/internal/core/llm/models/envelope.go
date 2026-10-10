package models

// EnvelopeAlg names how a secret was sealed.
const (
	EnvelopeAlgKMS   = "kms"   // iface.KMSProvider, per-tenant DEK
	EnvelopeAlgLocal = "local" // AES-256-GCM with LLM_SECRET_ENCRYPTION_KEY
)

// EnvelopeSchemaVersion is part of the AAD: bumping it invalidates every
// stored ciphertext on purpose.
const EnvelopeSchemaVersion = 1

// Envelope is the stored form of every secret the module owns (API keys
// now, OAuth tokens in PR 3). Ciphertext is base64 (std) of the sealed
// bytes. The display-only last four characters live on the owning
// document (Credential.SecretLast4), never inside the envelope.
type Envelope struct {
	Alg           string `bson:"alg" json:"-"`
	KeyID         string `bson:"keyId,omitempty" json:"-"`
	KeyVersion    int    `bson:"keyVersion" json:"-"`
	SchemaVersion int    `bson:"schemaVersion" json:"-"`
	Ciphertext    string `bson:"ciphertext" json:"-"`
}

// IsZero reports an unset envelope (no secret stored).
func (e Envelope) IsZero() bool { return e.Ciphertext == "" }

// Last4 returns the final four characters of s, or "" when s is shorter
// than 8 — a shorter secret would leak most of itself.
func Last4(s string) string {
	if len(s) < 8 {
		return ""
	}
	return s[len(s)-4:]
}
