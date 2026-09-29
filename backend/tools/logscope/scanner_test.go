package logscope

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

const sample = `package sample

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"
)

type user struct{ Email string }

type email string

type valuer struct{}

func (valuer) LogValue() slog.Value { return slog.StringValue("x") }

type codedErr interface {
	error
	Code() int
}

func f(ctx context.Context, u user, p *user, v any, tok string, l *slog.Logger, e email, ce codedErr) {
	slog.Info("x",
		slog.Any("user", u),                 // struct: flagged
		slog.Any("ptr", p),                  // pointer to struct: flagged
		slog.Any("anything", v),             // interface other than error: flagged
		slog.Any("err", errors.New("e")),    // error: ok
		slog.Any("coded", ce),               // interface embedding error: ok
		slog.Any("tags", []string{"a"}),     // slice: ok
		slog.Any("meta", map[string]any{}),  // map: ok (masked recursively)
		slog.Any("to", e),                   // named string: ok
		slog.Any("raw", json.RawMessage{}),  // byte slice: ok
		slog.Any("strp", &tok),              // pointer to string: ok
		slog.Any("when", time.Now()),        // time.Time: ok
		slog.Any("val", valuer{}),           // LogValuer: ok
		slog.Any("users", []user{u}),        // slice of structs: flagged
		slog.Any("byid", map[int]string{}),  // map with non-string keys: flagged
		slog.Any("umap", map[string]user{}), // map of structs: flagged
		slog.String("token", tok),           // secret key, dynamic value: flagged
		slog.String("token_type", "bearer"), // secret key, constant value: ok
		slog.String("module", tok),          // not a secret key: ok
	)
	slog.Warn("token refresh failed", errors.New("e")) // message, not a key: ok
	// key/value form, package level and on a *slog.Logger
	slog.Info("kv", "kvuser", u, "kvcount", 3, slog.Any("kvattr", u), "kvsecret", tok)
	l.WarnContext(ctx, "kv", "kvctxptr", p, "kvok", e)
	l.Log(ctx, slog.LevelInfo, "kv", "kvlog", v)
	l.With("kvwith", u).Info("m", "password", "constant")
	slog.Group("g", "kvgroup", u)
	args := []any{"spread", u}
	slog.Info("spread", args...) // not analysable: skipped
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
		"logscope.any_opaque_value:sample.go:f:byid",
		"logscope.any_opaque_value:sample.go:f:kvattr",
		"logscope.any_opaque_value:sample.go:f:kvctxptr",
		"logscope.any_opaque_value:sample.go:f:kvgroup",
		"logscope.any_opaque_value:sample.go:f:kvlog",
		"logscope.any_opaque_value:sample.go:f:kvuser",
		"logscope.any_opaque_value:sample.go:f:kvwith",
		"logscope.any_opaque_value:sample.go:f:ptr",
		"logscope.any_opaque_value:sample.go:f:umap",
		"logscope.any_opaque_value:sample.go:f:user",
		"logscope.any_opaque_value:sample.go:f:users",
		"logscope.secret_key_dynamic_value:sample.go:f:kvsecret",
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

// A package that fails to type-check must fail the scan, not be skipped:
// otherwise the gate passes vacuously.
func TestScan_FailsOnPackageErrors(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module broken\n\ngo 1.26\n")
	write("broken.go", "package broken\n\nimport \"log/slog\"\n\nfunc f() { slog.Info(\"x\", \"k\", undefinedValue) }\n")
	if _, err := Scan(dir, []string{"./..."}); err == nil {
		t.Fatal("Scan ignored a package that does not type-check")
	}
	write("broken.go", "package broken\n\nimport \"log/slog\"\n\nfunc f(v any) { slog.Info(\"x\", \"k\", v) }\n")
	got, err := Scan(dir, []string{"./..."})
	if err != nil {
		t.Fatalf("clean package: %v", err)
	}
	if len(got) != 1 || got[0].Key != "k" {
		t.Fatalf("findings = %+v, want the key/value finding", got)
	}
}
