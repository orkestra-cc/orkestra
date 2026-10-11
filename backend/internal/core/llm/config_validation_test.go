package llm

import (
	"context"
	"errors"
	"testing"

	"github.com/orkestra/backend/internal/shared/errcode"
	"github.com/orkestra/backend/pkg/sdk/module"
)

func snap(eff map[string]string) module.ConfigValidationSnapshot {
	return module.ConfigValidationSnapshot{Values: eff, EffectiveValues: eff}
}

func TestValidateConfigSnapshot(t *testing.T) {
	loopback := "http://127.0.0.1:3007"
	cases := []struct {
		name      string
		url       string
		prodLike  bool
		eff       map[string]string
		wantField string // empty = accepted
	}{
		{"absent mode means disabled", "", false, map[string]string{}, ""},
		{"explicit disabled", "", false, map[string]string{"siwc_mode": "disabled", "allow_hosted": "false"}, ""},
		{"loopback with loopback url", loopback, false, map[string]string{"siwc_mode": "loopback_local", "allow_hosted": "true"}, ""},
		{"allow_hosted accepts 1", loopback, false, map[string]string{"siwc_mode": "loopback_local", "allow_hosted": "1"}, ""},
		{"loopback without allow_hosted", loopback, false, map[string]string{"siwc_mode": "loopback_local", "allow_hosted": "false"}, "siwc_mode"},
		{"loopback with absent allow_hosted", loopback, false, map[string]string{"siwc_mode": "loopback_local"}, "siwc_mode"},
		{"loopback with https console url", "https://console.example.test", false, map[string]string{"siwc_mode": "loopback_local", "allow_hosted": "true"}, "siwc_mode"},
		{"loopback with localhost", "http://localhost:3000", false, map[string]string{"siwc_mode": "loopback_local", "allow_hosted": "true"}, "siwc_mode"},
		{"allow_hosted yes is true like the runtime", loopback, false, map[string]string{"siwc_mode": "loopback_local", "allow_hosted": "yes"}, ""},
		{"allow_hosted TRUE is false like the runtime", loopback, false, map[string]string{"siwc_mode": "loopback_local", "allow_hosted": "TRUE"}, "siwc_mode"},
		{"allow_hosted t is false like the runtime", loopback, false, map[string]string{"siwc_mode": "loopback_local", "allow_hosted": "t"}, "siwc_mode"},
		{"allow_hosted empty is false", loopback, false, map[string]string{"siwc_mode": "loopback_local", "allow_hosted": ""}, "siwc_mode"},
		{"unknown mode", "", false, map[string]string{"siwc_mode": "hosted", "allow_hosted": "true"}, "siwc_mode"},
		{"paste default port", "", false, map[string]string{"siwc_mode": "hosted_paste", "allow_hosted": "true"}, ""},
		{"paste low port", "", false, map[string]string{"siwc_mode": "hosted_paste", "allow_hosted": "true", "siwc_paste_port": "80"}, "siwc_paste_port"},
		{"paste non-numeric port", "", false, map[string]string{"siwc_mode": "hosted_paste", "allow_hosted": "true", "siwc_paste_port": "abc"}, "siwc_paste_port"},
		{"paste over http in production", "http://console.example.test", true, map[string]string{"siwc_mode": "hosted_paste", "allow_hosted": "true"}, "siwc_mode"},
		{"paste over https in production", "https://console.example.test", true, map[string]string{"siwc_mode": "hosted_paste", "allow_hosted": "true"}, ""},
		{"stale paste port ignored when disabled", "", false, map[string]string{"siwc_mode": "disabled", "siwc_paste_port": "abc"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := NewModule(WithOperatorPublicURL(tc.url))
			m.prodLike = tc.prodLike
			err := m.ValidateConfigSnapshot(context.Background(), snap(tc.eff))
			if tc.wantField == "" {
				if err != nil {
					t.Fatalf("rejected: %v", err)
				}
				return
			}
			var cve *module.ConfigValidationError
			if !errors.As(err, &cve) {
				t.Fatalf("err = %v, want *ConfigValidationError", err)
			}
			if cve.Field != tc.wantField || cve.Code != errcode.LLMSIWCConfigInvalid {
				t.Fatalf("field=%q code=%q, want %q/%q", cve.Field, cve.Code, tc.wantField, errcode.LLMSIWCConfigInvalid)
			}
		})
	}
}

func TestNewModule_OperatorPublicURLOption(t *testing.T) {
	if got := NewModule(WithOperatorPublicURL(" http://127.0.0.1:3007 ")).operatorPublicURL; got != "http://127.0.0.1:3007" {
		t.Fatalf("operatorPublicURL = %q", got)
	}
	if NewModule().operatorPublicURL != "" {
		t.Fatal("default must be empty")
	}
}

// Pins runtimeBool to the GetConfigBool predicate (module.go): a validator
// that disagrees would accept a SIWC mode the runtime treats as hosted-off.
func TestRuntimeBool_ParityWithGetConfigBool(t *testing.T) {
	for v, want := range map[string]bool{
		"true": true, "1": true, "yes": true,
		"TRUE": false, "True": false, "t": false, "T": false, "false": false, "0": false, "no": false, "": false, " true": false,
	} {
		if got := runtimeBool(v); got != want {
			t.Errorf("runtimeBool(%q) = %v, want %v", v, got, want)
		}
	}
}
