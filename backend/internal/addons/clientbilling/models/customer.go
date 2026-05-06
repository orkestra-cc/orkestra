// Package models defines the persisted shape of the user-level billing
// projection introduced in the post-onboarding refactor (Phase 2).
//
// The clientbilling addon owns one collection: clientbilling_customers.
// Every self-registered Tier-2 client has at most one row, keyed by
// userUUID — the row carries the natural-person / sole-proprietor billing
// identity used to drive Stripe customer creation and subscription
// renewals when the owner is `Kind="user"`.
package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// CustomersCollection is the single MongoDB collection owned by the
// clientbilling addon. The name follows the mongo-collection-naming
// convention (module-directory prefix) even though the rule is only
// strictly required for modules with 2+ collections.
const CustomersCollection = "clientbilling_customers"

// UserBillingCustomer is the persisted form of a user's billing profile.
// Mirrors iface.UserBillingProfile field-for-field so the cross-module
// adapter is a straight pass-through.
//
// Invariants:
//   - UserUUID is unique (enforced by index).
//   - Either LegalName (when IsCompany=true) or FirstName+LastName (when
//     IsCompany=false) is populated. The service layer validates this.
//   - StripeCustomerID is populated lazily on the first charge / setup
//     session, never required at row creation.
type UserBillingCustomer struct {
	ID               primitive.ObjectID `bson:"_id,omitempty" json:"-"`
	UserUUID         string             `bson:"userUUID" json:"userUUID"`
	LegalName        string             `bson:"legalName,omitempty" json:"legalName,omitempty"`
	FirstName        string             `bson:"firstName,omitempty" json:"firstName,omitempty"`
	LastName         string             `bson:"lastName,omitempty" json:"lastName,omitempty"`
	Email            string             `bson:"email,omitempty" json:"email,omitempty"`
	VATNumber        string             `bson:"vatNumber,omitempty" json:"vatNumber,omitempty"`
	FiscalCode       string             `bson:"fiscalCode,omitempty" json:"fiscalCode,omitempty"`
	Country          string             `bson:"country,omitempty" json:"country,omitempty"`
	AddressLine1     string             `bson:"addressLine1,omitempty" json:"addressLine1,omitempty"`
	AddressLine2     string             `bson:"addressLine2,omitempty" json:"addressLine2,omitempty"`
	City             string             `bson:"city,omitempty" json:"city,omitempty"`
	PostalCode       string             `bson:"postalCode,omitempty" json:"postalCode,omitempty"`
	Province         string             `bson:"province,omitempty" json:"province,omitempty"`
	IsCompany        bool               `bson:"isCompany" json:"isCompany"`
	StripeCustomerID string             `bson:"stripeCustomerID,omitempty" json:"stripeCustomerID,omitempty"`
	CreatedAt        time.Time          `bson:"createdAt" json:"createdAt"`
	UpdatedAt        time.Time          `bson:"updatedAt" json:"updatedAt"`
}
