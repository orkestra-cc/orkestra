---
title: ADR-0024 — Core `jobs` module for scheduled background work
status: accepted
public: true
---

# ADR-0024 — Core `jobs` module for scheduled background work

| Field | Value |
|---|---|
| **Status** | ✅ Accepted |
| **Date** | 2026-11-08 |
| **Authors** | @salvatore.balestrino |
| **Amends** | [ADR-0006](0006-collapse-to-core-only-base.md): the core-only base gains an **eleventh** core module. Running work on a schedule, once per cluster, with retries and a record of what happened, is horizontal plumbing every product rebuilds; it is not a domain vertical. It passes the same test [ADR-0009](0009-core-compliance-module.md), [ADR-0022](0022-core-llm-module.md) and [ADR-0023](0023-core-retrieval-module.md) applied. |
| **Uses** | [ADR-0002](0002-metrics-label-schema.md) (metric labels), [ADR-0009](0009-core-compliance-module.md) (audit sink), [ADR-0012](0012-module-config-group-contract.md) (settings as config groups) |

## Context

Every module that needs background work has written its own loop: a `time.NewTicker` in `Start()` and a goroutine. The base alone has about a dozen of them (token sweeps, retention, outbox drains, reconcilers), and a fork's addons add more. Each copy decides for itself, and mostly wrongly, the same questions:

- **Many replicas.** With two backend replicas every ticker fires twice. Some loops guard themselves with a hand-rolled lock, most do not.
- **Restarts.** A sweep due at 02:00 is lost when the process is down at 02:00. A retry held in a goroutine is lost with the process.
- **Visibility.** Whether a job ran, how long it took and why it failed is only in logs. An operator cannot change a schedule or run a job now without a deploy.
- **Time.** "Every day at 02:00" has to mean a time zone, and a daylight-saving change must not run it twice or never.
- **Forks.** A product built on the base can carry dozens of scheduled jobs in its addons. Each would otherwise bring its own scheduler.

This ADR fixes one contract for declaring, scheduling, running and observing such jobs. The design spec numbered its decisions J1 to J22; they are condensed here under those labels (code comments cite them), with the amendments the implementation made.

## Decision

Add the SDK package `pkg/sdk/jobs` (schedules, cron parser, `Definition`, `Run`, `Catalog`), the optional module sub-interface `module.HasJobs`, and a core module `internal/core/jobs/` that schedules and runs the declared jobs. Acceptance test: *a module declares a job; on a three-replica cluster it runs once per slot, survives a restart without losing a retry, shows up in `/admin/modules/jobs` with its run history, and an operator can change its schedule, disable it or run it now, with each change audited.*

1. **Core, always-on, upstream (J1, J2).** The module is registered right after `logging`; load order becomes `user → notification → tenant → authz → auth → navigation → logging → jobs → llm → retrieval → compliance`, pinned by `core_load_order_test.go`. It declares no dependencies and provides no service: other modules reach it only by implementing `module.HasJobs`.
2. **Declaration, not registration (J3).** A module returns `[]jobs.Definition` from `Jobs()`. After every `Init`, and again after a `RetryInit`, the registry collects them into a `*jobs.Catalog` (service `module.ServiceJobCatalog`). Keys are `<module>.<name>` with `name` matching `^[a-z][a-z0-9_]{0,62}$`; a module's jobs are registered atomically, so a bad definition rejects that module's set and not the boot.
3. **A stopped module runs nothing (J4).** A job is claimed only while its module is started. A manual request for a job of a stopped module answers `jobs.module_stopped`.
4. **Schedules (J5).** `jobs.Every(d)` (at least one minute; an operator override is capped at 366 days), `jobs.Cron(expr)` and `jobs.Manual()`. Cron is five fields with `*`, lists, ranges, steps, English month and weekday names, the macros `@hourly @daily @midnight @weekly @monthly @yearly @annually`, and the Vixie rule (day-of-month OR day-of-week when both are restricted). The parser lives in the SDK; no library is added.
5. **Time zones (J6).** Cron is evaluated in the job's zone: operator override, else the module setting `timezone` (IANA name, default `UTC`). The zone `Local` is refused because it depends on the host. A wall time that does not exist (spring forward) is skipped; a repeated one (fall back) runs once.
6. **Code holds the default, the operator holds the override (J7).** Per job the operator may override `enabled`, `schedule` and `timezone`, and may clear the override. A `Manual` job takes no schedule override. The state stores a fingerprint of the effective settings; when it changes (override, new code default, zone change) the job is rescheduled from now and the slot that was due under the old schedule is skipped. An edit made during a run wins over the run's completion.
7. **Multi-replica lease (J8, amended).** One document per job in `jobs_states`; a replica claims a due job with a single conditional `findOneAndUpdate` (due, lease free or expired, revision unchanged). The lease lasts 2 minutes and is renewed every 30 seconds. **Amendment:** a renew *error* is transient; the run is cancelled only when the lease is lost (the renew matched nothing) or when renew errors last until the lease expires. A lease that expired while holding a run id is booked `abandoned` by the next claimant. The guarantee is **at most one run in progress per job**, never exactly one: job bodies must be idempotent.
8. **Loop (J9).** Each replica wakes at the earliest known due time or every 10 seconds, and runs at most `maxConcurrentRuns` jobs (default 4).
9. **Retry and timeout (J10).** `RetryPolicy{MaxAttempts, Backoff, MaxBackoff}`: one attempt by default, exponential backoff with jitter (default 1 minute up to 1 hour). A retry is durable (`retryAt` in the state) and any replica takes it. `jobs.Permanent(err)` skips the retries. Timeout per attempt: default 10 minutes, maximum 6 hours; a timeout is a failed attempt.
10. **Catch-up (J11).** A slot missed while the process or the module was down runs once (trigger `catchup`), never once per missed slot. `SkipMissed` drops it, records a `skipped` run and reschedules.
11. **Manual runs (J12).** `POST /v1/admin/jobs/{key}/run` writes `runRequestedAt` and the actor; the first free replica runs it through the same lease. Answers 202; a running job answers 409.
12. **Run log (J13).** One record per attempt in `jobs_runs`, with trigger, attempt, slot, timing, status (`running`, `succeeded`, `failed`, `timed_out`, `interrupted`, `abandoned`, `skipped`) and bounded details. Retention is the setting `runRetentionDays` (default 30), applied by the module's own job `jobs.runs_cleanup`.
13. **No personal data in the log (J14).** Errors (cut at 2 KiB) and details (at most 32 scalar keys, strings at most 256 runes, values JSON cannot encode dropped and counted) are operator-readable and outside the DSR sweep, so a job must not put personal data in them. A job that works per tenant iterates the tenants itself.
14. **Tier 1 only, platform scope (J15).** `jobs_states` and `jobs_runs` carry no tenant scope, like `module_configs`.
15. **Permissions (J16, amended).** `system.jobs.read` and `system.jobs.manage`, seeded like the other system permissions on super_admin, administrator and developer. **Amendment:** the spec named them `jobs.read` and `jobs.manage`; the `system.` prefix follows `system.compliance.*` and places them under the existing `system_actions.require_platform_role` Cedar rule.
16. **Audit (J17).** `jobs.override_updated`, `jobs.override_cleared` and `jobs.run_requested` go to the compliance audit sink with the actor's UUID (no email); a missing sink skips.
17. **Shutdown (J18).** `Stop` cancels the running jobs, waits up to 30 seconds, books them `interrupted` (a failed attempt) and releases the leases.
18. **Console (J19).** A specialised workspace at `/admin/modules/jobs` (no separate menu entry; the module registry stays the only navigation authority): job table, per-job drawer, schedule editor with a preview of the next five runs computed by the backend (`POST /v1/admin/jobs/schedule-preview`), run now, and paginated run history with a status filter.
19. **Metrics and health (J20).** Three families are registered here, as [ADR-0002](0002-metrics-label-schema.md) requires for a new label:
    - `orkestra_jobs_runs_total{job,status}`
    - `orkestra_jobs_run_duration_seconds{job}` (buckets 0.1 s to 3600 s; `abandoned` and `skipped` runs are counted but not timed)
    - `orkestra_jobs_lease_lost_total{job}`

    **Why the `job` label is bounded (ADR-0002 rule 1).** Its value is the catalog key, `<module>.<name>`. Keys come only from `Definition`s in compiled code, collected at boot and fixed for the life of the process; no request, tenant or operator input can create one (an operator can change a job's schedule, not its key). The domain is the number of declared jobs, tens per installation, known at code-review time, and `status` is the closed set above, so the worst case is about (jobs x 7) series for the counter. `tenant_id`, run ids and error text never become labels. Jobs of a module that no longer declares them stop being observed. `HealthCheck` reports degraded when the loop is not running or claims have failed for more than 5 minutes.
20. **Existing tickers stay (J21).** This change migrates none of them; the rule that new background work uses `module.HasJobs` is added to `backend/AGENTS.md`, and each ticker is migrated as follow-up work.
21. **Addon jobs (J22).** A fork's addons declare their scheduled work as `Definition`s with their default schedule, never as their own tickers.
22. **Additions made while building.** Error codes `jobs.invalid_query` (400) and `jobs.persistence_failed` (500, cause logged and never in the body) join the spec's list; an every-schedule override is capped at 366 days; the `Local` zone is refused; PATCH and DELETE carry the `revision` the caller read (optimistic concurrency) and answer 409 `jobs.revision_conflict` when a claim or completion bumped it in between.

## Consequences

- A module gets a cluster-safe, observable, operator-controllable schedule by returning a slice. Hand-written tickers can be retired one at a time, each migrated and tested on its own.
- **Idempotency is the author's duty.** The lease bounds concurrency but a retry, a catch-up or a lost lease can repeat work. The contract says so and the docs show it.
- It is not a work queue: no payloads, workers or per-item retries. Outbox-style drains (llm, push dispatchers) stay their own.
- Mongo is the only dependency; the 10-second poll and the per-claim write are the whole cost. No Redis, no new service.
- Deploys with replicas on different builds that declare different code defaults reschedule once per due time until all run the same build (consequence of the fingerprint).
- One more core module and two collections (`jobs_states`, `jobs_runs`); one more section in the console.
- Three metric families and one bounded label are added to the frozen schema of ADR-0002.

## Alternatives considered

- **An addon.** Rejected: the core modules that need it (auth sweep, compliance retention) cannot depend on an addon, and ADR-0006 keeps the base free of addons.
- **A library only (a scheduler package each module starts itself).** Rejected: no shared state, so no lease, no operator override, no run log, no console. That is the status quo with a nicer API.
- **A scheduler in the kernel / registry.** Rejected: the registry must stay small and frozen (the SDK rule); scheduling policy, persistence and an HTTP surface belong in a module.
- **`robfig/cron`.** Rejected: the SDK takes no new dependency without a decision, the parser is small and tested, and the library has been unmaintained since 2020. We also need `Next` in an arbitrary zone with the DST rules above.
- **A Redis lease.** Rejected: Mongo already holds the state and the run log, a Redis lease would split the truth (a lease and a state that disagree), and the base does not require Redis for correctness here.
