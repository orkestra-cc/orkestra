# Backend Error Quality Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Land a CI analyzer that makes unreadable, dishonest backend error responses impossible to add, then fix the 60 existing core violations one module at a time.

**Architecture:** A new `go/analysis` analyzer (`backend/tools/errquality`) modelled on the existing `tools/tenantscope` — same layout, same baseline-file mechanism, same `allow`-comment opt-out, wired into `make ci-backend`. It reports three rules at client-facing error constructors. A baseline file freezes today's violations so the gate is green on day one; each burn-down commit deletes its module's lines from that file.

**Tech Stack:** Go 1.25.13, `golang.org/x/tools/go/analysis` (+ `singlechecker`, `inspect`), Huma v2, `internal/shared/errcode`, GNU make.

**Spec:** `docs/superpowers/specs/2026-08-21-backend-error-quality-design.md`

## Global Constraints

- **Every commit in this plan carries the trailer `Prop: upstream`** (ADR-0010: generic core, cherry-picked to the public repo). No commit may mix in addon or private changes.
- **Commit messages are Conventional Commits** — a pre-commit hook rejects anything else. Never write the literal `[skip ci]` in a normal commit.
- **The pre-push hook re-runs `make ci` (~8–10 min).** Push in the background, never with a 2-minute timeout.
- **Error codes are `<module>.<situation>` in snake_case**, declared in `internal/shared/errcode/codes.go`, and every new const needs a matching row in `codes_test.go`'s `goldenCodes` map in the same commit or CI fails.
- **Never hand-edit `backend/openapi/enterprise.json`** — regenerate with `make openapi-dump` (needs Mongo+Redis up).
- **Working directory for all `go` commands is `backend/`.** All paths below are repo-root relative.

**Decision table — every burn-down task applies this to every site it touches:**

| Situation at the site | Fix |
| --- | --- |
| The caller did something the caller can correct | 4xx, detail naming the field or the rule that was broken |
| A dependency or configuration is absent or down | `errcode.ServiceUnavailable(code, …)`, detail naming which one and who fixes it |
| The handler cannot name the error at all | `errcode.Internal(code, …)`, detail saying what operation failed; log the cause with `slog` |
| The detail was `err.Error()` | Move the error into the existing `slog` call (add one if absent), replace the detail with a written sentence |
| The SPA must branch on the case, not merely display it | Add an `errcode` const + a `codes_test.go` golden row + `errors.<code>` in both locale files |

---

## File Structure

**Created:**

| File | Responsibility |
| --- | --- |
| `backend/tools/errquality/analyzer.go` | The three rules + baseline/allow suppression. Exposes `Analyzer` and the pure `inspectFile` used by tests. |
| `backend/tools/errquality/analyzer_test.go` | Parses synthetic sources and asserts findings per rule. No `analysistest`, no testdata module. |
| `backend/tools/errquality/cmd/errquality/main.go` | `singlechecker` driver. |
| `backend/tools/errquality/baseline.txt` | Frozen pre-existing violations, `relpath:line:rule` per line. |
| `backend/tools/errquality/CLAUDE.md` | What the analyzer enforces and how to fix each rule. |

**Modified:**

| File | Change |
| --- | --- |
| `backend/internal/shared/errcode/errcode.go` | Add `ServiceUnavailable` + `Internal` builders. |
| `backend/internal/shared/errcode/codes.go` | Add `AuthJWTNotConfigured`, `AuthUnavailable`. |
| `backend/internal/shared/errcode/codes_test.go` | Two golden rows. |
| `Makefile` | `backend-errquality` target, added to `ci-backend`. |
| `backend/CLAUDE.md` | Document the gate under "Error-code contract". |
| `backend/internal/core/{auth,tenant,authz,user}/**` | The burn-down (Tasks 7–10). |
| `frontend-admin/src/locales/{en,it}.json` | `errors.<code>` keys for new codes. |

**Design note — why `inspectFile` is exported to the test, not the `Pass`:** `tenantscope/analyzer_test.go` emulates an `analysis.Pass` because its logic reads `pass.Files` directly. This analyzer keeps `run` thin (adapt `Pass` → `inspectFile`) so tests parse a string and call one pure function. Simpler, and it needs neither `analysistest` nor a testdata module.

---

### Task 1: 5xx builders and the two auth codes in `errcode`

R3's remedy does not exist yet: `errcode` has builders for 4xx only. This task adds the other half and the two codes the auth burn-down (Task 7) will use.

**Files:**
- Modify: `backend/internal/shared/errcode/errcode.go` (append after `UnprocessableEntity`)
- Modify: `backend/internal/shared/errcode/codes.go` (in the `--- auth ---` block)
- Test: `backend/internal/shared/errcode/codes_test.go` (the `goldenCodes` map)

**Interfaces:**
- Produces: `errcode.ServiceUnavailable(code, detail string) *errcode.Error` (503), `errcode.Internal(code, detail string) *errcode.Error` (500), `errcode.AuthJWTNotConfigured` = `"auth.jwt_not_configured"`, `errcode.AuthUnavailable` = `"auth.unavailable"`.

- [ ] **Step 1: Write the failing test** — add two rows to `goldenCodes` in `codes_test.go`, keeping the existing map order (new rows go at the end of the `auth` group):

```go
	"AuthJWTNotConfigured":             "auth.jwt_not_configured",
	"AuthUnavailable":                  "auth.unavailable",
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/shared/errcode/`
Expected: FAIL — compile error, `undefined: AuthJWTNotConfigured`.

- [ ] **Step 3: Add the consts** to `codes.go`, inside the `// --- auth ---` block, below `AuthEmailInUse`:

```go
// AuthJWTNotConfigured signals that the server cannot read its RS256
// signing keys, so no token can be issued or verified. This is a
// deployment fault, never the caller's: it answers 503, and the detail
// names the cause so an operator reading the response alone can act on
// it without tailing container logs.
const AuthJWTNotConfigured = "auth.jwt_not_configured"

// AuthUnavailable is the honest fallback for an unrecognized error on a
// password-auth path. An error the handler cannot name is a server
// fault, so it answers 500 — never a 4xx, which would blame the caller
// for the server's own gap.
const AuthUnavailable = "auth.unavailable"
```

- [ ] **Step 4: Add the builders** to `errcode.go`, after `UnprocessableEntity`:

```go
// ServiceUnavailable returns a 503 — the dependency or configuration a
// request needs is absent or down. Use it for faults an operator can
// fix (missing keys, unconfigured mailer), and say which one in detail.
func ServiceUnavailable(code, detail string) *Error {
	return New(http.StatusServiceUnavailable, code, detail)
}

// Internal returns a 500 — the server hit a state it does not model.
// The detail must stay a written sentence, never the underlying error
// text: log the cause, tell the caller what failed.
func Internal(code, detail string) *Error { return New(http.StatusInternalServerError, code, detail) }
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd backend && go test ./internal/shared/errcode/`
Expected: PASS (`TestCodesMatchGoldenSnapshot`, `TestEveryConstSnapshotted`).

- [ ] **Step 6: Commit**

```bash
git add backend/internal/shared/errcode/
git commit -F - <<'EOF'
feat(errcode): add 5xx builders and the two auth availability codes

errcode covered 4xx only, so a handler that wanted to report a server
fault honestly had no builder to reach for and fell back to huma's
text-only helpers, losing the code. Add ServiceUnavailable and Internal
plus auth.jwt_not_configured and auth.unavailable, the two situations
the password-auth path needs to stop misreporting as client errors.

Prop: upstream
EOF
```

---

### Task 2: Analyzer skeleton and R1 (raw error text)

**Files:**
- Create: `backend/tools/errquality/analyzer.go`
- Create: `backend/tools/errquality/analyzer_test.go`
- Create: `backend/tools/errquality/cmd/errquality/main.go`

**Interfaces:**
- Produces: `errquality.Analyzer` (`*analysis.Analyzer`), and the internal `inspectFile(fset *token.FileSet, f *ast.File, report func(pos token.Pos, rule, msg string))` that Tasks 3–5 extend.
- Consumes: nothing from Task 1 (independent).

**Scope decision to preserve:** R1 checks the **detail string argument only**. Huma's variadic `errs ...error` (e.g. `huma.Error500InternalServerError("Upload failed", err)`) nests the error under a separate `errors[]` field; whether that should be exposed is a wider contract question and is explicitly out of scope. Do not flag it.

- [ ] **Step 1: Write the failing test** — create `analyzer_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./tools/errquality/`
Expected: FAIL — `undefined: inspectFile`.

- [ ] **Step 3: Write the analyzer** — create `analyzer.go`:

```go
// Package errquality implements a static analyzer that keeps client-facing
// error responses honest. It reports three defects at the call sites that
// build an HTTP error:
//
//	R1 — the detail is the underlying error's text (err.Error()). That text
//	     is written for a log, not a person, and it leaks internals.
//	R2 — the detail is semantically empty ("Request failed", "Failed", …).
//	     It tells the caller nothing the status code did not.
//	R3 — a 4xx is returned from the default: branch of an errors.Is switch.
//	     An error the handler cannot name is a server fault, not the
//	     caller's; blaming the caller sends operators hunting in the wrong
//	     place. This is the rule that would have caught the 2026-08-21
//	     login incident, where unreadable JWT keys answered 400.
//
// Scope: packages under internal/. Suppression: a baseline file of
// pre-existing violations (-baseline) plus //errquality:allow <reason>
// comments for the legitimate exceptions.
package errquality

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/tools/go/analysis"
)

// Analyzer is the go/analysis Analyzer registered by the command-line runner.
var Analyzer = &analysis.Analyzer{
	Name: "errquality",
	Doc:  "reports client-facing error constructors that leak raw error text, say nothing, or report a server fault as a client error",
	URL:  "https://github.com/orkestra/orkestra/blob/main/backend/CLAUDE.md#error-code-contract",
	Run:  run,
}

var baselinePath string

func init() {
	Analyzer.Flags.StringVar(&baselinePath, "baseline", "",
		"path to a baseline file of accepted violations (format: relpath:line:rule)")
}

// allowComment silences a single diagnostic. It must sit on its own line
// directly above the flagged call and carry a reason of at least 5 chars.
const allowComment = "//errquality:allow"

// emptyDetails are details that carry no information the status code did
// not already carry. Matched case-insensitively against the whole string
// with trailing punctuation trimmed.
var emptyDetails = map[string]bool{
	"request failed":        true,
	"internal server error": true,
	"internal error":        true,
	"error":                 true,
	"failed":                true,
	"operation failed":      true,
	"invalid request":       true,
	"bad request":           true,
	"something went wrong":  true,
	"unexpected error":      true,
}

// errcodeStatus maps an errcode builder name to the status it returns, so
// R3 can tell a 4xx builder from a 5xx one.
var errcodeStatus = map[string]int{
	"BadRequest":          400,
	"Unauthorized":        401,
	"Forbidden":           403,
	"NotFound":            404,
	"Conflict":            409,
	"UnprocessableEntity": 422,
	"Internal":            500,
	"ServiceUnavailable":  503,
}

func run(pass *analysis.Pass) (any, error) {
	if !inScope(pass.Pkg.Path()) {
		return nil, nil
	}
	if err := loadBaseline(); err != nil {
		return nil, err
	}
	for _, f := range pass.Files {
		inspectFile(pass.Fset, f, func(pos token.Pos, rule, msg string) {
			p := pass.Fset.Position(pos)
			if baselineMatches(p.Filename, p.Line, rule) {
				return
			}
			if hasAllowComment(pass.Fset, f, pos) {
				return
			}
			pass.Reportf(pos, "[%s] %s", rule, msg)
		})
	}
	return nil, nil
}

// inScope reports whether the analyzer should inspect the package. Only
// internal/ is checked; tools/ (this analyzer and its siblings) never
// builds HTTP responses.
func inScope(pkgPath string) bool {
	return strings.Contains(pkgPath, "/internal/")
}

// detailArg returns the index of the human-readable detail argument for a
// client-facing error constructor, the constructor's status, and whether
// the call is one at all.
//
//	huma.ErrorNNNXxx(detail string, errs ...error)  → index 0, status NNN
//	errcode.Forbidden(code, detail string)          → index 1, status 403
//	errcode.New(status int, code, detail string)    → index 2, status unknown (0)
func detailArg(call *ast.CallExpr) (idx, status int, ok bool) {
	sel, isSel := call.Fun.(*ast.SelectorExpr)
	if !isSel {
		return 0, 0, false
	}
	pkg, isIdent := sel.X.(*ast.Ident)
	if !isIdent {
		return 0, 0, false
	}
	switch pkg.Name {
	case "huma":
		name := sel.Sel.Name
		if !strings.HasPrefix(name, "Error") || len(name) < 8 {
			return 0, 0, false
		}
		code, err := strconv.Atoi(name[5:8])
		if err != nil {
			return 0, 0, false
		}
		return 0, code, true
	case "errcode":
		if s, known := errcodeStatus[sel.Sel.Name]; known {
			return 1, s, true
		}
		if sel.Sel.Name == "New" {
			return 2, 0, true
		}
	}
	return 0, 0, false
}

// inspectFile walks one parsed file and reports every rule violation. It
// is the whole detection surface — run() only adds suppression on top, so
// tests exercise this directly with a parsed string.
func inspectFile(fset *token.FileSet, f *ast.File, report func(pos token.Pos, rule, msg string)) {
	ast.Inspect(f, func(n ast.Node) bool {
		call, isCall := n.(*ast.CallExpr)
		if !isCall {
			return true
		}
		idx, _, ok := detailArg(call)
		if !ok || idx >= len(call.Args) {
			return true
		}
		detail := call.Args[idx]
		if containsErrorText(detail) {
			report(call.Pos(), "R1", "the underlying error's text reaches the client — log err, return a written sentence")
		}
		return true
	})
}

// containsErrorText reports whether the expression evaluates (in whole or
// in part) to an error's own text: a bare err.Error() or one embedded in a
// formatting call.
func containsErrorText(e ast.Expr) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		call, isCall := n.(*ast.CallExpr)
		if !isCall {
			return true
		}
		if sel, isSel := call.Fun.(*ast.SelectorExpr); isSel &&
			sel.Sel.Name == "Error" && len(call.Args) == 0 {
			found = true
			return false
		}
		return true
	})
	return found
}
```

- [ ] **Step 4: Add stubs so the file compiles** — append to `analyzer.go` (Task 5 fills them in):

```go
var (
	baselineOnce sync.Once
	baselineSet  map[string]bool
	baselineErr  error
)

func loadBaseline() error { return nil }

func baselineMatches(absFile string, line int, rule string) bool { return false }

func hasAllowComment(fset *token.FileSet, f *ast.File, pos token.Pos) bool { return false }

var _ = bufio.NewScanner
var _ = os.Open
var _ = filepath.ToSlash
var _ = fmt.Sprintf
var _ = emptyDetails
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd backend && go test ./tools/errquality/`
Expected: PASS — 5 tests.

- [ ] **Step 6: Add the command runner** — create `cmd/errquality/main.go`:

```go
// Command errquality is the standalone runner for the errquality analyzer.
//
//	go run ./tools/errquality/cmd/errquality -baseline=tools/errquality/baseline.txt ./internal/...
//
// A non-zero exit code means one or more findings, which CI treats as a
// build failure.
package main

import (
	"github.com/orkestra/backend/tools/errquality"
	"golang.org/x/tools/go/analysis/singlechecker"
)

func main() {
	singlechecker.Main(errquality.Analyzer)
}
```

- [ ] **Step 7: Verify it runs against the real tree**

Run: `cd backend && go run ./tools/errquality/cmd/errquality ./internal/core/tenant/... 2>&1 | head -20`
Expected: ~18 `[R1]` diagnostics (tenant is where the raw-error leaks concentrate). A zero-finding run means detection is broken — investigate before continuing.

- [ ] **Step 8: Commit**

```bash
git add backend/tools/errquality/
git commit -F - <<'EOF'
feat(errquality): add the analyzer skeleton and the raw-error rule

First rule of the error-quality gate: a client-facing constructor whose
detail is err.Error() — text written for a log, handed to a person, and
carrying internals with it. 24 core call sites do this today, 18 of them
in tenant.

Detection lives in a pure inspectFile so tests parse a string and assert
findings, without emulating an analysis.Pass or standing up an
analysistest testdata module. Suppression (baseline, allow-comments) is
stubbed here and implemented next.

Prop: upstream
EOF
```

---

### Task 3: R2 — the empty detail

**Files:**
- Modify: `backend/tools/errquality/analyzer.go` (inside `inspectFile`)
- Test: `backend/tools/errquality/analyzer_test.go`

**Interfaces:**
- Consumes: `inspectFile`, `detailArg`, `emptyDetails` from Task 2.
- Produces: rule string `"R2"`.

- [ ] **Step 1: Write the failing test** — append to `analyzer_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./tools/errquality/ -run TestR2`
Expected: FAIL — `want [R2], got []`.

- [ ] **Step 3: Implement** — in `inspectFile`, after the `containsErrorText` block:

```go
		if isEmptyDetail(detail) {
			report(call.Pos(), "R2", "the detail says nothing the status code did not — name what failed and what the caller can do")
		}
```

and add the predicate:

```go
// isEmptyDetail reports whether the expression is a string literal whose
// content is in emptyDetails, compared case-insensitively with surrounding
// whitespace and trailing punctuation removed.
func isEmptyDetail(e ast.Expr) bool {
	lit, isLit := e.(*ast.BasicLit)
	if !isLit || lit.Kind != token.STRING {
		return false
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		return false
	}
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimRight(s, ".!…")
	return emptyDetails[s]
}
```

Delete the `var _ = emptyDetails` stub line from Task 2 Step 4.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd backend && go test ./tools/errquality/`
Expected: PASS — 8 tests.

- [ ] **Step 5: Commit**

```bash
git add backend/tools/errquality/
git commit -F - <<'EOF'
feat(errquality): flag details that say nothing

Second rule: a detail drawn from a denylist of phrases that repeat the
status code without adding a fact — "Request failed", "Failed",
"Something went wrong". 36 core call sites answer this way today, and
one of them is the login response that hid a missing keypair for two
hours.

Prop: upstream
EOF
```

---

### Task 4: R3 — a 4xx from the fallback branch

This is the rule that catches the incident: `mapPasswordError`'s `default:` returning `huma.Error400BadRequest("Request failed")` for an error it could not name.

**Files:**
- Modify: `backend/tools/errquality/analyzer.go`
- Test: `backend/tools/errquality/analyzer_test.go`

**Interfaces:**
- Consumes: `detailArg` (for the status), `inspectFile`.
- Produces: rule string `"R3"`.

- [ ] **Step 1: Write the failing test** — append to `analyzer_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./tools/errquality/ -run TestR3`
Expected: FAIL — `want [R3], got []`.

- [ ] **Step 3: Implement** — add a second walk inside `inspectFile`, after the existing `ast.Inspect` call:

```go
	ast.Inspect(f, func(n ast.Node) bool {
		sw, isSwitch := n.(*ast.SwitchStmt)
		if !isSwitch || sw.Tag != nil || !hasErrorsIsCase(sw) {
			return true
		}
		for _, stmt := range sw.Body.List {
			clause, isClause := stmt.(*ast.CaseClause)
			if !isClause || clause.List != nil { // List == nil marks default:
				continue
			}
			ast.Inspect(clause, func(inner ast.Node) bool {
				call, isCall := inner.(*ast.CallExpr)
				if !isCall {
					return true
				}
				if _, status, ok := detailArg(call); ok && status >= 400 && status < 500 {
					report(call.Pos(), "R3",
						"an error this function could not name is a server fault — return 5xx, not a status that blames the caller")
				}
				return true
			})
		}
		return true
	})
```

and the helper:

```go
// hasErrorsIsCase reports whether any non-default clause of a tagless
// switch tests errors.Is — the idiom that marks the switch as an error
// mapper rather than an ordinary branch.
func hasErrorsIsCase(sw *ast.SwitchStmt) bool {
	for _, stmt := range sw.Body.List {
		clause, isClause := stmt.(*ast.CaseClause)
		if !isClause {
			continue
		}
		for _, cond := range clause.List {
			call, isCall := cond.(*ast.CallExpr)
			if !isCall {
				continue
			}
			if sel, isSel := call.Fun.(*ast.SelectorExpr); isSel &&
				sel.Sel.Name == "Is" {
				if pkg, isIdent := sel.X.(*ast.Ident); isIdent && pkg.Name == "errors" {
					return true
				}
			}
		}
	}
	return false
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd backend && go test ./tools/errquality/`
Expected: PASS — 11 tests.

- [ ] **Step 5: Verify it catches the real incident site**

Run: `cd backend && go run ./tools/errquality/cmd/errquality ./internal/core/auth/... 2>&1 | grep R3`
Expected: at least one hit on `internal/core/auth/handlers/password_handler.go` — the `default:` branch of `mapPasswordError`. If this prints nothing, R3 does not match the real idiom and must be fixed before proceeding; the whole plan rests on it.

- [ ] **Step 6: Commit**

```bash
git add backend/tools/errquality/
git commit -F - <<'EOF'
feat(errquality): flag a client status returned from an error mapper's fallback

Third rule, and the one with a scalp: mapPasswordError answered 400
"Request failed" for every sentinel it did not list — including the
ErrJWTKeysNotLoaded that fires when the server cannot read its own
signing keys. A configuration fault was reported as the caller's
mistake, indistinguishable from a wrong password.

The rule matches the narrow shape of Go's error-mapping idiom: a tagless
switch whose cases test errors.Is, returning a 4xx from default.

Prop: upstream
EOF
```

---

### Task 5: Suppression — baseline file and allow-comments

**Files:**
- Modify: `backend/tools/errquality/analyzer.go` (replace the Task 2 stubs)
- Test: `backend/tools/errquality/analyzer_test.go`

**Interfaces:**
- Produces: baseline line format `relpath:line:rule` (e.g. `internal/core/tenant/handlers/org_handler.go:214:R1`), and the `//errquality:allow <reason>` comment contract.

- [ ] **Step 1: Write the failing test** — append to `analyzer_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./tools/errquality/ -run 'TestAllowComment|TestBaseline'`
Expected: FAIL — the stubs return `false` unconditionally, so suppression never happens and the baseline never matches.

- [ ] **Step 3: Replace the stubs** in `analyzer.go`:

```go
// loadBaseline reads the baseline file into baselineSet once. Blank lines
// and # comments are ignored. Entries are repo-relative so the file is
// identical on a laptop, in CI, and in a Docker build.
func loadBaseline() error {
	baselineOnce.Do(func() {
		if baselinePath == "" {
			baselineSet = map[string]bool{}
			return
		}
		f, err := os.Open(baselinePath)
		if err != nil {
			baselineErr = fmt.Errorf("errquality: open baseline %s: %w", baselinePath, err)
			return
		}
		defer f.Close()
		set := make(map[string]bool)
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			set[line] = true
		}
		if err := sc.Err(); err != nil {
			baselineErr = fmt.Errorf("errquality: read baseline: %w", err)
			return
		}
		baselineSet = set
	})
	return baselineErr
}

// baselineMatches reports whether a diagnostic is already accepted. The
// absolute path is normalized by locating the "/internal/" segment, which
// is stable across checkouts and runners.
func baselineMatches(absFile string, line int, rule string) bool {
	if len(baselineSet) == 0 {
		return false
	}
	rel := absFile
	if i := strings.Index(absFile, "/internal/"); i >= 0 {
		rel = absFile[i+1:]
	}
	rel = filepath.ToSlash(rel)
	return baselineSet[fmt.Sprintf("%s:%d:%s", rel, line, rule)]
}

// hasAllowComment reports whether the line directly above pos carries
// //errquality:allow with a reason of at least 5 characters — a bare
// marker does not suppress, so the exemption always states why.
func hasAllowComment(fset *token.FileSet, f *ast.File, pos token.Pos) bool {
	file := fset.File(pos)
	if file == nil {
		return false
	}
	line := file.Line(pos)
	for _, cg := range f.Comments {
		for _, c := range cg.List {
			if file.Line(c.Pos()) != line-1 {
				continue
			}
			if !strings.HasPrefix(c.Text, allowComment) {
				continue
			}
			reason := strings.TrimSpace(strings.TrimPrefix(c.Text, allowComment))
			reason = strings.TrimSpace(strings.TrimPrefix(reason, ":"))
			if len(reason) >= 5 {
				return true
			}
		}
	}
	return false
}
```

Delete the now-unused `var _ = bufio.NewScanner` / `os.Open` / `filepath.ToSlash` / `fmt.Sprintf` stub lines.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd backend && go test ./tools/errquality/`
Expected: PASS — 14 tests.

- [ ] **Step 5: Commit**

```bash
git add backend/tools/errquality/
git commit -F - <<'EOF'
feat(errquality): add baseline and allow-comment suppression

Two escape hatches, both deliberate. The baseline file freezes today's
violations so the gate can land green and the burn-down can proceed one
module at a time; deleting a line from it is what "fixed" means.
//errquality:allow needs a reason of at least five characters, so an
exemption always says why and reads as an audit point in review.

Both mirror tools/tenantscope, which solved the same problem for
tenant-scoping — same file format, same normalization on /internal/,
same reason requirement.

Prop: upstream
EOF
```

---

### Task 6: Freeze the baseline and land the CI gate

After this task the floor is permanent: no new violation can merge.

**Files:**
- Create: `backend/tools/errquality/baseline.txt`
- Create: `backend/tools/errquality/CLAUDE.md`
- Modify: `Makefile` (the `.PHONY` line ~170, the `ci-backend` chain ~287, and a new target beside `backend-tenantscope` ~308)
- Modify: `backend/CLAUDE.md` (the "Error-code contract" section, ~line 135)

**Interfaces:**
- Consumes: everything from Tasks 2–5.
- Produces: `make backend-errquality`, and `baseline.txt` whose per-module line counts Tasks 7–10 delete.

- [ ] **Step 1: Generate the baseline**

```bash
cd backend && go run ./tools/errquality/cmd/errquality ./internal/... 2>&1 \
  | sed -n 's#^.*/\(internal/[^:]*\):\([0-9]*\):[0-9]*: \[\([A-Z0-9]*\)\].*#\1:\2:\3#p' \
  | sort -u > tools/errquality/baseline.txt
wc -l tools/errquality/baseline.txt
```

Expected: several hundred lines (core ~60 + the addon backlog). If the file is empty the `sed` did not match the driver's `path:line:col: message` output — print a few raw diagnostics and adjust before continuing.

- [ ] **Step 2: Prepend the header** to `baseline.txt`:

```
# errquality analyzer baseline — pre-existing violations exempted from CI.
#
# One "relpath:line:rule" per line. CI fails on any violation NOT listed
# here, so new code is held to the rule while the backlog is burned down.
#
# Deleting lines is the unit of progress: each burn-down commit removes
# its module's entries. internal/core/ must reach zero (see the design
# doc); internal/addons/ is frozen here on purpose and burns down in a
# later Prop: addon round.
#
# Line numbers move when a file is edited. If a rebase leaves stale
# entries, regenerate with:
#   go run ./tools/errquality/cmd/errquality ./internal/... 2>&1 \
#     | sed -n 's#^.*/\(internal/[^:]*\):\([0-9]*\):[0-9]*: \[\([A-Z0-9]*\)\].*#\1:\2:\3#p' \
#     | sort -u
# — but never regenerate to silence a NEW violation. Fix that one.
```

- [ ] **Step 3: Verify the gate is green**

Run: `cd backend && go run ./tools/errquality/cmd/errquality -baseline=tools/errquality/baseline.txt ./internal/...`
Expected: no output, exit 0.

- [ ] **Step 4: Verify the gate actually bites** — temporarily append to any file under `internal/core/user/handlers/`:

```go
func errqualityCanary(err error) error { return huma.Error400BadRequest(err.Error()) }
```

Run the same command. Expected: one `[R1]` diagnostic, exit non-zero. **Delete the canary** and confirm the run is green again.

- [ ] **Step 5: Wire it into make** — in the root `Makefile`, add `backend-errquality` to the `.PHONY` line that already lists `backend-tenantscope`, insert it into the `ci-backend` chain right after `backend-tenantscope`, and add the target next to `backend-tenantscope`:

```make
# backend-errquality fails on any client-facing error response that leaks
# raw error text, says nothing, or reports a server fault as a client
# error. Pre-existing violations are frozen in tools/errquality/baseline.txt;
# new ones fail the build. See docs/superpowers/specs/2026-08-21-backend-error-quality-design.md.
backend-errquality:
	@cd backend && go test ./tools/errquality/...
	@cd backend && go run ./tools/errquality/cmd/errquality \
	  -baseline=tools/errquality/baseline.txt ./internal/...
```

- [ ] **Step 6: Write `backend/tools/errquality/CLAUDE.md`** — cover: what each rule means, how to fix it (reproduce the decision table from **Global Constraints** verbatim), when `//errquality:allow` is legitimate, how the baseline works and why regenerating it to hide a new finding is the one forbidden move.

- [ ] **Step 7: Document the gate** in `backend/CLAUDE.md`, appended to the "Error-code contract" section:

```markdown
`make ci-backend` runs the **errquality** analyzer over `internal/`: a client-facing error may not pass `err.Error()` as its detail (R1), may not use a detail that repeats the status code — "Request failed", "Failed" — (R2), and may not return a 4xx from the `default:` branch of an `errors.Is` switch (R3), because an error the handler cannot name is the server's fault, not the caller's. Pre-existing violations are frozen in `tools/errquality/baseline.txt` and burned down per module; a genuine exception carries `//errquality:allow <reason>`.
```

- [ ] **Step 8: Run the full backend gate**

Run: `make ci-backend`
Expected: `Backend CI: OK`.

- [ ] **Step 9: Commit**

```bash
git add backend/tools/errquality/ backend/CLAUDE.md Makefile
git commit -F - <<'EOF'
feat(ci): gate backend error quality from make, with today's backlog frozen

The analyzer now runs in ci-backend beside tenantscope. Existing
violations are baselined — core's ~60 and the addon backlog — so the gate
lands green and holds the floor while the burn-down proceeds module by
module. Anything new fails the build.

Verified by canary: an added huma.Error400BadRequest(err.Error()) fails
the run, and its removal restores exit 0.

Prop: upstream
EOF
```

---

### Task 7: Burn down `auth` (18 sites) — and close the incident

**Files:**
- Modify: `backend/internal/core/auth/handlers/password_handler.go` (`mapPasswordError`, ~line 428–488; the `codedError` type, ~line 417)
- Modify: other `internal/core/auth/handlers/*.go` files carrying R2 sites (enumerate from the baseline, see Step 1)
- Modify: `backend/tools/errquality/baseline.txt` (delete the `internal/core/auth/` lines)
- Modify: `frontend-admin/src/locales/en.json`, `frontend-admin/src/locales/it.json`
- Test: `backend/internal/core/auth/handlers/error_mapping_test.go`

**Interfaces:**
- Consumes: `errcode.ServiceUnavailable`, `errcode.Internal`, `errcode.AuthJWTNotConfigured`, `errcode.AuthUnavailable` (Task 1); `services.ErrJWTKeysNotLoaded` (exists, `internal/core/auth/services/jwt_service.go:20`).

- [ ] **Step 1: List this module's sites**

```bash
grep '^internal/core/auth/' backend/tools/errquality/baseline.txt
```

For each, apply the decision table in **Global Constraints**.

- [ ] **Step 2: Write the failing tests** — append to `error_mapping_test.go`:

```go
func TestMapPasswordError_JWTKeysNotLoadedIsUnavailable(t *testing.T) {
	err := mapPasswordError(services.ErrJWTKeysNotLoaded)
	if got := statusOf(t, err); got != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", got)
	}
	var ce *errcode.Error
	if !errors.As(err, &ce) {
		t.Fatalf("expected *errcode.Error, got %T", err)
	}
	if ce.Code != errcode.AuthJWTNotConfigured {
		t.Fatalf("code = %q, want %q", ce.Code, errcode.AuthJWTNotConfigured)
	}
	if !strings.Contains(strings.ToLower(ce.Detail), "sign") &&
		!strings.Contains(strings.ToLower(ce.Detail), "key") {
		t.Fatalf("detail must name the cause, got %q", ce.Detail)
	}
}

func TestMapPasswordError_UnknownErrorIsServerFault(t *testing.T) {
	err := mapPasswordError(errors.New("something the handler has never seen"))
	if got := statusOf(t, err); got != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 — an unnamed error is not the caller's fault", got)
	}
	var ce *errcode.Error
	if !errors.As(err, &ce) || ce.Code != errcode.AuthUnavailable {
		t.Fatalf("want code %q, got %#v", errcode.AuthUnavailable, err)
	}
}
```

Add the import `"github.com/orkestra/backend/internal/shared/errcode"`.

- [ ] **Step 3: Run tests to verify they fail**

Run: `cd backend && go test ./internal/core/auth/handlers/ -run TestMapPasswordError`
Expected: FAIL — status 400 for both.

- [ ] **Step 4: Fix `mapPasswordError`** — add the case before `default:` and rewrite `default:`:

```go
	case errors.Is(err, services.ErrJWTKeysNotLoaded):
		return errcode.ServiceUnavailable(errcode.AuthJWTNotConfigured,
			"Sign-in is unavailable: the server cannot read its signing keys. An administrator must restore them.")
	default:
		slog.Default().Error("unmapped password auth error", slog.String("error", err.Error()))
		return errcode.Internal(errcode.AuthUnavailable,
			"Sign-in failed for an unexpected reason. The failure has been logged for an administrator.")
	}
```

Note the log level moves from `Warn` to `Error`: reaching this branch is now, by construction, a gap in the mapping.

- [ ] **Step 5: Delete the duplicated `codedError`** — remove the `type codedError struct` block and its two methods (~line 417–428), and rewrite every `&codedError{Status: …, Code: …}` literal in the file as the matching `errcode` builder, e.g.

```go
	case errors.Is(err, services.ErrRegistrationDisabled):
		return errcode.Forbidden(errcode.AuthRegistrationDisabled,
			"Self-service registration is disabled for this surface. Contact an administrator.")
```

Each such literal's `Code` string becomes a new const in `codes.go` **plus a golden row** in `codes_test.go` (same commit — CI enforces it). Existing strings to promote: `registration_disabled`, `email_domain_not_allowed`, `login_disabled`, `country_blocked`, `email_not_verified` — namespace them as `auth.<situation>`, keeping the wire value identical is **not** required here since these codes were never namespaced; changing them is a deliberate contract change, so update `statusOf`/code assertions in `error_mapping_test.go` and the SPA keys together.

- [ ] **Step 6: Run tests to verify they pass**

Run: `cd backend && go test ./internal/core/auth/...`
Expected: PASS.

- [ ] **Step 7: Fix the remaining R2 sites** listed in Step 1, applying the decision table.

- [ ] **Step 8: Delete this module's baseline lines**

```bash
sed -i '\#^internal/core/auth/#d' backend/tools/errquality/baseline.txt
cd backend && go run ./tools/errquality/cmd/errquality \
  -baseline=tools/errquality/baseline.txt ./internal/core/auth/...
```

Expected: no output, exit 0.

- [ ] **Step 9: Add the SPA translations** — for every new code, add `errors.<code>` to both `frontend-admin/src/locales/en.json` and `it.json`, e.g.

```json
"errors": {
  "auth.jwt_not_configured": "Sign-in is unavailable: the server cannot read its signing keys. Contact an administrator.",
  "auth.unavailable": "Sign-in failed unexpectedly. The problem has been logged."
}
```

Run: `cd frontend-admin && npm run test -- src/locales` — the EN/IT parity test must pass.

- [ ] **Step 10: Regenerate the OpenAPI spec**

Run: `cd backend && make openapi-dump` (infra must be up: `docker compose -f docker/docker-compose.infra.yml up -d`)
Expected: `enterprise.json` diff limited to the auth error responses.

- [ ] **Step 11: Run the full gate**

Run: `make ci`
Expected: `Backend CI: OK` and `Frontend-admin CI: OK`.

- [ ] **Step 12: Commit**

```bash
git add backend/internal/core/auth backend/internal/shared/errcode backend/tools/errquality/baseline.txt backend/openapi/enterprise.json frontend-admin/src/locales
git commit -F - <<'EOF'
fix(auth): report configuration faults as server faults, not bad requests

A backend that cannot read its signing keys answered every login with
400 "Request failed" — the caller's fault, said the status, and
indistinguishable from a wrong password. It now answers 503
auth.jwt_not_configured with a sentence naming the cause, and an error
the mapper cannot name answers 500 auth.unavailable instead of blaming
the caller for the server's gap.

Drops the private codedError struct: errcode does the same job for the
whole backend, and the duplicate is why this file drifted from the
convention in the first place. Clears auth from the errquality baseline.

Prop: upstream
EOF
```

---

### Task 8: Burn down `tenant` (18 raw-error sites)

**Files:**
- Modify: the `internal/core/tenant/` files listed in the baseline
- Modify: `backend/tools/errquality/baseline.txt`
- Test: the module's existing handler tests

**Interfaces:**
- Consumes: `errcode.Internal` / `errcode.ServiceUnavailable` (Task 1) and the decision table in **Global Constraints**.

- [ ] **Step 1: List the sites**

```bash
grep '^internal/core/tenant/' backend/tools/errquality/baseline.txt
```

- [ ] **Step 2: Write a failing test for the first handler you touch** — assert the status and that the response detail does **not** contain the underlying error text:

```go
func TestCreateOrg_DuplicateSlugDoesNotLeakDriverText(t *testing.T) {
	_, err := h.CreateOrg(ctx, &models.CreateOrgInput{Body: dupBody})
	var se huma.StatusError
	if !errors.As(err, &se) {
		t.Fatalf("want huma.StatusError, got %T", err)
	}
	if strings.Contains(err.Error(), "E11000") {
		t.Fatalf("Mongo driver text reached the client: %q", err.Error())
	}
}
```

- [ ] **Step 3: Run it to verify it fails**

Run: `cd backend && go test ./internal/core/tenant/... -run DoesNotLeak`
Expected: FAIL — the driver's duplicate-key text is in the detail.

- [ ] **Step 4: Fix the sites**, moving each `err` into a `slog` call and writing a sentence for the client. Where the SPA must discriminate (duplicate slug, membership conflict), add an `errcode` const + golden row + `errors.<code>` translations.

- [ ] **Step 5: Run tests**

Run: `cd backend && go test ./internal/core/tenant/...`
Expected: PASS.

- [ ] **Step 6: Clear the baseline and verify**

```bash
sed -i '\#^internal/core/tenant/#d' backend/tools/errquality/baseline.txt
cd backend && go run ./tools/errquality/cmd/errquality \
  -baseline=tools/errquality/baseline.txt ./internal/core/tenant/...
```

Expected: no output, exit 0.

- [ ] **Step 7: Regenerate OpenAPI if any status changed**

Run: `cd backend && make openapi-dump`

- [ ] **Step 8: Run the gate and commit**

Run: `make ci-backend`

```bash
git add backend/internal/core/tenant backend/tools/errquality/baseline.txt backend/openapi/enterprise.json
git commit -F - <<'EOF'
fix(tenant): stop handing Mongo's error text to API callers

Eighteen tenant endpoints passed err.Error() straight through as the
response detail — driver strings like E11000 duplicate key, written for
a log and read by an operator who cannot act on them, carrying index and
collection names with them. The cause now goes to slog and the caller
gets a sentence.

Clears tenant from the errquality baseline.

Prop: upstream
EOF
```

---

### Task 9: Burn down `authz` (5 raw-error sites)

**Files:**
- Modify: the `internal/core/authz/` files listed in the baseline
- Modify: `backend/tools/errquality/baseline.txt`
- Test: the module's existing handler tests

- [ ] **Step 1: List the sites**

```bash
grep '^internal/core/authz/' backend/tools/errquality/baseline.txt
```

- [ ] **Step 2: Write a failing test** for the first site, asserting the detail carries no policy-engine internals:

```go
func TestBindRole_UnknownRoleDoesNotLeakEngineText(t *testing.T) {
	_, err := h.CreateBinding(ctx, &models.CreateBindingInput{Body: badRole})
	if strings.Contains(err.Error(), "cedar") {
		t.Fatalf("policy-engine text reached the client: %q", err.Error())
	}
}
```

- [ ] **Step 3: Run it to verify it fails**

Run: `cd backend && go test ./internal/core/authz/... -run DoesNotLeak`
Expected: FAIL.

- [ ] **Step 4: Fix the five sites** using the decision table in **Global Constraints**. An unknown role or permission is the caller's error — 400 naming the value that was rejected; a Cedar evaluation failure is the server's — `errcode.Internal`.

- [ ] **Step 5: Run tests, clear the baseline, verify**

```bash
cd backend && go test ./internal/core/authz/...
sed -i '\#^internal/core/authz/#d' tools/errquality/baseline.txt
go run ./tools/errquality/cmd/errquality -baseline=tools/errquality/baseline.txt ./internal/core/authz/...
```

Expected: tests PASS, analyzer silent.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/core/authz backend/tools/errquality/baseline.txt
git commit -F - <<'EOF'
fix(authz): keep policy-engine internals out of authorization errors

Five binding and permission endpoints returned the evaluator's own error
text. A caller who names a role that does not exist gets told which
value was rejected; an evaluation failure is the server's problem and
says so with a 500, instead of dressing an internal fault as a bad
request.

Clears authz from the errquality baseline.

Prop: upstream
EOF
```

---

### Task 10: Burn down `user` (19 empty-detail sites) and close the core baseline

**Files:**
- Modify: the `internal/core/user/` files listed in the baseline
- Modify: `backend/tools/errquality/baseline.txt`
- Test: `backend/internal/core/user/handlers/*_test.go`

- [ ] **Step 1: List the sites**

```bash
grep '^internal/core/user/' backend/tools/errquality/baseline.txt
```

- [ ] **Step 2: Write a failing test** asserting a named detail on the most-used path:

```go
func TestUpdateUser_RejectionNamesTheField(t *testing.T) {
	_, err := h.UpdateUser(ctx, &models.UpdateUserInput{ID: id, Body: badBody})
	var se huma.StatusError
	if !errors.As(err, &se) {
		t.Fatalf("want huma.StatusError, got %T", err)
	}
	detail := strings.ToLower(err.Error())
	for _, empty := range []string{"request failed", "failed", "invalid request"} {
		if detail == empty {
			t.Fatalf("detail says nothing: %q", err.Error())
		}
	}
}
```

- [ ] **Step 3: Run it to verify it fails**

Run: `cd backend && go test ./internal/core/user/... -run NamesTheField`
Expected: FAIL.

- [ ] **Step 4: Rewrite the 19 details**, each naming what failed. `user` already uses `errcode` in 15 places — follow the file's own precedent (`errcode.Forbidden(errcode.UserSelfDeleteForbidden, "You cannot delete your own account")`) rather than introducing a second style.

- [ ] **Step 5: Run tests and clear the last core entries**

```bash
cd backend && go test ./internal/core/user/...
sed -i '\#^internal/core/user/#d' tools/errquality/baseline.txt
grep -c '^internal/core/' tools/errquality/baseline.txt
```

Expected: tests PASS, and the final `grep -c` prints **0** — the definition of done from the spec.

- [ ] **Step 6: Verify the whole core tree is clean**

Run: `cd backend && go run ./tools/errquality/cmd/errquality -baseline=tools/errquality/baseline.txt ./internal/core/...`
Expected: no output, exit 0.

- [ ] **Step 7: Update the baseline header** — replace the line about core reaching zero with a statement that it has, and that remaining entries are the addon backlog awaiting its `Prop: addon` round.

- [ ] **Step 8: Run the full gate**

Run: `make ci`
Expected: `Backend CI: OK`, `Frontend-admin CI: OK`.

- [ ] **Step 9: Commit**

```bash
git add backend/internal/core/user backend/tools/errquality/baseline.txt backend/openapi/enterprise.json
git commit -F - <<'EOF'
fix(user): give every rejection a detail that names what failed

Nineteen user endpoints answered with a phrase that repeated the status
code and added nothing — "Request failed", "Invalid request". They now
name the field or the rule, following the errcode style this module
already used in fifteen other places.

internal/core/ is now clear of the errquality baseline; what remains is
the addon backlog, frozen for its own round.

Prop: upstream
EOF
```

---

## Definition of done

- `make ci-backend` runs `backend-errquality`; a new violation fails the build (proved by the Task 6 canary).
- `grep -c '^internal/core/' backend/tools/errquality/baseline.txt` prints `0`.
- `mapPasswordError(services.ErrJWTKeysNotLoaded)` returns **503** with code `auth.jwt_not_configured`, asserted by a test.
- No `internal/core/` handler passes `err.Error()` to a client.
- Addon entries remain in the baseline, deferred to a `Prop: addon` round.
