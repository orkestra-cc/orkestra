package main

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/orkestra/backend/internal/shared/config"
	"github.com/orkestra/backend/internal/shared/errors"
	authMiddleware "github.com/orkestra/backend/internal/shared/middleware"
)

// newAudienceTestMux builds an audience mux the way main.go does: the global
// middleware of setupMiddleware, the shared huma config, the mux-level API
// and one module-style sub-router minting its own huma API from the same
// config (every module does this, and each such API registers huma's
// built-in spec routes again).
func newAudienceTestMux(t *testing.T, env string, apiDocsEnabled bool) *chi.Mux {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{}
	cfg.Server.Environment = env
	cfg.Server.APIDocsEnabled = apiDocsEnabled
	cfg.Server.MaxBodySize = 1 << 20
	errorManager := errors.NewManager(logger, env == "development")
	deviceMW := authMiddleware.NewDeviceMiddleware(errorManager, "", false)
	trustedProxies, err := authMiddleware.NewTrustedProxyPolicy(0, nil)
	if err != nil {
		t.Fatalf("trusted proxy policy: %v", err)
	}

	mux := chi.NewRouter()
	setupMiddleware(mux, cfg, errorManager, deviceMW, nil, config.AudienceConfig{}, logger, trustedProxies)

	apiConfig := huma.DefaultConfig("Orkestra API", "1.0.0")
	apiConfig.DocsPath = ""
	api := humachi.New(mux, apiConfig)
	registerHealthEndpoints(api, nil, nil)
	mux.Group(func(r chi.Router) {
		humachi.New(r, apiConfig)
	})
	if apiDocsEnabled {
		registerDocsEndpoints(mux, api)
	}
	return mux
}

func getStatus(mux http.Handler, path string) int {
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec.Code
}

// specPaths are the routes huma serves on its own from the shared config:
// the OpenAPI document in four encodings and one JSON Schema per type.
var specPaths = []string{
	"/openapi.json",
	"/openapi.yaml",
	"/openapi-3.0.json",
	"/openapi-3.0.yaml",
	"/schemas/ErrorModel.json",
}

// With API docs off (the default in staging and production) the route
// inventory must not be readable. main.go skipped registerDocsEndpoints but
// huma kept serving its built-in copies, so an internet-facing deployment
// answered 200 on all of these while logging "API docs disabled".
func TestSpecRoutes_NotServedWhenAPIDocsAreDisabled(t *testing.T) {
	mux := newAudienceTestMux(t, "production", false)
	for _, p := range specPaths {
		if got := getStatus(mux, p); got != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404 with API docs disabled", p, got)
		}
	}
	if got := getStatus(mux, "/docs"); got != http.StatusNotFound {
		t.Errorf("GET /docs = %d, want 404 with API docs disabled", got)
	}
}

// Development keeps the document and the schema links working.
func TestSpecRoutes_ServedWhenAPIDocsAreEnabled(t *testing.T) {
	mux := newAudienceTestMux(t, "development", true)
	for _, p := range append([]string{"/docs"}, specPaths...) {
		if got := getStatus(mux, p); got != http.StatusOK {
			t.Errorf("GET %s = %d, want 200 with API docs enabled", p, got)
		}
	}
}

// The gate must not swallow application routes that merely look similar.
func TestSpecRoutes_GateLeavesOtherRoutesAlone(t *testing.T) {
	mux := newAudienceTestMux(t, "production", false)
	mux.Get("/v1/schemas/list", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.Get("/openapi", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	for _, p := range []string{"/v1/schemas/list", "/openapi"} {
		if got := getStatus(mux, p); got != http.StatusNoContent {
			t.Errorf("GET %s = %d, want 204: not a spec route", p, got)
		}
	}
}

// X-XSS-Protection: "1; mode=block" turns on the legacy XSS auditor in the
// browsers that still have one, and the auditor itself has been used to
// leak data and to disable scripts selectively. The current recommendation
// is to switch it off explicitly; the frontends already send "0".
func TestSecurityHeaders_LegacyXSSAuditorIsSwitchedOff(t *testing.T) {
	mux := newAudienceTestMux(t, "production", false)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nowhere", nil))
	if got := rec.Header().Get("X-XSS-Protection"); got != "0" {
		t.Errorf("X-XSS-Protection = %q, want \"0\"", got)
	}
}

// The operator host is an internet-facing origin in staging and production,
// and /metrics lists every route template, job and auth counter. It stays on
// the LAN ops handler, which only answers a host that is not an audience
// host. In development the host mux sends every unmatched host to the
// operator mux, so that is where a local Prometheus scrape lands.
func TestOperatorMetrics_OnTheOperatorHostInDevelopmentOnly(t *testing.T) {
	metricsHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	for _, tc := range []struct {
		env  string
		want int
	}{
		{"production", http.StatusNotFound},
		{"staging", http.StatusNotFound},
		// Any other value is not development either: only there does the
		// host mux fall through to the operator mux.
		{"preview", http.StatusNotFound},
		{"development", http.StatusOK},
	} {
		cfg := &config.Config{}
		cfg.Server.Environment = tc.env
		mux := chi.NewRouter()
		mountOperatorMetrics(mux, metricsHandler, cfg)
		if got := getStatus(mux, "/metrics"); got != tc.want {
			t.Errorf("%s: GET /metrics on the operator mux = %d, want %d", tc.env, got, tc.want)
		}
	}
}
