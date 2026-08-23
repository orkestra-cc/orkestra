package tenant

import (
	"context"
	"errors"
	"testing"

	"github.com/orkestra/backend/internal/core/tenant/models"
	"github.com/orkestra/backend/internal/shared/errcode"
	"github.com/orkestra/backend/pkg/sdk/module"
)

// provisioningCase exercises the Tier-1 provisioning policy shared by
// ValidateConfig and ValidateConfigActivation. wantCountCalled asserts the
// slot-counter seam is (or is not) consulted — manual/absent must short
// circuit before ever touching it.
type provisioningCase struct {
	name            string
	values          map[string]string
	count           int64
	wantField       string
	wantCode        string
	wantCountCalled bool
}

var provisioningCases = []provisioningCase{
	{name: "absent key", values: map[string]string{}},
	{name: "empty mode", values: map[string]string{"provisioning.internal.mode": ""}},
	{name: "manual", values: map[string]string{"provisioning.internal.mode": "manual"}},
	{name: "manual with surrounding whitespace", values: map[string]string{"provisioning.internal.mode": "  manual  "}},
	{
		name: "open rejected as invalid tier-1 mode", values: map[string]string{"provisioning.internal.mode": "open"},
		wantField: "provisioning.internal.mode", wantCode: errcode.TenantInternalModeInvalid,
	},
	{
		name: "unknown mode rejected", values: map[string]string{"provisioning.internal.mode": "bogus"},
		wantField: "provisioning.internal.mode", wantCode: errcode.TenantInternalModeInvalid,
	},
	{
		name: "single with two occupied slots rejected", values: map[string]string{"provisioning.internal.mode": "single"},
		count: 2, wantField: "provisioning.internal.mode", wantCode: errcode.TenantSingleModeConflict, wantCountCalled: true,
	},
	{
		name: "single with one occupied slot accepted", values: map[string]string{"provisioning.internal.mode": "single"},
		count: 1, wantCountCalled: true,
	},
	{
		name: "single with zero occupied slots accepted", values: map[string]string{"provisioning.internal.mode": "single"},
		count: 0, wantCountCalled: true,
	},
}

// provisioningValidatorMethods lets every case run against both hooks so a
// divergence between the PATCH-time and activation-time policy is caught —
// they must share one function, not duplicate the logic.
var provisioningValidatorMethods = []struct {
	name string
	call func(m *Module, ctx context.Context, values map[string]string) error
}{
	{"ValidateConfig", func(m *Module, ctx context.Context, values map[string]string) error {
		return m.ValidateConfig(ctx, values)
	}},
	{"ValidateConfigActivation", func(m *Module, ctx context.Context, values map[string]string) error {
		return m.ValidateConfigActivation(ctx, values)
	}},
}

func TestTenantProvisioningPolicyValidation(t *testing.T) {
	for _, tc := range provisioningCases {
		for _, meth := range provisioningValidatorMethods {
			t.Run(tc.name+"/"+meth.name, func(t *testing.T) {
				called := false
				m := &Module{slotCount: func(_ context.Context, kind models.TenantKind) (int64, error) {
					called = true
					if kind != models.TenantKindInternal {
						t.Fatalf("slotCount kind = %v, want %v", kind, models.TenantKindInternal)
					}
					return tc.count, nil
				}}

				err := meth.call(m, context.Background(), tc.values)

				if called != tc.wantCountCalled {
					t.Errorf("slotCount called = %v, want %v", called, tc.wantCountCalled)
				}

				if tc.wantField == "" {
					if err != nil {
						t.Fatalf("%s(%v) = %v, want nil", meth.name, tc.values, err)
					}
					return
				}

				var typed *module.ConfigValidationError
				if !errors.As(err, &typed) {
					t.Fatalf("%s(%v) = %v, want *ConfigValidationError", meth.name, tc.values, err)
				}
				if typed.Field != tc.wantField {
					t.Errorf("Field = %q, want %q", typed.Field, tc.wantField)
				}
				if typed.Code != tc.wantCode {
					t.Errorf("Code = %q, want %q", typed.Code, tc.wantCode)
				}
			})
		}
	}
}

// TestTenantProvisioningPolicyValidation_CounterError verifies a slot-count
// failure surfaces as a plain wrapped error, never a *ConfigValidationError
// — a database outage must not be reported to the operator as bad input.
func TestTenantProvisioningPolicyValidation_CounterError(t *testing.T) {
	boom := errors.New("mongo down")
	for _, meth := range provisioningValidatorMethods {
		t.Run(meth.name, func(t *testing.T) {
			m := &Module{slotCount: func(_ context.Context, _ models.TenantKind) (int64, error) {
				return 0, boom
			}}

			err := meth.call(m, context.Background(), map[string]string{"provisioning.internal.mode": "single"})

			if err == nil {
				t.Fatal("want error, got nil")
			}
			var typed *module.ConfigValidationError
			if errors.As(err, &typed) {
				t.Fatalf("got *ConfigValidationError (%+v), want a plain wrapped error", typed)
			}
			if !errors.Is(err, boom) {
				t.Errorf("error chain does not wrap the counter error: %v", err)
			}
		})
	}
}

func TestTenantModuleImplementsConfigValidatorHooks(t *testing.T) {
	var _ module.HasConfigValidator = (*Module)(nil)
	var _ module.HasConfigActivationValidator = (*Module)(nil)
}
