---
title: ADR-0023 — Core `retrieval` module for vector and hybrid search with local inference
status: proposed
public: true
---

# ADR-0023 — Core `retrieval` module for vector and hybrid search with local inference

| Field | Value |
|---|---|
| **Status** | 📝 Proposed |
| **Date** | 2026-10-26 |
| **Authors** | @salvatore.balestrino |
| **Amends** | [ADR-0006](0006-collapse-to-core-only-base.md): the core-only base gains a **tenth** core module. Finding tenant data by meaning, under the tenant's own permissions, is horizontal plumbing every product that uses a language model rebuilds; it is not a domain vertical. It passes the same test [ADR-0009](0009-core-compliance-module.md) and [ADR-0022](0022-core-llm-module.md) applied. [ADR-0009](0009-core-compliance-module.md): an erasure request is completed only when every producer reports its purge done (D12). |
| **Extends** | [ADR-0022](0022-core-llm-module.md): the `llm` module gains installation-level inference endpoints declared by the operator, a `tei` provider, and two companion interfaces (`LLMReranker`, `LLMPinnedEmbedder`); `LLMGateway` itself is unchanged. |
| **Uses** | [ADR-0001](0001-unified-tenant-model.md) (tenant scope), [ADR-0002](0002-metrics-label-schema.md) (metric labels), [ADR-0005](0005-observability-logging-tracing-metrics.md) (opt-in overlay precedent), [ADR-0012](0012-module-config-group-contract.md) (settings as config groups) |

## Context

Forks built on Orkestra now want to search their own data by meaning: a CRM wants to find contacts whose professional summary and past positions match a request in natural language, combined with ordinary field filters; an assistant wants to ground its answers in the tenant's records and, later, in its documents. Every such feature needs the same pieces, and none of them exists in the base:

- **Vectors have nowhere to live.** `iface.LLMGateway.Embed` (ADR-0022) computes embeddings, but nothing stores or searches them, and `EmbedRequest` cannot pin a model, so two calls may land in different vector spaces. MongoDB Community 8.0 has no vector search without an additional search process, and the base's only text search is a literal regular expression.
- **Keyword and meaning must be combined.** Pure vector search misses exact names, codes and acronyms; pure keyword search misses synonyms and paraphrases ("head of sales" versus "responsabile commerciale"). Production retrieval fuses both and may reorder the top candidates with a cross-encoder reranker.
- **The text is personal data.** Profiles, notes and documents describe people. Embedding vectors can be partially inverted back into text, so they carry the same obligations as their source, including an erasure that does not come back. Sending that text to a hosted embedding API is a transfer the operator must justify; computing it locally avoids the question.
- **Visibility is finer than the tenant.** `authz` is RBAC per tenant today, but documents and records will be restricted by role, by team and to single users. A retrieval layer that only filters by tenant would return passages the caller cannot open, and fixing that later means re-indexing everything.
- **CPU-only hosts.** Reference installations run on virtual machines without a GPU. Inference has to be sized and measured for CPU at the volumes a single organization produces, while staying movable to a GPU host later.

Retrieval patterns built on top (corrective RAG, query rewriting, agentic multi-step search) change quickly; the storage, permission and inference contract underneath them should not. This ADR fixes the contract and leaves the patterns to consumers. It deliberately keeps the first version small where the guarantees are hardest (engine sharing, index backups, zero-downtime migrations) and strict where they matter most (visibility, ordering, erasure).

## Decision

Add a core module `internal/core/retrieval/` that owns corpora, indexing, hybrid search, reranking and the permission filter, on top of a vector engine and local inference servers. Consumers declare a *corpus* and send it text and filterable fields; they never see the engine. Acceptance test: *a fork module declares a corpus, indexes records through the outbox, and a user's hybrid query returns only records that user may open, in the same tenant, with provenance; an erased subject is never served again from the moment its erasure is recorded, and never reappears through re-indexing, a stale worker, reconciliation or rebuild; no consumer imports the engine's client.*

### D1 — Core, always-on (amends ADR-0006), with a defined degraded mode

Registered in `coreModules()` after `llm` and before `compliance`; `Category()` is `CategoryCore`, so an Init failure is fatal. Load order becomes `user → notification → tenant → authz → auth → navigation → logging → llm → retrieval → compliance`; a test pins it.

The module is always loaded, but its infrastructure is optional (D9). Init never contacts the engine or the inference servers, so a missing or unreachable sidecar can never fail the boot. What works without them:

| Operation | Without the overlay configured | Overlay configured but unreachable |
|---|---|---|
| Corpus registration (Init) | works (MongoDB only) | works |
| `Outbox.Enqueue` in the owner's transaction | works; intents are kept | works; intents are kept and drained later |
| Search | `ErrRetrievalNotConfigured` | `ErrRetrievalUnavailable`, or keyword-only with `degraded: true` when only inference is down (D5) |
| Data-subject export, erasure, tenant purge | works on MongoDB state; erasure writes tombstones (D12) | tombstones written at once and effective on reads; engine deletes retried until confirmed |
| Administrative operations | report "not configured" | report the failing component |

Owners therefore never have to choose between failing their own writes and losing index intents.

### D2 — MongoDB stays the source of truth; the index is derived and rebuildable

The engine holds a projection of data whose authoritative copy lives in the owning module's collections. Losing the index loses no data: a corpus can always be rebuilt from its source (D11). The index has no backup of its own; recovery is a rebuild. No module reads business data back out of the index except as search results.

### D3 — Qdrant is the vector engine, one per stack, behind an engine-neutral driver

[Qdrant](https://github.com/qdrant/qdrant) (Apache-2.0, an OSI license), version 1.20 or later, single node, reachable only on the stack's internal network, with an API key. The capabilities this ADR relies on:

- payload filters applied during graph traversal rather than before or after it;
- named dense, sparse and multi-vectors per point;
- server-side fusion, including weighted RRF;
- BM25 ranking on the full-text index, with **scoring statistics (inverse document frequency and average document length) scoped by a payload filter**, so a tenant's keyword scores are computed from that tenant's documents only;
- delete by filter.

**One engine per stack.** Stacks are isolated by network and resources today; the engine follows that model, and development slots get their own engine too. Sharing an engine between stacks is outside this decision.

**Driver.** The module talks to Qdrant through an internal `engine` interface, never from consumer code. A driver conformance suite runs against the real engine in CI. It covers tenant isolation, tenant-scoped keyword statistics, principal filtering, fusion, delete-by-filter across generations, and the ordering and erasure protocol of D11 and D12, including a worker paused *after* its lease check and resumed after its successor has written. Replacing the engine is then a new driver plus a re-index, not a change of contract; licenses of infrastructure software have changed under their users before, and the base must be able to move.

**Physical layout.** One collection per corpus and generation, named `<stackPrefix>_<corpus>_g<generation>`. Queries never resolve a collection by alias; they read the active generation from the corpus state (D11). Point identifiers are derived deterministically from `(tenantId, corpus, sourceId, chunkOrdinal)`, so a replayed write lands on the same point. Payload fields are split into two namespaces: reserved fields under `_rt` (`tenantId`, `visibleTo`, `subjects`, `sourceId`, `sourceRevision`, `chunkId`, `contentHash`, `ingestionProfile`) that only the module writes and that no consumer filter can address, and the corpus's declared filter fields (D7) under `f`.

### D4 — Local inference on installation-level endpoints

Embeddings and reranking run on **Hugging Face Text Embeddings Inference** (TEI, Apache-2.0, CPU image). A TEI process loads one model, at one revision, chosen at start-up, so every model in use runs as its own instance (D9).

**Installation-level endpoints.** ADR-0022 models are organization rows whose endpoints an organization administrator enters in `/admin/llm`; in production-like environments the gateway accepts only public HTTPS hosts and refuses private addresses at dial time, to stop a tenant administrator from pointing the backend at the internal network. That guard stays as it is. Retrieval inference instead uses **installation-level endpoints** declared by the operator in the environment (D9), never in the database or through the API. Because only the operator can set them, they may live on the stack's internal network; nothing a tenant administrator enters can reach that path. The `llm` module loads them at boot as platform models of a new provider `tei`. They do not appear in, and cannot be edited from, the per-organization `/admin/llm` catalog, and they are served only to the `retrieval` module. Per-user grants do not apply to them: access to retrieved data is governed by the corpus permission and the principal filter (D10). Every call still writes a ledger row and counts against the budget of the organization on whose behalf the module indexes or searches.

**Each endpoint declares its processing boundary.** Alongside its URL and kind (`embed`, `rerank`, `chat`), every installation-level endpoint declares `boundary: local | hosted`. `local` asserts that the operator runs the service inside the installation's own infrastructure; the module never infers locality from the provider type or from where the setting was configured.

**Inference profiles.** An endpoint, the model it serves, the model revision, and the pooling, normalization, truncation and query/document prefixes form an immutable *inference profile*, identified by a hash. The module verifies a profile against what the endpoint reports before using it. There is no fallback between profiles, because two embedding profiles are two vector spaces.

**Companion interfaces, not a wider gateway.** The SDK rule forbids adding a required method to an existing `iface` interface. The `llm` module's gateway service additionally implements two new interfaces, which consumers obtain by type assertion as with `LLMAvailabilityProbe`:

```go
type LLMPinnedEmbedder interface {
    EmbedWithProfile(ctx context.Context, req PinnedEmbedRequest) (*EmbedResult, error)
    // PinnedEmbedRequest{Caller, OnBehalfOf TenantID, Profile ProfileID, Kind query|document, Inputs []string}
}
type LLMReranker interface {
    Rerank(ctx context.Context, req RerankRequest) (*RerankResult, error)
    // RerankRequest{Caller, OnBehalfOf TenantID, Profile ProfileID, Query string, Documents []string, TopN int}
    // RerankResult{Results []RerankScore{Index int, Score float32}, Profile ProfileID, Usage LLMUsage}
}
```

Both accept only installation-level profiles and only the caller `retrieval`, and apply the existing limits on input count and size.

**One embedding profile per corpus generation.** All tenants of a corpus share its collection (D3). The engine could hold several vector spaces side by side as named vectors, but this decision keeps one embedding profile per corpus generation for the whole installation: one model to benchmark, size and operate, and one place where the choice is made. Changing the model is a new generation (D11) built against a second embedding endpoint that runs next to the first until the swap.

**Locality is a corpus property, enforced on profiles.** A corpus declares `locality: local_only | hosted_allowed`, default `local_only`. A `local_only` corpus can use only profiles whose endpoint boundary is `local`, for embeddings, reranking and contextual enrichment (D8); the module refuses any other at registration, at generation build and at query time. The label travels on every search result (D5), so a consumer that forwards results to a chat model knows which results must not leave the installation.

**Reference configuration** (defaults the tooling ships, not part of the contract): `BAAI/bge-m3` for embeddings (MIT, dense, 1024 dimensions, multilingual) and `BAAI/bge-reranker-v2-m3` for reranking (Apache-2.0, multilingual), each pinned by repository revision and recorded with its license in the deployment's model inventory. Weights are fetched into a volume at first start or pre-seeded for installations without internet access. These defaults stand only if the benchmark of D14 confirms them.

### D5 — One hybrid query pipeline

Every search runs the same pipeline:

1. **Gate**: tenant must be internal (D13); the caller must hold the corpus permission (D7); the filter is validated against the corpus schema (D7); the corpus's owning module must be started.
2. **Resolve**: read the corpus's active generation and pin the pair `{collection, embedding profile}` for the whole request, so a generation swap during the query cannot mix vector spaces (D11).
3. **Principal filter**: `_rt.tenantId = <caller tenant>` AND `_rt.visibleTo` intersects the caller's principals (D10), injected by the module and impossible to omit or override.
4. **Candidate generation**: a dense arm (query embedded with the pinned profile) and a keyword arm (BM25 with the corpus analyzer, statistics scoped to the caller's tenant), both under the same filter.
5. **Fusion**: weighted reciprocal rank fusion of the two arms; weights are corpus defaults, overridable per request within bounds. `mode: dense | keyword` disables one arm.
6. **Erasure veto and source check**, before any text is processed further or returned:
   - candidates covered by an erasure tombstone (D12) are dropped;
   - the owning module confirms, per candidate source id, that the caller may see it and reports its current revision (D10); refused ids, and chunks whose `sourceRevision` differs from the current one, are dropped.

   A failing or timed-out check fails the search closed; it never passes candidates through.
7. **Rerank** (optional): the top *K* surviving candidates are rescored by the corpus's reranking profile. Off by default; a corpus enables it by configuration once its benchmark (D14) shows a gain.
8. **Result**: per hit, `sourceId`, `chunkId`, rank, a final relevance score, a snippet, provenance (D8) and the corpus `locality` label. Per-stage scores are not returned to consumers, because keyword statistics computed over records a caller cannot see are an information channel; they are available only through the diagnostics of D15.

If inference is unavailable, the search runs the keyword arm only and says so (`degraded: true`). If the engine is unavailable, it fails with `ErrRetrievalUnavailable`. It never returns fewer than the permitted results without a flag.

### D6 — Orchestration stays with consumers

The core provides retrieval primitives. Corrective RAG (grading retrieved passages and re-querying when they are insufficient), query rewriting, multi-query, hypothetical-document expansion, tool loops and answer generation belong to the consumer (an assistant addon in a fork). On the search path the core never calls a chat model. The only chat-model call the core makes is optional contextual enrichment at index time (D8), under the corpus locality. Any chat-model data flow at query time lives in a consumer, which must honor the `locality` label.

### D7 — A corpus declares its contract

A consumer registers a corpus at Init with `iface.RetrievalCorpusRegistry`. Registration is a local declaration and never contacts the engine (D1).

- **Identity**: name (unique per installation), owning module.
- **Permission**: the permission a caller must hold to search it (for example `crm.contact.read`).
- **Filter schema**: the fields that may be filtered on, each with a type (`keyword`, `keyword[]`, `integer`, `float`, `bool`, `datetime`) and a description. Filters are a bounded tree of `and`, `or`, `not` and typed comparisons over declared fields only, with limits on depth and size. The module validates every filter, creates the payload indexes, and exposes the schema in process so a language model or a tool can turn a natural-language request into a typed filter, the way a CRM segment catalog does.
- **Ingestion profile**: the keyword analyzer (tokenizer, stemmer, stop words), the chunking strategy and parameters (D8), the contextual-enrichment settings including its profile and prompt template version (D8), and the embedding profile (D4). Its hash identifies everything that turns source text into indexed text and vectors. One per corpus generation, because all tenants share the corpus's collections; changing any part of it starts a new generation (D11).
- **Locality** (D4) and the optional reranking profile.
- **Source**: an `iface.RetrievalSource` implemented by the owner. Every call is scoped to one tenant passed explicitly; none of them may answer for another tenant:
  - `Load(ctx, tenant, ids)` returns, for each live id, text, filter fields, `visibleTo`, `subjects`, a content hash and a **monotonic source revision**; absent ids are reported as deleted.
  - `Enumerate(ctx, tenant, cursor)` lists every live id with its revision and hash, for rebuild and reconciliation.
  - `Visible(ctx, tenant, principalCtx, ids)` answers, per id, whether the caller may see it and the item's current revision (D5 step 6). Its errors are treated as "not visible".

If the owning module is disabled, its corpora stop answering searches (`ErrRetrievalCorpusUnavailable`), their outbox intents are kept, and data-subject operations still apply to their points.

### D8 — Ingestion, chunking and provenance

An item is split into chunks by the corpus's strategy: `none` (short records, one chunk per item), `fixed` (token windows with overlap) or `hierarchical` (small chunks are searched, their parent section is returned). Optional *contextual enrichment* prepends a short, model-generated description of the item to each chunk before embedding. It runs through an installation-level `chat` endpoint with its own boundary; a `local_only` corpus can enable it only on a `local` chat endpoint, and without one enrichment is unavailable to that corpus. The content hash is computed over the final indexed text, enrichment included, and the enrichment settings are part of the ingestion profile (D7), so a changed model or prompt never mixes two transformations in one generation. Every chunk carries provenance (`sourceId`, `chunkId`, ordinal, and, for documents, page and character offsets) so consumers can cite.

Parsing binary documents (PDF, office formats, scans) into text is a seam, `iface.DocumentParser`. The base ships no parser; choosing the reference parser sidecar is a separate decision taken with the first document corpus. Named vectors (D3) leave room for multi-vector and visual-document embeddings without a schema change.

### D9 — Optional deployment overlay

An opt-in overlay, `docker/docker-compose.retrieval.yml`, layered into the stack's project like `docker-compose.observability.yml` (ADR-0005), runs `qdrant` and one TEI instance per configured model: in the reference configuration `tei-embed` and `tei-rerank`, plus a second embedding instance only while a model change is being rebuilt (D11). They sit on the stack's internal network only, with no host ports in staging or production.

New settings are generated by the init script, checked by the environment checker (keys refused as placeholders in staging and production) and passed by the compose files:

- `QDRANT_URL`, `QDRANT_API_KEY`;
- a list of named installation-level endpoints, each with URL, kind (`embed`, `rerank`, `chat`), boundary (`local` or `hosted`), and the model identifier and revision it serves, plus a shared `RETRIEVAL_INFERENCE_API_KEY` (or one per endpoint). Corpus profiles refer to endpoints by name.

Images are pinned by digest and model weights by repository revision; all of them enter the operator's supply-chain process like every other artifact. The endpoints are configuration, not topology: a GPU host or an inference server shared between stacks replaces the local containers by changing the URLs, with TLS and an honest boundary declaration.

### D10 — Visibility inside the tenant

Tenant isolation is necessary but not sufficient. Visibility is enforced twice.

**Early, in the index.** Every point carries `_rt.visibleTo`, a list of opaque principal strings decided by the owning module: `tenant:*`, `role:<roleId>`, `team:<teamId>`, `user:<userUuid>`. At query time `iface.RetrievalPrincipalResolver` computes the caller's principals as the union of:

- `user:<callerUuid>`;
- `tenant:*`, only for an active member of the tenant;
- `role:<roleId>` for each role of the caller in the tenant, from non-expired bindings to active roles, through a new narrow `authz` interface that lists them (`AuthzProvider` only answers permission checks today);
- whatever registered `iface.RetrievalPrincipalContributor`s add. Contributors must be deterministic and bounded; the resolver caps the total number of principals and fails closed when a contributor errors.

The query filter requires a non-empty intersection. Role or membership changes need no re-indexing; a future team concept, in the core or in a fork, plugs in as a contributor without touching the index.

**At the source, before any further processing.** `RetrievalSource.Visible` confirms the candidates before reranking and before results leave the module (D5 step 6). This closes the window in which a record was just made private but the index has not caught up: a revocation takes effect at once, and no text of a record the caller can no longer see is processed further or returned.

**No bypass for platform roles.** `super_admin`, `administrator` and `developer` pass ordinary permission checks through global grants, but they get no exception here: they need the corpus permission like anyone else and see only what their principals reach. A record shared with nobody is found by nobody. An audited break-glass search is outside this decision.

A change of sharing on a source item re-queues the item; the worker updates only the payload when the content hash is unchanged. Chunks inherit the visibility of their item.

### D11 — Index freshness, ordering and generations

The engine cannot make an insert conditional (a conditional upsert on a missing point inserts it), so a stale writer cannot be stopped at the engine alone. The protocol therefore makes stale writes harmless on reads and removes them afterwards.

- **Transactional outbox.** Owners enqueue intents `{tenant, corpus, sourceId, sourceRevision}` with `iface.RetrievalOutbox.Enqueue`, inside the same MongoDB transaction as the source write, so an index change is never lost or applied to a write that rolled back. Intents name an item, not its content.
- **One writer per key, applying current state.** Intents are claimed per key `(tenant, corpus, sourceId)` with a lease and an epoch in MongoDB, so one key has one current holder. The holder checks tombstones (D12), calls `Load`, and writes what the source holds *now*, with a client deadline shorter than its remaining lease. Updates to existing points are conditional on revision: a point is never replaced by data with a lower `sourceRevision`.
- **Chunk sets are replaced, not merged.** An upsert writes the item's new chunks with the new revision, then deletes the item's chunks carrying any other revision; a deleted item loses all of them.
- **After writing, the holder re-checks its epoch.** If the lease was lost meanwhile, the key is re-queued, so the current holder re-applies the current state and deletes whatever the stale write left.
- **Stale writes are never served.** A worker paused after its lease check can still write when it resumes. Whatever it writes carries a revision that no longer matches the source, or an item or subject covered by a tombstone, so the read path drops it (D5 step 6). Reconciliation removes such points, and erasure finalization repeats its deletes after the fencing window (D12).
- **Content hash.** The worker skips re-embedding when the content hash and the ingestion profile are unchanged, and updates only the payload.
- **Fairness and backpressure.** The outbox is drained with per-tenant fairness and bounded batches; interactive search has priority over indexing and rebuild for inference capacity.
- **Reconciliation.** A periodic sweep compares `Enumerate` with the index per corpus and tenant and repairs drift: missing points, stale revisions, orphaned points. It never writes a point covered by a tombstone.
- **Corpus state is global; progress is per tenant.** `retrieval_corpus_state` holds, per installation and corpus, the active generation and its ingestion profile, and any generation being built. A separate per-tenant record holds rebuild and reconciliation progress. Only the installation-wide record decides which collection queries use.
- **Generations.** Changing the ingestion profile creates a new generation:
  1. The active generation keeps answering queries, read-only, with the erasure veto and the source check of D5 step 6, so it never serves erased or newly private records.
  2. The new collection is built from `Enumerate`. Outbox intents keep accumulating meanwhile and are not applied to the old generation.
  3. When the build ends, the accumulated intents are drained into the new generation. Then a single update of the corpus state makes it active.
  4. Queries already running finish on the pair they pinned (D5 step 2). The old generation receives every erasure until it is dropped after a short grace period.
- **Recovery is a rebuild.** There are no index snapshots in this decision. After a lost or corrupted index, or after a MongoDB restore, corpora are rebuilt from their sources, subject to the tombstones present in MongoDB. The index therefore never holds anything that MongoDB does not. Re-applying erasures that a MongoDB restore rolled back is a platform-wide concern of the compliance plane, outside this decision.

### D12 — Personal data, erasure that does not come back, and audit

- **Vectors are personal data** with the same classification as their source text.
- **Subjects.** Points carry `_rt.subjects`, opaque references to the people they describe, declared by the owner: `user:<userUuid>` for platform users, `<owner>:<id>` for anyone else (a CRM contact, for example).
- **Tombstones.** Every erasure first writes a tombstone to `retrieval_erasures` in MongoDB. There are two scopes:
  - **global** tombstones for platform users (`user:<userUuid>`). They cover every tenant and every corpus, present and future, because a data-subject request names a user, not an organization, and a source may describe a user who is not a member of its tenant;
  - **tenant** tombstones for items (`tenant, corpus, sourceId`), for owner-declared subjects (`tenant, <owner>:<id>`) and for a whole tenant.
- **Effective at once, on reads and writes.** From the moment a tombstone is written:
  - the read path drops every candidate it covers (D5 step 6), even while the engine still holds the points;
  - the worker, reconciliation and rebuilds refuse to write any point it covers.
- **Finalization.** The engine deletes run on every generation, retried until confirmed. After the fencing window (the maximum lease plus the maximum write duration), they run once more to remove anything a stale writer may have written meanwhile. An erasure is complete only when both passes are confirmed. Tombstones are kept for as long as their source could still be re-indexed.
- **Platform users.** The module registers an `iface.PIIProducer`. A data-subject erasure run by compliance writes the global `user:<userUuid>` tombstone, deletes the matching search-log rows, and reports its purge as pending until finalization is confirmed.
- **Erasure requests complete only when every producer is done (amends ADR-0009).** Today the erasure workflow records producer errors but still marks the request completed. With this decision a request stays `incomplete`, with durable retries of the failed or pending producers, until every producer reports its purge done. `PurgeResult` gains a pending indicator, an additive field. This applies to every producer, not only to retrieval.
- **Other people.** Compliance's data-subject pipeline identifies subjects by user only, so there is no automatic safety net for them. The module offers `DeleteBySubject(ctx, tenant, ref)`, which follows the same tombstone and finalization protocol and also deletes the search-log rows naming the subject. The owning module calls it from its own erasure path. Extending compliance to subjects who are not users is outside this decision.
- **Retention.** Indexed content has no retention of its own: it follows the source item.
- **Tenant offboarding.** ADR-0009's per-tenant crypto-shred does not reach the engine. Tenant purge writes a tenant tombstone, deletes the tenant's points in every generation by filter, deletes the tenant's search log, and is verified by a count per generation.
- **Search audit.** Every search writes a row to `retrieval_search_log`: tenant, user, corpus, query text, validated filter, principals applied, returned source ids, the subjects of the returned points, and timings. Searching for people is profiling, and the operator must be able to answer who looked up whom. Rows are tenant-scoped and readable only through `retrieval.searchlog.admin` (D15); reading them is itself audited. The module's PII producer exports and erases rows where a user is either the searcher or a subject of the results. A person named only in the free text of a query that returned nothing cannot be linked to the row; that residual is bounded by the log's retention (default 90 days). Administrative actions (rebuild, generation swap, corpus drop) go to the compliance audit sink.

### D13 — Tier 1 first, tenant-agnostic data model

In this decision every search and indexing entry point refuses tenants that are not internal, as the `llm` gateway does (ADR-0022 D2). Data-subject operations are the exception: they act on a subject across organizations through explicitly annotated paths (`//tenantscope:allow dsr:`), as the other core modules do, and global tombstones carry no tenant. Nothing in the index layout or the contract assumes the internal kind, so serving external client tenants later is an entry-point change, not a migration.

### D14 — Measured before it is defaulted

- **Benchmark first.** Before the reference configuration becomes a default, a benchmark on a representative corpus with selective visibility compares keyword, dense, hybrid and hybrid-plus-rerank. It measures quality (recall@k, nDCG@k), p95 latency, memory and indexing throughput on the reference CPU host. Smaller embedding models are part of the comparison. The results decide the shipped defaults, including whether reranking is worth enabling.
- **Golden sets.** A corpus may ship a golden set (queries with the source ids they must return). A `make` target runs it against a fixture index; a corpus's ingestion profile, fusion weights, *K* and reranking switch change only with a run that does not regress it.
- **Metrics** following ADR-0002: query count and latency per corpus, mode and outcome; degraded and empty searches; drops by erasure veto, visibility and stale revision; outbox age and depth; inference queue time; reconciliation repairs; rebuild progress; erasures pending finalization.

### D15 — Administrative surface

Operating the index is an administrative surface on the operator host, following ADR-0022 D5. Permission names are chosen against the seeding rules of `authz`, where suffixes decide which seeded roles inherit a key:

| Permission | Scope | Grants | Seeded holders |
|---|---|---|---|
| `retrieval.corpus.read` | system | corpus states, generations, health and counters; never content or scores | platform roles that inherit `.read` keys |
| `retrieval.corpus.admin` | system | rebuild, swap, drop, per-stage score diagnostics; second factor and step-up required | `super_admin`, `administrator` |
| `retrieval.tenant.read` | tenant | the organization's indexing state and backlog, counts only | tenant roles that inherit `.read` keys |
| `retrieval.tenant.admin` | tenant | reconciling the organization's slice of a corpus | organization owners and administrators |
| `retrieval.searchlog.admin` | tenant | reading the organization's search log; second factor and step-up required | organization owners and administrators, and custom roles granted it explicitly; never `manager`, `operator`, `guest`, member or viewer roles |

The seeded `administrator` and `super_admin` platform roles hold every registered key by design, and so does `developer` outside production (in production it keeps only `.read`, `.view` and `.self` keys). They can therefore read a search log; that access is audited like any other. A test pins this matrix against the seeding code, so a change of seeding rules cannot silently widen it.

The console gains one page under `/admin/retrieval` (corpora, generations, health, backlog) and a search-log view. Searching itself has no generic HTTP route in this decision: consumers expose search through their own routes and tools, with their own permissions and corpus.

### D16 — The legacy RAG contract is deprecated

`iface.RAGQueryProvider`, `SourceRef`, `RAGQueryResponse` and `module.ServiceRAGQuery` predate the core-only base and are implemented by nothing in it; only archived addons use them. They are marked deprecated in favour of `iface.RetrievalSearch` and removed in a later release, so forks that still build an archived `rag` addon have time to migrate.

## Consequences

- **Load order** gains `retrieval` between `llm` and `compliance`; the module depends on `tenant`, `authz` and `llm`. Compliance pushes its audit sink into the module the way it does for `llm`.
- **SDK surface.**
  - New: `iface.RetrievalCorpusRegistry`, `iface.RetrievalSource`, `iface.RetrievalOutbox`, `iface.RetrievalSearch` (including `DeleteBySubject`), `iface.RetrievalPrincipalResolver`, `iface.RetrievalPrincipalContributor`, `iface.LLMPinnedEmbedder`, `iface.LLMReranker`, a narrow `authz` interface listing a user's active tenant role ids, `iface.DocumentParser` (seam only), `module.ServiceRetrieval`, and `ErrRetrieval*` sentinels.
  - Additive: `PurgeResult` gains a pending indicator.
  - Unchanged: `iface.LLMGateway`; no existing interface gains a method.
  - Deprecated: the legacy RAG types (D16).
- **`llm` gains** installation-level endpoints loaded from the environment, inference profiles, and the `tei` provider. The private-address guard is unchanged: organization endpoints entered through the API keep ADR-0022's rule, and only operator-declared environment endpoints reach the internal network.
- **Compliance changes behavior for every producer**: an erasure request with a failed or pending producer stays `incomplete` and is retried, instead of being marked completed.
- **Collections**: `retrieval_outbox`, `retrieval_corpus_state` (installation-wide), `retrieval_tenant_progress`, `retrieval_erasures` (global and tenant tombstones), `retrieval_search_log`. All but the installation-wide corpus state and the global tombstones are tenant-scoped. The outbox drain, reconciliation, rebuild and erasure-finalization sweeps find work across tenants (`//tenantscope:allow system:`) and process each row under its own tenant.
- **Infrastructure.** One Qdrant and one TEI instance per model, per stack, in an opt-in overlay; development slots get their own. On a CPU host the reference models need a few GB of RAM and several GB of disk for images and weights; operators size for that before enabling the overlay. The index needs no backup: recovery is a rebuild, whose duration on CPU the benchmark of D14 measures.
- **Consumers own their sharing rules.** A fork module that indexes data:
  - decides `visibleTo` and `subjects`;
  - implements `Visible` with a fail-closed answer that reports the current revision;
  - keeps a monotonic revision per item;
  - enqueues changes in its transactions;
  - calls `DeleteBySubject` from its erasure path.

  A record restricted by a rule the module cannot express as principals must not be indexed until it can.
- **Platform administrators gain no shortcut in search.** Results for them follow the same principals as for everyone else (D10).
- **Delivery in three steps**, each usable on its own:
  1. The module, driver and conformance suite, installation-level endpoints, profiles and companion interfaces, and the overlay. Corpora, and the outbox with per-key leases, epoch re-checks and revision-conditional writes. The hybrid pipeline with principal filtering, erasure veto and source checks. Tombstones with finalization, the compliance change for erasure requests, data-subject rights, the search log, the administrative surface of D15, and the benchmark of D14.
  2. Reconciliation, generations with rebuild and swap, metrics and the golden-set target.
  3. The document seam, hierarchical chunking and contextual enrichment, with the first document corpus.

## Alternatives considered

- **Vectors in MongoDB, similarity computed in the backend.** Adequate for a few thousand short records after strong pre-filtering, and needs no new service. Rejected as the contract: no keyword ranking, no approximate index, linear cost per query, and it would be replaced as soon as documents arrive.
- **MongoDB Community with its search process (`$vectorSearch`).** Keeps one database, but the search process is a separate component under SSPL, not an OSI license, and community support was recent at the time of this decision.
- **Redis Query Engine.** Already deployed with Redis 8, and offered under AGPLv3 among Redis's license options, with vector and hybrid search. Rejected for operational separation: the base runs Redis as a cache whose loss is harmless, and a second, durable Redis would run the same product under two operating profiles. Qdrant's filter-aware graph search and tenant-scoped keyword statistics map directly onto D3 and D5.
- **OpenSearch.** Mature hybrid search with neural and rerank pipelines, Apache-2.0. Rejected for this role: a JVM service with a large memory floor for the volumes involved, and operators may already run it for logs with different retention and access rules.
- **Weaviate.** BSD-3, strong hybrid search and native multi-tenancy. It accepts vectors computed outside it, so it can sit behind the `llm` gateway like Qdrant. It is the closest alternative and the first candidate for a second driver. Qdrant is preferred for its lighter single-binary footprint at this scale, for filtering during graph traversal, and for tenant-scoped scoring statistics. The benchmark of D14 can still reopen the choice, at the cost of a driver and a re-index.
- **Milvus, Vespa.** Built for billion-scale corpora or complex ranking programs; the operational cost (coordination services, cluster topology, ranking language) is not justified here.
- **PostgreSQL with pgvector.** Rejected: it introduces a second primary database next to MongoDB.
- **Ollama for inference.** Serves embeddings but not cross-encoder reranking; TEI serves both. An Ollama embedding endpoint remains possible as an installation-level endpoint with its own profile.
- **Hosted embedding and rerank APIs.** Rejected as the default by D4: they move personal data out of the installation for every indexed record. A `hosted_allowed` corpus may still use a `hosted` endpoint.
- **Retrieval models per organization, through the existing `/admin/llm` catalog.** Technically possible with one named vector per model in a shared collection. Rejected under D4 for two reasons:
  - it multiplies the profiles to benchmark, size and migrate;
  - reaching a self-hosted server on the internal network would mean relaxing the private-address guard for endpoints a tenant administrator can type.
- **Adding `Rerank` and a model pin to `LLMGateway`.** Rejected: the SDK forbids adding required methods to an existing `iface` interface, because it breaks every external implementation and test double; companion interfaces carry the new capability instead.
- **Resolving the active generation through an engine alias.** Rejected under D11: an alias swap is atomic for the engine but not for a query that has already embedded its text with the previous profile.
- **Relying on conditional writes in the engine for fencing.** Rejected under D11: a conditional upsert on a missing point inserts it, so it cannot stop a stale writer from recreating deleted chunks; read-time vetoes plus finalization do.
- **Index snapshots for faster recovery.** Deferred: a snapshot is a copy that erasures must also reach, and restoring one safely needs an erasure ledger that survives a rollback. Rebuilding from MongoDB keeps the index no more durable than its source.
- **Platform roles bypass the principal filter.** Rejected under D10: it would expose records shared with nobody to every platform administrator, and auditing the access afterwards does not undo it.
- **Filtering by tenant only, permissions checked after the search.** Rejected under D10: post-filtering a fixed top *K* returns empty or short pages for restricted users and leaks the existence of results through counts and timings.

## Left to the implementer

- How the keyword arm is built in the engine (BM25 ranking on the full-text index, or a server-side BM25 sparse vector), provided tenant-scoped statistics, D5's fusion and the filters hold; settle it with the driver conformance suite.
- Default *K*, fusion weights, chunk sizes and overlap, generation grace period, principal cap, lease length and fencing window; tune them with the benchmark and golden sets of D14.
- Batch sizes, backoff and fairness parameters of the outbox worker, following the module patterns already in the base.

## Out of scope

Graph-based retrieval (GraphRAG), a semantic cache of answers, corrective-RAG and agentic loops inside the core, web search as a fallback, a team model in the core, an audited break-glass search for platform roles, data-subject rights for people who are not platform users, re-applying erasures after a MongoDB restore, index snapshots, sharing an engine between stacks, zero-downtime migrations with dual writes, serving external client tenants, multi-node engine clusters, GPU deployment recipes, the choice of a document parser, and multimodal ingestion (the schema leaves room for it).
