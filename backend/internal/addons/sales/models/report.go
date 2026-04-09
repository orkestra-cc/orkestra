package models

import (
	"encoding/json"
	"time"
)

// Report is a generated prospect analysis report
type Report struct {
	UUID        string          `bson:"uuid" json:"uuid"`
	JobUUID     string          `bson:"jobUuid" json:"jobUuid"`
	CreatedBy   string          `bson:"createdBy" json:"createdBy"`
	CompanyURL  string          `bson:"companyUrl" json:"companyUrl"`
	CompanyName string          `bson:"companyName" json:"companyName"`
	Score       int             `bson:"score" json:"score"`
	Grade       string          `bson:"grade" json:"grade"`
	ContentMD   string          `bson:"contentMd" json:"contentMd"`
	AgentData   json.RawMessage `bson:"agentData,omitempty" json:"agentData,omitempty"` // structured agent findings
	CreatedAt   time.Time       `bson:"createdAt" json:"createdAt"`
}

// --- DTOs ---

type ListReportsRequest struct {
	Page     int `query:"page" doc:"Page number" default:"1" minimum:"1"`
	PageSize int `query:"pageSize" doc:"Items per page" default:"20" minimum:"1" maximum:"100"`
}

type ListReportsResponse struct {
	Body struct {
		Reports  []Report `json:"reports"`
		Total    int64    `json:"total"`
		Page     int      `json:"page"`
		PageSize int      `json:"pageSize"`
	}
}

type GetReportRequest struct {
	UUID string `path:"uuid" doc:"Report UUID"`
}

type GetReportResponse struct {
	Body Report
}

type DownloadReportMDRequest struct {
	UUID string `path:"uuid" doc:"Report UUID"`
}

type GenerateReportRequest struct {
	JobUUID string `path:"jobUuid" doc:"Job UUID to generate report for"`
}
