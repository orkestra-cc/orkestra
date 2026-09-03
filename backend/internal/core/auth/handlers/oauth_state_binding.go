package handlers

// OAuth state ↔ browser binding.
//
// The signed-state JWT and its one-shot Redis row prove a callback
// belongs to a flow *we* started. They do not prove it belongs to a flow
// **this browser** started — and that gap is a login-CSRF: an attacker
// starts a flow against their own account, gets the victim to open the
// resulting authorize URL, and the victim's browser finishes it, landing
// the victim inside the attacker's session. In link mode (`mode=link`)
// the same trick attaches the victim's identity-provider account to the
// attacker's Orkestra account, so the victim's next "Sign in with
// Google" hands them to the attacker.
//
// The binding is the textbook remedy: the CSRF nonce that already keys
// the Redis row is also written to an HttpOnly cookie when the flow
// starts, and the callback requires the two to agree.

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/orkestra/backend/internal/core/auth/services"
)

// OAuthStateCookieName carries the per-flow CSRF nonce.
const OAuthStateCookieName = "orkestra_oauth_state"

// oauthStateCookieMaxAge matches the 10-minute state TTL. A stale cookie
// is worthless anyway once the Redis row expires.
const oauthStateCookieMaxAge = 10 * 60

// ErrOAuthStateNotBound signals that the callback could not be tied to
// the browser that started the flow. Callers render the same neutral
// error they use for a bad state — an attacker must not learn which of
// the two checks rejected them.
var ErrOAuthStateNotBound = errors.New("oauth state is not bound to this browser")

// buildOAuthStateCookie renders the Set-Cookie value for a starting
// flow. Lax rather than Strict: the callback arrives as a top-level
// redirect from the identity provider, which Strict would suppress.
func buildOAuthStateCookie(csrf string, secure bool) string {
	c := &http.Cookie{
		Name:     OAuthStateCookieName,
		Value:    csrf,
		Path:     "/",
		MaxAge:   oauthStateCookieMaxAge,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	}
	return c.String()
}

// clearOAuthStateCookie renders the Set-Cookie that evicts the nonce
// once a flow completes, so a second callback cannot reuse it.
func clearOAuthStateCookie(secure bool) string {
	c := &http.Cookie{
		Name:     OAuthStateCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	}
	return c.String()
}

// verifyOAuthStateBinding checks that the callback is being made by the
// browser that started the flow.
//
// Three outcomes, decided in this order. A state with no StartHost is
// rejected. A CROSS-HOST callback is (true, nil): DEFERRED — the ADR-0003
// tier split puts client-tier starts on `api.*` while every provider
// callback lands on `console.*`, so the cookie set at start cannot reach
// this request and any cookie this host holds is irrelevant; the caller may
// continue ONLY by handing the flow to the relay endpoint on the start host,
// which requires that cookie (verifyRelayBinding). A SAME-HOST callback is
// (false, nil) when the cookie matches — bound — and rejected when the
// cookie is missing or names another flow. A cross-host callback is never
// simply accepted: before v4.3 it was, which made the "exception" the client
// tier's normal path and left login CSRF open there.
func verifyOAuthStateBinding(r *http.Request, claims *services.OAuthStateClaims) (deferred bool, err error) {
	if claims == nil || claims.CSRF == "" {
		return false, ErrOAuthStateNotBound
	}
	if claims.StartHost == "" {
		return false, fmt.Errorf("%w: state carries no start host", ErrOAuthStateNotBound)
	}
	// StartHost FIRST. A flow started on another host set its cookie there;
	// whatever cookie THIS host holds (typically the nonce of an unrelated
	// operator flow in the same browser) proves nothing about it and must
	// neither bind nor block it — the relay endpoint on the start host is
	// the only place that can decide.
	if !sameHost(claims.StartHost, r.Host) {
		return true, nil
	}

	cookie, cerr := r.Cookie(OAuthStateCookieName)
	if cerr != nil || cookie.Value == "" {
		return false, fmt.Errorf("%w: no state cookie presented on the starting host", ErrOAuthStateNotBound)
	}
	if subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(claims.CSRF)) == 1 {
		return false, nil
	}
	// A cookie that names a different flow is the clearest signal there
	// is: this browser started something else.
	return false, fmt.Errorf("%w: state nonce does not match the browser's cookie", ErrOAuthStateNotBound)
}

// verifyRelayBinding is the relay endpoint's check: it runs on the host
// that set the state cookie at start, so the cookie is REQUIRED and must
// equal the relay record's nonce. Fails closed on every other shape.
func verifyRelayBinding(r *http.Request, csrf string) error {
	if csrf == "" {
		return ErrOAuthStateNotBound
	}
	cookie, err := r.Cookie(OAuthStateCookieName)
	if err != nil || cookie.Value == "" {
		return fmt.Errorf("%w: no state cookie presented to the relay", ErrOAuthStateNotBound)
	}
	if subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(csrf)) != 1 {
		return fmt.Errorf("%w: relay nonce does not match the browser's cookie", ErrOAuthStateNotBound)
	}
	return nil
}

// sameHost compares two Host values ignoring the port — the browser
// talks to console.localhost:8080 while the backend sees
// console.localhost:3000, and they are the same surface.
func sameHost(a, b string) bool {
	return strings.EqualFold(hostWithoutPort(a), hostWithoutPort(b))
}

func hostWithoutPort(h string) string {
	h = strings.TrimSpace(h)
	if h == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(h); err == nil {
		return host
	}
	return h
}

// oauthStateTTL is the lifetime shared by the signed state, its Redis
// row, and the browser cookie.
const oauthStateTTL = 10 * time.Minute

// requestHost reads the Host of the request a Huma handler is serving.
// The raw *http.Request is stashed in the context by setupMiddleware.
// Returns "" when it is absent, which makes the callback fail closed on
// the binding check rather than silently skipping it.
func requestHost(ctx context.Context) string {
	if r, ok := ctx.Value("http_request").(*http.Request); ok && r != nil {
		return r.Host
	}
	return ""
}

// refreshCookieMaxAge keeps the browser cookie and the refresh token it
// carries on the same clock. They used to disagree: the cookie was
// pinned at 7 days while the token's own TTL came from
// JWT_REFRESH_TOKEN_EXPIRY, whose shipped default is 7d. 30d appears only
// as NewJWTService's unreachable zero-guard fallback; no configured
// deployment ever hits it, so it must not be read as the default.
func refreshCookieMaxAge(jwt services.JWTService) int {
	return int(jwt.RefreshTokenTTL().Seconds())
}
