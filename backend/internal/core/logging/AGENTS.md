# Module: Logging — Runtime log-level admin

_Path: `/backend/internal/core/logging`_
_Parent: [../AGENTS.md](../AGENTS.md)_

[← Core](../AGENTS.md) | [☰ Backend](../../../AGENTS.md) | [Root](../../../../AGENTS.md)

## Purpose

Owns runtime log-level configuration and bounded Tier-1 diagnostics for the slog pipeline (ADR-0005 Phase F plus the logging-operations workspace). Operators can atomically replace permanent thresholds, start expiring per-module overrides, and preview up to 100 recent Loki events. Every existing module logger picks up level changes instantly — no restart, env-var edit, or image pull.

Sits at the end of the core init order so by the time it runs, every other module has already taken its `deps.Logger` clone. The shared `resolverBox` behind `PerModuleLevelHandler` means those pre-existing clones still observe the swap.

## Operator workspace and navigation

The authoritative operator surface is the specialized `/admin/modules/logging` workspace. Its URL-synced `section` selects `overview`, `levels`, `diagnostics`, or `logs`; `/admin/modules/logging?section=levels` opens permanent configuration directly. The former `/admin/observability/log-levels` URL is retained only as a history-replacing redirect to that levels section, so old bookmarks stay useful without creating a second UI. `LoggingModule` deliberately contributes no standalone `NavItems()` entry: the existing Modules navigation is the single authority.

## What it owns

| File | Purpose |
|---|---|
| `module.go` | Module registration; reads `ServiceLogLevelModuleNames` to render admin rows; publishes `ServiceLogLevelResolver` |
| `routes.go` | Huma route registration (9 endpoints under `/v1/admin/observability/log-levels`) |
| `handlers/log_level_handler.go` | HTTP ↔ service/provider translation; pulls actor UUID via `ctxauth` |
| `logquery/client.go` | Constrained optional Loki provider (closed LogQL template, 3 s / 1 MiB / 100-event caps) |
| `logquery/redact.go` | Recursive masking for structured sensitive keys; does not sanitize free-text messages |
| `services/log_level_service.go` | lock-free `atomic.Pointer` snapshot, Mongo-authoritative CAS mutations, bounded replica refresh |
| `services/log_level_service_test.go` | unit tests including `-race` concurrent reads/writes |
| `repository/log_level_repository.go` | Mongo single-document read + compare-and-swap (`_id="default"`, `revision`) |
| `models/log_level.go` | `LogLevel` value-type, `Parse`, `Slog`, `LogLevelDoc`, `AdminView` |

## MongoDB collections

| Collection | Shape | Notes |
|---|---|---|
| `log_levels` | single document with `_id="default"`, `global`, `perModule`, `diagnostics`, `revision`, `permanentRevision`, and update metadata | Mongo-authoritative compare-and-swap; legacy documents without revision fields are revision zero and migrate on their next write |

No declared indexes — `_id` is the primary key and that's all we filter on.

## Dependencies

- **Modules**: none declared. Logging is intentionally a leaf in the DAG so it can be the last to init.
- **Required services**: none — it reads `ServiceLogLevelModuleNames` from the registry but tolerates its absence (the admin view just renders zero module rows).
- **Optional process configuration**: `LOKI_QUERY_URL` is the trusted server-side Loki base; empty or malformed makes preview unavailable. `GRAFANA_URL` is an optional validated HTTP(S) browser-facing base. Request data can select neither URL.
- **Provides**:
  - `ServiceLogLevelResolver` → `*services.LogLevelService` (satisfies `utils.LevelResolver` structurally).
- **Permissions contributed**: none. Every endpoint is mounted inside the Tier-1 operator group guarded by `RequireSystemPermission("system.modules.admin")`; the OpenAPI `administrator` scope is documentation, not the enforcement layer.

## Lifecycle

`Init` builds the repository, captures the boot env defaults (`utils.GlobalLevelFromEnv` + `utils.LoadPerModuleLevels`), looks up the module-names catalog via `ServiceLogLevelModuleNames`, constructs the `LogLevelService` seeded with the env snapshot, and calls `svc.Load(ctx)` to pull the persisted document (if any) into the atomic snapshot. The service is then registered under `ServiceLogLevelResolver`. Separately, `LOKI_QUERY_URL` creates either a constrained client or an unavailable provider; `GRAFANA_URL` is validated for the status view.

`main.go` reads that key **after** `InitAll` returns and calls `utils.SwapLevelResolver(svc)` to replace the boot-time `StaticLevelResolver` in `PerModuleLevelHandler`. Every existing module logger picks up the swap through the shared `resolverBox` atomic pointer.

`Start` launches one maintenance loop. It refreshes the local immutable snapshot from Mongo every two seconds, bounding cross-replica staleness, and periodically removes expired diagnostic records. `Stop` cancels and joins that loop; expiry correctness remains on the resolver hot path and does not depend on cleanup. `HealthCheck` is inherited from `BaseModule`.

A diagnostic starts or replaces one module's temporary threshold for 15, 60, or 240 minutes, or with no expiry. The temporary value wins only while it remains live; expiry is enforced on every resolver read, and the cleanup loop later removes expired records from storage. Stopping a diagnostic removes it explicitly and reveals the permanent threshold again.

## HTTP endpoints

All nine are mounted on the Tier-1 operator-protected router and require `system.modules.admin` (`Security: bearerAuth.administrator` documents the same boundary on every operation).

| Method | Path | Purpose |
|---|---|---|
| GET    | `/v1/admin/observability/log-levels` | Returns `AdminView`: global level + one row per registered module |
| POST   | `/v1/admin/observability/log-levels/logs` | Returns at most 100 recent minimized events for one registered module; filters are in the JSON body |
| PUT    | `/v1/admin/observability/log-levels` | Atomically replaces the complete permanent snapshot with optimistic concurrency |
| PUT    | `/v1/admin/observability/log-levels/global` | Sets the global threshold |
| PUT    | `/v1/admin/observability/log-levels/{module}` | Sets a per-module override |
| DELETE | `/v1/admin/observability/log-levels/{module}` | Removes a per-module override (falls back to global) |
| POST   | `/v1/admin/observability/log-levels/reset` | Reverts global + every override to boot env defaults |
| PUT    | `/v1/admin/observability/log-levels/{module}/diagnostic` | Starts/replaces a 15/60/240-minute or no-expiry diagnostic |
| DELETE | `/v1/admin/observability/log-levels/{module}/diagnostic` | Stops a module diagnostic |

The preview accepts only a registered module, a 5/15/60-minute window, an optional closed log-level enum, at most 200 search characters, and a result limit clamped to 100. Filters travel in a POST body rather than a URL, successful responses carry `Cache-Control: private, no-store`, and the admin client evicts unused preview data immediately. It constructs LogQL itself and never accepts raw LogQL or an upstream URL. Provider absence returns a stable 503; timeout is 504; other upstream failures are 502; rejected filters are 400. Upstream bodies are never included in errors. This is deliberately a bounded diagnostic preview, not a log browser: streaming, arbitrary exploration, and full investigations remain Grafana's responsibility.

Mutations return the fresh `AdminView` so the UI re-renders without a separate refetch.

## Service contract

The service implements both `utils.LevelResolver` (consumed by `PerModuleLevelHandler.Enabled` on the hot read path) and `services.LevelResolver` (the local mirror so dependent code doesn't have to import `shared/utils`). The two interfaces have the same shape; structural typing satisfies both.

## Key invariants

- **Mongo is authoritative across replicas.** Every mutation reads the current document and uses `revision` as the replace predicate. CAS misses retry from a fresh authoritative document a bounded number of times, preserving unrelated writes. The process-local mutex only serializes this replica; reads consult the atomic snapshot lock-free.
- **Separate permanent concurrency.** `revision` advances for every stored mutation. `permanentRevision` advances only when the durable global/per-module configuration changes, so start/stop/expiry maintenance cannot invalidate an operator's permanent draft. The UI sends this token on atomic apply and latches a 409 until it adopts a fresh snapshot.
- **Legacy compatibility.** A document with neither revision field decodes as revision zero. The first successful CAS accepts the missing field and writes both counters without requiring a migration job.
- **Atomic snapshot for the hot path.** `*snapshot` lives behind `atomic.Pointer[snapshot]`; readers (every log call) get a consistent view without locks. Mutations build a new snapshot and `Store` it after persisting — readers either see the old snapshot or the new one, never partial state.
- **Persist before publish.** If the Mongo CAS fails, the candidate snapshot is **not** published. A winning document is published locally and other replicas observe it through the bounded refresh loop.
- **Env defaults are remembered separately.** `ResetToEnv` reverts to the values captured at `NewLogLevelService` time, not to "whatever the env says right now" — restarts re-resolve from env, but a Mongo doc takes precedence.
- **No retroactive seeding.** When the document is missing (fresh deployment), the service stays on the env snapshot. The first admin write creates the document; subsequent restarts load from Mongo.
- **Bounded, minimized preview.** Loki calls use a dedicated non-redirecting client with a 3-second timeout and one-MiB response cap. Results are normalized chronologically and capped at 100. Only timestamp, normalized level, preserved message, requested module, and explicitly allowlisted correlation/duration attributes are serialized.
- **Masking is defense in depth.** Structured keys matching credentials or personal-data names are recursively replaced with `[REDACTED]`. Free-text `message` is intentionally preserved for usefulness and may still contain personal data; preview results are never stored, logged, or sent to telemetry by Orkestra.

## What this module does NOT do

- Loki retention overrides — out of scope for Phase F; reserved as a future amendment.
- Per-tenant log levels — the threshold is global per module. Tenant-scoped filtering happens at query time in Loki via `tenant_id`, which is present on the `http_request` line (through `ctxauth.RequestAnnotations`) and on the lines where the calling code adds it itself. No handler stamps it on other records: `TraceContextHandler` adds only `trace_id`/`span_id`, and the tenant a `*Context` call carries only selects the masking policy.
- An unbounded or streaming log console — preview is a small manual/periodic diagnostic aid; full investigation remains in Grafana.
- Audit-log integration — runtime log-level changes are persisted with `updatedBy`/`updatedAt` but are not pushed through the compliance `AuditSink`. Future work.

## Rules

- **Never mutate the snapshot in place.** Always build a new `*snapshot` and `Store` it — readers depend on the immutability invariant.
- **Never expose the resolver as `services.LogLevelService` to consumers.** The interface boundary is `utils.LevelResolver`; the concrete type can rename without breaking consumers.
- **Env vars are seed-only.** After first boot, the Mongo doc is authoritative. Setting `LOG_LEVEL_<MODULE>` after the document exists is silently shadowed — surface this in operator-facing docs whenever it changes.

## Compliance masking (compliance spec §2)

- Every `slog` record passes through `utils.PolicyHandler` (after the level
  gate, before the fan-out to stdout and OTLP). Not masked: chi's
  `Recoverer` writes recovered panic values and stacks to stderr outside the
  `slog` chain. Secrets are always masked; IP,
  user agent, user ids, personal-data keys and free text follow the
  compliance policy of the record's tenant. A record without a tenant gets
  the strictest policy in force. Until the compliance module provides the
  live policy (T2), the platform defaults apply.
- Free-text IBAN detection requires the ISO 13616 mod-97 checksum only for
  candidates made entirely of hex characters (the shape a 32-hex trace id
  shares with an IBAN); every other IBAN-shaped match is masked whatever its
  checksum, so a mistyped IBAN does not leak. `trace_id`/`span_id` skip the
  free-text scan only when the value is exactly 32/16 lowercase hex
  characters; any other value under those keys is scanned. `request_id` is
  always scanned: chi's `RequestID` copies the client's `X-Request-Id`.
- Values are scanned whatever their static type: named string types, `[]byte`
  and `json.RawMessage` as text, map keys as free text (key rules still apply
  to the value under its raw key), `LogValuer`s inside containers resolved.
  Keys found inside values never enter the key-classification cache.
- `utils.SwapLogPolicyResolver` reaches the handler of every `SetupLogger`
  call (main.go calls it twice) and of any later one.
- `http_request` logs the chi route template (`route`), not the raw path;
  `path` appears only when no template matched.
- Outgoing spans are masked the same way by `telemetry.MaskingExporter`. The
  RequestLogger stamps `http.route` on the active server span; a span with a
  route gets the route template as the value of `url.path`/`http.target`. The
  exporter drops `url.path` and `http.target` when a span has no route (fail
  closed); `http.url`, `url.full` and `url.query` are never exported;
  `network.peer.address`, `client.address` and forwarded-for headers follow
  the IP rule.
- `make backend-logscope` rejects new values of a type the masker does not
  scan (an allowlist: string kinds, byte slices, errors, `LogValuer`s, basic
  numbers/booleans/times, maps with string keys, slices of those; everything
  else, structs first, is flagged) and secret-looking keys with dynamic
  values, both in `slog.Any` and in the key/value form (`slog.Info("m", "k",
  v)`, `With`, `Log`, `Group`); a package that fails to load fails the gate.
  Pre-existing findings are in `tools/logscope/baseline.txt`.
- Log masking overhead budget is ≤ 2 µs per record over a plain JSON handler
  (10 attrs, no free-text scan), measured by `BenchmarkPolicyHandler_TenAttrs`
  vs `BenchmarkPlainJSON_TenAttrs` in `internal/shared/utils`.

## Related

- [`../../shared/utils/per_module_level_handler.go`](../../shared/utils/per_module_level_handler.go) — the slog handler that consumes the resolver
- [`../../shared/utils/logger.go`](../../shared/utils/logger.go) — `SwapLevelResolver` global called by `main.go`
- [`../../../pkg/sdk/module/services.go`](../../../pkg/sdk/module/services.go) — `ServiceLogLevelResolver` / `ServiceLogLevelModuleNames` keys
- [`../../../../docs/adr/0005-observability-logging-tracing-metrics.md`](../../../../docs/adr/0005-observability-logging-tracing-metrics.md) — full Phase F design
- [`../navigation/AGENTS.md`](../navigation/AGENTS.md) — neighbour core module; same module-interface shape
