package models

import "encoding/json"

// SkillResultInternal holds the output of a single skill execution
type SkillResultInternal struct {
	Skill        string          `json:"skill"`
	Result       json.RawMessage `json:"result"`
	InputTokens  int             `json:"inputTokens"`
	OutputTokens int             `json:"outputTokens"`
	LatencyMs    int64           `json:"latencyMs"`
	ModelUsed    string          `json:"modelUsed"`
}
