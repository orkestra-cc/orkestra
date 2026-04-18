package models

import "time"

// Subscription links a Client to a Service tier with a recurring billing
// cycle. The renewal job scans NextBillingAt and transitions Status through
// the state machine defined in services/subscription_service.go.
//
// ADR-0001: TenantUUID is the forward-looking pointer to a Tier-2 Tenant
// (external client). Populated by Phase 3 onboarding when a Client is
// promoted to a Tenant via /v1/public/tenants. Both fields coexist during
// the migration window; handlers SHOULD prefer TenantUUID when set and fall
// back to ClientUUID otherwise. Once every row carries TenantUUID, the
// ClientUUID field and the Client entity itself are removed.
type Subscription struct {
	UUID               string         `bson:"uuid" json:"uuid"`
	ClientUUID         string         `bson:"clientUUID" json:"clientUUID"`
	TenantUUID         string         `bson:"tenantUUID,omitempty" json:"tenantUUID,omitempty"`
	ServiceUUID        string         `bson:"serviceUUID" json:"serviceUUID"`
	TierCode           string         `bson:"tierCode" json:"tierCode"`
	Status             SubStatus      `bson:"status" json:"status"`
	StartedAt          time.Time      `bson:"startedAt" json:"startedAt"`
	CurrentPeriodStart time.Time      `bson:"currentPeriodStart" json:"currentPeriodStart"`
	CurrentPeriodEnd   time.Time      `bson:"currentPeriodEnd" json:"currentPeriodEnd"`
	NextBillingAt      time.Time      `bson:"nextBillingAt" json:"nextBillingAt"`
	CancelledAt        *time.Time     `bson:"cancelledAt,omitempty" json:"cancelledAt,omitempty"`
	EndsAt             *time.Time     `bson:"endsAt,omitempty" json:"endsAt,omitempty"`
	CancelAtPeriodEnd  bool           `bson:"cancelAtPeriodEnd" json:"cancelAtPeriodEnd"`
	FailedChargeCount  int            `bson:"failedChargeCount" json:"failedChargeCount"`
	PaymentProvider    string         `bson:"paymentProvider,omitempty" json:"paymentProvider,omitempty"`
	PaymentMethodID    string         `bson:"paymentMethodID,omitempty" json:"paymentMethodID,omitempty"`
	Metadata           map[string]any `bson:"metadata,omitempty" json:"metadata,omitempty"`
	CreatedAt          time.Time      `bson:"createdAt" json:"createdAt"`
	UpdatedAt          time.Time      `bson:"updatedAt" json:"updatedAt"`
}
