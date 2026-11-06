// Package csvsafe neutralizes CSV/formula injection for files a spreadsheet
// re-parses on open (OWASP): a cell whose first non-space character is one of
// = + - @ \t \r gets an apostrophe prefix. Apply to CSV only — an XLSX cell
// written as a string is never re-parsed and the apostrophe would corrupt it.
package csvsafe

import "strings"

func Cell(v string) string {
	body := strings.TrimLeft(v, " ")
	if body == "" {
		return v
	}
	switch body[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + v
	}
	return v
}

func Row(in []string) []string {
	out := make([]string, len(in))
	for i, v := range in {
		out[i] = Cell(v)
	}
	return out
}
