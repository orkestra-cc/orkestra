package errors

import (
	"regexp"
	"strings"
)

// ErrorSanitizer handles sanitization of errors before sending to clients
type ErrorSanitizer struct {
	sensitivePatterns []*regexp.Regexp
	genericMessages   map[ErrorCategory]string
}

// NewErrorSanitizer creates a new error sanitizer
func NewErrorSanitizer() *ErrorSanitizer {
	// Compile patterns for sensitive information
	sensitivePatterns := []*regexp.Regexp{
		regexp.MustCompile(`password[=:\s]+[^\s]+`),                                            // passwords
		regexp.MustCompile(`token[=:\s]+[^\s]+`),                                               // tokens
		regexp.MustCompile(`key[=:\s]+[^\s]+`),                                                 // keys
		regexp.MustCompile(`secret[=:\s]+[^\s]+`),                                              // secrets
		regexp.MustCompile(`\b[A-Za-z0-9+/]{40,}\b`),                                           // potential base64 tokens
		regexp.MustCompile(`mongodb://[^@\s]+@[^\s]+`),                                         // MongoDB connection strings
		regexp.MustCompile(`redis://[^@\s]+@[^\s]+`),                                           // Redis connection strings
		regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`),                                      // IP addresses
		regexp.MustCompile(`[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`),                   // email addresses
		regexp.MustCompile(`\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`), // UUIDs
	}

	// Generic messages for different error categories in production
	genericMessages := map[ErrorCategory]string{
		CategoryValidation:     "Invalid input provided",
		CategoryAuthentication: "Authentication failed",
		CategoryAuthorization:  "Access denied",
		CategoryBusiness:       "Request could not be processed",
		CategoryExternal:       "External service unavailable",
		CategorySystem:         "Internal server error",
		CategorySecurity:       "Security violation detected",
		CategoryRateLimit:      "Rate limit exceeded",
	}

	return &ErrorSanitizer{
		sensitivePatterns: sensitivePatterns,
		genericMessages:   genericMessages,
	}
}

// Sanitize sanitizes an error for client consumption
func (s *ErrorSanitizer) Sanitize(err *AppError, isDevelopment bool) *AppError {
	// Create a copy of the error
	sanitized := &AppError{
		Code:          err.Code,
		Category:      err.Category,
		Message:       err.Message,
		Details:       make(map[string]any),
		Operation:     err.Operation,
		Resource:      err.Resource,
		UserID:        err.UserID,
		CorrelationID: err.CorrelationID,
		Timestamp:     err.Timestamp,
		HTTPStatus:    err.HTTPStatus,
		Retryable:     err.Retryable,
		// Never expose Internal error to client
		Internal: nil,
	}

	// In production, sanitize sensitive information
	if !isDevelopment {
		sanitized = s.sanitizeForProduction(sanitized)
	} else {
		// In development, still sanitize credentials and sensitive data
		sanitized.Message = s.sanitizeMessage(sanitized.Message)
		sanitized.Details = s.sanitizeDetails(sanitized.Details)
	}

	return sanitized
}

// sanitizeForProduction applies production-level sanitization
func (s *ErrorSanitizer) sanitizeForProduction(err *AppError) *AppError {
	// For production, use generic messages for system errors
	switch err.Category {
	case CategorySystem:
		err.Message = s.genericMessages[CategorySystem]
		err.Details = map[string]any{} // Clear all details
		err.Operation = ""             // Clear operation details

	case CategorySecurity:
		err.Message = s.genericMessages[CategorySecurity]
		err.Details = map[string]any{} // Clear all details for security
		err.Operation = ""

	case CategoryExternal:
		err.Message = s.genericMessages[CategoryExternal]
		// Keep minimal details for external errors
		err.Details = s.sanitizeDetails(err.Details)

	default:
		// For other categories, sanitize messages and details
		err.Message = s.sanitizeMessage(err.Message)
		err.Details = s.sanitizeDetails(err.Details)
	}

	return err
}

// sanitizeMessage removes sensitive information from error messages
func (s *ErrorSanitizer) sanitizeMessage(message string) string {
	sanitized := message

	// Apply all sensitive patterns
	for _, pattern := range s.sensitivePatterns {
		sanitized = pattern.ReplaceAllString(sanitized, "[REDACTED]")
	}

	// Remove potential SQL injection attempts
	sanitized = s.sanitizeSQLPatterns(sanitized)

	// Remove file paths that might leak system information
	sanitized = s.sanitizeFilePaths(sanitized)

	return sanitized
}

// sanitizeDetails removes sensitive information from error details
func (s *ErrorSanitizer) sanitizeDetails(details map[string]any) map[string]any {
	if details == nil {
		return nil
	}

	sanitized := make(map[string]any)

	for key, value := range details {
		// Skip sensitive keys entirely
		if s.isSensitiveKey(key) {
			continue
		}

		switch v := value.(type) {
		case string:
			sanitized[key] = s.sanitizeMessage(v)
		case map[string]any:
			sanitized[key] = s.sanitizeDetails(v)
		case []any:
			sanitized[key] = s.sanitizeSlice(v)
		default:
			// For other types, include as-is (numbers, booleans, etc.)
			sanitized[key] = value
		}
	}

	return sanitized
}

// sanitizeSlice sanitizes slice values
func (s *ErrorSanitizer) sanitizeSlice(slice []any) []any {
	sanitized := make([]any, len(slice))

	for i, item := range slice {
		switch v := item.(type) {
		case string:
			sanitized[i] = s.sanitizeMessage(v)
		case map[string]any:
			sanitized[i] = s.sanitizeDetails(v)
		default:
			sanitized[i] = item
		}
	}

	return sanitized
}

// isSensitiveKey checks if a key is considered sensitive
func (s *ErrorSanitizer) isSensitiveKey(key string) bool {
	sensitiveKeys := []string{
		"password", "token", "secret", "key", "auth", "credential",
		"session", "cookie", "jwt", "oauth", "api_key", "access_token",
		"refresh_token", "private_key", "certificate", "signature",
		"stack_trace", "panic_value", // internal debugging info
	}

	keyLower := strings.ToLower(key)
	for _, sensitive := range sensitiveKeys {
		if strings.Contains(keyLower, sensitive) {
			return true
		}
	}

	return false
}

// sanitizeSQLPatterns removes potential SQL injection patterns
func (s *ErrorSanitizer) sanitizeSQLPatterns(message string) string {
	sqlPatterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(select|insert|update|delete|drop|create|alter|exec|execute)\b.*`),
		regexp.MustCompile(`(?i)union\s+select`),
		regexp.MustCompile(`(?i)or\s+1\s*=\s*1`),
		regexp.MustCompile(`(?i)and\s+1\s*=\s*1`),
		regexp.MustCompile(`(?i)'\s*or\s*'1'\s*=\s*'1`),
	}

	sanitized := message
	for _, pattern := range sqlPatterns {
		sanitized = pattern.ReplaceAllString(sanitized, "[POTENTIAL_SQL_INJECTION_BLOCKED]")
	}

	return sanitized
}

// sanitizeFilePaths removes or sanitizes file paths
func (s *ErrorSanitizer) sanitizeFilePaths(message string) string {
	// Common file path patterns
	filePathPatterns := []*regexp.Regexp{
		regexp.MustCompile(`/[a-zA-Z0-9._/-]+\.[a-zA-Z0-9]+`),         // Unix paths
		regexp.MustCompile(`[A-Z]:\\[a-zA-Z0-9._\\-]+\.[a-zA-Z0-9]+`), // Windows paths
		regexp.MustCompile(`/home/[a-zA-Z0-9._/-]+`),                  // Home directories
		regexp.MustCompile(`/var/[a-zA-Z0-9._/-]+`),                   // Var directories
		regexp.MustCompile(`/tmp/[a-zA-Z0-9._/-]+`),                   // Temp directories
	}

	sanitized := message
	for _, pattern := range filePathPatterns {
		sanitized = pattern.ReplaceAllString(sanitized, "[FILE_PATH]")
	}

	return sanitized
}

// GetSafeErrorMessage returns a safe error message for the given category
func (s *ErrorSanitizer) GetSafeErrorMessage(category ErrorCategory) string {
	if msg, exists := s.genericMessages[category]; exists {
		return msg
	}
	return "An error occurred while processing your request"
}
