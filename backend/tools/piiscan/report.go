package piiscan

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

// Severity controls which diagnostics fail CI. piiscan emits a single
// ERROR-severity category today; the type mirrors policycoverage so the
// CLI and report share a shape.
type Severity int

const (
	SeverityInfo Severity = iota
	SeverityWarn
	SeverityError
)

func (s Severity) String() string {
	switch s {
	case SeverityError:
		return "ERROR"
	case SeverityWarn:
		return "WARN"
	default:
		return "INFO"
	}
}

// Diagnostic is one finding. Category is the stable identifier the baseline
// matches on; Key is the module being flagged; Sites point at the offending
// subject fields.
type Diagnostic struct {
	Severity Severity `json:"severity"`
	Category string   `json:"category"`
	Key      string   `json:"key"`
	Detail   string   `json:"detail,omitempty"`
	Sites    []string `json:"sites,omitempty"`
}

// Report is the reconciliation output.
type Report struct {
	Diagnostics []Diagnostic     `json:"diagnostics"`
	Summary     map[Severity]int `json:"summary"`
}

// CategoryNoProducer is the one failing category: a module owns subject PII
// but registers no PIIProducer.
const CategoryNoProducer = "pii.subject_collection.no_producer"

// Reconcile turns findings into a report. A module is flagged when it has at
// least one subject-bearing field but no NewPIIProducer constructor. baseline
// suppresses matching "category:key" lines so accepted cases (e.g. the
// compliance audit trail, retained by design rather than DSR-erasable) keep
// CI green while new gaps still fail.
func Reconcile(f *Findings, baseline map[string]bool) *Report {
	r := &Report{Summary: map[Severity]int{}}
	if baseline == nil {
		baseline = map[string]bool{}
	}

	producerModules := map[string]bool{}
	for _, p := range f.Producers {
		producerModules[p.Module] = true
	}

	// Group subject-field sites by module, preserving sorted order.
	sitesByModule := map[string][]string{}
	tagsByModule := map[string]map[string]bool{}
	var modules []string
	for _, sf := range f.SubjectFields {
		if _, seen := sitesByModule[sf.Module]; !seen {
			modules = append(modules, sf.Module)
			tagsByModule[sf.Module] = map[string]bool{}
		}
		sitesByModule[sf.Module] = append(sitesByModule[sf.Module],
			fmt.Sprintf("%s (%s.%s)", sf.Pos.String(), sf.Struct, sf.Field))
		tagsByModule[sf.Module][sf.Tag] = true
	}
	sort.Strings(modules)

	for _, m := range modules {
		if producerModules[m] {
			continue
		}
		tags := make([]string, 0, len(tagsByModule[m]))
		for t := range tagsByModule[m] {
			tags = append(tags, t)
		}
		sort.Strings(tags)
		r.add(baseline, Diagnostic{
			Severity: SeverityError,
			Category: CategoryNoProducer,
			Key:      m,
			Detail: fmt.Sprintf("module persists data-subject PII (bson %s) but registers no iface.PIIProducer; "+
				"this personal data escapes the GDPR DSR export/erase sweep. Add a PIIProducer, or baseline this module if the data is retained by design (audit trail, legal hold).",
				strings.Join(backtickEach(tags), ", ")),
			Sites: sitesByModule[m],
		})
	}
	return r
}

func backtickEach(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = "`" + s + "`"
	}
	return out
}

// add appends a diagnostic unless the baseline suppresses it, counting either
// way so the report distinguishes "0 problems" from "all problems masked".
func (r *Report) add(baseline map[string]bool, d Diagnostic) {
	key := d.Category + ":" + d.Key
	if baseline[key] {
		return
	}
	r.Diagnostics = append(r.Diagnostics, d)
	r.Summary[d.Severity]++
}

// HasErrors reports whether any ERROR-severity diagnostic survived the
// baseline. The CLI uses this to decide the exit code.
func (r *Report) HasErrors() bool { return r.Summary[SeverityError] > 0 }

// WriteMarkdown renders a human-readable report.
func WriteMarkdown(w io.Writer, r *Report, f *Findings) error {
	var b strings.Builder
	b.WriteString("# PII producer coverage report\n\n")
	fmt.Fprintf(&b, "Scanned %d Go packages. Found %d subject-bearing field(s) and %d PIIProducer constructor(s).\n\n",
		f.Packages, len(f.SubjectFields), len(f.Producers))
	fmt.Fprintf(&b, "Summary: **%d errors**, **%d warnings**, **%d info**.\n\n",
		r.Summary[SeverityError], r.Summary[SeverityWarn], r.Summary[SeverityInfo])

	b.WriteString("A module is flagged when one of its persisted models carries a data-subject field ")
	b.WriteString("(a `bson:\"...\"` tag in the subject allow-list) but the module registers no ")
	b.WriteString("`iface.PIIProducer`. Such data never reaches the compliance DSR export/erase sweep (ADR-0009). ")
	b.WriteString("Fix by registering a producer, or baseline the module when the data is retained by design ")
	b.WriteString("(audit trail, legal hold). Baseline entries are `pii.subject_collection.no_producer:<module>`.\n\n")

	matching := make([]Diagnostic, 0, len(r.Diagnostics))
	for _, d := range r.Diagnostics {
		if d.Severity == SeverityError {
			matching = append(matching, d)
		}
	}
	fmt.Fprintf(&b, "## Errors (fail CI) (%d)\n\n", len(matching))
	if len(matching) == 0 {
		b.WriteString("_None._\n\n")
	} else {
		b.WriteString("| Module | Tags | Sites |\n|---|---|---|\n")
		for _, d := range matching {
			fmt.Fprintf(&b, "| `%s` | %s | %s |\n", d.Key, d.Detail, strings.Join(d.Sites, "<br>"))
		}
		b.WriteString("\n")
	}

	_, err := io.WriteString(w, b.String())
	return err
}

// WriteJSON writes the machine-readable report for CI tooling.
func WriteJSON(w io.Writer, r *Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}
