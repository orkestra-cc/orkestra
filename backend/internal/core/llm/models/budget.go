package models

import "time"

// Budget actions: block refuses a reservation that does not fit the
// remaining monthly balance; warn_only admits it (no cap promise) and
// only notifies.
const (
	BudgetActionBlock    = "block"
	BudgetActionWarnOnly = "warn_only"
)

// Outbox thresholds: one logical notification each per org and period.
const (
	ThresholdWarn      = "warn"
	ThresholdExhausted = "exhausted"
)

// Outbox statuses. failed is terminal after the bounded retries.
const (
	OutboxStatusPending = "pending"
	OutboxStatusSending = "sending"
	OutboxStatusSent    = "sent"
	OutboxStatusFailed  = "failed"
)

// RefusalAlerts reports whether a refused reservation fires the period's
// "exhausted" alert: only when consumed + reserved (before the refused
// request) already reach the warn threshold, or the limit when warnPct
// is 0. A refusal caused by one oversized request while the balance is
// largely untouched is ledgered budget_blocked but keeps the month's only
// exhausted alert for when the balance is really running out.
func RefusalAlerts(consumedPlusReserved, limit int64, warnPct int) bool {
	if limit <= 0 {
		return false
	}
	if warnPct <= 0 {
		return consumedPlusReserved >= limit
	}
	return consumedPlusReserved*100 >= limit*int64(warnPct)
}

// Budget is the org's monthly token cap (llm_budgets, one per org; no
// document means no budget). MonthlyTokenLimit counts input + output of
// both credential kinds.
type Budget struct {
	TenantID             string    `json:"-" bson:"tenantId"`
	MonthlyTokenLimit    int64     `json:"monthlyTokenLimit" bson:"monthlyTokenLimit"`
	WarnPct              int       `json:"warnPct" bson:"warnPct"`
	Action               string    `json:"action" bson:"action"`
	NotifyExtraUserUUIDs []string  `json:"notifyExtraUserUuids" bson:"notifyExtraUserUuids"`
	UpdatedBy            string    `json:"updatedBy" bson:"updatedBy"`
	UpdatedAt            time.Time `json:"updatedAt" bson:"updatedAt"`
}

// OutboxEvent is one llm_budget_outbox row: a threshold notification
// written in the same transaction that marked the threshold on the
// rollup, delivered at-least-once by the outbox worker. Payload carries
// counters only (consumedTokens, limitTokens, pct).
type OutboxEvent struct {
	UUID          string         `json:"uuid" bson:"uuid"`
	TenantID      string         `json:"-" bson:"tenantId"`
	Period        string         `json:"period" bson:"period"`
	Threshold     string         `json:"threshold" bson:"threshold"`
	Payload       map[string]any `json:"payload,omitempty" bson:"payload,omitempty"`
	Status        string         `json:"status" bson:"status"`
	Attempts      int            `json:"attempts" bson:"attempts"`
	NextAttemptAt time.Time      `json:"nextAttemptAt" bson:"nextAttemptAt"`
	LeaseUntil    *time.Time     `json:"leaseUntil,omitempty" bson:"leaseUntil"`
	CreatedAt     time.Time      `json:"createdAt" bson:"createdAt"`
	SentAt        *time.Time     `json:"sentAt,omitempty" bson:"sentAt,omitempty"`
	// DeliveredTo lists the recipients (user UUIDs) already delivered by an
	// earlier attempt; a retry sends only to the others.
	DeliveredTo []string `json:"deliveredTo,omitempty" bson:"deliveredTo,omitempty"`
}
