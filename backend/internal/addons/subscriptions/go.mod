// Orkestra Subscriptions addon — the recurring-revenue core for the
// modular monolith. Owns the catalog of AI services the operator
// sells (`subscriptions_services`), the subscription records that
// bind Tier-2 external tenants to those services
// (`subscriptions_subscriptions`), cycle-based invoice generation
// (`subscriptions_invoices`), an append-only activity log
// (`subscriptions_activity`), and the renewal job that walks the
// `nextBillingAt` index every hour. Stays cycle-free with the
// `payments` addon by resolving `iface.PaymentProvider` lazily from
// the kernel's ServiceRegistry on every charge, and registers its
// own `iface.SubscriptionReconciler` + `iface.SelfServiceCheckoutPlanner`
// the same way. Hosted in-tree at backend/internal/addons/subscriptions/
// as its own Go module so the same source can be consumed by the
// orkestra monolith AND extracted to a standalone repository
// (orkestra-cc/orkestra-addon-subscriptions) at the same import path.
// The in-tree path is intentionally still under backend/internal/ —
// Go's internal-package rule operates on import paths, not filesystem
// locations, and the import path here does NOT contain "internal".

module github.com/orkestra-cc/orkestra-addon-subscriptions

go 1.25.10

require (
	github.com/danielgtaylor/huma/v2 v2.34.1
	github.com/go-chi/chi/v5 v5.2.5
	github.com/google/uuid v1.6.0
	github.com/orkestra-cc/orkestra-sdk v0.2.0
	go.mongodb.org/mongo-driver v1.17.6
)

require (
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/golang/snappy v1.0.0 // indirect
	github.com/klauspost/compress v1.18.1 // indirect
	github.com/montanaflynn/stats v0.7.1 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/prometheus/client_golang v1.23.2 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.66.1 // indirect
	github.com/prometheus/procfs v0.16.1 // indirect
	github.com/xdg-go/pbkdf2 v1.0.0 // indirect
	github.com/xdg-go/scram v1.1.2 // indirect
	github.com/xdg-go/stringprep v1.0.4 // indirect
	github.com/youmark/pkcs8 v0.0.0-20240726163527-a2c0da244d78 // indirect
	go.yaml.in/yaml/v2 v2.4.2 // indirect
	golang.org/x/crypto v0.52.0 // indirect
	golang.org/x/sync v0.20.0 // indirect
	golang.org/x/sys v0.45.0 // indirect
	golang.org/x/text v0.37.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)
