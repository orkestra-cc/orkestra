module github.com/orkestra/backend

go 1.25.10

// SDK split (Phase 4): the public SDK lives at
// github.com/orkestra-cc/orkestra-sdk (released v0.1.0 from the
// in-tree source at pkg/sdk/). The replace directive points at the
// in-tree path so local development uses the live tree; CI and
// external consumers without the replace resolve v0.1.0 through the
// Go module proxy. The replace will drop once cross-cutting changes
// no longer need round-trip through the SDK repo.
replace github.com/orkestra-cc/orkestra-sdk => ./pkg/sdk

// Phase 5a: the documents addon is its own Go module. Source lives
// in-tree at backend/internal/addons/documents/ and is mirrored to
// the public repo orkestra-cc/orkestra-addon-documents on each tag.
// The replace directive keeps monorepo development against live
// in-tree source; external consumers and the proxy fetch the tagged
// version. Subsequent addon extractions follow the same pattern —
// one replace line per addon until cross-cutting churn settles.
replace github.com/orkestra-cc/orkestra-addon-documents => ./internal/addons/documents

// Phase 5b: the aimodels addon is its own Go module. Mirrors the
// documents pattern — source lives in-tree, mirrored to
// orkestra-cc/orkestra-addon-aimodels and tagged from v0.1.0. Note
// that rag and sales consume aimodels' providers package directly,
// so when those addons are extracted later they'll pick up
// orkestra-addon-aimodels as a transitive require.
replace github.com/orkestra-cc/orkestra-addon-aimodels => ./internal/addons/aimodels

// Phase 5c: the openapiauth shared helper is its own Go module so the
// company addon (also extracted in Phase 5c) and billing (still
// in-tree) can both import it across module boundaries. Source lives
// in-tree at backend/internal/shared/openapiauth/; the same tree is
// mirrored to orkestra-cc/orkestra-openapi-auth and tagged from v0.1.0.
replace github.com/orkestra-cc/orkestra-openapi-auth => ./internal/shared/openapiauth

// Phase 5c: the company addon is its own Go module. Mirrors the
// documents/aimodels pattern — source lives in-tree, mirrored to
// orkestra-cc/orkestra-addon-company and tagged from v0.1.0. The
// addon depends on orkestra-openapi-auth (above) for the OAuth-token
// minter previously at internal/shared/openapiauth/.
replace github.com/orkestra-cc/orkestra-addon-company => ./internal/addons/company

// Phase 5d: the graph addon is its own Go module. Source lives
// in-tree, mirrored to orkestra-cc/orkestra-addon-graph and tagged
// from v0.1.0. The graph-specific Bolt-driver helpers that used to
// live at internal/shared/database/graph.go moved into this addon
// as part of the extraction (only graph/module.go consumed them).
replace github.com/orkestra-cc/orkestra-addon-graph => ./internal/addons/graph

// Phase 5e: the sales addon is its own Go module. Source lives
// in-tree, mirrored to orkestra-cc/orkestra-addon-sales and tagged
// from v0.1.0. The SalesConfig struct that used to live at
// internal/shared/config/config.go moved into this addon under
// internal/addons/sales/config — only the sales addon consumed it,
// and the env-var populator in the shared config package was
// already dead code.
replace github.com/orkestra-cc/orkestra-addon-sales => ./internal/addons/sales

// Phase 5f: the subscriptions addon is its own Go module. Source
// lives in-tree, mirrored to orkestra-cc/orkestra-addon-subscriptions
// and tagged from v0.1.0. The subscriptions handler tests dropped
// their `internal/testkit` import in favor of an inline `authedCtx`
// helper that stamps the SDK ctxauth keys directly; testkit stays
// in-tree until the remaining consumers (payments, compliance) get
// extracted in their own phases.
replace github.com/orkestra-cc/orkestra-addon-subscriptions => ./internal/addons/subscriptions

// Phase 5g: the payments addon is its own Go module. Source lives
// in-tree, mirrored to orkestra-cc/orkestra-addon-payments and
// tagged from v0.1.0. Same testkit-replacement playbook as Phase 5f
// — client_handler_test.go uses an inline `authedCtx` helper instead
// of importing internal/testkit (testkit still serves compliance,
// which extracts later).
replace github.com/orkestra-cc/orkestra-addon-payments => ./internal/addons/payments

// Phase 5h: the billing addon is its own Go module — the last of the
// "easy tier" extractions and the largest at 15K LOC. Zero
// cross-module backend internal deps because the OpenAPI.com OAuth
// minter (openapiauth) was already carved out in Phase 5c. Source
// lives in-tree, mirrored to orkestra-cc/orkestra-addon-billing and
// tagged from v0.1.0.
replace github.com/orkestra-cc/orkestra-addon-billing => ./internal/addons/billing

// Phase 5i: the dev addon is its own Go module — first of the "hard
// tier" extractions. Source lives in-tree, mirrored to
// orkestra-cc/orkestra-addon-dev and tagged from v0.1.0. The handler
// dropped its `core/user/models.User` import in favor of the SDK's
// canonical `iface.User` (the in-tree models package is already a
// re-export shim around iface.User, so the swap was a one-symbol
// edit). The audience test stubbed `module.PlatformInfo` directly
// instead of importing `shared/config.Config`.
replace github.com/orkestra-cc/orkestra-addon-dev => ./internal/addons/dev

// Phase 5j: the compliance addon is its own Go module. Source lives
// in-tree, mirrored to orkestra-cc/orkestra-addon-compliance and
// tagged from v0.1.0. Three small interfaces were lifted into the
// SDK to unblock the extraction (`iface.AuditSinkSetter`,
// `iface.KMSProviderSetter`, `iface.ClientSelfDeletionGate`); the
// module's post-init wiring now probes `module.GetTyped[iface.*]`
// instead of importing concrete-pointer types from auth/tenant/
// identity. SOC2 evidence keeps two cross-module Mongo collection
// names as inlined string constants (operator_users,
// operator_mfa_factors) with a contract note in soc2.go. testkit
// was the last serial consumer — `me_handler_test.go` switched to
// an inline `authedCtx` helper (Phase 5f/5g playbook), leaving
// internal/testkit backend-internal only.
replace github.com/orkestra-cc/orkestra-addon-compliance => ./internal/addons/compliance

// Phase 5k: the identity addon is its own Go module. Source lives
// in-tree, mirrored to orkestra-cc/orkestra-addon-identity and
// tagged from v0.1.0. Two SDK iface lifts unblock the extraction:
// `iface.LoginTokenIssuer` + `iface.LoginTokens` (so identity can
// mint sessions through core/auth without importing its concrete
// `*PasswordAuthService`) — bumped the SDK to v0.4.0. The handful
// of `internal/shared/utils` helpers identity used (OAuth-token
// encryption, secure-random, OIDC state/nonce, GetClientIP) were
// inlined into the addon's private `internal/cryptoutil` subpackage
// with byte-for-byte identical algorithms.
replace github.com/orkestra-cc/orkestra-addon-identity => ./internal/addons/identity

// Phase 5l: the rag addon is its own Go module — the final addon
// extraction in the SDK split. Source lives in-tree, mirrored to
// orkestra-cc/orkestra-addon-rag and tagged from v0.1.0. The
// RAGConfig struct that used to live at internal/shared/config
// moved into this addon under internal/addons/rag/config (same
// playbook as Phase 5e sales — the env-var populator block in
// shared/config was already dead code because module.go builds a
// fresh RAGConfig from its own Settings on every Init). rag depends
// transitively on orkestra-cc/orkestra-addon-aimodels (for the
// EmbeddingProvider / LLMProvider contracts in its `providers`
// package).
replace github.com/orkestra-cc/orkestra-addon-rag => ./internal/addons/rag

// Marketing addon (Phase 1 — Fondazione anagrafica MVP): contact base,
// importer pipeline, and (in future phases) immutable activity log,
// scoring engine, card lifecycle. Hosted in-tree at
// backend/internal/addons/marketing/ as its own Go module, mirrored to
// orkestra-cc/orkestra-addon-marketing from v0.1.0 once Phase 1 stabilizes.
// Design + per-phase plan at docs/plans/marketing-addon/ in this repo.
replace github.com/orkestra-cc/orkestra-addon-marketing => ./internal/addons/marketing

require (
	github.com/alicebob/miniredis/v2 v2.38.0
	github.com/cedar-policy/cedar-go v1.6.0
	github.com/danielgtaylor/huma/v2 v2.34.1
	github.com/docker/docker v28.5.2+incompatible
	github.com/docker/go-connections v0.7.0
	github.com/go-chi/chi/v5 v5.2.5
	github.com/go-chi/cors v1.2.2
	github.com/go-playground/validator/v10 v10.28.0
	github.com/go-webauthn/webauthn v0.17.3
	github.com/golang-jwt/jwt/v5 v5.3.1
	github.com/google/uuid v1.6.0
	github.com/joho/godotenv v1.5.1
	github.com/orkestra-cc/orkestra-addon-aimodels v0.1.0
	github.com/orkestra-cc/orkestra-addon-billing v0.1.0
	github.com/orkestra-cc/orkestra-addon-company v0.1.1
	github.com/orkestra-cc/orkestra-addon-compliance v0.1.0
	github.com/orkestra-cc/orkestra-addon-dev v0.1.0
	github.com/orkestra-cc/orkestra-addon-documents v0.1.0
	github.com/orkestra-cc/orkestra-addon-graph v0.1.1
	github.com/orkestra-cc/orkestra-addon-identity v0.1.1
	github.com/orkestra-cc/orkestra-addon-marketing v0.1.0
	github.com/orkestra-cc/orkestra-addon-payments v0.1.0
	github.com/orkestra-cc/orkestra-addon-rag v0.1.0
	github.com/orkestra-cc/orkestra-addon-sales v0.1.0
	github.com/orkestra-cc/orkestra-addon-subscriptions v0.1.0
	github.com/orkestra-cc/orkestra-sdk v0.4.0
	github.com/pquerna/otp v1.5.0
	github.com/redis/go-redis/v9 v9.19.0
	github.com/vectorize-io/hindsight/hindsight-clients/go v0.0.0-20260323151157-26944e25bc03
	go.mongodb.org/mongo-driver v1.17.6
	go.opentelemetry.io/contrib/bridges/otelslog v0.18.0
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.68.0
	go.opentelemetry.io/otel v1.43.0
	go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp v0.19.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp v1.43.0
	go.opentelemetry.io/otel/sdk v1.43.0
	go.opentelemetry.io/otel/sdk/log v0.19.0
	go.opentelemetry.io/otel/trace v1.43.0
	golang.org/x/crypto v0.52.0
	golang.org/x/tools v0.45.0
)

require (
	github.com/Microsoft/go-winio v0.6.2 // indirect
	github.com/PuerkitoBio/goquery v1.11.0 // indirect
	github.com/andybalholm/cascadia v1.3.3 // indirect
	github.com/antchfx/htmlquery v1.3.5 // indirect
	github.com/antchfx/xmlquery v1.5.0 // indirect
	github.com/antchfx/xpath v1.3.6 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/bits-and-blooms/bitset v1.24.4 // indirect
	github.com/boombuler/barcode v1.0.1-0.20190219062509-6c824513bacc // indirect
	github.com/cenkalti/backoff/v5 v5.0.3 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/containerd/errdefs v1.0.0 // indirect
	github.com/containerd/errdefs/pkg v0.3.0 // indirect
	github.com/containerd/log v0.1.0 // indirect
	github.com/coreos/go-oidc/v3 v3.18.0 // indirect
	github.com/distribution/reference v0.6.0 // indirect
	github.com/docker/go-units v0.5.0 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/fxamacker/cbor/v2 v2.9.2 // indirect
	github.com/gabriel-vasile/mimetype v1.4.11 // indirect
	github.com/go-jose/go-jose/v4 v4.1.4 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-playground/locales v0.14.1 // indirect
	github.com/go-playground/universal-translator v0.18.1 // indirect
	github.com/go-viper/mapstructure/v2 v2.5.0 // indirect
	github.com/go-webauthn/x v0.2.5 // indirect
	github.com/gobwas/glob v0.2.3 // indirect
	github.com/gocolly/colly/v2 v2.3.0 // indirect
	github.com/golang/groupcache v0.0.0-20241129210726-2c02b8208cf8 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/golang/snappy v1.0.0 // indirect
	github.com/google/go-tpm v0.9.8 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.28.0 // indirect
	github.com/kennygrant/sanitize v1.2.4 // indirect
	github.com/klauspost/compress v1.18.1 // indirect
	github.com/leodido/go-urn v1.4.0 // indirect
	github.com/moby/docker-image-spec v1.3.1 // indirect
	github.com/moby/sys/atomicwriter v0.1.0 // indirect
	github.com/moby/term v0.5.2 // indirect
	github.com/montanaflynn/stats v0.7.1 // indirect
	github.com/morikuni/aec v1.1.0 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/neo4j/neo4j-go-driver/v5 v5.28.4 // indirect
	github.com/nlnwa/whatwg-url v0.6.2 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/opencontainers/image-spec v1.1.1 // indirect
	github.com/orkestra-cc/orkestra-openapi-auth v0.1.0 // indirect
	github.com/philhofer/fwd v1.2.0 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/prometheus/client_golang v1.23.2 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.66.1 // indirect
	github.com/prometheus/procfs v0.16.1 // indirect
	github.com/richardlehane/mscfb v1.0.6 // indirect
	github.com/richardlehane/msoleps v1.0.6 // indirect
	github.com/saintfish/chardet v0.0.0-20230101081208-5e3ef4b5456d // indirect
	github.com/sashabaranov/go-openai v1.41.2 // indirect
	github.com/stripe/stripe-go/v76 v76.25.0 // indirect
	github.com/temoto/robotstxt v1.1.2 // indirect
	github.com/tiendc/go-deepcopy v1.7.2 // indirect
	github.com/tinylib/msgp v1.6.4 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	github.com/xdg-go/pbkdf2 v1.0.0 // indirect
	github.com/xdg-go/scram v1.1.2 // indirect
	github.com/xdg-go/stringprep v1.0.4 // indirect
	github.com/xuri/efp v0.0.1 // indirect
	github.com/xuri/excelize/v2 v2.10.1 // indirect
	github.com/xuri/nfp v0.0.2-0.20250530014748-2ddeb826f9a9 // indirect
	github.com/youmark/pkcs8 v0.0.0-20240726163527-a2c0da244d78 // indirect
	github.com/yuin/goldmark v1.7.17 // indirect
	github.com/yuin/gopher-lua v1.1.1 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.43.0 // indirect
	go.opentelemetry.io/otel/log v0.19.0 // indirect
	go.opentelemetry.io/otel/metric v1.43.0 // indirect
	go.opentelemetry.io/proto/otlp v1.10.0 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	go.yaml.in/yaml/v2 v2.4.2 // indirect
	golang.org/x/exp v0.0.0-20220921023135-46d9e7742f1e // indirect
	golang.org/x/mod v0.36.0 // indirect
	golang.org/x/net v0.55.0 // indirect
	golang.org/x/oauth2 v0.36.0 // indirect
	golang.org/x/sync v0.20.0 // indirect
	golang.org/x/sys v0.45.0 // indirect
	golang.org/x/text v0.37.0 // indirect
	golang.org/x/time v0.15.0 // indirect
	google.golang.org/appengine v1.6.8 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260401024825-9d38bb4040a9 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260401024825-9d38bb4040a9 // indirect
	google.golang.org/grpc v1.80.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
	gotest.tools/v3 v3.5.2 // indirect
)
