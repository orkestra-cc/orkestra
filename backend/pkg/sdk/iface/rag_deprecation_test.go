package iface

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// typeDocs returns the doc text of every type declared in file.
func typeDocs(t *testing.T, file string) map[string]string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	docs := map[string]string{}
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, s := range gd.Specs {
			ts := s.(*ast.TypeSpec)
			doc := ts.Doc
			if doc == nil {
				doc = gd.Doc
			}
			if doc != nil {
				docs[ts.Name.Name] = doc.Text()
			}
		}
	}
	return docs
}

func TestLegacyRAGContractIsDeprecated(t *testing.T) {
	want := map[string]string{
		"SourceRef":        "rag_types.go",
		"QueryMeta":        "rag_types.go",
		"RAGQueryResponse": "rag_types.go",
		"RAGQueryProvider": "interfaces.go",
	}
	cache := map[string]map[string]string{}
	for name, file := range want {
		if cache[file] == nil {
			cache[file] = typeDocs(t, file)
		}
		doc := cache[file][name]
		hasLine := false
		for _, l := range strings.Split(doc, "\n") {
			if strings.HasPrefix(l, "Deprecated: ") {
				hasLine = true
			}
		}
		if !hasLine {
			t.Errorf("%s: doc has no line starting \"Deprecated: \"", name)
		}
		if !strings.Contains(doc, "iface.RetrievalSearch") {
			t.Errorf("%s: doc does not mention iface.RetrievalSearch", name)
		}
	}
}
