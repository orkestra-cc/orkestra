package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/services"
	"github.com/orkestra/backend/internal/shared/config"
	"github.com/orkestra/backend/internal/shared/errors"
	"github.com/orkestra/backend/internal/shared/utils"
)

type AuthMiddleware struct {
	jwtService   services.JWTService
	authService  services.AuthService
	errorManager *errors.Manager
	cookieName   string
	config       *config.Config
}

func NewAuthMiddleware(jwtService services.JWTService, errorManager *errors.Manager) *AuthMiddleware {
	return &AuthMiddleware{
		jwtService:   jwtService,
		authService:  nil, // No auth service for backward compatibility
		errorManager: errorManager,
		cookieName:   "access_token", // Default cookie name for backward compatibility
		config:       nil,
	}
}

func NewAuthMiddlewareWithConfig(jwtService services.JWTService, errorManager *errors.Manager, cfg *config.Config) *AuthMiddleware {
	cookieName := cfg.Auth.Cookie.Name
	if cookieName == "" {
		cookieName = "access_token" // Fallback to default
	}
	return &AuthMiddleware{
		jwtService:   jwtService,
		authService:  nil, // Will be set with SetAuthService
		errorManager: errorManager,
		cookieName:   cookieName,
		config:       cfg,
	}
}

// SetAuthService sets the auth service for auto-refresh functionality
func (m *AuthMiddleware) SetAuthService(authService services.AuthService) {
	m.authService = authService
}

func (m *AuthMiddleware) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// First try to get and validate Bearer token
		token := m.extractBearerToken(r)

		if token != "" {
			// Try to validate the access token
			claims, err := m.jwtService.ValidateAccessToken(token)
			if err == nil {
				// Valid access token, proceed with request
				m.setUserContext(w, r, claims, next)
				return
			}

			// If token is expired or invalid, we'll try auto-refresh below
			if err != services.ErrTokenExpired && err != services.ErrInvalidToken {
				// Some other error, fail immediately
				m.sendErrorResponse(w, r, errors.TokenInvalidError().
					WithOperation("require_auth").
					WithInternal(err).
					Build())
				return
			}
		}

		// No valid Bearer token, try auto-refresh with cookie (but not for logout or OAuth callback endpoints)
		if m.authService != nil && m.config != nil && !isLogoutRequest(r) && !isOAuthCallbackRequest(r) {
			// Check if we have a refresh token in cookie
			refreshToken := m.extractRefreshTokenFromCookie(r)
			if refreshToken != "" {
				// Prepare security context
				securityCtx := &models.SecurityContext{
					IPAddress: utils.GetClientIP(r),
					Timestamp: time.Now(),
				}

				// Try to refresh the tokens
				tokenResponse, err := m.authService.RefreshTokensWithRiskAssessment(r.Context(), refreshToken, securityCtx)
				if err == nil {
					// Set new refresh token in cookie
					cookieDomain := m.config.Auth.Cookie.Domain
					isSecure := m.config.Auth.Cookie.Secure
					utils.SetRefreshTokenCookie(w, m.cookieName, tokenResponse.RefreshToken, 7*24*3600, cookieDomain, isSecure)

					// Add new access token to response header for client to use
					w.Header().Set("X-New-Access-Token", tokenResponse.AccessToken)
					w.Header().Set("X-Token-Refreshed", "true")

					// Validate the new access token and proceed
					claims, err := m.jwtService.ValidateAccessToken(tokenResponse.AccessToken)
					if err == nil {
						m.setUserContext(w, r, claims, next)
						return
					}
				}
			}
		}

		// All attempts failed, return unauthorized
		m.sendErrorResponse(w, r, errors.AuthenticationError("authentication required").
			WithOperation("require_auth").
			Build())
	})
}

// Helper method to set user context
func (m *AuthMiddleware) setUserContext(w http.ResponseWriter, r *http.Request, claims *models.JWTClaims, next http.Handler) {
	// Use UUID as primary identifier, keep ObjectID for backward compatibility
	userIdentifier := claims.UserUUID
	if userIdentifier == "" {
		userIdentifier = claims.UserID // Fall back to legacy ObjectID
	}

	ctx := context.WithValue(r.Context(), "userUUID", userIdentifier)
	ctx = context.WithValue(ctx, "userID", claims.UserID) // Keep for backward compatibility
	ctx = context.WithValue(ctx, "userEmail", claims.Email)
	ctx = context.WithValue(ctx, "userRole", claims.Role)
	ctx = context.WithValue(ctx, "claims", claims)

	next.ServeHTTP(w, r.WithContext(ctx))
}

// Extract Bearer token from Authorization header
func (m *AuthMiddleware) extractBearerToken(r *http.Request) string {
	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		parts := strings.Split(authHeader, " ")
		if len(parts) == 2 && strings.ToLower(parts[0]) == "bearer" {
			return parts[1]
		}
	}
	return ""
}

// Extract refresh token from cookie
func (m *AuthMiddleware) extractRefreshTokenFromCookie(r *http.Request) string {
	cookie, err := r.Cookie(m.cookieName)
	if err == nil && cookie.Value != "" {
		// Check if it's a refresh token by trying to parse it
		claims, err := m.jwtService.ParseUnverifiedClaims(cookie.Value)
		if err == nil && claims.TokenType == "refresh" {
			return cookie.Value
		}
	}
	return ""
}

func (m *AuthMiddleware) RequireRole(allowedRoles ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userRole := r.Context().Value("userRole")
			if userRole == nil {
				m.sendErrorResponse(w, r, errors.AuthenticationError("authentication required").
					WithOperation("require_role").
					Build())
				return
			}

			role := userRole.(string)
			for _, allowedRole := range allowedRoles {
				if role == allowedRole {
					next.ServeHTTP(w, r)
					return
				}
			}

			m.sendErrorResponse(w, r, errors.AuthorizationError("insufficient permissions").
				WithOperation("require_role").
				WithDetail("user_role", role).
				WithDetail("required_roles", allowedRoles).
				Build())
		})
	}
}

func (m *AuthMiddleware) OptionalAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := m.extractToken(r)
		if token == "" {
			next.ServeHTTP(w, r)
			return
		}

		claims, err := m.jwtService.ValidateAccessToken(token)
		if err == nil {
			// Use UUID as primary identifier, keep ObjectID for backward compatibility
			userIdentifier := claims.UserUUID
			if userIdentifier == "" {
				userIdentifier = claims.UserID // Fall back to legacy ObjectID
			}

			ctx := context.WithValue(r.Context(), "userUUID", userIdentifier)
			ctx = context.WithValue(ctx, "userID", claims.UserID) // Keep for backward compatibility
			ctx = context.WithValue(ctx, "userEmail", claims.Email)
			ctx = context.WithValue(ctx, "userRole", claims.Role)
			ctx = context.WithValue(ctx, "claims", claims)
			r = r.WithContext(ctx)
		}

		next.ServeHTTP(w, r)
	})
}

func (m *AuthMiddleware) extractToken(r *http.Request) string {
	// Only extract Bearer token for backward compatibility
	// Cookie handling is now done separately for refresh tokens
	return m.extractBearerToken(r)
}

// isLogoutRequest checks if the current request is a logout request
func isLogoutRequest(r *http.Request) bool {
	return r.Method == "POST" && (r.URL.Path == "/v1/auth/logout" || r.URL.Path == "/auth/logout")
}

// isOAuthCallbackRequest checks if the current request is an OAuth callback request
func isOAuthCallbackRequest(r *http.Request) bool {
	return r.Method == "GET" && (strings.Contains(r.URL.Path, "/auth/callback") ||
		strings.Contains(r.URL.Path, "/oauth/callback") ||
		r.URL.Path == "/v1/auth/google/callback" ||
		r.URL.Path == "/v1/auth/apple/callback" ||
		r.URL.Path == "/v1/auth/discord/callback" ||
		r.URL.Path == "/v1/auth/github/callback")
}

// GetUserUUID extracts the user UUID from the request context
func GetUserUUID(ctx context.Context) (string, bool) {
	userUUID, ok := ctx.Value("userUUID").(string)
	return userUUID, ok
}

// GetUserID extracts the legacy user ID from the request context (deprecated)
func GetUserID(ctx context.Context) (string, bool) {
	userID, ok := ctx.Value("userID").(string)
	return userID, ok
}

// GetUserEmail extracts the user email from the request context
func GetUserEmail(ctx context.Context) (string, bool) {
	email, ok := ctx.Value("userEmail").(string)
	return email, ok
}

// GetUserRole extracts the user role from the request context
func GetUserRole(ctx context.Context) (string, bool) {
	role, ok := ctx.Value("userRole").(string)
	return role, ok
}

type RoleHierarchy map[string][]string

var DefaultRoleHierarchy = RoleHierarchy{
	"developer":     {"developer", "ceo", "administrator", "manager", "operator", "guest"},
	"ceo":           {"ceo", "administrator", "manager", "operator", "guest"},
	"administrator": {"administrator", "manager", "operator", "guest"},
	"manager":       {"manager", "operator", "guest"},
	"operator":      {"operator", "guest"},
	"guest":         {"guest"},
}

func (h RoleHierarchy) HasPermission(userRole string, requiredRole string) bool {
	permissions, exists := h[userRole]
	if !exists {
		return false
	}

	for _, perm := range permissions {
		if perm == requiredRole {
			return true
		}
	}

	return false
}

func (m *AuthMiddleware) RequireHierarchicalRole(requiredRole string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userRole := r.Context().Value("userRole")
			if userRole == nil {
				m.sendErrorResponse(w, r, errors.AuthenticationError("authentication required").
					WithOperation("require_hierarchical_role").
					Build())
				return
			}

			if !DefaultRoleHierarchy.HasPermission(userRole.(string), requiredRole) {
				m.sendErrorResponse(w, r, errors.AuthorizationError("insufficient permissions").
					WithOperation("require_hierarchical_role").
					WithDetail("user_role", userRole.(string)).
					WithDetail("required_role", requiredRole).
					Build())
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// sendErrorResponse sends a structured error response using the error manager
func (m *AuthMiddleware) sendErrorResponse(w http.ResponseWriter, r *http.Request, appErr *errors.AppError) {
	// Add correlation ID from context if available
	if correlationID := errors.GetCorrelationID(r.Context()); correlationID != "" {
		appErr.CorrelationID = correlationID
	}

	// Use the error manager to handle the error
	humaErr := m.errorManager.HandleError(r.Context(), appErr)

	// Set headers
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(humaErr.GetStatus())

	// Send JSON response
	response := map[string]interface{}{
		"status": humaErr.GetStatus(),
		"title":  appErr.Message,
		"detail": appErr.Message,
		"type":   "about:blank",
		"errors": []map[string]interface{}{
			{
				"message":  appErr.Message,
				"location": appErr.Operation,
				"value":    string(appErr.Code),
			},
		},
	}

	json.NewEncoder(w).Encode(response)
}
