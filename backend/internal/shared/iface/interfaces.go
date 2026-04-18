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

type Org struct {
	UUID     string
	Name     string
	Slug     string
	Plan     string
	Features []string
}

type Membership struct {
	OrgUUID string
	OrgName string
	OrgSlug string
	Roles   []string // authz role names the user holds in this org
	IsOwner bool
}

type TenantProvider interface {
	GetOrg(ctx context.Context, orgUUID string) (*Org, error)
	ListUserMemberships(ctx context.Context, userUUID string) ([]Membership, error)
	IsMember(ctx context.Context, userUUID, orgUUID string) (bool, error)
	HasEntitlement(ctx context.Context, orgUUID, feature string) (bool, error)
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
	ClientUUID string
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
// ClientOwnershipProvider — consumed by: payments
//
// Subscriptions owns the `subscriptions_clients` collection. Payments stores
// a bare ClientUUID on each Transaction and needs to resolve the owning org
// to gate by-id reads/mutations. This interface keeps payments free of any
// direct import of the subscriptions repository package.
// ---------------------------------------------------------------------------

type ClientOwnershipProvider interface {
	// GetClientOrgUUID returns the org UUID the client is bound to, or an
	// empty string if the client is not scoped to an org. Returns an error
	// when the client does not exist.
	GetClientOrgUUID(ctx context.Context, clientUUID string) (string, error)
}
