package handlers

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/services"
	"github.com/orkestra/backend/internal/shared/config"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// --- fakes: each embeds the interface so an unexpected call panics loudly ---

type fakeStateService struct {
	services.OAuthStateService
	info      *services.OAuthStateInfo
	err       error
	validated int
	stored    []*services.StoreOAuthStateRequest
	// relay: the last stored record, handed out exactly once by TakeOAuthRelay.
	relay      *services.OAuthRelayRecord
	relayTaken bool
	relayErr   error
}

func (f *fakeStateService) ValidateOAuthState(context.Context, string) (*services.OAuthStateInfo, error) {
	f.validated++
	if f.err != nil {
		return nil, f.err
	}
	return f.info, nil
}

func (f *fakeStateService) StoreOAuthState(_ context.Context, req *services.StoreOAuthStateRequest) (*services.OAuthStateInfo, error) {
	f.stored = append(f.stored, req)
	return &services.OAuthStateInfo{State: req.State, Tier: req.Tier, Provider: req.Provider, RedirectURI: req.RedirectURI}, nil
}

func (f *fakeStateService) StoreOAuthRelay(_ context.Context, rec *services.OAuthRelayRecord) (string, error) {
	if f.relayErr != nil {
		return "", f.relayErr
	}
	f.relay = rec
	f.relayTaken = false
	return "relay-1", nil
}

func (f *fakeStateService) TakeOAuthRelay(_ context.Context, id string) (*services.OAuthRelayRecord, error) {
	if id != "relay-1" || f.relay == nil || f.relayTaken {
		return nil, errors.New("oauth relay not found, expired or already used")
	}
	f.relayTaken = true
	return f.relay, nil
}

type fakeResolver struct {
	cfg    *services.OAuthProviderConfig
	usable bool
	err    error
	list   []models.OAuthProvider
	calls  int
}

func (f *fakeResolver) Get(context.Context, models.OAuthProvider) (*services.OAuthProviderConfig, bool) {
	return f.cfg, f.cfg != nil
}
func (f *fakeResolver) RedirectURL(context.Context, models.OAuthProvider) string {
	if f.cfg == nil {
		return ""
	}
	return f.cfg.AdditionalConfig["redirect_url"]
}
func (f *fakeResolver) MobileAudience(context.Context, models.OAuthProvider, string) string {
	return ""
}
func (f *fakeResolver) ConfiguredProviders(context.Context) []models.OAuthProvider { return f.list }
func (f *fakeResolver) OAuthWebProviderUsable(context.Context, services.PolicyAudience, models.OAuthProvider) (*services.OAuthProviderConfig, bool, error) {
	f.calls++
	if f.err != nil {
		return nil, false, f.err
	}
	if !f.usable {
		return nil, false, nil
	}
	return f.cfg, true, nil
}
func (f *fakeResolver) UsableWebProviders(context.Context, services.PolicyAudience) ([]models.OAuthProvider, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.list, nil
}

type fakeProvider struct {
	services.OAuthProviderInterface
	token       *services.TokenResponse
	exchangeErr error
	info        *services.UserInfo
	infoErr     error
	exchanges   int
}

func (p *fakeProvider) ExchangeCodeForToken(context.Context, *services.CodeExchangeRequest) (*services.TokenResponse, error) {
	p.exchanges++
	return p.token, p.exchangeErr
}
func (p *fakeProvider) GetUserInfo(context.Context, string) (*services.UserInfo, error) {
	return p.info, p.infoErr
}
func (p *fakeProvider) ValidateIDToken(context.Context, *services.IDTokenValidationRequest) (*services.UserInfo, error) {
	return p.info, p.infoErr
}
func (p *fakeProvider) GetClientID() string { return "client-id" }
func (p *fakeProvider) GetAuthURL(state, _, redirect string) string {
	return "https://idp.example/authorize?state=" + url.QueryEscape(state) + "&redirect_uri=" + url.QueryEscape(redirect)
}

type fakeFactory struct {
	prov services.OAuthProviderInterface
	err  error
}

func (f fakeFactory) CreateProvider(models.OAuthProvider, *services.OAuthProviderConfig) (services.OAuthProviderInterface, error) {
	return f.prov, f.err
}
func (f fakeFactory) GetSupportedProviders() []models.OAuthProvider { return nil }

type fakeAuthService struct {
	services.AuthService
	resp      *models.TokenResponse
	err       error
	calls     int
	lastInfo  map[string]interface{}
	linkErr   error
	linkCalls int
}

func (f *fakeAuthService) HandleOAuthCallbackWithLinking(_ context.Context, _ models.OAuthProvider, info map[string]interface{}, _ *models.OAuthProviderTokens, _ *models.SecurityContext, _ *models.DeviceInfo) (*models.TokenResponse, error) {
	f.calls++
	f.lastInfo = info
	return f.resp, f.err
}
func (f *fakeAuthService) SelfLinkOAuthFromCallback(context.Context, string, iface.OAuthProvider, map[string]interface{}, *models.OAuthProviderTokens) error {
	f.linkCalls++
	return f.linkErr
}

type fakeJWT struct {
	services.JWTService
	ttl time.Duration
}

func (f fakeJWT) RefreshTokenTTL() time.Duration { return f.ttl }

// --- harness ---

type callbackHarness struct {
	dispatcher *AuthHandler // operator-mux instance that owns the callback routes
	operator   *AuthHandler
	client     *AuthHandler
	state      *fakeStateService
	resolver   *fakeResolver
	provider   *fakeProvider
	opAuth     *fakeAuthService
	clAuth     *fakeAuthService
	secret     []byte
}

const (
	callbackHost  = "console.example"
	clientAPIHost = "api.example"
)

func newCallbackHarness(t *testing.T) *callbackHarness {
	t.Helper()
	cfg := &config.Config{}
	cfg.Server.FrontendURL = "https://legacy.example"
	cfg.Server.Client.Host = clientAPIHost
	cfg.Server.Client.PublicURL = "https://" + clientAPIHost
	cfg.Auth.Cookie.Name = "orkestra_cookie"
	cfg.Auth.Cookie.Secure = true

	provider := &fakeProvider{
		token: &services.TokenResponse{AccessToken: "idp-at", RefreshToken: "idp-rt", TokenType: "Bearer", ExpiresIn: 3600, IDToken: "idp-idt", Scope: []string{"email"}},
		info:  &services.UserInfo{ProviderID: "g-1", Email: "u@example.com", EmailVerified: true, Name: "U", Picture: "https://p"},
	}
	resolver := &fakeResolver{
		cfg:    &services.OAuthProviderConfig{ClientID: "cid", ClientSecret: "csecret", AdditionalConfig: map[string]string{"redirect_url": "https://console.example/v1/auth/oauth/google/callback"}},
		usable: true,
	}
	state := &fakeStateService{info: &services.OAuthStateInfo{Provider: models.OAuthProviderGoogle}}
	mkAuth := func() *fakeAuthService {
		return &fakeAuthService{resp: &models.TokenResponse{
			AccessToken: "orkestra-at", RefreshToken: "orkestra-rt", TokenType: "Bearer", ExpiresIn: 900,
			User: &iface.UserManagementResponse{ID: "u-1", Email: "u@example.com"},
		}}
	}
	opAuth, clAuth := mkAuth(), mkAuth()
	secret := []byte("0123456789abcdef0123456789abcdef")

	mk := func(tier, spa, cookieDomain string, svc services.AuthService, ttl time.Duration) *AuthHandler {
		h := NewAuthHandler(svc, fakeFactory{prov: provider}, resolver, state, nil, fakeJWT{ttl: ttl}, cfg, cookieDomain)
		h.SetTier(tier)
		h.SetStateSecret(secret)
		h.SetSPAURL(spa)
		return h
	}
	// Distinct refresh TTLs per tier so a test can prove the cookie's
	// Max-Age comes from the TARGET tier's JWT service.
	operator := mk(services.AudienceOperator, "https://console.example", "console.example", opAuth, 7*24*time.Hour)
	client := mk(services.AudienceClient, "https://app.example", clientAPIHost, clAuth, 3*24*time.Hour)
	operator.SetTierDispatch(map[string]*AuthHandler{services.AudienceOperator: operator, services.AudienceClient: client})
	return &callbackHarness{dispatcher: operator, operator: operator, client: client, state: state, resolver: resolver, provider: provider, opAuth: opAuth, clAuth: clAuth, secret: secret}
}

type callbackOpts struct {
	tier      string
	mode      string // "" or services.OAuthStateModeLink
	linkUUID  string
	startHost string // defaults to callbackHost (same host); use clientAPIHost for a client-tier flow
	query     string // extra query, e.g. "&error=access_denied"
	noCode    bool
	noState   bool
	badState  bool
	form      bool // Apple form-post
	path      string
	cookie    bool // present the state cookie on THIS request
	provider  models.OAuthProvider
}

const testCSRF = "nonce-1"

func (hx *callbackHarness) request(t *testing.T, o callbackOpts) *http.Request {
	t.Helper()
	startHost := o.startHost
	if startHost == "" {
		startHost = callbackHost
	}
	var signed string
	var err error
	if o.mode == services.OAuthStateModeLink {
		signed, err = services.SignOAuthLinkStateToken(hx.secret, o.tier, testCSRF, o.linkUUID, startHost, 10*time.Minute)
	} else {
		signed, err = services.SignOAuthStateToken(hx.secret, o.tier, testCSRF, startHost, 10*time.Minute)
	}
	if err != nil {
		t.Fatal(err)
	}
	if o.badState {
		signed += "tampered"
	}
	hx.state.info.Tier = o.tier
	hx.state.info.Mode = o.mode
	hx.state.info.LinkUserUUID = o.linkUUID
	if o.provider != "" {
		hx.state.info.Provider = o.provider
	}

	values := url.Values{}
	if !o.noState {
		values.Set("state", signed)
	}
	if !o.noCode {
		values.Set("code", "abc")
	}
	extra, _ := url.ParseQuery(strings.TrimPrefix(o.query, "&"))
	for k, vs := range extra {
		values[k] = vs
	}
	path := o.path
	if path == "" {
		path = "/v1/auth/oauth/google/callback"
	}
	var r *http.Request
	if o.form {
		r = httptest.NewRequest(http.MethodPost, path, strings.NewReader(values.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		r = httptest.NewRequest(http.MethodGet, path+"?"+values.Encode(), nil)
	}
	r.Host = callbackHost
	if o.cookie {
		r.AddCookie(&http.Cookie{Name: OAuthStateCookieName, Value: testCSRF})
	}
	// setupMiddleware stashes the raw request for resolveStateForCallback.
	return r.WithContext(context.WithValue(r.Context(), "http_request", r))
}

// relayRequest is the browser arriving at the client API host's relay
// endpoint after the operator-host redirect.
func relayRequest(id, cookie string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/v1/auth/client/oauth/complete?relay="+url.QueryEscape(id), nil)
	r.Host = clientAPIHost
	if cookie != "" {
		r.AddCookie(&http.Cookie{Name: OAuthStateCookieName, Value: cookie})
	}
	return r.WithContext(context.WithValue(r.Context(), "http_request", r))
}

func location(t *testing.T, rec *httptest.ResponseRecorder) (string, url.Values, url.Values) {
	t.Helper()
	loc := rec.Header().Get("Location")
	if loc == "" {
		t.Fatalf("no Location; status=%d body=%q", rec.Code, rec.Body.String())
	}
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatal(err)
	}
	frag, _ := url.ParseQuery(u.Fragment)
	return u.Scheme + "://" + u.Host + u.Path, u.Query(), frag
}

// assertNoPII checks the EXACT parameter names of the query and the
// fragment against the forbidden set, and every value against the marker
// strings the harness uses — a substring match on "email" would trip on
// the allowlisted auth.oauth_email_unverified code.
func assertNoPII(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	loc := rec.Header().Get("Location")
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("parse Location %q: %v", loc, err)
	}
	frag, _ := url.ParseQuery(u.Fragment)
	forbidden := map[string]bool{"access_token": true, "refresh_token": true, "email": true, "user_id": true}
	markers := []string{"u@example.com", "u-1", "orkestra-rt", "orkestra-at", "idp-at", "idp-rt", "idp-idt", "csecret", "abc"}
	for part, vals := range map[string]url.Values{"query": u.Query(), "fragment": frag} {
		for key, vs := range vals {
			if forbidden[key] {
				t.Errorf("Location %s carries forbidden parameter %q", part, key)
			}
			for _, v := range vs {
				for _, m := range markers {
					if v == m || strings.Contains(v, "@") {
						t.Errorf("Location %s parameter %q carries marker/PII value %q", part, key, v)
					}
				}
			}
		}
	}
	for _, m := range []string{"u@example.com", "orkestra-rt", "orkestra-at", "idp-at", "idp-rt"} {
		if strings.Contains(u.Path, m) {
			t.Errorf("Location path carries %q", m)
		}
	}
}

func assertCallbackHeaders(t *testing.T, rec *httptest.ResponseRecorder, expectStateCookieCleared bool) {
	t.Helper()
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 (body %q)", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Referrer-Policy") != "no-referrer" || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("headers = %v", rec.Header())
	}
	cleared := false
	for _, c := range rec.Result().Cookies() {
		if c.Name == OAuthStateCookieName && c.MaxAge < 0 {
			cleared = true
		}
	}
	if cleared != expectStateCookieCleared {
		t.Fatalf("state cookie cleared = %v, want %v", cleared, expectStateCookieCleared)
	}
}

func refreshCookie(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == "orkestra_cookie" {
			return c
		}
	}
	return nil
}

// assertRejected is the shape of every terminal 400 in the callback/relay
// flow: no redirect, no cookie, and the two security headers — the request
// URL of such a response carries state, code or relay.
func assertRejected(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %q)", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "" {
		t.Fatalf("no redirect may be issued: %q", loc)
	}
	if rec.Header().Get("Referrer-Policy") != "no-referrer" || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("a terminal 400 must carry the security headers: %v", rec.Header())
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == "orkestra_cookie" {
			t.Fatalf("no refresh cookie may be written on a rejection: %+v", c)
		}
	}
}

func assertNoRefreshCookie(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if c := refreshCookie(rec); c != nil {
		t.Fatalf("no refresh cookie may be written here: %+v", c)
	}
}

// --- operator tier: inline completion ---

func TestCallback_OperatorTierCompletesInline(t *testing.T) {
	hx := newCallbackHarness(t)
	rec := httptest.NewRecorder()
	hx.dispatcher.HandleGoogleCallbackHTTP(rec, hx.request(t, callbackOpts{tier: services.AudienceOperator, cookie: true}))
	assertCallbackHeaders(t, rec, true)
	base, q, frag := location(t, rec)
	if base != "https://console.example/auth/callback" {
		t.Fatalf("operator flow must land on OPERATOR_FRONTEND_URL: %q", base)
	}
	if len(q) != 2 || q.Get("success") != "true" || q.Get("provider") != "google" || len(frag) != 0 {
		t.Fatalf("q=%v frag=%v", q, frag)
	}
	c := refreshCookie(rec)
	if c == nil || c.Value != "orkestra-rt" || c.Domain != "console.example" || !c.HttpOnly || !c.Secure {
		t.Fatalf("refresh cookie = %+v", c)
	}
	if c.MaxAge != int((7 * 24 * time.Hour).Seconds()) {
		t.Fatalf("Max-Age = %d, want the OPERATOR tier's refresh TTL", c.MaxAge)
	}
	if hx.opAuth.calls != 1 || hx.clAuth.calls != 0 {
		t.Fatalf("operator=%d client=%d", hx.opAuth.calls, hx.clAuth.calls)
	}
	if hx.opAuth.lastInfo["email_verified"] != true || hx.opAuth.lastInfo["provider_id"] != "g-1" {
		t.Fatalf("userinfo map = %v", hx.opAuth.lastInfo)
	}
	assertNoPII(t, rec)
}

// --- client tier: relay ---

func TestCallback_ClientTierDefersToRelay_NoCookieNoTokenHere(t *testing.T) {
	hx := newCallbackHarness(t)
	rec := httptest.NewRecorder()
	// The browser carries no state cookie on console.* — it lives on api.*.
	hx.dispatcher.HandleGoogleCallbackHTTP(rec, hx.request(t, callbackOpts{tier: services.AudienceClient, startHost: clientAPIHost}))
	assertCallbackHeaders(t, rec, false) // the cookie is not here, so it is not cleared here
	if got := rec.Header().Get("Location"); got != "https://api.example/v1/auth/client/oauth/complete?relay=relay-1" {
		t.Fatalf("Location = %q", got)
	}
	assertNoRefreshCookie(t, rec)
	if hx.clAuth.calls != 0 || hx.opAuth.calls != 0 {
		t.Fatal("the operator host must not run the application half of a client-tier flow")
	}
	rec2 := hx.state.relay
	if rec2 == nil || rec2.Tier != services.AudienceClient || rec2.Provider != models.OAuthProviderGoogle || rec2.CSRF != testCSRF || rec2.FailureCode != "" ||
		rec2.UserInfo["provider_id"] != "g-1" || rec2.UserInfo["email_verified"] != true || rec2.Tokens == nil || rec2.Tokens.AccessToken != "idp-at" {
		t.Fatalf("relay record = %+v", rec2)
	}
	assertNoPII(t, rec)
}

func TestCallback_ClientTierRelaysWhateverCookieConsoleHolds(t *testing.T) {
	// P1 of review round 2: the same browser may hold the nonce of an
	// unrelated operator flow on console.*; it must neither block nor bind
	// a client-tier login. And a matching cookie on console is still no
	// reason to complete here — the api.* cookie is set on api.*.
	for name, cookie := range map[string]string{"foreign operator nonce": "some-operator-flows-nonce", "matching nonce": testCSRF} {
		t.Run(name, func(t *testing.T) {
			hx := newCallbackHarness(t)
			r := hx.request(t, callbackOpts{tier: services.AudienceClient, startHost: clientAPIHost})
			r.AddCookie(&http.Cookie{Name: OAuthStateCookieName, Value: cookie})
			rec := httptest.NewRecorder()
			hx.dispatcher.HandleGoogleCallbackHTTP(rec, r)
			assertCallbackHeaders(t, rec, false)
			if !strings.HasPrefix(rec.Header().Get("Location"), "https://api.example/v1/auth/client/oauth/complete?relay=") {
				t.Fatalf("a client-tier login always relays: %q", rec.Header().Get("Location"))
			}
			assertNoRefreshCookie(t, rec)
			if hx.clAuth.calls != 0 {
				t.Fatal("no application half on the operator host")
			}
		})
	}
}

func TestCallback_OperatorSameHostForeignNonce_400(t *testing.T) {
	hx := newCallbackHarness(t)
	r := hx.request(t, callbackOpts{tier: services.AudienceOperator})
	r.AddCookie(&http.Cookie{Name: OAuthStateCookieName, Value: "attackers-nonce"})
	rec := httptest.NewRecorder()
	hx.dispatcher.HandleGoogleCallbackHTTP(rec, r)
	assertRejected(t, rec)
	if hx.state.validated != 0 || hx.provider.exchanges != 0 {
		t.Fatal("a same-host foreign nonce is rejected before the state is consumed")
	}
}

func TestCallback_ClientTierWithoutClientSurfaceIsRejected(t *testing.T) {
	hx := newCallbackHarness(t)
	hx.dispatcher.config.Server.Client.PublicURL = ""
	rec := httptest.NewRecorder()
	hx.dispatcher.HandleGoogleCallbackHTTP(rec, hx.request(t, callbackOpts{tier: services.AudienceClient, startHost: clientAPIHost}))
	assertRejected(t, rec) // no destination to trust → no redirect at all
	if hx.provider.exchanges != 0 || hx.state.relay != nil {
		t.Fatal("nothing is exchanged or stored when the relay has no destination")
	}
}

func TestCallback_ClientTierRelayStoreFailureIsRejected(t *testing.T) {
	// The second exception to "every client outcome goes through the relay":
	// the record cannot be stored. Safe — no token, no cookie, state spent —
	// and a terminal 400 here; the api.* cookie expires on its own Max-Age.
	hx := newCallbackHarness(t)
	hx.state.relayErr = errors.New("redis down")
	rec := httptest.NewRecorder()
	hx.dispatcher.HandleGoogleCallbackHTTP(rec, hx.request(t, callbackOpts{tier: services.AudienceClient, startHost: clientAPIHost}))
	assertRejected(t, rec)
	if hx.state.validated != 1 || hx.provider.exchanges != 1 || hx.clAuth.calls != 0 || hx.opAuth.calls != 0 {
		t.Fatalf("validated=%d exchanges=%d client=%d operator=%d", hx.state.validated, hx.provider.exchanges, hx.clAuth.calls, hx.opAuth.calls)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == OAuthStateCookieName {
			t.Fatal("the operator host must not pretend to clear a cookie that lives on api.*")
		}
	}
}

func TestCallback_ClientTierFailuresAreRelayed(t *testing.T) {
	// Every terminal outcome of a valid client-tier state goes through the
	// relay, so the deferred binding is verified and the start-host cookie
	// cleared before the browser reaches the client SPA — even for a
	// failure decided on the operator host.
	cases := map[string]struct {
		arrange func(hx *callbackHarness)
		opts    callbackOpts
		want    string
	}{
		"IdP denial":        {func(*callbackHarness) {}, callbackOpts{noCode: true, query: "&error=access_denied"}, OAuthCallbackErrAccessDenied},
		"missing code":      {func(*callbackHarness) {}, callbackOpts{noCode: true}, OAuthCallbackErrLoginFailed},
		"provider unusable": {func(hx *callbackHarness) { hx.resolver.usable = false }, callbackOpts{}, OAuthCallbackErrProviderUnavailable},
		"exchange failed":   {func(hx *callbackHarness) { hx.provider.exchangeErr = errors.New("idp 500 u@example.com") }, callbackOpts{}, OAuthCallbackErrProviderUnavailable},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			hx := newCallbackHarness(t)
			tc.arrange(hx)
			o := tc.opts
			o.tier, o.startHost = services.AudienceClient, clientAPIHost
			rec := httptest.NewRecorder()
			hx.dispatcher.HandleGoogleCallbackHTTP(rec, hx.request(t, o))
			assertCallbackHeaders(t, rec, false)
			if got := rec.Header().Get("Location"); got != "https://api.example/v1/auth/client/oauth/complete?relay=relay-1" {
				t.Fatalf("a client-tier failure must be relayed, not sent to the SPA from here: %q", got)
			}
			if hx.state.relay == nil || hx.state.relay.FailureCode != tc.want || hx.state.relay.CSRF != testCSRF {
				t.Fatalf("relay record = %+v, want FailureCode=%s", hx.state.relay, tc.want)
			}
			assertNoPII(t, rec)

			// The relay endpoint binds, clears the start-host cookie, then
			// renders the recorded failure; no application half, no token.
			relay := httptest.NewRecorder()
			hx.client.HandleOAuthRelayCompleteHTTP(relay, relayRequest("relay-1", testCSRF))
			assertCallbackHeaders(t, relay, true)
			base, q, _ := location(t, relay)
			if base != "https://app.example/auth/callback" || q.Get("success") != "false" || q.Get("error") != tc.want {
				t.Fatalf("base=%q q=%v", base, q)
			}
			assertNoRefreshCookie(t, relay)
			if hx.clAuth.calls != 0 {
				t.Fatal("a relayed failure never runs the application half")
			}
			// …and without the cookie the failure is not even rendered.
			hx2 := newCallbackHarness(t)
			tc.arrange(hx2)
			hx2.dispatcher.HandleGoogleCallbackHTTP(httptest.NewRecorder(), hx2.request(t, o))
			unbound := httptest.NewRecorder()
			hx2.client.HandleOAuthRelayCompleteHTTP(unbound, relayRequest("relay-1", ""))
			assertRejected(t, unbound)
		})
	}
}

func TestRelayComplete_BindsCookieAndSetsClientCookie(t *testing.T) {
	hx := newCallbackHarness(t)
	hx.dispatcher.HandleGoogleCallbackHTTP(httptest.NewRecorder(), hx.request(t, callbackOpts{tier: services.AudienceClient, startHost: clientAPIHost}))

	rec := httptest.NewRecorder()
	hx.client.HandleOAuthRelayCompleteHTTP(rec, relayRequest("relay-1", testCSRF))
	assertCallbackHeaders(t, rec, true) // the cookie lives here → cleared here
	base, q, frag := location(t, rec)
	if base != "https://app.example/auth/callback" || q.Get("success") != "true" || q.Get("provider") != "google" || len(frag) != 0 {
		t.Fatalf("base=%q q=%v frag=%v", base, q, frag)
	}
	c := refreshCookie(rec)
	if c == nil || c.Value != "orkestra-rt" || c.Domain != clientAPIHost || !c.HttpOnly || !c.Secure {
		t.Fatalf("refresh cookie = %+v; must be set by the CLIENT host with the client domain", c)
	}
	if c.MaxAge != int((3 * 24 * time.Hour).Seconds()) {
		t.Fatalf("Max-Age = %d, want the CLIENT tier's refresh TTL", c.MaxAge)
	}
	if hx.clAuth.calls != 1 || hx.opAuth.calls != 0 {
		t.Fatalf("client=%d operator=%d", hx.clAuth.calls, hx.opAuth.calls)
	}
	if hx.clAuth.lastInfo["provider_id"] != "g-1" || hx.clAuth.lastInfo["email_verified"] != true {
		t.Fatalf("relayed userinfo = %v", hx.clAuth.lastInfo)
	}
	assertNoPII(t, rec)
}

func TestRelayComplete_RefusalsAre400WithoutRedirectOrToken(t *testing.T) {
	cases := map[string]func(hx *callbackHarness) *http.Request{
		"missing relay id":     func(hx *callbackHarness) *http.Request { return relayRequest("", testCSRF) },
		"unknown relay id":     func(hx *callbackHarness) *http.Request { return relayRequest("relay-9", testCSRF) },
		"no state cookie":      func(hx *callbackHarness) *http.Request { return relayRequest("relay-1", "") },
		"foreign nonce (CSRF)": func(hx *callbackHarness) *http.Request { return relayRequest("relay-1", "attackers-nonce") },
		"link-mode record": func(hx *callbackHarness) *http.Request {
			hx.state.relay.Mode = services.OAuthStateModeLink
			return relayRequest("relay-1", testCSRF)
		},
		"operator-tier record": func(hx *callbackHarness) *http.Request {
			hx.state.relay.Tier = services.AudienceOperator
			return relayRequest("relay-1", testCSRF)
		},
	}
	for name, arrange := range cases {
		t.Run(name, func(t *testing.T) {
			hx := newCallbackHarness(t)
			hx.dispatcher.HandleGoogleCallbackHTTP(httptest.NewRecorder(), hx.request(t, callbackOpts{tier: services.AudienceClient, startHost: clientAPIHost}))
			rec := httptest.NewRecorder()
			hx.client.HandleOAuthRelayCompleteHTTP(rec, arrange(hx))
			assertRejected(t, rec)
			if hx.clAuth.calls != 0 {
				t.Fatal("no token may be minted")
			}
		})
	}
}

func TestRelayComplete_IsOneShot(t *testing.T) {
	hx := newCallbackHarness(t)
	hx.dispatcher.HandleGoogleCallbackHTTP(httptest.NewRecorder(), hx.request(t, callbackOpts{tier: services.AudienceClient, startHost: clientAPIHost}))
	first := httptest.NewRecorder()
	hx.client.HandleOAuthRelayCompleteHTTP(first, relayRequest("relay-1", testCSRF))
	if first.Code != http.StatusFound {
		t.Fatalf("first: %d", first.Code)
	}
	second := httptest.NewRecorder()
	hx.client.HandleOAuthRelayCompleteHTTP(second, relayRequest("relay-1", testCSRF))
	assertRejected(t, second)
	if hx.clAuth.calls != 1 {
		t.Fatalf("application half ran %d times, want 1", hx.clAuth.calls)
	}
}

func TestRelayComplete_ApplicationErrorMapsToAllowlist(t *testing.T) {
	hx := newCallbackHarness(t)
	hx.clAuth.err = services.ErrOAuthEmailUnverified
	hx.dispatcher.HandleGoogleCallbackHTTP(httptest.NewRecorder(), hx.request(t, callbackOpts{tier: services.AudienceClient, startHost: clientAPIHost}))
	rec := httptest.NewRecorder()
	hx.client.HandleOAuthRelayCompleteHTTP(rec, relayRequest("relay-1", testCSRF))
	assertCallbackHeaders(t, rec, true)
	base, q, _ := location(t, rec)
	if base != "https://app.example/auth/callback" || q.Get("success") != "false" || q.Get("error") != OAuthCallbackErrEmailUnverified {
		t.Fatalf("base=%q q=%v", base, q)
	}
	assertNoRefreshCookie(t, rec)
	assertNoPII(t, rec)
}

// --- trust before destination ---

func TestCallback_TrustBeforeDestination(t *testing.T) {
	cases := map[string]callbackOpts{
		"missing state":                     {tier: services.AudienceClient, startHost: clientAPIHost, noState: true},
		"tampered state":                    {tier: services.AudienceClient, startHost: clientAPIHost, badState: true},
		"no browser binding on same host":   {tier: services.AudienceOperator, cookie: false},
		"IdP error with an invalid state":   {tier: services.AudienceClient, startHost: clientAPIHost, badState: true, query: "&error=access_denied"},
		"cross-host operator-tier state":    {tier: services.AudienceOperator, startHost: clientAPIHost, cookie: false},
		"cross-host link-mode state":        {tier: services.AudienceClient, mode: services.OAuthStateModeLink, linkUUID: "u-1", startHost: clientAPIHost, cookie: false},
		"state stored for another provider": {tier: services.AudienceOperator, cookie: true, provider: models.OAuthProviderDiscord},
	}
	for name, o := range cases {
		t.Run(name, func(t *testing.T) {
			hx := newCallbackHarness(t)
			rec := httptest.NewRecorder()
			hx.dispatcher.HandleGoogleCallbackHTTP(rec, hx.request(t, o))
			assertRejected(t, rec)
			if hx.provider.exchanges != 0 || hx.opAuth.calls != 0 || hx.clAuth.calls != 0 || hx.state.relay != nil {
				t.Fatal("nothing downstream may run")
			}
			for _, c := range rec.Result().Cookies() {
				if c.Name == OAuthStateCookieName {
					t.Fatal("an untrusted request must not touch the state cookie")
				}
			}
		})
	}
}

func TestCallback_ReplayedOrUnknownState_400(t *testing.T) {
	hx := newCallbackHarness(t)
	hx.state.err = errors.New("OAuth state not found, expired or already used")
	rec := httptest.NewRecorder()
	hx.dispatcher.HandleGoogleCallbackHTTP(rec, hx.request(t, callbackOpts{tier: services.AudienceOperator, cookie: true}))
	assertRejected(t, rec)
}

func TestCallback_ValidStateThenIdPDenial(t *testing.T) {
	hx := newCallbackHarness(t)
	rec := httptest.NewRecorder()
	hx.dispatcher.HandleGoogleCallbackHTTP(rec, hx.request(t, callbackOpts{tier: services.AudienceOperator, cookie: true, noCode: true, query: "&error=access_denied&error_description=The+user+u-1+%3Cu%40example.com%3E+said+no"}))
	assertCallbackHeaders(t, rec, true)
	base, q, _ := location(t, rec)
	if base != "https://console.example/auth/callback" || q.Get("success") != "false" || q.Get("error") != OAuthCallbackErrAccessDenied {
		t.Fatalf("base=%q q=%v", base, q)
	}
	assertNoPII(t, rec)
	if hx.resolver.calls != 0 || hx.provider.exchanges != 0 {
		t.Fatal("a denial ends the flow before the provider is resolved")
	}
	if hx.state.validated != 1 {
		t.Fatal("the state must be consumed before the IdP error is interpreted")
	}
}

func TestCallback_MissingCode_Generic(t *testing.T) {
	hx := newCallbackHarness(t)
	rec := httptest.NewRecorder()
	hx.dispatcher.HandleGoogleCallbackHTTP(rec, hx.request(t, callbackOpts{tier: services.AudienceOperator, cookie: true, noCode: true}))
	assertCallbackHeaders(t, rec, true)
	_, q, _ := location(t, rec)
	if q.Get("error") != OAuthCallbackErrLoginFailed {
		t.Fatalf("q=%v", q)
	}
}

func TestCallback_ProviderProblemsAreUnavailable(t *testing.T) {
	cases := map[string]func(hx *callbackHarness){
		"config document unreadable": func(hx *callbackHarness) { hx.resolver.err = errors.New("mongo down") },
		"provider disabled mid-flow": func(hx *callbackHarness) { hx.resolver.usable = false },
		"exchange failed":            func(hx *callbackHarness) { hx.provider.exchangeErr = errors.New("idp 500 for u@example.com") },
		"userinfo failed":            func(hx *callbackHarness) { hx.provider.infoErr = errors.New("idp userinfo 502") },
	}
	for name, arrange := range cases {
		t.Run(name, func(t *testing.T) {
			hx := newCallbackHarness(t)
			arrange(hx)
			rec := httptest.NewRecorder()
			hx.dispatcher.HandleGoogleCallbackHTTP(rec, hx.request(t, callbackOpts{tier: services.AudienceOperator, cookie: true}))
			assertCallbackHeaders(t, rec, true)
			_, q, _ := location(t, rec)
			if q.Get("error") != OAuthCallbackErrProviderUnavailable {
				t.Fatalf("q=%v", q)
			}
			assertNoPII(t, rec)
			if hx.opAuth.calls != 0 {
				t.Fatal("the application must not be consulted")
			}
		})
	}
}

func TestCallback_ApplicationErrorsMapToAllowlist(t *testing.T) {
	// The default logger is captured so the raw error text (which carries a
	// marker email and user id) is proven absent from logs as well as URLs.
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	cases := map[error]string{
		services.ErrOAuthSignupDisabled:                                OAuthCallbackErrSignupDisabled,
		services.ErrOAuthLinkDisabled:                                  OAuthCallbackErrLinkDisabled,
		services.ErrOAuthEmailUnverified:                               OAuthCallbackErrEmailUnverified,
		services.ErrAuthPolicyUnavailable:                              OAuthCallbackErrProviderUnavailable,
		services.ErrInvalidCredentials:                                 OAuthCallbackErrLoginFailed,
		errors.New("failed to create user u-1 u@example.com: dup key"): OAuthCallbackErrLoginFailed,
	}
	for err, want := range cases {
		t.Run(err.Error(), func(t *testing.T) {
			logs.Reset()
			hx := newCallbackHarness(t)
			hx.opAuth.err = err
			rec := httptest.NewRecorder()
			hx.dispatcher.HandleGoogleCallbackHTTP(rec, hx.request(t, callbackOpts{tier: services.AudienceOperator, cookie: true}))
			assertCallbackHeaders(t, rec, true)
			_, q, _ := location(t, rec)
			if q.Get("success") != "false" || q.Get("error") != want {
				t.Fatalf("q=%v want error=%s", q, want)
			}
			assertNoRefreshCookie(t, rec)
			assertNoPII(t, rec)
			if strings.Contains(logs.String(), "u@example.com") || strings.Contains(logs.String(), "dup key") {
				t.Fatalf("raw error text reached the logs: %s", logs.String())
			}
			if !strings.Contains(logs.String(), `"msg":"oauth callback failed"`) || !strings.Contains(logs.String(), `"outcome":"`) {
				t.Fatalf("sanitized log line with a stable outcome expected: %s", logs.String())
			}
		})
	}
}

func TestCallback_MFAPartial_FragmentOnly_NoCookie(t *testing.T) {
	hx := newCallbackHarness(t)
	hx.opAuth.resp = &models.TokenResponse{RequiresMFA: true, MFAToken: "challenge-1", WebAuthnAvailable: true, User: &iface.UserManagementResponse{ID: "u-1", Email: "u@example.com"}}
	rec := httptest.NewRecorder()
	hx.dispatcher.HandleGoogleCallbackHTTP(rec, hx.request(t, callbackOpts{tier: services.AudienceOperator, cookie: true}))
	assertCallbackHeaders(t, rec, true)
	base, q, frag := location(t, rec)
	if base != "https://console.example/auth/callback" || len(q) != 0 {
		t.Fatalf("base=%q q=%v (MFA carries no query)", base, q)
	}
	if frag.Get("requiresMfa") != "true" || frag.Get("mfaToken") != "challenge-1" || frag.Get("webauthnAvailable") != "true" || len(frag) != 3 {
		t.Fatalf("frag=%v", frag)
	}
	assertNoRefreshCookie(t, rec)
	assertNoPII(t, rec)
}

func TestCallback_LinkMode_OwnContract(t *testing.T) {
	hx := newCallbackHarness(t)
	rec := httptest.NewRecorder()
	hx.dispatcher.HandleGoogleCallbackHTTP(rec, hx.request(t, callbackOpts{tier: services.AudienceOperator, mode: services.OAuthStateModeLink, linkUUID: "u-1", cookie: true}))
	assertCallbackHeaders(t, rec, true)
	base, q, _ := location(t, rec)
	if base != "https://console.example/user/security" || q.Get("tab") != "oauth" || q.Get("link") != "success" || q.Get("provider") != "google" {
		t.Fatalf("base=%q q=%v", base, q)
	}
	if hx.opAuth.linkCalls != 1 || hx.opAuth.calls != 0 || refreshCookie(rec) != nil {
		t.Fatalf("link=%d login=%d cookie=%v", hx.opAuth.linkCalls, hx.opAuth.calls, refreshCookie(rec))
	}
	assertNoPII(t, rec)

	for err, code := range map[error]string{
		services.ErrOAuthLinkClaimedByOther:                oauthLinkCodeAlreadyLinked,
		services.ErrOAuthLinkAlreadyExists:                 oauthLinkCodeDuplicateProvider,
		services.ErrOAuthLinkInvalidUserInfo:               oauthLinkCodeInvalidUserInfo,
		errors.New("persist user link: u-1 u@example.com"): oauthLinkCodeInternal,
	} {
		hx := newCallbackHarness(t)
		hx.opAuth.linkErr = err
		rec := httptest.NewRecorder()
		hx.dispatcher.HandleGoogleCallbackHTTP(rec, hx.request(t, callbackOpts{tier: services.AudienceOperator, mode: services.OAuthStateModeLink, linkUUID: "u-1", cookie: true}))
		_, q, _ := location(t, rec)
		if q.Get("link") != "failed" || q.Get("code") != code {
			t.Fatalf("%v: q=%v want code=%s", err, q, code)
		}
		assertNoPII(t, rec)
	}

	hx = newCallbackHarness(t)
	rec = httptest.NewRecorder()
	hx.dispatcher.HandleGoogleCallbackHTTP(rec, hx.request(t, callbackOpts{tier: services.AudienceOperator, mode: services.OAuthStateModeLink, linkUUID: "u-1", cookie: true, noCode: true, query: "&error=access_denied"}))
	_, q, _ = location(t, rec)
	if q.Get("link") != "failed" || q.Get("code") != oauthLinkCodeAccessDenied {
		t.Fatalf("link-mode denial: q=%v", q)
	}
}

func TestCallback_AppleFormPost(t *testing.T) {
	hx := newCallbackHarness(t)
	rec := httptest.NewRecorder()
	hx.dispatcher.HandleAppleCallbackHTTP(rec, hx.request(t, callbackOpts{tier: services.AudienceOperator, form: true, path: "/v1/auth/oauth/apple/callback", cookie: true, provider: models.OAuthProviderApple}))
	assertCallbackHeaders(t, rec, true)
	base, q, _ := location(t, rec)
	if base != "https://console.example/auth/callback" || q.Get("provider") != "apple" || q.Get("success") != "true" {
		t.Fatalf("base=%q q=%v", base, q)
	}
	assertNoPII(t, rec)

	// No dev-only fallback: a missing state is a terminal 400 everywhere.
	rec = httptest.NewRecorder()
	hx.dispatcher.HandleAppleCallbackHTTP(rec, hx.request(t, callbackOpts{tier: services.AudienceOperator, form: true, path: "/v1/auth/oauth/apple/callback", noState: true, cookie: true, provider: models.OAuthProviderApple}))
	assertRejected(t, rec)
}

func TestCallback_GitHubSetsRefreshCookie(t *testing.T) {
	hx := newCallbackHarness(t)
	rec := httptest.NewRecorder()
	hx.dispatcher.HandleGitHubCallbackHTTP(rec, hx.request(t, callbackOpts{tier: services.AudienceOperator, path: "/v1/auth/oauth/github/callback", cookie: true, provider: models.OAuthProviderGitHub}))
	assertCallbackHeaders(t, rec, true)
	_, q, _ := location(t, rec)
	if q.Get("provider") != "github" || q.Get("success") != "true" {
		t.Fatalf("q=%v", q)
	}
	if c := refreshCookie(rec); c == nil || c.Value != "orkestra-rt" {
		t.Fatalf("GitHub must set the refresh cookie like every other provider: %+v", c)
	}
}

func TestCallback_DiscordAndLegacyTier(t *testing.T) {
	hx := newCallbackHarness(t)
	rec := httptest.NewRecorder()
	// tier "" (pre-cutover state) self-handles on the dispatcher, i.e. the operator SPA, inline.
	hx.dispatcher.HandleDiscordCallbackHTTP(rec, hx.request(t, callbackOpts{tier: "", path: "/v1/auth/oauth/discord/callback", cookie: true, provider: models.OAuthProviderDiscord}))
	assertCallbackHeaders(t, rec, true)
	base, q, _ := location(t, rec)
	if base != "https://console.example/auth/callback" || q.Get("provider") != "discord" {
		t.Fatalf("base=%q q=%v", base, q)
	}
}

func TestInitiateOAuthLogin_StoredRedirectURIComesFromSPAURLNotOrigin(t *testing.T) {
	hx := newCallbackHarness(t)
	r := httptest.NewRequest(http.MethodPost, "/v1/auth/client/oauth/login", nil)
	r.Host = clientAPIHost
	r.Header.Set("Origin", "https://evil.example")
	ctx := context.WithValue(r.Context(), "http_request", r)
	req := &OAuthLoginRequest{}
	req.Body.Provider = models.OAuthProviderGoogle
	if _, err := hx.client.InitiateOAuthLogin(ctx, req); err != nil {
		t.Fatal(err)
	}
	if len(hx.state.stored) != 1 || hx.state.stored[0].RedirectURI != "https://app.example/auth/callback" {
		t.Fatalf("stored = %+v; RedirectURI must come from the configured tier SPA, never from Origin", hx.state.stored)
	}
}
