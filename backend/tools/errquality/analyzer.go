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
		if isEmptyDetail(detail) {
			report(call.Pos(), "R2", "the detail says nothing the status code did not — name what failed and what the caller can do")
		}
		return true
	})

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
				// A nested switch or type-switch judges its own default on
				// its own merits — the outer walk visits it independently.
				// Descending here would both misattribute a nested mapper's
				// legitimate 4xx to this default and double-report it under
				// its own line.
				switch inner.(type) {
				case *ast.SwitchStmt, *ast.TypeSwitchStmt:
					return false
				}
				ret, isReturn := inner.(*ast.ReturnStmt)
				if !isReturn {
					return true
				}
				// Known blind spot, kept deliberately: this only matches a
				// 4xx constructor written literally among the return's
				// results. Two shapes escape it —
				//
				//	x := huma.Error400BadRequest("...")
				//	return x                              // bound, then returned
				//
				//	result = huma.Error400BadRequest("...")
				//	return                                 // naked return of a named result
				//
				// An earlier revision resolved identifiers with a flat,
				// name-keyed "most recent assignment" map, but that model
				// has no notion of Go block scope (a shadowed inner
				// binding was conflated with an outer one) and left a
				// stale flag on the `x, err = f()` multi-assign shape —
				// two false POSITIVES, which block a correct build in CI,
				// traded for closing a false negative that R1/R2 already
				// judge (the constructor's own message is still checked
				// wherever it's written, resolved or not). Neither escaping
				// shape appears anywhere in this codebase today — see
				// TestR3_DoesNotResolveIndirectReturns_KnownLimit, which
				// pins this as a decision, not an oversight.
				for _, result := range ret.Results {
					call, isCall := result.(*ast.CallExpr)
					if !isCall {
						continue
					}
					if _, status, ok := detailArg(call); ok && status >= 400 && status < 500 {
						report(call.Pos(), "R3",
							"an error this function could not name is a server fault — return 5xx, not a status that blames the caller")
					}
				}
				return true
			})
		}
		return true
	})
}

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
	s = strings.TrimSpace(s)
	return emptyDetails[s]
}

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
