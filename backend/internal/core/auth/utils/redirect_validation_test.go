package utils

import (
	"strings"
	"testing"
)

// TestValidateRedirectURI_AcceptsItsOwnDefaults is the assertion that was
// missing when the OAuth defaults moved to the mounted /v1 path: the four
// backend callbacks this package ships in AllowedRedirectURIs have to pass
// the validator in the same package. They did not — validateLocalhostURI
// required an "/auth/" path prefix, which predates the /v1 mount — so the
// file shipped defaults its own validator refused.
//
// The frontend entry keeps the "/auth/" prefix and must keep passing; the
// two mobile deep links go down the scheme branch instead. Every entry of
// the default allow-list is therefore driven here, so a new entry that the
// validator would refuse fails immediately.
func TestValidateRedirectURI_AcceptsItsOwnDefaults(t *testing.T) {
	cfg := NewRedirectURIConfig(true)
	if len(cfg.AllowedRedirectURIs) != 7 {
		t.Fatalf("allow-list has %d entries, want 7 — add the new one to this test deliberately", len(cfg.AllowedRedirectURIs))
	}
	for _, uri := range cfg.AllowedRedirectURIs {
		t.Run(uri, func(t *testing.T) {
			if err := ValidateRedirectURI(uri, cfg); err != nil {
				t.Errorf("ValidateRedirectURI(%q) = %v, want nil — this package ships it as a default", uri, err)
			}
		})
	}
}

// The bound: widening the localhost path prefix must not turn the check off.
// An arbitrary localhost path is still refused, and the refusal still names
// both accepted prefixes.
func TestValidateRedirectURI_RejectsOffAuthLocalhostPaths(t *testing.T) {
	cfg := NewRedirectURIConfig(true)
	for _, uri := range []string{
		"http://localhost:3000/evil",
		"http://localhost:3000/v1/admin/users",
		"http://127.0.0.1:3000/",
	} {
		t.Run(uri, func(t *testing.T) {
			err := ValidateRedirectURI(uri, cfg)
			if err == nil {
				t.Fatalf("ValidateRedirectURI(%q) = nil, want a refusal", uri)
			}
			if !strings.Contains(err.Error(), "/auth/ or /v1/auth/") {
				t.Errorf("err = %v, want the message to name both accepted prefixes", err)
			}
		})
	}
}

// The other localhost branch is untouched: with AllowLocalhost=false every
// localhost URI is refused outright, whatever its path, and only the mobile
// schemes get through.
func TestValidateRedirectURI_LocalhostStillRefusedWhenDisallowed(t *testing.T) {
	cfg := NewRedirectURIConfig(false)
	if err := ValidateRedirectURI("http://localhost:3000/v1/auth/oauth/google/callback", cfg); err == nil {
		t.Error("localhost must stay refused when AllowLocalhost is false")
	}
	if err := ValidateRedirectURI("com.orkestra://oauth/callback", cfg); err != nil {
		t.Errorf("mobile deep link = %v, want nil", err)
	}
}
