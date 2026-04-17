package models

import "time"

// WebhookEvent records a received + verified webhook payload. The unique
// (provider, providerEventID) index is the idempotency guard: duplicate
// deliveries are rejected at insert time with a duplicate key error.
type WebhookEvent struct {
	UUID            string         `bson:"uuid" json:"uuid"`
	Provider        ProviderName   `bson:"provider" json:"provider"`
	ProviderEventID string         `bson:"providerEventID" json:"providerEventID"`
	Type            string         `bson:"type" json:"type"` // provider-native type, e.g. "payment_intent.succeeded"
	Normalized      string         `bson:"normalized,omitempty" json:"normalized,omitempty"` // "charge.succeeded" | "charge.failed" | "charge.refunded"
	InvoiceUUID     string         `bson:"invoiceUUID,omitempty" json:"invoiceUUID,omitempty"`
	SubscriptionUUID string        `bson:"subscriptionUUID,omitempty" json:"subscriptionUUID,omitempty"`
	Processed       bool           `bson:"processed" json:"processed"`
	ProcessError    string         `bson:"processError,omitempty" json:"processError,omitempty"`
	RawPayload      map[string]any `bson:"rawPayload,omitempty" json:"rawPayload,omitempty"`
	ReceivedAt      time.Time      `bson:"receivedAt" json:"receivedAt"`
	ProcessedAt     *time.Time     `bson:"processedAt,omitempty" json:"processedAt,omitempty"`
}
