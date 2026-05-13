package policycoverage

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

// Severity controls which diagnostics fail CI versus which only surface in
// the report. Phase 5.1 fails on DeclaredUnused and UsedUndeclared for both
// permissions and capabilities; warns on capability declarations that no
// route consumes; and treats the Cedar-vs-permission overlap as INFO only.
type Severity int

const (
	SeverityInfo Severity = iota
	SeverityWarn
	SeverityError
)

func (s Severity) String() string {
	switch s {
	case SeverityError:
		return "ERROR"
	case SeverityWarn:
		return "WARN"
	default:
		return "INFO"
	}
}

// Diagnostic is a single reconciliation finding. Category is the stable
// short identifier the baseline file matches on; Key is the permission /
// capability ID being flagged; Detail is human-readable context.
type Diagnostic struct {
	Severity Severity `json:"severity"`
	Category string   `json:"category"`
	Key      string   `json:"key"`
	Owner    string   `json:"owner,omitempty"`
	Detail   string   `json:"detail,omitempty"`
	Sites    []string `json:"sites,omitempty"`
}

// Report is the reconciliation output. Diagnostics is the full list across
// severities; Summary is counts by severity for the CLI exit-code decision.
type Report struct {
	Diagnostics []Diagnostic        `json:"diagnostics"`
	Summary     map[Severity]int    `json:"summary"`
	Cedar       CedarReconciliation `json:"cedar"`
}

// CedarReconciliation surfaces the overlap between permission keys and the
// Action::"name" literals the Cedar policies reference, plus the broader
// coverage view that also accepts indirect coverage via
// `context.action_suffix == "X"` clauses (a permission `foo.bar.read` is
// suffix-covered when some policy mentions `"read"`) and
// `context.action_module == "Y"` clauses (a permission `foo.bar.read` is
// module-covered when some policy mentions `"foo"`).
//
// CedarSuffixes and CedarModules list the literals extracted from the
// two context-equality forms. CoveredPermissions lists declared permissions
// that are directly named, suffix-covered, or module-covered.
// UncoveredPermissions lists declared permissions that have none — these
// become `permission.cedar.unreferenced` diagnostics (ERROR severity,
// baseline-able while the catalog catches up).
//
// MatchedPermissions / UnmatchedCedar remain for the older overlap report
// (Cedar action literal ↔ declared permission) which is informational only.
type CedarReconciliation struct {
	CedarActions         []string `json:"cedarActions"`
	CedarSuffixes        []string `json:"cedarSuffixes"`
	CedarModules         []string `json:"cedarModules"`
	MatchedPermissions   []string `json:"matchedPermissions"`
	UnmatchedCedar       []string `json:"unmatchedCedar"`
	CoveredPermissions   []string `json:"coveredPermissions"`
	UncoveredPermissions []string `json:"uncoveredPermissions"`
}

// Reconcile computes the report from a set of findings. baseline is a set
// of "category:key" lines that suppress matching diagnostics so the gate
// can land in CI green and drift can be migrated out over time.
func Reconcile(f *Findings, baseline map[string]bool) *Report {
	r := &Report{Summary: map[Severity]int{}}
	if baseline == nil {
		baseline = map[string]bool{}
	}

	declaredPermSet := toSet(f.DeclaredPermissions)
	declaredCapSet := toSet(f.DeclaredCapabilities)
	usedPermSet, usedPermSites := usageIndex(f.UsedPermissions)
	usedCapSet, usedCapSites := usageIndex(f.UsedCapabilities)

	// Permissions used at a call site but never declared in any module's
	// Permissions(). This is a hard bug — the authz service will refuse
	// the check because the key isn't in the catalog.
	for _, key := range sortedKeys(usedPermSet) {
		if _, ok := declaredPermSet[key]; ok {
			continue
		}
		r.add(baseline, Diagnostic{
			Severity: SeverityError,
			Category: "permission.used.undeclared",
			Key:      key,
			Detail:   "route requires a permission that no module declares in Permissions()",
			Sites:    usedPermSites[key],
		})
	}

	// Permissions declared but never used by any route. Likely dead code
	// from a removed feature or a typo that split one permission into
	// two — either way the catalog should not carry entries the
	// evaluator can never be asked about.
	for _, key := range sortedKeys(declaredPermSet) {
		if _, ok := usedPermSet[key]; ok {
			continue
		}
		d := Diagnostic{
			Severity: SeverityError,
			Category: "permission.declared.unused",
			Key:      key,
			Owner:    declaredPermSet[key].Owner,
			Detail:   "Permissions() catalog entry not referenced by any RequirePermission or RequireSystemPermission call",
			Sites:    []string{declaredPermSet[key].Pos.String()},
		}
		r.add(baseline, d)
	}

	// Capabilities used at a call site but never declared. Same severity
	// as the permission case — the TenantProvider.HasCapability check
	// will always evaluate false, so the route is permanently 402.
	for _, key := range sortedKeys(usedCapSet) {
		if _, ok := declaredCapSet[key]; ok {
			continue
		}
		r.add(baseline, Diagnostic{
			Severity: SeverityError,
			Category: "capability.used.undeclared",
			Key:      key,
			Detail:   "route requires a capability that no module declares in Capabilities()",
			Sites:    usedCapSites[key],
		})
	}

	// Capabilities declared but not consumed by a RequireCapability call.
	// Lower severity — a capability might be declared for future routes
	// (published in the catalog so tenants can subscribe before the
	// consuming module ships) or for use by a non-HTTP surface.
	for _, key := range sortedKeys(declaredCapSet) {
		if _, ok := usedCapSet[key]; ok {
			continue
		}
		d := Diagnostic{
			Severity: SeverityWarn,
			Category: "capability.declared.unused",
			Key:      key,
			Owner:    declaredCapSet[key].Owner,
			Detail:   "Capabilities() entry not yet consumed by any RequireCapability gate",
			Sites:    []string{declaredCapSet[key].Pos.String()},
		}
		r.add(baseline, d)
	}

	// Dynamic (non-literal) gate calls can't be reconciled — surface as
	// INFO so the reviewer sees them in the audit trail. Replacing them
	// with a literal or a constant fixes the coverage blind spot.
	for _, u := range f.Dynamic {
		r.add(baseline, Diagnostic{
			Severity: SeverityInfo,
			Category: "gate.dynamic_key",
			Key:      string(u.Caller),
			Detail:   "gate middleware called with a non-literal first argument; coverage cannot verify the key",
			Sites:    []string{u.Pos.String()},
		})
	}

	// Cedar reconciliation: which declared permissions are reachable from
	// the policy set, either directly (`Action::"key"` literal) or via a
	// `context.action_suffix == "X"` clause that matches the permission's
	// suffix. Permissions with neither become `permission.cedar.unreferenced`
	// errors so a new endpoint cannot ship without the policy author also
	// thinking about Cedar coverage. The baseline carries pre-existing gaps.
	r.Cedar = buildCedarReconciliation(f.CedarActions, f.CedarSuffixes, f.CedarModules, declaredPermSet)
	for _, key := range r.Cedar.UncoveredPermissions {
		owner := ""
		if d, ok := declaredPermSet[key]; ok {
			owner = d.Owner
		}
		r.add(baseline, Diagnostic{
			Severity: SeverityError,
			Category: "permission.cedar.unreferenced",
			Key:      key,
			Owner:    owner,
			Detail:   "permission is not named in any .cedar policy and no context.action_suffix clause matches its suffix",
			Sites:    []string{declaredPermSet[key].Pos.String()},
		})
	}
	return r
}

// add appends a diagnostic unless the baseline suppresses it, incrementing
// the summary counter either way so the report can distinguish "0
// problems" from "all problems masked".
func (r *Report) add(baseline map[string]bool, d Diagnostic) {
	key := d.Category + ":" + d.Key
	if baseline[key] {
		return
	}
	r.Diagnostics = append(r.Diagnostics, d)
	r.Summary[d.Severity]++
}

// HasErrors reports whether any ERROR-severity diagnostic survived the
// baseline. The CLI uses this to decide the exit code.
func (r *Report) HasErrors() bool { return r.Summary[SeverityError] > 0 }

func buildCedarReconciliation(cedarActions, cedarSuffixes, cedarModules []string, decl map[string]Declaration) CedarReconciliation {
	rec := CedarReconciliation{
		CedarActions:  append([]string(nil), cedarActions...),
		CedarSuffixes: append([]string(nil), cedarSuffixes...),
		CedarModules:  append([]string(nil), cedarModules...),
	}
	for _, a := range cedarActions {
		if _, ok := decl[a]; ok {
			rec.MatchedPermissions = append(rec.MatchedPermissions, a)
		} else {
			rec.UnmatchedCedar = append(rec.UnmatchedCedar, a)
		}
	}
	actionSet := make(map[string]struct{}, len(cedarActions))
	for _, a := range cedarActions {
		actionSet[a] = struct{}{}
	}
	suffixSet := make(map[string]struct{}, len(cedarSuffixes))
	for _, s := range cedarSuffixes {
		suffixSet[s] = struct{}{}
	}
	moduleSet := make(map[string]struct{}, len(cedarModules))
	for _, m := range cedarModules {
		moduleSet[m] = struct{}{}
	}
	for _, key := range sortedKeys(decl) {
		if permissionCovered(key, actionSet, suffixSet, moduleSet) {
			rec.CoveredPermissions = append(rec.CoveredPermissions, key)
		} else {
			rec.UncoveredPermissions = append(rec.UncoveredPermissions, key)
		}
	}
	sort.Strings(rec.MatchedPermissions)
	sort.Strings(rec.UnmatchedCedar)
	return rec
}

// permissionCovered reports whether a permission key is reachable by some
// Cedar policy. Three paths (any one suffices):
//
//   - Direct:  named as `Action::"key"` in some .cedar file.
//   - Suffix:  the last dot-separated component matches a
//     `context.action_suffix == "X"` clause. Keys without dots are treated
//     as their own suffix (matches keys like `rag.query`).
//   - Module:  the first dot-separated component matches a
//     `context.action_module == "Y"` clause. This lets a single policy
//     cover every permission under one module prefix (e.g. org_billing
//     covering billing.*/payments.*/subscriptions.*). Keys without dots
//     have no module and never match this path.
func permissionCovered(key string, actions, suffixes, modules map[string]struct{}) bool {
	if _, ok := actions[key]; ok {
		return true
	}
	suffix := key
	if i := strings.LastIndex(key, "."); i >= 0 {
		suffix = key[i+1:]
	}
	if _, ok := suffixes[suffix]; ok {
		return true
	}
	if i := strings.Index(key, "."); i > 0 {
		if _, ok := modules[key[:i]]; ok {
			return true
		}
	}
	return false
}

func toSet(decls []Declaration) map[string]Declaration {
	out := make(map[string]Declaration, len(decls))
	for _, d := range decls {
		out[d.Key] = d
	}
	return out
}

func usageIndex(usages []Usage) (map[string]struct{}, map[string][]string) {
	set := map[string]struct{}{}
	sites := map[string][]string{}
	for _, u := range usages {
		set[u.Key] = struct{}{}
		sites[u.Key] = append(sites[u.Key], u.Pos.String())
	}
	for k := range sites {
		sort.Strings(sites[k])
	}
	return set, sites
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// WriteMarkdown renders a human-readable coverage report. The layout is
// three top-level sections (errors, warnings, info) followed by the Cedar
// reconciliation table. Keep it flat — CI uploads this as an artifact and
// reviewers skim it first.
func WriteMarkdown(w io.Writer, r *Report, findings *Findings) error {
	var b strings.Builder
	b.WriteString("# Policy coverage report\n\n")
	fmt.Fprintf(&b, "Scanned %d Go packages. Declared %d permissions, %d capabilities. Cedar: %d action literals.\n\n",
		findings.Packages, len(findings.DeclaredPermissions), len(findings.DeclaredCapabilities), len(findings.CedarActions))
	fmt.Fprintf(&b, "Summary: **%d errors**, **%d warnings**, **%d info**.\n\n",
		r.Summary[SeverityError], r.Summary[SeverityWarn], r.Summary[SeverityInfo])

	writeSeveritySection(&b, "Errors (fail CI)", SeverityError, r.Diagnostics)
	writeSeveritySection(&b, "Warnings", SeverityWarn, r.Diagnostics)
	writeSeveritySection(&b, "Informational", SeverityInfo, r.Diagnostics)

	b.WriteString("## Cedar policy coverage\n\n")
	fmt.Fprintf(&b, "Cedar policies reference **%d action literal(s)**, **%d action_suffix value(s)**, and **%d action_module value(s)**. ",
		len(r.Cedar.CedarActions), len(r.Cedar.CedarSuffixes), len(r.Cedar.CedarModules))
	fmt.Fprintf(&b, "Of %d declared permissions: **%d covered**, **%d uncovered**.\n\n",
		len(r.Cedar.CoveredPermissions)+len(r.Cedar.UncoveredPermissions),
		len(r.Cedar.CoveredPermissions),
		len(r.Cedar.UncoveredPermissions),
	)
	b.WriteString("A permission is *covered* when its full key appears as an `Action::\"key\"` literal, its suffix (part after the last `.`) matches a `context.action_suffix == \"X\"` clause, or its module (part before the first `.`) matches a `context.action_module == \"Y\"` clause. Uncovered permissions become `permission.cedar.unreferenced` errors above (baseline-able).\n\n")
	if len(r.Cedar.CedarSuffixes) > 0 {
		b.WriteString("Suffix values found: ")
		for i, s := range r.Cedar.CedarSuffixes {
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(&b, "`%s`", s)
		}
		b.WriteString(".\n\n")
	}
	if len(r.Cedar.CedarModules) > 0 {
		b.WriteString("Module values found: ")
		for i, m := range r.Cedar.CedarModules {
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(&b, "`%s`", m)
		}
		b.WriteString(".\n\n")
	}
	if len(r.Cedar.CedarActions) == 0 {
		b.WriteString("_No Cedar `Action::\"...\"` literals found — only suffix-based coverage is in play._\n\n")
	} else {
		b.WriteString("| Cedar action literal | Matches a declared permission? |\n|---|---|\n")
		for _, a := range r.Cedar.CedarActions {
			m := "no"
			for _, mm := range r.Cedar.MatchedPermissions {
				if mm == a {
					m = "yes"
					break
				}
			}
			fmt.Fprintf(&b, "| `%s` | %s |\n", a, m)
		}
		b.WriteString("\n")
	}

	_, err := io.WriteString(w, b.String())
	return err
}

func writeSeveritySection(b *strings.Builder, title string, sev Severity, diags []Diagnostic) {
	matching := make([]Diagnostic, 0, len(diags))
	for _, d := range diags {
		if d.Severity == sev {
			matching = append(matching, d)
		}
	}
	fmt.Fprintf(b, "## %s (%d)\n\n", title, len(matching))
	if len(matching) == 0 {
		b.WriteString("_None._\n\n")
		return
	}
	b.WriteString("| Category | Key | Owner | Detail | Sites |\n|---|---|---|---|---|\n")
	for _, d := range matching {
		owner := d.Owner
		if owner == "" {
			owner = "—"
		}
		sites := strings.Join(d.Sites, ", ")
		if sites == "" {
			sites = "—"
		}
		fmt.Fprintf(b, "| `%s` | `%s` | %s | %s | %s |\n", d.Category, d.Key, owner, d.Detail, sites)
	}
	b.WriteString("\n")
}

// WriteJSON writes the machine-readable report for CI tooling.
func WriteJSON(w io.Writer, r *Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}
