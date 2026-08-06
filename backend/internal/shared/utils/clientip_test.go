package utils

// GetClientIP is the accessor every auth flow uses to learn who is
// calling: the login rate-limit bucket key, the geo-block lookup, the
// IP stamped on refresh rows, sessions, and audit events.
//
// It used to read X-Forwarded-For / X-Real-IP / CF-Connecting-IP /
// X-Client-IP directly, which made all of those attacker-controlled.
// Header interpretation now belongs to exactly one place —
// middleware.RealIP, which applies the deployment's trusted-proxy policy
// and rewrites RemoteAddr. This accessor must therefore read RemoteAddr
// and nothing else.

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetClientIP_IgnoresForwardingHeaders(t *testing.T) {
	spoofHeaders := []string{"X-Forwarded-For", "X-Real-IP", "CF-Connecting-IP", "X-Client-IP"}

	for _, header := range spoofHeaders {
		t.Run(header, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = "10.0.0.5:41234"
			r.Header.Set(header, "1.2.3.4")

			if got := GetClientIP(r); got != "10.0.0.5" {
				t.Errorf("GetClientIP = %q, want 10.0.0.5 (%s must not be trusted)", got, header)
			}
		})
	}
}

func TestGetClientIP_ReadsRemoteAddr(t *testing.T) {
	cases := map[string]string{
		"with port":         "203.0.113.9:41234",
		"without port":      "203.0.113.9",
		"ipv6 with port":    "[2001:db8::1]:41234",
		"ipv6 without port": "2001:db8::1",
	}
	want := map[string]string{
		"with port":         "203.0.113.9",
		"without port":      "203.0.113.9",
		"ipv6 with port":    "2001:db8::1",
		"ipv6 without port": "2001:db8::1",
	}

	for name, remoteAddr := range cases {
		t.Run(name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = remoteAddr

			if got := GetClientIP(r); got != want[name] {
				t.Errorf("GetClientIP(%q) = %q, want %q", remoteAddr, got, want[name])
			}
		})
	}
}
