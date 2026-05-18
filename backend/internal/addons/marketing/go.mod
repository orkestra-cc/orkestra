// Orkestra Marketing addon — contact base, importer pipeline, and (in
// future phases) activity log, scoring, and card lifecycle. Hosted
// in-tree at backend/internal/addons/marketing/ as its own Go module
// so the same source can be consumed by the orkestra monolith AND
// extracted to a standalone repository
// (orkestra-cc/orkestra-addon-marketing) at the same import path. The
// in-tree path is intentionally still under backend/internal/ — Go's
// internal-package rule operates on import paths, not filesystem
// locations, and the import path here does NOT contain "internal".
//
// Phase 1 (Fondazione anagrafica MVP — design at
// docs/plans/marketing-addon/Orkestra_marketing_addon.md §9) ships
// only the module scaffold; collections, models, handlers, services,
// and importers land in subsequent PRs on feature/marketing-addon.

module github.com/orkestra-cc/orkestra-addon-marketing

go 1.25.10

require github.com/orkestra-cc/orkestra-sdk v0.4.0

require (
	github.com/danielgtaylor/huma/v2 v2.34.1 // indirect
	github.com/go-chi/chi/v5 v5.2.5 // indirect
	github.com/golang/snappy v1.0.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/klauspost/compress v1.18.1 // indirect
	github.com/montanaflynn/stats v0.7.1 // indirect
	github.com/xdg-go/pbkdf2 v1.0.0 // indirect
	github.com/xdg-go/scram v1.1.2 // indirect
	github.com/xdg-go/stringprep v1.0.4 // indirect
	github.com/youmark/pkcs8 v0.0.0-20240726163527-a2c0da244d78 // indirect
	go.mongodb.org/mongo-driver v1.17.6 // indirect
	golang.org/x/crypto v0.51.0 // indirect
	golang.org/x/sync v0.20.0 // indirect
	golang.org/x/text v0.37.0 // indirect
)
