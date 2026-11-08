// Package models holds the persisted shapes of the jobs core module:
// one durable state document per job key and one record per run.
package models

import "time"

// Collection names (fixed; see Collections() in the module).
const (
	StatesCollection = "jobs_states"
	RunsCollection   = "jobs_runs"
)

// Run statuses.
const (
	StatusRunning     = "running"
	StatusSucceeded   = "succeeded"
	StatusFailed      = "failed"
	StatusTimedOut    = "timed_out"
	StatusAbandoned   = "abandoned"
	StatusInterrupted = "interrupted"
	StatusSkipped     = "skipped"
)

// ScheduleSpec is the persisted form of a schedule.
type ScheduleSpec struct {
	Kind         string `bson:"kind" json:"kind"`
	EverySeconds int64  `bson:"everySeconds,omitempty" json:"everySeconds,omitempty"`
	Cron         string `bson:"cron,omitempty" json:"cron,omitempty"`
}

// Override is an operator-set deviation from the declared definition.
type Override struct {
	Enabled  *bool         `bson:"enabled,omitempty" json:"enabled,omitempty"`
	Schedule *ScheduleSpec `bson:"schedule,omitempty" json:"schedule,omitempty"`
	Timezone string        `bson:"timezone,omitempty" json:"timezone,omitempty"`
}

// Lease marks a job as running on a replica until Until. Trigger, Attempt,
// SlotAt and StartedAt describe the run that holds it, so a replica taking
// over an expired lease can account for the abandoned run.
type Lease struct {
	Owner     string     `bson:"owner" json:"owner"`
	RunID     string     `bson:"runId" json:"runId"`
	Trigger   string     `bson:"trigger" json:"trigger"`
	Until     time.Time  `bson:"until" json:"until"`
	Attempt   int        `bson:"attempt" json:"attempt"`
	SlotAt    *time.Time `bson:"slotAt,omitempty" json:"slotAt,omitempty"`
	StartedAt time.Time  `bson:"startedAt" json:"startedAt"`
}

// LastRun summarises the most recent finished run.
type LastRun struct {
	RunID      string    `bson:"runId" json:"runId"`
	Status     string    `bson:"status" json:"status"`
	Error      string    `bson:"error,omitempty" json:"error,omitempty"`
	StartedAt  time.Time `bson:"startedAt" json:"startedAt"`
	FinishedAt time.Time `bson:"finishedAt" json:"finishedAt"`
	DurationMs int64     `bson:"durationMs" json:"durationMs"`
}

// State is the durable per-job document, keyed by job key.
type State struct {
	Key            string     `bson:"_id" json:"key"`
	Module         string     `bson:"module" json:"module"`
	Override       *Override  `bson:"override,omitempty" json:"override,omitempty"`
	Fingerprint    string     `bson:"fingerprint,omitempty" json:"fingerprint,omitempty"`
	NextRunAt      *time.Time `bson:"nextRunAt,omitempty" json:"nextRunAt,omitempty"`
	RetryAt        *time.Time `bson:"retryAt,omitempty" json:"retryAt,omitempty"`
	RetrySlotAt    *time.Time `bson:"retrySlotAt,omitempty" json:"retrySlotAt,omitempty"`
	Attempt        int        `bson:"attempt" json:"attempt"`
	RunRequestedAt *time.Time `bson:"runRequestedAt,omitempty" json:"runRequestedAt,omitempty"`
	RunRequestedBy string     `bson:"runRequestedBy,omitempty" json:"runRequestedBy,omitempty"`
	Lease          *Lease     `bson:"lease,omitempty" json:"lease,omitempty"`
	LastRun        *LastRun   `bson:"lastRun,omitempty" json:"lastRun,omitempty"`
	Revision       int64      `bson:"revision" json:"revision"`
	UpdatedAt      time.Time  `bson:"updatedAt" json:"updatedAt"`
	UpdatedBy      string     `bson:"updatedBy,omitempty" json:"updatedBy,omitempty"`
}

// RunRecord is one execution attempt of a job.
type RunRecord struct {
	ID             string         `bson:"_id" json:"id"`
	Key            string         `bson:"key" json:"key"`
	Module         string         `bson:"module" json:"module"`
	Trigger        string         `bson:"trigger" json:"trigger"`
	Status         string         `bson:"status" json:"status"`
	Error          string         `bson:"error,omitempty" json:"error,omitempty"`
	Replica        string         `bson:"replica,omitempty" json:"replica,omitempty"`
	RequestedBy    string         `bson:"requestedBy,omitempty" json:"requestedBy,omitempty"`
	Attempt        int            `bson:"attempt" json:"attempt"`
	ScheduledFor   *time.Time     `bson:"scheduledFor,omitempty" json:"scheduledFor,omitempty"`
	StartedAt      time.Time      `bson:"startedAt" json:"startedAt"`
	FinishedAt     *time.Time     `bson:"finishedAt,omitempty" json:"finishedAt,omitempty"`
	DurationMs     int64          `bson:"durationMs" json:"durationMs"`
	Details        map[string]any `bson:"details,omitempty" json:"details,omitempty"`
	DroppedDetails int            `bson:"droppedDetails,omitempty" json:"droppedDetails,omitempty"`
}
