package models

import "time"

// BreakdownEntry explains one activity's contribution to a snapshot's
// Value. The engine emits one entry per activity that matched at
// least one rule (capped by the activityBreakdownMax config — entries
// past the cap collapse into a single synthetic aggregate row with
// ActivityUUID="").
//
// Stored on every snapshot to answer "why does Jane have 95.5 points"
// without replaying the engine. Each entry is small, the per-snapshot
// total is bounded, and the storage cost is trivial relative to the
// observability win.
type BreakdownEntry struct {
	ActivityUUID      string       `bson:"activityUuid" json:"activityUuid"`
	ActivityKind      ActivityKind `bson:"activityKind" json:"activityKind"`
	OccurredAt        time.Time    `bson:"occurredAt" json:"occurredAt"`
	RuleIndex         int          `bson:"ruleIndex" json:"ruleIndex"`
	RawPoints         float64      `bson:"rawPoints" json:"rawPoints"`
	AppliedDecay      float64      `bson:"appliedDecay" json:"appliedDecay"`
	PointsContributed float64      `bson:"pointsContributed" json:"pointsContributed"`
}

// ScoreSnapshot is the cache row for one (Person, ScoreProfile) pair.
// Not a primary source — every value here is reproducible from the
// activity log + profile. The unique
// (tenantId, personUuid, profileUuid) index guarantees that
// concurrent eager + nightly recomputes upsert deterministically onto
// the same row.
//
// Schema mirrors
// docs/plans/marketing-addon/schemas/marketing_score_snapshots.md.
type ScoreSnapshot struct {
	UUID           string  `bson:"uuid" json:"uuid"`
	TenantID       string  `bson:"tenantId" json:"tenantId"`
	PersonUUID     string  `bson:"personUuid" json:"personUuid"`
	ProfileUUID    string  `bson:"profileUuid" json:"profileUuid"`
	ProfileVersion int     `bson:"profileVersion" json:"profileVersion"`
	Value          float64 `bson:"value" json:"value"`

	Breakdown []BreakdownEntry `bson:"breakdown,omitempty" json:"breakdown,omitempty"`

	// AsOf is the cutoff used at compute time — activities with
	// OccurredAt > AsOf were excluded. For eager + nightly the
	// recomputer passes time.Now(); time-travel queries pass a past
	// instant.
	AsOf time.Time `bson:"asOf" json:"asOf"`

	// ComputedAt is when the recomputer ran. Lags AsOf by the
	// duration of the compute itself; almost always close to AsOf
	// for live recomputes.
	ComputedAt time.Time `bson:"computedAt" json:"computedAt"`

	// Applicable is false when the person fails the profile's
	// filter (e.g. excluded by a tag rule). The row is kept so the
	// leaderboard can skip it without an ambiguous missing-row case.
	Applicable bool `bson:"applicable" json:"applicable"`

	// Stale is flipped to true when the profile is saved after this
	// row's ComputedAt. The nightly job + the next eager hit on the
	// person clear it.
	Stale bool `bson:"stale" json:"stale"`

	ActivityCount  int        `bson:"activityCount" json:"activityCount"`
	LastActivityAt *time.Time `bson:"lastActivityAt,omitempty" json:"lastActivityAt,omitempty"`
}

// IsStaleAgainst reports whether the snapshot needs a recompute given
// the current profile state. True when the snapshot was computed
// against an older profile version, even if the Stale flag has not
// yet been flipped by ScoreService.InvalidateProfile (defensive
// against a race between profile save and the staleness bulk update).
func (s *ScoreSnapshot) IsStaleAgainst(profileVersion int) bool {
	if s.Stale {
		return true
	}
	return s.ProfileVersion < profileVersion
}
