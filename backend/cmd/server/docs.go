package main

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/chi/v5"
)

// scalarScriptURL pins the Scalar API-reference bundle to an exact version
// AND file. The bare package URL (https://cdn.jsdelivr.net/npm/@scalar/api-reference)
// resolves to "latest" on every page load, so a compromised release would
// run on the API origin the moment it was published. The pin plus the SRI
// hash below make the served bytes explicit; the browser refuses anything
// else. Bump both together:
//
//	curl -sO https://cdn.jsdelivr.net/npm/@scalar/api-reference@<ver>/dist/browser/standalone.js
//	printf 'sha384-'; openssl dgst -sha384 -binary standalone.js | openssl base64 -A
const scalarScriptURL = "https://cdn.jsdelivr.net/npm/@scalar/api-reference@1.67.0/dist/browser/standalone.js"

// scalarScriptSRI is the Subresource Integrity digest of scalarScriptURL.
const scalarScriptSRI = "sha384-6c7Vmx+i0yi8gBbltn0x1cavD+zsMGw2xmXXVyacPJLIGBxwaVimW5TW0WiW17Ir"

// docsCSP is the Content-Security-Policy of the docs page. The page is
// same-origin with the API — the origin that carries the HttpOnly refresh
// cookie — so it must not be a script sink:
//
//   - script-src names the pinned bundle only. No 'unsafe-inline', no
//     'unsafe-eval': Scalar 1.67 needs neither (its one Function("") call
//     is a feature probe inside try/catch), and the config <script> is
//     empty, which the HTML "prepare a script" algorithm returns from before
//     the CSP check.
//   - connect-src is 'self' only, so "try it" requests can reach neither
//     Scalar's hosted proxy (proxy.scalar.com, the bundle's default) nor any
//     other origin.
//   - style-src keeps 'unsafe-inline' because Scalar injects its theme via
//     <style> elements; fonts stay local (withDefaultFonts:false below).
const docsCSP = "default-src 'self'; " +
	"script-src " + scalarScriptURL + "; " +
	"style-src 'self' 'unsafe-inline'; " +
	"connect-src 'self'; " +
	"img-src 'self' data: https:; " +
	"font-src 'self' data:; " +
	"object-src 'none'; " +
	"base-uri 'self'; " +
	"form-action 'self'; " +
	"frame-ancestors 'none'"

// docsHTML is the Scalar page. The data-configuration keeps the page
// self-contained: no hosted proxy for "try it" (proxyUrl ""), no telemetry,
// no fonts from fonts.scalar.com, no "open in Scalar client" hand-off of the
// spec URL, and no auth persisted to localStorage on the API origin.
const docsHTML = `<!doctype html>
<html>
<head>
    <title>Orkestra API Documentation</title>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <meta name="robots" content="noindex, nofollow" />
    <style>
        body { margin: 0; padding: 0; }
    </style>
</head>
<body>
    <script id="api-reference" data-url="/openapi.json" data-configuration='{"proxyUrl":"","telemetry":false,"withDefaultFonts":false,"hideClientButton":true,"persistAuth":false}'></script>
    <script src="` + scalarScriptURL + `" integrity="` + scalarScriptSRI + `" crossorigin="anonymous"></script>
</body>
</html>
`

// registerDocsEndpoints registers /docs (Scalar UI) and /openapi.json.
// main.go calls it only when cfg.Server.APIDocsEnabled is true.
func registerDocsEndpoints(router *chi.Mux, publicAPI huma.API) {
	router.Get("/docs", func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", docsCSP)
		h.Set("Content-Type", "text/html; charset=utf-8")
		h.Set("X-Robots-Tag", "noindex, nofollow")
		_, _ = w.Write([]byte(docsHTML))
	})

	router.Get("/openapi.json", func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Type", "application/json")
		h.Set("X-Robots-Tag", "noindex, nofollow")
		if err := json.NewEncoder(w).Encode(publicAPI.OpenAPI()); err != nil {
			http.Error(w, "Failed to generate OpenAPI spec", http.StatusInternalServerError)
			return
		}
	})
}

// isSpecRoute reports whether path is one of the routes huma serves from the
// shared API config on its own: the OpenAPI document (/openapi.json,
// /openapi.yaml, /openapi-3.0.json, /openapi-3.0.yaml) and the JSON Schema of
// each type (/schemas/{schema}).
func isSpecRoute(path string) bool {
	return strings.HasPrefix(path, "/openapi.") ||
		strings.HasPrefix(path, "/openapi-") ||
		strings.HasPrefix(path, "/schemas/")
}

// denySpecRoutes answers 404 on the spec routes. setupMiddleware mounts it
// when API docs are disabled (API_DOCS_ENABLED, off by default in staging
// and production), so the route inventory is not readable from the API
// origin whatever huma instance registered the route.
func denySpecRoutes(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isSpecRoute(r.URL.Path) {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}
