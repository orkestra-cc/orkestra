package models

import "encoding/json"

// QuickProspectResult is the output of a synchronous quick prospect analysis
type QuickProspectResult struct {
	Score        int             `json:"score"`
	Grade        string          `json:"grade"`
	CompanyName  string          `json:"companyName"`
	Summary      string          `json:"summary"`
	Findings     json.RawMessage `json:"findings"`
	InputTokens  int             `json:"inputTokens"`
	OutputTokens int             `json:"outputTokens"`
	LatencyMs    int64           `json:"latencyMs"`
}
