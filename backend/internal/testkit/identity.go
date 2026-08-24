// Package testkit holds shared helpers for Go tests that need to simulate
// authenticated, tenant-scoped requests without spinning up the full auth
// middleware chain.
//
// Why a dedicated package: Phase 4's cross-tenant integration tests will
// exercise handlers and repositories under many different (user, system role,
// tenant, tenant role) combinations. Every test package would otherwise
// reimplement the same 20 lines of context-key plumbing, and drift between
// those copies would hide real bugs.
//
// Keys used here must stay in sync with the package-private constants in
// backend/internal/shared/middleware/auth.go. The TestContextKeysRoundTrip
// test in this package runs the injected context through middleware's public
// accessors (GetUserUUID, GetTenantID, ...) and fails loudly if a rename
// silently breaks the contract.
package testkit

import (
	"context"

	"github.com/orkestra/backend/internal/core/auth/models"
)

// Context keys — mirror of the unexported constants in the middleware
// package. Changing one side requires changing the other; the round-trip
// test catches drift. Treat these as a tightly coupled pair.
const (
	ctxUserUUID          = "userUUID"
	ctxUserEmail         = "userEmail"
	ctxSystemRole        = "userRole"
	ctxClaims            = "claims"
	ctxTenantID          = "tenantID"
	ctxTenantMemberships = "tenantMemberships"
	ctxTenantRoles       = "tenantRoles"
)

// Identity is the compact description of a test principal. The zero value
// is meaningless; use NewIdentity to build one.
type Identity struct {
	UserUUID         string
	Email            string
	SystemRole       string
	Memberships      []models.TenantMembership
	TenantFallbackID string
}

// NewIdentity returns a platform-level (no-tenant) identity. Use WithTenant
// to add a tenant context. Chain multiple times for a user with multiple
// tenants.
func NewIdentity(userUUID, email, systemRole string) Identity {
	return Identity{
		UserUUID:   userUUID,
		Email:      email,
		SystemRole: systemRole,
	}
}

// WithTenant appends a tenant membership. If defaultIfUnset is true and the
// Identity has no TenantFallbackID, this call also sets it — the "first
// tenant wins" rule matches JWTService.embedMemberships on new accounts.
func (i Identity) WithTenant(tenantUUID string, roles []string, defaultIfUnset bool) Identity {
	i.Memberships = append(i.Memberships, models.TenantMembership{
		TenantUUID: tenantUUID,
		Roles:      append([]string(nil), roles...),
	})
	if defaultIfUnset && i.TenantFallbackID == "" {
		i.TenantFallbackID = tenantUUID
	}
	return i
}

// WithTenantFallback sets the tenant fallback explicitly, overriding the
// first-wins rule from WithTenant. The tenant must already be in
// Memberships; otherwise the returned Identity will fail tenant resolution
// when used through ContextFor.
func (i Identity) WithTenantFallback(tenantUUID string) Identity {
	i.TenantFallbackID = tenantUUID
	return i
}

// Claims builds a *models.JWTClaims snapshot equivalent to what JWTService
// would embed if it signed a token for this identity right now. Only the
// fields the middleware reads are populated; the timing / issuer fields
// are left zero because validators downstream of the testkit don't look at
// them.
func (i Identity) Claims() *models.JWTClaims {
	return &models.JWTClaims{
		UserUUID:         i.UserUUID,
		Email:            i.Email,
		SystemRole:       i.SystemRole,
		TokenType:        "access",
		Memberships:      i.Memberships,
		TenantFallbackID: i.TenantFallbackID,
	}
}

// ContextFor returns ctx with the identity injected exactly as the auth
// middleware would inject it after validating a real JWT.
//
// activeTenantID selects which tenant the context is currently scoped to —
// this mirrors the X-Tenant-ID header the client would send. Pass "" to let
// the tenant fallback (if any) take over; pass "-" to explicitly leave the
// context tenant-less (useful for testing RequireGlobal handlers).
//
// Panics if activeTenantID is set but does not match any membership,
// because that's exactly the condition middleware.resolveCurrentTenant
// rejects in production — a test that sets up such a context is configuring
// an invalid session and would produce misleading green bars if allowed to
// pass.
func (i Identity) ContextFor(ctx context.Context, activeTenantID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = context.WithValue(ctx, ctxUserUUID, i.UserUUID)
	ctx = context.WithValue(ctx, ctxUserEmail, i.Email)
	ctx = context.WithValue(ctx, ctxSystemRole, i.SystemRole)
	ctx = context.WithValue(ctx, ctxClaims, i.Claims())
	ctx = context.WithValue(ctx, ctxTenantMemberships, i.Memberships)

	// Resolve the active tenant.
	if activeTenantID == "-" {
		return ctx
	}
	resolved := activeTenantID
	if resolved == "" {
		resolved = i.TenantFallbackID
	}
	if resolved == "" {
		return ctx
	}
	for _, m := range i.Memberships {
		if m.TenantUUID == resolved {
			ctx = context.WithValue(ctx, ctxTenantID, resolved)
			ctx = context.WithValue(ctx, ctxTenantRoles, append([]string(nil), m.Roles...))
			return ctx
		}
	}
	panic("testkit: activeTenantID " + resolved + " is not in this identity's memberships — that's an invalid session the real middleware would reject")
}
