package module

import "strings"

// Record-list storage is a schema-level construct over the SAME flat
// key/value maps every other config field uses. An element's values live at
// dotted keys built from the field key, the element's immutable slug, and the
// declared sub-field key — which is what leaves per-key AES-256-GCM secret
// encryption untouched: an element's secret is an ordinary encrypted value at
// an ordinary key.
//
// Two key segments are reserved to the SDK, both prefixed "__" (a prefix
// ValidateConfigDeclarations forbids to module sub-fields):
//
//	<field>.__items          the roster — comma-joined slugs, in order
//	<field>.<slug>.__label   the element's editable display label

// RosterKey names the SDK-owned key holding a record list's slug roster.
func RosterKey(field string) string { return field + ".__items" }

// LabelKey names an element's editable display label.
func LabelKey(field, slug string) string { return field + "." + slug + ".__label" }

// ItemKey names one declared sub-field of one element.
func ItemKey(field, slug, sub string) string { return field + "." + slug + "." + sub }

// ItemPrefix is the key prefix owned by one element. The trailing separator is
// load-bearing: "email.profiles.a" alone also matches "email.profiles.a-b.host",
// and a-b is an ordinary sibling of a under the slug grammar.
func ItemPrefix(field, slug string) string { return field + "." + slug + "." }

// KeysUnderElement selects exactly the keys belonging to one element.
func KeysUnderElement(keys []string, field, slug string) []string {
	prefix := ItemPrefix(field, slug)
	out := make([]string, 0, 4)
	for _, k := range keys {
		if strings.HasPrefix(k, prefix) {
			out = append(out, k)
		}
	}
	return out
}
