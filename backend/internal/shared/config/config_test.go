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

// A presigned browser upload is a cross-origin PUT to the storage host, so the
// bucket policy must name the console's origin. Defaulting that list to
// CORS_ORIGINS alone is not enough: a deployment that has moved to the
// per-audience lists leaves CORS_ORIGINS on its localhost default, and the
// real console lives in OPERATOR_CORS_ORIGINS.
func TestStorageCORSOriginsDefaultToEveryAPIOrigin(t *testing.T) {
	t.Setenv("CORS_ORIGINS", "http://localhost:8080")
	t.Setenv("OPERATOR_CORS_ORIGINS", "https://console.example.com")
	t.Setenv("CLIENT_CORS_ORIGINS", "https://app.example.com,http://localhost:8080")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	got := cfg.Storage.CORSAllowedOrigins
	want := []string{
		"http://localhost:8080",
		"https://console.example.com",
		"https://app.example.com",
	}
	if len(got) != len(want) {
		t.Fatalf("origins = %v, want %v (deduplicated union)", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("origins = %v, want %v", got, want)
		}
	}
}

func TestStorageCORSOriginsHonorAnExplicitList(t *testing.T) {
	t.Setenv("CORS_ORIGINS", "http://localhost:8080")
	t.Setenv("OPERATOR_CORS_ORIGINS", "https://console.example.com")
	t.Setenv("STORAGE_CORS_ALLOWED_ORIGINS", "https://uploads.example.com")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(cfg.Storage.CORSAllowedOrigins) != 1 ||
		cfg.Storage.CORSAllowedOrigins[0] != "https://uploads.example.com" {
		t.Fatalf("explicit list ignored: %v", cfg.Storage.CORSAllowedOrigins)
	}
}
