# Retrieval PR 4 — Indexing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** owners enqueue index intents in their own MongoDB transaction, and a lease-fenced worker turns them into chunked, embedded points in the corpus's generation-1 Qdrant collection, refusing anything a tombstone covers.

**Architecture:** `retrieval_outbox` holds one row per key `(tenantId, corpus, sourceId)`; `Enqueue` upserts it (`$max` revision, `dirty`). A worker claims keys per tenant in round-robin, under a lease with an epoch, loads the item's *current* state from the owner, chunks it (`chunk` package), embeds it through `iface.LLMPinnedEmbedder` under the row's tenant, writes `Upsert` then `DeleteOtherRevisions`, and re-checks its epoch before deleting and when releasing; a lost lease re-queues the key. Corpus bootstrap (generation 1, corpus state) and the initial build (`Enumerate` → outbox, resumable per tenant) run on the worker's passes.

**Tech Stack:** Go 1.26.10, mongo-driver v1.17.7 (replica set, transactions), PR 3's `engine.Engine` and Qdrant REST driver, PR 1's installation inference interfaces.

**Spec:** `docs/adr/0023-core-retrieval-module.md` (D1, D3, D4, D7, D8, D11 minus reconciliation and generation swap, D12 tombstone store and write refusal, D13) and `docs/superpowers/plans/2026-10-26-retrieval-00-roadmap.md` (row 4 and the Shared contract).

## Global Constraints

Everything in the roadmap's **Global constraints** applies. In addition, for this PR:

- PRs 1 and 3 are prerequisites. Build only on the roadmap's contract for them: `iface` types in `llm_installation.go` and `retrieval.go`, `engine.Engine` and its types, `engine.PointID`, the module layout. Where PR 3's code names something the roadmap does not (registry lookup, the module's engine and prefix fields, the Qdrant constructor, the module test harness, the corpus-state collection constant), use PR 3's name, adapt in `module.go`, and never rename PR 3 identifiers.
- Every commit ends with the trailer block below; commit steps show the subject only.
  ```
  Prop: upstream
  Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
  ```
- No HTTP route, permission, errcode or console change in this PR.
- The worker handles each row under `tenantCtx(ctx, row.TenantID)` (tenant ID + kind `internal`); every query not scoped through `tenantrepo` carries `//tenantscope:allow system: <reason>` (or `dsr:` for erasure inserts).
- Logs and stored error fields never carry item text, field values, principals or subjects: only corpus, tenant ID, source ID, counts and the closed error codes of Task 10.
- Fixed values (exported where later PRs need them):

  | Name | Value |
  |---|---|
  | `worker_lease` | default 2m, clamp [30s, 15m] (`services.MinWorkerLease`, `services.MaxWorkerLease`) |
  | `worker_batch` | default 32, clamp [1, 256]; ≤ 0 → default |
  | `worker_poll_interval` | default 5s, clamp [1s, 5m] |
  | write deadline | `claim.LeaseUntil − lease/4` |
  | `services.IndexingFencingWindow` | `2 × MaxWorkerLease` = 30m (consumed by PR 6) |
  | embed batch | 32 inputs per `EmbedWithProfile` call |
  | build page | 200 ids per `Enumerate` call; ≤ 16 tenant pages per pass |
  | tenants per pass | 64 (`DueTenants` limit) |
  | intents per `Enqueue` | ≤ 1000; `SourceID` 1–256 bytes, valid UTF-8, no NUL; `Revision ≥ 0` |
  | backoff | `5s · 2^(attempts−1)`, cap 30m; `invalid_item` always 30m; never abandoned |
  | item bounds | title cut to 512 bytes; `VisibleTo` ≤ 1000, `Subjects` ≤ 100, each entry 1–256 bytes |
  | chunk bounds | `MaxPieceRunes` 32, `MaxChunkBytes` 64512, `MaxChunksPerItem` 512, `MaxTokens` 1–4096 |

## Review Focus

1. **A stale worker paused after its lease check resumes after its successor wrote** (paused before `Upsert`, or before `DeleteOtherRevisions`): the item ends with exactly the newest revision's points once the re-queued key is processed — pinned by `TestWorker_StaleHolderResumingAfterSuccessor` (Task 10) and `TestIntegration_StaleHolderResumingAfterSuccessor` (Task 13).
2. **An owner enqueues revision 2 while a worker holds the key for revision 1**: the key is still dirty after the release and revision 2 is applied on the next pass, never lost — `TestWorker_EnqueueWhileHeldIsAppliedNextPass` (Task 10).
3. **An item names a subject whose erasure tombstone was written after the intent was queued**: the worker embeds nothing, writes nothing and deletes the item's existing points — `TestWorker_TombstonedSubjectWritesNothingAndDeletesPoints` (Task 10).
4. **Only the sharing of an item changes (new `VisibleTo`, new revision, same text)**: no embedding call, payload updated to the new revision; if the engine lost the points meanwhile, a full re-embed instead — `TestWorker_SharingChangeUpdatesPayloadWithoutEmbedding`, `TestWorker_PayloadPathFallsBackWhenPointsMissing` (Task 10).
5. **The owning module is disabled while intents are pending**: nothing is claimed, attempts stay 0, intents are applied once the module starts again — `TestWorker_KeepsIntentsWhileOwnerStopped` (Task 11).

---

## File structure

```
backend/pkg/sdk/iface/tenant_directory.go        + TenantIDLister                       (Task 1)
backend/pkg/sdk/iface/retrieval.go               + ErrRetrievalInvalidIntent            (Task 1)
backend/pkg/sdk/module/services.go               + ServiceModuleStatus, ModuleStatusReader (Task 1)
backend/cmd/server/main.go                       register the module registry           (Task 1)
backend/internal/core/tenant/services/tenant_ids.go  ListActiveTenantIDs                (Task 1)
backend/internal/core/retrieval/
├── engine/engine.go, engine/qdrant/*, enginetest/*  + UpdatePayload                   (Task 2)
├── chunk/chunk.go                                tokenization + none/fixed windows     (Task 3)
├── models/outbox.go, corpus_state.go, tenant_progress.go, erasure.go  (Tasks 4–6)
├── repository/outbox.go, corpus_state.go, tenant_progress.go, erasures.go (Tasks 4–6)
├── services/tombstones.go                        Probe, Tombstones.Covered             (Task 6)
├── services/outbox.go                            Outbox.Enqueue, Service.Enqueue        (Task 7)
├── services/profilehash.go, services/gate.go     hash, locality, bootstrap, profile_changed (Task 8)
├── services/ingest.go                            item validation, fields, points, hashes (Task 9)
├── services/worker.go                            per-key processing and fencing         (Task 10)
├── services/worker_pass.go                       fair claim, initial build, Run loop    (Task 11)
├── services/fakes_test.go                        in-memory fakes                        (Tasks 8–11)
├── services/worker_integration_test.go           real Mongo + Qdrant                    (Task 13)
├── module.go                                     collections, config, Init, Start/Stop, HealthCheck (Tasks 4–6, 12)
└── AGENTS.md                                                                            (Task 14)
docs/site/modules/core/retrieval.mdx, docs/site/sdk/shared-iface.mdx, docs/site/sdk/service-registry.mdx,
backend/internal/core/tenant/AGENTS.md                                                   (Tasks 1, 14)
```

---

### Task 1: SDK seams — tenant lister, module status, invalid-intent sentinel

The worker must list internal tenants for the initial build and know whether a corpus's owning module is started; no SDK surface does either today (`iface.TenantProvider` has no listing; `ModuleRegistry.IsStarted`, `backend/pkg/sdk/module/registry.go:523`, is not reachable from a module). Both are new narrow interfaces resolved by type assertion; no existing interface gains a method.

**Files:**
- Modify: `backend/pkg/sdk/iface/tenant_directory.go` (append)
- Create: `backend/internal/core/tenant/services/tenant_ids.go`
- Test: `backend/internal/core/tenant/services/tenant_ids_integration_test.go`
- Modify: `backend/pkg/sdk/module/services.go` (beside `ServiceTenantService`, ~L123)
- Modify: `backend/pkg/sdk/module/registry.go` (compile-time assertion near `IsStarted`, L522)
- Test: `backend/pkg/sdk/module/module_status_test.go`
- Modify: `backend/cmd/server/main.go:241-242` (register right after `module.NewModuleRegistry(logger)`, before `InitAll`)
- Modify: `backend/pkg/sdk/iface/retrieval.go` (error var block)
- Docs: `backend/internal/core/tenant/AGENTS.md` (§ Service contract, L293), `docs/site/sdk/shared-iface.mdx` (table L14 and § Error sentinels L55), `docs/site/sdk/service-registry.mdx` (one paragraph on `ServiceModuleStatus`)

**Interfaces:**
- Consumes: `tenant/repository.(*Repository).ListTenantsByKind(ctx, kind models.TenantKind, includeDeleted bool)` (repository.go:172).
- Produces:
  ```go
  // iface — implemented by tenant's *services.Service; resolve with
  // module.GetTyped[iface.TenantIDLister](reg, module.ServiceTenantService).
  type TenantIDLister interface {
      // ListActiveTenantIDs returns the UUIDs of the tenants of kind
      // (TenantKindInternal | TenantKindExternal) that are not soft-deleted and
      // whose status is TenantStatusActive, sorted ascending. Any other kind is refused.
      ListActiveTenantIDs(ctx context.Context, kind string) ([]string, error)
  }
  // module
  const ServiceModuleStatus ServiceKey = "module.status" // value: *ModuleRegistry
  type ModuleStatusReader interface{ IsStarted(name string) bool }
  // iface
  var ErrRetrievalInvalidIntent = errors.New("retrieval: invalid index intent")
  ```

- [ ] **Step 1: Write the failing tests**

```go
// tenant/services/tenant_ids_integration_test.go — uses newSetupTenantTestDB, newSetupTenantService, seedRawTenant
func TestListActiveTenantIDs_ReturnsLiveActiveTenantsOfKind(t *testing.T) {
    // seed: i-b, i-a internal active; i-s internal suspended; i-d internal active with DeletedAt set; e-a external active
    // (suffix every UUID and slug with randSuffix(t))
    got, err := svc.ListActiveTenantIDs(ctx, iface.TenantKindInternal)
    // err == nil; got == []string{"i-a"+sfx, "i-b"+sfx}
    got, _ = svc.ListActiveTenantIDs(ctx, iface.TenantKindExternal) // == []string{"e-a"+sfx}
    _, err = svc.ListActiveTenantIDs(ctx, "bogus")                  // err != nil
}

// module/module_status_test.go
func TestModuleRegistry_IsAModuleStatusReader(t *testing.T) {
    r := NewModuleRegistry(slog.Default())
    reg := NewServiceRegistry()
    reg.Register(ServiceModuleStatus, r)
    ms, ok := GetTyped[ModuleStatusReader](reg, ServiceModuleStatus)
    // ok == true; ms.IsStarted("crm") == false
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd backend && go test ./pkg/sdk/module/ -run TestModuleRegistry_IsAModuleStatusReader && MONGO_TEST_URI=$MONGO_TEST_URI go test ./internal/core/tenant/services/ -run TestListActiveTenantIDs`
Expected: FAIL to compile (`ServiceModuleStatus`, `ListActiveTenantIDs` undefined).

- [ ] **Step 3: Implement**

`ListActiveTenantIDs` on `*services.Service` in `tenant_ids.go` (calls `ListTenantsByKind(ctx, kind, false)`, keeps `Status == active`, `sort.Strings`) plus `var _ iface.TenantIDLister = (*Service)(nil)`. Add the key, the interface and `var _ ModuleStatusReader = (*ModuleRegistry)(nil)`. In `main.go` add `svcRegistry.Register(module.ServiceModuleStatus, modRegistry)` with a one-line comment (consumed by `retrieval` to keep intents of stopped owners, ADR-0023 D7). Add `ErrRetrievalInvalidIntent` to the `retrieval.go` var block. Update the four docs listed above (the tenant contract gains the `TenantIDLister` block; shared-iface gains a `TenantIDLister` row "tenant → retrieval (initial index build)" and the sentinel).

- [ ] **Step 4: Run tests to verify they pass**

Run: same commands as Step 2, then `cd backend && go build ./... && grep -rn "internal/" pkg/sdk/ --include="*.go" | grep import`
Expected: PASS; build OK; grep prints nothing.

- [ ] **Step 5: Commit**

```bash
git add backend/pkg/sdk backend/cmd/server/main.go backend/internal/core/tenant docs/site/sdk
git commit -m "feat(sdk): add tenant ID lister, module status reader and invalid-intent sentinel"
```

---

### Task 2: `Engine.UpdatePayload`

The payload-only path of D10/D11 ("updates only the payload when the content hash is unchanged") needs an engine write that changes `_rt` and `f` without vectors. The roadmap's `Engine` has none (`Upsert` refuses a nil `Dense`). This task adds one method to PR 3's internal `engine.Engine` (internal interface, not `iface`), its Qdrant implementation and a conformance case.

**Files:**
- Modify: `backend/internal/core/retrieval/engine/engine.go`
- Modify: the PR 3 file in `backend/internal/core/retrieval/engine/qdrant/` that implements `Upsert` (add `UpdatePayload` beside it)
- Modify: the PR 3 suite in `backend/internal/core/retrieval/enginetest/` (`enginetest.Run`: add one `t.Run` case `UpdatePayload`, named like its siblings, built with the suite's `point(...)` fixture)

**Interfaces:**
- Consumes: `engine.Point`, `engine.Runtime`, `engine.Selector`, `Engine.Upsert`, `Engine.Count`, `Engine.Query` (roadmap).
- Produces:
  ```go
  // UpdatePayload replaces Runtime and Fields of existing points, keeping
  // their vectors and text. A point is updated only when it exists, its
  // stored _rt.contentHash equals the incoming Runtime.ContentHash and its
  // stored _rt.sourceRevision is not higher than the incoming one; otherwise
  // it is left unchanged. It never creates a point. Dense and Text are ignored.
  UpdatePayload(ctx context.Context, collection string, points []Point) error
  ```

- [ ] **Step 1: Write the failing conformance case** (`UpdatePayload`)

```go
// p := point(tenant "t1", source "s1", ordinal 0, rev 2, hash "h1", visibleTo ["tenant:*"], fields {"stage":"a"}, dense v)
// Upsert(p)
// UpdatePayload(p with rev 3, visibleTo ["user:u1"], fields {"stage":"b"}, Dense nil, Text "")
// Query{TenantID "t1", Principals ["user:u1"], Dense v, Weights{Dense:1}, PerArm 10, Limit 10}
//   → 1 candidate; Runtime.SourceRevision == 3; Fields["stage"] == "b"; Text == p.Text
// keyword Query for a word of p.Text as t1/[user:u1] → still found (the BM25 entry of _rt.text survived the merge)
// p stored with Page 3, UpdatePayload with Page 0 → Runtime.Page == 0 (every _rt field is sent; a merge never clears an omitted key)
// UpdatePayload(rev 1)                → stored revision stays 3
// UpdatePayload(rev 4, hash "h2")     → stored revision stays 3, Fields["stage"] stays "b"
// UpdatePayload(point for "s2")       → Count(Selector{TenantID "t1", SourceIDs ["s2"]}) == 0
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd backend && QDRANT_TEST_URL=$QDRANT_TEST_URL QDRANT_TEST_API_KEY=$QDRANT_TEST_API_KEY go test ./internal/core/retrieval/... -run 'Conformance' -v`
Expected: FAIL to compile (`UpdatePayload` not a method of the driver).

- [ ] **Step 3: Implement `UpdatePayload` in the Qdrant driver**

One `POST /collections/{c}/points/batch?wait=true` request carrying, per point, two `set_payload` operations on the selector filter below: one with `"key": "_rt"` that **merges** every `Runtime` field into the nested `_rt` object (send every field, `page` included even when 0, because a merge never clears an omitted key; `_rt.text` — the indexed text stored by PR 3's `Upsert` — is not sent and therefore kept), and one that sets the top-level key `f` (the declared fields, replaced whole). A top-level `set_payload` of `_rt` would replace the object and drop `_rt.text` with its BM25 entry and snippet (verified on Qdrant 1.20 during PR 3's final review). The selector filter is `has_id: [id]` AND `_rt.contentHash` matches the incoming hash AND `_rt.sourceRevision` `lte` the incoming revision. A filter that matches nothing changes nothing, so a missing point is never created. Network/5xx/timeout → `engine.ErrUnavailable`; missing collection → `engine.ErrNotFound`.

- [ ] **Step 4: Run to verify it passes**

Run: same as Step 2. Expected: PASS (and SKIP without the Qdrant variables — that run proves nothing).

- [ ] **Step 5: Commit**

```bash
git add backend/internal/core/retrieval/engine backend/internal/core/retrieval/enginetest
git commit -m "feat(retrieval): add payload-only updates to the engine contract"
```

---

### Task 3: `chunk` package — tokenization approximation and `none` / `fixed`

**Files:**
- Create: `backend/internal/core/retrieval/chunk/chunk.go`
- Test: `backend/internal/core/retrieval/chunk/chunk_test.go`

**Interfaces:**
- Consumes: `iface.RetrievalChunking{Strategy, MaxTokens, Overlap}`.
- Produces:
  ```go
  const (
      StrategyNone     = "none"
      StrategyFixed    = "fixed"
      MaxPieceRunes    = 32
      MaxChunkBytes    = 64512 // + 512-byte title + "\n\n" stays under the gateway's 64 KiB per input
      MaxChunksPerItem = 512
      MaxTokensLimit   = 4096
  )
  type Chunk struct {
      Ordinal   int
      Text      string // verbatim substring of the input
      CharStart int    // rune offset in the input, inclusive
      CharEnd   int    // rune offset, exclusive
  }
  func Validate(c iface.RetrievalChunking) error
  func CountTokens(text string) int
  // Split returns the chunks; truncated is true when text was cut by MaxChunkBytes (none) or MaxChunksPerItem (fixed).
  func Split(text string, c iface.RetrievalChunking) (chunks []Chunk, truncated bool, err error)
  ```

**Tokenization approximation (put verbatim in the package doc):** a token is a maximal run of runes for which `unicode.IsSpace` is false, when that run has at most 32 runes; a longer run counts as consecutive 32-rune pieces (the last may be shorter). Whitespace is never a token. One counted token approximates one word; subword tokenizers (bge-m3's XLM-R) produce roughly 1–2 model tokens per word in English and Italian, so a window of `MaxTokens` counted tokens may exceed `MaxTokens` model tokens. Operators choose `MaxTokens ≤ MaxInputTokens/2` of the embedding profile; the profile's `Truncate` covers any excess (the tail of an over-long window still reaches BM25, not the vector). A chunk's text runs from its first token's first rune to its last token's last rune.

`Validate`: `""` and `none` require `MaxTokens == 0 && Overlap == 0`; `fixed` requires `1 ≤ MaxTokens ≤ 4096` and `0 ≤ Overlap < MaxTokens`; anything else (including `hierarchical`) is an error. `""` is treated as `none` by `Split`.

`fixed` windows (not determined by the signature):

```go
start := 0
for start < n && len(out) < MaxChunksPerItem {
    end := min(start+c.MaxTokens, n)
    for end > start+1 && bytesOf(tokens[start:end]) > MaxChunkBytes { end-- }
    out = append(out, window(tokens[start:end]))
    if end == n { break }
    start = max(end-c.Overlap, start+1)
}
truncated = the loop stopped with tokens left
```

`none`: one window over every token; if its text exceeds `MaxChunkBytes`, it ends at the last token that fits (`truncated = true`). Whitespace-only text yields zero chunks for both strategies.

- [ ] **Step 1: Write the failing tests**

```go
func TestSplit_FixedWindowsWithOverlap(t *testing.T) {
    got, tr, _ := Split("a b c d e f g h i j", iface.RetrievalChunking{Strategy: "fixed", MaxTokens: 4, Overlap: 1})
    // tr == false; got == []Chunk{{0,"a b c d",0,7},{1,"d e f g",6,13},{2,"g h i j",12,19}}
}
func TestSplit_NoneTrimsOuterWhitespace(t *testing.T) {
    // Split("  hello world  ", {Strategy:"none"}) == []Chunk{{0,"hello world",2,13}}, false
    // Split(" \n\t ", {Strategy:"none"}) == nil/empty, false;  same for "fixed",4,1
}
func TestSplit_RuneOffsets(t *testing.T) {
    // Split("città è", {fixed,1,0}) == []Chunk{{0,"città",0,5},{1,"è",6,7}}
}
func TestSplit_LongRunsCountAs32RunePieces(t *testing.T) {
    // CountTokens(strings.Repeat("x",70)) == 3
    // Split(strings.Repeat("x",70), {fixed,2,0}) == []Chunk{{0, 64×"x", 0, 64}, {1, 6×"x", 64, 70}}
}
func TestSplit_NoneTruncatesAtTokenBoundary(t *testing.T) {
    got, tr, _ := Split(strings.Repeat("abcdefghij ", 7000), iface.RetrievalChunking{Strategy: "none"})
    // tr == true; len(got) == 1; len(got[0].Text) == 64503; got[0].CharEnd == 64503
    // strings.HasSuffix(got[0].Text, "abcdefghij")
}
func TestSplit_FixedCapsChunksPerItem(t *testing.T) {
    // Split(strings.Repeat("w ", 600), {fixed,1,0}) → len == 512, tr == true
}
func TestValidate(t *testing.T) {
    // ok: {none,0,0} {"",0,0} {fixed,1,0} {fixed,4096,0} {fixed,4,3}
    // err: {fixed,0,0} {fixed,4097,0} {fixed,4,4} {fixed,4,-1} {none,10,0} {hierarchical,4,0}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `cd backend && go test ./internal/core/retrieval/chunk/ -v`
Expected: FAIL (package has no Go files / undefined).

- [ ] **Step 3: Implement `Validate`, `CountTokens`, `Split` in `chunk.go`** following the definition and loop above.

- [ ] **Step 4: Run to verify they pass**

Run: `cd backend && go test ./internal/core/retrieval/chunk/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/core/retrieval/chunk
git commit -m "feat(retrieval): add chunking with a word-based token approximation"
```

---

### Task 4: `retrieval_outbox` model and repository

**Files:**
- Create: `backend/internal/core/retrieval/models/outbox.go`
- Create: `backend/internal/core/retrieval/repository/outbox.go`
- Create (if PR 3 has none): `backend/internal/core/retrieval/repository/testdb_test.go` — copy of `backend/internal/core/llm/repository/testdb_test.go` with db name prefix `retrieval_test_`, plus `tctx(tenantID string) context.Context` (tenant ID + kind `internal`)
- Test: `backend/internal/core/retrieval/repository/outbox_test.go`
- Modify: `backend/internal/core/retrieval/module.go` (`Collections()`), `backend/internal/core/retrieval/module_test.go` (PR 3's `TestModule_Declarations` pins the collection list to `[retrieval_corpus_state]` and an empty `ConfigSchema`; Tasks 4–6 and 12 extend those expectations as they add collections and keys)

**Interfaces:**
- Consumes: `iface.RetrievalIntent`, `tenantrepo.Scope`.
- Produces:
  ```go
  // models
  type AppliedState struct {
      Generation  int    `bson:"generation"`
      Revision    int64  `bson:"revision"`
      ProfileHash string `bson:"profileHash"`
      IndexedHash string `bson:"indexedHash"`
      Chunks      int    `bson:"chunks"`
  }
  type OutboxEntry struct {
      TenantID      string        `bson:"tenantId"`
      Corpus        string        `bson:"corpus"`
      SourceID      string        `bson:"sourceId"`
      Revision      int64         `bson:"revision"`   // highest revision enqueued ($max)
      Dirty         bool          `bson:"dirty"`      // set by Enqueue/Requeue/Reschedule, cleared by Claim
      Epoch         int64         `bson:"epoch"`      // +1 on every Claim
      LeaseOwner    string        `bson:"leaseOwner,omitempty"`
      LeaseUntil    *time.Time    `bson:"leaseUntil,omitempty"`
      Attempts      int           `bson:"attempts"`
      NextAttemptAt time.Time     `bson:"nextAttemptAt"`
      LastError     string        `bson:"lastError,omitempty"` // closed code set (Task 10)
      Applied       *AppliedState `bson:"applied,omitempty"`
      CreatedAt     time.Time     `bson:"createdAt"`
      UpdatedAt     time.Time     `bson:"updatedAt"`
  }
  // repository
  const CollOutbox = "retrieval_outbox"
  var ErrLeaseLost = errors.New("retrieval repository: lease lost")
  func NewOutbox(db *mongo.Database) *Outbox
  func (r *Outbox) Enqueue(ctx context.Context, intents []iface.RetrievalIntent, now time.Time) error
  func (r *Outbox) DueTenants(ctx context.Context, now time.Time, corpora []string, limit int) ([]string, error)
  func (r *Outbox) Claim(ctx context.Context, now time.Time, corpora []string, owner string, lease time.Duration) (*models.OutboxEntry, error) // nil when none
  func (r *Outbox) HoldsLease(ctx context.Context, corpus, sourceID string, epoch int64, owner string, now time.Time) (bool, error)
  func (r *Outbox) Release(ctx context.Context, corpus, sourceID string, epoch int64, owner string, applied *models.AppliedState, now time.Time) error // ErrLeaseLost
  func (r *Outbox) Reschedule(ctx context.Context, corpus, sourceID string, epoch int64, owner string, attempts int, next time.Time, code string) error // ErrLeaseLost
  func (r *Outbox) Requeue(ctx context.Context, corpus, sourceID string, now time.Time) error
  ```

Query shapes (all tenant-scoped through `tenantrepo.Scope(ctx, …)` except `DueTenants`):
- **Enqueue**, one `UpdateOne` per intent (keeps the `tenantscope` analyzer's coverage; `BulkWrite` is not checked), upsert on `{tenantId, corpus, sourceId}`: `$max {revision}`, `$set {dirty: true, updatedAt}`, `$setOnInsert {epoch: 0, attempts: 0, nextAttemptAt: now, createdAt: now}`. The caller's ctx is passed through unchanged, so a `mongo.SessionContext` makes it part of the caller's transaction. A key in backoff keeps its `nextAttemptAt`.
- **Claimable** = `corpus ∈ corpora AND nextAttemptAt ≤ now AND ((dirty = true AND leaseUntil = null) OR leaseUntil < now)`. The second branch recovers a holder that died.
- **DueTenants**: aggregate `$match claimable → $group {_id: $tenantId, oldest: $min nextAttemptAt} → $sort {oldest: 1, _id: 1} → $limit`; `//tenantscope:allow system: the index worker finds tenants with due keys; each key is then claimed and processed under its own tenant`.
- **Claim**: `FindOneAndUpdate(claimable, {$inc {epoch: 1}, $set {dirty: false, leaseOwner, leaseUntil: now+lease truncated to ms, updatedAt}}, sort {nextAttemptAt: 1}, ReturnDocument After)`.
- **HoldsLease**: count of `{corpus, sourceId, epoch, leaseOwner: owner, leaseUntil > now}` is 1.
- **Release**: filter `{corpus, sourceId, epoch, leaseOwner: owner}`; `$unset {leaseOwner, leaseUntil, lastError}`, `$set {attempts: 0, updatedAt, applied}` (`$unset applied` when nil). `dirty` is untouched, so an intent enqueued during the lease survives.
- **Reschedule**: same filter; `$unset {leaseOwner, leaseUntil}`, `$set {dirty: true, attempts, nextAttemptAt: next, lastError: code, updatedAt}`.
- **Requeue**: upsert on the key, `$set {dirty: true, updatedAt}`, `$setOnInsert` as Enqueue; no lease fields touched.

Indexes (`Collections()`): `{tenantId, corpus, sourceId}` unique; `{tenantId, dirty, nextAttemptAt}`; `{tenantId, leaseUntil}`; system sweeps `{dirty, nextAttemptAt}` and `{leaseUntil}`.

- [ ] **Step 1: Write the failing tests** (Mongo-gated; `now := time.Date(2026,10,26,10,0,0,0,time.UTC)`, `lease := 2*time.Minute`)

```go
func TestOutbox_EnqueueCoalescesRevisionsWithMax(t *testing.T)
//   Enqueue rev 3, then 2, then 5 for (t1, crm_contacts, c1) → one row; Revision 5, Dirty true, Epoch 0, Attempts 0
func TestOutbox_EnqueueJoinsTheCallerTransaction(t *testing.T)
//   sess.WithTransaction(tctx("t1"), fn): fn inserts {_id: "src"} into "src" collection and Enqueue(sc, c1 rev 1)
//   commit → row exists; second run whose fn returns errors.New("abort") after Enqueue(c2) → no c2 row, no second src doc
func TestOutbox_ClaimIncrementsEpochAndClearsDirty(t *testing.T)
//   Claim(now, ["crm_contacts"], "w1", lease) → Epoch 1, Dirty false, LeaseOwner "w1", *LeaseUntil == now.Add(lease)
//   Claim(now, …, "w2") → nil;  Claim(now.Add(lease+time.Millisecond), …, "w2") → Epoch 2, LeaseOwner "w2"
func TestOutbox_ClaimHonoursCorporaAndBackoff(t *testing.T)
//   row of corpus "other" → Claim(now, ["crm_contacts"]) nil; row with NextAttemptAt now+1m → nil until now+1m
func TestOutbox_ReleaseIsFencedByEpoch(t *testing.T)
//   w1 claims (epoch 1); w2 claims after expiry (epoch 2)
//   HoldsLease(c1, 1, "w1", t) == false; Release(c1, 1, "w1", …) == ErrLeaseLost; row LeaseOwner still "w2"
//   Reschedule(c1, 1, "w1", …) == ErrLeaseLost
func TestOutbox_EnqueueDuringLeaseKeepsKeyDirty(t *testing.T)
//   Claim (rev 1) → Enqueue rev 2 → Release(epoch 1, applied{Revision 1}) == nil → Dirty true, Revision 2, LeaseOwner "", Applied.Revision 1
func TestOutbox_RescheduleSetsBackoffAndDirty(t *testing.T)
//   Claim → Reschedule(…, attempts 1, now+5s, "embed") → Dirty true, Attempts 1, NextAttemptAt now+5s, LastError "embed", LeaseUntil nil
func TestOutbox_RequeueUpsertsDirty(t *testing.T)
//   Requeue on a missing key → row Dirty true, Epoch 0; on a held key → lease fields unchanged, Dirty true
func TestOutbox_DueTenantsOrdersByOldestAndHonoursLimit(t *testing.T)
//   t2 key due at now-2m, t1 at now-1m, t3 not due → DueTenants(now, corpora, 64) == ["t2","t1"]; limit 1 → ["t2"]
func TestOutbox_TenantScoped(t *testing.T)
//   row of t1 is never claimed under tctx("t2")

// module_test.go
func TestModule_IndexesLeadWithTenantOrAreDeclaredSweeps(t *testing.T)
//   same shape as llm/module_test.go:140-173, except each sweep entry carries `unique bool` (llm's test rejects any unique sweep);
//   sweeps: CollOutbox {"dirty","nextAttemptAt"} and {"leaseUntil"}, non-unique
//   (Task 6 adds CollErasures {"uuid"} unique — global tombstones have no tenantId — and {"scope","kind","subject"} non-unique)
```

- [ ] **Step 2: Run to verify they fail**

Run: `cd backend && MONGO_TEST_URI=$MONGO_TEST_URI go test ./internal/core/retrieval/repository/ ./internal/core/retrieval/ -run 'TestOutbox|TestModule_Indexes' -v`
Expected: FAIL to compile.

- [ ] **Step 3: Implement the model, repository and indexes** as specified above.

- [ ] **Step 4: Run to verify they pass** — same command. Expected: PASS (SKIP of Mongo tests without `MONGO_TEST_URI` proves nothing).

- [ ] **Step 5: Commit**

```bash
git add backend/internal/core/retrieval/models backend/internal/core/retrieval/repository backend/internal/core/retrieval/module.go backend/internal/core/retrieval/module_test.go
git commit -m "feat(retrieval): add the per-key outbox with leases and epochs"
```

---

### Task 5: corpus state and tenant progress

**Files:**
- Create: `backend/internal/core/retrieval/models/corpus_state.go`, `models/tenant_progress.go`
- Create: `backend/internal/core/retrieval/repository/corpus_state.go`, `repository/tenant_progress.go`
- Test: `repository/corpus_state_test.go`, `repository/tenant_progress_test.go`
- Modify: `module.go` (`Collections()`: `retrieval_tenant_progress` with `{tenantId, corpus, generation}` unique; `retrieval_corpus_state` stays as PR 3 declared it, `_id` only)

**Interfaces:**
- Produces:
  ```go
  const (
      CorpusStatusActive         = "active"
      CorpusStatusProfileChanged = "profile_changed"
      CorpusStatusError          = "error"
      ReasonLocality        = "locality"         // local_only corpus, embed endpoint boundary hosted
      ReasonEndpointUnknown = "endpoint_unknown" // InferenceProfile → ErrLLMNotConfigured
      ReasonEndpointKind    = "endpoint_kind"    // endpoint kind is not embed
  )
  type CorpusState struct { // installation-wide, _id = corpus
      Corpus               string    `bson:"_id"`
      Owner                string    `bson:"owner"`
      Status               string    `bson:"status"`
      Reason               string    `bson:"reason,omitempty"`
      ActiveGeneration     int       `bson:"activeGeneration"` // 0 = none yet
      Collection           string    `bson:"collection,omitempty"`
      IngestionProfileHash string    `bson:"ingestionProfileHash,omitempty"`
      EmbeddingProfileID   string    `bson:"embeddingProfileId,omitempty"`
      Dimensions           int       `bson:"dimensions,omitempty"`
      Locality             string    `bson:"locality"`
      DeclaredProfileHash  string    `bson:"declaredProfileHash,omitempty"` // set while profile_changed
      Version              int64     `bson:"version"`
      CreatedAt            time.Time `bson:"createdAt"`
      UpdatedAt            time.Time `bson:"updatedAt"`
      // Delivery step 2 adds the generation being built.
  }
  const ProgressBuilding, ProgressDone = "building", "done"
  type TenantProgress struct {
      TenantID   string     `bson:"tenantId"`
      Corpus     string     `bson:"corpus"`
      Generation int        `bson:"generation"`
      Status     string     `bson:"status"`
      Cursor     string     `bson:"cursor"`
      Enqueued   int64      `bson:"enqueued"`
      Epoch      int64      `bson:"epoch"`
      LeaseOwner string     `bson:"leaseOwner,omitempty"`
      LeaseUntil *time.Time `bson:"leaseUntil,omitempty"`
      StartedAt  time.Time  `bson:"startedAt"`
      UpdatedAt  time.Time  `bson:"updatedAt"`
      DoneAt     *time.Time `bson:"doneAt,omitempty"`
  }
  // repository (use PR 3's constant for retrieval_corpus_state if it declared one)
  const CollTenantProgress = "retrieval_tenant_progress"
  var ErrStateExists   = errors.New("retrieval repository: corpus state exists")
  var ErrStateConflict = errors.New("retrieval repository: corpus state changed concurrently")
  func NewCorpusStates(db *mongo.Database) *CorpusStates
  func (r *CorpusStates) Get(ctx context.Context, corpus string) (*models.CorpusState, error) // nil, nil when absent
  func (r *CorpusStates) List(ctx context.Context) ([]models.CorpusState, error)
  func (r *CorpusStates) Insert(ctx context.Context, st *models.CorpusState) error // Version 1; ErrStateExists
  func (r *CorpusStates) Save(ctx context.Context, st *models.CorpusState) error   // CAS on st.Version, then st.Version++; ErrStateConflict
  func NewTenantProgress(db *mongo.Database) *TenantProgress
  func (r *TenantProgress) ClaimBuild(ctx context.Context, corpus string, generation int, owner string, now time.Time, lease time.Duration) (*models.TenantProgress, error) // nil when done or held
  func (r *TenantProgress) Advance(ctx context.Context, corpus string, generation int, epoch int64, owner, cursor string, added int64, done bool, now time.Time) error // releases the lease; ErrLeaseLost
  func (r *TenantProgress) Get(ctx context.Context, corpus string, generation int) (*models.TenantProgress, error)
  ```

`CorpusStates` queries carry `//tenantscope:allow system: installation-wide corpus state (ADR-0023 D11), one document per corpus`. `ClaimBuild` is one `FindOneAndUpdate` with `upsert: true` on `{tenantId, corpus, generation, status ≠ done, leaseUntil null or < now}`, `$inc epoch`, `$set {leaseOwner, leaseUntil, updatedAt}`, `$setOnInsert {status: building, cursor: "", enqueued: 0, startedAt}`; a duplicate-key error means the row exists and is done or held → `nil, nil`. `Advance` is fenced on `{epoch, leaseOwner}`, `$set {cursor, updatedAt}`, `$inc {enqueued: added}`, `$unset` the lease, and when `done` sets `status: done, doneAt`.

- [ ] **Step 1: Write the failing tests** (Mongo-gated)

```go
func TestCorpusStates_InsertGetSaveCAS(t *testing.T)
//   Get("crm_contacts") == nil,nil; Insert(st) → st.Version 1; Insert again → ErrStateExists
//   a := Get; b := Get; a.Status = profile_changed; Save(a) == nil, a.Version == 2; Save(b) == ErrStateConflict
//   List() has 1 element
func TestTenantProgress_ClaimBuildAdvanceResume(t *testing.T)
//   ClaimBuild(tctx t1, "crm_contacts", 1, "w1", now, lease) → Status building, Cursor "", Epoch 1
//   ClaimBuild(…, "w2", now, …) → nil (held)
//   Advance(…, epoch 1, "w1", "c200", 200, false, now) == nil
//   ClaimBuild(…, "w2", now, …) → Cursor "c200", Enqueued 200, Epoch 2
//   Advance(…, epoch 1, "w1", …) == ErrLeaseLost
//   Advance(…, epoch 2, "w2", "", 50, true, now) → Get: Status done, Enqueued 250, DoneAt != nil
//   ClaimBuild again → nil;  Get under tctx t2 → nil (tenant-scoped)
```

- [ ] **Step 2: Run to verify they fail** — `cd backend && MONGO_TEST_URI=$MONGO_TEST_URI go test ./internal/core/retrieval/repository/ -run 'TestCorpusStates|TestTenantProgress' -v` → FAIL to compile.
- [ ] **Step 3: Implement** the models, both repositories and the index.
- [ ] **Step 4: Run to verify they pass** — same command → PASS.
- [ ] **Step 5: Commit**

```bash
git add backend/internal/core/retrieval
git commit -m "feat(retrieval): add corpus state and per-tenant build progress"
```

---

### Task 6: tombstone store and `Tombstones.Covered`

**Files:**
- Create: `backend/internal/core/retrieval/models/erasure.go` (the roadmap's `Erasure` struct verbatim, with the PR 6 comment)
- Create: `backend/internal/core/retrieval/repository/erasures.go`, test `repository/erasures_test.go`
- Create: `backend/internal/core/retrieval/services/tombstones.go`, test `services/tombstones_test.go`
- Modify: `module.go` (`Collections()`), `module_test.go` (sweep list)

**Interfaces:**
- Produces:
  ```go
  // models
  const ErasureScopeGlobal, ErasureScopeTenant = "global", "tenant"
  const ErasureKindUser, ErasureKindSubject, ErasureKindItem, ErasureKindTenant = "user", "subject", "item", "tenant"
  // repository
  const CollErasures = "retrieval_erasures"
  var ErrInvalidErasure = errors.New("retrieval repository: invalid erasure")
  func NewErasures(db *mongo.Database) *Erasures
  // Insert validates the shape, sets UUID (if empty) and CreatedAt (now UTC).
  // global: Kind user, TenantID "", Subject "user:<non-empty>", no Corpus/SourceID.
  // tenant: TenantID = ctx tenant (StampInsert; a different non-empty TenantID is refused),
  //         Kind subject (Subject set) | item (Corpus and SourceID set) | tenant.
  func (r *Erasures) Insert(ctx context.Context, e *models.Erasure) error
  // Covering returns the global user tombstones naming any of subjects and the
  // tenant tombstones of ctx's tenant (kind tenant; item for corpus + sourceIDs; subject in subjects).
  func (r *Erasures) Covering(ctx context.Context, corpus string, sourceIDs, subjects []string) ([]models.Erasure, error)
  // services
  type ErasureReader interface {
      Covering(ctx context.Context, corpus string, sourceIDs, subjects []string) ([]models.Erasure, error)
  }
  type Probe struct { SourceID string; Subjects []string }
  type Tombstones struct{ /* reader */ }
  func NewTombstones(r ErasureReader) *Tombstones
  func (t *Tombstones) Covered(ctx context.Context, tenantID, corpus string, probes []Probe) (map[string]bool, error)
  ```

`Covering` runs two queries: global (`{scope: global, kind: user, subject: {$in: subjects with prefix "user:"}}`, `//tenantscope:allow system: global user tombstones carry no tenant (ADR-0023 D12)`; skipped when no `user:` subject) and tenant (`tenantrepo.Scope(ctx, {scope: tenant, $or: [{kind: tenant}, {kind: item, corpus, sourceId: {$in}}, {kind: subject, subject: {$in}}]})`). The global insert carries `//tenantscope:allow dsr: global user tombstone written by the erasure path (ADR-0023 D12)`. `Covered` refuses `tenantID == ""` and a ctx tenant different from `tenantID`; returns an entry (true/false) for every probe; with no probes it returns an empty map without a query; it re-matches every returned document in Go against scope, kind, tenant, corpus, source and subject before marking a probe covered. Callers treat an error as "covered" for writes (the worker reschedules; PR 5 fails closed).

Indexes: `{uuid}` unique (sweep), `{scope, kind, subject}` (sweep), `{tenantId, kind, corpus, sourceId}`, `{tenantId, kind, subject}`.

- [ ] **Step 1: Write the failing tests**

```go
// services/tombstones_test.go (fake ErasureReader returning a fixed slice and recording calls)
func TestTombstones_Covered(t *testing.T) {
    // table, tenant "t1", corpus "crm_contacts", probes a{Subjects [user:u1, crm:1]}, b{Subjects [user:u2]}:
    // no erasures                                 → {a:false, b:false}
    // global user "user:u1"                       → {a:true,  b:false}
    // tenant t1 kind tenant                       → {a:true,  b:true}
    // tenant t1 item crm_contacts/a               → {a:true,  b:false}
    // tenant t1 item other_corpus/a               → {a:false, b:false}
    // tenant t2 kind tenant (reader misbehaves)   → {a:false, b:false}
    // tenant t1 subject "crm:1"                   → {a:true,  b:false}
}
func TestTombstones_CoveredRefusesForeignTenantAndPropagatesErrors(t *testing.T)
//   ctx tenant t2, tenantID t1 → error, reader not called; reader error → (nil, error); no probes → empty map, reader not called

// repository/erasures_test.go (Mongo-gated)
func TestErasures_InsertValidatesShape(t *testing.T)
//   refused (errors.Is ErrInvalidErasure): global with TenantID; global kind item; global subject "crm:1";
//   tenant kind user; item without SourceID; subject without Subject; tenant TenantID "t2" under tctx t1
//   accepted: tenant item under tctx t1 → stored TenantID "t1", UUID non-empty
func TestErasures_CoveringFindsGlobalAndTenantTombstones(t *testing.T)
//   insert global user:u1, t1 item crm_contacts/a, t1 subject crm:1, t2 kind tenant
//   Covering(tctx t1, "crm_contacts", ["a","b"], ["user:u1","crm:1"]) → exactly the first three
```

- [ ] **Step 2: Run to verify they fail** — `cd backend && MONGO_TEST_URI=$MONGO_TEST_URI go test ./internal/core/retrieval/repository/ ./internal/core/retrieval/services/ -run 'TestErasures|TestTombstones' -v` → FAIL to compile.
- [ ] **Step 3: Implement** model, repository, service, indexes and sweep-list entries.
- [ ] **Step 4: Run to verify they pass** — same command → PASS.
- [ ] **Step 5: Commit**

```bash
git add backend/internal/core/retrieval
git commit -m "feat(retrieval): add erasure tombstones and the coverage check"
```

---

### Task 7: `Outbox.Enqueue` — `iface.RetrievalOutbox`

**Files:**
- Create: `backend/internal/core/retrieval/services/outbox.go`
- Test: `backend/internal/core/retrieval/services/outbox_test.go`
- Modify: PR 3's `services.Service` (one field + two methods; if PR 3 left a stub `Enqueue`, replace it)

**Interfaces:**
- Consumes: `repository.(*Outbox).Enqueue` (Task 4); PR 3's corpus registry through:
  ```go
  // services — what indexing needs from PR 3's registry. PR 3's plan gives
  // *services.Service exactly these two methods; if the merged code differs,
  // module.go wraps it in an adapter.
  type CorpusCatalog interface {
      Corpus(name string) (iface.RetrievalCorpusSpec, bool)
      Corpora() []iface.RetrievalCorpusSpec // sorted by Name
  }
  ```
- Produces:
  ```go
  type OutboxWriter interface {
      Enqueue(ctx context.Context, intents []iface.RetrievalIntent, now time.Time) error
  }
  const MaxIntentsPerEnqueue = 1000
  func ValidateIntent(i iface.RetrievalIntent) error // wraps iface.ErrRetrievalInvalidIntent
  func NewOutbox(store OutboxWriter, catalog CorpusCatalog, now func() time.Time) *Outbox
  func (o *Outbox) Enqueue(ctx context.Context, intents ...iface.RetrievalIntent) error
  func (s *Service) SetOutbox(o *Outbox)
  func (s *Service) Enqueue(ctx context.Context, intents ...iface.RetrievalIntent) error // delegates
  var _ iface.RetrievalOutbox = (*Outbox)(nil)
  var _ iface.RetrievalOutbox = (*Service)(nil)
  ```

Order: no intents → nil, no I/O; more than 1000 → `ErrRetrievalInvalidIntent`; `tenantrepo.RequireInternalTenant(ctx)` error → returned wrapped (D13); any unregistered corpus → `ErrRetrievalUnknownCorpus`; any `ValidateIntent` failure → `ErrRetrievalInvalidIntent`; all checks before any write; then `store.Enqueue(ctx, intents, now())` with the caller's ctx untouched. The owning module being stopped or the engine being unconfigured never refuses an intent (D1).

- [ ] **Step 1: Write the failing tests** (fake store records the ctx and intents; fake catalog knows `crm_contacts`; ctx helper sets tenant + kind)

```go
func TestEnqueue_PassesIntentsWithTheCallerContext(t *testing.T)
//   ctx := context.WithValue(tctx("t1"), probeKey{}, "session") ; Enqueue(ctx, {crm_contacts, c1, 1})
//   store called once; store ctx.Value(probeKey{}) == "session"; intents equal; now == fixed clock
func TestEnqueue_EmptyIsNoop(t *testing.T)                  // store not called, nil
func TestEnqueue_RefusesExternalTenant(t *testing.T)        // kind "external" → errors.Is(err, tenantrepo.ErrTenantKindMismatch); store not called
func TestEnqueue_RefusesUnknownCorpus(t *testing.T)         // {"nope", c1, 1} → errors.Is(err, iface.ErrRetrievalUnknownCorpus)
func TestEnqueue_RefusesInvalidIntents(t *testing.T)
//   table → errors.Is(err, iface.ErrRetrievalInvalidIntent), store not called:
//   SourceID "", 257×"a", "a\x00b", string([]byte{0xff}), Revision -1, 1001 intents;
//   a valid intent followed by an invalid one → nothing written
func TestService_IsTheRegisteredOutbox(t *testing.T)        // s.SetOutbox(o); s.Enqueue(...) reaches the fake store
```

- [ ] **Step 2: Run to verify they fail** — `cd backend && go test ./internal/core/retrieval/services/ -run 'TestEnqueue|TestService_IsTheRegisteredOutbox' -v` → FAIL to compile.
- [ ] **Step 3: Implement** `outbox.go` and the `Service` additions.
- [ ] **Step 4: Run to verify they pass** — same command → PASS.
- [ ] **Step 5: Commit**

```bash
git add backend/internal/core/retrieval/services
git commit -m "feat(retrieval): accept index intents in the owner's transaction"
```

---

### Task 8: ingestion profile hash, locality and corpus bootstrap

**Files:**
- Create: `backend/internal/core/retrieval/services/profilehash.go`, test `profilehash_test.go`
- Create: `backend/internal/core/retrieval/services/gate.go`, test `gate_test.go`
- Create: `backend/internal/core/retrieval/services/fakes_test.go` (`fakeEngine`, `fakeProfiles`, `fakeModules`, `fakeStates`, `fakeCatalog`, `clock`)
- Create: `backend/internal/core/retrieval/services/worker.go` (types and constructor only; Task 10 adds processing)

**Interfaces:**
- Consumes: `iface.LLMInferenceProfiles`, `iface.InferenceProfile`, `iface.ErrLLMNotConfigured`, `iface.ErrLLMProfileMismatch`; `engine.Engine.EnsureCollection`, `engine.CollectionSpec`; Task 5 `CorpusStates`; Task 1 `module.ModuleStatusReader`.
- Produces:
  ```go
  func IngestionProfileHash(p iface.RetrievalIngestionProfile, embeddingProfileID string) string // "ing_" + hex sha256
  // Collection names come only from PR 3's engine.CollectionName(prefix, corpus, generation).
  type CorpusStateStore interface {
      Get(ctx context.Context, corpus string) (*models.CorpusState, error)
      Insert(ctx context.Context, st *models.CorpusState) error
      Save(ctx context.Context, st *models.CorpusState) error
  }
  type OutboxStore interface { // implemented by *repository.Outbox; used from Task 10
      OutboxWriter
      DueTenants(ctx context.Context, now time.Time, corpora []string, limit int) ([]string, error)
      Claim(ctx context.Context, now time.Time, corpora []string, owner string, lease time.Duration) (*models.OutboxEntry, error)
      HoldsLease(ctx context.Context, corpus, sourceID string, epoch int64, owner string, now time.Time) (bool, error)
      Release(ctx context.Context, corpus, sourceID string, epoch int64, owner string, applied *models.AppliedState, now time.Time) error
      Reschedule(ctx context.Context, corpus, sourceID string, epoch int64, owner string, attempts int, next time.Time, code string) error
      Requeue(ctx context.Context, corpus, sourceID string, now time.Time) error
  }
  type ProgressStore interface { // implemented by *repository.TenantProgress; used from Task 11
      ClaimBuild(ctx context.Context, corpus string, generation int, owner string, now time.Time, lease time.Duration) (*models.TenantProgress, error)
      Advance(ctx context.Context, corpus string, generation int, epoch int64, owner, cursor string, added int64, done bool, now time.Time) error
  }
  type TombstoneChecker interface { // implemented by *Tombstones
      Covered(ctx context.Context, tenantID, corpus string, probes []Probe) (map[string]bool, error)
  }
  var _ OutboxStore = (*repository.Outbox)(nil)
  var _ ProgressStore = (*repository.TenantProgress)(nil)
  var _ CorpusStateStore = (*repository.CorpusStates)(nil)
  type WorkerSettings struct { Lease time.Duration; Batch int; PollInterval time.Duration }
  const MinWorkerLease, MaxWorkerLease = 30 * time.Second, 15 * time.Minute
  const IndexingFencingWindow = 2 * MaxWorkerLease
  func ClampWorkerSettings(s WorkerSettings) WorkerSettings
  type WorkerDeps struct {
      Outbox     OutboxStore
      States     CorpusStateStore
      Progress   ProgressStore
      Tombstones TombstoneChecker
      Catalog    CorpusCatalog
      Engine     engine.Engine
      Profiles   iface.LLMInferenceProfiles
      Embedder   iface.LLMPinnedEmbedder
      Modules    module.ModuleStatusReader
      Tenants    iface.TenantIDLister
      Prefix     string
      Settings   func() WorkerSettings // read every pass (hot reload), clamped
      Now        func() time.Time
      Logger     *slog.Logger
  }
  func NewWorker(d WorkerDeps) *Worker // owner ID = uuid.NewString(); no I/O
  // corpusGate is what one pass may write for a corpus.
  type corpusGate struct { Spec iface.RetrievalCorpusSpec; State models.CorpusState; ProfileHash string }
  func (w *Worker) prepareCorpus(ctx context.Context, spec iface.RetrievalCorpusSpec) (gate *corpusGate, problem string, err error)
  ```

**Hash definition.** `IngestionProfileHash` marshals, with `encoding/json`, a private struct whose fields and tags are fixed here (independent of the `iface` tags):

```go
type hashInput struct {
    V                int              `json:"v"`                // 1: bump when the code's transformation changes (title prefix, tokenization)
    EmbeddingProfile string           `json:"embeddingProfile"` // resolved InferenceProfile.ID
    Analyzer         struct{ Tokenizer string `json:"tokenizer"`; Language string `json:"language"`; Stemming bool `json:"stemming"`; StopWords bool `json:"stopWords"` } `json:"analyzer"`
    Chunking         struct{ Strategy string `json:"strategy"`; MaxTokens int `json:"maxTokens"`; Overlap int `json:"overlap"` } `json:"chunking"`
    Enrichment       *struct{ ChatEndpoint string `json:"chatEndpoint"`; PromptVersion string `json:"promptVersion"` } `json:"enrichment"`
}
```

Normalization before marshalling: `Tokenizer "" → "word"`; `Strategy "" → "none"`; for `none`, `MaxTokens = Overlap = 0`. `EmbedEndpoint` (the endpoint *name*) is excluded: the resolved profile ID identifies the model, and renaming an endpoint must not start a new generation.

**Gate decision table** (`prepareCorpus`; checks in this order; "problem" strings are exact, `%s` = corpus, `%q` = endpoint):

| Condition | State write | Engine | Result | Problem |
|---|---|---|---|---|
| `!Modules.IsStarted(spec.Owner)` | none | none | skip | "" (expected while disabled) |
| `InferenceProfile` → `ErrLLMNotConfigured` | upsert error/`endpoint_unknown` | none | skip | `corpus %s: embed endpoint %q is not configured` |
| other `InferenceProfile` error, or `Dimensions ≤ 0` | none | none | skip | `corpus %s: inference profile unavailable` |
| `profile.Kind != embed` | upsert error/`endpoint_kind` | none | skip | `corpus %s: endpoint %q is not an embedding endpoint` |
| locality `""`/`local_only` and `Boundary != local` | upsert error/`locality` | none | skip | `corpus %s: local_only corpus uses hosted endpoint %q` |
| no state, or `ActiveGeneration == 0` | after `EnsureCollection`, Insert/Save `{active, gen 1, engine.CollectionName(prefix, corpus, 1), hash, profile.ID, Dimensions, Locality, Owner}` | `EnsureCollection{Name, Dimensions: profile.Dimensions, Fields: spec.Filters, Analyzer: spec.Ingestion.Analyzer}` | gate | engine error → skip, `corpus %s: vector engine unavailable` |
| state hash ≠ computed hash | Save `{profile_changed, DeclaredProfileHash: hash}` (only if not already so) | none | skip | `corpus %s: ingestion profile changed; indexing paused until a new generation is built` |
| state not active, hash equal | Save `{active, Reason "", DeclaredProfileHash ""}` | none | gate | "" |
| active, hash equal | none | none | gate | "" |

"Upsert error/…" = Insert `{Status error, Reason, ActiveGeneration 0, Owner, Locality}` when absent, else Save with `Status error, Reason` (generation fields kept). `ErrStateExists`/`ErrStateConflict` → skip this pass with no problem (another replica moved it). `profile_changed` keeps `ActiveGeneration`, `Collection`, `EmbeddingProfileID` and `IngestionProfileHash` of the active generation untouched; the generation build that would drain its intents is delivery step 2.

- [ ] **Step 1: Write the failing tests**

```go
func TestIngestionProfileHash_Golden(t *testing.T) {
    p := iface.RetrievalIngestionProfile{EmbedEndpoint: "embed",
        Analyzer: iface.RetrievalAnalyzer{Tokenizer: "word", Language: "english", Stemming: true, StopWords: true},
        Chunking: iface.RetrievalChunking{Strategy: "fixed", MaxTokens: 256, Overlap: 32}}
    // IngestionProfileHash(p, "ip_test") == "ing_2081e190bb0f276d81cd2dfb97d27dab2db7c910dddd201a4f615e7588336ab7"
}
func TestIngestionProfileHash_NormalizesDefaults(t *testing.T) {
    p := iface.RetrievalIngestionProfile{EmbedEndpoint: "other",
        Chunking: iface.RetrievalChunking{Strategy: "", MaxTokens: 99, Overlap: 3}}
    // IngestionProfileHash(p, "ip_test") == "ing_2dcbe5e728fe270dc429a92aa72aca3ecc62698136dfcc2da75f31cde34bebac"
}
func TestIngestionProfileHash_EveryTransformationFieldMatters(t *testing.T)
//   vs the golden input, each differs: profile ID "ip_other"; Language "italian"; Stemming false; StopWords false;
//   Tokenizer "multilingual"; Strategy "none"; MaxTokens 128; Overlap 0; Enrichment &{ "chat", "v1" }
//   and only EmbedEndpoint "renamed" → equal
func TestClampWorkerSettings(t *testing.T)
//   {0,0,0} → {2m,32,5s}; {1s,1000,100ms} → {30s,256,1s}; {1h,-3,1h} → {15m,32,5m}; {90s,7,2s} → unchanged
func TestGate_BootstrapCreatesGenerationOne(t *testing.T)
//   no state; profile {ID "ip_a", Kind embed, Boundary local, Dimensions 8}; prefix "orkestra"
//   → EnsureCollection once with {Name "orkestra_crm_contacts_g1", Dimensions 8, Fields spec.Filters, Analyzer spec.Ingestion.Analyzer}
//   → state {active, gen 1, Collection "orkestra_crm_contacts_g1", EmbeddingProfileID "ip_a", Dimensions 8,
//            IngestionProfileHash == IngestionProfileHash(spec.Ingestion, "ip_a")}; gate != nil; problem ""
func TestGate_LocalOnlyRefusesHostedEndpoint(t *testing.T)
//   Locality "" and Boundary hosted → no EnsureCollection; state {error, Reason locality, gen 0};
//   problem == `corpus crm_contacts: local_only corpus uses hosted endpoint "embed"`
//   same with Locality hosted_allowed → gate, state active
func TestGate_ProfileChangedKeepsTheActiveGeneration(t *testing.T)
//   state active gen 1 with hash of {fixed,256,32}; spec now {fixed,128,16}
//   → state {profile_changed, DeclaredProfileHash = new hash, IngestionProfileHash = old hash, gen 1, same Collection}
//   → no EnsureCollection; gate nil; problem contains "ingestion profile changed"
//   revert spec → state active, DeclaredProfileHash "", gate != nil
func TestGate_OwnerNotStartedTouchesNothing(t *testing.T)   // no state, no profile call, no engine call, problem ""
func TestGate_TransientProfileErrorWritesNothing(t *testing.T)
//   ErrLLMProviderUnavailable, ErrLLMProfileMismatch, Dimensions 0 → no state write; problem "corpus crm_contacts: inference profile unavailable"
func TestGate_UnknownEndpointPersistsErrorAndRecovers(t *testing.T)
//   ErrLLMNotConfigured → state {error, endpoint_unknown}; next call with a valid profile → bootstrap, state active gen 1
```

- [ ] **Step 2: Run to verify they fail** — `cd backend && go test ./internal/core/retrieval/services/ -run 'TestIngestionProfileHash|TestClampWorkerSettings|TestGate' -v` → FAIL to compile.
- [ ] **Step 3: Implement** `profilehash.go`, `gate.go`, the types and constructor in `worker.go`, and the fakes. `fakeEngine` mirrors the `engine.Engine` contract in memory (collection → point ID → `engine.Point`): `Upsert` leaves a point whose stored revision is higher; `UpdatePayload` per Task 2; `DeleteOtherRevisions`, `DeleteByFilter` (Selector fields ANDed), `Count`; `EnsureCollection` records specs; `Query` returns nil; `Health` returns a settable error; optional hooks `beforeUpsert`, `beforeDeleteOthers func()`; a call log.
- [ ] **Step 4: Run to verify they pass** — same command → PASS.
- [ ] **Step 5: Commit**

```bash
git add backend/internal/core/retrieval/services
git commit -m "feat(retrieval): bootstrap generation 1 and enforce corpus locality"
```

---

### Task 9: item validation, fields and points

**Files:**
- Create: `backend/internal/core/retrieval/services/ingest.go`, test `ingest_test.go`

**Interfaces:**
- Consumes: `chunk.Chunk`, `engine.Point`, `engine.Runtime`, `engine.PointID`, `iface.RetrievalItem`, `iface.RetrievalFilterField`.
- Produces:
  ```go
  const MaxTitleBytes, MaxPrincipalsPerItem, MaxSubjectsPerItem, MaxRefBytes = 512, 1000, 100, 256
  var errInvalidItem = errors.New("retrieval: invalid item")
  func validateItem(item iface.RetrievalItem) error
  func normalizeFields(schema []iface.RetrievalFilterField, in map[string]any) (map[string]any, error)
  func indexedText(title, chunkText string) string
  func contentHash(text string) string           // hex(sha256(text))
  func ChunkID(sourceID string, ordinal int) string // sourceID + "#" + strconv.Itoa(ordinal)
  func buildPoints(tenantID, corpus, profileHash string, item iface.RetrievalItem, chunks []chunk.Chunk, fields map[string]any) []engine.Point
  func indexedHash(points []engine.Point) string  // hex(sha256(strings.Join(each point's Runtime.ContentHash, "\n"))), ordinal order
  ```

Rules: `validateItem` refuses (wrapping `errInvalidItem`) more than 1000 `VisibleTo`, more than 100 `Subjects`, or any entry empty or longer than 256 bytes. `normalizeFields` (use PR 3's `filter` field-value validator instead if it exports one with these rules): undeclared key → error naming the key only; `nil` value → key omitted; `keyword` string ≤ 1024 bytes; `keyword[]` `[]string` or `[]any` of strings, ≤ 100 elements; `integer` any Go integer, or a `float64` with an integral value within ±2^53, → `int64`; `float` any integer or float, finite, → `float64`; `bool`; `datetime` `time.Time` or RFC 3339 string → `int64` Unix **milliseconds** UTC (PR 3's engine value representation; the driver indexes it as `integer`). `indexedText` = title cut to 512 bytes at a rune boundary + `"\n\n"` + chunk text, or the chunk text alone when the title is blank. `buildPoints`: one point per chunk, `ID = engine.PointID(tenantID, corpus, item.SourceID, ordinal)`, `Text = indexedText`, `Runtime{TenantID, SourceID, SourceRevision: item.Revision, ChunkID, Ordinal, VisibleTo, Subjects (copies), ContentHash: contentHash(Text), IngestionProfile: profileHash, CharStart, CharEnd}`, `Fields = fields`; chunks whose indexed text is blank are skipped.

- [ ] **Step 1: Write the failing tests**

```go
func TestNormalizeFields(t *testing.T)
//   schema stage keyword, tags keyword[], size integer, score float, vip bool, seen datetime
//   ok: {"stage":"lead","tags":[]any{"a","b"},"size":float64(3),"score":2,"vip":true,"seen":"2026-10-26T12:00:00+02:00","x":nil}
//     → {"stage":"lead","tags":[]string{"a","b"},"size":int64(3),"score":float64(2),"vip":true,"seen":int64(1793008800000)}  // 2026-10-26T10:00:00Z in ms
//   errors (errors.Is errInvalidItem; message contains the key, never the value):
//     {"undeclared":1} {"size":3.5} {"score":math.Inf(1)} {"seen":"yesterday"} {"tags":101 strings} {"stage":1}
func TestValidateItem_Bounds(t *testing.T)            // 1001 VisibleTo, 101 Subjects, "" entry, 257-byte entry → error; 1000/100/256 → ok
func TestIndexedText(t *testing.T)
//   ("Mario Rossi","sales lead") == "Mario Rossi\n\nsales lead"; ("  ","x") == "x"; 600-byte title → first 512 bytes, valid UTF-8
func TestBuildPoints(t *testing.T)
//   tenant t1, corpus crm_contacts, hash "ing_x", item c1 rev 4, chunks from chunk.Split("a b c d e f g h i j", {fixed,4,1})
//   → 3 points; IDs == engine.PointID("t1","crm_contacts","c1",0..2); Runtime.ChunkID "c1#0".."c1#2";
//     CharStart/End (0,7) (6,13) (12,19); SourceRevision 4; IngestionProfile "ing_x"; ContentHash == contentHash(Text)
//   mutating item.VisibleTo afterwards does not change points[0].Runtime.VisibleTo
func TestIndexedHash_ChangesWithAnyChunk(t *testing.T) // same points → same hash; one chunk text differs → different; order matters
```

- [ ] **Step 2: Run to verify they fail** — `cd backend && go test ./internal/core/retrieval/services/ -run 'TestNormalizeFields|TestValidateItem|TestIndexedText|TestBuildPoints|TestIndexedHash' -v` → FAIL to compile.
- [ ] **Step 3: Implement `ingest.go`.**
- [ ] **Step 4: Run to verify they pass** — same command → PASS.
- [ ] **Step 5: Commit**

```bash
git add backend/internal/core/retrieval/services
git commit -m "feat(retrieval): build points with provenance and indexed-text hashes"
```

---

### Task 10: per-key processing, fencing and the stale-worker scenario

**Files:**
- Modify: `backend/internal/core/retrieval/services/worker.go`
- Modify: `services/fakes_test.go` (`fakeOutbox` with Task 4 semantics, `fakeSource`, `fakeEmbedder`, real `Tombstones` over a fake `ErasureReader`)
- Test: `backend/internal/core/retrieval/services/worker_test.go`

**Interfaces:**
- Consumes: Tasks 3, 4, 6, 8, 9; `iface.RetrievalSource.Load`; `iface.LLMPinnedEmbedder.EmbedWithProfile`; `engine.Engine` `Upsert`, `UpdatePayload`, `DeleteOtherRevisions`, `DeleteByFilter`, `Count`.
- Produces:
  ```go
  const ( codeSource = "source"; codeSourceBehind = "source_behind"; codeInvalidItem = "invalid_item"; codeEmbed = "embed"
          codeBudget = "budget"; codeEngine = "engine"; codeDeadline = "deadline"; codeTombstone = "tombstone" )
  func backoff(attempts int) time.Duration // 5s<<(attempts-1), capped at 30m (shift capped to avoid overflow)
  func tenantCtx(ctx context.Context, tenantID string) context.Context // ctxauth.KeyTenantID + WithTenantKind "internal"
  type PassReport struct { Claimed, Indexed, PayloadOnly, Removed, Rescheduled, Requeued, Enqueued int }
  func (w *Worker) process(ctx context.Context, g *corpusGate, e *models.OutboxEntry, s WorkerSettings, rep *PassReport)
  ```

**Processing algorithm** (`process`; `T` = `e.TenantID`, `C` = corpus, `id` = `e.SourceID`, `coll` = `g.State.Collection`):

```
tctx  := tenantCtx(ctx, T)
wctx  := context.WithDeadline(tctx, e.LeaseUntil.Add(-s.Lease/4))   // every Load/Embed/engine call uses wctx
guard := HoldsLease(tctx, C, id, e.Epoch, owner, now()); false → Requeue(tctx, C, id), rep.Requeued++, stop (no release)
1. Covered(wctx, T, C, [{id, nil}])            err → reschedule(tombstone)   covered → remove
2. Load(wctx, T, [id])                          err → reschedule(source)
   no entry for id, or Deleted                  → remove
   item.Revision < e.Revision                   → reschedule(source_behind)
3. Covered(wctx, T, C, [{id, item.Subjects}])   err → reschedule(tombstone)   covered → remove
4. validateItem, normalizeFields                err → reschedule(invalid_item, next = now+30m), WARN corpus/tenant/source/key name
5. chunk.Split(item.Text, spec.Ingestion.Chunking) (truncated → WARN once per key); buildPoints; none → remove
6. if e.Applied matches {Generation g.State.ActiveGeneration, ProfileHash g.ProfileHash, IndexedHash indexedHash(points), Chunks len(points)} (Revision ignored):
       guard; UpdatePayload(coll, points); guard; DeleteOtherRevisions(coll, T, id, item.Revision)
       Count(coll, Selector{T, [id]}) == len(points) → release(applied with Revision item.Revision), rep.PayloadOnly++, stop
       otherwise fall through to 7
7. EmbedWithProfile(wctx, {Caller "retrieval", OnBehalfOf T, Profile g.State.EmbeddingProfileID, Kind EmbedInputDocument, Inputs})
   in batches of 32 over the points' Text; any error → reschedule(budget if errors.Is ErrLLMBudgetExceeded, else embed);
   vector count ≠ inputs or a vector length ≠ g.State.Dimensions → reschedule(embed)
8. guard; Upsert(coll, points); guard; DeleteOtherRevisions(coll, T, id, item.Revision); release(applied); rep.Indexed++
remove:     guard; DeleteByFilter(coll, Selector{TenantID: T, SourceIDs: [id]}); release(nil); rep.Removed++
release(a): Release(tctx, …, a, now()); ErrLeaseLost → Requeue, rep.Requeued++
reschedule(code): attempts = e.Attempts+1; next = now()+backoff(attempts) (30m for invalid_item);
            any error whose errors.Is(context.DeadlineExceeded) uses code deadline; engine call errors use code engine;
            Reschedule(tctx, …); ErrLeaseLost → nothing more (the successor owns the key); rep.Rescheduled++
```

- [ ] **Step 1: Write the failing tests** (`newTestWorker(t)` wires the fakes: corpus `crm_contacts`, owner `crm` started, filters `stage keyword`, chunking `{fixed,4,1}`, profile `{ID "ip_a", embed, local, Dimensions 8}`, prefix `orkestra`, settings `{Lease 2m, Batch 32, Poll 5s}`, clock fixed at `2026-10-26T10:00:00Z`; `item(rev)` = `c1` with text `"a b c d e f g h i j"`, `VisibleTo ["tenant:*"]`, `Subjects ["crm:1"]`, `Fields {"stage":"lead"}`; `run()` = `RunOnce(ctx)` from Task 11 — until then call `prepareCorpus` + `Claim` + `process` through a test helper `processNext(w)`)

```go
func TestWorker_IndexesNewItem(t *testing.T)
//   enqueue c1 rev 1 → processNext
//   engine "orkestra_crm_contacts_g1": 3 points, IDs engine.PointID("t1","crm_contacts","c1",0..2), SourceRevision 1,
//     IngestionProfile == g.ProfileHash, Fields {"stage":"lead"}
//   embedder: 1 call, 3 inputs, Caller "retrieval", OnBehalfOf "t1", Profile "ip_a", Kind document,
//     ctx tenant "t1", kind "internal", ctx deadline == leaseUntil - 30s
//   outbox row: Dirty false, LeaseOwner "", Applied {Generation 1, Revision 1, Chunks 3, ProfileHash g.ProfileHash}
func TestWorker_DeletedItemRemovesAllPoints(t *testing.T)    // index, then source deletes c1, enqueue rev 2 → 0 points, Applied nil
func TestWorker_SourceBehindEnqueuedRevisionRetries(t *testing.T)
//   enqueue rev 3, source has rev 2 → no engine write; Dirty true, Attempts 1, LastError "source_behind", NextAttemptAt now+5s
func TestWorker_TombstonedSubjectWritesNothingAndDeletesPoints(t *testing.T)   // Review Focus 3
//   index rev 1 (3 points); erasure {tenant t1, subject "crm:1"}; enqueue rev 2 → embedder calls still 1, 0 points, row clean
func TestWorker_ItemTombstoneSkipsLoad(t *testing.T)          // item tombstone t1/crm_contacts/c1 → Load not called, 0 points
func TestWorker_SharingChangeUpdatesPayloadWithoutEmbedding(t *testing.T)     // Review Focus 4
//   index rev 1; source rev 2 with VisibleTo ["user:u1"], same text; enqueue rev 2
//   → embedder calls still 1; 3 points, SourceRevision 2, VisibleTo ["user:u1"]; Applied.Revision 2; report PayloadOnly 1
func TestWorker_PayloadPathFallsBackWhenPointsMissing(t *testing.T)            // Review Focus 4
//   index rev 1; wipe the fake collection; sharing change rev 2 → embedder calls 2; 3 points rev 2
func TestWorker_EmbedFailureReschedulesWithBackoff(t *testing.T)
//   at T: ErrLLMProviderUnavailable → Attempts 1, LastError "embed", NextAttemptAt T+5s
//   clock to T+5s, same error → Attempts 2, NextAttemptAt T+15s
//   ErrLLMBudgetExceeded → LastError "budget"; vectors of length 7 → LastError "embed"; no engine write in any case
func TestWorker_InvalidItemParksForMaxBackoff(t *testing.T)   // Fields {"undeclared":1} → "invalid_item", NextAttemptAt now+30m, no write
func TestBackoff(t *testing.T)                                 // 1→5s 2→10s 3→20s 9→21m20s 10→30m 50→30m
func TestWorker_EnqueueWhileHeldIsAppliedNextPass(t *testing.T)               // Review Focus 2
//   fakeEmbedder hook during the rev-1 call: source moves to rev 2 (new text "k l m"), outbox.Enqueue rev 2
//   after processNext: points SourceRevision 1, row Dirty true, Revision 2
//   processNext again → 1 point, SourceRevision 2, text "k l m", row clean
func TestWorker_StaleHolderResumingAfterSuccessor(t *testing.T)               // Review Focus 1
//   rev 1 text "a b c d e f g h i j" (3 chunks), rev 2 text "k l m" (1 chunk); workers w1 and w2 share every fake and the clock
//   subtest "paused before upsert": fakeEngine.beforeUpsert blocks w1 (after its guard passed) and signals
//     while w1 is blocked: source → rev 2, Enqueue rev 2, clock += lease+1s, w2 processNext (claims epoch 2, writes, releases)
//     unblock w1; wait for it
//     → points: ordinal 0 rev 2, ordinals 1 and 2 rev 1; row Dirty true (w1 re-queued); w1 report Requeued 1, no release by w1
//     w2 processNext → payload path: exactly 1 point, SourceRevision 2; embedder calls == 2 (w1 rev 1, w2 rev 2)
//   subtest "paused before delete": fakeEngine.beforeDeleteOthers blocks w1 after its second guard passed; same interleaving
//     → after w1 resumes: w2's rev-2 point deleted, ordinals 1 and 2 rev 1 remain; row Dirty true
//     w2 processNext → full path (Count mismatch): exactly 1 point, SourceRevision 2
```

- [ ] **Step 2: Run to verify they fail** — `cd backend && go test ./internal/core/retrieval/services/ -run 'TestWorker|TestBackoff' -race -v` → FAIL to compile.
- [ ] **Step 3: Implement `process`, `backoff`, `tenantCtx`, `OutboxStore` and the fakes** per the algorithm.
- [ ] **Step 4: Run to verify they pass** — same command → PASS (with `-race`).
- [ ] **Step 5: Commit**

```bash
git add backend/internal/core/retrieval/services
git commit -m "feat(retrieval): index one key under its lease with epoch fencing"
```

---

### Task 11: passes — fair claim, initial build, run loop

**Files:**
- Create: `backend/internal/core/retrieval/services/worker_pass.go`, test `worker_pass_test.go`
- Modify: `services/fakes_test.go` (`fakeProgress`, `fakeTenants`; `fakeSource.Enumerate` pages ids sorted ascending, cursor = last id)

**Interfaces:**
- Consumes: Tasks 5, 7 (`ValidateIntent`), 8, 10; `iface.RetrievalSource.Enumerate`; `iface.TenantIDLister`.
- Produces:
  ```go
  const maxTenantsPerPass, buildPageSize, buildPagesPerPass = 64, 200, 16
  func (w *Worker) RunOnce(ctx context.Context) (PassReport, error)
  func (w *Worker) Run(ctx context.Context)      // RunOnce at once, then every Settings().PollInterval (re-read each time) until ctx is done; errors WARN
  func (w *Worker) Problems() []string           // sorted copy of the last pass's problems; safe for concurrent use
  ```

`RunOnce`: (1) `s := ClampWorkerSettings(Settings())`; (2) `prepareCorpus` for every `Catalog.Corpora()` entry, collecting gates and problems; (3) index pass; (4) build pass; (5) store problems. A store error aborts the pass and is returned.

Index pass (fairness, not determined by signatures):

```
names := writable corpus names; if none → return
tenants := Outbox.DueTenants(ctx, now(), names, maxTenantsPerPass)
for claimed < s.Batch {
    progress := false
    for each tenant t not exhausted, in order {
        if claimed == s.Batch { break }
        e := Outbox.Claim(tenantCtx(ctx, t), now(), names, owner, s.Lease)
        if e == nil { mark t exhausted; continue }
        claimed++; progress = true
        process(ctx, gates[e.Corpus], e, s, &rep)       // processed at once, so its lease time is spent on it
    }
    if !progress { break }
}
```

Build pass: `tenants := Tenants.ListActiveTenantIDs(ctx, iface.TenantKindInternal)` once; for each gate (sorted by corpus) and tenant, while fewer than 16 pages were loaded this pass: `p := ClaimBuild(tctx, corpus, gate.State.ActiveGeneration, owner, now(), s.Lease)`; nil → next; `page := Source.Enumerate(ctx with timeout s.Lease/2, t, p.Cursor, 200)`; error → WARN, `Advance(cursor unchanged, 0, false)`; else keep the intents whose `ValidateIntent` passes (WARN with the count of the others), `Outbox.Enqueue(tctx, intents, now())`, `Advance(p.Cursor→page.Next, len(intents), page.Next == "")`; `rep.Enqueued += len(intents)`. The build only enqueues; the index pass does the writes, so tombstones, hashes and fencing apply to it unchanged, and re-running a page is harmless (`$max`).

- [ ] **Step 1: Write the failing tests**

```go
func TestWorker_ClaimIsFairAcrossTenants(t *testing.T)
//   t1 has 5 dirty keys (older), t2 has 1; Batch 2 → one RunOnce processes 1 key of t1 and t2's key
func TestWorker_KeepsIntentsWhileOwnerStopped(t *testing.T)                    // Review Focus 5
//   owner "crm" not started; enqueue c1 rev 1; RunOnce → Claimed 0, no state, no engine call, row Dirty true, Attempts 0
//   start "crm" → RunOnce → 3 points, row clean
func TestWorker_ProfileChangedKeepsIntents(t *testing.T)
//   bootstrap and index c1; change chunking to {fixed,2,0}; enqueue c2 → RunOnce: Claimed 0, c2 row Dirty true Attempts 0,
//   c1 points untouched; Problems() contains "corpus crm_contacts: ingestion profile changed; indexing paused until a new generation is built"
func TestWorker_LocalOnlyCorpusWithHostedEndpointWritesNothing(t *testing.T)
//   Boundary hosted → RunOnce: no EnsureCollection, no embed, row Dirty true; Problems() has the locality line
func TestBuild_EnqueuesEveryEnumeratedItemAndResumes(t *testing.T)
//   t1 source has 450 items (rev 1); internal tenants [t1]
//   RunOnce #1: progress Cursor = id #200, Enqueued 200; a new Worker (other owner) RunOnce #2: Enqueued 400;
//   RunOnce #3: Status done, 450 outbox rows with Revision 1; RunOnce #4: ClaimBuild returns nil, Enumerate not called
func TestBuild_RunsPerInternalTenantUnderItsContext(t *testing.T)
//   tenants [t1, t2] → Enumerate called with tenantID == ctx tenant, ctx kind "internal", for both
func TestBuild_SkipsInvalidEnumeratedIDs(t *testing.T)  // ids "", "a\x00b", "ok" → only "ok" enqueued; progress advances
func TestWorker_RunStopsOnCancel(t *testing.T)
//   PollInterval clamped to 1s: Run in a goroutine, wait for the first RunOnce (fake hook), cancel → Run returns within 2s
```

- [ ] **Step 2: Run to verify they fail** — `cd backend && go test ./internal/core/retrieval/services/ -run 'TestWorker_ClaimIsFair|TestWorker_Keeps|TestWorker_ProfileChanged|TestWorker_LocalOnly|TestBuild|TestWorker_RunStops' -race -v` → FAIL to compile.
- [ ] **Step 3: Implement `worker_pass.go`.** Switch Task 10's tests from `processNext` to `RunOnce` where they only need one pass (keep `processNext` for the stale scenario's controlled interleaving).
- [ ] **Step 4: Run to verify they pass** — `cd backend && go test ./internal/core/retrieval/services/ -race -v` → PASS.
- [ ] **Step 5: Commit**

```bash
git add backend/internal/core/retrieval/services
git commit -m "feat(retrieval): drain the outbox fairly and build generation 1 from Enumerate"
```

---

### Task 12: module wiring — config, Init, Start/Stop, HealthCheck

**Files:**
- Modify: `backend/internal/core/retrieval/module.go`, `module_test.go`

**Interfaces:**
- Consumes: everything above; `module.ServiceLLMGateway`, `module.ServiceTenantService`, `module.ServiceModuleStatus`; PR 3's engine (nil when `QDRANT_URL` is empty) and collection prefix; PR 3's registry (wrapped as `CorpusCatalog` if needed).
- Produces: `ConfigSchema` keys and the lifecycle below.

Config (PR 3 declares none: add `ConfigGroups()` and `ConfigSchema()`; group `{Key: "indexing", Label: "Indexing", Description: "Outbox worker: lease per key, keys per pass and pass interval."}`):

| Key | Type | Default | EnvVar | Description (verbatim) |
|---|---|---|---|---|
| `worker_lease` | `FieldDuration` | `2m` | `RETRIEVAL_WORKER_LEASE` | How long a worker owns one item while indexing it (30s–15m). Embedding and engine calls end a quarter of the lease before it expires. |
| `worker_batch` | `FieldInt`, Min 1, Max 256 | `32` | `RETRIEVAL_WORKER_BATCH` | Items indexed per pass, shared round-robin across organizations. |
| `worker_poll_interval` | `FieldDuration` | `5s` | `RETRIEVAL_WORKER_POLL_INTERVAL` | Pause between passes (1s–5m). |

Init additions (no network I/O, as D1 requires): build `repository.NewOutbox/NewCorpusStates/NewTenantProgress/NewErasures`, `NewTombstones`, `NewOutbox(...)` and `service.SetOutbox` **before** the service is registered; resolve `GetTyped[iface.LLMPinnedEmbedder]` and `GetTyped[iface.LLMInferenceProfiles]` on `ServiceLLMGateway`, `GetTyped[iface.TenantIDLister]` on `ServiceTenantService`, `GetTyped[module.ModuleStatusReader]` on `ServiceModuleStatus`. Engine configured and all four resolved → `m.worker = services.NewWorker(...)` with `Settings` reading the three keys through `deps.GetConfigDuration/GetConfigInt("retrieval", …)` on every call; otherwise `m.worker = nil` and one WARN naming exactly what is missing (`QDRANT_URL` unset is INFO, not WARN: D1's not-configured mode). `Enqueue` works in every case.

Start/Stop: copy the `llm` pattern (`backend/internal/core/llm/module.go:515-597`): `lifecycleMu`, `cancel`, `done`; `Start` returns nil when `m.worker == nil`; a second `Start` while running is a no-op; `Stop` cancels and waits for `done` or ctx. Bootstrap therefore happens on the worker's first pass, right after `Start`, and again on later passes for owners that start after `retrieval` (the registry starts modules in dependency order, so a fork owner starts after it).

HealthCheck (extend PR 3's; keep its checks first): engine nil → nil; `m.worker == nil` → `retrieval: indexing disabled: <missing>`; `engine.Health` with a 2s timeout fails → `retrieval: vector engine unreachable`; `worker.Problems()` non-empty → `"retrieval: " + strings.Join(problems, "; ")`; else nil.

- [ ] **Step 1: Write the failing tests** (PR 3's module test harness; inject a `fakeEngine` through PR 3's engine seam — if none exists, add an unexported `newEngine func(url, key string) engine.Engine` field defaulting to the Qdrant constructor)

```go
func TestModule_ConfigSchemaDeclaresIndexingKeys(t *testing.T)
//   worker_lease/worker_batch/worker_poll_interval present with the defaults, types, EnvVars and Group "indexing" above;
//   module.ValidateConfigDeclarations passes
func TestModule_InitTouchesNoEngine(t *testing.T)               // after Init with QDRANT_URL set: fake engine call log empty
func TestModule_RegisteredServiceIsARetrievalOutbox(t *testing.T)
//   GetTyped[iface.RetrievalOutbox](reg, module.ServiceRetrieval) ok; Enqueue under an internal tenant ctx reaches retrieval_outbox (Mongo-gated)
func TestModule_StartWithoutEngineRunsNoWorker(t *testing.T)     // QDRANT_URL "" → Start nil, worker nil, HealthCheck nil
func TestModule_InitWarnsWhenTheGatewayLacksPinnedEmbeddings(t *testing.T)
//   gateway fake without LLMPinnedEmbedder → Init nil; exactly one WARN containing "LLMPinnedEmbedder"; worker nil;
//   HealthCheck error contains "indexing disabled"
func TestModule_StartStopIsIdempotent(t *testing.T)              // as llm/module_test.go:738
func TestModule_HealthCheckReportsEngineAndCorpusProblems(t *testing.T)
//   fake engine Health error → "retrieval: vector engine unreachable"; then healthy engine + hosted endpoint on a local_only corpus
//   after one pass → error contains `local_only corpus uses hosted endpoint "embed"`
```

- [ ] **Step 2: Run to verify they fail** — `cd backend && MONGO_TEST_URI=$MONGO_TEST_URI go test ./internal/core/retrieval/ -run TestModule -race -v` → FAIL.
- [ ] **Step 3: Implement the wiring.**
- [ ] **Step 4: Run to verify they pass** — same command, then `cd backend && go test ./cmd/server/ -run TestCoreModulesLoadOrder` → PASS (order unchanged).
- [ ] **Step 5: Commit**

```bash
git add backend/internal/core/retrieval
git commit -m "feat(retrieval): run the index worker under the module lifecycle"
```

---

### Task 13: stale-worker and bootstrap against real MongoDB and Qdrant

**Files:**
- Create: `backend/internal/core/retrieval/services/worker_integration_test.go`

**Interfaces:**
- Consumes: real `repository` stores (Tasks 4–6), PR 3's Qdrant driver (`qdrant.New(qdrant.Config{…})`, as module Init builds it, with `QDRANT_TEST_URL` and `QDRANT_TEST_API_KEY`), the Task 8–11 fakes for source, embedder (8-dimensional deterministic vectors), profiles, modules and tenants.

`newIntegrationEnv(t)` skips with a message naming the missing variable unless `MONGO_TEST_URI`, `QDRANT_TEST_URL` and `QDRANT_TEST_API_KEY` are all set; creates an isolated database (prefix `retrieval_it_`) with the unique outbox key index and the progress index; uses collection prefix `it<unixnano>`; drops the database and `DropCollection`s the generation-1 collection in `t.Cleanup`. Two `Worker`s share the stores, the engine and a fake clock. Pausing is a decorator `pausingEngine{engine.Engine; beforeUpsert, beforeDeleteOthers func()}`.

- [ ] **Step 1: Write the failing tests**

```go
func TestIntegration_BootstrapIndexAndTombstone(t *testing.T)
//   RunOnce → collection "<prefix>_crm_contacts_g1" exists (Count == 0 succeeds); build enqueues c1; RunOnce → Count(t1,[c1]) == 3
//   Query{TenantID t1, Principals ["tenant:*"], Dense v(c1 chunk 0), Weights{Dense:1}, PerArm 10, Limit 10} → 3 candidates, SourceRevision 1
//   Erasures.Insert(tenant t1 item crm_contacts/c1); Requeue c1; RunOnce → Count == 0
func TestIntegration_StaleHolderResumingAfterSuccessor(t *testing.T)
//   the two subtests of Task 10 with the same interleaving and assertions, observed through engine.Count and engine.Query
//   (after the final pass: Count(t1,[c1]) == 1 and the only candidate has SourceRevision 2)
```

- [ ] **Step 2: Run to verify they fail** — `cd backend && MONGO_TEST_URI=$MONGO_TEST_URI QDRANT_TEST_URL=$QDRANT_TEST_URL QDRANT_TEST_API_KEY=$QDRANT_TEST_API_KEY go test ./internal/core/retrieval/services/ -run TestIntegration -race -v` → FAIL to compile.
- [ ] **Step 3: Implement the env helper and the decorator.**
- [ ] **Step 4: Run to verify they pass** — same command → PASS. Then the same command without the variables → SKIP (and say so in the PR: a SKIP proves nothing).
- [ ] **Step 5: Commit**

```bash
git add backend/internal/core/retrieval/services/worker_integration_test.go
git commit -m "test(retrieval): pin the stale-worker scenario against MongoDB and Qdrant"
```

---

### Task 14: documentation and the CI gate

**Files:**
- Modify: `backend/internal/core/retrieval/AGENTS.md`
- Modify: `docs/site/modules/core/retrieval.mdx` (if PR 3 did not create it: create it with the frontmatter shape of `llm.mdx`, `sidebar_position: 9`, set `compliance.mdx` to 10, add `retrieval` to `docs/site/modules/core/_category_.json`'s description)

- [ ] **Step 1: Write `AGENTS.md` § Indexing** — for owners: call `Enqueue` with the `mongo.SessionContext` of the source write; revisions monotonic per item; `Load` reports absent ids as deleted and answers only for the tenant passed; `Enumerate` pages by cursor. Then: collections and their scope (`retrieval_outbox`, `retrieval_tenant_progress` tenant-scoped; `retrieval_corpus_state` installation-wide; `retrieval_erasures` global + tenant; the system sweeps and their annotations); the per-key protocol (claim clears `dirty` and increments `epoch`; guard before every write; release fenced; lost lease → re-queue; write deadline `leaseUntil − lease/4`; `IndexingFencingWindow` for PR 6); the processing order of Task 10; the tokenization approximation of Task 3 verbatim; the hash definition and why the endpoint name is excluded; the gate table of Task 8 including "`profile_changed`: intents are kept, nothing is claimed, the active generation keeps serving, delivery step 2 builds and drains"; owner stopped → intents kept; config keys; HealthCheck strings; the `Engine.UpdatePayload` addition; the closed `lastError` codes.
- [ ] **Step 2: Write the site page sections** `## Indexing` (how an owner indexes: declare, enqueue in the transaction, what the worker does, chunking `none`/`fixed` with the token approximation, provenance offsets in runes), `## Configuration` (the three keys with env vars and bounds), `## Health` (the messages an operator sees and what to do: locality → fix the endpoint boundary or the corpus locality; profile changed → revert the declaration until delivery step 2).
- [ ] **Step 3: Render the site locally** with the recipe in `docs/site/README.md`. Expected: the page builds without broken links.
- [ ] **Step 4: Run the CI gate** — `make ci-backend` from the repository root, with `MONGO_TEST_URI`, `QDRANT_TEST_URL`, `QDRANT_TEST_API_KEY` exported. Expected: lint, tenantscope (no new baseline entries), policycoverage, piiscan, logscope, vuln, tests, build and openapi-check all pass; `backend/openapi/enterprise.json` unchanged (no route in this PR).
- [ ] **Step 5: Commit**

```bash
git add backend/internal/core/retrieval/AGENTS.md docs/site/modules/core
git commit -m "docs(retrieval): document indexing, chunking and the worker protocol"
```
