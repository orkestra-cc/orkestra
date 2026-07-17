package main

import (
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
)

// TestHealthProbes_NoDuplicateOperationID guards the huma v2.39 regression:
// operatorAPI and clientAPI share ONE OpenAPI document (both built from the
// same huma.Config), so registering the health/readiness operations on both
// APIs panics with "duplicate operation ID". The fix registers the Huma
// operations once (operator, which owns the shared document) and serves the
// client host with raw probe routes. This test mirrors that wiring and asserts
// (a) no panic, (b) each operation appears once in the shared document, and
// (c) the client mux serves the raw probes.
func TestHealthProbes_NoDuplicateOperationID(t *testing.T) {
	cfg := huma.DefaultConfig("Orkestra API", "1.0.0")
	cfg.DocsPath = ""

	operatorMux := chi.NewRouter()
	operatorAPI := humachi.New(operatorMux, cfg)
	clientMux := chi.NewRouter()
	_ = humachi.New(clientMux, cfg) // shares cfg.OpenAPI with operatorAPI

	// Must not panic. If the client host is ever switched back to
	// registerHealthEndpoints(clientAPI, ...), huma >=2.39 panics here on the
	// duplicate operation ID in the shared document.
	registerHealthEndpoints(operatorAPI, nil, nil)
	registerHealthProbes(clientMux, nil, nil)

	// Each operation is documented exactly once in the shared OpenAPI doc.
	doc := operatorAPI.OpenAPI()
	for _, p := range []string{"/health", "/ready"} {
		item := doc.Paths[p]
		if item == nil || item.Get == nil {
			t.Errorf("shared OpenAPI document missing GET %s", p)
		}
	}

	// The client mux serves the raw probes (route match only — invoking the
	// handler would require a live Mongo/Redis, which this unit test omits).
	for _, p := range []string{"/health", "/ready"} {
		if !clientMux.Match(chi.NewRouteContext(), http.MethodGet, p) {
			t.Errorf("client mux does not serve GET %s", p)
		}
	}
}
