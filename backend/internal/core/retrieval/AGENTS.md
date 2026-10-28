# Module: Retrieval — corpora, indexing and hybrid search over tenant data

_Path: `/backend/internal/core/retrieval`_
_Parent: [../AGENTS.md](../AGENTS.md)_

[← Core](../AGENTS.md) | [☰ Backend](../../../AGENTS.md) | [Root](../../../../AGENTS.md)

## Purpose

Lets modules declare **corpora** (searchable collections of their own data), indexes them through a transactional outbox and a worker (PR 4), and, in later steps, answers hybrid (dense + BM25) searches with a principal filter, over a vector engine behind a driver boundary ([ADR-0023](../../../../docs/adr/0023-core-retrieval-module.md)). Inference is local by default: a corpus is `local_only` unless its owner declares `hosted_allowed`.

## What it owns

PR 3 delivered the registry, the engine contract, the Qdrant driver and the conformance suite; PR 4 adds indexing (outbox, worker, corpus state, chunking). There is still no route, permission or nav item; `ConfigSchema` holds only the three indexing keys.

| File | Purpose |
|---|---|
| `module.go` | `Module`: declarations, `Init` (operator env, builds the engine and the service, registers `module.ServiceRetrieval`), `Start` (seals registration), `HealthCheck` |
| `repository/` | Collection constants and index specs; `Outbox`, `CorpusStates`, `TenantProgress`, `Erasures` repositories |
| `models/` | Document shapes: `OutboxEntry` (+ `AppliedState`), `CorpusState`, `TenantProgress`, `Erasure` |
| `chunk/` | `Split`, `Validate`, `CountTokens`: the `none` and `fixed` strategies and the token approximation |
| `services/` | `Service` (`New(Options{Engine, CollectionPrefix, Logger})`, `Seal`, `Engine()`, `CollectionPrefix()`, `SetOutbox`): the corpus registry and principal-contributor registry; implements `iface.RetrievalCorpusRegistry` and `iface.RetrievalOutbox`. Also `Outbox`, `Worker` (`worker.go`, `worker_pass.go`, `gate.go`), `Tombstones`, `IngestionProfileHash`, item validation (`ingest.go`) |
| `engine/` | Engine-neutral contract: `Engine`, `Point`, `Query`, `Filter`, `Selector`, `Runtime`, `PointID`, sentinels |
| `engine/qdrant/` | Qdrant REST driver (`New(Config{URL, APIKey, Timeout, MaxResponseBytes, HTTPClient, Logger})`) |
| `enginetest/` | Conformance suite `Run(t, newEngine)`: 22 cases (incl. `EmptySourceIDsRefused`, `SourceIDsWithoutTenantRefused`, `UpsertReplacesPayload`, tenant-scoped BM25, principal filter, reserved-namespace isolation); imports `engine` and `iface` |
| `filter/` | `Validate`s a public `iface.RetrievalFilter` against a corpus's declared fields (imports `iface`). It only validates in this PR: `Compile` into `engine.Filter` arrives in PR 5 |

Later PRs: 5 search path, filter compilation use and principal resolver; 6 compliance wiring (audit sink, DSR); 7 admin page.

## MongoDB collections

| Collection | Scope | Notes |
|---|---|---|
| `retrieval_corpus_state` | installation-wide, `_id` = corpus name | One document per corpus: status, active generation, collection, profile hashes. Reads and writes carry `//tenantscope:allow system:` |
| `retrieval_outbox` | tenant-scoped (`tenantrepo`) | One row per (tenant, corpus, source) key. `DueTenants` is the only cross-tenant sweep (`system:`) |
| `retrieval_tenant_progress` | tenant-scoped | Resumable initial build per (tenant, corpus, generation): cursor, enqueued count, lease |
| `retrieval_erasures` | global + tenant | Tombstones (`scope` `global` for user tombstones, `tenant` otherwise). The insert of a global row is `dsr:`; the covering lookup of global rows is `system:` |

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

`Start` calls `Seal()` first (every module's Init has run by then, so a module retried later with `RetryInit` cannot register a corpus), then starts the index worker goroutine under `context.WithoutCancel(ctx)` (the admin enable endpoint passes a request context; precedent `auth/maintenance.go`), so only `Stop` ends it; a second `Start` while running is a no-op. `Stop` cancels the worker and waits for it or the context. A key held at that moment still ends its lease: `Release`, `Reschedule` and `Requeue` run under `context.WithTimeout(context.WithoutCancel(tctx), 5s)`, while the `HoldsLease` guard and the Load/Embed/engine calls stay on the run context (a cancellation that surfaces at the guard re-queues the key, which then stays leased until expiry). `Init` builds the outbox (`SetOutbox`, so `Enqueue` whenever a database handle is present) and, when an engine is configured and the gateway (`LLMPinnedEmbedder`, `LLMInferenceProfiles`), tenant lister and module status reader resolve, the worker; otherwise the worker is nil and one WARN names what is missing. Config (group `indexing`, read every pass): `worker_lease` (2m, `RETRIEVAL_WORKER_LEASE`), `worker_batch` (32, `RETRIEVAL_WORKER_BATCH`; a value <= 0 falls back to 32, only the upper bound 256 clamps), `worker_poll_interval` (5s, `RETRIEVAL_WORKER_POLL_INTERVAL`). Tests inject an engine through the unexported `Module.newEngine`. The one case where the outbox is not wired is a nil `deps.DB` (production always has one; the repository constructors would panic): `Init` still returns nil with a single WARN naming the missing database, there is no worker, and `Enqueue` returns an error.

`HealthCheck` (only `/v1/admin/modules/health`; `/ready` does not consult modules): engine nil returns nil (not configured is the designed degraded mode); worker nil returns `retrieval: indexing disabled: <missing>`; otherwise `engine.Health` under a 2s timeout, and any error returns `iface.ErrRetrievalUnavailable` itself, because the handler copies `err.Error()` into the response. The driver error goes to the DEBUG log. A healthy engine then reports `Worker.Problems()` as `retrieval: <problem>; ...`.

## Indexing

### For owners

- Call `RetrievalOutbox.Enqueue(ctx, intents...)` with the `mongo.SessionContext` of the transaction that writes the source, so the intent commits or aborts with it. At most 1000 intents per call; `SourceID` 1-256 bytes, valid UTF-8, no NUL; `Revision >= 0`. The tenant must be internal; the corpus must be registered. Validation failures wrap `iface.ErrRetrievalInvalidIntent` / `ErrRetrievalUnknownCorpus`.
- `Revision` is monotonic per item (the outbox keeps the highest with `$max`); a `Load` that returns an older revision than enqueued is retried (`source_behind`). It must stay monotonic per id **across delete and re-create**: an id re-created with a lower revision keeps the old `$max` value and stays `source_behind` forever.
- `Enqueue` keeps an existing backoff (`nextAttemptAt` is only set on insert): an item parked as `invalid_item` and then fixed waits up to 30m before it is retried.
- `Load(ctx, tenantID, ids)` answers only for the tenant passed and reports an absent id as `Deleted`; the worker also treats a missing entry as deleted. `Enumerate(ctx, tenantID, cursor, limit)` pages by an opaque cursor (`""` starts; `Next == ""` ends) and feeds the initial build.
- Item bounds: title cut to 512 bytes (rune boundary) and prefixed to every chunk with `\n\n`; `VisibleTo` <= 1000 and `Subjects` <= 100 entries, each 1-256 bytes; fields must be declared, keyword <= 1024 bytes, keyword[] <= 100 elements. A violation is `invalid_item`.

### Per-key protocol

`Claim` sets `dirty=false` and `epoch += 1` and takes the lease (owner, `leaseUntil`). Every write is preceded by `HoldsLease(epoch, owner)`; `Release` and `Reschedule` are fenced on the same pair; `Release` leaves `dirty` untouched, so an intent enqueued during the lease survives, and `Reschedule` sets `dirty: true` with the backoff. A lost lease at a guard re-queues the key (`Requeue`, no release); a `Reschedule` that finds the lease lost does nothing unless the run already wrote to the engine, in which case the key is re-queued. The write deadline is `leaseUntil - lease/4`: Load, Embed and engine calls use a context with that deadline. `services.IndexingFencingWindow` (`2 x MaxWorkerLease` = 30m) is how long a stale worker can still write after losing its lease; PR 6 erasure waits it out.

Processing order of `process` (`worker.go`):

1. Item tombstone covers the key: remove. 2. `Load`: error `source`; a reply that cannot be trusted for the id (two entries for it, an entry neither `Deleted` nor carrying an `Item`, or an `Item.SourceID` naming another id) `invalid_item`; absent or deleted: remove; older revision than enqueued: `source_behind`. 3. Subject tombstone against the item's subjects: remove. 4. `normalizeFields`, `validateItem`: `invalid_item`. 5. `chunk.Split` and build points (no points: remove; text cut by the chunk bounds logs a WARN). 6. If `Applied` has the same generation, profile hash, indexed hash and chunk count (revision ignored): `UpdatePayload`, `DeleteOtherRevisions`, then `Count`; a count equal to the points released as payload-only, otherwise fall through. 7. Embed in batches of 32 (`budget` when the tenant budget is exhausted, else `embed`; a wrong vector count or length is `embed`). 8. `Upsert`, `DeleteOtherRevisions`, `Release` with the new `Applied`.

Backoff `5s * 2^(attempts-1)`, capped at 30m; `invalid_item` always waits 30m; keys are never abandoned. `lastError` and the logs carry only these closed codes: `source`, `source_behind`, `invalid_item`, `embed`, `budget`, `engine`, `deadline` (any error that is a `context.DeadlineExceeded`), `tombstone`. Logs name corpus, tenant, source ID, counts and codes, never text, field values, principals or subjects.

### Passes and the initial build

A pass (`RunOnce`) prepares every corpus (the gate below), claims keys round-robin across at most 64 due tenants until `worker_batch` keys are claimed, then advances initial builds. Passes repeat every `worker_poll_interval`; settings are re-read on every pass and clamped. Fairness state is per process (in memory, each replica keeps its own):

- **Tenant cursor.** `DueTenants(now, corpora, after, 64)` lists tenants with a claimable key whose ID sorts after the cursor, in tenant ID order (never by age, so an old backlog cannot win every pass). The cursor moves to the last tenant that received a claim; it wraps to `""` when the list was shorter than 64 and every listed tenant was offered a claim, it moves to the last listed tenant (past a full list) when nothing in it was claimable, and an empty list after a non-empty cursor is retried once from `""` in the same pass. With more due tenants than `worker_batch`, every tenant gets a claim within `ceil(tenants / worker_batch)` passes; the pass that reaches the tail of the list may claim fewer than `worker_batch` keys.
- **Build pass.** One `Enumerate` page (200 ids, call timeout `lease/2`) per (corpus, tenant) per pass, at most 16 pages per pass overall (a failed call counts); it only enqueues intents, the index pass writes. The sorted (corpus, tenant) pairs are walked from an offset that advances by the pairs visited, so the next pass starts where this one stopped. After an `Enumerate` error the corpus's remaining tenants are skipped for the rest of the pass (one call per pass for a broken source), and if any call failed the next pass instead starts at the pair right after the first failing pair of this pass (mod pair count): advancing by the visited count would leave the offset unchanged after a full walk and starve the tenants behind the failing one; a wholly failing source retries each tenant in turn. A build is remembered as done when this worker's `Advance(done)` succeeds, or when `ClaimBuild` returns nil and `Get` shows `done`; a done build costs no store call afterwards, a build held by another replica costs `ClaimBuild` + `Get` per pass.

### Gate (`prepareCorpus`)

Checks run in this order; the problem text is what `HealthCheck` reports (`%s` corpus, `%q` endpoint).

| Condition | State write | Result | Problem |
|---|---|---|---|
| Owner module not started | none | skip | none (expected while disabled; intents are kept) |
| Endpoint unknown (`ErrLLMNotConfigured`) | error / `endpoint_unknown` | skip | `corpus %s: embed endpoint %q is not configured` |
| Other profile error, or dimensions <= 0 | none | skip | `corpus %s: inference profile unavailable` |
| Endpoint is not an embedding endpoint | error / `endpoint_kind` | skip | `corpus %s: endpoint %q is not an embedding endpoint` |
| `local_only` corpus (or `""`) and endpoint boundary not local | error / `locality` | skip | `corpus %s: local_only corpus uses hosted endpoint %q` |
| No state or no active generation | `EnsureCollection`, then `active`, generation 1, collection `<prefix>_<corpus>_g1` | gate | engine error: `corpus %s: vector engine unavailable` |
| State hash differs from the declared hash | `profile_changed` with `DeclaredProfileHash` (written once) | skip | `corpus %s: ingestion profile changed; indexing paused until a new generation is built` |
| Not active, hash equal | the once-per-process `EnsureCollection` below, then back to `active`, reason cleared | gate | as below |
| Active, hash equal | none (the first time this process gates the generation: `EnsureCollection` with the current filters, analyzer and stored dimensions, so a filter declared after the bootstrap gets its payload index) | gate | engine error: `corpus %s: vector engine unavailable` (skip, retried next pass) |

`profile_changed`: intents are kept, nothing is claimed, the active generation keeps serving, and delivery step 2 (generation build) builds the new generation and drains the intents. Reverting the declaration clears it. `ErrStateExists` / `ErrStateConflict` (another replica moved the state) skip the pass silently.

### Ingestion profile hash

`IngestionProfileHash` = `"ing_"` + hex SHA-256 of the JSON of a private struct `{v, embeddingProfile, analyzer{tokenizer, language, stemming, stopWords}, chunking{strategy, maxTokens, overlap}, enrichment{chatEndpoint, promptVersion}|null}` (tags independent of `iface`). `v` (now 1) is bumped when the code's transformation changes (title prefix, tokenization). Normalization: tokenizer `""` becomes `word`; strategy `""` becomes `none`; `none` zeroes `maxTokens` and `overlap`. `EmbedEndpoint` (the endpoint **name**) is excluded: the resolved profile ID identifies the model, and renaming an endpoint must not start a new generation.

### Chunking and the token approximation

A token is a maximal run of runes for which `unicode.IsSpace` is false, when that run has at most 32 runes; a longer run counts as consecutive 32-rune pieces (the last may be shorter). Whitespace is never a token. One counted token approximates one word; subword tokenizers (bge-m3's XLM-R) produce roughly 1-2 model tokens per word in English and Italian, so a window of `MaxTokens` counted tokens may exceed `MaxTokens` model tokens. Operators choose `MaxTokens <= MaxInputTokens/2` of the embedding profile; the profile's `Truncate` covers any excess (the tail of an over-long window still reaches BM25, not the vector). A chunk's text runs from its first token's first rune to its last token's last rune.

`none`: one chunk, cut at `MaxChunkBytes` (64512). `fixed`: windows of `MaxTokens` with `Overlap`, at most `MaxChunksPerItem` (512), each also bounded by `MaxChunkBytes`. `MaxTokens` is 1-4096 (`chunk.MaxTokensLimit`): registration calls `chunk.Validate` for `none` and `fixed`, so a corpus the chunker would refuse never registers. Provenance offsets `CharStart`/`CharEnd` are rune offsets in the item text.

### Engine additions and config

`Engine.UpdatePayload` replaces `Runtime` and `Fields` of existing points (vector and text kept) only when the stored content hash matches and the stored revision is not higher; it never creates a point and sends every `_rt` field, `page` included. Config group `indexing`: `worker_lease` (2m, clamp 30s-15m, `RETRIEVAL_WORKER_LEASE`), `worker_batch` (32, `RETRIEVAL_WORKER_BATCH`: <= 0 falls back to 32, above 256 clamps to 256), `worker_poll_interval` (5s, clamp 1s-5m, `RETRIEVAL_WORKER_POLL_INTERVAL`). `HotReloadConfig()` is `true` (the worker's settings closure re-reads all three on every pass, so a config write leaves no restart-required flag; any later per-call key, e.g. PR 5's, is covered by the same module-wide flag). `HealthCheck` strings: `retrieval: indexing disabled: <missing>`, `retrieval: the vector engine is unavailable` (the bare `iface.ErrRetrievalUnavailable`), `retrieval: <problem>; <problem>`.

## Corpus registration rules

`RegisterCorpus` is syntax-only, runs during any module's `Init`, never contacts the engine or llm, and fails once sealed. Rules (`services/registry.go`):

- Name `^[a-z][a-z0-9_]{0,31}$`, unique; owner `^[a-z][a-z0-9_-]{0,63}$`; permission `a.b[.c[.d]]` lowercase; `Source` required.
- At most 32 filters; names `^[a-z][a-zA-Z0-9_]{0,63}$`, unique, known type, description 1..500 bytes.
- `embedEndpoint` required (no whitespace, <= 64 bytes). Tokenizer `word` (default) or `multilingual`; stemming needs a Snowball language, stop words a stop-word language (Qdrant 1.20 enums). Chunking `none` (no sizes) or `fixed` (`maxTokens` 1..4096, `0 <= overlap < maxTokens`, checked by `chunk.Validate`); `hierarchical` and enrichment are refused until delivery step 3.
- Locality `""` becomes `local_only`; fusion defaults 1/1/50 (weights in (0,10], candidates 1..500); rerank `topK` defaults to 20 (1..200), enabled needs an endpoint.
- The registry stores a normalized deep copy. Locality against the actual endpoints is checked at profile resolution (PR 4/5), not here.

## Engine contract

- Import direction: `engine` -> `iface`, `uuid`, stdlib; `engine/qdrant` -> `engine`, `iface`; `enginetest` -> `engine`, `iface`; `filter` -> `iface`; `services` -> `engine`, `filter`, `iface`; `module.go` -> `services`, `engine`, `engine/qdrant`, `repository`. `engine` never imports a driver, and consumers outside this module never import `engine`.
- Values: keyword `string`, keyword[] `[]string`, integer `int64`, float `float64`, bool `bool`, datetime `int64` Unix **milliseconds** UTC (`Point.Fields`) and `float64` milliseconds (`Range`). Datetime fields get an integer payload index, so range compilation needs no field-type lookup.
- Payload: `_rt` = module-only runtime fields plus `_rt.text` (the indexed text); `f` = declared filter fields. A user filter field is always rendered as `f.<name>`, never a raw key.
- `Upsert` replaces a point's whole payload (a field the new write omits is gone; conformance `UpsertReplacesPayload`). `UpdatePayload` (the payload-only path) **merges into `_rt`**: the driver sends Qdrant `set_payload` with `"key": "_rt"` (a top-level `set_payload` of `_rt` would replace the object and drop `_rt.text`, the indexed text; verified on Qdrant 1.20) and replaces `f` whole with a second `set_payload`, both filtered on the point id, the stored `_rt.contentHash` and `_rt.sourceRevision <= ` the incoming one. A merge never clears a key it omits, so the driver sends every `_rt` field, `page` included, through its own shape without `omitempty` (ruling R1).
- Point ids: UUIDv5 (`engine.PointID`) over `tenantID\x00corpus\x00sourceID\x00ordinal`. Collection names: `<prefix>_<corpus>_g<generation>`.

## Qdrant driver

- **Floor v1.20.0**: BM25 scoring on the full-text index. `Health` parses the server version as `major.minor` and refuses older servers (the error carries the parsed numbers, never the server string), then lists collections to prove the API key works.
- **Errors** never contain the URL, the key or a response body (a non-2xx body is drained, at most 64 KiB, for connection reuse, never read into the error). Classification:
  - `engine.ErrUnavailable`: network, dial or TLS failure; a timeout (the caller's deadline or the driver's `Timeout`, also `context.DeadlineExceeded`); 5xx; 401/403 (wrong API key); 429; a response over `MaxResponseBytes`; an undecodable body (every 2xx reply, including those of writes and deletes, must be a JSON object with `result` or `status:"ok"`, so a proxy's HTML 200 is an outage, not a success); `Health` on a server older than 1.20.
  - `engine.ErrNotFound`: 404, i.e. a missing collection (`DropCollection` treats it as success).
  - `context.Canceled` (not `ErrUnavailable`): the caller canceled the context.
  - Neither sentinel (a refusal): any other 4xx, and every check made before a request (empty collection name, invalid point, selector or query, malformed filter (including a composite node carrying its own Match, Range or Exists), dimension mismatch on an existing collection).
- **Fail closed**: `Query` requires a tenant and non-empty principals; selectors with `SourceIDs` set but empty (or holding an empty id), or with `SourceIDs` but no `TenantID` (source ids are tenant-local), are refused, while a `Subject`-only selector stays tenant-less on purpose (global user erasure); a malformed compiled filter is refused rather than dropped; the scope filter is repeated on every prefetch arm and at top level, and a candidate scope backstop drops (and WARNs about) any returned candidate outside tenant/principals.
- **BM25 statistics** are tenant-scoped via `params.idf.corpus` on the keyword arm; collections use one shard so statistics stay exact.
- **Conditional upsert**: a write applies only when the stored `_rt.sourceRevision` is <= the incoming one, so a request carries a single revision (groups are sent in order; a failure in a later group leaves earlier groups written). Limitation: a point that does not exist is inserted whatever its revision, so a stale writer racing a delete can recreate a point; detection is by revision on read (ADR D11).

## Invariants

- No route, permission, Cedar policy, errcode or nav item; the Qdrant settings are operator environment only (ADR D4/D9). `ConfigSchema` has only the three indexing keys.
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
