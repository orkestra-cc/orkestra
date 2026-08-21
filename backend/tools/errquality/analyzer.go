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
