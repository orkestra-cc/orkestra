package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// EntitlementSource labels the origin of a capability entitlement. The
// projection carries it so we can tell grants apart from subscription-driven
// entitlements when auditing or revoking.
type EntitlementSource string

const (
	// EntitlementSourceSubscription is an entitlement materialized from a
	// subscription row (payments/subscriptions modules write these).
	EntitlementSourceSubscription EntitlementSource = "subscription"
	// EntitlementSourceGrant is an admin-issued grant — typically used to
	// bundle capabilities with internal tenants or as a goodwill trial
	// outside the Stripe billing flow.
	EntitlementSourceGrant EntitlementSource = "grant"
	// EntitlementSourceTrial is a time-bound trial attached to a tenant on
	// signup. ExpiresAt is always set.
	EntitlementSourceTrial EntitlementSource = "trial"
)

// Valid returns true if the source is a known value.
func (s EntitlementSource) Valid() bool {
	switch s {
	case EntitlementSourceSubscription, EntitlementSourceGrant, EntitlementSourceTrial:
		return true
	}
	return false
}

// Entitlement is a projection row: a single record that a given tenant is
// entitled to a specific capability, coming from a named source. A tenant may
// historically hold many rows for the same capability (superseded grants,
// previous subscription cycles), but at most one active row at any given
// time — enforced in the service layer (Grant revokes any existing active
// row before inserting the replacement).
//
// The row is intentionally flat (no embedded subscription detail) so the
// projection stays simple to rebuild from an event log. Richer context lives
// on the SourceRef pointer.
type Entitlement struct {
	ID           primitive.ObjectID `bson:"_id,omitempty" json:"-"`
	UUID         string             `bson:"uuid" json:"uuid"`
	TenantUUID   string             `bson:"tenantId" json:"tenantId"`
	CapabilityID string             `bson:"capabilityId" json:"capabilityId"`
	Source       EntitlementSource  `bson:"source" json:"source"`
	// SourceRef is the opaque identifier of the thing that created this
	// entitlement — e.g. a subscription UUID, a Stripe subscription ID, or
	// an admin user UUID on a manual grant.
	SourceRef string `bson:"sourceRef,omitempty"`
	// GrantedBy is a user UUID or "system" for automated grants.
	GrantedBy string     `bson:"grantedBy,omitempty"`
	GrantedAt time.Time  `bson:"grantedAt"`
	ExpiresAt *time.Time `bson:"expiresAt,omitempty"`
	RevokedAt *time.Time `bson:"revokedAt,omitempty"`
	// Metadata is a free-form blob for provider-specific breadcrumbs
	// (e.g. Stripe invoice ID, plan tier code). Opaque to the projection.
	Metadata map[string]any `bson:"metadata,omitempty"`
}

// IsActive reports whether the entitlement is currently effective — not
// revoked and not expired. Callers must decide the "now" reference so tests
// can pin time deterministically.
func (e Entitlement) IsActive(now time.Time) bool {
	if e.RevokedAt != nil && !e.RevokedAt.After(now) {
		return false
	}
	if e.ExpiresAt != nil && !e.ExpiresAt.After(now) {
		return false
	}
	return true
}
