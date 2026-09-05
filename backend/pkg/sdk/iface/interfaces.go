// Package iface defines cross-module service interfaces for the shared kernel.
//
// These interfaces live in their own package (not shared/module) to avoid
// import cycles: shared/module → shared/middleware → auth/services, so
// auth/services cannot import shared/module. This package only imports
// leaf model packages, keeping the dependency graph acyclic.
//
// Provider implementations satisfy these via Go structural typing —
// no explicit "implements" declaration is needed.
package iface

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// ObjectStore — S3-compatible object storage (avatars, attachments, …)
// Promoted from internal/shared/blob so addons can consume it through the
// SDK seam. internal/shared/blob keeps `type Store = iface.ObjectStore` and
// `type PresignedPut = iface.PresignedPut` aliases for existing callers.
// ---------------------------------------------------------------------------

// PresignedPut groups a presigned upload URL with the headers the client
// must echo verbatim on the PUT for the signature to validate.
type PresignedPut struct {
	URL       string
	Headers   map[string]string
	Key       string
	ExpiresAt time.Time
}

// ObjectStore is a bucket-pinned S3-compatible object-storage handle.
// Implementations are safe for concurrent use.
type ObjectStore interface {
	// PresignPut mints a URL the client PUTs to directly; the signer pins
	// the content-type. Size limits are the caller's responsibility.
	PresignPut(ctx context.Context, key, contentType string, ttl time.Duration) (*PresignedPut, error)
	// Put streams a server-assembled payload to the backend, bypassing the
	// presigned dance. Overwrites an existing key.
	Put(ctx context.Context, key, contentType string, body io.Reader) error
	// PresignGet returns a short-lived URL the client GETs to read the blob.
	PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error)
	// Delete removes an object. Missing keys do not error (idempotent).
	Delete(ctx context.Context, key string) error
	// Exists is a HEAD-style probe: false/nil when missing, true/nil when
	// present; network/auth errors propagate.
	Exists(ctx context.Context, key string) (bool, error)
}

// ObjectStoreProvider vends a bucket-pinned ObjectStore per logical domain
// (e.g. "avatars", "crm-photos"). One connection, many buckets.
type ObjectStoreProvider interface {
	Bucket(domain string) (ObjectStore, error)
}

// ---------------------------------------------------------------------------
// UserProvider — consumed by: auth
// Matches the subset of user.UserService that auth actually calls.
// ---------------------------------------------------------------------------

type UserProvider interface {
	GetUserByID(ctx context.Context, id string) (*User, error)
	GetUserByEmail(ctx context.Context, email string) (*UserManagementResponse, error)
	// GetUserForAuth returns the raw user (including password hash and
	// lockout fields) for authentication flows. Do NOT use for regular
	// user lookups — the response contains sensitive fields.
	GetUserForAuth(ctx context.Context, email string) (*User, error)
	CreateUserFromOAuth(ctx context.Context, input *CreateUserInput) (*User, error)
	// CreateUserWithPassword creates a new user with a pre-hashed password.
	CreateUserWithPassword(ctx context.Context, input *CreateUserInput) (*User, error)
	// UpdatePasswordHash stores a new argon2id hash and timestamps the change.
	UpdatePasswordHash(ctx context.Context, userUUID, hash string) error
	// MarkEmailVerified flips emailVerified to true.
	MarkEmailVerified(ctx context.Context, userUUID string) error
	// RecordFailedLogin increments failed counter and optionally sets a lockout.
	RecordFailedLogin(ctx context.Context, userUUID string, lockUntil *time.Time) error
	// ClearFailedLogins resets the counter after a successful login.
	ClearFailedLogins(ctx context.Context, userUUID string) error
	UpdateUser(ctx context.Context, id string, input *UpdateUserInput) (*UserManagementResponse, error)
	UpdateUserLastLogin(ctx context.Context, id string) error
	DeleteUser(ctx context.Context, id string) error
	// SoftDeleteAndAliasEmail soft-deletes the user and renames the
	// email to a one-shot alias so the unique email index no longer
	// blocks a fresh signup. Used by the tenant cascade-delete hook
	// when an external Tier-2 tenant is dropped and its owner has no
	// other live memberships. Idempotent.
	SoftDeleteAndAliasEmail(ctx context.Context, userUUID string) error
	GetUserOAuthLinks(ctx context.Context, userUUID string) ([]OAuthLink, error)
	// AddOAuthLinkToUser appends an OAuth identity to the user's
	// embedded OAuthLinks slice. Used by the self-service "add a
	// sign-in provider" flow on /user/security and by any future
	// caller that needs to bind a new identity to an existing
	// account. Mirrors RemoveOAuthLinkFromUser.
	AddOAuthLinkToUser(ctx context.Context, userUUID string, link OAuthLink) error
	RemoveOAuthLinkFromUser(ctx context.Context, userUUID string, provider OAuthProvider, providerID string) error
	SetPrimaryOAuthLink(ctx context.Context, userUUID string, provider OAuthProvider, providerID string) error
	GetUserCount(ctx context.Context, filters *UserFilters) (int64, error)

	// StartMFAGraceIfUnset stamps MFAGraceStartedAt on the user if nil.
	// Idempotent — an existing grace timestamp is preserved so repeated
	// privileged logins during the grace window don't keep resetting the
	// countdown.
	StartMFAGraceIfUnset(ctx context.Context, userUUID string) error
	// ResetMFAGrace unconditionally restarts the grace clock. Used by the
	// admin MFA reset flow after a factor is deleted so the target must
	// re-enroll within the full window, regardless of prior state.
	ResetMFAGrace(ctx context.Context, userUUID string) error
	// ClearMFAGrace removes the grace stamp — called on successful
	// enrollment so a later privilege revoke → re-grant starts afresh.
	ClearMFAGrace(ctx context.Context, userUUID string) error
}

// ---------------------------------------------------------------------------
// UserLifecycleStateProvider — consumed by: shared/setup (finalizer-access
// probe + recovery). Narrow on purpose: UserProvider.GetUserByID excludes
// soft-deleted rows and so cannot distinguish `missing` from `deleted` for
// recovery audit; this seam resolves ONLY the lifecycle class and returns
// no profile data. The wide UserProvider is not widened.
// ---------------------------------------------------------------------------

type UserLifecycleState string

const (
	UserLifecycleActive   UserLifecycleState = "active"   // exists, not soft-deleted, IsActive
	UserLifecycleInactive UserLifecycleState = "inactive" // exists, not soft-deleted, !IsActive
	UserLifecycleDeleted  UserLifecycleState = "deleted"  // soft-deleted row
	UserLifecycleMissing  UserLifecycleState = "missing"  // no row
)

// UserLifecycleStateProvider classifies one operator user UUID. A database
// error is returned as an error and is distinct from every state — callers
// fail closed and must never map a lookup failure onto a lifecycle class.
type UserLifecycleStateProvider interface {
	UserLifecycleState(ctx context.Context, userUUID string) (UserLifecycleState, error)
}

// OAuthLinkDataUpdater is the additive sub-interface that lets the auth
// module refresh the embedded OAuthLink.OAuthData map (typically the
// cached `picture` URL) on every OAuth callback. Adding this method
// directly to UserProvider would break every external implementor; the
// auth callback paths type-assert against this interface and gracefully
// skip the refresh on implementations that don't satisfy it. Satisfied
// by the canonical user-module UserService.
type OAuthLinkDataUpdater interface {
	UpdateOAuthLinkData(ctx context.Context, userUUID string, provider OAuthProvider, providerID string, data map[string]interface{}) error
}

// ServiceAccountLister is an OPTIONAL extension a UserProvider may
// implement (additive-only rule; consumers type-assert). Lists machine
// principals for the operator admin surface.
type ServiceAccountLister interface {
	ListUsersByKind(ctx context.Context, kind string) ([]User, error)
}

// ---------------------------------------------------------------------------
// OperatorUserProvider / ClientUserProvider — ADR-0003 PR-B
// Tier-aware user-data interfaces. Same surface as UserProvider but each
// implementation is bound to its tier's collection (operator_users vs
// client_users) and stamps / asserts Tier on every read & write.
//
// At the PR-B boundary the legacy UserProvider remains authoritative;
// these interfaces are registered under separate ServiceRegistry keys
// (ServiceOperatorUserProvider / ServiceClientUserProvider) but no
// consumer reads them yet. PR-D is the cutover — auth flows switch off
// the legacy provider onto the audience-matching one.
//
// The named-interface distinction is a compile-time hint at the call
// site (you can read which audience a function targets from the type)
// but does not prevent injecting the wrong implementation. Real
// enforcement is the per-tier Tier guard inside the repository.
// ---------------------------------------------------------------------------

type OperatorUserProvider interface {
	UserProvider
}

type ClientUserProvider interface {
	UserProvider
}

// ---------------------------------------------------------------------------
// JWTProvider — consumed by: dev
// Only the method dev actually needs for token generation.
// ---------------------------------------------------------------------------

type JWTProvider interface {
	GenerateAccessToken(user *User) (string, error)
}

// TenantScopedTokenProvider is an OPTIONAL extension a JWTProvider may also
// implement. It mints an access token carrying an explicit acting tenant plus a
// synthetic membership, for principals that have no database membership — the
// dev-token endpoint's synthetic user. This lets a dev token satisfy
// tenant-scoped reads (billing/documents) that fail closed without an acting
// tenant. Declared as a new interface (not a method on JWTProvider) per the
// SDK additive-only rule; consumers type-assert for it.
//
// Consumed by: dev-token endpoint. Implemented by: auth jwtService.
type TenantScopedTokenProvider interface {
	GenerateAccessTokenForTenant(user *User, tenantUUID, tenantKind string, roles []string) (string, error)
}

// ---------------------------------------------------------------------------
// PasswordHasher — consumed by: user (admin-direct client-user create)
// Slim view of auth.PasswordService for callers that only need to hash
// and policy-check a plaintext password. Auth's PasswordService satisfies
// this via structural typing.
// ---------------------------------------------------------------------------

type PasswordHasher interface {
	Hash(plaintext string) (string, error)
	ValidatePolicy(ctx context.Context, plaintext, email string) error
}

// ---------------------------------------------------------------------------
// AdminAuthInviter — consumed by: user (admin-triggered Tier-2 flows)
// Slim view of auth.PasswordAuthService for the admin "Resend
// verification" / "Send password reset" / "Send invite" buttons on the
// client-user detail page. Each tier's *services.PasswordAuthService
// satisfies this via structural typing — the user module fetches the
// client-tier instance from the registry by key.
// ---------------------------------------------------------------------------

type AdminAuthInviter interface {
	AdminSendInvite(ctx context.Context, userUUID, inviterName string) error
	AdminResendVerification(ctx context.Context, userUUID string) error
	AdminTriggerPasswordReset(ctx context.Context, userUUID string) error
}

// ErrPasswordLoginDisabled reports that the email/password method is
// administratively disabled for the surface the target user signs in on
// (auth module keys passwordLoginEnabled{Admin,Client}). Declared here —
// not in the auth module — because AdminAuthInviter consumers in other
// modules must map it across the module boundary with errors.Is: the
// admin send-password-reset routes answer 409 with it. The auth module's
// services package aliases it, so both names are one identity.
var ErrPasswordLoginDisabled = errors.New("password login disabled for this surface")

// ErrAuthPolicyUnavailable reports that a persisted sign-in policy — or
// the auth config document it lives in — could not be read or parsed.
// The decision fails closed (503 auth.policy_unavailable), never open.
// Shares this file for the same reason as ErrPasswordLoginDisabled:
// AdminAuthInviter consumers map it with errors.Is.
var ErrAuthPolicyUnavailable = errors.New("auth policy unavailable")

// ErrUserNotFound is the cross-module "this user does not exist" sentinel.
//
// It exists because a consumer outside the user module — auth's refresh path,
// which must answer 401 for a deleted account and 503 for an unreachable store
// — cannot import internal/core/user/services to compare against its sentinel,
// and MUST NOT: reporting a storage outage as an authentication failure is
// exactly what ADR-0017 gave session enforcement its own 503 to prevent.
// user/services.ErrUserNotFound is an alias of this value, so every existing
// `return nil, ErrUserNotFound` classifies through errors.Is with no other
// change and no change to the message.
var ErrUserNotFound = errors.New("user not found")

// ---------------------------------------------------------------------------
// PDFProvider — consumed by: billing
// Only the methods billing's invoice service calls.
// ---------------------------------------------------------------------------

type PDFProvider interface {
	GenerateInvoicePDF(ctx context.Context, invoiceData map[string]interface{}, templateUUID string, generatedBy string) (*GeneratedDocument, error)
	GetDocumentContent(ctx context.Context, uuid string) ([]byte, string, error)
}

// ---------------------------------------------------------------------------
// GraphProvider — consumed by: rag
// The three execution methods rag uses for Cypher queries.
// ---------------------------------------------------------------------------

type GraphProvider interface {
	ExecuteRead(ctx context.Context, database string, cypher string, params map[string]interface{}) (*QueryResult, error)
	ExecuteWrite(ctx context.Context, database string, cypher string, params map[string]interface{}) (*QueryResult, error)
	ExecuteAutoCommit(ctx context.Context, database string, cypher string, params map[string]interface{}) error
}

// ---------------------------------------------------------------------------
// AIModelProvider — consumed by: rag, sales
// Union of the methods both modules need for embedding + LLM access.
// ---------------------------------------------------------------------------

type AIModelProvider interface {
	GetDefaultEmbeddingProvider(ctx context.Context) (EmbeddingProvider, error)
	GetDefaultLLMProvider(ctx context.Context) (LLMProvider, error)
	GetLLMProvider(ctx context.Context, uuid string) (LLMProvider, error)
	GetEmbeddingProvider(ctx context.Context, uuid string) (EmbeddingProvider, error)
	// GetDefaultLLMConfig returns the raw configuration of the default LLM
	// model (provider name, model name, API key, base URL). Consumed by
	// modules that need the underlying credentials, e.g. the agents
	// module which passes them to the Hindsight container as env vars.
	// Returns an error if no model is marked isDefault or if the module
	// is disabled.
	GetDefaultLLMConfig(ctx context.Context) (LLMConfig, error)
}

// LLMConfig is a serialization-friendly projection of an aimodels record
// containing the fields needed to configure an external LLM client.
type LLMConfig struct {
	Provider string // "openai" | "anthropic" | "gemini" | "ollama"
	Model    string // e.g. "gpt-4o-mini", "claude-3-5-sonnet"
	APIKey   string // plaintext — callers must not log this
	BaseURL  string // optional override for self-hosted / compat endpoints
}

// ---------------------------------------------------------------------------
// RAGQueryProvider — consumed by: agents
// The single query method the agents module wraps in its own RAGBridge.
// ---------------------------------------------------------------------------

type RAGQueryProvider interface {
	Query(ctx context.Context, question string, topK int, minScore float64, isoStandard, llmOverrideUUID, requirementLevel, nodeType, retrievalMode string, documentUUIDs []string) (*RAGQueryResponse, error)
}

// ---------------------------------------------------------------------------
// NotificationSender — consumed by: auth (verification, password reset),
// future consumers like billing (invoice delivery), sales (digest), etc.
//
// The notification module owns rendering, template lookup, preferences and
// delivery. Consumers only describe what they want sent, not how.
// ---------------------------------------------------------------------------

type Recipient struct {
	UserUUID string
	Address  string
	Name     string
}

type NotificationRequest struct {
	Channel        string
	Type           string // "transactional" | "marketing"
	Category       string // e.g. "auth.verify_email"
	Recipients     []Recipient
	Subject        string
	Body           string
	BodyHTML       string
	IdempotencyKey string
	Metadata       map[string]any
}

type TemplatedNotificationRequest struct {
	Channel        string
	Type           string
	Category       string
	TemplateID     string
	Locale         string
	Recipients     []Recipient
	Data           map[string]any
	IdempotencyKey string
	Metadata       map[string]any
}

type NotificationResult struct {
	ID       string
	Status   string // "sent" | "failed" | "suppressed" | "queued"
	Provider string
	Error    string
}

type NotificationSender interface {
	// IsConfigured returns true when the active email channel has valid
	// transport credentials. Consumers should check this at request time
	// to decide whether to fail fast or degrade gracefully.
	IsConfigured(ctx context.Context) bool

	// Send dispatches a fully pre-rendered notification.
	Send(ctx context.Context, req NotificationRequest) (*NotificationResult, error)

	// SendTemplated resolves a template from the notification module's
	// template store, renders it with the given data + auto-injected
	// unsubscribe variables, and dispatches it.
	SendTemplated(ctx context.Context, req TemplatedNotificationRequest) (*NotificationResult, error)
}

// CategoryConfiguredChecker is an OPTIONAL companion to NotificationSender
// (ADR-0019 D7). With sender profiles routed by category, IsConfigured's
// single boolean is wrong in both directions for a caller about to send one
// category; this answers for that category. A sender that does not
// implement it keeps working — IsConfiguredForCategory falls back.
type CategoryConfiguredChecker interface {
	IsConfiguredFor(ctx context.Context, category string) bool
}

// IsConfiguredForCategory is the accessor every pre-flight guard should use:
// the exact answer when s implements CategoryConfiguredChecker, the coarse
// IsConfigured otherwise, false for a nil sender. Follows the
// HasConfigGroups/ConfigGroupsOf idiom for extending a frozen contract.
func IsConfiguredForCategory(ctx context.Context, s NotificationSender, category string) bool {
	if s == nil {
		return false
	}
	if c, ok := s.(CategoryConfiguredChecker); ok {
		return c.IsConfiguredFor(ctx, category)
	}
	return s.IsConfigured(ctx)
}

// ---------------------------------------------------------------------------
// TenantProvider — consumed by: authz, auth (JWT issuance), middleware,
// every data module via the tenantrepo helper.
//
// The tenant module owns organizations, per-user memberships, and plan-based
// entitlements. Users have a global identity and belong to many orgs with
// possibly different roles in each.
// ---------------------------------------------------------------------------

// TenantKindInternal / TenantKindExternal mirror tenant/models.TenantKind.
// Redeclared here as constants on the iface package so consumers (auth JWT
// issuance, middleware, Cedar evaluator) do not have to import the tenant
// module to read the tier.
const (
	TenantKindInternal = "internal"
	TenantKindExternal = "external"
)

// TenantStatusActive is the only status that grants request access. All
// other statuses cause middleware to 403 or 503 as appropriate.
const (
	TenantStatusProvisioning = "provisioning"
	TenantStatusActive       = "active"
	TenantStatusSuspended    = "suspended"
	TenantStatusArchived     = "archived"
	TenantStatusPurged       = "purged"
)

// SignupChannelSelfServe mirrors tenant/models.SignupChannelSelfServe.
// Redeclared here so consumers (impersonation bypass, audit emitters)
// reading iface.Tenant.SignupChannel can match against the canonical
// "personal tenant" predicate (IsCompany=false + SignupChannel=self_serve)
// without importing the tenant module.
const (
	SignupChannelSelfServe = "self_serve"
)

// Tenant is the DTO shape the tenant module exposes across the module boundary.
type Tenant struct {
	UUID             string
	Kind             string // iface.TenantKindInternal | iface.TenantKindExternal
	ParentTenantUUID string // empty for root tenants
	Status           string // iface.TenantStatus*
	Name             string
	Slug             string
	// Plan is an informational label kept for UI and reporting. Access to
	// features is driven by capability entitlements (see HasCapability),
	// not by plan name.
	Plan string

	// Billing-relevant fields. Populated for external tenants so the
	// subscriptions renewal service can drive Stripe customer creation
	// without a separate cross-module fetch. All optional — empty values
	// mean the underlying tenant row hasn't been enriched yet.
	LegalName        string
	Email            string
	VATNumber        string
	FiscalCode       string
	Country          string
	StripeCustomerID string

	// IsCompany discriminates the legal-entity shape (false = natural person /
	// sole-proprietor, true = corporate entity). Combined with SignupChannel,
	// it identifies the canonical "personal tenant" predicate
	// (IsCompany=false + SignupChannel=SignupChannelSelfServe) consumed by
	// the impersonation bypass to require MFA step-up and to split the
	// admin.tenant.impersonate audit action between .personal and .business.
	IsCompany bool
	// SignupChannel records how the tenant was created — see the
	// tenant/models.SignupChannel* constants. Mirrored here so consumers
	// can match against SignupChannelSelfServe without importing the
	// tenant module.
	SignupChannel string
}

// TenantMembership is a user's membership in a tenant — identifying the
// tenant, its tier, and the role names the user holds there.
type TenantMembership struct {
	TenantUUID string
	TenantName string
	TenantSlug string
	TenantKind string   // iface.TenantKind* — lets consumers dispatch on tier without a tenant lookup
	Roles      []string // authz role names the user holds in this tenant
	IsOwner    bool
}

type TenantProvider interface {
	GetTenant(ctx context.Context, tenantUUID string) (*Tenant, error)
	ListUserMemberships(ctx context.Context, userUUID string) ([]TenantMembership, error)
	IsMember(ctx context.Context, userUUID, tenantUUID string) (bool, error)
	// ActivateTenant transitions a tenant from `provisioning` to `active`.
	// Idempotent: calling it on an already-active tenant is a no-op on the
	// status field. Does not validate the previous state — admin callers
	// can also use it to unsuspend, though that is not the primary use
	// case and richer transitions live on the concrete tenant service.
	ActivateTenant(ctx context.Context, tenantUUID string) error
	// SetTenantStripeCustomerID persists the Stripe customer identifier the
	// payment provider returned for the tenant. Called on the first charge
	// for an external tenant so subsequent renewal cycles skip customer
	// creation. Idempotent: re-applying the same value is a no-op.
	SetTenantStripeCustomerID(ctx context.Context, tenantUUID, stripeCustomerID string) error
	// EnsureTenantForUser is the lazy-provisioning seam introduced by the
	// Unified Client Aggregate refactor (Phase 1). It returns the user's
	// existing personal tenant — uniquely identified by
	// (Kind=external, IsCompany=false, SignupChannel=self_serve,
	// OwnerUserUUID=userUUID) — or creates one when none exists, with
	// Name=user.FullName (the User aggregate's display field). Idempotent:
	// concurrent calls collapse onto the first row that wins the slug-
	// uniqueness race; later callers receive the existing personal tenant.
	// Phase 2 wires this into subscriptions/payments callers behind a
	// feature flag; Phase 3 flips the flag and migrates legacy clientbilling
	// rows onto the resulting personal tenants.
	EnsureTenantForUser(ctx context.Context, userUUID string) (*Tenant, error)
}

// ---------------------------------------------------------------------------
// DefaultTenantProvider — consumed by: auth (operator JWT tenant-fallback
// seeding), the local dev-token resolver. Deliberately narrow: the wide
// TenantProvider is NOT widened (its test fakes and consumers stay intact).
// ---------------------------------------------------------------------------

// DefaultTenantProvider resolves the platform default Tier-1 tenant.
// Returns (nil, nil) when no default is assigned or the pointer's target is
// not operational (status active, not soft-deleted) — the provider never
// hands out a non-operational target. It grants nothing: membership
// validation, RBAC, audience checks, and X-Tenant-ID override all still
// apply downstream.
type DefaultTenantProvider interface {
	GetDefaultTenant(ctx context.Context) (*Tenant, error)
}

// ---------------------------------------------------------------------------
// BillingTenantProvider — consumed by: billing (Phase 5), payments self-
// service (Phase 5), invoice send path.
//
// Owns the billing-party resolution algorithm for the Unified Client Aggregate
// (Phase 1): walk up Tenant.ParentTenantUUID until a tenant with FatturaPA
// fields is found, then return a snapshot the consumer can stamp onto an
// invoice's CessionarioCommittente. Subscriptions stay attached to the
// consuming workspace; invoicing rolls up to the right legal entity in the
// hierarchy.
//
// Phase 1 wires the resolver but does not switch any consumer onto it —
// billing.invoice_service still uses the legacy billing.Customer lookup.
// Phase 5 deletes Customer and points Send at this seam.
// ---------------------------------------------------------------------------

// BillingParty is the snapshot the billing module stamps onto an invoice
// when it would otherwise reach for a billing.Customer row. Flat by design:
// invoice send is a one-way snapshot for legal-immutability reasons (FatturaPA
// requires the party fields to remain stable after issuance). Mirrors the
// fields the FatturaPA XML's CessionarioCommittente section carries.
type BillingParty struct {
	// TenantUUID is the tenant the resolver landed on after walking up the
	// parent chain. May differ from the requested tenantUUID when the input
	// is a division and billing rolls up to its parent.
	TenantUUID string
	// LegalName is the canonical party name. For IsCompany=true this is the
	// corporate Denominazione; for IsCompany=false the resolver falls back to
	// the owner User's FullName (rendered server-side; see ResolveBillingParty
	// implementation note).
	LegalName  string
	IsCompany  bool
	VATNumber  string
	FiscalCode string
	Country    string
	Email      string
	Address    BillingAddress
	FatturaPA  FatturaPAProfile
}

// BillingAddress is the snapshot variant of TenantAddress kept inside iface
// so consumers don't reach into the tenant module's models. Field shape
// matches tenant/models.TenantAddress one-for-one.
type BillingAddress struct {
	Line1      string
	Line2      string
	City       string
	Province   string
	PostalCode string
	Country    string
}

// FatturaPAProfile is the cross-module snapshot of tenant/models.FatturaPAProfile.
// Same field shape, repeated here so consumers (billing, payments) don't
// import the tenant model package.
type FatturaPAProfile struct {
	CodiceDestinatario string
	PECDestinatario    string
	IsPA               bool
	CodiceUfficio      string
	RiferimentoAmm     string
	ConvenzioneNumero  string
}

// ErrBillingPartyNotConfigured is the sentinel returned by ResolveBillingParty
// when neither the requested tenant nor any ancestor carries FatturaPA fields
// — i.e. the tenant is not yet ready to be billed via FatturaPA. Handlers map
// this to 422 so the SPA can prompt the operator to fill in the billing
// identity before sending an invoice.
var ErrBillingPartyNotConfigured = errors.New("billing: tenant has no FatturaPA profile in its ancestor chain")

// BillingTenantProvider resolves the legal-entity tenant a Tier-2 client
// (or one of its divisions) bills under, plus the snapshot the billing send
// path needs.
type BillingTenantProvider interface {
	// ResolveBillingParty walks up the parent chain from tenantUUID until it
	// finds a tenant whose IsItalianBillable=true AND FatturaPA carries at
	// least one routing field (CodiceDestinatario or PECDestinatario). Returns
	// ErrBillingPartyNotConfigured when the chain bottoms out with no match.
	ResolveBillingParty(ctx context.Context, tenantUUID string) (*BillingParty, error)
}

// ---------------------------------------------------------------------------
// AccessProvider — consumed by: middleware (RequireCapability), authz
// (Cedar principal builder), subscriptions (entitlement syncer).
//
// Owns the capability-entitlements projection. Entitlements are keyed by
// tenant UUID — every billable principal is a Tenant aggregate after the
// Unified Client Aggregate refactor. Internal operator tenants short-circuit
// to true: capabilities are the monetization seam for external clients only.
// ---------------------------------------------------------------------------

type AccessProvider interface {
	// HasCapability reports whether the tenant currently holds an active
	// entitlement to the capability ID. Returns false if no active row
	// exists, regardless of whether the capability ID is known in the
	// catalog — the catalog is advisory, the projection is authoritative.
	HasCapability(ctx context.Context, tenantUUID, capabilityID string) (bool, error)
	// GrantCapability creates an active entitlement for (tenant, capID).
	// If an active entitlement already exists for that pair, it is revoked
	// first so the one-active-per-(tenant, capability) invariant holds.
	// Safe to replay — the same input produces the same effective state,
	// with a fresh audit row.
	GrantCapability(ctx context.Context, in GrantCapabilityInput) error
	// RevokeCapability marks the active entitlement for (tenant, capID) as
	// revoked. Idempotent: a no-op if no active row exists.
	RevokeCapability(ctx context.Context, tenantUUID, capabilityID string) error
	// ListCapabilityIDs returns the capability IDs the tenant currently
	// holds active entitlements for, deduplicated and in deterministic
	// order.
	ListCapabilityIDs(ctx context.Context, tenantUUID string) ([]string, error)
}

// GrantCapabilityInput is the cross-module payload for granting a capability
// entitlement. Source/SourceRef tell the projection where the grant came
// from (subscription row, admin action, trial program) so it can be replayed
// or revoked by origin.
type GrantCapabilityInput struct {
	// TenantUUID is the tenant receiving the grant. Required — empty values
	// are rejected at the service layer.
	TenantUUID   string
	CapabilityID string
	// Source categorizes the grant. Known values: "subscription", "grant",
	// "trial". Matches models.EntitlementSource on the tenant side.
	Source    string
	SourceRef string
	GrantedBy string
	// ExpiresAt is an optional absolute deadline. Nil = no expiry (lifetime
	// of the grant); expired entitlements are treated as inactive.
	ExpiresAt *time.Time
	// Metadata is a free-form blob for provider-specific breadcrumbs. Opaque
	// to the projection.
	Metadata map[string]any
}

// ---------------------------------------------------------------------------
// AuthzProvider — consumed by: middleware (RequirePermission checks),
// auth (populating JWT with default system permissions).
//
// The authz module owns the permissions catalog, roles (system + custom per
// tenant), and role bindings with optional expiresAt. Permissions are not
// embedded in JWTs — they are resolved per-request so revocation is instant.
// ---------------------------------------------------------------------------

type PermissionSpec struct {
	Key         string // dot-notation: "billing.invoice.create"
	Module      string // owning module name
	Description string
	// System marks the permission as granted globally by system roles
	// (platform-wide, not org-scoped). E.g. "system.modules.admin".
	System bool
}

type AuthzProvider interface {
	// HasPermission checks whether the user has the permission in the given org.
	// If orgUUID is empty, only system-level grants are checked.
	HasPermission(ctx context.Context, userUUID, orgUUID, permission string) (bool, error)

	// GetEffectivePermissions returns the union of permissions from all
	// non-expired role bindings for (userUUID, orgUUID), plus any system
	// permissions granted globally by the user's system role.
	GetEffectivePermissions(ctx context.Context, userUUID, orgUUID string) ([]string, error)

	// RegisterPermissions upserts a module's permission catalog. Called by
	// the module registry during boot after the authz module is initialized.
	RegisterPermissions(ctx context.Context, specs []PermissionSpec) error
}

// ---------------------------------------------------------------------------
// AuthzCacheInvalidator — consumed by: the user module after a system-role
// change, and any module that mutates what a user is allowed to do without
// going through authz's own role/binding endpoints.
//
// Narrow and separate from AuthzProvider on purpose: AuthzProvider is
// implemented by forks, so it stays additive-only, and a consumer that only
// needs to retire a cached verdict should not have to satisfy the whole
// evaluator.
//
// Resolve it with module.GetTyped against ServiceAuthzProvider — the stored
// value's dynamic type is the authz service, which implements both.
//
// A consumer that only READS verdicts may treat a missing value as
// tolerable: the cached verdict expires on its own 60s TTL. A consumer that
// MUTATES must not treat it as nothing — but what it does about it depends
// on the DIRECTION of the mutation (D27 as amended):
//
//   - A mutation that GRANTS may be gated: a stale verdict after it is a
//     DENY, so refusing the write and asking the caller to retry costs only
//     a delay.
//   - A mutation that REVOKES is written first and the shortfall reported.
//     A stale verdict after a revocation is an ALLOW, and refusing the write
//     holds the privilege indefinitely where writing it holds it for at most
//     the cache TTL. With the cache store fully down, reads bypass the cache
//     anyway, so a written revocation takes effect at once.
//
// The base does NOT apply one uniform shape — the direction is decided per
// call site, and authz's own service is the worked example (three seams,
// all going through one bump helper):
//
//   - `withGeneration` — the gate. Used by `UpdateRole` for a patch that
//     only ADDS access (a permission set that is a superset of the current
//     one, or isActive:true) and by `CreateBinding`/`EnsureBinding` when a
//     real actor is the granter.
//   - `writeThenInvalidate` — write, then invalidate best-effort. Used by
//     `DeleteRole`, `DeleteBinding`, both tenant cascades, by `UpdateRole`
//     when the patch removes any key OR sets isActive:false, and by a
//     platform-issued binding grant (granter "system"), where a refusal
//     would abort a tenant flow midway rather than reach an operator who
//     could retry.
//   - `bindingGrantGeneration` — the one seam that picks between the two, so
//     `CreateBinding` and `EnsureBinding` cannot drift apart.
//
// DISABLING a role is a revocation, not a neutral toggle: the evaluator
// drops every permission of an inactive role, from every holder, so
// isActive:false takes the second shape. The permission set-difference
// cannot see that — the stored list does not change — which is why it is
// tested for explicitly. Enabling one is the granting direction.
//
// Two writes bump nothing at all, in either shape: `CreateRole` (a role it
// creates has no bindings, and the unique (tenantId, name) index keeps that
// true by refusing a taken name with a 409) and an `UpdateRole` patch that
// only touches name/description (the cache stores permission keys, which
// such a patch cannot move).
//
// The user module's system-role change is `writeThenInvalidate`-shaped and
// is never refused in either direction: refusing a demotion would keep the
// old role's verdicts alive indefinitely instead of for at most the TTL.
//
// Whatever the shape, the failure must be surfaced — logged, counted, and
// (for the system-role change) recorded in the audit row — never swallowed.
//
// There are THREE cache states, not two, and only the middle one is an
// error:
//
//   - No cache configured (no Redis wired). There is no cached verdict to
//     retire, so InvalidateUserPermissions returns nil. That success is
//     truthful, not a swallowed failure.
//   - Cache configured but UNAVAILABLE (the counter cannot be bumped). This
//     returns an error, and a mutating caller must act on it per the
//     direction rule above.
//   - Cache configured but bypassed on THIS replica (the Redis client in use
//     lacks the MGET the implementation needs). Reads and writes are skipped
//     locally — every check resolves from the database — but the bump is
//     still issued, because a peer replica that does have MGET holds entries
//     this mutation must retire, and no replica can know what its peers use.
//
// ---------------------------------------------------------------------------

type AuthzCacheInvalidator interface {
	// InvalidateUserPermissions retires, in one atomic operation, every
	// verdict cached for one user in every tenant BEFORE the call
	// returns. Returns an error the caller is expected to act on.
	//
	// It does NOT, on its own, cover a verdict a concurrent reader
	// computed before the call and writes back after it. The
	// implementation closes the deterministic half of that race — a
	// reader files its result under the generation it read the cache
	// with, so a verdict computed before this call is born unreachable —
	// but a reader that resolved the database across the call can still
	// publish a stale entry. A caller that GRANTS closes that window
	// with the pre-write / write / post-write protocol (D27):
	// invalidate, refuse the change if that fails, write, then
	// invalidate again best-effort. A caller that REVOKES writes first
	// and invalidates once, best-effort, because refusing a revocation
	// is worse than applying it late; the window is then bounded by the
	// cache TTL instead of closed. A caller whose write cannot change
	// any existing verdict (authz `CreateRole`, or a role rename) needs
	// neither. A single bump is sufficient only when the caller is not
	// itself the writer.
	InvalidateUserPermissions(ctx context.Context, userUUID string) error
}

// ---------------------------------------------------------------------------
// PaymentProvider — consumed by: subscriptions
//
// Façade over one or more payment gateways (Stripe in v1, PayPal later).
// The payments module registers an implementation in the ServiceRegistry;
// consumers use it through this interface so gateway code never leaks out.
//
// All monetary amounts are in minor currency units (e.g. cents). Currency
// is a 3-letter ISO code (EUR in v1).
// ---------------------------------------------------------------------------

type CustomerInput struct {
	// TenantUUID is the tenant the gateway customer represents. Echoed into
	// Stripe customer metadata so cross-system reconciliation can locate
	// the owning tenant from the gateway side.
	TenantUUID string
	Email      string
	Name       string
	VATNumber  string
	Country    string
	Metadata   map[string]string
}

type CustomerRef struct {
	Provider string // "stripe" | "paypal"
	ID       string // provider-side customer id (e.g. cus_xxx)
}

type PaymentMethodRef struct {
	Provider    string
	ID          string // e.g. pm_xxx
	Brand       string // e.g. "visa"
	Last4       string
	ExpiryMonth int
	ExpiryYear  int
}

type SubscriptionCharge struct {
	SubscriptionUUID string
	InvoiceUUID      string // used as idempotency key (provider metadata)
	// TenantUUID is the tenant the charge is attributed to. Stamped into
	// the resulting PaymentIntent metadata so the webhook reconciler can
	// route events back to the tenant without a join.
	TenantUUID    string
	Customer      CustomerRef
	PaymentMethod *PaymentMethodRef // nil = use default on customer
	AmountCents   int64
	Currency      string
	Description   string
	Metadata      map[string]string
}

type ChargeResult struct {
	ProviderTxID string // e.g. pi_xxx
	Status       string // "succeeded" | "requires_action" | "failed"
	FailureCode  string
	FailureMsg   string
	ChargedAt    time.Time
}

type RefundResult struct {
	ProviderRefundID string
	Status           string // "succeeded" | "pending" | "failed"
}

// WebhookEvent is the normalized payload the dispatcher passes to the
// SubscriptionReconciler after signature verification + dedupe.
type WebhookEvent struct {
	Provider         string
	ProviderEventID  string // used for dedupe (unique index on webhook_events)
	Type             string // provider-native event type, e.g. "payment_intent.succeeded"
	Normalized       string // "charge.succeeded" | "charge.failed" | "charge.refunded" | ""
	SubscriptionUUID string // decoded from metadata
	InvoiceUUID      string // decoded from metadata
	ProviderTxID     string
	ProviderRefundID string
	AmountCents      int64
	Currency         string
	FailureCode      string
	FailureMsg       string
	OccurredAt       time.Time
	RawPayload       map[string]any
}

type PaymentProvider interface {
	Name() string

	// CreateCustomer registers a client with the gateway and returns the
	// provider-side identifier. Called lazily before the first charge.
	CreateCustomer(ctx context.Context, in CustomerInput) (CustomerRef, error)

	// AttachPaymentMethod associates a tokenized payment method (from the
	// client-side SDK, e.g. Stripe Elements) with an existing customer.
	AttachPaymentMethod(ctx context.Context, customer CustomerRef, token string) (PaymentMethodRef, error)

	// ChargeSubscription synchronously charges a subscription invoice and
	// returns the immediate outcome. Webhooks later confirm settlement.
	ChargeSubscription(ctx context.Context, in SubscriptionCharge) (ChargeResult, error)

	// RefundCharge refunds an existing provider transaction. AmountCents = 0
	// refunds the full amount.
	RefundCharge(ctx context.Context, providerTxID string, amountCents int64, reason string) (RefundResult, error)

	// VerifyWebhook validates provider-specific signature headers and
	// decodes the raw body into a normalized WebhookEvent. Returns an
	// error if the signature is invalid.
	VerifyWebhook(ctx context.Context, rawBody []byte, headers map[string]string) (WebhookEvent, error)

	// CreateCheckoutSession opens a hosted-checkout session in payment mode
	// for a one-shot charge that also saves the chosen payment method on
	// the customer for future off-session renewals. The returned URL is the
	// gateway-hosted page the SPA redirects the user to.
	CreateCheckoutSession(ctx context.Context, in CheckoutSessionInput) (CheckoutSessionResult, error)

	// CreateSetupCheckoutSession opens a hosted-checkout session in setup
	// mode — no charge, only collects and stores a payment method on the
	// customer. Used by the "add card" flow on the client account page.
	CreateSetupCheckoutSession(ctx context.Context, in SetupCheckoutInput) (CheckoutSessionResult, error)
}

// CheckoutSessionInput parameterises a payment-mode hosted checkout. The
// gateway charges Customer for AmountCents in Currency once, surfaces the
// transaction under the metadata-stamped subscription/invoice, and saves
// the card off-session so the renewal job can reuse it next cycle.
type CheckoutSessionInput struct {
	Customer      CustomerRef
	AmountCents   int64
	Currency      string // ISO 4217 (e.g. "EUR")
	Description   string
	SuccessURL    string // absolute URL the gateway redirects to on success
	CancelURL     string // absolute URL the gateway redirects to on cancel
	CustomerEmail string // optional pre-fill for the checkout email field
	// Metadata is copied to the underlying PaymentIntent so the existing
	// webhook reconciler picks up subscriptionUUID/invoiceUUID/tenantUUID
	// without any new plumbing.
	Metadata map[string]string
}

// SetupCheckoutInput parameterises a setup-mode hosted checkout — collects
// a payment method against Customer with no charge.
type SetupCheckoutInput struct {
	Customer      CustomerRef
	SuccessURL    string
	CancelURL     string
	CustomerEmail string
	Metadata      map[string]string // copied to the underlying SetupIntent
}

// CheckoutSessionResult is the shape returned to the SPA — the hosted URL
// to redirect to plus the gateway-side session id for client-side recovery.
type CheckoutSessionResult struct {
	SessionID string
	URL       string
}

// ---------------------------------------------------------------------------
// SubscriptionReconciler — consumed by: payments (webhook dispatcher)
//
// Reverse-direction interface: the subscriptions module implements it so
// the payments module can update invoice/subscription state when Stripe
// fires webhook events. Keeps the two modules free of direct imports.
// ---------------------------------------------------------------------------

type SubscriptionReconciler interface {
	MarkInvoicePaid(ctx context.Context, invoiceUUID, providerTxID string, paidAt time.Time) error
	MarkInvoiceFailed(ctx context.Context, invoiceUUID, failureCode, failureMsg string) error
	RecordRefund(ctx context.Context, invoiceUUID, providerRefundID string, amountCents int64, reason string) error
}

// ---------------------------------------------------------------------------
// SelfServiceCheckoutPlanner — consumed by: payments self-service handler.
//
// Resolves a subscription UUID into the snapshot the payments module needs
// to open a hosted Stripe Checkout session: the owning tenant (for the
// ownership re-check), the pending invoice (for the metadata stamp the
// existing webhook reconciler keys off), and the catalog tier price + label.
//
// Implemented by the subscriptions module and registered under
// module.ServiceSelfServiceCheckoutPlanner. Optional from payments' point
// of view — when absent, the checkout-session route returns 503.
// ---------------------------------------------------------------------------

type CheckoutPlan struct {
	SubscriptionUUID string
	// TenantUUID is the tenant owning the subscription. Handlers re-check
	// ownership (caller is a member of the tenant) before opening the
	// gateway session.
	TenantUUID    string
	ServiceUUID   string
	ServiceName   string
	TierCode      string
	InvoiceUUID   string // pending invoice; required by the webhook reconciler
	InvoiceNumber string
	AmountCents   int64
	Currency      string
	Description   string // human-readable line for the Checkout page
}

// SelfServiceCheckoutPlanner translates a subscription UUID into a payable
// checkout snapshot. Returns ErrCheckoutNoPendingInvoice when the
// subscription has no invoice in `pending` status — the handler maps that
// to 409 so the SPA can prompt the user to retry the renewal cycle.
//
// Contract implementations MUST honor (audit M-2): the caller (the
// payments self-service handler) re-checks ownership against
// CheckoutPlan.TenantUUID on BOTH the error and success return paths,
// before it is allowed to translate an error into a distinguishable
// status code. Once the subscription itself has been resolved,
// PlanCheckoutSession must return TenantUUID populated on every
// subsequent error too — including ErrCheckoutNoPendingInvoice and any
// other error (a transient repository failure, a data-integrity gap in
// the catalog, etc.). Only "subscription UUID is unknown/unresolved" may
// return the zero-value CheckoutPlan{}. Returning a zero-value plan
// alongside any other error silently turns the legitimate owner's real
// status (409, or a bubbled 5xx) into an indistinguishable 404, because
// the handler cannot tell "unowned" apart from "owned but TenantUUID
// wasn't reported".
type SelfServiceCheckoutPlanner interface {
	PlanCheckoutSession(ctx context.Context, subscriptionUUID string) (CheckoutPlan, error)
}

// ErrCheckoutNoPendingInvoice is returned by SelfServiceCheckoutPlanner.
// PlanCheckoutSession when the subscription exists and is owned by the
// caller but has no invoice in `pending` status to charge. Handlers map
// this to a 409 response so the SPA can guide the user to wait for the
// next renewal tick or trigger a retry-charge first.
//
// The plan returned alongside this error must NOT be the zero value —
// see the "Contract implementations MUST honor" note on
// SelfServiceCheckoutPlanner: TenantUUID has to be populated or the
// handler's ownership check masks the 409 as a 404 even for the owner.
var ErrCheckoutNoPendingInvoice = errors.New("self-service checkout: no pending invoice for subscription")

// ---------------------------------------------------------------------------
// SessionTerminator — consumed by: any module that can revoke a principal's
// right to be signed in (the user module's deactivate / delete paths).
// Provided by the auth module; satisfied by *services.AuthService.
//
// The auth refresh paths already refuse a user whose account is no longer
// active, which caps a revoked principal's remaining access at one
// access-token TTL. This interface closes the rest of that window: it
// revokes the refresh tokens, flips the session documents, and pushes
// every sid into the revocation set so bearers already in flight stop
// working on their next request.
//
// Resolve it with module.GetTyped against ServiceAuthService (operator
// tier) or ServiceClientAuthService (client tier). A nil result means the
// auth module is not wired — callers must degrade to "the account is
// disabled and the refresh path will catch it" rather than erroring, so
// that lifecycle changes never fail on a missing optional collaborator.
// ---------------------------------------------------------------------------

// SessionTerminator ends every active session belonging to a user.
type SessionTerminator interface {
	// TerminateAllSessionsByUUID revokes the user's refresh tokens,
	// deactivates their session documents, and revokes their session ids.
	// Callers treat a returned error as advisory: the state change that
	// prompted the call has already been persisted.
	TerminateAllSessionsByUUID(ctx context.Context, userUUID string) error
}

// ---------------------------------------------------------------------------
// AuditSink — consumed by: every module that performs a security-sensitive
// or lifecycle-changing action. Provided by the compliance module.
//
// Emit is fire-and-forget: the sink logs internal failures but never
// surfaces them, so callers on the hot path do not need to branch on the
// return value. Consumers resolve the sink with module.GetTyped — a nil
// result means the compliance module is disabled, in which case callers
// should simply skip the emit rather than erroring.
// ---------------------------------------------------------------------------

// AuditEvent is the cross-module shape consumers hand to AuditSink.Emit.
// Fields mirror the persisted compliance_audit_events record but are kept
// here so consumers never import the compliance package.
//
// Field semantics:
//   - TenantID / TenantKind — the tenant this action took place in. Leave
//     empty for platform-level events (no acting tenant).
//   - ActorUserID / ActorEmail — the authenticated principal. Leave empty
//     for system or anonymous actors.
//   - ActorType — "user" | "system" | "anonymous". When empty, the sink
//     infers "user" if ActorUserID is set, otherwise "system".
//   - Action — dotted hierarchy identifier, e.g. "auth.login.succeeded".
//     Consumers use the constants in compliance/models/audit_event.go.
//   - ResourceType / ResourceID — the subject of the action (e.g.
//     "tenant" / tenantUUID, "user" / userUUID).
//   - Outcome — "success" | "failure" | "denied". Empty defaults to
//     "success".
//   - IPAddress / UserAgent — request provenance. Safe to leave empty for
//     non-HTTP emitters.
//   - Metadata — free-form structured payload, opaque to the sink.
type AuditEvent struct {
	TenantID     string
	TenantKind   string
	ActorUserID  string
	ActorEmail   string
	ActorType    string
	Action       string
	ResourceType string
	ResourceID   string
	Outcome      string
	IPAddress    string
	UserAgent    string
	Metadata     map[string]any
}

// AuditSink persists audit events on behalf of consumer modules.
// Implementations must be safe for concurrent use and must not block the
// caller on backend failures.
type AuditSink interface {
	// Emit appends one audit event. The sink stamps UUID + Timestamp;
	// callers populate only the semantic fields. Errors are logged
	// internally and never returned — auditing must never break the
	// hot path.
	Emit(ctx context.Context, event AuditEvent)
}

// LoginTokens is the shape an external authentication flow (OIDC,
// SAML, etc.) needs back from the auth module after the user has
// been verified. Consumed by the identity addon's OIDC service to
// complete a federated login and return tokens to the caller.
type LoginTokens struct {
	AccessToken  string
	RefreshToken string
	TokenType    string
	ExpiresIn    int64
	User         *UserManagementResponse
}

// LoginTokenIssuer mints a fresh Orkestra session for an already-
// verified user. Implemented by core/auth's PasswordAuthService via
// `IssueLoginTokensExternal`; consumed by extracted addons (today:
// identity) that need to bridge an external authentication into a
// platform session without importing concrete auth-service types.
//
// `amr` is the RFC 8176 authentication-methods reference list
// describing how the user proved identity (e.g. `["oidc"]`).
type LoginTokenIssuer interface {
	IssueLoginTokensExternal(ctx context.Context, user *User, deviceID, platform, ip string, amr []string) (*LoginTokens, error)
}

// AuditSinkSetter is satisfied by any module-level service that
// receives the platform AuditSink during compliance's post-init
// wiring. The pattern lets compliance push its sink into the auth,
// tenant, identity, and subscription services without those services
// importing compliance — they only expose a SetAuditSink method.
//
// Compliance probes the kernel's ServiceRegistry with
// `module.GetTyped[iface.AuditSinkSetter](svcs, key)` and calls
// SetAuditSink when the assertion succeeds; missing or
// non-matching services are silently skipped, which is the desired
// behaviour because compliance is optional and the consumers register
// independently.
type AuditSinkSetter interface {
	SetAuditSink(AuditSink)
}

// KMSProviderSetter is satisfied by services that opt into per-tenant
// envelope encryption + crypto-shred on purge (currently:
// core/tenant.Service). Compliance probes for it from its own Init
// after constructing the KMS provider — the receiver caches the
// provider and uses it during tenant purge.
type KMSProviderSetter interface {
	SetKMSProvider(KMSProvider)
}

// ClientSelfDeletionGate is the slice of core/auth.AuthPolicyService
// compliance needs to gate the Tier-2 self-service DSR erasure flow.
// When the policy is closed the client-tier erase route is not
// mounted; when it's open every call re-checks live so admin toggles
// take effect without a restart.
type ClientSelfDeletionGate interface {
	SelfServiceAccountDeletionClient(ctx context.Context) bool
}

// ---------------------------------------------------------------------------
// PIIProducer — consumed by: the compliance module's DSR service.
//
// Modules that hold personal data tied to a user UUID implement this
// interface and register themselves with the PIIProducerRegistry during
// their own Init. The compliance module enumerates registered producers
// to satisfy GDPR right-of-access (export) and right-to-erasure (purge)
// requests — Phase 4.2 of ADR-0001.
// ---------------------------------------------------------------------------

// PersonalDataBundle is the exported payload returned by the DSR
// pipeline. Keys are producer subject names (stable identifiers like
// "user", "auth"); values are each producer's serializable personal-data
// projection. JSON-round-trippable so the bundle can be written to a
// file or streamed to the DSR requester.
type PersonalDataBundle = map[string]any

// EraseMode selects how a PIIProducer satisfies a right-to-erasure request.
// Producers branch on it: hard-delete removes rows outright; anonymize strips
// PII in place where a tombstone must survive for referential integrity (the
// canonical user identity row), falling back to hard-delete for satellite
// rows that carry no anonymizable residue.
type EraseMode int

const (
	// EraseHardDelete removes every matching row outright — the default,
	// strongest erasure.
	EraseHardDelete EraseMode = iota
	// EraseAnonymize strips personal data in place but keeps the row where
	// referential integrity requires it (e.g. the user identity row keeps
	// its UUID, with the email aliased and profile fields blanked). The
	// retention job later hard-deletes the anonymized tombstone.
	EraseAnonymize
)

// PurgeResult summarizes the data a single producer erased. Bundled into
// the DSR audit event so auditors can reconstruct exactly what was wiped
// without reading the now-deleted rows.
type PurgeResult struct {
	RowsDeleted    int      `json:"rowsDeleted"`
	RowsAnonymized int      `json:"rowsAnonymized"`
	Collections    []string `json:"collections,omitempty"`
}

// PIIProducer is implemented by modules that hold personal data tied to
// a user UUID. The compliance module's DSR service enumerates registered
// producers.
type PIIProducer interface {
	// Subject is the stable identifier used as the bundle-key for this
	// producer's data. Must not change after first use — DSR audit rows
	// reference it by value.
	Subject() string

	// ExportPersonalData returns a JSON-serializable projection of every
	// record the producer holds for userUUID. Returning (nil, nil) is
	// interpreted as "no data held" and the key is omitted from the
	// bundle — errors bubble up so the caller can decide whether to
	// include partial results or abort.
	ExportPersonalData(ctx context.Context, userUUID string) (any, error)

	// PurgePersonalData erases every record the producer holds for userUUID
	// per the requested EraseMode (hard-delete or anonymize). GDPR
	// right-to-erasure semantics apply. Returns a summary so the DSR audit
	// log can record what was wiped without re-reading the rows.
	PurgePersonalData(ctx context.Context, userUUID string, mode EraseMode) (PurgeResult, error)
}

// PIIProducerRegistry is the boot-time catalog of registered producers.
// cmd/server/main.go constructs it before InitAll so producer modules
// can register during their own Init. The compliance module resolves
// the registry when servicing DSR requests.
//
// Concurrency: safe for concurrent reads and writes. In practice writes
// happen serially during boot and reads happen during request handling,
// but the locking makes the life-cycle explicit.
type PIIProducerRegistry struct {
	mu        sync.RWMutex
	producers []PIIProducer
}

// NewPIIProducerRegistry constructs an empty registry.
func NewPIIProducerRegistry() *PIIProducerRegistry { return &PIIProducerRegistry{} }

// Register appends p to the producer list. Idempotent only in the sense
// that duplicate registrations are allowed — the DSR service happily
// runs both, and the resulting bundle key collisions are the module
// authors' responsibility to avoid by picking unique Subject values.
func (r *PIIProducerRegistry) Register(p PIIProducer) {
	if p == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.producers = append(r.producers, p)
}

// List returns a snapshot of registered producers. Safe to call from
// request handlers — the returned slice is a copy so callers can iterate
// without holding the lock.
func (r *PIIProducerRegistry) List() []PIIProducer {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]PIIProducer, len(r.producers))
	copy(out, r.producers)
	return out
}

// ---------------------------------------------------------------------------
// KMSProvider — consumed by: tenant (creates a key on provisioning,
// shreds on purge) and any module that envelope-encrypts per-tenant
// data (e.g. a future PII-field encryption layer).
//
// Implementations: local Mongo-backed provider in compliance (dev +
// simple prod deployments), AWS KMS provider in a later phase (prod HSM
// backing). The interface is intentionally narrow so both satisfy it.
// ---------------------------------------------------------------------------

// ErrKMSKeyNotFound is the sentinel returned when a key lookup fails,
// either because the key never existed or because it was shredded.
// Defined here so callers can branch on it without importing the
// compliance implementation.
var ErrKMSKeyNotFound = newStringError("kms: key not found")

// ErrKMSKeyDeleted signals the key exists in the metadata table but its
// wrapped DEK has been scheduled for deletion (crypto-shred). Decrypt
// returns this when called against a shredded key.
var ErrKMSKeyDeleted = newStringError("kms: key scheduled for deletion")

// KMSProvider manages per-tenant envelope-encryption keys. Each
// tenant's data is encrypted with a tenant-scoped Data Encryption Key
// (DEK); the DEK is wrapped with a master key never exposed to
// consumers. Shredding a tenant's DEK ( DeleteKey ) makes every
// ciphertext encrypted with it cryptographically unrecoverable — the
// GDPR crypto-shred primitive.
type KMSProvider interface {
	// CreateKey mints a fresh DEK for tenantUUID, wraps it with the
	// master key, persists the wrapped form, and returns the opaque
	// keyID callers stamp on their tenant row. Idempotent at the
	// tenant level AND under concurrency: two racing calls for one
	// tenantUUID both return the single winning keyID (implementations
	// back this with a unique tenant index + duplicate-key reread).
	// A caller may replay this call after a crash or a lost response —
	// e.g. a resumable provisioning saga retrying a stage — and must
	// never observe a second DEK for the same tenant.
	CreateKey(ctx context.Context, tenantUUID string) (keyID string, err error)

	// Encrypt seals plaintext with the tenant's DEK. The returned
	// ciphertext is self-contained (carries the nonce + auth tag)
	// and safe to store alongside other tenant-scoped data.
	Encrypt(ctx context.Context, keyID string, plaintext []byte) (ciphertext []byte, err error)

	// Decrypt opens ciphertext previously produced by Encrypt with
	// the same keyID. Returns ErrKMSKeyDeleted if the key has been
	// shredded — the "dead" signal crypto-shred relies on.
	Decrypt(ctx context.Context, keyID string, ciphertext []byte) (plaintext []byte, err error)

	// DeleteKey performs crypto-shred: the wrapped DEK is destroyed
	// (or marked for destruction, depending on the backend). After
	// this call, any ciphertext encrypted with keyID is unrecoverable
	// — there is no undo even by an operator with DB access, because
	// the DEK no longer exists anywhere. Call on PurgeTenant.
	DeleteKey(ctx context.Context, keyID string) error
}

// newStringError is a tiny helper so iface can declare sentinels
// without pulling in errors.New at package top-level formatting. Kept
// unexported.
func newStringError(s string) error { return &stringError{s: s} }

type stringError struct{ s string }

func (e *stringError) Error() string { return e.s }

// CRMActivityInput describes a billing event worth a human follow-up.
//
// Email is how the CRM finds the person: the billing stack and the CRM keep
// separate directories, and Email is the only field both hold.
type CRMActivityInput struct {
	TenantUUID string
	Email      string
	Kind       string // "payment_failed" | "subscription_suspended" | "manual_payment_required"
	Summary    string
	Metadata   map[string]string
}

// CRMActivitySink — implemented by: crm. Consumed by: subscriptions.
//
// Records a billing event against an existing CRM person so someone can act
// on it. Implementations MUST NOT create a person that does not exist: the
// CRM carries explicit consent records and a lawful basis per contact, and
// enrolling someone off the back of a failed charge is not a decision a
// billing notifier gets to make. No match = log and return nil.
type CRMActivitySink interface {
	RecordActivity(ctx context.Context, in CRMActivityInput) error
}
