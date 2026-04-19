package module

import (
	"fmt"
	"sync"
)

// ServiceKey identifies a cross-module service in the registry.
type ServiceKey string

// Well-known service keys for cross-module communication.
const (
	ServiceNavItems        ServiceKey = "system.nav_items"   // []NavItemSpec from registry
	ServiceConfigService   ServiceKey = "system.config_svc"  // *ModuleConfigService

	ServiceUserService     ServiceKey = "user.service"
	ServiceAuthService     ServiceKey = "auth.service"
	ServiceJWTService      ServiceKey = "auth.jwt"
	ServiceOAuthProviderFactory ServiceKey = "auth.oauth_factory"
	ServiceOAuthStateService    ServiceKey = "auth.oauth_state"
	ServiceOAuthProviderRepo    ServiceKey = "auth.oauth_provider_repo"
	ServiceAIModelProvider ServiceKey = "aimodels.provider"
	ServicePDFService      ServiceKey = "documents.pdf"
	ServiceGraphRepo       ServiceKey = "graph.repository"
	ServiceRAGQuery        ServiceKey = "rag.query"

	ServiceNotificationSender  ServiceKey = "notification.sender"
	ServicePasswordService     ServiceKey = "auth.password"
	ServicePasswordAuthService ServiceKey = "auth.password_auth"

	ServiceTenantProvider ServiceKey = "tenant.provider"
	// ServiceTenantService is the concrete *tenant/services.Service registered
	// alongside the TenantProvider interface. Compliance consumes it to wire
	// its audit sink via SetAuditSink — the public provider interface stays
	// slim, the concrete service carries post-init setters.
	ServiceTenantService  ServiceKey = "tenant.service"
	ServiceAuthzProvider  ServiceKey = "authz.provider"

	// ServiceCapabilityRegistry is the boot-time catalog of Capability
	// declarations collected from every module's Capabilities() method.
	// Value: *capability.Registry. Consumers use it to enumerate or resolve
	// capability IDs when building the admin catalog UI or validating grants.
	ServiceCapabilityRegistry ServiceKey = "system.capability_registry"

	// ServiceFirstAdminClaimer gates the platform's one-time super_admin seat.
	// Registered by main.go wiring; consumed by password auth + setup wizard
	// to race-proof their first-user heuristic. Value: *systeminit.Repo
	// (structurally an auth/services.FirstAdminClaimer).
	ServiceFirstAdminClaimer ServiceKey = "system.first_admin_claimer"

	ServicePaymentProvider        ServiceKey = "payments.provider"
	ServiceSubscriptionReconciler ServiceKey = "subscriptions.reconciler"
	ServiceClientOwnership        ServiceKey = "subscriptions.client_ownership"

	// ServiceAuditSink is the platform-wide append-only audit trail sink
	// provided by the compliance module. Consumers resolve it via
	// module.GetTyped[iface.AuditSink] and call Emit on hot paths — the
	// sink is fire-and-forget, so a missing implementation is not fatal.
	ServiceAuditSink ServiceKey = "compliance.audit_sink"

	// Identity concretes published alongside the module's handlers so the
	// compliance module can drive their post-init SetAuditSink calls.
	// Split by emit locus: OIDC callbacks emit from the service, IdP CRUD
	// emits from the admin handler, SCIM rotations from the scim admin
	// handler. Compliance looks each up by key — missing keys are
	// tolerated so the identity module can be disabled independently.
	ServiceIdentityOIDCService       ServiceKey = "identity.oidc_service"
	ServiceIdentityAdminHandler      ServiceKey = "identity.admin_handler"
	ServiceIdentityScimAdminHandler  ServiceKey = "identity.scim_admin_handler"

	// ServiceSubscriptionService is the concrete *subscriptions/services.
	// SubscriptionService — compliance wires the audit sink into it so
	// subscription lifecycle events (created, cancelled, reactivated,
	// self-subscribed) land on the audit trail alongside the module's
	// own subscriptions_activity log.
	ServiceSubscriptionService ServiceKey = "subscriptions.service"
)

// ServiceRegistry is a typed key-value store for cross-module service sharing.
// Producer modules register their services during Init(); consumer modules
// retrieve them during their own Init() with a nil-safe pattern.
type ServiceRegistry struct {
	mu          sync.RWMutex
	services    map[ServiceKey]any
	replaceMode bool // when true, Register silently replaces existing keys (used by RetryInit)
}

// NewServiceRegistry creates an empty service registry.
func NewServiceRegistry() *ServiceRegistry {
	return &ServiceRegistry{
		services: make(map[ServiceKey]any),
	}
}

// SetReplaceMode toggles whether Register silently replaces existing keys.
// Used during RetryInit so modules can re-register their services without panicking.
func (r *ServiceRegistry) SetReplaceMode(on bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.replaceMode = on
}

// Register stores a service under the given key.
// Panics if the key is already registered (catches wiring bugs at startup),
// unless replace mode is enabled (used during module retry-init).
func (r *ServiceRegistry) Register(key ServiceKey, svc any) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.services[key]; exists {
		if r.replaceMode {
			r.services[key] = svc
			return
		}
		panic(fmt.Sprintf("module: service %q already registered", key))
	}
	r.services[key] = svc
}

// Get retrieves a service by key. Returns nil if not registered.
// Consumer modules should handle nil gracefully (feature degradation).
func (r *ServiceRegistry) Get(key ServiceKey) any {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.services[key]
}

// MustGet retrieves a service by key. Panics if not found.
// Use only for hard dependencies that cannot be degraded.
func (r *ServiceRegistry) MustGet(key ServiceKey) any {
	svc := r.Get(key)
	if svc == nil {
		panic(fmt.Sprintf("module: required service %q not registered", key))
	}
	return svc
}

// GetTyped retrieves a service and asserts it to type T.
// Returns the zero value and false if the service is not registered or has the wrong type.
func GetTyped[T any](r *ServiceRegistry, key ServiceKey) (T, bool) {
	svc := r.Get(key)
	if svc == nil {
		var zero T
		return zero, false
	}
	typed, ok := svc.(T)
	return typed, ok
}

// MustGetTyped retrieves a service and asserts it to type T.
// Panics if the service is not registered or has the wrong type.
func MustGetTyped[T any](r *ServiceRegistry, key ServiceKey) T {
	svc := r.MustGet(key)
	typed, ok := svc.(T)
	if !ok {
		panic(fmt.Sprintf("module: service %q has wrong type: got %T", key, svc))
	}
	return typed
}
