package module

import (
	"fmt"
	"sync"
)

// ServiceKey identifies a cross-module service in the registry.
type ServiceKey string

// Well-known service keys for cross-module communication.
const (
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
)

// ServiceRegistry is a typed key-value store for cross-module service sharing.
// Producer modules register their services during Init(); consumer modules
// retrieve them during their own Init() with a nil-safe pattern.
type ServiceRegistry struct {
	mu       sync.RWMutex
	services map[ServiceKey]any
}

// NewServiceRegistry creates an empty service registry.
func NewServiceRegistry() *ServiceRegistry {
	return &ServiceRegistry{
		services: make(map[ServiceKey]any),
	}
}

// Register stores a service under the given key.
// Panics if the key is already registered (catches wiring bugs at startup).
func (r *ServiceRegistry) Register(key ServiceKey, svc any) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.services[key]; exists {
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
