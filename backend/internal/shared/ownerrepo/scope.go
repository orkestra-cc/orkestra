// Package ownerrepo provides scoping helpers for collections keyed by a
// tenant UUID — siblings of tenantrepo for the addon collections that
// previously used a polymorphic owner. After the Unified Client Aggregate
// refactor (Phase 4), every billable principal is a Tenant aggregate, so
// the owner key is just `tenantUUID` and these helpers wrap that single
// field-name constant for consistency across subscriptions, transactions,
// payment methods, and capability entitlements.
//
// Usage:
//
//	filter := ownerrepo.Scope(tenantUUID, bson.M{"status": "succeeded"})
//	cur, err := coll.Find(ctx, filter)
//
// The helper rejects an empty tenantUUID via panic in dev (so missing-scope
// bugs surface during development) and a bson filter that will match nothing
// in production (the safest fallback when callers somehow reach the
// repository without a tenant).
package ownerrepo

import (
	"os"

	"go.mongodb.org/mongo-driver/bson"
)

// TenantUUIDField is the bson field every owner-scoped document carries.
// Centralized so renames flow through one constant rather than hopping
// through every repository.
const TenantUUIDField = "tenantUUID"

// Scope returns the filter with tenantUUID added. Panics in dev when the
// tenantUUID is empty so missing-scope bugs surface loudly during
// development; in production, returns a filter that cannot match any
// document (defense in depth — better an empty result set than a silent
// cross-tenant read).
func Scope(tenantUUID string, filter bson.M) bson.M {
	if tenantUUID == "" {
		if isDev() {
			panic("ownerrepo.Scope: tenantUUID is empty — caller forgot to scope this query")
		}
		// Production fallback: a filter that cannot match any document.
		return bson.M{"_id": bson.M{"$exists": false}, "_": bson.M{"$exists": false}}
	}
	if filter == nil {
		filter = bson.M{}
	}
	filter[TenantUUIDField] = tenantUUID
	return filter
}

// MustScope is Scope for call sites statically known to have a non-empty
// tenantUUID. Panics on an empty tenantUUID unconditionally.
func MustScope(tenantUUID string, filter bson.M) bson.M {
	if tenantUUID == "" {
		panic("ownerrepo.MustScope: tenantUUID is empty")
	}
	return Scope(tenantUUID, filter)
}

// StampInsertM mutates and returns doc with tenantUUID stamped. Mirrors
// tenantrepo.StampInsertM for bson-map inserts.
func StampInsertM(tenantUUID string, doc bson.M) bson.M {
	if tenantUUID == "" {
		if isDev() {
			panic("ownerrepo.StampInsertM: tenantUUID is empty")
		}
		return doc
	}
	if doc == nil {
		doc = bson.M{}
	}
	doc[TenantUUIDField] = tenantUUID
	return doc
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
