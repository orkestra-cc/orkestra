package models

import (
	"time"
)

// Transaction is one provider-side charge attempt. One invoice may have
// multiple transactions (e.g. after retries), distinguished by ProviderTxID.
//
// TenantUUID is the tenant billed for the charge. Webhook reconciliation
// reads tenantUUID off the metadata stamp the renewal service places on
// every PaymentIntent.
type Transaction struct {
	UUID             string            `bson:"uuid" json:"uuid"`
	Provider         ProviderName      `bson:"provider" json:"provider"`
	ProviderTxID     string            `bson:"providerTxID" json:"providerTxID"`
	SubscriptionUUID string            `bson:"subscriptionUUID,omitempty" json:"subscriptionUUID,omitempty"`
	InvoiceUUID      string            `bson:"invoiceUUID,omitempty" json:"invoiceUUID,omitempty"`
	TenantUUID       string            `bson:"tenantUUID,omitempty" json:"tenantUUID,omitempty"`
	AmountCents      int64             `bson:"amountCents" json:"amountCents"`
	Currency         string            `bson:"currency" json:"currency"`
	Status           TransactionStatus `bson:"status" json:"status"`
	FailureCode      string            `bson:"failureCode,omitempty" json:"failureCode,omitempty"`
	FailureMsg       string            `bson:"failureMsg,omitempty" json:"failureMsg,omitempty"`
	RefundedCents    int64             `bson:"refundedCents,omitempty" json:"refundedCents,omitempty"`
	RefundedAt       *time.Time        `bson:"refundedAt,omitempty" json:"refundedAt,omitempty"`
	ChargedAt        *time.Time        `bson:"chargedAt,omitempty" json:"chargedAt,omitempty"`
	Description      string            `bson:"description,omitempty" json:"description,omitempty"`
	Metadata         map[string]string `bson:"metadata,omitempty" json:"metadata,omitempty"`
	CreatedAt        time.Time         `bson:"createdAt" json:"createdAt"`
	UpdatedAt        time.Time         `bson:"updatedAt" json:"updatedAt"`
}
