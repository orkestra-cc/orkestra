package models

import (
	"time"
)

// ActivityLog is an append-only audit record per subscription. Used both
// for human-readable history on the subscription detail page and for
// operational troubleshooting (why did this charge fail?).
type ActivityLog struct {
	UUID             string         `bson:"uuid" json:"uuid"`
	SubscriptionUUID string         `bson:"subscriptionUUID" json:"subscriptionUUID"`
	TenantUUID       string         `bson:"tenantUUID,omitempty" json:"tenantUUID,omitempty"`
	Type             ActivityType   `bson:"type" json:"type"`
	Actor            string         `bson:"actor" json:"actor"` // "system" or userUUID
	Message          string         `bson:"message" json:"message"`
	Payload          map[string]any `bson:"payload,omitempty" json:"payload,omitempty"`
	CreatedAt        time.Time      `bson:"createdAt" json:"createdAt"`
}
