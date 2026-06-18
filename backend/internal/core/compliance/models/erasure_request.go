package models

import "time"

// ErasureRequestsCollection backs the GDPR right-to-erasure request workflow:
// a data subject lodges a request, an operator reviews and executes (or
// rejects) it. Distinct from the immediate self-service /me/dsr/erase path —
// this is the mediated, audit-tracked workflow.
const ErasureRequestsCollection = "compliance_erasure_requests"

const (
	ErasureRequestPending   = "pending"
	ErasureRequestCompleted = "completed"
	ErasureRequestRejected  = "rejected"
)

// ErasureRequest is one lodged right-to-erasure request.
type ErasureRequest struct {
	UUID           string     `bson:"uuid" json:"uuid"`
	UserUUID       string     `bson:"userUuid" json:"userUuid"`
	TenantID       string     `bson:"tenantId,omitempty" json:"tenantId,omitempty"`
	Reason         string     `bson:"reason,omitempty" json:"reason,omitempty"`
	Status         string     `bson:"status" json:"status"`
	RequestedAt    time.Time  `bson:"requestedAt" json:"requestedAt"`
	ResolvedAt     *time.Time `bson:"resolvedAt,omitempty" json:"resolvedAt,omitempty"`
	ResolvedBy     string     `bson:"resolvedBy,omitempty" json:"resolvedBy,omitempty"`
	Mode           string     `bson:"mode,omitempty" json:"mode,omitempty"`
	ResolutionNote string     `bson:"resolutionNote,omitempty" json:"resolutionNote,omitempty"`
}
