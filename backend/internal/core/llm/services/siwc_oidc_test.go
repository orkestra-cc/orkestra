package services

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/orkestra/backend/internal/core/llm/providers"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

const testRedirect = "http://127.0.0.1:14555/v1/llm/accounts/openai/callback"

type oidcFixture struct {
	srv *httptest.Server

	mu   sync.Mutex
	keys map[string]*rsa.PrivateKey // kid → key published in the JWKS
	form url.Values                 // last token / revoke request form

	jwksHits atomic.Int32
	// tokenReply, when set, replaces the default token endpoint answer.
	tokenReply func(w http.ResponseWriter, form url.Values) bool
	revokeCode int
	noRevoke   bool
	issuer     string // discovery issuer override
	jwksURI    string // discovery jwks_uri override
	revokeURI  string // discovery revocation_endpoint override
	revokeHits atomic.Int32
}

func newOIDCFixture(t *testing.T) *oidcFixture {
	t.Helper()
	f := &oidcFixture{keys: map[string]*rsa.PrivateKey{"kid-1": testRSAKey(t)}, revokeCode: 200}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		f.mu.Lock()
		iss, noRevoke, jwksURI, revokeURI := f.issuer, f.noRevoke, f.jwksURI, f.revokeURI
		f.mu.Unlock()
		if iss == "" {
			iss = f.srv.URL
		}
		if jwksURI == "" {
			jwksURI = f.srv.URL + "/jwks"
		}
		if revokeURI == "" {
			revokeURI = f.srv.URL + "/revoke"
		}
		doc := map[string]any{"issuer": iss, "jwks_uri": jwksURI}
		if !noRevoke {
			doc["revocation_endpoint"] = revokeURI
		}
		_ = json.NewEncoder(w).Encode(doc)
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		f.jwksHits.Add(1)
		f.mu.Lock()
		defer f.mu.Unlock()
		var keys []map[string]any
		for kid, key := range f.keys {
			keys = append(keys, map[string]any{
				"kty": "RSA", "kid": kid, "use": "sig", "alg": "RS256",
				"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
				"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
			})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": keys})
	})
	mux.HandleFunc("/api/accounts/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.mu.Lock()
		f.form = r.PostForm
		reply := f.tokenReply
		f.mu.Unlock()
		if reply != nil && reply(w, r.PostForm) {
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "at-1", "refresh_token": "rt-2", "id_token": f.idToken(t, "kid-1", jwt.MapClaims{"aud": "oaiapp_1", "nonce": "nonce-1"}),
			"token_type": "Bearer", "expires_in": 3600, "scope": SIWCScope,
			"earliest_refresh_at": time.Now().Add(time.Minute).UTC().Format(time.RFC3339),
		})
	})
	mux.HandleFunc("/revoke", func(w http.ResponseWriter, r *http.Request) {
		f.revokeHits.Add(1)
		_ = r.ParseForm()
		f.mu.Lock()
		f.form = r.PostForm
		code := f.revokeCode
		f.mu.Unlock()
		w.WriteHeader(code)
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func testRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func (f *oidcFixture) lastForm() url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.form
}

// idToken signs a synthetic RS256 ID token with the fixture key kid; extra
// overrides the default claims.
func (f *oidcFixture) idToken(t *testing.T, kid string, extra jwt.MapClaims) string {
	t.Helper()
	f.mu.Lock()
	key := f.keys[kid]
	f.mu.Unlock()
	if key == nil {
		key = testRSAKey(t)
	}
	return signRS(t, jwt.SigningMethodRS256, key, kid, f.claims(extra))
}

func (f *oidcFixture) claims(extra jwt.MapClaims) jwt.MapClaims {
	claims := jwt.MapClaims{"iss": f.srv.URL, "aud": "oaiapp_1", "sub": "user-sub-1", "email": "u@example.test", "nonce": "nonce-1", "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix()}
	for k, v := range extra {
		claims[k] = v
	}
	return claims
}

func signRS(t *testing.T, m jwt.SigningMethod, key *rsa.PrivateKey, kid string, claims jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(m, claims)
	tok.Header["kid"] = kid
	s, err := tok.SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// newClientFor uses the module's bounded, redirect-refusing transport (not
// the httptest client) so the tests exercise the production client shape.
func newClientFor(f *oidcFixture) *OIDCClient {
	c := NewOIDCClient(providers.NewHTTPClient(providers.TransportConfig{}), slog.Default())
	c.Issuer = f.srv.URL
	c.DiscoveryEndpoint = f.srv.URL + "/.well-known/openid-configuration"
	c.AuthorizeEndpoint = f.srv.URL + "/api/accounts/authorize"
	c.TokenEndpoint = f.srv.URL + "/api/accounts/oauth/token"
	return c
}

func TestOIDC_ProductionEndpointsAreTheDocumentedOnes(t *testing.T) {
	c := NewOIDCClient(nil, nil)
	if c.Issuer != "https://auth.openai.com" || c.AuthorizeEndpoint != "https://auth.openai.com/api/accounts/authorize" ||
		c.TokenEndpoint != "https://auth.openai.com/api/accounts/oauth/token" || c.DiscoveryEndpoint != "https://auth.openai.com/.well-known/openid-configuration" {
		t.Fatalf("endpoints = %+v", c)
	}
	if SIWCScope != "openid profile email offline_access resource.invoke chatgpt.tokens.use.direct" || SIWCResource != "https://api.openai.com/v1" || SIWCDynamicClientID != "dynamic_agent_client" {
		t.Fatal("documented constants changed")
	}
}

func TestOIDC_AuthorizeURLCarriesEveryDocumentedParam(t *testing.T) {
	c := newClientFor(newOIDCFixture(t))
	u, _ := url.Parse(c.AuthorizeURL(AuthorizeParams{ClientID: SIWCDynamicClientID, RedirectURI: testRedirect, State: "s", Nonce: "n", CodeChallenge: "cc", HostID: "urn:uuid:h", AgentNameHint: SIWCAgentNameHint}))
	q := u.Query()
	for k, want := range map[string]string{"response_type": "code", "client_id": SIWCDynamicClientID, "redirect_uri": testRedirect, "scope": SIWCScope, "state": "s", "nonce": "n", "code_challenge": "cc", "code_challenge_method": "S256", "resource": SIWCResource, "ext_agent_host_id": "urn:uuid:h", "agent_name_hint": "Orkestra"} {
		if q.Get(k) != want {
			t.Errorf("%s = %q, want %q", k, q.Get(k), want)
		}
	}
	if q.Has("id_token_hint") || q.Has("login_hint") {
		t.Fatal("first registration must not send id_token_hint / login_hint")
	}
	reauth, _ := url.Parse(c.AuthorizeURL(AuthorizeParams{ClientID: "oaiapp_1", RedirectURI: testRedirect, State: "s", Nonce: "n", CodeChallenge: "cc", HostID: "h", IDTokenHint: "idt", LoginHint: "u@example.test"}))
	rq := reauth.Query()
	if rq.Get("id_token_hint") != "idt" || rq.Get("login_hint") != "u@example.test" || rq.Get("client_id") != "oaiapp_1" || rq.Has("agent_name_hint") {
		t.Fatalf("reauth query = %v", rq)
	}
}

func TestOIDC_ExchangeSendsTheDocumentedForm(t *testing.T) {
	f := newOIDCFixture(t)
	c := newClientFor(f)
	tok, err := c.Exchange(context.Background(), "oaiapp_1", "code-1", "verifier-1", testRedirect)
	if err != nil || tok.AccessToken != "at-1" || tok.RefreshToken != "rt-2" || tok.TokenType != "Bearer" || tok.ExpiresIn != 3600 || tok.Scope != SIWCScope || tok.EarliestRefreshAt == nil {
		t.Fatalf("exchange = %+v, %v", tok != nil, err)
	}
	form := f.lastForm()
	for k, want := range map[string]string{"grant_type": "authorization_code", "client_id": "oaiapp_1", "code": "code-1", "code_verifier": "verifier-1", "redirect_uri": testRedirect, "resource": SIWCResource} {
		if form.Get(k) != want {
			t.Errorf("form %s = %q, want %q", k, form.Get(k), want)
		}
	}
}

func TestOIDC_TokenResponseScopeAndEarliestRefresh(t *testing.T) {
	f := newOIDCFixture(t)
	c := newClientFor(f)
	at := time.Now().Add(2 * time.Minute).Unix()
	f.tokenReply = func(w http.ResponseWriter, _ url.Values) bool {
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "at-1", "refresh_token": "rt-2", "token_type": "Bearer", "expires_in": 3600, "earliest_refresh_at": at})
		return true
	}
	// 3.T4.4: an omitted scope on the code exchange is the requested one.
	tok, err := c.Exchange(context.Background(), "oaiapp_1", "c", "v", testRedirect)
	if err != nil || tok.Scope != SIWCScope || tok.EarliestRefreshAt == nil || tok.EarliestRefreshAt.Unix() != at {
		t.Fatalf("exchange scope/earliest wrong (err %v)", err)
	}
	// ... while a refresh reports it as omitted, so the caller keeps the stored scopes.
	ref, err := c.Refresh(context.Background(), "oaiapp_1", "rt-1")
	if err != nil || ref.Scope != "" {
		t.Fatalf("refresh scope = %q, %v", ref.Scope, err)
	}
}

func TestOIDC_ExchangeFailuresAreClassifiedWithoutProviderText(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   error
	}{
		{400, `{"error":"invalid_grant","error_description":"code at-secret expired"}`, ErrSIWCExchangeFailed},
		{400, `{"error":"invalid_request","error_description":"at-secret"}`, ErrSIWCExchangeFailed},
		{401, `{"error":"invalid_client"}`, ErrSIWCExchangeFailed},
		{403, `at-secret`, ErrSIWCExchangeFailed},
		{503, `{"error":"server_error","error_description":"at-secret"}`, iface.ErrLLMProviderUnavailable},
		{200, `{"access_token":""}`, ErrSIWCExchangeFailed},
		{200, `not json at-secret`, ErrSIWCExchangeFailed},
	}
	for _, tc := range cases {
		f := newOIDCFixture(t)
		f.tokenReply = func(w http.ResponseWriter, _ url.Values) bool {
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(tc.body))
			return true
		}
		_, err := newClientFor(f).Exchange(context.Background(), "oaiapp_1", "code-secret", "verifier-secret", testRedirect)
		if !errors.Is(err, tc.want) {
			t.Errorf("%d %s → %v, want %v", tc.status, tc.body, err, tc.want)
		}
		// 3.T4.2: every outcome carries an iface identity a consumer can map.
		if !errors.Is(err, iface.ErrLLMProviderUnavailable) {
			t.Errorf("%d → %v does not wrap ErrLLMProviderUnavailable", tc.status, err)
		}
		if tc.status >= 500 && errors.Is(err, ErrSIWCExchangeFailed) {
			t.Errorf("a 5xx is a provider outage, not a failed exchange: %v", err)
		}
		for _, secret := range []string{"at-secret", "code-secret", "verifier-secret", "expired"} {
			if strings.Contains(err.Error(), secret) {
				t.Errorf("error leaks %q: %v", secret, err)
			}
		}
	}
}

func TestOIDC_RefreshSendsTheDocumentedFormAndClassifies(t *testing.T) {
	f := newOIDCFixture(t)
	c := newClientFor(f)
	ctx := context.Background()
	ref, err := c.Refresh(ctx, "oaiapp_1", "rt-1")
	if err != nil || ref.RefreshToken != "rt-2" || ref.AccessToken != "at-1" {
		t.Fatalf("refresh = %v", err)
	}
	form := f.lastForm()
	for k, want := range map[string]string{"grant_type": "refresh_token", "client_id": "oaiapp_1", "refresh_token": "rt-1", "resource": SIWCResource} {
		if form.Get(k) != want {
			t.Errorf("form %s = %q, want %q", k, form.Get(k), want)
		}
	}
	if form.Has("scope") || form.Has("redirect_uri") {
		t.Errorf("refresh must omit scope (keeps the grant) and redirect_uri: %v", form)
	}

	cases := []struct {
		status int
		body   string
		want   error
	}{
		{400, `{"error":"invalid_grant","error_description":"rt-secret"}`, ErrSIWCRefreshTerminal},
		{400, `{"error":"refresh_token_reused"}`, ErrSIWCRefreshTerminal},
		{401, `{"error":"invalid_client"}`, ErrSIWCInvalidClient},
		{400, `{"error":{"code":"refresh_token_expired"}}`, ErrSIWCRefreshTerminal},
		{502, `rt-secret`, iface.ErrLLMProviderUnavailable},
		{429, `{"error":"rate_limited"}`, iface.ErrLLMProviderUnavailable},
	}
	for _, tc := range cases {
		f.tokenReply = func(w http.ResponseWriter, _ url.Values) bool {
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(tc.body))
			return true
		}
		_, err := c.Refresh(ctx, "oaiapp_1", "rt-secret")
		if !errors.Is(err, tc.want) || strings.Contains(err.Error(), "rt-secret") {
			t.Errorf("%d %s → %v, want %v", tc.status, tc.body, err, tc.want)
		}
		if tc.want != ErrSIWCRefreshTerminal && errors.Is(err, ErrSIWCRefreshTerminal) {
			t.Errorf("%d must not be terminal: %v", tc.status, err)
		}
	}
}

func TestOIDC_VerifyIDToken(t *testing.T) {
	f := newOIDCFixture(t)
	c := newClientFor(f)
	ctx := context.Background()
	good := f.idToken(t, "kid-1", nil)
	claims, err := c.VerifyIDToken(ctx, good, "oaiapp_1", "nonce-1")
	if err != nil || claims.Subject != "user-sub-1" || claims.Email != "u@example.test" || claims.Issuer != f.srv.URL || len(claims.Audience) != 1 || claims.Audience[0] != "oaiapp_1" || claims.ExpiresAt.IsZero() {
		t.Fatalf("verify = %+v, %v", claims, err)
	}
	other := testRSAKey(t)
	bad := map[string]struct {
		raw, aud, nonce string
	}{
		"wrong aud":      {good, "oaiapp_other", "nonce-1"},
		"empty aud":      {good, "", "nonce-1"},
		"wrong nonce":    {good, "oaiapp_1", "nonce-x"},
		"empty nonce":    {good, "oaiapp_1", ""},
		"garbage":        {"not.a.jwt", "oaiapp_1", "nonce-1"},
		"wrong issuer":   {f.idToken(t, "kid-1", jwt.MapClaims{"iss": "https://evil.example"}), "oaiapp_1", "nonce-1"},
		"expired":        {f.idToken(t, "kid-1", jwt.MapClaims{"exp": time.Now().Add(-time.Hour).Unix()}), "oaiapp_1", "nonce-1"},
		"no exp":         {signRS(t, jwt.SigningMethodRS256, f.keys["kid-1"], "kid-1", jwt.MapClaims{"iss": f.srv.URL, "aud": "oaiapp_1", "sub": "s", "nonce": "nonce-1"}), "oaiapp_1", "nonce-1"},
		"no sub":         {f.idToken(t, "kid-1", jwt.MapClaims{"sub": ""}), "oaiapp_1", "nonce-1"},
		"foreign key":    {signRS(t, jwt.SigningMethodRS256, other, "kid-1", f.claims(nil)), "oaiapp_1", "nonce-1"},
		"RS384 (3.T4.5)": {signRS(t, jwt.SigningMethodRS384, f.keys["kid-1"], "kid-1", f.claims(nil)), "oaiapp_1", "nonce-1"},
		"HS256 confusion": {func() string {
			tok := jwt.NewWithClaims(jwt.SigningMethodHS256, f.claims(nil))
			tok.Header["kid"] = "kid-1"
			s, _ := tok.SignedString([]byte("shared"))
			return s
		}(), "oaiapp_1", "nonce-1"},
		"alg none": {func() string {
			tok := jwt.NewWithClaims(jwt.SigningMethodNone, f.claims(nil))
			s, _ := tok.SignedString(jwt.UnsafeAllowNoneSignatureType)
			return s
		}(), "oaiapp_1", "nonce-1"},
	}
	for name, b := range bad {
		if _, err := c.VerifyIDToken(ctx, b.raw, b.aud, b.nonce); !errors.Is(err, ErrSIWCIDTokenInvalid) {
			t.Errorf("%s: err = %v", name, err)
		} else if strings.Contains(err.Error(), b.raw) {
			t.Errorf("%s: error echoes the token", name)
		}
	}
}

func TestOIDC_JWKSCacheRotationAndRefetchCap(t *testing.T) {
	f := newOIDCFixture(t)
	c := newClientFor(f)
	now := time.Now()
	c.now = func() time.Time { return now }
	ctx := context.Background()
	if _, err := c.VerifyIDToken(ctx, f.idToken(t, "kid-1", nil), "oaiapp_1", "nonce-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.VerifyIDToken(ctx, f.idToken(t, "kid-1", nil), "oaiapp_1", "nonce-1"); err != nil || f.jwksHits.Load() != 1 {
		t.Fatalf("cached verify: hits=%d err=%v", f.jwksHits.Load(), err)
	}
	// Key rotation: an unknown kid refetches once the 1/min cap allows it.
	f.mu.Lock()
	f.keys["kid-2"] = testRSAKey(t)
	f.mu.Unlock()
	rotated := f.idToken(t, "kid-2", nil)
	if _, err := c.VerifyIDToken(ctx, rotated, "oaiapp_1", "nonce-1"); !errors.Is(err, ErrSIWCIDTokenInvalid) || f.jwksHits.Load() != 1 {
		t.Fatalf("unknown kid inside the cap: hits=%d err=%v", f.jwksHits.Load(), err)
	}
	now = now.Add(61 * time.Second)
	if _, err := c.VerifyIDToken(ctx, rotated, "oaiapp_1", "nonce-1"); err != nil || f.jwksHits.Load() != 2 {
		t.Fatalf("rotated kid after the cap: hits=%d err=%v", f.jwksHits.Load(), err)
	}
	// A flood of unknown kids costs at most one fetch per minute.
	for i := range 5 {
		_, _ = c.VerifyIDToken(ctx, f.idToken(t, "kid-x"+string(rune('a'+i)), nil), "oaiapp_1", "nonce-1")
	}
	if f.jwksHits.Load() != 2 {
		t.Fatalf("unknown-kid flood fetched the JWKS %d times", f.jwksHits.Load())
	}
	// After the hour TTL the set is refetched even for a known kid.
	now = now.Add(time.Hour + time.Second)
	c.now = func() time.Time { return now }
	if _, err := c.VerifyIDToken(ctx, f.idToken(t, "kid-1", jwt.MapClaims{"exp": now.Add(time.Hour).Unix()}), "oaiapp_1", "nonce-1"); err != nil || f.jwksHits.Load() != 3 {
		t.Fatalf("stale set: hits=%d err=%v", f.jwksHits.Load(), err)
	}
}

func TestOIDC_DiscoveryIssuerMismatchIsAnOutage(t *testing.T) {
	f := newOIDCFixture(t)
	f.issuer = "https://evil.example"
	_, err := newClientFor(f).VerifyIDToken(context.Background(), f.idToken(t, "kid-1", nil), "oaiapp_1", "nonce-1")
	if !errors.Is(err, iface.ErrLLMProviderUnavailable) || errors.Is(err, ErrSIWCIDTokenInvalid) {
		t.Fatalf("err = %v", err)
	}
}

func TestOIDC_Revoke(t *testing.T) {
	f := newOIDCFixture(t)
	c := newClientFor(f)
	ctx := context.Background()
	if err := c.Revoke(ctx, "oaiapp_1", "rt-2"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	form := f.lastForm()
	for k, want := range map[string]string{"token": "rt-2", "token_type_hint": "refresh_token", "client_id": "oaiapp_1"} {
		if form.Get(k) != want {
			t.Errorf("form %s = %q, want %q", k, form.Get(k), want)
		}
	}
	f.revokeCode = 503
	if err := c.Revoke(ctx, "oaiapp_1", "rt-secret"); !errors.Is(err, iface.ErrLLMProviderUnavailable) || strings.Contains(err.Error(), "rt-secret") {
		t.Fatalf("revoke 503 = %v", err)
	}
	g := newOIDCFixture(t)
	g.noRevoke = true
	if err := newClientFor(g).Revoke(ctx, "oaiapp_1", "rt-2"); err != nil {
		t.Fatalf("no revocation endpoint must be a no-op, got %v", err)
	}
}

func TestOIDC_RedirectsAreNotFollowed(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("redirect was followed")
	}))
	t.Cleanup(target.Close)
	f := newOIDCFixture(t)
	f.tokenReply = func(w http.ResponseWriter, _ url.Values) bool {
		w.Header().Set("Location", target.URL)
		w.WriteHeader(http.StatusTemporaryRedirect)
		return true
	}
	if _, err := newClientFor(f).Exchange(context.Background(), "oaiapp_1", "c", "v", testRedirect); !errors.Is(err, iface.ErrLLMProviderUnavailable) {
		t.Fatalf("err = %v", err)
	}
}

func TestOIDC_TokenResponseIsBounded(t *testing.T) {
	f := newOIDCFixture(t)
	f.tokenReply = func(w http.ResponseWriter, _ url.Values) bool {
		_, _ = w.Write([]byte(`{"access_token":"` + strings.Repeat("a", 2<<20) + `"}`))
		return true
	}
	_, err := newClientFor(f).Exchange(context.Background(), "oaiapp_1", "c", "v", testRedirect)
	if !errors.Is(err, iface.ErrLLMProviderUnavailable) || errors.Is(err, ErrSIWCExchangeFailed) {
		t.Fatalf("oversized body must be a size refusal, not a failed exchange: %v", err)
	}
}

func TestParseEarliestRefreshClamps(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	num := func(v float64) json.RawMessage { b, _ := json.Marshal(v); return b }
	str := func(v string) json.RawMessage { b, _ := json.Marshal(v); return b }
	cases := []struct {
		name      string
		raw       json.RawMessage
		expiresIn int
		want      *time.Time
	}{
		{"rfc3339 in range", str(now.Add(time.Minute).UTC().Format(time.RFC3339)), 3600, ptrTime(now.Add(time.Minute))},
		{"unix seconds", num(float64(now.Add(10 * time.Minute).Unix())), 3600, ptrTime(now.Add(10 * time.Minute))},
		{"numeric string", str(strconv.FormatInt(now.Add(time.Minute).Unix(), 10)), 3600, ptrTime(now.Add(time.Minute))},
		{"small skew in the past", num(float64(now.Add(-time.Minute).Unix())), 3600, ptrTime(now.Add(-time.Minute))},
		{"far in the past", str(now.Add(-time.Hour).UTC().Format(time.RFC3339)), 3600, nil},
		{"beyond max(expires_in,1h)", num(float64(now.Add(2 * time.Hour).Unix())), 3600, nil},
		{"within a long expires_in", num(float64(now.Add(2 * time.Hour).Unix())), 7200, ptrTime(now.Add(2 * time.Hour))},
		{"beyond 24h whatever expires_in", num(float64(now.Add(25 * time.Hour).Unix())), 200_000, nil},
		{"millisecond epoch", num(float64(now.Add(time.Minute).UnixMilli())), 3600, nil},
		{"huge float", num(1e300), 3600, nil},
		{"negative", num(-1), 3600, nil},
		{"far-future date", str("9999-12-31T23:59:59Z"), 3600, nil},
		{"garbage", str("soon"), 3600, nil},
		{"null", json.RawMessage(`null`), 3600, nil},
		{"absent", nil, 3600, nil},
	}
	for _, c := range cases {
		got := parseEarliestRefresh(c.raw, c.expiresIn, now)
		switch {
		case c.want == nil && got != nil:
			t.Errorf("%s: got %v, want nil", c.name, *got)
		case c.want != nil && (got == nil || !got.Equal(*c.want)):
			t.Errorf("%s: got %v, want %v", c.name, got, *c.want)
		}
	}
}

func ptrTime(t time.Time) *time.Time { return &t }

func TestOIDC_VerifyIDTokenPassesCallerCancellationThrough(t *testing.T) {
	f := newOIDCFixture(t)
	c := newClientFor(f)
	raw := f.idToken(t, "kid-1", nil)

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.VerifyIDToken(canceled, raw, "oaiapp_1", "nonce-1")
	if !errors.Is(err, context.Canceled) || errors.Is(err, ErrSIWCIDTokenInvalid) {
		t.Fatalf("canceled err = %v", err)
	}
	expired, cancel2 := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel2()
	_, err = c.VerifyIDToken(expired, raw, "oaiapp_1", "nonce-1")
	if !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrSIWCIDTokenInvalid) {
		t.Fatalf("deadline err = %v", err)
	}
	// The interrupted fetches did not use up the 1/min refetch budget.
	if _, err := c.VerifyIDToken(context.Background(), raw, "oaiapp_1", "nonce-1"); err != nil || f.jwksHits.Load() != 1 {
		t.Fatalf("verify after interruptions: hits=%d err=%v", f.jwksHits.Load(), err)
	}
}

func TestOIDC_RefreshMalformedSuccessIsTransientNotAFailedExchange(t *testing.T) {
	f := newOIDCFixture(t)
	f.tokenReply = func(w http.ResponseWriter, _ url.Values) bool {
		_, _ = w.Write([]byte(`{"token_type":"Bearer"}`))
		return true
	}
	_, err := newClientFor(f).Refresh(context.Background(), "oaiapp_1", "rt-1")
	if !errors.Is(err, iface.ErrLLMProviderUnavailable) || errors.Is(err, ErrSIWCExchangeFailed) || errors.Is(err, ErrSIWCRefreshTerminal) {
		t.Fatalf("err = %v", err)
	}
}

func TestOIDC_DiscoveryRequiresHTTPSEndpointsUnderAnHTTPSIssuer(t *testing.T) {
	quiet := slog.New(slog.DiscardHandler)
	f := newOIDCFixture(t)
	f.issuer = "https://issuer.example"
	c := newClientFor(f) // jwks_uri stays the fixture's http URL
	c.Issuer, c.logger = "https://issuer.example", quiet
	_, err := c.VerifyIDToken(context.Background(), f.idToken(t, "kid-1", jwt.MapClaims{"iss": "https://issuer.example"}), "oaiapp_1", "nonce-1")
	if !errors.Is(err, iface.ErrLLMProviderUnavailable) || f.jwksHits.Load() != 0 {
		t.Fatalf("http jwks_uri under an https issuer: hits=%d err=%v", f.jwksHits.Load(), err)
	}

	g := newOIDCFixture(t)
	g.issuer, g.jwksURI = "https://issuer.example", "https://issuer.example/jwks" // revocation_endpoint stays http
	r := newClientFor(g)
	r.Issuer, r.logger = "https://issuer.example", quiet
	if err := r.Revoke(context.Background(), "oaiapp_1", "rt-2"); err != nil || g.revokeHits.Load() != 0 {
		t.Fatalf("http revocation_endpoint under an https issuer: hits=%d err=%v", g.revokeHits.Load(), err)
	}
}
