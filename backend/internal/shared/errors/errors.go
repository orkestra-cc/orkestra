package errors

import (
	"context"
	"log/slog"

	"github.com/danielgtaylor/huma/v2"
)

// Manager centralizes all error handling functionality
type Manager struct {
	handler     *ErrorHandler
	rateLimiter *RateLimiter
	validator   *ValidationMiddleware
	logger      *slog.Logger
}

// NewManager creates a new error management system
func NewManager(logger *slog.Logger, isDevelopment bool) *Manager {
	return &Manager{
		handler:     NewErrorHandler(logger, isDevelopment),
		rateLimiter: NewRateLimiter(),
		validator:   NewValidationMiddleware(nil),
		logger:      logger,
	}
}

// GetErrorHandler returns the error handler
func (m *Manager) GetErrorHandler() *ErrorHandler {
	return m.handler
}

// GetRateLimiter returns the rate limiter
func (m *Manager) GetRateLimiter() *RateLimiter {
	return m.rateLimiter
}

// GetValidator returns the validation middleware
func (m *Manager) GetValidator() *ValidationMiddleware {
	return m.validator
}

// HandleError is a convenience method for handling errors
func (m *Manager) HandleError(ctx context.Context, err error) huma.StatusError {
	return m.handler.HandleError(ctx, err)
}

// HandlePanic is a convenience method for handling panics
func (m *Manager) HandlePanic(ctx context.Context, recovered any) huma.StatusError {
	return m.handler.HandlePanic(ctx, recovered)
}

// Validate is a convenience method for validation
func (m *Manager) Validate(ctx context.Context, data interface{}) *AppError {
	return m.validator.Validate(ctx, data)
}

// Close cleans up resources
func (m *Manager) Close() {
	if m.rateLimiter != nil {
		m.rateLimiter.Close()
	}
}

// Global error helpers for common operations

// WrapError wraps an existing error as an AppError
func WrapError(err error, code ErrorCode, category ErrorCategory, message string) *AppError {
	return NewError(code, category, message).
		WithInternal(err).
		Build()
}

// DatabaseError creates a database error
func DatabaseError(operation string, err error) *ErrorBuilder {
	return InternalError("database operation failed").
		WithOperation(operation).
		WithInternal(err).
		WithDetail("database_operation", operation)
}

// ExternalAPIError creates an external API error
func ExternalAPIError(service string, operation string, err error) *ErrorBuilder {
	return ExternalServiceError(service, "external API call failed").
		WithOperation(operation).
		WithInternal(err).
		WithDetail("external_service", service).
		WithDetail("operation", operation)
}

// ConfigurationError creates a configuration error
func ConfigurationError(setting string, message string) *ErrorBuilder {
	return NewError(CodeConfigurationError, CategorySystem, message).
		WithDetail("setting", setting).
		WithRetryable(false)
}

// BusinessRuleError creates a business rule violation error
func BusinessRuleError(rule string, message string) *ErrorBuilder {
	return NewError(CodeBusinessRuleViolation, CategoryBusiness, message).
		WithDetail("business_rule", rule).
		WithRetryable(false)
}

// TimeoutError creates a timeout error
func TimeoutError(operation string, timeout string) *ErrorBuilder {
	return NewError(CodeExternalServiceTimeout, CategoryExternal, "operation timed out").
		WithOperation(operation).
		WithDetail("timeout", timeout).
		WithRetryable(true)
}

// ConflictError creates a conflict error (409)
func ConflictError(resource string, message string) *ErrorBuilder {
	return NewError(CodeResourceAlreadyExists, CategoryBusiness, message).
		WithResource(resource).
		WithHTTPStatus(409).
		WithRetryable(false)
}

// ForbiddenError creates a forbidden error with more context
func ForbiddenError(operation string, reason string) *ErrorBuilder {
	return AuthorizationError("access denied").
		WithOperation(operation).
		WithDetail("reason", reason)
}

// BadRequestError creates a generic bad request error
func BadRequestError(message string) *ErrorBuilder {
	return ValidationError(message)
}

// UnprocessableEntityError creates a 422 error
func UnprocessableEntityError(message string) *ErrorBuilder {
	return ValidationError(message).
		WithHTTPStatus(422)
}

// ServiceUnavailableError creates a 503 error
func ServiceUnavailableError(service string, message string) *ErrorBuilder {
	return ExternalServiceError(service, message).
		WithHTTPStatus(503).
		WithRetryable(true)
}
