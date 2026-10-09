package middleware

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEveryPrincipalStampAnnotates fails when a function in this package
// stamps ctxauth.KeyUserUUID (a new auth path) without calling
// annotatePrincipal, or stamps AudienceContextKey without SetAudience: the
// http_request line would silently lose tenant and user again.
func TestEveryPrincipalStampAnnotates(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		file, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			var body *ast.BlockStmt
			var label string
			switch fn := n.(type) {
			case *ast.FuncDecl:
				body, label = fn.Body, fn.Name.Name
			case *ast.FuncLit:
				body, label = fn.Body, "func literal"
			default:
				return true
			}
			if body == nil {
				return true
			}
			text := string(src[fset.Position(body.Pos()).Offset:fset.Position(body.End()).Offset])
			if strings.Contains(text, "ctxauth.KeyUserUUID, claims") && !strings.Contains(text, "annotatePrincipal(") {
				t.Errorf("%s: %s stamps ctxauth.KeyUserUUID without annotatePrincipal", name, label)
			}
			if strings.Contains(text, "AudienceContextKey, aud") && !strings.Contains(text, "SetAudience(") {
				t.Errorf("%s: %s stamps AudienceContextKey without SetAudience", name, label)
			}
			return true
		})
	}
}
