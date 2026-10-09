package services

// M-20 (spec §4.10 D35): the mobile validators checked the signature and,
// when the config read succeeded, the audience. No issuer check at all,
// and an EMPTY expected audience SKIPPED the check — so MobileAudience
// returning "" disabled it rather than failing the login.

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type idClaims struct {
	Iss, Aud, Sub, Email, Nonce string
	Exp                         int64
}

func defaultClaims(iss string) *idClaims {
	return &idClaims{Iss: iss, Aud: "client-id", Sub: "sub-1", Email: "u@example.com", Exp: time.Now().Add(time.Hour).Unix()}
}

func signIDToken(t *testing.T, key *rsa.PrivateKey, c *idClaims) string {
	t.Helper()
	m := jwt.MapClaims{"iss": c.Iss, "aud": c.Aud, "sub": c.Sub, "email": c.Email, "email_verified": true, "iat": time.Now().Unix()}
	if c.Exp != 0 {
		m["exp"] = c.Exp
	}
	if c.Nonce != "" {
		m["nonce"] = c.Nonce
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, m)
	tok.Header["kid"] = "k1"
	s, err := tok.SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

type fakeGoogleKeys struct {
	GoogleKeysService
	key *rsa.PublicKey
}

func (f fakeGoogleKeys) GetPublicKey(context.Context, string) (*rsa.PublicKey, error) {
	return f.key, nil
}

func testKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

type validatorCase struct {
	name    string
	mutate  func(*idClaims)
	req     func(*IDTokenValidationRequest)
	wantErr bool
}

func validatorCases(goodIss, altIss string) []validatorCase {
	cases := []validatorCase{
		{"happy path", nil, nil, false},
		{"wrong issuer", func(c *idClaims) { c.Iss = "https://evil.example.com" }, nil, true},
		{"wrong audience", func(c *idClaims) { c.Aud = "someone-elses-client-id" }, nil, true},
		{"EMPTY expected audience is an ERROR, not a skip", nil, func(r *IDTokenValidationRequest) { r.Audience = "" }, true},
		{"missing exp", func(c *idClaims) { c.Exp = 0 }, nil, true},
		{"expired", func(c *idClaims) { c.Exp = time.Now().Add(-time.Hour).Unix() }, nil, true},
		{"nonce matches", func(c *idClaims) { c.Nonce = "abc" }, func(r *IDTokenValidationRequest) { r.ExpectedNonce = "abc" }, false},
		{"nonce mismatch", func(c *idClaims) { c.Nonce = "abc" }, func(r *IDTokenValidationRequest) { r.ExpectedNonce = "xyz" }, true},
		{"nonce missing when expected", nil, func(r *IDTokenValidationRequest) { r.ExpectedNonce = "abc" }, true},
		{"no nonce expected (web Apple exchange)", func(c *idClaims) { c.Nonce = "" }, nil, false},
		{"nonce is returned verbatim", func(c *idClaims) { c.Nonce = "n-1" }, nil, false},
	}
	if altIss != "" {
		cases = append(cases, validatorCase{altIss + " is accepted", func(c *idClaims) { c.Iss = altIss }, nil, false})
	}
	return cases
}

func runValidatorCases(t *testing.T, goodIss string, cases []validatorCase, key *rsa.PrivateKey, validate func(context.Context, *IDTokenValidationRequest) (*UserInfo, error)) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := defaultClaims(goodIss)
			if tc.mutate != nil {
				tc.mutate(c)
			}
			req := &IDTokenValidationRequest{IDToken: signIDToken(t, key, c), Audience: "client-id"}
			if tc.req != nil {
				tc.req(req)
			}
			info, err := validate(context.Background(), req)
			if tc.wantErr {
				if err == nil {
					t.Fatal("want an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if info.ProviderID != "sub-1" || info.Email != "u@example.com" {
				t.Fatalf("info = %+v", info)
			}
			if info.Nonce != c.Nonce {
				t.Fatalf("Nonce = %q, want the claim %q verbatim", info.Nonce, c.Nonce)
			}
		})
	}
}

func TestGoogleValidateIDToken(t *testing.T) {
	key := testKey(t)
	svc := &googleOAuthService{config: &OAuthProviderConfig{ClientID: "client-id"}, keysService: fakeGoogleKeys{key: &key.PublicKey}}
	runValidatorCases(t, "https://accounts.google.com", validatorCases("https://accounts.google.com", "accounts.google.com"), key, svc.ValidateIDToken)
}

func TestAppleValidateIDToken(t *testing.T) {
	key := testKey(t)
	svc := &appleOAuthService{
		config:     &OAuthProviderConfig{ClientID: "client-id"},
		keyFetcher: func(context.Context, string) (interface{}, error) { return &key.PublicKey, nil },
	}
	runValidatorCases(t, "https://appleid.apple.com", validatorCases("https://appleid.apple.com", ""), key, svc.ValidateIDToken)
}

// The error returned to the CALLER must not carry the wrapped parse
// error (L-27): details go to the log.
func TestValidateIDToken_ErrorDoesNotLeakParserDetail(t *testing.T) {
	key := testKey(t)
	g := &googleOAuthService{config: &OAuthProviderConfig{ClientID: "client-id"}, keysService: fakeGoogleKeys{key: &key.PublicKey}}
	a := &appleOAuthService{config: &OAuthProviderConfig{ClientID: "client-id"}, keyFetcher: func(context.Context, string) (interface{}, error) { return &key.PublicKey, nil }}
	garbage := "eyJhbGciOiJSUzI1NiIsImtpZCI6ImsxIn0.e30.not-a-signature"
	for name, validate := range map[string]func(context.Context, *IDTokenValidationRequest) (*UserInfo, error){"google": g.ValidateIDToken, "apple": a.ValidateIDToken} {
		_, err := validate(context.Background(), &IDTokenValidationRequest{IDToken: garbage, Audience: "client-id"})
		if err == nil {
			t.Fatalf("%s: want an error", name)
		}
		for _, leak := range []string{"token is malformed", "signature is invalid", "crypto/rsa", "illegal base64"} {
			if strings.Contains(err.Error(), leak) {
				t.Fatalf("%s: the caller's error must be opaque, got %q", name, err.Error())
			}
		}
	}
}
