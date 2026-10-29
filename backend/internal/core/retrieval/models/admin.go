package models

import (
	"time"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

// RetrievalComponentStatus is the health of one component the admin page
// reports on (the engine, or one inference endpoint in one role).
type RetrievalComponentStatus string

const (
	ComponentOK            RetrievalComponentStatus = "ok"
	ComponentNotConfigured RetrievalComponentStatus = "not_configured"
	ComponentUnavailable   RetrievalComponentStatus = "unavailable"
	ComponentMismatch      RetrievalComponentStatus = "mismatch"
	ComponentWrongKind     RetrievalComponentStatus = "wrong_kind"
)

// RetrievalEngineStatus reports the vector engine behind QDRANT_URL.
type RetrievalEngineStatus struct {
	Status RetrievalComponentStatus `json:"status" enum:"ok,not_configured,unavailable"`
}

// RetrievalInferenceStatus reports one inference endpoint in one role: the
// embed role for an ingestion profile, or the rerank role when enabled.
type RetrievalInferenceStatus struct {
	Endpoint string                   `json:"endpoint"`
	Kind     string                   `json:"kind" enum:"embed,rerank"`
	Status   RetrievalComponentStatus `json:"status" enum:"ok,not_configured,unavailable,mismatch,wrong_kind"`
	Profile  string                   `json:"profile,omitempty"` // InferenceProfile.ID when ok
	Boundary iface.InferenceBoundary  `json:"boundary,omitempty"`
}

// RetrievalBacklog is the outbox work not yet applied.
type RetrievalBacklog struct {
	Pending  int64      `json:"pending"`
	OldestAt *time.Time `json:"oldestAt,omitempty"`
}

// RetrievalCorpusStatus is one corpus on the admin page. Registered corpora
// come from the spec, orphans (state row, no registration) from the state.
type RetrievalCorpusStatus struct {
	Name             string                  `json:"name"`
	Owner            string                  `json:"owner,omitempty"`
	OwnerRunning     bool                    `json:"ownerRunning"`
	Registered       bool                    `json:"registered"`
	Locality         iface.RetrievalLocality `json:"locality,omitempty"` // spec for registered, state row for orphans
	EmbedEndpoint    string                  `json:"embedEndpoint,omitempty"`
	RerankEnabled    bool                    `json:"rerankEnabled"`
	State            string                  `json:"state" enum:"uninitialized,active,profile_changed,error"`
	Reason           string                  `json:"reason,omitempty" enum:"locality,endpoint_unknown,endpoint_kind,unknown_state"`
	ActiveGeneration int                     `json:"activeGeneration"` // 0 = none; an error corpus with one still serves it
	Collection       string                  `json:"collection,omitempty"`
	IngestionProfile string                  `json:"ingestionProfile,omitempty"` // hash only
	EmbeddingProfile string                  `json:"embeddingProfile,omitempty"` // InferenceProfile.ID the generation was built with
	Outbox           RetrievalBacklog        `json:"outbox"`
	TenantsDone      int64                   `json:"tenantsDone"`     // active generation
	TenantsBuilding  int64                   `json:"tenantsBuilding"` // active generation
}

// RetrievalErasureCounts counts erasure tombstones not yet finished.
type RetrievalErasureCounts struct {
	Pending int64 `json:"pending"` // status pending
	Fencing int64 `json:"fencing"` // first pass done, waiting for the fencing window
}

// RetrievalCorporaReport is the installation-wide admin view.
type RetrievalCorporaReport struct {
	Engine    RetrievalEngineStatus      `json:"engine"`
	Inference []RetrievalInferenceStatus `json:"inference"`
	Corpora   []RetrievalCorpusStatus    `json:"corpora"` // registered first (by name), then orphans (by name)
	Erasures  RetrievalErasureCounts     `json:"erasures"`
}

// RetrievalTenantCorpus is one registered corpus for one tenant.
type RetrievalTenantCorpus struct {
	Corpus       string           `json:"corpus"`
	Owner        string           `json:"owner"`
	OwnerRunning bool             `json:"ownerRunning"`
	InitialBuild string           `json:"initialBuild" enum:"pending,building,done"`
	Points       *int64           `json:"points,omitempty"` // active generation, this tenant; absent unless engine ok
	Outbox       RetrievalBacklog `json:"outbox"`
}

// RetrievalTenantIndexingReport is the tenant's view of indexing progress.
type RetrievalTenantIndexingReport struct {
	TenantID        string                  `json:"tenantId"`
	Engine          RetrievalEngineStatus   `json:"engine"`
	Corpora         []RetrievalTenantCorpus `json:"corpora"`         // registered corpora only, by name
	ErasuresPending int64                   `json:"erasuresPending"` // tenant-scope tombstones not done
}

// RetrievalDiagnosticsRequest asks what a search would return for the caller.
type RetrievalDiagnosticsRequest struct {
	Query  string                 `json:"query" minLength:"1" maxLength:"8000"` // rune bound 2000 enforced by the pipeline
	Filter *iface.RetrievalFilter `json:"filter,omitempty"`
	Mode   iface.RetrievalMode    `json:"mode,omitempty" enum:"hybrid,dense,keyword"`
	Limit  int                    `json:"limit,omitempty" minimum:"1" maximum:"100" default:"10"`
}

// RetrievalDiagnosticHit is one returned source with its per-stage scores.
type RetrievalDiagnosticHit struct {
	SourceID string   `json:"sourceId"`
	ChunkID  string   `json:"chunkId"`
	Rank     int      `json:"rank"`
	Score    float32  `json:"score"` // final, as a consumer would see it
	Fused    float32  `json:"fused"` // RRF score before rerank
	Dense    *float32 `json:"dense,omitempty"`
	Keyword  *float32 `json:"keyword,omitempty"`
	Rerank   *float32 `json:"rerank,omitempty"`
	Snippet  string   `json:"snippet"`
}

// RetrievalDiagnosticsReport is the result of a diagnostic search.
type RetrievalDiagnosticsReport struct {
	Corpus     string                   `json:"corpus"`
	Collection string                   `json:"collection"`
	Profile    string                   `json:"profile"`    // embedding profile pinned for the request
	Principals []string                 `json:"principals"` // the caller's own, as applied
	Degraded   bool                     `json:"degraded"`
	Hits       []RetrievalDiagnosticHit `json:"hits"`
}

// RetrievalSearchLogEntry is one audited search. Filter is the decoded
// canonical filter JSON, with numbers kept exact.
type RetrievalSearchLogEntry struct {
	ID                string    `json:"id"` // row uuid
	CreatedAt         time.Time `json:"createdAt"`
	UserUUID          string    `json:"userUuid"`
	Corpus            string    `json:"corpus"`
	Query             string    `json:"query"`
	Filter            any       `json:"filter,omitempty"`
	Mode              string    `json:"mode"`
	Principals        []string  `json:"principals"`
	ReturnedSourceIDs []string  `json:"returnedSourceIds"`
	Subjects          []string  `json:"subjects"`
	HitCount          int       `json:"hitCount"`
	Degraded          bool      `json:"degraded"`
	Outcome           string    `json:"outcome" enum:"ok,degraded,error"`
	ErrorKind         string    `json:"errorKind,omitempty"`
	DurationMs        int64     `json:"durationMs"` // Timings.TotalMs
}

// RetrievalSearchLogPage is one page of the audited searches, newest first.
type RetrievalSearchLogPage struct {
	Items  []RetrievalSearchLogEntry `json:"items"`
	Total  int64                     `json:"total"`
	Limit  int                       `json:"limit"`
	Offset int                       `json:"offset"`
}

// SearchLogQuery selects search-log rows for one tenant (the tenant comes
// from the context). Since and Until are inclusive; nil means unbounded.
type SearchLogQuery struct {
	UserUUID      string
	Since, Until  *time.Time
	Limit, Offset int
}

// ProgressKey identifies one generation's initial-build progress for a corpus.
type ProgressKey struct {
	Corpus     string
	Generation int
}

// ProgressCounts counts tenants by initial-build status for one generation.
type ProgressCounts struct{ Done, Building int64 }
