package services

import "testing"

// SupportsPKCE (spec §4.9 D34) means "PROVEN to accept a code_verifier at
// the token endpoint", not "documents PKCE": a provider that ignores
// code_challenge but rejects code_verifier breaks the exchange entirely
// (edge case 24). Google and Discord are proven; GitHub and Apple stay
// false until the staging round-trip of §7 promotes them.
func TestSupportsPKCE_PerProvider(t *testing.T) {
	cfg := &OAuthProviderConfig{ClientID: "id", ClientSecret: "secret"}
	if !NewGoogleOAuthService(cfg).SupportsPKCE() {
		t.Error("google: proven, must be true")
	}
	if !NewDiscordOAuthService(cfg).SupportsPKCE() {
		t.Error("discord: proven, must be true")
	}
	if NewGitHubOAuthService(cfg).SupportsPKCE() {
		t.Error("github: must stay false until the staging round-trip proves it")
	}
	if (&appleOAuthService{}).SupportsPKCE() {
		t.Error("apple: must stay false until the staging round-trip proves it")
	}
}
