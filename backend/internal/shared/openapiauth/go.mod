// Orkestra OpenAPI auth — shared OAuth-token minter for OpenAPI.com
// (openapi.it) products. Used by the company and billing addons to
// exchange (account email, API key) Basic credentials for a short-lived
// JWT bearer with a caller-specified scope list. Hosted in-tree at
// backend/internal/shared/openapiauth/ as its own Go module so the same
// source can be consumed by the orkestra monolith AND extracted to a
// standalone repository (orkestra-cc/orkestra-openapi-auth) at the
// same import path. The in-tree path is intentionally still under
// backend/internal/ — Go's internal-package rule operates on import
// paths, not filesystem locations, and the import path here does NOT
// contain "internal".

module github.com/orkestra-cc/orkestra-openapi-auth

go 1.25.10
