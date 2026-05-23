// Package models defines the marketing addon's MongoDB document types and
// the canonical collection-name constants used by both repositories and
// the module's Collections() index declarations. Every collection owned
// by this module is prefixed `marketing_` (enforced repo-wide by the
// mongo-collection-naming skill).
package models

// Collection-name constants. The marketing addon owns these collections;
// any code reading or writing one must reference these constants — never
// hardcode the string. Adding a new collection here is the first step
// when introducing one in a later phase (Phase 3 brings
// ConflictReviewsCollection and the full ImportJobsCollection;
// Phase 4 brings CardTypesCollection and CardsCollection — schemas
// already live in docs/plans/marketing-addon/schemas/).
const (
	// Phase 1 — Fondazione anagrafica MVP.
	OrganizationsCollection      = "marketing_organizations"
	PersonsCollection            = "marketing_persons"
	MembershipsCollection        = "marketing_memberships"
	TagsCollection               = "marketing_tags"
	CustomFieldSchemasCollection = "marketing_custom_field_schemas"
	ImportJobsCollection         = "marketing_import_jobs"

	// Phase 2 — Storicizzazione & scoring.
	ActivitiesCollection     = "marketing_activities"
	ScoreProfilesCollection  = "marketing_score_profiles"
	ScoreSnapshotsCollection = "marketing_score_snapshots"

	// Phase 3 — Import avanzati.
	ConflictReviewsCollection = "marketing_conflict_reviews"

	// Phase 4 — Card lifecycle. CardTypesCollection holds per-tenant
	// card templates; CardsCollection holds the concrete instances
	// emitted to a Person. CardSequencesCollection is an internal
	// helper that backs the {seq:N} placeholder in code_format — one
	// counter document per (tenantId, cardTypeUuid), updated atomically
	// via findAndModify. The sequences collection is intentionally
	// rebuildable from MAX(card.code) if a recovery is ever needed and
	// is not part of the public schema set in
	// docs/plans/marketing-addon/schemas/.
	CardTypesCollection     = "marketing_card_types"
	CardsCollection         = "marketing_cards"
	CardSequencesCollection = "marketing_card_sequences"
)
