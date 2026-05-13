// Package tenantscope implements a static analyzer that enforces the org-scoping
// invariant #1 from the Org-scoped RBAC plan (and ADR-0001): every MongoDB
// read/write in an addon package must derive its filter (or aggregation
// pipeline) from shared/tenantrepo.Scope, MustScope, or ScopeAggregate.
//
// ADR-0001 reframes org-scoping as tenant-scoping with a two-tier Kind
// discriminator. The tenantrepo helpers are unchanged at the call-site level
// (they still take ctx and return a scoped filter); the tier distinction is
// enforced by middleware, not by this analyzer. This analyzer's job is to
// guarantee that every addon query is tenant-scoped at all, regardless of tier.
//
// Why: any addon repository that skips tenantrepo is a cross-tenant data leak
// waiting to happen. The helper already panics in dev when the org context is
// missing, but that only catches paths that get exercised. A compile-time
// analyzer catches the rest.
//
// Scope: the analyzer inspects packages whose import path contains
// "/internal/addons/", "/internal/core/", or "/internal/shared/". Phase
// 5.4 extended the scope from addons-only to every module under
// internal/ because core and shared packages were not previously
// audited — a raw bson.M filter in core/user/repository could leak
// cross-tenant without anyone noticing. A handful of packages that
// manage platform-global state legitimately skip tenant-scoping; they
// carry //tenantscope:allow comments that document why.
//
// Packages that are part of the analyzer itself (tools/tenantscope) are
// always exempt — they don't talk to MongoDB.
//
// Target call sites: method calls named Find, FindOne, FindOneAndUpdate,
// FindOneAndDelete, FindOneAndReplace, UpdateOne, UpdateMany, ReplaceOne,
// DeleteOne, DeleteMany, CountDocuments, Distinct, and Aggregate. These are
// the *mongo.Collection methods that accept a filter/pipeline argument. Insert
// operations (InsertOne, InsertMany) are checked separately — callers must
// invoke tenantrepo.StampInsert or StampInsertM against the document before
// the insert; a weaker heuristic is used because insert payloads vary widely.
//
// Acceptable filter sources:
//  1. A direct call to tenantrepo.Scope / MustScope / ScopeAggregate /
//     StampInsertM passed inline as the filter argument.
//  2. A local variable whose most recent assignment in the same function is
//     a call to one of the above helpers.
//
// Anything else — a bson.M literal, a struct value, a function parameter, a
// field access — is reported.
//
// Opt-out: prepend a line comment `//tenantscope:allow <reason>` directly
// above the flagged call. Reviewers should treat the presence of such a
// comment as a tenant-isolation audit point.
package tenantscope

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

// Analyzer is the go/analysis Analyzer registered by the command-line runner
// and (in future) by a vet-tool driver.
var Analyzer = &analysis.Analyzer{
	Name:     "tenantscope",
	Doc:      "reports MongoDB collection queries in addon packages whose filter does not come from shared/tenantrepo.Scope*",
	URL:      "https://github.com/orkestra/orkestra/blob/main/backend/internal/core/authz/CLAUDE.md#org-scoping-invariants-system-wide",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

// baselinePath is the path to a file listing pre-existing violations that
// should not fail CI. Lines have the form  <relpath>:<line>:<method>  where
// relpath is relative to the repository root (i.e. starts with "internal/").
// As modules migrate in Phase 4, entries are deleted from the baseline.
//
// Using a baseline rather than //tenantscope:allow comments at every call
// site keeps the migration surgical: addon files get exactly one diff per
// repository touch rather than interleaved allow-comments that would have to
// be removed later.
var baselinePath string

// baselineOnce guards loading of the baseline set. Read-only after init.
var (
	baselineOnce sync.Once
	baselineSet  map[string]bool
	baselineErr  error
)

func init() {
	Analyzer.Flags.StringVar(&baselinePath, "baseline", "",
		"path to a baseline file of accepted violations; each matching diagnostic is suppressed (format: relpath:line:method)")
}

// targetMethods maps *mongo.Collection method names to the argument index that
// must be scoped. The index is relative to the call-expression args (not
// counting the receiver); Mongo methods take ctx as arg 0 in almost every
// case, and the filter follows.
var targetMethods = map[string]int{
	"Find":              1,
	"FindOne":           1,
	"FindOneAndUpdate":  1,
	"FindOneAndDelete":  1,
	"FindOneAndReplace": 1,
	"UpdateOne":         1,
	"UpdateMany":        1,
	"ReplaceOne":        1,
	"DeleteOne":         1,
	"DeleteMany":        1,
	"CountDocuments":    1,
	"Aggregate":         1,
	"Distinct":          2, // (ctx, fieldName, filter, ...)
}

// scopeFuncs lists the helper functions in shared/tenantrepo that the
// analyzer treats as safe sources of a filter/pipeline. Every additional
// helper added to that package for tenant scoping must be listed here.
var scopeFuncs = map[string]bool{
	"Scope":          true,
	"MustScope":      true,
	"ScopeAggregate": true,
	// StampInsertM returns a document (for inserts) with tenantId stamped.
	// Including it allows InsertOne(ctx, tenantrepo.MustStampInsert(...))
	// style call sites to pass the analyzer if we ever target InsertOne.
	"StampInsertM": true,
}

// allowComment is the magic marker that silences a single diagnostic. The
// comment must appear on its own line directly above the flagged call. A
// reason after the colon is required; analyzer passes silently when the
// reason is at least five characters so "//tenantscope:allow" alone doesn't
// slip through.
const allowComment = "//tenantscope:allow"

// inScope reports whether the analyzer should inspect the given package
// path. Phase 5.4 extended the set from addons-only to every module
// under internal/. The tools/ directory is always skipped (the
// analyzer reflects on its own code would be a fun sort of embarrassing
// footgun).
func inScope(pkgPath string) bool {
	if strings.Contains(pkgPath, "/tools/") {
		return false
	}
	return strings.Contains(pkgPath, "/internal/addons/") ||
		strings.Contains(pkgPath, "/internal/core/") ||
		strings.Contains(pkgPath, "/internal/shared/")
}

func run(pass *analysis.Pass) (any, error) {
	if !inScope(pass.Pkg.Path()) {
		return nil, nil
	}

	if err := loadBaseline(); err != nil {
		return nil, err
	}

	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)

	insp.Preorder([]ast.Node{(*ast.FuncDecl)(nil), (*ast.FuncLit)(nil)}, func(n ast.Node) {
		var body *ast.BlockStmt
		switch x := n.(type) {
		case *ast.FuncDecl:
			body = x.Body
		case *ast.FuncLit:
			body = x.Body
		}
		if body == nil {
			return
		}
		analyzeBody(pass, body)
	})

	return nil, nil
}

// loadBaseline reads the baseline file (if configured) into baselineSet.
// The file format is one "relpath:line:method" per line. Blank lines and
// lines starting with # are ignored. The relpath is expected to be relative
// to the repository root — the comparison in baselineMatches below strips
// the absolute prefix from the diagnostic position at runtime.
func loadBaseline() error {
	baselineOnce.Do(func() {
		if baselinePath == "" {
			baselineSet = map[string]bool{}
			return
		}
		f, err := os.Open(baselinePath)
		if err != nil {
			baselineErr = fmt.Errorf("tenantscope: open baseline %s: %w", baselinePath, err)
			return
		}
		defer f.Close()
		set := make(map[string]bool)
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			set[line] = true
		}
		if err := sc.Err(); err != nil {
			baselineErr = fmt.Errorf("tenantscope: read baseline: %w", err)
			return
		}
		baselineSet = set
	})
	return baselineErr
}

// baselineMatches reports whether the given diagnostic is in the baseline.
// It normalizes the absolute file path down to a repository-relative path by
// locating the "/internal/" segment, which is stable across local checkouts,
// CI runners, and Docker builds.
func baselineMatches(absFile string, line int, method string) bool {
	if len(baselineSet) == 0 {
		return false
	}
	rel := absFile
	if i := strings.Index(absFile, "/internal/"); i >= 0 {
		rel = absFile[i+1:] // keep "internal/..." prefix
	}
	rel = filepath.ToSlash(rel)
	key := fmt.Sprintf("%s:%d:%s", rel, line, method)
	return baselineSet[key]
}

// analyzeBody walks a function body twice. The first walk collects every
// local variable whose most recent assignment is a call to tenantrepo.Scope*;
// the second walk inspects every method call with a target name and checks
// that the filter argument is scoped.
//
// The two-pass design intentionally uses "most recent assignment wins"
// semantics: a later assignment that overwrites `filter` with an unscoped
// value would strip it from the scoped-vars set. This catches bugs like
//
//	filter, _ := tenantrepo.Scope(ctx, bson.M{...})
//	filter = bson.M{"uuid": uuid}  // oops — scope lost
//	coll.Find(ctx, filter)          // flagged
func analyzeBody(pass *analysis.Pass, body *ast.BlockStmt) {
	scopedVars := make(map[string]bool)

	ast.Inspect(body, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || len(as.Rhs) == 0 || len(as.Lhs) == 0 {
			return true
		}
		// Match either  x := tenantrepo.Scope(...)  or  x, err := tenantrepo.Scope(...)
		// The RHS length is 1 in both cases (single multi-return call).
		call, ok := as.Rhs[0].(*ast.CallExpr)
		if !ok {
			// Overwriting without a scope call — the LHS vars lose scope status.
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
			// Non-scope RHS: any LHS that was previously scoped loses the flag,
			// but only for the first LHS (which is typically the value the
			// caller keeps). Errors on LHS[1] are unaffected.
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
		if hasAllowComment(pass, call.Pos()) {
			return true
		}
		if isScopedExpr(call.Args[idx], scopedVars) {
			return true
		}
		callPos := pass.Fset.Position(call.Pos())
		if baselineMatches(callPos.Filename, callPos.Line, sel.Sel.Name) {
			return true
		}
		pass.Reportf(
			call.Pos(),
			"tenantscope: %s in addon package must derive its filter from tenantrepo.Scope/MustScope/ScopeAggregate (invariant #1 — see backend/internal/core/authz/CLAUDE.md#org-scoping-invariants-system-wide). Silence with //tenantscope:allow <reason> if the call genuinely operates outside any tenant.",
			sel.Sel.Name,
		)
		return true
	})
}

func isScopeCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	if pkg.Name != "tenantrepo" {
		return false
	}
	return scopeFuncs[sel.Sel.Name]
}

func isScopedExpr(e ast.Expr, scopedVars map[string]bool) bool {
	switch v := e.(type) {
	case *ast.Ident:
		return scopedVars[v.Name]
	case *ast.CallExpr:
		return isScopeCall(v)
	case *ast.ParenExpr:
		return isScopedExpr(v.X, scopedVars)
	}
	return false
}

// allowUntilRE matches the optional expiry clause in an allow comment:
//
//	//tenantscope:allow admin-view: operator-wide lookup
//	//tenantscope:allow-until=2026-12-31 admin-view: temporary shim until X
//
// The capturing group returns the YYYY-MM-DD substring. Phase 5.4
// introduced this so rationales expire rather than pile up — after the
// date, the analyzer treats the call site as unscoped again.
var allowUntilRE = regexp.MustCompile(`allow-until=(\d{4}-\d{2}-\d{2})`)

// hasAllowComment reports whether the line immediately above the given
// position carries a //tenantscope:allow comment with a non-trivial
// reason. Phase 5.4 additions:
//
//   - An optional allow-until=YYYY-MM-DD clause is honored: if the date
//     is in the past (relative to "now"), the allow comment no longer
//     suppresses the diagnostic.
//   - Reasons should begin with one of the canonical prefixes so the
//     audit is self-categorizing: "admin-view:", "webhook:", "system:".
//     The analyzer still accepts bare reasons today to avoid a
//     per-site rewrite; a future Phase 5.x sub-phase can tighten it.
func hasAllowComment(pass *analysis.Pass, pos token.Pos) bool {
	file := pass.Fset.File(pos)
	if file == nil {
		return false
	}
	line := file.Line(pos)

	for _, f := range pass.Files {
		if pass.Fset.File(f.Pos()) != file {
			continue
		}
		for _, cg := range f.Comments {
			for _, c := range cg.List {
				cLine := file.Line(c.Pos())
				if cLine != line-1 {
					continue
				}
				if !strings.HasPrefix(c.Text, allowComment) {
					continue
				}
				// Require "//tenantscope:allow <reason>" with reason >= 5 chars.
				reason := strings.TrimSpace(strings.TrimPrefix(c.Text, allowComment))
				reason = strings.TrimPrefix(reason, ":")
				if len(strings.TrimSpace(reason)) < 5 {
					continue
				}
				// Optional expiry: allow-until=YYYY-MM-DD. If it's in
				// the past (relative to the clock at analyze time)
				// the comment no longer suppresses the diagnostic.
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

// isExpired reports whether the given YYYY-MM-DD date is strictly before
// today's UTC date. Any parse error is treated as "not expired" so a
// typo in the allow comment doesn't silently break the build; the
// diagnostic surface is already the reason-length check above.
func isExpired(date string) (bool, error) {
	t, err := time.Parse("2006-01-02", date)
	if err != nil {
		return false, err
	}
	today := time.Now().UTC().Truncate(24 * time.Hour)
	return t.Before(today), nil
}
