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

import "io"

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
