---
title: ADR-0007 — Per-addon i18n namespaces; addon translations never touch core locale files
status: proposed
public: true
---

# ADR-0007 — Per-addon i18n namespaces; addon translations never touch core locale files

| Field | Value |
|---|---|
| **Status** | 🟡 Proposed |
| **Date** | 2026-06-14 (proposed) |
| **Authors** | @salvatore.balestrino |
| **Related** | [ADR-0006](0006-collapse-to-core-only-base.md) (core-only base; addons are per-fork and must not modify core), [ADR-0003](0003-three-audience-host-split.md) (operator vs client SPAs) |

## Context

[ADR-0006](0006-collapse-to-core-only-base.md) settled that Orkestra is a **core-only base**: a fork adds verticals through the same `Module` + `catalog_<name>.go` + `iface` seam the core uses, and — the load-bearing invariant — **an addon never modifies core.** The backend honours this: core → addon import coupling is zero, addons reach core only through `iface` + `ServiceRegistry`.

The frontend i18n layer **breaks that invariant.** Both SPAs wire translations as a single monolithic bundle per language under one i18next namespace:

```ts
// frontend-admin/src/i18n.ts
import en from './locales/en.json';
import it from './locales/it.json';
// ...
resources: {
  en: { translation: en },   // one giant file, ~90 KB
  it: { translation: it }    // one giant file, ~96 KB
}
```

- A **single** i18next namespace (`translation`) holds every string from every module.
- Imports are **static** — no dynamic/lazy namespace loading.
- The `ModuleManifest` contract (`src/modules/types.ts`) exposes `routes` and `injectApi` (lazy API-slice injection) but **no i18n seam**.
- Type safety (`i18n-types.d.ts`) and EN/IT parity (`locales/parity.test.ts`) are both pinned to the core `en.json`/`it.json`.

The consequence: to add localized UI, an addon author **must edit `src/locales/{en,it}.json`, `i18n-types.d.ts`, and `parity.test.ts` of the core.** Addon strings physically live in core files. This is the exact coupling ADR-0006 forbids, just in the i18n layer instead of the import graph.

Direct evidence it already bit us: ADR-0006's frontend collapse had to **"prune addon i18n namespaces"** by hand from the core locale files when the 14 verticals were removed — surgery that would have been a directory delete if addon translations had lived with their addons.

This was tolerable while addons lived in-tree and shipped together. Under a core-only base where forks own their verticals, it is a structural defect: every addon grows the core locale files unboundedly, key names can collide across addons, and adding/removing an addon is no longer a self-contained operation.

## Decision

**Each addon ships its own translation bundles inside its own directory, registered at runtime as a dedicated i18next namespace named after the module. An addon never writes to the core `src/locales/*.json`, `i18n-types.d.ts`, or `parity.test.ts`.**

The core stays on the default `translation` namespace and its behaviour does not change — the seam is purely additive.

### D1 — The namespace is the addon's name

An addon `billing` registers its strings under the i18next namespace `billing` and consumes them via `useTranslation('billing')` → `t('invoice.header')`, or `t('billing:invoice.header')`. Because the namespace equals the module name (the same `name` used by the manifest and the backend module), two addons cannot collide, and an addon can never overwrite a core key.

### D2 — Registration rides the existing manifest seam

`ModuleManifest` gains an optional `injectI18n` mirroring `injectApi`:

```ts
injectI18n?: () => Promise<Record<string /*lng*/, Record<string, unknown>>>;
```

The addon returns all supported languages at once (`{ en: {...}, it: {...} }`), keeping each bundle a separate dynamically-imported chunk. A boot-time hook iterates `moduleCatalog` and calls `i18n.addResourceBundle(lng, name, bundle, /*deep*/ true, /*overwrite*/ false)` per language. `overwrite: false` guarantees an addon can never clobber the core or another addon.

### D3 — Registration is ungated by auth and enabled-state

Unlike `injectApi` — gated on the admin-only `GET /v1/admin/modules` query — i18n registration runs for **every** catalogued module at app boot, independent of role and enabled-state. Rationale: an addon page can render for a non-admin operator, so its namespace must exist regardless of who is signed in. Loading the locale chunk of a disabled module is harmless: its routes are already gated by `ModuleGate`, so its pages never render. Locale JSON is small (~10 KB), so eager registration of catalogued addons is cheap.

### D4 — Type safety and parity are the addon's responsibility, in the addon's files

- The addon ships its own `i18n.d.ts` that augments react-i18next's `CustomTypeOptions['resources']` with its namespace. Module augmentation merges, so the addon's keys become typed **without touching core `i18n-types.d.ts`**.
- The core's `parity.test.ts` flatten/compare logic is extracted into a reusable `src/locales/parityCheck.ts`. Each addon ships its own parity test that calls the shared helper over its own bundles. The core parity test stays scoped to core EN/IT.

### D5 — Backend error codes: an opt-in namespaced resolver

A backend error carries `{ code, detail }` and core call sites resolve it inline with `t('errors.<code>')`, falling back to the English `detail`. Rather than sweep those decentralized sites (a behaviour-change risk in core auth/user flows for marginal gain), the seam ships an **opt-in** helper `helpers/resolveErrorMessage(err, fallback?)` whose resolution order is: addon namespace `<module>:errors.<rest>` (for a dotted code `<module>.<rest>`) → core `errors.<code>` → backend `detail` → explicit fallback / raw code. An addon that wants localized error messages ships the keys in its own namespace bundle and renders through this helper — no keys are added to the core `errors.*`. Existing core sites keep their inline logic; they may adopt the helper later. The 80% of value — addon **page** strings — is delivered by D1–D4; this closes error-code isolation without a risky refactor.

## Consequences

**What changes**

- `ModuleManifest` grows one optional field (`injectI18n`); a new boot hook (`useModuleI18nInjection`) is mounted at app root alongside `useModuleApiInjection`/`useLanguageSync`.
- `src/modules/_template/` gains example locale bundles, an `i18n.d.ts`, a parity test, and a README step documenting the convention.
- The core locale parity helper is extracted to `src/locales/parityCheck.ts` (core behaviour unchanged).

**What we enable**

- An addon is fully self-contained: adding it touches only addon files; removing it is a directory delete with **zero** diff against core locale files.
- No cross-addon key collisions; no unbounded growth of the core `en.json`/`it.json`.
- The frontend i18n layer now honours the same "addons never modify core" invariant ADR-0006 established for the backend.

**What we give up / accept**

- Two ways to localize now coexist: the core monolith (`translation` ns) and per-addon namespaces. Core strings stay in the monolith — we explicitly do **not** retrofit core into namespaces (no payoff, large churn).
- Addon authors must learn one extra convention (bind `useTranslation('<name>')`, ship `{ en, it }`). Documented in `_template/README.md` and `frontend-admin/CLAUDE.md`.
- Adding a brand-new **language** (e.g. `fr`) remains a core change (`i18n.ts` `resources`/`supportedLngs`), not an addon operation — unchanged from today.
- The D5 error-code resolver is opt-in: core call sites keep their inline `t('errors.<code>')` logic, so two error-resolution styles coexist until/unless a fork consolidates them.

**Scope**

- Applies first to `frontend-admin` (operator console). `frontend-client` (Tier-2 SPA, [ADR-0003](0003-three-audience-host-split.md)) mirrors the seam for its own future addons; if it has no module system yet, the `addResourceBundle` convention is documented for when it does.

## Alternatives considered

1. **Status quo — addons edit the core locale files.** Rejected: it is the defect this ADR exists to fix and directly violates ADR-0006's core-immutability invariant.
2. **Build-time merge — a fork's build script scans `src/**/locales/*.json` and merges them into the core monolith.** Rejected: re-creates a single namespace (collisions return), couples the build to a directory-scanning convention, and still produces a core file an addon effectively "owns" — just generated instead of hand-edited.
3. **i18next http-backend with lazy per-namespace fetch over the network.** Rejected: adds a runtime dependency and network round-trips for bundles that are tiny and known at build time; the dynamic-import-at-boot approach gets code-splitting without the plumbing.
4. **Retrofit the entire core into per-module namespaces too.** Rejected: large churn across the whole console for no functional gain; the core ships as one unit and has no isolation requirement against itself. The seam is additive and leaves core untouched.
