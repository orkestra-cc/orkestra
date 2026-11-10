---
title: ADR-0009 — Re-home the compliance plane (audit + GDPR DSR) to core
status: accepted
public: true
---

# ADR-0009 — Core `compliance` module: audit trail + GDPR data-subject rights

| Field | Value |
|---|---|
| **Status** | ✅ Accepted — Phases 1–4 shipped in v0.3.9 (2026-06-18); Phase 5 grafts shipped, including the stored export; Phase 6 console shipped with the admin-side DSR amendment (D7); amended by D7 below and by [ADR-0023](0023-core-retrieval-module.md) (D12: an erasure request completes only when every producer is done) |
| **Date** | 2026-06-18 |
| **Amends** | [ADR-0006](0006-collapse-to-core-only-base.md) — **partially reverses** the compliance-addon removal. ADR-0006 carved compliance into its own Go module and dropped it from the base; this ADR brings the audit + DSR + KMS plane back as a **core** module (SOC2 rides along, gated off). The data it governs is core-owned, so it fails ADR-0006's "is this a domain vertical?" test. |
| **Uses** | [ADR-0001](0001-unified-tenant-model.md) (DSR is tenant-aware via `tenantrepo`), [ADR-0003](0003-three-audience-host-split.md) (admin on operator, self-service on client). [ADR-0005](0005-observability-logging-tracing-metrics.md) — the audit trail is **separate** from operational logs. |

## Context

ADR-0006 collapsed Orkestra to a core-only base and **removed the `compliance` addon**, which had owned the append-only audit log, the GDPR DSR pipeline (right-of-access export + right-to-erasure), per-tenant KMS crypto-shred, and SOC2 evidence. What it left behind is asymmetric:

- **Survived in core (the *producer* side):** `iface.PIIProducer` / `PIIProducerRegistry` — the registry is created in `cmd/server/main.go` before `InitAll`, and the `user` + `auth` modules register a producer each. Plus the nil-checked `iface.AuditSink` and `iface.KMSProvider` *setter* seams on user/auth/tenant.
- **Left with the addon (the *consumer* side):** the DSR service that walks the producers, the audit-sink implementation, the KMS provider, SOC2 evidence, and the admin/self-service handlers.

Net effect: **the base ships the hooks for data-subject rights but nothing that services them.** A deployment stores personal data (user identity, auth credentials/sessions/MFA, tenant memberships, authz bindings) with no way to export or erase it, and emits no audit trail (the sink resolves to nil).

The personal data at stake is **core-owned**. Data-subject rights (Art. 15/17/20) and the accountability audit trail (Art. 5(2)) are therefore horizontal properties of the platform, not a domain vertical a particular business adds — the same line ADR-0006 used to decide core-vs-addon, applied honestly, puts this plane back in core.

We already have the implementation: it is recoverable from git history (the pre-removal addon at `internal/addons/compliance/`), authored on this same stack. The work is to **re-home it to core, reuse the surviving seam, and complete it** — not to design from scratch.

We remain pre-GA with permission to make breaking changes.

## Decision

Re-home the compliance plane — **audit sink + GDPR DSR + per-tenant KMS** — to a core module `internal/core/compliance/`, reusing the existing `PIIProducer` seam, completing producer coverage across all five core data-holding modules, and adding an erase-mode dimension. SOC2 evidence ships in the same module but **gated off by default**. Acceptance test: *with the module present, every core deployment can export and erase a data subject and emits an audit trail; the compliance package imports no other module's internals — all personal-data access flows through `iface.PIIProducer`.*

### D1 — Core, always-on (amends ADR-0006)

Registered in `coreModules()` (`cmd/server/catalog.go`), ordered after `user`/`auth`/`tenant`. `Category()` is `CategoryCore`, so Init failure is fatal. No enable/disable for the plane as a whole — audit + DSR are unconditional.

### D2 — Reuse the existing `iface.PIIProducer` seam — do not invent a parallel one

The producer side already shipped and survived ADR-0006. This module brings back the **consumer** (the DSR service over `PIIProducerRegistry.List()`) and **completes producer coverage**: `user` + `auth` already registered producers; we add **`tenant`** (memberships), **`authz`** (role bindings), and **`notification`** (message history + preferences). Each module registers its own producer in its `Init`; compliance imports none of them.

> An earlier draft of this ADR proposed a *new* `DataSubjectProvider` pull interface. **Rejected** once the existing `PIIProducer` was found — a second seam would duplicate a shipped SDK contract (see Alternatives).

### D3 — Erase modes: anonymize vs hard-delete (`iface.EraseMode`)

`PurgePersonalData` gains an `EraseMode` (`EraseHardDelete` | `EraseAnonymize`). The **user identity row** is the one place the modes differ: anonymize aliases the email and blanks the profile while **keeping the UUID** (referential integrity; the canonical tombstone the retention job later hard-deletes). **Satellite rows** (auth tokens/sessions/MFA, tenant memberships, authz bindings, notification messages) carry no anonymizable residue — a row that *is* the user linkage — so they hard-delete under both modes.

### D4 — Audit sink + KMS become core-provided

The base previously ran with a nil audit sink and no crypto-shred. With this module core, the append-only `audit_events` sink is **always present** (accountability everywhere), and the per-tenant KMS provider is wired whenever a master key is configured (crypto-shred on tenant purge, already consumed by the tenant module's nil-checked setter).

### D5 — SOC2 evidence gated behind a flag (default off)

SOC2 is the one sub-feature not every deployment needs. It lives in the module but behind `compliance.soc2_enabled` (`FieldBool`, default `false`, env `COMPLIANCE_SOC2_ENABLED`) — handler, routes, and nav item all dormant unless enabled. Audit + DSR carry no such gate.

### D6 — Grafts on top of the recovered DSR

The recovered DSR was synchronous export/erase only. We add, in the compliance module: **legal hold** (gates erasure — an active hold makes erase/retention return `409`), **retention auto-cleanup** (`Startable` ticker, **off by default**, reaps anonymized tombstones past `retention_years`, dry-run preview), **export to an encrypted RustFS blob** with a TTL + signed-URL download (replacing the inline synchronous bundle for large exports), an **erasure-request workflow** (client lodges, operator executes), and `RequireStepUp` on destructive + bulk-export routes. All DSR queries are tenant-aware (ADR-0001).

### D7 — Admin-side data-subject requests (amendment)

D6 modelled the DSR from the subject's side: inline self-service export, a request the subject lodges, a single operator who executes. Deployments that receive data-subject requests by email or post, or that hold data on people who never use the application, need the operator side as well. D7 adds it inside the same pipeline; every default keeps the behaviour of D6, and no new permission key is introduced (reads use `system.compliance.audit.read`, writes `system.compliance.dsr.manage` or `system.compliance.legalhold.manage`, both with step-up).

- **Export on behalf of the subject.** An operator creates an export (JSON, or CSV as `producer,path,value` rows with formula-safe cells) for a subject with a mandatory masked reason. The file is stored in a short-lived blob (`dsr-exports` bucket) and expires after `export_retention_days` (default 30, 1–365). It is downloaded through a signed link minted per request and valid for 5 minutes, so access follows the permission, not whoever holds a URL; each download is audited. A download after expiry is refused (410) even if the cleanup job has not run, the hourly job `compliance.dsr_exports_cleanup` only reclaims space, and erasing the subject deletes their stored exports (an internal PII producer), including one created while the erasure ran. Without an object store creation answers 503. The self-service inline export is unchanged.
- **Dry-run preview.** `GET …/dsr/preview` shows, without deleting anything, the active legal holds, the retention obligation, the subject's open requests and, per producer, how many rows an erasure would touch. Counting is an optional companion of `iface.PIIProducer`, `iface.PIIProducerCounter`, using the same filters as the purge; a producer without it is reported as not countable. A counter is bounded (5 s) and a failure or panic affects only its own line. Counts are per producer and not summed, because two producers can report the same stored rows. Each preview is audited (counts only).
- **Admin-opened erasure requests.** An operator can open a request for a subject (reason mandatory, mode chosen). One open request per subject at a time; a subject under an active hold cannot be opened. The request list can be filtered by status and subject, and closed requests stay visible.
- **N-of-M approvals.** `erasure_required_approvals` (1–5, default 1, today's behaviour) is copied onto each request when it is created, so a later configuration change never unlocks or locks a request already open. Approvals are from distinct operators, the subject never approves or executes their own erasure, and executing counts as the executor's approval; below the threshold execution is refused with the number held and needed and nothing is recorded. Approvals are compare-and-set on the request, so simultaneous approvers neither duplicate nor lose an approval. The first approval fixes the erase mode and a different mode is refused. Self-service erasure, retention reaping and retries of a request already started do not need approvals.
- **Minimum-retention gate on hard delete.** `min_retention_years` (0–30, default 0 = off) refuses a hard delete until that many years after the account's creation; modules can declare longer obligations through the optional companion `iface.PIIRetentionReporter` (a date and a machine-readable reason, no personal data). The gate applies on every hard-delete path (operator execute, self-service erase, retries, retention reaper) before any producer runs, answers 409 with the date and reasons, and fails closed when a reporter errors. Anonymization is not gated. Retention minima are a policy choice of the deployer; the platform imposes none.
- **Erasure receipt.** A completed or partially closed request yields a structured receipt built from the retained request row: subject and operator UUIDs only, approvals with their masked notes, per-producer outcome and row counts, totals, abandoned keys. An audit event is emitted once at closure; the row outlives the audit TTL.
- **Legal holds, extended.** A hold may carry a review date (a flag, never a release), an expiry date (must be in the future) and notes; releasing needs a reason. A lapsed hold stops blocking at its instant (reads are expiry-aware), and the hourly job `compliance.legal_hold_expiry` records the release exactly once even with several replicas. Placing, changing, annotating, releasing and expiring a hold are audited.

Out of scope for now (see the plan): data subjects that are not users, module-declared retention classes and statistics, review notifications, and per-subject crypto-shred.

## Consequences

### What we enable

- **GDPR data-subject rights + an audit trail are guaranteed in every deployment** — not a fork's responsibility, not silently absent.
- **`iface.PIIProducer` becomes load-bearing**: any module (core or fork) holding personal data implements it and is automatically covered by export/erase.
- **Accountability is on by default** — the previously-nil audit sink now records every lifecycle + DSR event.

### What we give up

- **Core grows by one module — the thing ADR-0006 minimized.** Accepted: the data is core-owned, so this passes ADR-0006's own core-vs-vertical test. This is an explicit *amendment* to its catalog, not a violation of its principle.
- **A compliance opinion is now baseline.** Mitigated by dormancy (nothing is erased/exported until invoked; retention auto-cleanup defaults off), the SOC2 gate (D5), and KMS being optional. A non-GDPR deployment pays only a few always-registered routes + collections.
- **The freeze-surface seam grew** (`EraseMode` + the `PurgePersonalData` signature). Minimal, and `PurgeResult` already carried `RowsAnonymized`.

### Forbidden patterns

- **Introducing a second DSR/erasure seam** beside `iface.PIIProducer` — duplicates a shipped contract (D2).
- **Shipping SOC2 on by default**, or hard-requiring KMS — both re-impose opinions D4/D5 keep optional.
- **The compliance module importing another module's internals** to reach personal data — all access is through the producer registry.

## Implementation

Phased, each gated by `make ci-backend` green; reversible until merge. Detailed file-level plan in [`docs/plans/compliance-module.md`](../plans/compliance-module.md).

| Phase | Scope | Status |
|---|---|---|
| **1** | Recover `compliance` from git → `internal/core/compliance/`; port to the current tree (SDK paths, drop removed-addon coupling, ADR-0003 `RouteInfo`, ctxauth, per-tier collections); register in `coreModules()`. | ✅ done — builds, vets, tests pass |
| **2** | SOC2 gate behind `compliance.soc2_enabled` (default off). | ✅ done |
| **3** | Complete producer coverage: `tenant`, `authz`, `notification` producers (user/auth already shipped). | ✅ done |
| **4** | `iface.EraseMode` (anonymize \| hard-delete) on the seam; user anonymizes (tombstone), satellites hard-delete; threaded through DSR + handlers. | ✅ done |
| **5** | Grafts (D6): legal hold, retention `Startable` (off by default) + dry-run preview, export→RustFS blob+TTL, erasure-request workflow, step-up on destructive routes. | ✅ done (export to blob and the admin side: D7) |
| **6** | `frontend-admin` (audit events, DSR, legal holds, retention, optional SOC2) + `frontend-client` self-service; CLAUDE.md; OpenAPI regen. | ✅ admin console done (DSR, holds, requests, receipts); client self-service as before |

## Alternatives considered

1. **Keep compliance an optional addon** (status quo post-ADR-0006). Rejected — the personal data is core-owned; an addon makes subject-rights *and the audit trail* omissible on a platform that stores PII the moment it boots.
2. **Introduce a new `DataSubjectProvider` pull seam** (this ADR's earlier draft). Rejected — `iface.PIIProducer`/`PIIProducerRegistry` already shipped and survived ADR-0006, with `user`+`auth` producers live; a parallel seam would duplicate a frozen-surface contract and split producer registration.
3. **Bring SOC2 in always-on.** Rejected — not universally needed; gated behind a default-off flag (D5).
4. **Hard-delete only (no anonymize).** Rejected by decision — anonymize preserves referential integrity for the canonical identity row and is the basis of the retention tombstone model (D3).
5. **Per-subject crypto-shred now.** Deferred — the per-tenant KMS + crypto-shred-on-purge already exists; per-subject keying (and an `EraseMode` crypto-shred variant) is a follow-up, not blocking the field-level baseline.
