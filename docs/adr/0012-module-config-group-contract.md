---
title: ADR-0012 — Module configuration group contract (data-driven settings rail)
status: accepted
public: true
---

# ADR-0012 — Module configuration group contract (data-driven settings rail)

| Field | Value |
|---|---|
| **Status** | ✅ Accepted — adopted 2026-08-03 |
| **Date** | 2026-08-03 |
| **Authors** | @salvatore.balestrino |
| **Related** | [ADR-0006](0006-collapse-to-core-only-base.md) (core-only base — the contract is consumed by every fork's addons through the same `Module` seam the core uses); [ADR-0007](0007-per-addon-i18n-namespaces.md) (per-addon i18n namespaces — the label-resolution order below) |

## Context

`GET /v1/admin/modules/{name}` returned a **flat** `configSchema[]`. The only grouping primitive was `ConfigField.Group`, a bare display string with no hierarchy, no description, no icon, and no ordering beyond declaration order — rendered as horizontal tabs inside one card. That shape does not scale: `auth` alone carries 62 of the base's ~79 core fields, 19 of them OAuth provider credentials that stayed on screen even when the provider was switched off; the other three schema-bearing core modules (`notification`, `tenant`, `compliance`) rendered a single flat column.

The settings surface is **not** core-private. Per [ADR-0006](0006-collapse-to-core-only-base.md) every optional module a fork adds is built against the same in-tree SDK contract the eight core modules use, and its configuration renders through the same `/admin/modules/{name}` page. So any layout primitive added here is a **contract consumed by every fork** — it must be expressible as declarative data, degrade cleanly for a module that declares nothing, and never force existing addons to change.

Three constraints shaped the design:

- **The `Module` interface is frozen at v1.** Adding a mandatory `ConfigGroups()` method would break every addon in every fork on the next sync.
- **`ConfigField` is persisted** (`bson:"configSchema"` on `ModuleConfig`) and rewritten from the running binary by `RefreshMetadata` on every boot — so any new field member is persisted and must carry both `json` and `bson` tags.
- **The client must not validate more strictly than the server** — a rule the UI enforces that the backend accepts is a divergence an operator hits as a phantom error.

## Decision

Make grouped, conditional module configuration a **first-class, data-driven, forward-compatible** part of the SDK contract.

1. **Groups are declared through an optional interface, not the `Module` interface.** `ConfigGroup{Key, Label, Description, Icon, Parent, Order}` is surfaced by `HasConfigGroups` (`ConfigGroups() []ConfigGroup`), resolved via the house `ConfigGroupsOf(m)` accessor (the same idiom as `RequiredServicesOf`). A module that does not implement it returns `nil`. `ConfigField.Group` now carries a `ConfigGroup.Key`, not a display label; `Parent` nests groups to any depth.

2. **Groups are never persisted.** `ConfigGroup` is presentational and fully code-derived, so — unlike `ConfigField` — it carries `json` tags only and is resolved **live** from the registry by the admin handler on each request (exactly as `RequiredServicesOf` already is). `RefreshMetadata` is untouched, so a schema change propagates on the next boot with no migration.

3. **Per-field metadata rides on the persisted `ConfigField`.** New members — `Advanced`, `DependsOn []FieldCondition` (+ `DependsOnMatch` `all`/`any`), `Min`, `Max`, `Pattern`, `Placeholder`, `HelpURL` — each carry `json` **and** `bson` tags, `omitempty`. `DependsOn` is a struct list (`{Key, In}`), not an expression string — no parser to keep consistent between Go and TypeScript. Visibility combines AND across entries, OR within one entry's `In`; `DependsOnMatch: "any"` ORs across entries (needed where a capability is reachable from more than one switch, e.g. an OAuth provider enabled per audience surface). A hidden field is never required, never enters the save diff, and **keeps its stored value** (switching a provider off must not discard its secret).

4. **i18n keys are derived, not declared.** The translation key comes from the stable `Key`: `config.fields.<fieldKey>.{label,desc}` and `config.groups.<groupKey>.{label,desc}`. Resolution order (twin of `helpers/navLabel.ts`): the addon's own namespace ([ADR-0007](0007-per-addon-i18n-namespaces.md)) → the core bundle (`moduleConfig.<module>.…`) → the literal `Label`/`Description` the backend sent. A present-but-empty key counts as absent, so an un-migrated addon keeps showing English rather than a raw key path — and the schema carries no redundant i18n field.

5. **Graceful degradation has two thresholds, both defaulting to zero work for a fork.** The full master-detail page (Overview / configuration tree / Dependencies / Environments) is promoted only when a module **declares `ConfigGroups()` *and*** its resolved tree has **≥2 top-level nodes** (`hasPageRail`). A looser predicate (`hasCardRail`) gives the configuration *card* its own internal tab rail whenever there are ≥2 top-level nodes **or** any groups are declared at all — so a single declared group, or the legacy heuristic of ≥2 distinct `field.group` labels, still opts into the card rail without being promoted to the whole-page framing. A module that declares no groups and whose fields fall into fewer than two legacy buckets renders the **flat form**, exactly as before. Every threshold reads off the declared data, so an un-migrated fork addon needs no changes and **declaring none is a supported end state, not a gap**.

6. **A declaration-integrity gate runs over the real catalog.** `ValidateConfigDeclarations` (invoked by `cmd/server`'s catalog test) checks, for every registered module, that each `Field.Group` resolves to a declared `ConfigGroup.Key`, each `DependsOn.Key` resolves to a field of the same module, `DependsOnMatch` is valid, and `Parent` has no cycles — among other structural checks (duplicate keys, an uncompilable `Pattern`, inverted `Min`/`Max`, a `DependsOn` value outside its target field's domain). Living in the SDK package, it runs for fork addons too — a typo fails a test instead of producing a phantom rail entry.

7. **The declarative client validator replaces the stricter regex.** Validation is generated from the same metadata (`Required`/`Min`/`Max`/`Pattern`/type), so the UI never rejects a value the server accepts. `Required` remains a UI hint — nothing in `pkg/sdk/module` enforces it — so conditional visibility introduces no server/client divergence.

## Consequences

- **A fork gets the sectioned settings rail for free** by declaring `ConfigGroups()` and moving its `Field.Group` values to keys — no page code to write. The four core modules (`auth` in phase 4; `notification`/`tenant`/`compliance` in phase 5) are the reference migrations.
- **Nothing a fork already shipped breaks.** An addon that declares no `ConfigGroups()` renders exactly as before — the flat form, or the legacy card-internal rail if its fields already carry ≥2 distinct group labels — and keeps its English labels; the response merely gains `configGroups` and the seven `ConfigField` members, all `omitempty`.
- **The `ConfigField`/`ConfigGroup` shapes are now part of the frozen v1 surface.** They serialise into `openapi/enterprise.json`; a change to either is an OpenAPI diff gated by `openapi-check`. Declaring *data* against them (a module's own groups/conditions) is **not** a shape change and produces no spec diff.
- **The forbidden shapes** ([ADR-0006](0006-collapse-to-core-only-base.md)) still hold: this contract lives in the single in-tree `pkg/sdk` — no satellite `go.mod`, no published module.

## Alternatives considered

- **Extend the existing `Group` display string** (encode hierarchy/description into it) instead of a first-class type — rejected: no typed home for description, icon, order, or parent, and string-encoded hierarchy is fragile to author and parse. `ConfigGroup` is self-describing and validated.
- **Add `ConfigGroups()` to the `Module` interface** — rejected: the interface is frozen at v1, so a new mandatory method breaks every addon in every fork on the next sync. The optional `HasConfigGroups` sub-interface (the house `…Of` accessor idiom) costs an un-migrated module nothing.
- **A DSL/expression string for `DependsOn`** (e.g. `provider == "smtp"`) — rejected: a parser to write, ship, and keep byte-identical between Go and TypeScript. The `{Key, In}` struct list needs no parser and is checked structurally by the integrity gate.
- **Persist `ConfigGroup`** alongside `ConfigField` — rejected: it is presentational and fully code-derived, so persisting it would demand a data migration on every layout tweak. It is resolved live from the registry each request, and a test forbids ever adding a `bson` tag.
- **A declared i18n-key field** on each field/group — rejected: redundant with the stable `Key`. Deriving `config.fields.<key>` / `config.groups.<key>` keeps the schema free of translation concerns and lets an un-migrated addon fall back to the backend's English literal.
- **An explicit opt-in flag** to turn the rail on — rejected in favour of the automatic `hasPageRail` / `hasCardRail` predicates, so a fork gets the right layout from its declared data with no extra switch to remember.
