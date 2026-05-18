package importers

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra-cc/orkestra-addon-marketing/models"
	"github.com/orkestra-cc/orkestra-addon-marketing/repository"
	"github.com/orkestra-cc/orkestra-sdk/ctxauth"
	"go.mongodb.org/mongo-driver/bson"
)

// Pipeline is the adapter-independent half of the import flow. It
// reads CanonicalRecord values from a Source, dedups against the
// existing tenant data, applies the Phase-1 auto-merge / conflict-
// skip policy, and writes the result to the marketing collections.
//
// Phase 1 scope:
//   - Person dedup primary key: primary_email
//   - Organization dedup primary key: vat → taxCode fallback
//   - Auto-merge: fill empty fields, set-union additive arrays
//     (tags, sources). Conflicts on the dedup-key fields (primary
//     email, vat, taxCode) are SKIPPED — the field is left alone
//     and Stats.ConflictsSkipped increments. Phase 3 will route
//     these to a review queue instead.
//   - Memberships are created (or reactivated) whenever a row
//     carries both an organization and a person identity.
type Pipeline struct {
	orgs       *repository.OrganizationRepository
	persons    *repository.PersonRepository
	mships     *repository.MembershipRepository
	tagRepo    *repository.TagRepository
	jobUUID    string
	importer   string
	stats      models.ImportJobStats
	tagSlugMap map[string]string // slug → tag UUID, lazy-cached
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

	if existing != nil {
		patch, conflicts := orgMergePatch(existing, rec, tagUUIDs)
		p.stats.ConflictsSkipped += conflicts
		if len(patch) > 0 {
			if err := p.orgs.Update(ctx, existing.UUID, patch); err != nil {
				return "", err
			}
		}
		if err := p.orgs.AppendSource(ctx, existing.UUID, prov); err != nil {
			return "", err
		}
		p.stats.OrgsMerged++
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
// primary key is the lowercased email.
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

	if existing != nil {
		patch, conflicts := personMergePatch(existing, rec, tagUUIDs)
		p.stats.ConflictsSkipped += conflicts
		if len(patch) > 0 {
			if err := p.persons.Update(ctx, existing.UUID, patch); err != nil {
				return "", err
			}
		}
		if err := p.persons.AppendSource(ctx, existing.UUID, prov); err != nil {
			return "", err
		}
		p.stats.PersonsMerged++
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
	return per.UUID, nil
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
