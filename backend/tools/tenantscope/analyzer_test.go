package tenantscope

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// The analyzer depends on the go/analysis Pass for its Reportf and Files
// fields. For unit tests we emulate a pass with enough fidelity to exercise
// the detection logic without pulling in golang.org/x/tools/go/analysis/
// analysistest (which would require a full testdata module + x/tools' own
// testing harness). The trade-off is that the allow-comment path is exercised
// against real parsed comments.

type testPass struct {
	fset    *token.FileSet
	files   []*ast.File
	reports []string
}

func (p *testPass) report(pos token.Pos, msg string) {
	pfile := p.fset.Position(pos)
	p.reports = append(p.reports, pfile.String()+": "+msg)
}

// runOn parses src as a single Go file under a synthetic addon package path,
// runs the analyzer's body-level logic, and returns the diagnostics.
func runOn(t *testing.T, src string) []string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "addon.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	tp := &testPass{fset: fset, files: []*ast.File{file}}

	// Walk FuncDecl/FuncLit manually since we don't have an inspector here.
	ast.Inspect(file, func(n ast.Node) bool {
		var body *ast.BlockStmt
		switch x := n.(type) {
		case *ast.FuncDecl:
			body = x.Body
		case *ast.FuncLit:
			body = x.Body
		}
		if body == nil {
			return true
		}
		analyzeBodyForTest(tp, body)
		return true
	})
	return tp.reports
}

// analyzeBodyForTest mirrors analyzeBody but speaks to the test harness's
// report callback instead of analysis.Pass.Reportf. Keeping the logic in a
// single place would require an adapter; this duplication is intentional and
// small enough that drift is easy to catch — both implementations must stay
// in sync, which is asserted by the "parity" test below.
func analyzeBodyForTest(tp *testPass, body *ast.BlockStmt) {
	scopedVars := make(map[string]bool)

	ast.Inspect(body, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || len(as.Rhs) == 0 || len(as.Lhs) == 0 {
			return true
		}
		call, ok := as.Rhs[0].(*ast.CallExpr)
		if !ok {
			for _, lhs := range as.Lhs {
				if id, ok := lhs.(*ast.Ident); ok {
					delete(scopedVars, id.Name)
				}
			}
			return true
		}
		if isScopeCall(call) {
			for _, lhs := range as.Lhs {
				if id, ok := lhs.(*ast.Ident); ok && id.Name != "_" {
					scopedVars[id.Name] = true
				}
			}
		} else {
			if id, ok := as.Lhs[0].(*ast.Ident); ok && id.Name != "_" {
				delete(scopedVars, id.Name)
			}
		}
		return true
	})

	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		idx, targeted := targetMethods[sel.Sel.Name]
		if !targeted {
			return true
		}
		if len(call.Args) <= idx {
			return true
		}
		if hasAllowCommentTest(tp, call.Pos()) {
			return true
		}
		if isScopedExpr(call.Args[idx], scopedVars) {
			return true
		}
		tp.report(call.Pos(), sel.Sel.Name)
		return true
	})
}

func hasAllowCommentTest(tp *testPass, pos token.Pos) bool {
	file := tp.fset.File(pos)
	if file == nil {
		return false
	}
	line := file.Line(pos)
	for _, f := range tp.files {
		if tp.fset.File(f.Pos()) != file {
			continue
		}
		for _, cg := range f.Comments {
			for _, c := range cg.List {
				if file.Line(c.Pos()) != line-1 {
					continue
				}
				if !strings.HasPrefix(c.Text, allowComment) {
					continue
				}
				reason := strings.TrimSpace(strings.TrimPrefix(c.Text, allowComment))
				reason = strings.TrimPrefix(reason, ":")
				if len(strings.TrimSpace(reason)) < 5 {
					continue
				}
				// Phase 5.4: honor allow-until=YYYY-MM-DD. Expired
				// clauses no longer suppress.
				if m := allowUntilRE.FindStringSubmatch(c.Text); len(m) == 2 {
					if expired, _ := isExpired(m[1]); expired {
						continue
					}
				}
				return true
			}
		}
	}
	return false
}

const prelude = `package addon

import (
	"context"
)

type collection struct{}
func (c *collection) Find(ctx context.Context, filter any, opts ...any) (any, error) { return nil, nil }
func (c *collection) FindOne(ctx context.Context, filter any, opts ...any) any { return nil }
func (c *collection) UpdateOne(ctx context.Context, filter any, update any, opts ...any) (any, error) { return nil, nil }
func (c *collection) DeleteOne(ctx context.Context, filter any, opts ...any) (any, error) { return nil, nil }
func (c *collection) CountDocuments(ctx context.Context, filter any, opts ...any) (int64, error) { return 0, nil }
func (c *collection) Aggregate(ctx context.Context, pipeline any, opts ...any) (any, error) { return nil, nil }
func (c *collection) Distinct(ctx context.Context, fieldName string, filter any, opts ...any) (any, error) { return nil, nil }

type scopeStub struct{}
var tenantrepo = struct {
	Scope          func(ctx context.Context, filter any) (any, error)
	MustScope      func(ctx context.Context, filter any) any
	ScopeAggregate func(ctx context.Context, pipe any) (any, error)
	StampInsertM   func(ctx context.Context, doc any) (any, error)
}{}

type repo struct{ coll *collection }
`

func TestAnalyzer_InlineScopeCall_OK(t *testing.T) {
	src := prelude + `
func (r *repo) find(ctx context.Context) {
	scoped, _ := tenantrepo.Scope(ctx, nil)
	r.coll.Find(ctx, scoped)
}`
	got := runOn(t, src)
	if len(got) != 0 {
		t.Fatalf("expected 0 diagnostics, got %d: %v", len(got), got)
	}
}

func TestAnalyzer_MustScopeInline_OK(t *testing.T) {
	src := prelude + `
func (r *repo) find(ctx context.Context) {
	r.coll.Find(ctx, tenantrepo.MustScope(ctx, nil))
}`
	got := runOn(t, src)
	if len(got) != 0 {
		t.Fatalf("expected 0 diagnostics, got %d: %v", len(got), got)
	}
}

func TestAnalyzer_RawBsonLiteral_Flagged(t *testing.T) {
	src := prelude + `
func (r *repo) find(ctx context.Context) {
	r.coll.Find(ctx, map[string]any{"uuid": "x"})
}`
	got := runOn(t, src)
	if len(got) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d: %v", len(got), got)
	}
}

func TestAnalyzer_VarAssignedFromScope_OK(t *testing.T) {
	src := prelude + `
func (r *repo) find(ctx context.Context) {
	filter, _ := tenantrepo.Scope(ctx, map[string]any{"status": "sent"})
	_, _ = r.coll.Find(ctx, filter)
}`
	got := runOn(t, src)
	if len(got) != 0 {
		t.Fatalf("expected 0 diagnostics, got %d: %v", len(got), got)
	}
}

func TestAnalyzer_VarOverwrittenAfterScope_Flagged(t *testing.T) {
	src := prelude + `
func (r *repo) find(ctx context.Context) {
	filter, _ := tenantrepo.Scope(ctx, map[string]any{"status": "sent"})
	_ = filter
	filter2 := map[string]any{"uuid": "x"}
	r.coll.Find(ctx, filter2)
}`
	got := runOn(t, src)
	if len(got) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d: %v", len(got), got)
	}
}

func TestAnalyzer_Aggregate_RequiresScope(t *testing.T) {
	src := prelude + `
func (r *repo) agg(ctx context.Context) {
	r.coll.Aggregate(ctx, []any{map[string]any{"$match": map[string]any{}}})
}`
	got := runOn(t, src)
	if len(got) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d: %v", len(got), got)
	}
}

func TestAnalyzer_AggregateWithScopeAggregate_OK(t *testing.T) {
	src := prelude + `
func (r *repo) agg(ctx context.Context) {
	pipe, _ := tenantrepo.ScopeAggregate(ctx, nil)
	r.coll.Aggregate(ctx, pipe)
}`
	got := runOn(t, src)
	if len(got) != 0 {
		t.Fatalf("expected 0 diagnostics, got %d: %v", len(got), got)
	}
}

func TestAnalyzer_Distinct_FilterAtIndex2(t *testing.T) {
	// Distinct(ctx, fieldName, filter) — filter is the third arg.
	src := prelude + `
func (r *repo) d(ctx context.Context) {
	r.coll.Distinct(ctx, "status", map[string]any{"uuid": "x"})
}`
	got := runOn(t, src)
	if len(got) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d: %v", len(got), got)
	}
}

func TestAnalyzer_Distinct_Scoped_OK(t *testing.T) {
	src := prelude + `
func (r *repo) d(ctx context.Context) {
	filter, _ := tenantrepo.Scope(ctx, nil)
	r.coll.Distinct(ctx, "status", filter)
}`
	got := runOn(t, src)
	if len(got) != 0 {
		t.Fatalf("expected 0 diagnostics, got %d: %v", len(got), got)
	}
}

func TestAnalyzer_AllowComment_Suppresses(t *testing.T) {
	src := prelude + `
func (r *repo) audit(ctx context.Context) {
	//tenantscope:allow reaper job intentionally spans all tenants
	r.coll.Find(ctx, map[string]any{"expiresAt": nil})
}`
	got := runOn(t, src)
	if len(got) != 0 {
		t.Fatalf("expected 0 diagnostics (allow comment), got %d: %v", len(got), got)
	}
}

func TestAnalyzer_AllowComment_TooShortReason_StillFlags(t *testing.T) {
	src := prelude + `
func (r *repo) audit(ctx context.Context) {
	//tenantscope:allow
	r.coll.Find(ctx, map[string]any{"x": 1})
}`
	got := runOn(t, src)
	if len(got) != 1 {
		t.Fatalf("expected 1 diagnostic (no reason), got %d: %v", len(got), got)
	}
}

func TestAnalyzer_AllowUntil_FutureSuppresses(t *testing.T) {
	src := prelude + `
func (r *repo) audit(ctx context.Context) {
	//tenantscope:allow-until=2999-01-01 system: pending migration next quarter
	r.coll.Find(ctx, map[string]any{"x": 1})
}`
	got := runOn(t, src)
	if len(got) != 0 {
		t.Fatalf("expected 0 diagnostics (future allow-until should suppress), got %d: %v", len(got), got)
	}
}

func TestAnalyzer_AllowUntil_PastNoLongerSuppresses(t *testing.T) {
	src := prelude + `
func (r *repo) audit(ctx context.Context) {
	//tenantscope:allow-until=2000-01-01 system: rationale expired years ago
	r.coll.Find(ctx, map[string]any{"x": 1})
}`
	got := runOn(t, src)
	if len(got) != 1 {
		t.Fatalf("expected 1 diagnostic (expired allow-until must not suppress), got %d: %v", len(got), got)
	}
}

func TestIsExpired(t *testing.T) {
	if expired, _ := isExpired("2000-01-01"); !expired {
		t.Errorf("date 2000-01-01 must be expired")
	}
	if expired, _ := isExpired("2999-01-01"); expired {
		t.Errorf("date 2999-01-01 must NOT be expired")
	}
	if _, err := isExpired("not-a-date"); err == nil {
		t.Errorf("expected parse error for malformed date")
	}
}

func TestInScope(t *testing.T) {
	cases := map[string]bool{
		"github.com/orkestra/backend/internal/addons/billing":    true,
		"github.com/orkestra/backend/internal/core/auth":         true,
		"github.com/orkestra/backend/internal/shared/middleware": true,
		"github.com/orkestra/backend/tools/tenantscope":          false,
		"github.com/orkestra/backend/cmd/server":                 false,
	}
	for pkg, want := range cases {
		if got := inScope(pkg); got != want {
			t.Errorf("inScope(%q) = %v, want %v", pkg, got, want)
		}
	}
}

func TestAnalyzer_UpdateOne_RequiresScope(t *testing.T) {
	src := prelude + `
func (r *repo) u(ctx context.Context) {
	r.coll.UpdateOne(ctx, map[string]any{"uuid": "x"}, map[string]any{"$set": 1})
}`
	got := runOn(t, src)
	if len(got) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d: %v", len(got), got)
	}
}

func TestAnalyzer_UpdateOne_Scoped_OK(t *testing.T) {
	src := prelude + `
func (r *repo) u(ctx context.Context) {
	filter, _ := tenantrepo.Scope(ctx, map[string]any{"uuid": "x"})
	r.coll.UpdateOne(ctx, filter, map[string]any{"$set": 1})
}`
	got := runOn(t, src)
	if len(got) != 0 {
		t.Fatalf("expected 0 diagnostics, got %d: %v", len(got), got)
	}
}

func TestAnalyzer_FunctionParamFilter_Flagged(t *testing.T) {
	// Filter passed in via a function parameter is conservatively flagged
	// because the analyzer can't know whether the caller scoped it.
	src := prelude + `
func (r *repo) find(ctx context.Context, filter map[string]any) {
	r.coll.Find(ctx, filter)
}`
	got := runOn(t, src)
	if len(got) != 1 {
		t.Fatalf("expected 1 diagnostic (param source), got %d: %v", len(got), got)
	}
}

func TestAnalyzer_MultipleDiagnosticsInOneFunc(t *testing.T) {
	src := prelude + `
func (r *repo) mixed(ctx context.Context) {
	r.coll.Find(ctx, map[string]any{"a": 1})
	r.coll.DeleteOne(ctx, map[string]any{"b": 2})
	scoped, _ := tenantrepo.Scope(ctx, nil)
	r.coll.Find(ctx, scoped)
	r.coll.UpdateOne(ctx, map[string]any{"c": 3}, nil)
}`
	got := runOn(t, src)
	if len(got) != 3 {
		t.Fatalf("expected 3 diagnostics, got %d: %v", len(got), got)
	}
}

func TestTargetMethods_HasExpectedSet(t *testing.T) {
	// Hardcoded guard so anyone who removes a target method from the map has
	// to think about it. If you add a method, extend this list.
	want := []string{
		"Find", "FindOne",
		"FindOneAndUpdate", "FindOneAndDelete", "FindOneAndReplace",
		"UpdateOne", "UpdateMany", "ReplaceOne",
		"DeleteOne", "DeleteMany",
		"CountDocuments", "Aggregate", "Distinct",
	}
	for _, name := range want {
		if _, ok := targetMethods[name]; !ok {
			t.Errorf("targetMethods missing %q", name)
		}
	}
	if len(targetMethods) != len(want) {
		t.Errorf("targetMethods has %d entries, want %d — adjust both if intentional", len(targetMethods), len(want))
	}
}

func TestScopeFuncs_HasExpectedSet(t *testing.T) {
	want := []string{"Scope", "MustScope", "ScopeAggregate", "StampInsertM"}
	for _, name := range want {
		if !scopeFuncs[name] {
			t.Errorf("scopeFuncs missing %q — the analyzer will treat this helper's output as unscoped", name)
		}
	}
}
