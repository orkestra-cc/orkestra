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

**Phase 1 (Fondazione anagrafica MVP) — shipped on
`feature/marketing-addon`.** Six MongoDB collections, ~16 HTTP
routes under `/v1/marketing/*`, a synchronous CSV importer with
email/VAT dedup, and the operator-console UI all land before the
branch promotes to `dev`.

Design rationale, per-collection schemas, and the 6-PR execution
plan live in the monorepo:

- [`docs/plans/marketing-addon/Orkestra_marketing_addon.md`](../../../../docs/plans/marketing-addon/Orkestra_marketing_addon.md) — full design (716 lines)
- [`docs/plans/marketing-addon/schemas/`](../../../../docs/plans/marketing-addon/schemas/) — per-collection field-by-field schemas
- [`docs/plans/marketing-addon/IMPLEMENTATION_PLAN.md`](../../../../docs/plans/marketing-addon/IMPLEMENTATION_PLAN.md) — Phase 1 execution plan

## What it does today

Phase 1 owns six MongoDB collections (all `marketing_*` prefixed):

| Collection | Purpose | Key indexes |
|---|---|---|
| `marketing_organizations` | Anagrafica enti (B2B + PA + foundations + associations) | unique sparse on `(tenantId, vat)` / `(tenantId, taxCode)`; multikey on tags; sort on updatedAt |
| `marketing_persons` | Persone fisiche, importer dedup primary key on `emails.address` | unique sparse multikey on `(tenantId, emails.address)`; tags + activeCardUuids (Phase 4 reserved) multikey |
| `marketing_memberships` | Person↔Organization relations with role + period | compound on `(tenantId, personUuid, orgUuid)` and `(tenantId, personUuid, primary)` |
| `marketing_tags` | Hierarchical labels with materialized path | unique `(tenantId, slug)`; prefix-scan on `path` |
| `marketing_custom_field_schemas` | Per-tenant typed bag declaration (one row per `(tenant, persons|organizations)`) | unique `(tenantId, targetCollection)` |
| `marketing_import_jobs` | Audit log of every CSV import run | sort on `(tenantId, createdAt)` |

The HTTP surface (registered via three permission-bucketed chi
subgroups on the operator router) is:

- `marketing.contact.read`   → GET on all 5 contact collections + GET on imports
- `marketing.contact.write`  → POST + PATCH on the 5 contact collections (custom-field-schemas via PUT)
- `marketing.contact.delete` → DELETE on the 5 contact collections
- `marketing.import.run`     → POST `/v1/marketing/imports` (multipart CSV upload)

### Importer pipeline

`POST /v1/marketing/imports` accepts a CSV file + a JSON
`ColumnMapping` and runs synchronously inside the handler. The
pipeline:

1. The `csv` adapter (`importers/csv/`) yields `CanonicalRecord`
   values via a buffered channel, honouring header-based or
   numeric-index column mappings.
2. The generic pipeline (`importers/pipeline.go`) dedups against
   the tenant's existing data:
   - Person primary key: `primary_email` (normalised lowercase).
   - Organization primary key: `vat` → `taxCode` fallback (both
     normalised uppercase).
3. Matched rows go through the auto-merge policy — fill empty
   scalars, set-union additive arrays (tags / emails / phones /
   sources). Conflicts on dedup-key fields (primary email, vat,
   taxCode) increment `Stats.ConflictsSkipped` and leave the
   existing value alone; Phase 3's review queue replaces this
   with a routed flow.
4. New rows are inserted via the matching repository (which
   stamps `tenantId` + timestamps + normalises the dedup keys).
5. When a row carries both org and person identity, an active
   `marketing_membership` is created (or reused) linking them.
6. Every touched row gets a `ProvenanceSource` appended to its
   `sources[]` array referencing the import job's UUID + the
   CSV row index.

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
Six routes: contacts list (Persons + Organizations tabs), contact
detail (overview / memberships / sources tabs), tags admin, custom-
field schema editor, imports audit list, import wizard. URL-synced
tabs across the board per the
[`url-tabs`](../../../../.claude/skills/url-tabs/SKILL.md) skill.

## What it does (eventual)

The full design ships in 4 functional phases plus a future phase 5:

- **Phase 1 — Anagrafica.** Shipped. See "What it does today".
- **Phase 2 — Activity log + scoring.** Append-only
  `marketing_activities` (event sourcing, `occurred_at` +
  `recorded_at` doubled timestamps), `marketing_score_profiles`
  (multiple parallel profiles per tenant), `marketing_score_snapshots`
  (rebuildable cache). Score = pure function of activities + profile
  rules with decay; eager-on-insert + nightly recompute.
- **Phase 3 — Advanced import.** Excel + Odoo adapters,
  `marketing_conflict_reviews` queue, full UI for resolving conflicts.
  Replaces Phase-1's conflict-skip behaviour.
- **Phase 4 — Card lifecycle.** `marketing_card_types` templates +
  `marketing_cards` instances, staff-only issue/suspend/revoke flow,
  per-type multi-card-per-person policy. The
  `marketing_persons.activeCardUuids` field + its index are already
  declared so the Phase 4 rollout doesn't need a write-blocking
  index build.
- **Phase 5 — (future) marketing operativo.** Segments, lead-capture
  forms, campaign sends, ESP webhooks, AI-assisted scoring.

## What it does (eventual)

The full design ships in 4 functional phases plus a future phase 5:

- **Phase 1 — Anagrafica.** `marketing_organizations` +
  `marketing_persons` + `marketing_memberships` + `marketing_tags` +
  `marketing_custom_field_schemas`. CSV importer with email/VAT/tax
  code dedup and auto-merge of non-conflicting fields. Provenance via
  `sources[]`.
- **Phase 2 — Activity log + scoring.** Append-only
  `marketing_activities` (event sourcing, `occurred_at` +
  `recorded_at` doubled timestamps), `marketing_score_profiles`
  (multiple parallel profiles per tenant), `marketing_score_snapshots`
  (rebuildable cache). Score = pure function of activities + profile
  rules with decay; eager-on-insert + nightly recompute.
- **Phase 3 — Advanced import.** Excel + Odoo adapters,
  `marketing_conflict_reviews` queue, full UI for resolving conflicts.
- **Phase 4 — Card lifecycle.** `marketing_card_types` templates +
  `marketing_cards` instances, staff-only issue/suspend/revoke flow,
  per-type multi-card-per-person policy.
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
- **Activity append-only.** When `marketing_activities` lands in
  Phase 2, no UPDATE / DELETE — corrections happen via a new activity
  of kind `corrected_by` pointing at the row to supersede. GDPR
  right-to-be-forgotten is the documented exception and logs to a
  separate audit collection.
- **Permissions.** Cedar permissions are namespaced `marketing.*`
  (see [Orkestra_marketing_addon.md §3.6](../../../../docs/plans/marketing-addon/Orkestra_marketing_addon.md#36-permessi-cedar)
  for the full catalog). Phase 1 declares 4 keys —
  `marketing.contact.{read,write,delete}` and `marketing.import.run`;
  later phases add `marketing.activity.*`, `marketing.score_profile.*`,
  `marketing.card_type.write`, `marketing.card.{issue,suspend,revoke}`,
  and `marketing.conflict.resolve` as those features arrive.
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
  permission has no Cedar coverage. The 3 Phase-1 permissions
  (`marketing.contact.{read,write,delete}`) are not visible to the
  analyzer, so the gate passes regardless. At the Cedar engine level
  the platform `super_admin` / `administrator` wildcard rules in
  `internal/core/authz/cedar/policies/platform.cedar` already cover
  every marketing action; finer-grained role-based coverage will
  arrive when the design's role catalog firms up (Phase 2+).

Widening these analyzers to traverse `go.work` is tracked as
deferred infra work — see the
`project_policycoverage_addon_scan` memory entry.
