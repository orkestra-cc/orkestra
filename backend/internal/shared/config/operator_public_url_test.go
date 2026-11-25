package config

import "testing"

func TestOperatorPublicURL_ExplicitAndDerived(t *testing.T) {
	t.Setenv("OPERATOR_API_URL", "http://127.0.0.1:3007")
	t.Setenv("CONSOLE_HOST", "console.example.test")
	t.Setenv("ENV", "development")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Operator.PublicURL != "http://127.0.0.1:3007" {
		t.Fatalf("explicit = %q", cfg.Server.Operator.PublicURL)
	}

	t.Setenv("OPERATOR_API_URL", "")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Operator.PublicURL != "http://console.example.test" {
		t.Fatalf("derived (development) = %q", cfg.Server.Operator.PublicURL)
	}

	// Production-like environments terminate TLS in front of the API. Load()
	// refuses production without JWT keys, so the scheme rule is pinned on
	// the helper Load() feeds.
	if got := derivedPublicURL("console.example.test", true); got != "https://console.example.test" {
		t.Fatalf("derived (secure) = %q", got)
	}
}
