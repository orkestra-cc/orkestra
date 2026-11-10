package compliance

import (
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strconv"
	"testing"
)

// routeGuard is what the middleware of one route group in RegisterRoutes
// requires, read from the source.
type routeGuard struct {
	permissions []string
	stepUp      bool
}

// groupGuards parses module.go and returns, per handlers.Register* call, the
// guard of the Group(func(r chi.Router){…}) literal that contains it.
func groupGuards(t *testing.T) map[string]routeGuard {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "module.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]routeGuard{}
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.FuncLit)
		if !ok || len(lit.Type.Params.List) != 1 {
			return true
		}
		var g routeGuard
		var registers []string
		ast.Inspect(lit.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			switch {
			case sel.Sel.Name == "RequireSystemPermission" && len(call.Args) == 1:
				if bl, ok := call.Args[0].(*ast.BasicLit); ok {
					p, _ := strconv.Unquote(bl.Value)
					g.permissions = append(g.permissions, p)
				}
			case sel.Sel.Name == "RequireStepUp":
				g.stepUp = true
			}
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == "handlers" && len(sel.Sel.Name) > 8 && sel.Sel.Name[:8] == "Register" {
				registers = append(registers, sel.Sel.Name)
			}
			return true
		})
		for _, r := range registers {
			out[r] = g
		}
		return true
	})
	return out
}

// TestAdminDSRRoutesAreGuarded locks the permission model of the admin DSR
// surface: writes (open, approve, execute, reject, close, stored-export create
// and download) need dsr.manage and a fresh step-up; the lists, the receipt and
// the preview need only audit.read and no step-up.
func TestAdminDSRRoutesAreGuarded(t *testing.T) {
	guards := groupGuards(t)
	for _, fn := range []string{
		"RegisterErasureRequestAdminWriteRoutes", // open, approve, execute, reject, close
		"RegisterDSRExportWriteRoutes",           // create, download
	} {
		g, ok := guards[fn]
		if !ok {
			t.Fatalf("%s is not mounted in a guarded group", fn)
		}
		if !slices.Equal(g.permissions, []string{"system.compliance.dsr.manage"}) || !g.stepUp {
			t.Errorf("%s guard = %+v, want dsr.manage + step-up", fn, g)
		}
	}
	for _, fn := range []string{
		"RegisterErasureRequestAdminReadRoutes", // list, receipt
		"RegisterDSRExportReadRoutes",           // export list
		"RegisterDSRPreviewRoutes",              // preview
	} {
		g, ok := guards[fn]
		if !ok {
			t.Fatalf("%s is not mounted in a guarded group", fn)
		}
		if !slices.Equal(g.permissions, []string{"system.compliance.audit.read"}) || g.stepUp {
			t.Errorf("%s guard = %+v, want audit.read without step-up", fn, g)
		}
	}
}
