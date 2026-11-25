package services

import (
	"bytes"
	"encoding/csv"
	"strings"
	"testing"
	"time"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

func parseCSV(t *testing.T, b []byte) [][]string {
	t.Helper()
	rows, err := csv.NewReader(bytes.NewReader(b)).ReadAll()
	if err != nil {
		t.Fatalf("parse csv: %v", err)
	}
	return rows
}

func TestFlattenBundleCSVShapeAndOrder(t *testing.T) {
	bundle := iface.PersonalDataBundle{
		"zeta":     map[string]any{"b": 2, "a": []any{"x", map[string]any{"k": true}}},
		"alpha":    "scalar",
		"metadata": map[string]any{"userUuid": "u-1", "version": "2"},
		"empty":    map[string]any{"m": map[string]any{}, "l": []any{}, "n": nil},
	}
	var buf bytes.Buffer
	if err := FlattenBundleCSV(&buf, bundle); err != nil {
		t.Fatal(err)
	}
	rows := parseCSV(t, buf.Bytes())
	want := [][]string{
		{"producer", "path", "value"},
		{"metadata", "userUuid", "u-1"},
		{"metadata", "version", "2"},
		{"alpha", "", "scalar"},
		{"empty", "l", "[]"},
		{"empty", "m", "{}"},
		{"empty", "n", ""},
		{"zeta", "a[0]", "x"},
		{"zeta", "a[1].k", "true"},
		{"zeta", "b", "2"},
	}
	if len(rows) != len(want) {
		t.Fatalf("rows = %v", rows)
	}
	for i := range want {
		if strings.Join(rows[i], "|") != strings.Join(want[i], "|") {
			t.Fatalf("row %d = %v, want %v", i, rows[i], want[i])
		}
	}
}

func TestFlattenBundleCSVDeterministic(t *testing.T) {
	bundle := iface.PersonalDataBundle{"metadata": map[string]any{"a": 1}, "p": map[string]any{"x": 1, "y": 2, "z": 3, "w": 4}}
	var first bytes.Buffer
	_ = FlattenBundleCSV(&first, bundle)
	for i := 0; i < 20; i++ {
		var again bytes.Buffer
		_ = FlattenBundleCSV(&again, bundle)
		if again.String() != first.String() {
			t.Fatal("output differs between runs")
		}
	}
}

func TestFlattenBundleCSVNeutralizesFormulas(t *testing.T) {
	bundle := iface.PersonalDataBundle{
		"metadata": map[string]any{},
		"p":        map[string]any{"name": "=HYPERLINK(\"http://x\")", "note": "+1", "at": "@cmd", "dash": "-2+3", "-key": "ok"},
	}
	var buf bytes.Buffer
	if err := FlattenBundleCSV(&buf, bundle); err != nil {
		t.Fatal(err)
	}
	for _, r := range parseCSV(t, buf.Bytes())[1:] {
		for _, cell := range r {
			if cell != "" && strings.ContainsAny(cell[:1], "=+-@") {
				t.Fatalf("cell %q starts with a formula leader", cell)
			}
		}
	}
	if !strings.Contains(buf.String(), "'=HYPERLINK") {
		t.Fatalf("formula value not prefixed: %s", buf.String())
	}
}

func TestFlattenBundleCSVTypedValues(t *testing.T) {
	type rec struct {
		At time.Time `json:"at"`
		N  int64     `json:"n"`
	}
	bundle := iface.PersonalDataBundle{"p": []rec{{At: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), N: 9007199254740993}}}
	var buf bytes.Buffer
	if err := FlattenBundleCSV(&buf, bundle); err != nil {
		t.Fatal(err)
	}
	s := buf.String()
	if !strings.Contains(s, "p,[0].at,2026-01-02T03:04:05Z") || !strings.Contains(s, "9007199254740993") {
		t.Fatalf("typed values wrong: %s", s)
	}
}
