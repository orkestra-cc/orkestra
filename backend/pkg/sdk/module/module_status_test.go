package module

import (
	"log/slog"
	"testing"
)

func TestModuleRegistry_IsAModuleStatusReader(t *testing.T) {
	r := NewModuleRegistry(slog.Default())
	reg := NewServiceRegistry()
	reg.Register(ServiceModuleStatus, r)

	ms, ok := GetTyped[ModuleStatusReader](reg, ServiceModuleStatus)
	if !ok {
		t.Fatal("ModuleStatusReader not resolvable from ServiceModuleStatus")
	}
	if ms.IsStarted("crm") {
		t.Fatal("IsStarted(crm) = true on an empty registry")
	}
}
