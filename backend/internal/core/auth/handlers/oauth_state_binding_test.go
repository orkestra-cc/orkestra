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
// HttpOnly cookie at start, and the callback requires the two to match
// — or, when the cookie lives on another host (the ADR-0003 tier
// split), DEFERS that match to the relay endpoint that can see it.

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

	deferred, err := verifyOAuthStateBinding(r, stateClaims("nonce-abc", "console.example.com"))
	if err != nil || deferred {
		t.Fatalf("a flow completed in the browser that started it must be bound here, not deferred: deferred=%v err=%v", deferred, err)
	}
}

func TestOAuthStateBinding_RejectsForeignCookie(t *testing.T) {
	// The victim's browser carries its own nonce (or none of the
	// attacker's); the state names a different flow.
	r := callbackRequest("console.example.com", "victims-own-nonce")

	deferred, err := verifyOAuthStateBinding(r, stateClaims("attacker-nonce", "console.example.com"))
	if err == nil || deferred {
		t.Fatalf("a state whose nonce does not match this browser's cookie must be rejected, never deferred: deferred=%v err=%v", deferred, err)
	}
}

func TestOAuthStateBinding_RejectsMissingCookieOnSameHost(t *testing.T) {
	// Start and callback share a host, so the cookie was set and should
	// have come back. Its absence is the signature of a flow pasted into
	// a browser that never started one.
	r := callbackRequest("console.example.com", "")

	deferred, err := verifyOAuthStateBinding(r, stateClaims("attacker-nonce", "console.example.com"))
	if err == nil || deferred {
		t.Fatalf("a missing state cookie on the starting host must be rejected, never deferred: deferred=%v err=%v", deferred, err)
	}
}

func TestOAuthStateBinding_DefersCrossHostTierSplit(t *testing.T) {
	// ADR-0003: the client tier starts its flow on api.* but every
	// provider callback lands on console.* (one registered redirect URI
	// per provider). The cookie set by api.* is not sent to console.*, so
	// binding cannot be verified HERE — it is deferred to the relay
	// endpoint on api.*, which requires the cookie. It is never accepted.
	r := callbackRequest("console.example.com", "")

	deferred, err := verifyOAuthStateBinding(r, stateClaims("nonce-abc", "api.example.com"))
	if err != nil || !deferred {
		t.Fatalf("a cross-host tier-split callback must be DEFERRED, not bound or rejected: deferred=%v err=%v", deferred, err)
	}
}

func TestOAuthStateBinding_CrossHostIgnoresForeignCookie(t *testing.T) {
	// The same browser may hold the nonce of an unrelated OPERATOR flow on
	// console.*. That cookie proves nothing about a flow started on api.*
	// and must not block it: StartHost decides first, the cookie is only
	// compared on the starting host.
	r := callbackRequest("console.example.com", "some-operator-flows-nonce")

	deferred, err := verifyOAuthStateBinding(r, stateClaims("nonce-abc", "api.example.com"))
	if err != nil || !deferred {
		t.Fatalf("a cross-host callback must be deferred regardless of the callback host's cookie: deferred=%v err=%v", deferred, err)
	}
	// …and a matching cookie on the callback host is still a deferral, not
	// a binding — the api.* cookie can only be verified on api.*.
	r = callbackRequest("console.example.com", "nonce-abc")
	if deferred, err := verifyOAuthStateBinding(r, stateClaims("nonce-abc", "api.example.com")); err != nil || !deferred {
		t.Fatalf("deferred=%v err=%v", deferred, err)
	}
}

func TestOAuthStateBinding_RejectsLegacyStateWithNoStartHost(t *testing.T) {
	// A state carrying no start host predates this binding, or was
	// stripped. Without a start host we cannot tell "binding impossible"
	// from "binding skipped", so fail closed: a 10-minute-old in-flight
	// state is a small price next to an unbindable callback.
	r := callbackRequest("console.example.com", "")

	deferred, err := verifyOAuthStateBinding(r, stateClaims("nonce-abc", ""))
	if err == nil || deferred {
		t.Fatalf("a state with no start host must not silently skip the binding check: deferred=%v err=%v", deferred, err)
	}
}

func TestOAuthStateBinding_HostComparisonIgnoresPort(t *testing.T) {
	// Dev runs console.localhost:8080 in the browser and the backend
	// sees console.localhost:3000 — the same surface.
	r := callbackRequest("console.localhost:3000", "nonce-abc")

	deferred, err := verifyOAuthStateBinding(r, stateClaims("nonce-abc", "console.localhost:8080"))
	if err != nil || deferred {
		t.Fatalf("host comparison must ignore the port: deferred=%v err=%v", deferred, err)
	}
}

func TestVerifyRelayBinding(t *testing.T) {
	// The relay endpoint runs on the start host, so the cookie is REQUIRED.
	r := httptest.NewRequest(http.MethodGet, "/v1/auth/client/oauth/complete?relay=x", nil)
	r.Host = "api.example.com"
	if err := verifyRelayBinding(r, "nonce-abc"); err == nil {
		t.Fatal("a relay without the start-host state cookie must be refused")
	}
	r.AddCookie(&http.Cookie{Name: OAuthStateCookieName, Value: "victims-own-nonce"})
	if err := verifyRelayBinding(r, "nonce-abc"); err == nil {
		t.Fatal("a foreign nonce must be refused")
	}
	r2 := httptest.NewRequest(http.MethodGet, "/v1/auth/client/oauth/complete?relay=x", nil)
	r2.AddCookie(&http.Cookie{Name: OAuthStateCookieName, Value: "nonce-abc"})
	if err := verifyRelayBinding(r2, "nonce-abc"); err != nil {
		t.Fatalf("matching nonce must bind: %v", err)
	}
	if err := verifyRelayBinding(r2, ""); err == nil {
		t.Fatal("a record without a nonce can never bind")
	}
}
