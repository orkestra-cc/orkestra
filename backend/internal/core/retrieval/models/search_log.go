package models

import "time"

// SearchTimings are per-stage wall times of one search, in milliseconds.
type SearchTimings struct {
	TotalMs   int64 `bson:"totalMs"`
	ResolveMs int64 `bson:"resolveMs"` // steps 1-3
	EmbedMs   int64 `bson:"embedMs"`
	EngineMs  int64 `bson:"engineMs"` // every engine.Query call
	CheckMs   int64 `bson:"checkMs"`  // tombstone veto + Visible
	RerankMs  int64 `bson:"rerankMs"`
}

const (
	SearchOutcomeOK       = "ok"
	SearchOutcomeDegraded = "degraded"
	SearchOutcomeError    = "error"
)

// SearchLog is one row per search that passed the gate (ADR-0023 D12): who
// looked up what, and which sources and subjects came back.
type SearchLog struct {
	UUID     string `bson:"uuid"`
	TenantID string `bson:"tenantId"`
	UserUUID string `bson:"userUuid"` // the searcher
	Corpus   string `bson:"corpus"`
	Query    string `bson:"query"`
	// FilterJSON is the request's filter as canonical JSON: json.Marshal of
	// the validated iface.RetrievalFilter (the SDK's json tags, unset fields
	// omitted, a datetime as RFC 3339). It is stored as a string so the row
	// keeps one shape for its whole retention, whatever Go or BSON type a
	// value had; json.Unmarshal into iface.RetrievalFilter gives a filter
	// that passes filter.Validate again. Empty (field absent): no filter.
	FilterJSON string        `bson:"filter,omitempty"`
	Mode       string        `bson:"mode"` // the defaulted request mode (hybrid|dense|keyword), not the arm that answered; Degraded records a fallback
	Principals []string      `bson:"principals"`
	SourceIDs  []string      `bson:"sourceIds"` // returned, rank order, unique
	Subjects   []string      `bson:"subjects"`  // union over returned points, sorted unique
	HitCount   int           `bson:"hitCount"`
	Degraded   bool          `bson:"degraded"`
	Outcome    string        `bson:"outcome"`
	ErrorKind  string        `bson:"errorKind,omitempty"` // not_configured|unavailable|principals|visibility_check|canceled
	Timings    SearchTimings `bson:"timings"`
	CreatedAt  time.Time     `bson:"createdAt"`
}
