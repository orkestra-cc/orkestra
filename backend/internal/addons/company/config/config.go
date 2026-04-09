package config

import "time"

// CompanyAPIConfig holds the configuration for OpenAPI Company API integration
type CompanyAPIConfig struct {
	// Base URL for API calls
	// Production: https://company.openapi.com
	// Sandbox: https://test.company.openapi.com
	BaseURL string

	// OAuth Bearer Token for authentication (shared with billing/SDI)
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

// Validate checks if the configuration is valid
func (c *CompanyAPIConfig) Validate() error {
	if c.BaseURL == "" {
		return ErrMissingBaseURL
	}
	if c.BearerToken == "" {
		return ErrMissingBearerToken
	}
	return nil
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
