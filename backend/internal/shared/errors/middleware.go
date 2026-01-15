package errors

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"runtime"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/chi/v5/middleware"
)

// ContextKey type for context keys
type ContextKey string

const (
	// CorrelationIDKey is the context key for correlation ID
	CorrelationIDKey ContextKey = "correlation_id"
	// ErrorContextKey is the context key for error context
	ErrorContextKey ContextKey = "error_context"
)

// ErrorContext holds contextual information for error handling
type ErrorContext struct {
	UserID      string
	Operation   string
	Resource    string
	RequestPath string
	Method      string
	UserAgent   string
	RemoteIP    string
}

// ErrorHandler handles application errors and converts them to HTTP responses
type ErrorHandler struct {
	logger        *slog.Logger
	sanitizer     *ErrorSanitizer
	metrics       *ErrorMetrics
	isDevelopment bool
}

// NewErrorHandler creates a new error handler
func NewErrorHandler(logger *slog.Logger, isDevelopment bool) *ErrorHandler {
	return &ErrorHandler{
		logger:        logger,
		sanitizer:     NewErrorSanitizer(),
		metrics:       NewErrorMetrics(),
		isDevelopment: isDevelopment,
	}
}

// Middleware returns a middleware that handles errors and adds correlation IDs
func (h *ErrorHandler) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Generate or extract correlation ID
			correlationID := r.Header.Get("X-Correlation-ID")
			if correlationID == "" {
				correlationID = generateCorrelationID()
			}

			// Add correlation ID to context
			ctx := context.WithValue(r.Context(), CorrelationIDKey, correlationID)

			// Add error context
			errorCtx := &ErrorContext{
				RequestPath: r.URL.Path,
				Method:      r.Method,
				UserAgent:   r.Header.Get("User-Agent"),
				RemoteIP:    getRemoteIP(r),
			}

			// Extract user ID if available
			if userID := r.Context().Value("userID"); userID != nil {
				if uid, ok := userID.(string); ok {
					errorCtx.UserID = uid
				}
			}

			ctx = context.WithValue(ctx, ErrorContextKey, errorCtx)

			// Add correlation ID to response headers
			w.Header().Set("X-Correlation-ID", correlationID)

			// Create a response wrapper to capture status and errors
			wrapper := &responseWrapper{
				ResponseWriter: w,
				statusCode:     http.StatusOK,
			}

			// Continue with the request
			next.ServeHTTP(wrapper, r.WithContext(ctx))

			// Record metrics
			h.metrics.RecordRequest(wrapper.statusCode, r.Method, r.URL.Path)
		})
	}
}

// HandleError processes application errors and returns appropriate HTTP responses
func (h *ErrorHandler) HandleError(ctx context.Context, err error) huma.StatusError {
	correlationID := GetCorrelationID(ctx)
	errorCtx := GetErrorContext(ctx)

	// Convert to AppError if it's not already
	var appErr *AppError
	if ae, ok := err.(*AppError); ok {
		appErr = ae
	} else {
		// Wrap unknown errors as internal errors
		appErr = InternalError("an unexpected error occurred").
			WithInternal(err).
			Build()
	}

	// Add correlation ID and context if not already present
	if appErr.CorrelationID == "" {
		appErr.CorrelationID = correlationID
	}

	if errorCtx != nil {
		if appErr.UserID == "" {
			appErr.UserID = errorCtx.UserID
		}
		if appErr.Operation == "" {
			appErr.Operation = fmt.Sprintf("%s %s", errorCtx.Method, errorCtx.RequestPath)
		}
	}

	// Log the error
	h.logError(appErr, errorCtx)

	// Record metrics
	h.metrics.RecordError(appErr)

	// Sanitize error for client response
	sanitizedErr := h.sanitizer.Sanitize(appErr, h.isDevelopment)

	return sanitizedErr.ToHumaError()
}

// HandlePanic recovers from panics and converts them to internal errors
func (h *ErrorHandler) HandlePanic(ctx context.Context, recovered any) huma.StatusError {
	correlationID := GetCorrelationID(ctx)
	errorCtx := GetErrorContext(ctx)

	// Create stack trace
	stack := make([]byte, 4096)
	length := runtime.Stack(stack, false)
	stackTrace := string(stack[:length])

	// Create panic error
	panicErr := InternalError("internal server error").
		WithCorrelationID(correlationID).
		WithDetail("panic_value", fmt.Sprintf("%v", recovered)).
		WithDetail("stack_trace", stackTrace).
		Build()

	if errorCtx != nil {
		panicErr.UserID = errorCtx.UserID
		panicErr.Operation = fmt.Sprintf("%s %s", errorCtx.Method, errorCtx.RequestPath)
	}

	// Log the panic
	h.logger.Error("Panic recovered",
		slog.String("correlation_id", correlationID),
		slog.String("panic_value", fmt.Sprintf("%v", recovered)),
		slog.String("stack_trace", stackTrace),
		slog.String("user_id", panicErr.UserID),
		slog.String("operation", panicErr.Operation),
	)

	// Record metrics
	h.metrics.RecordPanic()

	// Return sanitized error (never expose panic details to client)
	sanitizedErr := h.sanitizer.Sanitize(panicErr, false) // Never show panic details
	return sanitizedErr.ToHumaError()
}

// logError logs the error with appropriate level and context
func (h *ErrorHandler) logError(err *AppError, errorCtx *ErrorContext) {
	logLevel := h.getLogLevel(err)

	logAttrs := []slog.Attr{
		slog.String("error_code", string(err.Code)),
		slog.String("error_category", string(err.Category)),
		slog.String("correlation_id", err.CorrelationID),
		slog.Int("http_status", err.HTTPStatus),
		slog.Bool("retryable", err.Retryable),
		slog.Time("timestamp", err.Timestamp),
	}

	if err.UserID != "" {
		logAttrs = append(logAttrs, slog.String("user_id", err.UserID))
	}

	if err.Operation != "" {
		logAttrs = append(logAttrs, slog.String("operation", err.Operation))
	}

	if err.Resource != "" {
		logAttrs = append(logAttrs, slog.String("resource", err.Resource))
	}

	if errorCtx != nil {
		logAttrs = append(logAttrs,
			slog.String("request_path", errorCtx.RequestPath),
			slog.String("method", errorCtx.Method),
			slog.String("user_agent", errorCtx.UserAgent),
			slog.String("remote_ip", errorCtx.RemoteIP),
		)
	}

	if len(err.Details) > 0 {
		logAttrs = append(logAttrs, slog.Any("details", err.Details))
	}

	if err.Internal != nil {
		logAttrs = append(logAttrs, slog.String("internal_error", err.Internal.Error()))
	}

	h.logger.LogAttrs(context.Background(), logLevel, err.Message, logAttrs...)
}

// getLogLevel determines the appropriate log level for the error
func (h *ErrorHandler) getLogLevel(err *AppError) slog.Level {
	switch err.Category {
	case CategoryValidation, CategoryAuthentication, CategoryAuthorization:
		return slog.LevelWarn
	case CategoryBusiness:
		if err.Code == CodeResourceNotFound {
			return slog.LevelInfo
		}
		return slog.LevelWarn
	case CategoryExternal:
		return slog.LevelWarn
	case CategorySystem, CategorySecurity:
		return slog.LevelError
	case CategoryRateLimit:
		return slog.LevelInfo
	default:
		return slog.LevelError
	}
}

// responseWrapper wraps http.ResponseWriter to capture status codes
type responseWrapper struct {
	http.ResponseWriter
	statusCode int
}

func (w *responseWrapper) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *responseWrapper) Write(data []byte) (int, error) {
	return w.ResponseWriter.Write(data)
}

// Utility functions

// GetCorrelationID extracts correlation ID from context
func GetCorrelationID(ctx context.Context) string {
	if id := ctx.Value(CorrelationIDKey); id != nil {
		if correlationID, ok := id.(string); ok {
			return correlationID
		}
	}
	return ""
}

// GetErrorContext extracts error context from context
func GetErrorContext(ctx context.Context) *ErrorContext {
	if ctx := ctx.Value(ErrorContextKey); ctx != nil {
		if errorCtx, ok := ctx.(*ErrorContext); ok {
			return errorCtx
		}
	}
	return nil
}

// generateCorrelationID generates a new correlation ID
func generateCorrelationID() string {
	return middleware.GetReqID(context.Background())
}

// getRemoteIP extracts the real remote IP address
func getRemoteIP(r *http.Request) string {
	// Check X-Forwarded-For header first
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Take the first IP in the list
		ips := strings.Split(xff, ",")
		if len(ips) > 0 {
			return strings.TrimSpace(ips[0])
		}
	}

	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}

	// Fall back to RemoteAddr
	return r.RemoteAddr
}
