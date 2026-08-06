package auth

import (
	"testing"

	"github.com/orkestra/backend/pkg/sdk/module"
)

func schemaOf(t *testing.T) []module.ConfigField {
	t.Helper()
	return (&AuthModule{}).ConfigSchema()
}

// TestConfigGroups_TreeShape pins the full declared shape of every group —
// not just its presence. `Order` and `Label` drive the operator-facing rail
// directly (rail position and heading text), and both are silent-failure
// footguns: swapping two `Order` values, or a typo'd `Label`, produces a
// tree that still "has all 11 keys" and would pass a presence-only check
// while visibly misordering or mislabeling the rail. The exact count (11) is
// asserted too, so a stray extra group (declared but never wired to
// anything) doesn't slip through unnoticed.
func TestConfigGroups_TreeShape(t *testing.T) {
	groups := (&AuthModule{}).ConfigGroups()
	if len(groups) != 11 {
		t.Fatalf("ConfigGroups() returned %d groups, want 11", len(groups))
	}
	byKey := make(map[string]module.ConfigGroup, len(groups))
	for _, g := range groups {
		byKey[g.Key] = g
	}

	wantOrder := map[string]struct {
		label  string
		parent string
		order  int
	}{
		"registration":  {"Registration", "", 1},
		"login":         {"Login & Sessions", "", 2},
		"password":      {"Password Policy", "", 3},
		"mfa":           {"MFA", "", 4},
		"oauth":         {"OAuth Providers", "", 5},
		"oauth.google":  {"Google", "oauth", 6},
		"oauth.apple":   {"Apple", "oauth", 7},
		"oauth.github":  {"GitHub", "oauth", 8},
		"oauth.discord": {"Discord", "oauth", 9},
		"antiabuse":     {"Anti-abuse & Notifications", "", 10},
		"sessions":      {"Sessions & Account", "", 11},
	}

	for key, want := range wantOrder {
		got, ok := byKey[key]
		if !ok {
			t.Errorf("group %q not declared", key)
			continue
		}
		if got.Label != want.label {
			t.Errorf("group %q Label = %q, want %q", key, got.Label, want.label)
		}
		if got.Parent != want.parent {
			t.Errorf("group %q Parent = %q, want %q", key, got.Parent, want.parent)
		}
		if got.Order != want.order {
			t.Errorf("group %q Order = %d, want %d", key, got.Order, want.order)
		}
	}
}

func TestConfigGroups_EveryFieldIsGrouped(t *testing.T) {
	groups := (&AuthModule{}).ConfigGroups()
	declared := make(map[string]bool, len(groups))
	for _, g := range groups {
		declared[g.Key] = true
	}
	for _, f := range schemaOf(t) {
		if f.Group == "" {
			t.Errorf("field %q has no group — it would be unreachable in the settings rail", f.Key)
			continue
		}
		if !declared[f.Group] {
			t.Errorf("field %q references undeclared group %q", f.Key, f.Group)
		}
	}
}

// TestConfigGroups_FieldCountPerGroup pins the exact bucket every one of the
// 62 fields lands in. `TestConfigGroups_EveryFieldIsGrouped` above only
// proves a field's `Group` resolves to *some* declared key — it would not
// notice `passwordMinLength` moving from `password` to `login`, since
// `login` is declared too. This is the regression guard the migration brief
// calls its central invariant: every field changed identifier, none changed
// bucket. The counts are the ones the brief's own table specifies and sum to
// 62, the module's full field count.
func TestConfigGroups_FieldCountPerGroup(t *testing.T) {
	want := map[string]int{
		"registration":  5,
		"login":         6,
		"password":      7,
		"mfa":           5,
		"oauth":         11,
		"oauth.google":  5,
		"oauth.apple":   8,
		"oauth.github":  3,
		"oauth.discord": 3,
		"antiabuse":     7,
		"sessions":      2,
	}
	got := make(map[string]int)
	schema := schemaOf(t)
	for _, f := range schema {
		got[f.Group]++
	}
	if len(schema) != 62 {
		t.Errorf("ConfigSchema() returned %d fields, want 62", len(schema))
	}
	for group, wantCount := range want {
		if got[group] != wantCount {
			t.Errorf("group %q has %d fields, want %d", group, got[group], wantCount)
		}
	}
	for group, gotCount := range got {
		if _, ok := want[group]; !ok {
			t.Errorf("group %q has %d fields but is not in the expected bucket map — update the map alongside any intentional rebucketing", group, gotCount)
		}
	}
}

func TestConfigGroups_DeclarationsValidate(t *testing.T) {
	// The same checker cmd/server runs over the whole catalog, applied here so
	// a mistake fails this module's own package first.
	if err := module.ValidateConfigDeclarations(
		schemaOf(t), (&AuthModule{}).ConfigGroups(),
	); err != nil {
		t.Errorf("ValidateConfigDeclarations: %v", err)
	}
}

// TestProviderCredentials_HiddenUntilEitherSurfaceEnabled asserts the exact
// shape of the gate on all 19 provider-credential fields, not just that the
// two expected keys are somewhere in the `DependsOn` list. A `DependsOn`
// containing a stray third condition (e.g. a Google field also keyed off
// Apple's toggle) would pass a "contains" check while making that field
// *more* visible than intended, and `In: ["false"]` on an expected key would
// invert the gate entirely — both slip past `ValidateConfigDeclarations`,
// which only rejects non-boolean literals, not the wrong ones. Asserting the
// condition set exactly, and that every condition's `In` is exactly
// `["true"]`, closes both gaps.
func TestProviderCredentials_HiddenUntilEitherSurfaceEnabled(t *testing.T) {
	// The whole point of the migration: 19 of 62 fields are provider
	// credentials that are dead weight on an install not using that provider.
	cases := map[string][]string{
		"oauth.google":  {"googleEnabledAdmin", "googleEnabledClient"},
		"oauth.apple":   {"appleEnabledAdmin", "appleEnabledClient"},
		"oauth.github":  {"githubEnabledAdmin", "githubEnabledClient"},
		"oauth.discord": {"discordEnabledAdmin", "discordEnabledClient"},
	}
	counted := 0
	for _, f := range schemaOf(t) {
		want, ok := cases[f.Group]
		if !ok {
			continue
		}
		counted++
		if f.DependsOnMatch != "any" {
			t.Errorf("field %q DependsOnMatch = %q, want \"any\" — a provider enabled on only one surface still needs its credentials",
				f.Key, f.DependsOnMatch)
		}
		if len(f.DependsOn) != len(want) {
			t.Errorf("field %q has %d DependsOn conditions, want exactly %d (%v)",
				f.Key, len(f.DependsOn), len(want), want)
		}
		got := make(map[string][]string, len(f.DependsOn))
		for _, c := range f.DependsOn {
			got[c.Key] = c.In
		}
		for _, key := range want {
			in, ok := got[key]
			if !ok {
				t.Errorf("field %q does not depend on %q", f.Key, key)
				continue
			}
			if len(in) != 1 || in[0] != "true" {
				t.Errorf("field %q condition on %q has In = %v, want [\"true\"]", f.Key, key, in)
			}
		}
		// Every condition on the field must be one of the two expected keys
		// — a stray extra condition would widen visibility beyond what the
		// two toggles alone should grant.
		wantSet := make(map[string]bool, len(want))
		for _, key := range want {
			wantSet[key] = true
		}
		for key := range got {
			if !wantSet[key] {
				t.Errorf("field %q has an unexpected DependsOn condition on %q", f.Key, key)
			}
		}
	}
	if counted != 19 {
		t.Errorf("gated %d provider credential fields, want 19", counted)
	}
}

// TestProviderToggles_NeverGated is the inverse of the test above, and the
// highest-consequence invariant in the whole migration: the eight enable
// toggles (one per provider, per audience surface) are how an operator turns
// a provider on in the first place. Gating any one of them on itself (or on
// anything else) would make that provider permanently unrecoverable through
// the UI — there would be no visible control left to switch it back on. The
// previous test suite never exercised this because it only iterated fields
// whose `Group` is a provider subgroup, and the toggles live in the `oauth`
// parent group, so they were silently skipped. Adding a `DependsOn` to
// `googleEnabledAdmin` would pass every other test in this file, including
// `ValidateConfigDeclarations` (the shape is structurally valid) — this is
// the only guard against that regression.
func TestProviderToggles_NeverGated(t *testing.T) {
	toggles := []string{
		"googleEnabledAdmin", "googleEnabledClient",
		"appleEnabledAdmin", "appleEnabledClient",
		"githubEnabledAdmin", "githubEnabledClient",
		"discordEnabledAdmin", "discordEnabledClient",
	}
	byKey := make(map[string]module.ConfigField)
	for _, f := range schemaOf(t) {
		byKey[f.Key] = f
	}
	if len(toggles) != 8 {
		t.Fatalf("test bug: toggles list has %d entries, want 8", len(toggles))
	}
	for _, key := range toggles {
		f, ok := byKey[key]
		if !ok {
			t.Errorf("toggle %q not found in ConfigSchema()", key)
			continue
		}
		if f.Group != "oauth" {
			t.Errorf("toggle %q Group = %q, want %q — toggles live in the parent, not a provider subgroup", key, f.Group, "oauth")
		}
		if len(f.DependsOn) != 0 {
			t.Errorf("toggle %q has %d DependsOn condition(s), want 0 — gating it would make the provider unrecoverable through the UI", key, len(f.DependsOn))
		}
		if f.DependsOnMatch != "" {
			t.Errorf("toggle %q DependsOnMatch = %q, want \"\"", key, f.DependsOnMatch)
		}
	}
}
