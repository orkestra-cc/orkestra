package middleware

// Tests for trusted-proxy-aware client IP resolution.
//
// X-Forwarded-For and friends are attacker-controlled: any client can
// send them. Trusting the leftmost entry — which is what chi's RealIP
// and the old utils.GetClientIP did — lets an anonymous caller claim any
// source address, defeating the operator IP allowlist/blocklist, the
// geo-block on login, and the per-IP login rate limiter, while poisoning
// every audit row.
//
// The correct reading walks the forwarding chain from the RIGHT (the
// direct peer, which cannot be forged) and skips exactly the hops we
// know sit in front of us. Everything to the left of that is client-
// supplied and must not be trusted.

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func requestWithForwarding(remoteAddr, xff, xRealIP string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = remoteAddr
	if xff != "" {
		r.Header.Set("X-Forwarded-For", xff)
	}
	if xRealIP != "" {
		r.Header.Set("X-Real-IP", xRealIP)
	}
	return r
}

func TestResolveClientIP_NoTrustedProxiesIgnoresHeaders(t *testing.T) {
	// Default posture: nothing is trusted, so the direct peer is the
	// only truth. A spoofed header must have no effect.
	policy := TrustedProxyPolicy{}
	r := requestWithForwarding("10.0.0.5:41234", "1.2.3.4", "5.6.7.8")

	if got := policy.ResolveClientIP(r); got != "10.0.0.5" {
		t.Errorf("ResolveClientIP = %q, want 10.0.0.5 (headers must be ignored)", got)
	}
}

func TestResolveClientIP_HopCountSkipsOwnProxies(t *testing.T) {
	// Cloudflare → HAProxy → backend. Each hop appends its peer, so the
	// chain seen by the backend is [client, cloudflare] + peer haproxy.
	policy := TrustedProxyPolicy{Count: 2}
	r := requestWithForwarding("10.0.0.5:41234", "203.0.113.7, 198.51.100.1", "")

	if got := policy.ResolveClientIP(r); got != "203.0.113.7" {
		t.Errorf("ResolveClientIP = %q, want 203.0.113.7", got)
	}
}

func TestResolveClientIP_HopCountDefeatsSpoofedPrefix(t *testing.T) {
	// One real proxy in front. The attacker pre-seeds XFF with a lie;
	// the proxy appends the attacker's true address. Counting from the
	// right lands on the true address, never on the injected one.
	policy := TrustedProxyPolicy{Count: 1}
	r := requestWithForwarding("10.0.0.5:41234", "1.1.1.1, 203.0.113.9", "")

	got := policy.ResolveClientIP(r)
	if got == "1.1.1.1" {
		t.Fatal("spoofed leftmost XFF entry was trusted")
	}
	if got != "203.0.113.9" {
		t.Errorf("ResolveClientIP = %q, want 203.0.113.9", got)
	}
}

func TestResolveClientIP_CIDRModeSkipsTrustedHops(t *testing.T) {
	policy := mustPolicy(t, 0, []string{"10.0.0.0/8"})
	r := requestWithForwarding("10.0.0.5:41234", "1.1.1.1, 203.0.113.9, 10.0.0.2", "")

	if got := policy.ResolveClientIP(r); got != "203.0.113.9" {
		t.Errorf("ResolveClientIP = %q, want 203.0.113.9", got)
	}
}

func TestResolveClientIP_CIDRModeIgnoresHeaderFromUntrustedPeer(t *testing.T) {
	// Direct connection from an address outside the trusted set: the
	// whole header is client-supplied and must be discarded.
	policy := mustPolicy(t, 0, []string{"10.0.0.0/8"})
	r := requestWithForwarding("203.0.113.50:41234", "1.1.1.1", "")

	if got := policy.ResolveClientIP(r); got != "203.0.113.50" {
		t.Errorf("ResolveClientIP = %q, want 203.0.113.50", got)
	}
}

func TestResolveClientIP_ChainShorterThanHopCountFallsBackToPeer(t *testing.T) {
	// Misconfiguration (count larger than the real chain) must not
	// return an attacker-supplied entry — fall back to the direct peer.
	policy := TrustedProxyPolicy{Count: 5}
	r := requestWithForwarding("10.0.0.5:41234", "1.1.1.1", "")

	if got := policy.ResolveClientIP(r); got != "10.0.0.5" {
		t.Errorf("ResolveClientIP = %q, want 10.0.0.5", got)
	}
}

func TestResolveClientIP_IgnoresMalformedChainEntries(t *testing.T) {
	policy := TrustedProxyPolicy{Count: 1}
	r := requestWithForwarding("10.0.0.5:41234", "not-an-ip, 203.0.113.9", "")

	if got := policy.ResolveClientIP(r); got != "203.0.113.9" {
		t.Errorf("ResolveClientIP = %q, want 203.0.113.9", got)
	}
}

func TestResolveClientIP_XRealIPHonouredOnlyFromTrustedPeer(t *testing.T) {
	trusted := mustPolicy(t, 0, []string{"10.0.0.0/8"})
	if got := trusted.ResolveClientIP(requestWithForwarding("10.0.0.5:1", "", "203.0.113.7")); got != "203.0.113.7" {
		t.Errorf("trusted peer X-Real-IP = %q, want 203.0.113.7", got)
	}

	untrusted := mustPolicy(t, 0, []string{"10.0.0.0/8"})
	if got := untrusted.ResolveClientIP(requestWithForwarding("198.51.100.9:1", "", "203.0.113.7")); got != "198.51.100.9" {
		t.Errorf("untrusted peer X-Real-IP must be ignored, got %q", got)
	}
}

// RealIP rewrites RemoteAddr so every downstream consumer — the IP gate,
// the device middleware, utils.GetClientIP, the request logger — reads
// one already-validated value instead of re-parsing headers itself.
func TestRealIPMiddleware_RewritesRemoteAddr(t *testing.T) {
	policy := TrustedProxyPolicy{Count: 1}

	var seen string
	h := RealIP(policy)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = r.RemoteAddr
	}))
	h.ServeHTTP(httptest.NewRecorder(), requestWithForwarding("10.0.0.5:41234", "1.1.1.1, 203.0.113.9", ""))

	if seen != "203.0.113.9" {
		t.Errorf("downstream RemoteAddr = %q, want 203.0.113.9", seen)
	}
}

func TestRealIPMiddleware_StripsForwardingHeaders(t *testing.T) {
	// Downstream code must not be able to re-derive a spoofed value by
	// reading the raw headers itself.
	policy := TrustedProxyPolicy{Count: 1}

	var xff, xrip string
	h := RealIP(policy)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		xff = r.Header.Get("X-Forwarded-For")
		xrip = r.Header.Get("X-Real-IP")
	}))
	h.ServeHTTP(httptest.NewRecorder(), requestWithForwarding("10.0.0.5:1", "1.1.1.1, 203.0.113.9", "5.6.7.8"))

	if xff != "" {
		t.Errorf("X-Forwarded-For survived the middleware: %q", xff)
	}
	if xrip != "" {
		t.Errorf("X-Real-IP survived the middleware: %q", xrip)
	}
}

func mustPolicy(t *testing.T, count int, cidrs []string) TrustedProxyPolicy {
	t.Helper()
	p, err := NewTrustedProxyPolicy(count, cidrs)
	if err != nil {
		t.Fatalf("NewTrustedProxyPolicy: %v", err)
	}
	return p
}
