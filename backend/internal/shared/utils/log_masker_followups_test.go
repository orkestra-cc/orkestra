package utils

import (
	"reflect"
	"strings"
	"testing"

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
