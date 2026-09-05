package setup

// Payload validation for POST /v1/setup/finalize that the request schema
// cannot express. Huma's minLength:"1" applies to the RAW string, so a
// whitespace-only tenantName passes it; normalizeFinalize then collapses
// it to "". Nothing downstream re-checks — createTenantWithUUID only
// TrimSpaces the name — so setup could complete against a Tier-1 tenant
// with an empty name, and the reservation hash would be computed over that
// empty value, making every whitespace variant replay as "the same
// request".

import (
	"context"
	"errors"
	"testing"

	"github.com/orkestra/backend/internal/shared/systeminit"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// TestFinalize_BlankTenantName_Rejected pins the normalized value, not the
// raw one, as what must be non-empty — and pins it BEFORE any stage runs,
// so a blank name can never reach the reservation or the tenant seam.
func TestFinalize_BlankTenantName_Rejected(t *testing.T) {
	blanks := []struct {
		name string
		in   string
	}{
		{"spaces", "   "},
		{"tab", "\t"},
		{"newline", "\n"},
		{"mixed whitespace", " \t \n "},
	}
	for _, tc := range blanks {
		t.Run(tc.name, func(t *testing.T) {
			fx := newSagaFixture(
				&systeminit.FinalizationRecord{AdminUUID: "admin-1", Source: systeminit.SourceFresh, Stage: systeminit.StageConfig, Revision: 1},
				map[string]iface.UserLifecycleState{"admin-1": iface.UserLifecycleActive},
			)

			in := testInput(true)
			in.TenantName = tc.in
			fx.users.withRoles("admin-1", "super_admin")
			_, err := fx.svc.Finalize(context.Background(), "admin-1", in)
			if !errors.Is(err, ErrFinalizationTenantNameRequired) {
				t.Fatalf("Finalize(tenantName=%q) = %v, want ErrFinalizationTenantNameRequired", tc.in, err)
			}

			// Rejected before anything was reserved or provisioned.
			if got := fx.log.only("config.", "tenants."); len(got) != 0 {
				t.Fatalf("a blank tenant name still ran saga effects: %v", got)
			}
			if rec := fx.store.snapshotRecord(); rec.RequestHash != "" {
				t.Fatalf("a blank tenant name still reserved a request (hash %q)", rec.RequestHash)
			}
		})
	}
}

// TestFinalize_BlankTenantSlug_Rejected covers the same guard on the slug.
// The route's `pattern` makes a blank slug unreachable over HTTP today,
// which is exactly why the service-level check matters: it keeps the
// invariant true for non-HTTP callers and survives a future pattern change.
func TestFinalize_BlankTenantSlug_Rejected(t *testing.T) {
	fx := newSagaFixture(
		&systeminit.FinalizationRecord{AdminUUID: "admin-1", Source: systeminit.SourceFresh, Stage: systeminit.StageConfig, Revision: 1},
		map[string]iface.UserLifecycleState{"admin-1": iface.UserLifecycleActive},
	)

	in := testInput(true)
	in.TenantSlug = "   "
	fx.users.withRoles("admin-1", "super_admin")
	if _, err := fx.svc.Finalize(context.Background(), "admin-1", in); !errors.Is(err, ErrFinalizationTenantSlugRequired) {
		t.Fatalf("Finalize(blank slug) = %v, want ErrFinalizationTenantSlugRequired", err)
	}
}

// TestFinalize_NamePreservedWhenOnlyPadded guards the boundary: collapsing
// whitespace is normalization, not rejection. A name that still has content
// after collapsing must go through untouched.
func TestFinalize_NamePreservedWhenOnlyPadded(t *testing.T) {
	fx := newSagaFixture(
		&systeminit.FinalizationRecord{AdminUUID: "admin-1", Source: systeminit.SourceFresh, Stage: systeminit.StageConfig, Revision: 1},
		map[string]iface.UserLifecycleState{"admin-1": iface.UserLifecycleActive},
	)

	in := testInput(true)
	in.TenantName = "  \t Acme   Corp \n "
	fx.users.withRoles("admin-1", "super_admin")
	res, err := fx.svc.Finalize(context.Background(), "admin-1", in)
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if res.TenantName != "Acme Corp" {
		t.Fatalf("normalized name = %q, want %q", res.TenantName, "Acme Corp")
	}
}
