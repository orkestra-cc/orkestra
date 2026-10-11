package models

import "time"

// Ledger outcomes (llm_usage.outcome). A row is born pending with its
// reservation and is closed exactly once, by the router's finalization
// or, for an orphan whose owner died, by the reconciler.
const (
	OutcomePending       = "pending"
	OutcomeOK            = "ok"
	OutcomeFallback      = "fallback"
	OutcomeError         = "error"
	OutcomeBudgetBlocked = "budget_blocked"
)

// MaxLedgerAttempts bounds Attempts on a ledger row (spec: at most 8).
const MaxLedgerAttempts = 8

// Attempt is one provider try inside a call: counters and codes only,
// never a body, a prompt or an output.
type Attempt struct {
	Provider       string `json:"provider" bson:"provider"`
	CredentialKind string `json:"credentialKind" bson:"credentialKind"`
	Outcome        string `json:"outcome" bson:"outcome"`
	InputTokens    int    `json:"inputTokens" bson:"inputTokens"`
	OutputTokens   int    `json:"outputTokens" bson:"outputTokens"`
}

// UsageEvent is one llm_usage row. No free text beyond caller, purpose
// and errorCode is ever stored: the ledger carries counters, codes and
// identifiers, not prompts or outputs.
//
// ReservedTokens is the whole admission reservation (estimated input +
// reserved output) while the row is pending and 0 once finalized;
// ReservedOutputTokens keeps the output share so the reconciler can
// charge an orphan's reservation with the right input/output split.
type UsageEvent struct {
	UUID                 string     `json:"uuid" bson:"uuid"`
	TenantID             string     `json:"-" bson:"tenantId"`
	UserUUID             string     `json:"userUuid,omitempty" bson:"userUuid,omitempty"`
	ModelUUID            string     `json:"modelUuid" bson:"modelUuid"`
	Provider             string     `json:"provider" bson:"provider"`
	ModelID              string     `json:"modelId" bson:"modelId"`
	CredentialKind       string     `json:"credentialKind" bson:"credentialKind"`
	CredentialUUID       string     `json:"credentialUuid,omitempty" bson:"credentialUuid,omitempty"`
	Caller               string     `json:"caller" bson:"caller"`
	Purpose              string     `json:"purpose" bson:"purpose"`
	InputTokens          int        `json:"inputTokens" bson:"inputTokens"`
	OutputTokens         int        `json:"outputTokens" bson:"outputTokens"`
	ReservedTokens       int        `json:"reservedTokens" bson:"reservedTokens"`
	ReservedOutputTokens int        `json:"reservedOutputTokens" bson:"reservedOutputTokens"`
	ReservationExpiresAt *time.Time `json:"reservationExpiresAt,omitempty" bson:"reservationExpiresAt,omitempty"`
	LatencyMs            int64      `json:"latencyMs" bson:"latencyMs"`
	Outcome              string     `json:"outcome" bson:"outcome"`
	ErrorCode            string     `json:"errorCode,omitempty" bson:"errorCode,omitempty"`
	Fallback             bool       `json:"fallback" bson:"fallback"`
	Attempts             []Attempt  `json:"attempts,omitempty" bson:"attempts,omitempty"`
	CreatedAt            time.Time  `json:"createdAt" bson:"createdAt"`
	FinalizedAt          *time.Time `json:"finalizedAt,omitempty" bson:"finalizedAt,omitempty"`
}

// FinalizeUpdate is what closing a pending row writes. Empty identity
// fields (Provider … CredentialUUID) keep the values stamped at
// reservation; a fallback sets the model that actually answered.
type FinalizeUpdate struct {
	Outcome        string
	ErrorCode      string
	Fallback       bool
	InputTokens    int
	OutputTokens   int
	Attempts       []Attempt
	LatencyMs      int64
	Provider       string
	ModelID        string
	ModelUUID      string
	CredentialKind string
	CredentialUUID string
	FinalizedAt    time.Time
}

// UsageMonthly is one llm_usage_monthly rollup: the org's consumption in
// a UTC calendar month, split by who pays, plus the tokens reserved by
// calls still in flight.
type UsageMonthly struct {
	TenantID                string     `json:"-" bson:"tenantId"`
	Period                  string     `json:"period" bson:"period"`
	Requests                int64      `json:"requests" bson:"requests"`
	OrgInputTokens          int64      `json:"orgInputTokens" bson:"orgInputTokens"`
	OrgOutputTokens         int64      `json:"orgOutputTokens" bson:"orgOutputTokens"`
	UserAccountInputTokens  int64      `json:"userAccountInputTokens" bson:"userAccountInputTokens"`
	UserAccountOutputTokens int64      `json:"userAccountOutputTokens" bson:"userAccountOutputTokens"`
	ReservedTokens          int64      `json:"reservedTokens" bson:"reservedTokens"`
	WarnedAt                *time.Time `json:"warnedAt,omitempty" bson:"warnedAt,omitempty"`
	ExhaustedAt             *time.Time `json:"exhaustedAt,omitempty" bson:"exhaustedAt,omitempty"`
	OversizedAt             *time.Time `json:"oversizedAt,omitempty" bson:"oversizedAt,omitempty"`
	UpdatedAt               time.Time  `json:"updatedAt" bson:"updatedAt"`
}

// Consumed is the real consumption counted against the budget: input and
// output of both credential kinds. Pointer receiver: see the Huma note in
// AGENTS.md (types embedded in HTTP bodies must not have value methods).
func (m *UsageMonthly) Consumed() int64 {
	if m == nil {
		return 0
	}
	return m.OrgInputTokens + m.OrgOutputTokens + m.UserAccountInputTokens + m.UserAccountOutputTokens
}

// LLMUsageRow is one group of the admin usage report (by model, user or
// caller): finalized rows that consumed tokens.
type LLMUsageRow struct {
	Key          string `json:"key" bson:"key"`
	Requests     int64  `json:"requests" bson:"requests"`
	InputTokens  int64  `json:"inputTokens" bson:"inputTokens"`
	OutputTokens int64  `json:"outputTokens" bson:"outputTokens"`
}

// LLMSelfUsage is a user's own consumption in one month, split by
// credential kind (GET /v1/llm/me/usage).
type LLMSelfUsage struct {
	Period                  string `json:"period"`
	Requests                int64  `json:"requests"`
	OrgInputTokens          int64  `json:"orgInputTokens"`
	OrgOutputTokens         int64  `json:"orgOutputTokens"`
	UserAccountInputTokens  int64  `json:"userAccountInputTokens"`
	UserAccountOutputTokens int64  `json:"userAccountOutputTokens"`
}

// PeriodLayout is the format of a rollup period (UTC calendar month).
const PeriodLayout = "2006-01"

// PeriodOf returns the UTC calendar month t falls in, as "YYYY-MM".
func PeriodOf(t time.Time) string { return t.UTC().Format(PeriodLayout) }

// PeriodBounds returns [start, end) of a "YYYY-MM" period in UTC.
func PeriodBounds(period string) (time.Time, time.Time, error) {
	start, err := time.ParseInLocation(PeriodLayout, period, time.UTC)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	return start, start.AddDate(0, 1, 0), nil
}
