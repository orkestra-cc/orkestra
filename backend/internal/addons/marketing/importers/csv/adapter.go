// Package csv implements the Phase-1 CSV adapter for the marketing
// importer pipeline. Any system that exports its data as CSV
// (campaign tools, CRMs, custom dumps) lands here via the operator's
// column mapping — there is no per-tool adapter, per design decision
// D17 in docs/plans/marketing-addon/Orkestra_marketing_addon.md.
package csv

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/orkestra-cc/orkestra-addon-marketing/importers"
)

// Adapter implements importers.Importer for CSV files.
type Adapter struct{}

// New returns a fresh CSV adapter. Stateless — one instance can serve
// any number of concurrent imports.
func New() *Adapter { return &Adapter{} }

// Name is the canonical identifier persisted on ImportJob.Importer.
func (a *Adapter) Name() string { return "csv" }

// Parse reads CSV bytes from reader, builds a streaming Source that
// emits one CanonicalRecord per data row, and returns it. The
// caller (pipeline) must invoke Source.Close() when done.
//
// Mapping options honoured:
//   - "delimiter": single rune separator, default ","
//   - "hasHeaderRow": "false" disables the first-row skip + header-
//     based column lookup (the mapping then must use 0-based column
//     indexes as keys: "0", "1", "2"…). Default true.
func (a *Adapter) Parse(reader io.Reader, mapping importers.ColumnMapping) (importers.Source, error) {
	delim := ','
	if d, ok := mapping.Options["delimiter"]; ok && d != "" {
		if len(d) != 1 {
			return nil, fmt.Errorf("csv: delimiter must be a single character, got %q", d)
		}
		delim = rune(d[0])
	}
	hasHeader := true
	if v, ok := mapping.Options["hasHeaderRow"]; ok && strings.EqualFold(v, "false") {
		hasHeader = false
	}

	csvReader := csv.NewReader(reader)
	csvReader.Comma = delim
	csvReader.FieldsPerRecord = -1 // tolerate uneven row widths

	src := &source{
		csv:     csvReader,
		mapping: mapping.Columns,
		records: make(chan importers.CanonicalRecord, 64),
	}

	// Resolve header → canonical-key index lookup up front.
	if hasHeader {
		header, err := csvReader.Read()
		if err != nil {
			return nil, fmt.Errorf("csv: read header row: %w", err)
		}
		src.fieldIdx = buildFieldIndex(header, mapping.Columns)
		if len(src.fieldIdx) == 0 {
			return nil, errors.New("csv: none of the mapping columns matched the header row")
		}
	} else {
		// Numeric-key mapping. Keys are "0", "1", …
		src.fieldIdx = buildNumericFieldIndex(mapping.Columns)
		if len(src.fieldIdx) == 0 {
			return nil, errors.New("csv: mapping is empty and hasHeaderRow=false leaves no columns to read")
		}
	}

	go src.run()
	return src, nil
}

type source struct {
	csv      *csv.Reader
	mapping  map[string]string // source-key → canonical-key
	fieldIdx map[int]string    // 0-based column index → canonical-key
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
	return nil
}

// run feeds the records channel until the CSV is exhausted or an
// extraction error occurs. The channel close signals end-of-stream
// to the pipeline.
func (s *source) run() {
	defer close(s.records)
	rowIdx := 0
	for {
		row, err := s.csv.Read()
		if errors.Is(err, io.EOF) {
			return
		}
		if err != nil {
			s.err = fmt.Errorf("csv: read row %d: %w", rowIdx, err)
			return
		}
		s.records <- s.rowToRecord(row, rowIdx)
		rowIdx++
	}
}

// rowToRecord projects one CSV row into the canonical shape.
// Multi-value fields (tags, customField.*) are populated by splitting
// the cell on ";".
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

// assignCanonical routes one canonical-key value into the right
// CanonicalRecord field. Unknown keys are ignored — the operator's
// mapping may include canonical keys we haven't wired yet (a Phase 2+
// feature surface) and they should silently no-op rather than fail
// the whole import.
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

// buildFieldIndex resolves the operator's header→canonical mapping
// against the CSV's actual header row, producing a column-index→
// canonical lookup the row loop consumes.
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

// buildNumericFieldIndex handles the no-header-row case where the
// mapping keys are 0-based column indexes (as strings).
func buildNumericFieldIndex(mapping map[string]string) map[int]string {
	idx := make(map[int]string, len(mapping))
	for k, canonical := range mapping {
		var n int
		if _, err := fmt.Sscanf(k, "%d", &n); err != nil {
			continue
		}
		idx[n] = canonical
	}
	return idx
}
