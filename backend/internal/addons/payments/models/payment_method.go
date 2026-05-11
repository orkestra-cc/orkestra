package models

import (
	"time"
)

// PaymentMethod is a tokenized card/SEPA/etc. stored on a provider customer.
// We never store raw PANs — only the provider's reference token (pm_xxx).
//
// TenantUUID is the tenant the saved card belongs to.
type PaymentMethod struct {
	UUID             string       `bson:"uuid" json:"uuid"`
	TenantUUID       string       `bson:"tenantUUID" json:"tenantUUID"`
	Provider         ProviderName `bson:"provider" json:"provider"`
	ProviderMethodID string       `bson:"providerMethodID" json:"providerMethodID"` // pm_xxx
	Brand            string       `bson:"brand,omitempty" json:"brand,omitempty"`
	Last4            string       `bson:"last4,omitempty" json:"last4,omitempty"`
	ExpiryMonth      int          `bson:"expiryMonth,omitempty" json:"expiryMonth,omitempty"`
	ExpiryYear       int          `bson:"expiryYear,omitempty" json:"expiryYear,omitempty"`
	IsDefault        bool         `bson:"isDefault" json:"isDefault"`
	CreatedAt        time.Time    `bson:"createdAt" json:"createdAt"`
	UpdatedAt        time.Time    `bson:"updatedAt" json:"updatedAt"`
}
