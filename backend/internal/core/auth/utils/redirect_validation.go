package utils

import (
	"fmt"
	"net/url"
	"strings"
)

// RedirectURIConfig holds the allowed redirect URIs for each provider
type RedirectURIConfig struct {
	AllowedRedirectURIs []string `json:"allowedRedirectURIs"`
	AllowedSchemes      []string `json:"allowedSchemes"`
	AllowLocalhost      bool     `json:"allowLocalhost"`
}

// DefaultRedirectURIConfig returns a default configuration for redirect URI validation
// DEPRECATED: Use NewRedirectURIConfig(allowLocalhost bool) instead for production safety
func DefaultRedirectURIConfig() *RedirectURIConfig {
	return NewRedirectURIConfig(true) // Default to allowing localhost for backwards compatibility
}

// NewRedirectURIConfig creates a redirect URI configuration with explicit localhost control
// Set allowLocalhost to false in production/staging environments
func NewRedirectURIConfig(allowLocalhost bool) *RedirectURIConfig {
	return &RedirectURIConfig{
		AllowedRedirectURIs: []string{
			"http://localhost:3000/auth/oauth/google/callback",
			"http://localhost:3000/auth/oauth/apple/callback",
			"http://localhost:3000/auth/oauth/discord/callback",
			"http://localhost:3000/auth/oauth/github/callback",
			"http://localhost:8080/auth/callback", // Frontend dev server
			// Mobile app deep links
			"com.orkestra://oauth/callback",
			"com.orkestra.app://oauth/callback",
		},
		AllowedSchemes: []string{
			"http",
			"https",
			"com.orkestra",     // Mobile app scheme
			"com.orkestra.app", // Alternative mobile app scheme
		},
		AllowLocalhost: allowLocalhost,
	}
}

// ValidateRedirectURI validates a redirect URI against the allowed list and security rules
func ValidateRedirectURI(redirectURI string, config *RedirectURIConfig) error {
	if redirectURI == "" {
		return nil // Empty redirect URI is allowed (uses default)
	}

	// Security checks before parsing
	if err := validateRedirectURISecurity(redirectURI); err != nil {
		return err
	}

	// Parse the URL
	parsedURL, err := url.Parse(redirectURI)
	if err != nil {
		return fmt.Errorf("invalid redirect URI format: %w", err)
	}

	// Check for path traversal attacks
	if containsPathTraversal(parsedURL.Path) {
		return fmt.Errorf("redirect URI contains path traversal attempt")
	}

	// Check if scheme is allowed
	if !isSchemeAllowed(parsedURL.Scheme, config.AllowedSchemes) {
		return fmt.Errorf("redirect URI scheme '%s' is not allowed", parsedURL.Scheme)
	}

	// Special handling for localhost in development
	if config.AllowLocalhost && isLocalhost(parsedURL.Host) {
		return validateLocalhostURI(parsedURL)
	}

	// If localhost is not allowed, reject any localhost URLs explicitly
	if !config.AllowLocalhost && isLocalhost(parsedURL.Host) {
		return fmt.Errorf("localhost redirect URIs are not allowed in this environment")
	}

	// Check against exact whitelist
	if isURIInWhitelist(redirectURI, config.AllowedRedirectURIs) {
		return nil
	}

	// Check if it's a mobile app scheme
	if isMobileAppScheme(parsedURL.Scheme, config.AllowedSchemes) {
		return validateMobileAppURI(parsedURL)
	}

	return fmt.Errorf("redirect URI '%s' is not in the allowed list", redirectURI)
}

// validateRedirectURISecurity performs pre-parsing security checks
func validateRedirectURISecurity(redirectURI string) error {
	// Limit maximum URL length to prevent DoS
	if len(redirectURI) > 2048 {
		return fmt.Errorf("redirect URI exceeds maximum length")
	}

	// Check for double encoding attacks (%%2f, etc.)
	if strings.Contains(redirectURI, "%25") {
		return fmt.Errorf("redirect URI contains double encoding")
	}

	// Check for null bytes
	if strings.Contains(redirectURI, "%00") || strings.Contains(redirectURI, "\x00") {
		return fmt.Errorf("redirect URI contains null bytes")
	}

	// Check for CRLF injection
	if strings.ContainsAny(redirectURI, "\r\n") || strings.Contains(redirectURI, "%0d") || strings.Contains(redirectURI, "%0a") {
		return fmt.Errorf("redirect URI contains CRLF characters")
	}

	// Block javascript: and data: schemes in any form (case insensitive)
	lowerURI := strings.ToLower(redirectURI)
	if strings.HasPrefix(lowerURI, "javascript:") || strings.HasPrefix(lowerURI, "data:") ||
		strings.HasPrefix(lowerURI, "vbscript:") {
		return fmt.Errorf("redirect URI uses forbidden scheme")
	}

	return nil
}

// containsPathTraversal checks for path traversal attempts
func containsPathTraversal(path string) bool {
	// Check for various forms of path traversal
	dangerousPatterns := []string{
		"..",
		"%2e%2e",
		"%252e%252e",
		"..%2f",
		"%2f..",
		"..\\",
		"..%5c",
		"%5c..",
	}

	lowerPath := strings.ToLower(path)
	for _, pattern := range dangerousPatterns {
		if strings.Contains(lowerPath, pattern) {
			return true
		}
	}

	return false
}

// isSchemeAllowed checks if the URL scheme is in the allowed list
func isSchemeAllowed(scheme string, allowedSchemes []string) bool {
	scheme = strings.ToLower(scheme)
	for _, allowed := range allowedSchemes {
		if strings.ToLower(allowed) == scheme {
			return true
		}
	}
	return false
}

// isLocalhost checks if the host is localhost or 127.0.0.1
func isLocalhost(host string) bool {
	host = strings.ToLower(strings.Split(host, ":")[0]) // Remove port
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// validateLocalhostURI validates localhost URIs with additional security checks
func validateLocalhostURI(parsedURL *url.URL) error {
	// Ensure path is reasonable for OAuth callback
	if !strings.HasPrefix(parsedURL.Path, "/auth/") && parsedURL.Path != "/auth/callback" {
		return fmt.Errorf("localhost redirect URI must use /auth/ path prefix")
	}

	// Prevent suspicious query parameters
	if parsedURL.RawQuery != "" {
		if strings.Contains(parsedURL.RawQuery, "javascript:") || strings.Contains(parsedURL.RawQuery, "data:") {
			return fmt.Errorf("localhost redirect URI contains suspicious query parameters")
		}
	}

	return nil
}

// isURIInWhitelist checks if the URI is exactly in the whitelist
func isURIInWhitelist(uri string, whitelist []string) bool {
	for _, allowed := range whitelist {
		if uri == allowed {
			return true
		}
	}
	return false
}

// isMobileAppScheme checks if the scheme is a mobile app scheme
func isMobileAppScheme(scheme string, allowedSchemes []string) bool {
	scheme = strings.ToLower(scheme)
	for _, allowed := range allowedSchemes {
		allowed = strings.ToLower(allowed)
		if allowed != "http" && allowed != "https" && scheme == allowed {
			return true
		}
	}
	return false
}

// validateMobileAppURI validates mobile app deep link URIs
func validateMobileAppURI(parsedURL *url.URL) error {
	// Mobile app URIs should have specific patterns
	if parsedURL.Host != "" && parsedURL.Host != "oauth" {
		return fmt.Errorf("mobile app redirect URI should use format 'scheme://oauth/callback'")
	}

	// Validate path for mobile apps
	if parsedURL.Path != "/callback" && parsedURL.Path != "//oauth/callback" {
		return fmt.Errorf("mobile app redirect URI should end with '/callback'")
	}

	return nil
}

// NormalizeRedirectURI normalizes a redirect URI for consistent comparison
func NormalizeRedirectURI(uri string) string {
	if uri == "" {
		return uri
	}

	parsedURL, err := url.Parse(uri)
	if err != nil {
		return uri // Return as-is if parsing fails
	}

	// Remove fragment for security
	parsedURL.Fragment = ""

	// Normalize scheme to lowercase
	parsedURL.Scheme = strings.ToLower(parsedURL.Scheme)

	// Normalize host to lowercase
	parsedURL.Host = strings.ToLower(parsedURL.Host)

	return parsedURL.String()
}
