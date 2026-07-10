package config

import "testing"

// TestCookieDomainDefaults_HostOnly guards the fix for the LAN-IP refresh
// bug: the cookie-domain default MUST be empty (host-only) in every
// environment. A non-empty dev default (previously "console.localhost" /
// "api.localhost") silently broke LAN-IP and hostname access — a cookie
// scoped to console.localhost is never sent back to 192.168.x.x, so every
// page refresh bounced the user to /login. An IP literal also cannot be a
// cookie Domain at all, so host-only is the only correct default; a
// cross-subdomain deployment sets OPERATOR_COOKIE_DOMAIN / CLIENT_COOKIE_DOMAIN
// explicitly instead.
func TestCookieDomainDefaults_HostOnly(t *testing.T) {
	for _, env := range []string{"development", "staging", "production", ""} {
		if got := defaultOperatorCookieDomain(env); got != "" {
			t.Errorf("defaultOperatorCookieDomain(%q) = %q, want \"\" (host-only)", env, got)
		}
		if got := defaultClientCookieDomain(env); got != "" {
			t.Errorf("defaultClientCookieDomain(%q) = %q, want \"\" (host-only)", env, got)
		}
	}
}
