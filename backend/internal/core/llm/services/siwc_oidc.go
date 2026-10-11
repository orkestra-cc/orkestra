package services

import (
	"context"
	"crypto/rsa"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/orkestra/backend/internal/core/llm/providers"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// Documented Sign in with ChatGPT values (developers.openai.com/siwc, read
// 2026-10-10 and again 2026-10-11).
const (
	SIWCIssuer          = "https://auth.openai.com"
	SIWCAuthorizeURL    = SIWCIssuer + "/api/accounts/authorize"
	SIWCTokenURL        = SIWCIssuer + "/api/accounts/oauth/token"
	SIWCDiscoveryURL    = SIWCIssuer + "/.well-known/openid-configuration"
	SIWCResource        = "https://api.openai.com/v1"
	SIWCDynamicClientID = "dynamic_agent_client"
	SIWCAgentNameHint   = "Orkestra"
	SIWCScope           = "openid profile email offline_access resource.invoke chatgpt.tokens.use.direct"
)

const (
	oidcRequestTimeout = 15 * time.Second
	oidcMaxTokenBody   = 64 << 10 // token / error responses
	oidcMaxDocBody     = 256 << 10
	oidcMaxJWKSBody    = 1 << 20
	oidcCacheTTL       = time.Hour   // discovery + JWKS
	oidcRefetchCap     = time.Minute // at most one JWKS fetch per minute for unknown kids
	oidcMinRSABits     = 2048
)

// OIDCClient speaks the documented OpenAI OAuth/OIDC endpoints. The endpoint
// fields are exported so tests point them at a fixture; production never
// changes them. Nothing it returns or logs carries a token, a code, a
// verifier or provider free text: error bodies are read bounded and only an
// identifier-shaped error code survives.
type OIDCClient struct {
	Issuer            string
	AuthorizeEndpoint string
	TokenEndpoint     string
	DiscoveryEndpoint string

	http   *http.Client
	logger *slog.Logger
	now    func() time.Time

	fetchMu sync.Mutex // serialises discovery/JWKS fetches

	mu            sync.Mutex
	discovery     *discoveryDoc
	discoveryAt   time.Time
	jwks          map[string]*rsa.PublicKey
	jwksAt        time.Time // last successful fetch
	jwksAttemptAt time.Time // last fetch attempt (the refetch cap)
}

type discoveryDoc struct {
	Issuer             string `json:"issuer"`
	JWKSURI            string `json:"jwks_uri"`
	RevocationEndpoint string `json:"revocation_endpoint"`
}

// NewOIDCClient builds the client. client should be the module's SSRF-safe
// client (providers.NewHTTPClient: no redirects, no proxy, capped bodies);
// nil builds an equivalent one. Every request is additionally bounded by
// oidcRequestTimeout, because the module's shared clients have no total
// timeout (they also serve streams).
func NewOIDCClient(client *http.Client, logger *slog.Logger) *OIDCClient {
	if client == nil {
		client = providers.NewHTTPClient(providers.TransportConfig{ProductionLike: true, Timeout: oidcRequestTimeout, MaxResponseBytes: oidcMaxJWKSBody})
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &OIDCClient{
		Issuer: SIWCIssuer, AuthorizeEndpoint: SIWCAuthorizeURL, TokenEndpoint: SIWCTokenURL, DiscoveryEndpoint: SIWCDiscoveryURL,
		http: client, logger: logger, now: time.Now,
	}
}

// AuthorizeParams: AgentNameHint only on a first registration; IDTokenHint
// and LoginHint only on a reauth (the URL then carries the ID token: never
// log it).
type AuthorizeParams struct {
	ClientID, RedirectURI, State, Nonce, CodeChallenge, HostID string
	AgentNameHint                                              string
	IDTokenHint                                                string
	LoginHint                                                  string
}

func (c *OIDCClient) AuthorizeURL(p AuthorizeParams) string {
	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", p.ClientID)
	q.Set("redirect_uri", p.RedirectURI)
	q.Set("scope", SIWCScope)
	q.Set("state", p.State)
	q.Set("nonce", p.Nonce)
	q.Set("code_challenge", p.CodeChallenge)
	q.Set("code_challenge_method", "S256")
	q.Set("resource", SIWCResource)
	q.Set("ext_agent_host_id", p.HostID)
	if p.AgentNameHint != "" {
		q.Set("agent_name_hint", p.AgentNameHint)
	}
	if p.IDTokenHint != "" {
		q.Set("id_token_hint", p.IDTokenHint)
	}
	if p.LoginHint != "" {
		q.Set("login_hint", p.LoginHint)
	}
	return c.AuthorizeEndpoint + "?" + q.Encode()
}

// TokenResponse is a successful token-endpoint answer. Scope is the granted
// space-separated scope: on an exchange an omitted scope means the requested
// one (RFC 6749 §5.1) and is filled in; on a refresh it stays "" and the
// caller keeps the stored scopes. ExpiresIn 0 means "not stated".
type TokenResponse struct {
	AccessToken       string
	RefreshToken      string
	IDToken           string
	TokenType         string
	Scope             string
	ExpiresIn         int
	EarliestRefreshAt *time.Time
}

// tokenFailure is a non-2xx token-endpoint answer reduced to its status and
// an identifier-checked OAuth error code.
type tokenFailure struct {
	status int
	code   string
}

func (c *OIDCClient) token(ctx context.Context, form url.Values) (*TokenResponse, *tokenFailure, error) {
	ctx, cancel := context.WithTimeout(ctx, oidcRequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, nil, fmt.Errorf("%w: token request could not be built", iface.ErrLLMProviderUnavailable)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	body, status, err := c.do(ctx, req, oidcMaxTokenBody)
	if err != nil {
		return nil, nil, err
	}
	if status < 200 || status > 299 {
		return nil, &tokenFailure{status: status, code: providers.OAuthErrorCode(body)}, nil
	}
	var raw struct {
		AccessToken       string          `json:"access_token"`
		RefreshToken      string          `json:"refresh_token"`
		IDToken           string          `json:"id_token"`
		TokenType         string          `json:"token_type"`
		Scope             string          `json:"scope"`
		ExpiresIn         int             `json:"expires_in"`
		EarliestRefreshAt json.RawMessage `json:"earliest_refresh_at"`
	}
	if err := json.Unmarshal(body, &raw); err != nil || raw.AccessToken == "" {
		return nil, nil, fmt.Errorf("%w: %w: malformed token response", ErrSIWCExchangeFailed, iface.ErrLLMProviderUnavailable)
	}
	return &TokenResponse{
		AccessToken: raw.AccessToken, RefreshToken: raw.RefreshToken, IDToken: raw.IDToken, TokenType: raw.TokenType,
		Scope: strings.TrimSpace(raw.Scope), ExpiresIn: max(raw.ExpiresIn, 0), EarliestRefreshAt: parseEarliestRefresh(raw.EarliestRefreshAt),
	}, nil, nil
}

// parseEarliestRefresh accepts an RFC 3339 string or Unix seconds (the guide
// names the field without fixing its encoding); anything else is nil.
func parseEarliestRefresh(raw json.RawMessage) *time.Time {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		if ts, err := time.Parse(time.RFC3339, s); err == nil {
			return &ts
		}
		if n, err := strconv.ParseInt(s, 10, 64); err == nil && n > 0 {
			ts := time.Unix(n, 0)
			return &ts
		}
		return nil
	}
	var n float64
	if json.Unmarshal(raw, &n) == nil && n > 0 {
		ts := time.Unix(int64(n), 0)
		return &ts
	}
	return nil
}

// Exchange redeems the authorization code. Every 4xx (bad/expired code,
// invalid_request, invalid_client, …) is ErrSIWCExchangeFailed (3.T4.3); a
// 5xx or a transport failure is iface.ErrLLMProviderUnavailable.
func (c *OIDCClient) Exchange(ctx context.Context, clientID, code, codeVerifier, redirectURI string) (*TokenResponse, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", clientID)
	form.Set("code", code)
	form.Set("code_verifier", codeVerifier)
	form.Set("redirect_uri", redirectURI)
	form.Set("resource", SIWCResource)
	tok, fail, err := c.token(ctx, form)
	switch {
	case err != nil:
		return nil, err
	case fail != nil && fail.status >= 400 && fail.status < 500:
		return nil, fmt.Errorf("%w: %w (status %d, code %q)", ErrSIWCExchangeFailed, iface.ErrLLMProviderUnavailable, fail.status, fail.code)
	case fail != nil:
		return nil, fmt.Errorf("%w: token endpoint answered %d", iface.ErrLLMProviderUnavailable, fail.status)
	}
	if tok.Scope == "" {
		tok.Scope = SIWCScope
	}
	return tok, nil
}

// Refresh rotates the token set (the documented form: issued client id,
// refresh token, resource; scope omitted to keep the grant). A terminal
// refresh code is ErrSIWCRefreshTerminal, invalid_client is
// ErrSIWCInvalidClient; anything else leaves the credentials intact
// (iface.ErrLLMProviderUnavailable).
func (c *OIDCClient) Refresh(ctx context.Context, clientID, refreshToken string) (*TokenResponse, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("client_id", clientID)
	form.Set("refresh_token", refreshToken)
	form.Set("resource", SIWCResource)
	tok, fail, err := c.token(ctx, form)
	switch {
	case err != nil:
		return nil, err
	case fail != nil && fail.status >= 400 && fail.status < 500 && fail.code != "":
		return nil, providers.ClassifySIWCCode(fail.code)
	case fail != nil:
		return nil, fmt.Errorf("%w: token endpoint answered %d", iface.ErrLLMProviderUnavailable, fail.status)
	}
	return tok, nil
}

// Revoke is best-effort (the caller never fails an unlink on it): a missing
// revocation_endpoint is a no-op; a 2xx is success even for a token that was
// already invalid; anything else is iface.ErrLLMProviderUnavailable.
func (c *OIDCClient) Revoke(ctx context.Context, clientID, refreshToken string) error {
	d, err := c.discover(ctx)
	if err != nil {
		return err
	}
	if d.RevocationEndpoint == "" {
		c.logger.Debug("llm siwc: discovery has no revocation endpoint, skipping revoke")
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, oidcRequestTimeout)
	defer cancel()
	form := url.Values{}
	form.Set("token", refreshToken)
	form.Set("token_type_hint", "refresh_token")
	form.Set("client_id", clientID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.RevocationEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("%w: revocation request could not be built", iface.ErrLLMProviderUnavailable)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	_, status, err := c.do(ctx, req, oidcMaxTokenBody)
	if err != nil {
		return err
	}
	if status < 200 || status > 299 {
		return fmt.Errorf("%w: revocation endpoint answered %d", iface.ErrLLMProviderUnavailable, status)
	}
	return nil
}

// IDClaims are the validated ID-token claims the accounts service uses.
type IDClaims struct {
	Issuer    string
	Subject   string
	Email     string
	Nonce     string
	Audience  []string
	ExpiresAt time.Time
}

// VerifyIDToken checks the RS256 signature against the discovery JWKS,
// issuer, audience (the issued client id), expiry and nonce. A token that
// does not validate is ErrSIWCIDTokenInvalid; signing keys that cannot be
// fetched are iface.ErrLLMProviderUnavailable (an outage, not a bad token).
func (c *OIDCClient) VerifyIDToken(ctx context.Context, raw, expectedAud, expectedNonce string) (*IDClaims, error) {
	if expectedAud == "" || expectedNonce == "" {
		return nil, fmt.Errorf("%w: no expected audience or nonce", ErrSIWCIDTokenInvalid)
	}
	claims := jwt.MapClaims{}
	_, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (any, error) {
		kid, _ := t.Header["kid"].(string)
		return c.keyFor(ctx, kid)
	},
		jwt.WithValidMethods([]string{"RS256"}),
		jwt.WithIssuer(c.Issuer),
		jwt.WithAudience(expectedAud),
		jwt.WithExpirationRequired(),
		jwt.WithLeeway(30*time.Second),
		jwt.WithTimeFunc(c.now),
	)
	if err != nil {
		if errors.Is(err, iface.ErrLLMProviderUnavailable) {
			return nil, fmt.Errorf("%w: id token signing keys unavailable", iface.ErrLLMProviderUnavailable)
		}
		return nil, fmt.Errorf("%w: %s", ErrSIWCIDTokenInvalid, jwtFailureReason(err))
	}
	nonce, _ := claims["nonce"].(string)
	if subtle.ConstantTimeCompare([]byte(nonce), []byte(expectedNonce)) != 1 {
		return nil, fmt.Errorf("%w: nonce mismatch", ErrSIWCIDTokenInvalid)
	}
	sub, _ := claims["sub"].(string)
	if sub == "" {
		return nil, fmt.Errorf("%w: missing sub", ErrSIWCIDTokenInvalid)
	}
	iss, _ := claims.GetIssuer()
	email, _ := claims["email"].(string)
	aud, _ := claims.GetAudience()
	exp, _ := claims.GetExpirationTime()
	return &IDClaims{Issuer: iss, Subject: sub, Email: email, Nonce: nonce, Audience: aud, ExpiresAt: exp.Time}, nil
}

// jwtFailureReason is a fixed phrase per failure class (the library's text
// is not echoed).
func jwtFailureReason(err error) string {
	switch {
	case errors.Is(err, jwt.ErrTokenMalformed):
		return "malformed"
	case errors.Is(err, jwt.ErrTokenSignatureInvalid):
		return "bad signature"
	case errors.Is(err, jwt.ErrTokenUnverifiable):
		return "unverifiable (algorithm or key id)"
	case errors.Is(err, jwt.ErrTokenExpired):
		return "expired"
	case errors.Is(err, jwt.ErrTokenInvalidIssuer):
		return "issuer mismatch"
	case errors.Is(err, jwt.ErrTokenInvalidAudience):
		return "audience mismatch"
	case errors.Is(err, jwt.ErrTokenRequiredClaimMissing):
		return "required claim missing"
	}
	return "invalid"
}

var errUnknownKID = fmt.Errorf("%w: unknown key id", ErrSIWCIDTokenInvalid)

// keyFor returns the signing key for kid. The JWKS is cached for an hour; an
// unknown kid (key rotation) or a stale set triggers a refetch, at most one
// fetch attempt per minute whatever the reason, so a flood of forged kids
// cannot hammer the issuer.
func (c *OIDCClient) keyFor(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	if k, ok := c.cachedKey(kid); ok {
		return k, nil
	}
	c.fetchMu.Lock()
	defer c.fetchMu.Unlock()
	if k, ok := c.cachedKey(kid); ok { // another caller fetched meanwhile
		return k, nil
	}
	now := c.now()
	c.mu.Lock()
	stale := c.jwks == nil || now.Sub(c.jwksAt) >= oidcCacheTTL
	capped := !c.jwksAttemptAt.IsZero() && now.Sub(c.jwksAttemptAt) < oidcRefetchCap
	old := c.jwks[kid]
	c.mu.Unlock()
	if capped {
		if old != nil {
			return old, nil // stale but still published a minute ago
		}
		if stale {
			return nil, fmt.Errorf("%w: id token signing keys unavailable", iface.ErrLLMProviderUnavailable)
		}
		return nil, errUnknownKID
	}
	keys, err := c.fetchJWKS(ctx)
	c.mu.Lock()
	c.jwksAttemptAt = now
	if err == nil {
		c.jwks, c.jwksAt = keys, now
	}
	c.mu.Unlock()
	if err != nil {
		if old != nil {
			c.logger.Warn("llm siwc: JWKS refetch failed, using the cached key")
			return old, nil
		}
		return nil, err
	}
	if k := keys[kid]; k != nil {
		return k, nil
	}
	return nil, errUnknownKID
}

func (c *OIDCClient) cachedKey(kid string) (*rsa.PublicKey, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.jwks == nil || c.now().Sub(c.jwksAt) >= oidcCacheTTL {
		return nil, false
	}
	k := c.jwks[kid]
	return k, k != nil
}

func (c *OIDCClient) fetchJWKS(ctx context.Context) (map[string]*rsa.PublicKey, error) {
	d, err := c.discover(ctx)
	if err != nil {
		return nil, err
	}
	body, err := c.get(ctx, d.JWKSURI, oidcMaxJWKSBody)
	if err != nil {
		return nil, err
	}
	var set struct {
		Keys []struct {
			Kty string `json:"kty"`
			Kid string `json:"kid"`
			Use string `json:"use"`
			Alg string `json:"alg"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if json.Unmarshal(body, &set) != nil {
		return nil, fmt.Errorf("%w: JWKS unusable", iface.ErrLLMProviderUnavailable)
	}
	keys := map[string]*rsa.PublicKey{}
	for _, k := range set.Keys {
		if k.Kty != "RSA" || k.Kid == "" || (k.Use != "" && k.Use != "sig") || (k.Alg != "" && k.Alg != "RS256") {
			continue
		}
		nb, err1 := base64.RawURLEncoding.DecodeString(k.N)
		eb, err2 := base64.RawURLEncoding.DecodeString(k.E)
		if err1 != nil || err2 != nil || len(eb) == 0 || len(eb) > 4 {
			continue
		}
		pub := &rsa.PublicKey{N: new(big.Int).SetBytes(nb), E: int(new(big.Int).SetBytes(eb).Int64())}
		if pub.N.BitLen() < oidcMinRSABits || pub.E < 3 || pub.E%2 == 0 {
			continue
		}
		keys[k.Kid] = pub
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("%w: JWKS has no usable RS256 key", iface.ErrLLMProviderUnavailable)
	}
	return keys, nil
}

// discover returns the cached discovery document (1 h), refetching it when
// stale. Its issuer must be ours and its endpoint URLs absolute http(s).
func (c *OIDCClient) discover(ctx context.Context) (*discoveryDoc, error) {
	c.mu.Lock()
	if c.discovery != nil && c.now().Sub(c.discoveryAt) < oidcCacheTTL {
		d := c.discovery
		c.mu.Unlock()
		return d, nil
	}
	c.mu.Unlock()
	body, err := c.get(ctx, c.DiscoveryEndpoint, oidcMaxDocBody)
	if err != nil {
		return nil, err
	}
	var d discoveryDoc
	if json.Unmarshal(body, &d) != nil || d.Issuer != c.Issuer || !absoluteHTTPURL(d.JWKSURI) {
		return nil, fmt.Errorf("%w: discovery document unusable", iface.ErrLLMProviderUnavailable)
	}
	if !absoluteHTTPURL(d.RevocationEndpoint) {
		d.RevocationEndpoint = ""
	}
	c.mu.Lock()
	c.discovery, c.discoveryAt = &d, c.now()
	c.mu.Unlock()
	return &d, nil
}

func absoluteHTTPURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != ""
}

func (c *OIDCClient) get(ctx context.Context, target string, limit int64) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, oidcRequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: request could not be built", iface.ErrLLMProviderUnavailable)
	}
	req.Header.Set("Accept", "application/json")
	body, status, err := c.do(ctx, req, limit)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("%w: %s answered %d", iface.ErrLLMProviderUnavailable, req.URL.Path, status)
	}
	return body, nil
}

// do performs the request and reads at most limit bytes of the body. A
// transport failure keeps only its cause (the *url.Error wrapper, which
// carries the URL, is dropped); a caller cancel stays context.Canceled.
func (c *OIDCClient) do(ctx context.Context, req *http.Request, limit int64) ([]byte, int, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, 0, fmt.Errorf("llm siwc: request canceled: %w", context.Canceled)
		}
		var ue *url.Error
		if errors.As(err, &ue) && ue.Err != nil {
			err = ue.Err
		}
		return nil, 0, fmt.Errorf("%w: %w", iface.ErrLLMProviderUnavailable, err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
		_ = resp.Body.Close()
	}()
	body, err := providers.ReadAllBounded(resp.Body, limit)
	if err != nil {
		if ctx.Err() != nil && errors.Is(ctx.Err(), context.Canceled) {
			return nil, 0, fmt.Errorf("llm siwc: request canceled: %w", context.Canceled)
		}
		return nil, 0, fmt.Errorf("%w: response unreadable or too large", iface.ErrLLMProviderUnavailable)
	}
	return body, resp.StatusCode, nil
}
