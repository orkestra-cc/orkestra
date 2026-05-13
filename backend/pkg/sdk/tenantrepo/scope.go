// Package tenantrepo provides the fail-closed tenant-scoping helpers every
// data module must use for every MongoDB query. Its single purpose is to
// make cross-tenant data leaks impossible: any repository code path that
// forgets to scope by tenantId either panics in dev or returns a 403 error
// in production, rather than silently reading data from other tenants.
//
// Usage:
//
//	filter, err := tenantrepo.Scope(ctx, bson.M{"status": "sent"})
//	if err != nil { return nil, err }
//	cur, err := coll.Find(ctx, filter)
//
// The helper extracts the current tenantID from the request context (set by
// the auth middleware from the X-Tenant-ID header) and adds it to the
// filter. Routes that are intentionally global (auth flows, tenant listing,
// user self-service) must not use scoped repositories.
//
// See docs/adr/0001-unified-tenant-model.md for the two-tier tenancy model.
package tenantrepo

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/orkestra-cc/orkestra-sdk/ctxauth"
	"go.mongodb.org/mongo-driver/bson"
)

// ErrTenantScopeMissing is returned when a tenant-scoped operation runs in
// a request that has no resolved tenant (typically because the route is
// missing its RequireAuth chain).
var ErrTenantScopeMissing = errors.New("tenant scope missing")

// ErrTenantKindMismatch wraps a tenant-tier check failure. Use errors.Is
// to test for it; the wrapped message names the expected tier and the
// actual tier.
var ErrTenantKindMismatch = errors.New("tenant kind mismatch")

// TenantIDField is the canonical BSON field name used to store the owning
// tenant on every tenant-scoped document.
const TenantIDField = "tenantId"

// Scope returns the input filter with tenantId added from the request
// context. If the context has no resolved tenant, Scope returns an
// authorization error and additionally panics in dev mode so missing-scope
// bugs surface loudly during development instead of silently exposing data
// in production.
func Scope(ctx context.Context, filter bson.M) (bson.M, error) {
	tenantID, ok := ctxauth.GetTenantID(ctx)
	if !ok || tenantID == "" {
		if isDev() {
			panic("tenantrepo.Scope: context has no tenantID — caller forgot to scope this query")
		}
		return nil, fmt.Errorf("%w (tenantrepo.Scope)", ErrTenantScopeMissing)
	}
	if filter == nil {
		filter = bson.M{}
	}
	filter[TenantIDField] = tenantID
	return filter, nil
}

// MustScope is Scope for call sites statically known to run inside an
// authenticated, tenant-scoped request handler. Panics on missing context.
func MustScope(ctx context.Context, filter bson.M) bson.M {
	f, err := Scope(ctx, filter)
	if err != nil {
		panic("tenantrepo.MustScope: " + err.Error())
	}
	return f
}

// StampInsert is used before inserting a new document. It ensures the doc
// carries the tenantId from the current request context. Accepts either a
// bson.M or any struct; for structs, the caller is responsible for setting
// the tenantId field themselves — StampInsert returns the tenantID so they
// can.
func StampInsert(ctx context.Context) (string, error) {
	tenantID, ok := ctxauth.GetTenantID(ctx)
	if !ok || tenantID == "" {
		if isDev() {
			panic("tenantrepo.StampInsert: context has no tenantID")
		}
		return "", fmt.Errorf("%w (tenantrepo.StampInsert)", ErrTenantScopeMissing)
	}
	return tenantID, nil
}

// StampInsertM is StampInsert for BSON-map inserts. It mutates and returns
// the doc with tenantId set.
func StampInsertM(ctx context.Context, doc bson.M) (bson.M, error) {
	tenantID, err := StampInsert(ctx)
	if err != nil {
		return nil, err
	}
	if doc == nil {
		doc = bson.M{}
	}
	doc[TenantIDField] = tenantID
	return doc, nil
}

// ScopeAggregate prepends a $match stage filtering by tenantId to an
// aggregation pipeline. Use for every tenant-scoped aggregation.
func ScopeAggregate(ctx context.Context, pipeline []bson.M) ([]bson.M, error) {
	tenantID, ok := ctxauth.GetTenantID(ctx)
	if !ok || tenantID == "" {
		if isDev() {
			panic("tenantrepo.ScopeAggregate: context has no tenantID")
		}
		return nil, fmt.Errorf("%w (tenantrepo.ScopeAggregate)", ErrTenantScopeMissing)
	}
	match := bson.M{"$match": bson.M{TenantIDField: tenantID}}
	out := make([]bson.M, 0, len(pipeline)+1)
	out = append(out, match)
	out = append(out, pipeline...)
	return out, nil
}

// CurrentTenantID is a thin wrapper over ctxauth.GetTenantID for callers
// that need the tenantID for reasons other than filtering (logging, audit).
func CurrentTenantID(ctx context.Context) (string, bool) {
	return ctxauth.GetTenantID(ctx)
}

// CurrentTenantKind returns the tier ("internal" | "external") of the
// tenant the current request is acting in, or empty when no tier is known
// (global/platform routes, or pre-ADR-0001 tokens). Consumers that need to
// dispatch on tier — e.g. FatturaPA emission must be internal-only — read
// this alongside CurrentTenantID.
func CurrentTenantKind(ctx context.Context) string {
	return ctxauth.TenantKindFromContext(ctx)
}

// RequireInternalTenant returns an error (and panics in dev) when the
// current request is not acting in an internal operator tenant. Use at the
// top of repository methods that must never run under an external client
// tenant — e.g. FatturaPA invoice issuance, module admin, platform settings.
//
// Tier is determined from the JWT claim at middleware time — this check is a
// defense-in-depth guard against a missing or misapplied middleware-level
// RequireInternalTenant/RequireTenantKind chain.
func RequireInternalTenant(ctx context.Context) error {
	kind := CurrentTenantKind(ctx)
	if kind == "" {
		if isDev() {
			panic("tenantrepo.RequireInternalTenant: tenant kind missing — route not gated by RequireAuth?")
		}
		return fmt.Errorf("%w: kind unset (tenantrepo.RequireInternalTenant)", ErrTenantScopeMissing)
	}
	if kind != "internal" {
		return fmt.Errorf("%w: want internal, got %q", ErrTenantKindMismatch, kind)
	}
	return nil
}

// RequireExternalTenant is the mirror of RequireInternalTenant. Use at the
// top of repository methods that only make sense for external clients.
func RequireExternalTenant(ctx context.Context) error {
	kind := CurrentTenantKind(ctx)
	if kind == "" {
		if isDev() {
			panic("tenantrepo.RequireExternalTenant: tenant kind missing — route not gated by RequireAuth?")
		}
		return fmt.Errorf("%w: kind unset (tenantrepo.RequireExternalTenant)", ErrTenantScopeMissing)
	}
	if kind != "external" {
		return fmt.Errorf("%w: want external, got %q", ErrTenantKindMismatch, kind)
	}
	return nil
}

func isDev() bool {
	env := os.Getenv("ENVIRONMENT")
	if env == "" {
		env = os.Getenv("ENV")
	}
	switch env {
	case "development", "dev", "local", "":
		return true
	}
	return false
}
