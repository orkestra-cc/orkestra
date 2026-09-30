// Package logscope flags slog calls the compliance PolicyHandler cannot
// mask reliably (compliance spec §2.6): a value of a type the masker does not
// scan (anything outside an allowlist: strings, byte slices, errors, basic
// numbers/booleans/times, maps with basic-kind keys, slices of those, LogValuers),
// and secret-looking keys carrying a non-constant value. Both slog.Any and the
// key/value form of the logging calls (slog.Info("m", "key", v), With, ...)
// are checked. Existing calls live in baseline.txt; a new one fails CI.
package logscope

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"path/filepath"
	"strings"

	"github.com/orkestra/backend/internal/shared/redact"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/types/typeutil"
)

const (
	CategoryAnyOpaque        = "logscope.any_opaque_value"
	CategorySecretKeyDynamic = "logscope.secret_key_dynamic_value"
)

// Keys written in findings when the source has no constant key: an
// attribute whose key is computed at run time, and a value slog logs under
// its own !BADKEY because it has no key at all.
const (
	dynamicKey = "<dynamic>"
	badKey     = "!BADKEY"
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

// kvArgsFrom maps the slog functions and *slog.Logger methods taking
// alternating key/value arguments to the index of the first of them.
var kvArgsFrom = map[string]int{
	"Debug": 1, "Info": 1, "Warn": 1, "Error": 1,
	"DebugContext": 2, "InfoContext": 2, "WarnContext": 2, "ErrorContext": 2,
	"Log": 3, "With": 0, "Group": 1,
}

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
				if !ok {
					return true
				}
				callee, ok := typeutil.Callee(info, call).(*types.Func)
				if !ok || callee.Pkg() == nil || callee.Pkg().Path() != "log/slog" {
					return true
				}
				pos := fset.Position(call.Pos())
				mk := func(cat, key string) Finding {
					return Finding{Category: cat, File: relFile(pos.Filename), Line: pos.Line, Func: fn.Name.Name, Key: key}
				}
				check := func(keyExpr, valExpr ast.Expr, anyValue bool) {
					key, constKey := dynamicKey, false
					if keyTV, ok := info.Types[keyExpr]; ok && keyTV.Value != nil && keyTV.Value.Kind() == constant.String {
						key, constKey = constant.StringVal(keyTV.Value), true
					}
					valTV := info.Types[valExpr]
					if anyValue && !maskable(valTV.Type, callee.Pkg(), true) {
						out = append(out, mk(CategoryAnyOpaque, key))
					}
					// A computed key is classified by the masker at run time;
					// only a constant secret key with a dynamic value is a
					// finding the source can prove.
					if constKey && redact.IsSecretKey(key) && valTV.Value == nil {
						out = append(out, mk(CategorySecretKeyDynamic, key))
					}
				}
				if isAttrConstructor(callee) {
					if len(call.Args) == 2 {
						check(call.Args[0], call.Args[1], callee.Name() == "Any")
					}
					return true
				}
				from, ok := kvArgsFrom[callee.Name()]
				if !ok || !kvCallee(callee) || call.Ellipsis.IsValid() {
					return true
				}
				// slog's own pairing: an Attr stands alone, a string is a key
				// followed by its value, anything else is a value logged under
				// !BADKEY — which the masker sees without a key.
				for i := from; i < len(call.Args); {
					t := info.Types[call.Args[i]].Type
					switch {
					case isSlogNamed(t, "Attr"):
						i++
					case isString(t) && i+1 < len(call.Args):
						check(call.Args[i], call.Args[i+1], true)
						i += 2
					default:
						if !maskable(t, callee.Pkg(), true) {
							out = append(out, mk(CategoryAnyOpaque, badKey))
						}
						i++
					}
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
// message, not an attribute key. Group is handled with the key/value form:
// its arguments after the name are pairs or attributes.
func isAttrConstructor(fn *types.Func) bool {
	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.Recv() != nil || sig.Results().Len() != 1 || fn.Name() == "Group" {
		return false
	}
	return isSlogNamed(sig.Results().At(0).Type(), "Attr")
}

// kvCallee reports whether fn is a package-level slog function or a method
// of *slog.Logger (not, say, slog.Value.Group or a Handler method).
func kvCallee(fn *types.Func) bool {
	sig, ok := fn.Type().(*types.Signature)
	if !ok {
		return false
	}
	if sig.Recv() == nil {
		return true
	}
	ptr, ok := sig.Recv().Type().(*types.Pointer)
	return ok && isSlogNamed(ptr.Elem(), "Logger")
}

func isSlogNamed(t types.Type, name string) bool {
	named, ok := t.(*types.Named)
	return ok && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == "log/slog" && named.Obj().Name() == name
}

func isString(t types.Type) bool {
	b, ok := t.(*types.Basic)
	return ok && b.Info()&types.IsString != 0
}

// basicMapKey mirrors the masker's key rendering (log_masker.go maskReflect):
// only string, bool, integer and float keys are rendered.
func basicMapKey(t types.Type) bool {
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Info()&(types.IsString|types.IsBoolean|types.IsInteger|types.IsFloat) != 0
}

// maskable reports whether the PolicyHandler scans (or has nothing to scan
// in) a value of static type t. It is an allowlist of what the masker
// handles: string kinds, byte slices (json.RawMessage), errors, LogValuers,
// slog.Value, basic numbers and booleans, time.Time and time.Duration, maps
// with string, bool, integer or float keys, slices, arrays and pointers of
// maskable types. top=false inside a container, where an interface element is accepted: the
// masker walks the dynamic value. Everything else (structs, pointers to
// structs, other interfaces, maps with other keys, funcs, chans) is opaque.
func maskable(t types.Type, slogPkg *types.Package, top bool) bool {
	if t == nil {
		return true
	}
	// Also covers interfaces embedding error or LogValuer: every dynamic
	// value of such a type is one.
	if types.Implements(t, errorType) || implementsLogValuer(t, slogPkg) {
		return true
	}
	if isSlogNamed(t, "Value") {
		return true
	}
	if named, ok := t.(*types.Named); ok && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == "time" && named.Obj().Name() == "Time" {
		return true
	}
	switch u := t.Underlying().(type) {
	case *types.Basic:
		return u.Kind() != types.UnsafePointer && u.Kind() != types.Invalid
	case *types.Slice:
		return maskable(u.Elem(), slogPkg, false)
	case *types.Array:
		return maskable(u.Elem(), slogPkg, false)
	case *types.Map:
		// The masker renders keys of basic kinds (string, bool, integers,
		// floats) as text and masks them; a map with any other key kind is
		// replaced whole by [REDACTED], so its content is lost: flag it.
		return basicMapKey(u.Key()) && maskable(u.Elem(), slogPkg, false)
	case *types.Pointer:
		if _, isStruct := u.Elem().Underlying().(*types.Struct); isStruct {
			return false
		}
		return maskable(u.Elem(), slogPkg, top)
	case *types.Interface:
		return !top && u.Empty()
	default: // struct, func, chan, signature
		return false
	}
}

func implementsLogValuer(t types.Type, slogPkg *types.Package) bool {
	obj := slogPkg.Scope().Lookup("LogValuer")
	if obj == nil {
		return false
	}
	iface, ok := obj.Type().Underlying().(*types.Interface)
	return ok && types.Implements(t, iface)
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
	// A package that does not load or type-check would be scanned partially
	// or not at all, and the gate would pass vacuously: fail instead.
	var loadErrs []string
	packages.Visit(pkgs, nil, func(p *packages.Package) {
		for _, e := range p.Errors {
			loadErrs = append(loadErrs, e.Error())
		}
	})
	if len(loadErrs) > 0 {
		return nil, fmt.Errorf("logscope: %d package error(s):\n%s", len(loadErrs), strings.Join(loadErrs, "\n"))
	}
	var out []Finding
	for _, p := range pkgs {
		out = append(out, ScanFiles(p.Fset, p.Syntax, p.TypesInfo, rel)...)
	}
	return out, nil
}
