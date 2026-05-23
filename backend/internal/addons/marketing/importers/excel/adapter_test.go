package excel

import (
	"bytes"
	"testing"

	"github.com/orkestra-cc/orkestra-addon-marketing/importers"
	"github.com/xuri/excelize/v2"
)

// buildWorkbook constructs an in-memory xlsx file with the given sheets.
// rows[sheet][rowIndex][colIndex] is the cell value as a Go any (string,
// int, float64, bool, time.Time supported).
//
// Iteration order of the map is randomized; tests that need a specific
// active sheet pass an explicit `sheet` option to Adapter.Parse.
func buildWorkbook(t *testing.T, sheets map[string][][]any) []byte {
	t.Helper()
	f := excelize.NewFile()
	defer f.Close()

	// excelize.NewFile creates a default "Sheet1". Rename or augment
	// rather than delete + add, because DeleteSheet fails when the
	// last sheet would disappear.
	created := make(map[string]bool, len(sheets))
	first := true
	for name, rows := range sheets {
		if first {
			if name != "Sheet1" {
				if err := f.SetSheetName("Sheet1", name); err != nil {
					t.Fatalf("SetSheetName: %v", err)
				}
			}
			created[name] = true
			first = false
		} else if !created[name] {
			if _, err := f.NewSheet(name); err != nil {
				t.Fatalf("NewSheet(%q): %v", name, err)
			}
			created[name] = true
		}
		for r, row := range rows {
			for c, v := range row {
				cell, err := excelize.CoordinatesToCellName(c+1, r+1)
				if err != nil {
					t.Fatalf("coords %d,%d: %v", c, r, err)
				}
				if err := f.SetCellValue(name, cell, v); err != nil {
					t.Fatalf("SetCellValue %s!%s: %v", name, cell, err)
				}
			}
		}
	}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatalf("Write: %v", err)
	}
	return buf.Bytes()
}

func collectRecords(t *testing.T, src importers.Source) []importers.CanonicalRecord {
	t.Helper()
	got := []importers.CanonicalRecord{}
	for rec := range src.Records() {
		got = append(got, rec)
	}
	if err := src.Err(); err != nil {
		t.Fatalf("Source.Err(): %v", err)
	}
	return got
}

func TestAdapter_Name(t *testing.T) {
	if name := New().Name(); name != "excel" {
		t.Fatalf("Name() = %q, want %q", name, "excel")
	}
}

func TestAdapter_Capabilities(t *testing.T) {
	caps := New().DescribeCapabilities()
	if !caps.SheetSelection {
		t.Fatal("SheetSelection should be true for excel adapter")
	}
	if caps.ActivityEmission {
		t.Fatal("ActivityEmission should be false for Phase 3 excel adapter")
	}
}

func TestAdapter_Parse_FirstSheetByDefault(t *testing.T) {
	wb := buildWorkbook(t, map[string][][]any{
		"Contacts": {
			{"name", "email"},
			{"Jane Doe", "jane@a.example"},
			{"John Roe", "john@b.example"},
		},
	})
	a := New()
	src, err := a.Parse(bytes.NewReader(wb), importers.ColumnMapping{
		Columns: map[string]string{
			"name":  "person.firstName",
			"email": "person.email",
		},
	})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	defer src.Close()
	got := collectRecords(t, src)
	if len(got) != 2 {
		t.Fatalf("len=%d want 2", len(got))
	}
	if got[0].PersonFirstName != "Jane Doe" {
		t.Errorf("row0 firstName=%q", got[0].PersonFirstName)
	}
	if got[1].PersonEmail != "john@b.example" {
		t.Errorf("row1 email=%q", got[1].PersonEmail)
	}
}

func TestAdapter_Parse_SpecificSheet(t *testing.T) {
	wb := buildWorkbook(t, map[string][][]any{
		"Ignored": {
			{"a", "b"},
			{"1", "2"},
		},
		"Contacts": {
			{"email"},
			{"x@a.example"},
		},
	})
	a := New()
	src, err := a.Parse(bytes.NewReader(wb), importers.ColumnMapping{
		Columns: map[string]string{"email": "person.email"},
		Options: map[string]string{"sheet": "Contacts"},
	})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	defer src.Close()
	got := collectRecords(t, src)
	if len(got) != 1 || got[0].PersonEmail != "x@a.example" {
		t.Fatalf("unexpected rows: %+v", got)
	}
}

func TestAdapter_Parse_UnknownSheet(t *testing.T) {
	wb := buildWorkbook(t, map[string][][]any{
		"Contacts": {{"email"}, {"x@a.example"}},
	})
	a := New()
	_, err := a.Parse(bytes.NewReader(wb), importers.ColumnMapping{
		Columns: map[string]string{"email": "person.email"},
		Options: map[string]string{"sheet": "DoesNotExist"},
	})
	if err == nil {
		t.Fatal("expected error for missing sheet")
	}
}

func TestAdapter_Parse_NumericCellsCoercedToStrings(t *testing.T) {
	// VAT/TaxCode-style number that excelize would otherwise return in
	// scientific notation if treated as a raw float.
	wb := buildWorkbook(t, map[string][][]any{
		"Contacts": {
			{"name", "vat"},
			{"Acme S.p.A.", "01234567890"},
		},
	})
	a := New()
	src, err := a.Parse(bytes.NewReader(wb), importers.ColumnMapping{
		Columns: map[string]string{
			"name": "org.legalName",
			"vat":  "org.vat",
		},
	})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	defer src.Close()
	got := collectRecords(t, src)
	if len(got) != 1 {
		t.Fatalf("len=%d", len(got))
	}
	if got[0].OrgVAT != "01234567890" {
		t.Errorf("VAT = %q, want %q", got[0].OrgVAT, "01234567890")
	}
}

func TestAdapter_Parse_HeaderRowOption(t *testing.T) {
	// Two banner rows then a header on row 3, then data on rows 4+.
	wb := buildWorkbook(t, map[string][][]any{
		"Contacts": {
			{"Export from Foo"},
			{"Generated 2026-05-22"},
			{"name", "email"},
			{"Jane", "jane@a.example"},
		},
	})
	a := New()
	src, err := a.Parse(bytes.NewReader(wb), importers.ColumnMapping{
		Columns: map[string]string{
			"name":  "person.firstName",
			"email": "person.email",
		},
		Options: map[string]string{"headerRow": "3"},
	})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	defer src.Close()
	got := collectRecords(t, src)
	if len(got) != 1 {
		t.Fatalf("len=%d want 1", len(got))
	}
	if got[0].PersonFirstName != "Jane" || got[0].PersonEmail != "jane@a.example" {
		t.Errorf("row0: %+v", got[0])
	}
}

func TestAdapter_Parse_NumericMapping(t *testing.T) {
	wb := buildWorkbook(t, map[string][][]any{
		"Contacts": {
			{"Jane", "jane@a.example"},
			{"John", "john@b.example"},
		},
	})
	a := New()
	src, err := a.Parse(bytes.NewReader(wb), importers.ColumnMapping{
		Columns: map[string]string{
			"0": "person.firstName",
			"1": "person.email",
		},
		Options: map[string]string{"hasHeaderRow": "false"},
	})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	defer src.Close()
	got := collectRecords(t, src)
	if len(got) != 2 {
		t.Fatalf("len=%d", len(got))
	}
	if got[0].PersonFirstName != "Jane" || got[1].PersonEmail != "john@b.example" {
		t.Errorf("rows: %+v", got)
	}
}

func TestAdapter_Parse_EmptyRowsSkipped(t *testing.T) {
	wb := buildWorkbook(t, map[string][][]any{
		"Contacts": {
			{"email"},
			{"jane@a.example"},
			{""}, // empty trailing row — skipped
			{"john@b.example"},
		},
	})
	a := New()
	src, err := a.Parse(bytes.NewReader(wb), importers.ColumnMapping{
		Columns: map[string]string{"email": "person.email"},
	})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	defer src.Close()
	got := collectRecords(t, src)
	if len(got) != 2 {
		t.Fatalf("len=%d want 2 (empty row should be skipped)", len(got))
	}
}

func TestAdapter_Parse_MalformedFile(t *testing.T) {
	a := New()
	_, err := a.Parse(bytes.NewReader([]byte("not an xlsx file")), importers.ColumnMapping{
		Columns: map[string]string{"email": "person.email"},
	})
	if err == nil {
		t.Fatal("expected error for malformed file")
	}
}

func TestAdapter_Parse_UnmappedHeadersIgnored(t *testing.T) {
	wb := buildWorkbook(t, map[string][][]any{
		"Contacts": {
			{"name", "unmapped_col", "email"},
			{"Jane", "trash", "jane@a.example"},
		},
	})
	a := New()
	src, err := a.Parse(bytes.NewReader(wb), importers.ColumnMapping{
		Columns: map[string]string{
			"name":  "person.firstName",
			"email": "person.email",
		},
	})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	defer src.Close()
	got := collectRecords(t, src)
	if len(got) != 1 {
		t.Fatalf("len=%d", len(got))
	}
	if got[0].PersonFirstName != "Jane" || got[0].PersonEmail != "jane@a.example" {
		t.Errorf("row0: %+v", got[0])
	}
}
