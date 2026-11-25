# Retrieval PR 6 — Erasure finalization and data-subject rights Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** make every retrieval erasure final: a sweep deletes tombstoned points in every generation twice around a fencing window and verifies them by count, the module answers GDPR export/erasure as PII producer `retrieval` (pending until finalized), owners erase non-user subjects with `DeleteBySubject`, a hard tenant purge reaches the engine through `iface.TenantPurgeHooks`, and erasure events reach the compliance audit sink.

**Architecture:** `retrieval_erasures` (PR 4) gains finalization fields; a `Finalizer` in `retrieval/services` claims due tombstones across tenants with a compare-and-set on `nextAttemptAt`, runs `DeleteByFilter` + `Count` over every live collection, moves `pending → fencing → done`, and retries with backoff. Three writers create tombstones through one idempotent repository and then try one finalization step inline: the `PIIProducer` (global `user:<uuid>`), `ErasureService.DeleteBySubject` (tenant `<owner>:<id>`) and `ErasureService.PurgeTenant` (tenant), the last registered on tenant's new `TenantPurgeHooks`. Reads never wait for any of this: PR 4's `Tombstones.Covered` vetoes covered points from the moment the row exists, whatever its status.

**Tech Stack:** Go 1.26.10, MongoDB 8.0 (`mongo-driver v1.17.7`, replica set, pipeline updates), the `engine.Engine` contract of PR 3, an in-memory `engine.Engine` for tests.

**Spec:** `docs/adr/0023-core-retrieval-module.md` (D1 table, D7, D11, D12, D13) and `docs/superpowers/plans/2026-10-26-retrieval-00-roadmap.md` (row 6, "Shared contract"). Executors read both.

## Global Constraints

- Everything in the roadmap's **Global constraints** section applies to every task (trailers `Prop: upstream` + `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`, no method on an existing `iface` interface, no cross-module `services/`/`repository/` import outside `cmd/server` and the existing compliance exceptions, `tenantrepo` or an annotation on every Mongo call, Mongo tests gated by `MONGO_TEST_URI`).
- Start only after PRs 2, 4 and 5 are merged into `dev`. Branch `feat/retrieval-06-erasure-dsr` from `dev`, in its own worktree.
- Before Task 1, open the merged code and resolve the names this plan writes in angle brackets. They come from PRs 4 and 5, whose plans did not exist when this one was written; use their real names everywhere the placeholder appears. PR 2's names (`ErasureRequestService.Execute` returning `*ErasureOutcome`, `NewErasureRequestService(repo, dsr, sink, logger)`, `RetryDue`, `models.ErasureRequestIncomplete`) are taken from `2026-10-26-retrieval-02-compliance-erasure-incomplete.md`; if the merged PR 2 differs, follow the merged code.

  | Placeholder | What it is | Where |
  |---|---|---|
  | `<ErasuresCollection>` | PR 4's constant for `"retrieval_erasures"` | `retrieval/models/` |
  | `<erasure model file>` | the file declaring PR 4's `type Erasure` | `retrieval/models/` |
  | `<Tombstones constructor>` | how PR 4's tests build `*services.Tombstones` over a Mongo DB | `retrieval/services/tombstones*_test.go` |
  | `<CorpusStateRepo>` | PR 4's repository over `retrieval_corpus_state` | `retrieval/repository/` |
  | `<SearchLog>`, `<SearchLogCollection>` | PR 5's search-log document type and collection constant | `retrieval/models/` |
  | `<search fixture>` | how PR 5's search tests build a `*services.Service` that answers `Search` with a given `engine.Engine` and `*Tombstones` | `retrieval/services/search*_test.go` |

- Search-log fields this PR reads, as ADR D12 lists them: tenant (`tenantId`), searcher (`userUuid`), corpus, query text, validated filter, returned source ids, returned subjects (`subjects`, values like `user:<uuid>`), creation time. If PR 5's bson names differ, use PR 5's.
- Fencing window (ADR D12 "maximum lease plus maximum write duration"), computed with `fence = clamp(lease + writeTimeout + 30s, 1m, 1h)` where production passes `lease = services.MaxWorkerLease` (PR 4, 15m: the upper clamp of `worker_lease`, so a lease lowered live never shortens the fence for a worker that claimed under the old value) and `writeTimeout = 30s` (PR 4 ends every write a quarter of the lease before it expires, so a write never outlives its lease; the 30 s is margin). PR 4 has no `engine_write_timeout` key and this PR adds none. Production fence: 16 m.
- Backoff after a failed pass: `min(15s · 2^(attempts−1), 15m)`. Sweep interval 15 s, batch 50 claims, claim hold 2 min, inline attempt bounded to 20 s.
- **Tombstones are never deleted in step 1**, whatever their kind or status (decision for ADR D12 "kept for as long as their source could still be re-indexed"): user tombstones cover present and future corpora and tenants; subject and tenant tombstones cover owner rows the module cannot see disappear (a purge crypto-shreds keys, it does not delete owner rows, and `Enumerate` in a rebuild could reach them); item tombstones stay because `RetrievalSource.Load` reports soft-deleted, restorable and permanently removed items alike as `Deleted` (D7), so the module can never prove an item is beyond re-indexing. A `done` tombstone keeps vetoing reads and writes.
- Data-subject paths carry `//tenantscope:allow dsr: <reason>`, sweeps `//tenantscope:allow system: <reason>`. None of them calls `tenantrepo.RequireInternalTenant` (ADR D13 exception).

## Review Focus

1. **An installation without the retrieval overlay** (engine not configured, no corpus collection ever created): a DSR erasure request completes in the same `Execute` call, exactly as before this PR. Pinned in Task 3 (`TestFinalizer_NoEngineNoCollections_DoneAtOnce`) and Task 8 (`TestRetrievalErasure_NoOverlay_RequestCompletesAtOnce`).
2. **The overlay removed after it was used** (engine nil, corpus state still lists collections): the tombstone stays unfinished and `Pending` stays true; it is never reported done on faith. Pinned in Task 3 (`TestFinalizer_NoEngineWithCollections_StaysPending`).
3. **A tombstone PR 4 wrote without finalization fields** (no `status`, no `nextAttemptAt`, for example an item tombstone): the sweep claims it as `pending`. Pinned in Task 2 (`TestErasureFinalization_ClaimDue_RowWithoutStatus`).
4. **Two replicas sweeping the same tombstone**: one transition wins, the loser gets `ErrErasureClaimLost`, `attempts` is not counted twice. Pinned in Task 2 (`TestErasureFinalization_StaleClaimCannotTransition`).
5. **A finalized (`done`) tombstone**: still covers its points for the read veto and the worker, since `Covered` must never filter on status. Pinned in Task 2 (`TestErasureFinalization_DoneTombstoneStillCovers`).

---

## File map

- Create `backend/pkg/sdk/iface/tenant_purge.go`; modify `backend/internal/core/tenant/services/service.go` (Task 1)
- Modify `<erasure model file>`; create `backend/internal/core/retrieval/repository/erasure_finalization.go` (Task 2)
- Create `backend/internal/core/retrieval/engine/enginefake/enginefake.go`, `services/audit.go`, `services/finalizer.go` (Task 3)
- Create `backend/internal/core/retrieval/repository/search_log_dsr.go`, `services/pii_producer.go` (Task 4)
- Create `backend/internal/core/retrieval/services/erasure.go`; modify the file holding PR 3's `func (s *Service) DeleteBySubject` stub (Task 5)
- Create `backend/internal/core/retrieval/repository/corpus_state_collections.go`; modify `backend/internal/core/retrieval/module.go` (Task 6)
- Modify `backend/internal/core/compliance/module.go`; create `backend/internal/core/compliance/retrieval_wiring_test.go` (Task 7)
- Create `backend/cmd/server/retrieval_erasure_e2e_test.go` (Task 8)
- Docs: tenant (Task 1), compliance (Task 7), retrieval and SDK (Task 9)

---

### Task 1: `iface.TenantPurgeHooks` on the tenant service

**Files:**
- Create: `backend/pkg/sdk/iface/tenant_purge.go`
- Modify: `backend/internal/core/tenant/services/service.go` (struct L25-31, hooks L208-224, `PurgeTenant` L1121-1190, `runPostDeleteHooks` L1372)
- Test: `backend/internal/core/tenant/services/purge_hooks_integration_test.go`, `backend/internal/core/tenant/services/purge_hooks_test.go`
- Docs: `backend/internal/core/tenant/AGENTS.md` (cascade bullet L336), `docs/site/modules/core/tenant.mdx` (GDPR section L95), `docs/site/sdk/shared-iface.mdx` (table L14-30), `backend/pkg/sdk/AGENTS.md` (package map, `iface/` row: narrow sub-interfaces list)

**Interfaces:**
- Produces (verbatim from the roadmap):
  ```go
  type TenantPurgeHooks interface {
  	RegisterTenantPurgeHook(fn func(ctx context.Context, tenantUUID string) error)
  }
  ```
  `func (s *Service) RegisterTenantPurgeHook(fn func(ctx context.Context, tenantUUID string) error)` on tenant's `*services.Service`, resolved with `module.GetTyped[iface.TenantPurgeHooks](reg, module.ServiceTenantService)`.

- [ ] **Step 1: Write the failing tests**

`purge_hooks_test.go` (no Mongo):

```go
func TestService_ImplementsTenantPurgeHooks(t *testing.T) {
	var _ iface.TenantPurgeHooks = (*Service)(nil)
}

func TestRegisterTenantPurgeHook_NilIgnored(t *testing.T) {
	svc := New(nil)
	svc.RegisterTenantPurgeHook(nil)
	if len(svc.purgeHooks) != 0 {
		t.Fatalf("purgeHooks = %d, want 0", len(svc.purgeHooks))
	}
}
```

`purge_hooks_integration_test.go` (reuse `newDefaultsTestDB`, `seedDefaultsTenant`, `recordingAuditSink` from `default_tenant_integration_test.go`):

```go
func TestPurgeTenant_RunsPurgeHooksWithTenantUUID(t *testing.T) {
	// seed tenant tn; register a hook appending its tenantUUID to got
	// PurgeTenant(ctx, tn.UUID) == nil
	// got == []string{tn.UUID}
}

func TestDeleteTenant_DoesNotRunPurgeHooks(t *testing.T) {
	// DeleteTenant(ctx, tn.UUID) == nil; got is empty (soft delete is not Hard)
}

func TestPurgeTenant_PurgeHookErrorAuditedAndLaterHooksRun(t *testing.T) {
	// hook 1 returns errors.New("boom"); hook 2 records
	// PurgeTenant == nil; hook 2 called once
	// ev, ok := sink.lastByAction("tenant.cascade.hook_failed"); ok
	// ev.Outcome == "failure"; ev.Metadata["hook"] == "purge"; ev.ResourceID == tn.UUID
}

func TestPurgeTenant_DefaultTargetRunsNoPurgeHook(t *testing.T) {
	// AssignDefaultTenant(ctx, tn.UUID, "", models.DefaultUpdateSourceSetup)
	// errors.Is(PurgeTenant(ctx, tn.UUID), ErrDefaultReassignmentRequired); got is empty
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd backend && go test ./internal/core/tenant/services/ -run 'TenantPurgeHook|PurgeHook' -v`
Expected: FAIL to compile (`iface.TenantPurgeHooks`, `RegisterTenantPurgeHook`, `purgeHooks` undefined).

- [ ] **Step 3: Implement**

`iface/tenant_purge.go`: the interface above with the roadmap's doc comment, plus "the hook receives the purged tenant's UUID even when its row could not be read; it runs after the post-delete hooks; a returned error is audited as `tenant.cascade.hook_failed` and never aborts the purge or later hooks".

`service.go`: field `purgeHooks []func(context.Context, string) error`; `RegisterTenantPurgeHook` ignores nil; `var _ iface.TenantPurgeHooks = (*Service)(nil)`; `runPurgeHooks(ctx, tenantUUID string)` called in `PurgeTenant` right after `s.runPostDeleteHooks(ctx, cascadeCtx)` with the method's `tenantUUID` argument (not `cascadeCtx.TenantUUID`, which is empty when the row lookup failed). Failure audit: same shape as `runPostDeleteHooks` plus `Metadata["hook"] = "purge"`. `DeleteTenant` does not call it.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd backend && MONGO_TEST_URI=$MONGO_TEST_URI go test ./internal/core/tenant/... -v -run 'TenantPurgeHook|PurgeHook|PurgeTenant'`
Expected: PASS (the four integration tests SKIP without `MONGO_TEST_URI`; run them with it).

- [ ] **Step 5: Update docs**

Tenant AGENTS cascade bullet and `tenant.mdx`: hard purge also runs `iface.TenantPurgeHooks` hooks (tenant UUID only, after the post-delete hooks, best-effort, `hook: purge` in the failure audit); `retrieval` uses it to erase the tenant's index. `shared-iface.mdx`: row `TenantPurgeHooks | tenant | retrieval (tenant offboarding)`. SDK AGENTS `iface/` row: add `TenantPurgeHooks` to the narrow sub-interfaces resolved by `module.GetTyped` against `ServiceTenantService`.

- [ ] **Step 6: Commit**

```bash
git add backend/pkg/sdk/iface/tenant_purge.go backend/internal/core/tenant docs/site/modules/core/tenant.mdx docs/site/sdk/shared-iface.mdx backend/pkg/sdk/AGENTS.md
git commit -m "feat(tenant): add TenantPurgeHooks for hard-purge cascades

Prop: upstream
Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: Finalization fields and the erasure repository

**Files:**
- Modify: `<erasure model file>` (PR 4's `type Erasure`)
- Modify: `backend/internal/core/retrieval/module.go` (`Collections()` entry for `retrieval_erasures`)
- Create: `backend/internal/core/retrieval/repository/erasure_finalization.go`
- Test: `backend/internal/core/retrieval/repository/erasure_finalization_test.go` (Mongo-gated; use the package's `newTestDB`, added by PR 3/4 after `llm/repository/testdb_test.go`; add it there if absent), `backend/internal/core/retrieval/services/tombstones_done_test.go` (Mongo-gated)

**Interfaces:**
- Consumes: PR 4's `models.Erasure`, `<ErasuresCollection>`, `services.Tombstones.Covered` (roadmap signature).
- Produces:
  ```go
  // models (added to Erasure, roadmap's PR 6 list)
  Status        string     `bson:"status,omitempty"` // "" is read as pending
  FirstPassAt   *time.Time `bson:"firstPassAt,omitempty"`
  FenceUntil    *time.Time `bson:"fenceUntil,omitempty"`
  DoneAt        *time.Time `bson:"doneAt,omitempty"`
  Attempts      int        `bson:"attempts"`
  NextAttemptAt *time.Time `bson:"nextAttemptAt,omitempty"`
  const ErasureStatusPending, ErasureStatusFencing, ErasureStatusDone = "pending", "fencing", "done"
  const ErasureScopeGlobal, ErasureScopeTenant = "global", "tenant"            // only if PR 4 has no constants
  const ErasureKindUser, ErasureKindSubject, ErasureKindItem, ErasureKindTenant // likewise
  func (e *Erasure) EffectiveStatus() string // Status, or pending when ""

  // repository
  var ErrErasureClaimLost = errors.New("retrieval: erasure claim lost")
  type ErasureFinalization struct{ /* coll */ }
  func NewErasureFinalization(db *mongo.Database) *ErasureFinalization
  func (r *ErasureFinalization) EnsureUser(ctx context.Context, userUUID string, now time.Time) (*models.Erasure, bool, error)
  func (r *ErasureFinalization) EnsureSubject(ctx context.Context, tenantID, subject string, now time.Time) (*models.Erasure, bool, error)
  func (r *ErasureFinalization) EnsureTenant(ctx context.Context, tenantID string, now time.Time) (*models.Erasure, bool, error)
  func (r *ErasureFinalization) ListUser(ctx context.Context, userUUID string) ([]models.Erasure, error)
  func (r *ErasureFinalization) Get(ctx context.Context, uuid string) (*models.Erasure, error)
  func (r *ErasureFinalization) ClaimDue(ctx context.Context, now time.Time, hold time.Duration) (*models.Erasure, error) // nil, nil when none
  func (r *ErasureFinalization) ClaimOne(ctx context.Context, uuid string, now time.Time, hold time.Duration) (*models.Erasure, error) // nil, nil when not due
  func (r *ErasureFinalization) MarkFencing(ctx context.Context, uuid string, claim, firstPassAt, fenceUntil time.Time) error
  func (r *ErasureFinalization) MarkDone(ctx context.Context, uuid string, claim, at time.Time) error
  func (r *ErasureFinalization) MarkRetry(ctx context.Context, uuid string, claim, next time.Time) error
  ```
  `Ensure*` return `created == true` only for the call that inserted the row. `claim` is the `NextAttemptAt` the claim wrote; `Mark*` return `ErrErasureClaimLost` when the row no longer carries it.

- [ ] **Step 1: Write the failing tests**

```go
func TestErasureFinalization_EnsureUserIdempotent(t *testing.T) {
	// e1, c1 := EnsureUser(ctx, "u1", t0); e2, c2 := EnsureUser(ctx, "u1", t0.Add(time.Hour))
	// c1 && !c2; e1.UUID == e2.UUID; e1.Scope == "global"; e1.TenantID == ""; e1.Kind == "user"
	// e1.Subject == "user:u1"; e1.EffectiveStatus() == "pending"; *e1.NextAttemptAt == t0 (ms)
	// count of documents with subject "user:u1" == 1
}

func TestErasureFinalization_EnsureSubjectAndTenantKeyedPerTenant(t *testing.T) {
	// EnsureSubject(ctx, "tA", "crm:42", t0) and (ctx, "tB", "crm:42", t0) → two rows, both created
	// EnsureTenant(ctx, "tA", t0) twice → one row, Kind "tenant", Scope "tenant", TenantID "tA"
}

func TestErasureFinalization_ClaimDue_RowWithoutStatus(t *testing.T) {
	// insert a PR-4-shaped document directly: {uuid, scope:"tenant", tenantId:"tA", kind:"item",
	//   corpus:"people", sourceId:"s1", createdAt:t0} (no status, attempts, nextAttemptAt)
	// e := ClaimDue(ctx, t0, 2*time.Minute); e != nil; e.EffectiveStatus() == "pending"; e.Attempts == 0
	// *e.NextAttemptAt == t0.Add(2*time.Minute) truncated to ms
	// ClaimDue(ctx, t0, 2*time.Minute) == nil (held)
}

func TestErasureFinalization_ClaimSkipsNotDueAndDone(t *testing.T) {
	// fencing row with nextAttemptAt = t0+5m → ClaimDue(t0) == nil; ClaimDue(t0+5m) != nil
	// done row → never claimed; ClaimOne(done uuid) == nil
}

func TestErasureFinalization_Transitions(t *testing.T) {
	// claim → MarkFencing(uuid, claim, t1, t1+3m): status fencing, FirstPassAt t1, FenceUntil t1+3m, NextAttemptAt t1+3m
	// claim at t1+3m → MarkRetry(uuid, claim, t1+4m): Attempts 1, status still fencing
	// claim at t1+4m → MarkDone(uuid, claim, t2): status done, DoneAt t2, NextAttemptAt nil, FirstPassAt kept
}

func TestErasureFinalization_MarkDoneFromPendingFillsPasses(t *testing.T) {
	// claim pending row → MarkDone(uuid, claim, t1): FirstPassAt == FenceUntil == DoneAt == t1
}

func TestErasureFinalization_StaleClaimCannotTransition(t *testing.T) {
	// a := ClaimDue(t0, hold); after hold expires b := ClaimDue(t0+hold, hold)
	// errors.Is(MarkRetry(uuid, *a.NextAttemptAt, t9), ErrErasureClaimLost)
	// MarkRetry(uuid, *b.NextAttemptAt, t9) == nil; Get(uuid).Attempts == 1
}

func TestErasureFinalization_DoneTombstoneStillCovers(t *testing.T) {
	// EnsureUser("u1") → claim → MarkDone
	// tombs := <Tombstones constructor>(db)
	// got := tombs.Covered(ctx, "tA", "people", []services.Probe{{SourceID: "s1", Subjects: []string{"user:u1"}}})
	// got["s1"] == true
}
```

`TestErasureFinalization_DoneTombstoneStillCovers` lives in `backend/internal/core/retrieval/services/tombstones_done_test.go` (package `services`, Mongo-gated), because it needs both packages.

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd backend && MONGO_TEST_URI=$MONGO_TEST_URI go test ./internal/core/retrieval/repository/ -run ErasureFinalization -v`
Expected: FAIL to compile (`NewErasureFinalization` undefined).

- [ ] **Step 3: Implement**

- All times stored `UTC().Truncate(time.Millisecond)` so a claim value read back compares equal.
- `Ensure*`: `FindOneAndUpdate(naturalKey, {$setOnInsert: full row with uuid = uuid.NewString(), status pending, attempts 0, nextAttemptAt now, createdAt now}, upsert, ReturnDocument After)`; `created` = returned UUID equals the generated one. Natural keys: user `{scope: global, kind: user, subject: "user:"+uuid}`; subject `{scope: tenant, tenantId, kind: subject, subject}`; tenant `{scope: tenant, tenantId, kind: tenant}`. No unique index (PR 4 inserts item rows freely); a duplicate from a concurrent upsert race is harmless because both rows cover and both get finalized, and `ListUser` returns all of them.
- `ClaimDue`: `FindOneAndUpdate` with filter `{$or: [{status: {$exists: false}}, {status: {$in: [pending, fencing]}, nextAttemptAt: {$lte: now}}, {status: pending, nextAttemptAt: {$exists: false}}]}`, sort `{nextAttemptAt: 1}`, and a pipeline update `[{$set: {status: {$ifNull: ["$status", "pending"]}, attempts: {$ifNull: ["$attempts", 0]}, nextAttemptAt: now+hold}}]`, ReturnDocument After. `ClaimOne` is the same with `uuid` ANDed.
- `MarkFencing`/`MarkDone`/`MarkRetry`: `UpdateOne({uuid, nextAttemptAt: claim}, …)`, `MatchedCount == 0` → `ErrErasureClaimLost`. `MarkDone` is a pipeline update: `status: done, doneAt: at, firstPassAt: {$ifNull: ["$firstPassAt", at]}, fenceUntil: {$ifNull: ["$fenceUntil", at]}` and `$unset nextAttemptAt`. `MarkRetry` sets `nextAttemptAt: next` and `attempts: {$add: [{$ifNull: ["$attempts", 0]}, 1]}`.
- Annotations: `EnsureUser`, `ListUser` → `//tenantscope:allow dsr: global user tombstone of a data-subject erasure (ADR-0023 D12)`; `EnsureSubject` → `dsr: owner-module subject erasure for an explicit tenant`; `EnsureTenant` → `system: tenant purge tombstone for an explicit tenant`; `Get`, `Claim*`, `Mark*` → `system: erasure finalization sweep across tenants and global tombstones`.
- `Collections()`: add index `OrderedKeys {status 1, nextAttemptAt 1}` and, unless PR 4 already declares the same key pattern, `OrderedKeys {kind 1, subject 1}`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd backend && MONGO_TEST_URI=$MONGO_TEST_URI go test ./internal/core/retrieval/... -run 'ErasureFinalization|DoneTombstone' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/core/retrieval
git commit -m "feat(retrieval): add erasure finalization state and claims

Prop: upstream
Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 3: The finalization sweep

**Files:**
- Create: `backend/internal/core/retrieval/engine/enginefake/enginefake.go` (skip if the branch already has an exported in-memory `engine.Engine` with seeding and delete/count failure injection; then use that one wherever this plan says `enginefake`)
- Create: `backend/internal/core/retrieval/services/audit.go`, `backend/internal/core/retrieval/services/finalizer.go`
- Test: `backend/internal/core/retrieval/engine/enginefake/enginefake_test.go`, `backend/internal/core/retrieval/services/finalizer_test.go`

**Interfaces:**
- Consumes: Task 2's repository method set (as the `ErasureStore` interface), `engine.Engine`, `engine.Selector`, `engine.ErrNotFound`, `engine.ErrUnavailable`.
- Produces:
  ```go
  // enginefake
  type Engine struct{ /* collections of engine.Point, failure switches */ }
  func New() *Engine
  func (e *Engine) Seed(collection string, pts ...engine.Point) // creates the collection, stores by ID unconditionally
  func (e *Engine) FailDeletes(err error)                       // nil clears
  func (e *Engine) FailCounts(err error)
  func (e *Engine) IgnoreDeletes(on bool)                       // DeleteByFilter returns nil and deletes nothing
  func (e *Engine) Points(collection string) []engine.Point
  // implements engine.Engine; DeleteByFilter/Count on a missing collection → engine.ErrNotFound;
  // an empty Selector → error; Query keeps TenantID and Principals∩VisibleTo, ranks by insertion order

  // services/audit.go
  type AuditRelay struct{ /* mu, sink */ }
  func NewAuditRelay() *AuditRelay
  func (a *AuditRelay) SetAuditSink(s iface.AuditSink) // nil ignored
  func (a *AuditRelay) Emit(ctx context.Context, action, resourceType, resourceID string, meta map[string]any) // actor from ctx (admin actions, PR 7)
  func (a *AuditRelay) EmitSystem(ctx context.Context, tenantID, action, resourceType, resourceID string, meta map[string]any) // ActorType "system"
  const AuditErasureRecorded = "retrieval.erasure.recorded"
  const AuditErasureFinalized = "retrieval.erasure.finalized"

  // services/finalizer.go
  type ErasureStore interface { /* the ten methods of Task 2, same signatures */ }
  var _ ErasureStore = (*repository.ErasureFinalization)(nil)
  type CollectionLister interface {
  	// LiveCollections maps every corpus in corpus state to the collections of its
  	// active, building and retained generations; a corpus with none is absent.
  	LiveCollections(ctx context.Context) (map[string][]string, error)
  }
  type FinalizerConfig struct {
  	Lease, WriteTimeout time.Duration
  	Interval            time.Duration // 15s
  	Batch               int           // 50
  	Hold                time.Duration // 2m
  }
  func FencingWindow(lease, writeTimeout time.Duration) time.Duration
  func Backoff(attempts int) time.Duration
  func SelectorFor(e models.Erasure) (engine.Selector, error)
  type Finalizer struct{ /* … */ }
  func NewFinalizer(store ErasureStore, cols CollectionLister, eng engine.Engine, cfg func() FinalizerConfig,
  	audit *AuditRelay, now func() time.Time, logger *slog.Logger) *Finalizer // eng nil = engine not configured
  func (f *Finalizer) RunOnce(ctx context.Context) (int, error)
  func (f *Finalizer) Attempt(ctx context.Context, uuid string) (*models.Erasure, error) // one step if due; returns the row after it
  func (f *Finalizer) Run(ctx context.Context) // ticks every cfg().Interval until ctx is done
  ```

- [ ] **Step 1: Write the failing tests**

`enginefake_test.go`: `TestEngineFake_DeleteAndCountBySelector` (seed 3 points over two tenants and subjects; `DeleteByFilter(Selector{Subject: "user:u1"})` removes exactly the points carrying it in every tenant; `Count` with `{TenantID: "tA"}` returns the rest; missing collection → `engine.ErrNotFound`; `Selector{}` → error); `TestEngineFake_UpsertKeepsHigherRevision`.

`finalizer_test.go` uses an in-memory `memStore` implementing `ErasureStore` (claim semantics as in Task 2), a `staticLister map[string][]string`, `enginefake`, a `recordingSink`, and a settable clock:

```go
func TestFencingWindow(t *testing.T) {
	cases := []struct{ lease, write, want time.Duration }{
		{2 * time.Minute, 30 * time.Second, 3 * time.Minute},
		{5 * time.Second, 5 * time.Second, time.Minute},          // floor
		{2 * time.Hour, time.Minute, time.Hour},                  // ceiling
		{0, -1, 3 * time.Minute},                                 // fallbacks 2m + 30s
	}
}

func TestBackoff(t *testing.T) {
	// Backoff(1)==15s, Backoff(2)==30s, Backoff(4)==2m, Backoff(10)==15m, Backoff(0)==15s
}

func TestSelectorFor(t *testing.T) {
	// user:    {Subject: "user:u1"} (no TenantID)
	// subject: {TenantID: "tA", Subject: "crm:42"}
	// item:    {TenantID: "tA", SourceIDs: []string{"s1"}}
	// tenant:  {TenantID: "tA"}
	// kind "x" → error; subject kind with empty Subject → error
}

func TestFinalizer_FirstPassThenFenceThenDone(t *testing.T) {
	// cols {"people": {"c_people_g1", "c_people_g2"}}; both seeded with a point carrying user:u1 and one without
	// EnsureUser(u1) at t0; RunOnce at t0 → processed 1
	// status fencing; FirstPassAt t0; FenceUntil t0+3m; no user:u1 point left in either collection
	// RunOnce at t0+2m → processed 0
	// RunOnce at t0+3m → status done, DoneAt t0+3m; the point without user:u1 is still there
	// sink has exactly one AuditErasureFinalized event, ResourceType "retrieval_erasure", ResourceID == uuid,
	//   ActorType "system", Metadata["kind"] == "user", Metadata["collections"] == 2
}

func TestFinalizer_StaleWriteDuringFenceRemovedBySecondPass(t *testing.T) {
	// as above to fencing; at t0+1m Seed("c_people_g1", point with user:u1)  // a stale writer
	// RunOnce at t0+3m → done; Count(c_people_g1, {Subject: user:u1}) == 0
}

func TestFinalizer_SecondPassRereadsCollections(t *testing.T) {
	// first pass with cols {"people": {g1}}; before the fence ends lister returns {g1, g2} and g2 holds a user:u1 point
	// second pass deletes it; status done
}

func TestFinalizer_PassOutcomes(t *testing.T) {
	// table, each from a fresh pending user tombstone at t0:
	//  - DeleteByFilter → engine.ErrUnavailable: status pending, Attempts 1, NextAttemptAt t0+15s, no audit
	//  - Count → engine.ErrUnavailable: same
	//  - IgnoreDeletes(true), so Count > 0 after the delete: pending, Attempts 1
	//  - collection listed but missing in the engine (ErrNotFound): counts as confirmed → fencing
	//  - lister error: pending, Attempts 1
}

func TestFinalizer_ItemTombstoneOnlyTouchesItsCorpus(t *testing.T) {
	// cols {"people": {p_g1}, "notes": {n_g1}}; both hold tenant tA source s1
	// item tombstone (tA, people, s1) → after the first pass p_g1 has no s1 point, n_g1 still has it
}

func TestFinalizer_NoEngineNoCollections_DoneAtOnce(t *testing.T) {
	// eng nil, lister returns empty map; EnsureUser at t0; RunOnce at t0 → status done, DoneAt t0, no fencing wait
}

func TestFinalizer_NoEngineWithCollections_StaysPending(t *testing.T) {
	// eng nil, lister {"people": {"c_people_g1"}}; RunOnce at t0 → status pending, Attempts 1
	// RunOnce at t0+1h (still nil engine) → never done
}

func TestFinalizer_AttemptRunsOneStepOnlyWhenDue(t *testing.T) {
	// Attempt(uuid) on a fresh pending row → fencing
	// Attempt(uuid) again before FenceUntil → row unchanged (still fencing), no engine call
}

func TestFinalizer_LostClaimIsNotAnError(t *testing.T) {
	// memStore option: MarkFencing returns repository.ErrErasureClaimLost → RunOnce returns (1, nil)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd backend && go test ./internal/core/retrieval/engine/enginefake/ ./internal/core/retrieval/services/ -run 'EngineFake|Fencing|Backoff|SelectorFor|Finalizer' -v`
Expected: FAIL to compile.

- [ ] **Step 3: Implement**

`FencingWindow`: non-positive inputs take `2m`/`30s`, then `clamp(lease+write+30s, 1m, 1h)`. `Backoff(n)`: `n < 1` is treated as 1. `AuditRelay` follows `llm/services/catalog.go` `SetAuditSink`/`emit` (L99-128): RWMutex, nil sink is a no-op; `Emit` reads tenant, kind, actor, email and IP from ctx; `EmitSystem` takes the tenant explicitly and sets `ActorType: "system"`, `Outcome: "success"`.

One step of the sweep (`step(ctx, e)`, used by `RunOnce` after `ClaimDue` and by `Attempt` after `ClaimOne`; the pass runs under `context.WithTimeout(ctx, cfg.Hold-10s)`):

```
targets := lister result: cols[e.Corpus] for kind item, else every listed collection (deduplicated)
lister error                      → MarkRetry(now + Backoff(e.Attempts+1))
eng == nil:
    len(targets) == 0             → MarkDone(now) + finalized event   // nothing can have been written
    otherwise                     → MarkRetry, WARN "engine not configured but corpus collections recorded"
pass: for each target: DeleteByFilter(sel); ErrNotFound → next; other error → fail
                       Count(sel);        ErrNotFound → next; error or n > 0 → fail
fail                              → MarkRetry(now + Backoff(e.Attempts+1)), WARN with erasure uuid and kind (never the subject)
ok and status pending             → MarkFencing(firstPassAt now, fenceUntil now + FencingWindow(cfg().Lease, cfg().WriteTimeout))
ok and status fencing             → MarkDone(now); EmitSystem(ctx, e.TenantID, AuditErasureFinalized, "retrieval_erasure", e.UUID,
                                      {"kind", "scope", "attempts", "collections": len(targets)})
```

`ErrErasureClaimLost` from any `Mark*` is logged at DEBUG and counted as processed. `RunOnce` claims up to `Batch` rows and returns the first non-claim error only for `ClaimDue` failures. `Run` logs `RunOnce` errors at WARN and never exits before ctx is done.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd backend && go test -race ./internal/core/retrieval/engine/enginefake/ ./internal/core/retrieval/services/ -run 'EngineFake|Fencing|Backoff|SelectorFor|Finalizer' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/core/retrieval
git commit -m "feat(retrieval): finalize erasures in two passes around a fencing window

Prop: upstream
Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 4: PII producer `retrieval`

**Files:**
- Create: `backend/internal/core/retrieval/repository/search_log_dsr.go`
- Modify: `backend/internal/core/retrieval/module.go` (`Collections()` entry for `retrieval_search_log`)
- Create: `backend/internal/core/retrieval/services/pii_producer.go`
- Test: `backend/internal/core/retrieval/repository/search_log_dsr_test.go` (Mongo), `backend/internal/core/retrieval/services/pii_producer_test.go` (fakes), `backend/internal/core/retrieval/services/erasure_veto_integration_test.go` (Mongo)

**Interfaces:**
- Consumes: Task 2 (`ErasureStore`), Task 3 (`Finalizer.Attempt`, `enginefake`), PR 5's `<SearchLog>` and `<search fixture>`, PR 4's `Tombstones`.
- Produces:
  ```go
  // repository
  type SearchLogDSR struct{ /* coll */ }
  func NewSearchLogDSR(db *mongo.Database) *SearchLogDSR
  func (r *SearchLogDSR) ListByUserAllTenants(ctx context.Context, userUUID string) ([]models.<SearchLog>, error) // searcher OR subjects ∋ "user:"+uuid
  func (r *SearchLogDSR) DeleteByUserAllTenants(ctx context.Context, userUUID string) (int64, error)
  func (r *SearchLogDSR) DeleteBySubject(ctx context.Context, tenantID, subject string) (int64, error)
  func (r *SearchLogDSR) DeleteByTenant(ctx context.Context, tenantID string) (int64, error)

  // services
  type SearchLogDSRRepo interface { /* the four methods above */ }
  var ErrErasureEmptySubject = errors.New("retrieval: empty data subject")
  type ExportedSearch struct {
  	TenantID    string    `json:"tenantId"`
  	Corpus      string    `json:"corpus"`
  	Query       string    `json:"query"`
  	Filter      any       `json:"filter,omitempty"`
  	ResultCount int       `json:"resultCount"`
  	At          time.Time `json:"at"`
  }
  type ExportedAppearance struct {
  	TenantID string    `json:"tenantId"`
  	Corpus   string    `json:"corpus"`
  	At       time.Time `json:"at"`
  }
  type PersonalDataExport struct {
  	Searches          []ExportedSearch     `json:"searches"`
  	AppearedInResults []ExportedAppearance `json:"appearedInResults"`
  }
  type PIIProducer struct{ /* store, logs, fin, now, logger */ }
  func NewPIIProducer(store ErasureStore, logs SearchLogDSRRepo, fin *Finalizer, now func() time.Time, logger *slog.Logger) *PIIProducer
  func (p *PIIProducer) Subject() string // "retrieval"
  func (p *PIIProducer) ExportPersonalData(ctx context.Context, userUUID string) (any, error)
  func (p *PIIProducer) PurgePersonalData(ctx context.Context, userUUID string, mode iface.EraseMode) (iface.PurgeResult, error)
  var _ iface.PIIProducer = (*PIIProducer)(nil)
  ```

- [ ] **Step 1: Write the failing tests**

`search_log_dsr_test.go` (Mongo): seed rows `r1 {tA, searcher u1, subjects []}`, `r2 {tB, searcher u2, subjects ["user:u1","crm:7"]}`, `r3 {tA, searcher u2, subjects ["crm:7"]}`, `r4 {tB, searcher u3, subjects []}`.

```go
func TestSearchLogDSR_ListByUserMatchesSearcherOrSubjectAcrossTenants(t *testing.T) { /* u1 → {r1, r2} */ }
func TestSearchLogDSR_DeleteByUser(t *testing.T)    { /* DeleteByUserAllTenants(u1) == 2; r3, r4 remain */ }
func TestSearchLogDSR_DeleteBySubjectStaysInTenant(t *testing.T) { /* DeleteBySubject(tA, "crm:7") == 1 (r3); r2 remains */ }
func TestSearchLogDSR_DeleteByTenant(t *testing.T)  { /* DeleteByTenant(tB) == 2; r1, r3 remain */ }
```

`pii_producer_test.go` (memStore, a fake `SearchLogDSRRepo`, `enginefake`, staticLister, clock):

```go
func TestPIIProducer_Subject(t *testing.T) { /* "retrieval" */ }

func TestPIIProducer_RefusesEmptySubject(t *testing.T) {
	// ExportPersonalData(ctx, "") and PurgePersonalData(ctx, "", EraseHardDelete) → errors.Is(err, ErrErasureEmptySubject)
	// no tombstone written, no log call
}

func TestPIIProducer_ExportProjection(t *testing.T) {
	// rows: searcher u1 in tA {query "head of sales", filter, sourceIds [s1,s2], subjects ["user:u9"]}
	//       subject u1 in tB {searcher u2, query "mario", subjects ["user:u1"]}
	// out := export.(*PersonalDataExport)
	// out.Searches == [{tA, corpus, "head of sales", filter, 2, at}]
	// out.AppearedInResults == [{tB, corpus, at}]  — no searcher, no query, no other subject
	// json.Marshal(out) contains neither "u9" nor "u2" nor "mario"
}

func TestPIIProducer_ExportNothingHeld(t *testing.T) { /* (nil, nil) */ }

func TestPIIProducer_PurgePendingUntilFinalized(t *testing.T) {
	// engine configured, cols {"people": {g1}} holding a user:u1 point; 2 log rows for u1
	// r1 := Purge(u1, EraseAnonymize) at t0 → r1.Pending; r1.RowsDeleted == 2;
	//   r1.Collections == []string{"retrieval_search_log", "retrieval_index"}; status fencing (inline first pass)
	// r2 := Purge(u1, EraseHardDelete) at t0+1m → r2.Pending; r2.RowsDeleted == 0; one user tombstone in memStore
	// fin.RunOnce at t0+3m → done
	// r3 := Purge(u1, EraseHardDelete) → !r3.Pending
}

func TestPIIProducer_PurgeNoEngineNoCollectionsNotPending(t *testing.T) {
	// eng nil, empty lister → first Purge returns Pending false
}

func TestPIIProducer_PurgeEngineDownStillRecordsTombstone(t *testing.T) {
	// FailDeletes(engine.ErrUnavailable) → err == nil, Pending true, tombstone present, status pending
}
```

`erasure_veto_integration_test.go` (Mongo; ADR acceptance "never served from the moment its erasure is recorded"):

```go
func TestPurgePersonalData_TombstoneVetoesSearchWhilePointsRemain(t *testing.T) {
	// eng := enginefake.New(); collection of corpus "people" (as the fixture names it) seeded in tenant tA with
	//   p1 {sourceId s1, subjects ["user:u1"], visibleTo ["tenant:*"]} and p2 {sourceId s2, subjects ["crm:9"], visibleTo ["tenant:*"]}
	// eng.FailDeletes(engine.ErrUnavailable)
	// producer built over repository.NewErasureFinalization(db), repository.NewSearchLogDSR(db), a Finalizer on eng
	// res, err := producer.PurgePersonalData(ctx, "u1", iface.EraseHardDelete); err == nil; res.Pending
	// n, _ := eng.Count(coll, engine.Selector{Subject: "user:u1"}); n == 1      // the point still exists
	// svc := <search fixture>(t, eng, <Tombstones constructor>(db))  // Visible answers true at the stored revision
	// out, err := svc.Search(ctxForTenant(tA, member), iface.RetrievalSearchRequest{Corpus: "people", Query: "x", Mode: iface.RetrievalModeKeyword})
	// err == nil; source ids of out.Hits == ["s2"]
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd backend && MONGO_TEST_URI=$MONGO_TEST_URI go test ./internal/core/retrieval/... -run 'SearchLogDSR|PIIProducer|TombstoneVetoes' -v`
Expected: FAIL to compile.

- [ ] **Step 3: Implement**

- `SearchLogDSR`: user methods filter `{$or: [{userUuid: u}, {subjects: "user:"+u}]}` with `//tenantscope:allow dsr: right-of-access/erasure of the subject across every org, invoked only by the compliance DSR pipeline through iface.PIIProducer`; `DeleteBySubject` and `DeleteByTenant` filter on the explicit `tenantId` (`dsr: owner-module subject erasure for an explicit tenant` / `system: tenant purge of an explicit tenant`). `Collections()`: indexes `{userUuid 1}` and `{subjects 1}` unless PR 5 declares the same key patterns.
- `ExportPersonalData`: a row goes to `Searches` when the user is the searcher (`ResultCount = len(sourceIds)`; never sourceIds, subjects or principals) and to `AppearedInResults` when `"user:"+uuid` is among its subjects (tenant, corpus, time only: the searcher and the query are another person's data, GDPR art. 15(4)); both lists empty → `(nil, nil)`.
- `PurgePersonalData` (both modes delete, ADR D12: vectors are personal data):
  1. empty uuid → `ErrErasureEmptySubject`;
  2. `store.EnsureUser(ctx, uuid, now())` — error returned (compliance retries it);
  3. `logs.DeleteByUserAllTenants` — error returned;
  4. if the tombstone is not done: `fin.Attempt(ctx', e.UUID)` with `ctx'` = 20 s timeout; its error is logged, never returned;
  5. `store.ListUser(uuid)`; `Pending` = any row whose `EffectiveStatus()` is not `done`;
  6. return `PurgeResult{RowsDeleted: n, Collections: {"retrieval_search_log", "retrieval_index"}, Pending: pending}`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd backend && MONGO_TEST_URI=$MONGO_TEST_URI go test -race ./internal/core/retrieval/... -run 'SearchLogDSR|PIIProducer|TombstoneVetoes' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/core/retrieval
git commit -m "feat(retrieval): export and erase personal data as PII producer

Prop: upstream
Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 5: `DeleteBySubject` and the tenant purge

**Files:**
- Create: `backend/internal/core/retrieval/services/erasure.go`
- Modify: the file holding PR 3's stub `func (s *Service) DeleteBySubject` (find it with `grep -rn 'func (s \*Service) DeleteBySubject' backend/internal/core/retrieval`), and the `Service` struct for two fields
- Test: `backend/internal/core/retrieval/services/erasure_test.go`

**Interfaces:**
- Consumes: Tasks 2–4 (`ErasureStore`, `SearchLogDSRRepo`, `*Finalizer`, `*AuditRelay`, `AuditErasureRecorded`).
- Produces:
  ```go
  type ErasureService struct{ /* store, logs, fin, isOwner, audit, now, logger */ }
  func NewErasureService(store ErasureStore, logs SearchLogDSRRepo, fin *Finalizer, isOwner func(owner string) bool,
  	audit *AuditRelay, now func() time.Time, logger *slog.Logger) *ErasureService
  func (e *ErasureService) DeleteBySubject(ctx context.Context, tenantID, subject string) error
  func (e *ErasureService) PurgeTenant(ctx context.Context, tenantUUID string) error // the TenantPurgeHooks hook
  func ValidateOwnerSubject(subject string) (owner string, err error)

  func (s *Service) SetErasure(e *ErasureService)
  func (s *Service) SetAuditRelay(a *AuditRelay)
  func (s *Service) SetAuditSink(sink iface.AuditSink) // forwards to the relay; satisfies iface.AuditSinkSetter
  // Service.DeleteBySubject (iface.RetrievalSearch) delegates to the ErasureService; nil ErasureService → iface.ErrRetrievalNotConfigured
  ```

- [ ] **Step 1: Write the failing tests**

```go
func TestValidateOwnerSubject(t *testing.T) {
	ok := []string{"crm:42", "crm_contact:0f8c-11", "a:x"}
	bad := []string{"", "crm", ":42", "crm:", "user:u1", "CRM:42", "crm:4 2", "crm:" + strings.Repeat("x", 201),
		"1crm:4", strings.Repeat("a", 33) + ":4"}
	// ok → owner == prefix, nil; bad → errors.Is(err, iface.ErrRetrievalInvalidRequest)
}

func TestDeleteBySubject_RefusesUnknownOwnerAndEmptyTenant(t *testing.T) {
	// isOwner = func(o string) bool { return o == "crm" }
	// DeleteBySubject(ctx, "tA", "billing:1") and (ctx, "", "crm:1") → ErrRetrievalInvalidRequest; no tombstone, no log call
}

func TestDeleteBySubject_TombstoneLogsAndFirstPass(t *testing.T) {
	// cols {"people": {g1}}: point (tA, subjects ["crm:42"]), point (tB, subjects ["crm:42"])
	// DeleteBySubject(ctx, "tA", "crm:42") == nil
	// one subject tombstone {scope tenant, tenantId tA, kind subject, subject crm:42}, status fencing
	// logs.DeleteBySubject called with (tA, crm:42); the tB point is still there
	// one AuditErasureRecorded event, TenantID "tA", ResourceType "retrieval_erasure", Metadata["kind"] == "subject"
	// second call == nil, still one tombstone, still one recorded event
}

func TestDeleteBySubject_NeedsNoTenantInContext(t *testing.T) {
	// context.Background() (no tenant, no kind) → nil; tombstone written for tA
}

func TestPurgeTenant_TombstoneLogsAndCount(t *testing.T) {
	// two collections hold tA and tB points; PurgeTenant(ctx, "tA") == nil
	// tenant tombstone for tA; logs.DeleteByTenant(tA) called; no tA point in either collection; tB untouched
	// clock to FenceUntil, RunOnce → done (Count == 0 per collection was the confirmation)
}

func TestPurgeTenant_RetriesTombstoneWriteThenFails(t *testing.T) {
	// memStore.EnsureTenant fails 3 times → PurgeTenant returns that error after exactly 3 calls; no recorded event
	// fails twice then succeeds → nil
}

func TestPurgeTenant_SurvivesCancelledRequestContext(t *testing.T) {
	// ctx already cancelled → still writes the tombstone (detached context) and returns nil
}

func TestService_DeleteBySubjectDelegates(t *testing.T) {
	// var _ iface.AuditSinkSetter = (*Service)(nil)
	// Service with SetErasure(…) → DeleteBySubject reaches the ErasureService; without → ErrRetrievalNotConfigured
	// SetAuditSink(sink) then an erasure → sink received the event
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd backend && go test ./internal/core/retrieval/services/ -run 'OwnerSubject|DeleteBySubject|PurgeTenant|DeleteBySubjectDelegates' -v`
Expected: FAIL to compile.

- [ ] **Step 3: Implement**

- `ValidateOwnerSubject`: `^([a-z][a-z0-9_]{0,31}):(\S{1,200})$`, owner `user` refused (platform users are erased only through compliance, ADR D12); errors wrap `iface.ErrRetrievalInvalidRequest`.
- `DeleteBySubject`: validate; `tenantID == ""` refused; `isOwner(owner)` must hold (the owner is a module that registered a corpus); `EnsureSubject`; `logs.DeleteBySubject`; on `created` emit `AuditErasureRecorded` via `EmitSystem(ctx, tenantID, …, {"kind": "subject"})` (never the subject value); inline `fin.Attempt` (20 s, errors logged). Returns errors of the two Mongo writes only.
- `PurgeTenant`: `tenantUUID == ""` refused; work on `context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)`; `EnsureTenant` up to 3 tries spaced 100 ms then 400 ms; `logs.DeleteByTenant`; recorded event `{"kind": "tenant"}` on `created`; inline `fin.Attempt`. Returned errors become tenant's `tenant.cascade.hook_failed` audit (Task 1).
- No `RequireInternalTenant` anywhere here (ADR D13).

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd backend && go test -race ./internal/core/retrieval/services/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/core/retrieval
git commit -m "feat(retrieval): erase owner subjects and purged tenants from the index

Prop: upstream
Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 6: Module wiring

**Files:**
- Create: `backend/internal/core/retrieval/repository/corpus_state_collections.go`
- Modify: `backend/internal/core/retrieval/module.go` (`Init`, `Start`, module struct)
- Test: `backend/internal/core/retrieval/repository/corpus_state_collections_test.go` (Mongo), `backend/internal/core/retrieval/erasure_wiring_test.go`

**Interfaces:**
- Consumes: `<CorpusStateRepo>`, PR 3's engine construction (nil when `QDRANT_URL` is empty), PR 3's corpus registry, Tasks 2–5.
- Produces:
  ```go
  func (r *<CorpusStateRepo>) LiveCollections(ctx context.Context) (map[string][]string, error) // satisfies services.CollectionLister
  func finalizerConfig(deps *module.Dependencies) func() services.FinalizerConfig // in module.go
  ```

- [ ] **Step 1: Write the failing tests**

```go
// corpus_state_collections_test.go (Mongo)
func TestLiveCollections_ListsEveryGeneration(t *testing.T) {
	// state "people": active 1, building 2 → {"people": {"<prefix>_people_g1", "<prefix>_people_g2"}}
	// state "notes": no active, no building generation → "notes" absent
	// state "legacy" (no corpus registered for it) with active 3 → present: erasure must reach it
}

// erasure_wiring_test.go (package retrieval; builds deps the way PR 3's module tests do)
func TestInit_RegistersRetrievalPIIProducer(t *testing.T) {
	// registry := iface.NewPIIProducerRegistry() under module.ServicePIIProducerRegistry
	// after Init: exactly one producer with Subject() == "retrieval"
}

func TestInit_RegistersTenantPurgeHook(t *testing.T) {
	// fakeHooks{fns} registered under module.ServiceTenantService; after Init len(fns) == 1
}

func TestInit_WithoutTenantPurgeHooksWarnsAndSucceeds(t *testing.T) {
	// nothing under ServiceTenantService → Init == nil; a WARN naming TenantPurgeHooks is logged
}

func TestInit_RegisteredServiceAcceptsAuditSink(t *testing.T) {
	// s, ok := module.GetTyped[iface.AuditSinkSetter](deps.Services, module.ServiceRetrieval); ok
}

func TestFinalizerConfig_FenceUsesMaxWorkerLease(t *testing.T) {
	// → Lease services.MaxWorkerLease (15m), WriteTimeout 30s, Interval 15s, Batch 50, Hold 2m
	// worker_lease set to "5m" in config → still Lease 15m (the fence never follows a live lease change)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd backend && MONGO_TEST_URI=$MONGO_TEST_URI go test ./internal/core/retrieval/ ./internal/core/retrieval/repository/ -run 'LiveCollections|Init_|FinalizerConfig' -v`
Expected: FAIL.

- [ ] **Step 3: Implement**

- `LiveCollections`: every corpus-state document (not only registered corpora), each generation it names (active, building, retained), mapped to collection names with the same helper PR 3/4 use for `<prefix>_<corpus>_g<generation>`; annotation `//tenantscope:allow system: installation-wide corpus state listed for erasure finalization`.
- `Init` (after PR 3 builds the `Service`; never contacts the engine): build `ErasureFinalization`, `SearchLogDSR`, `AuditRelay`, `Finalizer(store, corpusStateRepo, eng, finalizerConfig(deps), relay, time.Now, logger)`, `ErasureService(…, isOwner from the registered corpus specs' `Owner`, …)`; `svc.SetErasure`, `svc.SetAuditRelay`; register `NewPIIProducer(…)` on `module.GetTyped[*iface.PIIProducerRegistry](deps.Services, module.ServicePIIProducerRegistry)` (as `llm/module.go` L377-380); resolve `iface.TenantPurgeHooks` from `module.ServiceTenantService` and register `erasure.PurgeTenant`, else WARN. Keep the finalizer on the module struct.
- `finalizerConfig`: `Lease: services.MaxWorkerLease`, `WriteTimeout: 30*time.Second`, constants for the rest; no config key is read.
- `Start`: launch `m.finalizer.Run(runCtx)` with the same run context and stop handling PR 4 uses for the outbox worker.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd backend && MONGO_TEST_URI=$MONGO_TEST_URI go test -race ./internal/core/retrieval/... && go test ./cmd/server/ -run TestCoreModulesLoadOrder`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/core/retrieval
git commit -m "feat(retrieval): wire erasure finalization, PII producer and tenant purge hook

Prop: upstream
Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 7: Compliance pushes its audit sink into retrieval

**Files:**
- Modify: `backend/internal/core/compliance/module.go` (`publishAuditSink`, L368-383)
- Test: `backend/internal/core/compliance/retrieval_wiring_test.go`
- Docs: `backend/internal/core/compliance/AGENTS.md` (invariants L31 producer list, a new bullet for retrieval's sink), `docs/site/modules/core/compliance.mdx` (L15 producer list)

**Interfaces:**
- Consumes: `module.ServiceRetrieval` (PR 3), `iface.AuditSinkSetter` on the registered retrieval `*services.Service` (Task 5).

- [ ] **Step 1: Write the failing test** (reuse `nopSink` from `llm_wiring_test.go`)

```go
type fakeRetrieval struct{ audit iface.AuditSink }

func (f *fakeRetrieval) SetAuditSink(s iface.AuditSink) { f.audit = s }

func TestRetrievalService_ReceivesAuditSink(t *testing.T) {
	reg := module.NewServiceRegistry()
	rs := &fakeRetrieval{}
	reg.Register(module.ServiceRetrieval, rs)
	publishAuditSink(reg, nopSink{})
	if rs.audit == nil {
		t.Fatal("retrieval service not wired with the audit sink")
	}
}

func TestRetrievalService_AbsentIsTolerated(t *testing.T) {
	publishAuditSink(module.NewServiceRegistry(), nopSink{}) // must not panic
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/core/compliance/ -run RetrievalService -v`
Expected: FAIL (`rs.audit == nil`).

- [ ] **Step 3: Implement**: in `publishAuditSink`, after the llm block, `if rs, ok := module.GetTyped[iface.AuditSinkSetter](registry, module.ServiceRetrieval); ok { rs.SetAuditSink(sink) }`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd backend && go test ./internal/core/compliance/...`
Expected: PASS.

- [ ] **Step 5: Update docs**: compliance AGENTS live producers gain `retrieval` ("reports `Pending` until its index erasure is finalized, normally one fencing window, a few minutes; with no retrieval overlay it is never pending"); the sink bullet names `retrieval` beside `llm`; `compliance.mdx` producer list gains `retrieval`.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/core/compliance docs/site/modules/core/compliance.mdx
git commit -m "feat(compliance): push the audit sink into the retrieval service

Prop: upstream
Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 8: End-to-end: an erasure request stays incomplete until finalization

**Files:**
- Create: `backend/cmd/server/retrieval_erasure_e2e_test.go` (the composition root already imports module services, `main.go` L22-24)

**Interfaces:**
- Consumes: compliance `services.NewDSRService`, `services.NewErasureRequestService(repo, dsr, logger)`, `repository.NewErasureRequestRepo`, `ErasureRequestService.Execute` → `*ErasureOutcome{Request, Run}` (an `incomplete` row may be executed again: the operator retry of PR 2, which `RetryDue` repeats on its own once `nextAttemptAt` passes), `models.ErasureRequestIncomplete` / `ErasureRequestCompleted`; retrieval `repository.NewErasureFinalization`, `repository.NewSearchLogDSR`, `services.NewFinalizer`, `services.NewPIIProducer`, `enginefake`.

- [ ] **Step 1: Write the tests** (Mongo-gated; a local `e2eDB(t)` like `llm/repository/testdb_test.go`; a local `staticLister`; a settable clock for the finalizer)

```go
func TestRetrievalErasure_RequestIncompleteUntilFinalized(t *testing.T) {
	// eng seeded: collection "orkestra_people_g1" with a point {tenant tA, subjects ["user:u1"]}
	// producers := iface.NewPIIProducerRegistry(); producers.Register(retrieval PIIProducer)
	// dsr := NewDSRService(producers, nil, logger); ers := NewErasureRequestService(NewErasureRequestRepo(db), dsr, nil, logger)
	// req := ers.Lodge(ctx, "u1", "tA", "test")
	// out, err := ers.Execute(ctx, req.UUID, iface.EraseHardDelete, "op"); err == nil
	// out.Request.Status == models.ErasureRequestIncomplete
	// eng.Points(coll) has no user:u1 point (inline first pass)
	// eng.Seed(coll, stale point with user:u1)                        // stale writer in the fence
	// ers.Execute(ctx, req.UUID, iface.EraseHardDelete, "op") before the fence ends → Status still incomplete
	// finalizer clock past FenceUntil; fin.RunOnce(ctx) → eng has no user:u1 point
	// ers.Execute(ctx, req.UUID, iface.EraseHardDelete, "op") → out.Request.Status == models.ErasureRequestCompleted
}

func TestRetrievalErasure_NoOverlay_RequestCompletesAtOnce(t *testing.T) {
	// finalizer with eng nil and an empty lister
	// first Execute → out.Request.Status == models.ErasureRequestCompleted
}
```

- [ ] **Step 2: Run tests**

Run: `cd backend && MONGO_TEST_URI=$MONGO_TEST_URI go test -race ./cmd/server/ -run RetrievalErasure -v`
Expected: PASS (Tasks 2–6 provide every piece; a failure here is an integration bug to fix in the owning task's code, not in the test).

- [ ] **Step 3: Commit**

```bash
git add backend/cmd/server/retrieval_erasure_e2e_test.go
git commit -m "test(retrieval): erasure request stays incomplete until index finalization

Prop: upstream
Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 9: Static gates, retrieval docs, CI

**Files:**
- Modify: `backend/tools/piiscan/baseline.txt` (remove `pii.subject_collection.no_producer:retrieval` if PR 5 added it)
- Modify: `backend/internal/core/retrieval/AGENTS.md`, `docs/site/modules/core/retrieval.mdx`, `docs/site/sdk/shared-iface.mdx` (`PIIProducer` row L28)

- [ ] **Step 1: Run the gates**

Run: `cd backend && go run ./tools/piiscan/cmd/piiscan -baseline=tools/piiscan/baseline.txt ./internal/... && go run ./tools/tenantscope/cmd/tenantscope -baseline=tools/tenantscope/baseline.txt ./internal/...`
Expected: exit 0; piiscan reports a `NewPIIProducer` for `retrieval` and no `retrieval` diagnostic; tenantscope reports nothing in files this PR touched (every new Mongo call is scoped or annotated with `dsr:`/`system:`).

- [ ] **Step 2: Update docs**

`retrieval/AGENTS.md`, section "Erasure and data-subject rights": the status machine `pending → fencing → done` and that `Covered` ignores status; the fencing formula with its two config keys, clamp and the 30 s margin (a lease lowered while a longer one is held is covered only by the read veto); backoff, batch, hold; the claim CAS and multi-replica safety; the no-engine fast path and the "engine nil but collections recorded" pending case; the retention decision of the Global Constraints (no tombstone is ever deleted in step 1) with its reasons; PII producer `retrieval` (export projection, both modes delete, `Pending`); `DeleteBySubject` format and owner rule; tenant purge hook (detached 30 s context, 3 tries, failures audited by tenant); audit actions `retrieval.erasure.recorded` / `.finalized` and `AuditRelay.Emit` for PR 7's admin actions. `retrieval.mdx`: the same for operators, without internals (what an erasure guarantees, how long it takes, what operators see). If `retrieval.mdx` does not exist yet (roadmap row 8 creates the page), write the section only in AGENTS.md and say so in the PR description. `shared-iface.mdx`: add `retrieval` to the `PIIProducer` row.

- [ ] **Step 3: Full backend CI**

Run: `make ci-backend` from the repo root, with `MONGO_TEST_URI` set (and `QDRANT_TEST_URL`/`QDRANT_TEST_API_KEY` if PR 3's conformance suite should run).
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add backend/tools/piiscan/baseline.txt backend/internal/core/retrieval/AGENTS.md docs/site
git commit -m "docs(retrieval): document erasure finalization and data-subject rights

Prop: upstream
Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
