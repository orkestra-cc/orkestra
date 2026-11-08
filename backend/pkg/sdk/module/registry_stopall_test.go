package module

import (
	"context"
	"testing"
)

// stopProbeModule records, when its Stop runs, whether the registry still
// reports it started.
type stopProbeModule struct {
	name             string
	r                *ModuleRegistry
	stopped          bool
	startedAtStopped bool
}

func (m *stopProbeModule) Name() string                  { return m.name }
func (m *stopProbeModule) Category() ModuleCategory      { return CategoryToggleable }
func (m *stopProbeModule) Init(_ *Dependencies) error    { return nil }
func (m *stopProbeModule) Start(_ context.Context) error { return nil }
func (m *stopProbeModule) Stop(_ context.Context) error {
	m.stopped = true
	m.startedAtStopped = m.r.IsStarted(m.name)
	return nil
}

var (
	_ Startable = (*stopProbeModule)(nil)
	_ Stoppable = (*stopProbeModule)(nil)
)

// StopAll marks every module not started, as StopModule does, and already
// while the module's Stop runs: a consumer reading IsStarted during shutdown
// (the jobs scheduler) must not keep using a module that is stopping.
func TestStopAll_MarksModulesNotStarted(t *testing.T) {
	r := newTestRegistry(t)
	a := &stopProbeModule{name: "a", r: r}
	b := &stopProbeModule{name: "b", r: r}
	r.initialized = []Module{a, b}
	if err := r.StartAll(context.Background(), map[string]bool{"a": true, "b": true}); err != nil {
		t.Fatal(err)
	}
	if !r.IsStarted("a") || !r.IsStarted("b") {
		t.Fatal("modules not started")
	}
	r.StopAll(context.Background())
	for _, m := range []*stopProbeModule{a, b} {
		if !m.stopped {
			t.Errorf("%s: Stop not called", m.name)
		}
		if m.startedAtStopped {
			t.Errorf("%s: IsStarted was still true while its Stop ran", m.name)
		}
		if r.IsStarted(m.name) {
			t.Errorf("%s: IsStarted true after StopAll", m.name)
		}
	}
	if got := r.EnabledModules(); len(got) != 0 {
		t.Errorf("EnabledModules after StopAll = %v, want none", got)
	}
}
