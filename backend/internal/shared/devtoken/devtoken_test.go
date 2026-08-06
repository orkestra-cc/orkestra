package devtoken

// Security tests for the dev-token endpoint's environment gate.
//
// POST /dev/token mints a signed, fully-privileged JWT for any role the
// caller asks for, with no credentials whatsoever. It is a deliberate
// backdoor for local development. The gate that keeps it out of
// internet-reachable environments is therefore the only thing standing
// between an anonymous request and a super_admin token — it must cover
// staging, not just production.

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

// fakePlatform implements module.PlatformInfo for a named environment.
type fakePlatform struct{ env string }

func (p fakePlatform) IsProduction() bool     { return p.env == "production" }
func (p fakePlatform) IsStaging() bool        { return p.env == "staging" }
func (p fakePlatform) IsDevelopment() bool    { return p.env == "development" }
func (p fakePlatform) IsProductionLike() bool { return p.env == "production" || p.env == "staging" }
func (p fakePlatform) GetEnvironment() string { return p.env }
func (p fakePlatform) FrontendURL() string    { return "http://localhost:8080" }

// fakeJWT mints a recognisable placeholder instead of a real signature —
// these tests are about the gate, not about token contents.
type fakeJWT struct{}

func (fakeJWT) GenerateAccessToken(u *iface.User) (string, error) {
	return "minted-token-for-" + u.Role, nil
}

func routerFor(env string) chi.Router {
	r := chi.NewRouter()
	h := NewHandler(fakeJWT{}, fakeJWT{}, fakePlatform{env: env}, nil, slog.Default())
	h.RegisterRoutes(r)
	return r
}

func postDevToken(r chi.Router, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/dev/token", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestRegisterRoutes_RefusesInStaging(t *testing.T) {
	w := postDevToken(routerFor("staging"), `{"role":"super_admin"}`)

	if w.Code != http.StatusNotFound {
		t.Fatalf("staging must not expose /dev/token; got status %d body %q", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "minted-token") {
		t.Fatalf("staging minted a dev token: %q", w.Body.String())
	}
}

func TestRegisterRoutes_RefusesInProduction(t *testing.T) {
	w := postDevToken(routerFor("production"), `{"role":"super_admin"}`)

	if w.Code != http.StatusNotFound {
		t.Fatalf("production must not expose /dev/token; got status %d", w.Code)
	}
}

func TestRegisterRoutes_ServesInDevelopment(t *testing.T) {
	w := postDevToken(routerFor("development"), `{"role":"super_admin"}`)

	if w.Code != http.StatusOK {
		t.Fatalf("development must serve /dev/token; got status %d body %q", w.Code, w.Body.String())
	}
	var resp generateTokenResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.AccessToken != "minted-token-for-super_admin" {
		t.Errorf("AccessToken = %q", resp.AccessToken)
	}
}

// generateToken carries its own guard as a safety net for a caller that
// mounts the handler without going through RegisterRoutes.
func TestGenerateToken_HandlerGuardRejectsStaging(t *testing.T) {
	h := NewHandler(fakeJWT{}, fakeJWT{}, fakePlatform{env: "staging"}, nil, slog.Default())

	req := httptest.NewRequest(http.MethodPost, "/dev/token", strings.NewReader(`{"role":"super_admin"}`))
	w := httptest.NewRecorder()
	h.generateToken(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("handler guard must reject staging; got status %d body %q", w.Code, w.Body.String())
	}
}

func TestListRoles_HandlerGuardRejectsStaging(t *testing.T) {
	h := NewHandler(fakeJWT{}, fakeJWT{}, fakePlatform{env: "staging"}, nil, slog.Default())

	req := httptest.NewRequest(http.MethodGet, "/dev/token/roles", nil)
	w := httptest.NewRecorder()
	h.listRoles(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("listRoles must reject staging; got status %d", w.Code)
	}
}

// The expiry field was validated and echoed back but never applied to
// the minted token, so callers were told a lie about the token's TTL.
func TestGenerateToken_RejectsExpiryItCannotHonour(t *testing.T) {
	w := postDevToken(routerFor("development"), `{"role":"developer","expiry":"24h"}`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("an expiry the endpoint cannot apply must be rejected; got status %d body %q", w.Code, w.Body.String())
	}
}
