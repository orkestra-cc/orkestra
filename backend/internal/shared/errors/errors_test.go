package errors

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"testing"
	"time"
)

func TestAppError(t *testing.T) {
	// Test error creation
	err := ValidationError("test validation error").
		WithOperation("test_operation").
		WithResource("test_resource").
		WithUserID("user123").
		WithCorrelationID("corr456").
		WithDetail("field", "email").
		Build()

	// Verify error properties
	if err.Code != CodeInvalidInput {
		t.Errorf("Expected code %s, got %s", CodeInvalidInput, err.Code)
	}

	if err.Category != CategoryValidation {
		t.Errorf("Expected category %s, got %s", CategoryValidation, err.Category)
	}

	if err.Message != "test validation error" {
		t.Errorf("Expected message 'test validation error', got %s", err.Message)
	}

	if err.Operation != "test_operation" {
		t.Errorf("Expected operation 'test_operation', got %s", err.Operation)
	}

	if err.UserID != "user123" {
		t.Errorf("Expected userID 'user123', got %s", err.UserID)
	}

	if err.HTTPStatus != 400 {
		t.Errorf("Expected HTTP status 400, got %d", err.HTTPStatus)
	}

	// Test error chaining
	if err.Error() != "[INVALID_INPUT] validation: test validation error" {
		t.Errorf("Unexpected error string: %s", err.Error())
	}
}

func TestErrorSanitizer(t *testing.T) {
	sanitizer := NewErrorSanitizer()

	// Test message sanitization
	err := ValidationError("Invalid input: password=secret123 and token=abc").Build()

	// Test production sanitization
	sanitized := sanitizer.Sanitize(err, false)

	// Should sanitize sensitive patterns in message
	if sanitized.Message == err.Message {
		t.Errorf("Expected sanitized message in production")
	}

	// Test system error production sanitization
	systemErr := InternalError("Database error").Build()
	systemSanitized := sanitizer.Sanitize(systemErr, false)

	// Should use generic message for system errors in production
	if systemSanitized.Message != "Internal server error" {
		t.Errorf("Expected generic message for system error in production, got: %s", systemSanitized.Message)
	}

	// Test sensitive key removal
	errWithDetails := ValidationError("test").
		WithDetail("password", "secret").
		WithDetail("safe_field", "safe_value").
		Build()

	sanitizedWithDetails := sanitizer.Sanitize(errWithDetails, false)

	// Should remove sensitive keys
	if _, exists := sanitizedWithDetails.Details["password"]; exists {
		t.Errorf("Expected sensitive key 'password' to be removed")
	}

	// Test that details are processed (removed if empty map, but sanitizeDetails was called)
	// For now, just verify that sensitive keys are removed properly
	t.Logf("Sanitized details: %v", sanitizedWithDetails.Details)
}

func TestRateLimiter(t *testing.T) {
	rateLimiter := NewRateLimiter()
	defer rateLimiter.Close()

	ctx := context.Background()

	// Test rate limiting
	result1 := rateLimiter.Check(ctx, "test-key", "auth:login")
	if !result1.Allowed {
		t.Error("First request should be allowed")
	}

	// Consume all tokens
	for i := 0; i < 5; i++ {
		rateLimiter.Check(ctx, "test-key", "auth:login")
	}

	// Should be rate limited now
	result2 := rateLimiter.Check(ctx, "test-key", "auth:login")
	if result2.Allowed {
		t.Error("Request should be rate limited")
	}

	if result2.Remaining >= result1.Remaining {
		t.Error("Remaining tokens should decrease")
	}
}

func TestSetAuthFailedConfig(t *testing.T) {
	rl := NewRateLimiter()
	defer rl.Close()

	// Tighten the lockout to a single attempt so the next IsBlocked call
	// flips immediately. Window is generous so the bucket doesn't refill
	// inside the test window.
	rl.SetAuthFailedConfig(1, time.Hour)

	ctx := context.Background()
	if rl.IsBlocked(ctx, "fresh-id") {
		t.Fatalf("first probe must be allowed")
	}
	if !rl.IsBlocked(ctx, "fresh-id") {
		t.Fatalf("second probe must trip the new tighter lockout")
	}

	// Invalid config (threshold < 1) is a no-op — preserves the
	// last-good config so a misedit can't lock everyone out.
	rl.SetAuthFailedConfig(0, time.Hour)
	rl.mu.RLock()
	cfg := rl.configs["auth:failed"]
	rl.mu.RUnlock()
	if cfg.Capacity != 1 {
		t.Fatalf("invalid threshold should not overwrite config; got capacity=%d", cfg.Capacity)
	}
}

func TestValidationMiddleware(t *testing.T) {
	validator := NewValidationMiddleware(nil)
	ctx := context.Background()

	// Test struct for validation
	type TestStruct struct {
		Email    string `json:"email" validate:"required,email"`
		Provider string `json:"provider" validate:"required,oauth_provider"`
		Password string `json:"password" validate:"required,strong_password"`
	}

	// Test valid data
	validData := TestStruct{
		Email:    "test@example.com",
		Provider: "google",
		Password: "StrongPass123!",
	}

	err := validator.Validate(ctx, validData)
	if err != nil {
		t.Errorf("Valid data should not produce validation error: %v", err)
	}

	// Test invalid data
	invalidData := TestStruct{
		Email:    "invalid-email",
		Provider: "invalid-provider",
		Password: "weak",
	}

	err = validator.Validate(ctx, invalidData)
	if err == nil {
		t.Error("Invalid data should produce validation error")
	}

	// Verify error details
	if err.Category != CategoryValidation {
		t.Errorf("Expected validation category, got %s", err.Category)
	}

	if err.Details["validation_errors"] == nil {
		t.Error("Expected validation_errors in details")
	}
}

func TestErrorMetrics(t *testing.T) {
	metrics := NewErrorMetrics()

	// Record some errors
	err1 := ValidationError("test error 1").Build()
	err2 := AuthenticationError("test error 2").Build()
	err3 := InternalError("test error 3").Build()

	metrics.RecordError(err1)
	metrics.RecordError(err2)
	metrics.RecordError(err3)
	metrics.RecordPanic()

	// Test error counts
	counts := metrics.GetErrorCounts()
	if counts[CategoryValidation][CodeInvalidInput] != 1 {
		t.Errorf("Expected 1 validation error, got %d", counts[CategoryValidation][CodeInvalidInput])
	}

	if counts[CategoryAuthentication][CodeInvalidCredentials] != 1 {
		t.Errorf("Expected 1 auth error, got %d", counts[CategoryAuthentication][CodeInvalidCredentials])
	}

	// Test security metrics
	securityMetrics := metrics.GetSecurityMetrics()
	if securityMetrics.PanicCount != 1 {
		t.Errorf("Expected 1 panic, got %d", securityMetrics.PanicCount)
	}

	// Test top errors
	topErrors := metrics.GetTopErrors(5)
	if len(topErrors) != 3 {
		t.Errorf("Expected 3 top errors, got %d", len(topErrors))
	}

	// Test health status
	healthStatus := metrics.IsHealthy()
	if !healthStatus.Healthy {
		t.Errorf("System should be healthy with low error counts")
	}
}

func TestErrorManager(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	manager := NewManager(logger, true)
	defer manager.Close()

	ctx := context.WithValue(context.Background(), CorrelationIDKey, "test-correlation-id")

	// Test error handling
	appErr := ValidationError("test validation").Build()
	humaErr := manager.HandleError(ctx, appErr)

	if humaErr.GetStatus() != 400 {
		t.Errorf("Expected status 400, got %d", humaErr.GetStatus())
	}

	// Test validation
	type TestData struct {
		Email string `json:"email" validate:"required,email"`
	}

	invalidData := TestData{Email: "invalid"}
	validationErr := manager.Validate(ctx, invalidData)

	if validationErr == nil {
		t.Error("Expected validation error for invalid data")
	}

	// Test rate limiting
	result := manager.GetRateLimiter().Check(ctx, "test", "api:general")
	if !result.Allowed {
		t.Error("First request should be allowed")
	}
}

func TestCorrelationID(t *testing.T) {
	// Test correlation ID context
	correlationID := "test-correlation-123"
	ctx := context.WithValue(context.Background(), CorrelationIDKey, correlationID)

	retrievedID := GetCorrelationID(ctx)
	if retrievedID != correlationID {
		t.Errorf("Expected correlation ID %s, got %s", correlationID, retrievedID)
	}

	// Test empty context
	emptyCtx := context.Background()
	emptyID := GetCorrelationID(emptyCtx)
	if emptyID != "" {
		t.Errorf("Expected empty correlation ID, got %s", emptyID)
	}
}

func TestErrorJSONSerialization(t *testing.T) {
	err := ValidationError("test error").
		WithOperation("test_op").
		WithDetail("field", "email").
		Build()

	// Test JSON serialization
	jsonData, jsonErr := json.Marshal(err)
	if jsonErr != nil {
		t.Errorf("Error serializing to JSON: %v", jsonErr)
	}

	// Test JSON deserialization
	var deserializedErr AppError
	if unmarshalErr := json.Unmarshal(jsonData, &deserializedErr); unmarshalErr != nil {
		t.Errorf("Error deserializing from JSON: %v", unmarshalErr)
	}

	// Verify key fields
	if deserializedErr.Code != err.Code {
		t.Errorf("Code mismatch after JSON round-trip")
	}

	if deserializedErr.Message != err.Message {
		t.Errorf("Message mismatch after JSON round-trip")
	}
}

func TestErrorBuilderChaining(t *testing.T) {
	// Test method chaining
	err := NewError(CodeInvalidInput, CategoryValidation, "test").
		WithOperation("op1").
		WithResource("resource1").
		WithUserID("user1").
		WithCorrelationID("corr1").
		WithDetail("key1", "value1").
		WithDetails(map[string]any{"key2": "value2", "key3": "value3"}).
		WithHTTPStatus(422).
		WithRetryable(true).
		Build()

	// Verify all properties were set
	if err.Operation != "op1" {
		t.Error("Operation not set correctly")
	}
	if err.Resource != "resource1" {
		t.Error("Resource not set correctly")
	}
	if err.UserID != "user1" {
		t.Error("UserID not set correctly")
	}
	if err.CorrelationID != "corr1" {
		t.Error("CorrelationID not set correctly")
	}
	if err.HTTPStatus != 422 {
		t.Error("HTTPStatus not set correctly")
	}
	if !err.Retryable {
		t.Error("Retryable not set correctly")
	}
	if len(err.Details) != 3 {
		t.Errorf("Expected 3 details, got %d", len(err.Details))
	}
}

func BenchmarkErrorCreation(b *testing.B) {
	for i := 0; i < b.N; i++ {
		err := ValidationError("benchmark test").
			WithOperation("benchmark").
			WithUserID("user123").
			WithDetail("iteration", i).
			Build()
		_ = err
	}
}

func BenchmarkErrorSanitization(b *testing.B) {
	sanitizer := NewErrorSanitizer()
	err := InternalError("Database error with mongodb://user:pass@host/db and secret=abc123").
		WithDetail("password", "secret123").
		WithDetail("token", "jwt.token.here").
		Build()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sanitized := sanitizer.Sanitize(err, false)
		_ = sanitized
	}
}

func BenchmarkRateLimiterCheck(b *testing.B) {
	rateLimiter := NewRateLimiter()
	defer rateLimiter.Close()
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result := rateLimiter.Check(ctx, "benchmark-key", "api:general")
		_ = result
	}
}
