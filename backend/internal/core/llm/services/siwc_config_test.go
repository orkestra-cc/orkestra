package services

import (
	"errors"
	"testing"
)

func TestSIWCConfig_RedirectURI(t *testing.T) {
	c := SIWCConfig{Mode: SIWCModeLoopbackLocal, OperatorPublicURL: "http://127.0.0.1:3007", AllowHosted: true}
	if uri, err := c.RedirectURI(); err != nil || uri != "http://127.0.0.1:3007/v1/llm/accounts/openai/callback" {
		t.Fatalf("loopback = %q, %v", uri, err)
	}
	c = SIWCConfig{Mode: SIWCModeHostedPaste, PastePort: 14555, OperatorPublicURL: "https://console.example.test", AllowHosted: true, ProductionLike: true}
	if uri, err := c.RedirectURI(); err != nil || uri != "http://127.0.0.1:14555/v1/llm/accounts/openai/callback" {
		t.Fatalf("paste = %q, %v", uri, err)
	}
	if _, err := (SIWCConfig{Mode: SIWCModeDisabled}).RedirectURI(); !errors.Is(err, ErrSIWCDisabled) {
		t.Fatalf("disabled err = %v", err)
	}
	c = SIWCConfig{Mode: SIWCModeLoopbackLocal, OperatorPublicURL: "http://localhost:3000", AllowHosted: true}
	if _, err := c.RedirectURI(); !errors.Is(err, ErrSIWCUnsupportedDeployment) {
		t.Fatalf("localhost err = %v", err)
	}
}

func TestSIWCConfig_Mounting(t *testing.T) {
	for mode, want := range map[string][2]bool{
		SIWCModeDisabled:      {false, false},
		SIWCModeLoopbackLocal: {true, true},
		SIWCModeHostedPaste:   {true, false},
	} {
		c := SIWCConfig{Mode: mode}
		if c.Enabled() != want[0] || c.CallbackRouteMounted() != want[1] {
			t.Errorf("%s: enabled=%v mounted=%v", mode, c.Enabled(), c.CallbackRouteMounted())
		}
	}
}

func TestValidateSIWCConfig(t *testing.T) {
	bad := []struct {
		c     SIWCConfig
		field string
		kind  error
	}{
		{SIWCConfig{Mode: "hosted", AllowHosted: true}, SIWCFieldMode, ErrSIWCConfigInvalid},
		{SIWCConfig{Mode: SIWCModeLoopbackLocal, OperatorPublicURL: "http://127.0.0.1:3000"}, SIWCFieldMode, ErrSIWCConfigInvalid},
		{SIWCConfig{Mode: SIWCModeHostedPaste, PastePort: 14555}, SIWCFieldMode, ErrSIWCConfigInvalid},
		{SIWCConfig{Mode: SIWCModeLoopbackLocal, AllowHosted: true, OperatorPublicURL: "http://localhost:3000"}, SIWCFieldMode, ErrSIWCUnsupportedDeployment},
		{SIWCConfig{Mode: SIWCModeLoopbackLocal, AllowHosted: true, OperatorPublicURL: "https://127.0.0.1:3000"}, SIWCFieldMode, ErrSIWCUnsupportedDeployment},
		{SIWCConfig{Mode: SIWCModeLoopbackLocal, AllowHosted: true, OperatorPublicURL: "http://10.0.0.5:3000"}, SIWCFieldMode, ErrSIWCUnsupportedDeployment},
		{SIWCConfig{Mode: SIWCModeLoopbackLocal, AllowHosted: true}, SIWCFieldMode, ErrSIWCUnsupportedDeployment},
		{SIWCConfig{Mode: SIWCModeHostedPaste, AllowHosted: true, PastePort: 80}, SIWCFieldPastePort, ErrSIWCConfigInvalid},
		{SIWCConfig{Mode: SIWCModeHostedPaste, AllowHosted: true, PastePort: 70000}, SIWCFieldPastePort, ErrSIWCConfigInvalid},
		{SIWCConfig{Mode: SIWCModeHostedPaste, AllowHosted: true, PastePort: 14555, ProductionLike: true, OperatorPublicURL: "http://console.example.test"}, SIWCFieldMode, ErrSIWCConfigInvalid},
		{SIWCConfig{Mode: SIWCModeHostedPaste, AllowHosted: true, PastePort: 14555, ProductionLike: true}, SIWCFieldMode, ErrSIWCConfigInvalid},
	}
	for i, tc := range bad {
		err := ValidateSIWCConfig(tc.c)
		var ce *SIWCConfigError
		if !errors.As(err, &ce) {
			t.Errorf("case %d: err = %v, want *SIWCConfigError", i, err)
			continue
		}
		if ce.Field != tc.field || !errors.Is(err, tc.kind) {
			t.Errorf("case %d: field=%q kind-match=%v, want %q/%v", i, ce.Field, errors.Is(err, tc.kind), tc.field, tc.kind)
		}
	}
	good := []SIWCConfig{
		{Mode: SIWCModeDisabled},
		{Mode: SIWCModeLoopbackLocal, AllowHosted: true, OperatorPublicURL: "http://127.0.0.1:3007"},
		{Mode: SIWCModeHostedPaste, AllowHosted: true, PastePort: 14555, ProductionLike: true, OperatorPublicURL: "https://console.example.test"},
		{Mode: SIWCModeHostedPaste, AllowHosted: true, PastePort: 14555, ProductionLike: false, OperatorPublicURL: "http://localhost:3000"},
	}
	for i, c := range good {
		if err := ValidateSIWCConfig(c); err != nil {
			t.Errorf("case %d rejected: %v", i, err)
		}
	}
}
