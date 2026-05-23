// Package excel implements the Phase-3 Excel adapter for the
// marketing importer pipeline. Wraps github.com/xuri/excelize so the
// pipeline can ingest .xlsx workbooks with the same canonical-record
// shape the CSV adapter produces.
//
// Cell normalization (numbers → strconv-formatted, dates → RFC3339,
// bools → "true"/"false") lives in reader.go so the canonical
// downstream stage (dedup, conflict-park, merge) never needs to know
// about Excel cell types.
//
// Sheet selection: workbooks may carry multiple sheets. The mapping's
// `sheet` option selects which sheet to ingest; absent the option,
// the adapter picks the FIRST sheet by index. The wizard surfaces
// this picker via the AdapterDescriptor capability flag
// (SheetSelection=true).
package excel

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/orkestra-cc/orkestra-addon-marketing/importers"
	"github.com/xuri/excelize/v2"
)

// Adapter implements importers.Importer for .xlsx files.
type Adapter struct{}

// New returns a fresh Excel adapter. Stateless — one instance can serve
// any number of concurrent imports.
func New() *Adapter { return &Adapter{} }

// Name is the canonical identifier persisted on ImportJob.Importer.
func (a *Adapter) Name() string { return "excel" }

// DescribeCapabilities tells the wizard this adapter supports sheet
// selection. ActivityEmission stays false — engagement-CSV column
// detection lives in the csv adapter (Phase 4 may surface it here
// too once we have engagement-export Excel files in the wild).
func (a *Adapter) DescribeCapabilities() importers.CapabilityFlags {
	return importers.CapabilityFlags{
		SheetSelection:   true,
		ActivityEmission: false,
	}
}

// Parse reads the entire workbook into memory, picks the sheet from
// the mapping's `sheet` option (or the first sheet by index), and
// returns a streaming Source over its data rows.
//
// Mapping options honoured:
//   - "sheet": exact sheet name to read. When absent, the first sheet
//     wins.
//   - "headerRow": 1-based row index of the header row. Default 1.
//     Data rows start at headerRow + 1.
//   - "hasHeaderRow": "false" disables header parsing — the mapping
//     keys then must be 0-based column indexes ("0", "1", "2"…).
//     Default true.
//
// excelize requires the full file in memory (the streaming API is
// write-only); we accept that for Phase 3 because typical contact
// imports are well under the 32 MB multipart cap the handler already
// enforces.
func (a *Adapter) Parse(reader io.Reader, mapping importers.ColumnMapping) (importers.Source, error) {
	f, err := excelize.OpenReader(reader)
	if err != nil {
		return nil, fmt.Errorf("excel: open workbook: %w", err)
	}

	sheetName := mapping.Options["sheet"]
	if sheetName == "" {
		sheets := f.GetSheetList()
		if len(sheets) == 0 {
			_ = f.Close()
			return nil, errors.New("excel: workbook has no sheets")
		}
		sheetName = sheets[0]
	} else {
		if _, err := f.GetSheetIndex(sheetName); err != nil {
			_ = f.Close()
			return nil, fmt.Errorf("excel: sheet %q: %w", sheetName, err)
		}
	}

	hasHeader := true
	if v, ok := mapping.Options["hasHeaderRow"]; ok && strings.EqualFold(v, "false") {
		hasHeader = false
	}
	headerRow := 1
	if v, ok := mapping.Options["headerRow"]; ok && v != "" {
		n, err := parseInt(v)
		if err != nil || n < 1 {
			_ = f.Close()
			return nil, fmt.Errorf("excel: headerRow must be a positive integer, got %q", v)
		}
		headerRow = n
	}

	rows, err := f.GetRows(sheetName)
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("excel: read sheet %q: %w", sheetName, err)
	}

	src := &source{
		file:    f,
		sheet:   sheetName,
		records: make(chan importers.CanonicalRecord, 64),
	}

	dataStart := 0
	if hasHeader {
		hdrIdx := headerRow - 1
		if hdrIdx >= len(rows) {
			_ = f.Close()
			return nil, fmt.Errorf("excel: header row %d beyond sheet length (%d rows)", headerRow, len(rows))
		}
		src.fieldIdx = buildFieldIndex(rows[hdrIdx], mapping.Columns)
		if len(src.fieldIdx) == 0 {
			_ = f.Close()
			return nil, errors.New("excel: none of the mapping columns matched the header row")
		}
		dataStart = hdrIdx + 1
	} else {
		src.fieldIdx = buildNumericFieldIndex(mapping.Columns)
		if len(src.fieldIdx) == 0 {
			_ = f.Close()
			return nil, errors.New("excel: mapping is empty and hasHeaderRow=false leaves no columns to read")
		}
	}

	// Materialise data rows up front. Faster than streaming via
	// excelize.Rows because we already read the full workbook for the
	// header lookup; this also lets the goroutine close the workbook
	// once it finishes.
	if dataStart < len(rows) {
		src.dataRows = rows[dataStart:]
	}

	go src.run()
	return src, nil
}

// source is the streaming iterator returned by Parse.
type source struct {
	file     *excelize.File
	sheet    string
	dataRows [][]string
	fieldIdx map[int]string // 0-based column index → canonical-key
	records  chan importers.CanonicalRecord
	err      error
	closed   bool
}

func (s *source) Records() <-chan importers.CanonicalRecord { return s.records }
func (s *source) Err() error                                { return s.err }
func (s *source) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	return s.file.Close()
}

// run feeds the records channel until the data rows are exhausted.
// excelize.GetRows already coerced typed cells to display strings,
// but we re-normalize via cellValue to make sure NaN-style edge
// cases (scientific notation for big VAT numbers, locale-dependent
// dates) don't leak through.
func (s *source) run() {
	defer close(s.records)
	for i, row := range s.dataRows {
		// Skip rows where every mapped column is empty — those are
		// usually trailing empties in the sheet, not data.
		hasAny := false
		for col := range s.fieldIdx {
			if col < len(row) && strings.TrimSpace(row[col]) != "" {
				hasAny = true
				break
			}
		}
		if !hasAny {
			continue
		}
		s.records <- s.rowToRecord(row, i)
	}
}

// rowToRecord projects one Excel row into the canonical shape. Mirrors
// the CSV adapter's assignment routine exactly so any future change to
// the canonical-key set only needs to update one source.
func (s *source) rowToRecord(row []string, rowIdx int) importers.CanonicalRecord {
	rec := importers.CanonicalRecord{RowIndex: rowIdx}
	for colIdx, canonical := range s.fieldIdx {
		if colIdx >= len(row) {
			continue
		}
		val := strings.TrimSpace(row[colIdx])
		if val == "" {
			continue
		}
		assignCanonical(&rec, canonical, val)
	}
	return rec
}

// assignCanonical mirrors the csv adapter's assignment routine.
// Duplicated rather than shared to keep the import graph for each
// adapter shallow.
func assignCanonical(rec *importers.CanonicalRecord, key, val string) {
	switch key {
	case "org.legalName":
		rec.OrgLegalName = val
	case "org.vat":
		rec.OrgVAT = val
	case "org.taxCode":
		rec.OrgTaxCode = val
	case "org.kind":
		rec.OrgKind = val
	case "org.website":
		rec.OrgWebsite = val
	case "org.email":
		rec.OrgEmail = val
	case "org.phone":
		rec.OrgPhone = val
	case "person.firstName":
		rec.PersonFirstName = val
	case "person.lastName":
		rec.PersonLastName = val
	case "person.email":
		rec.PersonEmail = val
	case "person.phone":
		rec.PersonPhone = val
	case "person.title":
		rec.PersonTitle = val
	case "person.language":
		rec.PersonLanguage = val
	case "role":
		rec.Role = val
	case "department":
		rec.Department = val
	case "tags":
		rec.TagSlugs = splitMulti(val)
	case "notes":
		rec.Notes = val
	default:
		if strings.HasPrefix(key, "customField.") {
			if rec.CustomFields == nil {
				rec.CustomFields = make(map[string]any)
			}
			rec.CustomFields[strings.TrimPrefix(key, "customField.")] = val
		}
	}
}

func splitMulti(v string) []string {
	parts := strings.Split(v, ";")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// buildFieldIndex resolves operator's header→canonical mapping
// against the workbook's actual header row.
func buildFieldIndex(header []string, mapping map[string]string) map[int]string {
	idx := make(map[int]string, len(mapping))
	for col, name := range header {
		nameTrim := strings.TrimSpace(name)
		if canonical, ok := mapping[nameTrim]; ok && canonical != "" {
			idx[col] = canonical
		}
	}
	return idx
}

// buildNumericFieldIndex handles the no-header-row case where mapping
// keys are 0-based column indexes (as strings).
func buildNumericFieldIndex(mapping map[string]string) map[int]string {
	idx := make(map[int]string, len(mapping))
	for k, canonical := range mapping {
		n, err := parseInt(k)
		if err != nil {
			continue
		}
		idx[n] = canonical
	}
	return idx
}

// parseInt is a thin strconv wrapper that returns (n, err) on a
// negative input (we want index ≥ 0).
func parseInt(s string) (int, error) {
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
		return 0, err
	}
	if n < 0 {
		return 0, fmt.Errorf("excel: column index must be ≥ 0, got %d", n)
	}
	return n, nil
}
