package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/services"
	"github.com/orkestra/backend/internal/shared/errcode"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
)

func codeOf(t *testing.T, err error) (int, string) {
	t.Helper()
	var e *errcode.Error
	if !errors.As(err, &e) {
		t.Fatalf("expected *errcode.Error, got %T (%v)", err, err)
	}
	return e.Status, e.Code
}

func TestListOAuthProviders_UsableOnly(t *testing.T) {
	hx := newCallbackHarness(t)
	hx.resolver.list = []models.OAuthProvider{models.OAuthProviderGoogle, models.OAuthProviderGitHub}
	resp, err := hx.operator.ListOAuthProviders(context.Background(), &ListOAuthProvidersRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(resp.Body.Providers, ",") != "google,github" {
		t.Fatalf("providers = %v", resp.Body.Providers)
	}
}

func TestListOAuthProviders_DocumentLevelFailureIs503(t *testing.T) {
	hx := newCallbackHarness(t)
	hx.resolver.err = errors.New("mongo down")
	_, err := hx.operator.ListOAuthProviders(context.Background(), &ListOAuthProvidersRequest{})
	if status, code := codeOf(t, err); status != http.StatusServiceUnavailable || code != errcode.AuthPolicyUnavailable {
		t.Fatalf("got %d %s", status, code)
	}
}

func TestListOAuthProviders_EmptyIsNotAnError(t *testing.T) {
	hx := newCallbackHarness(t)
	hx.resolver.list = nil
	resp, err := hx.operator.ListOAuthProviders(context.Background(), &ListOAuthProvidersRequest{})
	if err != nil || resp.Body.Providers == nil || len(resp.Body.Providers) != 0 {
		t.Fatalf("resp=%+v err=%v; an empty list is a 200 with [], never null", resp, err)
	}
}

func startCtx(host string) context.Context {
	r := httptest.NewRequest(http.MethodPost, "/v1/auth/operator/oauth/login", nil)
	r.Host = host
	return context.WithValue(r.Context(), "http_request", r)
}

func TestInitiateOAuthLogin_UsableProviderStartsFlowFromResolvedConfig(t *testing.T) {
	hx := newCallbackHarness(t)
	req := &OAuthLoginRequest{}
	req.Body.Provider = models.OAuthProviderGoogle
	resp, err := hx.operator.InitiateOAuthLogin(startCtx("console.example"), req)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resp.Body.AuthURL, "redirect_uri=https%3A%2F%2Fconsole.example%2Fv1%2Fauth%2Foauth%2Fgoogle%2Fcallback") {
		t.Fatalf("authUrl must use the backend callback from the resolved config: %q", resp.Body.AuthURL)
	}
	if hx.resolver.calls != 1 {
		t.Fatalf("resolver consulted %d times, want exactly 1 (no check-then-reread)", hx.resolver.calls)
	}
	if !strings.Contains(resp.SetCookie, OAuthStateCookieName+"=") {
		t.Fatalf("state cookie missing: %q", resp.SetCookie)
	}
	if len(hx.state.stored) != 1 || hx.state.stored[0].RedirectURI != "https://console.example/auth/callback" || hx.state.stored[0].Tier != services.AudienceOperator {
		t.Fatalf("stored state = %+v", hx.state.stored)
	}
}

func TestInitiateOAuthLogin_PerProviderDefectIs403(t *testing.T) {
	hx := newCallbackHarness(t)
	hx.resolver.usable = false
	req := &OAuthLoginRequest{}
	req.Body.Provider = models.OAuthProviderApple
	_, err := hx.operator.InitiateOAuthLogin(startCtx("console.example"), req)
	if status, code := codeOf(t, err); status != http.StatusForbidden || code != errcode.AuthOAuthProviderDisabled {
		t.Fatalf("got %d %s", status, code)
	}
	if len(hx.state.stored) != 0 {
		t.Fatal("no state may be stored for a refused start")
	}
}

func TestInitiateOAuthLogin_DocumentLevelFailureIs503(t *testing.T) {
	hx := newCallbackHarness(t)
	hx.resolver.err = errors.New("mongo down")
	req := &OAuthLoginRequest{}
	req.Body.Provider = models.OAuthProviderGoogle
	_, err := hx.operator.InitiateOAuthLogin(startCtx("console.example"), req)
	if status, code := codeOf(t, err); status != http.StatusServiceUnavailable || code != errcode.AuthPolicyUnavailable {
		t.Fatalf("got %d %s", status, code)
	}
}

func TestInitiateOAuthLink_UsesStrictResolverAndSPAURL(t *testing.T) {
	hx := newCallbackHarness(t)
	r := httptest.NewRequest(http.MethodPost, "/v1/auth/operator/me/oauth/link/github", nil)
	r.Host = "console.example"
	r.Header.Set("Origin", "https://evil.example")
	ctx := context.WithValue(r.Context(), "http_request", r)
	ctx = context.WithValue(ctx, ctxauth.KeyUserUUID, "u-1") // what AuthMiddleware stamps; InitiateOAuthLink reads it via ctxauth.GetUserUUID
	_, err := hx.operator.InitiateOAuthLink(ctx, &OAuthLinkRequest{Provider: "github"})
	if err != nil {
		t.Fatal(err)
	}
	if len(hx.state.stored) != 1 || hx.state.stored[0].RedirectURI != "https://console.example/user/security" || hx.state.stored[0].Mode != services.OAuthStateModeLink {
		t.Fatalf("stored = %+v", hx.state.stored)
	}
	hx.resolver.usable = false
	_, err = hx.operator.InitiateOAuthLink(ctx, &OAuthLinkRequest{Provider: "github"})
	if status, code := codeOf(t, err); status != http.StatusForbidden || code != errcode.AuthOAuthProviderDisabled {
		t.Fatalf("got %d %s", status, code)
	}
}
