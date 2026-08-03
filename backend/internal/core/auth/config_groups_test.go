package auth

import (
	"testing"

	"github.com/orkestra/backend/pkg/sdk/module"
)

func schemaOf(t *testing.T) []module.ConfigField {
	t.Helper()
	return (&AuthModule{}).ConfigSchema()
}

func TestConfigGroups_TreeShape(t *testing.T) {
	groups := (&AuthModule{}).ConfigGroups()
	byKey := make(map[string]module.ConfigGroup, len(groups))
	for _, g := range groups {
		byKey[g.Key] = g
	}

	for _, key := range []string{
		"registration", "login", "password", "mfa", "oauth",
		"oauth.google", "oauth.apple", "oauth.github", "oauth.discord",
		"antiabuse", "sessions",
	} {
		if _, ok := byKey[key]; !ok {
			t.Errorf("group %q not declared", key)
		}
	}

	for _, key := range []string{"oauth.google", "oauth.apple", "oauth.github", "oauth.discord"} {
		if got := byKey[key].Parent; got != "oauth" {
			t.Errorf("group %q Parent = %q, want %q", key, got, "oauth")
		}
	}
	if byKey["oauth"].Parent != "" {
		t.Errorf("oauth must be top-level, got Parent %q", byKey["oauth"].Parent)
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

func TestConfigGroups_DeclarationsValidate(t *testing.T) {
	// The same checker cmd/server runs over the whole catalog, applied here so
	// a mistake fails this module's own package first.
	if err := module.ValidateConfigDeclarations(
		schemaOf(t), (&AuthModule{}).ConfigGroups(),
	); err != nil {
		t.Errorf("ValidateConfigDeclarations: %v", err)
	}
}

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
		got := make(map[string]bool, len(f.DependsOn))
		for _, c := range f.DependsOn {
			got[c.Key] = true
		}
		for _, key := range want {
			if !got[key] {
				t.Errorf("field %q does not depend on %q", f.Key, key)
			}
		}
	}
	if counted != 19 {
		t.Errorf("gated %d provider credential fields, want 19", counted)
	}
}
