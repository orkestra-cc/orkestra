package services

import (
	"bytes"
	"crypto/rand"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestParseCardCodeFormat_OK(t *testing.T) {
	cases := []struct {
		name  string
		tpl   string
		nodes int
	}{
		{"date only", "{YYYY}-{MM}-{DD}", 5},
		{"with seq", "PREM-{YYYY}-{seq:05}", 4},
		{"rand", "M-{rand:8}", 2},
		{"two-digit year", "TIX-{YY}{MM}-{seq:04}", 5},
		{"literal only", "BASE-PASS", 1},
		{"max widths", "X{seq:12}{rand:12}", 3},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ast, err := ParseCardCodeFormat(c.tpl)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := len(ast.nodes); got != c.nodes {
				t.Errorf("node count = %d, want %d (ast=%+v)", got, c.nodes, ast)
			}
		})
	}
}

func TestParseCardCodeFormat_Errors(t *testing.T) {
	cases := []struct {
		name string
		tpl  string
		want string
	}{
		{"empty", "", "empty template"},
		{"unterminated", "PRE-{YYYY", "unterminated placeholder"},
		{"empty placeholder", "PRE-{}", "empty placeholder"},
		{"unknown name", "PRE-{foo}", "unknown placeholder"},
		{"unknown with width", "PRE-{bogus:4}", "unknown placeholder"},
		{"zero width seq", "PRE-{seq:0}", "invalid width"},
		{"negative width seq", "PRE-{seq:-3}", "invalid width"},
		{"too-wide seq", "PRE-{seq:13}", "exceeds CodeFormatLimit"},
		{"too-wide rand", "PRE-{rand:99}", "exceeds CodeFormatLimit"},
		{"two seqs", "PRE-{seq:4}-{seq:4}", "more than one {seq:N}"},
		{"missing width seq", "PRE-{seq:}", "invalid width"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ParseCardCodeFormat(c.tpl)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error = %q, want substring %q", err, c.want)
			}
		})
	}
}

func TestRenderCardCode_DatePartsAndSeq(t *testing.T) {
	ast, err := ParseCardCodeFormat("PREM-{YYYY}-{MM}-{DD}-{seq:05}")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	when := time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC)
	got, err := RenderCardCode(ast, when, 42, rand.Reader)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if got != "PREM-2026-07-04-00042" {
		t.Errorf("render = %q, want PREM-2026-07-04-00042", got)
	}
}

func TestRenderCardCode_TwoDigitYear(t *testing.T) {
	ast, err := ParseCardCodeFormat("{YY}{MM}{DD}")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	when := time.Date(2026, 1, 9, 0, 0, 0, 0, time.UTC)
	got, _ := RenderCardCode(ast, when, 0, rand.Reader)
	if got != "260109" {
		t.Errorf("render = %q, want 260109", got)
	}
}

func TestRenderCardCode_SeqOverflowWidens(t *testing.T) {
	ast, _ := ParseCardCodeFormat("{seq:3}")
	got, _ := RenderCardCode(ast, time.Now(), 99999, rand.Reader)
	// %03d on 99999 widens to "99999" — no truncation. The fail-safe
	// (tenantId, code) unique index catches operator-level mistakes.
	if got != "99999" {
		t.Errorf("render = %q, want 99999 (overflow widens, never truncates)", got)
	}
}

func TestRenderCardCode_DeterministicRandSource(t *testing.T) {
	// All-zero source maps to crockfordAlphabet[0] which is '0'.
	ast, _ := ParseCardCodeFormat("X-{rand:6}")
	src := bytes.NewReader(make([]byte, 64))
	got, err := RenderCardCode(ast, time.Now(), 0, src)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if got != "X-000000" {
		t.Errorf("render with zero-source = %q, want X-000000", got)
	}
}

func TestRenderCardCode_RandRejectsTopBytes(t *testing.T) {
	// Feed bytes 230, 231, ..., then 0. drawRandom rejects everything
	// ≥ 224 and falls through to the 0 byte → crockfordAlphabet[0].
	ast, _ := ParseCardCodeFormat("{rand:1}")
	src := bytes.NewReader([]byte{230, 240, 250, 0})
	got, err := RenderCardCode(ast, time.Now(), 0, src)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if got != "0" {
		t.Errorf("render = %q, want 0 after rejection", got)
	}
}

func TestRenderCardCode_RandDistribution(t *testing.T) {
	// Chi-square goodness-of-fit against uniform over the 32-symbol
	// alphabet on 10^5 cryptographically-random draws. With 31 degrees
	// of freedom the 0.001 critical value is ~64; observed chi² will
	// blow past that only on a real bias, not on natural variance.
	const samples = 100_000
	ast, _ := ParseCardCodeFormat("{rand:1}")

	counts := make(map[rune]int, 32)
	for i := 0; i < samples; i++ {
		got, err := RenderCardCode(ast, time.Now(), 0, rand.Reader)
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		counts[rune(got[0])]++
	}

	expected := float64(samples) / 32.0
	var chi2 float64
	for _, ch := range crockfordAlphabet {
		obs := float64(counts[ch])
		diff := obs - expected
		chi2 += diff * diff / expected
	}
	// Critical value at p=0.001 with df=31 is ~64. We give a wide
	// safety margin (120) so test flakes are vanishingly unlikely
	// while still catching catastrophic bias.
	if chi2 > 120 {
		t.Errorf("chi-square = %.2f over %d samples — distribution looks biased", chi2, samples)
	}
}

func TestRenderCardCode_LiteralPassthrough(t *testing.T) {
	ast, _ := ParseCardCodeFormat("MEMBER-FOUNDERS")
	got, _ := RenderCardCode(ast, time.Now(), 0, rand.Reader)
	if got != "MEMBER-FOUNDERS" {
		t.Errorf("render = %q, want MEMBER-FOUNDERS", got)
	}
}

func TestRenderCardCode_NilAST(t *testing.T) {
	_, err := RenderCardCode(nil, time.Now(), 0, rand.Reader)
	if err == nil {
		t.Error("expected error for nil AST")
	}
}

func TestParseCardCodeFormat_ErrorPosition(t *testing.T) {
	_, err := ParseCardCodeFormat("PRE-{unknown}")
	var cfe *CodeFormatError
	if !errors.As(err, &cfe) {
		t.Fatalf("expected CodeFormatError, got %T: %v", err, err)
	}
	if cfe.Position != 4 {
		t.Errorf("position = %d, want 4 (offset of '{')", cfe.Position)
	}
}

func TestHasSequence(t *testing.T) {
	cases := []struct {
		tpl  string
		want bool
	}{
		{"PREM-{YYYY}-{seq:04}", true},
		{"M-{rand:6}", false},
		{"{YYYY}-{MM}-{DD}", false},
		{"BASE-PASS", false},
	}
	for _, c := range cases {
		t.Run(c.tpl, func(t *testing.T) {
			ast, err := ParseCardCodeFormat(c.tpl)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if got := ast.HasSequence(); got != c.want {
				t.Errorf("HasSequence = %v, want %v", got, c.want)
			}
		})
	}
}
