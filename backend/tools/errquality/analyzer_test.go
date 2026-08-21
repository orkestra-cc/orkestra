package errquality

import (
	"go/parser"
	"go/token"
	"testing"
)

// findings parses src as a Go file and returns "rule" strings for every
// diagnostic inspectFile reports, in source order.
func findings(t *testing.T, src string) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "handler.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var got []string
	inspectFile(fset, f, func(_ token.Pos, rule, _ string) {
		got = append(got, rule)
	})
	return got
}

func TestR1_FlagsErrErrorAsDetail(t *testing.T) {
	src := `package h
func f(err error) error {
	return huma.Error400BadRequest(err.Error())
}`
	if got := findings(t, src); len(got) != 1 || got[0] != "R1" {
		t.Fatalf("want [R1], got %v", got)
	}
}

func TestR1_FlagsErrEmbeddedInSprintf(t *testing.T) {
	src := `package h
func f(err error) error {
	return huma.Error404NotFound(fmt.Sprintf("not found: %v", err.Error()))
}`
	if got := findings(t, src); len(got) != 1 || got[0] != "R1" {
		t.Fatalf("want [R1], got %v", got)
	}
}

func TestR1_IgnoresHumaVariadicError(t *testing.T) {
	src := `package h
func f(err error) error {
	return huma.Error500InternalServerError("Upload failed", err)
}`
	if got := findings(t, src); len(got) != 0 {
		t.Fatalf("want no findings, got %v", got)
	}
}

func TestR1_IgnoresWrittenDetail(t *testing.T) {
	src := `package h
func f() error {
	return huma.Error404NotFound("No invoice with that number")
}`
	if got := findings(t, src); len(got) != 0 {
		t.Fatalf("want no findings, got %v", got)
	}
}

func TestR1_FlagsErrcodeBuilderDetail(t *testing.T) {
	src := `package h
func f(err error) error {
	return errcode.Conflict(errcode.AuthEmailInUse, err.Error())
}`
	if got := findings(t, src); len(got) != 1 || got[0] != "R1" {
		t.Fatalf("want [R1], got %v", got)
	}
}

func TestR2_FlagsEmptyDetail(t *testing.T) {
	src := `package h
func f() error {
	return huma.Error400BadRequest("Request failed")
}`
	if got := findings(t, src); len(got) != 1 || got[0] != "R2" {
		t.Fatalf("want [R2], got %v", got)
	}
}

func TestR2_IsCaseAndPunctuationInsensitive(t *testing.T) {
	src := `package h
func f() error {
	return huma.Error500InternalServerError("Something went wrong.")
}`
	if got := findings(t, src); len(got) != 1 || got[0] != "R2" {
		t.Fatalf("want [R2], got %v", got)
	}
}

func TestR2_AllowsASentenceThatNamesTheCause(t *testing.T) {
	src := `package h
func f() error {
	return huma.Error503ServiceUnavailable("Email delivery is not configured — contact an administrator.")
}`
	if got := findings(t, src); len(got) != 0 {
		t.Fatalf("want no findings, got %v", got)
	}
}

func TestR2_FlagsEmptyDetailWithSpaceBeforePunctuation(t *testing.T) {
	src := `package h
func f() error {
	return huma.Error400BadRequest("Failed .")
}`
	if got := findings(t, src); len(got) != 1 || got[0] != "R2" {
		t.Fatalf("want [R2], got %v", got)
	}
}
