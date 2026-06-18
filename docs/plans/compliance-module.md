# Plan — Core `compliance` module (audit + GDPR DSR)

**Status:** 🟡 In progress — implements [ADR-0009](../adr/0009-core-compliance-module.md). Phases 1–4 shipped (recover + port + core wiring + SOC2 gate + 5-module producer coverage + erase modes); phases 5–6 (grafts + frontend) planned. Source: the pre-ADR-0006 `compliance` addon recovered from git (`git show e9d703f5:backend/internal/addons/compliance/...`), re-homed and completed.

## Context

ADR-0006 removed the compliance addon but left the `iface.PIIProducer` producer seam (with `user`+`auth` producers) and nil-checked `AuditSink`/`KMSProvider` setters in core. This plan brings back the **consumer** (DSR pipeline, audit sink impl, KMS provider, SOC2) as a **core** module reusing that seam, completes producer coverage, adds erase modes, and grafts legal-hold/retention/export-blob/workflow. See ADR-0009 for the rationale (the data is core-owned).

## Architecture decisions

- **Reuse `iface.PIIProducer`/`PIIProducerRegistry`** (created in `cmd/server/main.go`); do not add a parallel seam. The compliance `DSRService` walks `registry.List()`.
- **Five producers** cover all core PII: `user`, `auth` (already shipped), + new `tenant`, `authz`, `notification`.
- **`iface.EraseMode`** (`EraseHardDelete` | `EraseAnonymize`): user anonymizes (alias email + tombstone, keep UUID); satellites hard-delete in both modes.
- **Audit sink + KMS are core-provided** by this module (previously nil in base). Audit emit stays fire-and-forget + nil-safe everywhere it's consumed.
- **SOC2 gated** behind `compliance.soc2_enabled` (`FieldBool`, default false). Audit + DSR ungated.
- **Two audiences (ADR-0003)**: admin reads on `ri.Operator`; DSR self-service (`/v1/me/dsr/*`) behind `RequireGlobal()`. Destructive + bulk-export routes get `RequireStepUp` (graft).
- **Tenant scoping**: DSR-by-user queries are deliberately cross-tenant-by-data-subject; new repo methods carry `//tenantscope:allow` (mirror existing by-user methods).

## Shipped (phases 1–4)

### Backend — `internal/core/compliance/` (recovered + ported)
- `module.go` — core Module: `Collections()` (`audit_events` w/ TTL, `kms_keys`), `Permissions()` (`system.compliance.audit.read`), `ConfigSchema()` (`soc2_enabled`), `NavItems()` (Audit Events always; SOC2 only when enabled), `Dependencies()=["user","auth","tenant"]`, `ProvidedServices()=[ServiceAuditSink]`, `Init` (sink + DSR + KMS + push sink into auth/tenant), `RegisterRoutes` (operator + me).
- `services/{dsr,sink,kms,soc2}.go`, `handlers/{admin,me,soc2}_handler.go`, `repository/{audit_event,kms}_repo.go`, `models/{audit_event,kms_key}.go`.
- `services/dsr.go` — `Export(ctx,uuid)` + `Erase(ctx,uuid,mode)` over the registry; audit row per op (mode label included).
- Registered in `cmd/server/catalog.go` `coreModules()`.

### Backend — new producers + SDK
- `pkg/sdk/iface/interfaces.go` — added `EraseMode` + `PurgePersonalData(ctx,uuid,mode)`.
- `internal/core/tenant/services/pii_producer.go` + repo `DeleteMembershipsByUser`.
- `internal/core/authz/services/pii_producer.go` + repo `ListBindingsByUser`/`DeleteBindingsByUser`.
- `internal/core/notification/services/pii_producer.go` (direct DB on `notification_messages`/`notification_preferences`).
- `internal/core/{user,auth}/services/pii_producer.go` — updated to the new `PurgePersonalData` signature (user branches on mode).
- Producer registration added to each module's `Init`.

*All of the above: `go build ./...`, `go vet`, and the compliance package tests pass.*

## Phase 5 — grafts (shipped, except export→blob)

Shipped — `go build ./...` / `go vet` / package tests green. New files under `internal/core/compliance/`:
- ✅ **Legal hold** — `models/legal_hold.go`, `repository/legal_hold_repo.go`, `services/legal_hold.go`, `handlers/legal_hold_handler.go`. Collection `compliance_legal_holds` (`(userUuid,active)` index). `DSRService.Erase` checks `IsHeld(userUUID)` (via `SetLegalHoldChecker`) → `ErrLegalHoldActive` → `409`. Place/release admin routes behind `system.compliance.legalhold.manage` + `RequireStepUp`.
- ✅ **Retention auto-cleanup** — `services/retention.go`; module implements `Startable`/`Stoppable` (24h `time.Ticker`). Reaps anonymized tombstones (`operator_users`/`client_users` `deletedAt < now - retention_years`) by hard-erasing through the DSR pipeline (so the legal-hold gate applies). Config: `retention_years` (5), `auto_cleanup_enabled` (**false**). Dry-run preview `GET /v1/admin/compliance/retention/preview`.
- ✅ **Erasure-request workflow** — `models/erasure_request.go` + repo + service + handler. Collection `compliance_erasure_requests`. Client lodges `POST /v1/me/dsr/erasure-request`; operator lists (`GET /v1/admin/compliance/erasure-requests`) and executes/rejects (`…/{id}/execute|reject`, choosing `EraseMode`) behind `system.compliance.dsr.manage` + `RequireStepUp`.
- ✅ **Config schema** additions: `auto_cleanup_enabled`, `retention_years`, `export_retention_days`, `soc2_enabled`.
- ⏸ **Export → blob — deferred.** The inline synchronous export (`POST /v1/me/dsr/export`) already satisfies right-of-access (Art. 15) — it returns the full bundle. Blob+TTL is an optimization for large payloads. Blocked on a seam gap: `blob.Store` exposes only client-driven `PresignPut` (no server-side `Put`), so a server-assembled bundle can't be written without either extending the interface or doing a presigned-PUT-then-HTTP-PUT from the backend. Tracked as a follow-up; `export_retention_days` config is already in place for when it lands.

## Planned (phase 6 — frontend + docs)
- `frontend-admin/src/pages/admin/compliance/` (audit events, DSR, legal holds, retention preview; SOC2 page when enabled) + `store/api/complianceApi.ts` + routes. Nav is backend-driven.
- `frontend-client/` self-service: download my data + request erasure.
- `internal/core/compliance/CLAUDE.md` (required by CONTRIBUTING) + update touched modules' CLAUDE.md.
- OpenAPI regen (`openapi/enterprise.json`) — compliance adds routes; `make openapi-check` must be green.

## Verification

Per phase, plus end-to-end on the dev stack (`docker compose -f docker-compose.dev.yml up -d`):
- **DSR export/erase across 5 producers**: throwaway user with data in user/auth/tenant/authz/notification → `/v1/me/dsr/export` bundles all; `/erase` (hard) wipes all; `anonymize` keeps the aliased user tombstone, satellites gone; both emit an `gdpr.dsr.*` audit event with the mode.
- **SOC2 gate**: with `soc2_enabled=false`, `/admin/compliance/soc2` absent + nav hidden; flip true → present.
- **Legal hold** (phase 5): hold → erase `409`; release (step-up) → erase ok.
- **Retention** (phase 5): `auto_cleanup_enabled=false` → preview lists, ticker purges nothing; true → anonymized tombstones past `retention_years` hard-deleted, audited.
- **Decoupling**: `go list -deps ./internal/core/compliance` references no other `internal/core/*` internals beyond `pkg/sdk` + shared infra.
