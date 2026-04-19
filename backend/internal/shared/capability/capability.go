// Package capability defines the first-class Capability type and the boot-time
// Registry that collects capabilities declared by modules.
//
// A Capability is a stable, globally-unique identifier for a unit of
// functionality the platform sells or grants to tenants (e.g. "rag.query",
// "agents.run", "documents.pdf.generate"). Tenants hold Entitlements to
// Capability IDs; middleware and policies gate access on whether the current
// tenant is entitled to the Capability a route exposes.
//
// Capabilities are declared by modules via Module.Capabilities() and collected
// at boot by the registry. This package intentionally has zero imports from
// the rest of the project so it can be imported by shared/module without a
// cycle.
package capability

import (
	"fmt"
	"sort"
	"sync"
)

// Capability is the canonical description of a unit of functionality a
// tenant may be entitled to. IDs are the vocabulary of the entitlement
// projection — once a Capability ID ships, it must not be renamed without a
// migration of the tenant_entitlements collection.
type Capability struct {
	// ID is the globally-unique stable identifier, dotted form:
	// "<module>.<action>[.<qualifier>]". Example: "rag.query".
	ID string
	// Module is the name of the module that owns this capability. Used for
	// grouping in the admin UI and for sanity-checking that the ID starts
	// with the module name.
	Module string
	// Action is a short verb/noun describing what the capability allows
	// (e.g. "query", "generate", "send"). Optional — admin-UI only.
	Action string
	// Title is a human-readable label for the admin UI.
	Title string
	// Description is a longer marketing/docs blurb surfaced in the catalog.
	Description string
	// Published is true when the capability should appear in the external
	// catalog (tenants can see it as something they can subscribe to).
	// Internal-only capabilities (e.g. platform admin helpers) set this to
	// false so they're evaluated at runtime but hidden from the public
	// catalog.
	Published bool
}

// Validate returns an error if required fields are missing or malformed.
// Called by Registry.Register before the catalog accepts an entry so bad
// declarations fail loudly at boot rather than silently drifting.
func (c Capability) Validate() error {
	if c.ID == "" {
		return fmt.Errorf("capability: ID is required")
	}
	if c.Module == "" {
		return fmt.Errorf("capability %q: Module is required", c.ID)
	}
	return nil
}

// Registry is the canonical in-memory catalog of all capabilities declared
// across the running modules. Populated once at boot and thereafter read-only
// (safe for concurrent readers). The module registry pushes entries in during
// InitAll; everyone else reads.
type Registry struct {
	mu   sync.RWMutex
	byID map[string]Capability
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{byID: make(map[string]Capability)}
}

// Register adds capabilities to the registry. Idempotent for identical
// entries (same ID + same body) so a module can safely declare the same
// capability twice (e.g. during hot-reload). Returns an error if two
// different bodies collide on the same ID — that's a wiring bug.
func (r *Registry) Register(caps ...Capability) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, c := range caps {
		if err := c.Validate(); err != nil {
			return err
		}
		existing, ok := r.byID[c.ID]
		if ok && existing != c {
			return fmt.Errorf("capability %q: duplicate declaration with different body (existing=%+v new=%+v)", c.ID, existing, c)
		}
		r.byID[c.ID] = c
	}
	return nil
}

// Get returns the capability for an ID and whether it's known.
func (r *Registry) Get(id string) (Capability, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.byID[id]
	return c, ok
}

// List returns all capabilities in deterministic ID order.
func (r *Registry) List() []Capability {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Capability, 0, len(r.byID))
	for _, c := range r.byID {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Count returns how many capabilities are registered. Useful for boot-time
// diagnostic logs.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.byID)
}
