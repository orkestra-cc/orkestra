# Module: Jobs — Background job scheduler

_Path: `/backend/internal/core/jobs`_
_Parent: [../AGENTS.md](../AGENTS.md)_

[← Core](../AGENTS.md) | [☰ Backend](../../../AGENTS.md) | [Root](../../../../AGENTS.md)

## Purpose

The eleventh core module. It schedules and runs the background jobs other modules **declare** through `module.HasJobs` (ADR-0024, decisions J1–J22). A module returns `[]jobs.Definition`; scheduling, the multi-replica lease, durable retries, catch-up, the run log, metrics and the operator UI belong to this module. Mongo is the only source of truth (no Redis, no new service). The guarantee is **at most one run in progress per job in the cluster**, not exactly one: job bodies must be idempotent.

Tier 1 only (J15): the jobs are the installation's, not a client's. Both collections are platform-level and carry no tenant scope, like `module_configs` and `log_levels`.

## What it owns

| Path | Purpose |
|---|---|
| `module.go` | Module declarations, Init (repositories, admin service, handler), Start/Stop/HealthCheck of the scheduler |
| `config.go` | ConfigSchema/ConfigGroups, `ValidateConfigSnapshot`, the 30 s timezone cache |
| `routes.go` | The two permission-gated route groups and their Huma operations |
| `cleanup_job.go` | `Jobs()`: the module's own `jobs.runs_cleanup` |
| `metrics.go` | Adapter from `scheduler.Metrics` to `metrics.Default()` |
| `models/` | `State` (one per job key), `RunRecord`, `Override`, `Lease`, statuses |
| `repository/` | Atomic claim (`findOneAndUpdate`), lease renew, fingerprint-guarded completion, keyset run history |
| `scheduler/` | Per-replica loop: `Reconcile`, `Tick`, `Start`, `Stop`, `Wake`, `Healthy`; `EffectiveOf`, `ScheduleOf`, `NextRun` |
| `services/` | `AdminService`: views, overrides, run requests, schedule preview, audit |
| `handlers/` | HTTP ↔ service translation and error codes |

The SDK half lives in `pkg/sdk/jobs` (`Schedule`, cron parser, `Definition`, `Run`, `Catalog`) and `module.HasJobs` / `module.ServiceJobCatalog`.

## MongoDB collections

| Collection | Shape | Indexes |
|---|---|---|
| `jobs_states` | `_id` = job key; `module`, `override{enabled,schedule,timezone}`, `fingerprint`, `nextRunAt`, `retryAt`, `retrySlotAt`, `attempt`, `runRequestedAt/By`, `lease{owner,runId,trigger,until,attempt,slotAt,startedAt}`, `lastRun`, `revision`, `updatedAt/By` | `{nextRunAt:1}`, `{retryAt:1}` sparse, `{runRequestedAt:1}` sparse |
| `jobs_runs` | one record per attempt: `_id` (UUID), `key`, `module`, `trigger`, `status`, `error` (≤ 2 KiB), `replica`, `requestedBy`, `attempt`, `scheduledFor`, `startedAt`, `finishedAt`, `durationMs`, `details`, `droppedDetails` | `{key:1,startedAt:-1,_id:-1}`, `{startedAt:1}`, `{key:1,status:1,startedAt:-1}` |

`updatedBy`, `runRequestedBy` and `requestedBy` hold an operator UUID, never an email. The run log is operator-readable and outside the DSR sweep (J14), so errors and details must carry no personal data.

## Dependencies

- **Modules**: none declared. Catalogued right after `logging` (`cmd/server/catalog.go`, pinned by `core_load_order_test.go`).
- **Services read** (all from the registry, when used): `ServiceJobCatalog` (the `*jobs.Catalog` the registry fills after `InitAll` and on `RetryInit`), `ServiceModuleStatus` (`module.ModuleStatusReader`, `IsStarted`; core modules count once `StartAll` started them), `ServiceAuditSink` (registered by compliance in its own later Init: resolved per event, nil skips).
- **Provides**: nothing. Other modules talk to it only by declaring jobs.

## Lifecycle

- **Init** builds the repositories, the timezone cache, the `AdminService` and the handler. No scheduler yet: the catalog is filled only after every Init.
- **Start** reads the catalog and `maxConcurrentRuns`, builds a `scheduler.Engine` (replica id = hostname + 8 random hex) and runs `engine.Start` in a goroutine on `context.WithoutCancel(ctx)`. The loop reconciles first (creates missing states, reschedules drifted fingerprints; repeated until it succeeds and whenever the catalog grows), then polls every 10 s or at the earliest locally known due time, and wakes early when a run finishes or `Wake` is called.
- **Stop** → `engine.Stop`: cancels running jobs, waits up to 30 s, books them `interrupted` (a failed attempt, J18) and releases their leases.
- **HealthCheck** → `engine.Healthy()`: degraded when the loop is not running or claims have failed for more than 5 minutes (J20).

## Configuration (ADR-0012 schema, `/admin/modules/jobs`)

| Key | Default | Range | Read |
|---|---|---|---|
| `timezone` | `UTC` (`JOBS_TIMEZONE`) | IANA name (`Local` refused) | live, cached ≤ 30 s |
| `maxConcurrentRuns` | `4` (`JOBS_MAX_CONCURRENT_RUNS`) | 1–32 | at Start (restart) |
| `runRetentionDays` | `30` (`JOBS_RUN_RETENTION_DAYS`) | 1–365 | at each cleanup run |

`ValidateConfigSnapshot` enforces the ranges and the timezone on every config mutation surface (the SDK does not enforce `Min`/`Max` itself). `HotReloadConfig()` is true; the `maxConcurrentRuns` description says it needs a restart.

## HTTP endpoints (Tier 1, operator-protected router, tag `Jobs`)

| Method | Path | Permission | Result |
|---|---|---|---|
| GET | `/v1/admin/jobs` | `system.jobs.read` | `{jobs: [JobsJobView]}` |
| GET | `/v1/admin/jobs/{key}` | `system.jobs.read` | `JobsJobView` |
| GET | `/v1/admin/jobs/{key}/runs?status=&cursor=&limit=` | `system.jobs.read` | `{runs: [JobsRunView], nextCursor?}` (newest first, limit 1–100, default 25) |
| POST | `/v1/admin/jobs/schedule-preview` | `system.jobs.read` | `{next: [5 × RFC 3339 in the zone], description}` |
| PATCH | `/v1/admin/jobs/{key}` | `system.jobs.manage` | body `{revision, enabled?, schedule?{kind,everySeconds?,cron?}, timezone?}` → `JobsJobView` |
| DELETE | `/v1/admin/jobs/{key}/override?revision=` | `system.jobs.manage` | `JobsJobView` with the declared defaults |
| POST | `/v1/admin/jobs/{key}/run` | `system.jobs.manage` | 202 `JobsJobView` (`runRequestedAt` set) |

`JobsJobView`: `key, module, name, title, description, defaultSchedule, schedule` (effective), `timezone` (effective), `enabled, defaultEnabled, overridden, moduleStarted, running` (a live lease of a real run), `nextRunAt, retryAt, attempt, runRequestedAt, lastRun{runId,status,startedAt,finishedAt,durationMs,error}, timeoutSeconds, maxAttempts, revision`. Jobs whose module no longer declares them are not listed (their state stays in Mongo, inert).

PATCH semantics: absent fields are unchanged; `timezone: ""` removes the timezone override; an override with no field left is removed. Every write carries the `revision` the caller read (optimistic concurrency); claims and completions bump it too, so a run finishing between read and write answers 409.

Error codes (`internal/shared/errcode`, block `jobs`): `jobs.not_found` 404, `jobs.revision_conflict` 409, `jobs.already_running` 409, `jobs.already_requested` 409, `jobs.module_stopped` 409, `jobs.invalid_schedule` 422 (the detail names the cron field), `jobs.manual_schedule_locked` 422, `jobs.invalid_timezone` 422, `jobs.invalid_query` 400 (unknown status filter, foreign cursor), `jobs.persistence_failed` 500 (cause logged, never in the body).

## Permissions, audit and metrics

- `system.jobs.read` and `system.jobs.manage`, `Module: "jobs"`, `System: true`: seeded on super_admin / administrator / developer like every system permission, covered by the `system_actions.require_platform_role` Cedar forbid (the spec's J16 named them `jobs.read` / `jobs.manage`; the `system.` prefix follows `system.compliance.*`). The keys are written as literals in `routes.go` and `Permissions()` so policycoverage can match them.
- Audit (J17) on the compliance sink: `jobs.override_updated`, `jobs.override_cleared`, `jobs.run_requested`; resource type `job`, resource id the key, actor UUID from `ctxauth` (no email), metadata `job`, `before`/`after` override and the new `revision`. Best-effort: a missing sink skips, a panicking sink is recovered.
- Metrics (`pkg/sdk/metrics`, ADR-0002): `orkestra_jobs_runs_total{job,status}`, `orkestra_jobs_run_duration_seconds{job}` (buckets 0.1 s … 3600 s; `abandoned` and `skipped` are counted, never timed), `orkestra_jobs_lease_lost_total{job}`. `job` is the catalog key: closed at boot.

## Invariants

1. **A job is claimed only through the lease**: one `findOneAndUpdate` on "due, lease free or expired, revision unchanged". Manual runs too (J12): `POST …/run` only writes `runRequestedAt/By`.
2. **A stopped module runs nothing** (J4, J5): its jobs are not even offered to the due query; a run request answers `jobs.module_stopped`.
3. **The default is code, the operator's choice is the override** (principle 4). The state stores the `fingerprint` (`on|off`, schedule, zone) its `nextRunAt` was computed from; a fingerprint change — an override, a new code default, a module timezone change — **reschedules from now and skips the slot that was due under the old schedule**. Rolling deploys whose replicas declare different code defaults reschedule once per due time until every replica runs the same build.
4. **An operator edit made during a run wins**: the run's completion writes its schedule fields only while the fingerprint is the one it claimed under.
5. **Disabled jobs are not claimed** for schedule, retry or catch-up, and their due times are cleared; a manual request still runs them.
6. **Lease renewal (J8 as amended)**: the lease (TTL 2 min) is renewed every 30 s. A renew **error** is transient: the run goes on while the lease has time left. The run is cancelled only when the lease is lost (renew matched nothing) or renew errors persisted until the lease expired. A lease that expired with a run id is booked `abandoned` by whichever replica claims next.
7. **Retries are durable** (`retryAt`, J10) and any replica takes them; missed slots coalesce into one `catchup` run (or one `skipped` record with `SkipMissed`, J11). Interrupted and timed-out runs are failed attempts.
8. **Details are bounded and encodable**: ≤ 32 keys, scalars, strings ≤ 256 runes; NaN/±Inf and unsigned values above MaxInt64 are dropped and counted.

## Adding a job (any module, core or addon)

Implement `module.HasJobs` on the module; the registry collects the definitions after Init (and after a `RetryInit`) and registers them atomically per module under `<module>.<name>`.

```go
func (m *Module) Jobs() []jobs.Definition {
	return []jobs.Definition{{
		Name:        "expire_quotes",           // ^[a-z][a-z0-9_]{0,62}$ → key "sales.expire_quotes"
		Title:       "Expire stale quotes",
		Description: "Marks quotes past their validity date as expired.",
		Schedule:    jobs.Cron("0 2 * * *"),    // or jobs.Every(15*time.Minute), jobs.Manual()
		Timeout:     5 * time.Minute,           // default 10m, max 6h
		Retry:       jobs.RetryPolicy{MaxAttempts: 3}, // default 1 = no retry
		Run: func(ctx context.Context, run *jobs.Run) error {
			n, err := m.svc.ExpireStale(ctx, run.ScheduledFor())
			run.Detail("expired", n) // counts, never personal data
			if errors.Is(err, services.ErrMisconfigured) {
				return jobs.Permanent(err) // do not retry
			}
			return err
		},
	}}
}
```

Rules for the body: honour `ctx` (Stop and the timeout cancel it); be idempotent (re-running the same period is safe); log through the module logger with `job_key` / `run_id`, not into details; a job that works per tenant iterates the tenants itself and stamps the context for each (J14). A new job uses `HasJobs` — do not start a ticker in `Start()`.

## What it does NOT do

- **No work queue**: no payloads, workers or per-item retries (the llm outbox, push dispatchers stay their own).
- **No tenant fan-out**, no dependencies between jobs, no failure emails (a job can read `jobs_runs` and use `NotificationSender`).
- **No migration of the existing hand-written tickers** (J21): auth's token sweep, compliance retention, llm outbox/reconciler and any loop a fork's addon runs keep running as they are until each is migrated and tested on its own.
