package setup

// Task 5.3: proves the public/protected route split at the HTTP layer —
// not just that RegisterPublicRoutes and RegisterProtectedRoutes are two
// separate Go methods, but that mounting them the way cmd/server/main.go
// does produces the intended security boundary: the two pre-existing
// setup endpoints stay reachable anonymously, while the two new
// finalization endpoints require a verified operator-audience bearer
// token, even though every route lives under the same /v1/setup/ path
// prefix.
//
// The mini operator surface mirrors main.go's real wiring: a chi.Mux
// carrying RequireAudience (same shape as middleware/audience_test.go's
// mux-level tests) with a public Huma API built directly on it, plus a
// RequireAuth-gated sub-router mounted on that mux carrying its own Huma
// API for the protected group. The operator token is minted through a
// real *rsa.PrivateKey-backed JWTService via GenerateAccessToken — the
// same fixture pattern middleware/require_auth_test.go uses — because
// RequireAuth verifies a real signature; internal/testkit's
// context-injection helpers bypass the HTTP layer entirely and so cannot
// stand in for a bearer token here. The client-audience token only needs
// to satisfy RequireAudience's unverified-claim read, so it is a
// throwaway self-signed JWT in the same style as
// middleware/audience_test.go's signTestToken — it never reaches
// RequireAuth.
import (
	"crypto/rand"
	"crypto/rsa"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"

	authServices "github.com/orkestra/backend/internal/core/auth/services"
	"github.com/orkestra/backend/internal/shared/config"
	sharederrors "github.com/orkestra/backend/internal/shared/errors"
	"github.com/orkestra/backend/internal/shared/middleware"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newMountTestHandler builds a Handler backed by the same in-package
// stubs service_test.go uses (stubUsers, stubAdmin, fakeFinalizationStore)
// so Status() resolves to phase admin_required without touching a real
// database.
func newMountTestHandler(t *testing.T) *Handler {
	t.Helper()
	svc := NewService(&stubUsers{}, &stubAdmin{}, &fakeFinalizationStore{}, nil, nil, nil, discardLogger())
	return NewHandler(svc, config.CookieConfig{})
}

// mountTestSurface bundles the mux plus the tokens the table tests need.
type mountTestSurface struct {
	mux           *chi.Mux
	operatorToken string
	clientToken   string
}

// newMountTestSurface builds the mini operator surface: RequireAudience
// on the mux, public Huma API registered directly on it, and a
// RequireAuth-gated sub-router (its own Huma API) mounted before it goes
// live — mirroring cmd/server/main.go's operatorMux / operatorProtected
// split.
func newMountTestSurface(t *testing.T) mountTestSurface {
	t.Helper()

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	jwtSvc, err := authServices.NewJWTServiceWithAudience(
		priv, &priv.PublicKey, "test", authServices.AudienceOperator,
		15*time.Minute, 7*24*time.Hour,
	)
	if err != nil {
		t.Fatalf("NewJWTServiceWithAudience: %v", err)
	}
	em := sharederrors.NewManager(discardLogger(), false)
	authMW := middleware.NewAuthMiddleware(jwtSvc, em)

	handler := newMountTestHandler(t)

	mux := chi.NewRouter()
	mux.Use(middleware.RequireAudience("operator", "service"))

	publicAPI := humachi.New(mux, huma.DefaultConfig("mount-test-public", "1.0.0"))
	handler.RegisterPublicRoutes(publicAPI)

	protected := chi.NewRouter()
	protected.Use(authMW.RequireAuth)
	protected.Group(func(r chi.Router) {
		protectedAPI := humachi.New(r, huma.DefaultConfig("mount-test-protected", "1.0.0"))
		handler.RegisterProtectedRoutes(protectedAPI)
	})
	mux.Mount("/", protected)

	operatorUser := &iface.User{UUID: "u-operator", Email: "operator@example.com", Role: "administrator"}
	operatorToken, err := jwtSvc.GenerateAccessToken(operatorUser)
	if err != nil {
		t.Fatalf("GenerateAccessToken(operator): %v", err)
	}

	return mountTestSurface{
		mux:           mux,
		operatorToken: operatorToken,
		clientToken:   signRawAudienceToken(t, "client"),
	}
}

// signRawAudienceToken mints a self-signed JWT carrying only an `aud`
// claim. RequireAudience reads `aud` unverified, so this never needs to
// share a key with the fixture's real JWTService — it exists purely to
// exercise the audience gate, and (by construction) never reaches
// RequireAuth in any case this file asserts.
func signRawAudienceToken(t *testing.T, aud string) string {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	claims := jwt.MapClaims{
		"sub": "u-client",
		"aud": aud,
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(15 * time.Minute).Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	signed, err := tok.SignedString(priv)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return signed
}

func TestSetupRouteMount_AudienceAndAuth(t *testing.T) {
	surface := newMountTestSurface(t)

	cases := []struct {
		name       string
		method     string
		path       string
		bearer     string // "" = anonymous
		wantStatus int
	}{
		{
			name:       "anonymous status is public",
			method:     http.MethodGet,
			path:       "/v1/setup/status",
			wantStatus: http.StatusOK,
		},
		{
			name:       "anonymous finalization-access is rejected",
			method:     http.MethodGet,
			path:       "/v1/setup/finalization-access",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "anonymous finalize is rejected",
			method:     http.MethodPost,
			path:       "/v1/setup/finalize",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "client-audience token on finalization-access is rejected",
			method:     http.MethodGet,
			path:       "/v1/setup/finalization-access",
			bearer:     surface.clientToken,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "client-audience token on finalize is rejected",
			method:     http.MethodPost,
			path:       "/v1/setup/finalize",
			bearer:     surface.clientToken,
			wantStatus: http.StatusUnauthorized,
		},
		{
			// Task 5.4 implements FinalizationAccess for real: with the
			// mount test's zero-value fakes the coordinator record is
			// absent, so the binding is empty and the caller falls to the
			// recovery check. Since D28 that check reads the caller's role
			// from the STORE, not from the token — here stubUsers.role is
			// the zero value, which is not super_admin — so the probe
			// answers 200 with {canFinalize:false, canClaimRecovery:false,
			// reason:"recovery_requires_super_admin"} rather than 501.
			// (Setting stubUsers.role to "super_admin" flips this case;
			// changing the token's role no longer does.)
			name:       "operator token reaches finalization-access handler",
			method:     http.MethodGet,
			path:       "/v1/setup/finalization-access",
			bearer:     surface.operatorToken,
			wantStatus: http.StatusOK,
		},
		{
			// Task 5.5 implements Finalize for real. The fake store has no
			// coordinator record, so the binding is empty and the caller
			// falls to the recovery check, which since D28 reads the role
			// from stubUsers (zero value, not super_admin) rather than from
			// the token: the handler answers 403
			// setup.recovery_requires_super_admin. What this case proves is
			// unchanged — the request reached the handler rather than being
			// stopped by the audience or auth gate.
			name:       "operator token reaches finalize handler",
			method:     http.MethodPost,
			path:       "/v1/setup/finalize",
			bearer:     surface.operatorToken,
			wantStatus: http.StatusForbidden,
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			// POST /v1/setup/finalize declares a required JSON body, so
			// Huma's request validation rejects a malformed one before the
			// handler runs — send a schema-valid payload unconditionally so
			// every POST case reaches the same point in the pipeline the
			// case is actually trying to test (auth/audience), rather than
			// failing earlier on a 422.
			var body io.Reader
			if c.method == http.MethodPost {
				body = strings.NewReader(`{"tenantName":"Acme Corp","tenantSlug":"acme","allowAdditionalInternalTenants":false}`)
			}
			req := httptest.NewRequest(c.method, c.path, body)
			if body != nil {
				req.Header.Set("Content-Type", "application/json")
			}
			if c.bearer != "" {
				req.Header.Set("Authorization", "Bearer "+c.bearer)
			}
			rec := httptest.NewRecorder()
			surface.mux.ServeHTTP(rec, req)
			if rec.Code != c.wantStatus {
				t.Errorf("status = %d, want %d (body=%s)", rec.Code, c.wantStatus, rec.Body.String())
			}
		})
	}
}

// TestSetupRouteMount_ClientAudienceBodyIsAudienceMismatch pins the
// specific rejection reason for the client-audience cases above: the
// request must be rejected by RequireAudience (before it ever reaches
// RequireAuth or the handler), not by some other 401 path.
func TestSetupRouteMount_ClientAudienceBodyIsAudienceMismatch(t *testing.T) {
	surface := newMountTestSurface(t)

	req := httptest.NewRequest(http.MethodGet, "/v1/setup/finalization-access", nil)
	req.Header.Set("Authorization", "Bearer "+surface.clientToken)
	rec := httptest.NewRecorder()
	surface.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body=%s)", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); !strings.Contains(got, "audience_mismatch") {
		t.Errorf("body = %q, want it to contain %q", got, "audience_mismatch")
	}
}

// TestRegisterPublicRoutes_ExposesOnlyPreExistingOps proves the public
// registrar's actual Huma output — not just its source code — contains
// exactly the two pre-existing setup operations. RegisterProtectedRoutes
// is never called against this API, so if the split were ever collapsed
// back into one registrar (or a future edit mounted the protected routes
// on the public API by mistake) this test fails by inspecting the
// generated OpenAPI document rather than by reading the registrar's Go
// source.
func TestRegisterPublicRoutes_ExposesOnlyPreExistingOps(t *testing.T) {
	handler := newMountTestHandler(t)

	mux := chi.NewRouter()
	api := humachi.New(mux, huma.DefaultConfig("public-only-test", "1.0.0"))
	handler.RegisterPublicRoutes(api)

	got := map[string]bool{}
	for _, item := range api.OpenAPI().Paths {
		for _, op := range []*huma.Operation{
			item.Get, item.Put, item.Post, item.Delete,
			item.Options, item.Head, item.Patch, item.Trace,
		} {
			if op != nil {
				got[op.OperationID] = true
			}
		}
	}

	want := map[string]bool{"setup-status": true, "setup-create-admin": true}
	if len(got) != len(want) {
		t.Fatalf("public registrar operations = %v, want exactly %v", got, want)
	}
	for id := range want {
		if !got[id] {
			t.Errorf("public registrar is missing operation %q; got %v", id, got)
		}
	}
}
