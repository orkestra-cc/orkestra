package config

// getEnvAsDuration silently swallowed the one unit the config actually
// uses.
//
// Go's time.ParseDuration has no "d" — the largest unit is "h". So
// JWT_REFRESH_TOKEN_EXPIRY="7d" failed to parse, the "7d" fallback
// default failed the same way, and the function returned 0. NewJWTService
// then treated 0 as "unset" and substituted 30 days. Every compose file
// and .env.example in the tree writes this value with a "d" suffix, so
// the refresh-token lifetime was 30 days on every deployment regardless
// of configuration — and nothing surfaced it, because a zero return is
// indistinguishable from "not set".
//
// This mattered little while the persisted refresh row carried its own
// hardcoded 7-day expiry (the row is what actually gates a refresh, so
// it capped sessions at 7 days). Once the row started following the
// configured TTL, the broken parse became the effective session
// lifetime.

import (
	"testing"
	"time"
)

func TestGetEnvAsDuration_SupportsDaySuffix(t *testing.T) {
	cases := map[string]time.Duration{
		"7d":   7 * 24 * time.Hour,
		"1d":   24 * time.Hour,
		"30d":  30 * 24 * time.Hour,
		"0d":   0,
		"1.5d": 36 * time.Hour,
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			t.Setenv("ORKESTRA_TEST_DURATION", in)
			if got := getEnvAsDuration("ORKESTRA_TEST_DURATION", "15m"); got != want {
				t.Errorf("getEnvAsDuration(%q) = %s, want %s", in, got, want)
			}
		})
	}
}

func TestGetEnvAsDuration_DaySuffixInTheDefault(t *testing.T) {
	// The fallback path must understand the same syntax the caller is
	// allowed to write, or an unset variable silently yields zero.
	if got := getEnvAsDuration("ORKESTRA_TEST_DURATION_UNSET", "7d"); got != 7*24*time.Hour {
		t.Errorf("default %q = %s, want 168h", "7d", got)
	}
}

func TestGetEnvAsDuration_StandardUnitsUnchanged(t *testing.T) {
	cases := map[string]time.Duration{
		"15m":   15 * time.Minute,
		"1h":    time.Hour,
		"3s":    3 * time.Second,
		"0":     0,
		"1h30m": 90 * time.Minute,
		"500ms": 500 * time.Millisecond,
		"720h":  720 * time.Hour,
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			t.Setenv("ORKESTRA_TEST_DURATION", in)
			if got := getEnvAsDuration("ORKESTRA_TEST_DURATION", "15m"); got != want {
				t.Errorf("getEnvAsDuration(%q) = %s, want %s", in, got, want)
			}
		})
	}
}

func TestGetEnvAsDuration_GarbageFallsBackToDefault(t *testing.T) {
	t.Setenv("ORKESTRA_TEST_DURATION", "not-a-duration")
	if got := getEnvAsDuration("ORKESTRA_TEST_DURATION", "7d"); got != 7*24*time.Hour {
		t.Errorf("garbage value must fall back to the default, got %s", got)
	}
}

func TestLoadedRefreshTTL_HonoursDaySuffix(t *testing.T) {
	// End-to-end through the real config accessor: this is the value
	// that becomes the refresh row's expiry, the cookie Max-Age, and the
	// token's own exp claim.
	t.Setenv("JWT_REFRESH_TOKEN_EXPIRY", "7d")
	if got := getEnvAsDuration("JWT_REFRESH_TOKEN_EXPIRY", "7d"); got != 7*24*time.Hour {
		t.Errorf("JWT_REFRESH_TOKEN_EXPIRY=7d resolved to %s, want 168h", got)
	}
}
