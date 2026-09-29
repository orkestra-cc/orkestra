// Package logscope flags slog calls the compliance PolicyHandler cannot
// mask reliably (compliance spec §2.6): slog.Any with an opaque value (a
// struct, a pointer to one, or an interface other than error), and
// secret-looking keys carrying a non-constant value. Existing calls live in
// baseline.txt; a new one fails CI.
package logscope

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"path/filepath"

	"github.com/orkestra/backend/internal/shared/redact"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/types/typeutil"
)

const (
	CategoryAnyOpaque        = "logscope.any_opaque_value"
	CategorySecretKeyDynamic = "logscope.secret_key_dynamic_value"
)

type Finding struct {
	Category string
	File     string
	Line     int
	Func     string
	Key      string
}

// BaselineKey omits the line number so unrelated edits do not churn the
// baseline.
func (f Finding) BaselineKey() string {
	return f.Category + ":" + f.File + ":" + f.Func + ":" + f.Key
}

var errorType = types.Universe.Lookup("error").Type().Underlying().(*types.Interface)

// ScanFiles inspects already type-checked files. relFile maps an absolute
// file name to the path written in findings.
func ScanFiles(fset *token.FileSet, files []*ast.File, info *types.Info, relFile func(string) string) []Finding {
	var out []Finding
	for _, file := range files {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || len(call.Args) != 2 {
					return true
				}
				callee, ok := typeutil.Callee(info, call).(*types.Func)
				if !ok || callee.Pkg() == nil || callee.Pkg().Path() != "log/slog" {
					return true
				}
				if !isAttrConstructor(callee) {
					return true
				}
				keyTV, ok := info.Types[call.Args[0]]
				if !ok || keyTV.Value == nil || keyTV.Value.Kind() != constant.String {
					return true
				}
				key := constant.StringVal(keyTV.Value)
				pos := fset.Position(call.Pos())
				mk := func(cat string) Finding {
					return Finding{Category: cat, File: relFile(pos.Filename), Line: pos.Line, Func: fn.Name.Name, Key: key}
				}
				valTV := info.Types[call.Args[1]]
				if callee.Name() == "Any" && opaque(valTV.Type) {
					out = append(out, mk(CategoryAnyOpaque))
				}
				if redact.IsSecretKey(key) && valTV.Value == nil {
					out = append(out, mk(CategorySecretKeyDynamic))
				}
				return true
			})
		}
	}
	return out
}

// isAttrConstructor reports whether fn is one of slog's key/value attribute
// constructors (String, Int, Any, ...). Logging functions and methods such as
// slog.Info("msg", err) also take two arguments, but their first one is a
// message, not an attribute key. Group is skipped too: its second argument is
// a nested attribute and the masker walks group members by their own keys.
func isAttrConstructor(fn *types.Func) bool {
	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.Recv() != nil || sig.Results().Len() != 1 || fn.Name() == "Group" {
		return false
	}
	named, ok := sig.Results().At(0).Type().(*types.Named)
	return ok && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == "log/slog" && named.Obj().Name() == "Attr"
}

// opaque reports values the masker passes through unchanged: structs,
// pointers to structs, and interfaces other than error.
func opaque(t types.Type) bool {
	if t == nil {
		return false
	}
	switch u := t.Underlying().(type) {
	case *types.Struct:
		return true
	case *types.Pointer:
		_, isStruct := u.Elem().Underlying().(*types.Struct)
		return isStruct
	case *types.Interface:
		return !types.Implements(t, errorType) || u.NumMethods() == 0
	default:
		return false
	}
}

// Scan loads the packages matching patterns (relative to dir) and scans
// their non-test files.
func Scan(dir string, patterns []string) ([]Finding, error) {
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo,
		Dir:  dir,
	}
	pkgs, err := packages.Load(cfg, patterns...)
	if err != nil {
		return nil, err
	}
	abs, _ := filepath.Abs(dir)
	rel := func(p string) string {
		if r, err := filepath.Rel(abs, p); err == nil {
			return filepath.ToSlash(r)
		}
		return p
	}
	var out []Finding
	for _, p := range pkgs {
		out = append(out, ScanFiles(p.Fset, p.Syntax, p.TypesInfo, rel)...)
	}
	return out, nil
}
