# Core `retrieval` module — Implementation Roadmap (ADR-0023, delivery step 1)

> **For agentic workers:** this file is not executed task by task. It fixes the
> order of the PR plans below and the contract they share. Every PR plan
> implements against the types and names here **verbatim**; a PR that needs a
> change to this contract changes this file in the same PR and says so in its
> description.

**Goal:** deliver step 1 of [ADR-0023](../../adr/0023-core-retrieval-module.md):
a core module that indexes fork data through an outbox and answers hybrid
searches filtered by tenant and principals, with local inference, durable
erasure and an admin surface.

**Spec:** `docs/adr/0023-core-retrieval-module.md` (status Proposed when this
roadmap was written; the PRs merge only after it is Accepted).

**Tech stack:** Go 1.26.10, Huma v2, MongoDB 8.0 (`mongo-driver v1.17.7`,
replica set), Qdrant ≥ 1.20 over its **REST API** (hand-written client on
`net/http`, like the `ollama` and `openai_compatible` adapters; no gRPC and no
`qdrant/go-client` dependency), Hugging Face TEI (CPU image), React 19 console.

## Delivery order

| # | Plan file | Delivers | Depends on |
|---|---|---|---|
| 1 | `2026-10-26-retrieval-01-llm-installation-inference.md` | `llm`: installation-level endpoints from env, `tei` provider, inference profiles, `LLMPinnedEmbedder`, `LLMReranker`, `LLMInferenceProfiles` | — |
| 2 | `2026-10-26-retrieval-02-compliance-erasure-incomplete.md` | ADR-0009 amendment: `PurgeResult.Pending`, erasure requests `incomplete` with durable retries | — |
| 3 | `2026-10-26-retrieval-03-module-and-engine.md` | `pkg/sdk/iface/retrieval.go`, module skeleton in the core catalog, corpus registry and filter schema, `engine` interface, Qdrant REST driver, driver conformance suite, Qdrant in CI, legacy RAG types deprecated | — |
| 4 | `2026-10-26-retrieval-04-indexing.md` | outbox, per-key leases and epochs, worker, chunking `none`/`fixed`, tombstone store and write refusal, initial build of generation 1 from `Enumerate` | 1, 3 |
| 5 | `2026-10-26-retrieval-05-search.md` | `iface.AuthzRoleLister`, principal resolver and contributors, filter compilation, the 8-step pipeline, erasure veto, `Visible`, rerank, degraded mode, search log writes | 1, 3, 4 |
| 6 | `2026-10-26-retrieval-06-erasure-and-dsr.md` | finalization sweep, `PIIProducer` (global user tombstone, search-log export/erase, pending), `DeleteBySubject`, tenant purge through `iface.TenantPurgeHooks`, compliance audit of admin actions | 2, 4, 5 |
| 7 | `2026-10-26-retrieval-07-admin-surface.md` | permissions, Cedar, D15 routes, seeding-matrix test, `/admin/retrieval` console page and search-log view | 5, 6 |
| 8 | `2026-10-26-retrieval-08-deployment-and-benchmark.md` | `docker-compose.retrieval.yml`, `./orkestra.sh retrieval …`, init/env-validate, docs site page, D14 benchmark harness | 1, 3 (benchmark: 5) |

Plans 1, 2 and 3 can run in parallel. 8's overlay part can start after 1 and 3;
its benchmark part needs 5. Delivery steps 2 (reconciliation, generations with
rebuild and swap, metrics, golden sets) and 3 (document seam, hierarchical
chunking, contextual enrichment) get their own roadmaps later.

## Global constraints

Copied from the ADR and the repo rules; every task in every PR includes them.

- `Prop: upstream` trailer on every commit (ADR-0010); Conventional Commits; `Co-Authored-By` line as configured.
- No method is added to an existing `iface` interface; new capabilities are new interfaces, obtained by type assertion on the registered service (SDK rule).
- No module imports another module's `services/` or `repository/`; cross-module access goes through `pkg/sdk/iface` and the service registry.
- Every tenant-scoped collection is read and written through `pkg/sdk/tenantrepo` (`Scope`, `StampInsert`); cross-tenant sweeps carry `//tenantscope:allow system: <reason>`, data-subject paths `//tenantscope:allow dsr: <reason>`.
- Search and indexing entry points refuse tenants that are not internal (`tenantrepo.RequireInternalTenant`); data-subject operations are the exception (ADR D13).
- `Init` of `retrieval` never contacts Qdrant or an inference endpoint (ADR D1). A core `Init` error is fatal, so missing infrastructure is a WARN, never an error.
- Qdrant ≥ 1.20, single node, API key, internal network only. Collection name `<prefix>_<corpus>_g<generation>`; payload namespaces `_rt` (module-only) and `f` (declared filter fields).
- Reference models: `BAAI/bge-m3` (MIT, dense 1024) and `BAAI/bge-reranker-v2-m3` (Apache-2.0), pinned by revision; images pinned by digest.
- Corpus default `locality: local_only`; rerank off by default.
- No bypass of the principal filter for any platform role (ADR D10).
- Error responses follow `shared/errcode` (`retrieval.<situation>`, snake_case); 5xx details never carry `err.Error()` (errquality).
- Huma schema names are package-less: every HTTP-visible type in `retrieval` carries the `Retrieval` prefix.
- Docs move with code: each PR updates the AGENTS.md files and `docs/site` pages it makes stale (root `AGENTS.md` module table and load order, `backend/AGENTS.md`, `backend/internal/core/retrieval/AGENTS.md`, `docs/site/modules/core/*.mdx`, `docker/AGENTS.md`).
- Verification before every commit: `cd backend && go test ./<touched packages>/...`; before the PR: `make ci-backend` (and `make ci-frontend-admin` for PR 7). Mongo tests need `MONGO_TEST_URI` (replica set); Qdrant tests need `QDRANT_TEST_URL` and `QDRANT_TEST_API_KEY`; a green run without them proves nothing about those paths.

## Review Focus (whole feature)

Inputs the ADR implies that the step most likely gets wrong; each PR plan pins
the ones it owns with a test.

1. **A stale worker resuming after its successor wrote** (PR 3 conformance, PR 4): the item ends at the newest revision, and anything the stale write left is dropped on read and removed after re-queue.
2. **A record made private between indexing and query** (PR 5): `Visible` drops it before rerank and before any text is returned; a `Visible` error or timeout fails the search closed.
3. **A user erased while points still exist in the engine** (PR 4, 5, 6): from the tombstone write on, no search returns those points and no worker, rebuild or replay writes them again.
4. **Tenant A's documents shifting tenant B's keyword ranking** (PR 3 conformance): B's BM25 scores are identical with and without A's data.
5. **An inference endpoint replaced under a running corpus** (PR 1, 5): a different model or revision is detected as a profile mismatch, the dense arm is not used, and the search answers keyword-only with `degraded: true`.

## Shared contract

### SDK — `pkg/sdk/iface/llm_installation.go` (PR 1)

```go
// Installation-level inference (ADR-0023 D4): endpoints declared by the
// operator in RETRIEVAL_INFERENCE_ENDPOINTS, served only to the retrieval
// module. LLMGateway is unchanged; the registered gateway also implements
// the three interfaces below (assert them on the resolved LLMGateway).

type InferenceKind string

const (
	InferenceKindEmbed  InferenceKind = "embed"
	InferenceKindRerank InferenceKind = "rerank"
	InferenceKindChat   InferenceKind = "chat" // parsed and listed; served from delivery step 3
)

type InferenceBoundary string

const (
	InferenceBoundaryLocal  InferenceBoundary = "local"
	InferenceBoundaryHosted InferenceBoundary = "hosted"
)

type EmbedInputKind string

const (
	EmbedInputQuery    EmbedInputKind = "query"
	EmbedInputDocument EmbedInputKind = "document"
)

// InferenceProfile is the immutable identity of what an endpoint computes.
// ID = "ip_" + hex(sha256(canonical JSON of Kind, ModelID, Revision, Pooling,
// Normalize, Truncate, QueryPrefix, DocumentPrefix)); the endpoint name and
// URL are not part of it, so moving a model to another host keeps its ID.
type InferenceProfile struct {
	ID             string            `json:"id"`
	Endpoint       string            `json:"endpoint"`
	Kind           InferenceKind     `json:"kind"`
	Boundary       InferenceBoundary `json:"boundary"`
	ModelID        string            `json:"modelId"`
	Revision       string            `json:"revision"`
	Dimensions     int               `json:"dimensions,omitempty"` // embed: measured at verification
	MaxInputTokens int               `json:"maxInputTokens,omitempty"`
	Pooling        string            `json:"pooling,omitempty"`
	Normalize      bool              `json:"normalize"`
	Truncate       bool              `json:"truncate"`
	QueryPrefix    string            `json:"queryPrefix,omitempty"`
	DocumentPrefix string            `json:"documentPrefix,omitempty"`
}

type LLMInferenceProfiles interface {
	// InferenceProfile returns the profile the named endpoint serves, verified
	// against what the endpoint reports. Unknown name: ErrLLMNotConfigured;
	// endpoint reachable but serving another model or revision:
	// ErrLLMProfileMismatch; unreachable: ErrLLMProviderUnavailable.
	InferenceProfile(ctx context.Context, endpoint string) (*InferenceProfile, error)
}

type PinnedEmbedRequest struct {
	Caller     string         // must be "retrieval"
	OnBehalfOf string         // org billed; must equal the tenant in ctx
	Profile    string         // InferenceProfile.ID; refused if the endpoint now serves another
	Kind       EmbedInputKind // selects QueryPrefix or DocumentPrefix
	Inputs     []string
}

type LLMPinnedEmbedder interface {
	EmbedWithProfile(ctx context.Context, req PinnedEmbedRequest) (*EmbedResult, error)
}

type RerankRequest struct {
	Caller     string
	OnBehalfOf string
	Profile    string
	Query      string
	Documents  []string
	TopN       int // 0 = all
}

type RerankScore struct {
	Index int     `json:"index"`
	Score float32 `json:"score"`
}

type RerankResult struct {
	Results []RerankScore `json:"results"` // ordered by Score descending
	Profile string        `json:"profile"`
	Usage   LLMUsage      `json:"usage"`
}

type LLMReranker interface {
	Rerank(ctx context.Context, req RerankRequest) (*RerankResult, error)
}

var ErrLLMProfileMismatch = errors.New("llm: the endpoint no longer serves this inference profile")
```

Environment (read by `llm` Init with `os.Getenv`, like `LLM_SECRET_ENCRYPTION_KEY`):

```
RETRIEVAL_INFERENCE_ENDPOINTS=[{"name":"embed","url":"http://tei-embed:80","kind":"embed","boundary":"local","model":"BAAI/bge-m3","revision":"<sha>","pooling":"cls","normalize":true,"truncate":true},{"name":"rerank","url":"http://tei-rerank:80","kind":"rerank","boundary":"local","model":"BAAI/bge-reranker-v2-m3","revision":"<sha>","truncate":true}]
RETRIEVAL_INFERENCE_API_KEY=<shared bearer key; an endpoint may name another variable in "apiKeyEnv">
```

Malformed JSON, a duplicate name or an invalid field: WARN naming the field,
and that endpoint (or all of them for malformed JSON) is absent. Never fatal.

Ledger: one `llm_usage` row per call with `caller = "retrieval"`, `purpose =
"retrieval.embed" | "retrieval.rerank"`, `modelUuid = "installation:<endpoint>"`,
`provider = "tei"`, `credentialKind = "installation"` (new constant
`models.CredentialKindInstallation`), billed to `OnBehalfOf`.

### SDK — `PurgeResult` (PR 2)

```go
type PurgeResult struct {
	RowsDeleted    int      `json:"rowsDeleted"`
	RowsAnonymized int      `json:"rowsAnonymized"`
	Collections    []string `json:"collections,omitempty"`
	// Pending is true when the producer accepted the erasure but has not
	// finished it (ADR-0023 D12). The request stays incomplete and the
	// producer is called again until it reports Pending false.
	Pending bool `json:"pending,omitempty"`
}
```

Erasure request status gains `incomplete` (`models.ErasureRequestIncomplete`).
`PurgePersonalData` must be idempotent: compliance calls it again on retry.

### SDK — `pkg/sdk/iface/retrieval.go` (PR 3)

```go
type RetrievalLocality string

const (
	RetrievalLocalOnly     RetrievalLocality = "local_only"
	RetrievalHostedAllowed RetrievalLocality = "hosted_allowed"
)

type RetrievalFieldType string

const (
	RetrievalFieldKeyword  RetrievalFieldType = "keyword"
	RetrievalFieldKeywords RetrievalFieldType = "keyword[]"
	RetrievalFieldInteger  RetrievalFieldType = "integer"
	RetrievalFieldFloat    RetrievalFieldType = "float"
	RetrievalFieldBool     RetrievalFieldType = "bool"
	RetrievalFieldDatetime RetrievalFieldType = "datetime" // RFC 3339 in, stored as UTC
)

type RetrievalFilterField struct {
	Name        string             `json:"name"` // ^[a-z][a-zA-Z0-9_]{0,63}$
	Type        RetrievalFieldType `json:"type"`
	Description string             `json:"description"`
}

type RetrievalAnalyzer struct {
	Tokenizer string `json:"tokenizer"` // "word" (default) | "multilingual"
	Language  string `json:"language"`  // snowball language for stemming and stop words; "" = none
	Stemming  bool   `json:"stemming"`
	StopWords bool   `json:"stopWords"`
}

type RetrievalChunking struct {
	Strategy  string `json:"strategy"`  // "none" | "fixed"; "hierarchical" from step 3
	MaxTokens int    `json:"maxTokens"` // fixed only
	Overlap   int    `json:"overlap"`   // fixed only, < MaxTokens
}

type RetrievalEnrichment struct { // delivery step 3; must be nil in step 1
	ChatEndpoint  string `json:"chatEndpoint"`
	PromptVersion string `json:"promptVersion"`
}

type RetrievalIngestionProfile struct {
	EmbedEndpoint string               `json:"embedEndpoint"` // installation endpoint name, kind embed
	Analyzer      RetrievalAnalyzer    `json:"analyzer"`
	Chunking      RetrievalChunking    `json:"chunking"`
	Enrichment    *RetrievalEnrichment `json:"enrichment,omitempty"`
}

type RetrievalFusion struct {
	DenseWeight   float64 `json:"denseWeight"`   // 0 < w <= 10
	KeywordWeight float64 `json:"keywordWeight"` // 0 < w <= 10
	Candidates    int     `json:"candidates"`    // per arm, 1–500
}

type RetrievalRerank struct {
	Endpoint string `json:"endpoint"` // installation endpoint name, kind rerank
	Enabled  bool   `json:"enabled"`
	TopK     int    `json:"topK"` // 1–200
}

type RetrievalCorpusSpec struct {
	Name       string // ^[a-z][a-z0-9_]{0,31}$, unique per installation
	Owner      string // owning module name
	Permission string // org permission the caller must hold
	Filters    []RetrievalFilterField
	Ingestion  RetrievalIngestionProfile
	Locality   RetrievalLocality // "" = local_only
	Fusion     RetrievalFusion   // zero = module defaults
	Rerank     RetrievalRerank
	Source     RetrievalSource
}

type RetrievalItem struct {
	SourceID    string
	Revision    int64 // monotonic per item
	Title       string
	Text        string
	Fields      map[string]any // keys must be declared filter fields
	VisibleTo   []string       // "tenant:*", "role:<roleId>", "team:<teamId>", "user:<uuid>"
	Subjects    []string       // "user:<uuid>" or "<owner>:<id>"
	ContentHash string         // over the source content; the module hashes the indexed text separately
}

type RetrievalLoaded struct {
	SourceID string
	Deleted  bool
	Item     *RetrievalItem // nil when Deleted
}

type RetrievalEnumerated struct {
	SourceID    string
	Revision    int64
	ContentHash string
}

type RetrievalPage struct {
	Items []RetrievalEnumerated
	Next  string // "" at the end
}

type RetrievalPrincipalContext struct {
	TenantID   string
	UserUUID   string
	Principals []string
}

type RetrievalVisibility struct {
	Visible  bool
	Revision int64
}

// RetrievalSource is implemented by the owning module. Every call is scoped
// to the tenant passed in; the ctx carries that tenant too.
type RetrievalSource interface {
	Load(ctx context.Context, tenantID string, ids []string) ([]RetrievalLoaded, error)
	Enumerate(ctx context.Context, tenantID, cursor string, limit int) (RetrievalPage, error)
	// Visible answers for every id; an id missing from the map is "not visible".
	Visible(ctx context.Context, tenantID string, pc RetrievalPrincipalContext, ids []string) (map[string]RetrievalVisibility, error)
}

type RetrievalPrincipalContributor interface {
	Name() string
	Principals(ctx context.Context, tenantID, userUUID string) ([]string, error)
}

type RetrievalCorpusRegistry interface {
	RegisterCorpus(spec RetrievalCorpusSpec) error // during any module's Init; closed when retrieval starts (Seal); never contacts the engine
	RegisterPrincipalContributor(c RetrievalPrincipalContributor) error
}

type RetrievalIntent struct {
	Corpus   string
	SourceID string
	Revision int64
}

type RetrievalOutbox interface {
	// Enqueue records intents for the tenant in ctx. Call it with the
	// mongo.SessionContext of the transaction that writes the source.
	Enqueue(ctx context.Context, intents ...RetrievalIntent) error
}

type RetrievalOp string

const (
	RetrievalOpEq     RetrievalOp = "eq"
	RetrievalOpIn     RetrievalOp = "in"
	RetrievalOpLt     RetrievalOp = "lt"
	RetrievalOpLte    RetrievalOp = "lte"
	RetrievalOpGt     RetrievalOp = "gt"
	RetrievalOpGte    RetrievalOp = "gte"
	RetrievalOpExists RetrievalOp = "exists"
)

// RetrievalFilter is a node: exactly one of And, Or, Not, or a comparison
// (Field + Op + Value/Values). Limits: depth <= 4, <= 64 nodes, <= 100 Values.
type RetrievalFilter struct {
	And    []RetrievalFilter `json:"and,omitempty"`
	Or     []RetrievalFilter `json:"or,omitempty"`
	Not    *RetrievalFilter  `json:"not,omitempty"`
	Field  string            `json:"field,omitempty"`
	Op     RetrievalOp       `json:"op,omitempty"`
	Value  any               `json:"value,omitempty"`
	Values []any             `json:"values,omitempty"`
}

type RetrievalMode string

const (
	RetrievalModeHybrid  RetrievalMode = "hybrid" // "" means hybrid
	RetrievalModeDense   RetrievalMode = "dense"
	RetrievalModeKeyword RetrievalMode = "keyword"
)

type RetrievalSearchRequest struct {
	Corpus string
	Query  string // 1–2000 runes
	Filter *RetrievalFilter
	Mode   RetrievalMode
	Limit  int              // 1–100, default 10
	Fusion *RetrievalFusion // optional per-request weights within the bounds above
}

type RetrievalProvenance struct {
	SourceID  string `json:"sourceId"`
	ChunkID   string `json:"chunkId"`
	Ordinal   int    `json:"ordinal"`
	Page      int    `json:"page,omitempty"`
	CharStart int    `json:"charStart"`
	CharEnd   int    `json:"charEnd"`
}

type RetrievalHit struct {
	SourceID   string              `json:"sourceId"`
	ChunkID    string              `json:"chunkId"`
	Rank       int                 `json:"rank"`
	Score      float32             `json:"score"`
	Snippet    string              `json:"snippet"`
	Provenance RetrievalProvenance `json:"provenance"`
	Locality   RetrievalLocality   `json:"locality"`
}

type RetrievalSearchResult struct {
	Hits     []RetrievalHit `json:"hits"`
	Degraded bool           `json:"degraded"`
}

type RetrievalSearch interface {
	Search(ctx context.Context, req RetrievalSearchRequest) (*RetrievalSearchResult, error)
	FilterSchema(corpus string) ([]RetrievalFilterField, error)
	DeleteBySubject(ctx context.Context, tenantID, subject string) error
}

type RetrievalPrincipalResolver interface {
	Principals(ctx context.Context) (RetrievalPrincipalContext, error)
}

var (
	ErrRetrievalNotConfigured     = errors.New("retrieval: the vector engine is not configured")
	ErrRetrievalUnavailable       = errors.New("retrieval: the vector engine is unavailable")
	ErrRetrievalCorpusUnavailable = errors.New("retrieval: the corpus's owning module is not running")
	ErrRetrievalUnknownCorpus     = errors.New("retrieval: unknown corpus")
	ErrRetrievalInvalidSpec       = errors.New("retrieval: invalid corpus declaration")
	ErrRetrievalInvalidFilter     = errors.New("retrieval: invalid filter")
	ErrRetrievalInvalidRequest    = errors.New("retrieval: invalid search request")
	ErrRetrievalForbidden         = errors.New("retrieval: the caller may not search this corpus")
	ErrRetrievalLocality          = errors.New("retrieval: an inference endpoint violates the corpus locality")
	ErrRetrievalVisibilityCheck   = errors.New("retrieval: the source visibility check failed")
)
```

Service key, in `pkg/sdk/module/services.go`:
`ServiceRetrieval ServiceKey = "retrieval.service"`. The registered value is one
`*services.Service` that implements `RetrievalCorpusRegistry`, `RetrievalOutbox`,
`RetrievalSearch` and `RetrievalPrincipalResolver`; consumers resolve each with
`module.GetTyped[iface.<Interface>](reg, module.ServiceRetrieval)`.

### SDK — narrow interfaces on other core modules

```go
// PR 5 — implemented by authz's *services.Service, resolved with
// module.GetTyped[iface.AuthzRoleLister](reg, module.ServiceAuthzProvider)
// (precedent: AuthzCacheInvalidator).
type AuthzRoleLister interface {
	// ActiveRoleIDs lists the UUIDs of active roles bound to the user in the
	// tenant through non-expired bindings. tenantID "" is refused.
	ActiveRoleIDs(ctx context.Context, userUUID, tenantID string) ([]string, error)
}

// PR 6 — implemented by tenant's *services.Service, resolved with
// module.GetTyped[iface.TenantPurgeHooks](reg, module.ServiceTenantService).
type TenantPurgeHooks interface {
	// RegisterTenantPurgeHook runs fn after PurgeTenant (hard purge only),
	// best-effort like the other post-delete hooks.
	RegisterTenantPurgeHook(fn func(ctx context.Context, tenantUUID string) error)
}
```

### Module — `backend/internal/core/retrieval/`

Layout (each PR adds its files):

```
retrieval/
├── AGENTS.md
├── module.go                 # PR 3 (extended by every PR)
├── routes.go                 # PR 7
├── engine/                   # PR 3: Engine interface + types (no Qdrant import)
│   └── qdrant/               # PR 3: REST driver
├── enginetest/               # PR 3: conformance suite, run against any Engine
├── filter/                   # PR 3: validation; PR 5: compilation to engine.Filter
├── chunk/                    # PR 4
├── models/                   # PR 3+: Mongo documents
├── repository/               # PR 3+: Mongo access
├── services/                 # registry, outbox, worker, search, principals, erasure, PII
└── handlers/                 # PR 7
```

Load order (pinned by `cmd/server/core_load_order_test.go`, PR 3):
`user → notification → tenant → authz → auth → navigation → logging → llm → retrieval → compliance`.
`Dependencies()`: `tenant`, `authz`, `llm`. Name `retrieval`, `CategoryCore`.

Environment read by `retrieval` Init: `QDRANT_URL`, `QDRANT_API_KEY`,
`QDRANT_COLLECTION_PREFIX` (default `orkestra`). `QDRANT_URL` empty → search
answers `ErrRetrievalNotConfigured`, everything else of D1's table still works.

### Internal engine contract — `retrieval/engine/engine.go` (PR 3)

Consumed by PRs 4–7. The conformance suite in `enginetest` is the definition of
each method's behaviour.

```go
type Engine interface {
	Health(ctx context.Context) error
	EnsureCollection(ctx context.Context, spec CollectionSpec) error // idempotent; creates payload and full-text indexes
	DropCollection(ctx context.Context, name string) error
	// Upsert writes points; a point whose stored _rt.sourceRevision is higher
	// than the incoming one is left unchanged (conditional update).
	Upsert(ctx context.Context, collection string, points []Point) error
	// DeleteOtherRevisions removes the item's points whose revision differs from keep.
	DeleteOtherRevisions(ctx context.Context, collection, tenantID, sourceID string, keep int64) error
	DeleteByFilter(ctx context.Context, collection string, sel Selector) error
	Count(ctx context.Context, collection string, sel Selector) (int64, error)
	Query(ctx context.Context, collection string, q Query) ([]Candidate, error)
}

type CollectionSpec struct {
	Name       string
	Dimensions int
	Fields     []iface.RetrievalFilterField
	Analyzer   iface.RetrievalAnalyzer
}

type Runtime struct { // payload namespace _rt
	TenantID         string   `json:"tenantId"`
	SourceID         string   `json:"sourceId"`
	SourceRevision   int64    `json:"sourceRevision"`
	ChunkID          string   `json:"chunkId"`
	Ordinal          int      `json:"ordinal"`
	VisibleTo        []string `json:"visibleTo"`
	Subjects         []string `json:"subjects"`
	ContentHash      string   `json:"contentHash"` // over the indexed text
	IngestionProfile string   `json:"ingestionProfile"`
	CharStart        int      `json:"charStart"`
	CharEnd          int      `json:"charEnd"`
	Page             int      `json:"page,omitempty"`
}

type Point struct {
	ID      string         // PointID(tenantID, corpus, sourceID, ordinal)
	Dense   []float32      // nil: keyword-only write is refused (every point has a vector)
	Text    string         // indexed text; feeds BM25 and the snippet
	Runtime Runtime
	Fields  map[string]any // namespace f
}

// PointID is UUIDv5 over "tenantID\x00corpus\x00sourceID\x00ordinal" in a fixed namespace.
func PointID(tenantID, corpus, sourceID string, ordinal int) string

type Selector struct { // all set fields are ANDed; at least one must be set
	TenantID  string
	SourceIDs []string
	Subject   string // matches _rt.subjects
}

type Query struct {
	TenantID   string   // mandatory; also scopes BM25 statistics
	Principals []string // mandatory, non-empty; _rt.visibleTo must intersect
	Filter     *Filter  // compiled user filter over namespace f; nil = none
	Dense      []float32
	Keyword    string
	Weights    Weights  // RRF weights; an arm with no input is skipped
	PerArm     int
	Limit      int
	Diagnostics bool    // fill per-arm scores (admin diagnostics only)
}

type Weights struct{ Dense, Keyword float64 }

type Filter struct { // compiled form, produced only by retrieval/filter
	Must, Should, MustNot []Filter
	Field string
	Match []any   // eq / in
	Range *Range  // lt, lte, gt, gte (numbers and datetimes)
	Exists bool
}

type Range struct{ Lt, Lte, Gt, Gte *float64 }

type Candidate struct {
	PointID      string
	Runtime      Runtime
	Text         string
	Fields       map[string]any
	Score        float32  // fused
	DenseScore   *float32 // only with Diagnostics
	KeywordScore *float32 // only with Diagnostics
}

var (
	ErrUnavailable = errors.New("engine: unavailable")       // network, 5xx, timeout
	ErrNotFound    = errors.New("engine: collection not found")
)
```

How the keyword arm is built (full-text index BM25 or server-side BM25 sparse
vector) is the driver's choice (ADR "Left to the implementer"), as long as the
conformance suite passes.

### Collections (owner PR → readers)

| Collection | Scope | Created in | Shape owner | Read/written by |
|---|---|---|---|---|
| `retrieval_corpus_state` | installation (`_id` = corpus) | PR 3 | PR 4 | 4, 5, 7 |
| `retrieval_tenant_progress` | tenant | PR 4 | PR 4 | 4, 7 |
| `retrieval_outbox` | tenant | PR 4 | PR 4 | 4, 7 |
| `retrieval_erasures` | global (user) + tenant | PR 4 | PR 4 (fields below), PR 6 adds finalization fields | 4, 5, 6, 7 |
| `retrieval_search_log` | tenant | PR 5 | PR 5 | 5, 6, 7 |

`retrieval_erasures` document (PR 4):

```go
type Erasure struct {
	UUID      string    `bson:"uuid"`
	Scope     string    `bson:"scope"`              // "global" | "tenant"
	TenantID  string    `bson:"tenantId,omitempty"` // absent for global
	Kind      string    `bson:"kind"`               // "user" | "subject" | "item" | "tenant"
	Corpus    string    `bson:"corpus,omitempty"`   // item
	SourceID  string    `bson:"sourceId,omitempty"` // item
	Subject   string    `bson:"subject,omitempty"`  // user ("user:<uuid>") and subject
	CreatedAt time.Time `bson:"createdAt"`
	// PR 6: Status ("pending"|"fencing"|"done"), FirstPassAt, FenceUntil,
	// DoneAt, Attempts, NextAttemptAt.
}
```

Tombstone check (PR 4, `services/tombstones.go`), consumed by the worker (PR 4),
the read path (PR 5) and finalization (PR 6):

```go
type Probe struct {
	SourceID string
	Subjects []string
}

// Covered reports, per SourceID, whether any tombstone covers it: global user
// tombstone on any subject, tenant tombstone for the tenant, item tombstone
// for (tenant, corpus, sourceID), subject tombstone for (tenant, subject).
func (t *Tombstones) Covered(ctx context.Context, tenantID, corpus string, probes []Probe) (map[string]bool, error)
```

### Error codes, permissions, routes

Owned by PR 7 except where noted; listed here so the earlier PRs name things
consistently. Permissions declared in step 1 (only those with a route, as
policycoverage requires): `retrieval.corpus.read`, `retrieval.corpus.admin`
(System); `retrieval.tenant.read`, `retrieval.searchlog.admin` (org).
`retrieval.tenant.admin` arrives with reconciliation in delivery step 2.

## Contract additions settled by the PR plans

The PR plans were written in parallel against the contract above. These
additions and pins were reconciled afterwards; they are binding like the rest
of this file.

| Addition | Owner PR | Consumers |
|---|---|---|
| `iface.PIIProducerKeyer { ProducerKey() string }` (optional; auth keys its two producers `auth.operator` / `auth.client`); DSR maps keyed by producer key | 2 | 6 |
| Erasure request status `closed_partial` (operator closes an `incomplete` request only when every producer it still waits for is no longer registered; `POST /v1/admin/compliance/erasure-requests/{id}/close`, note required, audited); fields `abandoned`, `closable`. Export bundle keyed by producer key too, `metadata.version` `"2"` | 2 | — |
| `iface.TenantIDLister { ListActiveTenantIDs(ctx, kind string) ([]string, error) }` on tenant's `*services.Service`, resolved from `ServiceTenantService` | 4 | 4 |
| `module.ServiceModuleStatus` + `module.ModuleStatusReader { IsStarted(name string) bool }` (the `*ModuleRegistry`, registered in `main.go`): the single "owning module started" check for indexing **and** search | 4 | 4, 5, 7 |
| `iface.ErrRetrievalInvalidIntent` | 4 | 4 |
| `engine.Engine.UpdatePayload` (payload-only update when the indexed-text hash and profile are unchanged; never inserts, never lowers a revision), with conformance cases | 4 | 4 |
| `engine.CollectionName(prefix, corpus string, generation int) string`, `Selector.IsZero()`, `qdrant.New(Config)`; indexed text stored at payload `_rt.text` | 3 | 4–8 |
| Value representation at the engine boundary: `keyword` string, `keyword[]` []string, `integer` int64, `float` float64, `bool`, **`datetime` = Unix milliseconds UTC** (`int64` in `Point.Fields`, `float64` in `engine.Range`, `integer` payload index); `engine.Filter.Field` is the bare declared name, the driver adds `f.` | 3 | 4, 5 |
| Op/type table of `filter.Validate`: keyword and keyword[] `eq in exists`; integer `eq in lt lte gt gte exists`; float and datetime `lt lte gt gte exists`; bool `eq exists` | 3 | 5 |
| Registry accessors on `services.Service`: `Corpus(name)`, `Corpora()`, `PrincipalContributors()`, `FilterSchema`, `Engine()`, `CollectionPrefix()`, `Seal()`; defaults `DefaultFusion = {1, 1, 50}`, `DefaultRerankTopK = 20` applied at registration | 3 | 4–7 |
| `retrieval_corpus_state` document `models.CorpusState` (`_id` corpus, owner, status `active\|profile_changed\|error`, reason, `activeGeneration` (0 = none), collection, `ingestionProfileHash`, `embeddingProfileId`, dimensions, locality, `declaredProfileHash`, version CAS) and `retrieval_tenant_progress` `models.TenantProgress` | 4 | 5, 6, 7 |
| Config keys `worker_lease` (2m, 30s–15m), `worker_batch` (32), `worker_poll_interval` (5s); `services.MaxWorkerLease` = 15m; every engine write ends at `leaseUntil − lease/4` | 4 | 6 |
| Erasure fencing window: `FencingWindow(MaxWorkerLease, 30s)` = `clamp(MaxWorkerLease + 30s + 30s, 1m, 1h)` = 16m, independent of the live `worker_lease`; derived, no config key. `services.IndexingFencingWindow` is deleted | 6 | 6 |
| `retrieval_search_log` document `models.SearchLog` (uuid, tenantId, userUuid, corpus, query, filter, mode, principals, sourceIds, subjects, hitCount, degraded, outcome, errorKind, timings, createdAt); a failed log write fails the search closed (wraps `ErrRetrievalUnavailable`); config `principal_cap` (256), `search_log_retention_days` (90) | 5 | 6, 7 |
| A short page after drops is reported with `Degraded: true` (no separate flag) | 5 | 7 |
| `(*Service).Diagnose`: the normal pipeline with `engine.Query.Diagnostics` set, admin's own principals, same `Visible` check and search-log row | 7 | 7 |
| `DeleteBySubject` returned a sentinel wrapping `errors.ErrUnsupported` in PR 5; PR 6 replaced it (`ErrDeleteBySubjectNotImplemented` is deleted) | 5, 6 | 6 |
| Exported in-memory engine `retrieval/engine/enginefake` | 6 | 6 (PR 4 and 5 keep their package-private fakes: PR 4 `fake*`, PR 5 `search*`) |
| Audit: retrieval's own events through PR 6's `AuditRelay` (late-wired by compliance via `iface.AuditSinkSetter` on `ServiceRetrieval`); PR 7's admin events resolve `ServiceAuditSink` per call. Both reach the same sink | 6, 7 | — |
| `iface.PurgedTenantIDLister` (lists purged tenant ids; tenant's `*services.Service`) beside `iface.TenantPurgeHooks`, for the purge reconcile loop | 6 | 6 |
| Finalizer stage 0 (search-log delete by kind on every pass, before the engine work), with `SearchLogDSRRepo` | 6 | 6 |
| Search-log post-write re-check: `SearchLogRetractor` (`DeleteRow`, `MarkChecked`) used by `Searcher.retract`, one tombstone lookup per logged search; rows are inserted `unchecked`, settled by the re-check, and a row left unchecked (failed re-check, delete or mark) is swept by the retention run after `UncheckedSearchLogGrace` (10 min) via `PurgeUncheckedBefore` and the partial `{unchecked, createdAt}` index | 6 | 7 |
| `services.ErasureBackoff(attempts)` = 15s·2^(n−1), capped at 15m | 6 | 6 |
| `services.ValidateOwnerSubject`: `<owner>:<id>`, owner matches the corpus owner shape and is a registered owner, at most 256 bytes, no `user` owner | 6 | 7 |
| `retrieval/AGENTS.md` and `docs/site/modules/core/retrieval.mdx` are created by PR 3; later PRs add sections | 3 | 4–8 |
| **Implementation rulings (PRs 1–3 executed):** `engine.Selector` fails closed — a non-nil empty `SourceIDs`, an empty id, or `SourceIDs` without `TenantID` is refused by `DeleteByFilter`/`Count` (a `Subject`-only selector stays tenant-less for global user erasure) | 3 | 4, 6 |
| The Qdrant driver refuses malformed filter nodes, drops (and WARN-counts) any candidate outside the query's tenant/principals, treats a blank keyword as an absent arm; `QDRANT_COLLECTION_PREFIX` matches `^[a-z][a-z0-9-]{0,31}$` (no `_`, so names cannot collide) | 3 | 5, 8 |
| A payload-only update must merge into `_rt` with Qdrant `set_payload` `"key": "_rt"` and send every `_rt` field: the indexed text lives at `_rt.text` | 3 | 4 |
| Compliance counts an erasure attempt when a run **claims** the request (Claim → 1, ClaimDue `$inc`, EraseNow inserts 1), producer panics are recovered as `failed`, Settle is bounded by the lease; `NewErasureRequestService(repo, dsr, sink, logger)`; a `Pending` producer is retried with backoff and exhausts after 20 attempts (~3 days) | 2 | 6 |
| Installation inference: an unknown but well-formed profile ID is `ErrLLMProfileMismatch` (search degrades); a caller other than `retrieval` is `ErrLLMModelAccessDenied`; `OnBehalfOf` ≠ ctx tenant is `ErrLLMInvalidRequest`; duplicate endpoint names or profile IDs drop every endpoint involved; the worker's ctx must carry tenant kind `internal`; indexing ledger rows have no `userUuid` | 1 | 4, 5 |

## Not in step 1

Reconciliation sweep, generation rebuild and swap (a changed ingestion profile
in step 1 marks the corpus `profile_changed` and keeps serving the active
generation), metrics of D14, golden sets, `iface.DocumentParser`, hierarchical
chunking, contextual enrichment, dev-slot wiring in the octolabs-worktree kit
and in the forks.
