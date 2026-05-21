package models

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"
)

// Activity is one row in the append-only marketing_activities event
// log — the single source of truth from which every
// marketing_score_snapshots row is derived. Schema mirrors
// docs/plans/marketing-addon/schemas/marketing_activities.md.
//
// Append-only invariants (decision D22):
//   - No UPDATE / DELETE at the repository surface, with one exception
//     (HardDeleteByPersonUUID for GDPR right-to-be-forgotten).
//   - Corrections happen by inserting a new activity with
//     Kind == KindCorrectedBy and Refs.CorrectsActivityUUID pointing
//     at the row being superseded. The scoring engine de-applies the
//     corrected row when it sees the correction.
//
// The dedup_key invariant (decision D21) makes re-import safe: the
// repository's unique index on dedupKey rejects exact duplicates and
// the service layer treats the rejection as a no-op success.
type Activity struct {
	UUID       string `bson:"uuid" json:"uuid"`
	TenantID   string `bson:"tenantId" json:"tenantId"`
	PersonUUID string `bson:"personUuid" json:"personUuid"`

	// OrgUUID is denormalised at write time from the person's
	// primary+active membership (decision D12). Activities outlive
	// membership changes: when a person switches employer in the
	// future, prior activities keep the historical OrgUUID rather
	// than retroactively re-binding. Empty when the person had no
	// active primary membership at insert.
	OrgUUID string `bson:"orgUuid,omitempty" json:"orgUuid,omitempty"`

	Kind ActivityKind `bson:"kind" json:"kind"`

	// OccurredAt is when the event happened in the real world. Can
	// be in the past (historical engagement imported from a legacy
	// CRM / ESP). Drives every scoring decay calculation.
	OccurredAt time.Time `bson:"occurredAt" json:"occurredAt"`

	// RecordedAt is when the row landed in the database. The two
	// timestamps diverge for historical imports (decision D11) and
	// are equal for live ingestion (webhooks, manual entry).
	RecordedAt time.Time `bson:"recordedAt" json:"recordedAt"`

	Source ActivitySource `bson:"source" json:"source"`

	// Payload is a kind-specific object. Schema variation per kind
	// is documented in marketing_activities.md §payload. The model
	// does not enforce schema — that lives in the handler / service
	// layer at write time.
	Payload map[string]any `bson:"payload,omitempty" json:"payload,omitempty"`

	Refs ActivityRefs `bson:"refs,omitempty" json:"refs,omitempty"`

	// DedupKey is sha256(personUuid|kind|occurredAt.RFC3339|externalId).
	// Unique index in the repository. ComputeDedupKey is the canonical
	// implementation; never assemble the key inline.
	DedupKey string `bson:"dedupKey" json:"dedupKey"`

	// ExternalID is the row identity in the source system (ESP
	// message id, Odoo res.partner.id, ...). Empty for manual
	// activities; the service layer mints a UUID for ExternalID on
	// manual creates so DedupKey remains unique.
	ExternalID string `bson:"externalId,omitempty" json:"externalId,omitempty"`

	// CreatedBy is the user UUID that authored the row when Source
	// is ActivitySourceManual. Empty for importer / system / webhook
	// rows.
	CreatedBy string `bson:"createdBy,omitempty" json:"createdBy,omitempty"`
}

// ActivityRefs collects optional foreign-key pointers that link an
// activity to its broader context (campaign, event, form, import
// run, card, or the activity it corrects). All fields are stored
// as the target's UUID string (not Mongo ObjectID) for stability
// across re-indexings, matching the rest of the marketing models.
type ActivityRefs struct {
	CampaignUUID         string `bson:"campaignUuid,omitempty" json:"campaignUuid,omitempty"`
	EventUUID            string `bson:"eventUuid,omitempty" json:"eventUuid,omitempty"`
	FormUUID             string `bson:"formUuid,omitempty" json:"formUuid,omitempty"`
	ContentUUID          string `bson:"contentUuid,omitempty" json:"contentUuid,omitempty"`
	ImportJobUUID        string `bson:"importJobUuid,omitempty" json:"importJobUuid,omitempty"`
	CardUUID             string `bson:"cardUuid,omitempty" json:"cardUuid,omitempty"`
	CorrectsActivityUUID string `bson:"correctsActivityUuid,omitempty" json:"correctsActivityUuid,omitempty"`
}

// IsZero reports whether refs carries no pointers — used by callers
// that want to omit the field entirely from bson on insert.
func (r ActivityRefs) IsZero() bool {
	return r == ActivityRefs{}
}

// ErrInvalidActivity is returned by Validate when the activity does
// not satisfy the append-only write contract.
var ErrInvalidActivity = errors.New("marketing: invalid activity")

// Validate checks the minimum invariants the service layer enforces
// before insert. PersonUUID, Kind, OccurredAt, and Source must be
// populated; Kind must be one of AllKinds; Source must be one of
// AllSources. The repository depends on these for the unique-index
// derivation and the org-denormalisation flow.
func (a *Activity) Validate() error {
	if a == nil {
		return ErrInvalidActivity
	}
	if a.PersonUUID == "" {
		return errors.Join(ErrInvalidActivity, errors.New("personUuid required"))
	}
	if a.Kind == "" {
		return errors.Join(ErrInvalidActivity, errors.New("kind required"))
	}
	if !IsKnownKind(a.Kind) {
		return errors.Join(ErrInvalidActivity, errors.New("unknown kind"))
	}
	if a.OccurredAt.IsZero() {
		return errors.Join(ErrInvalidActivity, errors.New("occurredAt required"))
	}
	if a.Source == "" {
		return errors.Join(ErrInvalidActivity, errors.New("source required"))
	}
	if !IsKnownSource(a.Source) {
		return errors.Join(ErrInvalidActivity, errors.New("unknown source"))
	}
	return nil
}

// ComputeDedupKey produces the canonical dedup_key for an activity.
// The format is sha256 over the pipe-joined tuple
//
//	personUuid | kind | occurredAt.UTC().Format(time.RFC3339) | externalId
//
// with empty externalId folded to the empty string. The unique
// repository index on dedupKey makes re-imports idempotent: a second
// import that hashes the same tuple is rejected with E11000, which
// the service layer maps to a no-op success.
//
// occurredAt is rounded to the second via the RFC3339 formatter —
// sub-second precision would defeat dedup across importers that
// truncate timestamps. Callers must pass the value they intend to
// persist (the repository does not normalise occurredAt before
// hashing).
func ComputeDedupKey(personUUID string, kind ActivityKind, occurredAt time.Time, externalID string) string {
	h := sha256.New()
	h.Write([]byte(personUUID))
	h.Write([]byte("|"))
	h.Write([]byte(kind))
	h.Write([]byte("|"))
	h.Write([]byte(occurredAt.UTC().Format(time.RFC3339)))
	h.Write([]byte("|"))
	h.Write([]byte(externalID))
	return hex.EncodeToString(h.Sum(nil))
}
