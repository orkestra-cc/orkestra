# `compliance` — audit trail + GDPR data-subject rights (core module)

Core module (ADR-0009). Always-on. Owns the platform **audit trail**, the **GDPR DSR pipeline** (right-of-access export + right-to-erasure), **per-tenant KMS** crypto-shred, **legal hold**, **retention auto-cleanup**, the mediated **erasure-request workflow**, and (gated off by default) **SOC2 evidence**.

Re-homed from the ADR-0006-removed `internal/addons/compliance` addon; see [ADR-0009](../../../../docs/adr/0009-core-compliance-module.md) + [plan](../../../../docs/plans/compliance-module.md).

## Layout

```
module.go                         Module: collections, permissions, config, nav, Init (wires sink/DSR/KMS/legal-hold/retention/workflow), Start/Stop (retention ticker), routes
services/
  sink.go                         AuditSink impl (append-only compliance_audit_events); satisfies iface.AuditSink
  dsr.go                          DSRService: Export + Erase over iface.PIIProducerRegistry; EraseMode; legal-hold gate (ErrLegalHoldActive)
  legal_hold.go                   LegalHoldService (Place/Release/ListActive/IsHeld) — IsHeld satisfies DSRService.LegalHoldChecker
  retention.go                    RetentionService: daily Loop reaping anonymized user tombstones via the DSR (off by default)
  erasure_request.go              ErasureRequestService: lodge / list / execute / reject
  kms.go                          Local KMS provider (per-tenant envelope encryption + crypto-shred); satisfies iface.KMSProvider
  soc2.go                         SOC2 evidence aggregation (gated)
handlers/                         admin (audit list), me (self-service DSR), legal_hold, retention (preview), erasure_request, soc2
repository/                       Mongo persistence per collection
models/                           audit_event, legal_hold, erasure_request, kms_key
```

## Key contracts & invariants

- **DSR reuses the existing `iface.PIIProducer` seam — do NOT add a parallel one.** Producers register themselves with the `PIIProducerRegistry` (created in `cmd/server/main.go` before `InitAll`) during their own `Init`. Live producers: `user`, `auth`, `tenant`, `authz`, `notification`. `DSRService` walks `registry.List()` at request time, so producer registration order/timing doesn't matter.
- **EraseMode (`iface.EraseMode`)**: `EraseAnonymize` keeps the user identity row (UUID survives, email aliased, profile blanked — the tombstone retention later reaps); `EraseHardDelete` removes it. Satellite producers (auth/tenant/authz/notification) delete in **both** modes (their rows are pure user linkage — no anonymizable residue).
- **Audit is fire-and-forget + nil-safe.** `sink.Emit` never returns an error and writes on a detached context so a cancelled request can't abort the insert. Other core modules consume the sink via `SetAuditSink` and nil-check it; this module provides it.
- **Legal hold blocks erasure platform-wide.** `DSRService.Erase` (and therefore retention + the workflow's execute) returns `ErrLegalHoldActive` → `409` when any active hold exists for the subject.
- **Retention is OFF by default** (`auto_cleanup_enabled=false`) and reads config fresh each run. The ticker is launched by `Start()` but `RunOnce` no-ops while disabled. It hard-erases through the DSR, so the legal-hold gate applies.
- **SOC2 is gated** behind `compliance.soc2_enabled` (default false): handler, routes, and nav item stay dormant unless enabled. Audit + DSR are ungated.
- **Cross-module collection names are inlined as string constants** (`operator_users`, `client_users`, `operator_mfa_factors`) in `services/soc2.go` and `services/retention.go` — compliance must NOT import other modules' packages. Keep these in lock-step with the owning module if it renames a collection.
- **`//tenantscope:allow` on DSR-by-user queries**: erasure/export/legal-hold/retention scan by data-subject and are deliberately cross-tenant (a hold blocks erasure platform-wide; retention scans all tenants). Each such query is annotated.

## Permissions

- `system.compliance.audit.read` — audit list + DSR/legal-hold/retention/erasure-request reads.
- `system.compliance.legalhold.manage` — place/release holds (+ step-up).
- `system.compliance.dsr.manage` — execute/reject erasure requests (+ step-up).

All three are `System: true`. `audit.read` is Cedar-covered by the `read` suffix clause; the two `.manage` keys are named as operator-only tier-aware forbids in [`authz/cedar/policies/tenant_scope.cedar`](../authz/cedar/policies/tenant_scope.cedar) (vetoed against `external` tenants) — that literal is also what satisfies the `policycoverage` CI gate. A new `system.compliance.*` permission with an uncovered suffix must add its own Cedar reference there or CI fails with `permission.cedar.unreferenced`.

## Config (`ConfigSchema`)

`soc2_enabled` (bool, false) · `auto_cleanup_enabled` (bool, false) · `retention_years` (int, 5) · `export_retention_days` (int, 30).

## Routes

- Self-service: `POST /v1/me/dsr/{export,erase,erasure-request}`.
- Admin reads (audit.read): `GET /v1/admin/audit-events`, `…/compliance/legal-holds`, `…/compliance/retention/preview`, `…/compliance/erasure-requests`; SOC2 `…/compliance/soc2` (when enabled).
- Admin writes (step-up): `POST/DELETE …/compliance/legal-holds[/{id}]` (legalhold.manage); `POST …/compliance/erasure-requests/{id}/{execute,reject}` (dsr.manage).

## Follow-ups

- **Export → blob (deferred):** the inline `/me/dsr/export` returns the full bundle and satisfies Art. 15. A blob+TTL variant for large payloads is blocked on `blob.Store` exposing only client-driven `PresignPut` (no server-side `Put`). See the plan.
- Per-subject crypto-shred (today KMS is per-tenant); a static lint flagging a `userUUID`-owning collection without a `PIIProducer`.
