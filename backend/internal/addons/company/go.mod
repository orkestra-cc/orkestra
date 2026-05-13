// Orkestra Company addon — Italian business-registry lookup against
// the OpenAPI Company API (company.openapi.com). Looks up
// codice-fiscale and partita-IVA holders, persists each result in
// MongoDB (company_lookups) with a Redis cache in front, and exposes
// enrichment endpoints for advanced / marketing / stakeholders / AML
// data sets. The addon depends on orkestra-openapi-auth for the
// OAuth-token minter that exchanges (account email, API key) Basic
// credentials for short-lived JWT bearers at oauth.openapi.it/token —
// previously imported from backend/internal/shared/openapiauth before
// the Phase 5c carve-out. Hosted in-tree at
// backend/internal/addons/company/ as its own Go module so the same
// source can be consumed by the orkestra monolith AND extracted to a
// standalone repository (orkestra-cc/orkestra-addon-company) at the
// same import path. The in-tree path is intentionally still under
// backend/internal/ — Go's internal-package rule operates on import
// paths, not filesystem locations, and the import path here does NOT
// contain "internal".

module github.com/orkestra-cc/orkestra-addon-company

go 1.25.10

require (
	github.com/danielgtaylor/huma/v2 v2.34.1
	github.com/go-chi/chi/v5 v5.2.5
	github.com/google/uuid v1.6.0
	github.com/orkestra-cc/orkestra-openapi-auth v0.1.0
	github.com/orkestra-cc/orkestra-sdk v0.2.0
	go.mongodb.org/mongo-driver v1.17.6
)
