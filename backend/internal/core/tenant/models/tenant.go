// Package models holds the tenant aggregate, membership, invite, and the
// closure-table row for the tenant hierarchy. ADR-0001 reframes the old
// "Org" aggregate as the unified Tenant aggregate with a Kind discriminator
// for the two-tier architecture.
package models

import (
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// TenantKind discriminates the two tenant tiers.
//
//   - Internal: the companies that run Orkestra (operator side). They manage
//     their own users, FatturaPA invoicing, billing, and decide which modules
//     are enabled for the platform.
//   - External: client tenants that registered on the platform. They subscribe
//     to Orkestra-exposed services via the subscriptions + payments modules and
//     consume those services scoped to their own data. External tenants may be
//     multi-tenant themselves via ParentTenantUUID.
type TenantKind string

const (
	TenantKindInternal TenantKind = "internal"
	TenantKindExternal TenantKind = "external"
)

// Valid returns true if the TenantKind is a known value. Empty is treated as
// invalid; callers must always set Kind explicitly.
func (k TenantKind) Valid() bool {
	switch k {
	case TenantKindInternal, TenantKindExternal:
		return true
	}
	return false
}

// TenantStatus drives the tenant lifecycle.
//
//	provisioning → active ↔ suspended
//	                     ↘ archived → purged (terminal, triggers crypto-shred)
type TenantStatus string

const (
	TenantStatusProvisioning TenantStatus = "provisioning"
	TenantStatusActive       TenantStatus = "active"
	TenantStatusSuspended    TenantStatus = "suspended"
	TenantStatusArchived     TenantStatus = "archived"
	TenantStatusPurged       TenantStatus = "purged"
)

// Valid returns true for a known lifecycle value. Empty is invalid.
func (s TenantStatus) Valid() bool {
	switch s {
	case TenantStatusProvisioning, TenantStatusActive, TenantStatusSuspended,
		TenantStatusArchived, TenantStatusPurged:
		return true
	}
	return false
}

// SignupChannel records how a tenant was created so we can segment onboarding
// analytics and drive different welcome-email paths.
const (
	SignupChannelSelfServe     = "self_serve"
	SignupChannelSalesAssisted = "sales_assisted"
	SignupChannelSeeded        = "seeded"
	SignupChannelInvite        = "invite"
)

// Plan names. Kept for the short transition window until Phase 2
// capability-based entitlements ship. Do not add new plan values — introduce
// subscription products instead.
const (
	PlanFree       = "free"
	PlanPro        = "pro"
	PlanEnterprise = "enterprise"
)

// ContactInfo is the primary point of contact for a tenant. Used for billing
// notifications, subscription events, and DSR correspondence.
type ContactInfo struct {
	Email string `bson:"email,omitempty" json:"email,omitempty"`
	Phone string `bson:"phone,omitempty" json:"phone,omitempty"`
	Name  string `bson:"name,omitempty" json:"name,omitempty"`
}

// TenantAddress is the billing / legal address. Called TenantAddress (not
// Address) to avoid Huma schema collisions with other modules that also
// export an Address type.
type TenantAddress struct {
	Line1      string `bson:"line1,omitempty" json:"line1,omitempty"`
	Line2      string `bson:"line2,omitempty" json:"line2,omitempty"`
	City       string `bson:"city,omitempty" json:"city,omitempty"`
	Province   string `bson:"province,omitempty" json:"province,omitempty"`
	PostalCode string `bson:"postalCode,omitempty" json:"postalCode,omitempty"`
	Country    string `bson:"country,omitempty" json:"country,omitempty"`
}

// FatturaPAProfile carries the FatturaPA-specific routing fields a tenant
// needs to act as a CessionarioCommittente on an electronic invoice. Required
// when Tenant.IsItalianBillable is true; optional otherwise. Either
// CodiceDestinatario or PECDestinatario must be present at send time —
// CodiceDestinatario is the seven-character SDI code; PECDestinatario is the
// fallback PEC address used when the recipient hasn't registered an SDI code.
// IsPA toggles the public-administration variant; CodiceUfficio,
// RiferimentoAmm, ConvenzioneNumero are the PA-only routing fields populated
// when IsPA is true.
type FatturaPAProfile struct {
	CodiceDestinatario string `bson:"codiceDestinatario,omitempty" json:"codiceDestinatario,omitempty"`
	PECDestinatario    string `bson:"pecDestinatario,omitempty" json:"pecDestinatario,omitempty"`
	IsPA               bool   `bson:"isPA,omitempty" json:"isPA,omitempty"`
	CodiceUfficio      string `bson:"codiceUfficio,omitempty" json:"codiceUfficio,omitempty"`
	RiferimentoAmm     string `bson:"riferimentoAmm,omitempty" json:"riferimentoAmm,omitempty"`
	ConvenzioneNumero  string `bson:"convenzioneNumero,omitempty" json:"convenzioneNumero,omitempty"`
}

// HasRouting reports whether the profile carries at least one routing handle
// (SDI code or PEC). FatturaPA send-time validation rejects a profile that
// has neither.
func (p *FatturaPAProfile) HasRouting() bool {
	if p == nil {
		return false
	}
	return strings.TrimSpace(p.CodiceDestinatario) != "" ||
		strings.TrimSpace(p.PECDestinatario) != ""
}

// Tenant is the unified aggregate for the two-tier tenancy model.
type Tenant struct {
	ID primitive.ObjectID `bson:"_id,omitempty" json:"-"`

	UUID string `bson:"uuid" json:"id" validate:"required"`

	// Kind discriminates internal (operator) vs external (client) tenants.
	Kind TenantKind `bson:"kind" json:"kind"`

	// ParentTenantUUID supports hierarchical external tenants (clients that
	// are themselves multi-tenant with sub-workspaces). Nil for root tenants.
	// Internal tenants SHOULD NOT have a parent (enforced at service layer).
	ParentTenantUUID *string `bson:"parentTenantUUID,omitempty" json:"parentTenantUUID,omitempty"`

	// Status drives the tenant lifecycle.
	Status TenantStatus `bson:"status" json:"status"`

	// Display + identity.
	Name        string `bson:"name" json:"name" validate:"required,min=1,max=120"`
	Slug        string `bson:"slug" json:"slug" validate:"required,min=1,max=80"`
	LegalName   string `bson:"legalName,omitempty" json:"legalName,omitempty"`
	DisplayName string `bson:"displayName,omitempty" json:"displayName,omitempty"`

	// Ownership + contact.
	OwnerUserUUID  string        `bson:"ownerUserUUID" json:"ownerUserUUID"`
	PrimaryContact ContactInfo   `bson:"primaryContact,omitempty" json:"primaryContact,omitempty"`
	BillingAddress TenantAddress `bson:"billingAddress,omitempty" json:"billingAddress,omitempty"`

	// Italian tax identifiers. Relevant for Tier-1 FatturaPA and for external
	// EU B2B tenants once the billing module is exposed as a service.
	VATNumber  string `bson:"vatNumber,omitempty" json:"vatNumber,omitempty"`
	FiscalCode string `bson:"fiscalCode,omitempty" json:"fiscalCode,omitempty"`

	// IsCompany discriminates the legal-entity shape. False = natural person /
	// sole-proprietor / freelancer (the FatturaPA CedentePrestatore party name
	// is rendered from the owner User's profile). True = corporate entity
	// (LegalName is the canonical party name). Mandatory after the Unified
	// Client Aggregate refactor, but zero-defaults to false on legacy rows so
	// the additive Phase 1 migration is safe — Phase 5 enforces the discriminator
	// when collapsing billing.Customer into this struct.
	IsCompany bool `bson:"isCompany,omitempty" json:"isCompany,omitempty"`

	// IsItalianBillable toggles the FatturaPA invoicing path for this tenant.
	// When true, FatturaPA must carry either a CodiceDestinatario or a
	// PECDestinatario — SetItalianBillable enforces this at toggle time and
	// the billing send path enforces it again at send time.
	IsItalianBillable bool `bson:"isItalianBillable,omitempty" json:"isItalianBillable,omitempty"`

	// FatturaPA holds the SDI routing fields used by the billing module when
	// emitting a FatturaPA XML for this tenant. Nil = not configured. Pointer
	// so the BSON document omits the sub-document entirely on tenants that
	// will never invoice (most external Tier-2 clients pre-onboarding).
	FatturaPA *FatturaPAProfile `bson:"fatturaPA,omitempty" json:"fatturaPA,omitempty"`

	// SignupChannel — see signup constants above.
	SignupChannel string `bson:"signupChannel,omitempty" json:"signupChannel,omitempty"`

	// IdPConfigUUID — BYO identity provider (Phase 3). Nil = platform default.
	IdPConfigUUID *string `bson:"idpConfigUUID,omitempty" json:"-"`

	// RetentionPolicyID — per-tenant data retention (Phase 4). Nil = platform default.
	RetentionPolicyID *string `bson:"retentionPolicyID,omitempty" json:"-"`

	// KMSKeyID — per-tenant envelope encryption key (Phase 4). Reserved now.
	KMSKeyID *string `bson:"kmsKeyID,omitempty" json:"-"`

	// Region — reserved for multi-region routing. Default "eu-west".
	Region string `bson:"region,omitempty" json:"region,omitempty"`

	// StripeCustomerID is the Stripe customer for external tenants.
	StripeCustomerID string `bson:"stripeCustomerID,omitempty" json:"stripeCustomerID,omitempty"`

	// Metadata is a free-form map for callers that need to stash a small
	// amount of tenant-scoped key-value state.
	Metadata map[string]string `bson:"metadata,omitempty" json:"metadata,omitempty"`

	// Plan is an informational label (e.g. "free", "pro", "enterprise") kept
	// for admin UI display only. Feature access is driven by capability
	// entitlements — see the entitlement projection and the capability
	// registry.
	Plan string `bson:"plan,omitempty" json:"plan,omitempty"`

	// Settings is a free-form UI config blob.
	Settings map[string]string `bson:"settings,omitempty" json:"settings,omitempty"`

	CreatedAt  time.Time  `bson:"createdAt" json:"createdAt"`
	UpdatedAt  time.Time  `bson:"updatedAt" json:"updatedAt"`
	ArchivedAt *time.Time `bson:"archivedAt,omitempty" json:"archivedAt,omitempty"`
	PurgedAt   *time.Time `bson:"purgedAt,omitempty" json:"purgedAt,omitempty"`

	// DeletedAt is retained transitionally for lazy callers. New code uses
	// Status=archived instead.
	//
	// Deprecated: use Status.
	DeletedAt *time.Time `bson:"deletedAt,omitempty" json:"-"`
}

// IsInternal reports whether the tenant is a Tier-1 operator tenant.
func (t *Tenant) IsInternal() bool { return t.Kind == TenantKindInternal }

// IsExternal reports whether the tenant is a Tier-2 client tenant.
func (t *Tenant) IsExternal() bool { return t.Kind == TenantKindExternal }

// IsActive reports whether the tenant is in an operational state.
func (t *Tenant) IsActive() bool { return t.Status == TenantStatusActive }

// TenantAncestor is one row of the transitive-closure hierarchy table
// materialized for external multi-tenant clients. Every tenant has a
// self-row with depth=0 and one row per ancestor up to the root.
//
// Queries:
//   - "ancestors of X": find {descendantUUID: X}.
//   - "descendants of X": find {ancestorUUID: X}.
//   - "is X an ancestor of Y": exists {ancestorUUID: X, descendantUUID: Y}.
type TenantAncestor struct {
	ID             primitive.ObjectID `bson:"_id,omitempty" json:"-"`
	DescendantUUID string             `bson:"descendantUUID" json:"descendantUUID"`
	AncestorUUID   string             `bson:"ancestorUUID" json:"ancestorUUID"`
	Depth          int                `bson:"depth" json:"depth"`
	CreatedAt      time.Time          `bson:"createdAt" json:"createdAt"`
}

// TenantMembership links a user to a tenant with a set of role names. A user
// with no membership cannot access that tenant.
//
// TenantKind is cached here so middleware can dispatch on tier without an
// extra tenant lookup.
type TenantMembership struct {
	ID         primitive.ObjectID `bson:"_id,omitempty" json:"-"`
	UUID       string             `bson:"uuid" json:"id"`
	UserUUID   string             `bson:"userUUID" json:"userUUID" validate:"required"`
	TenantUUID string             `bson:"tenantId" json:"tenantId" validate:"required"`
	TenantKind TenantKind         `bson:"tenantKind,omitempty" json:"tenantKind,omitempty"`
	Roles      []string           `bson:"roles" json:"roles"`
	IsOwner    bool               `bson:"isOwner" json:"isOwner"`
	InvitedBy  string             `bson:"invitedBy,omitempty" json:"invitedBy,omitempty"`
	JoinedAt   time.Time          `bson:"joinedAt" json:"joinedAt"`
	ExpiresAt  *time.Time         `bson:"expiresAt,omitempty" json:"expiresAt,omitempty"`
}

// TenantInvite is a pending invitation for a user to join a tenant. Tokens
// are single-use, expire via a TTL index on ExpiresAt, and are stored as
// SHA-256 hashes rather than in plaintext.
//
// Token is transient (bson:"-"): populated once on create and returned to the
// caller, never persisted. TokenHash is the SHA-256 hex digest.
type TenantInvite struct {
	ID         primitive.ObjectID `bson:"_id,omitempty" json:"-"`
	UUID       string             `bson:"uuid" json:"id"`
	TenantUUID string             `bson:"tenantId" json:"tenantId"`
	Email      string             `bson:"email" json:"email"`
	Roles      []string           `bson:"roles" json:"roles"`
	Token      string             `bson:"-" json:"token,omitempty"`
	TokenHash  string             `bson:"tokenHash" json:"-"`
	InvitedBy  string             `bson:"invitedBy" json:"invitedBy"`
	CreatedAt  time.Time          `bson:"createdAt" json:"createdAt"`
	ExpiresAt  time.Time          `bson:"expiresAt" json:"expiresAt"`
	AcceptedAt *time.Time         `bson:"acceptedAt,omitempty" json:"acceptedAt,omitempty"`
}

// --- API DTOs ---

type CreateTenantInput struct {
	Name string `json:"name" validate:"required,min=1,max=120"`
	Slug string `json:"slug" validate:"required,min=1,max=80"`
	Plan string `json:"plan,omitempty"`
	// Kind lets administrators explicitly create an external tenant via the
	// operator console. Empty defaults to internal so the setup wizard and
	// tests keep working. External client self-registration in Phase 3 goes
	// through a dedicated handler that always stamps Kind=external.
	Kind TenantKind `json:"kind,omitempty"`
	// ParentTenantUUID lets an external-tenant admin create a sub-tenant.
	// Ignored for internal tenants.
	ParentTenantUUID *string `json:"parentTenantUUID,omitempty"`
}

type UpdateTenantInput struct {
	Name     *string           `json:"name,omitempty"`
	Slug     *string           `json:"slug,omitempty"`
	Settings map[string]string `json:"settings,omitempty"`
}

type UpdatePlanInput struct {
	Plan string `json:"plan" validate:"required"`
}

type InviteInput struct {
	Email string   `json:"email" validate:"required,email"`
	Roles []string `json:"roles" validate:"required,min=1"`
}

type AcceptInviteInput struct {
	Token string `json:"token" validate:"required"`
}

type TenantListResponse struct {
	Tenants []Tenant `json:"tenants"`
}

type MembershipListResponse struct {
	Memberships []TenantMembership `json:"memberships"`
}
