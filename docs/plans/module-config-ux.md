# Module configuration UX — grouped schema + master-detail settings page

**Status:** 🟡 In progress

Replaces the single-page module configuration form at `/admin/modules/:name` with a
master-detail settings surface, and extends the SDK `ConfigField` contract so the
layout is driven by data instead of guessed from a flat list.

Related: [ADR-0006](../adr/0006-collapse-to-core-only-base.md) (core-only base — the
contract is consumed by every fork's addons), [ADR-0007](../adr/0007-per-addon-i18n-namespaces.md)
(per-addon i18n namespaces). A companion ADR-0012 records the contract decision itself.

---

## 1. Problem

`GET /v1/admin/modules/{name}` returns a **flat** `configSchema[]`. The only grouping
primitive is `ConfigField.Group`, a bare string with no hierarchy, no description, no
icon, and no ordering beyond declaration order. `ModuleConfigSection` renders those
groups as horizontal `Nav variant="tabs"` inside one Card.

Real shape of the four core modules that declare a schema (counted inside
`ConfigSchema()` only — `Permissions()` and `IndexSpec` also use a `Key:` field and
inflate a naive grep):

| Module         | Fields | Groups | Symptom |
| -------------- | -----: | -----: | ------- |
| `auth`         | **62** | 11 | 11 horizontal tabs that wrap; `Google`/`Apple`/`GitHub`/`Discord` are *siblings* of `OAuth Providers`, not children |
| `notification` | **11** |  0 | one flat column, no hierarchy |
| `tenant`       |  **2** |  1 | half in `Provisioning`, half in the synthetic `General` bucket |
| `compliance`   |  **4** |  0 | flat list |

`auth` alone is 78% of all core configuration. 19 of its 62 fields are OAuth provider
credentials (Google 5, Apple 8, GitHub 3, Discord 3) that stay on screen even when that
provider is switched off — dead weight on nearly every install.

Any new UI built on the current schema stays crippled: three of the four core modules
would render a rail with a single "General" entry, i.e. today's flat list with more code
underneath.

## 2. Decisions

### 2.1 Groups are first-class, declared through an optional interface

```go
// pkg/sdk/module/types.go

// ConfigGroup describes one section of the settings rail. Hierarchy via Parent.
type ConfigGroup struct {
    Key         string `json:"key"`                   // "oauth.google" — stable, never translated
    Label       string `json:"label"`                 // literal EN fallback
    Description string `json:"description,omitempty"` // panel subtitle
    Icon        string `json:"icon,omitempty"`        // FontAwesome name
    Parent      string `json:"parent,omitempty"`      // Key of the parent group
    Order       int    `json:"order,omitempty"`
}
```

`ConfigGroups()` is **not** added to the `Module` interface — that would break every
addon in every fork. It follows the house idiom already used by `RequiredServicesOf`:

```go
func ConfigGroupsOf(m Module) []ConfigGroup {
    if g, ok := m.(interface{ ConfigGroups() []ConfigGroup }); ok {
        return g.ConfigGroups()
    }
    return nil
}
```

`ConfigField.Group` now carries a `ConfigGroup.Key` instead of a display label.

### 2.2 Groups are never persisted

`ModuleConfig.ConfigSchema` **is** persisted (`bson:"configSchema"`) and `GetConfig`
returns the stored document. That is safe only because `SeedFromModules` calls
`RefreshMetadata()` on every boot, which `$set`s `configSchema` from the running binary
while leaving admin-editable values alone — so schema changes propagate on restart with
no migration.

`configGroups` is presentational and fully code-derived, so it is **not** stored at all.
The handler resolves it from the live registry via `ConfigGroupsOf(m)`, exactly as it
already does for `RequiredServicesOf(m)`. `RefreshMetadata` is untouched.

### 2.3 Per-field metadata

```go
type FieldCondition struct {
    Key string   `json:"key" bson:"key"`  // another field key of the SAME module
    In  []string `json:"in" bson:"in"`    // satisfying values
}

type ConfigField struct {
    // …existing fields unchanged…
    Group       string           `json:"group,omitempty" bson:"group,omitempty"`         // now a ConfigGroup.Key
    Advanced    bool             `json:"advanced,omitempty" bson:"advanced,omitempty"`   // collapsed under "▸ Advanced"
    DependsOn   []FieldCondition `json:"dependsOn,omitempty" bson:"dependsOn,omitempty"` // AND across entries, OR within In
    Min         *int             `json:"min,omitempty" bson:"min,omitempty"`
    Max         *int             `json:"max,omitempty" bson:"max,omitempty"`
    Pattern     string           `json:"pattern,omitempty" bson:"pattern,omitempty"`
    Placeholder string           `json:"placeholder,omitempty" bson:"placeholder,omitempty"`
    HelpURL     string           `json:"helpUrl,omitempty" bson:"helpUrl,omitempty"`
}
```

`ConfigField` is persisted (`bson:"configSchema"` on `ModuleConfig`), so every field —
including `FieldCondition` nested inside `DependsOn` — carries both `json` and `bson`
tags, `omitempty`. `ConfigGroup` (§2.1) is the exception: it is never persisted, so it
carries `json` tags only.

`DependsOn` is a struct list, not an expression string — no parser to write, ship, or
keep consistent between Go and TypeScript.

**`Required` is never enforced backend-side** (verified: nothing in `pkg/sdk/module`
reads it). It is a UI hint, so conditional visibility introduces no server/client
divergence to keep in sync.

Hidden-field semantics, to be held in both the model helper and its tests:

- a hidden field is never validated as required;
- a hidden field never enters the save diff;
- **a hidden field's stored value is preserved** — switching Google off must not discard
  its client secret;
- completeness counts **visible** fields only, otherwise `auth` reports a fraction that
  counts credentials for disabled providers.

### 2.4 i18n keys are derived, not declared

`Label` stays the literal EN fallback. The translation key is derived from the already
stable `Key`:

```
config.fields.<fieldKey>.label     config.fields.<fieldKey>.desc
config.groups.<groupKey>.label     config.groups.<groupKey>.desc
```

Resolution order, in a helper twinned with `helpers/navLabel.ts`:

1. `<moduleName>:config.fields.<key>.label` — a fork addon's own namespace (ADR-0007)
2. `moduleConfig.<moduleName>.fields.<key>.label` — the core bundle
3. `field.label` — the literal from the backend

No redundant schema field, and an addon that translates nothing keeps showing English
instead of a raw key.

### 2.5 Client validation must not be stricter than the server

`ModuleConfigFields` validates durations with `/^\d+[smh]$/`, while the backend parses
them with `time.ParseDuration`. `1h30m`, `500ms` and `1.5h` are accepted by the server
and rejected by the UI. The declarative validator replaces that regex.

(Unrelated to `internal/shared/config`, which additionally accepts a `d` suffix for
env-var durations — module config durations do not go through that path.)

## 3. Group trees

### `auth` — 62 fields, 11 flat groups → 7 top-level + 4 nested

| Key | Label | Fields | Notes |
| --- | --- | ---: | --- |
| `registration` | Registration | 5 | |
| `login` | Login & Sessions | 6 | |
| `password` | Password Policy | 7 | |
| `mfa` | MFA | 5 | |
| `oauth` | OAuth Providers | 11 | on/off toggles, signup, auto-link |
| `oauth.google` | Google | 5 | `dependsOn` `googleEnabled{Admin,Client}` |
| `oauth.apple` | Apple | 8 | `dependsOn` `appleEnabled{Admin,Client}` |
| `oauth.github` | GitHub | 3 | `dependsOn` `githubEnabled{Admin,Client}` |
| `oauth.discord` | Discord | 3 | `dependsOn` `discordEnabled{Admin,Client}` |
| `antiabuse` | Anti-abuse & Notifications | 7 | |
| `sessions` | Sessions & Account | 2 | |

On a fresh install every provider is off, so 19 fields are hidden: **62 → 43 visible**,
across 7 rail entries instead of 11 wrapping tabs.

### `notification` — 11 fields, 3 groups

| Key | Label | Fields |
| --- | --- | ---: |
| `delivery` | Delivery | 6 — `email.provider` + `email.smtp.*` |
| `sender` | Sender | 3 — from address / from name / reply-to |
| `branding` | Branding & templates | 2 — app name, support email |

The five `email.smtp.*` fields get `dependsOn: email.provider in [smtp]`. The default
provider is `noop`, so a fresh install shows **1 field instead of 6** under Delivery.

`email.provider` also converts from `FieldString` to `FieldEnum` with its real options —
today it is a free-text box where you type `noop` by hand. Stored values are strings
either way, so no data migration; an unrecognised stored value must still render as a
selected option rather than silently blanking the select.

### `tenant` — 2 fields, 2 groups · `compliance` — 4 fields, 2 groups

| Module | Key | Label | Fields |
| --- | --- | --- | ---: |
| `tenant` | `provisioning.internal` | Internal provisioning (Tier-1) | 1 |
| `tenant` | `provisioning.external` | External provisioning (Tier-2) | 1 |
| `compliance` | `soc2` | SOC2 evidence | 1 |
| `compliance` | `retention` | Retention & DSR | 3 |

`compliance` gains `dependsOn: auto_cleanup_enabled in [true]` on `retention_years` and
`export_retention_days` — 4 → 2 visible fields at the default.

> **Recorded trade-off.** A rail over 2 fields is more frame than substance, and the
> alternative (leave `tenant`/`compliance` on the plain-form degradation path, keeping
> only `dependsOn` + validation + i18n) was put on the table and declined in favour of
> every core module having the same layout. If the two small pages read as overhead once
> built, dropping their `ConfigGroups()` reverts them to the flat form with no other
> change.

## 4. Frontend

```
Pre-flight:
- Production precedent: src/pages/admin/navigation/index.tsx (master-detail Row/Col lg=8|4,
                        PageHeader, Card shadow-none border), src/pages/admin/modules/detail/*
- Reference read:       src/reference/components/navigation/Navs.tsx (Nav flex-column + pills)
- Primitives:           StatCard, SectionCard, OrkestraCardHeader, SubtleBadge, PageHeader,
                        react-bootstrap Nav/Form/Alert
```

### 4.1 Layout

The rail is the navigation for the **whole module page**, not just the configuration
card: `Overview` (the current `StatCard` row) → the configuration groups → `Dependencies`
/ `Health` / `Environments`. The rail is sticky full-height; a sticky save bar sits at
the bottom.

Rejected alternatives, for the record: a rail scoped to the configuration Card only (the
rail scrolls out of view on long groups), a single scrolling page with a scrollspy index
(62 fields is a very long scroll), and a card grid that drills down into a dedicated page
per group (two clicks per edit).

### 4.2 Files

```
src/pages/admin/modules/
  configModel.ts            buildGroupTree(schema, groups) · isFieldVisible(field, values)
                            visibleFields(group, values) · groupStatus(group, values, secrets)
  configI18n.ts             translateConfigLabel/Desc/Group — twin of helpers/navLabel.ts
  useModuleConfigForm.ts    react-hook-form + yup built from metadata; cross-group dirty state
  detail/
    index.tsx               two-column layout, active section from ?section=
    ModuleConfigRail.tsx    Nav flex-column, hierarchy, per-entry status badge
    ModuleConfigPanel.tsx   group title + description, fields, "▸ Advanced"
    ModuleSaveBar.tsx       sticky: change count, errors linking to their section, Discard/Save
    ModuleOverviewPanel.tsx was ModuleDashboardCards — becomes the "Overview" section
    ModuleDependencyCard.tsx / ModuleEnvironmentSwitcher.tsx / ModuleDetailHeader.tsx (unchanged)
  ✗ ModuleConfigModal.tsx   deleted — dead code, nothing imports it
  ✗ utils.ts::bucketByGroup replaced by buildGroupTree
```

Two pieces of stale state found while scoping, both cleaned up here: `ModuleConfigModal.tsx`
(247 lines) has no importer, and the `ModuleConfigFields` docstring claims it is shared
with the first-install wizard — the wizard only reads a `smtpConfigured` boolean. The
only live consumer is `ModuleConfigSection`.

### 4.3 Forms

`ModuleConfigFields` moves from raw `useState` to **react-hook-form + yup**, per the
stack mandate. This is not ceremony: the yup schema is generated from the new metadata
(`Required`/`Min`/`Max`/`Pattern`/type), `watch()` feeds `dependsOn`, and
`formState.dirtyFields` feeds both the save-bar counter and the outgoing diff.

**That migration lands in phase 3, not phase 2.** `react-hook-form`, `yup`, and
`@hookform/resolvers` are all dependencies, but `useForm` appears only in
`src/reference/` demos and `components/wizard/WizardLayout.tsx` — **no production page
uses it**; every form under `pages/` is `useState`. So this would be the app's first
production use of the library, on the hardest possible case: a schema generated at
runtime, with secrets, conditional visibility, and dirty state spanning groups.

The requirement that actually justifies a form library is aggregating `dirtyFields`
across groups for the sticky save bar — and that does not exist until phase 3. Doing it
in phase 2 means designing the integration blind of its only consumer. Phase 2 therefore
delivers the pure model layer against the existing `useState` form; phase 3 migrates the
form and the save bar together.

### 4.4 Save model

One form for the whole module, mounted in `detail/index.tsx`; the rail only selects which
slice is visible. Consequences that must hold:

- switching section must **not** trip `useBlocker` — same route, shared state. This is
  precisely what today's per-card form cannot do;
- the save bar shows aggregated changes (`3 changes · Password Policy (2), MFA (1)`);
- **validation errors in non-visible sections** surface in the save bar with a link that
  navigates there. Without it, save fails with no indication of where;
- the payload stays today's diff (changed non-secret keys + non-empty secrets), so the
  already-fixed "UpdateConfig wipes secrets" behaviour is not disturbed.

### 4.5 URL sync and degradation

Active section lives in `?section=oauth.google` (per the `url-tabs` mandate), sanitised
against the tree — an unknown key falls back to the first group.

`configGroups` absent or with fewer than 2 groups → **no rail, flat form as today**. This
is the automatic path for un-migrated fork addons: no regressions, and no mandatory work
for anyone who has forked.

## 5. Testing

**Backend**

- `ConfigGroupsOf` against a module that implements the optional interface and one that
  does not (must return `nil`, not panic).
- **Schema integrity test**, table-driven over every registered module: each
  `Field.Group` resolves to a declared `ConfigGroup.Key`; each `DependsOn.Key` resolves to
  a field key of the same module; no cycles in `Parent`. Living in the SDK package means
  it runs for fork addons too, so a typo in `Group` fails a test instead of producing a
  phantom rail entry.
- `configGroups` present in `GET /v1/admin/modules` and `/{name}`.

**Frontend** (vitest + RTL + MSW)

- `configModel.test.ts` — pure, no DOM: tree ordering and nesting, AND-across /
  OR-within `In` semantics, hidden field never required, completeness ignores hidden.
- `detail/index.test.tsx` — MSW serves a module with groups: the rail renders the tree,
  clicking an entry updates `?section=`, edits in **two different groups** accumulate into
  one save bar, save sends only the diff, a hidden field never reaches the payload.
- ⚠️ Vitest exits non-zero on an unhandled MSW request even with every assertion passing.
  Handlers are needed for all four endpoints the page touches (`/admin/modules`,
  `/{name}`, `/{name}/environments/{env}`, `/modules/health`) or the suite flakes with an
  unrelated error.
- EN/IT parity: the existing test covers the ~100 new `moduleConfig.*` keys for free.

**CI**

`make openapi-dump` is mandatory — `ConfigField` gains seven fields and the response gains
`configGroups`, so `openapi-check` fails without it. Then `make ci-backend` and
`make ci-frontend-admin`.

## 6. Documentation

Same commit as the code, per the repo's commit-doc-hygiene rule:

- **ADR-0012** — the SDK contract is consumed by every fork: groups, the optional
  interface, `dependsOn` semantics, the degradation rule.
- `backend/pkg/sdk/CLAUDE.md` — `ConfigGroup`, the accessor, `dependsOn`.
- `docs/site/sdk/build-your-first-addon.mdx` — addon authors need to know groups exist
  and that declaring none is legitimate.
- `frontend-admin/CLAUDE.md` + `src/modules/_template/README.md` — the
  `config.fields.*` / `config.groups.*` i18n keys for addons.

## 7. Where the work happens

Phases 1–6 are **core** — `pkg/sdk/module`, the four core modules, and
`frontend-admin/src/pages/admin/modules/`. They land in this repo (upstream) and reach
the fork chain through the normal two-hop sync (upstream → commons → client forks, per
[ADR-0010](../adr/0010-commons-fork-chain.md)).

They must **not** be written in `orkestra-commons`. A fork-side change to `pkg/sdk`
fights every subsequent sync — the ADR-0009 sync recorded auto-merge silently dropping
fork-only `pkg/sdk` symbols, and the new `ConfigGroup` / `FieldCondition` types are exactly
that shape.

The payoff is larger downstream than here. `orkestra-commons` carries seven addons whose
combined configuration is **71 fields with zero groups declared** — more surface than the
79 core fields, and entirely flat:

| Addon | Fields | Groups |
| ----- | -----: | -----: |
| `crm` | 28 | 0 |
| `billing` | 16 | 0 |
| `forms` | 11 | 0 |
| `company` | 7 | 0 |
| `payments` | 4 | 0 |
| `documents` | 3 | 0 |
| `subscriptions` | 2 | 0 |

So phase 7 is commons-only, and runs after the sync brings the contract down. Until then
those addons keep rendering the flat form through the degradation path in §4.5 — which is
the whole reason that path exists.

## 8. Phases

| # | Phase | Repo | Status |
| - | ----- | ---- | ------ |
| 1 | SDK contract: `ConfigGroup`, `ConfigGroupsOf`, `ConfigField` metadata, handler serialisation, schema-integrity test, `openapi-dump` | upstream | ✅ |
| 2 | Frontend model layer: `configModel`, `configI18n`, declarative validation on the existing `useState` form — **no visual change** | upstream | ✅ |
| 3 | Frontend layout: rail, panel, save bar, URL sync — **plus** the `ModuleConfigFields` migration to RHF + yup, which the cross-group save bar is what justifies (§4.3) | upstream | 🔴 |
| 4 | Migrate `auth` (62 fields, OAuth tree, `dependsOn`) + EN/IT keys | upstream | 🔴 |
| 5 | Migrate `notification` (+ `email.provider` → enum), `tenant`, `compliance` | upstream | 🔴 |
| 6 | Delete `ModuleConfigModal`, ADR-0012, docs | upstream | 🔴 |
| 7 | Migrate the seven commons addons (71 fields → groups + `dependsOn` + addon-namespace i18n keys) | commons, post-sync | 🔴 |

Phases 1–2 are shippable on their own with no user-visible change, so a problem in phase
3 does not leave a half-finished redesign in production.

Each phase gets its own task-level plan as it starts. Phase 1:
[`module-config-ux-phase1-sdk.md`](module-config-ux-phase1-sdk.md).
