package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// TenantKind discriminates the two tenant tiers (see ADR-0001).
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

// TenantStatus drives the tenant lifecycle. Replaces the previous boolean
// soft-delete model. See ADR-0001.
//
//	provisioning → active → suspended ↔ active
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

// Plan names used by the tenant module. Kept for the short transition window
// until Phase 2 capability-based entitlements ship (see ADR-0001 and
// project_tenancy_plan_v2 memory). Do not add new plan values — introduce
// subscription products instead.
const (
	PlanFree       = "free"
	PlanPro        = "pro"
	PlanEnterprise = "enterprise"
)

// Feature keys are the entitlements a plan can grant.
//
// Deprecated: superseded by capability-based entitlements in Phase 2.
const FeatureWildcard = "*"

// ContactInfo is the primary point of contact for a tenant. Used for billing
// notifications, subscription events, and DSR correspondence.
type ContactInfo struct {
	Email string `bson:"email,omitempty" json:"email,omitempty"`
	Phone string `bson:"phone,omitempty" json:"phone,omitempty"`
	Name  string `bson:"name,omitempty" json:"name,omitempty"`
}

// TenantAddress is the billing / legal address. Called TenantAddress (not
// Address) to avoid Huma schema collisions with other modules that also
// export an Address type (company, subscriptions.ClientAddress).
type TenantAddress struct {
	Line1      string `bson:"line1,omitempty" json:"line1,omitempty"`
	Line2      string `bson:"line2,omitempty" json:"line2,omitempty"`
	City       string `bson:"city,omitempty" json:"city,omitempty"`
	Province   string `bson:"province,omitempty" json:"province,omitempty"`
	PostalCode string `bson:"postalCode,omitempty" json:"postalCode,omitempty"`
	Country    string `bson:"country,omitempty" json:"country,omitempty"`
}

// Org is the tenant aggregate. The Go type name is kept as Org during the
// transitional window to bound the blast radius of the Phase 0 rename — it
// will be renamed to Tenant in a follow-up commit per ADR-0001. Semantically
// this IS the Tenant aggregate: two-tier (Kind), hierarchical (ParentTenantUUID),
// lifecycle-managed (Status).
type Org struct {
	ID primitive.ObjectID `bson:"_id,omitempty" json:"-"`

	UUID string `bson:"uuid" json:"id" validate:"required"`

	// Kind discriminates internal (operator) vs external (client) tenants.
	// See ADR-0001. Defaults to internal at the repository layer if unset,
	// preserving the implicit semantics of pre-ADR-0001 rows. New code MUST
	// set Kind explicitly.
	Kind TenantKind `bson:"kind" json:"kind"`

	// ParentTenantUUID supports hierarchical external tenants (clients that
	// are themselves multi-tenant with sub-workspaces). Nil for root tenants.
	// Internal tenants SHOULD NOT have a parent (enforced at service layer).
	ParentTenantUUID *string `bson:"parentTenantUUID,omitempty" json:"parentTenantUUID,omitempty"`

	// Status drives the tenant lifecycle. Replaces the boolean DeletedAt soft
	// delete. DeletedAt is retained for a transitional period so existing
	// queries that filter on it keep working; once every call site moves to
	// Status checks, DeletedAt will be removed.
	Status TenantStatus `bson:"status" json:"status"`

	// Display + identity.
	Name        string `bson:"name" json:"name" validate:"required,min=1,max=120"`
	Slug        string `bson:"slug" json:"slug" validate:"required,min=1,max=80"`
	LegalName   string `bson:"legalName,omitempty" json:"legalName,omitempty"`
	DisplayName string `bson:"displayName,omitempty" json:"displayName,omitempty"`

	// Ownership + contact.
	OwnerUserUUID  string      `bson:"ownerUserUUID" json:"ownerUserUUID"`
	PrimaryContact ContactInfo `bson:"primaryContact,omitempty" json:"primaryContact,omitempty"`
	BillingAddress TenantAddress `bson:"billingAddress,omitempty" json:"billingAddress,omitempty"`

	// Italian tax identifiers. Relevant for Tier-1 FatturaPA and for external
	// EU B2B tenants once the billing module is exposed as a service.
	VATNumber  string `bson:"vatNumber,omitempty" json:"vatNumber,omitempty"`
	FiscalCode string `bson:"fiscalCode,omitempty" json:"fiscalCode,omitempty"`

	// SignupChannel — see signup constants above.
	SignupChannel string `bson:"signupChannel,omitempty" json:"signupChannel,omitempty"`

	// IdPConfigUUID — BYO identity provider (Phase 3). Nil = platform default.
	IdPConfigUUID *string `bson:"idpConfigUUID,omitempty" json:"-"`

	// RetentionPolicyID — per-tenant data retention (Phase 4). Nil = platform default.
	RetentionPolicyID *string `bson:"retentionPolicyID,omitempty" json:"-"`

	// KMSKeyID — per-tenant envelope encryption key (Phase 4). Reserved now;
	// populated when the KMS integration lands. GDPR crypto-shred on purge
	// destroys this key.
	KMSKeyID *string `bson:"kmsKeyID,omitempty" json:"-"`

	// Region — reserved for multi-region routing. Default "eu-west".
	Region string `bson:"region,omitempty" json:"region,omitempty"`

	// StripeCustomerID mirrors what used to live on subscriptions.Client.
	// For external tenants this is the Stripe customer the subscription
	// module charges; for internal tenants it is unused.
	StripeCustomerID string `bson:"stripeCustomerID,omitempty" json:"stripeCustomerID,omitempty"`

	// Metadata is a free-form map for callers that need to stash a small
	// amount of tenant-scoped key-value state (e.g. feature flags scoped to
	// one customer). Do not use for anything that needs to be queryable —
	// model it as a first-class field instead.
	Metadata map[string]string `bson:"metadata,omitempty" json:"metadata,omitempty"`

	// Plan + Features — deprecated. Kept to keep callers compiling during
	// the Phase 2 capability+entitlement rewrite. Do not read these from
	// new code; use the subscription entitlements projection once it ships.
	Plan     string   `bson:"plan,omitempty" json:"plan,omitempty"`
	Features []string `bson:"features,omitempty" json:"features,omitempty"`

	// Settings is a free-form UI config blob. Kept as-is for the frontend.
	Settings map[string]string `bson:"settings,omitempty" json:"settings,omitempty"`

	CreatedAt  time.Time  `bson:"createdAt" json:"createdAt"`
	UpdatedAt  time.Time  `bson:"updatedAt" json:"updatedAt"`
	ArchivedAt *time.Time `bson:"archivedAt,omitempty" json:"archivedAt,omitempty"`
	PurgedAt   *time.Time `bson:"purgedAt,omitempty" json:"purgedAt,omitempty"`

	// DeletedAt is retained during the Status migration. New code should
	// set Status=archived instead and leave DeletedAt untouched. Existing
	// queries filter deletedAt=null to hide archived rows; this is fine for
	// the transition because SoftDelete sets both fields.
	//
	// Deprecated: use Status.
	DeletedAt *time.Time `bson:"deletedAt,omitempty" json:"-"`
}

// IsInternal reports whether the tenant is a Tier-1 operator tenant.
func (o *Org) IsInternal() bool { return o.Kind == TenantKindInternal }

// IsExternal reports whether the tenant is a Tier-2 client tenant.
func (o *Org) IsExternal() bool { return o.Kind == TenantKindExternal }

// IsActive reports whether the tenant is in an operational state. Returns
// false for provisioning/suspended/archived/purged rows.
func (o *Org) IsActive() bool { return o.Status == TenantStatusActive }

// HasFeature reports whether the org's plan includes the given feature.
//
// Deprecated: feature entitlements move to capability-based subscriptions
// in Phase 2 (see ADR-0001).
func (o *Org) HasFeature(feature string) bool {
	for _, f := range o.Features {
		if f == FeatureWildcard || f == feature {
			return true
		}
	}
	return false
}

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

// Membership links a user to an org with a set of role names (defined in
// the authz module). A user with no membership cannot access that org.
//
// In Phase 1 (Cedar authorization) the Roles slice will be reinterpreted as
// Cedar role entities; for now it is still the denormalized list of authz
// role names. TenantKind is cached here so middleware can dispatch on tier
// without an extra tenant lookup.
type Membership struct {
	ID          primitive.ObjectID `bson:"_id,omitempty" json:"-"`
	UUID        string             `bson:"uuid" json:"id"`
	UserUUID    string             `bson:"userUUID" json:"userUUID" validate:"required"`
	OrgUUID     string             `bson:"orgId" json:"orgId" validate:"required"`
	TenantKind  TenantKind         `bson:"tenantKind,omitempty" json:"tenantKind,omitempty"`
	Roles       []string           `bson:"roles" json:"roles"`
	IsOwner     bool               `bson:"isOwner" json:"isOwner"`
	InvitedBy   string             `bson:"invitedBy,omitempty" json:"invitedBy,omitempty"`
	JoinedAt    time.Time          `bson:"joinedAt" json:"joinedAt"`
	ExpiresAt   *time.Time         `bson:"expiresAt,omitempty" json:"expiresAt,omitempty"`
}

// Invite is a pending invitation for a user to join an org. Tokens are
// single-use, expire automatically via a TTL index on ExpiresAt, and are
// stored as SHA-256 hashes rather than in plaintext — an attacker with DB
// read access cannot replay a pending invite.
//
// Token is a transient field (bson:"-"): the service populates it with the
// raw token exactly once on CreateInvite and returns it to the caller in the
// create response. It is never persisted to MongoDB. TokenHash is the
// SHA-256 hex digest of the raw token and is the field queried on accept.
type Invite struct {
	ID         primitive.ObjectID `bson:"_id,omitempty" json:"-"`
	UUID       string             `bson:"uuid" json:"id"`
	OrgUUID    string             `bson:"orgId" json:"orgId"`
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

type CreateOrgInput struct {
	Name string `json:"name" validate:"required,min=1,max=120"`
	Slug string `json:"slug" validate:"required,min=1,max=80"`
	Plan string `json:"plan,omitempty"`
	// Kind lets administrators explicitly create an external tenant via the
	// operator console. Empty defaults to internal so the pre-ADR-0001
	// CreateOrg flow (used by the setup wizard and tests) keeps working.
	// External client self-registration goes through a different handler
	// (Phase 3) that always stamps Kind=external regardless of this field.
	Kind TenantKind `json:"kind,omitempty"`
	// ParentTenantUUID lets an external-tenant admin create a sub-tenant.
	// Ignored for internal tenants.
	ParentTenantUUID *string `json:"parentTenantUUID,omitempty"`
}

type UpdateOrgInput struct {
	Name     *string           `json:"name,omitempty"`
	Slug     *string           `json:"slug,omitempty"`
	Settings map[string]string `json:"settings,omitempty"`
}

type UpdatePlanInput struct {
	Plan     string   `json:"plan" validate:"required"`
	Features []string `json:"features"`
}

type InviteInput struct {
	Email string   `json:"email" validate:"required,email"`
	Roles []string `json:"roles" validate:"required,min=1"`
}

type AcceptInviteInput struct {
	Token string `json:"token" validate:"required"`
}

type OrgListResponse struct {
	Orgs []Org `json:"orgs"`
}

type MembershipListResponse struct {
	Memberships []Membership `json:"memberships"`
}
