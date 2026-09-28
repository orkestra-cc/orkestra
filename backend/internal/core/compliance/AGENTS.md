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
- **SOC2 is gated** behind `compliance.soc2_enabled` (default false) — but **at request time, not boot time**, so the toggle works without a restart. The handler + route (`GET /v1/admin/compliance/soc2/evidence`) are mounted unconditionally; the handler 404s when the flag is off (`SOC2Handler.enabled` closure, read live via `deps.GetConfigBool`). The `SOC2 Evidence` nav item is emitted unconditionally with `NavItemSpec.RequiresConfig: "soc2_enabled"` and hidden by the navigation filter when off — **don't** gate it inside `NavItems()` on an Init-set bool: `NavItems()` is collected before `Init` runs, so the flag is always false there. Audit + DSR are ungated.
- **Cross-module collection names are inlined as string constants** (`operator_users`, `client_users`, `operator_mfa_factors`) in `services/soc2.go` and `services/retention.go` — compliance must NOT import other modules' packages. Keep these in lock-step with the owning module if it renames a collection.
- **`LocalKMS.CreateKey` is idempotent under concurrency, not just for sequential callers.** Two racing calls for the same tenantUUID both converge on the single winning keyID: `repository.KMSKeyRepository.Insert` translates the unique `tenantUuid` index's duplicate-key rejection into the exported `ErrKMSKeyExists` sentinel, and `CreateKey` rereads the winning row via `GetByTenant` rather than surfacing the raw error. This is load-bearing for a resumable setup saga that may replay the key-creation stage after a crash or a lost response — a second DEK for one tenant would orphan the ciphertexts wrapped with the first on crypto-shred (they'd never be shredded, since `DeleteKey` only ever targets the one keyID the tenant row remembers).
- **`//tenantscope:allow` on DSR-by-user queries**: erasure/export/legal-hold/retention scan by data-subject and are deliberately cross-tenant (a hold blocks erasure platform-wide; retention scans all tenants). Each such query is annotated.
- **Module-config mutations are audited by the SDK admin handler, not by this module.** `pkg/sdk/module.ModuleAdminHandler` emits one event per actual mutation result through `SetAuditSink`/`SetActorResolver` (wired in `cmd/server/admin_wiring.go`): `module.config.updated` (`PATCH /v1/admin/modules/{name}` and `…/environments/{env}`, `Metadata.env`), `module.config.environment_activated` (`PUT …/active-environment`), `module.enabled` / `module.disabled` (the `enabled` half of a PATCH — a separate event from the config half, each with its own outcome). `ResourceType: "module"`, `ResourceID: <name>`, `Outcome` uses the existing `success`/`failure` vocabulary. **Metadata carries key NAMES only** (`keys`, `secretKeys` — the key names the request submitted, so a re-sent unchanged value is listed; schema-derived, sorted, ≤ 64; record-list element keys collapsed to their schema item names; unknown request keys only as `unknownKeyCount`), the stable `code` on failure (validation codes, `module.config_revision_stale`) and `requestId` — never a value, never a secret. The actor is `ActorUserID` + tenant context + IP + User-Agent; **`ActorEmail` is deliberately empty** (the UUID is the attribution; email is mutable PII). Persistence is **best-effort** under this sink's contract — `Emit` returns nothing, may add its bounded 2 s insert latency, and a failed insert is a structured WARN naming `action`/`resourceType`/`resourceId`/`outcome` (never the payload), not a rolled-back config change. This is not complete SOC2 evidence; guaranteed evidence needs the durable-outbox follow-up in the password-login-toggle spec §8. The events reuse `compliance_audit_events` and its existing two-year TTL; actor UUID, tenant context, IP and User-Agent are retained only for privileged-change forensics. **Deployers remain responsible for documenting the lawful basis and retention of these records in their RoPA/privacy materials** — the module records them, it does not decide the legal basis.

## Permissions

- `system.compliance.audit.read` — audit list + DSR/legal-hold/retention/erasure-request reads.
- `system.compliance.legalhold.manage` — place/release holds (+ step-up).
- `system.compliance.dsr.manage` — execute/reject erasure requests (+ step-up).

All three are `System: true`. `audit.read` is Cedar-covered by the `read` suffix clause; the two `.manage` keys are named as operator-only tier-aware forbids in [`authz/cedar/policies/tenant_scope.cedar`](../authz/cedar/policies/tenant_scope.cedar) (vetoed against `external` tenants) — that literal is also what satisfies the `policycoverage` CI gate. A new `system.compliance.*` permission with an uncovered suffix must add its own Cedar reference there or CI fails with `permission.cedar.unreferenced`.

## Config (`ConfigSchema`)

`soc2_enabled` (bool, false) · `auto_cleanup_enabled` (bool, false) · `retention_years` (int, 5) · `export_retention_days` (int, 30).

`ConfigGroups()` puts these on the full-page rail as two groups: `soc2` ("SOC2 evidence", just `soc2_enabled`) and `retention` ("Retention & DSR", the other three fields). `retention_years` carries `DependsOn: auto_cleanup_enabled in [true]` — it only matters once the reaper is on, so it stays hidden until then (4 → 3 visible fields at the default).

**`export_retention_days` is deliberately NOT gated on `auto_cleanup_enabled`.** It governs the download TTL of a DSR export — the always-on right-of-access pipeline — which is independent of the retention cleanup job; hiding it behind the cleanup toggle would make an always-relevant setting invisible on a fresh install where auto-cleanup is off. Do not "tidy up" the two `retention` fields into a single `DependsOn` block. `config_groups_test.go` plus this note are the guard rail against that regression.

## Routes

- Self-service: `POST /v1/me/dsr/{export,erase,erasure-request}`.
- Admin reads (audit.read): `GET /v1/admin/audit-events`, `…/compliance/legal-holds`, `…/compliance/retention/preview`, `…/compliance/erasure-requests`; SOC2 `GET …/compliance/soc2/evidence` (always mounted; 404 when `soc2_enabled` is off).
- Admin writes (step-up): `POST/DELETE …/compliance/legal-holds[/{id}]` (legalhold.manage); `POST …/compliance/erasure-requests/{id}/{execute,reject}` (dsr.manage).

## Follow-ups

- **Export → blob (partly unblocked):** the inline `/me/dsr/export` returns the full bundle and satisfies Art. 15. The blob+TTL variant for large payloads was blocked on `blob.Store` having no server-side write — that seam now exists (`blob.Store.Put(ctx, key, contentType, io.Reader)`, added as a follow-up). Remaining work: wire a `blob.Store` into the compliance module and add an export-to-blob path that stores the bundle then hands the subject a short-lived `PresignGet` URL. See the plan.
- Per-subject crypto-shred (today KMS is per-tenant).
- ✅ **Static lint shipped:** `tools/piiscan` (CI gate `make backend-piiscan`) flags any module whose persisted model carries a data-subject `bson` field but registers no `iface.PIIProducer`, so new PII can't silently escape the DSR sweep. The compliance module itself is baselined (`tools/piiscan/baseline.txt`) — its erasure-request / legal-hold / audit rows reference the subject but are retained by design, not self-erased.
