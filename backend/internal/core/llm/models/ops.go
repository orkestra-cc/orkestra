package models

import (
	"time"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

// HTTP bodies of the PR 2 admin operations, usage and budget routes. Like
// dto.go, every type carries the LLM prefix (Huma schema names are
// package-less and duplicates panic).

// Credential test outcomes (llm_credentials.lastTestStatus).
const (
	TestStatusOK    = "ok"
	TestStatusError = "error"
)

// LLMCredentialTestResult is the answer of POST …/credentials/{uuid}/test:
// ok, or error with an llm.* code (never provider text).
type LLMCredentialTestResult struct {
	Outcome  string    `json:"outcome" enum:"ok,error"`
	Code     string    `json:"code,omitempty" doc:"llm.* error code when outcome is error"`
	TestedAt time.Time `json:"testedAt"`
}

// LLMDiscoveredModel is one model id the provider reports it serves.
type LLMDiscoveredModel struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName,omitempty"`
}

// LLMPromptTestBody is the admin prompt test request.
type LLMPromptTestBody struct {
	Prompt string `json:"prompt" minLength:"1" maxLength:"4000" doc:"Sent as a single user message; not stored"`
}

// LLMPromptTestResult is the collected answer of an admin prompt test.
// Truncated is set when the output exceeded the 64 KiB the console shows.
type LLMPromptTestResult struct {
	Text         string         `json:"text"`
	Truncated    bool           `json:"truncated,omitempty"`
	Usage        iface.LLMUsage `json:"usage"`
	FinishReason string         `json:"finishReason"`
	Fallback     bool           `json:"fallback"`
}

// Usage report groupings (GET /v1/admin/llm/usage?groupBy=…) and the
// ledger field each one aggregates on.
var UsageGroupFields = map[string]string{"model": "modelUuid", "user": "userUuid", "caller": "caller"}

// LLMUsageMonth is the HTTP view of the current month's rollup.
type LLMUsageMonth UsageMonthly

// LLMBudgetView is the HTTP view of the org's budget.
type LLMBudgetView Budget

// LLMUsageReport is the admin usage report: the grouped ledger rows in
// [from, to), the current month's rollup and the budget (absent when the
// org has none).
type LLMUsageReport struct {
	From    time.Time      `json:"from"`
	To      time.Time      `json:"to"`
	GroupBy string         `json:"groupBy" enum:"model,user,caller"`
	Rows    []LLMUsageRow  `json:"rows"`
	Month   LLMUsageMonth  `json:"month"`
	Budget  *LLMBudgetView `json:"budget,omitempty"`
}

// LLMBudgetPutBody creates or replaces the org's budget. warnPct absent
// takes the module default (budget_warn_pct); 0 disables the warning.
type LLMBudgetPutBody struct {
	MonthlyTokenLimit    int64    `json:"monthlyTokenLimit" minimum:"1" doc:"Input + output tokens per UTC month, both credential kinds"`
	WarnPct              *int     `json:"warnPct,omitempty" minimum:"0" maximum:"100"`
	Action               string   `json:"action" enum:"block,warn_only" doc:"block refuses a call that does not fit the remaining budget; warn_only admits it and only notifies"`
	NotifyExtraUserUUIDs []string `json:"notifyExtraUserUuids,omitempty" maxItems:"50" doc:"Members notified in addition to the org owners and admins"`
}
