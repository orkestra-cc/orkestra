// Package models holds the persisted shapes for the identity module. A
// single document type lives here in v1: IdPConfig, one per tenant, scoped
// by tenantId. Richer protocols (SAML) will live beside OIDC on the same
// collection with a Protocol discriminator when they land.
package models

import "time"

// IdPConfigsCollection is the Mongo collection name. Stored with the module
// directory prefix (`identity_`) per the collection-naming convention.
const IdPConfigsCollection = "identity_idp_configs"

// Protocols supported by the identity module. Constants rather than free
// strings so a typo at a call site becomes a compile error.
const (
	ProtocolOIDC = "oidc"
)

// IdPConfig is a per-tenant configuration of an external identity provider.
//
// Tenant scope: every document carries `tenantId` so `tenantrepo.Scope`
// can gate reads/writes. The `(tenantId, protocol)` composite is unique —
// one OIDC config per external tenant in v1. Extending to multi-IdP-per-
// tenant (e.g. customer wants to federate two upstreams) requires adding a
// `name` field and dropping the composite uniqueness.
//
// Secret handling: ClientSecret is stored encrypted using the shared
// OAuth-token envelope (AES-256-GCM with OAUTH_TOKEN_ENCRYPTION_KEY). It is
// never returned by admin GET endpoints — the handler redacts it.
type IdPConfig struct {
	UUID     string `bson:"uuid" json:"uuid"`
	TenantID string `bson:"tenantId" json:"tenantId"`
	Protocol string `bson:"protocol" json:"protocol"`

	// Display name shown in the admin UI ("Acme Okta", "Staff SSO", etc.).
	DisplayName string `bson:"displayName" json:"displayName"`

	// OIDC-specific fields. IssuerURL is the base (no trailing slash) that
	// discovery appends `/.well-known/openid-configuration` to.
	IssuerURL string `bson:"issuerURL" json:"issuerURL"`
	ClientID  string `bson:"clientId" json:"clientId"`
	// ClientSecret is the AES-256-GCM-encrypted client secret. Handlers
	// return "***" (or the empty string) in place of this field — never the
	// ciphertext.
	ClientSecret string   `bson:"clientSecret" json:"-"`
	RedirectURL  string   `bson:"redirectURL" json:"redirectURL"`
	Scopes       []string `bson:"scopes" json:"scopes"`

	// Claim mappings. Empty strings fall back to the OIDC defaults
	// (`sub`, `email`, `name`). A tenant using a non-standard IdP can
	// override any of them without code changes.
	SubClaim   string `bson:"subClaim,omitempty" json:"subClaim,omitempty"`
	EmailClaim string `bson:"emailClaim,omitempty" json:"emailClaim,omitempty"`
	NameClaim  string `bson:"nameClaim,omitempty" json:"nameClaim,omitempty"`

	// Enabled gates the public login endpoints. Storing the config with
	// Enabled=false is a legitimate "pre-configure, flip on later" flow.
	Enabled bool `bson:"enabled" json:"enabled"`

	CreatedAt time.Time `bson:"createdAt" json:"createdAt"`
	UpdatedAt time.Time `bson:"updatedAt" json:"updatedAt"`
}

// EffectiveSubClaim returns SubClaim or the OIDC default.
func (c *IdPConfig) EffectiveSubClaim() string {
	if c.SubClaim != "" {
		return c.SubClaim
	}
	return "sub"
}

// EffectiveEmailClaim returns EmailClaim or the OIDC default.
func (c *IdPConfig) EffectiveEmailClaim() string {
	if c.EmailClaim != "" {
		return c.EmailClaim
	}
	return "email"
}

// EffectiveNameClaim returns NameClaim or the OIDC default.
func (c *IdPConfig) EffectiveNameClaim() string {
	if c.NameClaim != "" {
		return c.NameClaim
	}
	return "name"
}
