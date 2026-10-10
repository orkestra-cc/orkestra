package main

import (
	"slices"
	"testing"

	"github.com/orkestra/backend/internal/shared/config"
	"github.com/orkestra/backend/pkg/sdk/module"
)

// TestCoreModulesLoadOrder pins the init order of the core modules.
// llm must init before compliance: compliance pushes the KMS provider and the
// audit sink into the llm gateway (module.ServiceLLMGateway) from its own
// Init, so the gateway has to be registered by then. compliance does not
// declare a dependency on llm (the gateway is optional for it), which makes
// the order a property of the catalog that only a test can hold.
func TestCoreModulesLoadOrder(t *testing.T) {
	// main.go registers the core modules one by one (Register, not
	// RegisterAll), so the catalog order is the init order; mirror that.
	reg := module.NewModuleRegistry(nil)
	for _, f := range coreModules(&config.Config{}) {
		reg.Register(f())
	}
	var got []string
	for _, m := range reg.AllModules() {
		got = append(got, m.Name())
	}
	for _, m := range reg.AllModules() {
		pos := slices.Index(got, m.Name())
		for _, dep := range module.DependenciesOf(m) {
			if d := slices.Index(got, dep); d < 0 || d > pos {
				t.Errorf("%s inits before its dependency %s (order %v)", m.Name(), dep, got)
			}
		}
	}
	want := []string{"user", "notification", "tenant", "authz", "auth", "navigation", "logging", "llm", "compliance"}
	if !slices.Equal(got, want) {
		t.Fatalf("core load order = %v, want %v", got, want)
	}
}
