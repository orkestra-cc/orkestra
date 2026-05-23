// Package importers defines the common shape every marketing-data
// adapter implements. The Phase-1 deliverable ships only the `csv`
// adapter (see csv/adapter.go); excel and odoo arrive in Phase 3.
//
// Pipeline:
//
//	source ── extract  ──► per-row CanonicalRecord
//	       ── normalize ──► canonical field shape (e-mail lowercased, vat normalised, …)
//	       ── map       ──► (Organization?, Person?, Membership?, ProvenanceSource)
//	       ── dedup     ──► email for Person, vat → taxCode fallback for Organization
//	       ── commit    ──► auto-merge non-conflicting; skip conflicts on the dedup-key fields;
//	                       append sources[] entry on the matched / created row
//
// The pipeline lives in pipeline.go; the per-adapter responsibility
// is exclusively the extract+normalize step (Parse returns a Source
// that yields canonical rows).
package importers

import (
	"encoding/json"
	"io"
	"time"

	"github.com/orkestra-cc/orkestra-addon-marketing/models"
)

// EngagementSignal is the per-row engagement event the adapter
// extracts when the operator opts into "engagement mode" (Phase 4 —
// closes one of the three Phase-3 leftovers). The pipeline turns each
// signal into a marketing_activities row after the Person upsert
// resolves a personUuid; the resulting Activity's ExternalID
// (engagement:<jobUuid>:<personUuid>:<kind>:<unixNano>) keeps
// re-imports idempotent at the dedupKey unique index.
type EngagementSignal struct {
	Kind       models.ActivityKind
	OccurredAt time.Time
	// FallbackOccurredAt is set when the row's occurred_at cell was
	// missing or unparseable and the adapter substituted time.Now() at
	// extract time. Bumps Stats.EngagementOccurredAtFallback so
	// operators can see how much fidelity the import sacrificed.
	FallbackOccurredAt bool
}

// DecodeMapping deserializes a persisted ColumnMapping JSON blob.
// Empty / nil input yields an empty (non-nil) ColumnMapping so the
// pipeline can run with adapter defaults — useful for adapters that
// derive their mapping from the source itself (e.g. Odoo res.partner
// has a fixed schema). Errors surface as adapter-parse failures with
// the underlying json error attached.
func DecodeMapping(raw []byte) (ColumnMapping, error) {
	if len(raw) == 0 {
		return ColumnMapping{Columns: map[string]string{}}, nil
	}
	var m ColumnMapping
	if err := json.Unmarshal(raw, &m); err != nil {
		return ColumnMapping{}, err
	}
	if m.Columns == nil {
		m.Columns = map[string]string{}
	}
	return m, nil
}

// ColumnMapping is the operator-supplied translation from the
// adapter's native column space (CSV headers, Excel cell coords,
// Odoo field names) to the canonical-field keys the pipeline
// understands.
//
// Canonical keys recognised in Phase 1:
//
//	org.legalName   org.vat    org.taxCode    org.kind    org.website
//	org.email       org.phone
//	person.firstName   person.lastName    person.email    person.phone
//	person.title       person.language
//	role            (free-text job title, populates Membership.Role)
//	department      (Membership.Department)
//	tags            (semicolon-separated tag slugs, set-unioned into both Person and Organization)
//	notes           (free-text, set on Organization when org.* fields present, else on Person)
//	customField.<key>  (per-tenant typed bag, validated against marketing_custom_field_schemas)
type ColumnMapping struct {
	// Columns maps the source's native column name to a canonical
	// field key.
	Columns map[string]string `json:"columns"`

	// Options carries per-adapter parsing tweaks (delimiter,
	// header-row presence, etc.). The csv adapter currently
	// honours `delimiter` (default ",") and `hasHeaderRow`
	// (default true).
	Options map[string]string `json:"options,omitempty"`
}

// CanonicalRecord is the adapter→pipeline contract. Every field is
// optional; the pipeline decides what to do based on which fields
// are populated. RowIndex is 0-based across the source's rows for
// error messages.
type CanonicalRecord struct {
	RowIndex int

	// Organization fields
	OrgLegalName string
	OrgVAT       string
	OrgTaxCode   string
	OrgKind      string
	OrgWebsite   string
	OrgEmail     string
	OrgPhone     string

	// Person fields
	PersonFirstName string
	PersonLastName  string
	PersonEmail     string
	PersonPhone     string
	PersonTitle     string
	PersonLanguage  string

	// Membership fields
	Role       string
	Department string

	// Shared fields
	TagSlugs     []string
	CustomFields map[string]any
	Notes        string

	// EngagementSignals carries engagement-CSV / engagement-Odoo
	// events the adapter extracted from this row. Empty when engagement
	// mode is off or the row had no truthy engagement cells. The
	// pipeline consumes the slice after upsertPerson succeeds, emitting
	// one Activity per signal via ActivityEmitter.EmitEngagement.
	EngagementSignals []EngagementSignal
}

// Source is the adapter-yielded iterator the pipeline consumes. Implementations
// are expected to call Close once when the consumer is done — pipeline.Run
// does this in a defer.
type Source interface {
	// Records emits one CanonicalRecord per source row. The channel
	// is closed when the source is exhausted or after an error
	// (which the consumer can read via Err()).
	Records() <-chan CanonicalRecord

	// Err returns the first error encountered during extraction,
	// or nil when the source terminated cleanly. The consumer
	// reads it after the Records channel closes.
	Err() error

	// Close releases the underlying reader. Safe to call multiple
	// times.
	Close() error
}

// Importer is the contract every adapter implements. Future adapters
// (excel, odoo) drop in as a second implementation without changing
// the pipeline.
type Importer interface {
	// Name returns the adapter identifier persisted on ImportJob.Importer.
	Name() string

	// Parse turns a raw reader + an operator-supplied mapping into a
	// streaming Source. Errors here are configuration mistakes
	// (malformed CSV, missing required column mapping) and should
	// surface as 400s — runtime errors during row extraction flow
	// through Source.Err() instead.
	Parse(reader io.Reader, mapping ColumnMapping) (Source, error)
}

// CapabilityFlags is the static feature surface an adapter advertises
// to the operator UI. The wizard reads these to decide which steps to
// render (e.g. show the sheet picker for adapters where
// SheetSelection=true). New flags are append-only; defaults are zero
// values so adapters that don't care don't need to update.
type CapabilityFlags struct {
	// SheetSelection — the source has multiple addressable sheets
	// (xlsx workbooks). When true, the wizard surfaces a sheet picker
	// driven by the dry-run preview.
	SheetSelection bool

	// ActivityEmission — the adapter can produce Activity rows
	// alongside the canonical-record stream (engagement-CSV mode,
	// Odoo mail.message pull). When true, the wizard surfaces the
	// engagement opt-in toggle.
	ActivityEmission bool
}

// AdapterDescriptor pairs Name + Capabilities for adapters that opt
// into the description surface. The pipeline does not require it —
// only the wizard does — so adapters that don't implement
// DescribeCapabilities use the zero-value defaults.
type AdapterDescriptor interface {
	Importer
	// DescribeCapabilities returns the static feature surface for
	// this adapter. Called by the wizard's adapter-picker endpoint.
	DescribeCapabilities() CapabilityFlags
}
