package models

import "encoding/json"

// --- Skill Request/Response ---

// SkillRequest is the Huma input for individual skill endpoints
type SkillRequest struct {
	Body struct {
		URL    string `json:"url" doc:"Target company URL" minLength:"1"`
		Locale string `json:"locale,omitempty" doc:"Locale for prompt selection (default: config default)" enum:"it,en"`
		// Context allows passing additional info to the skill prompt
		Context string `json:"context,omitempty" doc:"Additional context for the skill"`
	}
}

// SkillResponse is the Huma output for individual skill endpoints
type SkillResponse struct {
	Body struct {
		Skill        string          `json:"skill" doc:"Skill that was executed"`
		Result       json.RawMessage `json:"result" doc:"Skill output (structure varies by skill)"`
		InputTokens  int             `json:"inputTokens" doc:"LLM input tokens used"`
		OutputTokens int             `json:"outputTokens" doc:"LLM output tokens used"`
		LatencyMs    int64           `json:"latencyMs" doc:"Execution time in milliseconds"`
		ModelUsed    string          `json:"modelUsed" doc:"LLM model identifier"`
	}
}

// SkillTaskResponse is the Huma output for async skill submission
type SkillTaskResponse struct {
	Body struct {
		TaskID string `json:"taskId" doc:"Task UUID for polling"`
	}
}

// SkillTaskPollRequest is the Huma input for GET /v1/sales/skills/{taskId}
type SkillTaskPollRequest struct {
	TaskID string `path:"taskId" doc:"Task UUID returned by skill submission"`
}

// SkillTaskPollResponse is the Huma output for polling a skill task
type SkillTaskPollResponse struct {
	Body struct {
		TaskID       string          `json:"taskId"`
		Status       string          `json:"status" doc:"running, completed, or failed"`
		Skill        string          `json:"skill,omitempty"`
		Result       json.RawMessage `json:"result,omitempty"`
		InputTokens  int             `json:"inputTokens,omitempty"`
		OutputTokens int             `json:"outputTokens,omitempty"`
		LatencyMs    int64           `json:"latencyMs,omitempty"`
		ModelUsed    string          `json:"modelUsed,omitempty"`
		Error        string          `json:"error,omitempty"`
	}
}

// --- Prospect Request/Response ---

// ProspectRequest is the Huma input for POST /v1/sales/prospect
type ProspectRequest struct {
	Body struct {
		URL    string `json:"url" doc:"Target company URL" minLength:"1"`
		Locale string `json:"locale,omitempty" doc:"Locale for prompts (default: config default)" enum:"it,en"`
	}
}

// ProspectResponse is the Huma output for POST /v1/sales/prospect (async)
type ProspectResponse struct {
	Body struct {
		JobID     string `json:"jobId" doc:"Job UUID for tracking"`
		StreamURL string `json:"streamUrl" doc:"SSE endpoint for real-time progress"`
	}
}

// QuickProspectResponse is the Huma output for POST /v1/sales/prospect/quick (sync)
type QuickProspectResponse struct {
	Body struct {
		Score        int             `json:"score" doc:"Composite prospect score (0-100)"`
		Grade        string          `json:"grade" doc:"Letter grade (A-F)"`
		CompanyName  string          `json:"companyName" doc:"Detected company name"`
		Summary      string          `json:"summary" doc:"Brief prospect summary"`
		Findings     json.RawMessage `json:"findings" doc:"Detailed findings"`
		InputTokens  int             `json:"inputTokens"`
		OutputTokens int             `json:"outputTokens"`
		LatencyMs    int64           `json:"latencyMs"`
	}
}

// --- Job Request/Response ---

// JobListRequest is the Huma input for GET /v1/sales/jobs
type JobListRequest struct {
	Page     int    `query:"page" doc:"Page number (1-based)" default:"1" minimum:"1"`
	PageSize int    `query:"pageSize" doc:"Items per page" default:"20" minimum:"1" maximum:"100"`
	Status   string `query:"status,omitempty" doc:"Filter by status"`
}

// JobListResponse is the Huma output for GET /v1/sales/jobs
type JobListResponse struct {
	Body struct {
		Jobs     []Job `json:"jobs"`
		Total    int64 `json:"total"`
		Page     int   `json:"page"`
		PageSize int   `json:"pageSize"`
	}
}

// JobDetailRequest is the Huma input for GET /v1/sales/jobs/{uuid}
type JobDetailRequest struct {
	UUID string `path:"uuid" doc:"Job UUID"`
}

// JobDetailResponse is the Huma output for GET /v1/sales/jobs/{uuid}
type JobDetailResponse struct {
	Body Job
}

// JobDeleteRequest is the Huma input for DELETE /v1/sales/jobs/{uuid}
type JobDeleteRequest struct {
	UUID string `path:"uuid" doc:"Job UUID"`
}

// JobRetryRequest is the Huma input for POST /v1/sales/jobs/{uuid}/retry
type JobRetryRequest struct {
	UUID string `path:"uuid" doc:"Job UUID"`
}

// JobRerunRequest is the Huma input for POST /v1/sales/jobs/{uuid}/rerun
type JobRerunRequest struct {
	UUID string `path:"uuid" doc:"Job UUID"`
}

// JobRerunResponse is the Huma output for POST /v1/sales/jobs/{uuid}/rerun
type JobRerunResponse struct {
	Body Job
}
