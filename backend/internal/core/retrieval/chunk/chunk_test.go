package chunk

import (
	"reflect"
	"strings"
	"testing"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

func eq(t *testing.T, want, got any) {
	t.Helper()
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("want %+v, got %+v", want, got)
	}
}

func noErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func fixed(max, overlap int) iface.RetrievalChunking {
	return iface.RetrievalChunking{Strategy: "fixed", MaxTokens: max, Overlap: overlap}
}

func TestSplit_FixedWindowsWithOverlap(t *testing.T) {
	got, tr, err := Split("a b c d e f g h i j", fixed(4, 1))
	noErr(t, err)
	eq(t, false, tr)
	eq(t, []Chunk{{0, "a b c d", 0, 7}, {1, "d e f g", 6, 13}, {2, "g h i j", 12, 19}}, got)
}

func TestSplit_NoneTrimsOuterWhitespace(t *testing.T) {
	got, tr, err := Split("  hello world  ", iface.RetrievalChunking{Strategy: "none"})
	noErr(t, err)
	eq(t, false, tr)
	eq(t, []Chunk{{0, "hello world", 2, 13}}, got)

	for _, c := range []iface.RetrievalChunking{{Strategy: "none"}, {}, fixed(4, 1)} {
		got, tr, err = Split(" \n\t ", c)
		noErr(t, err)
		eq(t, false, tr)
		eq(t, 0, len(got))
	}
}

func TestSplit_RuneOffsets(t *testing.T) {
	got, _, err := Split("città è", fixed(1, 0))
	noErr(t, err)
	eq(t, []Chunk{{0, "città", 0, 5}, {1, "è", 6, 7}}, got)
}

func TestSplit_LongRunsCountAs32RunePieces(t *testing.T) {
	eq(t, 3, CountTokens(strings.Repeat("x", 70)))
	got, _, err := Split(strings.Repeat("x", 70), fixed(2, 0))
	noErr(t, err)
	eq(t, []Chunk{{0, strings.Repeat("x", 64), 0, 64}, {1, strings.Repeat("x", 6), 64, 70}}, got)
}

func TestSplit_NoneTruncatesAtTokenBoundary(t *testing.T) {
	got, tr, err := Split(strings.Repeat("abcdefghij ", 7000), iface.RetrievalChunking{Strategy: "none"})
	noErr(t, err)
	eq(t, true, tr)
	eq(t, 1, len(got))
	eq(t, 64503, len(got[0].Text))
	eq(t, 64503, got[0].CharEnd)
	eq(t, true, strings.HasSuffix(got[0].Text, "abcdefghij"))
}

func TestSplit_FixedCapsChunksPerItem(t *testing.T) {
	got, tr, err := Split(strings.Repeat("w ", 600), fixed(1, 0))
	noErr(t, err)
	eq(t, 512, len(got))
	eq(t, true, tr)
}

func TestSplit_InvalidConfigIsAnError(t *testing.T) {
	got, _, err := Split("a b", fixed(0, 0))
	if err == nil {
		t.Fatal("want error")
	}
	eq(t, 0, len(got))
}

func TestValidate(t *testing.T) {
	for _, c := range []iface.RetrievalChunking{
		{Strategy: "none"}, {}, fixed(1, 0), fixed(4096, 0), fixed(4, 3),
	} {
		if err := Validate(c); err != nil {
			t.Errorf("%+v: %v", c, err)
		}
	}
	for _, c := range []iface.RetrievalChunking{
		fixed(0, 0), fixed(4097, 0), fixed(4, 4), fixed(4, -1),
		{Strategy: "none", MaxTokens: 10}, {Strategy: "hierarchical", MaxTokens: 4},
	} {
		if Validate(c) == nil {
			t.Errorf("%+v: want error", c)
		}
	}
}
