package handlers

// M-18 (spec §4.9 D34): the state row has carried a CodeVerifier field all
// along and every provider adapter already emits code_challenge/S256 and
// code_verifier when non-empty. Nothing ever supplied them, so the
// authorization code was interceptable with no proof of possession.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/services"
	"github.com/orkestra/backend/internal/core/auth/utils"
)

// newOAuthStartHandler is the callback harness; the provider fake answers
// SupportsPKCE from its pkce flag, which each case sets for the adapter it
// stands in for.
func newOAuthStartHandler(t *testing.T, pkce bool) (*callbackHarness, *fakeStateService, *fakeProvider) {
	t.Helper()
	hx := newCallbackHarness(t)
	hx.provider.pkce = pkce
	return hx, hx.state, hx.provider
}

func callOAuthStart(t *testing.T, hx *callbackHarness, provider models.OAuthProvider) error {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/v1/auth/oauth/login", nil)
	r.Host = callbackHost
	ctx := context.WithValue(r.Context(), "http_request", r)
	req := &OAuthLoginRequest{}
	req.Body.Provider = provider
	_, err := hx.operator.InitiateOAuthLogin(ctx, req)
	return err
}

func callOAuthLinkStart(t *testing.T, hx *callbackHarness, provider models.OAuthProvider) error {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/v1/auth/oauth/link/"+string(provider), nil)
	r.Host = callbackHost
	ctx := context.WithValue(r.Context(), "http_request", r)
	ctx = context.WithValue(ctx, "userUUID", "u-1")
	_, err := hx.operator.InitiateOAuthLink(ctx, &OAuthLinkRequest{Provider: string(provider)})
	return err
}

func (f *fakeStateService) lastStored() *services.StoreOAuthStateRequest {
	if len(f.stored) == 0 {
		return &services.StoreOAuthStateRequest{}
	}
	return f.stored[len(f.stored)-1]
}

func TestOAuthStart_StoresAVerifierAndSendsAChallengeForPKCEProviders(t *testing.T) {
	for _, p := range []models.OAuthProvider{models.OAuthProviderGoogle, models.OAuthProviderDiscord} {
		t.Run(string(p), func(t *testing.T) {
			hx, store, prov := newOAuthStartHandler(t, true)
			if err := callOAuthStart(t, hx, p); err != nil {
				t.Fatalf("start: %v", err)
			}
			row := store.lastStored()
			if row.CodeVerifier == "" {
				t.Fatal("the verifier must be stored in the state row")
			}
			if prov.lastChallenge() == "" {
				t.Fatal("the challenge must be sent to the provider")
			}
			if prov.lastChallenge() != utils.GenerateCodeChallenge(row.CodeVerifier) {
				t.Fatal("the challenge must be the S256 of the stored verifier")
			}
		})
	}
}

// The link-start endpoint is the second half of the same contract.
func TestOAuthLinkStart_StoresAVerifierAndSendsAChallenge(t *testing.T) {
	hx, store, prov := newOAuthStartHandler(t, true)
	if err := callOAuthLinkStart(t, hx, models.OAuthProviderGoogle); err != nil {
		t.Fatalf("link start: %v", err)
	}
	row := store.lastStored()
	if row.CodeVerifier == "" || prov.lastChallenge() != utils.GenerateCodeChallenge(row.CodeVerifier) {
		t.Fatalf("verifier %q / challenge %q — the link start must mint and send PKCE too", row.CodeVerifier, prov.lastChallenge())
	}
}

// Edge case 24: a provider that ignores code_challenge but REJECTS
// code_verifier would break entirely. SupportsPKCE is what prevents it.
func TestOAuthStart_NonPKCEProvidersGetNeither(t *testing.T) {
	for _, p := range []models.OAuthProvider{models.OAuthProviderGitHub, models.OAuthProviderApple} {
		t.Run(string(p), func(t *testing.T) {
			hx, store, prov := newOAuthStartHandler(t, false)
			if err := callOAuthStart(t, hx, p); err != nil {
				t.Fatalf("start: %v", err)
			}
			if store.lastStored().CodeVerifier != "" {
				t.Fatal("no verifier for a provider that has not been proven to accept one")
			}
			if prov.lastChallenge() != "" {
				t.Fatal("no challenge either")
			}
		})
	}
}

func TestOAuthCallback_VerifierReachesTheExchange(t *testing.T) {
	hx := newCallbackHarness(t)
	hx.state.info.CodeVerifier = "the-verifier"

	rec := httptest.NewRecorder()
	hx.dispatcher.HandleGoogleCallbackHTTP(rec, hx.request(t, callbackOpts{tier: "", cookie: true}))
	if hx.provider.exchanges != 1 {
		t.Fatalf("exchanges = %d, want 1 (status %d, location %q)", hx.provider.exchanges, rec.Code, rec.Header().Get("Location"))
	}
	if got := hx.provider.lastExchange().CodeVerifier; got != "the-verifier" {
		t.Fatalf("CodeVerifier = %q, want the stored one", got)
	}
}

func TestOAuthCallback_EmptyVerifierIsSentAsEmpty(t *testing.T) {
	hx := newCallbackHarness(t)
	hx.state.info.CodeVerifier = ""

	rec := httptest.NewRecorder()
	hx.dispatcher.HandleGitHubCallbackHTTP(rec, hx.request(t, callbackOpts{tier: "", path: "/v1/auth/oauth/github/callback", cookie: true, provider: models.OAuthProviderGitHub}))
	if hx.provider.exchanges != 1 {
		t.Fatalf("exchanges = %d, want 1 (status %d)", hx.provider.exchanges, rec.Code)
	}
	if hx.provider.lastExchange().CodeVerifier != "" {
		t.Fatal("a non-PKCE provider must receive no verifier — today's behaviour exactly")
	}
}
