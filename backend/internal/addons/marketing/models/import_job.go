package models

import "time"

// ImportJobStatus enumerates the lifecycle states of an import job.
// Phase 3 moved the importer behind a background worker, so the
// queued → running → done|failed transitions are split across the
// HTTP boundary; the paused_for_review branch holds a job while
// the operator drains its pending marketing_conflict_reviews.
//
// State diagram:
//
//	queued ──► running ──┬─► done
//	                     │
//	                     ├─► failed
//	                     │
//	                     └─► paused_for_review ──► running (on last resolve)
type ImportJobStatus string

const (
	ImportJobStatusQueued          ImportJobStatus = "queued"
	ImportJobStatusRunning         ImportJobStatus = "running"
	ImportJobStatusPausedForReview ImportJobStatus = "paused_for_review"
	ImportJobStatusDone            ImportJobStatus = "done"
	ImportJobStatusFailed          ImportJobStatus = "failed"
)

// IsKnownImportJobStatus is the closed-enum guard used by repository
// filters and the boot-recovery sweep.
func IsKnownImportJobStatus(s ImportJobStatus) bool {
	switch s {
	case ImportJobStatusQueued, ImportJobStatusRunning,
		ImportJobStatusPausedForReview, ImportJobStatusDone, ImportJobStatusFailed:
		return true
	}
	return false
}

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

	// EngagementEmitted counts the marketing_activities rows the
	// importer auto-emitted from per-row engagement signals
	// (Phase 4 engagement-CSV flow). The Activity collection's
	// dedupKey unique index makes a re-import of the same payload
	// a no-op at the write boundary; this counter reflects the
	// *intent* (one bump per signal), not the post-dedup write count.
	EngagementEmitted int `bson:"engagementEmitted,omitempty" json:"engagementEmitted,omitempty"`

	// EngagementOccurredAtFallback counts engagement signals where
	// the row's occurred_at cell was missing or unparseable and the
	// adapter substituted time.Now() at extract time. Useful for
	// operators auditing the import — a high ratio means the source
	// data lost the original event timestamps.
	EngagementOccurredAtFallback int `bson:"engagementOccurredAtFallback,omitempty" json:"engagementOccurredAtFallback,omitempty"`
}

// ImportJob is the persisted audit record of one import run. Sources[]
// entries on Person / Organization rows carry the JobUUID so any row
// can be traced back to the run that produced it.
//
// Phase 3 extends the row with:
//   - IdempotencyKey — sha256(file_bytes || mapping_json) (or the
//     operator-supplied Idempotency-Key header). Same key within 24h
//     per tenant returns the existing job from POST /imports.
//   - ConflictReviewUUIDs — back-reference to every review the worker
//     parked for this job. CountPendingForJob on the review repo is
//     the canonical "is the queue drained" check, but this array lets
//     the timeline UI surface them without an extra query.
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
	// otherwise. Reserved values: "runner_crash" (backend restarted
	// mid-job), "queue_full" (worker queue saturated at enqueue time).
	Error string `bson:"error,omitempty" json:"error,omitempty"`

	// IdempotencyKey is the SHA-256 of (file bytes || mapping JSON) by
	// default, overridable via the Idempotency-Key request header. Two
	// POST /imports calls with the same key inside the 24h window
	// return the existing UUID instead of creating a duplicate job.
	IdempotencyKey string `bson:"idempotencyKey,omitempty" json:"idempotencyKey,omitempty"`

	// ConflictReviewUUIDs lists every review the worker parked for this
	// job. Append-only; never trimmed even after every review resolves
	// (the audit value is in seeing how many manual decisions this run
	// generated).
	ConflictReviewUUIDs []string `bson:"conflictReviewUuids,omitempty" json:"conflictReviewUuids,omitempty"`

	CreatedAt   time.Time  `bson:"createdAt" json:"createdAt"`
	StartedAt   *time.Time `bson:"startedAt,omitempty" json:"startedAt,omitempty"`
	CompletedAt *time.Time `bson:"completedAt,omitempty" json:"completedAt,omitempty"`
	CreatedBy   string     `bson:"createdBy,omitempty" json:"createdBy,omitempty"`
}
