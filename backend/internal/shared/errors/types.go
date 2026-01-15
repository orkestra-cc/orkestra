package errors

import (
	"fmt"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
)

// ErrorCategory represents the category of error
type ErrorCategory string

const (
	CategoryValidation     ErrorCategory = "validation"
	CategoryAuthentication ErrorCategory = "authentication"
	CategoryAuthorization  ErrorCategory = "authorization"
	CategoryBusiness       ErrorCategory = "business"
	CategoryExternal       ErrorCategory = "external"
	CategorySystem         ErrorCategory = "system"
	CategorySecurity       ErrorCategory = "security"
	CategoryRateLimit      ErrorCategory = "rate_limit"
)

// ErrorCode represents specific error codes for different scenarios
type ErrorCode string

const (
	// Validation errors
	CodeInvalidInput    ErrorCode = "INVALID_INPUT"
	CodeMissingField    ErrorCode = "MISSING_FIELD"
	CodeInvalidFormat   ErrorCode = "INVALID_FORMAT"
	CodeValueOutOfRange ErrorCode = "VALUE_OUT_OF_RANGE"

	// Authentication errors
	CodeInvalidCredentials ErrorCode = "INVALID_CREDENTIALS"
	CodeTokenExpired       ErrorCode = "TOKEN_EXPIRED"
	CodeTokenInvalid       ErrorCode = "TOKEN_INVALID"
	CodeTokenMissing       ErrorCode = "TOKEN_MISSING"

	// Authorization errors
	CodeInsufficientPermissions ErrorCode = "INSUFFICIENT_PERMISSIONS"
	CodeAccountDeactivated      ErrorCode = "ACCOUNT_DEACTIVATED"
	CodeAccountSuspended        ErrorCode = "ACCOUNT_SUSPENDED"

	// Business logic errors
	CodeResourceNotFound      ErrorCode = "RESOURCE_NOT_FOUND"
	CodeResourceAlreadyExists ErrorCode = "RESOURCE_ALREADY_EXISTS"
	CodeInvalidOperation      ErrorCode = "INVALID_OPERATION"
	CodeBusinessRuleViolation ErrorCode = "BUSINESS_RULE_VIOLATION"

	// External service errors
	CodeExternalServiceUnavailable ErrorCode = "EXTERNAL_SERVICE_UNAVAILABLE"
	CodeExternalServiceTimeout     ErrorCode = "EXTERNAL_SERVICE_TIMEOUT"
	CodeExternalServiceError       ErrorCode = "EXTERNAL_SERVICE_ERROR"

	// System errors
	CodeDatabaseError      ErrorCode = "DATABASE_ERROR"
	CodeInternalError      ErrorCode = "INTERNAL_ERROR"
	CodeConfigurationError ErrorCode = "CONFIGURATION_ERROR"

	// Security errors
	CodeSecurityViolation  ErrorCode = "SECURITY_VIOLATION"
	CodeSuspiciousActivity ErrorCode = "SUSPICIOUS_ACTIVITY"

	// Rate limiting errors
	CodeRateLimitExceeded ErrorCode = "RATE_LIMIT_EXCEEDED"
	CodeQuotaExceeded     ErrorCode = "QUOTA_EXCEEDED"
)

// AppError represents a structured application error
type AppError struct {
	Code          ErrorCode      `json:"code"`
	Category      ErrorCategory  `json:"category"`
	Message       string         `json:"message"`
	Details       map[string]any `json:"details,omitempty"`
	Operation     string         `json:"operation,omitempty"`
	Resource      string         `json:"resource,omitempty"`
	UserID        string         `json:"user_id,omitempty"`
	CorrelationID string         `json:"correlation_id,omitempty"`
	Timestamp     time.Time      `json:"timestamp"`
	HTTPStatus    int            `json:"http_status"`
	Retryable     bool           `json:"retryable"`
	Internal      error          `json:"-"` // Internal error (not exposed to client)
}

// Error implements the error interface
func (e *AppError) Error() string {
	return fmt.Sprintf("[%s] %s: %s", e.Code, e.Category, e.Message)
}

// Unwrap implements the unwrap interface for error chains
func (e *AppError) Unwrap() error {
	return e.Internal
}

// ToHumaError converts AppError to Huma error format
func (e *AppError) ToHumaError() huma.StatusError {
	errors := []*huma.ErrorDetail{
		{
			Message:  e.Message,
			Location: e.Operation,
			Value:    string(e.Code),
		},
	}

	return &huma.ErrorModel{
		Status: e.HTTPStatus,
		Detail: e.Message,
		Errors: errors,
	}
}

// ErrorBuilder provides a fluent interface for building errors
type ErrorBuilder struct {
	error *AppError
}

// NewError creates a new error builder
func NewError(code ErrorCode, category ErrorCategory, message string) *ErrorBuilder {
	return &ErrorBuilder{
		error: &AppError{
			Code:       code,
			Category:   category,
			Message:    message,
			Details:    make(map[string]any),
			Timestamp:  time.Now().UTC(),
			HTTPStatus: getDefaultHTTPStatus(category, code),
			Retryable:  isRetryableByDefault(category, code),
		},
	}
}

// WithOperation adds operation context
func (b *ErrorBuilder) WithOperation(operation string) *ErrorBuilder {
	b.error.Operation = operation
	return b
}

// WithResource adds resource context
func (b *ErrorBuilder) WithResource(resource string) *ErrorBuilder {
	b.error.Resource = resource
	return b
}

// WithUserID adds user context
func (b *ErrorBuilder) WithUserID(userID string) *ErrorBuilder {
	b.error.UserID = userID
	return b
}

// WithCorrelationID adds correlation ID
func (b *ErrorBuilder) WithCorrelationID(correlationID string) *ErrorBuilder {
	b.error.CorrelationID = correlationID
	return b
}

// WithDetail adds a detail field
func (b *ErrorBuilder) WithDetail(key string, value any) *ErrorBuilder {
	b.error.Details[key] = value
	return b
}

// WithDetails adds multiple detail fields
func (b *ErrorBuilder) WithDetails(details map[string]any) *ErrorBuilder {
	for k, v := range details {
		b.error.Details[k] = v
	}
	return b
}

// WithHTTPStatus overrides the default HTTP status
func (b *ErrorBuilder) WithHTTPStatus(status int) *ErrorBuilder {
	b.error.HTTPStatus = status
	return b
}

// WithRetryable sets whether the error is retryable
func (b *ErrorBuilder) WithRetryable(retryable bool) *ErrorBuilder {
	b.error.Retryable = retryable
	return b
}

// WithInternal wraps an internal error
func (b *ErrorBuilder) WithInternal(err error) *ErrorBuilder {
	b.error.Internal = err
	return b
}

// Build returns the constructed error
func (b *ErrorBuilder) Build() *AppError {
	return b.error
}

// getDefaultHTTPStatus returns the default HTTP status for error categories
func getDefaultHTTPStatus(category ErrorCategory, code ErrorCode) int {
	switch category {
	case CategoryValidation:
		return http.StatusBadRequest
	case CategoryAuthentication:
		return http.StatusUnauthorized
	case CategoryAuthorization:
		return http.StatusForbidden
	case CategoryBusiness:
		if code == CodeResourceNotFound {
			return http.StatusNotFound
		}
		if code == CodeResourceAlreadyExists {
			return http.StatusConflict
		}
		return http.StatusBadRequest
	case CategoryExternal:
		return http.StatusBadGateway
	case CategorySystem:
		return http.StatusInternalServerError
	case CategorySecurity:
		return http.StatusForbidden
	case CategoryRateLimit:
		return http.StatusTooManyRequests
	default:
		return http.StatusInternalServerError
	}
}

// isRetryableByDefault determines if an error is retryable by default
func isRetryableByDefault(category ErrorCategory, code ErrorCode) bool {
	switch category {
	case CategoryExternal:
		return true
	case CategorySystem:
		return code != CodeConfigurationError
	case CategoryRateLimit:
		return true
	default:
		return false
	}
}

// Common error constructors for frequently used errors

// ValidationError creates a validation error
func ValidationError(message string) *ErrorBuilder {
	return NewError(CodeInvalidInput, CategoryValidation, message)
}

// AuthenticationError creates an authentication error
func AuthenticationError(message string) *ErrorBuilder {
	return NewError(CodeInvalidCredentials, CategoryAuthentication, message)
}

// AuthorizationError creates an authorization error
func AuthorizationError(message string) *ErrorBuilder {
	return NewError(CodeInsufficientPermissions, CategoryAuthorization, message)
}

// NotFoundError creates a not found error
func NotFoundError(resource string) *ErrorBuilder {
	return NewError(CodeResourceNotFound, CategoryBusiness, fmt.Sprintf("%s not found", resource)).
		WithResource(resource)
}

// AlreadyExistsError creates an already exists error
func AlreadyExistsError(resource string) *ErrorBuilder {
	return NewError(CodeResourceAlreadyExists, CategoryBusiness, fmt.Sprintf("%s already exists", resource)).
		WithResource(resource)
}

// InternalError creates an internal system error
func InternalError(message string) *ErrorBuilder {
	return NewError(CodeInternalError, CategorySystem, message)
}

// ExternalServiceError creates an external service error
func ExternalServiceError(service string, message string) *ErrorBuilder {
	return NewError(CodeExternalServiceError, CategoryExternal, message).
		WithDetail("service", service).
		WithRetryable(true)
}

// RateLimitError creates a rate limit error
func RateLimitError(message string) *ErrorBuilder {
	return NewError(CodeRateLimitExceeded, CategoryRateLimit, message)
}

// TokenExpiredError creates a token expired error
func TokenExpiredError() *ErrorBuilder {
	return NewError(CodeTokenExpired, CategoryAuthentication, "token has expired")
}

// TokenInvalidError creates a token invalid error
func TokenInvalidError() *ErrorBuilder {
	return NewError(CodeTokenInvalid, CategoryAuthentication, "token is invalid")
}

// SecurityViolationError creates a security violation error
func SecurityViolationError(message string) *ErrorBuilder {
	return NewError(CodeSecurityViolation, CategorySecurity, message)
}
