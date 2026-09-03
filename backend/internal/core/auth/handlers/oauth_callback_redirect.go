package handlers

// The SPA-facing OAuth callback contract (spec §4.10) lives in this one
// file. Nothing else in the package may build a /auth/callback or
// /user/security URL — TestCallbackURLBuilders_StructuralScan enforces it —
// and no builder here may ever carry an access token, a refresh token, an
// email or a user id. The wire shape is CLOSED:
//
//	success:  {spa}/auth/callback?success=true&provider=<google|apple|github|discord>
//	failure:  {spa}/auth/callback?success=false&error=<allowlisted code>
//	MFA:      {spa}/auth/callback#requiresMfa=true&mfaToken=<one-shot id>&webauthnAvailable=<true|false>
//	link:     {spa}/user/security?tab=oauth&link=success|failed&provider=<p>[&code=<allowlisted>]
//	relay:    {client api}/v1/auth/client/oauth/complete?relay=<one-shot id>  (client tier only)
//
// The MFA continuation travels in the FRAGMENT so the five-minute one-shot
// challenge id never reaches a server log, a reverse proxy or a Referer;
// every redirect additionally sets Referrer-Policy: no-referrer and
// Cache-Control: no-store. Session bootstrap after success is recovered
// only from the audience-scoped HttpOnly refresh cookie. The relay id is a
// single-use, browser-bound handle like the IdP code, never a credential.

import (
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/services"
	"github.com/orkestra/backend/internal/shared/errcode"
)

const (
	oauthCallbackPath      = "/auth/callback"
	oauthLinkReturnPath    = "/user/security"
	oauthRelayCompletePath = "/v1/auth/client/oauth/complete"

	// Login-callback failure codes — the closed allowlist of spec §4.10.
	OAuthCallbackErrAccessDenied        = "oauth_access_denied"
	OAuthCallbackErrSignupDisabled      = "oauth_signup_disabled"
	OAuthCallbackErrLinkDisabled        = "oauth_link_disabled"
	OAuthCallbackErrEmailUnverified     = errcode.AuthOAuthEmailUnverified
	OAuthCallbackErrProviderUnavailable = "oauth_provider_unavailable"
	OAuthCallbackErrLoginFailed         = "oauth_login_failed"

	// Link-mode result codes on /user/security?tab=oauth&link=failed.
	oauthLinkCodeAlreadyLinked       = "already_linked"
	oauthLinkCodeDuplicateProvider   = "duplicate_provider"
	oauthLinkCodeInvalidUserInfo     = "invalid_userinfo"
	oauthLinkCodeAccessDenied        = "access_denied"
	oauthLinkCodeProviderUnavailable = "provider_unavailable"
	oauthLinkCodeInternal            = "internal"
)

var oauthCallbackErrorAllowlist = map[string]bool{
	OAuthCallbackErrAccessDenied:        true,
	OAuthCallbackErrSignupDisabled:      true,
	OAuthCallbackErrLinkDisabled:        true,
	OAuthCallbackErrEmailUnverified:     true,
	OAuthCallbackErrProviderUnavailable: true,
	OAuthCallbackErrLoginFailed:         true,
}

var oauthLinkCodeAllowlist = map[string]bool{
	oauthLinkCodeAlreadyLinked:       true,
	oauthLinkCodeDuplicateProvider:   true,
	oauthLinkCodeInvalidUserInfo:     true,
	oauthLinkCodeAccessDenied:        true,
	oauthLinkCodeProviderUnavailable: true,
	oauthLinkCodeInternal:            true,
}

// oauthLoginResult is the outcome a login callback hands the SPA. Exactly
// one shape is populated; the constructors below are the only way to build
// one so a caller cannot combine a success with an MFA token.
type oauthLoginResult struct {
	Provider          models.OAuthProvider
	Success           bool
	ErrorCode         string
	RequiresMFA       bool
	MFAToken          string
	WebAuthnAvailable bool
}

func oauthLoginSuccess(p models.OAuthProvider) oauthLoginResult {
	return oauthLoginResult{Provider: p, Success: true}
}

func oauthLoginFailure(p models.OAuthProvider, code string) oauthLoginResult {
	return oauthLoginResult{Provider: p, ErrorCode: code}
}

func oauthLoginMFA(p models.OAuthProvider, token string, webauthnAvailable bool) oauthLoginResult {
	return oauthLoginResult{Provider: p, RequiresMFA: true, MFAToken: token, WebAuthnAvailable: webauthnAvailable}
}

// SetSPAURL records this tier's SPA origin (see the spaBaseURL field).
func (h *AuthHandler) SetSPAURL(u string) {
	h.spaBaseURL = strings.TrimRight(strings.TrimSpace(u), "/")
}

// spaURL is the sole post-callback destination origin for this tier.
func (h *AuthHandler) spaURL() string {
	if h.spaBaseURL != "" {
		return h.spaBaseURL
	}
	if h.config != nil {
		return strings.TrimRight(h.config.Server.FrontendURL, "/")
	}
	return ""
}

// oauthLoginCallbackURL renders the closed login-callback contract.
func (h *AuthHandler) oauthLoginCallbackURL(res oauthLoginResult) string {
	base := h.spaURL() + oauthCallbackPath
	switch {
	case res.RequiresMFA:
		frag := url.Values{}
		frag.Set("requiresMfa", "true")
		frag.Set("mfaToken", res.MFAToken)
		frag.Set("webauthnAvailable", strconv.FormatBool(res.WebAuthnAvailable))
		return base + "#" + frag.Encode()
	case res.Success:
		q := url.Values{}
		q.Set("success", "true")
		q.Set("provider", string(res.Provider))
		return base + "?" + q.Encode()
	default:
		code := res.ErrorCode
		if !oauthCallbackErrorAllowlist[code] {
			code = OAuthCallbackErrLoginFailed
		}
		q := url.Values{}
		q.Set("success", "false")
		q.Set("error", code)
		return base + "?" + q.Encode()
	}
}

// writeOAuthLoginRedirect is the ONLY writer of a login-callback redirect.
func (h *AuthHandler) writeOAuthLoginRedirect(w http.ResponseWriter, r *http.Request, res oauthLoginResult) {
	setCallbackRedirectHeaders(w)
	http.Redirect(w, r, h.oauthLoginCallbackURL(res), http.StatusFound)
}

// oauthLinkReturnURL renders the link-mode return contract. Kept separate
// from the login builder on purpose: link mode never mints tokens and has
// its own page to land on.
func (h *AuthHandler) oauthLinkReturnURL(p models.OAuthProvider, ok bool, code string) string {
	q := url.Values{}
	q.Set("tab", "oauth")
	q.Set("provider", string(p))
	if ok {
		q.Set("link", "success")
	} else {
		if !oauthLinkCodeAllowlist[code] {
			code = oauthLinkCodeInternal
		}
		q.Set("link", "failed")
		q.Set("code", code)
	}
	return h.spaURL() + oauthLinkReturnPath + "?" + q.Encode()
}

// writeOAuthLinkRedirect is the ONLY writer of a link-mode redirect.
func (h *AuthHandler) writeOAuthLinkRedirect(w http.ResponseWriter, r *http.Request, p models.OAuthProvider, ok bool, code string) {
	setCallbackRedirectHeaders(w)
	http.Redirect(w, r, h.oauthLinkReturnURL(p, ok, code), http.StatusFound)
}

// relayCompleteURL renders the client API's relay endpoint for a one-shot
// relay id. ok is false when no client surface is configured — the caller
// then refuses the client-tier flow instead of guessing a host.
func (h *AuthHandler) relayCompleteURL(id string) (string, bool) {
	if h.config == nil || id == "" {
		return "", false
	}
	base := strings.TrimRight(strings.TrimSpace(h.config.Server.Client.PublicURL), "/")
	if base == "" {
		return "", false
	}
	q := url.Values{}
	q.Set("relay", id)
	return base + oauthRelayCompletePath + "?" + q.Encode(), true
}

// writeRelayRedirect is the ONLY writer of the relay redirect.
func (h *AuthHandler) writeRelayRedirect(w http.ResponseWriter, r *http.Request, target string) {
	setCallbackRedirectHeaders(w)
	http.Redirect(w, r, target, http.StatusFound)
}

func setCallbackRedirectHeaders(w http.ResponseWriter) {
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
}

// oauthLoginErrorCode maps an application error to the coarse allowlisted
// code the SPA may see plus the structured `outcome` for the server log.
// Raw error text never reaches a URL; the default is the generic code, not
// an HTTP error (errquality R3 does not apply to a redirect code).
func oauthLoginErrorCode(err error) (code, outcome string) {
	switch {
	case errors.Is(err, services.ErrOAuthSignupDisabled):
		return OAuthCallbackErrSignupDisabled, "signup_disabled"
	case errors.Is(err, services.ErrOAuthLinkDisabled):
		return OAuthCallbackErrLinkDisabled, "link_disabled"
	case errors.Is(err, services.ErrOAuthEmailUnverified):
		return OAuthCallbackErrEmailUnverified, "email_unverified"
	case errors.Is(err, services.ErrAuthPolicyUnavailable):
		return OAuthCallbackErrProviderUnavailable, "policy_unavailable"
	case errors.Is(err, services.ErrInvalidCredentials):
		return OAuthCallbackErrLoginFailed, "invalid_credentials"
	}
	return OAuthCallbackErrLoginFailed, "internal_error"
}

var idpErrorToken = regexp.MustCompile(`^[a-z_]{1,64}$`)

// sanitizeIdPError reduces the IdP's `error` parameter to a plain OAuth
// error token for the log line; anything else is "unrecognized". The value
// is never copied to the SPA URL — the SPA sees oauth_access_denied.
func sanitizeIdPError(raw string) string {
	if idpErrorToken.MatchString(raw) {
		return raw
	}
	return "unrecognized"
}
