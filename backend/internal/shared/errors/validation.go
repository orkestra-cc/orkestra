package errors

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/go-playground/validator/v10"
)

// ValidationMiddleware provides advanced validation capabilities
type ValidationMiddleware struct {
	validator    *validator.Validate
	cache        map[string]*ValidationResult
	cacheMu      sync.RWMutex
	cacheTimeout time.Duration
	customRules  map[string]validator.Func
}

// ValidationResult represents a cached validation result
type ValidationResult struct {
	Valid     bool
	Errors    []ValidationFieldError
	Timestamp time.Time
}

// ValidationFieldError represents a single validation error
type ValidationFieldError struct {
	Field   string      `json:"field"`
	Tag     string      `json:"tag"`
	Value   interface{} `json:"value,omitempty"`
	Message string      `json:"message"`
	Code    ErrorCode   `json:"code"`
}

// ValidationConfig contains validation configuration
type ValidationConfig struct {
	EnableCaching  bool          `json:"enable_caching"`
	CacheTimeout   time.Duration `json:"cache_timeout"`
	MaxFieldErrors int           `json:"max_field_errors"`
	StrictMode     bool          `json:"strict_mode"`
	SanitizeInputs bool          `json:"sanitize_inputs"`
}

// NewValidationMiddleware creates a new validation middleware
func NewValidationMiddleware(config *ValidationConfig) *ValidationMiddleware {
	if config == nil {
		config = &ValidationConfig{
			EnableCaching:  true,
			CacheTimeout:   time.Minute * 5,
			MaxFieldErrors: 10,
			StrictMode:     true,
			SanitizeInputs: true,
		}
	}

	vm := &ValidationMiddleware{
		validator:    validator.New(),
		cache:        make(map[string]*ValidationResult),
		cacheTimeout: config.CacheTimeout,
		customRules:  make(map[string]validator.Func),
	}

	// Register custom validation functions
	vm.registerCustomValidators()

	// Use JSON tag names for validation errors
	vm.validator.RegisterTagNameFunc(func(fld reflect.StructField) string {
		name := strings.SplitN(fld.Tag.Get("json"), ",", 2)[0]
		if name == "-" {
			return ""
		}
		return name
	})

	return vm
}

// registerCustomValidators registers custom validation functions
func (vm *ValidationMiddleware) registerCustomValidators() {
	// OAuth provider validation
	vm.validator.RegisterValidation("oauth_provider", vm.validateOAuthProvider)

	// JWT token format validation
	vm.validator.RegisterValidation("jwt_token", vm.validateJWTToken)

	// Strong password validation
	vm.validator.RegisterValidation("strong_password", vm.validateStrongPassword)

	// Phone number validation
	vm.validator.RegisterValidation("phone", vm.validatePhoneNumber)

	// Secure URL validation
	vm.validator.RegisterValidation("secure_url", vm.validateSecureURL)

	// MongoDB ObjectID validation
	vm.validator.RegisterValidation("objectid", vm.validateObjectID)

	// Username validation (alphanumeric + underscore, 3-30 chars)
	vm.validator.RegisterValidation("username", vm.validateUsername)

	// No SQL injection validation
	vm.validator.RegisterValidation("no_sql_injection", vm.validateNoSQLInjection)

	// Store custom rules for error messages
	vm.customRules["oauth_provider"] = vm.validateOAuthProvider
	vm.customRules["jwt_token"] = vm.validateJWTToken
	vm.customRules["strong_password"] = vm.validateStrongPassword
	vm.customRules["phone"] = vm.validatePhoneNumber
	vm.customRules["secure_url"] = vm.validateSecureURL
	vm.customRules["objectid"] = vm.validateObjectID
	vm.customRules["username"] = vm.validateUsername
	vm.customRules["no_sql_injection"] = vm.validateNoSQLInjection
}

// Validate validates a struct and returns detailed validation errors
func (vm *ValidationMiddleware) Validate(ctx context.Context, data interface{}) *AppError {
	// Generate cache key if caching is enabled
	var cacheKey string
	if vm.cacheTimeout > 0 {
		cacheKey = vm.generateCacheKey(data)
		if result := vm.getCachedResult(cacheKey); result != nil {
			if !result.Valid {
				return vm.createValidationError(ctx, result.Errors)
			}
			return nil
		}
	}

	// Perform validation
	err := vm.validator.Struct(data)
	if err == nil {
		// Cache successful validation
		if cacheKey != "" {
			vm.cacheResult(cacheKey, &ValidationResult{
				Valid:     true,
				Timestamp: time.Now(),
			})
		}
		return nil
	}

	// Parse validation errors
	validationErrs := vm.parseValidationErrors(err)

	// Cache validation errors
	if cacheKey != "" {
		vm.cacheResult(cacheKey, &ValidationResult{
			Valid:     false,
			Errors:    validationErrs,
			Timestamp: time.Now(),
		})
	}

	return vm.createValidationError(ctx, validationErrs)
}

// parseValidationErrors converts validator errors to our format
func (vm *ValidationMiddleware) parseValidationErrors(err error) []ValidationFieldError {
	var validationErrs []ValidationFieldError

	if validatorErrs, ok := err.(validator.ValidationErrors); ok {
		for _, fieldErr := range validatorErrs {
			validationErr := ValidationFieldError{
				Field:   fieldErr.Field(),
				Tag:     fieldErr.Tag(),
				Value:   fieldErr.Value(),
				Message: vm.getErrorMessage(fieldErr),
				Code:    vm.getErrorCode(fieldErr.Tag()),
			}
			validationErrs = append(validationErrs, validationErr)
		}
	}

	return validationErrs
}

// createValidationError creates an AppError from validation errors
func (vm *ValidationMiddleware) createValidationError(ctx context.Context, validationErrs []ValidationFieldError) *AppError {
	correlationID := GetCorrelationID(ctx)

	details := make(map[string]any)
	details["validation_errors"] = validationErrs
	details["error_count"] = len(validationErrs)

	// Create field-specific error map
	fieldErrors := make(map[string][]string)
	for _, err := range validationErrs {
		fieldErrors[err.Field] = append(fieldErrors[err.Field], err.Message)
	}
	details["field_errors"] = fieldErrors

	message := "Validation failed"
	if len(validationErrs) == 1 {
		message = fmt.Sprintf("Validation failed: %s", validationErrs[0].Message)
	} else {
		message = fmt.Sprintf("Validation failed: %d errors found", len(validationErrs))
	}

	return ValidationError(message).
		WithCorrelationID(correlationID).
		WithDetails(details).
		WithOperation("validation").
		Build()
}

// getErrorMessage returns a user-friendly error message
func (vm *ValidationMiddleware) getErrorMessage(fieldErr validator.FieldError) string {
	field := fieldErr.Field()
	tag := fieldErr.Tag()
	param := fieldErr.Param()

	switch tag {
	case "required":
		return fmt.Sprintf("%s is required", field)
	case "email":
		return fmt.Sprintf("%s must be a valid email address", field)
	case "min":
		return fmt.Sprintf("%s must be at least %s characters long", field, param)
	case "max":
		return fmt.Sprintf("%s must be at most %s characters long", field, param)
	case "len":
		return fmt.Sprintf("%s must be exactly %s characters long", field, param)
	case "gt":
		return fmt.Sprintf("%s must be greater than %s", field, param)
	case "gte":
		return fmt.Sprintf("%s must be greater than or equal to %s", field, param)
	case "lt":
		return fmt.Sprintf("%s must be less than %s", field, param)
	case "lte":
		return fmt.Sprintf("%s must be less than or equal to %s", field, param)
	case "oneof":
		return fmt.Sprintf("%s must be one of: %s", field, param)
	case "url":
		return fmt.Sprintf("%s must be a valid URL", field)
	case "uri":
		return fmt.Sprintf("%s must be a valid URI", field)
	case "uuid":
		return fmt.Sprintf("%s must be a valid UUID", field)
	case "oauth_provider":
		return fmt.Sprintf("%s must be a valid OAuth provider (google, apple, discord, github)", field)
	case "jwt_token":
		return fmt.Sprintf("%s must be a valid JWT token", field)
	case "strong_password":
		return fmt.Sprintf("%s must contain at least 8 characters with uppercase, lowercase, number, and special character", field)
	case "phone":
		return fmt.Sprintf("%s must be a valid phone number", field)
	case "secure_url":
		return fmt.Sprintf("%s must be a secure HTTPS URL", field)
	case "objectid":
		return fmt.Sprintf("%s must be a valid MongoDB ObjectID", field)
	case "username":
		return fmt.Sprintf("%s must be 3-30 characters and contain only letters, numbers, and underscores", field)
	case "no_sql_injection":
		return fmt.Sprintf("%s contains potentially dangerous characters", field)
	default:
		return fmt.Sprintf("%s is invalid", field)
	}
}

// getErrorCode returns the appropriate error code for validation tags
func (vm *ValidationMiddleware) getErrorCode(tag string) ErrorCode {
	switch tag {
	case "required":
		return CodeMissingField
	case "email", "url", "uri", "uuid", "jwt_token", "phone", "objectid":
		return CodeInvalidFormat
	case "min", "max", "len", "gt", "gte", "lt", "lte":
		return CodeValueOutOfRange
	case "oneof", "oauth_provider":
		return CodeInvalidInput
	case "strong_password":
		return CodeInvalidFormat
	case "secure_url":
		return CodeInvalidFormat
	case "username":
		return CodeInvalidFormat
	case "no_sql_injection":
		return CodeInvalidInput
	default:
		return CodeInvalidInput
	}
}

// Custom validation functions

func (vm *ValidationMiddleware) validateOAuthProvider(fl validator.FieldLevel) bool {
	provider := fl.Field().String()
	validProviders := []string{"google", "apple", "discord", "github"}

	for _, valid := range validProviders {
		if provider == valid {
			return true
		}
	}
	return false
}

func (vm *ValidationMiddleware) validateJWTToken(fl validator.FieldLevel) bool {
	token := fl.Field().String()
	parts := strings.Split(token, ".")
	return len(parts) == 3 && len(parts[0]) > 0 && len(parts[1]) > 0 && len(parts[2]) > 0
}

func (vm *ValidationMiddleware) validateStrongPassword(fl validator.FieldLevel) bool {
	password := fl.Field().String()

	if len(password) < 8 {
		return false
	}

	hasUpper := regexp.MustCompile(`[A-Z]`).MatchString(password)
	hasLower := regexp.MustCompile(`[a-z]`).MatchString(password)
	hasNumber := regexp.MustCompile(`[0-9]`).MatchString(password)
	hasSpecial := regexp.MustCompile(`[!@#$%^&*()_+\-=\[\]{};':"\\|,.<>\/?]`).MatchString(password)

	return hasUpper && hasLower && hasNumber && hasSpecial
}

func (vm *ValidationMiddleware) validatePhoneNumber(fl validator.FieldLevel) bool {
	phone := fl.Field().String()
	// Simple international phone number validation
	phoneRegex := regexp.MustCompile(`^\+?[1-9]\d{1,14}$`)
	return phoneRegex.MatchString(phone)
}

func (vm *ValidationMiddleware) validateSecureURL(fl validator.FieldLevel) bool {
	url := fl.Field().String()
	return strings.HasPrefix(url, "https://")
}

func (vm *ValidationMiddleware) validateObjectID(fl validator.FieldLevel) bool {
	id := fl.Field().String()
	objectIDRegex := regexp.MustCompile(`^[0-9a-fA-F]{24}$`)
	return objectIDRegex.MatchString(id)
}

func (vm *ValidationMiddleware) validateUsername(fl validator.FieldLevel) bool {
	username := fl.Field().String()
	if len(username) < 3 || len(username) > 30 {
		return false
	}
	usernameRegex := regexp.MustCompile(`^[a-zA-Z0-9_]+$`)
	return usernameRegex.MatchString(username)
}

func (vm *ValidationMiddleware) validateNoSQLInjection(fl validator.FieldLevel) bool {
	value := fl.Field().String()

	// Check for common SQL injection patterns
	dangerousPatterns := []string{
		"'", "\"", ";", "--", "/*", "*/", "xp_", "sp_",
		"union", "select", "insert", "update", "delete",
		"drop", "create", "alter", "exec", "execute",
	}

	valueLower := strings.ToLower(value)
	for _, pattern := range dangerousPatterns {
		if strings.Contains(valueLower, pattern) {
			return false
		}
	}

	return true
}

// Caching functions

func (vm *ValidationMiddleware) generateCacheKey(data interface{}) string {
	// Simple cache key generation based on struct hash
	// In production, you might want a more sophisticated approach
	jsonData, _ := json.Marshal(data)
	return fmt.Sprintf("validation:%x", jsonData)
}

func (vm *ValidationMiddleware) getCachedResult(key string) *ValidationResult {
	vm.cacheMu.RLock()
	defer vm.cacheMu.RUnlock()

	if result, exists := vm.cache[key]; exists {
		if time.Since(result.Timestamp) < vm.cacheTimeout {
			return result
		}
		// Remove expired cache entry
		delete(vm.cache, key)
	}

	return nil
}

func (vm *ValidationMiddleware) cacheResult(key string, result *ValidationResult) {
	vm.cacheMu.Lock()
	defer vm.cacheMu.Unlock()

	vm.cache[key] = result

	// Simple cache cleanup - remove old entries if cache is too large
	if len(vm.cache) > 1000 {
		cutoff := time.Now().Add(-vm.cacheTimeout)
		for k, v := range vm.cache {
			if v.Timestamp.Before(cutoff) {
				delete(vm.cache, k)
			}
		}
	}
}

// Middleware returns an HTTP middleware for request validation
func (vm *ValidationMiddleware) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Add validation context
			ctx := context.WithValue(r.Context(), "validator", vm)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetValidator returns the validator from context
func GetValidator(ctx context.Context) *ValidationMiddleware {
	if v := ctx.Value("validator"); v != nil {
		if validator, ok := v.(*ValidationMiddleware); ok {
			return validator
		}
	}
	return nil
}
