package handlers

// The OAuth state parameter proves the callback belongs to a flow we
// started — but on its own it does not prove it belongs to the flow
// THIS browser started.
//
// The signed-state JWT plus the one-shot Redis row already defeat state
// forgery and replay. What was missing is a binding to the user agent:
// an attacker could start a flow with their own account, hand the
// resulting authorize URL to a victim, and have the victim's browser
// complete it — landing the victim in the ATTACKER's session (login
// CSRF). In link mode the same trick binds the victim's identity
// provider account to the attacker's Orkestra account, so a later
// "Sign in with Google" delivers the victim into an account the
// attacker controls.
//
// The fix is the standard one: the CSRF nonce is also dropped in an
// HttpOnly cookie at start, and the callback requires the two to match.

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/orkestra/backend/internal/core/auth/services"
)

func stateClaims(csrf, startHost string) *services.OAuthStateClaims {
	return &services.OAuthStateClaims{CSRF: csrf, StartHost: startHost}
}

func callbackRequest(host, cookieValue string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/v1/auth/oauth/google/callback", nil)
	r.Host = host
	if cookieValue != "" {
		r.AddCookie(&http.Cookie{Name: OAuthStateCookieName, Value: cookieValue})
	}
	return r
}

func TestOAuthStateBinding_AcceptsMatchingCookie(t *testing.T) {
	r := callbackRequest("console.example.com", "nonce-abc")

	if err := verifyOAuthStateBinding(r, stateClaims("nonce-abc", "console.example.com")); err != nil {
		t.Fatalf("a flow completed in the browser that started it must be accepted: %v", err)
	}
}

func TestOAuthStateBinding_RejectsForeignCookie(t *testing.T) {
	// The victim's browser carries its own nonce (or none of the
	// attacker's); the state names a different flow.
	r := callbackRequest("console.example.com", "victims-own-nonce")

	if err := verifyOAuthStateBinding(r, stateClaims("attacker-nonce", "console.example.com")); err == nil {
		t.Fatal("a state whose nonce does not match this browser's cookie must be rejected")
	}
}

func TestOAuthStateBinding_RejectsMissingCookieOnSameHost(t *testing.T) {
	// Start and callback share a host, so the cookie was set and should
	// have come back. Its absence is the signature of a flow pasted into
	// a browser that never started one.
	r := callbackRequest("console.example.com", "")

	if err := verifyOAuthStateBinding(r, stateClaims("attacker-nonce", "console.example.com")); err == nil {
		t.Fatal("a missing state cookie on the starting host must be rejected")
	}
}

func TestOAuthStateBinding_AllowsCrossHostTierSplit(t *testing.T) {
	// ADR-0003: the client tier starts its flow on api.* but every
	// provider callback lands on console.* (one registered redirect URI
	// per provider). A cookie set by api.* is not sent to console.*, so
	// binding is impossible for that hop and must not break the flow.
	r := callbackRequest("console.example.com", "")

	if err := verifyOAuthStateBinding(r, stateClaims("nonce-abc", "api.example.com")); err != nil {
		t.Fatalf("a cross-host tier-split callback must still complete: %v", err)
	}
}

func TestOAuthStateBinding_RejectsLegacyStateWithNoStartHost(t *testing.T) {
	// A state carrying no start host predates this binding, or was
	// stripped. Without a start host we cannot tell "binding impossible"
	// from "binding skipped", so fail closed: a 10-minute-old in-flight
	// state is a small price next to an unbindable callback.
	r := callbackRequest("console.example.com", "")

	if err := verifyOAuthStateBinding(r, stateClaims("nonce-abc", "")); err == nil {
		t.Fatal("a state with no start host must not silently skip the binding check")
	}
}

func TestOAuthStateBinding_HostComparisonIgnoresPort(t *testing.T) {
	// Dev runs console.localhost:8080 in the browser and the backend
	// sees console.localhost:3000 — the same surface.
	r := callbackRequest("console.localhost:3000", "nonce-abc")

	if err := verifyOAuthStateBinding(r, stateClaims("nonce-abc", "console.localhost:8080")); err != nil {
		t.Fatalf("host comparison must ignore the port: %v", err)
	}
}
