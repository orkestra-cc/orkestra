package models

import (
	"errors"
	"time"
)

// ConflictTargetKind identifies which contact collection a review row
// is attached to. Two values today; cards may join in Phase 4 if the
// card-issue path ever needs an operator-resolved merge surface.
type ConflictTargetKind string

const (
	ConflictTargetPerson       ConflictTargetKind = "person"
	ConflictTargetOrganization ConflictTargetKind = "organization"
)

// IsKnownConflictTargetKind validates ConflictReview.TargetKind.
func IsKnownConflictTargetKind(k ConflictTargetKind) bool {
	switch k {
	case ConflictTargetPerson, ConflictTargetOrganization:
		return true
	}
	return false
}

// ConflictReviewStatus is the lifecycle state of a review row.
//
//	pending   — created by the importer pipeline; awaiting operator action.
//	resolved  — operator picked one of the four ConflictAction values; the
//	            commit pass has applied the chosen merge.
//	dismissed — operator decided the incoming row was a false-positive
//	            match; the incoming payload + activities were discarded.
type ConflictReviewStatus string

const (
	ConflictReviewStatusPending   ConflictReviewStatus = "pending"
	ConflictReviewStatusResolved  ConflictReviewStatus = "resolved"
	ConflictReviewStatusDismissed ConflictReviewStatus = "dismissed"
)

// IsKnownConflictReviewStatus validates ConflictReview.Status.
func IsKnownConflictReviewStatus(s ConflictReviewStatus) bool {
	switch s {
	case ConflictReviewStatusPending, ConflictReviewStatusResolved, ConflictReviewStatusDismissed:
		return true
	}
	return false
}

// ConflictAction is the resolution the operator picked at close time.
//
//	keep_existing — discard incoming conflicting fields; existing record unchanged on
//	                those fields. Incoming activities still commit (engagement is real).
//	take_incoming — overwrite existing with incoming on the conflicting fields.
//	manual_merge  — apply FieldOverrides per-key. Scalars get the literal value; for
//	                additive-array fields the operator can pass "$append" to union-merge
//	                or an explicit array to replace.
//	dismiss       — drop the incoming row entirely (Status flips to dismissed instead of
//	                resolved — included here so the action enum is closed).
type ConflictAction string

const (
	ConflictActionKeepExisting ConflictAction = "keep_existing"
	ConflictActionTakeIncoming ConflictAction = "take_incoming"
	ConflictActionManualMerge  ConflictAction = "manual_merge"
	ConflictActionDismiss      ConflictAction = "dismiss"
)

// IsKnownConflictAction validates ConflictResolution.Action.
func IsKnownConflictAction(a ConflictAction) bool {
	switch a {
	case ConflictActionKeepExisting, ConflictActionTakeIncoming, ConflictActionManualMerge, ConflictActionDismiss:
		return true
	}
	return false
}

// ConflictSeverity tags each ConflictField as either dedup-key sensitive
// (blocking — the importer pipeline parks the row pending resolution) or
// a soft-match hit (the strict dedup found nothing but the soft-match
// helper, design §5.5, flagged it as similar enough to need a human eye).
type ConflictSeverity string

const (
	ConflictSeverityBlocking ConflictSeverity = "blocking"
	ConflictSeveritySoft     ConflictSeverity = "soft"
)

// ConflictField records one disagreement between the existing record
// and the incoming payload. The pipeline emits one entry per protected
// field that differs; the resolver UI renders them side-by-side.
type ConflictField struct {
	Field         string           `bson:"field" json:"field"`
	ExistingValue any              `bson:"existingValue,omitempty" json:"existingValue,omitempty"`
	IncomingValue any              `bson:"incomingValue,omitempty" json:"incomingValue,omitempty"`
	Severity      ConflictSeverity `bson:"severity" json:"severity"`
}

// ConflictResolution carries the operator's choice. FieldOverrides is
// only populated for the manual_merge action.
type ConflictResolution struct {
	Action         ConflictAction `bson:"action" json:"action"`
	FieldOverrides map[string]any `bson:"fieldOverrides,omitempty" json:"fieldOverrides,omitempty"`
}

// ConflictReview is the persisted review row. The importer pipeline
// writes one per blocked record + parks the parent ImportJob in
// paused_for_review until every pending review for that job has been
// resolved or dismissed. See schemas/marketing_conflict_reviews.md.
type ConflictReview struct {
	UUID     string `bson:"uuid" json:"uuid"`
	TenantID string `bson:"tenantId" json:"-"`

	ImportJobUUID string `bson:"importJobUuid" json:"importJobUuid"`

	TargetKind   ConflictTargetKind `bson:"targetKind" json:"targetKind"`
	ExistingUUID string             `bson:"existingUuid" json:"existingUuid"`

	// ExistingSnapshot is the immutable copy of the existing record at
	// the moment the conflict was detected. It is for debug/audit only;
	// Resolve applies the merge against the CURRENT existing record, not
	// against this snapshot (the schema doc spells this out — race
	// conditions silently corrupting the merge are worse than the
	// occasional "snapshot is stale" debugging moment).
	ExistingSnapshot map[string]any `bson:"existingSnapshot,omitempty" json:"existingSnapshot,omitempty"`

	// IncomingPayload is the parsed-and-normalized record the importer
	// wanted to insert/merge.
	IncomingPayload map[string]any `bson:"incomingPayload" json:"incomingPayload"`

	// IncomingActivities are activities that the importer produced for
	// this row but parked alongside the review. They commit when the
	// operator resolves the review (keep_existing / take_incoming /
	// manual_merge) or get dropped if the operator dismisses.
	IncomingActivities []map[string]any `bson:"incomingActivities,omitempty" json:"incomingActivities,omitempty"`

	Conflicts []ConflictField `bson:"conflicts" json:"conflicts"`

	Status     ConflictReviewStatus `bson:"status" json:"status"`
	Resolution *ConflictResolution  `bson:"resolution,omitempty" json:"resolution,omitempty"`

	ResolvedAt    *time.Time `bson:"resolvedAt,omitempty" json:"resolvedAt,omitempty"`
	ResolvedBy    string     `bson:"resolvedBy,omitempty" json:"resolvedBy,omitempty"`
	ResolvedNotes string     `bson:"resolvedNotes,omitempty" json:"resolvedNotes,omitempty"`

	CreatedAt time.Time `bson:"createdAt" json:"createdAt"`
	UpdatedAt time.Time `bson:"updatedAt" json:"updatedAt"`
}

// ErrInvalidConflictReview signals a malformed review payload — emitted
// by Validate when required fields are missing, enums are off-list, or
// a manual_merge resolution arrives without FieldOverrides.
var ErrInvalidConflictReview = errors.New("marketing: invalid conflict review")

// Validate enforces the model invariants at the boundary. Repositories
// call this before Create; the resolve handler calls it on the
// Resolution payload separately.
func (c *ConflictReview) Validate() error {
	if c == nil {
		return ErrInvalidConflictReview
	}
	if c.ImportJobUUID == "" {
		return errors.Join(ErrInvalidConflictReview, errors.New("missing importJobUuid"))
	}
	if !IsKnownConflictTargetKind(c.TargetKind) {
		return errors.Join(ErrInvalidConflictReview, errors.New("invalid targetKind"))
	}
	if c.ExistingUUID == "" {
		return errors.Join(ErrInvalidConflictReview, errors.New("missing existingUuid"))
	}
	if len(c.IncomingPayload) == 0 {
		return errors.Join(ErrInvalidConflictReview, errors.New("missing incomingPayload"))
	}
	if len(c.Conflicts) == 0 {
		return errors.Join(ErrInvalidConflictReview, errors.New("empty conflicts list"))
	}
	for i, cf := range c.Conflicts {
		if cf.Field == "" {
			return errors.Join(ErrInvalidConflictReview, errors.New("empty field name in conflicts"))
		}
		if cf.Severity != ConflictSeverityBlocking && cf.Severity != ConflictSeveritySoft {
			return errors.Join(ErrInvalidConflictReview, errors.New("invalid severity in conflicts"))
		}
		_ = i
	}
	if !IsKnownConflictReviewStatus(c.Status) {
		return errors.Join(ErrInvalidConflictReview, errors.New("invalid status"))
	}
	if c.Resolution != nil {
		if err := c.Resolution.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// Validate checks ConflictResolution's payload. Used by Resolve handler
// to reject malformed bodies before they reach the apply path.
func (r *ConflictResolution) Validate() error {
	if r == nil {
		return errors.Join(ErrInvalidConflictReview, errors.New("nil resolution"))
	}
	if !IsKnownConflictAction(r.Action) {
		return errors.Join(ErrInvalidConflictReview, errors.New("invalid action"))
	}
	if r.Action == ConflictActionManualMerge && len(r.FieldOverrides) == 0 {
		return errors.Join(ErrInvalidConflictReview, errors.New("manual_merge requires fieldOverrides"))
	}
	if r.Action != ConflictActionManualMerge && len(r.FieldOverrides) > 0 {
		return errors.Join(ErrInvalidConflictReview, errors.New("fieldOverrides only valid for manual_merge"))
	}
	return nil
}
