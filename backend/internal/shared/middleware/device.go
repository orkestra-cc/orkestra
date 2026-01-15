package middleware

import (
	"context"
	"crypto/md5"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/orkestra/backend/internal/shared/errors"
	"github.com/orkestra/backend/internal/shared/types"
)

// DeviceMiddleware extracts device information from HTTP requests
type DeviceMiddleware struct {
	errorManager *errors.Manager
}

// NewDeviceMiddleware creates a new device middleware instance
func NewDeviceMiddleware(errorManager *errors.Manager) *DeviceMiddleware {
	return &DeviceMiddleware{
		errorManager: errorManager,
	}
}

// ExtractDeviceInfo middleware extracts device information and adds it to request context
func (m *DeviceMiddleware) ExtractDeviceInfo(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deviceInfo := m.extractDeviceInfo(r)
		ctx := context.WithValue(r.Context(), "deviceInfo", deviceInfo)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// extractDeviceInfo extracts device information from the HTTP request
func (m *DeviceMiddleware) extractDeviceInfo(r *http.Request) *types.DeviceInfo {
	userAgent := r.Header.Get("User-Agent")
	ip := m.extractClientIP(r)

	// Extract or generate device ID from headers
	deviceID := m.extractDeviceID(r)
	if deviceID == "" {
		deviceID = m.generateDeviceFingerprint(userAgent, ip, r)
	}

	deviceType := m.detectDeviceType(userAgent)
	platform := m.detectPlatformWithHeaders(userAgent, r)

	return &types.DeviceInfo{
		DeviceID:    deviceID,
		DeviceType:  deviceType,
		Platform:    platform,
		UserAgent:   userAgent,
		IP:          ip,
		Fingerprint: m.generateDeviceFingerprint(userAgent, ip, r),
		CreatedAt:   time.Now(),
	}
}

// extractDeviceID attempts to extract device ID from request headers
func (m *DeviceMiddleware) extractDeviceID(r *http.Request) string {
	// Check for custom device ID header (sent by mobile apps)
	if deviceID := r.Header.Get("X-Device-ID"); deviceID != "" {
		return deviceID
	}

	// Check for device ID in query parameters (for OAuth flows)
	if deviceID := r.URL.Query().Get("device_id"); deviceID != "" {
		return deviceID
	}

	return ""
}

// extractClientIP extracts the real client IP address
func (m *DeviceMiddleware) extractClientIP(r *http.Request) string {
	// Check X-Forwarded-For header first (proxy/load balancer)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		ips := strings.Split(xff, ",")
		if len(ips) > 0 {
			return strings.TrimSpace(ips[0])
		}
	}

	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}

	// Fall back to remote address
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}

	return host
}

// generateDeviceFingerprint creates a unique fingerprint for the device
func (m *DeviceMiddleware) generateDeviceFingerprint(userAgent, ip string, r *http.Request) string {
	// Collect fingerprinting data
	acceptLanguage := r.Header.Get("Accept-Language")
	acceptEncoding := r.Header.Get("Accept-Encoding")
	accept := r.Header.Get("Accept")

	// Create a fingerprint string
	fingerprint := fmt.Sprintf("%s|%s|%s|%s|%s",
		userAgent, ip, acceptLanguage, acceptEncoding, accept)

	// Generate MD5 hash of the fingerprint
	hash := md5.Sum([]byte(fingerprint))
	return fmt.Sprintf("%x", hash)
}

// detectDeviceType determines the device type from user agent
func (m *DeviceMiddleware) detectDeviceType(userAgent string) string {
	ua := strings.ToLower(userAgent)

	// Mobile detection
	mobileIndicators := []string{
		"mobile", "android", "iphone", "ipad", "ipod",
		"blackberry", "windows phone", "palm", "smartphone",
	}

	for _, indicator := range mobileIndicators {
		if strings.Contains(ua, indicator) {
			// Distinguish between tablet and mobile
			if strings.Contains(ua, "ipad") ||
				(strings.Contains(ua, "android") && !strings.Contains(ua, "mobile")) {
				return "tablet"
			}
			return "mobile"
		}
	}

	// Desktop/Web detection
	if strings.Contains(ua, "mozilla") ||
		strings.Contains(ua, "chrome") ||
		strings.Contains(ua, "safari") ||
		strings.Contains(ua, "firefox") {
		return "desktop"
	}

	return "unknown"
}

// detectPlatformWithHeaders determines the platform/OS from user agent and headers
func (m *DeviceMiddleware) detectPlatformWithHeaders(userAgent string, r *http.Request) string {
	// Check for explicit platform headers first (mobile apps can send these)
	if platformHeader := r.Header.Get("X-Platform"); platformHeader != "" {
		platform := strings.ToLower(platformHeader)
		if platform == "ios" || platform == "android" || platform == "windows" ||
			platform == "macos" || platform == "linux" {
			return platform
		}
	}

	// Fall back to User-Agent detection
	return m.detectPlatform(userAgent)
}

// detectPlatform determines the platform/OS from user agent
func (m *DeviceMiddleware) detectPlatform(userAgent string) string {
	ua := strings.ToLower(userAgent)

	// Mobile platforms
	if strings.Contains(ua, "iphone") || strings.Contains(ua, "ipad") || strings.Contains(ua, "ipod") {
		return "ios"
	}
	if strings.Contains(ua, "android") {
		return "android"
	}

	// Flutter mobile apps - determine platform from additional context
	if strings.Contains(ua, "flutter") {
		// Check for iOS indicators in custom User-Agent
		if strings.Contains(ua, "ios") || strings.Contains(ua, "iphone") ||
			strings.Contains(ua, "ipad") || strings.Contains(ua, "darwin") {
			return "ios"
		}

		// Check for Android indicators in custom User-Agent
		if strings.Contains(ua, "android") || strings.Contains(ua, "linux") {
			return "android"
		}

		// For Flutter apps without clear platform indicators, default to android
		// This can be improved by using X-Platform header or other indicators
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

	// Other mobile platforms
	if strings.Contains(ua, "blackberry") {
		return "blackberry"
	}
	if strings.Contains(ua, "windows phone") {
		return "windows_phone"
	}

	return "unknown"
}

// GetDeviceInfo retrieves device information from request context
func GetDeviceInfo(ctx context.Context) *types.DeviceInfo {
	if deviceInfo := ctx.Value("deviceInfo"); deviceInfo != nil {
		if di, ok := deviceInfo.(*types.DeviceInfo); ok {
			return di
		}
	}
	return nil
}
