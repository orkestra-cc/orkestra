package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
)

// newDocsTestMux wires /health + /docs + /openapi.json the way main.go does.
// Each test builds its own huma config, so the shared-document duplicate-ID
// rule guarded by TestHealthProbes_NoDuplicateOperationID is not in play.
func newDocsTestMux(t *testing.T) *chi.Mux {
	t.Helper()
	cfg := huma.DefaultConfig("Orkestra API", "1.0.0")
	cfg.DocsPath = ""
	mux := chi.NewRouter()
	api := humachi.New(mux, cfg)
	registerHealthEndpoints(api, nil, nil)
	registerDocsEndpoints(mux, api)
	return mux
}

func parseCSP(t *testing.T, csp string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, d := range strings.Split(csp, ";") {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		name, value, _ := strings.Cut(d, " ")
		if _, dup := out[name]; dup {
			t.Errorf("CSP directive %q appears twice", name)
		}
		out[name] = strings.TrimSpace(value)
	}
	return out
}

// TestDocsPage_IsNotAScriptSinkOnTheAPIOrigin guards the hardening of the
// docs page. /docs is same-origin with the API — the origin that carries the
// HttpOnly refresh cookie — so any script that runs there can mint an
// operator access token for whoever is logged in. The page therefore
// executes exactly one script, pinned by version and SRI, and talks to
// nobody but 'self'. The previous policy allowed 'unsafe-inline',
// 'unsafe-eval', an unpinned "latest" bundle, and connect-src to
// localhost:* plus a foreign wildcard domain left over from an old deployment.
func TestDocsPage_IsNotAScriptSinkOnTheAPIOrigin(t *testing.T) {
	mux := newDocsTestMux(t)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/docs", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /docs = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
	if got := rec.Header().Get("X-Robots-Tag"); !strings.Contains(got, "noindex") {
		t.Errorf("X-Robots-Tag = %q, want noindex", got)
	}

	csp := parseCSP(t, rec.Header().Get("Content-Security-Policy"))
	if got := csp["script-src"]; got != scalarScriptURL {
		t.Errorf("script-src = %q, want exactly the pinned bundle %q", got, scalarScriptURL)
	}
	if got := csp["connect-src"]; got != "'self'" {
		t.Errorf("connect-src = %q, want 'self' only", got)
	}
	if got := csp["frame-ancestors"]; got != "'none'" {
		t.Errorf("frame-ancestors = %q, want 'none'", got)
	}
	if got := csp["object-src"]; got != "'none'" {
		t.Errorf("object-src = %q, want 'none'", got)
	}
	for name, value := range csp {
		for _, bad := range []string{"'unsafe-eval'", "localhost", "*."} {
			if strings.Contains(value, bad) {
				t.Errorf("CSP %s contains %q: %q", name, bad, value)
			}
		}
		if name != "style-src" && strings.Contains(value, "'unsafe-inline'") {
			t.Errorf("CSP %s allows 'unsafe-inline': %q", name, value)
		}
	}

	// The bundle is pinned to an exact release and integrity-checked.
	pinned := regexp.MustCompile(`^https://cdn\.jsdelivr\.net/npm/@scalar/api-reference@\d+\.\d+\.\d+/dist/browser/standalone\.js$`)
	if !pinned.MatchString(scalarScriptURL) {
		t.Errorf("scalarScriptURL = %q, want an exact-version file path (no tag, range, or bare package URL)", scalarScriptURL)
	}
	if !regexp.MustCompile(`^sha384-[A-Za-z0-9+/]{64}$`).MatchString(scalarScriptSRI) {
		t.Errorf("scalarScriptSRI = %q, want a base64 sha384 digest", scalarScriptSRI)
	}

	body := rec.Body.String()
	scripts := regexp.MustCompile(`(?s)<script\b([^>]*)>(.*?)</script>`).FindAllStringSubmatch(body, -1)
	var external []string
	for _, m := range scripts {
		attrs, content := m[1], m[2]
		src := regexp.MustCompile(`\ssrc="([^"]*)"`).FindStringSubmatch(attrs)
		if src == nil {
			// Inline scripts must stay empty — there is no 'unsafe-inline' or
			// nonce to let one run; the config travels in data-* attributes.
			if strings.TrimSpace(content) != "" {
				t.Errorf("inline script has a body, which the CSP would block: %q", m[0])
			}
			continue
		}
		external = append(external, src[1])
		if !strings.Contains(attrs, `integrity="`+scalarScriptSRI+`"`) || !strings.Contains(attrs, `crossorigin="anonymous"`) {
			t.Errorf("bundle script tag lacks SRI + crossorigin: %s", m[0])
		}
	}
	if len(external) != 1 || external[0] != scalarScriptURL {
		t.Errorf("external scripts = %v, want exactly [%s]", external, scalarScriptURL)
	}

	// Scalar must stay on this origin: no hosted "try it" proxy, no
	// telemetry, no fonts CDN, no localStorage-persisted auth.
	cfgAttr := regexp.MustCompile(`data-configuration='([^']*)'`).FindStringSubmatch(body)
	if cfgAttr == nil {
		t.Fatal("docs page has no data-configuration attribute")
	}
	var scalarCfg map[string]any
	if err := json.Unmarshal([]byte(cfgAttr[1]), &scalarCfg); err != nil {
		t.Fatalf("data-configuration is not JSON: %v", err)
	}
	for k, want := range map[string]any{"proxyUrl": "", "telemetry": false, "withDefaultFonts": false, "persistAuth": false} {
		if scalarCfg[k] != want {
			t.Errorf("data-configuration[%q] = %v, want %v", k, scalarCfg[k], want)
		}
	}
}

// TestOpenAPIEndpoint_ServesTheSharedDocument keeps /openapi.json wired to
// the live huma document (the same one OPENAPI_DUMP serializes).
func TestOpenAPIEndpoint_ServesTheSharedDocument(t *testing.T) {
	mux := newDocsTestMux(t)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /openapi.json = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var doc struct {
		Paths map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	for _, p := range []string{"/health", "/ready"} {
		if _, ok := doc.Paths[p]; !ok {
			t.Errorf("document lacks %s", p)
		}
	}
}
