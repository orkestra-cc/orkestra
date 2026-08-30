package handlers

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

// TestCallbackURLBuilders_StructuralScan polices the contract at the
// syntax-tree level across every non-test file of this package:
//   - no string literal that names a callback path (/auth/callback,
//     /user/security) may live outside oauth_callback_redirect.go — every
//     redirect goes through the builders;
//   - inside the builder file, no url.Values key may be access_token,
//     refresh_token, email or user_id, whether set via .Set/.Add or as a
//     composite-literal key;
//   - no string literal anywhere may mention a callback path together with
//     one of those names (the legacy fmt.Sprintf shape).
func TestCallbackURLBuilders_StructuralScan(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool { return !strings.HasSuffix(fi.Name(), "_test.go") }, 0)
	if err != nil {
		t.Fatal(err)
	}
	forbidden := map[string]bool{"access_token": true, "refresh_token": true, "email": true, "user_id": true}
	callbackPaths := []string{"/auth/callback", "/user/security"}
	const builderFile = "oauth_callback_redirect.go"

	for _, pkg := range pkgs {
		for filename, file := range pkg.Files {
			base := filename[strings.LastIndex(filename, "/")+1:]
			ast.Inspect(file, func(n ast.Node) bool {
				switch node := n.(type) {
				case *ast.BasicLit:
					if node.Kind != token.STRING {
						return true
					}
					lit, _ := strconv.Unquote(node.Value)
					for _, p := range callbackPaths {
						if !strings.Contains(lit, p) {
							continue
						}
						if base != builderFile {
							t.Errorf("%s: callback path literal %q outside %s", fset.Position(node.Pos()), lit, builderFile)
						}
						for name := range forbidden {
							if strings.Contains(lit, name) {
								t.Errorf("%s: callback literal %q carries forbidden parameter %q", fset.Position(node.Pos()), lit, name)
							}
						}
					}
				case *ast.CallExpr:
					if base != builderFile {
						return true
					}
					sel, ok := node.Fun.(*ast.SelectorExpr)
					if !ok || (sel.Sel.Name != "Set" && sel.Sel.Name != "Add") || len(node.Args) == 0 {
						return true
					}
					if key, ok := node.Args[0].(*ast.BasicLit); ok && key.Kind == token.STRING {
						k, _ := strconv.Unquote(key.Value)
						if forbidden[k] {
							t.Errorf("%s: builder sets forbidden parameter %q", fset.Position(node.Pos()), k)
						}
					}
				case *ast.CompositeLit:
					if base != builderFile {
						return true
					}
					for _, elt := range node.Elts {
						kv, ok := elt.(*ast.KeyValueExpr)
						if !ok {
							continue
						}
						if key, ok := kv.Key.(*ast.BasicLit); ok && key.Kind == token.STRING {
							k, _ := strconv.Unquote(key.Value)
							if forbidden[k] {
								t.Errorf("%s: builder literal keys forbidden parameter %q", fset.Position(node.Pos()), k)
							}
						}
					}
				}
				return true
			})
		}
	}
}
