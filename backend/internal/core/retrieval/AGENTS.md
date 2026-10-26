# Module: Retrieval — corpora, indexing and hybrid search over tenant data

_Path: `/backend/internal/core/retrieval`_
_Parent: [../AGENTS.md](../AGENTS.md)_

[← Core](../AGENTS.md) | [☰ Backend](../../../AGENTS.md) | [Root](../../../../AGENTS.md)

## Purpose

Lets modules declare **corpora** (searchable collections of their own data) and, in later steps, indexes them and answers hybrid (dense + BM25) searches with a principal filter, over a vector engine behind a driver boundary ([ADR-0023](../../../../docs/adr/0023-core-retrieval-module.md)). Inference is local by default: a corpus is `local_only` unless its owner declares `hosted_allowed`.

## What it owns

This step (PR 3) delivers the registry, the engine contract, the Qdrant driver and the conformance suite. There is no route, permission, nav item or `ConfigSchema` yet.

| File | Purpose |
|---|---|
| `module.go` | `Module`: declarations, `Init` (operator env, builds the engine and the service, registers `module.ServiceRetrieval`), `Start` (seals registration), `HealthCheck` |
| `repository/collections.go` | `CollCorpusState` constant; no repository code yet |
| `services/` | `Service` (`New(Options{Engine, CollectionPrefix, Logger})`, `Seal`, `Engine()`, `CollectionPrefix()`): the corpus registry and principal-contributor registry; implements `iface.RetrievalCorpusRegistry` |
| `engine/` | Engine-neutral contract: `Engine`, `Point`, `Query`, `Filter`, `Selector`, `Runtime`, `PointID`, sentinels |
| `engine/qdrant/` | Qdrant REST driver (`New(Config{URL, APIKey, Timeout, MaxResponseBytes, HTTPClient, Logger})`) |
| `enginetest/` | Conformance suite `Run(t, newEngine)`: 21 cases (incl. `EmptySourceIDsRefused`, `SourceIDsWithoutTenantRefused`, tenant-scoped BM25, principal filter, reserved-namespace isolation); imports `engine` and `iface` |
| `filter/` | Compiles the public `iface.RetrievalFilter` into `engine.Filter` (imports `iface`) |

Later PRs: 4 outbox, indexing worker and corpus state (`retrieval_corpus_state` shape); 5 search path, filter compilation use and principal resolver; 6 compliance wiring (audit sink, DSR); 7 admin page.

## MongoDB collections

| Collection | Scope | Notes |
|---|---|---|
| `retrieval_corpus_state` | installation-wide, `_id` = corpus name | Declared only; the document shape and its repository belong to PR 4 |

## Dependencies and load order

`Dependencies()` is `tenant`, `authz`, `llm`. It sits between `llm` and `compliance` in `cmd/server/catalog.go` (pinned by `core_load_order_test.go`): compliance will push its audit sink into the retrieval service from its own Init (PR 6). Consumers (a module that registers a corpus) declare a dependency on `retrieval` so their Init runs after it.

## Lifecycle

`Init` reads the operator environment with `os.Getenv` (trimmed). It never contacts Qdrant or `llm` and never returns an error: a core Init error is fatal, so missing infrastructure is a log line.

| Variable | Meaning | When absent or bad |
|---|---|---|
| `QDRANT_URL` | `http(s)` URL of the Qdrant node | INFO "QDRANT_URL is not set..."; engine nil, search answers `ErrRetrievalNotConfigured` |
| `QDRANT_API_KEY` | Qdrant API key | WARN "QDRANT_API_KEY is not set" (URL still used) |
| `QDRANT_COLLECTION_PREFIX` | Collection name prefix, `^[a-z][a-z0-9-]{0,31}$` (no `_`: corpus names may hold one, so `a_b`+`c` would collide with `a`+`b_c`) | Default `orkestra`; an invalid value logs a WARN and uses the default |

An invalid URL (`qdrant.New` error) logs a WARN and is treated as not configured; the value is never logged, nor is the key. When not configured, `Init` passes an **untyped nil** engine to `services.New` (a typed-nil driver inside the interface would defeat `Service.Engine()`'s `== nil` check).

`Start` calls `Seal()`: every module's Init has run by then (`InitAll` precedes `StartAll`), so a module retried later with `RetryInit` cannot register a corpus. It is idempotent. `Stop` is a no-op until PR 4 adds the worker.

`HealthCheck` (only `/v1/admin/modules/health`; `/ready` does not consult modules): engine nil returns nil (not configured is the designed degraded mode); otherwise `engine.Health` under a 2s timeout, and any error returns `iface.ErrRetrievalUnavailable` itself, because the handler copies `err.Error()` into the response. The driver error goes to the DEBUG log.

## Corpus registration rules

`RegisterCorpus` is syntax-only, runs during any module's `Init`, never contacts the engine or llm, and fails once sealed. Rules (`services/registry.go`):

- Name `^[a-z][a-z0-9_]{0,31}$`, unique; owner `^[a-z][a-z0-9_-]{0,63}$`; permission `a.b[.c[.d]]` lowercase; `Source` required.
- At most 32 filters; names `^[a-z][a-zA-Z0-9_]{0,63}$`, unique, known type, description 1..500 bytes.
- `embedEndpoint` required (no whitespace, <= 64 bytes). Tokenizer `word` (default) or `multilingual`; stemming needs a Snowball language, stop words a stop-word language (Qdrant 1.20 enums). Chunking `none` (no sizes) or `fixed` (`maxTokens` 1..8192, `0 <= overlap < maxTokens`); `hierarchical` and enrichment are refused until delivery step 3.
- Locality `""` becomes `local_only`; fusion defaults 1/1/50 (weights in (0,10], candidates 1..500); rerank `topK` defaults to 20 (1..200), enabled needs an endpoint.
- The registry stores a normalized deep copy. Locality against the actual endpoints is checked at profile resolution (PR 4/5), not here.

## Engine contract

- Import direction: `engine` -> `iface`, `uuid`, stdlib; `engine/qdrant` -> `engine`, `iface`; `enginetest` -> `engine`, `iface`; `filter` -> `iface`; `services` -> `engine`, `filter`, `iface`; `module.go` -> `services`, `engine`, `engine/qdrant`, `repository`. `engine` never imports a driver, and consumers outside this module never import `engine`.
- Values: keyword `string`, keyword[] `[]string`, integer `int64`, float `float64`, bool `bool`, datetime `int64` Unix **milliseconds** UTC (`Point.Fields`) and `float64` milliseconds (`Range`). Datetime fields get an integer payload index, so range compilation needs no field-type lookup.
- Payload: `_rt` = module-only runtime fields plus `_rt.text` (the indexed text); `f` = declared filter fields. A user filter field is always rendered as `f.<name>`, never a raw key.
- Point ids: UUIDv5 (`engine.PointID`) over `tenantID\x00corpus\x00sourceID\x00ordinal`. Collection names: `<prefix>_<corpus>_g<generation>`.

## Qdrant driver

- **Floor v1.20.0**: BM25 scoring on the full-text index. `Health` parses the server version as `major.minor` and refuses older servers (the error carries the parsed numbers, never the server string), then lists collections to prove the API key works.
- **Errors**: classified onto `engine.ErrUnavailable` (network, 5xx, timeout) and `engine.ErrNotFound`; they never contain the URL, the key or a response body.
- **Fail closed**: `Query` requires a tenant and non-empty principals; selectors with `SourceIDs` set but empty (or holding an empty id), or with `SourceIDs` but no `TenantID` (source ids are tenant-local), are refused, while a `Subject`-only selector stays tenant-less on purpose (global user erasure); a malformed compiled filter is refused rather than dropped; the scope filter is repeated on every prefetch arm and at top level, and a candidate scope backstop drops (and WARNs about) any returned candidate outside tenant/principals.
- **BM25 statistics** are tenant-scoped via `params.idf.corpus` on the keyword arm; collections use one shard so statistics stay exact.
- **Conditional upsert**: a write applies only when the stored `_rt.sourceRevision` is <= the incoming one, so a request carries a single revision (groups are sent in order; a failure in a later group leaves earlier groups written). Limitation: a point that does not exist is inserted whatever its revision, so a stale writer racing a delete can recreate a point; detection is by revision on read (ADR D11).

## Invariants

- No route, permission, Cedar policy, errcode, nav item or `ConfigSchema` field in this step; the Qdrant settings are operator environment only (ADR D4/D9).
- `Init` never contacts Qdrant or an inference endpoint, and never returns an error.
- Logs and errors never carry the API key or the Qdrant URL.
- No bypass of the principal filter for any platform role (ADR D10).

## Verification

```bash
# unit (live tests skip without QDRANT_TEST_URL)
cd backend && go test ./internal/core/retrieval/... ./cmd/server/ && go vet ./internal/core/retrieval/...

# live conformance against a Qdrant v1.20.0 with an API key
QDRANT_TEST_URL=http://127.0.0.1:6333 QDRANT_TEST_API_KEY=<key> \
  go test ./internal/core/retrieval/engine/qdrant/ -count=1 -v
```

Throwaway server: `docker run -d --name qdrant-pr3-test -p 127.0.0.1:16333:6333 -e QDRANT__SERVICE__API_KEY=<key> qdrant/qdrant:v1.20.0` (CI pins the image by digest), then use `QDRANT_TEST_URL=http://127.0.0.1:16333`; remove the container afterwards. A green run without `QDRANT_TEST_URL` proves nothing about the driver.
