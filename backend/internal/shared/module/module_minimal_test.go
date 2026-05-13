package module

import (
	"context"
	"testing"
)

// minimalModule implements only the three Module-interface methods
// (Name / Category / Init) and explicitly does NOT embed BaseModule. It
// proves that the Phase 1b reshape leaves a callable minimum: addons
// extracted from the monorepo can implement just these three methods
// and still be registered, seeded, and routed by the registry.
type minimalModule struct {
	name string
}

func (m minimalModule) Name() string             { return m.name }
func (m minimalModule) Category() ModuleCategory { return CategoryToggleable }
func (m minimalModule) Init(_ *Dependencies) error {
	return nil
}

// Compile-time assertion: minimalModule satisfies Module without
// implementing any of the optional sub-interfaces.
var _ Module = minimalModule{}

func TestMinimalModule_ImplementsModuleOnly(t *testing.T) {
	m := minimalModule{name: "minimal"}

	// All optional sub-interfaces must report "not implemented".
	if _, ok := any(m).(HasDisplayInfo); ok {
		t.Error("minimalModule unexpectedly implements HasDisplayInfo")
	}
	if _, ok := any(m).(HasConfigSchema); ok {
		t.Error("minimalModule unexpectedly implements HasConfigSchema")
	}
	if _, ok := any(m).(HasCollections); ok {
		t.Error("minimalModule unexpectedly implements HasCollections")
	}
	if _, ok := any(m).(HasNavItems); ok {
		t.Error("minimalModule unexpectedly implements HasNavItems")
	}
	if _, ok := any(m).(HasPermissions); ok {
		t.Error("minimalModule unexpectedly implements HasPermissions")
	}
	if _, ok := any(m).(HasCapabilities); ok {
		t.Error("minimalModule unexpectedly implements HasCapabilities")
	}
	if _, ok := any(m).(HasDependencies); ok {
		t.Error("minimalModule unexpectedly implements HasDependencies")
	}
	if _, ok := any(m).(HasServiceContracts); ok {
		t.Error("minimalModule unexpectedly implements HasServiceContracts")
	}
	if _, ok := any(m).(HasDefaultEnabled); ok {
		t.Error("minimalModule unexpectedly implements HasDefaultEnabled")
	}
	if _, ok := any(m).(HotReloadable); ok {
		t.Error("minimalModule unexpectedly implements HotReloadable")
	}
	if _, ok := any(m).(Routable); ok {
		t.Error("minimalModule unexpectedly implements Routable")
	}
	if _, ok := any(m).(Startable); ok {
		t.Error("minimalModule unexpectedly implements Startable")
	}
	if _, ok := any(m).(Stoppable); ok {
		t.Error("minimalModule unexpectedly implements Stoppable")
	}
	if _, ok := any(m).(HealthCheckable); ok {
		t.Error("minimalModule unexpectedly implements HealthCheckable")
	}
	if _, ok := any(m).(HasInfraContainers); ok {
		t.Error("minimalModule unexpectedly implements HasInfraContainers")
	}
	if _, ok := any(m).(HasPreflight); ok {
		t.Error("minimalModule unexpectedly implements HasPreflight")
	}
}

func TestOptionalAccessors_DefaultsForMinimalModule(t *testing.T) {
	m := minimalModule{name: "minimal"}
	ctx := context.Background()

	if got := DisplayNameOf(m); got != "minimal" {
		t.Errorf("DisplayNameOf = %q, want %q (falls back to Name)", got, "minimal")
	}
	if got := DescriptionOf(m); got != "" {
		t.Errorf("DescriptionOf = %q, want empty", got)
	}
	if got := ConfigSchemaOf(m); got != nil {
		t.Errorf("ConfigSchemaOf = %v, want nil", got)
	}
	if got := CollectionsOf(m); got != nil {
		t.Errorf("CollectionsOf = %v, want nil", got)
	}
	if got := NavItemsOf(m); got != nil {
		t.Errorf("NavItemsOf = %v, want nil", got)
	}
	if got := PermissionsOf(m); got != nil {
		t.Errorf("PermissionsOf = %v, want nil", got)
	}
	if got := CapabilitiesOf(m); got != nil {
		t.Errorf("CapabilitiesOf = %v, want nil", got)
	}
	if got := DependenciesOf(m); got != nil {
		t.Errorf("DependenciesOf = %v, want nil", got)
	}
	if got := ProvidedServicesOf(m); got != nil {
		t.Errorf("ProvidedServicesOf = %v, want nil", got)
	}
	if got := RequiredServicesOf(m); got != nil {
		t.Errorf("RequiredServicesOf = %v, want nil", got)
	}
	if got := OptionalServicesOf(m); got != nil {
		t.Errorf("OptionalServicesOf = %v, want nil", got)
	}
	if !EnabledByDefault(m) {
		t.Errorf("EnabledByDefault = false, want true (default)")
	}
	if HotReloadsConfig(m) {
		t.Errorf("HotReloadsConfig = true, want false (default)")
	}
	if got := InfraContainersOf(m); got != nil {
		t.Errorf("InfraContainersOf = %v, want nil", got)
	}

	// Lifecycle invocations on optional helpers must be no-ops for a
	// minimal module — exercises the type-assertion-then-skip path.
	if err := StartModule(ctx, m); err != nil {
		t.Errorf("StartModule = %v, want nil", err)
	}
	if err := StopModule(ctx, m); err != nil {
		t.Errorf("StopModule = %v, want nil", err)
	}
	if err := CheckHealth(ctx, m); err != nil {
		t.Errorf("CheckHealth = %v, want nil", err)
	}
	if err := Preflight(ctx, m); err != nil {
		t.Errorf("Preflight = %v, want nil", err)
	}
	// RegisterRoutes is no-op when Routable not implemented; passing nil
	// must not panic.
	RegisterRoutes(m, nil)
}

// optionallyEnabledModule implements Module + HasDefaultEnabled to
// override the default. Exercises that EnabledByDefault picks up the
// override when the sub-interface is implemented.
type optionallyEnabledModule struct {
	name string
}

func (m optionallyEnabledModule) Name() string               { return m.name }
func (m optionallyEnabledModule) Category() ModuleCategory   { return CategoryToggleable }
func (m optionallyEnabledModule) Init(_ *Dependencies) error { return nil }
func (m optionallyEnabledModule) Enabled() bool              { return false }

var _ HasDefaultEnabled = optionallyEnabledModule{}

func TestHasDefaultEnabled_Override(t *testing.T) {
	m := optionallyEnabledModule{name: "opt"}
	if EnabledByDefault(m) {
		t.Errorf("EnabledByDefault = true, want false (sub-interface override)")
	}
}

// displayedModule exercises HasDisplayInfo — verifies the override
// path through DisplayNameOf / DescriptionOf.
type displayedModule struct {
	name string
}

func (m displayedModule) Name() string               { return m.name }
func (m displayedModule) Category() ModuleCategory   { return CategoryToggleable }
func (m displayedModule) Init(_ *Dependencies) error { return nil }
func (m displayedModule) DisplayName() string        { return "Fancy Display" }
func (m displayedModule) Description() string        { return "A module with cosmetics." }

var _ HasDisplayInfo = displayedModule{}

func TestHasDisplayInfo_Override(t *testing.T) {
	m := displayedModule{name: "displayed"}
	if got := DisplayNameOf(m); got != "Fancy Display" {
		t.Errorf("DisplayNameOf = %q, want %q", got, "Fancy Display")
	}
	if got := DescriptionOf(m); got != "A module with cosmetics." {
		t.Errorf("DescriptionOf = %q, want override", got)
	}
}

// BaseModule embedding test — proves that the ergonomic helper path
// (embed BaseModule, satisfy every sub-interface for free) still works
// post-reshape. Today's 14 concrete addons rely on this pattern.
type baseEmbeddedModule struct {
	BaseModule
}

func (baseEmbeddedModule) Name() string             { return "base-embedded" }
func (baseEmbeddedModule) Category() ModuleCategory { return CategoryToggleable }
func (baseEmbeddedModule) Init(_ *Dependencies) error {
	return nil
}

var _ Module = baseEmbeddedModule{}

func TestBaseModule_SatisfiesEverySubInterface(t *testing.T) {
	m := baseEmbeddedModule{}

	subInterfaces := []struct {
		name string
		ok   bool
	}{
		{"HasDisplayInfo", func() bool { _, ok := any(m).(HasDisplayInfo); return ok }()},
		{"HasConfigSchema", func() bool { _, ok := any(m).(HasConfigSchema); return ok }()},
		{"HasCollections", func() bool { _, ok := any(m).(HasCollections); return ok }()},
		{"HasNavItems", func() bool { _, ok := any(m).(HasNavItems); return ok }()},
		{"HasPermissions", func() bool { _, ok := any(m).(HasPermissions); return ok }()},
		{"HasCapabilities", func() bool { _, ok := any(m).(HasCapabilities); return ok }()},
		{"HasDependencies", func() bool { _, ok := any(m).(HasDependencies); return ok }()},
		{"HasServiceContracts", func() bool { _, ok := any(m).(HasServiceContracts); return ok }()},
		{"HasDefaultEnabled", func() bool { _, ok := any(m).(HasDefaultEnabled); return ok }()},
		{"HotReloadable", func() bool { _, ok := any(m).(HotReloadable); return ok }()},
		{"Startable", func() bool { _, ok := any(m).(Startable); return ok }()},
		{"Stoppable", func() bool { _, ok := any(m).(Stoppable); return ok }()},
		{"HealthCheckable", func() bool { _, ok := any(m).(HealthCheckable); return ok }()},
		{"HasInfraContainers", func() bool { _, ok := any(m).(HasInfraContainers); return ok }()},
		{"HasPreflight", func() bool { _, ok := any(m).(HasPreflight); return ok }()},
	}

	for _, sub := range subInterfaces {
		if !sub.ok {
			t.Errorf("BaseModule should satisfy %s but does not", sub.name)
		}
	}
}
