package services

import (
	"context"
	"testing"

	"github.com/orkestra/backend/internal/core/tenant/models"
)

// TestProvisioningMode_InternalFailsClosedToManual locks in the Tier-1
// fail-closed invariant: missing, unknown, or a legacy stored `open` value
// all normalise to manual — `open` is no longer a valid internal mode. An
// explicit `single` value is preserved (the cardinality mode, not authority).
func TestProvisioningMode_InternalFailsClosedToManual(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"", "open", "garbage", " open "} {
		s := New(nil)
		s.SetProvisioningModeResolver(func(context.Context, models.TenantKind) string { return raw })
		if got := s.ProvisioningMode(context.Background(), models.TenantKindInternal); got != models.ProvisioningModeManual {
			t.Fatalf("internal %q → %q, want manual", raw, got)
		}
	}
	s := New(nil)
	s.SetProvisioningModeResolver(func(context.Context, models.TenantKind) string { return models.ProvisioningModeSingle })
	if got := s.ProvisioningMode(context.Background(), models.TenantKindInternal); got != models.ProvisioningModeSingle {
		t.Fatalf("single not preserved: %q", got)
	}
}

// TestProvisioningMode_ExternalUnchanged locks in that Tier-2 keeps its
// historical fail-open default: with no resolver wired, external resolves to
// open, unchanged by the Tier-1 fail-closed change in this task.
func TestProvisioningMode_ExternalUnchanged(t *testing.T) {
	t.Parallel()
	s := New(nil) // no resolver wired
	if got := s.ProvisioningMode(context.Background(), models.TenantKindExternal); got != models.ProvisioningModeOpen {
		t.Fatalf("external default drifted: %q", got)
	}
}
