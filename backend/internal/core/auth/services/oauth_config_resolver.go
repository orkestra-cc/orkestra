package services

import (
	"context"
	"log/slog"

	"github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/pkg/sdk/module"
)

// OAuthConfigResolver builds a per-provider OAuthProviderConfig from the live
// module_configs document so admin-panel edits take effect without a restart.
// It keeps no process-local cache. Two read paths coexist: Get /
// RedirectURL / MobileAudience / ConfiguredProviders are the legacy per-key
// reads the mobile ID-token endpoints still use; OAuthWebProviderUsable /
// UsableWebProviders (oauth_provider_usability.go) are the strict, one-read
// web path.
type OAuthConfigResolver struct {
	cs     activeConfigReader
	logger *slog.Logger
	probe  KeyFileProbe
}

// NewOAuthConfigResolver wires the resolver to the running ConfigService.
// Passing a nil service is valid — every legacy lookup then returns
// (nil, false) and the strict path returns ErrAuthPolicyUnavailable.
func NewOAuthConfigResolver(cs *module.ModuleConfigService) *OAuthConfigResolver {
	r := &OAuthConfigResolver{logger: slog.Default(), probe: ReadableNonEmptyFile}
	if cs != nil {
		r.cs = cs
	}
	return r
}

// providerConfigFrom builds the OAuthProviderConfig for p from two accessor
// functions — GetValue/GetSecret closures on the legacy path, the view's
// Effective/Secret on the strict path — so both paths share one field map.
// ok is false when the client ID is empty (the legacy "not configured").
func providerConfigFrom(p models.OAuthProvider, get, sec func(string) string) (*OAuthProviderConfig, bool) {
	switch p {
	case models.OAuthProviderGoogle:
		id := get("googleClientId")
		if id == "" {
			return nil, false
		}
		return &OAuthProviderConfig{
			ClientID:     id,
			ClientSecret: sec("googleClientSecret"),
			Scopes:       []string{"openid", "email", "profile"},
			AdditionalConfig: map[string]string{
				"redirect_url":      get("googleRedirectURL"),
				"android_client_id": get("googleAndroidClientId"),
				"ios_client_id":     get("googleIOSClientId"),
			},
		}, true
	case models.OAuthProviderApple:
		id := get("appleClientId")
		if id == "" {
			return nil, false
		}
		return &OAuthProviderConfig{
			ClientID:     id,
			ClientSecret: "",
			Scopes:       []string{"name", "email"},
			AdditionalConfig: map[string]string{
				"team_id":           get("appleTeamId"),
				"key_id":            get("appleKeyId"),
				"private_key":       sec("applePrivateKey"),
				"private_key_path":  get("applePrivateKeyPath"),
				"redirect_url":      get("appleRedirectURL"),
				"ios_client_id":     get("appleIOSClientId"),
				"android_client_id": get("appleAndroidClientId"),
			},
		}, true
	case models.OAuthProviderGitHub:
		id := get("githubClientId")
		if id == "" {
			return nil, false
		}
		return &OAuthProviderConfig{
			ClientID:     id,
			ClientSecret: sec("githubClientSecret"),
			Scopes:       []string{"user:email", "read:user"},
			AdditionalConfig: map[string]string{
				"redirect_url": get("githubRedirectURL"),
			},
		}, true
	case models.OAuthProviderDiscord:
		id := get("discordClientId")
		if id == "" {
			return nil, false
		}
		return &OAuthProviderConfig{
			ClientID:     id,
			ClientSecret: sec("discordClientSecret"),
			Scopes:       []string{"identify", "email"},
			AdditionalConfig: map[string]string{
				"redirect_url": get("discordRedirectURL"),
			},
		}, true
	}
	return nil, false
}

// Get returns the current config for a provider, or (nil, false) if the
// client ID has not been set. Legacy per-key path (mobile). The web flow
// must use OAuthWebProviderUsable.
func (r *OAuthConfigResolver) Get(ctx context.Context, p models.OAuthProvider) (*OAuthProviderConfig, bool) {
	if r == nil || r.cs == nil {
		return nil, false
	}
	get := func(k string) string { return r.cs.GetValue(ctx, "auth", k) }
	sec := func(k string) string { return r.cs.GetSecret(ctx, "auth", k) }
	return providerConfigFrom(p, get, sec)
}

// RedirectURL returns the web callback URL for a provider. Prefer this over
// reading AdditionalConfig directly — it falls back to "" rather than panicking
// when the provider is unconfigured so callers can surface a clean 4xx.
func (r *OAuthConfigResolver) RedirectURL(ctx context.Context, p models.OAuthProvider) string {
	cfg, ok := r.Get(ctx, p)
	if !ok {
		return ""
	}
	return cfg.AdditionalConfig["redirect_url"]
}

// MobileAudience returns the platform-specific client ID used as the audience
// claim when validating a mobile ID token. platform is "ios", "android", or "".
// Empty platform falls back to the web ClientID.
func (r *OAuthConfigResolver) MobileAudience(ctx context.Context, p models.OAuthProvider, platform string) string {
	cfg, ok := r.Get(ctx, p)
	if !ok {
		return ""
	}
	switch platform {
	case "ios":
		if v := cfg.AdditionalConfig["ios_client_id"]; v != "" {
			return v
		}
	case "android":
		if v := cfg.AdditionalConfig["android_client_id"]; v != "" {
			return v
		}
	}
	return cfg.ClientID
}

// ConfiguredProviders returns only providers that currently have a client ID —
// the login UI uses this to decide which social buttons to render.
func (r *OAuthConfigResolver) ConfiguredProviders(ctx context.Context) []models.OAuthProvider {
	all := []models.OAuthProvider{
		models.OAuthProviderGoogle,
		models.OAuthProviderApple,
		models.OAuthProviderGitHub,
		models.OAuthProviderDiscord,
	}
	out := make([]models.OAuthProvider, 0, len(all))
	for _, p := range all {
		if _, ok := r.Get(ctx, p); ok {
			out = append(out, p)
		}
	}
	return out
}
