package importers

import (
	"testing"

	"github.com/orkestra-cc/orkestra-addon-marketing/models"
)

// TestOrgMergePatchFillEmpty: when the existing row has empty
// scalars, incoming fills them and no conflicts are recorded.
func TestOrgMergePatchFillEmpty(t *testing.T) {
	existing := &models.Organization{
		LegalName: "",
		VAT:       "",
		TaxCode:   "",
	}
	rec := CanonicalRecord{
		OrgLegalName: "Acme",
		OrgVAT:       "IT01234567890",
		OrgTaxCode:   "01234567890",
		OrgKind:      "company",
	}
	patch, conflicts := orgMergePatch(existing, rec, nil)
	if conflicts != 0 {
		t.Errorf("expected 0 conflicts on empty existing, got %d", conflicts)
	}
	if patch["legalName"] != "Acme" {
		t.Errorf("legalName fill missing: %v", patch["legalName"])
	}
	if patch["vat"] != "IT01234567890" {
		t.Errorf("vat fill missing: %v", patch["vat"])
	}
	if patch["taxCode"] != "01234567890" {
		t.Errorf("taxCode fill missing: %v", patch["taxCode"])
	}
}

// TestOrgMergePatchConflictOnVAT: differing VATs must not overwrite
// — Phase-1 policy is to skip the field and bump the conflict counter.
func TestOrgMergePatchConflictOnVAT(t *testing.T) {
	existing := &models.Organization{
		LegalName: "Acme",
		VAT:       "IT01234567890",
	}
	rec := CanonicalRecord{
		OrgVAT: "IT99999999999",
	}
	patch, conflicts := orgMergePatch(existing, rec, nil)
	if conflicts != 1 {
		t.Errorf("expected 1 conflict, got %d", conflicts)
	}
	if _, set := patch["vat"]; set {
		t.Errorf("vat must NOT be in patch when existing already has a different value")
	}
}

// TestOrgMergePatchSameVATIsNoConflict: identity match on the dedup
// key is the happy path of a re-import; no patch, no conflict.
func TestOrgMergePatchSameVATIsNoConflict(t *testing.T) {
	existing := &models.Organization{VAT: "IT01234567890"}
	rec := CanonicalRecord{OrgVAT: "IT01234567890"}
	_, conflicts := orgMergePatch(existing, rec, nil)
	if conflicts != 0 {
		t.Errorf("identity VAT match must not flag a conflict, got %d", conflicts)
	}
}

// TestOrgMergePatchTagsAreUnioned: the additive-array merge policy
// for tags. New tags get appended; duplicates are dropped.
func TestOrgMergePatchTagsAreUnioned(t *testing.T) {
	existing := &models.Organization{
		Tags: []string{"tag-a", "tag-b"},
	}
	rec := CanonicalRecord{} // tags come via the resolved arg
	patch, _ := orgMergePatch(existing, rec, []string{"tag-b", "tag-c"})
	got, ok := patch["tags"].([]string)
	if !ok {
		t.Fatalf("expected tags in patch, got %v", patch["tags"])
	}
	if len(got) != 3 {
		t.Errorf("tags = %v, want 3 unique", got)
	}
}

// TestPersonMergePatchEmailConflict: when the existing primary email
// differs from the incoming, the incoming is added as a non-primary
// entry and the conflict counter increments.
func TestPersonMergePatchEmailConflict(t *testing.T) {
	existing := &models.Person{
		FirstName: "Jane",
		Emails: []models.EmailEntry{
			{Address: "jane@old.example.com", Primary: true},
		},
	}
	rec := CanonicalRecord{
		PersonEmail: "jane@new.example.com",
	}
	patch, conflicts := personMergePatch(existing, rec, nil)
	if conflicts != 1 {
		t.Errorf("expected 1 conflict, got %d", conflicts)
	}
	emails, ok := patch["emails"].([]models.EmailEntry)
	if !ok {
		t.Fatalf("expected emails in patch")
	}
	if len(emails) != 2 {
		t.Errorf("expected primary to survive + incoming as non-primary, got %d entries", len(emails))
	}
	// The original primary stays primary.
	if !emails[0].Primary || emails[0].Address != "jane@old.example.com" {
		t.Errorf("primary should be preserved as the original, got %+v", emails[0])
	}
}

// TestPersonMergePatchNoPrimaryAdoptsIncoming: when the existing
// row has no primary, the incoming primary fills the gap with no
// conflict.
func TestPersonMergePatchNoPrimaryAdoptsIncoming(t *testing.T) {
	existing := &models.Person{
		FirstName: "Jane",
		Emails:    []models.EmailEntry{{Address: "alt@example.com"}}, // non-primary
	}
	rec := CanonicalRecord{PersonEmail: "primary@example.com"}
	patch, conflicts := personMergePatch(existing, rec, nil)
	if conflicts != 0 {
		t.Errorf("expected 0 conflicts when existing has no primary, got %d", conflicts)
	}
	emails, ok := patch["emails"].([]models.EmailEntry)
	if !ok {
		t.Fatalf("expected emails in patch")
	}
	// Find the primary-promoted entry.
	var promoted *models.EmailEntry
	for i, e := range emails {
		if e.Primary {
			promoted = &emails[i]
		}
	}
	if promoted == nil || promoted.Address != "primary@example.com" {
		t.Errorf("expected incoming to be promoted to primary, got emails=%+v", emails)
	}
}

// TestPersonMergePatchTagsUnion: same additive-merge policy for
// person tags.
func TestPersonMergePatchTagsUnion(t *testing.T) {
	existing := &models.Person{Tags: []string{"t1"}}
	rec := CanonicalRecord{}
	patch, _ := personMergePatch(existing, rec, []string{"t1", "t2"})
	got, _ := patch["tags"].([]string)
	if len(got) != 2 {
		t.Errorf("tags = %v, want 2 unique", got)
	}
}

// TestMergeStringSetDedupes: the helper that powers all additive
// merges. Pinned because subtle bugs here become silent data quality
// problems.
func TestMergeStringSetDedupes(t *testing.T) {
	got := mergeStringSet([]string{"a", "b"}, []string{"b", "c", ""})
	if len(got) != 3 {
		t.Errorf("got %v, want 3 unique (a, b, c, empty dropped)", got)
	}
}

// TestDeriveOrgKind: the case+synonym table the CSV adapter feeds
// through. Defaults to "company" on empty/unknown input.
func TestDeriveOrgKind(t *testing.T) {
	cases := []struct {
		in   string
		want models.OrganizationKind
	}{
		{"", models.OrgKindCompany},
		{"company", models.OrgKindCompany},
		{"Company", models.OrgKindCompany},
		{"pa", models.OrgKindPublicAdministration},
		{"public administration", models.OrgKindPublicAdministration},
		{"public_administration", models.OrgKindPublicAdministration},
		{"foundation", models.OrgKindFoundation},
		{"association", models.OrgKindAssociation},
		{"weird", models.OrgKindOther},
	}
	for _, c := range cases {
		if got := deriveOrgKind(c.in); got != c.want {
			t.Errorf("deriveOrgKind(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
