package module

import (
	"context"
	"errors"
	"testing"
)

// startFailingModule is a fake module that records whether Start was
// invoked and returns a configurable error. It exists to pin StartAll's
// category-based failure semantics (Task 7.1, PR 7 — versioned upgrade
// reconciliation): a reconciliation read/write failure inside a core
// module's Start (tenant.Module.Start, in the next task) must make
// StartAll return that error so main.go's log.Fatalf aborts the process
// before the HTTP listener/readiness are ever reached. An optional
// module's Start failure must keep behaving the way it always has —
// logged, tracked in FailedModules, startup proceeds — because a fork's
// broken addon must not be able to take the platform down.
type startFailingModule struct {
	name     string
	category ModuleCategory
	startErr error
	started  bool
}

func (m *startFailingModule) Name() string               { return m.name }
func (m *startFailingModule) Category() ModuleCategory   { return m.category }
func (m *startFailingModule) Init(_ *Dependencies) error { return nil }
func (m *startFailingModule) Start(_ context.Context) error {
	m.started = true
	return m.startErr
}

var (
	_ Module    = (*startFailingModule)(nil)
	_ Startable = (*startFailingModule)(nil)
)

// TestStartAll_CoreModuleStartError_AbortsStartup is the RED/GREEN pin for
// Task 7.1: a core module's Start error must propagate out of StartAll
// instead of being logged and swallowed.
func TestStartAll_CoreModuleStartError_AbortsStartup(t *testing.T) {
	boom := errors.New("reconciliation write failed")
	core := &startFailingModule{name: "tenant", category: CategoryCore, startErr: boom}

	r := newTestRegistry(t)
	r.initialized = []Module{core}

	err := r.StartAll(context.Background(), map[string]bool{"tenant": true})
	if err == nil {
		t.Fatalf("StartAll = nil, want an error propagated from the core module's Start failure")
	}
	if !errors.Is(err, boom) {
		t.Errorf("StartAll error = %v, want it to wrap %v", err, boom)
	}
	if !core.started {
		t.Errorf("core module's Start was never invoked")
	}
	if r.IsStarted("tenant") {
		t.Errorf("a core module that failed Start must not be marked started")
	}
}

// TestStartAll_OptionalModuleStartError_StartupProceeds preserves today's
// deliberate tolerance for optional-module Start failures: the error is
// logged and tracked, but StartAll keeps going and returns nil so the rest
// of the platform still boots.
func TestStartAll_OptionalModuleStartError_StartupProceeds(t *testing.T) {
	boom := errors.New("addon exploded")
	broken := &startFailingModule{name: "broken-addon", category: CategoryToggleable, startErr: boom}
	healthy := &startFailingModule{name: "healthy-addon", category: CategoryToggleable}

	r := newTestRegistry(t)
	r.initialized = []Module{broken, healthy}

	err := r.StartAll(context.Background(), map[string]bool{"broken-addon": true, "healthy-addon": true})
	if err != nil {
		t.Fatalf("StartAll = %v, want nil — an optional module's Start failure must not abort startup", err)
	}
	if !broken.started {
		t.Errorf("optional module's Start was never invoked")
	}
	if !healthy.started {
		t.Errorf("StartAll stopped after the optional failure instead of continuing to the next module")
	}
	if r.IsStarted("broken-addon") {
		t.Errorf("the failed optional module must not be marked started")
	}
	if !r.IsStarted("healthy-addon") {
		t.Errorf("the healthy module after it must still be marked started")
	}

	failed := r.FailedModules()
	if got, ok := failed["broken-addon"]; !ok || !errors.Is(got, boom) {
		t.Errorf("FailedModules()[broken-addon] = %v, want it to wrap %v", got, boom)
	}
	if _, ok := failed["healthy-addon"]; ok {
		t.Errorf("healthy optional module unexpectedly present in FailedModules: %v", failed)
	}
}

// TestStartAll_ModuleWithoutStart_StartsCleanly pins that Startable stays
// genuinely optional: a module implementing only the three required
// Module methods (no Start at all — minimalModule from
// module_minimal_test.go) must start cleanly via StartModule's no-op path
// and be marked started, exactly as before this task's change.
func TestStartAll_ModuleWithoutStart_StartsCleanly(t *testing.T) {
	m := minimalModule{name: "no-start"}

	r := newTestRegistry(t)
	r.initialized = []Module{m}

	if err := r.StartAll(context.Background(), map[string]bool{"no-start": true}); err != nil {
		t.Fatalf("StartAll = %v, want nil for a module without Start", err)
	}
	if !r.IsStarted("no-start") {
		t.Errorf("a module without Start should still be marked started")
	}
}
