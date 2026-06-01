package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/golang-jwt/jwt/v5"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

// stubPlatform is a minimal module.PlatformInfo for tests. The real
// implementation is `*shared/config.Config`, which addons can't import
// after the dev extraction (Phase 5i) — same way the subscriptions /
// payments addons replaced their internal/testkit import with an
// inline helper.
type stubPlatform struct {
	env string
}

func (s stubPlatform) IsProduction() bool     { return s.env == "production" }
func (s stubPlatform) IsStaging() bool        { return s.env == "staging" }
func (s stubPlatform) IsDevelopment() bool    { return s.env == "development" || s.env == "" }
func (s stubPlatform) IsProductionLike() bool { return s.IsProduction() || s.IsStaging() }
func (s stubPlatform) GetEnvironment() string { return s.env }
func (s stubPlatform) FrontendURL() string    { return "" }

// stubJWTProvider records the user it was called with and returns a
// pre-baked unsigned JWT carrying the configured audience claim. The
// real jwtService stamps `aud` from the audience constructor argument
// — the stub mirrors that contract so the test can assert the dev
// handler routes the audience-encoded request to the matching
// provider without booting the full JWT signing path.
type stubJWTProvider struct {
	audience string
	called   bool
}

func (s *stubJWTProvider) GenerateAccessToken(_ *iface.User) (string, error) {
	s.called = true
	tok := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.MapClaims{
		"sub": "dev",
		"aud": s.audience,
	})
	// SigningMethodNone needs the magic UnsafeAllowNoneSignatureType
	// sentinel for the v5 library to produce a serialized token.
	return tok.SignedString(jwt.UnsafeAllowNoneSignatureType)
}

// audienceFromToken reads the `aud` claim from an unsigned token. The
// dev handler's contract is "the response carries an audience field
// AND the JWT itself was minted by the matching provider"; this helper
// covers the second half so a future regression that wires the wrong
// provider into the handler can't pass by only stamping the response
// field.
func audienceFromToken(t *testing.T, raw string) string {
	t.Helper()
	parsed, _, err := jwt.NewParser().ParseUnverified(raw, jwt.MapClaims{})
	if err != nil {
		t.Fatalf("parse token: %v", err)
	}
	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		t.Fatalf("claims wrong type: %T", parsed.Claims)
	}
	aud, _ := claims["aud"].(string)
	return aud
}

// TestGenerateTokenHTTPDispatchesByAudience covers ADR-0003 PR-D D-10.
// The dev token endpoint must mint a token whose `aud` claim matches
// the requested audience — operator (default) and client. A bad
// audience must be rejected with 400 before either provider is
// invoked.
func TestGenerateTokenHTTPDispatchesByAudience(t *testing.T) {
	plat := stubPlatform{env: "development"}

	cases := []struct {
		name        string
		body        string
		wantStatus  int
		wantAud     string
		wantOpCall  bool
		wantClCall  bool
		wantErrText string
	}{
		{
			name:       "default audience routes to operator",
			body:       `{"role":"administrator"}`,
			wantStatus: http.StatusOK,
			wantAud:    "operator",
			wantOpCall: true,
		},
		{
			name:       "explicit operator routes to operator",
			body:       `{"role":"administrator","audience":"operator"}`,
			wantStatus: http.StatusOK,
			wantAud:    "operator",
			wantOpCall: true,
		},
		{
			name:       "explicit client routes to client",
			body:       `{"role":"administrator","audience":"client"}`,
			wantStatus: http.StatusOK,
			wantAud:    "client",
			wantClCall: true,
		},
		{
			name:        "unknown audience rejected with 400",
			body:        `{"role":"administrator","audience":"service"}`,
			wantStatus:  http.StatusBadRequest,
			wantErrText: "invalid audience",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opStub := &stubJWTProvider{audience: "operator"}
			clStub := &stubJWTProvider{audience: "client"}
			h := NewDevTokenHandler(iface.JWTProvider(opStub), iface.JWTProvider(clStub), plat)

			req := httptest.NewRequest(http.MethodPost, "/dev/token", bytes.NewBufferString(tc.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			h.GenerateTokenHTTP(rec, req)

			if got := rec.Code; got != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body: %s)", got, tc.wantStatus, rec.Body.String())
			}

			if tc.wantStatus != http.StatusOK {
				if tc.wantErrText != "" && !bytes.Contains(rec.Body.Bytes(), []byte(tc.wantErrText)) {
					t.Fatalf("error body %q does not contain %q", rec.Body.String(), tc.wantErrText)
				}
				if opStub.called || clStub.called {
					t.Fatalf("provider called on rejected audience: op=%v cl=%v", opStub.called, clStub.called)
				}
				return
			}

			var resp generateTokenHTTPResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("unmarshal response: %v (body: %s)", err, rec.Body.String())
			}
			if resp.Audience != tc.wantAud {
				t.Fatalf("response audience = %q, want %q", resp.Audience, tc.wantAud)
			}
			if got := audienceFromToken(t, resp.AccessToken); got != tc.wantAud {
				t.Fatalf("token aud claim = %q, want %q", got, tc.wantAud)
			}
			if opStub.called != tc.wantOpCall {
				t.Fatalf("operator provider called = %v, want %v", opStub.called, tc.wantOpCall)
			}
			if clStub.called != tc.wantClCall {
				t.Fatalf("client provider called = %v, want %v", clStub.called, tc.wantClCall)
			}
		})
	}
}
