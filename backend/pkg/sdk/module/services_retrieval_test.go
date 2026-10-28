package module

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

func TestServiceRetrievalKey(t *testing.T) {
	if ServiceRetrieval != "retrieval.service" {
		t.Fatalf("ServiceRetrieval = %q", ServiceRetrieval)
	}
}

func TestServiceRAGQueryIsDeprecated(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "services.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		vs, ok := n.(*ast.ValueSpec)
		if !ok {
			return true
		}
		for _, id := range vs.Names {
			if id.Name != "ServiceRAGQuery" {
				continue
			}
			found = true
			if vs.Doc == nil {
				t.Errorf("ServiceRAGQuery has no doc comment")
				continue
			}
			doc := vs.Doc.Text()
			if !strings.Contains(doc, "Deprecated: ") || !strings.Contains(doc, "ServiceRetrieval") {
				t.Errorf("ServiceRAGQuery doc = %q, want Deprecated: pointing at ServiceRetrieval", doc)
			}
		}
		return true
	})
	if !found {
		t.Fatal("ServiceRAGQuery not found in services.go")
	}
}
