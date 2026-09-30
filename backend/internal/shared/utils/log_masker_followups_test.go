package utils

import (
	"log/slog"
	"reflect"
	"strings"
	"testing"

	"github.com/orkestra/backend/internal/shared/redact"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// T1 follow-up: a map keyed by something other than a string, nested in a
// generic container, used to be returned as it was.
func TestMaskValue_MapWithNonStringKeys(t *testing.T) {
	m := logMasker{p: policy(nil), key: testHashKey}
	budget := maxMaskNodes
	got := m.maskValue([]any{
		map[int]string{7: "write to mario.rossi@example.com"},
		map[bool]any{true: map[string]any{"email": "a@b.it"}},
	}, 0, &budget)
	want := []any{
		map[string]any{"7": "write to [EMAIL]"},
		map[string]any{"true": map[string]any{"email": "[PII]"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("maskValue = %#v, want %#v", got, want)
	}
}

func TestMaskKV_TopLevelMapWithIntKeys(t *testing.T) {
	p := iface.DefaultLogContentPolicy()
	out, keep := MaskKV(&p, testHashKey, "byid", map[int]string{1: "from 10.1.2.3"})
	if !keep {
		t.Fatal("attribute dropped")
	}
	want := map[string]any{"1": "from 10.1.2.0/24"}
	if !reflect.DeepEqual(out, want) {
		t.Fatalf("MaskKV = %#v, want %#v", out, want)
	}
}

func TestMaskText_LongValueIsBoundedAndMarked(t *testing.T) {
	m := logMasker{p: policy(nil), key: testHashKey}
	// Exactly maxScanTextLen bytes of words, then an e-mail past the limit.
	head := strings.Repeat("word ", maxScanTextLen/5)
	long := head + "mario.rossi@example.com " + strings.Repeat("x", 3*maxScanTextLen)
	got := m.maskText(long)
	if len(got) > maxScanTextLen+len(textTruncated) {
		t.Fatalf("len = %d, want at most %d", len(got), maxScanTextLen+len(textTruncated))
	}
	if !strings.HasSuffix(got, textTruncated) {
		t.Fatalf("missing %s marker: …%q", textTruncated, got[len(got)-40:])
	}
	if strings.Contains(got, "mario") {
		t.Fatal("text past the limit leaked")
	}
}

func TestMaskText_CutNeverLeavesAPartialToken(t *testing.T) {
	m := logMasker{p: policy(nil), key: testHashKey}
	// The limit falls in the middle of the e-mail: half of it must not survive.
	prefix := strings.Repeat("a ", (maxScanTextLen-10)/2)
	s := prefix + "mario.rossi@example.com and more " + strings.Repeat("y", maxScanTextLen)
	got := m.maskText(s)
	if strings.Contains(got, "mario") || strings.Contains(got, "rossi") {
		t.Fatalf("partial token leaked: …%q", got[len(got)-60:])
	}
	// A single token longer than the limit is dropped whole.
	if got := m.maskText(strings.Repeat("z", 2*maxScanTextLen)); got != textTruncated {
		t.Fatalf("one huge token = %q…, want only the marker", got[:20])
	}
}

func TestMaskText_NoScanNoCut(t *testing.T) {
	off := logMasker{p: policy(func(p *iface.LogContentPolicy) { p.ScanFreeText = false }), key: testHashKey}
	long := strings.Repeat("b", 2*maxScanTextLen)
	if got := off.maskText(long); got != long {
		t.Fatal("text cut although scanFreeText is off")
	}
}

// Fix round 1: a map key is rendered only when it is a basic value. A struct
// or pointer key printed with fmt.Sprint would carry every field (names,
// passwords) into the log, so the whole map fails closed.
type keyPerson struct{ Name, Password string }

type keyLV struct{ v any }

func (k keyLV) LogValue() slog.Value { return slog.AnyValue(k.v) }

func maskMapValue(t *testing.T, v any) any {
	t.Helper()
	m := logMasker{p: policy(nil), key: testHashKey}
	budget := maxMaskNodes
	return m.maskValue([]any{v}, 0, &budget)
}

func TestMaskValue_MapWithStructKeyIsRedacted(t *testing.T) {
	got := maskMapValue(t, map[keyPerson]string{{"Mario Rossi", "hunter2"}: "x"})
	if want := []any{redact.Redacted}; !reflect.DeepEqual(got, want) {
		t.Fatalf("struct key: %#v, want %#v", got, want)
	}
	got = maskMapValue(t, map[*keyPerson]string{{"Mario Rossi", "hunter2"}: "x"})
	if want := []any{redact.Redacted}; !reflect.DeepEqual(got, want) {
		t.Fatalf("pointer key: %#v, want %#v", got, want)
	}
	got = maskMapValue(t, map[any]string{keyPerson{"Mario Rossi", "hunter2"}: "x"})
	if want := []any{redact.Redacted}; !reflect.DeepEqual(got, want) {
		t.Fatalf("interface holding a struct: %#v, want %#v", got, want)
	}
	got = maskMapValue(t, map[[2]int]string{{1, 2}: "x"})
	if want := []any{redact.Redacted}; !reflect.DeepEqual(got, want) {
		t.Fatalf("array key: %#v, want %#v", got, want)
	}
	got = maskMapValue(t, map[complex128]string{1i: "x"})
	if want := []any{redact.Redacted}; !reflect.DeepEqual(got, want) {
		t.Fatalf("complex key: %#v, want %#v", got, want)
	}
	got = maskMapValue(t, map[any]string{nil: "x"})
	if want := []any{redact.Redacted}; !reflect.DeepEqual(got, want) {
		t.Fatalf("nil interface key: %#v, want %#v", got, want)
	}
}

func TestMaskValue_MapWithLogValuerKeys(t *testing.T) {
	got := maskMapValue(t, map[any]string{keyLV{"mario.rossi@example.com"}: "x"})
	if want := []any{map[string]any{"[EMAIL]": "x"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("string LogValuer: %#v, want %#v", got, want)
	}
	got = maskMapValue(t, map[any]string{keyLV{keyPerson{"Mario Rossi", "hunter2"}}: "x"})
	if want := []any{redact.Redacted}; !reflect.DeepEqual(got, want) {
		t.Fatalf("struct LogValuer: %#v, want %#v", got, want)
	}
	got = maskMapValue(t, map[keyLV]string{{"plain"}: "x"})
	if want := []any{map[string]any{"plain": "x"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("typed LogValuer key: %#v, want %#v", got, want)
	}
}

func TestMaskValue_MapWithBasicKeys(t *testing.T) {
	got := maskMapValue(t, []any{
		map[float64]string{1.5: "a"},
		map[float32]string{2.25: "b"},
		map[bool]string{false: "c"},
		map[uint8]string{9: "d"},
		map[any]string{int64(-3): "e", "k": "f"},
	})
	want := []any{[]any{
		map[string]any{"1.5": "a"},
		map[string]any{"2.25": "b"},
		map[string]any{"false": "c"},
		map[string]any{"9": "d"},
		map[string]any{"-3": "e", "k": "f"},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("basic keys: %#v, want %#v", got, want)
	}
}
