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
