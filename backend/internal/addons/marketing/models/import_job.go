package models

import "time"

// ImportJobStatus enumerates the lifecycle states of an import job.
// Phase 1 ships a minimal subset — no `paused_for_review` (Phase 3,
// conflict-review queue) and no `queued`/`running` separation
// (Phase-1 imports run synchronously inside the POST handler, so the
// job transitions queued→running→done|failed in one request).
type ImportJobStatus string

const (
	ImportJobStatusQueued  ImportJobStatus = "queued"
	ImportJobStatusRunning ImportJobStatus = "running"
	ImportJobStatusDone    ImportJobStatus = "done"
	ImportJobStatusFailed  ImportJobStatus = "failed"
)

// ImportJobStats accumulates per-row outcomes during an import. Every
// counter is bumped on its own code path so the totals are mutually
// exclusive across rows (one row contributes to exactly one of the
// created / merged / skipped buckets per resource).
type ImportJobStats struct {
	RowsRead          int `bson:"rowsRead" json:"rowsRead"`
	RowsFailed        int `bson:"rowsFailed,omitempty" json:"rowsFailed,omitempty"`
	OrgsCreated       int `bson:"orgsCreated,omitempty" json:"orgsCreated,omitempty"`
	OrgsMerged        int `bson:"orgsMerged,omitempty" json:"orgsMerged,omitempty"`
	PersonsCreated    int `bson:"personsCreated,omitempty" json:"personsCreated,omitempty"`
	PersonsMerged     int `bson:"personsMerged,omitempty" json:"personsMerged,omitempty"`
	MembershipsLinked int `bson:"membershipsLinked,omitempty" json:"membershipsLinked,omitempty"`
	// ConflictsSkipped counts the per-field skips applied by the
	// pipeline's auto-merge path (Phase-1 conservative policy: when
	// incoming and existing disagree on primary_email / vat /
	// taxCode, the field is left alone and this counter increments).
	// Phase 3 replaces this with a routed-to-review-queue flow.
	ConflictsSkipped int `bson:"conflictsSkipped,omitempty" json:"conflictsSkipped,omitempty"`
}

// ImportJob is the persisted audit record of one import run. Sources[]
// entries on Person / Organization rows carry the JobUUID so any row
// can be traced back to the run that produced it.
//
// Phase 1 schema is intentionally narrower than
// docs/plans/marketing-addon/schemas/marketing_import_jobs.md — the
// full schema lands in Phase 3 with the review-queue work
// (paused_for_review status, ConflictReviewUUIDs reference array).
type ImportJob struct {
	UUID     string `bson:"uuid" json:"uuid"`
	TenantID string `bson:"tenantId" json:"tenantId"`

	// Importer names the adapter that produced this job (e.g. "csv").
	Importer string `bson:"importer" json:"importer"`

	// SourceName is the operator-facing label — usually the uploaded
	// filename, but the handler accepts an override so operators can
	// distinguish two imports of the same name.
	SourceName string `bson:"sourceName,omitempty" json:"sourceName,omitempty"`

	Status ImportJobStatus `bson:"status" json:"status"`
	Stats  ImportJobStats  `bson:"stats" json:"stats"`

	// Error carries the failure reason when Status == failed. Empty
	// otherwise.
	Error string `bson:"error,omitempty" json:"error,omitempty"`

	CreatedAt   time.Time  `bson:"createdAt" json:"createdAt"`
	StartedAt   *time.Time `bson:"startedAt,omitempty" json:"startedAt,omitempty"`
	CompletedAt *time.Time `bson:"completedAt,omitempty" json:"completedAt,omitempty"`
	CreatedBy   string     `bson:"createdBy,omitempty" json:"createdBy,omitempty"`
}
