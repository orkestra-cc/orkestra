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
	// ErasureRequestIncomplete: executed, but at least one producer is not done.
	ErasureRequestIncomplete = "incomplete"
	// ErasureRequestClosedPartial: closed by an operator; Abandoned lists the
	// producers never done.
	ErasureRequestClosedPartial = "closed_partial"
)

// Erasure request sources.
const (
	ErasureSourceRequest     = "request"      // lodged by the subject, executed by an operator (absent on legacy rows)
	ErasureSourceSelfService = "self_service" // POST /v1/me/dsr/erase
	ErasureSourceRetention   = "retention"    // retention auto-cleanup
)

// ActiveRetentionErasurePartialFilter scopes the unique userUuid index of
// ErasureRequestsCollection to incomplete retention requests: one subject has
// at most one, so two replicas running retention at once cannot both record
// and run a request for the same person. The two conditions are an ordered
// $and of single-key maps, not one two-key map: a Go map marshals its keys in
// random order, and the index specification must reach Mongo as the same
// bytes on every boot. Returned fresh per call (a map).
func ActiveRetentionErasurePartialFilter() map[string]any {
	return map[string]any{"$and": []any{
		map[string]any{"status": ErasureRequestIncomplete},
		map[string]any{"source": ErasureSourceRetention},
	}}
}

// Per-producer erasure outcomes.
const (
	ProducerOutcomeDone    = "done"
	ProducerOutcomePending = "pending"
	ProducerOutcomeFailed  = "failed"
)

// Run-level lastError values.
const (
	ErasureLastErrorLegalHold = "legal_hold_active"
	ErasureLastErrorRun       = "run_failed"
)

// ErasureProducerProgress is one producer instance's erasure state on a request.
type ErasureProducerProgress struct {
	Key            string    `bson:"key" json:"key"` // producer key (PIIProducerKeyer, else Subject, #n for duplicates)
	Subject        string    `bson:"subject" json:"subject"`
	Outcome        string    `bson:"outcome" json:"outcome" enum:"done,pending,failed"`
	RowsDeleted    int       `bson:"rowsDeleted" json:"rowsDeleted"`       // cumulative over attempts
	RowsAnonymized int       `bson:"rowsAnonymized" json:"rowsAnonymized"` // cumulative over attempts
	Attempts       int       `bson:"attempts" json:"attempts"`
	UpdatedAt      time.Time `bson:"updatedAt" json:"updatedAt"`
}

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

	Source        string                    `bson:"source,omitempty" json:"source,omitempty"`
	Progress      []ErasureProducerProgress `bson:"progress,omitempty" json:"progress,omitempty"`
	Attempts      int                       `bson:"attempts,omitempty" json:"attempts,omitempty"`
	NextAttemptAt *time.Time                `bson:"nextAttemptAt,omitempty" json:"nextAttemptAt,omitempty"` // nil on an incomplete row: retries exhausted
	LeaseUntil    *time.Time                `bson:"leaseUntil,omitempty" json:"-"`
	LastError     string                    `bson:"lastError,omitempty" json:"lastError,omitempty"`
	Abandoned     []string                  `bson:"abandoned,omitempty" json:"abandoned,omitempty"` // closed_partial: producer keys never done
	Closable      bool                      `bson:"-" json:"closable,omitempty"`                    // response-only, filled by ListOpen
}
