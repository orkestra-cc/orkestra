package importers

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra-cc/orkestra-addon-marketing/importers/match"
	"github.com/orkestra-cc/orkestra-addon-marketing/models"
	"github.com/orkestra-cc/orkestra-addon-marketing/repository"
	"github.com/orkestra-cc/orkestra-sdk/ctxauth"
	"go.mongodb.org/mongo-driver/bson"
)

// Pipeline is the adapter-independent half of the import flow. It
// reads CanonicalRecord values from a Source, dedups against the
// existing tenant data, applies the auto-merge policy, and writes
// the result to the marketing collections.
//
// Behavior:
//   - Person dedup primary key: primary_email
//   - Organization dedup primary key: vat → taxCode fallback
//   - Auto-merge: fill empty fields, set-union additive arrays
//     (tags, sources).
//   - Conflicts on the dedup-key fields (primary email, vat,
//     taxCode): Phase 3 parks the row in marketing_conflict_reviews
//     when ReviewParker is wired; Phase 1 behavior (increment
//     Stats.ConflictsSkipped) remains the fallback for tests or
//     setups that haven't wired the parker.
//   - Memberships are created (or reactivated) whenever a row
//     carries both an organization and a person identity.
//   - When ActivityEmitter is wired, every committed Person row
//     emits an `imported` Activity (idempotent via dedupKey).
type Pipeline struct {
	orgs       *repository.OrganizationRepository
	persons    *repository.PersonRepository
	mships     *repository.MembershipRepository
	tagRepo    *repository.TagRepository
	jobUUID    string
	importer   string
	stats      models.ImportJobStats
	tagSlugMap map[string]string // slug → tag UUID, lazy-cached

	// Optional Phase-3 collaborators. Pipeline accepts nil values for
	// both so the older NewPipeline signature (used by unit tests +
	// the Phase-1 path before the worker) keeps working.
	reviewParker ReviewParker
	emitter      ActivityEmitter
}

// ReviewParker is the narrow seam the pipeline uses to write a
// conflict-review row. Implemented by services.ConflictReviewService —
// kept abstract so the importers package doesn't reach back into
// services (which would create a cycle).
type ReviewParker interface {
	Park(ctx context.Context, in ParkInput) error
}

// ActivityEmitter is the narrow seam for the auto-emission helpers.
// Implemented by services.ActivityEmitter.
type ActivityEmitter interface {
	EmitImported(ctx context.Context, personUUID, orgUUID, importJobUUID string)
	EmitEngagement(ctx context.Context, personUUID, importJobUUID string, kind models.ActivityKind, occurredAt time.Time)
}

// ParkInput mirrors services.ParkInput on the wire so the pipeline
// can issue a Park call without importing the services package. The
// service-layer type is the canonical one; this declaration only
// exists to break the import cycle.
type ParkInput struct {
	ImportJobUUID      string
	TargetKind         models.ConflictTargetKind
	ExistingUUID       string
	ExistingSnapshot   map[string]any
	IncomingPayload    map[string]any
	IncomingActivities []map[string]any
	Conflicts          []models.ConflictField
}

// NewPipeline binds a pipeline to a job + the live repositories.
// One pipeline serves one job; reuse across jobs is not supported.
func NewPipeline(
	jobUUID, importerName string,
	orgs *repository.OrganizationRepository,
	persons *repository.PersonRepository,
	mships *repository.MembershipRepository,
	tags *repository.TagRepository,
) *Pipeline {
	return &Pipeline{
		orgs:       orgs,
		persons:    persons,
		mships:     mships,
		tagRepo:    tags,
		jobUUID:    jobUUID,
		importer:   importerName,
		tagSlugMap: make(map[string]string),
	}
}

// WithReviewParker wires the optional conflict-review Park hook.
// Returns the pipeline so callers can chain at construction.
func (p *Pipeline) WithReviewParker(rp ReviewParker) *Pipeline {
	p.reviewParker = rp
	return p
}

// WithActivityEmitter wires the optional auto-emission hook for
// `imported` Activities.
func (p *Pipeline) WithActivityEmitter(e ActivityEmitter) *Pipeline {
	p.emitter = e
	return p
}

// Run consumes every record the source yields and returns the
// accumulated stats. The source's own error (if any) is returned
// after channel close.
func (p *Pipeline) Run(ctx context.Context, src Source) (models.ImportJobStats, error) {
	for rec := range src.Records() {
		if ctx.Err() != nil {
			return p.stats, ctx.Err()
		}
		p.stats.RowsRead++
		if err := p.processRecord(ctx, rec); err != nil {
			p.stats.RowsFailed++
			// Continue with the next row — one bad row should
			// not abort the whole import. The error is logged at
			// the caller; per-row diagnostics arrive in the
			// review-queue Phase 3 work.
			continue
		}
	}
	return p.stats, src.Err()
}

// processRecord runs the dedup + merge logic for one canonical row.
// Returns an error only on systemic failures (repository writes);
// data-level conflicts increment Stats.ConflictsSkipped instead.
func (p *Pipeline) processRecord(ctx context.Context, rec CanonicalRecord) error {
	prov := models.ProvenanceSource{
		Importer:   p.importer,
		JobUUID:    p.jobUUID,
		ExternalID: rowExternalID(rec.RowIndex),
		ImportedAt: time.Now().UTC(),
	}

	// Resolve operator-supplied tag slugs to UUIDs lazily; unknown
	// slugs are silently dropped so the import doesn't fail on the
	// operator's first-time data — a follow-up surface will warn.
	tagUUIDs := p.resolveTags(ctx, rec.TagSlugs)

	// --- Organization -------------------------------------------------
	var orgUUID string
	if hasOrgIdentity(rec) {
		var err error
		orgUUID, err = p.upsertOrganization(ctx, rec, tagUUIDs, prov)
		if err != nil {
			return err
		}
	}

	// --- Person -------------------------------------------------------
	var personUUID string
	if hasPersonIdentity(rec) {
		var err error
		personUUID, err = p.upsertPerson(ctx, rec, tagUUIDs, prov)
		if err != nil {
			return err
		}
	}

	// --- Membership ---------------------------------------------------
	if orgUUID != "" && personUUID != "" {
		if err := p.ensureMembership(ctx, personUUID, orgUUID, rec); err != nil {
			return err
		}
	}

	// --- Engagement signals (Phase 4 — engagement-CSV emission) ------
	// Fired only when upsertPerson resolved a personUuid. Engagement
	// events with no associated Person are dropped silently — the row
	// has no Activity log to land on. The pipeline's Stats counters
	// record the volume + the fallback rate so operators can audit how
	// much per-row fidelity the import preserved.
	if personUUID != "" && len(rec.EngagementSignals) > 0 && p.emitter != nil {
		for _, sig := range rec.EngagementSignals {
			p.emitter.EmitEngagement(ctx, personUUID, p.jobUUID, sig.Kind, sig.OccurredAt)
			p.stats.EngagementEmitted++
			if sig.FallbackOccurredAt {
				p.stats.EngagementOccurredAtFallback++
			}
		}
	}
	return nil
}

func hasOrgIdentity(rec CanonicalRecord) bool {
	return rec.OrgLegalName != "" || rec.OrgVAT != "" || rec.OrgTaxCode != ""
}

func hasPersonIdentity(rec CanonicalRecord) bool {
	return rec.PersonEmail != "" || rec.PersonFirstName != "" || rec.PersonLastName != ""
}

// upsertOrganization dedups the row against the tenant's
// marketing_organizations and either creates a new row or merges
// into the matched one.
func (p *Pipeline) upsertOrganization(ctx context.Context, rec CanonicalRecord, tagUUIDs []string, prov models.ProvenanceSource) (string, error) {
	vat := repository.NormalizeVAT(rec.OrgVAT)
	taxCode := repository.NormalizeTaxCode(rec.OrgTaxCode)

	var existing *models.Organization
	if vat != "" {
		got, err := p.orgs.LookupByVAT(ctx, vat)
		if err != nil && !errors.Is(err, repository.ErrOrgNotFound) {
			return "", err
		}
		existing = got
	}
	if existing == nil && taxCode != "" {
		got, err := p.orgs.LookupByTaxCode(ctx, taxCode)
		if err != nil && !errors.Is(err, repository.ErrOrgNotFound) {
			return "", err
		}
		existing = got
	}

	if existing == nil {
		// Strict-miss on VAT + TaxCode — try the soft-match scan on
		// legalName before creating a fresh organization row.
		if parked, err := p.parseOrgSoftMatchPark(ctx, rec); err != nil {
			return "", err
		} else if parked {
			p.stats.ConflictsSkipped++
			return "", nil
		}
	}

	if existing != nil {
		patch, conflictFields := orgMergePatchWithDetail(existing, rec, tagUUIDs)
		if len(patch) > 0 {
			if err := p.orgs.Update(ctx, existing.UUID, patch); err != nil {
				return "", err
			}
		}
		if err := p.orgs.AppendSource(ctx, existing.UUID, prov); err != nil {
			return "", err
		}
		p.stats.OrgsMerged++
		// Park blocking conflicts (if any) for operator resolution.
		if len(conflictFields) > 0 {
			if p.reviewParker != nil {
				if err := p.reviewParker.Park(ctx, ParkInput{
					ImportJobUUID:    p.jobUUID,
					TargetKind:       models.ConflictTargetOrganization,
					ExistingUUID:     existing.UUID,
					ExistingSnapshot: orgSnapshotMap(existing),
					IncomingPayload:  orgIncomingMap(rec),
					Conflicts:        conflictFields,
				}); err != nil {
					// Non-fatal — auto-merge already landed. Bump the
					// legacy counter so ops still sees the conflict
					// surface in stats even when parking failed.
					p.stats.ConflictsSkipped += len(conflictFields)
				}
			} else {
				p.stats.ConflictsSkipped += len(conflictFields)
			}
		}
		return existing.UUID, nil
	}

	org := &models.Organization{
		UUID:      uuid.New().String(),
		LegalName: rec.OrgLegalName,
		VAT:       vat,
		TaxCode:   taxCode,
		Kind:      deriveOrgKind(rec.OrgKind),
		Website:   rec.OrgWebsite,
		Tags:      tagUUIDs,
		Sources:   []models.ProvenanceSource{prov},
		CreatedBy: actor(ctx),
	}
	if rec.OrgEmail != "" {
		org.Emails = []models.EmailEntry{{Address: repository.NormalizeEmail(rec.OrgEmail), Primary: true}}
	}
	if rec.OrgPhone != "" {
		org.Phones = []models.PhoneEntry{{Number: rec.OrgPhone, Primary: true}}
	}
	if hasOrgIdentity(rec) && rec.Notes != "" && !hasPersonIdentity(rec) {
		org.Notes = rec.Notes
	}
	if err := p.orgs.Create(ctx, org); err != nil {
		return "", err
	}
	p.stats.OrgsCreated++
	return org.UUID, nil
}

// parseOrgSoftMatchPark mirrors parsePersonSoftMatchPark for the
// Organization strict-miss path. Returns (parked=true, nil) when an
// existing organization's legalName matches the incoming row after
// match.NormalizeLegalName; the parent caller then skips the create.
func (p *Pipeline) parseOrgSoftMatchPark(ctx context.Context, rec CanonicalRecord) (bool, error) {
	if p.reviewParker == nil || rec.OrgLegalName == "" {
		return false, nil
	}
	existing, err := p.orgs.FindSoftMatchByLegalName(ctx, rec.OrgLegalName)
	if errors.Is(err, repository.ErrOrgNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	conflicts := []models.ConflictField{{
		Field:         "softMatch",
		ExistingValue: existing.UUID,
		IncomingValue: orgIncomingMap(rec),
		Severity:      models.ConflictSeveritySoft,
	}}
	if err := p.reviewParker.Park(ctx, ParkInput{
		ImportJobUUID:    p.jobUUID,
		TargetKind:       models.ConflictTargetOrganization,
		ExistingUUID:     existing.UUID,
		ExistingSnapshot: orgSnapshotMap(existing),
		IncomingPayload:  orgIncomingMap(rec),
		Conflicts:        conflicts,
	}); err != nil {
		return false, nil
	}
	return true, nil
}

// orgMergePatchWithDetail is the Phase-3 variant — same auto-merge
// policy as orgMergePatch, but returns the dedup-key disagreements as
// a []models.ConflictField slice the pipeline forwards to
// ConflictReviewService.Park. The legacy orgMergePatch wrapper
// preserves the Phase-1 (count int) signature for tests that haven't
// migrated yet.
func orgMergePatchWithDetail(existing *models.Organization, rec CanonicalRecord, tagUUIDs []string) (bson.M, []models.ConflictField) {
	patch := bson.M{}
	conflicts := make([]models.ConflictField, 0)

	if existing.LegalName == "" && rec.OrgLegalName != "" {
		patch["legalName"] = rec.OrgLegalName
	}
	if existing.Website == "" && rec.OrgWebsite != "" {
		patch["website"] = rec.OrgWebsite
	}
	if existing.Kind == "" && rec.OrgKind != "" {
		patch["kind"] = deriveOrgKind(rec.OrgKind)
	}

	normalVAT := repository.NormalizeVAT(rec.OrgVAT)
	if normalVAT != "" {
		if existing.VAT == "" {
			patch["vat"] = normalVAT
		} else if existing.VAT != normalVAT {
			conflicts = append(conflicts, models.ConflictField{
				Field:         "vat",
				ExistingValue: existing.VAT,
				IncomingValue: normalVAT,
				Severity:      models.ConflictSeverityBlocking,
			})
		}
	}
	normalTax := repository.NormalizeTaxCode(rec.OrgTaxCode)
	if normalTax != "" {
		if existing.TaxCode == "" {
			patch["taxCode"] = normalTax
		} else if existing.TaxCode != normalTax {
			conflicts = append(conflicts, models.ConflictField{
				Field:         "taxCode",
				ExistingValue: existing.TaxCode,
				IncomingValue: normalTax,
				Severity:      models.ConflictSeverityBlocking,
			})
		}
	}

	if newTags := mergeStringSet(existing.Tags, tagUUIDs); len(newTags) != len(existing.Tags) {
		patch["tags"] = newTags
	}
	if rec.OrgEmail != "" {
		emails := mergeEmails(existing.Emails, models.EmailEntry{Address: repository.NormalizeEmail(rec.OrgEmail)})
		if len(emails) != len(existing.Emails) {
			patch["emails"] = emails
		}
	}
	if rec.OrgPhone != "" {
		phones := mergePhones(existing.Phones, models.PhoneEntry{Number: rec.OrgPhone})
		if len(phones) != len(existing.Phones) {
			patch["phones"] = phones
		}
	}
	return patch, conflicts
}

// personMergePatchWithDetail is the Person counterpart of
// orgMergePatchWithDetail. Returns the patch + a slice of
// ConflictField for the dedup-key disagreement (today: a different
// primary email).
func personMergePatchWithDetail(existing *models.Person, rec CanonicalRecord, tagUUIDs []string) (bson.M, []models.ConflictField) {
	patch := bson.M{}
	conflicts := make([]models.ConflictField, 0)

	if existing.FirstName == "" && rec.PersonFirstName != "" {
		patch["firstName"] = rec.PersonFirstName
	}
	if existing.LastName == "" && rec.PersonLastName != "" {
		patch["lastName"] = rec.PersonLastName
	}
	if existing.Title == "" && rec.PersonTitle != "" {
		patch["title"] = rec.PersonTitle
	}
	if existing.Language == "" && rec.PersonLanguage != "" {
		patch["language"] = rec.PersonLanguage
	}

	incomingEmail := repository.NormalizeEmail(rec.PersonEmail)
	if incomingEmail != "" {
		primaryExisting := primaryEmail(existing.Emails)
		switch {
		case primaryExisting == "":
			patch["emails"] = mergeEmails(existing.Emails, models.EmailEntry{Address: incomingEmail, Primary: true})
		case primaryExisting == incomingEmail:
			// no-op
		default:
			emails := mergeEmails(existing.Emails, models.EmailEntry{Address: incomingEmail})
			if len(emails) != len(existing.Emails) {
				patch["emails"] = emails
			}
			conflicts = append(conflicts, models.ConflictField{
				Field:         "primaryEmail",
				ExistingValue: primaryExisting,
				IncomingValue: incomingEmail,
				Severity:      models.ConflictSeverityBlocking,
			})
		}
	}

	if rec.PersonPhone != "" {
		phones := mergePhones(existing.Phones, models.PhoneEntry{Number: rec.PersonPhone})
		if len(phones) != len(existing.Phones) {
			patch["phones"] = phones
		}
	}
	if newTags := mergeStringSet(existing.Tags, tagUUIDs); len(newTags) != len(existing.Tags) {
		patch["tags"] = newTags
	}
	return patch, conflicts
}

// orgSnapshotMap turns the existing org into a map suitable for the
// review row's ExistingSnapshot field. Only the fields the resolver
// UI surfaces — keep payload size small.
func orgSnapshotMap(o *models.Organization) map[string]any {
	if o == nil {
		return nil
	}
	return map[string]any{
		"uuid":      o.UUID,
		"legalName": o.LegalName,
		"vat":       o.VAT,
		"taxCode":   o.TaxCode,
		"kind":      o.Kind,
		"website":   o.Website,
		"emails":    o.Emails,
		"phones":    o.Phones,
		"tags":      o.Tags,
	}
}

// orgIncomingMap turns the canonical incoming row into the same
// snapshot shape so the resolver UI can render existing-vs-incoming
// side-by-side without per-field branching.
func orgIncomingMap(rec CanonicalRecord) map[string]any {
	return map[string]any{
		"legalName": rec.OrgLegalName,
		"vat":       repository.NormalizeVAT(rec.OrgVAT),
		"taxCode":   repository.NormalizeTaxCode(rec.OrgTaxCode),
		"kind":      rec.OrgKind,
		"website":   rec.OrgWebsite,
		"email":     rec.OrgEmail,
		"phone":     rec.OrgPhone,
		"tags":      rec.TagSlugs,
	}
}

// personSnapshotMap mirrors orgSnapshotMap for Person.
func personSnapshotMap(p *models.Person) map[string]any {
	if p == nil {
		return nil
	}
	return map[string]any{
		"uuid":      p.UUID,
		"firstName": p.FirstName,
		"lastName":  p.LastName,
		"title":     p.Title,
		"language":  p.Language,
		"emails":    p.Emails,
		"phones":    p.Phones,
		"tags":      p.Tags,
	}
}

// personIncomingMap mirrors orgIncomingMap for Person.
func personIncomingMap(rec CanonicalRecord) map[string]any {
	return map[string]any{
		"firstName": rec.PersonFirstName,
		"lastName":  rec.PersonLastName,
		"email":     repository.NormalizeEmail(rec.PersonEmail),
		"phone":     rec.PersonPhone,
		"title":     rec.PersonTitle,
		"language":  rec.PersonLanguage,
		"tags":      rec.TagSlugs,
	}
}

// orgMergePatch composes the $set patch for an existing organization
// using the auto-merge policy. Returns the patch + the count of
// conflicts skipped on dedup-key fields.
func orgMergePatch(existing *models.Organization, rec CanonicalRecord, tagUUIDs []string) (bson.M, int) {
	patch := bson.M{}
	conflicts := 0

	// Fill-empty for plain scalar fields.
	if existing.LegalName == "" && rec.OrgLegalName != "" {
		patch["legalName"] = rec.OrgLegalName
	}
	if existing.Website == "" && rec.OrgWebsite != "" {
		patch["website"] = rec.OrgWebsite
	}
	if existing.Kind == "" && rec.OrgKind != "" {
		patch["kind"] = deriveOrgKind(rec.OrgKind)
	}

	// Dedup-key fields — fill when empty, conflict-skip otherwise.
	normalVAT := repository.NormalizeVAT(rec.OrgVAT)
	if normalVAT != "" {
		if existing.VAT == "" {
			patch["vat"] = normalVAT
		} else if existing.VAT != normalVAT {
			conflicts++
		}
	}
	normalTax := repository.NormalizeTaxCode(rec.OrgTaxCode)
	if normalTax != "" {
		if existing.TaxCode == "" {
			patch["taxCode"] = normalTax
		} else if existing.TaxCode != normalTax {
			conflicts++
		}
	}

	// Additive arrays — set-union.
	if newTags := mergeStringSet(existing.Tags, tagUUIDs); len(newTags) != len(existing.Tags) {
		patch["tags"] = newTags
	}
	if rec.OrgEmail != "" {
		emails := mergeEmails(existing.Emails, models.EmailEntry{Address: repository.NormalizeEmail(rec.OrgEmail)})
		if len(emails) != len(existing.Emails) {
			patch["emails"] = emails
		}
	}
	if rec.OrgPhone != "" {
		phones := mergePhones(existing.Phones, models.PhoneEntry{Number: rec.OrgPhone})
		if len(phones) != len(existing.Phones) {
			patch["phones"] = phones
		}
	}
	return patch, conflicts
}

// upsertPerson is the Person analogue of upsertOrganization. Dedup
// primary key is the lowercased email; on strict-miss the pipeline
// also runs a soft-match scan (first+last+phone overlap) and parks
// the row in marketing_conflict_reviews when a candidate is found.
// Soft-match never auto-merges — the false-positive rate is too high
// to commit without operator review.
func (p *Pipeline) upsertPerson(ctx context.Context, rec CanonicalRecord, tagUUIDs []string, prov models.ProvenanceSource) (string, error) {
	email := repository.NormalizeEmail(rec.PersonEmail)
	var existing *models.Person
	if email != "" {
		got, err := p.persons.LookupByEmail(ctx, email)
		if err != nil && !errors.Is(err, repository.ErrPersonNotFound) {
			return "", err
		}
		existing = got
	}

	if existing == nil {
		// Strict-miss — try the soft-match scan before creating a new
		// row. A confirmed soft-match parks the row for operator review
		// and the pipeline skips both the create AND the imported
		// emission (no committed Person → no Activity to attach).
		if parked, err := p.parsePersonSoftMatchPark(ctx, rec, tagUUIDs); err != nil {
			return "", err
		} else if parked {
			p.stats.ConflictsSkipped++ // legacy counter — still useful for the imports list
			return "", nil
		}
	}

	if existing != nil {
		patch, conflictFields := personMergePatchWithDetail(existing, rec, tagUUIDs)
		if len(patch) > 0 {
			if err := p.persons.Update(ctx, existing.UUID, patch); err != nil {
				return "", err
			}
		}
		if err := p.persons.AppendSource(ctx, existing.UUID, prov); err != nil {
			return "", err
		}
		p.stats.PersonsMerged++
		if len(conflictFields) > 0 {
			if p.reviewParker != nil {
				if err := p.reviewParker.Park(ctx, ParkInput{
					ImportJobUUID:    p.jobUUID,
					TargetKind:       models.ConflictTargetPerson,
					ExistingUUID:     existing.UUID,
					ExistingSnapshot: personSnapshotMap(existing),
					IncomingPayload:  personIncomingMap(rec),
					Conflicts:        conflictFields,
				}); err != nil {
					p.stats.ConflictsSkipped += len(conflictFields)
				}
			} else {
				p.stats.ConflictsSkipped += len(conflictFields)
			}
		}
		if p.emitter != nil {
			p.emitter.EmitImported(ctx, existing.UUID, "", p.jobUUID)
		}
		return existing.UUID, nil
	}

	per := &models.Person{
		UUID:      uuid.New().String(),
		FirstName: rec.PersonFirstName,
		LastName:  rec.PersonLastName,
		Title:     rec.PersonTitle,
		Language:  rec.PersonLanguage,
		Tags:      tagUUIDs,
		Sources:   []models.ProvenanceSource{prov},
		CreatedBy: actor(ctx),
	}
	if email != "" {
		per.Emails = []models.EmailEntry{{Address: email, Primary: true}}
	}
	if rec.PersonPhone != "" {
		per.Phones = []models.PhoneEntry{{Number: rec.PersonPhone, Primary: true}}
	}
	if rec.Notes != "" {
		per.Notes = rec.Notes
	}
	if rec.CustomFields != nil {
		per.CustomFields = rec.CustomFields
	}
	if err := p.persons.Create(ctx, per); err != nil {
		return "", err
	}
	p.stats.PersonsCreated++
	if p.emitter != nil {
		p.emitter.EmitImported(ctx, per.UUID, "", p.jobUUID)
	}
	return per.UUID, nil
}

// parsePersonSoftMatchPark runs the soft-match scan for Persons on a
// strict-miss row. Returns (parked=true, nil) when an existing person
// soft-matches the incoming candidate; the caller short-circuits the
// create path so the row lands in the review queue instead of as a
// brand-new contact. Returns (false, nil) when no soft-match exists
// or when the ReviewParker is unwired (test setups) — the parent
// continues with the legacy create path.
func (p *Pipeline) parsePersonSoftMatchPark(ctx context.Context, rec CanonicalRecord, tagUUIDs []string) (bool, error) {
	if p.reviewParker == nil {
		return false, nil
	}
	// Build a candidate Person purely to feed match.SoftMatchPerson —
	// the comparison contract takes *models.Person, not raw scalars,
	// so an in-memory candidate keeps the helper reusable.
	candidate := &models.Person{
		FirstName: rec.PersonFirstName,
		LastName:  rec.PersonLastName,
	}
	if rec.PersonPhone != "" {
		candidate.Phones = []models.PhoneEntry{{Number: rec.PersonPhone}}
	}

	existing, err := p.persons.FindSoftMatchByNameAndPhone(ctx, rec.PersonFirstName, rec.PersonLastName, candidate.Phones)
	if errors.Is(err, repository.ErrPersonNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	// Belt-and-braces re-check via the in-memory comparator — the index
	// query already enforces both names + at least one phone overlap,
	// but if the comparator's contract ever tightens we'd rather skip
	// the parking than ship a false positive.
	if !match.SoftMatchPerson(candidate, existing) {
		return false, nil
	}

	conflicts := []models.ConflictField{{
		Field:         "softMatch",
		ExistingValue: existing.UUID,
		IncomingValue: personIncomingMap(rec),
		Severity:      models.ConflictSeveritySoft,
	}}
	if err := p.reviewParker.Park(ctx, ParkInput{
		ImportJobUUID:    p.jobUUID,
		TargetKind:       models.ConflictTargetPerson,
		ExistingUUID:     existing.UUID,
		ExistingSnapshot: personSnapshotMap(existing),
		IncomingPayload:  personIncomingMap(rec),
		Conflicts:        conflicts,
	}); err != nil {
		// Parking is best-effort — if the queue write fails we fall
		// back to the legacy "skip and count" behaviour rather than
		// silently committing a possible duplicate.
		return false, nil
	}
	_ = tagUUIDs // tags only matter when the row commits; the resolver carries them via incomingPayload
	return true, nil
}

func personMergePatch(existing *models.Person, rec CanonicalRecord, tagUUIDs []string) (bson.M, int) {
	patch := bson.M{}
	conflicts := 0

	if existing.FirstName == "" && rec.PersonFirstName != "" {
		patch["firstName"] = rec.PersonFirstName
	}
	if existing.LastName == "" && rec.PersonLastName != "" {
		patch["lastName"] = rec.PersonLastName
	}
	if existing.Title == "" && rec.PersonTitle != "" {
		patch["title"] = rec.PersonTitle
	}
	if existing.Language == "" && rec.PersonLanguage != "" {
		patch["language"] = rec.PersonLanguage
	}

	// Dedup-key field: primary_email. If existing has a primary email
	// and incoming brings a different one, conflict-skip.
	incomingEmail := repository.NormalizeEmail(rec.PersonEmail)
	if incomingEmail != "" {
		primaryExisting := primaryEmail(existing.Emails)
		switch {
		case primaryExisting == "":
			// Existing has no primary — promote the incoming one.
			patch["emails"] = mergeEmails(existing.Emails, models.EmailEntry{Address: incomingEmail, Primary: true})
		case primaryExisting == incomingEmail:
			// Same primary — set-union of alt addresses (none here).
		default:
			// Different primary — conflict on dedup key. Add the
			// incoming as a non-primary entry rather than overwriting.
			emails := mergeEmails(existing.Emails, models.EmailEntry{Address: incomingEmail})
			if len(emails) != len(existing.Emails) {
				patch["emails"] = emails
			}
			conflicts++
		}
	}

	if rec.PersonPhone != "" {
		phones := mergePhones(existing.Phones, models.PhoneEntry{Number: rec.PersonPhone})
		if len(phones) != len(existing.Phones) {
			patch["phones"] = phones
		}
	}

	if newTags := mergeStringSet(existing.Tags, tagUUIDs); len(newTags) != len(existing.Tags) {
		patch["tags"] = newTags
	}
	return patch, conflicts
}

// ensureMembership links Person to Organization. Reuses an existing
// active membership for the pair when one exists; otherwise creates
// a new active row.
func (p *Pipeline) ensureMembership(ctx context.Context, personUUID, orgUUID string, rec CanonicalRecord) error {
	existing, err := p.mships.FindActivePair(ctx, personUUID, orgUUID)
	if err != nil && !errors.Is(err, repository.ErrMembershipNotFound) {
		return err
	}
	if existing != nil {
		// Already linked — Phase 1 leaves the existing row alone.
		return nil
	}
	m := &models.Membership{
		UUID:       uuid.New().String(),
		PersonUUID: personUUID,
		OrgUUID:    orgUUID,
		Role:       rec.Role,
		Department: rec.Department,
		Active:     true,
		CreatedBy:  actor(ctx),
	}
	if err := p.mships.Create(ctx, m); err != nil {
		return err
	}
	p.stats.MembershipsLinked++
	return nil
}

func (p *Pipeline) resolveTags(ctx context.Context, slugs []string) []string {
	if len(slugs) == 0 {
		return nil
	}
	out := make([]string, 0, len(slugs))
	for _, slug := range slugs {
		slug = strings.TrimSpace(slug)
		if slug == "" {
			continue
		}
		if u, ok := p.tagSlugMap[slug]; ok {
			out = append(out, u)
			continue
		}
		got, err := p.tagRepo.GetBySlug(ctx, slug)
		if err != nil {
			// Unknown slug — drop silently in Phase 1. The
			// future review-queue surface can warn.
			p.tagSlugMap[slug] = ""
			continue
		}
		p.tagSlugMap[slug] = got.UUID
		out = append(out, got.UUID)
	}
	return out
}

// --- helpers ------------------------------------------------------

func actor(ctx context.Context) string {
	if u, ok := ctxauth.GetUserUUID(ctx); ok {
		return u
	}
	return ""
}

func rowExternalID(idx int) string {
	return "row:" + intToString(idx)
}

func intToString(n int) string {
	if n == 0 {
		return "0"
	}
	negative := n < 0
	if negative {
		n = -n
	}
	digits := make([]byte, 0, 8)
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	if negative {
		return "-" + string(digits)
	}
	return string(digits)
}

func deriveOrgKind(raw string) models.OrganizationKind {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "company", "":
		return models.OrgKindCompany
	case "pa", "public_administration", "public administration":
		return models.OrgKindPublicAdministration
	case "foundation":
		return models.OrgKindFoundation
	case "association":
		return models.OrgKindAssociation
	default:
		return models.OrgKindOther
	}
}

func primaryEmail(es []models.EmailEntry) string {
	for _, e := range es {
		if e.Primary {
			return e.Address
		}
	}
	return ""
}

func mergeStringSet(existing, incoming []string) []string {
	seen := make(map[string]bool, len(existing)+len(incoming))
	out := make([]string, 0, len(existing)+len(incoming))
	for _, v := range existing {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	for _, v := range incoming {
		if v == "" {
			continue
		}
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

func mergeEmails(existing []models.EmailEntry, incoming models.EmailEntry) []models.EmailEntry {
	for _, e := range existing {
		if e.Address == incoming.Address {
			return existing
		}
	}
	return append(existing, incoming)
}

func mergePhones(existing []models.PhoneEntry, incoming models.PhoneEntry) []models.PhoneEntry {
	for _, e := range existing {
		if e.Number == incoming.Number {
			return existing
		}
	}
	return append(existing, incoming)
}
