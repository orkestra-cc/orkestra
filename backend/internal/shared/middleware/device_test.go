package middleware

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/orkestra/backend/internal/shared/errors"
	"github.com/orkestra/backend/internal/shared/types"
)

func TestDeviceMiddleware_ExtractDeviceInfo(t *testing.T) {
	// Setup
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	errorManager := errors.NewManager(logger, true)
	middleware := NewDeviceMiddleware(errorManager)

	// Test cases
	tests := []struct {
		name             string
		userAgent        string
		xForwardedFor    string
		expectedDevice   string
		expectedType     string
		expectedPlatform string
	}{
		{
			name:             "Chrome on Windows",
			userAgent:        "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
			xForwardedFor:    "192.168.1.100",
			expectedType:     "desktop",
			expectedPlatform: "windows",
		},
		{
			name:             "iPhone Safari",
			userAgent:        "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1",
			xForwardedFor:    "10.0.0.1",
			expectedType:     "mobile",
			expectedPlatform: "ios",
		},
		{
			name:             "Android Chrome",
			userAgent:        "Mozilla/5.0 (Linux; Android 14; SM-G991B) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36",
			xForwardedFor:    "172.16.0.1",
			expectedType:     "mobile",
			expectedPlatform: "android",
		},
		{
			name:             "iPad Safari",
			userAgent:        "Mozilla/5.0 (iPad; CPU OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1",
			xForwardedFor:    "203.0.113.1",
			expectedType:     "tablet",
			expectedPlatform: "ios",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create request
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.Header.Set("User-Agent", tt.userAgent)
			req.Header.Set("X-Forwarded-For", tt.xForwardedFor)

			// Create response recorder
			rr := httptest.NewRecorder()

			// Test handler that verifies device info is in context
			var capturedDeviceInfo *types.DeviceInfo
			testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedDeviceInfo = types.GetDeviceInfoFromContext(r.Context())
				w.WriteHeader(http.StatusOK)
			})

			// Apply middleware
			handler := middleware.ExtractDeviceInfo(testHandler)
			handler.ServeHTTP(rr, req)

			// Assertions
			if capturedDeviceInfo == nil {
				t.Fatal("Device info was not extracted")
			}

			if capturedDeviceInfo.DeviceType != tt.expectedType {
				t.Errorf("Expected device type %s, got %s", tt.expectedType, capturedDeviceInfo.DeviceType)
			}

			if capturedDeviceInfo.Platform != tt.expectedPlatform {
				t.Errorf("Expected platform %s, got %s", tt.expectedPlatform, capturedDeviceInfo.Platform)
			}

			if capturedDeviceInfo.UserAgent != tt.userAgent {
				t.Errorf("Expected user agent %s, got %s", tt.userAgent, capturedDeviceInfo.UserAgent)
			}

			if capturedDeviceInfo.IP != tt.xForwardedFor {
				t.Errorf("Expected IP %s, got %s", tt.xForwardedFor, capturedDeviceInfo.IP)
			}

			if capturedDeviceInfo.DeviceID == "" {
				t.Error("Device ID should not be empty")
			}

			if capturedDeviceInfo.Fingerprint == "" {
				t.Error("Device fingerprint should not be empty")
			}
		})
	}
}

func TestDeviceMiddleware_DeviceTypeDetection(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	errorManager := errors.NewManager(logger, true)
	middleware := NewDeviceMiddleware(errorManager)

	tests := []struct {
		userAgent    string
		expectedType string
	}{
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36", "desktop"},
		{"Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X)", "mobile"},
		{"Mozilla/5.0 (iPad; CPU OS 17_0 like Mac OS X)", "tablet"},
		{"Mozilla/5.0 (Linux; Android 14; SM-G991B) AppleWebKit/537.36 Mobile", "mobile"},
		{"Mozilla/5.0 (Linux; Android 14; SM-T870) AppleWebKit/537.36", "tablet"},
		{"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36", "desktop"},
		{"Unknown/1.0", "unknown"},
	}

	for _, tt := range tests {
		result := middleware.detectDeviceType(tt.userAgent)
		if result != tt.expectedType {
			t.Errorf("For user agent %s, expected %s, got %s", tt.userAgent, tt.expectedType, result)
		}
	}
}

func TestDeviceMiddleware_PlatformDetection(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	errorManager := errors.NewManager(logger, true)
	middleware := NewDeviceMiddleware(errorManager)

	tests := []struct {
		userAgent        string
		expectedPlatform string
	}{
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64)", "windows"},
		{"Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X)", "ios"},
		{"Mozilla/5.0 (iPad; CPU OS 17_0 like Mac OS X)", "ios"},
		{"Mozilla/5.0 (Linux; Android 14; SM-G991B)", "android"},
		{"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)", "macos"},
		{"Mozilla/5.0 (X11; Linux x86_64)", "linux"},
		{"Unknown/1.0", "unknown"},
	}

	for _, tt := range tests {
		result := middleware.detectPlatform(tt.userAgent)
		if result != tt.expectedPlatform {
			t.Errorf("For user agent %s, expected %s, got %s", tt.userAgent, tt.expectedPlatform, result)
		}
	}
}
