package logscope

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"sort"
	"testing"
)

const sample = `package sample

import (
	"errors"
	"log/slog"
)

type user struct{ Email string }

func f(u user, p *user, v any, tok string) {
	slog.Info("x",
		slog.Any("user", u),                 // struct: flagged
		slog.Any("ptr", p),                  // pointer to struct: flagged
		slog.Any("anything", v),             // interface other than error: flagged
		slog.Any("err", errors.New("e")),    // error: ok
		slog.Any("tags", []string{"a"}),     // slice: ok
		slog.Any("meta", map[string]any{}),  // map: ok (masked recursively)
		slog.String("token", tok),           // secret key, dynamic value: flagged
		slog.String("token_type", "bearer"), // secret key, constant value: ok
		slog.String("module", tok),          // not a secret key: ok
	)
	slog.Warn("token refresh failed", errors.New("e")) // message, not a key: ok
}
`

func TestScanFiles(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "sample.go", sample, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Uses: map[*ast.Ident]types.Object{}, Defs: map[*ast.Ident]types.Object{}}
	conf := types.Config{Importer: importer.ForCompiler(fset, "source", nil)}
	if _, err := conf.Check("sample", fset, []*ast.File{file}, info); err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, f := range ScanFiles(fset, []*ast.File{file}, info, func(p string) string { return p }) {
		got = append(got, f.BaselineKey())
	}
	sort.Strings(got)
	want := []string{
		"logscope.any_opaque_value:sample.go:f:anything",
		"logscope.any_opaque_value:sample.go:f:ptr",
		"logscope.any_opaque_value:sample.go:f:user",
		"logscope.secret_key_dynamic_value:sample.go:f:token",
	}
	if len(got) != len(want) {
		t.Fatalf("findings = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("findings = %v, want %v", got, want)
		}
	}
}
