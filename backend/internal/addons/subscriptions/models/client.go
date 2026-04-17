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

// Client is an external buyer of services. Clients do not log in to Orkestra
// in v1; OrgUUID is a nullable hook for when a client also happens to be a
// tenant of the platform.
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
