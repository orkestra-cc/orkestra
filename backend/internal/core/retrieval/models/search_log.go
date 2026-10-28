package models

import (
	"time"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

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
	UUID       string                 `bson:"uuid"`
	TenantID   string                 `bson:"tenantId"`
	UserUUID   string                 `bson:"userUuid"` // the searcher
	Corpus     string                 `bson:"corpus"`
	Query      string                 `bson:"query"`
	Filter     *iface.RetrievalFilter `bson:"filter,omitempty"` // as validated
	Mode       string                 `bson:"mode"`             // the defaulted request mode (hybrid|dense|keyword), not the arm that answered; Degraded records a fallback
	Principals []string               `bson:"principals"`
	SourceIDs  []string               `bson:"sourceIds"` // returned, rank order, unique
	Subjects   []string               `bson:"subjects"`  // union over returned points, sorted unique
	HitCount   int                    `bson:"hitCount"`
	Degraded   bool                   `bson:"degraded"`
	Outcome    string                 `bson:"outcome"`
	ErrorKind  string                 `bson:"errorKind,omitempty"` // not_configured|unavailable|principals|visibility_check|canceled
	Timings    SearchTimings          `bson:"timings"`
	CreatedAt  time.Time              `bson:"createdAt"`
}
