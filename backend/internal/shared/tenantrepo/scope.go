// Package tenantrepo provides the fail-closed orgId scoping helpers every
// data module must use for every MongoDB query. Its single purpose is to
// make cross-tenant data leaks impossible: any repository code path that
// forgets to scope by orgId either panics in dev or returns a 403 error
// in production, rather than silently reading data from other tenants.
//
// Usage:
//
//	filter, err := tenantrepo.Scope(ctx, bson.M{"status": "sent"})
//	if err != nil { return nil, err }
//	cur, err := coll.Find(ctx, filter)
//
// The helper extracts the current orgID from the request context (set by
// the auth middleware from the X-Org-ID header) and adds it to the filter.
// Routes that are intentionally global (auth flows, org listing, user
// self-service) must not use scoped repositories.
package tenantrepo

import (
	"context"
	"os"

	"github.com/orkestra/backend/internal/shared/errors"
	"github.com/orkestra/backend/internal/shared/middleware"
	"go.mongodb.org/mongo-driver/bson"
)

// OrgIDField is the canonical BSON field name used to store the owning org
// on every tenant-scoped document.
const OrgIDField = "orgId"

// Scope returns the input filter with orgId added from the request context.
// If the context has no resolved org, Scope returns an authorization error
// and additionally panics in dev mode so missing-scope bugs surface loudly
// during development instead of silently exposing data in production.
func Scope(ctx context.Context, filter bson.M) (bson.M, error) {
	orgID, ok := middleware.GetOrgID(ctx)
	if !ok || orgID == "" {
		if isDev() {
			panic("tenantrepo.Scope: context has no orgID — caller forgot to scope this query")
		}
		return nil, errors.AuthorizationError("tenant scope missing").
			WithOperation("tenantrepo.Scope").
			Build()
	}
	if filter == nil {
		filter = bson.M{}
	}
	filter[OrgIDField] = orgID
	return filter, nil
}

// MustScope is Scope for call sites that are statically known to run inside
// an authenticated, org-scoped request handler. Panics on missing context.
func MustScope(ctx context.Context, filter bson.M) bson.M {
	f, err := Scope(ctx, filter)
	if err != nil {
		panic("tenantrepo.MustScope: " + err.Error())
	}
	return f
}

// StampInsert is used before inserting a new document. It ensures the doc
// carries the orgId from the current request context. Accepts either a
// bson.M or any struct; for structs, the caller is responsible for setting
// the orgId field themselves — StampInsert returns the orgID so they can.
func StampInsert(ctx context.Context) (string, error) {
	orgID, ok := middleware.GetOrgID(ctx)
	if !ok || orgID == "" {
		if isDev() {
			panic("tenantrepo.StampInsert: context has no orgID")
		}
		return "", errors.AuthorizationError("tenant scope missing").
			WithOperation("tenantrepo.StampInsert").Build()
	}
	return orgID, nil
}

// StampInsertM is StampInsert for BSON-map inserts. It mutates and returns
// the doc with orgId set.
func StampInsertM(ctx context.Context, doc bson.M) (bson.M, error) {
	orgID, err := StampInsert(ctx)
	if err != nil {
		return nil, err
	}
	if doc == nil {
		doc = bson.M{}
	}
	doc[OrgIDField] = orgID
	return doc, nil
}

// ScopeAggregate prepends a $match stage filtering by orgId to an aggregation
// pipeline. Use for every tenant-scoped aggregation.
func ScopeAggregate(ctx context.Context, pipeline []bson.M) ([]bson.M, error) {
	orgID, ok := middleware.GetOrgID(ctx)
	if !ok || orgID == "" {
		if isDev() {
			panic("tenantrepo.ScopeAggregate: context has no orgID")
		}
		return nil, errors.AuthorizationError("tenant scope missing").
			WithOperation("tenantrepo.ScopeAggregate").Build()
	}
	match := bson.M{"$match": bson.M{OrgIDField: orgID}}
	out := make([]bson.M, 0, len(pipeline)+1)
	out = append(out, match)
	out = append(out, pipeline...)
	return out, nil
}

// CurrentOrgID is a thin wrapper over middleware.GetOrgID for callers that
// need the orgID for reasons other than filtering (e.g. logging, audit).
func CurrentOrgID(ctx context.Context) (string, bool) {
	return middleware.GetOrgID(ctx)
}

// CurrentTenantKind returns the tier ("internal" | "external") of the
// tenant the current request is acting in, or empty when no tier is known
// (global/platform routes, or pre-ADR-0001 tokens). Consumers that need to
// dispatch on tier — e.g. FatturaPA emission must be internal-only — read
// this alongside CurrentOrgID.
func CurrentTenantKind(ctx context.Context) string {
	return middleware.TenantKindFromContext(ctx)
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
		return errors.AuthorizationError("tenant kind missing").
			WithOperation("tenantrepo.RequireInternalTenant").Build()
	}
	if kind != "internal" {
		return errors.AuthorizationError("this operation is restricted to internal tenants").
			WithOperation("tenantrepo.RequireInternalTenant").
			WithDetail("tenant_kind", kind).
			Build()
	}
	return nil
}

// RequireExternalTenant is the mirror of RequireInternalTenant. Use at the
// top of repository methods that only make sense for external clients — e.g.
// subscription self-service actions.
func RequireExternalTenant(ctx context.Context) error {
	kind := CurrentTenantKind(ctx)
	if kind == "" {
		if isDev() {
			panic("tenantrepo.RequireExternalTenant: tenant kind missing — route not gated by RequireAuth?")
		}
		return errors.AuthorizationError("tenant kind missing").
			WithOperation("tenantrepo.RequireExternalTenant").Build()
	}
	if kind != "external" {
		return errors.AuthorizationError("this operation is restricted to external tenants").
			WithOperation("tenantrepo.RequireExternalTenant").
			WithDetail("tenant_kind", kind).
			Build()
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
