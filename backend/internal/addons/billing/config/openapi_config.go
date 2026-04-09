package config

import "time"

// OpenAPIConfig holds the configuration for OpenAPI SDI integration
type OpenAPIConfig struct {
	// Base URL for API calls
	// Production: https://sdi.openapi.it
	// Sandbox: https://test.sdi.openapi.it
	BaseURL string

	// OAuth Bearer Token for authentication
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

// Validate checks if the configuration is valid
func (c *OpenAPIConfig) Validate() error {
	if c.BaseURL == "" {
		return ErrMissingBaseURL
	}
	if c.BearerToken == "" {
		return ErrMissingBearerToken
	}
	if c.FiscalID == "" {
		return ErrMissingFiscalID
	}
	return nil
}

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
