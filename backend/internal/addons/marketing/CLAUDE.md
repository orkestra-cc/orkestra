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

**Phase 1 (Fondazione anagrafica MVP) — in progress.** Currently only
the module scaffold exists; collections, models, handlers, services,
and the CSV importer land in subsequent PRs against
`feature/marketing-addon`. Phase 1 deliverables, design rationale,
and per-PR breakdown live in the monorepo at:

- [`docs/plans/marketing-addon/Orkestra_marketing_addon.md`](../../../../docs/plans/marketing-addon/Orkestra_marketing_addon.md) — full design (716 lines)
- [`docs/plans/marketing-addon/schemas/`](../../../../docs/plans/marketing-addon/schemas/) — per-collection field-by-field schemas
- [`docs/plans/marketing-addon/IMPLEMENTATION_PLAN.md`](../../../../docs/plans/marketing-addon/IMPLEMENTATION_PLAN.md) — Phase 1 execution plan

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
  for the full catalog). Phase 1 declares 5 keys; later phases add
  activity/score-profile/card/conflict-resolve permissions as the
  features arrive.

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
