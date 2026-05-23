package importers

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
)

// ComputeIdempotencyKey returns the canonical idempotency key for an
// import submission. The default (no operator-supplied
// `Idempotency-Key` header) is SHA-256 of:
//
//	body || "\x00" || mapping_canonical_json
//
// where mapping_canonical_json is a stable, sorted-by-key JSON
// serialization. Same multipart payload + mapping → same key
// deterministically.
//
// Operator-supplied header is honoured verbatim — Phase 3 trusts that
// the caller's chosen key collides only when intentional.
func ComputeIdempotencyKey(body []byte, mapping ColumnMapping) string {
	h := sha256.New()
	h.Write(body)
	h.Write([]byte{0})
	h.Write(canonicalMappingBytes(mapping))
	return hex.EncodeToString(h.Sum(nil))
}

// canonicalMappingBytes serialises the mapping in a sorted-key form so
// json-key reordering (which the caller may or may not control)
// doesn't perturb the hash.
func canonicalMappingBytes(m ColumnMapping) []byte {
	colKeys := make([]string, 0, len(m.Columns))
	for k := range m.Columns {
		colKeys = append(colKeys, k)
	}
	sort.Strings(colKeys)
	optKeys := make([]string, 0, len(m.Options))
	for k := range m.Options {
		optKeys = append(optKeys, k)
	}
	sort.Strings(optKeys)

	buf := make([]byte, 0, 256)
	buf = append(buf, "columns:"...)
	for _, k := range colKeys {
		buf = append(buf, k...)
		buf = append(buf, '=')
		buf = append(buf, m.Columns[k]...)
		buf = append(buf, ';')
	}
	buf = append(buf, "options:"...)
	for _, k := range optKeys {
		buf = append(buf, k...)
		buf = append(buf, '=')
		buf = append(buf, m.Options[k]...)
		buf = append(buf, ';')
	}
	return buf
}
