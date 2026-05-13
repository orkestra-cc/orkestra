// Orkestra Compliance addon — the platform compliance plane.
// Owns the append-only audit_events collection, publishes
// `iface.AuditSink` so every other module can emit without importing
// compliance, drives the GDPR DSR pipeline (export + erasure across
// every registered iface.PIIProducer), runs the per-tenant KMS
// envelope-encryption + crypto-shred lifecycle, and computes the
// SOC2 evidence snapshot consumed at /v1/admin/compliance/soc2/evidence.
// Pushes the audit sink into auth, tenant, identity, and
// subscriptions services via the SDK's `iface.AuditSinkSetter`
// contract — no concrete-type imports across the addon boundary.
// Hosted in-tree at backend/internal/addons/compliance/ as its own
// Go module so the same source can be consumed by the orkestra
// monolith AND extracted to a standalone repository
// (orkestra-cc/orkestra-addon-compliance) at the same import path.
// The in-tree path is intentionally still under backend/internal/ —
// Go's internal-package rule operates on import paths, not
// filesystem locations, and the import path here does NOT contain
// "internal".

module github.com/orkestra-cc/orkestra-addon-compliance

go 1.25.10

require (
	github.com/danielgtaylor/huma/v2 v2.34.1
	github.com/go-chi/chi/v5 v5.2.5
	github.com/google/uuid v1.6.0
	github.com/orkestra-cc/orkestra-sdk v0.3.0
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
	golang.org/x/crypto v0.51.0 // indirect
	golang.org/x/sync v0.20.0 // indirect
	golang.org/x/text v0.37.0 // indirect
)
