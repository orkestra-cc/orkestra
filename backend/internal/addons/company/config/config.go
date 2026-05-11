package config

import "time"

// CompanyAPIConfig holds the configuration for OpenAPI Company API integration
type CompanyAPIConfig struct {
	// Base URL for API calls
	// Production: https://company.openapi.com
	// Sandbox: https://test.company.openapi.com
	BaseURL string

	// AccountEmail + APIKey are the long-lived credentials used to mint
	// short-lived JWT bearer tokens at OAuthBaseURL/token. When both are
	// set, the API client mints + caches the JWT itself; when empty, the
	// client falls back to the static BearerToken below (back-compat).
	AccountEmail string
	APIKey       string
	OAuthBaseURL string

	// BearerToken is the legacy static-JWT fallback. Optional once
	// AccountEmail + APIKey are configured. Kept for back-compat with
	// installations that minted the JWT manually before the OAuth flow
	// landed in the module.
	BearerToken string

	// HTTP client timeout
	Timeout time.Duration

	// Number of retry attempts for failed requests
	RetryAttempts int

	// Redis cache TTL for company lookups
	CacheTTL time.Duration
}

// DefaultCompanyAPIConfig returns a config with sensible defaults
func DefaultCompanyAPIConfig() *CompanyAPIConfig {
	return &CompanyAPIConfig{
		BaseURL:       "https://test.company.openapi.com",
		Timeout:       15 * time.Second,
		RetryAttempts: 3,
		CacheTTL:      24 * time.Hour,
	}
}

// GetEndpoint returns the full URL for a given API path
func (c *CompanyAPIConfig) GetEndpoint(path string) string {
	return c.BaseURL + path
}

// Validate checks if the configuration is valid. Either an explicit
// BearerToken (legacy path) OR AccountEmail+APIKey (preferred — module
// mints + rotates JWTs) must be present.
func (c *CompanyAPIConfig) Validate() error {
	if c.BaseURL == "" {
		return ErrMissingBaseURL
	}
	if c.BearerToken == "" && (c.AccountEmail == "" || c.APIKey == "") {
		return ErrMissingBearerToken
	}
	return nil
}

// HasOAuthCredentials reports whether the module should mint JWTs itself
// rather than rely on the static BearerToken fallback.
func (c *CompanyAPIConfig) HasOAuthCredentials() bool {
	return c.AccountEmail != "" && c.APIKey != ""
}

// Config errors
type ConfigError string

func (e ConfigError) Error() string {
	return string(e)
}

const (
	ErrMissingBaseURL     ConfigError = "OPENAPI_COMPANY_BASE_URL is required"
	ErrMissingBearerToken ConfigError = "OPENAPI_COMPANY_BEARER_TOKEN is required"
)
