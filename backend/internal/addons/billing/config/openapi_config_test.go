package config

import (
	"errors"
	"testing"
	"time"
)

func TestDefaultOpenAPIConfig(t *testing.T) {
	cfg := DefaultOpenAPIConfig()
	if cfg == nil {
		t.Fatal("DefaultOpenAPIConfig returned nil")
	}
	if cfg.BaseURL != "https://test.sdi.openapi.it" {
		t.Errorf("BaseURL = %q", cfg.BaseURL)
	}
	if cfg.RecipientCode != "JKKZDGR" {
		t.Errorf("RecipientCode = %q, want JKKZDGR", cfg.RecipientCode)
	}
	if !cfg.ApplySignature || !cfg.ApplyStorage {
		t.Errorf("ApplySignature/ApplyStorage should default to true, got %v/%v", cfg.ApplySignature, cfg.ApplyStorage)
	}
	if cfg.Timeout != 30*time.Second {
		t.Errorf("Timeout = %v, want 30s", cfg.Timeout)
	}
	if cfg.RetryAttempts != 3 {
		t.Errorf("RetryAttempts = %d, want 3", cfg.RetryAttempts)
	}
	if cfg.PollingInterval != 5*time.Minute {
		t.Errorf("PollingInterval = %v, want 5m", cfg.PollingInterval)
	}
	if !cfg.SandboxMode {
		t.Errorf("SandboxMode should default to true")
	}
}

func TestOpenAPIConfig_GetEndpoint(t *testing.T) {
	cfg := &OpenAPIConfig{BaseURL: "https://test.sdi.openapi.it"}
	if got := cfg.GetEndpoint("/invoices"); got != "https://test.sdi.openapi.it/invoices" {
		t.Errorf("GetEndpoint = %q", got)
	}
	// Empty BaseURL just returns the path verbatim.
	empty := &OpenAPIConfig{}
	if got := empty.GetEndpoint("/x"); got != "/x" {
		t.Errorf("empty BaseURL GetEndpoint = %q", got)
	}
}

func TestOpenAPIConfig_Validate_OK_LegacyBearer(t *testing.T) {
	cfg := &OpenAPIConfig{
		BaseURL:     "https://test.sdi.openapi.it",
		BearerToken: "legacy-jwt",
		FiscalID:    "02081880490",
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("legacy bearer-only config must validate, got %v", err)
	}
}

func TestOpenAPIConfig_Validate_OK_OAuthCreds(t *testing.T) {
	cfg := &OpenAPIConfig{
		BaseURL:      "https://test.sdi.openapi.it",
		AccountEmail: "ops@example.com",
		APIKey:       "k123",
		FiscalID:     "02081880490",
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("AccountEmail+APIKey config must validate, got %v", err)
	}
}

func TestOpenAPIConfig_Validate_MissingBaseURL(t *testing.T) {
	cfg := &OpenAPIConfig{BearerToken: "x", FiscalID: "02081880490"}
	err := cfg.Validate()
	if !errors.Is(err, ErrMissingBaseURL) {
		t.Errorf("want ErrMissingBaseURL, got %v", err)
	}
}

func TestOpenAPIConfig_Validate_MissingBearerAndOAuth(t *testing.T) {
	cfg := &OpenAPIConfig{BaseURL: "https://x", FiscalID: "02081880490"}
	err := cfg.Validate()
	if !errors.Is(err, ErrMissingBearerToken) {
		t.Errorf("missing both creds must yield ErrMissingBearerToken, got %v", err)
	}
}

func TestOpenAPIConfig_Validate_PartialOAuthFallsBackToBearerError(t *testing.T) {
	// Only AccountEmail set — APIKey missing → must fail because OAuth pair is incomplete
	// and BearerToken is empty.
	cfg := &OpenAPIConfig{
		BaseURL:      "https://x",
		AccountEmail: "ops@example.com",
		FiscalID:     "02081880490",
	}
	if !errors.Is(cfg.Validate(), ErrMissingBearerToken) {
		t.Errorf("partial OAuth without bearer must fail")
	}

	cfg = &OpenAPIConfig{
		BaseURL:  "https://x",
		APIKey:   "key",
		FiscalID: "02081880490",
	}
	if !errors.Is(cfg.Validate(), ErrMissingBearerToken) {
		t.Errorf("partial OAuth (apikey only) must fail")
	}
}

func TestOpenAPIConfig_Validate_MissingFiscalID(t *testing.T) {
	cfg := &OpenAPIConfig{BaseURL: "https://x", BearerToken: "t"}
	if !errors.Is(cfg.Validate(), ErrMissingFiscalID) {
		t.Errorf("missing FiscalID must yield ErrMissingFiscalID")
	}
}

func TestOpenAPIConfig_HasOAuthCredentials(t *testing.T) {
	cases := []struct {
		email, key string
		want       bool
	}{
		{"ops@example.com", "k", true},
		{"", "k", false},
		{"ops@example.com", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		cfg := &OpenAPIConfig{AccountEmail: c.email, APIKey: c.key}
		if got := cfg.HasOAuthCredentials(); got != c.want {
			t.Errorf("HasOAuthCredentials(email=%q, key=%q) = %v, want %v", c.email, c.key, got, c.want)
		}
	}
}

func TestConfigError_Error(t *testing.T) {
	if ErrMissingBaseURL.Error() != "OPENAPI_BILLING_BASE_URL is required" {
		t.Errorf("ErrMissingBaseURL.Error() = %q", ErrMissingBaseURL.Error())
	}
	if ErrMissingBearerToken.Error() != "OPENAPI_BILLING_BEARER_TOKEN is required" {
		t.Errorf("ErrMissingBearerToken.Error() = %q", ErrMissingBearerToken.Error())
	}
	if ErrMissingFiscalID.Error() != "OPENAPI_BILLING_FISCAL_ID is required" {
		t.Errorf("ErrMissingFiscalID.Error() = %q", ErrMissingFiscalID.Error())
	}
	// Custom value
	c := ConfigError("custom message")
	if c.Error() != "custom message" {
		t.Errorf("ConfigError.Error() = %q", c.Error())
	}
}
