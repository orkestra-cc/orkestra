package odoo

import (
	"strings"

	"github.com/orkestra-cc/orkestra-addon-marketing/importers"
)

// ResPartnerFields is the explicit field allow-list the adapter
// requests from Odoo. Keeping the list small reduces wire size and
// makes the contract surface tractable. Add fields here when the
// canonical-record set grows; do NOT pass nil/empty to SearchRead.
var ResPartnerFields = []string{
	"id",
	"name",
	"is_company",
	"parent_id",
	"email",
	"phone",
	"mobile",
	"website",
	"vat",
	"function", // job title
	"comment",  // free-text notes
	"category_ids",
	"lang",
}

// MapResPartner converts one Odoo res.partner row into one or two
// CanonicalRecords:
//
//   - is_company == true  → Organization row (legalName + vat + email + phone)
//   - is_company == false → Person row (firstName + lastName + email + phone + title)
//
// When the partner is a person under a parent company (is_company=false
// AND parent_id is set), the caller is responsible for linking the
// person to the parent organization via Membership — the canonical
// record carries both halves so the pipeline's existing org+person
// flow handles it.
//
// category_ids → TagSlugs uses the category name as the slug. The
// pipeline's tag resolver auto-creates unknown slugs.
//
// Returns nil + nil when the partner is unmappable (missing both name
// AND email — Odoo treats the row as soft-deleted scaffolding).
func MapResPartner(raw map[string]any, parentNameByID map[int64]string, categoryNameByID map[int64]string) *importers.CanonicalRecord {
	if raw == nil {
		return nil
	}

	name := stringField(raw, "name")
	email := stringField(raw, "email")
	if name == "" && email == "" {
		return nil
	}

	rec := &importers.CanonicalRecord{}
	isCompany := boolField(raw, "is_company")
	parentID := manyOneID(raw, "parent_id")

	switch {
	case isCompany:
		rec.OrgLegalName = name
		rec.OrgVAT = stringField(raw, "vat")
		rec.OrgWebsite = stringField(raw, "website")
		rec.OrgEmail = email
		rec.OrgPhone = firstNonEmpty(stringField(raw, "phone"), stringField(raw, "mobile"))

	case !isCompany && parentID != 0:
		// Person under a parent company. Populate BOTH org + person
		// identity on the canonical record so the existing pipeline
		// creates the Membership link in one pass.
		fname, lname := splitFullName(name)
		rec.PersonFirstName = fname
		rec.PersonLastName = lname
		rec.PersonEmail = email
		rec.PersonPhone = firstNonEmpty(stringField(raw, "phone"), stringField(raw, "mobile"))
		rec.PersonTitle = stringField(raw, "function")
		rec.PersonLanguage = normalizeOdooLang(stringField(raw, "lang"))
		// Lookup the parent's legal name from the bulk-fetched map so
		// the pipeline's strict-match on VAT/TaxCode still works.
		// When the parent isn't in our fetch window, leave OrgLegalName
		// empty — the pipeline will skip the org branch and only
		// create/merge the Person row.
		if parentName, ok := parentNameByID[parentID]; ok && parentName != "" {
			rec.OrgLegalName = parentName
		}

	default:
		// Standalone person — no parent company link.
		fname, lname := splitFullName(name)
		rec.PersonFirstName = fname
		rec.PersonLastName = lname
		rec.PersonEmail = email
		rec.PersonPhone = firstNonEmpty(stringField(raw, "phone"), stringField(raw, "mobile"))
		rec.PersonTitle = stringField(raw, "function")
		rec.PersonLanguage = normalizeOdooLang(stringField(raw, "lang"))
	}

	// Categories → tags. Odoo returns category_ids as a list of int IDs;
	// resolve through the bulk-fetched name map.
	if ids, ok := raw["category_ids"].([]any); ok {
		for _, idRaw := range ids {
			id := coerceInt64(idRaw)
			if id == 0 {
				continue
			}
			if name, ok := categoryNameByID[id]; ok && name != "" {
				rec.TagSlugs = append(rec.TagSlugs, slugifyOdooCategory(name))
			}
		}
	}

	// HTML comment → strip tags into Notes (best-effort, no DOM parse —
	// Odoo's `comment` is short-form HTML, never structured docs).
	if c := stringField(raw, "comment"); c != "" {
		rec.Notes = stripHTML(c)
	}

	return rec
}

// CollectParentIDs scans a batch of res.partner rows for the unique
// parent_id values referenced by non-company partners. Used by the
// adapter to issue a single follow-up SearchRead against the parent
// company names instead of N+1 lookups.
func CollectParentIDs(batch []map[string]any) []int64 {
	seen := make(map[int64]struct{})
	ids := make([]int64, 0)
	for _, row := range batch {
		if boolField(row, "is_company") {
			continue
		}
		pid := manyOneID(row, "parent_id")
		if pid == 0 {
			continue
		}
		if _, ok := seen[pid]; ok {
			continue
		}
		seen[pid] = struct{}{}
		ids = append(ids, pid)
	}
	return ids
}

// CollectCategoryIDs scans the batch for every unique category id
// referenced via category_ids. Same single-roundtrip pattern.
func CollectCategoryIDs(batch []map[string]any) []int64 {
	seen := make(map[int64]struct{})
	ids := make([]int64, 0)
	for _, row := range batch {
		raw, ok := row["category_ids"].([]any)
		if !ok {
			continue
		}
		for _, idRaw := range raw {
			id := coerceInt64(idRaw)
			if id == 0 {
				continue
			}
			if _, dup := seen[id]; dup {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	return ids
}

// stringField extracts a string field. Odoo represents missing values
// as `false` (a quirk of the XML-RPC heritage); we treat false as
// empty.
func stringField(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	case bool:
		// Odoo returns false for "absent string"; ignore it.
		return ""
	default:
		return ""
	}
}

func boolField(m map[string]any, key string) bool {
	v, ok := m[key]
	if !ok || v == nil {
		return false
	}
	b, _ := v.(bool)
	return b
}

// manyOneID extracts the ID half of an Odoo Many2one field. JSON-2
// serializes Many2one as either `[id, display_name]` (when the
// reference is set) or `false` (when null). XML-RPC quirks aside,
// this implementation handles both.
func manyOneID(m map[string]any, key string) int64 {
	v, ok := m[key]
	if !ok || v == nil {
		return 0
	}
	switch x := v.(type) {
	case []any:
		if len(x) == 0 {
			return 0
		}
		return coerceInt64(x[0])
	case bool:
		return 0
	default:
		return 0
	}
}

// manyOneName extracts the display_name half. Exposed for the
// parent-name fetch path: the adapter can short-circuit the parent
// SearchRead when the inline display name is enough.
func manyOneName(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	if arr, ok := v.([]any); ok && len(arr) >= 2 {
		if s, ok := arr[1].(string); ok {
			return s
		}
	}
	return ""
}

func coerceInt64(v any) int64 {
	switch x := v.(type) {
	case int:
		return int64(x)
	case int64:
		return x
	case float64:
		return int64(x)
	}
	return 0
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// splitFullName breaks "Mario Rossi" → ("Mario", "Rossi"). Single-word
// names go to first name with empty last name; the marketing
// Person.HasMinimumIdentity invariant only requires (firstName,
// lastName) OR an email, so single-name imports still land.
func splitFullName(name string) (string, string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", ""
	}
	parts := strings.Fields(name)
	if len(parts) == 1 {
		return parts[0], ""
	}
	first := parts[0]
	last := strings.Join(parts[1:], " ")
	return first, last
}

// normalizeOdooLang maps Odoo's locale codes (en_US, it_IT) to BCP-47
// tags (en, it) — Person.Language is BCP-47 by contract.
func normalizeOdooLang(s string) string {
	if s == "" {
		return ""
	}
	s = strings.ToLower(s)
	// Trim country suffix.
	if idx := strings.IndexByte(s, '_'); idx > 0 {
		return s[:idx]
	}
	return s
}

// slugifyOdooCategory turns "Hot Lead" → "hot-lead". Conservative
// transform: lowercase + spaces to dashes; no other punctuation
// stripping. The tag system normalises further if needed.
func slugifyOdooCategory(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = strings.ReplaceAll(s, " ", "-")
	return s
}

// stripHTML removes HTML tags from a string. Best-effort — Odoo's
// `comment` field is short rich-text from operators, not structured
// HTML, so a single-pass tag strip is sufficient. A future Phase 5+
// might switch to goquery for a more robust transform.
func stripHTML(s string) string {
	var b strings.Builder
	inTag := false
	for _, r := range s {
		switch r {
		case '<':
			inTag = true
		case '>':
			inTag = false
		default:
			if !inTag {
				b.WriteRune(r)
			}
		}
	}
	return strings.TrimSpace(b.String())
}
