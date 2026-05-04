package middleware

import (
	"crypto/rand"
	"crypto/rsa"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// signTestToken mints a minimal JWT for the audience MW tests. The MW
// reads aud unverifiedly, so the signing key only needs to produce a
// well-formed token — verification happens deeper in the auth MW.
func signTestToken(t *testing.T, audClaim any) string {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	claims := jwt.MapClaims{
		"sub": "u-1",
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(15 * time.Minute).Unix(),
	}
	if audClaim != nil {
		claims["aud"] = audClaim
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	signed, err := tok.SignedString(priv)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return signed
}

// TestRequireAudienceCutoverBehaviour locks in the ADR-0003 PR-D D-3
// hard cutover: tokens missing aud or carrying the legacy
// "orkestra-api" value are rejected. Only an exact aud match (or no
// bearer at all, for public routes) passes through.
func TestRequireAudienceCutoverBehaviour(t *testing.T) {
	t.Parallel()

	mw := RequireAudience("operator")
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	cases := []struct {
		name       string
		aud        any // nil → no aud claim
		bearer     bool
		wantStatus int
	}{
		{"no bearer (public route)", nil, false, http.StatusOK},
		{"v1 token (no aud)", nil, true, http.StatusUnauthorized},
		{"legacy orkestra-api aud", "orkestra-api", true, http.StatusUnauthorized},
		{"matching operator aud", "operator", true, http.StatusOK},
		{"mismatched client aud", "client", true, http.StatusUnauthorized},
		{"empty string aud", "", true, http.StatusUnauthorized},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, "/v1/anything", nil)
			if c.bearer {
				req.Header.Set("Authorization", "Bearer "+signTestToken(t, c.aud))
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != c.wantStatus {
				t.Errorf("status = %d, want %d (body=%s)", rec.Code, c.wantStatus, rec.Body.String())
			}
		})
	}
}
