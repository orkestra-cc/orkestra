// Orkestra Payments addon — gateway-agnostic payment processing.
// Implements `iface.PaymentProvider` (Stripe in v1, PayPal reserved
// for v2) so the `subscriptions` addon can charge cards, issue
// refunds, and react to webhook events without importing any
// gateway-specific code. Owns `payments_transactions`,
// `payments_payment_methods`, and `payments_webhook_events`
// (the (provider, providerEventID) uniqueness invariant is the
// webhook-dedupe idempotency guard). Cycle-free with `subscriptions`
// via lazy `iface.SubscriptionReconciler` resolution from the
// kernel's ServiceRegistry. Hosted in-tree at
// backend/internal/addons/payments/ as its own Go module so the same
// source can be consumed by the orkestra monolith AND extracted to a
// standalone repository (orkestra-cc/orkestra-addon-payments) at the
// same import path. The in-tree path is intentionally still under
// backend/internal/ — Go's internal-package rule operates on import
// paths, not filesystem locations, and the import path here does NOT
// contain "internal".

module github.com/orkestra-cc/orkestra-addon-payments

go 1.25.10

require (
	github.com/danielgtaylor/huma/v2 v2.34.1
	github.com/go-chi/chi/v5 v5.2.5
	github.com/google/uuid v1.6.0
	github.com/orkestra-cc/orkestra-sdk v0.2.0
	github.com/stripe/stripe-go/v76 v76.25.0
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
