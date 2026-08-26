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

func TestR3_FlagsClientErrorFromErrorsIsDefault(t *testing.T) {
	src := `package h
func mapErr(err error) error {
	switch {
	case errors.Is(err, ErrInvalidCredentials):
		return huma.Error401Unauthorized("Invalid email or password")
	default:
		return huma.Error400BadRequest("Login is not available right now")
	}
}`
	got := findings(t, src)
	if len(got) != 1 || got[0] != "R3" {
		t.Fatalf("want [R3], got %v", got)
	}
}

func TestR3_AllowsServerErrorFromDefault(t *testing.T) {
	src := `package h
func mapErr(err error) error {
	switch {
	case errors.Is(err, ErrInvalidCredentials):
		return huma.Error401Unauthorized("Invalid email or password")
	default:
		return errcode.Internal(errcode.AuthUnavailable, "Sign-in is temporarily unavailable.")
	}
}`
	if got := findings(t, src); len(got) != 0 {
		t.Fatalf("want no findings, got %v", got)
	}
}

func TestR3_IgnoresSwitchWithoutErrorsIs(t *testing.T) {
	src := `package h
func pick(kind string) error {
	switch {
	case kind == "a":
		return huma.Error400BadRequest("Field a is not a valid target")
	default:
		return huma.Error400BadRequest("Unknown target kind")
	}
}`
	if got := findings(t, src); len(got) != 0 {
		t.Fatalf("want no findings, got %v", got)
	}
}

func TestR3_IgnoresNestedSwitchInsideDefault(t *testing.T) {
	src := `package h
func mapErr(err error) error {
	switch {
	case errors.Is(err, ErrInvalidCredentials):
		return huma.Error401Unauthorized("Invalid email or password")
	default:
		switch {
		case errors.Is(err, ErrTargetKindUnknown):
			return huma.Error400BadRequest("Unknown target kind")
		default:
			return errcode.Internal(errcode.AuthUnavailable, "Sign-in is temporarily unavailable.")
		}
	}
}`
	if got := findings(t, src); len(got) != 0 {
		t.Fatalf("want no findings, got %v", got)
	}
}

func TestR3_IgnoresUnreturnedClientErrorInDefault(t *testing.T) {
	src := `package h
func mapErr(err error) error {
	switch {
	case errors.Is(err, ErrInvalidCredentials):
		return huma.Error401Unauthorized("Invalid email or password")
	default:
		candidate := huma.Error400BadRequest("Unrecognized failure mode")
		slog.Default().Warn("mapErr", slog.Any("candidate", candidate))
		return errcode.Internal(errcode.AuthUnavailable, "Sign-in is temporarily unavailable.")
	}
}`
	if got := findings(t, src); len(got) != 0 {
		t.Fatalf("want no findings, got %v", got)
	}
}

// TestR3_DoesNotResolveIndirectReturns_KnownLimit pins a deliberate blind
// spot documented at the loop in inspectFile (see the comment above the
// `for _, result := range ret.Results` loop in the default-clause walk): a
// 4xx constructor bound to a variable and returned by name is not
// resolved. An earlier revision attempted identifier resolution and
// introduced two false positives (scope-blind shadowing, a stale flag on
// the multi-assign shape) for a shape that occurs nowhere in this
// codebase — this test keeps the limit a decision, not an accident.
func TestR3_DoesNotResolveIndirectReturns_KnownLimit(t *testing.T) {
	src := `package h
func mapErr(err error) error {
	switch {
	case errors.Is(err, ErrInvalidCredentials):
		return huma.Error401Unauthorized("Invalid email or password")
	default:
		result := huma.Error400BadRequest("Login is not available right now")
		return result
	}
}`
	if got := findings(t, src); len(got) != 0 {
		t.Fatalf("want no findings, got %v", got)
	}
}

func TestR3_IgnoresNestedTypeSwitchInsideDefault(t *testing.T) {
	src := `package h
func mapErr(err error) error {
	switch {
	case errors.Is(err, ErrInvalidCredentials):
		return huma.Error401Unauthorized("Invalid email or password")
	default:
		switch err.(type) {
		case *TargetError:
			return huma.Error400BadRequest("Unknown target kind")
		default:
			return errcode.Internal(errcode.AuthUnavailable, "Sign-in is temporarily unavailable.")
		}
	}
}`
	if got := findings(t, src); len(got) != 0 {
		t.Fatalf("want no findings, got %v", got)
	}
}

func TestAllowComment_SuppressesWhenReasonIsSubstantial(t *testing.T) {
	src := `package h
func f(err error) error {
	//errquality:allow surfaces the validation library's own message
	return huma.Error400BadRequest(err.Error())
}`
	fset := token.NewFileSet()
	f, perr := parser.ParseFile(fset, "handler.go", src, parser.ParseComments)
	if perr != nil {
		t.Fatalf("parse: %v", perr)
	}
	var suppressed, reported int
	inspectFile(fset, f, func(pos token.Pos, rule, _ string) {
		if hasAllowComment(fset, f, pos) {
			suppressed++
			return
		}
		reported++
	})
	if suppressed != 1 || reported != 0 {
		t.Fatalf("want 1 suppressed / 0 reported, got %d/%d", suppressed, reported)
	}
}

func TestAllowComment_RequiresAReason(t *testing.T) {
	src := `package h
func f(err error) error {
	//errquality:allow
	return huma.Error400BadRequest(err.Error())
}`
	fset := token.NewFileSet()
	f, perr := parser.ParseFile(fset, "handler.go", src, parser.ParseComments)
	if perr != nil {
		t.Fatalf("parse: %v", perr)
	}
	var reported int
	inspectFile(fset, f, func(pos token.Pos, _, _ string) {
		if !hasAllowComment(fset, f, pos) {
			reported++
		}
	})
	if reported != 1 {
		t.Fatalf("a bare allow-comment must not suppress; reported=%d", reported)
	}
}

func TestBaselineMatches_NormalizesToRepoRelativePath(t *testing.T) {
	baselineSet = map[string]bool{"internal/core/tenant/handlers/org_handler.go:214:R1": true}
	t.Cleanup(func() { baselineSet = nil })

	if !baselineMatches("/home/runner/work/orkestra/backend/internal/core/tenant/handlers/org_handler.go", 214, "R1") {
		t.Fatal("absolute CI path should normalize onto the baseline entry")
	}
	if baselineMatches("/home/runner/work/orkestra/backend/internal/core/tenant/handlers/org_handler.go", 215, "R1") {
		t.Fatal("a different line must not match")
	}
}
