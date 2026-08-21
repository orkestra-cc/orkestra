# Logging Module Operations Design

## Objective

Replace the standalone `/admin/observability/log-levels` page with a useful
Tier-1 operational workspace at `/admin/modules/logging`. The workspace must
support both durable log-level configuration and time-bounded incident
diagnostics, including a small, safe preview of recent logs.

The existing route remains as a redirect so saved links continue to work.

## Operator model

The surface separates two jobs that have different risk and lifecycle:

1. **Permanent configuration** changes the platform's global threshold or a
   module override and remains active until another saved change.
2. **Diagnostics** temporarily increases verbosity for one module, normally
   with an expiry, and helps the operator inspect the resulting events.

A diagnostic can be explicitly created without an expiry. The UI treats this
as an attention state and keeps it prominent until an operator stops it.

## Information architecture

`/admin/modules/logging` is a specialized module-detail page. It retains the
standard module header, health, and dependency context and adds four
URL-addressable sections through `?section=`:

- `overview`: global threshold, override count, active diagnostics, and log
  provider availability.
- `levels`: permanent global and per-module configuration.
- `diagnostics`: create, monitor, and stop temporary overrides.
- `logs`: bounded preview of recent Loki events and a deep link to Grafana.

The logging module's standalone navigation item is removed. Operators reach
the workspace through the existing Modules list. Requests to
`/admin/observability/log-levels` redirect to
`/admin/modules/logging?section=levels`.

This is implemented as a logging-specific detail surface selected by the
existing module detail route. It does not introduce a generic module-panel
extension framework until a second module needs one.

## Permanent configuration

The levels section renders all registered modules in the shared advanced-table
primitive. Each row distinguishes the permanent override, the inherited global
level, and the currently effective level.

Edits remain local until the operator selects **Apply changes**. A persistent
save bar reports the number of changed fields and offers **Discard** and
**Apply changes**. Saving sends the complete desired permanent snapshot in one
request. The backend validates every module and level before persisting or
publishing anything, so the update is atomic from the operator's perspective.

The batch endpoint uses optimistic concurrency based on the snapshot's
`updatedAt` value. A stale submission returns a conflict and the UI asks the
operator to reload rather than silently overwriting another administrator's
change.

## Diagnostics

A diagnostic contains:

- module name;
- temporary level;
- activation time and actor;
- optional expiration time.

The effective-level precedence is:

1. active diagnostic override;
2. permanent per-module override;
3. permanent global level.

Starting or stopping a diagnostic takes effect immediately. Default duration
choices are 15 minutes, 1 hour, and 4 hours, plus an explicit no-expiry option.
Active diagnostics show their remaining time, actor, and a stop action.

Expiration correctness does not depend on a background worker: the hot-path
resolver ignores an entry once its expiry has passed. A lightweight cleanup
loop removes expired persisted entries and stops with the module lifecycle.
Restarting the service preserves unexpired diagnostics and never revives an
expired one.

## Log preview

The browser never connects to Loki directly and never submits arbitrary LogQL.
It calls an operator-only Orkestra endpoint with a constrained request:

- required registered module;
- time window of 5, 15, or 60 minutes;
- optional level;
- optional bounded text search;
- maximum 100 results.

The backend constructs the query, applies a short timeout and response-size
limit, and normalizes returned events. The response contains timestamp, level,
message, module, and an allowlist of safe correlation fields such as trace ID.
Known credential, token, authorization, cookie, and personal-data keys are
removed or masked recursively before serialization. Raw Loki records are never
passed through wholesale.

The UI supports manual refresh and an optional five-second auto-refresh. It is
a diagnostic aid, not a streaming log console. A Grafana deep link preserves
the module, level, and time-window context for full investigation.

Loki is optional. Provider absence, timeout, query failure, empty results, and
loading are distinct states. A Loki failure never prevents level management or
diagnostic stop actions.

The initial provider contract targets the self-hosted Loki deployment already
shipped by Orkestra. It is kept behind a small backend interface so a future
managed-provider adapter does not affect handlers or the frontend, but no
multi-provider framework is built in this change.

## Backend API and persistence

Existing read and single-change endpoints remain temporarily available for
compatibility. New operations are:

- batch replacement of permanent global and module thresholds;
- start a module diagnostic;
- stop a module diagnostic;
- query the bounded log preview;
- report log-provider availability and Grafana link capability.

The single `log_levels` document gains a diagnostics map. Repository writes
continue to replace one validated document and publish an immutable in-memory
snapshot only after persistence succeeds. Permanent batch changes and
diagnostic mutations preserve this persist-before-publish invariant.

All operations serve the internal operator tier and require
`system.modules.admin`. No tenant-scoped log-level mutation is introduced.

## Frontend states and safeguards

- Debug-level choices explain their volume and sensitive-data risk before
  activation.
- Permanent changes and immediate diagnostic actions use visually distinct
  controls and language.
- A diagnostic without expiry receives a persistent warning treatment.
- Countdown display is derived from server timestamps; the server remains the
  authority for expiry.
- Destructive or disruptive actions identify the affected module and resulting
  effective threshold.
- All copy is localized in English and Italian, works in both themes, and uses
  the existing Orkestra primitives.
- Sections use URL search parameters and remain bookmarkable.

## Security and privacy

The Loki endpoint is a server-side query proxy, not a general query endpoint.
It validates enum values, registered module names, time ranges, search length,
result count, upstream response size, and timeout. User text is encoded into a
fixed query template rather than concatenated as LogQL syntax.

Operational logs may contain personal data despite logging guidelines. Preview
redaction is therefore defense in depth, not a claim that source logs are safe.
The preview does not broaden retention, does not store results in Orkestra, and
must not write returned log content into application logs, analytics, or error
telemetry.

## Testing and acceptance criteria

Backend tests cover:

- diagnostic precedence, expiry, restart behavior, and cleanup;
- atomic batch validation, conflict handling, and persistence failure;
- authorization on every operation;
- constrained Loki query construction, timeouts, size limits, and malformed
  upstream responses;
- recursive sensitive-field redaction;
- provider-unavailable behavior.

Frontend tests cover:

- section selection and stale-section fallback through the URL;
- dirty permanent edits, discard, atomic save, and conflict recovery;
- diagnostic creation, countdown, no-expiry warning, and stop;
- loading, empty, unavailable, error, and populated log-preview states;
- the legacy route redirect.

Completion requires targeted Go tests, frontend tests, typecheck, lint, and
production build. The logging workspace must remain fully usable when the
observability overlay is not running.

## Explicit non-goals

- Replacing Grafana Explore or implementing an unbounded live tail.
- Accepting arbitrary LogQL from the browser.
- Per-tenant log-level thresholds.
- Changing Loki retention rules.
- Building adapters for every managed observability vendor.
- Creating a generic module-detail extension framework before another module
  demonstrates the need.
