package csv

import (
	"strings"
	"testing"

	"github.com/orkestra-cc/orkestra-addon-marketing/importers"
)

// TestAdapterHappyPath exercises the basic CSV → CanonicalRecord
// path with header-based column mapping. Pins:
//   - whitespace gets trimmed off cell values
//   - tags column gets split on ";"
//   - unmapped columns are ignored without erroring
//   - empty cells are dropped (no zero-value pollution)
func TestAdapterHappyPath(t *testing.T) {
	csv := strings.Join([]string{
		"Name,Email,Company,VAT,Tag,Note,Ignored",
		"Jane Doe,Jane.Doe@Acme.com  ,Acme Manufacturing,IT01234567890,industry-mfg;region-northwest,Q1 lead,nope",
		"John Roe,john@example.com,Roe LLC,,vip,,",
	}, "\n")

	a := New()
	src, err := a.Parse(strings.NewReader(csv), importers.ColumnMapping{
		Columns: map[string]string{
			"Name":    "person.firstName",
			"Email":   "person.email",
			"Company": "org.legalName",
			"VAT":     "org.vat",
			"Tag":     "tags",
			"Note":    "notes",
		},
	})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	defer src.Close()

	got := drain(src)
	if err := src.Err(); err != nil {
		t.Fatalf("Source.Err: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(records) = %d, want 2", len(got))
	}

	r0 := got[0]
	if r0.PersonFirstName != "Jane Doe" {
		t.Errorf("rec0.PersonFirstName = %q, want %q", r0.PersonFirstName, "Jane Doe")
	}
	if r0.PersonEmail != "Jane.Doe@Acme.com" {
		t.Errorf("rec0.PersonEmail = %q (Adapter does NOT normalise — pipeline does)", r0.PersonEmail)
	}
	if r0.OrgLegalName != "Acme Manufacturing" {
		t.Errorf("rec0.OrgLegalName = %q", r0.OrgLegalName)
	}
	if r0.OrgVAT != "IT01234567890" {
		t.Errorf("rec0.OrgVAT = %q", r0.OrgVAT)
	}
	if len(r0.TagSlugs) != 2 || r0.TagSlugs[0] != "industry-mfg" || r0.TagSlugs[1] != "region-northwest" {
		t.Errorf("rec0.TagSlugs = %v", r0.TagSlugs)
	}
	if r0.Notes != "Q1 lead" {
		t.Errorf("rec0.Notes = %q", r0.Notes)
	}

	r1 := got[1]
	if r1.OrgVAT != "" {
		t.Errorf("rec1.OrgVAT (empty cell) = %q, want empty", r1.OrgVAT)
	}
	if r1.Notes != "" {
		t.Errorf("rec1.Notes (empty cell) = %q, want empty", r1.Notes)
	}
	if len(r1.TagSlugs) != 1 || r1.TagSlugs[0] != "vip" {
		t.Errorf("rec1.TagSlugs = %v", r1.TagSlugs)
	}
}

// TestAdapterUnknownMappingDoesntError captures the
// graceful-degradation behaviour: an operator-supplied mapping that
// includes a header the CSV doesn't have, plus a canonical key we
// haven't wired yet, should both silently no-op rather than failing
// the whole import.
func TestAdapterUnknownMappingDoesntError(t *testing.T) {
	csv := "Email\njane@example.com\n"
	src, err := New().Parse(strings.NewReader(csv), importers.ColumnMapping{
		Columns: map[string]string{
			"Email":             "person.email",
			"NonexistentColumn": "person.firstName",
			"Email2":            "future.field.we.dont.support.yet",
		},
	})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	defer src.Close()
	recs := drain(src)
	if len(recs) != 1 {
		t.Fatalf("len(records) = %d, want 1", len(recs))
	}
}

// TestAdapterCustomFieldRouting verifies that mapping a column to
// "customField.<key>" populates the CustomFields bag.
func TestAdapterCustomFieldRouting(t *testing.T) {
	csv := "Email,Industry\njane@example.com,manufacturing\n"
	src, err := New().Parse(strings.NewReader(csv), importers.ColumnMapping{
		Columns: map[string]string{
			"Email":    "person.email",
			"Industry": "customField.industry",
		},
	})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	defer src.Close()
	recs := drain(src)
	if len(recs) != 1 {
		t.Fatalf("got %d records", len(recs))
	}
	if v, ok := recs[0].CustomFields["industry"]; !ok || v != "manufacturing" {
		t.Errorf("CustomFields[\"industry\"] = %v, %t", v, ok)
	}
}

// TestAdapterNoHeaderRow exercises the numeric-index fallback.
func TestAdapterNoHeaderRow(t *testing.T) {
	csv := "jane@example.com,Jane\njohn@example.com,John\n"
	src, err := New().Parse(strings.NewReader(csv), importers.ColumnMapping{
		Columns: map[string]string{
			"0": "person.email",
			"1": "person.firstName",
		},
		Options: map[string]string{"hasHeaderRow": "false"},
	})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	defer src.Close()
	recs := drain(src)
	if len(recs) != 2 {
		t.Fatalf("got %d records", len(recs))
	}
	if recs[0].PersonEmail != "jane@example.com" || recs[0].PersonFirstName != "Jane" {
		t.Errorf("rec0 = %+v", recs[0])
	}
}

// TestAdapterEmptyMappingFails verifies the operator-error path: a
// mapping that doesn't reference any actual CSV header is invalid.
func TestAdapterEmptyMappingFails(t *testing.T) {
	csv := "A,B,C\n1,2,3\n"
	_, err := New().Parse(strings.NewReader(csv), importers.ColumnMapping{
		Columns: map[string]string{
			"Nope": "person.email",
		},
	})
	if err == nil {
		t.Fatalf("expected error on no-matching-columns mapping")
	}
}

func drain(src importers.Source) []importers.CanonicalRecord {
	var out []importers.CanonicalRecord
	for r := range src.Records() {
		out = append(out, r)
	}
	return out
}

// TestAdapterEngagementMode pins the Phase-4 engagement-CSV path:
//   - engagementMode option flips signal extraction on
//   - one signal per truthy engagement cell per row, kind from the
//     header's canonical map
//   - occurred_at cell parses to the signal's OccurredAt (no fallback)
//   - falsy cells (0/false/empty) produce no signal
//   - engagementMode off → no signals even when columns are present
func TestAdapterEngagementMode(t *testing.T) {
	csv := strings.Join([]string{
		"email,email_opened,email_clicked,occurred_at",
		"jane@example.com,1,true,2026-05-01T10:00:00Z",
		"john@example.com,0,,2026-05-02 11:30:00",
		"alice@example.com,yes,1,not-a-timestamp",
	}, "\n")

	src, err := New().Parse(strings.NewReader(csv), importers.ColumnMapping{
		Columns: map[string]string{"email": "person.email"},
		Options: map[string]string{"engagementMode": "true"},
	})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	defer src.Close()

	recs := drain(src)
	if len(recs) != 3 {
		t.Fatalf("len(records) = %d, want 3", len(recs))
	}

	// Row 0 — both engagement cells truthy, occurred_at parses.
	if len(recs[0].EngagementSignals) != 2 {
		t.Fatalf("rec0 signals = %d, want 2", len(recs[0].EngagementSignals))
	}
	if recs[0].EngagementSignals[0].FallbackOccurredAt {
		t.Errorf("rec0 sig0 should not be fallback (occurred_at parses cleanly)")
	}
	if recs[0].EngagementSignals[0].OccurredAt.IsZero() {
		t.Errorf("rec0 sig0 occurredAt is zero, want parsed value")
	}

	// Row 1 — only one engagement cell truthy ("0" + ""), occurred_at
	// "2006-01-02 15:04:05" layout still parses.
	if len(recs[1].EngagementSignals) != 0 {
		t.Errorf("rec1 signals = %d, want 0 (both cells falsy)", len(recs[1].EngagementSignals))
	}

	// Row 2 — both engagement cells truthy, occurred_at unparseable →
	// fallback.
	if len(recs[2].EngagementSignals) != 2 {
		t.Fatalf("rec2 signals = %d, want 2", len(recs[2].EngagementSignals))
	}
	for i, sig := range recs[2].EngagementSignals {
		if !sig.FallbackOccurredAt {
			t.Errorf("rec2 sig%d FallbackOccurredAt = false, want true (unparseable cell)", i)
		}
	}
}

// TestAdapterEngagementModeOff confirms the option-flag gate: even
// when the header carries engagement columns, signals are not emitted
// unless the operator opted in.
func TestAdapterEngagementModeOff(t *testing.T) {
	csv := "email,email_opened\njane@example.com,1\n"
	src, err := New().Parse(strings.NewReader(csv), importers.ColumnMapping{
		Columns: map[string]string{"email": "person.email"},
		// engagementMode option not set — should NOT extract signals.
	})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	defer src.Close()
	recs := drain(src)
	if len(recs[0].EngagementSignals) != 0 {
		t.Errorf("engagement-mode-off rec.EngagementSignals = %d, want 0", len(recs[0].EngagementSignals))
	}
}
