// Orkestra Billing addon — Italian electronic invoicing (Fattura
// Elettronica / FatturaPA) via the OpenAPI SDI integration. Generates
// XML invoices conformant to FatturaPA v1.2.3 (Specifiche Tecniche
// v1.9), submits them to Sistema di Interscambio through OpenAPI.it,
// receives SDI notifications via both webhook (`/v1/billing/webhooks/sdi`)
// and a polling job (safety net), and reconciles the per-invoice
// `marking` field on every sync. Depends on the shared
// orkestra-openapi-auth minter (carved out in Phase 5c) to exchange
// (account email, API key) Basic credentials for JWT bearers at
// oauth.openapi.it/token. Hosted in-tree at
// backend/internal/addons/billing/ as its own Go module so the same
// source can be consumed by the orkestra monolith AND extracted to a
// standalone repository (orkestra-cc/orkestra-addon-billing) at the
// same import path. The in-tree path is intentionally still under
// backend/internal/ — Go's internal-package rule operates on import
// paths, not filesystem locations, and the import path here does NOT
// contain "internal".

module github.com/orkestra-cc/orkestra-addon-billing

go 1.25.10

require (
	github.com/danielgtaylor/huma/v2 v2.34.1
	github.com/go-chi/chi/v5 v5.2.5
	github.com/google/uuid v1.6.0
	github.com/orkestra-cc/orkestra-openapi-auth v0.1.0
	github.com/orkestra-cc/orkestra-sdk v0.2.0
	go.mongodb.org/mongo-driver v1.17.6
)

require (
	github.com/golang/snappy v1.0.0 // indirect
	github.com/klauspost/compress v1.18.1 // indirect
	github.com/montanaflynn/stats v0.7.1 // indirect
	github.com/xdg-go/pbkdf2 v1.0.0 // indirect
	github.com/xdg-go/scram v1.1.2 // indirect
	github.com/xdg-go/stringprep v1.0.4 // indirect
	github.com/youmark/pkcs8 v0.0.0-20240726163527-a2c0da244d78 // indirect
	golang.org/x/crypto v0.52.0 // indirect
	golang.org/x/sync v0.20.0 // indirect
	golang.org/x/text v0.37.0 // indirect
)
