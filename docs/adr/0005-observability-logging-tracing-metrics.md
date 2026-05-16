---
title: ADR-0005 — Observability — logging, tracing, metrics as core platform features
status: proposed
public: true
---

# ADR-0005 — Observability: logging, tracing, metrics as core platform features

| Field | Value |
|---|---|
| **Status** | Proposed |
| **Date** | 2026-05-16 |
| **Authors** | @salvatore.balestrino |
| **Supersedes** | — |
| **Related** | [ADR-0001](0001-unified-tenant-model.md) (Tenant aggregate, baggage middleware), [ADR-0002](0002-metrics-label-schema.md) (Prometheus label schema), [ADR-0003](0003-three-audience-host-split.md) (Audience-aware request context) |

## Context

Orkestra is the plumbing every SaaS rebuilds. "Already-done logging" belongs in that plumbing alongside auth, RBAC, billing, and multi-tenancy — but the current state stops short of that promise:

- **Structured logging is wired** (`log/slog` stdlib, JSON in staging/prod, text in dev, `LOG_LEVEL` env-driven) — but the *request-log line itself* still flows through Chi's unstructured `middleware.Logger`. The most visible log in the system is the one that isn't JSON.
- **Distributed tracing is wired** (OpenTelemetry tracer, OTLP HTTP exporter, no-op when `OTEL_EXPORTER_OTLP_ENDPOINT` is unset, `TenantBaggage` middleware stamping `tenant.id`/`tenant.kind`/`user.id`/`user.role` on every authed span — ADR-0001 Phase 4.4) — but **traces and logs are not joinable**. No `trace_id` / `span_id` on log lines means Grafana's Loki ↔ Tempo jump doesn't work, and operators have to grep logs by timestamp and pray.
- **Prometheus metrics are wired** (`/metrics` on the operator host, gated by `METRICS_ENABLED`, three metric families per [ADR-0002](0002-metrics-label-schema.md)) — but there is no HTTP latency histogram. Service-level objectives (p95/p99 by audience, by tenant tier, by route) are unreachable from the data we collect.
- **Audit logging exists separately** (`compliance.AuditSink` writes append-only `audit_events` in MongoDB) — but operators routinely conflate audit and operational logs in tools that don't keep that split clean. Audit lines are forever; operational lines are sampled / retention-bounded. Those are different products.
- **Per-module debug** requires bouncing the entire backend's `LOG_LEVEL` to `debug`, drowning every other module's output in noise. RAG ingestion debugging shouldn't make the auth module unreadable.

This ADR establishes observability — **logs, traces, metrics, and audit** — as a first-class platform feature of Orkestra, on the same footing as `user` / `auth` / `tenant`. It is part of what an operator *buys into* when they pick Orkestra over a from-scratch repo. The mental model is **simple by default, pro on demand**: zero configuration must work; opting into a self-hosted Grafana stack or a managed OTLP backend must require only environment variables, not code changes in any module.

## Decision

Adopt a **three-tier observability model** with the following invariants, contracts, and tier definitions.

### 1. Cross-cutting invariants

These hold at every tier — they are the part operators are buying.

#### 1.1 Multi-tenant log correlation (the headline)

Every operational log line emitted from a request-scoped goroutine is automatically stamped with:

| Field | Source | Notes |
|---|---|---|
| `trace_id` | OTEL span context | 32-hex; empty when no span |
| `span_id` | OTEL span context | 16-hex; empty when no span |
| `tenant_id` | `TenantBaggage` middleware (ADR-0001 §4.4) | UUID of the acting tenant |
| `tenant_kind` | `TenantBaggage` middleware | `internal` (Tier-1) \| `external` (Tier-2) |
| `user_id` | request auth context | UUID; empty for anonymous |
| `user_role` | request auth context | RBAC role |
| `audience` | mux dispatch (ADR-0003) | `operator` \| `client` \| `service` |
| `request_id` | `chiMiddleware.RequestID` | Per-request correlation token |

This is the **multi-tenant correlation guarantee**. In Loki it becomes a one-liner: `{service="orkestra-backend", tenant_id="<uuid>"} | json` returns every log line that touched a given client's data across all modules. No other open-source SaaS framework ships this out of the box.

Implementation: a thin `slog.Handler` wrapper (`internal/shared/utils/trace_handler.go`) reads the OTEL `trace.SpanContext` from the log record's `context.Context` and the four tenant/user attrs from request-scoped values. It costs ~30 lines and zero allocations when no span is present.

#### 1.2 PII allowlist contract

Operational logs follow an **allowlist** — only attributes explicitly declared by the platform end up in stdout. There is no log statement anywhere in the codebase that dumps a request body, a header, a response body, or a `map[string]any` of arbitrary keys.

The allowlist for request logs is fixed by this ADR:

```
method, path, status, duration_ms, bytes, request_id,
trace_id, span_id, tenant_id, tenant_kind, user_id, user_role,
audience, remote, ua, slow
```

Notably **not** in the allowlist: `Authorization` header, `Cookie` header, query strings (path is logged with parameters intact since they're part of the OpenAPI surface; the raw query string is dropped), request bodies, response bodies, error messages from upstream services that may carry user input.

Module-emitted logs follow the same discipline: `slog.InfoContext(ctx, "msg", slog.String("key", "value"))` only — never `slog.Info("msg", slog.Any("everything", obj))` with user-shaped objects.

This contract is what makes the logging tier **GDPR-safe by construction** instead of by best-effort denylist scrubbing.

#### 1.3 Audit log vs operational log split

- **Audit log** (`compliance.AuditSink` → MongoDB `audit_events`): append-only, never sampled, retention governed by the compliance module's retention policy (default: forever). Captures security-relevant events (login, role grant/revoke, tenant create/suspend, payment, subscription change). The audit log is part of the **SOC2/GDPR evidence surface** and the contract for what it captures is owned by the compliance module, not by the logger.
- **Operational log** (stdout, JSON, Loki/SaaS sink): retention-bounded (default 14d for info, 30d for warn+), sampleable in Tier 2, may be redacted further by the exporter. Captures what happened operationally — HTTP requests, module init, errors with stack context.

The two **never share a sink** and an operator can drop the operational log entirely (e.g. in a regulated air-gapped environment where stdout is captured by a different agent) without losing audit fidelity.

#### 1.4 Per-module log levels

A small extension to the slog handler reads `LOG_LEVEL_<MODULE>` env vars (e.g. `LOG_LEVEL_RAG=debug`) and gates by the `module` attribute. Default behavior (no per-module env var) uses the global `LOG_LEVEL`.

Module loggers are obtained via `module.Deps.Logger` — already standard — so the `module` attribute is automatically stamped on every line emitted from a module. No code change required in module code.

A future iteration (out of scope for this ADR) moves per-module level into `module_configs` so it's mutable from `/admin/modules` without a restart, reusing the same admin pattern that already drives enable/disable and config environments.

#### 1.5 No sampling in v1

Operational logs are emitted at 100% in info+. Sampling is deferred — Loki at expected Orkestra deployment scale (single-digit-host fleets, see [ADR-0004](0004-external-services-integration.md)) handles full-fidelity ingest cheaply, and probabilistic sampling at the app layer hides bugs from the operator debugging them. If a future deployment justifies sampling, it lands at the collector (tail-based), not in the app.

### 2. Tier 0 — Zero config

The shape every operator gets without setting any environment variable beyond `ENV`.

- **Format**: text in `ENV=development`, JSON in staging/production. Already implemented.
- **Sink**: stdout. Captured by `docker logs` or any compose log driver.
- **Level**: `info` (default), `LOG_LEVEL` env var override (existing).
- **Request log**: structured JSON line emitted by `internal/shared/middleware/request_logger.go` (new in this ADR), allowlist-only.
- **Trace correlation**: `trace_id` / `span_id` stamped via §1.1 even when no OTLP exporter is configured (the tracer runs no-op but still produces valid span contexts).
- **Skipped paths** (no per-request log line): `/health`, `/ready`, `/metrics`, `/openapi.json`. Tunable via `LOG_HTTP_SKIP_PATHS` (comma-separated).
- **Slow flag**: `slow=true` stamped when `duration_ms > LOG_HTTP_SLOW_THRESHOLD_MS` (default `1000`).
- **Status → level mapping**: 5xx → `Error`, 4xx → `Warn`, else → `Info`.
- **Position in middleware chain**: outermost (slot 1) so 421 misdirected requests, CORS rejects, and audience-mismatch rejects all produce a log line.

Operators who deploy `docker compose up` and never touch an env var get correlated structured logs with tenant attribution. That is the **simple-by-default** half of the promise.

### 3. Tier 1 — Self-hosted observability stack

The "I want Grafana dashboards" path. No SaaS dependency.

- **Stack**: `docker-compose.observability.yml` (already exists with Tempo + Prometheus + Grafana) gains a **Loki** service and a **Promtail / Grafana Agent** sidecar that tails container stdout into Loki. Grafana datasources are auto-provisioned (Tempo, Prometheus, Loki) and the existing "Tenant traces" dashboard is extended to "Tenant traces + logs" — clicking a span jumps to filtered Loki logs via the shared `trace_id`.
- **Activation**: a single env var, `OBSERVABILITY_PROFILE=self-hosted`, layered through compose. Sets `OTEL_EXPORTER_OTLP_ENDPOINT=http://orkestra-otel:4318` (existing knob).
- **HTTP latency histogram**: a new Prometheus metric family `orkestra_http_request_duration_seconds` (registered by the SDK metrics package, label schema `{audience, method, route, status_class}` per ADR-0002 §"future families") populated by the same middleware that emits the structured log. Single metric family, no new export path.
- **Metric exemplars**: the latency histogram attaches the active `trace_id` as a Prometheus exemplar so Grafana's Prometheus → Tempo jump works without extra configuration.
- **Retention**: Loki configured for 14d on info, 30d on warn+. Tempo 72h (existing). Prometheus 15d (existing).

This is the **operator-grade** tier — everything stays on the operator's hardware, nothing leaves the fleet.

### 4. Tier 2 — OTLP-native (pro)

The "I already run Honeycomb / Datadog / Grafana Cloud / Axiom / New Relic" path.

- **Activation**: set `OTEL_EXPORTER_OTLP_ENDPOINT` to an OTLP-compatible vendor endpoint, supply `OTEL_EXPORTER_OTLP_HEADERS` if the vendor needs auth. Traces are already shipped this way today; this ADR extends the same env-var contract to **OTLP logs** via the SDK's `otlploghttp` exporter.
- **Stdout remains the source of truth**: OTLP logs are an *additional fanout*, not a replacement. If the collector or vendor backend goes down, stdout-captured logs are still recoverable. This is a deliberate design point — log loss on infrastructure failure is not acceptable for a platform marketed on audit/compliance.
- **Sampling**: if and when sampling is added, it lives at the collector layer (tail-based, drives on span outcome) — never in the app. The app emits 100%; the collector decides what to keep. This is the only sane place for sampling in a multi-tenant context where dropping a line about tenant X to save bytes could hide a security event.

This is the **pro-on-demand** half of the promise. Plug Orkestra into whatever the operator already pays for.

### 5. Migration path

Net-additive — no breaking changes to any module's logging behavior.

- **Phase A** (single PR): land §1.1 `TraceContextHandler` + §2 request middleware + `LOG_HTTP_SKIP_PATHS` / `LOG_HTTP_SLOW_THRESHOLD_MS` env vars. Remove Chi `middleware.Logger`. Old `slog.Info(...)` (no `ctx`) calls keep working — they just won't get `trace_id` until their hot paths are swept.
- **Phase B**: add `orkestra_http_request_duration_seconds` histogram with exemplars. New SDK metric registration — operators get it automatically once they upgrade.
- **Phase C**: per-module log levels (§1.4). Pure additive — env vars only.
- **Phase D**: Loki + Promtail in `docker-compose.observability.yml`, "Tenant traces + logs" dashboard provisioning.
- **Phase E**: OTLP logs exporter (Tier 2). Gated by `OTEL_LOGS_ENABLED=true` so operators who don't want it don't pay the exporter cost.
- **Phase F** (deferred): `/admin/modules` UI for per-module log levels and on-the-fly Loki retention overrides.

Each phase is independently shippable. A and B are the minimum to claim the selling-feature framing.

## Consequences

### What this enables

1. **One-line Loki query "show me everything client X did"** — the multi-tenant correlation guarantee (§1.1) makes cross-module client audit trivial without joining logs to a separate tenant table.
2. **Grafana span → log → metric trifecta works out of the box** — `trace_id` on every log, exemplars on every histogram, no operator setup beyond `OBSERVABILITY_PROFILE=self-hosted`.
3. **Selling-feature positioning** — README and docs can credibly claim "SOTA observability included" because every claim is backed by a concrete implementation, not a roadmap item.
4. **Per-module debug without global noise** — RAG engineers can `LOG_LEVEL_RAG=debug` without flooding the auth module.
5. **PII safety by allowlist contract** (§1.2) — instead of catching leaks in code review, the architecture makes leaks impossible without violating a documented contract.
6. **SLO-grade latency data** — `orkestra_http_request_duration_seconds` lets operators define and alert on p99 by audience / route / tenant tier without instrumenting any module.

### What we commit to maintaining

1. **The allowlist for request logs is a public contract.** Adding `email`, `phone`, query strings, or any other field to it requires an ADR amendment. Removing a field is a breaking change for log-based dashboards.
2. **`trace_id` / `span_id` field names are stable.** They match OpenTelemetry semantic conventions exactly — `trace_id` (not `traceId`, not `trace.id`) — so Grafana, Tempo, Honeycomb, and Datadog all join on them without per-vendor translation.
3. **The audit log (`compliance.AuditSink`) is never replaced by operational log scraping.** Audit captures intent; operational logs capture mechanics. They have different retention, different access controls, different schemas, and they will stay separate.
4. **OTLP logs are an addition, not a replacement, for stdout.** Even if `OTEL_LOGS_ENABLED=true`, stdout emission continues. This is non-negotiable for audit / compliance posture.

### What we give up

- **Free-form `slog.Any(...)` calls in module code.** The allowlist contract forbids `slog.Any("user", userObj)` because the keys are unbounded and a future struct field could leak PII. Modules must enumerate fields. This is a discipline cost, not a runtime cost.
- **Synchronous body-level debugging in production.** Bodies never reach logs. Operators debugging a wire-level issue use OTEL traces with body capture explicitly opt-in (debug-only, off by default, never in prod).
- **A small CI gate cost.** A future linter (`tools/logscope`, parallel to `tools/tenantscope`) may be added to enforce the allowlist statically. Not in scope for the v1 implementation but reserved as a follow-up.

### What stays the same

- The existing `compliance.AuditSink` interface and its consumers (auth, tenant, identity, subscriptions). Untouched.
- The existing OTEL tracer initialization (`internal/shared/telemetry/tracer.go`) and the `TenantBaggage` middleware. The new log handler **consumes** what they produce — it doesn't replace either.
- `LOG_LEVEL`, `ENV`, `METRICS_ENABLED`, `OTEL_EXPORTER_OTLP_ENDPOINT` env var semantics. All still mean what they meant before.
- The `/metrics` endpoint and its existing three metric families. The new latency histogram is additive.

## Alternatives considered

### Switch logger to zap or zerolog

Rejected. `slog` is stdlib, already wired across every module, and the performance gap to zap is single-digit-percent on JSON encoding — invisible compared to MongoDB latency on every request. Switching means a multi-PR sweep, a new dependency, and zero observable benefit. The bottleneck this ADR addresses is the *structure* of log lines, not the encoder.

### Denylist redaction instead of allowlist

Rejected for the request-log contract. Denylists ("scrub anything that looks like an email") are a well-documented source of PII leaks — every new field is a new potential bug. Allowlists ("only these enumerated keys ever appear") make leaks structurally impossible. The cost is module authors enumerating fields explicitly, which is a small discipline tax for a major safety win.

### Probabilistic sampling in v1

Rejected. At expected Orkestra deployment scale (operator-managed fleets), full-fidelity ingest into Loki is affordable. Sampling at the app layer would make multi-tenant debugging unpredictable ("did the log line drop for tenant X, or did the bug not fire?") and hide compliance-relevant events. If volume becomes a real constraint, sampling moves to the collector (tail-based, span-outcome-driven) — not into the app.

### Replace stdout with OTLP logs

Rejected. Stdout is the *fallback* path: when the collector dies, the operator's pod logs still capture everything. A logging stack that loses lines when the exporter is unreachable disqualifies Orkestra from any compliance posture that depends on audit completeness. OTLP logs are an additive fanout, governed by `OTEL_LOGS_ENABLED`.

### Two ADRs (broad strategy + concrete middleware)

Considered. Rejected for the simpler reason that the selling-feature framing only works when readers see the *whole* picture in one place — operators evaluating Orkestra need one ADR that says "this is what observability means here," not two cross-linked ones. Concrete implementation lives in PRs that reference this ADR.

### Mount the structured request logger inside Chi's existing `Logger` slot

Rejected. The existing slot is *after* CORS and `RequireAudience`, meaning 421 misdirected requests and audience-mismatch rejects produce no log line — exactly the security-relevant events you want logged. Moving the new logger to slot 1 (outermost) covers those cases and incurs no measurable overhead.

## Implementation pointers

| Concern | File(s) |
|---|---|
| Trace-context slog handler | `backend/internal/shared/utils/trace_handler.go` (new) |
| Logger setup wraps trace handler | `backend/internal/shared/utils/logger.go` (~5 LOC delta) |
| Structured request middleware | `backend/internal/shared/middleware/request_logger.go` (new) |
| Wire middleware on operator + client + service muxes | `backend/cmd/server/middleware.go` (~10 LOC delta) |
| AI sidecar middleware | `backend/cmd/ai-service/main.go` (parallel change) |
| HTTP latency histogram | `backend/pkg/sdk/metrics/metrics.go` (new family) |
| Env-var documentation | `docker/.env.example`, `docker/CLAUDE.md` |
| Loki + Promtail compose | `docker/docker-compose.observability.yml` |
| OTLP logs exporter | `backend/internal/shared/telemetry/logs.go` (new, Phase E) |

## Open questions

1. **Per-module level mutation at runtime** — Phase F lands the admin UI; is the storage layer `module_configs` (per-module) or a dedicated `logging_config` doc? Leaning toward `logging_config` because log level isn't a module property, it's an observability property *about* a module. Defer.
2. **Trace sampling defaults for Tier 2** — when OTEL logs ship, what sample rate does the OTLP traces exporter default to? Today it's 100% (no sampler configured). Tier 2 vendors charge per span; 100% is hostile. Likely answer: head-based parent-or-1% as the default with `OTEL_TRACES_SAMPLER` env override. Settle in the Phase E PR.
3. **`tools/logscope` linter** — worth building, or is code review enough? Defer until we see the first PR that tries to `slog.Any` a user object.
