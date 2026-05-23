package odoo

import "testing"

func TestMapResPartner_Organization(t *testing.T) {
	raw := map[string]any{
		"id":           float64(42),
		"name":         "ACME S.p.A.",
		"is_company":   true,
		"parent_id":    false,
		"email":        "info@acme.example",
		"phone":        "+39 02 1234567",
		"website":      "https://acme.example",
		"vat":          "IT01234567890",
		"category_ids": []any{float64(7)},
		"comment":      "<p>Long-term <b>partner</b></p>",
	}
	rec := MapResPartner(raw, nil, map[int64]string{7: "Hot Lead"})
	if rec == nil {
		t.Fatal("expected record, got nil")
	}
	if rec.OrgLegalName != "ACME S.p.A." {
		t.Errorf("legalName=%q", rec.OrgLegalName)
	}
	if rec.OrgVAT != "IT01234567890" {
		t.Errorf("vat=%q", rec.OrgVAT)
	}
	if rec.OrgEmail != "info@acme.example" {
		t.Errorf("email=%q", rec.OrgEmail)
	}
	if rec.OrgPhone != "+39 02 1234567" {
		t.Errorf("phone=%q", rec.OrgPhone)
	}
	if len(rec.TagSlugs) != 1 || rec.TagSlugs[0] != "hot-lead" {
		t.Errorf("tags=%v", rec.TagSlugs)
	}
	if rec.Notes != "Long-term partner" {
		t.Errorf("notes=%q", rec.Notes)
	}
	if rec.PersonFirstName != "" {
		t.Errorf("organization should not set PersonFirstName, got %q", rec.PersonFirstName)
	}
}

func TestMapResPartner_StandalonePerson(t *testing.T) {
	raw := map[string]any{
		"id":         float64(101),
		"name":       "Mario Rossi",
		"is_company": false,
		"parent_id":  false,
		"email":      "mario@rossi.example",
		"mobile":     "+39 333 1234567",
		"function":   "CEO",
		"lang":       "it_IT",
	}
	rec := MapResPartner(raw, nil, nil)
	if rec == nil {
		t.Fatal("expected record")
	}
	if rec.PersonFirstName != "Mario" || rec.PersonLastName != "Rossi" {
		t.Errorf("name split: first=%q last=%q", rec.PersonFirstName, rec.PersonLastName)
	}
	if rec.PersonEmail != "mario@rossi.example" {
		t.Errorf("email=%q", rec.PersonEmail)
	}
	// Phone falls back from `phone` (missing) to `mobile`.
	if rec.PersonPhone != "+39 333 1234567" {
		t.Errorf("phone=%q", rec.PersonPhone)
	}
	if rec.PersonTitle != "CEO" {
		t.Errorf("title=%q", rec.PersonTitle)
	}
	if rec.PersonLanguage != "it" {
		t.Errorf("lang=%q (want 'it' after BCP-47 normalize)", rec.PersonLanguage)
	}
	if rec.OrgLegalName != "" {
		t.Errorf("standalone person should not carry OrgLegalName, got %q", rec.OrgLegalName)
	}
}

func TestMapResPartner_PersonUnderParent(t *testing.T) {
	raw := map[string]any{
		"id":         float64(102),
		"name":       "Anna Bianchi",
		"is_company": false,
		"parent_id":  []any{float64(42), "ACME S.p.A."},
		"email":      "anna@acme.example",
		"function":   "CTO",
	}
	rec := MapResPartner(raw, map[int64]string{42: "ACME S.p.A."}, nil)
	if rec == nil {
		t.Fatal("expected record")
	}
	if rec.PersonFirstName != "Anna" || rec.PersonLastName != "Bianchi" {
		t.Errorf("name split: %q %q", rec.PersonFirstName, rec.PersonLastName)
	}
	if rec.OrgLegalName != "ACME S.p.A." {
		t.Errorf("expected parent legal name on record, got %q", rec.OrgLegalName)
	}
	if rec.PersonTitle != "CTO" {
		t.Errorf("title=%q", rec.PersonTitle)
	}
}

func TestMapResPartner_PersonUnderUnknownParent(t *testing.T) {
	raw := map[string]any{
		"id":         float64(103),
		"name":       "Luigi Verdi",
		"is_company": false,
		"parent_id":  []any{float64(999), "Mystery Corp"},
		"email":      "luigi@x.example",
	}
	rec := MapResPartner(raw, nil, nil)
	if rec == nil {
		t.Fatal("expected record")
	}
	// Parent not in our lookup map → OrgLegalName stays empty; pipeline
	// commits Person only, no Membership link.
	if rec.OrgLegalName != "" {
		t.Errorf("expected empty OrgLegalName, got %q", rec.OrgLegalName)
	}
	if rec.PersonFirstName != "Luigi" {
		t.Errorf("first=%q", rec.PersonFirstName)
	}
}

func TestMapResPartner_SingleWordName(t *testing.T) {
	raw := map[string]any{
		"id":         float64(104),
		"name":       "Madonna",
		"is_company": false,
		"parent_id":  false,
		"email":      "madonna@example.com",
	}
	rec := MapResPartner(raw, nil, nil)
	if rec.PersonFirstName != "Madonna" || rec.PersonLastName != "" {
		t.Errorf("single-word split: %q / %q", rec.PersonFirstName, rec.PersonLastName)
	}
}

func TestMapResPartner_OdooFalseAsAbsent(t *testing.T) {
	// Odoo serialises missing string fields as `false`. The mapper must
	// not panic and must return empty strings.
	raw := map[string]any{
		"id":         float64(105),
		"name":       "Test",
		"is_company": false,
		"parent_id":  false,
		"email":      false,
		"phone":      false,
		"website":    false,
		"vat":        false,
		"function":   false,
		"comment":    false,
	}
	rec := MapResPartner(raw, nil, nil)
	if rec == nil {
		t.Fatal("expected record")
	}
	if rec.PersonEmail != "" || rec.PersonPhone != "" || rec.PersonTitle != "" {
		t.Errorf("expected empty fields, got %+v", rec)
	}
}

func TestMapResPartner_NilAndUnmappable(t *testing.T) {
	if MapResPartner(nil, nil, nil) != nil {
		t.Error("nil raw should return nil")
	}
	// Empty name + empty email → unmappable scaffold row.
	r := map[string]any{"id": float64(1), "name": "", "email": false}
	if MapResPartner(r, nil, nil) != nil {
		t.Error("unmappable row should return nil")
	}
}

func TestCollectParentIDs_DedupAndSkipCompanies(t *testing.T) {
	batch := []map[string]any{
		{"id": float64(1), "is_company": true, "parent_id": false},
		{"id": float64(2), "is_company": false, "parent_id": []any{float64(1), "A"}},
		{"id": float64(3), "is_company": false, "parent_id": []any{float64(1), "A"}}, // dup
		{"id": float64(4), "is_company": false, "parent_id": []any{float64(7), "B"}},
		{"id": float64(5), "is_company": false, "parent_id": false},
	}
	got := CollectParentIDs(batch)
	if len(got) != 2 {
		t.Fatalf("got %d, want 2 (dedup + skip companies + skip null parents)", len(got))
	}
}

func TestCollectCategoryIDs_DedupAcrossBatch(t *testing.T) {
	batch := []map[string]any{
		{"id": float64(1), "category_ids": []any{float64(7), float64(8)}},
		{"id": float64(2), "category_ids": []any{float64(8), float64(9)}},
	}
	got := CollectCategoryIDs(batch)
	if len(got) != 3 {
		t.Fatalf("got %d, want 3 unique categories", len(got))
	}
}

func TestSlugifyOdooCategory(t *testing.T) {
	cases := map[string]string{
		"Hot Lead": "hot-lead",
		"  Cold  ": "cold",
		"Customer": "customer",
		"":         "",
	}
	for in, want := range cases {
		if got := slugifyOdooCategory(in); got != want {
			t.Errorf("slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeOdooLang(t *testing.T) {
	cases := map[string]string{
		"en_US": "en",
		"it_IT": "it",
		"en":    "en",
		"":      "",
	}
	for in, want := range cases {
		if got := normalizeOdooLang(in); got != want {
			t.Errorf("lang(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStripHTML(t *testing.T) {
	cases := map[string]string{
		"<p>hello</p>":          "hello",
		"<b>bold</b> <i>it</i>": "bold it",
		"no tags here":          "no tags here",
		"  <span>trim</span>  ": "trim",
	}
	for in, want := range cases {
		if got := stripHTML(in); got != want {
			t.Errorf("stripHTML(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestManyOneNameExposed(t *testing.T) {
	m := map[string]any{"parent_id": []any{float64(42), "Parent Co"}}
	if got := manyOneName(m, "parent_id"); got != "Parent Co" {
		t.Errorf("manyOneName = %q", got)
	}
	if got := manyOneName(m, "missing"); got != "" {
		t.Errorf("missing manyOneName should be empty, got %q", got)
	}
}
