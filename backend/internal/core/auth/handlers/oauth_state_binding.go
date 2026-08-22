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
	"log/slog"
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
// Fails closed everywhere binding is possible. The single exception is
// the ADR-0003 tier split: the client surface starts flows on `api.*`
// while every provider callback lands on `console.*` (one registered
// redirect URI per provider), so a cookie set at start is simply not
// sent to the callback host. That hop is identified by comparing the
// signed StartHost claim against the callback's own host — a caller
// cannot forge it, because the claim is inside the HMAC-signed state.
//
// A state with no StartHost at all fails closed: without it we cannot
// distinguish "binding was impossible" from "binding was skipped", and
// the only cost of rejecting is that flows in flight across the deploy
// of this change ask the user to click sign-in again.
func verifyOAuthStateBinding(r *http.Request, claims *services.OAuthStateClaims) error {
	if claims == nil || claims.CSRF == "" {
		return ErrOAuthStateNotBound
	}

	cookie, err := r.Cookie(OAuthStateCookieName)
	if err == nil && cookie.Value != "" {
		if subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(claims.CSRF)) == 1 {
			return nil
		}
		// A cookie that names a different flow is the clearest signal
		// there is: this browser started something else.
		return fmt.Errorf("%w: state nonce does not match the browser's cookie", ErrOAuthStateNotBound)
	}

	if claims.StartHost == "" {
		return fmt.Errorf("%w: state carries no start host", ErrOAuthStateNotBound)
	}
	if sameHost(claims.StartHost, r.Host) {
		return fmt.Errorf("%w: no state cookie presented on the starting host", ErrOAuthStateNotBound)
	}

	// Cross-host tier split — binding is structurally impossible here.
	slog.Default().Info("oauth callback accepted without browser binding (cross-host tier split)",
		slog.String("start_host", claims.StartHost),
		slog.String("callback_host", r.Host),
		slog.String("tier", claims.Tier))
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
