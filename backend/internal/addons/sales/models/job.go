package models

import "time"

// Job tracks the lifecycle of a sales intelligence job
type Job struct {
	UUID         string         `bson:"uuid" json:"uuid"`
	CreatedBy    string         `bson:"createdBy" json:"createdBy"`
	CompanyURL   string         `bson:"companyUrl" json:"companyUrl"`
	Locale       string         `bson:"locale" json:"locale"`
	Status       JobStatus      `bson:"status" json:"status"`
	Phases       []JobPhase     `bson:"phases" json:"phases"`
	AgentResults []*AgentResult `bson:"agentResults,omitempty" json:"agentResults,omitempty"`
	ReportUUID   string         `bson:"reportUuid,omitempty" json:"reportUuid,omitempty"`
	TotalScore   int            `bson:"totalScore,omitempty" json:"totalScore,omitempty"`
	Grade        string         `bson:"grade,omitempty" json:"grade,omitempty"`
	ErrorMessage string         `bson:"errorMessage,omitempty" json:"errorMessage,omitempty"`
	CreatedAt    time.Time      `bson:"createdAt" json:"createdAt"`
	UpdatedAt    time.Time      `bson:"updatedAt" json:"updatedAt"`
	CompletedAt  *time.Time     `bson:"completedAt,omitempty" json:"completedAt,omitempty"`
}

// JobPhase tracks the status of a single pipeline phase
type JobPhase struct {
	Name        string     `bson:"name" json:"name"`
	Status      string     `bson:"status" json:"status"` // "pending", "running", "completed", "failed"
	StartedAt   *time.Time `bson:"startedAt,omitempty" json:"startedAt,omitempty"`
	CompletedAt *time.Time `bson:"completedAt,omitempty" json:"completedAt,omitempty"`
	Error       string     `bson:"error,omitempty" json:"error,omitempty"`
}
