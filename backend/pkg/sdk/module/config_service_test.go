package module

import (
	"io"
	"log/slog"
	"sort"
	"testing"
)

// stubModule is a minimal Module that returns the given name and category;
// used by tests that only exercise computeProfileOverride's classification.
type stubModule struct {
	BaseModule
	name     string
	category ModuleCategory
}

func (s stubModule) Name() string              { return s.name }
func (s stubModule) Category() ModuleCategory  { return s.category }
func (stubModule) Init(_ *Dependencies) error  { return nil }
func (stubModule) RegisterRoutes(_ *RouteInfo) {}

func newTestService() *ModuleConfigService {
	return &ModuleConfigService{
		logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		coreModules:  make(map[string]bool),
		knownModules: make(map[string]Module),
	}
}

// allOptionalModules returns the canonical optional-module set used by every
// test case below. Mirrors what optional addons compile into the full binary
// today; if the catalog grows, append here.
func allOptionalModules() []Module {
	return []Module{
		stubModule{name: "user", category: CategoryCore},
		stubModule{name: "auth", category: CategoryCore},
		stubModule{name: "billing", category: CategoryToggleable},
		stubModule{name: "documents", category: CategoryToggleable},
		stubModule{name: "company", category: CategoryToggleable},
		stubModule{name: "graph", category: CategoryExternal},
		stubModule{name: "aimodels", category: CategoryToggleable},
		stubModule{name: "rag", category: CategoryToggleable},
		stubModule{name: "agents", category: CategoryExternal},
		stubModule{name: "sales", category: CategoryToggleable},
		stubModule{name: "subscriptions", category: CategoryToggleable},
		stubModule{name: "payments", category: CategoryToggleable},
		stubModule{name: "compliance", category: CategoryToggleable},
		stubModule{name: "identity", category: CategoryToggleable},
		stubModule{name: "marketing", category: CategoryToggleable},
		stubModule{name: "dev", category: CategoryToggleable},
	}
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestComputeProfileOverride_Unset(t *testing.T) {
	t.Setenv("ORKESTRA_PROFILE", "")
	s := newTestService()
	got := s.computeProfileOverride(allOptionalModules())
	if got != nil {
		t.Fatalf("expected nil override when env unset, got %v", got)
	}
}

func TestComputeProfileOverride_Unknown(t *testing.T) {
	t.Setenv("ORKESTRA_PROFILE", "bogus")
	s := newTestService()
	got := s.computeProfileOverride(allOptionalModules())
	if got != nil {
		t.Fatalf("expected nil override for unknown profile, got %v", got)
	}
}

func TestComputeProfileOverride_MinimalEmpty(t *testing.T) {
	t.Setenv("ORKESTRA_PROFILE", "minimal")
	s := newTestService()
	got := s.computeProfileOverride(allOptionalModules())
	if got == nil {
		t.Fatalf("expected non-nil empty override for minimal, got nil")
	}
	if len(got) != 0 {
		t.Fatalf("expected empty override for minimal, got %v", keys(got))
	}
}

func TestComputeProfileOverride_FullSentinel(t *testing.T) {
	t.Setenv("ORKESTRA_PROFILE", "full")
	s := newTestService()
	got := s.computeProfileOverride(allOptionalModules())
	// Every non-core, non-dev module from the binary should appear.
	want := []string{
		"agents", "aimodels", "billing", "company", "compliance",
		"documents", "graph", "identity", "marketing", "payments",
		"rag", "sales", "subscriptions",
	}
	if !equalSlices(keys(got), want) {
		t.Fatalf("full override = %v, want %v", keys(got), want)
	}
	// Specific exclusions: core + dev.
	for _, name := range []string{"user", "auth", "dev"} {
		if got[name] {
			t.Errorf("full override should exclude %q, got it enabled", name)
		}
	}
}

func TestComputeProfileOverride_Whitespace(t *testing.T) {
	t.Setenv("ORKESTRA_PROFILE", "  full  ")
	s := newTestService()
	got := s.computeProfileOverride(allOptionalModules())
	if got == nil {
		t.Fatalf("expected non-nil override for trimmed full, got nil")
	}
	if len(got) == 0 {
		t.Fatalf("expected non-empty override for trimmed full, got empty")
	}
}
