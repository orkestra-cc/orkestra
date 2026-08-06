package middleware

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/orkestra/backend/internal/shared/errors"
	"github.com/orkestra/backend/internal/shared/types"
	"github.com/orkestra/backend/internal/shared/utils"
)

// DeviceIDCookieName carries the server-minted device identifier.
//
// Device identity must not be reproducible from the request. It used to
// be MD5(User-Agent | IP | Accept-Language | Accept-Encoding | Accept)
// for every browser — all caller-chosen inputs — so anyone replaying a
// victim's header signature was, to this system, the victim's device.
// That id keys the session documents, the refresh rows, the risk
// scorer's new-device detection (which suppresses the "new device"
// email and lowers the login risk score), and device-trust grants.
//
// It is now 32 bytes of crypto/rand handed back in an HttpOnly cookie:
// unguessable, not settable by the caller, and stable for a real
// browser. Native apps keep supplying their own installation id via
// X-Device-ID.
const DeviceIDCookieName = "orkestra_did"

// deviceIDCookieMaxAge keeps a browser recognisable for a year. The
// value is not a credential — it identifies a device, it does not
// authenticate one — so a long lifetime is what makes new-device
// detection meaningful rather than a permanent false positive.
const deviceIDCookieMaxAge = 365 * 24 * 60 * 60

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
		// Mint the cookie only when this request had no device identity
		// of its own; a returning browser or a native app carries one
		// already and re-issuing would churn the id on every request.
		if deviceInfo.DeviceID != "" && !hasDeviceIdentity(r) {
			http.SetCookie(w, &http.Cookie{
				Name:     DeviceIDCookieName,
				Value:    deviceInfo.DeviceID,
				Path:     "/",
				MaxAge:   deviceIDCookieMaxAge,
				HttpOnly: true,
				Secure:   r.TLS != nil,
				SameSite: http.SameSiteLaxMode,
			})
		}
		ctx := context.WithValue(r.Context(), "deviceInfo", deviceInfo)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// hasDeviceIdentity reports whether the caller already presented one.
func hasDeviceIdentity(r *http.Request) bool {
	if r.Header.Get("X-Device-ID") != "" {
		return true
	}
	c, err := r.Cookie(DeviceIDCookieName)
	return err == nil && c.Value != ""
}

// newDeviceID returns 32 bytes of crypto/rand as base64url. On the
// (practically impossible) failure of the system RNG we return "" and
// the caller degrades to an id-less request rather than falling back to
// something guessable.
func newDeviceID() string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}

// extractDeviceInfo extracts device information from the HTTP request
func (m *DeviceMiddleware) extractDeviceInfo(r *http.Request) *types.DeviceInfo {
	userAgent := r.Header.Get("User-Agent")
	ip := m.extractClientIP(r)

	// Device id: caller-presented identity first, then a fresh random
	// one. NEVER the header fingerprint — see DeviceIDCookieName.
	deviceID := m.extractDeviceID(r)
	if deviceID == "" {
		deviceID = newDeviceID()
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

// extractDeviceID returns the identity the caller presented, if any.
//
// The query-string source that used to sit here ("for OAuth flows") is
// gone: a device id readable from a URL is a device id an attacker can
// hand you in a link. The cookie is SameSite=Lax, so it survives the
// top-level redirect back from the identity provider on its own.
func (m *DeviceMiddleware) extractDeviceID(r *http.Request) string {
	// Native apps supply a stable installation id.
	if deviceID := r.Header.Get("X-Device-ID"); deviceID != "" {
		return deviceID
	}
	// Browsers carry the server-minted cookie.
	if c, err := r.Cookie(DeviceIDCookieName); err == nil && c.Value != "" {
		return c.Value
	}
	return ""
}

// extractClientIP reads the address RealIP already resolved under the
// deployment's trusted-proxy policy. Header parsing lives in exactly one
// place (middleware/realip.go) so a spoofed X-Forwarded-For cannot reach
// the device fingerprint, the risk score, or the audit trail.
func (m *DeviceMiddleware) extractClientIP(r *http.Request) string {
	return utils.GetClientIP(r)
}

// generateDeviceFingerprint summarises the browser signature.
//
// This is a RISK SIGNAL, not an identity. Every input is caller-chosen,
// so a match means "looks like the same browser", never "is the same
// device" — do not key a trust decision on it. SHA-256 rather than MD5:
// the value is security-adjacent and MD5 has no place in new code.
func (m *DeviceMiddleware) generateDeviceFingerprint(userAgent, ip string, r *http.Request) string {
	fingerprint := fmt.Sprintf("%s|%s|%s|%s|%s",
		userAgent, ip,
		r.Header.Get("Accept-Language"),
		r.Header.Get("Accept-Encoding"),
		r.Header.Get("Accept"))
	sum := sha256.Sum256([]byte(fingerprint))
	return hex.EncodeToString(sum[:])
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
