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
	"sync"
	"time"

	aiProviders "github.com/orkestra/backend/internal/addons/aimodels/providers"
	docModels "github.com/orkestra/backend/internal/addons/documents/models"
	graphModels "github.com/orkestra/backend/internal/addons/graph/models"
	ragModels "github.com/orkestra/backend/internal/addons/rag/models"
	userModels "github.com/orkestra/backend/internal/core/user/models"
)

// ---------------------------------------------------------------------------
// UserProvider — consumed by: auth
// Matches the subset of user.UserService that auth actually calls.
// ---------------------------------------------------------------------------

type UserProvider interface {
	GetUserByID(ctx context.Context, id string) (*userModels.User, error)
	GetUserByEmail(ctx context.Context, email string) (*userModels.UserManagementResponse, error)
	// GetUserForAuth returns the raw user (including password hash and
	// lockout fields) for authentication flows. Do NOT use for regular
	// user lookups — the response contains sensitive fields.
	GetUserForAuth(ctx context.Context, email string) (*userModels.User, error)
	CreateUserFromOAuth(ctx context.Context, input *userModels.CreateUserInput) (*userModels.User, error)
	// CreateUserWithPassword creates a new user with a pre-hashed password.
	CreateUserWithPassword(ctx context.Context, input *userModels.CreateUserInput) (*userModels.User, error)
	// UpdatePasswordHash stores a new argon2id hash and timestamps the change.
	UpdatePasswordHash(ctx context.Context, userUUID, hash string) error
	// MarkEmailVerified flips emailVerified to true.
	MarkEmailVerified(ctx context.Context, userUUID string) error
	// RecordFailedLogin increments failed counter and optionally sets a lockout.
	RecordFailedLogin(ctx context.Context, userUUID string, lockUntil *time.Time) error
	// ClearFailedLogins resets the counter after a successful login.
	ClearFailedLogins(ctx context.Context, userUUID string) error
	UpdateUser(ctx context.Context, id string, input *userModels.UpdateUserInput) (*userModels.UserManagementResponse, error)
	UpdateUserLastLogin(ctx context.Context, id string) error
	DeleteUser(ctx context.Context, id string) error
	GetUserOAuthLinks(ctx context.Context, userUUID string) ([]userModels.OAuthLink, error)
	RemoveOAuthLinkFromUser(ctx context.Context, userUUID string, provider userModels.OAuthProvider, providerID string) error
	SetPrimaryOAuthLink(ctx context.Context, userUUID string, provider userModels.OAuthProvider, providerID string) error
	GetUserCount(ctx context.Context, filters *userModels.UserFilters) (int64, error)

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
// JWTProvider — consumed by: dev
// Only the method dev actually needs for token generation.
// ---------------------------------------------------------------------------

type JWTProvider interface {
	GenerateAccessToken(user *userModels.User) (string, error)
}

// ---------------------------------------------------------------------------
// PDFProvider — consumed by: billing
// Only the methods billing's invoice service calls.
// ---------------------------------------------------------------------------

type PDFProvider interface {
	GenerateInvoicePDF(ctx context.Context, invoiceData map[string]interface{}, templateUUID string, generatedBy string) (*docModels.GeneratedDocument, error)
	GetDocumentContent(ctx context.Context, uuid string) ([]byte, string, error)
}

// ---------------------------------------------------------------------------
// GraphProvider — consumed by: rag
// The three execution methods rag uses for Cypher queries.
// ---------------------------------------------------------------------------

type GraphProvider interface {
	ExecuteRead(ctx context.Context, database string, cypher string, params map[string]interface{}) (*graphModels.QueryResult, error)
	ExecuteWrite(ctx context.Context, database string, cypher string, params map[string]interface{}) (*graphModels.QueryResult, error)
	ExecuteAutoCommit(ctx context.Context, database string, cypher string, params map[string]interface{}) error
}

// ---------------------------------------------------------------------------
// AIModelProvider — consumed by: rag, sales
// Union of the methods both modules need for embedding + LLM access.
// ---------------------------------------------------------------------------

type AIModelProvider interface {
	GetDefaultEmbeddingProvider(ctx context.Context) (aiProviders.EmbeddingProvider, error)
	GetDefaultLLMProvider(ctx context.Context) (aiProviders.LLMProvider, error)
	GetLLMProvider(ctx context.Context, uuid string) (aiProviders.LLMProvider, error)
	GetEmbeddingProvider(ctx context.Context, uuid string) (aiProviders.EmbeddingProvider, error)
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
	Query(ctx context.Context, question string, topK int, minScore float64, isoStandard, llmOverrideUUID, requirementLevel, nodeType, retrievalMode string, documentUUIDs []string) (*ragModels.RAGQueryResponse, error)
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
	// HasCapability reports whether the tenant currently holds an active
	// entitlement to the capability ID. Backed by the tenant_entitlements
	// projection (Phase 2). Returns false if the tenant has no active
	// entitlement, regardless of whether the capability ID is known in the
	// catalog — the catalog is advisory, the projection is authoritative.
	HasCapability(ctx context.Context, tenantUUID, capabilityID string) (bool, error)
	// GrantCapability creates an active entitlement for (tenantUUID, capID).
	// If an active entitlement already exists for that pair, it is revoked
	// first so the one-active-per-(tenant,capability) invariant holds. Safe
	// to replay — the same input produces the same effective state, with a
	// fresh audit row.
	GrantCapability(ctx context.Context, in GrantCapabilityInput) error
	// RevokeCapability marks the active entitlement for (tenantUUID, capID)
	// as revoked. Idempotent: a no-op if no active row exists.
	RevokeCapability(ctx context.Context, tenantUUID, capabilityID string) error
	// ListCapabilityIDs returns the capability IDs the tenant currently
	// holds active entitlements for, deduplicated and in deterministic
	// order. Thin projection of ListEntitlements for consumers (e.g. the
	// Cedar engine's principal builder) that only need the IDs.
	ListCapabilityIDs(ctx context.Context, tenantUUID string) ([]string, error)
	// ProvisionExternalTenant is the onboarding entry point for anonymous
	// self-service signup. Creates a Tier-2 tenant owned by the given user
	// in the provisioning lifecycle state (kind=external, signup=self_serve).
	// A follow-up activate-on-verify hook flips the status to active after
	// the owner has completed email verification. Distinct from the
	// admin-facing CreateTenant surface — different defaults, narrower
	// input shape.
	ProvisionExternalTenant(ctx context.Context, ownerUserUUID string, in OnboardingTenantInput) (*Tenant, error)
	// ActivateTenant transitions a tenant from `provisioning` to `active`.
	// Idempotent: calling it on an already-active tenant is a no-op on the
	// status field. The onboarding activate-on-verify hook uses this after
	// the owner has completed email verification. Does not validate the
	// previous state — admin callers can also use it to unsuspend, though
	// that is not the primary use case and richer transitions live on the
	// concrete tenant service.
	ActivateTenant(ctx context.Context, tenantUUID string) error
	// SetTenantStripeCustomerID persists the Stripe customer identifier the
	// payment provider returned for the tenant. Called on the first charge
	// for an external tenant so subsequent renewal cycles skip customer
	// creation. Idempotent: re-applying the same value is a no-op.
	SetTenantStripeCustomerID(ctx context.Context, tenantUUID, stripeCustomerID string) error
}

// OnboardingTenantInput is the cross-module payload for self-service
// external-tenant creation. Kept minimal: anonymous signups only carry
// the bare identifiers; richer fields (legal name, VAT, address) are
// collected later in the onboarding wizard.
type OnboardingTenantInput struct {
	Name string
	// Slug is optional — when empty, the tenant service derives it from
	// Name via the same slugifier the authenticated create path uses.
	Slug string
	// Plan is optional — defaults to "free" when empty. Entitlements are
	// driven by subscriptions, not plan name, so this is a label only.
	Plan string
}

// GrantCapabilityInput is the cross-module payload for granting a capability
// entitlement. Source/SourceRef tell the projection where the grant came
// from (subscription row, admin action, trial program) so it can be replayed
// or revoked by origin.
type GrantCapabilityInput struct {
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
	// TenantUUID is the external tenant the customer is being created for.
	// Echoed into Stripe customer metadata so cross-system reconciliation
	// can locate the owning tenant from the gateway side.
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
	Provider     string
	ID           string // e.g. pm_xxx
	Brand        string // e.g. "visa"
	Last4        string
	ExpiryMonth  int
	ExpiryYear   int
}

type SubscriptionCharge struct {
	SubscriptionUUID string
	InvoiceUUID      string // used as idempotency key (provider metadata)
	Customer         CustomerRef
	PaymentMethod    *PaymentMethodRef // nil = use default on customer
	AmountCents      int64
	Currency         string
	Description      string
	Metadata         map[string]string
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
// TenantSubscriptionProvider — consumed by: core/tenant admin aggregator
//
// Exposes a read-only view of a tenant's subscriptions so the Phase 2
// endpoint GET /v1/admin/tenants/{id}/subscriptions can return data without
// importing the subscriptions addon from the core package. The returned
// shape is intentionally flat (no Service/Tier joins) — the aggregator
// trusts the caller to fetch richer details through the subscriptions API.
// ---------------------------------------------------------------------------

type TenantSubscription struct {
	UUID               string
	TenantUUID         string
	ServiceUUID        string
	TierCode           string
	Status             string
	CurrentPeriodStart time.Time
	CurrentPeriodEnd   time.Time
	NextBillingAt      time.Time
	CreatedAt          time.Time
}

type TenantSubscriptionProvider interface {
	// ListByTenant returns every subscription bound to the tenant via
	// Subscription.TenantUUID. Sorted by createdAt desc.
	ListByTenant(ctx context.Context, tenantUUID string) ([]TenantSubscription, error)
}

// ---------------------------------------------------------------------------
// TenantPaymentProvider — consumed by: core/tenant admin aggregator
//
// Parallels TenantSubscriptionProvider for payments. Flattened to the
// subset the aggregator endpoint needs.
// ---------------------------------------------------------------------------

type TenantPayment struct {
	UUID             string
	TenantUUID       string
	SubscriptionUUID string
	InvoiceUUID      string
	Provider         string
	ProviderTxID     string
	Status           string
	AmountCents      int64
	Currency         string
	RefundedCents    int64
	ChargedAt        *time.Time
	RefundedAt       *time.Time
	CreatedAt        time.Time
}

type TenantPaymentProvider interface {
	// ListByTenant returns every transaction bound to the tenant via
	// Transaction.TenantUUID. Sorted by createdAt desc.
	ListByTenant(ctx context.Context, tenantUUID string) ([]TenantPayment, error)
}

// ---------------------------------------------------------------------------
// TenantBillingCustomerProvider — consumed by: core/tenant admin aggregator
//
// Mirrors the pattern of TenantSubscriptionProvider / TenantPaymentProvider
// but for the billing module's FatturaPA Customer registry. ADR-0001 PR-4
// added an optional Customer.TenantUUID FK so external Tier-2 tenants can
// be paired with their FatturaPA invoice profile.
//
// GetByTenant returns (nil, nil) when no customer is linked to the tenant
// — the aggregator handler renders that as 404. PromoteTenant is the
// idempotent create-from-tenant entry point: returns the existing customer
// when one is already linked, otherwise creates a new Customer pre-filled
// from the tenant's iface.Tenant fields (LegalName, VATNumber, FiscalCode,
// Country, Email).
// ---------------------------------------------------------------------------

// TenantBillingCustomer is the DTO core/tenant exposes for the
// billing-customer aggregator endpoint. Flat shape — full editing happens
// through the billing module's own /v1/billing/customers/{id} routes.
type TenantBillingCustomer struct {
	UUID         string
	TenantUUID   string
	Denomination string
	Name         string
	Surname      string
	FiscalIDCode string
	IsCompany    bool
	Country      string
	IsActive     bool
	CreatedAt    time.Time
}

// TenantBillingCustomerProvider is consumed by core/tenant's
// /v1/admin/tenants/{id}/billing-customer aggregator. The billing addon
// registers an adapter that talks to its own repository.
type TenantBillingCustomerProvider interface {
	// GetByTenant returns (nil, nil) when no customer is linked.
	GetByTenant(ctx context.Context, tenantUUID string) (*TenantBillingCustomer, error)
	// PromoteTenant creates (or returns) the customer linked to this tenant.
	// Idempotent: a tenant with an existing link is returned unchanged.
	// Errors from the underlying service (tenant not external, missing
	// denomination, etc.) propagate and are mapped to HTTP status by the
	// handler — see core/tenant/handlers/handler.go::promoteTenantToBillingCustomer.
	PromoteTenant(ctx context.Context, tenantUUID string) (*TenantBillingCustomer, error)
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

	// PurgePersonalData deletes or anonymizes every record the producer
	// holds for userUUID. GDPR right-to-erasure semantics apply: a row
	// that merely sets a deleted-at flag is not erased. Returns a
	// summary so the DSR audit log can record what was wiped without
	// re-reading the now-deleted rows.
	PurgePersonalData(ctx context.Context, userUUID string) (PurgeResult, error)
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
	// tenant level: if a key already exists for tenantUUID, returns
	// the existing keyID rather than overwriting — avoids wiping data
	// when a create path runs twice.
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
