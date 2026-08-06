package utils

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Cookie utilities for secure token management

// CookieOptions defines options for setting HTTP cookies
type CookieOptions struct {
	Name     string
	Value    string
	Path     string
	Domain   string
	MaxAge   int // seconds
	Secure   bool
	HttpOnly bool
	SameSite http.SameSite
}

// SetSecureCookie sets a secure HTTP cookie with recommended security settings
func SetSecureCookie(w http.ResponseWriter, options *CookieOptions) {
	cookie := &http.Cookie{
		Name:     options.Name,
		Value:    options.Value,
		Path:     options.Path,
		Domain:   options.Domain,
		MaxAge:   options.MaxAge,
		Secure:   options.Secure,
		HttpOnly: options.HttpOnly,
		SameSite: options.SameSite,
	}

	// Set default secure options if not specified
	if options.Path == "" {
		cookie.Path = "/"
	}
	if options.SameSite == 0 {
		cookie.SameSite = http.SameSiteStrictMode
	}

	http.SetCookie(w, cookie)
}

// SetRefreshTokenCookie sets a secure refresh token cookie
func SetRefreshTokenCookie(w http.ResponseWriter, cookieName, token string, maxAgeSeconds int, domain string, secure bool) {
	SetSecureCookie(w, &CookieOptions{
		Name:     cookieName, // Use configured cookie name from environment
		Value:    token,
		Path:     "/", // Changed from "/auth" to "/" to make cookie available across all paths
		Domain:   domain,
		MaxAge:   maxAgeSeconds,
		Secure:   secure,               // Should be true in production (HTTPS)
		HttpOnly: true,                 // Prevent XSS access
		SameSite: http.SameSiteLaxMode, // Changed from Strict to Lax to allow OAuth redirects
	})
}

// ClearRefreshTokenCookie clears the refresh token cookie
func ClearRefreshTokenCookie(w http.ResponseWriter, cookieName, domain string, secure bool) {

	SetSecureCookie(w, &CookieOptions{
		Name:     cookieName, // Use configured cookie name from environment
		Value:    "",
		Path:     "/", // Must match the path used when setting the cookie
		Domain:   domain,
		MaxAge:   -1, // Expire immediately
		Secure:   secure,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode, // Must match SameSite used when setting the cookie
	})
}

// GetRefreshTokenFromCookie extracts refresh token from HTTP cookie using default name
func GetRefreshTokenFromCookie(r *http.Request) (string, error) {
	return GetRefreshTokenFromCookieByName(r, "refresh_token")
}

// GetRefreshTokenFromCookieByName extracts refresh token from HTTP cookie using specified name.
// Returns the last non-empty value — when the browser has multiple cookies under
// the same name (e.g., a stale Path=/auth cookie from a prior deployment coexisting
// with the current Path=/ cookie), `r.Cookie()` returns only the first match, which
// may be the stale one and trips the refresh-token family-replay guard. Returning
// the last one prefers the most-recently-set cookie; see
// GetAllRefreshTokensFromCookies for callers that need to try every candidate.
func GetRefreshTokenFromCookieByName(r *http.Request, cookieName string) (string, error) {
	candidates := GetAllRefreshTokensFromCookies(r, cookieName)
	if len(candidates) == 0 {
		return "", fmt.Errorf("refresh token cookie '%s' not found", cookieName)
	}
	return candidates[len(candidates)-1], nil
}

// GetAllRefreshTokensFromCookies returns every non-empty value present under
// `cookieName` in the request, in the order the browser sent them. Useful for
// callers that want to try each candidate against their validation logic before
// giving up — the /session and /refresh-cookie handlers use this to recover
// from stale cookies that would otherwise trip replay detection.
func GetAllRefreshTokensFromCookies(r *http.Request, cookieName string) []string {
	var out []string
	for _, c := range r.Cookies() {
		if c.Name != cookieName || c.Value == "" {
			continue
		}
		out = append(out, c.Value)
	}
	return out
}

// SetStateCookie sets a secure OAuth state cookie
func SetStateCookie(w http.ResponseWriter, state string, domain string, secure bool) {
	SetSecureCookie(w, &CookieOptions{
		Name:     "oauth_state",
		Value:    state,
		Path:     "/auth",
		Domain:   domain,
		MaxAge:   600, // 10 minutes
		Secure:   secure,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode, // Allow cross-site for OAuth redirects
	})
}

// GetStateFromCookie extracts OAuth state from HTTP cookie
func GetStateFromCookie(r *http.Request) (string, error) {
	cookie, err := r.Cookie("oauth_state")
	if err != nil {
		return "", fmt.Errorf("OAuth state cookie not found: %w", err)
	}
	return cookie.Value, nil
}

// ClearStateCookie clears the OAuth state cookie
func ClearStateCookie(w http.ResponseWriter, domain string, secure bool) {
	SetSecureCookie(w, &CookieOptions{
		Name:     "oauth_state",
		Value:    "",
		Path:     "/auth",
		Domain:   domain,
		MaxAge:   -1,
		Secure:   secure,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// Token extraction utilities

// ExtractBearerToken extracts Bearer token from Authorization header
func ExtractBearerToken(r *http.Request) (string, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return "", fmt.Errorf("authorization header not found")
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid authorization header format")
	}

	if strings.ToLower(parts[0]) != "bearer" {
		return "", fmt.Errorf("authorization header is not Bearer type")
	}

	token := strings.TrimSpace(parts[1])
	if token == "" {
		return "", fmt.Errorf("bearer token is empty")
	}

	return token, nil
}

// ExtractTokenFromRequest tries multiple methods to extract access token
func ExtractTokenFromRequest(r *http.Request) (string, error) {
	// Try Bearer token first
	if token, err := ExtractBearerToken(r); err == nil {
		return token, nil
	}

	// Try access_token cookie as fallback
	if cookie, err := r.Cookie("access_token"); err == nil && cookie.Value != "" {
		return cookie.Value, nil
	}

	// Try query parameter (not recommended, but for compatibility)
	if token := r.URL.Query().Get("access_token"); token != "" {
		return token, nil
	}

	return "", fmt.Errorf("no access token found in request")
}

// Request parsing utilities

// GetClientIP returns the caller's address.
//
// It reads r.RemoteAddr and NOTHING else. Interpreting forwarding
// headers (X-Forwarded-For, X-Real-IP, CF-Connecting-IP, X-Client-IP) is
// the exclusive job of shared/middleware.RealIP, which applies the
// deployment's trusted-proxy policy and rewrites RemoteAddr with the
// result before any handler runs.
//
// This function used to consult those headers itself, taking the first
// value it found. Because any client can set them, that made the source
// address of every request caller-controlled — which defeated the
// operator IP allow/blocklist, the login geo-block, and the per-IP login
// rate limiter, and let an attacker write arbitrary IPs into the audit
// trail. Do not reintroduce header parsing here: a single chokepoint is
// what makes the trusted-proxy policy enforceable.
func GetClientIP(r *http.Request) string {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		// RealIP rewrites RemoteAddr to a bare address (no port), so
		// this is the normal path behind the middleware.
		return strings.Trim(r.RemoteAddr, "[]")
	}
	return ip
}

// GetUserAgent extracts and sanitizes User-Agent header
func GetUserAgent(r *http.Request) string {
	userAgent := r.Header.Get("User-Agent")
	if userAgent == "" {
		return "unknown"
	}

	// Truncate if too long to prevent abuse
	if len(userAgent) > 500 {
		userAgent = userAgent[:500]
	}

	return userAgent
}

// Device fingerprinting from request headers

// DeviceFingerprint contains device characteristics from HTTP headers
type DeviceFingerprint struct {
	UserAgent      string
	AcceptLanguage string
	AcceptEncoding string
	Accept         string
	Connection     string
	DNT            string
	ScreenSize     string
	Timezone       string
	Platform       string
	IPAddress      string
}

// ExtractDeviceFingerprint creates device fingerprint from request headers
func ExtractDeviceFingerprint(r *http.Request) *DeviceFingerprint {
	return &DeviceFingerprint{
		UserAgent:      GetUserAgent(r),
		AcceptLanguage: r.Header.Get("Accept-Language"),
		AcceptEncoding: r.Header.Get("Accept-Encoding"),
		Accept:         r.Header.Get("Accept"),
		Connection:     r.Header.Get("Connection"),
		DNT:            r.Header.Get("DNT"),
		ScreenSize:     r.Header.Get("X-Screen-Size"), // Custom header
		Timezone:       r.Header.Get("X-Timezone"),    // Custom header
		Platform:       r.Header.Get("X-Platform"),    // Custom header
		IPAddress:      GetClientIP(r),
	}
}

// GenerateFingerprint creates a hash from device characteristics
func (df *DeviceFingerprint) GenerateFingerprint() string {
	characteristics := []string{
		df.UserAgent,
		df.AcceptLanguage,
		df.AcceptEncoding,
		df.Accept,
		df.Connection,
		df.DNT,
		df.ScreenSize,
		df.Timezone,
		df.Platform,
		// Note: IP address intentionally excluded as it can change
	}

	return GenerateDeviceFingerprint(characteristics...)
}

// Platform detection from User-Agent

// DetectPlatform attempts to detect the platform from User-Agent
func DetectPlatform(userAgent string) string {
	ua := strings.ToLower(userAgent)

	// Mobile platforms
	if strings.Contains(ua, "iphone") || strings.Contains(ua, "ipad") {
		return "ios"
	}
	if strings.Contains(ua, "android") {
		return "android"
	}

	// Desktop platforms
	if strings.Contains(ua, "windows") {
		return "windows"
	}
	if strings.Contains(ua, "macintosh") || strings.Contains(ua, "mac os") {
		return "macos"
	}
	if strings.Contains(ua, "linux") {
		return "linux"
	}

	// Web browsers
	if strings.Contains(ua, "chrome") || strings.Contains(ua, "firefox") ||
		strings.Contains(ua, "safari") || strings.Contains(ua, "edge") {
		return "web"
	}

	return "unknown"
}

// DetectDeviceType attempts to detect device type from User-Agent
func DetectDeviceType(userAgent string) string {
	ua := strings.ToLower(userAgent)

	// Mobile devices
	if strings.Contains(ua, "mobile") || strings.Contains(ua, "iphone") ||
		strings.Contains(ua, "android") {
		return "mobile"
	}

	// Tablets
	if strings.Contains(ua, "tablet") || strings.Contains(ua, "ipad") {
		return "tablet"
	}

	// Smart TV
	if strings.Contains(ua, "smart-tv") || strings.Contains(ua, "smarttv") {
		return "tv"
	}

	// Default to desktop
	return "desktop"
}

// Security headers utilities

// SecurityHeaders contains security-related HTTP headers
type SecurityHeaders struct {
	ContentSecurityPolicy   string
	StrictTransportSecurity string
	XFrameOptions           string
	XContentTypeOptions     string
	ReferrerPolicy          string
	PermissionsPolicy       string
}

// SetSecurityHeaders applies security headers to response
func SetSecurityHeaders(w http.ResponseWriter, headers *SecurityHeaders) {
	if headers.ContentSecurityPolicy != "" {
		w.Header().Set("Content-Security-Policy", headers.ContentSecurityPolicy)
	}
	if headers.StrictTransportSecurity != "" {
		w.Header().Set("Strict-Transport-Security", headers.StrictTransportSecurity)
	}
	if headers.XFrameOptions != "" {
		w.Header().Set("X-Frame-Options", headers.XFrameOptions)
	}
	if headers.XContentTypeOptions != "" {
		w.Header().Set("X-Content-Type-Options", headers.XContentTypeOptions)
	}
	if headers.ReferrerPolicy != "" {
		w.Header().Set("Referrer-Policy", headers.ReferrerPolicy)
	}
	if headers.PermissionsPolicy != "" {
		w.Header().Set("Permissions-Policy", headers.PermissionsPolicy)
	}
}

// GetDefaultSecurityHeaders returns recommended security headers
func GetDefaultSecurityHeaders() *SecurityHeaders {
	return &SecurityHeaders{
		ContentSecurityPolicy:   "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'",
		StrictTransportSecurity: "max-age=31536000; includeSubDomains",
		XFrameOptions:           "DENY",
		XContentTypeOptions:     "nosniff",
		ReferrerPolicy:          "strict-origin-when-cross-origin",
		PermissionsPolicy:       "geolocation=(), microphone=(), camera=()",
	}
}

// URL utilities

// BuildCallbackURL constructs OAuth callback URL
func BuildCallbackURL(baseURL, provider string, isWeb bool) string {
	flowType := "mobile"
	if isWeb {
		flowType = "web"
	}

	return fmt.Sprintf("%s/auth/oauth/%s/%s/callback",
		strings.TrimSuffix(baseURL, "/"), provider, flowType)
}

// ValidateRedirectURL checks if redirect URL is allowed
func ValidateRedirectURL(redirectURL string, allowedHosts []string) error {
	if redirectURL == "" {
		return fmt.Errorf("redirect URL is required")
	}

	parsedURL, err := url.Parse(redirectURL)
	if err != nil {
		return fmt.Errorf("invalid redirect URL format: %w", err)
	}

	// Check if host is in allowed list
	if len(allowedHosts) > 0 {
		allowed := false
		for _, host := range allowedHosts {
			if parsedURL.Host == host {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("redirect URL host not allowed: %s", parsedURL.Host)
		}
	}

	// Ensure HTTPS in production (allow HTTP for localhost in development)
	if parsedURL.Scheme != "https" {
		if parsedURL.Scheme != "http" ||
			(!strings.HasPrefix(parsedURL.Host, "localhost") &&
				!strings.HasPrefix(parsedURL.Host, "127.0.0.1")) {
			return fmt.Errorf("redirect URL must use HTTPS")
		}
	}

	return nil
}

// Request validation utilities

// ValidateOrigin validates request origin for CORS
func ValidateOrigin(r *http.Request, allowedOrigins []string) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true // No origin header (same-origin request)
	}

	for _, allowed := range allowedOrigins {
		if origin == allowed {
			return true
		}
	}

	return false
}

// Context utilities for request processing

// ContextKey type for context keys to avoid collisions
type ContextKey string

const (
	ContextKeyRequestID   ContextKey = "request_id"
	ContextKeyUserUUID    ContextKey = "user_uuid"
	ContextKeyUserID      ContextKey = "user_id"
	ContextKeyDeviceID    ContextKey = "device_id"
	ContextKeySessionID   ContextKey = "session_id"
	ContextKeyFingerprint ContextKey = "fingerprint"
	ContextKeyIPAddress   ContextKey = "ip_address"
	ContextKeyUserAgent   ContextKey = "user_agent"
)

// SetRequestContext adds values to request context
func SetRequestContext(ctx context.Context, key ContextKey, value interface{}) context.Context {
	return context.WithValue(ctx, key, value)
}

// GetFromContext extracts value from context
func GetFromContext(ctx context.Context, key ContextKey) (interface{}, bool) {
	value := ctx.Value(key)
	return value, value != nil
}

// GetStringFromContext extracts string value from context
func GetStringFromContext(ctx context.Context, key ContextKey) (string, bool) {
	value, ok := GetFromContext(ctx, key)
	if !ok {
		return "", false
	}

	str, ok := value.(string)
	return str, ok
}

// Rate limiting helpers

// GetRateLimitKey generates a rate limit key for a request
func GetRateLimitKey(r *http.Request, keyType string) string {
	ip := GetClientIP(r)
	return fmt.Sprintf("%s:%s", keyType, ip)
}

// GetUserRateLimitKey generates a user-specific rate limit key
func GetUserRateLimitKey(userUUID, keyType string) string {
	return fmt.Sprintf("%s:user:%s", keyType, userUUID)
}

// Time utilities

// GetRequestTime returns current time for request processing
func GetRequestTime() time.Time {
	return time.Now().UTC()
}

// FormatCookieExpiration formats time for cookie expiration
func FormatCookieExpiration(t time.Time) string {
	return t.Format(time.RFC1123)
}

// ParseQueryBool parses boolean query parameter
func ParseQueryBool(r *http.Request, key string, defaultValue bool) bool {
	value := r.URL.Query().Get(key)
	if value == "" {
		return defaultValue
	}

	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return defaultValue
	}

	return parsed
}

// ParseQueryInt parses integer query parameter
func ParseQueryInt(r *http.Request, key string, defaultValue int) int {
	value := r.URL.Query().Get(key)
	if value == "" {
		return defaultValue
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return defaultValue
	}

	return parsed
}
