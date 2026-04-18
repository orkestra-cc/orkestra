package models

import "time"

// ClientAddress is the billing address of a subscription Client. Named
// with the module prefix to avoid Huma schema collisions — other modules
// (e.g. company) also export an Address type.
type ClientAddress struct {
	Line1      string `bson:"line1,omitempty" json:"line1,omitempty"`
	Line2      string `bson:"line2,omitempty" json:"line2,omitempty"`
	City       string `bson:"city,omitempty" json:"city,omitempty"`
	Province   string `bson:"province,omitempty" json:"province,omitempty"`
	PostalCode string `bson:"postalCode,omitempty" json:"postalCode,omitempty"`
	Country    string `bson:"country,omitempty" json:"country,omitempty"`
}

// Client is an external buyer of services.
//
// Deprecated: ADR-0001 replaces this entity with tenant.Org{Kind="external"}.
// Until Phase 3 ships the external-client self-registration flow, Client
// records continue to exist as operator-managed "CRM-style" rows. Once a
// client goes through self-registration, Client will be removed and every
// subscription will point directly at a Tier-2 Tenant UUID.
//
// Do not add new fields to Client without documenting the Tenant equivalent.
// When in doubt, add the field to tenant/models/org.go instead.
//
// Migration plan:
//  1. Phase 3 adds POST /v1/public/tenants and creates Tier-2 Tenants.
//  2. For each pre-existing Client, we create a paired external Tenant and
//     set Client.OrgUUID to its UUID.
//  3. Subscription.ClientUUID is deprecated in favour of Subscription.TenantUUID.
//  4. Once every subscription is migrated, Client is deleted.
type Client struct {
	UUID             string       `bson:"uuid" json:"uuid"`
	OrgUUID          string       `bson:"orgUUID,omitempty" json:"orgUUID,omitempty"`
	LegalName        string       `bson:"legalName" json:"legalName"`
	DisplayName      string       `bson:"displayName" json:"displayName"`
	Email            string       `bson:"email" json:"email"`
	VATNumber        string       `bson:"vatNumber,omitempty" json:"vatNumber,omitempty"`
	FiscalCode       string       `bson:"fiscalCode,omitempty" json:"fiscalCode,omitempty"`
	BillingAddr      ClientAddress `bson:"billingAddr,omitempty" json:"billingAddr,omitempty"`
	Status           ClientStatus `bson:"status" json:"status"`
	StripeCustomerID string       `bson:"stripeCustomerID,omitempty" json:"stripeCustomerID,omitempty"`
	Notes            string       `bson:"notes,omitempty" json:"notes,omitempty"`
	CreatedAt        time.Time    `bson:"createdAt" json:"createdAt"`
	UpdatedAt        time.Time    `bson:"updatedAt" json:"updatedAt"`
}
