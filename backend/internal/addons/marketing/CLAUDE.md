# Module: Marketing

_Path: `/backend/internal/addons/marketing`_
_Parent: [../../../CLAUDE.md](../../../CLAUDE.md)_

[← Backend](../../../CLAUDE.md) | [☰ Module Map](../../../../../CLAUDE.md#module-map)

## Module home

This directory is a **separate Go module**
(`github.com/orkestra-cc/orkestra-addon-marketing`). Source lives
in-tree at this path for monorepo development; the same tree will be
mirrored to
[github.com/orkestra-cc/orkestra-addon-marketing](https://github.com/orkestra-cc/orkestra-addon-marketing)
and tagged starting from `v0.1.0` once Phase 1 stabilizes in `dev`.
Backend's `go.mod` carries a `replace` directive pointing at this
path so changes here take effect without a tag bump during
cross-cutting work; CI and external consumers will fetch the
published version through the Go module proxy.

## Status

**Phase 1 (Fondazione anagrafica MVP) — shipped 2026-05-19** as
`52ca4b6` on `dev`. Six MongoDB collections, ~16 HTTP routes under
`/v1/marketing/*`, a synchronous CSV importer with email/VAT dedup,
and the operator-console UI.

**Phase 2 (Storicizzazione & scoring) — shipped on
`feature/marketing-phase-2`.** Three more collections
(`marketing_activities`, `marketing_score_profiles`,
`marketing_score_snapshots`), the pure score engine
(`scoring/Engine.Compute(activities, profile, asOf) ScoreResult`),
the eager + nightly recompute scheduler, 8 new HTTP routes, and
the contact-detail Timeline + Scores tabs + the `/marketing/scoring`
admin page on the operator console.

**Phase 3 (Import avanzati) — shipped on
`feature/marketing-phase-3`.** One more collection
(`marketing_conflict_reviews`), async import runner with bounded
queue + disk-spooled payloads + boot-recovery, Excel adapter via
`xuri/excelize`, Odoo 19 JSON-2 adapter (replaces the design doc's
XML-RPC sketch — Odoo 19's new REST surface is cheaper to consume),
engagement-CSV column detection, soft-match dedup helpers
(person first+last+phone, organization legal_name normalized),
auto-emission of `tag_added` / `tag_removed` / `imported` /
`merged` activities, 4 new HTTP routes for the review queue, and
the `/marketing/reviews` admin page + side-by-side resolver modal
+ adapter picker on the import wizard.

Design rationale, per-collection schemas, and the per-phase
execution plans live in the monorepo:

- [`docs/plans/marketing-addon/Orkestra_marketing_addon.md`](../../../../docs/plans/marketing-addon/Orkestra_marketing_addon.md) — full design (716 lines)
- [`docs/plans/marketing-addon/schemas/`](../../../../docs/plans/marketing-addon/schemas/) — per-collection field-by-field schemas
- [`docs/plans/marketing-addon/IMPLEMENTATION_PLAN.md`](../../../../docs/plans/marketing-addon/IMPLEMENTATION_PLAN.md) — Phase 1 execution plan
- [`docs/plans/marketing-addon/IMPLEMENTATION_PLAN_PHASE_2.md`](../../../../docs/plans/marketing-addon/IMPLEMENTATION_PLAN_PHASE_2.md) — Phase 2 execution plan
- [`docs/plans/marketing-addon/IMPLEMENTATION_PLAN_PHASE_3.md`](../../../../docs/plans/marketing-addon/IMPLEMENTATION_PLAN_PHASE_3.md) — Phase 3 execution plan

## What it does today

The addon owns **ten** MongoDB collections (all `marketing_*` prefixed):

| Collection | Purpose | Key indexes |
|---|---|---|
| `marketing_organizations` | Anagrafica enti (B2B + PA + foundations + associations) | unique sparse on `(tenantId, vat)` / `(tenantId, taxCode)`; multikey on tags; sort on updatedAt |
| `marketing_persons` | Persone fisiche, importer dedup primary key on `emails.address` | unique sparse multikey on `(tenantId, emails.address)`; tags + activeCardUuids (Phase 4 reserved) multikey |
| `marketing_memberships` | Person↔Organization relations with role + period | compound on `(tenantId, personUuid, orgUuid)` and `(tenantId, personUuid, primary)` |
| `marketing_tags` | Hierarchical labels with materialized path | unique `(tenantId, slug)`; prefix-scan on `path` |
| `marketing_custom_field_schemas` | Per-tenant typed bag declaration (one row per `(tenant, persons|organizations)`) | unique `(tenantId, targetCollection)` |
| `marketing_import_jobs` | Audit log of every CSV import run | sort on `(tenantId, createdAt)` |
| `marketing_activities` | Append-only event log (Phase 2). One row per touchpoint with `personUuid` + `kind` + `occurredAt` + `recordedAt` + `payload` + `refs`. | unique `dedupKey` (idempotent re-imports, decision D21); compound `(tenantId, personUuid, occurredAt desc)` for timeline read; compound `(tenantId, kind, occurredAt desc)` for engine read; sparse on every `refs.*` field |
| `marketing_score_profiles` | Per-tenant scoring configuration (Phase 2). Rules, decay, filters. | unique `(tenantId, name)`; index on `(tenantId, active)` |
| `marketing_score_snapshots` | Rebuildable cache of per-(person, profile) score values (Phase 2). | unique `(tenantId, personUuid, profileUuid)` as upsert key; index on `(tenantId, profileUuid, value desc)` for leaderboard; index on `(tenantId, profileUuid, stale)` for the nightly drain |
| `marketing_conflict_reviews` | Queue of importer-flagged dedup conflicts awaiting operator resolution (Phase 3). One row per parked record + the parked-row activities + the conflict-field diff. | compound on `(tenantId, status)` for the pending-queue read; `(tenantId, importJobUuid)` for the per-job listing; `(tenantId, targetKind, status)` for type-scoped queries; `(tenantId, existingUuid)` for "this record has pending reviews" lookups |

The HTTP surface (registered via permission-bucketed chi subgroups
on the operator router) is:

- `marketing.contact.read`        → GET on all 5 contact collections + GET on imports + GET on per-person activity timelines + GET on score profiles, snapshots, per-profile leaderboards, and the conflict-review queue (Phase 2 plan §2.3 deliberately folds activity/scoring reads into contact-read since granting one without the other is semantically incoherent; Phase 3 extended the same fold-in to the review queue)
- `marketing.contact.write`       → POST + PATCH on the 5 contact collections (custom-field-schemas via PUT)
- `marketing.contact.delete`      → DELETE on the 5 contact collections
- `marketing.import.run`          → POST `/v1/marketing/imports` (multipart upload, Phase 3 returns 202 Accepted + jobUuid; the wizard polls the imports list for completion). Accepts an optional `Idempotency-Key` header — same key within 24h returns the existing job UUID instead of creating a duplicate.
- `marketing.activity.write`      → POST `/v1/marketing/activities` (manual log, restricted to `ManualKinds`: call_made, meeting_held, note_added, corrected_by) + POST `/v1/marketing/activities/{id}/correct` (insert a `corrected_by` row that supersedes the original)
- `marketing.score_profile.write` → POST + PATCH (full-body replace) + DELETE on `/v1/marketing/score-profiles` (Save bumps version + bulk-marks downstream snapshots stale; Delete cascades to snapshots)
- `marketing.conflict.resolve`    → POST `/v1/marketing/conflict-reviews/{id}/resolve` (keep_existing / take_incoming / manual_merge) + POST `/v1/marketing/conflict-reviews/{id}/dismiss`. Distinct from contact-write because operators may be granted queue-management authority without full record-edit access (e.g. import operators triaging but not curating contacts outside imports).

### Scoring engine

`scoring/Engine.Compute(activities, profile, asOf) ScoreResult` is a
**pure function** — no DB calls, no `time.Now()` reads beyond the
caller-supplied `asOf`. Three decay branches (`none` / `linear` /
`exponential`), Mongo-style payload matcher (`$eq` / `$ne` / `$in` /
`$exists` + implicit equality), polymorphic `activityKind` selector
(string | []string | "*"), per-rule `Cap` that scales contributing
breakdown entries proportionally so the sum of breakdown entries
always equals the (capped) Value. Time-travel queries pass a past
`asOf`. Test coverage 95.2%.

`ScoreService` orchestrates eager (on activity insert) + nightly
(`RecomputeJob` ticker, 24h default) recomputes. Per-(tenant, person)
mutex via `sync.Map` of `*sync.Mutex` so concurrent activity inserts
on the same person serialise on the snapshot upsert; different
persons recompute in parallel. The nightly job uses
`ListStaleAcrossTenants` — the one tenantrepo bypass in this package,
documented loudly in `repository/score_snapshot_repo.go`.

`ScoreProfileService.Save` runs Validate → `BumpVersion(now)` →
`ReplaceOne` (rules arrays are full-replace semantics, never
$set-merged) → `InvalidateProfile` (bulk flip snapshots
stale=true). The version bump on disk is the durable invalidation
signal — `IsStaleAgainst(profileVersion)` catches drift at read
time even if MarkStale fails.

### Importer pipeline

`POST /v1/marketing/imports` returns `202 Accepted + jobUuid`
(Phase 3 moved execution behind a background worker). The handler
persists the upload to disk under `MARKETING_IMPORT_SPOOL_DIR`
(default `/var/lib/orkestra/marketing/spool`), records the job in
`queued`, and hands it to the in-process worker queue. Idempotency:
`sha256(body || canonical_mapping)` — same payload inside 24h
returns the existing UUID (override via the `Idempotency-Key`
header).

The worker (`importers/worker.go`) runs N goroutines (default 2,
`MARKETING_IMPORT_WORKER_CONCURRENCY`), each draining a bounded
channel (default buffer 32, `MARKETING_IMPORT_QUEUE_BUFFER`). On
boot it re-enqueues every `queued` job whose spool file is still
present + flips every stuck-`running` job to
`failed (runner_crash)`. Stop drains in-flight work with a 30s
graceful timeout, then cancels the context.

Three adapters land canonical records into the pipeline:

- `importers/csv/` — encoding/csv reader, header- or numeric-key
  mapping, semicolon-split for multi-value fields. Phase 3 added
  engagement-CSV column detection: when the header carries
  `email_opened` / `email_clicked` / `email_bounced` /
  `email_unsubscribed` / `email_complained` / `form_submitted` /
  `page_visited` / `event_attended` (case-insensitive after
  TrimSpace), the source can emit Activity rows alongside the
  CanonicalRecord stream. Pairing with `occurred_at` preserves the
  original event time.
- `importers/excel/` — Phase 3 (`xuri/excelize`). Defaults to the
  first sheet by index; `Options["sheet"]=<exactName>` selects a
  specific one, `Options["headerRow"]=N` skips banner rows, and
  `Options["hasHeaderRow"]="false"` switches to numeric-key
  mapping. Cell values come out as display strings — numbers stay
  readable (no scientific notation for big VAT integers).
- `importers/odoo/` — Phase 3. Targets Odoo 19's External JSON-2
  API (NOT the design doc's XML-RPC sketch — Odoo 19's REST
  surface is cheaper to consume). The "file" the wizard uploads
  carries a JSON `OdooImportConfig` (baseUrl, database, apiKey,
  pageSize, includeEngagement, engagementSinceDays); the adapter
  pages res.partner in batches of 200, resolves parent + category
  names in one follow-up SearchRead per page, and projects each
  row into either an Organization, Person-under-parent (with the
  parent's legalName carried on the same CanonicalRecord so the
  pipeline links the Membership), or standalone Person.

The pipeline itself (`importers/pipeline.go`) dedups against the
tenant's existing data — Person primary key `primary_email`
(normalised lowercase), Organization primary key `vat` → `taxCode`
fallback (both normalised uppercase). Matched rows go through the
auto-merge policy: fill empty scalars, set-union additive arrays
(tags / emails / phones / sources). Conflicts on the dedup-key
fields route to `marketing_conflict_reviews` via the
`ReviewParker` seam — the parent job transitions to
`paused_for_review` on the first park and back to `done` when the
last pending review closes. When `ReviewParker` is nil (unit-test
setups), the Phase-1 `Stats.ConflictsSkipped` counter is the
fallback.

Soft-match dedup (`importers/match/`) — exposed for the pipeline
to layer on top of the strict-match pass. `SoftMatchPerson` checks
first+last (case-insensitive) AND any phone overlap (digits-only
last-10); `SoftMatchOrganization` does exact-after-normalize on
`legal_name`. Soft-match hits ALWAYS route to the review queue —
never auto-merge — because the false-positive rate is too high to
commit without operator review.

Activity auto-emission (`services/activity_emit.go`) fires
alongside the pipeline:

- `imported` — emitted on every committed Person row (created or
  merged-into). ExternalID `imported:<jobUuid>:<personUuid>`
  makes re-imports idempotent.
- `tag_added` / `tag_removed` — emitted by the
  `PersonService.RegisterUpdateListener` hook when the diff
  between before + after snapshot detects a tag-set delta.
- `merged` — emitted by `ConflictReviewService.Resolve` when an
  applied resolution closes a Person-target review. Org-target
  reviews skip emission until a per-Org `merged` kind is added in
  Phase 4.

The canonical-field set the operator can map to:
`org.{legalName,vat,taxCode,kind,website,email,phone}`,
`person.{firstName,lastName,email,phone,title,language}`,
`role`, `department`, `tags`, `notes`, `customField.<key>`.
Custom-field values flow through `CustomFieldService.Validate`
on the write path — the importer cannot bypass schema validation.

### Frontend

The operator UI lives at
[`frontend-admin/src/pages/marketing/`](../../../../frontend-admin/src/pages/marketing/)
with the manifest at
[`frontend-admin/src/modules/marketing.tsx`](../../../../frontend-admin/src/modules/marketing.tsx).
**Eight** routes: contacts list (Persons + Organizations tabs),
contact detail (overview / memberships / timeline / scores / sources
tabs), tags admin, custom-field schema editor, imports audit list,
import wizard, `/marketing/scoring` (Phase 2 — profile CRUD with
JSON rule editor + top-20 leaderboard preview), and
**`/marketing/reviews`** (Phase 3 — conflict-review queue + side-by-
side resolver modal). URL-synced tabs + filter state across the
board per the
[`url-tabs`](../../../../.claude/skills/url-tabs/SKILL.md) skill.

The contact-detail Timeline tab embeds a "Log activity" modal that
POSTs to `/v1/marketing/activities` with the kind restricted to
`{call_made, meeting_held, note_added}` at the UI surface (the
backend enforces the same restriction). The Scores tab lists every
snapshot for the person across active profiles; each row opens a
Breakdown drawer (Modal) that explains how the engine arrived at
the value — one row per activity that contributed plus a synthetic
"aggregate" row when the breakdown cap (default 100) was hit.

The /marketing/scoring page is a two-pane layout: profiles list on
the left, edit form on the right with three JSON textareas for
`rules`, `filters`, `defaultDecay`. A structured rule-builder UI
is a Phase 2 follow-up — operators editing rules today are internal
Tier-1 staff comfortable reading the example JSON in
`docs/plans/marketing-addon/schemas/marketing_score_profiles.md`.

The /marketing/reviews page lists pending / resolved / dismissed
conflict reviews with URL-synced filters
(`?status=...&targetKind=...&importJobUuid=...&reviewUuid=...`).
Clicking a row opens `ReviewResolverModal` — a side-by-side
diff with per-field radio buttons (keep existing / take incoming)
and four action buttons: Keep all existing, Take all incoming,
Apply manual merge (uses the radio state to build a `fieldOverrides`
payload), Dismiss (discards the incoming row + its parked
activities). Soft-match conflicts are tagged with an inline "soft
match" badge so operators can spot them without re-reading the
schema. Resolved/dismissed reviews open in read-only mode.

The import wizard (`/marketing/imports/new`) carries an adapter
picker (CSV / Excel / Odoo radio) on Step 1. CSV + Excel render
the file upload + mapping JSON. Odoo swaps the file upload for a
connection-config form (Base URL, Database, API key, page size,
optional engagement opt-in); the JSON config blob is submitted as
the `file` multipart field — the backend's odoo adapter `Parse`
reads it as the connection config. The imports list adds a
"Reviews" badge column that deep-links to
`/marketing/reviews?importJobUuid=<uuid>`.

## What it does (eventual)

The full design ships in 4 functional phases plus a future phase 5:

- **Phase 1 — Anagrafica.** Shipped. See "What it does today".
- **Phase 2 — Activity log + scoring.** Shipped. See "What it does
  today".
- **Phase 3 — Advanced import.** Shipped. See "What it does today".
- **Phase 4 — Card lifecycle.** `marketing_card_types` templates +
  `marketing_cards` instances, staff-only issue/suspend/revoke flow,
  per-type multi-card-per-person policy. The
  `marketing_persons.activeCardUuids` field + its index are already
  declared so the Phase 4 rollout doesn't need a write-blocking
  index build.
- **Phase 5 — (future) marketing operativo.** Segments, lead-capture
  forms, campaign sends, ESP webhooks, AI-assisted scoring.

## Conventions

- **Tenant scoping.** Every Mongo query goes through
  `github.com/orkestra-cc/orkestra-sdk/tenantrepo` (`Scope`,
  `MustScope`, `StampInsert`, `StampInsertM`). The CI `tenantscope`
  analyzer fails the build on direct `collection.Find(...)` without a
  scope helper — new marketing code must be clean (no baseline
  entries).
- **Collection naming.** All Mongo collections owned by this module
  are prefixed `marketing_` (consistent with the
  [`mongo-collection-naming`](../../../../.claude/skills/mongo-collection-naming/SKILL.md)
  skill enforced repo-wide).
- **Activity append-only.** `marketing_activities` has no UPDATE /
  DELETE on the write path — corrections happen via a new activity
  of kind `corrected_by` whose `refs.correctsActivityUuid` points at
  the row to supersede. The score engine reads that reference and
  excludes the original from contribution. GDPR right-to-be-forgotten
  is the documented exception (`HardDeleteByPersonUUID` on the repo,
  not yet exposed via HTTP).
- **Permissions.** Cedar permissions are namespaced `marketing.*`
  (see [Orkestra_marketing_addon.md §3.6](../../../../docs/plans/marketing-addon/Orkestra_marketing_addon.md#36-permessi-cedar)
  for the full catalog). Phase 1 + 2 + 3 declare 7 keys —
  `marketing.contact.{read,write,delete}`, `marketing.import.run`,
  `marketing.activity.write`, `marketing.score_profile.write`, and
  `marketing.conflict.resolve` (activity / snapshot / leaderboard /
  conflict-review reads deliberately fold into `marketing.contact.read`).
  Phase 4 adds `marketing.card_type.write` +
  `marketing.card.{issue,suspend,revoke}` when card lifecycle lands.
- **Membership invariants enforced at the service layer.** The SDK's
  `IndexSpec` does not yet expose `PartialFilterExpression`, so the
  schema's "one Active=true per (person,org) pair" and "one
  Active+Primary=true per person" constraints are kept by the
  `MembershipService` (the importer + handler write paths route
  through `Create` and `Update`, both of which call
  `FindActivePair` / `UnsetPrimaryForPerson` / `Close` to preserve
  the invariant). Widening the SDK is tracked as a Phase-2+
  follow-up.

## Dependencies

Phase 1: none. Phase 2+ may consume `aimodels` (AI-assisted scoring)
and `notification` (campaign delivery) via the `ServiceRegistry`
lazy-lookup pattern rather than hard `Dependencies()` entries —
marketing should degrade gracefully when those addons are disabled,
not refuse to boot.

## SKU enablement

Auto-enabled on the **enterprise** SKU only (which uses the `"*"`
sentinel in `pkg/sdk/module/config_service.go::profileAddons` to
pre-enable every optional addon on first boot). All other profiles
leave marketing off; operators flip it on at `/admin/modules`.

## CI analyzer blind spots inherited from the extracted-addon shape

Two static analyzers in `backend/tools/` walk `./internal/...` from
the backend module's perspective. Because the marketing addon is a
separate Go module (`github.com/orkestra-cc/orkestra-addon-marketing`)
they do **not** traverse into this tree:

- **`tenantscope`** — would normally fail the build on any
  `coll.Find(filter)` that does not flow through `pkg/sdk/tenantrepo`.
  Our code is clean by construction (every repository call uses
  `tenantrepo.Scope` / `StampInsert`), but the gate cannot enforce
  it from the outside. Manual review during code changes here.
- **`policycoverage`** — would normally fail when a declared
  permission has no Cedar coverage. The 7 Phase-1 + Phase-2 + Phase-3
  permissions (`marketing.contact.{read,write,delete}`,
  `marketing.import.run`, `marketing.activity.write`,
  `marketing.score_profile.write`, `marketing.conflict.resolve`) are
  not visible to the analyzer, so the gate passes regardless. At the
  Cedar engine level the platform `super_admin` / `administrator`
  wildcard rules in `internal/core/authz/cedar/policies/platform.cedar`
  already cover every marketing action; finer-grained role-based
  coverage will arrive when the design's role catalog firms up.

Widening these analyzers to traverse `go.work` is tracked as
deferred infra work — see the
`project_policycoverage_addon_scan` memory entry.
