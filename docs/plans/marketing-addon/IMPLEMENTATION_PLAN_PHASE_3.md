---
type: implementation-plan
author: claude
date: 2026-05-22
domain: orkestra
component: marketing-addon
status: ready-to-execute
phase: 3 (Import avanzati)
branch: feature/marketing-phase-3
---

# Marketing Addon — Phase 3 Implementation Plan

Concrete plan to land **Phase 3 (Import avanzati)** from
[`Orkestra_marketing_addon.md`](Orkestra_marketing_addon.md) §9 on
top of the Phase 2 work that lives on `feature/marketing-phase-2`
(5 commits `37c1fe7..8f95c71` + docs commit `63a60cf`).

Phase 1 reference plan: [`IMPLEMENTATION_PLAN.md`](IMPLEMENTATION_PLAN.md).
Phase 2 reference plan: [`IMPLEMENTATION_PLAN_PHASE_2.md`](IMPLEMENTATION_PLAN_PHASE_2.md).
Phases 4–5 are explicitly **out of scope** and will be planned
separately once Phase 3 is in `dev`.

Decisions resolved with the user before this plan was written
(2026-05-22):

- **PR split**: 5 sequential sub-PRs on `feature/marketing-phase-3`
  (data layer + async runner, conflict review + soft-match +
  auto-emission, Excel adapter, Odoo JSON-2 adapter, frontend + docs).
- **Import runner shape**: **async replaces sync**. Phase 1's
  inline-execution `POST /v1/marketing/imports` handler migrates to a
  background worker that drains a queue. The handler returns
  `202 Accepted` with a job UUID; existing `ImportJob` lifecycle
  states (`queued` / `running` / `done` / `failed`) gain a new
  `paused_for_review` state. This is required for Odoo JSON-2 pulls
  that can run for minutes against large catalogs.
- **Odoo adapter**: targets the **External JSON-2 API of Odoo 19.0**
  (REST-shaped JSON over HTTPS), **not** the legacy XML-RPC surface
  the design doc §5.4 originally specified. Auth is **API key +
  database name** stored as encrypted ConfigService secrets per
  environment.
- **Activity auto-emission from Phase 1 services**: **included in
  Phase 3**. The Phase-2 plan deliberately deferred this work; Phase 3
  picks it up because the engagement-CSV story (importer-emitted
  activity from external campaign exports) only becomes meaningful
  once the existing Phase-1 write paths also emit. Phase 3 ships
  listeners on `PersonService` / `TagService` / `MembershipService`
  for `imported` / `merged` / `tag_added` / `tag_removed`.
- **Plan language**: English, parity with Phase 1 + Phase 2.

## Phase 3 deliverables (from §9)

What ships at the end of this plan:

- 1 new MongoDB collection: `marketing_conflict_reviews`
  (schema already canonical at
  [`schemas/marketing_conflict_reviews.md`](schemas/marketing_conflict_reviews.md)).
- Async import-job runner: a `Worker` primitive backed by a
  bounded in-process queue + DB-checkpointed lifecycle, so a backend
  restart resumes `queued` jobs and marks `running` ones as
  `failed` with a `runner_crash` reason on the next boot.
- `marketing_import_jobs.status` gains the `paused_for_review` state.
  When a job hits one or more blocking conflicts, the worker writes
  the canonical-record payload + activities into pending
  `marketing_conflict_reviews` rows and parks the job in
  `paused_for_review`. Resolving the last review (or `dismiss`ing it)
  transitions the job back to `running` for the final commit pass.
- Soft-match dedup primitives (design §5.5):
  - Person: `first_name + last_name + (any phone overlap)` →
    soft-match → review queue.
  - Organization: `lower(trim(legal_name))` exact → soft-match →
    review queue.
- 2 new Cedar permissions: `marketing.conflict.resolve` (handler-level
  RBAC) + `marketing.conflict.dismiss` (folded into the same bucket;
  splits if/when we differentiate). Reads of the review queue fold
  into `marketing.contact.read` (same fold-in pattern Phase 2 used
  for activities/snapshots).
- Cedar permission for Phase 1 wiring touched: existing
  `marketing.import.run` is reused for `POST /v1/marketing/imports`
  (the contract is unchanged from the caller's point of view; only
  the response shape moves from inline `Result` to `202 + jobUuid`).
- New importer adapters under `importers/`:
  - `importers/excel/` — `.xlsx` reader via `github.com/xuri/excelize/v2`,
    sheet picker, dry-run that lists sheet names + row counts.
    Reuses the existing pipeline + column mapping primitives — the
    adapter only differs in extract.
  - `importers/odoo/` — Odoo 19.0 External JSON-2 client, `res.partner`
    fetch via paginated GET, `parent_id` → org/person linking,
    `category_ids` → `marketing_tags` mapping with auto-create policy.
- Engagement-CSV mode on the existing `importers/csv/` adapter:
  detect columns matching `^email_(opened|clicked|bounced|unsubscribed|complained)$`
  by header name + `^occurred_at$` timestamp → emit `Activity` rows
  alongside the canonical-record stream (decision §5.3, "output
  duplice"). Activities flow through `ActivityService.Create` so
  the `dedupKey` invariant + listener firing are preserved.
- Phase-2 auto-emission deferred work, included here:
  - `PersonService.Update` listener emits `tag_added` /
    `tag_removed` on diff against the previous tag set.
  - `MembershipService.Create` emits `imported` if the membership
    was created by the importer pipeline (detected via
    `ProvenanceSource.importJobUuid != nil`).
  - `MembershipService.Update` (auto-merge path) emits `merged`
    when the resolution closes a `marketing_conflict_reviews` row.
- Operator UI:
  - new `/marketing/reviews` page (review queue table + status
    filter + per-job filter + bulk actions).
  - per-review side-by-side resolver Modal: existing vs incoming,
    per-field radio (`keep_existing` / `take_incoming`) or
    `manual_merge` JSON textarea. Submit drives `POST
    /v1/marketing/conflict-reviews/{uuid}/resolve`.
  - adapter picker on the import wizard (Step 1): radio between
    `csv` (default), `excel`, `odoo`. Selecting `excel` lifts the
    sheet picker into Step 2 mapping; selecting `odoo` swaps the
    file upload for a connection-config form (uses the per-
    environment ConfigService secrets).
  - "View reviews" affordance on every `paused_for_review` import
    job row (deep-links the queue page filtered to that job UUID).
- One example soft-match auto-create — when the operator runs the
  same CSV twice with a `first+last+phone` overlap, the second pass
  parks the row in the queue rather than auto-creating a near-duplicate.

Explicitly **deferred** to Phase 4+:

- The `corrected_by` UI flow (Phase 2 shipped the backend; full UI
  with audit trail still lives in Phase 4's bigger UX pass).
- Card lifecycle activities (`card_issued` / `card_status_changed`)
  are Phase 4.
- ESP webhook ingestion (Sendgrid/Postmark/Mailgun) — Phase 5.
- Soft-match fuzzy-matching beyond exact-normalized: Phase 5 (need
  a real corpus to tune thresholds against).
- `marketing_import_jobs` retention policy — out of scope; jobs grow
  unbounded for now, prune in compliance addon when DSR ships.

---

## 1. Branch + workflow

```bash
git checkout feature/marketing-phase-2
git pull
git checkout -b feature/marketing-phase-3
```

Branched off `feature/marketing-phase-2` (not `dev`) because Phase 3
depends on Phase 2's `ActivityService` listener pattern + the
`marketing_activities` collection. When Phase 2 merges to `dev`,
Phase 3 will need a rebase (`git rebase dev`) before its own merge.

Sub-PR strategy onto `feature/marketing-phase-3` (each independently
green on CI; merge to branch, not yet to `dev`):

1. **PR-1**: data layer + async runner refactor —
   `marketing_conflict_reviews` collection + repository,
   `ImportJobStatus` enum gains `paused_for_review`, `Worker`
   primitive + bounded queue, sync→async migration of the existing
   `POST /v1/marketing/imports`, `marketing.conflict.resolve`
   permission registered. Phase 1's sync test path migrates to
   poll the job's `done`/`failed` state.
2. **PR-2**: conflict review service + soft-match + Phase-2
   auto-emission — pipeline.go emits review rows instead of
   incrementing `Stats.ConflictsSkipped` on blocking conflicts;
   soft-match helpers under `importers/match/`; listeners
   registered on `PersonService` / `TagService` /
   `MembershipService` that fire `ActivityService.Create` with
   the right kind. `POST /v1/marketing/conflict-reviews/{uuid}/resolve`
   handler + `Resolution` apply path.
3. **PR-3**: Excel adapter — `importers/excel/` (xlsx via
   `xuri/excelize`), sheet picker on the dry-run preview, header-
   based or numeric-index column mapping reused from CSV.
4. **PR-4**: Odoo JSON-2 adapter — `importers/odoo/` with the
   Odoo 19.0 External JSON-2 client, API-key + database auth via
   ConfigService secrets, paginated `res.partner` fetch,
   `parent_id` → org/person linking, `category_ids` →
   `marketing_tags` auto-create. Engagement-CSV column recognition
   on `importers/csv/`.
5. **PR-5**: frontend (review queue page, side-by-side resolver,
   adapter picker on wizard, Odoo connection form) + RTK Query
   slice extension + docs (CLAUDE.md updates, OpenAPI dump regen,
   memory file).

Each PR opens against `feature/marketing-phase-3`; the final squash
to `dev` happens after PR-5.

---

## 2. Module additions (PR-1 wiring)

### 2.1 Collection constants

Extend `models/collections.go` with the 1 new name:

```go
const (
    // ...Phase 1 + 2 entries...
    ConflictReviewsCollection = "marketing_conflict_reviews"
)
```

### 2.2 Collections() index declarations

Append the conflict-review indexes to the module's `Collections()`:

```go
{
    Name: models.ConflictReviewsCollection,
    Indexes: []module.IndexSpec{
        {Keys: bson.D{{"tenantId", 1}, {"status", 1}}},                     // pending queue
        {Keys: bson.D{{"tenantId", 1}, {"importJobUuid", 1}}},              // all reviews of a job
        {Keys: bson.D{{"tenantId", 1}, {"targetKind", 1}, {"status", 1}}},  // type filter
        {Keys: bson.D{{"tenantId", 1}, {"existingUuid", 1}}},               // "this record has pending reviews"
    },
},
```

### 2.3 Permissions

Two new keys on `module.Permissions()`:

```go
{Key: "marketing.conflict.resolve",
 Description: "Resolve marketing import conflict reviews (keep_existing / take_incoming / manual_merge / dismiss)."},
```

Single key covers all four resolution actions — the schema's
`Resolution.action` field carries the choice. Splits to
`.dismiss` separately if/when ops asks for narrower assignment.

### 2.4 ConfigSchema

Two new config keys:

```go
{Key: "import.worker_concurrency", Type: "int", Default: 2,
 EnvVar: "MARKETING_IMPORT_WORKER_CONCURRENCY",
 Description: "Number of import jobs that can run concurrently per backend instance."},

{Key: "import.queue_buffer", Type: "int", Default: 32,
 EnvVar: "MARKETING_IMPORT_QUEUE_BUFFER",
 Description: "In-process bounded queue size before POST /v1/marketing/imports returns 503 (queue full)."},
```

The Odoo adapter's per-tenant secrets (`odoo.base_url`,
`odoo.database`, `odoo.api_key`) live on the **environment**-scoped
config under the Odoo adapter's own keys, **not** the module config
schema — the latter is for ops-tunable defaults; the former are
per-import credentials. Wired via the existing `ModuleConfigService`
per-environment surface (see `pkg/sdk/module/config_service.go`).

### 2.5 NavItems

Append a "Reviews" child under the existing Marketing nav group:

```go
{Group: "Marketing", Label: "Reviews", Path: "/marketing/reviews",
 Icon: "fa-balance-scale", MinRole: "manager"},
```

Order in the sidebar: Contacts · Tags · Custom Fields · Imports ·
**Reviews** · Scoring.

### 2.6 Init() wiring

Phase 3 services + worker hook into `Init()`:

```go
m.ConflictReviewRepo = repository.NewConflictReviewRepository(deps.Mongo)
m.ConflictReviewService = services.NewConflictReviewService(services.ConflictReviewDeps{
    ReviewRepo:        m.ConflictReviewRepo,
    PersonRepo:        m.PersonRepo,
    OrgRepo:           m.OrgRepo,
    MembershipRepo:    m.MembershipRepo,
    ActivityService:   m.ActivityService,           // Phase 2
    ImportJobRepo:     m.ImportJobRepo,
})

m.ImportWorker = importers.NewWorker(importers.WorkerDeps{
    JobRepo:               m.ImportJobRepo,
    Pipeline:              m.Pipeline,
    ConflictReviewService: m.ConflictReviewService,
    Concurrency:           settings.ImportWorkerConcurrency,
    QueueBuffer:           settings.ImportQueueBuffer,
})
```

The existing `Pipeline` constructor gains a `ConflictReviewService`
dep so that `commit()` (§5.6 of the design) can park rows in the
queue instead of incrementing the skipped counter.

Phase-2 auto-emission listeners get registered next to where
`ScoreService` already registers its activity-insert listener:

```go
m.PersonService.RegisterUpdateListener(func(ctx, before, after *Person) {
    diff := tagDiff(before.Tags, after.Tags)
    for _, added := range diff.Added {
        _, _ = m.ActivityService.Create(ctx, &models.Activity{
            PersonUUID: after.UUID, Kind: models.KindTagAdded,
            OccurredAt: now(), Source: models.SourceSystem,
            Payload: map[string]any{"tagUuid": added},
        })
    }
    // ...mirror for diff.Removed...
})
```

`PersonService` / `MembershipService` need a `RegisterUpdateListener`
hook on the service struct — a 2-line addition that defers to the
same `sync.Map` + listener-fan pattern Phase 2's `ActivityService`
already implements. Mirror in `TagService` if/when tag rename emits
become useful.

### 2.7 Start() / Stop()

`Worker.Start(ctx)` runs N goroutines (per `Concurrency`); each
goroutine blocks on the queue channel + the shutdown context.
`Worker.Stop()` closes the queue producer side, drains in-flight
jobs (with a 30s graceful timeout), then cancels the context.
Mirrors the `RecomputeJob` pattern Phase 2 already established —
no new primitives.

The module's existing `Start` / `Stop` gain:

```go
func (m *Module) Start(ctx context.Context) error {
    // ...existing RecomputeJob start...
    m.workerCtx, m.workerCancel = context.WithCancel(ctx)
    return m.ImportWorker.Start(m.workerCtx)
}

func (m *Module) Stop(ctx context.Context) error {
    if m.workerCancel != nil { m.workerCancel() }
    if err := m.ImportWorker.Stop(ctx); err != nil { return err }
    // ...existing RecomputeJob stop...
}
```

---

## 3. Data layer (PR-1)

### 3.1 ConflictReview model

`models/conflict_review.go` (new):

```go
type ConflictReview struct {
    UUID               string                     `bson:"uuid" json:"uuid"`
    TenantID           string                     `bson:"tenantId" json:"-"`
    ImportJobUUID      string                     `bson:"importJobUuid" json:"importJobUuid"`
    TargetKind         ConflictTargetKind         `bson:"targetKind" json:"targetKind"`         // person | organization
    ExistingUUID       string                     `bson:"existingUuid" json:"existingUuid"`
    ExistingSnapshot   map[string]any             `bson:"existingSnapshot" json:"existingSnapshot"`
    IncomingPayload    map[string]any             `bson:"incomingPayload" json:"incomingPayload"`
    IncomingActivities []map[string]any           `bson:"incomingActivities" json:"incomingActivities"`
    Conflicts          []ConflictField            `bson:"conflicts" json:"conflicts"`
    Status             ConflictReviewStatus       `bson:"status" json:"status"`                 // pending | resolved | dismissed
    Resolution         *ConflictResolution        `bson:"resolution,omitempty" json:"resolution,omitempty"`
    ResolvedAt         *time.Time                 `bson:"resolvedAt,omitempty" json:"resolvedAt,omitempty"`
    ResolvedBy         string                     `bson:"resolvedBy,omitempty" json:"resolvedBy,omitempty"`
    ResolvedNotes      string                     `bson:"resolvedNotes,omitempty" json:"resolvedNotes,omitempty"`
    CreatedAt          time.Time                  `bson:"createdAt" json:"createdAt"`
    UpdatedAt          time.Time                  `bson:"updatedAt" json:"updatedAt"`
}

type ConflictField struct {
    Field          string `bson:"field" json:"field"`
    ExistingValue  any    `bson:"existingValue" json:"existingValue"`
    IncomingValue  any    `bson:"incomingValue" json:"incomingValue"`
    Severity       string `bson:"severity" json:"severity"`         // "blocking" | "soft"
}

type ConflictResolution struct {
    Action         ConflictAction      `bson:"action" json:"action"`
    FieldOverrides map[string]any      `bson:"fieldOverrides,omitempty" json:"fieldOverrides,omitempty"`
}

// Enums: ConflictTargetKindPerson / -Organization,
// ConflictReviewStatusPending / -Resolved / -Dismissed,
// ConflictActionKeepExisting / -TakeIncoming / -ManualMerge / -Dismiss
```

`Validate()` on each enum + a top-level `Validate()` that rejects
empty incoming payload, empty conflict list, and `manual_merge`
without `field_overrides`.

### 3.2 ImportJobStatus extension

`models/import_job.go` gains `ImportJobStatusPausedForReview`. Map
unchanged on the wire; the four existing states + this one form a
closed enum.

State diagram (after Phase 3):

```
queued ──► running ──┬─► done
                     │
                     ├─► failed
                     │
                     └─► paused_for_review ──► running (on last resolve)
```

The worker transitions `queued → running` when it picks up a job.
The pipeline's `commit()` transitions `running → paused_for_review`
the first time it finds a blocking conflict; the resolve handler
transitions `paused_for_review → running` when the last
pending review for that job moves to `resolved` or `dismissed`. The
final `done` / `failed` transition happens at the end of the second
pass (the commit step that was deferred).

### 3.3 ConflictReview repository

`repository/conflict_review_repo.go`:

```go
type ConflictReviewRepository struct { coll *mongo.Collection }

// Standard tenantrepo wiring; full surface:
Create(ctx, review) error
GetByUUID(ctx, uuid) (*ConflictReview, error)
List(ctx, filter ConflictReviewListFilter) ([]*ConflictReview, error)
ListPendingForJob(ctx, jobUUID) ([]*ConflictReview, error)
MarkResolved(ctx, uuid, resolution, userID, notes) error
MarkDismissed(ctx, uuid, userID, notes) error
CountPendingForJob(ctx, jobUUID) (int64, error)  // drives job-state transition
```

No `Update` beyond the targeted `MarkResolved` / `MarkDismissed` —
the row is immutable once resolved.

### 3.4 Worker + queue primitives

`importers/worker.go` (new):

```go
type Worker struct {
    queue   chan workerJob
    deps    WorkerDeps
    ctx     context.Context
    wg      sync.WaitGroup
}

type workerJob struct {
    JobUUID  string
    TenantID string
    Adapter  Importer
    Config   Config
    Source   Source
}

func (w *Worker) Enqueue(ctx context.Context, job workerJob) error {
    select {
    case w.queue <- job:
        return nil
    default:
        return ErrQueueFull   // → 503 on the HTTP surface
    }
}

func (w *Worker) Start(ctx context.Context) error {
    for i := 0; i < w.deps.Concurrency; i++ {
        w.wg.Add(1)
        go w.run(ctx)
    }
    return w.resumeQueuedJobsFromDB(ctx)
}

func (w *Worker) Stop(ctx context.Context) error {
    close(w.queue)
    done := make(chan struct{})
    go func() { w.wg.Wait(); close(done) }()
    select {
    case <-done:               return nil
    case <-time.After(30*time.Second): return ErrShutdownTimeout
    }
}
```

**Boot-time recovery** (`resumeQueuedJobsFromDB`):

- All `marketing_import_jobs` in state `queued` get re-enqueued
  in `created_at asc` order.
- All `marketing_import_jobs` in state `running` get marked
  `failed` with `reason: "runner_crash"` (we can't safely
  resume mid-pipeline — partial commits are visible, but the
  audit `sources[]` lets ops trace).
- Jobs in `paused_for_review` stay paused — they're already
  durably waiting on operator input.

### 3.5 Tenant scoping

All repository methods go through `pkg/sdk/tenantrepo` (`Scope`,
`StampInsert`). The boot-time `resumeQueuedJobsFromDB` is the
single cross-tenant query in the worker — uses the same
`ListAcrossTenants`-style bypass pattern Phase 2's
`ListStaleAcrossTenants` documented. The stamping is the same:
load the job, take its `TenantID`, build a fresh ctx with
`context.WithValue(ctx, ctxauth.KeyTenantID, job.TenantID)` before
calling `Pipeline.Run`.

### 3.6 Repository tests

`repository/conflict_review_repo_test.go` covers:

- Create + GetByUUID round-trip (full enum coverage).
- List by `(tenantId, status)`.
- `MarkResolved` writes `resolution` + `resolvedAt` atomically.
- `CountPendingForJob` decrements as reviews resolve.
- Tenant isolation: a `MarkResolved` in tenant A's ctx fails when
  the UUID belongs to tenant B (no-update, no panic).

---

## 4. Conflict review service + soft-match + auto-emission (PR-2)

### 4.1 Pipeline extension

`importers/pipeline.go` gains a `commitOrPark` step. Today its
`commit()` step inserts non-conflicting rows + increments
`Stats.ConflictsSkipped` on hard conflicts. New behavior:

```go
func (p *Pipeline) commitOrPark(ctx, normalized, matched, job) error {
    if conflicts := detectBlockingConflicts(matched); len(conflicts) > 0 {
        // Park the row + its incoming activities in marketing_conflict_reviews.
        return p.deps.ConflictReviewService.Park(ctx, ParkInput{
            ImportJobUUID:      job.UUID,
            TargetKind:         matched.Kind,
            ExistingUUID:       matched.ExistingUUID,
            ExistingSnapshot:   matched.ExistingSnapshot,
            IncomingPayload:    normalized.Payload,
            IncomingActivities: normalized.Activities,
            Conflicts:          conflicts,
        })
    }
    // existing auto-merge path unchanged
    return p.commitMerged(ctx, normalized, matched, job)
}
```

When `Park` returns, the worker checks `CountPendingForJob`. If
non-zero, the job transitions to `paused_for_review` at the end of
the first pass.

### 4.2 Soft-match dedup helpers

`importers/match/softmatch.go`:

```go
// Person: first+last match (case-insensitive, normalized) AND
// any phone number overlap (E.164-normalized, last 10 digits).
func SoftMatchPerson(incoming *Person, existing *Person) bool {
    if !equalsCI(incoming.FirstName, existing.FirstName) ||
       !equalsCI(incoming.LastName, existing.LastName) {
        return false
    }
    return phonesOverlap(incoming.Phones, existing.Phones)
}

// Organization: lower(trim(legal_name)) exact match.
func SoftMatchOrganization(incoming *Organization, existing *Organization) bool {
    return normalizeLegalName(incoming.LegalName) ==
           normalizeLegalName(existing.LegalName)
}
```

Soft-matched rows **always** go to the review queue, never
auto-merge — per design §5.5 the false-positive rate would otherwise
silently corrupt the dedup. `Conflicts[].Severity` is set to
`"soft"` so the UI can render them differently from blocking.

The strict-match path (email for Person, VAT/taxCode for Org) is
unchanged from Phase 1. Soft-match runs **only** when strict-match
yielded no candidate.

### 4.3 ConflictReviewService

`services/conflict_review_service.go`:

```go
type ConflictReviewService struct { /* deps */ }

func (s *ConflictReviewService) Park(ctx, ParkInput) error
func (s *ConflictReviewService) Get(ctx, uuid) (*ConflictReview, error)
func (s *ConflictReviewService) List(ctx, filter) ([]*ConflictReview, error)
func (s *ConflictReviewService) Resolve(ctx, uuid, ResolveInput) error
func (s *ConflictReviewService) Dismiss(ctx, uuid, DismissInput) error
```

`Resolve` applies the resolution to the **current** existing record
(not the snapshot — the snapshot is for debug/audit). For each
action:

- **`keep_existing`**: incoming conflicting fields discarded;
  `incomingActivities` still go through `ActivityService.Create`
  (engagement is real even if anagrafica differs).
- **`take_incoming`**: incoming conflicting fields overwrite via
  `PersonRepo.Update` / `OrgRepo.Update`; activities flushed.
- **`manual_merge`**: applies `field_overrides` per-key. `$append`
  for additive-array fields, literal value for scalars.
- **`dismiss`**: incoming row + its activities discarded; review
  status moves to `dismissed`.

After every state change `Resolve` / `Dismiss` calls
`CountPendingForJob`; if zero, transitions the parent job from
`paused_for_review` to `running`, then to `done` once the worker's
second-pass cleanup finishes (or `failed` if the resolution
application itself errors).

### 4.4 Activity auto-emission (Phase-2 deferred work)

Three listeners registered in `module.Init()`:

```go
m.PersonService.RegisterUpdateListener(emitPersonTagDiff)
m.MembershipService.RegisterCreateListener(emitImported)
m.MembershipService.RegisterUpdateListener(emitMerged)
```

Each listener:

1. Receives `(ctx, before, after)` (or `(ctx, created)`).
2. Diffs the relevant field (tags for Person, importJobUuid presence
   for Membership-create, conflict-review reference for
   Membership-update).
3. Calls `ActivityService.Create` with the right `kind` + `source:
   "system"`. The Create computes dedupKey, fires its own listener
   chain (which includes `ScoreService.OnActivityInserted`).
4. Listener failures **never block** the parent write — the
   listener pattern logs at WARN and returns nil (mirrors Phase 2's
   pattern in `ScoreService.OnActivityInserted`).

The diff helpers live in `services/activity_emit.go`:

```go
func tagDiff(before, after []string) struct{ Added, Removed []string }
func extractImportJobUUID(membership *Membership) string  // returns "" if not from importer
```

### 4.5 Cedar policy coverage

`marketing.conflict.resolve` covered by the platform `super_admin`
+ `administrator` wildcards in `internal/core/authz/cedar/policies/platform.cedar`
(same fold-through as every other marketing permission). Manual
review during merge — `policycoverage` analyzer still doesn't
traverse the extracted addon module ([[project_policycoverage_addon_scan]]).

### 4.6 Service tests

`services/conflict_review_service_test.go` covers:

- `Park` writes a review row with `status: pending` and the parent
  job transitions to `paused_for_review` on first park.
- `Resolve(keep_existing)` does not overwrite the existing record;
  incoming activities are still inserted.
- `Resolve(take_incoming)` overwrites conflicting fields; existing
  non-conflicting fields preserved.
- `Resolve(manual_merge)` applies per-field overrides correctly,
  including `$append` for additive arrays.
- `Dismiss` discards incoming entirely.
- `Resolve` on a non-pending review returns
  `ErrReviewAlreadyResolved` without touching state.
- Resolving the last pending review for a job transitions it
  to `running → done`.

---

## 5. Excel adapter (PR-3)

### 5.1 Package layout

`importers/excel/`:

```
excel/
├── adapter.go       // Importer interface impl
├── reader.go        // excelize wrapper, sheet picker, cell normalization
├── reader_test.go
└── adapter_test.go
```

### 5.2 Adapter contract

`adapter.go` implements the `Importer` interface from
`importers/importer.go` (Phase 1):

```go
func (a *ExcelAdapter) Describe() Descriptor {
    return Descriptor{
        Name: "excel",
        Capabilities: Capabilities{
            Multipart:        true,    // file upload
            DryRunPreview:    true,
            SheetSelection:   true,    // new capability flag
            ColumnMapping:    true,
            ActivityEmission: false,   // engagement-CSV is csv-only
        },
        ConfigSchema: ExcelConfigSchema,  // sheet name + 1-based header row
    }
}
```

A `Capabilities.SheetSelection` flag tells the import wizard to
render the sheet picker. The Phase-1 CSV adapter sets it false.

### 5.3 Dry-run

`DryRun` opens the workbook, lists sheet names + row counts +
detected header row for each sheet, returns:

```go
type ExcelPreview struct {
    Sheets []SheetPreview `json:"sheets"`
}

type SheetPreview struct {
    Name      string   `json:"name"`
    RowCount  int      `json:"rowCount"`
    Headers   []string `json:"headers"`   // first row, raw
    SampleRow []string `json:"sampleRow"` // second row, raw
}
```

The UI picks the sheet on Step 2; mapping flows through the same
canonical-field set as CSV. No new pipeline branch — the adapter
yields `CanonicalRecord`s identical to CSV's output, then the
generic pipeline does dedup → conflict → commit unchanged.

### 5.4 Cell normalization

`excelize` returns cells typed (number/string/date/bool). The
reader normalizes everything to `string` using:

- Numbers → strconv-formatted (no scientific notation for VAT-
  sized integers).
- Dates → RFC3339.
- Bools → `"true"` / `"false"`.

`reader_test.go` covers an .xlsx fixture with mixed cell types
that the operator-perceived value matches the post-normalization
string.

### 5.5 Tests

`adapter_test.go` covers:

- DryRun lists every sheet.
- Run on a single-sheet workbook produces same `CanonicalRecord`s
  as the CSV adapter on the same data.
- Header-row index respected (e.g. data starts on row 3 because
  rows 1-2 are an export banner).
- Malformed xlsx → `ImportJobStatusFailed` with `reason: "file_parse_error"`.

---

## 6. Odoo JSON-2 adapter (PR-4)

### 6.1 Package layout

`importers/odoo/`:

```
odoo/
├── adapter.go             // Importer interface impl
├── client.go              // JSON-2 HTTP client (api key + db)
├── client_test.go         // httptest server fixtures
├── res_partner_mapper.go  // Odoo res.partner → CanonicalRecord
├── engagement.go          // (light) Odoo CRM activity → marketing Activity
└── adapter_test.go
```

### 6.2 JSON-2 client

Odoo 19.0 ships a REST-style External JSON-2 API. The client
authenticates by including `X-API-Key` + `X-DB-Name` headers (or
the equivalent per the released spec — confirm against the actual
API doc during PR-4 wiring). All requests are JSON POST to
`/api/v2/<model>/<action>`.

```go
type Client struct {
    BaseURL    string
    Database   string
    APIKey     string
    HTTPClient *http.Client
}

func (c *Client) SearchRead(ctx, model string, opts SearchReadOpts) (SearchReadResult, error)
```

`SearchRead` is the single entry point this adapter needs: paginated
fetch of `res.partner` rows with the field list the canonical mapper
expects. Pagination via `offset` + `limit`; pages of 200 are a
sensible default.

Auth secrets (`odoo.base_url`, `odoo.database`, `odoo.api_key`)
flow through `ModuleConfigService` per-environment secrets, encrypted
at rest via the same AES-256-GCM path every other addon uses.
ConfigService caches in Redis (30s TTL) so a multi-page Odoo pull
doesn't hammer Mongo for the credential lookup.

### 6.3 res.partner mapping

A single Odoo `res.partner` row can be a person OR an organization
OR a hybrid (e.g. a contact under a parent company). Mapping rules:

- `is_company == true` → `Organization` (legal_name = `name`,
  `vat` = `vat`, etc.)
- `is_company == false && parent_id != null` → `Person` + create
  `Membership` linking to the parent `Organization`.
- `is_company == false && parent_id == null` → standalone `Person`.

`category_ids` (Odoo's tags) → `marketing_tags`. Auto-create policy:
the mapper looks up by Odoo's category name; if not found in
`marketing_tags`, creates a new tag with `source: "odoo:<categoryId>"`
in the slug. Subsequent imports re-use it.

`comment` (Odoo HTML notes) → `Person.notes` after a strip-HTML
pass.

### 6.4 Engagement emission (light)

Odoo CRM module exposes `mail.message` (per-partner thread history).
The adapter pulls the last 90 days of `mail.message` rows whose
`partner_ids` include the imported partner and emits one Activity
per message with:

- `kind: email_sent` if `message_type == "email"` and
  `direction == "outbound"`.
- `kind: note_added` if `message_type == "comment"`.
- `occurred_at`: Odoo's `date`.
- `external_id`: Odoo `message_id`, so `dedupKey` is stable on
  re-import.

Engagement emission is **opt-in** via the import config:
`include_engagement = true` on the Odoo import-job request. Default
false — for a first-time anagrafica pull you usually don't want the
full message history.

### 6.5 Engagement-CSV column recognition

`importers/csv/` gains a `detectActivityColumns()` step that runs
before the canonical-record build:

```go
var engagementColumns = map[string]models.ActivityKind{
    "email_opened":      models.KindEmailOpened,
    "email_clicked":     models.KindEmailClicked,
    "email_bounced":     models.KindEmailBounced,
    "email_unsubscribed": models.KindEmailUnsubscribed,
    "email_complained":  models.KindEmailComplained,
    "form_submitted":    models.KindFormSubmitted,
    "page_visited":      models.KindPageVisited,
    "event_attended":    models.KindEventAttended,
}
```

When the CSV has both a `primary_email` (or other person-key)
column **and** one of these engagement columns + an `occurred_at`
column, each non-empty engagement cell emits an Activity row
through `ActivityService.Create`. `external_id` = CSV row index +
column name, so re-import is idempotent.

### 6.6 Tests

`client_test.go` uses `httptest.NewServer` to stub Odoo's JSON-2
endpoints:

- Auth header round-trip.
- Paginated `res.partner` fetch (offset/limit honored, last page
  detection).
- 401 surfaces as `ErrOdooAuth` (operator-visible).
- 5xx triggers exponential-backoff retry (3 attempts, 1s / 2s / 4s).

`adapter_test.go` covers:

- res.partner → CanonicalRecord round-trip (organization,
  person-under-parent, standalone person).
- category_ids → tag auto-create.
- DryRun returns row count without writing.
- `include_engagement = true` produces the right activity kinds.

---

## 7. API surface (PR-1 + PR-2)

### 7.1 New endpoints

| Method | Path | Permission | Notes |
| ------ | ---- | ---------- | ----- |
| `POST` | `/v1/marketing/imports` (existing) | `marketing.import.run` | Now returns `202 Accepted` + `{jobUuid}`. Body unchanged. Idempotency: same multipart body → same job UUID via SHA-256 hash on the file bytes + mapping. Optional `Idempotency-Key` header overrides. |
| `GET` | `/v1/marketing/conflict-reviews` | `marketing.contact.read` | List with `?status=pending&importJobUuid=...&targetKind=...` filters. Pagination via `limit` + `skip`. |
| `GET` | `/v1/marketing/conflict-reviews/{uuid}` | `marketing.contact.read` | Single review with full payload. |
| `POST` | `/v1/marketing/conflict-reviews/{uuid}/resolve` | `marketing.conflict.resolve` | Body: `{action, fieldOverrides?, notes?}`. Action enum from §8.3. |
| `POST` | `/v1/marketing/conflict-reviews/{uuid}/dismiss` | `marketing.conflict.resolve` | Body: `{notes?}`. Convenience over `resolve` with action=`dismiss`. |

### 7.2 Idempotency on POST /imports

The hash-based idempotency key is the same pattern Phase 1's
sales-prospect endpoint uses. The middleware sits in
`importers/idempotency.go`:

- Computes `sha256(file_bytes || canonical_mapping_json)` if no
  explicit `Idempotency-Key` header.
- Looks up `marketing_import_jobs` for a row with that key in the
  last 24 hours **for this tenant**.
- If found: return the existing `jobUuid` (202 + body unchanged).
- If not: insert a new job in `queued` state, enqueue, return its
  UUID.

This prevents accidental double-submission via the wizard (which
e.g. retries on a 504).

### 7.3 OpenAPI dump

Run `make openapi-dump` after PR-1 and after PR-2 (anywhere the
route surface widens). PR-5 includes a final regen as a hygiene
step.

---

## 8. Frontend (PR-5)

### 8.1 Module manifest extension

`frontend-admin/src/modules/marketing.tsx`:

```tsx
const ReviewsPage = lazy(() => import('pages/marketing/reviews'));

// Add to routes():
{ path: 'marketing/reviews', element: wrap(<ReviewsPage />, 'marketing-reviews') }
```

The "Reviews" sidebar item comes from the backend nav
(`module.NavItems()` adds it in PR-1).

### 8.2 Pages

`pages/marketing/reviews/index.tsx` — review queue list:

- Filters: status (pending / resolved / dismissed / all), target
  kind (person / organization / all), import job UUID (free text).
- Columns: created, target kind, existing record (linked to
  contact detail), conflicts count, status, actions.
- Row click → opens `ReviewResolverModal`.
- Bulk action: select rows + "Resolve all as keep_existing" or
  "Dismiss all". Confirms via a typed confirmation modal.

`pages/marketing/reviews/ReviewResolverModal.tsx` — side-by-side
resolver:

- Left column: existing record (snapshot at time of conflict).
- Right column: incoming payload.
- Per-conflict row: field name + existing value + incoming value
  + radio (`keep` / `take`). Radio selection drives the
  `field_overrides` map.
- Action buttons: `Keep existing` (overrides all to existing),
  `Take incoming` (overrides all to incoming), `Save manual
  merge` (uses the per-field radio state), `Dismiss`.
- POST `/conflict-reviews/{uuid}/resolve` (or `/dismiss`) on
  submit; on success closes the modal + invalidates the
  `MarketingConflictReview` tag.

`pages/marketing/imports/wizard/` — adapter picker on Step 1:

- Radio: `csv` (default) / `excel` / `odoo`.
- Selecting `excel`: file upload + sheet picker (Step 2 calls
  `POST /imports/dry-run` first to enumerate sheets, then the
  operator picks one before mapping).
- Selecting `odoo`: no file upload; instead a connection-config
  form (base URL, database, API key — all live in
  `module_configs` so the form pre-fills from the active
  environment).

`pages/marketing/imports/list.tsx` — gains a "Reviews" badge
column on rows where the job is `paused_for_review`. Clicking the
badge deep-links to `/marketing/reviews?importJobUuid=<uuid>`.

### 8.3 RTK Query slice extension

`store/api/marketingApi.ts`:

```ts
listConflictReviews:    builder.query<...>(...)
getConflictReview:      builder.query<...>(...)
resolveConflictReview:  builder.mutation<...>(...)
dismissConflictReview:  builder.mutation<...>(...)
runOdooImport:          builder.mutation<...>(...)  // sugar over POST /imports with body shape
```

New tag type: `MarketingConflictReview`. The resolve / dismiss
mutations invalidate `MarketingConflictReview` + `MarketingImportJob`
(so the list refresh + job-state badge update together).

### 8.4 URL-sync

Reviews page uses `?status=pending` / `?importJobUuid=...` /
`?targetKind=person` via `useSearchParams`. Modal opens via
`?reviewUuid=<uuid>` (deep-linkable, copy-pasteable URL to a
specific resolver).

### 8.5 i18n

New namespaces:

```
marketing.reviews.*       — queue page + filters + actions
marketing.reviews.modal.* — resolver modal labels
marketing.imports.adapter.* — adapter picker labels (csv/excel/odoo)
marketing.imports.odoo.*    — odoo connection form labels
```

EN + IT in `frontend-admin/src/locales/en.json` + `it.json`. Follow
the same convention Phases 1-2 used; the parity test
(`locales/parity.test.ts`) catches missing IT keys at lint time.

---

## 9. Docs + CI hygiene (PR-5)

### 9.1 OpenAPI dump

`make openapi-dump` regenerates `backend/openapi/enterprise.json`.
Commit alongside the route additions. `make openapi-check` gates the
PR.

### 9.2 CLAUDE.md updates

`backend/internal/addons/marketing/CLAUDE.md`:

- Status block: flip Phase 3 from "future" to "shipped".
- Collections table: add `marketing_conflict_reviews` row.
- HTTP surface: list the 2 new permissions + the 4 new routes.
- Frontend section: enumerate the `/marketing/reviews` page +
  modal + adapter picker.
- Importer pipeline section: rewrite to describe the async runner +
  the worker queue + the review-queue flow. Strike the "synchronously
  inside the handler" sentence — the response is now `202 + jobUuid`.
- Adapter list: add `excel` and `odoo`.

`docs/plans/marketing-addon/Orkestra_marketing_addon.md` — no
changes (it's the canonical design doc, frozen at the design step).

### 9.3 Memory updates

Add a `project_marketing_addon_phase3.md` to
`/home/tore/.claude/projects/-home-tore-orkestra/memory/` mirroring
Phase 1 + 2's structure. Include: deferred work (Phase 4 card
lifecycle, Phase 5 ESP webhooks, fuzzy-match), Odoo 19.0 JSON-2
spec dependency, async-runner crash-recovery semantics. Pointer
under "Active work" in MEMORY.md.

### 9.4 CI gates checklist

- `make ci-backend` green (lint, tenantscope baseline check, vuln,
  tests, enterprise build, openapi-check).
- `make ci-frontend-admin` green (typecheck, eslint, tests,
  audit, build).
- Profile builds (`make build-starter`, `-saas`, `-enterprise`) all
  green — the marketing addon's `Dependencies()` declaration is
  unchanged from Phase 1 (`none`), so no profile changes needed.

---

## 10. Test plan

Per PR:

**PR-1 (data layer + async runner)**:
- Repo unit tests (§3.6).
- Worker boot recovery: seed jobs in `queued` / `running` /
  `paused_for_review`, restart, assert state transitions.
- Idempotency middleware: identical multipart body → identical
  jobUUID within 24h; differs on different tenant ctx.
- HTTP test: `POST /v1/marketing/imports` returns 202 + jobUuid
  immediately; the GET on the job UUID flips through `queued →
  running → done` over time.

**PR-2 (review service + soft-match + auto-emission)**:
- Service unit tests (§4.6).
- Soft-match table-driven: 8 fixtures covering person first+last
  variants + 4 fixtures for org legal_name normalization.
- Auto-emission integration: PATCH `/v1/marketing/persons/{uuid}`
  with new tags → an `tag_added` activity appears in `GET
  /v1/marketing/persons/{uuid}/activities` within the same request
  (eager listener fires synchronously).
- End-to-end: CSV with 100 rows / 5 blocking conflicts → import
  job enters `paused_for_review` after first pass; 5 reviews
  pending; resolve all 5 → job transitions to `done`; the auto-
  merged commits + the resolved-keep / resolved-take rows are
  visible in `marketing_persons`.

**PR-3 (Excel adapter)**:
- Adapter unit tests (§5.5).
- End-to-end: same CSV fixture re-exported as .xlsx → same
  dedup + commit result.

**PR-4 (Odoo adapter)**:
- Client unit tests (§6.6) against `httptest` stubs.
- res.partner mapping table-driven (10 fixtures covering the
  three shape branches).
- Engagement opt-in: same fixture, `include_engagement=true` vs
  false → activity count differs as expected.

**PR-5 (frontend)**:
- Vitest mount tests for `ReviewResolverModal` + the wizard's
  adapter picker.
- Manual smoke test of the full review flow: trigger a conflict
  via the wizard, resolve it via the modal, verify the contact
  detail reflects the merged state + the timeline carries the
  `merged` activity.
- Manual smoke test of the Odoo wizard step: connection-config
  form submits → dry-run preview shows partner count → run →
  poll-based progress UI moves through states.

End-to-end across PRs:

- Re-import the same CSV with conflicts → identical reviews
  appear (dedupKey on Activity + the `import_jobs.idempotencyKey`
  prevent double-commit).
- Time-travel a score after a review resolves → the engine sees
  the new activities at the right `occurred_at`.

---

## 11. Risks + open questions

| Risk | Likelihood | Mitigation |
| ---- | ---------- | ---------- |
| Odoo 19.0 External JSON-2 API spec drift | Medium — released recently | PR-4 starts with a discovery pass: fetch a real `/api/v2/res.partner/search_read` against a live tenant and confirm the headers + body shape. If the spec disagrees with §6.2's sketch, the client surface changes; the adapter contract upstream stays stable. |
| Worker crash mid-pipeline leaves partial commits | Low | `sources[]` audit lets ops trace what landed; `runner_crash` `failed` state surfaces the job. Full transactional pipeline is out of scope (would require a 2-phase write across 5 collections). |
| Soft-match false positive rate too high in real data | Medium | Soft-match always routes to review queue — operator decides. Phase 5 will tune thresholds against the cumulative resolved-vs-dismissed ratio. |
| Operator gets review-fatigue from large imports | Medium | Bulk actions (§8.2) + "Resolve all as keep_existing for this job" are the escape hatch. |
| Idempotency-key collision across tenants | Low | Hash includes tenant ID; no cross-tenant collision possible. |
| Phase-2 auto-emission listener loop (Activity insert → listener → ScoreService → ...) | Low | Listener pattern fires in the same goroutine but `ActivityService.Create` is the only insert point and it doesn't itself listen on activity inserts. Audit during PR-2 review. |
| Worker queue full at peak | Low | Returns 503 to the caller; UI shows a "queue full, retry shortly" toast. Operators tune `MARKETING_IMPORT_QUEUE_BUFFER`. |
| Odoo API key in plaintext via UI | Mitigated | ConfigService encrypts at rest via AES-256-GCM; the wizard form sends to the existing per-environment secret endpoint which never returns the raw value back to the frontend. |
| Listener panics on auto-emission corrupt downstream state | Low | Phase 2's listener pattern already has panic-recover. Phase 3 listeners inherit the same wrapping. |

Open questions to confirm during execution (mark resolved as
they're addressed in the PR commits):

1. Odoo 19.0 JSON-2 actual auth header names — `X-API-Key` is the
   working hypothesis; PR-4 discovery confirms or replaces.
2. Whether `mail.message` pull during Odoo import should be its
   own job (separate progress) or inline. Working hypothesis:
   inline, with the engagement-opt-in flag.
3. Whether bulk-resolve should be a single endpoint
   (`POST /v1/marketing/conflict-reviews/bulk-resolve`) or a
   client-side loop over the per-review endpoint. Working
   hypothesis: client-side loop for now (simpler RBAC story);
   batch endpoint if N>50 reviews becomes the common case.

---

## 12. Stepwise execution checklist

PR-1 (data layer + async runner):
- [ ] `models/collections.go`: add `ConflictReviewsCollection`.
- [ ] `models/conflict_review.go`: new file with all types + Validate.
- [ ] `models/import_job.go`: add `ImportJobStatusPausedForReview`.
- [ ] `repository/conflict_review_repo.go`: full surface + tests.
- [ ] `repository/import_job_repo.go`: add `ListByStatus` + `MarkRunnerCrash`.
- [ ] `importers/worker.go`: Worker + queue primitives.
- [ ] `importers/idempotency.go`: hash-based dedup middleware.
- [ ] `module.go`: wire ConflictReviewRepo (Repo only — Service lands PR-2) + Worker + ConfigSchema + NavItems + Permissions + Start/Stop.
- [ ] `handlers/imports_handler.go`: rewrite to 202 + jobUuid.
- [ ] `make openapi-dump`, commit.
- [ ] `make ci-backend` green.

PR-2 (review service + soft-match + auto-emission):
- [ ] `importers/match/softmatch.go`: SoftMatchPerson + SoftMatchOrganization.
- [ ] `importers/pipeline.go`: `commitOrPark` step.
- [ ] `services/conflict_review_service.go`: full surface + tests.
- [ ] `services/activity_emit.go`: emit helpers.
- [ ] `services/person_service.go`: `RegisterUpdateListener`.
- [ ] `services/membership_service.go`: `RegisterCreateListener` + `RegisterUpdateListener`.
- [ ] `module.go::Init`: wire ConflictReviewService + 3 listeners.
- [ ] `handlers/conflict_reviews_handler.go`: list/get/resolve/dismiss.
- [ ] `make openapi-dump`, commit.
- [ ] `make ci-backend` green.

PR-3 (Excel adapter):
- [ ] `importers/excel/adapter.go` + `reader.go` + tests.
- [ ] `importers/importer.go`: `Capabilities.SheetSelection` flag.
- [ ] `module.go::Init`: register `excel` in the adapter catalog.
- [ ] `make ci-backend` green.

PR-4 (Odoo adapter + engagement-CSV):
- [ ] Discovery: confirm Odoo 19.0 JSON-2 auth shape against a live tenant.
- [ ] `importers/odoo/client.go` + tests.
- [ ] `importers/odoo/res_partner_mapper.go` + tests.
- [ ] `importers/odoo/engagement.go` + tests.
- [ ] `importers/odoo/adapter.go`.
- [ ] `importers/csv/engagement.go`: `detectActivityColumns` + emission.
- [ ] `module.go::Init`: register `odoo` in the adapter catalog;
       Odoo config schema declared on the module's per-environment
       ConfigSchema branch.
- [ ] `make ci-backend` green.

PR-5 (frontend + docs):
- [ ] `types/marketing.ts`: ConflictReview, ConflictField, Resolution, ImportJobStatus extension.
- [ ] `store/api/baseApi.ts`: MarketingConflictReview tag type.
- [ ] `store/api/marketingApi.ts`: 4 new endpoints + hooks.
- [ ] `pages/marketing/reviews/index.tsx`: queue page.
- [ ] `pages/marketing/reviews/ReviewResolverModal.tsx`: modal.
- [ ] `pages/marketing/imports/wizard/`: adapter picker step + Odoo connection form.
- [ ] `pages/marketing/imports/list.tsx`: "Reviews" badge column.
- [ ] `modules/marketing.tsx`: lazy ReviewsPage + route.
- [ ] `locales/en.json` + `it.json`: 4 new namespaces.
- [ ] `backend/internal/addons/marketing/CLAUDE.md`: Phase 3 flip.
- [ ] `make openapi-dump`, commit (final regen).
- [ ] `make ci-frontend-admin` + `make ci-backend` green.
- [ ] Memory file + MEMORY.md pointer.
