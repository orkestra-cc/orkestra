# Retrieval PR 3 — SDK contract, module skeleton, engine and Qdrant driver — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** add `pkg/sdk/iface/retrieval.go`, a tenth core module `retrieval` (catalogued between `llm` and `compliance`) with its corpus registry and filter validation, the engine-neutral `engine` interface, a hand-written Qdrant REST driver, the driver conformance suite running against a pinned Qdrant in CI, and the D16 deprecation of the legacy RAG contract.

**Architecture:** `retrieval/engine` holds the contract types only (no Qdrant import); `retrieval/engine/qdrant` implements it on `net/http`; `retrieval/enginetest` is the conformance suite every driver must pass, run by `qdrant/qdrant_test.go` when `QDRANT_TEST_URL` is set. `retrieval/filter` validates consumer filters against a corpus's declared schema; `retrieval/services.Service` is the corpus registry registered under `module.ServiceRetrieval`. `module.go` reads `QDRANT_*` at Init without contacting anything and seals registration at Start.

**Tech Stack:** Go 1.26.10, `net/http`, `github.com/google/uuid` v1.6.0 (already in `backend/go.mod`, used for UUIDv5), Qdrant v1.20.0 REST API, GitHub Actions service containers.

**Spec:** `docs/adr/0023-core-retrieval-module.md` (D1, D3, D7, D16) and `docs/superpowers/plans/2026-10-26-retrieval-00-roadmap.md` (PR 3 row and the **Shared contract**: sections "SDK — `pkg/sdk/iface/retrieval.go` (PR 3)", "Module — `backend/internal/core/retrieval/`", "Internal engine contract — `retrieval/engine/engine.go` (PR 3)", "Collections").

Qdrant API sources used by this plan (read them before Tasks 6–7):
- BM25 on the full-text index, `text` query, `using` = payload field, BM25 `k`/`b`, corpus statistics per shard: https://qdrant.tech/documentation/search/text-search/full-text-search/ (BM25 scoring *available as of v1.20.0*: https://qdrant.tech/documentation/manage-data/indexing/#bm25-scoring).
- Tenant-scoped statistics, `params.idf.corpus` (*available as of v1.19.0*): https://qdrant.tech/documentation/manage-data/multitenancy/#per-tenant-scoring-statistics.
- Prefetch + weighted RRF (`query.rrf.weights`, *v1.17.0*; RRF `k` default 2): https://qdrant.tech/documentation/search/hybrid-queries/.
- Conditional upsert `update_filter` (*v1.16.0*; "New points will be inserted regardless of the filter", non-matching existing points "keep their current state"): https://qdrant.tech/documentation/manage-data/points/#conditional-updates.
- Text index parameters (`tokenizer`, `lowercase`, `stemmer` snowball, `stopwords`, `scoring`), tenant index `is_tenant`: https://qdrant.tech/documentation/manage-data/indexing/.
- Request/response schemas (`Prefetch.params`, `SearchParams.idf`, `IdfCorpusParams`, `PointsList.update_filter`, `VersionInfo`): https://github.com/qdrant/qdrant/blob/master/docs/redoc/master/openapi.json.

## Global Constraints

Every task includes the roadmap's **Global constraints** section verbatim. PR-specific additions:

- No new dependency in `backend/go.mod`: the driver uses `net/http` + `encoding/json`; UUIDv5 uses `uuid.NewSHA1` from `github.com/google/uuid` v1.6.0.
- Qdrant floor is **v1.20.0** (BM25 scoring on the full-text index). The driver refuses an older server in `Health`. CI pins `qdrant/qdrant:v1.20.0` by digest.
- Import direction: `engine` → (`iface`, `uuid`, stdlib) only; `engine/qdrant` → `engine`, `iface`; `enginetest` → `engine`; `filter` → `iface` (PR 5 adds `engine` for compilation); `services` → `engine`, `filter`, `iface`; `module.go` → `services`, `engine`, `engine/qdrant`, `repository`. `engine` never imports `qdrant`.
- Driver errors never contain the Qdrant URL, the API key, or any response body; logs never carry the API key (logscope: no secret-looking key with a dynamic value, no `slog.Any` with opaque values).
- This PR adds no route, permission, Cedar policy, errcode, nav item, `ConfigSchema` field or Mongo repository code. `retrieval_corpus_state` is only declared (its shape is PR 4's).
- Payload layout (D3): `_rt` = module-only runtime fields **plus** `_rt.text` (the indexed text); `f` = declared filter fields. A user filter field is always rendered as `f.<name>`, never as a raw key.
- Value representation at the engine boundary (fixed here, used by PRs 4–5): `keyword` → `string`, `keyword[]` → `[]string`, `integer` → `int64`, `float` → `float64`, `bool` → `bool`, `datetime` → `int64` Unix **milliseconds** UTC (in `Point.Fields`) and `float64` milliseconds (in `engine.Range`). Datetime fields get an `integer` payload index, not Qdrant's `datetime` index, so range compilation needs no field-type lookup.
- Commit trailers on every commit: `Prop: upstream` and `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

## Review Focus

1. **Tenant A's documents shifting tenant B's keyword ranking** (roadmap RF 4): B's BM25 scores must be identical with and without A's data — pinned by conformance `TenantScopedKeywordStatistics` (Task 5) and `TestQuery_HybridBody` asserting `params.idf.corpus` = tenant filter on the keyword prefetch (Task 7).
2. **A stale writer resuming after its successor wrote** (roadmap RF 1, engine level): a lower revision never overwrites a higher one, and a point re-created after delete carries the stale revision so the read path can drop it — pinned by `ConditionalUpsertNeverLowersRevision`, `StaleWriterAfterSuccessor`, `RecreatedPointIsDetectableByRevision` (Task 5).
3. **A filter naming a reserved or undeclared field** (`_rt.tenantId`, `tenantId`, `f.status`): refused by `filter.Validate` (Task 3 `TestValidate` cases), and even an engine filter that names `_rt.tenantId` cannot reach the runtime namespace because the driver prefixes `f.` (Task 7 `TestQuery_UserFilterIsNamespaced`, Task 5 `ReservedNamespaceUnreachable`).
4. **A query that lost its tenant or principals** (a caller bug): refused before any HTTP request, never run unfiltered; a point whose `visibleTo` is empty is found by nobody — pinned by Task 7 `TestQuery_RefusesUnscoped` and Task 5 `PrincipalFilter`.
5. **Qdrant absent, unreachable, slow, older than 1.20 or with a wrong API key**: boot never fails and Init opens no connection; `HealthCheck` answers `iface.ErrRetrievalUnavailable` with no URL or key in the text; the driver classifies timeouts, 5xx, 401/403 and oversize bodies as `engine.ErrUnavailable` — pinned by Task 8 `TestModule_InitNeverContactsTheEngine` / `TestModule_HealthCheck*` and Task 6 `TestDo_*`.

---

## File structure

```
backend/pkg/sdk/iface/retrieval.go             # T1: contract block, verbatim from the roadmap
backend/pkg/sdk/iface/retrieval_test.go        # T1
backend/pkg/sdk/iface/rag_types.go             # T1: Deprecated
backend/pkg/sdk/iface/interfaces.go:373-379    # T1: RAGQueryProvider Deprecated
backend/pkg/sdk/iface/rag_deprecation_test.go  # T1
backend/pkg/sdk/module/services.go:49-52       # T1: ServiceRetrieval, ServiceRAGQuery Deprecated
backend/pkg/sdk/module/services_retrieval_test.go  # T1
backend/internal/core/retrieval/
├── AGENTS.md                                  # T8
├── module.go, module_test.go                  # T8
├── engine/engine.go, engine_test.go           # T2
├── engine/qdrant/client.go                    # T5 skeleton, T6: Config, Driver, New, do, classification
├── engine/qdrant/collections.go               # T6: Health, EnsureCollection, DropCollection
├── engine/qdrant/points.go                    # T7: Upsert, DeleteOtherRevisions, DeleteByFilter, Count
├── engine/qdrant/query.go                     # T7: Query, filter rendering
├── engine/qdrant/*_test.go                    # T6, T7 (httptest, no Qdrant needed)
├── engine/qdrant/qdrant_test.go               # T5: live conformance, gated by QDRANT_TEST_URL
├── enginetest/enginetest.go, fixtures.go      # T5
├── filter/filter.go, filter_test.go           # T3
├── repository/collections.go                  # T8: CollCorpusState
└── services/service.go, registry.go, registry_test.go  # T4
backend/cmd/server/catalog.go:20-55            # T8
backend/cmd/server/core_load_order_test.go:36  # T8
backend/cmd/server/config_declarations_test.go:13  # T8
.github/workflows/backend.yml                  # T5
docs: AGENTS.md (root), backend/AGENTS.md, backend/internal/core/AGENTS.md, backend/pkg/sdk/AGENTS.md (T1, T8); docs/site/** (T9)
```

---

### Task 1: SDK contract and the D16 deprecation

**Files:**
- Create: `backend/pkg/sdk/iface/retrieval.go`, `backend/pkg/sdk/iface/retrieval_test.go`, `backend/pkg/sdk/iface/rag_deprecation_test.go`, `backend/pkg/sdk/module/services_retrieval_test.go`
- Modify: `backend/pkg/sdk/module/services.go:49-52`, `backend/pkg/sdk/iface/rag_types.go:8-45`, `backend/pkg/sdk/iface/interfaces.go:373-379`
- Docs: `backend/pkg/sdk/AGENTS.md:49` (iface row), `backend/pkg/sdk/README.md:172`, `docs/site/sdk/shared-iface.mdx:84-90`, `docs/onboarding/orkestra-sdk.md:495`

**Interfaces:**
- Consumes: nothing.
- Produces: every type, constant, interface and `ErrRetrieval*` sentinel of the roadmap section "SDK — `pkg/sdk/iface/retrieval.go` (PR 3)", verbatim; `module.ServiceRetrieval ServiceKey = "retrieval.service"`.

- [ ] **Step 1: Write the failing tests**

`backend/pkg/sdk/iface/retrieval_test.go`:

```go
func TestRetrievalSentinels_AreDistinctAndWrappable(t *testing.T) {
	// all ten: ErrRetrievalNotConfigured, ErrRetrievalUnavailable, ErrRetrievalCorpusUnavailable,
	// ErrRetrievalUnknownCorpus, ErrRetrievalInvalidSpec, ErrRetrievalInvalidFilter,
	// ErrRetrievalInvalidRequest, ErrRetrievalForbidden, ErrRetrievalLocality, ErrRetrievalVisibilityCheck
	// same body as TestLLMSentinels_AreDistinctAndWrappable (llm_test.go:9): distinct messages, errors.Is through fmt.Errorf("%w")
}

func TestRetrievalFilter_JSONOmitsEmptyBranches(t *testing.T) {
	b, _ := json.Marshal(RetrievalFilter{Field: "status", Op: RetrievalOpEq, Value: "open"})
	// want exactly: {"field":"status","op":"eq","value":"open"}
}
```

`backend/pkg/sdk/iface/rag_deprecation_test.go` — `TestLegacyRAGContractIsDeprecated`: parse `rag_types.go` and `interfaces.go` with `go/parser` (`parser.ParseComments`); for each of `SourceRef`, `QueryMeta`, `RAGQueryResponse`, `RAGQueryProvider` take the type's doc (`TypeSpec.Doc`, else the enclosing `GenDecl.Doc`) and assert it contains a line starting `Deprecated: ` and the text `iface.RetrievalSearch`.

`backend/pkg/sdk/module/services_retrieval_test.go`:

```go
func TestServiceRetrievalKey(t *testing.T) {
	if ServiceRetrieval != "retrieval.service" { t.Fatalf("ServiceRetrieval = %q", ServiceRetrieval) }
}
// TestServiceRAGQueryIsDeprecated: go/parser over services.go; the ValueSpec named ServiceRAGQuery
// has a Doc containing "Deprecated: " and "ServiceRetrieval".
```

- [ ] **Step 2: Run to verify they fail**

Run: `cd backend && go test ./pkg/sdk/iface/ ./pkg/sdk/module/ -run 'Retrieval|RAG' -v`
Expected: FAIL — `undefined: RetrievalFilter`, `undefined: ServiceRetrieval`, deprecation assertions fail.

- [ ] **Step 3: Implement**

- `iface/retrieval.go`: header `// backend/pkg/sdk/iface/retrieval.go` and a package-level comment block naming ADR-0023 and how consumers resolve the interfaces (`module.GetTyped[iface.<Interface>](reg, module.ServiceRetrieval)`), then the roadmap block **verbatim** (imports `context`, `errors`). Add a doc comment on each exported interface; change no name, field, tag or comment value.
- `module/services.go`: in the const block after `ServiceLLMGateway` (L49) add `ServiceRetrieval ServiceKey = "retrieval.service" // *retrieval/services.Service: RetrievalCorpusRegistry, RetrievalOutbox, RetrievalSearch, RetrievalPrincipalResolver (ADR-0023)`. Above `ServiceRAGQuery` (L52) put the doc line `// Deprecated: use ServiceRetrieval with iface.RetrievalSearch (ADR-0023 D16); removed in a later release.`
- `rag_types.go` / `interfaces.go:373-379`: append to each of the four type docs a separate paragraph `// Deprecated: use iface.RetrievalSearch, served by the core retrieval module under module.ServiceRetrieval (ADR-0023 D16). Removed in a later release.` (its own paragraph, so gocritic `deprecatedComment` accepts it).
- Docs: `pkg/sdk/AGENTS.md` iface row — add `RetrievalCorpusRegistry`, `RetrievalSource`, `RetrievalOutbox`, `RetrievalSearch`, `RetrievalPrincipalResolver`, `RetrievalPrincipalContributor` (core `retrieval`, ADR-0023; resolved with `module.GetTyped` on `ServiceRetrieval`) and mark `RAGQueryProvider` deprecated; same in `pkg/sdk/README.md:172`; `shared-iface.mdx`: mark `RAGQueryProvider` deprecated in the "Reserved for a fork's addons" list and add one paragraph after the `LLMGateway` paragraph: the retrieval interfaces are implemented by the core `retrieval` module (ADR-0023), defined in `pkg/sdk/iface/retrieval.go`; `orkestra-sdk.md:495` row: append "deprecated (ADR-0023 D16) — use `RetrievalSearch`".

- [ ] **Step 4: Run to verify they pass**

Run: `cd backend && go test ./pkg/sdk/... && go vet ./pkg/sdk/... && grep -rn "internal/" pkg/sdk/iface/retrieval.go`
Expected: PASS; the grep prints nothing.

- [ ] **Step 5: Commit**

```bash
git add backend/pkg/sdk docs/site/sdk/shared-iface.mdx docs/onboarding/orkestra-sdk.md
git commit -m "feat(sdk): add the retrieval contract and deprecate the legacy RAG types

Prop: upstream
Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: `engine` package — contract types, PointID, collection names

**Files:**
- Create: `backend/internal/core/retrieval/engine/engine.go`, `backend/internal/core/retrieval/engine/engine_test.go`

**Interfaces:**
- Consumes: `iface.RetrievalFilterField`, `iface.RetrievalAnalyzer` (Task 1).
- Produces: the roadmap block "Internal engine contract" verbatim (`Engine`, `CollectionSpec`, `Runtime`, `Point`, `PointID`, `Selector`, `Query`, `Weights`, `Filter`, `Range`, `Candidate`, `ErrUnavailable`, `ErrNotFound`), plus two additive helpers:
  - `func CollectionName(prefix, corpus string, generation int) string` → `"<prefix>_<corpus>_g<generation>"` (ADR D3 layout; PRs 4–7 build names only through it).
  - `func (s Selector) IsZero() bool` — true when `TenantID == "" && len(SourceIDs) == 0 && Subject == ""`.

- [ ] **Step 1: Write the failing tests**

```go
func TestPointID_Golden(t *testing.T) {
	// UUIDv5, namespace 21fc1e39-ea81-5111-b6fd-a430a1211388, name "t1\x00contacts\x00s1\x000"
	if got := PointID("t1", "contacts", "s1", 0); got != "d66fd356-1bfc-517f-879d-d8d3f6b78fbb" { t.Fatal(got) }
	if got := PointID("t1", "contacts", "s1", 1); got != "6fa2aea1-c234-5176-903f-a29ded43d276" { t.Fatal(got) }
	if got := PointID("t2", "contacts", "s1", 0); got != "35ebe444-6537-506f-93cc-c6e4a4131fae" { t.Fatal(got) }
	// uuid.MustParse(got).Version() == 5
}
func TestCollectionName(t *testing.T) { /* CollectionName("orkestra", "contacts", 1) == "orkestra_contacts_g1" */ }
func TestSelectorIsZero(t *testing.T) { /* Selector{}.IsZero() true; Selector{Subject: "user:u"}.IsZero() false; Selector{SourceIDs: []string{}}.IsZero() true */ }
```

- [ ] **Step 2: Run to verify they fail**

Run: `cd backend && go test ./internal/core/retrieval/engine/ -v`
Expected: FAIL — package does not exist / `undefined: PointID`.

- [ ] **Step 3: Implement `engine.go`**

Copy the roadmap block verbatim. Add:
- `var pointNamespace = uuid.MustParse("21fc1e39-ea81-5111-b6fd-a430a1211388")` — `uuid.NewSHA1(uuid.NameSpaceURL, []byte("https://orkestra.cc/retrieval/point-id/v1"))`, frozen; a comment says changing it re-keys every point of every installation.
- `PointID` = `uuid.NewSHA1(pointNamespace, []byte(tenantID+"\x00"+corpus+"\x00"+sourceID+"\x00"+strconv.Itoa(ordinal))).String()`.
- Package doc: the engine is engine-neutral and never imported by consumers; the value-representation table from Global Constraints (datetime = Unix ms UTC); `Filter.Field` is the **declared** field name, never a payload path — the driver maps it into namespace `f`; `Candidate.Fields` holds JSON-decoded values (numbers as `float64`).

- [ ] **Step 4: Run to verify they pass**

Run: `cd backend && go test ./internal/core/retrieval/engine/ -v`
Expected: PASS.

- [ ] **Step 5: Commit** — `feat(retrieval): engine contract, point ids and collection names` (trailers as in Task 1).

---

### Task 3: `filter.Validate` — the corpus filter schema gate

**Files:**
- Create: `backend/internal/core/retrieval/filter/filter.go`, `backend/internal/core/retrieval/filter/filter_test.go`

**Interfaces:**
- Consumes: `iface.RetrievalFilter`, `iface.RetrievalFilterField`, `iface.RetrievalOp*`, `iface.RetrievalField*`, `iface.ErrRetrievalInvalidFilter`.
- Produces:
  ```go
  const (MaxDepth = 4; MaxNodes = 64; MaxValues = 100; MaxStringBytes = 1024)
  // Validate checks f against the corpus schema; nil f is valid. Every error wraps
  // iface.ErrRetrievalInvalidFilter and names the node path, e.g. `and[1].or[0]`.
  func Validate(f *iface.RetrievalFilter, schema []iface.RetrievalFilterField) error
  ```
  Package doc states the boundary: this package owns filter validation (PR 3) and compilation to `*engine.Filter` (PR 5, `Compile`), so a filter is never compiled without being validated.

Rules (all exact):
- A node is exactly one of: `len(And) > 0`, `len(Or) > 0`, `Not != nil`, or a comparison (`Field != ""`). An empty node, or a node setting two of them (e.g. `And` plus `Field`/`Op`/`Value`), is invalid. On a logical node `Op`, `Value`, `Values` must be zero.
- Depth: the root is depth 1, each `And`/`Or` element and `Not` adds 1; depth > 4 is invalid. Nodes: every node counts, root included; > 64 is invalid.
- `Field` must be the `Name` of a schema entry (exact match; so `_rt.tenantId`, `tenantId`, `f.status` are refused as "not declared").
- Op ↔ type: `keyword`, `keyword[]`: `eq`, `in`, `exists`; `integer`: `eq`, `in`, `lt`, `lte`, `gt`, `gte`, `exists`; `float`: `lt`, `lte`, `gt`, `gte`, `exists`; `bool`: `eq`, `exists`; `datetime`: `lt`, `lte`, `gt`, `gte`, `exists`. Unknown op is invalid.
- Arity: `eq` and range ops need `Value != nil` and no `Values`; `in` needs `1 <= len(Values) <= 100` and `Value == nil`; `exists` needs both empty.
- Value types: keyword/keyword[] → `string`, 1..1024 bytes; integer → any Go integer kind, `float64` with zero fraction and |v| ≤ 2^53, or `json.Number` parseable by `Int64()`; float → any integer kind, `float32`/`float64`, or `json.Number`, finite (NaN/±Inf refused); bool → `bool`; datetime → `string` parseable by `time.Parse(time.RFC3339Nano, …)` or `time.Time`.

- [ ] **Step 1: Write the failing test** — `TestValidate`, table-driven over schema `status keyword`, `tags keyword[]`, `score integer`, `ratio float`, `active bool`, `created datetime`; every failing case asserts `errors.Is(err, iface.ErrRetrievalInvalidFilter)` and a message substring:

| case | filter | want |
|---|---|---|
| nil | `nil` | ok |
| eq keyword | `{Field:"status",Op:"eq",Value:"open"}` | ok |
| undeclared | `{Field:"owner",Op:"eq",Value:"x"}` | `field "owner" is not declared` |
| reserved | `{Field:"_rt.tenantId",Op:"eq",Value:"t"}` | `is not declared` |
| namespaced | `{Field:"f.status",Op:"eq",Value:"open"}` | `is not declared` |
| depth 4 | `And{And{And{cmp}}}` | ok |
| depth 5 | `And{And{And{And{cmp}}}}` | `depth` |
| 64 nodes | `And` with 63 comparisons | ok |
| 65 nodes | `And` with 64 comparisons | `nodes` |
| in 100 | `in` with 100 strings | ok |
| in 101 | `in` with 101 strings | `values` |
| eq float | `{Field:"ratio",Op:"eq",Value:0.5}` | `op "eq" is not allowed on float field "ratio"` |
| range keyword | `{Field:"status",Op:"gt",Value:"a"}` | `not allowed` |
| integer 1.5 | `{Field:"score",Op:"gte",Value:1.5}` | `integer` |
| integer 3.0, json.Number | `Value: float64(3)` / `json.Number("3")` | ok |
| NaN | `{Field:"ratio",Op:"lt",Value:math.NaN()}` | `finite` |
| datetime | `Value:"2026-10-26T10:00:00+02:00"` | ok |
| datetime bad | `Value:"yesterday"` | `RFC 3339` |
| bool string | `{Field:"active",Op:"eq",Value:"true"}` | `bool` |
| exists with value | `{Field:"ratio",Op:"exists",Value:1}` | `exists` |
| two kinds | `{And:[cmp],Field:"status"}` | `exactly one` |
| empty node | `{}` / `{And: []iface.RetrievalFilter{}}` | `exactly one` |
| long keyword | 1025-byte string | `1024` |
| path | `{And:[ok, {Or:[{Field:"owner",…}]}]}` | `and[1].or[0]` |

- [ ] **Step 2: Run** `cd backend && go test ./internal/core/retrieval/filter/ -v` — Expected: FAIL (`undefined: Validate`).
- [ ] **Step 3: Implement `Validate`** with one recursive walk carrying depth, a node counter and the path string; build a `map[string]iface.RetrievalFieldType` from the schema once per call.
- [ ] **Step 4: Run** the same command — Expected: PASS.
- [ ] **Step 5: Commit** — `feat(retrieval): validate consumer filters against the corpus schema`.

---

### Task 4: Corpus registry — `services.Service`

**Files:**
- Create: `backend/internal/core/retrieval/services/service.go`, `backend/internal/core/retrieval/services/registry.go`, `backend/internal/core/retrieval/services/registry_test.go`

**Interfaces:**
- Consumes: `engine.Engine` (Task 2); `iface.Retrieval*` (Task 1).
- Produces (PRs 4–7 extend `Service` with outbox, search, resolver methods):
  ```go
  type Options struct {
  	Engine           engine.Engine // nil = QDRANT_URL not configured
  	CollectionPrefix string
  	Logger           *slog.Logger
  }
  func New(opts Options) *Service
  func (s *Service) Engine() (engine.Engine, error)        // iface.ErrRetrievalNotConfigured when nil
  func (s *Service) CollectionPrefix() string
  func (s *Service) RegisterCorpus(spec iface.RetrievalCorpusSpec) error
  func (s *Service) RegisterPrincipalContributor(c iface.RetrievalPrincipalContributor) error
  func (s *Service) Seal()                                  // idempotent; called by Module.Start
  func (s *Service) Corpus(name string) (iface.RetrievalCorpusSpec, bool) // normalized copy
  func (s *Service) Corpora() []iface.RetrievalCorpusSpec   // sorted by Name
  func (s *Service) PrincipalContributors() []iface.RetrievalPrincipalContributor // registration order
  func (s *Service) FilterSchema(corpus string) ([]iface.RetrievalFilterField, error) // copy; ErrRetrievalUnknownCorpus
  var _ iface.RetrievalCorpusRegistry = (*Service)(nil)
  var DefaultFusion = iface.RetrievalFusion{DenseWeight: 1, KeywordWeight: 1, Candidates: 50}
  const DefaultRerankTopK = 20
  ```

Validation of `RegisterCorpus` (every error wraps `iface.ErrRetrievalInvalidSpec` and starts `retrieval: corpus "<name>": `). It is syntax only and never contacts the engine or `llm`. **Locality against endpoint boundary is not checked here**: the `llm` profiles are resolved when a generation is built (PR 4) and at query time (PR 5), which refuse with `iface.ErrRetrievalLocality`; registration only checks the locality value.
- `Name` matches `^[a-z][a-z0-9_]{0,31}$` and is unique; `Owner` matches `^[a-z][a-z0-9_-]{0,63}$`; `Permission` matches `^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*){1,3}$`; `Source != nil`.
- `Filters`: at most 32; each `Name` matches `^[a-z][a-zA-Z0-9_]{0,63}$` and is unique; `Type` is one of the six `RetrievalField*`; `Description` is 1..500 bytes after `strings.TrimSpace`.
- `Ingestion.EmbedEndpoint`: non-empty after trim, ≤ 64 bytes, no whitespace.
- `Ingestion.Analyzer`: `Tokenizer` `""` (normalized to `"word"`), `"word"` or `"multilingual"`; `Stemming` requires `Language` in the snowball set `arabic armenian danish dutch english finnish french german greek hungarian italian norwegian portuguese romanian russian spanish swedish tamil turkish`; `StopWords` requires `Language` in the stop-word set `arabic azerbaijani basque bengali catalan chinese danish dutch english finnish french german greek hebrew hinglish hungarian indonesian italian japanese kazakh nepali norwegian portuguese romanian russian slovene spanish swedish tajik turkish` (both lists are Qdrant v1.20's `SnowballLanguage` and `Language` enums; a comment says so).
- `Ingestion.Chunking`: `Strategy` `"none"` (then `MaxTokens == 0 && Overlap == 0`) or `"fixed"` (`1 <= MaxTokens <= 8192`, `0 <= Overlap < MaxTokens`); `"hierarchical"` → message `available from delivery step 3`; anything else invalid.
- `Ingestion.Enrichment` must be `nil` (`available from delivery step 3`).
- `Locality`: `""` normalized to `RetrievalLocalOnly`; otherwise `local_only` or `hosted_allowed`.
- `Fusion`: each zero field takes `DefaultFusion`'s; non-zero weights in `(0, 10]`, `Candidates` in `1..500`.
- `Rerank`: `TopK` 0 → `DefaultRerankTopK`, else `1..200`; `Enabled` requires a non-empty `Endpoint`.
- After `Seal()`: `RegisterCorpus` and `RegisterPrincipalContributor` return an error wrapping `ErrRetrievalInvalidSpec` with `registration is closed`.
- `RegisterPrincipalContributor`: non-nil; `Name()` matches `^[a-z][a-z0-9_.-]{0,63}$` and is unique.
- Stored spec is a deep copy (`Filters` slice copied); `Corpus`/`FilterSchema` return copies so a caller cannot mutate the registry.

- [ ] **Step 1: Write the failing tests** in `registry_test.go` (a `validSpec()` helper returns `Name "contacts"`, `Owner "crm"`, `Permission "crm.contact.read"`, one `status keyword` filter, `EmbedEndpoint "embed"`, chunking `none`, a stub `RetrievalSource`):
  - `TestRegisterCorpus_NormalizesDefaults`: `Corpus("contacts")` has `Locality == RetrievalLocalOnly`, `Analyzer.Tokenizer == "word"`, `Fusion == DefaultFusion`, `Rerank.TopK == 20`.
  - `TestRegisterCorpus_Rejects` table, each `errors.Is(err, iface.ErrRetrievalInvalidSpec)`: name `"Contacts"`, name 33 chars, duplicate name, owner `""`, permission `"read"`, nil source, 33 filters, duplicate filter name, filter type `"text"`, empty description, `EmbedEndpoint ""`, tokenizer `"code"`, `Stemming` with `Language ""`, `Stemming` with `"latin"`, chunking `"hierarchical"`, `"fixed"` with `Overlap == MaxTokens`, `"none"` with `MaxTokens 100`, non-nil `Enrichment`, locality `"anywhere"`, `DenseWeight 11`, `Candidates 501`, `Rerank{Enabled:true}` without endpoint, `TopK 201`.
  - `TestRegisterCorpus_ClosedAfterSeal` and `TestRegisterPrincipalContributor_ClosedAfterSeal`: after `Seal()` both return an error containing `registration is closed`; `Seal()` twice does not panic.
  - `TestRegisterPrincipalContributor_RejectsDuplicateAndBadNames`.
  - `TestFilterSchema`: returns the declared fields; mutating the returned slice does not change a second call; unknown corpus → `errors.Is(err, iface.ErrRetrievalUnknownCorpus)`.
  - `TestCorpora_SortedByName`: register `b`, `a` → `[a b]`.
  - `TestEngine_NotConfigured`: `New(Options{}).Engine()` → `errors.Is(err, iface.ErrRetrievalNotConfigured)`.
  - `TestRegisterCorpus_IsConcurrencySafe`: 16 goroutines registering distinct names under `-race`.
- [ ] **Step 2: Run** `cd backend && go test -race ./internal/core/retrieval/services/ -v` — Expected: FAIL (`undefined: New`).
- [ ] **Step 3: Implement** — `service.go` holds `Service` (a `sync.RWMutex`, `sealed bool`, `corpora map[string]iface.RetrievalCorpusSpec`, contributor slice + name set, engine, prefix, logger), `New`, `Engine`, `CollectionPrefix`, `Seal`; `registry.go` holds the registration methods, the validation helpers and the language sets.
- [ ] **Step 4: Run** the same command — Expected: PASS.
- [ ] **Step 5: Commit** — `feat(retrieval): corpus registry with declaration checks`.

---

### Task 5: Driver conformance suite, live gate and Qdrant in CI

**Files:**
- Create: `backend/internal/core/retrieval/enginetest/enginetest.go`, `backend/internal/core/retrieval/enginetest/fixtures.go`, `backend/internal/core/retrieval/engine/qdrant/client.go` (skeleton), `backend/internal/core/retrieval/engine/qdrant/qdrant_test.go`
- Modify: `.github/workflows/backend.yml` (job `ci`: `env` block L60-68, `services` block L69-74)

**Interfaces:**
- Consumes: `engine.*` (Task 2).
- Produces:
  ```go
  // enginetest
  func Run(t *testing.T, newEngine func(t *testing.T) engine.Engine)
  // qdrant (skeleton now, implemented in Tasks 6–7)
  type Config struct {
  	URL              string
  	APIKey           string
  	Timeout          time.Duration // per call; 0 = 10s; the caller's earlier deadline wins
  	MaxResponseBytes int64         // 0 = 16 MiB
  	HTTPClient       *http.Client  // nil = own client (dial 5s, TLS handshake 5s)
  }
  type Driver struct{ /* unexported */ }
  func New(cfg Config) (*Driver, error)
  var _ engine.Engine = (*Driver)(nil)
  ```

The suite (one `t.Run` per case, names exact; each case calls `newEngine(t)`, creates its own collections named `engine.CollectionName("ct"+<8 random hex>, "<case in snake_case>", 1)` with `Dimensions: 4`, and drops them in `t.Cleanup`; vectors are hand-made 4-dim, non-zero; principals `"tenant:*"` unless the case says otherwise; `fixtures.go` has `point(tenant, source string, rev int64, ordinal int, text string, vec []float32, visibleTo, subjects []string, fields map[string]any) engine.Point` filling `ID` with `engine.PointID(tenant, "c", source, ordinal)` and `Runtime` consistently):

| Case | Setup → assertion |
|---|---|
| `Health` | `Health(ctx) == nil` |
| `EnsureCollectionIsIdempotent` | `EnsureCollection` twice → nil; same name with `Dimensions: 8` → error that is neither `ErrUnavailable` nor `ErrNotFound` |
| `MissingCollection` | on a never-created name: `Count`, `Query`, `Upsert`, `DeleteByFilter`, `DeleteOtherRevisions` → `errors.Is(err, engine.ErrNotFound)`; `DropCollection` → nil |
| `UpsertRequiresDenseVector` | a point with `Dense: nil` → error and `Count(TenantID)` = 0; a 3-dim vector → error, not `ErrUnavailable` |
| `RoundTrip` | one point with every `Runtime` field set (incl. `Page: 3`, two subjects) and `Fields{"status":"open"}`; dense query → one candidate whose `Runtime` is `reflect.DeepEqual` to the input, `Text` equal, `Fields["status"] == "open"`, `PointID` equal |
| `TenantIsolation` | identical text and vectors for tenants A and B; as A with dense-only, keyword-only and hybrid → every candidate has `Runtime.TenantID == A` and the count equals A's points |
| `TenantScopedKeywordStatistics` | collection X: B's docs `B1 "orchid supplier contract"`, `B2 "orchid"`, `B3 "supplier invoice archive quarterly"`; collection Y: the same B points plus 50 A docs of 40 tokens each, every one containing `orchid` 5 times; keyword-only `"orchid supplier"` as B in X and in Y → same `PointID` order and `abs(sx-sy) <= 1e-5*max(1,abs(sx))` per hit; as A in Y → only A points |
| `PrincipalFilter` | points `Pu{user:u1}`, `Pr{role:r1}`, `Pt{tenant:*}`, `Pn{}` (empty visibleTo); principals `[user:u2 role:r1]` → `{Pr}`; `[tenant:* user:u1]` → `{Pu Pt}`; `[tenant:* user:u1 role:r1]` never returns `Pn`; `Principals: nil` → error; `TenantID: ""` → error |
| `ReservedNamespaceUnreachable` | tenant A points; `Filter{Field:"_rt.tenantId", Match:[]any{A}}` as A → 0 candidates; `Filter{Field:"tenantId", Match:[]any{A}}` → 0 |
| `UserFilter` | fields `status` (keyword), `tags` ([]string), `score` (int64), `ratio` (float64, absent on one point), `active` (bool), `created` (int64 ms); `Must{status∈[open]}`, `tags Match[vip]`, `Range score Gte 10`, `Range created Lt <ms>`, `MustNot{active Match[true]}`, `Exists ratio`, `Should{status=open, score Gte 50}` each return exactly the expected point set |
| `FusionWeightsAndModes` | `P1 [1,0,0,0] "alpha"`, `P2 [0,1,0,0] "zebra quagga"`, `P3 [0.9,0.1,0,0] "lorem"`; dense `[1,0,0,0]`, keyword `"quagga"`, `PerArm 2`, `Limit 2`: keyword `""` → first `P1`; dense nil → only `P2`; weights `{10,1}` → first `P1`; weights `{1,10}` → first `P2` (RRF k=2: 1/1.1 vs 1/2) |
| `Diagnostics` | same points, `PerArm 3`, hybrid, `Diagnostics: true` → `P2` has both arm scores non-nil, `P1.KeywordScore == nil`, `P1.DenseScore != nil`; `Diagnostics: false` → all arm scores nil |
| `ConditionalUpsertNeverLowersRevision` | write ordinal 0 rev 2 `"new"`; write rev 1 `"old"` → stored rev 2 `"new"`; write rev 2 with `VisibleTo [user:u9]` → updated (equal revision allowed); write rev 3 → rev 3 |
| `DeleteOtherRevisions` | (A,s1) rev 1 ordinals 0,1,2; rev 2 ordinals 0,1; (A,s2) rev 1; (B,s1) rev 1; `DeleteOtherRevisions(A, s1, 2)` → `Count{A,[s1]} == 2`, `Count{A,[s2]} == 1`, `Count{B,[s1]} == 1` |
| `DeleteByFilterAcrossCollections` | collections g1 and g2 each with (A, `user:x`), (A, `user:y`), (B, `user:x`); `DeleteByFilter{TenantID:A, Subject:"user:x"}` on both → `Count{A,user:x} == 0` in both, `Count{B,user:x} == 1`, `Count{A,user:y} == 1`; `DeleteByFilter(Selector{})` → error and counts unchanged; `DeleteByFilter{TenantID:B}` → `Count{TenantID:B} == 0` |
| `Count` | 3 points of A (s1 ×2 ordinals, s2), 1 of B → `{A}`=3, `{A,[s1,s2]}`=3, `{A,[s1]}`=2, `{Subject:"user:x"}` across tenants, `{}` → error |
| `StaleWriterAfterSuccessor` | successor: rev 2 ordinals 0,1 + `DeleteOtherRevisions(keep 2)`; stale worker resumes: rev 1 ordinals 0,1,2 → dense query returns `{(0,rev2),(1,rev2),(2,rev1)}` (texts of 0,1 still rev 2); successor re-applies `DeleteOtherRevisions(keep 2)` → `{(0,rev2),(1,rev2)}` |
| `RecreatedPointIsDetectableByRevision` | rev 3 ordinal 0; `DeleteByFilter{TenantID:A, SourceIDs:[s1]}`; stale rev 2 ordinal 0 → the point exists again with `Runtime.SourceRevision == 2` (ADR D11: the engine cannot refuse an insert; the read path drops it because the source reports 3) |
| `AnalyzerStemming` | `Analyzer{Tokenizer:"word", Language:"english", Stemming:true, StopWords:true}`; doc `"running shoes for the marathon"`; keyword `"runs"` → found; keyword `"the"` → 0 candidates |

`qdrant_test.go` (package `qdrant`):

```go
func TestConformance(t *testing.T) {
	url := os.Getenv("QDRANT_TEST_URL")
	if url == "" { t.Skip("set QDRANT_TEST_URL (and QDRANT_TEST_API_KEY) to run the Qdrant conformance suite") }
	// waitReady: poll Health for up to 30s (the CI service container may still be starting)
	enginetest.Run(t, func(t *testing.T) engine.Engine { d, err := New(Config{URL: url, APIKey: os.Getenv("QDRANT_TEST_API_KEY")}); /* t.Fatal on err */ return d })
}
func TestLive_WrongAPIKeyIsUnavailable(t *testing.T) // same gate; New with APIKey "wrong-" + key → Health: errors.Is(err, engine.ErrUnavailable), message contains neither url nor key
```

- [ ] **Step 1: Write the suite, the fixtures and `qdrant_test.go`; add the `qdrant` skeleton** — `Config`, `Driver`, `New` (stores cfg, no validation yet) and every `Engine` method returning `errors.New("qdrant: not implemented")`.

- [ ] **Step 2: Add Qdrant to CI** — in `.github/workflows/backend.yml`, job `ci`:

```yaml
    env:
      # … existing keys …
      # Unlock the retrieval driver conformance suite (ADR-0023 D3); without
      # them it skips and proves nothing.
      QDRANT_TEST_URL: http://localhost:6333
      QDRANT_TEST_API_KEY: ci-qdrant-test-key
    services:
      # … redis …
      qdrant:
        # The engine floor of ADR-0023 (BM25 on the full-text index needs
        # 1.20); pinned by digest like the compose images.
        image: qdrant/qdrant:v1.20.0@sha256:<digest>
        env:
          QDRANT__SERVICE__API_KEY: ci-qdrant-test-key
        ports: ['6333:6333']
```

Obtain the digest of the multi-arch index with `docker buildx imagetools inspect qdrant/qdrant:v1.20.0` (the `Digest:` line). On 2026-10-26 Docker Hub reported `sha256:9bfc89f9914fc14876a88c05270685921a55b8877b254a98bcc1cc2f0a424cd2` for that tag; use the value the command prints. If it differs, say so in the PR description.

- [ ] **Step 3: Run against a local Qdrant to verify it fails**

```bash
docker run -d --rm --name qdrant-pr3-test -p 127.0.0.1:16333:6333 -e QDRANT__SERVICE__API_KEY=local-test-key qdrant/qdrant:v1.20.0
cd backend && QDRANT_TEST_URL=http://127.0.0.1:16333 QDRANT_TEST_API_KEY=local-test-key go test ./internal/core/retrieval/engine/qdrant/ -run 'TestConformance|TestLive' -v
```
Expected: FAIL — `waitReady` times out on `qdrant: not implemented` (the throwaway container is reused by Tasks 6–7; stop it with `docker stop qdrant-pr3-test` at the end of Task 7). Without the env vars: `go test ./internal/core/retrieval/...` SKIPs both tests and passes.

- [ ] **Step 4: Commit** — `test(retrieval): engine conformance suite and Qdrant service in CI` (red against a live Qdrant until Task 7).

---

### Task 6: Qdrant driver — transport, error classification, collections

**Files:**
- Modify: `backend/internal/core/retrieval/engine/qdrant/client.go`
- Create: `backend/internal/core/retrieval/engine/qdrant/collections.go`, `backend/internal/core/retrieval/engine/qdrant/client_test.go`, `backend/internal/core/retrieval/engine/qdrant/collections_test.go`

**Interfaces:**
- Consumes: Task 5 skeleton, `engine.*`.
- Produces: `New` (validated), `Health`, `EnsureCollection`, `DropCollection`; unexported `(d *Driver) do(ctx context.Context, op, method, path string, body, out any) error` used by Task 7; constants `denseVector = "dense"`, `textField = "_rt.text"`, `maxPointsPerRequest = 256`, `maxQueryLimit = 500`, `minVersionMajor, minVersionMinor = 1, 20`.

Behaviour:
- `New`: URL must parse with scheme `http`/`https` and a host; a trailing `/` is trimmed; errors say `qdrant: invalid URL` without echoing it. `New` never dials.
- `do`: `http.NewRequestWithContext` under `context.WithTimeout(ctx, cfg.Timeout)`; header `api-key: <APIKey>` when set, `Content-Type: application/json` when a body is sent; read at most `MaxResponseBytes+1`; decode the `{"result": …}` envelope into `out`.
- Classification (an unexported `*statusError{op string; code int}` whose `Unwrap` returns the sentinel): caller ctx canceled → wraps `context.Canceled`, **not** `ErrUnavailable`; deadline exceeded (caller or `Timeout`) → wraps both `ErrUnavailable` and `context.DeadlineExceeded`; dial/transport error, 5xx, 429, 401, 403, oversize body, undecodable 2xx body → `ErrUnavailable`; 404 → `ErrNotFound`; other 4xx → plain error `qdrant: <op> rejected (status <n>)`. Messages carry only op and status.
- `Health`: `GET /` → `VersionInfo.version`; parse `major.minor` (ignore any `-suffix`); below 1.20 → `ErrUnavailable` wrapped with `qdrant version <v> is older than 1.20.0`; then `GET /collections` (authenticated: a wrong key fails here even if `/` is open).
- `EnsureCollection`: `Dimensions` 1..65536 and non-empty `Name`, else error. `GET /collections/{name}/exists`; when absent `PUT /collections/{name}` with `{"vectors":{"dense":{"size":D,"distance":"Cosine"}},"shard_number":1}` (one shard so BM25 statistics are exact — full-text-search docs, "Corpus Statistics"); a 409 on create is treated as "exists". When it exists, `GET /collections/{name}` and compare `result.config.params.vectors.dense.size`; a mismatch → error `qdrant: collection has %d dimensions, want %d`. Then `PUT /collections/{name}/index?wait=true` for, in this order: `_rt.tenantId` `{"type":"keyword","is_tenant":true}`, `_rt.sourceId` `"keyword"`, `_rt.visibleTo` `"keyword"`, `_rt.subjects` `"keyword"`, `_rt.sourceRevision` `"integer"`, `_rt.text` `{"type":"text","tokenizer":<word|multilingual>,"lowercase":true,"scoring":true}` plus `"stemmer":{"type":"snowball","language":L}` when `Stemming` and `"stopwords":L` when `StopWords`, then each `f.<name>`: keyword/keyword[] → `"keyword"`, integer → `"integer"`, float → `"float"`, bool → `"bool"`, datetime → `"integer"`.
- `DropCollection`: `DELETE /collections/{name}`; 404 → nil.

- [ ] **Step 1: Write the failing tests** (httptest servers; a recorder captures method, path, headers and decoded JSON bodies):
  - `TestNew_ValidatesURLWithoutDialing`: `""`, `"ftp://x"`, `"http://"` → error not containing the input; a valid httptest URL → nil and the server saw 0 requests.
  - `TestDo_SendsAPIKey`: every recorded request has `api-key: k-SECRET`.
  - `TestDo_ClassifiesStatus` (probe with `DropCollection` for 404 → nil and `Health` for the rest): 500, 502, 503, 429, 401, 403 → `ErrUnavailable`; 400 → neither sentinel.
  - `TestDo_ErrorsNeverLeak`: server answers 500 with body `BODY-SECRET`; `err.Error()` contains none of server URL, `k-SECRET`, `BODY-SECRET`.
  - `TestDo_TimeoutIsUnavailable`: `Timeout: 50ms`, handler sleeps 500ms → `errors.Is(err, ErrUnavailable) && errors.Is(err, context.DeadlineExceeded)`.
  - `TestDo_CanceledIsNotUnavailable`: canceled ctx → `errors.Is(err, context.Canceled) && !errors.Is(err, ErrUnavailable)`.
  - `TestDo_OversizeBodyIsUnavailable`: `MaxResponseBytes: 1024`, 4096-byte 200 body → `ErrUnavailable`.
  - `TestDo_UnreachableIsUnavailable`: URL of a closed listener → `ErrUnavailable`.
  - `TestHealth_Version`: `"1.19.2"` → `ErrUnavailable`; `"1.20.0"` and `"1.20.1-dev"` with `/collections` 200 → nil; `/collections` 401 → `ErrUnavailable`.
  - `TestEnsureCollection_CreatesWithIndexes`: exists=false → request sequence and bodies exactly as above for `Fields{status keyword, tags keyword[], score integer, ratio float, active bool, created datetime}`, zero analyzer (`tokenizer "word"`, no stemmer/stopwords).
  - `TestEnsureCollection_AnalyzerParams`: `{multilingual, italian, Stemming, StopWords}` → `_rt.text` schema carries `"tokenizer":"multilingual"`, `"stemmer":{"type":"snowball","language":"italian"}`, `"stopwords":"italian"`, `"scoring":true`.
  - `TestEnsureCollection_DimensionMismatch`: exists=true, info size 8, spec 4 → error, no `PUT /collections/{name}` and no index request.
  - `TestEnsureCollection_ConflictMeansExists`: create answers 409 → info read and indexes created, nil.
- [ ] **Step 2: Run** `cd backend && go test -race ./internal/core/retrieval/engine/qdrant/ -run 'TestNew|TestDo|TestHealth|TestEnsure' -v` — Expected: FAIL.
- [ ] **Step 3: Implement** `client.go` (`New`, `do`, `statusError`, default `http.Client` with `net.Dialer{Timeout: 5s}` and `TLSHandshakeTimeout: 5s`) and `collections.go`.
- [ ] **Step 4: Run** the Step 2 command, then the live subset: `cd backend && QDRANT_TEST_URL=http://127.0.0.1:16333 QDRANT_TEST_API_KEY=local-test-key go test ./internal/core/retrieval/engine/qdrant/ -run 'TestConformance/(Health|EnsureCollectionIsIdempotent)$|TestLive' -v` — Expected: PASS.
- [ ] **Step 5: Commit** — `feat(retrieval): Qdrant REST transport and collection management`.

---

### Task 7: Qdrant driver — points, counts and hybrid query

**Files:**
- Create: `backend/internal/core/retrieval/engine/qdrant/points.go`, `backend/internal/core/retrieval/engine/qdrant/query.go`, `backend/internal/core/retrieval/engine/qdrant/points_test.go`, `backend/internal/core/retrieval/engine/qdrant/query_test.go`

**Interfaces:**
- Consumes: `do`, constants (Task 6); `engine.*`.
- Produces: `Upsert`, `DeleteOtherRevisions`, `DeleteByFilter`, `Count`, `Query`; unexported `renderFilter(f *engine.Filter) map[string]any` and `scopeFilter(tenantID string, principals []string, user *engine.Filter) map[string]any`.

Behaviour:
- `Upsert`: empty slice → nil. Before any request, refuse the whole call if a point has an empty `ID`, `Dense == nil`, or empty `Runtime.TenantID`/`Runtime.SourceID`. Group points by `Runtime.SourceRevision` (first-seen order), split each group into requests of ≤ 256 points; `PUT /collections/{c}/points?wait=true` with `{"points":[{"id":ID,"vector":{"dense":Dense},"payload":{"_rt":{<Runtime JSON fields>,"text":Text},"f":Fields-or-{}}}],"update_filter":{"must":[{"key":"_rt.sourceRevision","range":{"lte":rev}}]}}` — the update applies only when the stored revision is ≤ the incoming one; a missing point is inserted (Qdrant semantics; ADR D11 handles that on reads).
- `DeleteOtherRevisions`: empty tenant or source → error, no request. `POST /collections/{c}/points/delete?wait=true` `{"filter":{"must":[{"key":"_rt.tenantId","match":{"value":T}},{"key":"_rt.sourceId","match":{"value":S}}],"must_not":[{"key":"_rt.sourceRevision","match":{"value":keep}}]}}`.
- Selector → filter: `must` of `_rt.tenantId` `match.value`, `_rt.sourceId` `match.any`, `_rt.subjects` `match.value`, only for set fields. `DeleteByFilter`/`Count` refuse `sel.IsZero()` with no request. `DeleteByFilter` → `POST …/points/delete?wait=true {"filter":…}`; `Count` → `POST …/points/count {"filter":…,"exact":true}` → `result.count`.
- `renderFilter`: leaf key is `"f." + Field`; `Match` len 1 → `{"key":k,"match":{"value":v}}`, len > 1 → `{"key":k,"match":{"any":vals}}`; `Range` → `{"key":k,"range":{…only set bounds…}}`; `Exists` → `{"must_not":[{"is_empty":{"key":k}}]}`; a composite node → `{"must":[…],"should":[…],"must_not":[…]}` (empty lists omitted).
- `scopeFilter` = `{"must":[{"key":"_rt.tenantId","match":{"value":T}},{"key":"_rt.visibleTo","match":{"any":principals}}, <renderFilter(user) when non-nil>]}`.
- `Query` checks, before any request: `TenantID != ""`; `len(Principals) > 0` and none empty; at least one of `len(Dense) > 0` / `Keyword != ""`; a present arm's weight `> 0`; `1 <= Limit <= 500`; `PerArm = max(PerArm, Limit)`, `<= 500`. Body for `POST /collections/{c}/points/query`:
  ```json
  {"prefetch":[
     {"query":[…Dense],"using":"dense","filter":S,"limit":PerArm},
     {"query":{"text":"…Keyword"},"using":"_rt.text","filter":S,
      "params":{"idf":{"corpus":{"must":[{"key":"_rt.tenantId","match":{"value":T}}]}}},"limit":PerArm}],
   "query":{"rrf":{"weights":[Weights.Dense, Weights.Keyword]}},
   "filter":S,"limit":Limit,"with_payload":["_rt","f"]}
  ```
  where `S = scopeFilter(...)`; absent arms are omitted together with their weight (one prefetch, one weight). The scope filter is repeated on every prefetch and at top level on purpose, so no Qdrant propagation rule can leave an arm unscoped.
- `Diagnostics: true` → `POST /collections/{c}/points/query/batch` `{"searches":[<fused body>, <each present arm alone: its prefetch body plus "with_payload":false>]}`; `DenseScore`/`KeywordScore` are set for candidates that appear in that arm's result, by point id.
- Decode `result.points[]` → `Candidate{PointID, Runtime (from payload._rt), Text (payload._rt.text), Fields (payload.f), Score}`.

- [ ] **Step 1: Write the failing tests:**
  - `TestUpsert_ConditionalOnRevisionPerGroup`: revisions `[2,2,3]` → 2 requests; first has 2 points and `update_filter.must[0] == {"key":"_rt.sourceRevision","range":{"lte":2}}`, second `lte 3`; path ends `?wait=true`.
  - `TestUpsert_PayloadShape`: `_rt` holds exactly `tenantId sourceId sourceRevision chunkId ordinal visibleTo subjects contentHash ingestionProfile charStart charEnd text` (+`page` when non-zero); `f.status == "open"`; `vector.dense` length 4; `id == p.ID`.
  - `TestUpsert_RefusesInvalidPointsBeforeAnyRequest`: nil `Dense`, empty `ID`, empty `TenantID` → error, 0 requests.
  - `TestUpsert_SplitsAt256`: 600 points, one revision → requests of 256, 256, 88.
  - `TestDeleteOtherRevisions_Body` (exact JSON) and `…_RefusesEmptyKeys`.
  - `TestSelector_RefusedWhenZero`: `DeleteByFilter`/`Count` with `Selector{}` → error, 0 requests.
  - `TestCount_Body`: `Selector{A,[s1,s2],"user:x"}` → `{"filter":{"must":[tenant value, sourceId any, subjects value]},"exact":true}`; response count 7 → 7.
  - `TestQuery_RefusesUnscoped`: `TenantID ""`, `Principals nil`, `Principals []string{""}`, no arm, `Limit 0`, `Limit 501`, present arm with weight 0 → error each, 0 requests.
  - `TestQuery_HybridBody`: weights `{2, 0.5}` → JSON equal to the template above (dense prefetch first, keyword prefetch with `params.idf.corpus`, `rrf.weights [2,0.5]`, identical scope filter on both prefetches and top level).
  - `TestQuery_SingleArm`: dense nil → one text prefetch, `weights [wk]`; keyword `""` → one dense prefetch, no `params`.
  - `TestQuery_PerArmAtLeastLimit`: `PerArm 5, Limit 10` → prefetch limits 10.
  - `TestQuery_UserFilterIsNamespaced`: `Filter{Must: []engine.Filter{{Field: "_rt.tenantId", Match: []any{"x"}}}}` → third element of `filter.must` is `{"must":[{"key":"f._rt.tenantId","match":{"value":"x"}}]}`.
  - `TestRenderFilter` table: match one/many, range with only `Gte`, exists, should, must_not, nested composite.
  - `TestQuery_DecodesCandidates`: canned response → `Candidate` fields; arm scores nil.
  - `TestQuery_Diagnostics`: batch endpoint, 3 searches, arm scores mapped by id; a candidate absent from the keyword arm has `KeywordScore == nil`.
- [ ] **Step 2: Run** `cd backend && go test -race ./internal/core/retrieval/engine/qdrant/ -v` — Expected: FAIL on the new tests.
- [ ] **Step 3: Implement** `points.go` and `query.go`.
- [ ] **Step 4: Run the unit tests and the full live suite**

```bash
cd backend && go test -race ./internal/core/retrieval/engine/qdrant/ -v
QDRANT_TEST_URL=http://127.0.0.1:16333 QDRANT_TEST_API_KEY=local-test-key go test -race ./internal/core/retrieval/engine/qdrant/ -run 'TestConformance|TestLive' -v
docker stop qdrant-pr3-test
```
Expected: PASS, every conformance case listed in Task 5 reported `--- PASS` (none skipped). If a case fails because Qdrant's behaviour differs from the docs cited above (for example `using` on a dotted payload key), fix the driver, not the suite, and note it in the PR description.

- [ ] **Step 5: Commit** — `feat(retrieval): Qdrant upsert, deletes, counts and hybrid query`.

---

### Task 8: Module skeleton in the core catalog

**Files:**
- Create: `backend/internal/core/retrieval/module.go`, `backend/internal/core/retrieval/module_test.go`, `backend/internal/core/retrieval/repository/collections.go`, `backend/internal/core/retrieval/AGENTS.md`
- Modify: `backend/cmd/server/catalog.go:3-55`, `backend/cmd/server/core_load_order_test.go:11-38`, `backend/cmd/server/config_declarations_test.go:13`
- Docs: `AGENTS.md` (root: L3, L36, L45, module table after `llm` row, load-order paragraph), `backend/AGENTS.md` (L3 "9 core modules", L37, L62, project tree, L246), `backend/internal/core/AGENTS.md` (L10 "nine", L15, module map, DAG, edges, service-key table), `backend/pkg/sdk/AGENTS.md:502` ("nine" → "ten")

**Interfaces:**
- Consumes: `services.New`/`Options`/`Seal`/`Engine` (Task 4), `qdrant.New`/`Config` (Tasks 5–6), `module.ServiceRetrieval` (Task 1).
- Produces:
  ```go
  // repository/collections.go
  const CollCorpusState = "retrieval_corpus_state" // installation-wide, _id = corpus; shape owned by PR 4
  // module.go
  func NewModule() *Module
  func (m *Module) Name() string                 // "retrieval"
  func (m *Module) DisplayName() string          // "Retrieval"
  func (m *Module) Description() string          // "Corpora, indexing and hybrid search over tenant data with local inference (ADR-0023)"
  func (m *Module) Category() module.ModuleCategory // CategoryCore
  func (m *Module) Dependencies() []string       // {"tenant", "authz", "llm"}
  func (m *Module) ProvidedServices() []module.ServiceKey // {module.ServiceRetrieval}
  func (m *Module) Collections() []module.CollectionSpec  // {{Name: repository.CollCorpusState}}
  func (m *Module) Init(deps *module.Dependencies) error
  func (m *Module) Start(ctx context.Context) error // seals registration; idempotent
  func (m *Module) Stop(ctx context.Context) error  // nil (PR 4 adds the worker)
  func (m *Module) HealthCheck(ctx context.Context) error
  ```
  No `ConfigSchema` (the Qdrant settings are operator environment, never database or API — ADR D4/D9), no permissions, routes or nav items in this PR.

Behaviour:
- Init reads, with `os.Getenv` (trimmed): `QDRANT_URL`, `QDRANT_API_KEY`, `QDRANT_COLLECTION_PREFIX` (default `orkestra`, must match `^[a-z][a-z0-9_-]{0,31}$`, else WARN and use the default). `QDRANT_URL` empty → INFO `retrieval: QDRANT_URL is not set; search answers not configured until the retrieval overlay is enabled`, engine nil. `qdrant.New` error → WARN `retrieval: QDRANT_URL is not a valid http(s) URL; the vector engine is treated as not configured` (the value is never logged), engine nil. URL set and key empty → WARN `retrieval: QDRANT_API_KEY is not set`. Init never contacts Qdrant or `llm`, never returns an error for any of these (a core Init error is fatal — D1), and registers `services.New(...)` under `module.ServiceRetrieval`.
- Start calls `svc.Seal()`: every module's Init has run by then (`InitAll` precedes `StartAll` in `cmd/server/main.go`), so consumers that declare a dependency on `retrieval` register during their own Init; a module retried later with `RetryInit` cannot register a corpus.
- HealthCheck (D1 degraded table; `/v1/admin/modules/health` only — `/ready` does not consult modules, `cmd/server/health.go:73`): engine nil → nil (not configured is the designed degraded mode, not a fault; PR 7's admin page reports "not configured"); otherwise `engine.Health` under a 2s timeout; any error → return `iface.ErrRetrievalUnavailable` itself (the handler copies `err.Error()` into the response, so no driver detail), and log the driver error at DEBUG.

- [ ] **Step 1: Write the failing tests** (`module_test.go`; Init uses `module.Dependencies{Logger: <buffer logger>, Services: module.NewServiceRegistry()}`, DB nil):
  - `TestModule_Declarations`: name, category, dependencies exactly `{tenant authz llm}`, provided `{ServiceRetrieval}`, collections exactly `[retrieval_corpus_state]`, `module.PermissionsOf(m)` and `module.ConfigSchemaOf(m)` empty.
  - `TestModule_InitWithoutQdrantURL`: `t.Setenv("QDRANT_URL", "")` → Init nil; `module.GetTyped[iface.RetrievalCorpusRegistry](reg, module.ServiceRetrieval)` ok; `svc.Engine()` → `ErrRetrievalNotConfigured`; `HealthCheck` nil.
  - `TestModule_InitNeverContactsTheEngine`: httptest server counting requests; `QDRANT_URL` = its URL, key `k-SECRET` → Init nil, `Engine()` non-nil, request count 0; logs do not contain `k-SECRET`.
  - `TestModule_InitMalformedURLWarns`: `QDRANT_URL=ftp://user:pw@host` → Init nil, WARN logged, log text does not contain `pw@host`, `Engine()` → `ErrRetrievalNotConfigured`.
  - `TestModule_InitMissingKeyWarns` and `TestModule_InitBadPrefixFallsBack` (`QDRANT_COLLECTION_PREFIX=Bad Prefix` → `svc.CollectionPrefix() == "orkestra"` and a WARN).
  - `TestModule_HealthCheckUnavailable`: server answers 503 → `errors.Is(err, iface.ErrRetrievalUnavailable)` and `err.Error()` contains neither server URL nor key; server answers version `1.20.0` and `/collections` 200 → nil.
  - `TestModule_StartSealsRegistration`: after `Start`, `RegisterCorpus` with a valid spec (a local helper mirroring Task 4's `validSpec`) returns an error containing `registration is closed`; `Start` twice → nil.
  - In `cmd/server`: `TestCoreModulesLoadOrder` `want` becomes `{"user","notification","tenant","authz","auth","navigation","logging","llm","retrieval","compliance"}`; `coreModuleCount` becomes `10`.
- [ ] **Step 2: Run** `cd backend && go test ./internal/core/retrieval/ ./cmd/server/ -run 'TestModule|TestCoreModulesLoadOrder|TestConfigDeclarations' -v` — Expected: FAIL (`undefined: NewModule`, load order mismatch).
- [ ] **Step 3: Implement** `module.go` (embed `module.BaseModule`), `repository/collections.go`; in `catalog.go` import `internal/core/retrieval`, insert `func() module.Module { return retrieval.NewModule() },` between the `llm` and `compliance` entries (L52-53) and add a `retrieval` bullet to the comment above `coreModules` (ADR-0023; depends on tenant/authz/llm; inits before compliance, which pushes its audit sink in later — PR 6); extend the `TestCoreModulesLoadOrder` comment with why `retrieval` sits after `llm`.
- [ ] **Step 4: Write `retrieval/AGENTS.md`** (sections: Purpose; What it owns (this PR: corpus registry, engine contract, Qdrant driver, conformance suite; later PRs listed by number); MongoDB collections (`retrieval_corpus_state`, installation-wide, declared only); Dependencies and load order; Lifecycle (Init env table `QDRANT_URL`/`QDRANT_API_KEY`/`QDRANT_COLLECTION_PREFIX`, never contacts anything; Start seals registration; HealthCheck semantics); Corpus registration rules (Task 4 list, locality checked at profile resolution in PR 4/5); Engine contract (package boundaries, value representation incl. datetime ms, payload namespaces `_rt`/`f`/`_rt.text`, point ids, collection names); Qdrant driver (v1.20.0 floor and why, error classification, BM25 tenant statistics via `params.idf.corpus`, conditional upsert semantics and the insert limitation); Invariants (no route/permission yet; consumers never import `engine`); Verification (unit command, live command with `QDRANT_TEST_URL`, the throwaway-container recipe of Task 5). Update the root/backend/core/sdk AGENTS.md lines listed under **Files**: "nine"/"9" → "ten"/"10", load order with `retrieval` between `llm` and `compliance`, a `retrieval` row in each module table (`Corpora, hybrid search with tenant-scoped BM25, principal filter; Qdrant behind an engine driver (ADR-0023)`), the `ServiceRetrieval` row in `core/AGENTS.md`'s service-key table, and a `retrieval` box/edge in its DAG.
- [ ] **Step 5: Run** `cd backend && go test ./internal/core/retrieval/... ./cmd/server/ && go vet ./internal/core/retrieval/...` — Expected: PASS (live tests skip without `QDRANT_TEST_URL`).
- [ ] **Step 6: Commit** — `feat(retrieval): core module skeleton between llm and compliance`.

---

### Task 9: Docs site and the remaining "nine core modules" references

**Files:**
- Create: `docs/site/modules/core/retrieval.mdx`
- Modify: `docs/site/modules/core/compliance.mdx:3` (`sidebar_position: 10`), `docs/site/modules/core/_category_.json` (description list adds `retrieval` before `compliance`), `docs/site/modules/index.mdx:9,15,19`, `docs/site/intro.mdx:12,19,31`, `README.md:125`, and every other hit of `grep -rn -i -E "nine (core|always)|9 core|nine modules|nine core" docs/site docs/onboarding .claude/skills/orkestra-go/SKILL.md` (today: `modules/addons/index.mdx:9`, `operating/troubleshooting.mdx:146`, `sdk/module-interface.mdx:8`, `operating/deployment/single-vm.mdx:28`, `architecture/index.mdx:9`, `architecture/module-lifecycle.mdx:8,12`, `getting-started/enabling-modules.mdx:8`, `getting-started/index.mdx:9,27,31`, `getting-started/forking.mdx:102`, `getting-started/installation.mdx:67`, `api/index.mdx:13`, `docs/onboarding/orkestra-sdk.md:8,15`, `.claude/skills/orkestra-go/SKILL.md:134`)

**Interfaces:**
- Consumes: Task 8's AGENTS.md as the source of facts.
- Produces: nothing code-facing.

- [ ] **Step 1: Write `retrieval.mdx`** — front matter `title: retrieval`, `sidebar_position: 9`; sections: intro (ADR-0023 link `/adrs/core-retrieval-module`, tenth core module, always loaded, optional infrastructure); "Status" (delivery step 1 in progress: this release ships the SDK contract, corpus registration and the engine driver; indexing, search, erasure and the admin page arrive in the next releases); "Degraded mode" (D1 table); "Declaring a corpus" (a minimal `RegisterCorpus` example in a fork module's Init with `Dependencies() = {"retrieval"}`, the validation rules in short, registration closes when the module starts); "Filters" (the six types, the op table, limits depth 4 / 64 nodes / 100 values); "Configuration" (`QDRANT_URL`, `QDRANT_API_KEY`, `QDRANT_COLLECTION_PREFIX`, Qdrant ≥ 1.20); "Legacy RAG contract" (D16 deprecation).
- [ ] **Step 2: Update the listed files** — counts to ten, lists and load-order chains include `retrieval` between `llm` and `compliance`; `modules/index.mdx:19` gains a sentence that `retrieval` (ADR-0023) is the tenth core module, catalogued after `llm` and before `compliance`.
- [ ] **Step 3: Verify** — `grep -rn -i -E "nine (core|always)|9 core|nine modules|nine core|llm → compliance|llm\` → \`compliance" AGENTS.md README.md backend/AGENTS.md backend/internal/core/AGENTS.md backend/pkg/sdk/AGENTS.md docs/site docs/onboarding .claude/skills/orkestra-go/SKILL.md` prints nothing; render the site locally per `docs/site/README.md` and check that the `retrieval` page sits between `llm` and `compliance`.
- [ ] **Step 4: Full gate** — restart the throwaway Qdrant with the Task 5 `docker run` line, then `make ci-backend` from the repo root with `MONGO_TEST_URI` set (the dev instance's throwaway database, as `backend/internal/core/llm/AGENTS.md` "Verification" shows) and `QDRANT_TEST_URL=http://127.0.0.1:16333 QDRANT_TEST_API_KEY=local-test-key`; `docker stop qdrant-pr3-test` afterwards. Expected: `Backend CI: OK`, and the `TestConformance` subtests reported as run, not skipped.
- [ ] **Step 5: Commit** — `docs(retrieval): module page and the tenth core module across the docs`.
