// Package piiscan flags Orkestra modules that persist personal data tied to
// a data subject (a collection whose model carries a userUUID-style field)
// but register no iface.PIIProducer — meaning that data would silently
// escape the GDPR DSR export/erase sweep the compliance module drives
// (ADR-0009).
//
// Like policycoverage (and unlike the go/analysis-based tenantscope) this is
// a standalone cross-package AST walker: the question is inherently
// per-module — a subject-bearing model lives in one package (models/) and
// the producer that covers it is constructed in another (services/,
// registered from module.go). go/packages lets us load the whole tree once
// and attribute both halves back to their owning module.
//
// The scan is deliberately conservative. It keys off persisted struct fields
// (a `bson:"..."` tag whose name denotes a data subject), so request/response
// DTOs — which carry only `json:` tags — never trip it. Actor / creator /
// grantor references (actorUserId, grantedBy, createdBy) are intentionally
// NOT treated as subject PII: those name who acted, not whose record it is,
// and are typically retained rather than erased. New subject-field shapes are
// added to subjectTagSet as they appear.
package piiscan

import (
	"fmt"
	"go/ast"
	"go/token"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"
)

// Position is a filename + line pointer for diagnostic output, stored flat so
// the report doesn't carry the go/token.FileSet around.
type Position struct {
	File string
	Line int
}

func (p Position) String() string { return fmt.Sprintf("%s:%d", p.File, p.Line) }

// SubjectField is one persisted struct field whose bson tag denotes a data
// subject. Module is the owning core/addon module; Struct/Field/Tag give the
// reviewer enough to find it.
type SubjectField struct {
	Module string
	Struct string
	Field  string
	Tag    string
	Pos    Position
}

// Producer records that a module constructs an iface.PIIProducer (a
// NewPIIProducer constructor), which by convention it registers with the
// PIIProducerRegistry at Init.
type Producer struct {
	Module string
	Pos    Position
}

// Findings is the raw collection the scanner produces before reconciliation.
type Findings struct {
	SubjectFields []SubjectField
	Producers     []Producer
	Packages      int
}

// subjectTagSet is the allow-list of normalized bson tag names that mark a row
// as being ABOUT a data subject. Normalization lower-cases and strips
// non-alphanumerics, so userUUID / userUuid / user_uuid all collapse to
// "useruuid". Keep this list curated — broadening it to actor/creator refs
// would flag retained audit/ownership rows that are not DSR-erasable.
var subjectTagSet = map[string]bool{
	"useruuid":          true,
	"userid":            true,
	"recipientuseruuid": true,
	"subjectuseruuid":   true,
	"owneruseruuid":     true,
	"targetuseruuid":    true,
	"memberuseruuid":    true,
}

// Scan loads every Go package under the patterns and walks their AST,
// collecting subject-bearing struct fields and PIIProducer constructors.
// Errors loading individual packages are surfaced via packages.Load; a
// partial AST still produces a useful report.
func Scan(patterns []string) (*Findings, error) {
	cfg := &packages.Config{
		Mode:  packages.NeedName | packages.NeedFiles | packages.NeedSyntax,
		Tests: false,
	}
	pkgs, err := packages.Load(cfg, patterns...)
	if err != nil {
		return nil, fmt.Errorf("piiscan: load packages: %w", err)
	}

	findings := &Findings{Packages: len(pkgs)}
	for _, pkg := range pkgs {
		// Skip the analyzer/tool packages themselves — they declare helper
		// structs that would look like models.
		if strings.HasPrefix(pkg.PkgPath, "github.com/orkestra/backend/tools/") {
			continue
		}
		module := moduleFromPkgPath(pkg.PkgPath)
		if module == "" {
			continue // not a core/addon module (shared, cmd, pkg/sdk, …)
		}
		for _, file := range pkg.Syntax {
			if file == nil {
				continue
			}
			filename := pkg.Fset.Position(file.Pos()).Filename
			scanFile(pkg.Fset, file, filename, module, findings)
		}
	}

	sort.Slice(findings.SubjectFields, func(i, j int) bool {
		if findings.SubjectFields[i].Module != findings.SubjectFields[j].Module {
			return findings.SubjectFields[i].Module < findings.SubjectFields[j].Module
		}
		return findings.SubjectFields[i].Pos.String() < findings.SubjectFields[j].Pos.String()
	})
	sort.Slice(findings.Producers, func(i, j int) bool {
		return findings.Producers[i].Module < findings.Producers[j].Module
	})
	return findings, nil
}

// moduleFromPkgPath extracts the owning module name from a package import
// path. Returns "" for packages that are not under internal/core or
// internal/addons (shared infrastructure, cmd, pkg/sdk) — those are not DSR
// modules and are never attributed a producer obligation.
func moduleFromPkgPath(p string) string {
	for _, anchor := range []string{"/internal/core/", "/internal/addons/"} {
		if i := strings.Index(p, anchor); i >= 0 {
			rest := p[i+len(anchor):]
			if j := strings.Index(rest, "/"); j >= 0 {
				return rest[:j]
			}
			return rest
		}
	}
	return ""
}

// scanFile walks one parsed file feeding subject fields and producer
// constructors into the accumulator.
func scanFile(fset *token.FileSet, file *ast.File, filename, module string, out *Findings) {
	rel := filename
	if i := strings.Index(filename, "/internal/"); i >= 0 {
		rel = filename[i+1:]
	}
	rel = strings.ReplaceAll(rel, "\\", "/")

	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.TypeSpec:
			st, ok := node.Type.(*ast.StructType)
			if !ok || st.Fields == nil {
				return true
			}
			for _, f := range st.Fields.List {
				if f.Tag == nil || len(f.Names) == 0 {
					continue
				}
				tagName, ok := bsonTagName(f.Tag.Value)
				if !ok {
					continue
				}
				if !subjectTagSet[normalizeTag(tagName)] {
					continue
				}
				p := fset.Position(f.Pos())
				out.SubjectFields = append(out.SubjectFields, SubjectField{
					Module: module,
					Struct: node.Name.Name,
					Field:  f.Names[0].Name,
					Tag:    tagName,
					Pos:    Position{File: rel, Line: p.Line},
				})
			}
		case *ast.FuncDecl:
			if node.Name != nil && node.Name.Name == "NewPIIProducer" {
				p := fset.Position(node.Pos())
				out.Producers = append(out.Producers, Producer{
					Module: module,
					Pos:    Position{File: rel, Line: p.Line},
				})
			}
		}
		return true
	})
}

// bsonTagName unwraps a struct-tag literal and returns the bson tag's name
// (the part before the first comma). Returns ("", false) when there is no
// bson tag or its name is empty / "-".
func bsonTagName(rawTag string) (string, bool) {
	unq, err := strconv.Unquote(rawTag)
	if err != nil {
		return "", false
	}
	val := reflect.StructTag(unq).Get("bson")
	if val == "" {
		return "", false
	}
	name := val
	if i := strings.Index(val, ","); i >= 0 {
		name = val[:i]
	}
	if name == "" || name == "-" {
		return "", false
	}
	return name, true
}

// normalizeTag lower-cases and strips non-alphanumerics so userUUID, userUuid
// and user_uuid all compare equal.
func normalizeTag(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}
