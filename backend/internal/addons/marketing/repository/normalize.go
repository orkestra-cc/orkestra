// Package repository provides tenant-scoped MongoDB access for the
// marketing addon. Every query goes through
// github.com/orkestra-cc/orkestra-sdk/tenantrepo helpers so the
// CI tenantscope analyzer stays clean — new marketing code must not
// land any baseline entries in backend/tools/tenantscope/baseline.txt.
package repository

import "strings"

// NormalizeVAT canonicalises a VAT number for storage and dedup. The
// importer pipeline and any service code writing Organization records
// must call this before persisting so the unique sparse index on
// (tenantId, vat) compares apples to apples. Leading zeros are
// preserved on purpose — some jurisdictions treat them as
// significant.
func NormalizeVAT(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, " ", "")
	return strings.ToUpper(s)
}

// NormalizeTaxCode mirrors NormalizeVAT for the codice-fiscale-style
// identifier. Same rules — uppercase, no internal whitespace.
func NormalizeTaxCode(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, " ", "")
	return strings.ToUpper(s)
}

// NormalizeEmail produces the canonical form used both for storage
// and for dedup lookups. Lowercased, trimmed; no further parsing
// (we are deliberately not normalising gmail-style dot/+ aliases —
// that is a marketing-domain decision better left to the importer
// when the operator asks for it).
func NormalizeEmail(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}
