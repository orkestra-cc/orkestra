package main

import (
	"testing"

	"github.com/orkestra/backend/internal/shared/config"
	"github.com/orkestra/backend/pkg/sdk/module"
)

// coreModuleCount guards against the gate silently going partial: if a core
// module is added to the catalog without this number moving, the new module
// is being checked, but a *removal* would otherwise shrink the gate unnoticed.
const coreModuleCount = 8

// buildAllModules instantiates every module compiled into this binary. The
// instances are used for reading declarations only — Init is never called —
// so no infrastructure is needed.
func buildAllModules(t *testing.T) []module.Module {
	t.Helper()
	factories := coreModules(&config.Config{})
	if len(factories) != coreModuleCount {
		t.Fatalf("coreModules returned %d factories, want %d — update coreModuleCount so the gate stays honest",
			len(factories), coreModuleCount)
	}
	for _, f := range optionalModules {
		factories = append(factories, f)
	}
	mods := make([]module.Module, 0, len(factories))
	for _, f := range factories {
		mods = append(mods, f())
	}
	return mods
}

// TestConfigDeclarationsAreValid runs the SDK's declaration checker over every
// module compiled into this binary. The SDK package cannot do this itself —
// pkg/sdk must never import internal/ — so the gate lives here, where the
// catalog is in scope. A fork gets the same coverage for its addons for free,
// because its catalog_<name>.go registers into the same optionalModules map.
func TestConfigDeclarationsAreValid(t *testing.T) {
	for _, m := range buildAllModules(t) {
		t.Run(m.Name(), func(t *testing.T) {
			if err := module.ValidateConfigDeclarations(
				module.ConfigSchemaOf(m),
				module.ConfigGroupsOf(m),
			); err != nil {
				t.Errorf("%s: %v", m.Name(), err)
			}
		})
	}
}

// railAnchorModule is the module the admin settings rail was first migrated
// onto (phase 4). It is named individually — rather than this test asserting
// over a list of "modules that declare groups" — so that phase 5 migrating
// notification/tenant/compliance keeps it passing untouched. Any module may
// declare groups; this one must.
const railAnchorModule = "auth"

// TestConfigGroupsResolveThroughCatalog pins the mechanism that decides
// whether /admin/modules/{name} renders the settings rail at all.
//
// The module's own tests call (&AuthModule{}).ConfigGroups() directly, which
// proves the declaration exists but not that it is *reachable*: the admin
// handler reads it through module.ConfigGroupsOf, which type-asserts
// HasConfigGroups on whatever the catalog factory returned. If a refactor ever
// made NewModule return a type that failed that assertion (a wrapper, or a
// value where a pointer receiver was needed), ConfigGroupsOf would return nil,
// the page would quietly fall back to the flat form, and every existing test
// would still pass — TestEveryGroupHasFields skips a module with zero groups
// rather than failing it.
func TestConfigGroupsResolveThroughCatalog(t *testing.T) {
	var found bool
	for _, m := range buildAllModules(t) {
		if m.Name() != railAnchorModule {
			continue
		}
		found = true

		groups := module.ConfigGroupsOf(m)
		if len(groups) == 0 {
			t.Fatalf("%s: ConfigGroupsOf returned no groups through the catalog — the "+
				"module no longer satisfies module.HasConfigGroups, so /admin/modules/%s "+
				"silently renders the flat form", railAnchorModule, railAnchorModule)
		}

		// The frontend only promotes the whole page to the rail layout when the
		// tree has at least two top-level nodes (`hasPageRail` in
		// configModel.ts). One root would serialize fine and still degrade to
		// the stacked page — the same invisible failure in a different guise.
		roots := 0
		for _, g := range groups {
			if g.Parent == "" {
				roots++
			}
		}
		if roots < 2 {
			t.Errorf("%s: %d top-level group(s), want >= 2 — the admin page needs two roots "+
				"to promote to the rail layout", railAnchorModule, roots)
		}
	}
	if !found {
		t.Fatalf("module %q is not registered in the catalog", railAnchorModule)
	}
}

// TestEveryGroupHasFields catches the inverse defect from
// ValidateConfigDeclarations: a declared group that nothing points at renders
// an empty panel in the admin rail.
func TestEveryGroupHasFields(t *testing.T) {
	for _, m := range buildAllModules(t) {
		groups := module.ConfigGroupsOf(m)
		if len(groups) == 0 {
			continue // flat-form module, nothing to check
		}
		t.Run(m.Name(), func(t *testing.T) {
			used := make(map[string]bool)
			for _, f := range module.ConfigSchemaOf(m) {
				used[f.Group] = true
			}
			// A parent group legitimately holds no fields of its own when it
			// exists only to nest children, so count children as usage.
			for _, g := range groups {
				if g.Parent != "" {
					used[g.Parent] = true
				}
			}
			for _, g := range groups {
				if !used[g.Key] {
					t.Errorf("group %q has no fields and no child groups — it renders an empty panel", g.Key)
				}
			}
		})
	}
}
