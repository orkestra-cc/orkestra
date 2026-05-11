package config

import "time"

// OpenAPIConfig holds the configuration for OpenAPI SDI integration
type OpenAPIConfig struct {
	// Base URL for API calls
	// Production: https://sdi.openapi.it
	// Sandbox: https://test.sdi.openapi.it
	BaseURL string

	// AccountEmail + APIKey are the long-lived credentials used to mint
	// short-lived JWT bearer tokens at OAuthBaseURL/token. When both are
	// set, the API client mints + caches the JWT itself; when empty, the
	// client falls back to the static BearerToken below (back-compat).
	AccountEmail string
	APIKey       string
	OAuthBaseURL string

	// OAuth Bearer Token for authentication (legacy static-JWT fallback —
	// used only when AccountEmail+APIKey are empty)
	BearerToken string

	// Company fiscal ID (P.IVA)
	FiscalID string

	// SDI recipient code (OpenAPI's code: JKKZDGR)
	RecipientCode string

	// Enable digital signature for invoices
	ApplySignature bool

	// Enable legal storage (conservazione sostitutiva)
	ApplyStorage bool

	// HTTP client timeout
	Timeout time.Duration

	// Number of retry attempts for failed requests
	RetryAttempts int

	// Interval between polling for notifications
	PollingInterval time.Duration

	// Enable sandbox mode (uses test.sdi.openapi.it)
	SandboxMode bool
}

// DefaultOpenAPIConfig returns a config with sensible defaults
func DefaultOpenAPIConfig() *OpenAPIConfig {
	return &OpenAPIConfig{
		BaseURL:         "https://test.sdi.openapi.it", // Sandbox by default
		RecipientCode:   "JKKZDGR",
		ApplySignature:  true,
		ApplyStorage:    true,
		Timeout:         30 * time.Second,
		RetryAttempts:   3,
		PollingInterval: 5 * time.Minute,
		SandboxMode:     true,
	}
}

// GetEndpoint returns the appropriate endpoint based on sandbox mode
func (c *OpenAPIConfig) GetEndpoint(path string) string {
	return c.BaseURL + path
}

// Validate checks if the configuration is valid. Either an explicit
// BearerToken (legacy path) OR AccountEmail+APIKey (preferred — module
// mints + rotates JWTs) must be present.
func (c *OpenAPIConfig) Validate() error {
	if c.BaseURL == "" {
		return ErrMissingBaseURL
	}
	if c.BearerToken == "" && (c.AccountEmail == "" || c.APIKey == "") {
		return ErrMissingBearerToken
	}
	if c.FiscalID == "" {
		return ErrMissingFiscalID
	}
	return nil
}

// HasOAuthCredentials reports whether the module should mint JWTs itself
// rather than rely on the static BearerToken fallback.
func (c *OpenAPIConfig) HasOAuthCredentials() bool {
	return c.AccountEmail != "" && c.APIKey != ""
}

// ConfigLoader returns a fresh OpenAPIConfig on each call,
// allowing hot-reload of admin UI changes without restart.
type ConfigLoader func() *OpenAPIConfig

// Config errors
type ConfigError string

func (e ConfigError) Error() string {
	return string(e)
}

const (
	ErrMissingBaseURL     ConfigError = "OPENAPI_BILLING_BASE_URL is required"
	ErrMissingBearerToken ConfigError = "OPENAPI_BILLING_BEARER_TOKEN is required"
	ErrMissingFiscalID    ConfigError = "OPENAPI_BILLING_FISCAL_ID is required"
)
