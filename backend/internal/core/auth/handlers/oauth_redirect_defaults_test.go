package handlers

import (
	"net/http"
	"net/url"
	"sort"
	"testing"

	"github.com/go-chi/chi/v5"

	authUtils "github.com/orkestra/backend/internal/core/auth/utils"
	"github.com/orkestra/backend/internal/shared/config"
)

// oauthRedirectDefaultHost is the host both default sets carry. It is
// convention A (spec §8 #13): the orkestra_oauth_state cookie is host-only
// and SameSite=Lax, so the host that starts the login POST and the host
// that receives the callback must be the SAME host — not merely the same
// site — and localhost:3000 is the host the shipped console talks to.
const oauthRedirectDefaultHost = "localhost:3000"

// TestOAuthRedirectDefaultsAreMountedRoutes covers the eight OAuth-callback
// defaults spec §8 #13 names: the four OAUTH_*_REDIRECT_URL fallbacks in
// internal/shared/config/config.go and the four backend entries in
// utils.NewRedirectURIConfig's AllowedRedirectURIs.
//
// Neither set is on the OAuth path. The redirect_uri the flow actually
// hands the IdP is the auth module config auth.<provider>RedirectURL
// (services.OAuthConfigResolver); cfg.Auth.<Provider>.RedirectURL has no
// reader outside this file, and ValidateRedirectURI — the only consumer the
// allow-list would ever have — has no production caller. They are two
// hand-maintained lists naming the mounted callback path, and they used to
// name /auth/oauth/{provider}/callback — the pre-/v1 path, which the router
// serves at no method: RegisterOAuthRoutes mounts all four callbacks under
// /v1/auth/oauth/{provider}/callback. This test is the only thing that would
// catch that drift again, precisely because no runtime read of either list
// exists to fail loudly.
//
// The assertion is against the REAL registration function walked with
// chi.Walk, not a second hand-written list of paths — a list would just be
// the same claim written twice, and would keep passing if someone moved the
// mount.
//
// Scope is deliberately those eight. AllowedRedirectURIs' three other
// entries are out: http://localhost:8080/auth/callback is a front-end route
// and com.orkestra://oauth/callback / com.orkestra.app://oauth/callback are
// mobile deep links. The Go router serves none of them, so "is this a
// mounted route" is not a question that applies. They are separated here by
// host rather than by index, so the filter keeps meaning what it says if the
// list is reordered.
func TestOAuthRedirectDefaultsAreMountedRoutes(t *testing.T) {
	// Empty means "unset" to getEnv, so this yields the compiled fallbacks
	// even in an environment that exports the real ones.
	for _, k := range []string{
		"OAUTH_GOOGLE_REDIRECT_URL",
		"OAUTH_APPLE_REDIRECT_URL",
		"OAUTH_DISCORD_REDIRECT_URL",
		"OAUTH_GITHUB_REDIRECT_URL",
	} {
		t.Setenv(k, "")
	}
	// Load() runs Validate(), which requires OAuth client credentials in a
	// production-like env — pin the environment so the test asserts the
	// compiled fallbacks regardless of the shell it runs in.
	t.Setenv("ENV", "development")
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}

	mounted := mountedOAuthCallbackPaths(t)
	if len(mounted) != 4 {
		t.Fatalf("router mounts %d OAuth callback path(s) (%v), want 4", len(mounted), mounted)
	}

	configured := map[string]string{
		"config.Auth.Google.RedirectURL":  cfg.Auth.Google.RedirectURL,
		"config.Auth.Apple.RedirectURL":   cfg.Auth.Apple.RedirectURL,
		"config.Auth.Discord.RedirectURL": cfg.Auth.Discord.RedirectURL,
		"config.Auth.GitHub.RedirectURL":  cfg.Auth.GitHub.RedirectURL,
	}
	for _, raw := range backendAllowedRedirectURIs(t) {
		configured["AllowedRedirectURIs "+raw] = raw
	}
	if len(configured) != 8 {
		t.Fatalf("collected %d default(s), want the 8 in scope: %v", len(configured), configured)
	}

	for name, raw := range configured {
		t.Run(name, func(t *testing.T) {
			u, err := url.Parse(raw)
			if err != nil {
				t.Fatalf("parse %q: %v", raw, err)
			}
			if u.Scheme != "http" || u.Host != oauthRedirectDefaultHost {
				t.Errorf("%q = %s://%s, want http://%s", raw, u.Scheme, u.Host, oauthRedirectDefaultHost)
			}
			if !mounted[u.Path] {
				t.Errorf("%q points at %q, which the router serves at no method; mounted callbacks are %v",
					raw, u.Path, sortedKeys(mounted))
			}
		})
	}

	// The two sets must also agree with each other: a fallback the config
	// carries that the allow-list would refuse (or vice versa) is a
	// split-brain that neither half's own assertions can see.
	cfgPaths := map[string]bool{}
	for _, raw := range configured {
		u, err := url.Parse(raw)
		if err != nil {
			continue
		}
		cfgPaths[u.Path] = true
	}
	if len(cfgPaths) != 4 {
		t.Errorf("the 8 defaults cover %d distinct path(s) (%v), want the same 4 on both sides",
			len(cfgPaths), sortedKeys(cfgPaths))
	}
}

// mountedOAuthCallbackPaths walks the routes RegisterOAuthRoutes actually
// registers and returns the OAuth-callback ones. The zero-value handler is
// enough: registration only takes method values, it dereferences nothing.
func mountedOAuthCallbackPaths(t *testing.T) map[string]bool {
	t.Helper()
	router := chi.NewRouter()
	(&AuthHandler{}).RegisterOAuthRoutes(nil, nil, router, nil)

	paths := map[string]bool{}
	err := chi.Walk(router, func(_ string, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if u, err := url.Parse(route); err == nil && isOAuthCallbackPath(u.Path) {
			paths[u.Path] = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("chi.Walk: %v", err)
	}
	return paths
}

func isOAuthCallbackPath(path string) bool {
	const prefix = "/v1/auth/oauth/"
	const suffix = "/callback"
	return len(path) > len(prefix)+len(suffix) &&
		path[:len(prefix)] == prefix &&
		path[len(path)-len(suffix):] == suffix
}

// backendAllowedRedirectURIs returns the entries of the default allow-list
// that name the backend host — the four in scope. The frontend route
// (localhost:8080) and the two mobile deep links have different hosts and
// schemes, so they fall out without being enumerated here.
func backendAllowedRedirectURIs(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, raw := range authUtils.NewRedirectURIConfig(true).AllowedRedirectURIs {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatalf("parse allow-list entry %q: %v", raw, err)
		}
		if u.Host == oauthRedirectDefaultHost {
			out = append(out, raw)
		}
	}
	if len(out) != 4 {
		t.Fatalf("allow-list has %d entry(ies) on %s (%v), want the 4 backend callbacks",
			len(out), oauthRedirectDefaultHost, out)
	}
	return out
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
