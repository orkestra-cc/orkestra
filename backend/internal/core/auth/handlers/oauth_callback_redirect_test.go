package handlers

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/services"
	"github.com/orkestra/backend/internal/shared/config"
)

func redirectHandler(spa string) *AuthHandler {
	cfg := &config.Config{}
	cfg.Server.FrontendURL = "https://legacy.example"
	h := &AuthHandler{config: cfg}
	if spa != "" {
		h.SetSPAURL(spa)
	}
	return h
}

// parseCallback splits a built URL into (base, query, fragment) so tests
// assert on the CONTRACT, not on parameter order.
func parseCallback(t *testing.T, raw string) (string, url.Values, url.Values) {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	frag, err := url.ParseQuery(u.Fragment)
	if err != nil {
		t.Fatalf("parse fragment %q: %v", u.Fragment, err)
	}
	return u.Scheme + "://" + u.Host + u.Path, u.Query(), frag
}

func TestOAuthLoginCallbackURL_Success(t *testing.T) {
	h := redirectHandler("https://app.example/")
	base, q, frag := parseCallback(t, h.oauthLoginCallbackURL(oauthLoginSuccess(models.OAuthProviderGoogle)))
	if base != "https://app.example/auth/callback" {
		t.Fatalf("base = %q (trailing slash must be trimmed)", base)
	}
	if len(q) != 2 || q.Get("success") != "true" || q.Get("provider") != "google" {
		t.Fatalf("query = %v, want exactly success=true&provider=google", q)
	}
	if len(frag) != 0 {
		t.Fatalf("success carries no fragment: %v", frag)
	}
}

func TestOAuthLoginCallbackURL_FailureAllowlist(t *testing.T) {
	h := redirectHandler("https://console.example")
	for _, code := range []string{
		OAuthCallbackErrAccessDenied, OAuthCallbackErrSignupDisabled, OAuthCallbackErrLinkDisabled,
		OAuthCallbackErrEmailUnverified, OAuthCallbackErrProviderUnavailable, OAuthCallbackErrLoginFailed,
	} {
		_, q, frag := parseCallback(t, h.oauthLoginCallbackURL(oauthLoginFailure(models.OAuthProviderGitHub, code)))
		if len(q) != 2 || q.Get("success") != "false" || q.Get("error") != code || len(frag) != 0 {
			t.Fatalf("code %q: query = %v frag = %v", code, q, frag)
		}
	}
	for _, bad := range []string{"", "internal: mongo down", "invalid_credentials", "<script>", "auth.registration_disabled"} {
		_, q, _ := parseCallback(t, h.oauthLoginCallbackURL(oauthLoginFailure(models.OAuthProviderGitHub, bad)))
		if q.Get("error") != OAuthCallbackErrLoginFailed {
			t.Fatalf("%q must collapse to the generic code, got %q", bad, q.Get("error"))
		}
	}
	if OAuthCallbackErrEmailUnverified != "auth.oauth_email_unverified" {
		t.Fatalf("the web code must equal the errcode constant: %q", OAuthCallbackErrEmailUnverified)
	}
}

func TestOAuthLoginCallbackURL_MFAInFragmentOnly(t *testing.T) {
	h := redirectHandler("https://console.example")
	raw := h.oauthLoginCallbackURL(oauthLoginMFA(models.OAuthProviderApple, "challenge-1", true))
	base, q, frag := parseCallback(t, raw)
	if base != "https://console.example/auth/callback" || len(q) != 0 {
		t.Fatalf("MFA continuation must carry NO query: base=%q q=%v", base, q)
	}
	if len(frag) != 3 || frag.Get("requiresMfa") != "true" || frag.Get("mfaToken") != "challenge-1" || frag.Get("webauthnAvailable") != "true" {
		t.Fatalf("fragment = %v", frag)
	}
	if !strings.Contains(raw, "#") || strings.Contains(raw[:strings.Index(raw, "#")], "challenge-1") {
		t.Fatalf("the one-shot id must live after the '#': %q", raw)
	}
	_, _, frag = parseCallback(t, h.oauthLoginCallbackURL(oauthLoginMFA(models.OAuthProviderApple, "c", false)))
	if frag.Get("webauthnAvailable") != "false" {
		t.Fatalf("webauthnAvailable is always explicit: %v", frag)
	}
}

func TestSPAURL_PerTierValueThenLegacyFallback(t *testing.T) {
	if got := redirectHandler("").spaURL(); got != "https://legacy.example" {
		t.Fatalf("no per-tier value → FRONTEND_URL, got %q", got)
	}
	if got := redirectHandler("  https://app.example//  ").spaURL(); got != "https://app.example" {
		t.Fatalf("trim + trailing slashes, got %q", got)
	}
	var nilConfig AuthHandler
	if got := nilConfig.spaURL(); got != "" {
		t.Fatalf("no config, no value → empty, got %q", got)
	}
}

func TestWriteOAuthLoginRedirect_HeadersAndStatus(t *testing.T) {
	h := redirectHandler("https://app.example")
	rec := httptest.NewRecorder()
	h.writeOAuthLoginRedirect(rec, httptest.NewRequest(http.MethodGet, "/cb", nil), oauthLoginSuccess(models.OAuthProviderDiscord))
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d", rec.Code)
	}
	if rec.Header().Get("Referrer-Policy") != "no-referrer" || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("headers = %v", rec.Header())
	}
	if !strings.HasPrefix(rec.Header().Get("Location"), "https://app.example/auth/callback?") {
		t.Fatalf("Location = %q", rec.Header().Get("Location"))
	}
}

func TestOAuthLinkReturnURL(t *testing.T) {
	h := redirectHandler("https://console.example")
	base, q, _ := parseCallback(t, h.oauthLinkReturnURL(models.OAuthProviderGoogle, true, ""))
	if base != "https://console.example/user/security" || q.Get("tab") != "oauth" || q.Get("link") != "success" || q.Get("provider") != "google" || q.Has("code") {
		t.Fatalf("success: base=%q q=%v", base, q)
	}
	for _, code := range []string{oauthLinkCodeAlreadyLinked, oauthLinkCodeDuplicateProvider, oauthLinkCodeInvalidUserInfo, oauthLinkCodeAccessDenied, oauthLinkCodeProviderUnavailable, oauthLinkCodeInternal} {
		_, q, _ := parseCallback(t, h.oauthLinkReturnURL(models.OAuthProviderGoogle, false, code))
		if q.Get("link") != "failed" || q.Get("code") != code {
			t.Fatalf("code %q: q=%v", code, q)
		}
	}
	_, q, _ = parseCallback(t, h.oauthLinkReturnURL(models.OAuthProviderGoogle, false, "mongo: no documents"))
	if q.Get("code") != oauthLinkCodeInternal {
		t.Fatalf("unknown link code must collapse to internal: %v", q)
	}
	rec := httptest.NewRecorder()
	h.writeOAuthLinkRedirect(rec, httptest.NewRequest(http.MethodGet, "/cb", nil), models.OAuthProviderGoogle, false, oauthLinkCodeAccessDenied)
	if rec.Code != http.StatusFound || rec.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("link redirect: %d %v", rec.Code, rec.Header())
	}
}

func TestRelayCompleteURL(t *testing.T) {
	h := redirectHandler("https://console.example")
	if _, ok := h.relayCompleteURL("relay-1"); ok {
		t.Fatal("no client API origin → no relay destination")
	}
	h.config.Server.Client.PublicURL = "https://api.example/"
	got, ok := h.relayCompleteURL("relay-1")
	if !ok || got != "https://api.example/v1/auth/client/oauth/complete?relay=relay-1" {
		t.Fatalf("got %q ok=%v", got, ok)
	}
	if _, ok := h.relayCompleteURL(""); ok {
		t.Fatal("an empty relay id must not build a destination")
	}
	rec := httptest.NewRecorder()
	h.writeRelayRedirect(rec, httptest.NewRequest(http.MethodGet, "/cb", nil), got)
	if rec.Code != http.StatusFound || rec.Header().Get("Referrer-Policy") != "no-referrer" || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("relay redirect: %d %v", rec.Code, rec.Header())
	}
}

func TestOAuthLoginErrorCode(t *testing.T) {
	cases := []struct {
		err         error
		code, outcm string
	}{
		{services.ErrOAuthSignupDisabled, OAuthCallbackErrSignupDisabled, "signup_disabled"},
		{services.ErrOAuthLinkDisabled, OAuthCallbackErrLinkDisabled, "link_disabled"},
		{services.ErrOAuthEmailUnverified, OAuthCallbackErrEmailUnverified, "email_unverified"},
		{services.ErrAuthPolicyUnavailable, OAuthCallbackErrProviderUnavailable, "policy_unavailable"},
		{services.ErrInvalidCredentials, OAuthCallbackErrLoginFailed, "invalid_credentials"},
		{errors.New("user u-1 <secret@example.com> inactive"), OAuthCallbackErrLoginFailed, "internal_error"},
	}
	for _, tc := range cases {
		code, outcome := oauthLoginErrorCode(tc.err)
		if code != tc.code || outcome != tc.outcm {
			t.Errorf("%v → %q/%q, want %q/%q", tc.err, code, outcome, tc.code, tc.outcm)
		}
	}
}

func TestSanitizeIdPError(t *testing.T) {
	if got := sanitizeIdPError("access_denied"); got != "access_denied" {
		t.Fatalf("got %q", got)
	}
	for _, raw := range []string{"", "Access Denied", "<script>", strings.Repeat("a", 65), "user u-1 secret@example.com"} {
		if got := sanitizeIdPError(raw); got != "unrecognized" {
			t.Fatalf("%q → %q, want unrecognized", raw, got)
		}
	}
}
