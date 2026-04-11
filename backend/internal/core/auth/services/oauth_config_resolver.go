package services

import (
	"context"

	"github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/shared/module"
)

// OAuthConfigResolver builds a per-provider OAuthProviderConfig from the live
// module_configs document so admin-panel edits take effect without a restart.
// Reads are served by ModuleConfigService (30s Redis cache in front of Mongo),
// so calling this on every OAuth request is cheap.
type OAuthConfigResolver struct {
	cs *module.ModuleConfigService
}

// NewOAuthConfigResolver wires the resolver to the running ConfigService.
// Passing a nil service is valid — every lookup will then return (nil, false),
// which callers treat as "provider not configured".
func NewOAuthConfigResolver(cs *module.ModuleConfigService) *OAuthConfigResolver {
	return &OAuthConfigResolver{cs: cs}
}

// Get returns the current config for a provider, or (nil, false) if the
// client ID has not been set. The bool is the "is this provider usable"
// signal — handlers should fail fast with 4xx when it's false rather than
// constructing a provider from empty strings.
func (r *OAuthConfigResolver) Get(ctx context.Context, p models.OAuthProvider) (*OAuthProviderConfig, bool) {
	if r == nil || r.cs == nil {
		return nil, false
	}
	get := func(k string) string { return r.cs.GetValue(ctx, "auth", k) }
	sec := func(k string) string { return r.cs.GetSecret(ctx, "auth", k) }

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
