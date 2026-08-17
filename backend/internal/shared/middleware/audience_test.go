package middleware

import (
	"crypto/rand"
	"crypto/rsa"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
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

// TestRequireAudienceWebhookCarveOut is a mux-level test: it builds the
// same shape cmd/server's setupMiddleware builds — RequireAudience
// mounted on the chi.Mux itself, above every route — and asserts that an
// authentic SDI callback reaches its handler.
//
// The SDI callback authenticates with `Authorization: Bearer <static
// webhook secret>` (billing/module.go configures that header upstream on
// OpenAPI.it; webhook_handler.go constant-time-compares it). That bearer
// is not a JWT, so it carries no `aud` — before the carve-out the mux
// 401'd every authentic delivery with audience_mismatch, before
// ModuleGate, the poll throttle, the replay dedup, or the secret check
// could run.
//
// The carve-out is deliberately surgical, so the same non-JWT bearer on
// any non-webhook path — including the other IsPublicRoute entries,
// which authenticate by their own token but are not webhooks — must
// still fail closed per the ADR-0003 PR-D D-3 hard cutover.
func TestRequireAudienceWebhookCarveOut(t *testing.T) {
	t.Parallel()

	const staticWebhookSecret = "not-a-jwt"

	newMux := func(reached *bool) *chi.Mux {
		mux := chi.NewRouter()
		mux.Use(RequireAudience("operator"))
		hit := func(w http.ResponseWriter, _ *http.Request) {
			*reached = true
			w.WriteHeader(http.StatusOK)
		}
		// Mirrors billing's webhook mount and payments' Stripe mount.
		mux.Post("/v1/billing/webhooks/sdi", hit)
		mux.Post("/v1/payments/webhooks/stripe", hit)
		// Non-webhook routes: a business route, and a non-webhook
		// PublicRoutes entry (the carve-out must not widen to those).
		mux.Get("/v1/billing/invoices", hit)
		mux.Get("/v1/setup/status", hit)
		return mux
	}

	cases := []struct {
		name        string
		method      string
		path        string
		bearer      string // "" → no Authorization header
		wantStatus  int
		wantReached bool
	}{
		{
			name:        "SDI webhook with static-secret bearer reaches the handler",
			method:      http.MethodPost,
			path:        "/v1/billing/webhooks/sdi",
			bearer:      staticWebhookSecret,
			wantStatus:  http.StatusOK,
			wantReached: true,
		},
		{
			name:        "Stripe webhook prefix is carved out too",
			method:      http.MethodPost,
			path:        "/v1/payments/webhooks/stripe",
			bearer:      staticWebhookSecret,
			wantStatus:  http.StatusOK,
			wantReached: true,
		},
		{
			name:        "bearer-less webhook still reaches the handler",
			method:      http.MethodPost,
			path:        "/v1/billing/webhooks/sdi",
			wantStatus:  http.StatusOK,
			wantReached: true,
		},
		{
			name:        "same non-JWT bearer on a business route still 401s",
			method:      http.MethodGet,
			path:        "/v1/billing/invoices",
			bearer:      staticWebhookSecret,
			wantStatus:  http.StatusUnauthorized,
			wantReached: false,
		},
		{
			name:        "carve-out does not widen to non-webhook public routes",
			method:      http.MethodGet,
			path:        "/v1/setup/status",
			bearer:      staticWebhookSecret,
			wantStatus:  http.StatusUnauthorized,
			wantReached: false,
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			var reached bool
			mux := newMux(&reached)

			req := httptest.NewRequest(c.method, c.path, nil)
			if c.bearer != "" {
				req.Header.Set("Authorization", "Bearer "+c.bearer)
			}
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != c.wantStatus {
				t.Errorf("status = %d, want %d (body=%s)", rec.Code, c.wantStatus, rec.Body.String())
			}
			if reached != c.wantReached {
				t.Errorf("handler reached = %v, want %v", reached, c.wantReached)
			}
		})
	}
}

// TestRequireAudienceWebhookCarveOutRejectsCrossAudienceElsewhere pins
// that the carve-out did not weaken the cutover for a real cross-tier
// token: a well-formed aud=client JWT on the operator mux is still 401'd
// on every non-webhook path.
func TestRequireAudienceWebhookCarveOutRejectsCrossAudienceElsewhere(t *testing.T) {
	t.Parallel()

	mux := chi.NewRouter()
	mux.Use(RequireAudience("operator"))
	mux.Get("/v1/billing/invoices", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/billing/invoices", nil)
	req.Header.Set("Authorization", "Bearer "+signTestToken(t, "client"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d (body=%s)", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}
