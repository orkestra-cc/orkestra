# Jobs framework Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give every Orkestra module a declarative background-job facility (interval/cron/manual schedules, one run at a time across replicas, durable retry, run log, operator UI) instead of hand-written tickers in `Start()`.

**Architecture:** SDK package `backend/pkg/sdk/jobs` (schedule types, own 5-field cron parser, `Definition`, `Run`, `Catalog`) + optional sub-interface `module.HasJobs` collected by `ModuleRegistry` after `InitAll` into a `*jobs.Catalog` published as `module.ServiceJobCatalog`. A new core module `jobs` (`backend/internal/core/jobs`, load order after `logging`) owns the Mongo state/lease/run-log collections, the per-replica scheduler loop, the Tier-1 admin API and the `/admin/modules/jobs` console workspace.

**Tech Stack:** Go 1.26, Huma v2, mongo-driver, Prometheus client (already in go.mod); React 19 + RTK Query + TanStack Table in `frontend-admin`.

**Spec:** decisions J1–J22, recorded in ADR-0024 (`docs/adr/0024-core-jobs-module.md`). Amendment: permission keys are `system.jobs.read` / `system.jobs.manage` (precedent `system.compliance.*`, covered by `system_actions.cedar`).

## Global Constraints

- No new third-party dependency in `backend/go.mod` (spec J5: cron parser is ours).
- `pkg/sdk/**` imports nothing from `backend/internal/` (`grep -rn '"github.com/orkestra/backend/internal' backend/pkg/sdk` must stay empty).
- The `Module` interface and `module.RedisClient` are not widened; `HasJobs` is a new optional sub-interface.
- Every commit carries `Prop: upstream` and `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`; conventional commit subjects.
- Collections: `jobs_states`, `jobs_runs` (module-prefixed, see `orkestra-mongo-collection-naming` skill).
- Defaults (spec): `Timeout` 10m (max 6h); `Retry.MaxAttempts` 1, `Backoff` 1m, `MaxBackoff` 1h, jitter ±20%; `Every` minimum 1m; lease TTL 2m renewed every 30s; poll ≤10s; `maxConcurrentRuns` 4; `runRetentionDays` 30; `timezone` `UTC`; stop grace 30s; error text truncated to 2 KiB; details ≤32 keys, strings ≤256 chars.
- Job key `<module>.<name>`, name regex `^[a-z][a-z0-9_]{0,62}$`.
- Tier 1 only: routes on `ri.Operator.ProtectedRouter` with `RequireSystemPermission`.
- Never start servers by hand; gate only with `ci-gate make ci-all` (VM rules in `/home/tore/AGENTS.md`).

## Review Focus

1. Two replicas polling the same due job at the same instant → exactly one claims it (atomic filter on lease + due), the other sees no document. Test in Task 4 with two engines on one collection.
2. A replica dies mid-run (lease not renewed) → after TTL another replica marks the old run `abandoned` and re-runs per retry policy; a live run whose renew fails has its ctx cancelled. Tests in Task 4.
3. Operator edits a job while it is running → the run's completion write must not clobber the override (completion `$set`s only run fields, filtered on `lease.runId`). Test in Task 4.
4. Cron edge cases: DST gap/overlap in `Europe/Rome`, `0 0 31 2 *` (never fires → Validate error "never matches within 5 years"), DOM+DOW OR semantics, `*/0` rejected. Tests in Task 1.
5. Code default changes after an install has state (new default schedule, no override) → reconcile recomputes `nextRunAt`; with an override nothing changes. Test in Task 4.

---

### Task 1: Cron parser and schedules (`pkg/sdk/jobs`)

**Files:**
- Create: `backend/pkg/sdk/jobs/cron.go`, `backend/pkg/sdk/jobs/schedule.go`, `backend/pkg/sdk/jobs/doc.go`
- Test: `backend/pkg/sdk/jobs/cron_test.go`, `backend/pkg/sdk/jobs/schedule_test.go`

**Interfaces — Produces:**
```go
type CronExpr struct{ /* bitsets minute(0-59) hour(0-23) dom(1-31) month(1-12) dow(0-6), domStar, dowStar bool, src string */ }
func ParseCron(expr string) (*CronExpr, error)
func (c *CronExpr) Next(after time.Time, loc *time.Location) (time.Time, bool) // strictly after `after`; false if none within 5 years
func (c *CronExpr) String() string

type ScheduleKind string // "every" | "cron" | "manual"
type Schedule struct{ /* kind, every time.Duration, cron *CronExpr, raw string, err error */ }
func Every(d time.Duration) Schedule
func Cron(expr string) Schedule       // parse error kept, reported by Validate
func Manual() Schedule
func (s Schedule) Kind() ScheduleKind
func (s Schedule) Every() time.Duration
func (s Schedule) Expr() string
func (s Schedule) Validate() error     // every < 1m, cron parse error, cron never matching within 5y (probe from 2026-01-01 UTC), zero Schedule{}
func (s Schedule) Next(after time.Time, loc *time.Location) (time.Time, bool) // every: after+d; cron: CronExpr.Next; manual: false
func (s Schedule) String() string      // "every 1h0m0s" | "cron 0 2 * * *" | "manual"
```
Grammar: 5 space-separated fields; `*`, `a`, `a-b`, `a-b/n`, `*/n`, `a/n` (= a-max/n), comma lists; month names `jan…dec`, weekday names `sun…sat` (case-insensitive); weekday `7` = Sunday; macros `@yearly|@annually`, `@monthly`, `@weekly`, `@daily|@midnight`, `@hourly`. Vixie rule: if both DOM and DOW are restricted (neither field is `*`), a day matches when either matches. Next algorithm: robfig-style field walk in `loc` wall-clock (advance month → day → hour → minute, resetting lower fields), seconds always 0. DST: a wall time that does not exist is skipped (the walk moves on to the next matching wall time that exists); an ambiguous wall time fires once (first occurrence).

- [ ] **Step 1: Write failing tests** — table-driven `TestParseCron_Valid` (e.g. `"*/15 9-17 * * mon-fri"`, `"0 2 * * *"`, `"@daily"`, `"0 0 1 jan *"`, `"0 0 * * 7"`) and `TestParseCron_Invalid` (`""`, `"* * * *"`, `"60 * * * *"`, `"*/0 * * * *"`, `"5-1 * * * *"`, `"* * * foo *"`, `"@reboot"`), each invalid returning an error mentioning the field. `TestCronNext`: `"0 2 * * *"` after `2026-03-07T02:00:00Z` UTC → `2026-03-08T02:00:00Z`; `"30 2 * * *"` in `Europe/Rome` after `2026-03-28T12:00` → `2026-03-30T02:30+02:00` (29 March 02:30 does not exist); `"30 2 * * *"` Rome after `2026-10-24T12:00` → exactly one firing on 25 Oct at `02:30+02:00` and the next at `2026-10-26T02:30+01:00`; `"0 0 13 * fri"` matches both the 13th and every Friday; `"0 0 31 2 *"` → `Next` returns false and `Cron(...).Validate()` errors "never". `TestSchedule`: `Every(30*time.Second).Validate()` errors; `Every(time.Hour).Next(t, nil) == t.Add(time.Hour)`; `Manual().Next` false; `Schedule{}.Validate()` errors; `String()` values above.
- [ ] **Step 2:** `cd backend && go test ./pkg/sdk/jobs/ -run 'Cron|Schedule' -v` → FAIL (undefined).
- [ ] **Step 3:** Implement. `nil` loc means UTC.
- [ ] **Step 4:** Same command → PASS; also `go test -race ./pkg/sdk/jobs/`.
- [ ] **Step 5: Commit** `feat(sdk/jobs): schedules and a dependency-free 5-field cron parser`.

### Task 2: Definitions, runs, catalog and `module.HasJobs`

**Files:**
- Create: `backend/pkg/sdk/jobs/definition.go`, `backend/pkg/sdk/jobs/run.go`, `backend/pkg/sdk/jobs/catalog.go`
- Modify: `backend/pkg/sdk/module/module.go` (add `HasJobs` + `JobsOf`), `backend/pkg/sdk/module/services.go` (add `ServiceJobCatalog`), `backend/pkg/sdk/module/registry.go` (register empty catalog before the Init loop next to `reportReg`; `registerJobs(catalog)` after `registerReportContributions`; same for the module recovered in `RetryInit`)
- Test: `backend/pkg/sdk/jobs/definition_test.go`, `backend/pkg/sdk/jobs/run_test.go`, `backend/pkg/sdk/jobs/catalog_test.go`, `backend/pkg/sdk/module/registry_jobs_test.go`
- Docs: `backend/pkg/sdk/AGENTS.md` package map row for `jobs/` and the `HasJobs` sub-interface count.

**Interfaces — Consumes:** Task 1 `Schedule`. **Produces:**
```go
type RetryPolicy struct{ MaxAttempts int; Backoff, MaxBackoff time.Duration }
type Definition struct {
    Name, Title, Description string
    Schedule          Schedule
    DisabledByDefault bool
    Timeout           time.Duration
    Retry             RetryPolicy
    SkipMissed        bool
    Run               func(ctx context.Context, run *Run) error
}
func (d Definition) Normalized() Definition   // fills defaults (Timeout 10m, MaxAttempts 1, Backoff 1m, MaxBackoff 1h)
func (d Definition) Validate() error          // name regex, Title non-empty, Schedule.Validate, Run != nil, 0 < Timeout ≤ 6h, MaxAttempts 1..10, Backoff ≤ MaxBackoff
func (p RetryPolicy) Delay(attempt int, jitter func() float64) time.Duration // attempt≥1: Backoff*2^(attempt-1) capped at MaxBackoff, times (0.8+0.4*jitter())

type Trigger string // TriggerSchedule "schedule", TriggerManual "manual", TriggerRetry "retry", TriggerCatchup "catchup"
type Run struct{ /* key, id, trigger, attempt, scheduledFor, details map + mutex, dropped int */ }
func NewRun(key, id string, trigger Trigger, attempt int, scheduledFor time.Time) *Run
func (r *Run) Key() string; ID() string; Trigger() Trigger; Attempt() int; ScheduledFor() time.Time
func (r *Run) Detail(key string, value any)  // concurrency-safe; keeps ≤32 keys; accepts string (truncated to 256 runes), bool, all int/uint kinds (stored int64), float32/64, time.Time, time.Duration (stored as String()); anything else is dropped and counted
func (r *Run) Details() (map[string]any, int) // copy + dropped count
func Permanent(err error) error; func IsPermanent(err error) bool

type Entry struct{ Key, Module string; Def Definition } // Def normalized
type Catalog struct{ /* mu, map */ }
func NewCatalog() *Catalog
func (c *Catalog) Register(module string, defs []Definition) error // atomic per module: any invalid/duplicate def → error, nothing from that module registered; key = module+"."+Name
func (c *Catalog) List() []Entry          // sorted by Key
func (c *Catalog) Get(key string) (Entry, bool)

// module package
type HasJobs interface{ Jobs() []jobs.Definition }
func JobsOf(m Module) []jobs.Definition
const ServiceJobCatalog ServiceKey = "jobs.catalog" // value *jobs.Catalog
```

- [ ] **Step 1: Failing tests** — `TestDefinitionValidate` (bad name `"Bad-Name"`, empty Title, nil Run, Timeout 7h, MaxAttempts 11 each error; a minimal valid def passes after `Normalized()`); `TestRetryDelay` (Backoff 1m, MaxBackoff 1h, jitter 0.5 → attempt1 1m, attempt2 2m, attempt8 1h); `TestRunDetail` (33rd key dropped, struct value dropped, long string truncated to 256 runes, `-race` concurrent Detail); `TestPermanent` (`errors.Is` chain preserved, `IsPermanent(fmt.Errorf("x: %w", Permanent(e)))` true); `TestCatalogRegister` (keys prefixed, List sorted, a module with one invalid def registers none, second Register of the same module errors); `TestRegistryCollectsJobs` in `registry_jobs_test.go` (a test module implementing `HasJobs` → after `InitAll` the catalog from `deps.Services` lists `testmod.sweep`; a module failing Init contributes nothing; a module with an invalid def is logged and skipped while another module's jobs register).
- [ ] **Step 2:** `go test ./pkg/sdk/jobs/ ./pkg/sdk/module/ -run 'Definition|Retry|RunDetail|Permanent|Catalog|RegistryCollectsJobs'` → FAIL.
- [ ] **Step 3:** Implement; registry logs `"job contribution rejected"` with module + error, then `"Registered job catalog"` with count.
- [ ] **Step 4:** Same → PASS; `go test -race ./pkg/sdk/...`.
- [ ] **Step 5: Commit** `feat(sdk): job definitions, catalog and module.HasJobs`.

### Task 3: `jobs` module data layer

**Files:**
- Create: `backend/internal/core/jobs/models/models.go`, `backend/internal/core/jobs/repository/state_repository.go`, `backend/internal/core/jobs/repository/run_repository.go`
- Test: `backend/internal/core/jobs/repository/*_integration_test.go` (follow the existing Mongo integration-test helper used by `internal/core/logging` or `internal/core/compliance` repository tests; skip when `MONGO_TEST_URI` is unset, like them)

**Interfaces — Produces:**
```go
// models
const StatesCollection = "jobs_states"; const RunsCollection = "jobs_runs"
type ScheduleSpec struct{ Kind string `bson:"kind"`; EverySeconds int64 `bson:"everySeconds,omitempty"`; Cron string `bson:"cron,omitempty"` }
type Override struct{ Enabled *bool `bson:"enabled,omitempty"`; Schedule *ScheduleSpec `bson:"schedule,omitempty"`; Timezone string `bson:"timezone,omitempty"` }
type Lease struct{ Owner, RunID string; Trigger string; Until time.Time; Attempt int; SlotAt *time.Time }
type LastRun struct{ RunID, Status, Error string; StartedAt, FinishedAt time.Time; DurationMs int64 }
type State struct {
    Key string `bson:"_id"`; Module string
    Override *Override; Fingerprint string            // effective schedule+tz+enabled string the nextRunAt was computed from
    NextRunAt, RetryAt, RetrySlotAt *time.Time; Attempt int
    RunRequestedAt *time.Time; RunRequestedBy string
    Lease *Lease; LastRun *LastRun
    Revision int64; UpdatedAt time.Time; UpdatedBy string
}
type RunRecord struct {
    ID string `bson:"_id"`; Key, Module, Trigger, Status, Error, Replica, RequestedBy string
    Attempt int; ScheduledFor *time.Time; StartedAt time.Time; FinishedAt *time.Time; DurationMs int64
    Details map[string]any; DroppedDetails int
}
// statuses: running succeeded failed timed_out abandoned interrupted skipped

// repository (all methods take ctx; `now` passed in, never time.Now() inside)
type StateRepository interface {
    EnsureState(ctx, key, module, fingerprint string, nextRunAt *time.Time, now time.Time) error            // insert if absent ($setOnInsert)
    Get(ctx, key string) (*models.State, error)                                                              // ErrNotFound
    List(ctx) ([]models.State, error)
    Reschedule(ctx, key string, revision int64, fingerprint string, nextRunAt *time.Time, now time.Time) (bool, error) // CAS on revision; clears RetryAt when nextRunAt==nil
    SetOverride(ctx, key string, revision int64, o *models.Override, fingerprint string, nextRunAt *time.Time, by string, now time.Time) (*models.State, error) // ErrRevisionConflict
    RequestRun(ctx, key, by string, now time.Time) (*models.State, error) // ErrAlreadyRunning if lease live, ErrAlreadyRequested if RunRequestedAt set
    Due(ctx, now time.Time, limit int) ([]models.State, error)            // lease free/expired AND (nextRunAt≤now OR retryAt≤now OR runRequestedAt≠nil)
    Claim(ctx, key string, revision int64, lease models.Lease, now time.Time) (*models.State, error) // filter {_id, revision, lease free/expired}; $set lease, $inc revision; returns pre-image or nil when lost
    Renew(ctx, key, runID string, until time.Time) (bool, error)          // filter lease.runId
    Complete(ctx, key, runID string, upd Completion) (bool, error)        // filter lease.runId; unset lease; $set only LastRun, NextRunAt?, RetryAt, RetrySlotAt, Attempt, clears RunRequested* when upd.ClearRequest; $inc revision. Never touches Override/Fingerprint.
}
type Completion struct{ LastRun models.LastRun; SetNext bool; NextRunAt *time.Time; RetryAt, RetrySlotAt *time.Time; Attempt int; ClearRequest bool }
type RunRepository interface {
    Insert(ctx, r *models.RunRecord) error
    Finish(ctx, id, status, errText string, finishedAt time.Time, durationMs int64, details map[string]any, dropped int) error
    MarkIfRunning(ctx, id, status string, finishedAt time.Time) error   // abandoned/interrupted only when still "running"
    List(ctx, key, status string, before *RunCursor, limit int) ([]models.RunRecord, *RunCursor, error) // startedAt desc, _id desc; limit clamped 1..100
    DeleteOlderThan(ctx, cutoff time.Time) (int64, error)
}
type RunCursor struct{ StartedAt time.Time; ID string } // opaque base64 JSON on the wire
var ErrNotFound, ErrRevisionConflict, ErrAlreadyRunning, ErrAlreadyRequested error
```
Indexes (declared later in `Collections()`, Task 5): `jobs_states` `{nextRunAt:1}`, `{retryAt:1}` sparse, `{runRequestedAt:1}` sparse; `jobs_runs` `{key:1, startedAt:-1, _id:-1}`, `{startedAt:1}`, `{key:1, status:1, startedAt:-1}`.

- [ ] **Step 1: Failing integration tests** — `TestClaimIsExclusive` (two goroutines `Claim` same revision → one non-nil); `TestClaimAfterLeaseExpiry` (Until in the past → claimable, returns pre-image with old Lease.RunID); `TestCompleteKeepsOverride` (SetOverride between Claim and Complete → Complete succeeds, Override intact, revision advanced twice); `TestCompleteLostLease` (wrong runID → false); `TestRequestRunConflicts`; `TestDueSelection` (disabled job with nil NextRunAt not due; requested one due; leased one not due); `TestRunListCursor` (5 runs, limit 2 → 3 pages, desc order, status filter); `TestDeleteOlderThan`.
- [ ] **Step 2:** `MONGO_TEST_URI=mongodb://127.0.0.1:27317/?directConnection=true go test ./internal/core/jobs/repository/ -v` → FAIL.
- [ ] **Step 3:** Implement with `FindOneAndUpdate` (`options.Before` for Claim, `options.After` for SetOverride/RequestRun).
- [ ] **Step 4:** Same → PASS.
- [ ] **Step 5: Commit** `feat(jobs): state and run repositories with atomic claim`.

### Task 4: Scheduler engine

**Files:**
- Create: `backend/internal/core/jobs/scheduler/engine.go`, `backend/internal/core/jobs/scheduler/effective.go`
- Test: `backend/internal/core/jobs/scheduler/engine_test.go` (fake repos + fake clock for logic), `backend/internal/core/jobs/scheduler/engine_integration_test.go` (real Mongo, two engines)

**Interfaces — Consumes:** Tasks 2–3. **Produces:**
```go
type Clock interface{ Now() time.Time }
type Options struct {
    Replica string; Clock Clock; Logger *slog.Logger
    PollInterval, LeaseTTL, RenewInterval, StopGrace time.Duration // defaults 10s, 2m, 30s, 30s
    MaxConcurrent int                                              // default 4
    DefaultLocation func() *time.Location                         // reads module config timezone live
    ModuleStarted func(module string) bool
    Metrics Metrics                                               // interface {RunFinished(key, status string, d time.Duration); LeaseLost(key string)}; nil = no-op
    Jitter func() float64
}
type Engine struct{ ... }
func New(cat *jobs.Catalog, states repository.StateRepository, runs repository.RunRepository, opt Options) *Engine
func (e *Engine) Reconcile(ctx context.Context) error   // EnsureState for every catalog entry; Reschedule when Fingerprint differs from Effective(entry,state).Fingerprint
func (e *Engine) Tick(ctx context.Context) int           // one poll: Due → claim → launch (bounded); returns runs launched. Exported for tests.
func (e *Engine) Start(ctx context.Context)              // Reconcile then loop: Tick, sleep min(PollInterval, until earliest due)
func (e *Engine) Stop(ctx context.Context) error         // cancel runs, wait ≤StopGrace, MarkIfRunning interrupted, Complete as failed attempt (retry policy applies)
func (e *Engine) Healthy() error                         // non-nil when loop not running or Due/Claim failing >5m

// effective.go
type Effective struct{ Enabled bool; Schedule jobs.Schedule; Location *time.Location; Fingerprint string }
func EffectiveOf(entry jobs.Entry, st *models.State, defLoc *time.Location) (Effective, error)
func NextRun(eff Effective, after time.Time) *time.Time // nil when disabled or manual
```
Claim/run rules (spec J8–J12, J18): trigger precedence manual > retry > schedule; schedule slot older than `now-1m` → `catchup`; `catchup` + `SkipMissed` → insert run `skipped`, Complete with SetNext, no execution. Skip (do not claim) when `!ModuleStarted(module)` or the key is not in the catalog. A pre-image with `Lease.RunID` set and expired → `runs.MarkIfRunning(oldID, "abandoned")` and treat as a failed attempt of that slot before deciding the new trigger. Run ctx = `context.WithTimeout(WithoutCancel(engineCtx), def.Timeout)`, cancelled on renew false/error (`Metrics.LeaseLost`). Recover panics in `Run` → status `failed`, error `"panic: …"`. Outcome: success → `Attempt 0`, clear retry, `SetNext` from `NextRun(eff, now)` unless trigger manual; failure (not `Permanent`, trigger ≠ manual, attempt < MaxAttempts) → `RetryAt = now + Delay(attempt)`, `RetrySlotAt` = slot, `Attempt+1`; scheduled trigger also sets next slot; a scheduled claim clears pending retry. Status `timed_out` when ctx deadline exceeded. Error text truncated to 2048 bytes. Logs with `slog` attrs `job_key`, `run_id`, `trigger`, `attempt`.

- [ ] **Step 1: Failing unit tests** (fake clock): `TestTickRunsDueJob`; `TestDisabledModuleNotClaimed`; `TestRetryScheduledThenSucceeds` (MaxAttempts 3, fails twice → RetryAt at +1m then +2m with jitter 0.5, third succeeds, Attempt reset); `TestPermanentErrorNoRetry`; `TestManualFailureNoRetry`; `TestTimeoutStatus`; `TestPanicRecovered`; `TestCatchupCoalesced` (slot 3 days old → one run, trigger catchup); `TestSkipMissed`; `TestLeaseLossCancelsRun` (Renew returns false → run ctx Done, status failed or interrupted, metric LeaseLost); `TestReconcileDefaultChange` (fingerprint drift without override → NextRunAt recomputed; with override untouched); `TestStopInterrupts`; `TestMaxConcurrent` (6 due, MaxConcurrent 4 → 4 launched).
- [ ] **Step 2: Failing integration test** `TestTwoEnginesOneRun` (two engines, shared Mongo, one due job counting executions with an atomic counter → exactly 1 after both Tick) and `TestAbandonedRunTakenOver` (engine A claims, never renews, clock +3m, engine B Tick → old run `abandoned`, new run executes).
- [ ] **Step 3:** `go test ./internal/core/jobs/scheduler/ -race -v` → FAIL. Implement. → PASS (with `MONGO_TEST_URI` set for the integration file).
- [ ] **Step 4: Commit** `feat(jobs): scheduler engine with lease, retry and catch-up`.

### Task 5: Module, admin API, permissions, metrics, cleanup job, wiring

**Files:**
- Create: `backend/internal/core/jobs/module.go`, `routes.go`, `services/admin_service.go`, `handlers/jobs_handler.go`, `metrics.go`, `cleanup_job.go`, `AGENTS.md`
- Modify: `backend/cmd/server/catalog.go` (insert `jobs.NewModule()` after `logging`; update the comment list), `backend/cmd/server/core_load_order_test.go`, OpenAPI artefacts via the repo's generator (`make openapi` or whatever `make openapi-check` compares — read `backend/AGENTS.md`), tenantscope/logscope/piiscan baselines only if their gates require it
- Test: `services/admin_service_test.go`, `handlers/jobs_handler_test.go`, `module_test.go`

**Interfaces — Consumes:** Tasks 2–4; `module.ServiceJobCatalog`, `module.ServiceModuleStateReader` (`iface.ModuleStateReader`), audit sink resolved lazily per request from `module.ServiceAuditSink` (nil-safe, like other core modules). **Produces:**
- `Permissions()`: `system.jobs.read` ("Read background jobs and their run history"), `system.jobs.manage` ("Change job schedules, enable or disable jobs and run them on demand"), both `Module: "jobs"`, `System: true`.
- `ConfigSchema()`: `timezone` (string, default `UTC`, validated with `time.LoadLocation`), `maxConcurrentRuns` (int 1–32, default 4), `runRetentionDays` (int 1–365, default 30). Follow an existing core module's ConfigSchema/ConfigGroups shape (ADR-0012).
- `Collections()`: indexes listed in Task 3.
- `Jobs()`: its own `runs_cleanup` (`Every(24*time.Hour)`, Title "Delete old job runs", details `deleted`).
- `Start` builds the engine (`Replica` = hostname + short random), runs it in a goroutine with `WithoutCancel`; `Stop` → `engine.Stop`; `HealthCheck` → `engine.Healthy()`.
- Routes (operator ProtectedRouter; read group `RequireSystemPermission("system.jobs.read")`, manage group `RequireSystemPermission("system.jobs.manage")`), tag `Jobs`, `Security bearerAuth administrator`:
  - `GET /v1/admin/jobs` → `{ jobs: [JobView] }`
  - `GET /v1/admin/jobs/{key}` → `JobView`
  - `GET /v1/admin/jobs/{key}/runs?status=&cursor=&limit=` → `{ runs: [RunView], nextCursor }`
  - `PATCH /v1/admin/jobs/{key}` body `{ revision, enabled?, schedule?{kind:"every"|"cron", everySeconds?, cron?}, timezone? }` → `JobView`
  - `DELETE /v1/admin/jobs/{key}/override` body/query `revision` → `JobView`
  - `POST /v1/admin/jobs/{key}/run` → 202 `JobView`
  - `POST /v1/admin/jobs/schedule-preview` body `{ kind, everySeconds?, cron?, timezone? }` → `{ next: [RFC3339 ×5], description }`
- `JobView`: `key, module, name, title, description, defaultSchedule{kind,everySeconds,cron}, schedule{…effective}, timezone, enabled, defaultEnabled, overridden, moduleStarted, running, nextRunAt, retryAt, attempt, runRequestedAt, lastRun{runId,status,startedAt,finishedAt,durationMs,error}, timeoutSeconds, maxAttempts, revision`.
- Error codes (register like the neighbouring core modules do): `jobs.not_found` 404, `jobs.revision_conflict` 409, `jobs.already_running` 409, `jobs.already_requested` 409, `jobs.module_stopped` 409, `jobs.invalid_schedule` 422 (message names the cron field), `jobs.manual_schedule_locked` 422 (schedule override on a `Manual` job), `jobs.invalid_timezone` 422.
- Audit events: `jobs.override_updated`, `jobs.override_cleared`, `jobs.run_requested` with actor UUID from `ctxauth`, target key, before/after override (no personal data).
- Metrics: `orkestra_jobs_runs_total{job,status}`, `orkestra_jobs_run_duration_seconds{job}` (histogram, buckets 0.1…3600), `orkestra_jobs_lease_lost_total{job}`, registered where the other core modules register theirs (check `pkg/sdk/metrics` and `internal/core/llm`).

- [ ] **Step 1: Failing tests** — admin service: PATCH with stale revision → `ErrRevisionConflict`; PATCH cron `"61 * * * *"` → invalid_schedule; PATCH schedule on Manual job → manual_schedule_locked; disabling clears NextRunAt; RequestRun when module stopped → module_stopped; preview `"0 2 * * *"` Europe/Rome returns 5 ascending times; handler tests: operator without `system.jobs.manage` gets 403 on PATCH but 200 on GET (follow the auth-middleware test double other core handler tests use); `TestCoreLoadOrder` updated with `jobs` after `logging`; module test that `Jobs()` returns valid `runs_cleanup` and `Permissions()` both keys.
- [ ] **Step 2:** `go test ./internal/core/jobs/... ./cmd/server/ -race` → FAIL. Implement. → PASS.
- [ ] **Step 3:** `make backend-lint backend-policycoverage backend-tenantscope backend-logscope backend-piiscan openapi-check` (target names per `make ci-help`) → all green; regenerate OpenAPI as documented if `openapi-check` fails only because of the new routes.
- [ ] **Step 4: Commit** `feat(jobs): core jobs module with admin API` (+ separate `docs(jobs): module contract` if AGENTS.md is large).

### Task 6: Console workspace `/admin/modules/jobs`

**Files:** invoke the `orkestra-frontend-admin` skill first; it dictates reference components. Expected: `frontend-admin/src/store/api/jobsApi.ts`, `frontend-admin/src/types/jobs.ts`, `frontend-admin/src/pages/admin/modules/jobs/{index.tsx,JobsTable.tsx,JobDrawer.tsx,ScheduleEditor.tsx,RunsPanel.tsx,index.test.tsx}`, branch in `frontend-admin/src/pages/admin/modules/detail/index.tsx` mirroring `moduleName === 'logging'`, i18n strings where the logging workspace keeps its own.

**Interfaces — Consumes:** Task 5 routes and views.
- Table columns: title (+ key muted), module, schedule (human text: "every 1 h", "cron 0 2 * * * (Europe/Rome)", "manual"), status badge (disabled / module stopped / running / retry pending / last ok / last failed), last run (relative + duration), next run.
- Row actions gated by `system.jobs.manage`: enable/disable toggle, edit schedule (drawer: kind select, every in minutes or cron input, timezone, live preview from `schedule-preview` debounced 300 ms, error from 422 shown inline), run now, reset to default (only when `overridden`). 409 revision conflict → toast + refetch.
- Runs panel: paginated with "load more" (cursor), filter by status, error text in a collapsible monospace block, details as key/value list. URL-synced selected job and tab (`url-tabs` skill).
- Tests (vitest + RTL, follow `pages/admin/modules/logging/index.test.tsx`): renders rows from mocked list; disable sends PATCH with revision; preview shows 5 times; read-only user sees no action buttons; 409 triggers refetch.
- [ ] Steps: failing tests → implement → `cd frontend-admin && npm run typecheck && npm run lint && npx vitest run src/pages/admin/modules/jobs` green → commit `feat(console): jobs workspace`.

### Task 7: Documentation and follow-up issue

**Files:**
- Create: `docs/adr/0024-core-jobs-module.md` (context, decision J1–J22 condensed, consequences, alternatives), `docs/site/modules/core/jobs.mdx` (operator + author reference; include "Adding a job" with a code example), plus the docs-site sidebar/index entry the other core pages use.
- Modify: root `AGENTS.md` (ten → eleven core modules, module table row, load order line), `backend/AGENTS.md` (rule: new background work uses `HasJobs`; never a ticker in `Start()`), `backend/internal/core/AGENTS.md` (count, load order, table row), `docs/adr/README.md`, `README.md` and any other "ten core modules" occurrence found with `grep -rn "[Tt]en core\|10 core" --include=*.md --include=*.mdx .`.
- Open a follow-up issue: "Migrate hand-written tickers to the jobs framework" listing each ticker file from `grep -rln "time.NewTicker" backend/internal --include=*.go | grep -v _test`.
- [ ] Commit `docs(jobs): ADR-0024, module reference and core-module count`.
