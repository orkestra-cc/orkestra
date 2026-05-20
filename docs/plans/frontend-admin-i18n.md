# Plan — Multi-language support for `frontend-admin` (EN + IT)

**Status:** Phase 0 ✅ (2026-05-20). Phase 1 ✅. Phase 2 setup ✅ (`errcode` + `AuthEmailInUse`). Phase 3 ✅ (i18n bootstrap, EN default, typed `t()`, `useLanguageSync`). Phase 4 substantially progressed in 2026-05-20 session:

- ✅ Item 1 Shared chrome (`5e82742`, ~65 strings)
- ✅ Item 2 Auth screens (`dc0bbdb`, ~120 strings)
- ✅ Item 17 User preferences settings (`e9b7774`, 5 files)
- ✅ Item 3 `/admin/modules` chrome (`20ba2d0`, 5 files)
- ✅ Item 4 `/admin/users` chrome (`635f1db`, 3 files)
- ✅ Items 5+6+8+9 Tenants/Clients/Roles/Observability chrome (`052c614`, 7 files — covers `/admin/tenants`, `/admin/clients`, `/admin/internal-tenants`, `/admin/roles`, `/admin/observability/log-levels`)
- ✅ EN/IT parity CI test (`f5e1032`) catches drift between locales going forward

**Deferred** — long-tail addon UIs across 92 files / 26K LOC:

- Item 7 `/admin/auth-policy` tabs (deep config forms inside the auth module detail page)
- Item 10 Billing (`/billing/*` — invoices issued/received, suppliers, companies, notifications, dashboard — Italian-heavy)
- Item 11 Documents (`/documents/templates`)
- Item 12 Company (`/company/search`, `/company/lookup` — Italian-heavy)
- Item 13 Subscriptions + Payments (`/subscriptions/*`, `/payments/*`)
- Item 14 Compliance / Identity / Marketing / Sales / AI / Graph addons (`/admin/compliance`, `/identity`, `/marketing/*`, `/sales/*`, `/ai/*`, `/graph/*`)
- Item 16 Dev module pages
- MFA settings wizard inside item 17 (`/user/settings/mfa/*`)
- Deep config sections inside item 3 (`ModuleConfigFields`, `ModuleConfigModal`, `AIModelsConfigSection`)
- Detail modals inside items 5/8 (Create/Delete/Purge tenant, Create/Edit/Delete role, audit-events detail, client detail tabs)

Each deferred chunk would benefit from focused per-addon review (each addon has its own domain vocabulary). IT reviewer assignment still open. Phase 5 (language picker UI) can now proceed — the wiring is end-to-end and the settings page chrome is extracted.
**Owner:** Salvatore
**Scope:** `frontend-admin/` primary. Thin backend slice for persisting `user.language` and an error-code contract for admin-facing handlers.
**Default language:** English. Italian ships alongside on day 1 (existing IT strings in JSX are the source of truth).

---

## Context

`frontend-admin` is currently **monolingual-with-leakage**: most labels are English but a long tail of user-facing strings is hard-coded Italian (mostly error messages and feature copy that was originally written for the SDI/billing modules). There is no i18n library, no string extraction, no language picker, and no `language` field on the user.

`frontend-client` already runs `react-i18next` with `en.json` + `it.json` (see `frontend-client/src/i18n.ts`) — but defaults to **Italian** and detects from the browser. That asymmetry isn't part of this plan; flipping the client default is a follow-up decision tracked separately.

Backend has partial scaffolding only:
- `notification_templates` collection has a `locale` field, but every seeded row is `locale="en"`.
- `Accept-Language` is parsed by `internal/shared/middleware/device.go` into request context but never consulted by any handler.
- The User model in `backend/internal/core/user/models/user.go` (re-export from `orkestra-cc/orkestra-sdk/iface`) has no `language` field.
- Handler error responses are free-text English strings passed straight to `huma.ErrorXxx(…)`. No stable error codes.

Goal: the operator picks **English** or **Italian** in their preferences. The choice persists per-user across devices. Every visible string in the admin SPA — including backend-originated error messages — switches with the choice.

## Decisions taken (scoping discussion 2026-05-20)

- **Surface:** `frontend-admin` only. `frontend-client`, mobile, and notification template seeding are explicit non-goals.
- **Languages:** English + Italian only on day 1. Adding more later is purely additive (new JSON file + select option).
- **Backend errors:** error-code contract. Handlers return a stable code (e.g. `auth.email_in_use`); frontend owns the user-facing string. Done lazily per page during string extraction, not as a big-bang refactor.
- **i18n library:** `react-i18next` + `i18next-browser-languagedetector` — same versions as `frontend-client` so the patterns transfer 1:1.
- **Default + fallback:** `lng: 'en'`, `fallbackLng: 'en'`. Detection: `user.language` (post-login) → cookie → `navigator.language` → `'en'`.
- **Persistence:** `language` field on the User model. Authoritative server-side; cookie is a UX-smoothing cache, not the source of truth.
- **Italian source-of-truth:** every IT string currently in JSX is the canonical IT translation. We extract rather than re-translate.

## Architecture decisions

- **Key convention:** namespace by feature, dot-separated. `billing.invoices.received.import.errorImporting`, `users.create.duplicateEmail`, `nav.adminModules`, etc. One JSON tree per locale: `frontend-admin/src/locales/en.json`, `it.json`. Sub-paths mirror the route tree where it makes sense.
- **Typed `t()` from day 1:** generate `Resources` types from `en.json` so misspelled keys fail typecheck. Retrofitting types after thousands of keys exist is painful — pay this cost in Phase 3, not Phase 7.
- **Error-code contract shape:** custom error type that implements `huma.StatusError` and JSON-serializes with a top-level `code` field. One const-per-code registry at `backend/internal/shared/errcode/codes.go`. The `detail` field stays as a human-readable English fallback; admin renders `t(\`errors.${code}\`, { defaultValue: detail })`.
- **Where the language picker lives:** new "Language" select inside the existing user preferences page. Not in `/admin/modules` (that's per-tenant module config, not per-user). Saves via `PATCH /v1/users/me { language }`.
- **No locale routing in URLs.** Operator console is not SEO-indexed; language is a per-user setting, not part of the URL. Saves us from rewriting `react-router` route definitions.
- **Lazy module-by-module extraction.** One PR per backend module's pages keeps reviews tight and lets the error-code refactor for that backend module happen in the same PR.

## Phases

### Phase 0 — Foundations (no code)

1. Decide and document the **key naming convention** in `frontend-admin/CLAUDE.md`. One paragraph + 3 examples.
2. Decide the **error-code naming convention**: `<module>.<situation>`, snake_case (e.g. `auth.email_in_use`, `billing.invoice_not_found`). Add to `backend/CLAUDE.md`.
3. Tag a native Italian speaker as reviewer for Phase 6.

**Exit criteria:** both CLAUDE.md updates merged.

### Phase 1 — Backend: `user.language` field

Smallest possible backend change to persist the preference.

1. **SDK iface first.** Add `Language string \`bson:"language,omitempty" json:"language,omitempty"\`` to the canonical User struct in `github.com/orkestra-cc/orkestra-sdk/iface`. Cut an SDK release.
2. **Bump the SDK** in `backend/go.mod`. The re-export in `backend/internal/core/user/models/user.go` picks it up automatically.
3. **Migration**: backfill `"en"` for all existing users. New idempotent step in `backend/internal/core/user/services/migrations.go` (or wherever user migrations live — confirm).
4. **Endpoint surface**: extend the existing self endpoint — `GET /v1/users/me` returns `language`; `PATCH /v1/users/me` accepts `language` with validation `^(en|it)$`. No new route.
5. **Tests**: handler test for PATCH validation, repo test that migration backfills, snapshot of OpenAPI dump (update `backend/openapi/enterprise.json` per [[project_ci_release_blockers]]).
6. **Tenantscope annotation** on any new query/update calls — see [[project_ci_release_blockers]].

**Exit criteria:** `curl PATCH /v1/users/me {"language":"it"}` then `GET /v1/users/me` returns `language:"it"`. Migration tested against a snapshot of dev Mongo.

### Phase 2 — Backend: error-code contract (lazy, per page)

This phase is **never done independently**. It piggybacks on Phase 4 PRs: when a frontend-admin page in module X is extracted, the same PR refactors module X's admin-facing handlers to return error codes.

**One-time setup (✅ shipped as the Phase 2 setup PR):**

1. ✅ Created `backend/internal/shared/errcode/` (renamed from the planned `errs/` to avoid collisions with stdlib `errors` and the existing `internal/shared/errors/` Manager package). Holds the `Error` envelope type, typed status builders (`BadRequest`, `Unauthorized`, `Forbidden`, `NotFound`, `Conflict`, `UnprocessableEntity`), and `codes.go` with the const registry. First entry: `AuthEmailInUse = "auth.email_in_use"`.
2. ✅ Picked **option A** — a custom error type implementing `huma.StatusError` that JSON-serializes with a top-level `code` field. No global `huma.NewError` override needed; handlers return `*errcode.Error` directly. Wire shape: `{status, title, detail, code}`. Option B was tempting because `admin_user_auth_handler.go` already does the `huma.NewError(status, "code", &huma.ErrorDetail{...})` workaround, but that overloads `detail` with the code string — option A's dedicated `code` field is cleaner and matches the pre-existing local `codedError` in `password_handler.go`, which can later swap to the shared type.
3. ✅ Golden-file contract test (`codes_test.go`): AST-parses `codes.go` and cross-checks every declared const against a snapshot map. Renames, value drift, and forgotten snapshots all fail CI.
4. ✅ Convention documented in `backend/CLAUDE.md` ("Error-code contract" section) with a worked-example code block.

**Per-page (folded into Phase 4 PRs):** every handler that returns an error and is consumed by the admin page being extracted gets a code. Handlers not yet touched stay as-is — the frontend falls back to `detail`.

**Exit criteria for the setup PR (✅):** `POST /v1/users` (and the parallel `POST /v1/admin/client-users` admin-direct create + PATCH `/v1/admin/client-users/{id}` admin update) converted end-to-end. The duplicate-email path now returns `errcode.Conflict(errcode.AuthEmailInUse, "Email already in use")` → `409 {"code":"auth.email_in_use",…}`. Verified against the regenerated OpenAPI dump.

### Phase 3 — `frontend-admin` i18n bootstrap

1. `npm i i18next@^23.16 react-i18next@^15.1 i18next-browser-languagedetector@^8` — match `frontend-client/package.json` versions exactly.
2. Create `frontend-admin/src/i18n.ts` modeled on `frontend-client/src/i18n.ts`. Differences from the client copy:
   - `lng: 'en'`, `fallbackLng: 'en'`.
   - Detection order: `user.language` (custom detector reading from the auth store) → `cookie` → `navigator` → `'en'`.
   - Cookie name `orkestra-admin-lang`, 30-day expiry — distinct from the client cookie so the two SPAs can diverge.
3. Import `./i18n` once in `frontend-admin/src/index.tsx` **before** `<App />`.
4. Add `useLanguageSync()` hook: subscribes to the auth store, calls `i18n.changeLanguage(user.language)` whenever `user.language` changes. Wire into the root component.
5. **Typed keys**: create `frontend-admin/src/i18n-types.d.ts` with:
   ```ts
   import 'react-i18next';
   import en from './locales/en.json';
   declare module 'react-i18next' {
     interface CustomTypeOptions {
       defaultNS: 'translation';
       resources: { translation: typeof en };
     }
   }
   ```
   Add `resolveJsonModule: true` to `tsconfig.json` if not already set.
6. Seed `locales/en.json` and `locales/it.json` with `{ "app": { "name": "Orkestra" } }` only. Render `t('app.name')` somewhere visible to prove the pipeline.
7. Add the language switcher to a dev-only debug panel (or just the new preferences page in Phase 5) so the rest of extraction can be validated in IT mode.

**Exit criteria:** flipping a cookie reloads the app in Italian (with only `app.name` translated). Type errors trigger on misspelled keys.

### Phase 4 — String extraction, module by module

Order chosen by user-visible impact and risk. One PR per item. Each PR also handles the error-code refactor for the backend module(s) the page consumes.

**Order:**

1. **Shared chrome** — top bar, sidebar nav (`Sidebar.tsx`, `NavbarTop*.tsx`), nine-dots menu, notifications dropdown, breadcrumbs, error/empty states, generic `<DataTable />` strings ("No data", "Filter…", pagination labels).
2. **Auth screens** — login, signup, password reset, email verification, MFA setup, OAuth flows. Many of these surface backend errors → first real use of the error-code contract.
3. **`/admin/modules`** + `/admin/modules/:name` — the catalog UI.
4. **`/admin/users`** + user profile.
5. **`/admin/tenant`** + memberships.
6. **`/admin/authz`** + role editor.
7. **`/admin/auth-policy`** (the tabs from [[project_auth_policy_roadmap]]).
8. **`/admin/clients`** (the surface from [[project_admin_clients_management]]).
9. **`/admin/observability`** (log levels) + dashboards links.
10. **Billing** (`/billing/*`) — biggest IT-string concentration (SDI invoice import, FatturaPA flows).
11. **Documents** (`/documents/*`).
12. **Company** (`/company/*`).
13. **Subscriptions + Payments** — Stripe surfaces.
14. **Compliance**, **Identity**, **Marketing**, **Sales**, **Agents/RAG admin**.
15. **Logging** (runtime log-levels page).
16. **Dev module pages** (token issuer).
17. **User preferences page** — must be done before/with Phase 5 so the language picker lives somewhere translated.

**Per-PR checklist:**
- Replace every JSX literal with `t('namespace.key')`.
- Add the key to both `en.json` (literal English) and `it.json` (literal Italian if one was hard-coded; otherwise `"TODO_IT"` marker).
- Refactor the backend handlers this page consumes to return error codes; add `errors.<code>` keys to both locale files.
- Update the page's error-rendering path from `error.message` → `t(\`errors.${error.errorCode}\`, { defaultValue: error.message })`.
- If the backend's OpenAPI changed, regenerate `backend/openapi/enterprise.json`.

**Exit criteria for the phase:** zero raw English/Italian literals in `frontend-admin/src/pages/`. Lint rule from Phase 7 stays green.

### Phase 5 — Language picker in preferences

1. Locate the existing user preferences page (likely `frontend-admin/src/pages/user/Settings.tsx` per the structure described in [[project_user_security_center]] — confirm in the impl PR).
2. Add a "Language" form section with a select: English / Italiano.
3. On change: optimistic `i18n.changeLanguage(value)` → `PATCH /v1/users/me { language: value }` → on failure, revert + toast.
4. Also write to the `orkestra-admin-lang` cookie so the choice survives logout.
5. Visually group with other personal preferences (theme, notification opt-ins).

**Exit criteria:** changing the select instantly re-renders the admin in the chosen language; a fresh login on a different browser inherits the choice from the server.

### Phase 6 — Italian completion pass

1. Grep `it.json` for `TODO_IT` markers — these are strings that had no Italian counterpart in the original JSX.
2. Translate them. Tag the native-speaker reviewer assigned in Phase 0.
3. **Visual smoke test every admin page in IT.** Italian runs ~25% longer than English on average — confirm sidebars, buttons, and table headers don't truncate or wrap badly.
4. Fix layout regressions case by case (`min-width`, `flex-shrink`, abbreviated labels).

**Exit criteria:** zero `TODO_IT` in `it.json`. Manual screenshot review of every top-level admin route in both languages.

### Phase 7 — CI guardrails

1. **Missing-key lint** — small Node script run in CI: fail if any key in `en.json` is missing from `it.json` (or vice versa). Wire into `make ci-frontend-admin`.
2. **Untranslated-literal lint** — enable `react/jsx-no-literals` (or write a small custom ESLint rule) with a curated allowlist (icons, numerics, brand names). Goal: a new hard-coded English string in a PR fails CI.
3. **Type-checked keys** — already on from Phase 3; just confirm `tsc --noEmit` runs in `make ci-frontend-admin`.
4. **Error-code contract** — the golden-file test from Phase 2 already runs in `make ci-backend`. Confirm it does.

**Exit criteria:** all four checks active in `make ci-frontend-admin` and `make ci-backend`.

## Critical files

| Path | Phase | Purpose |
|---|---|---|
| `orkestra-cc/orkestra-sdk/iface/user.go` (external repo) | 1 | Add `Language` field to canonical User |
| `backend/go.mod` | 1 | SDK version bump |
| `backend/internal/core/user/services/migrations.go` | 1 | Backfill `language="en"` |
| `backend/internal/core/user/handlers/user_handler.go` | 1 | Accept/return `language` on `/me` |
| `backend/openapi/enterprise.json` | 1, 4 | Regenerated after route/schema changes |
| `backend/internal/shared/errcode/errcode.go` (new) | 2 | `*errcode.Error` envelope + typed status builders |
| `backend/internal/shared/errcode/codes.go` (new) | 2 | Error code registry |
| `backend/internal/shared/errcode/codes_test.go` (new) | 2 | Golden-file contract test (AST-parses codes.go) |
| `backend/CLAUDE.md` | 0, 2 | Document error-code convention |
| `frontend-admin/package.json` | 3 | Add i18next deps |
| `frontend-admin/src/i18n.ts` (new) | 3 | i18n bootstrap |
| `frontend-admin/src/i18n-types.d.ts` (new) | 3 | Typed `t()` resources |
| `frontend-admin/src/index.tsx` | 3 | Import `./i18n` |
| `frontend-admin/src/locales/en.json` (new) | 3–6 | English strings |
| `frontend-admin/src/locales/it.json` (new) | 3–6 | Italian strings |
| `frontend-admin/src/hooks/useLanguageSync.ts` (new) | 3 | Sync `user.language` → `i18n.changeLanguage` |
| `frontend-admin/src/pages/user/Settings.tsx` (or equivalent) | 5 | Language picker UI |
| `frontend-admin/CLAUDE.md` | 0 | Document key convention |
| `frontend-admin/eslint.config.*` | 7 | Enable `react/jsx-no-literals` |
| `frontend-admin/scripts/check-locales.mjs` (new) | 7 | Missing-key lint script |
| `Makefile` | 7 | Wire lint into `ci-frontend-admin` |

## Explicit non-goals

- **`frontend-client` default-language flip** (currently defaults to IT). Separate decision; not blocked by this plan.
- **Mobile (Flutter) localization.** Flutter has no i18n today; greenfield work, separate plan when prioritized.
- **Notification email template per-locale seeding** beyond `en`. The collection's `locale` column exists; populating IT templates is a follow-up driven by when email matters for IT users.
- **`Accept-Language` middleware → handler context.** Only needed if a non-admin consumer (mobile, public API) starts consuming localized error codes. Admin reads codes, not localized messages, so the header path stays cold.
- **Translation memory / Crowdin / Lokalise integration.** Two languages + a single bilingual reviewer doesn't justify a TMS yet.
- **Right-to-left language support.** Not on the EN+IT path.

## Risks and mitigations

| Risk | Mitigation |
|---|---|
| **Phase 2 (error codes) bleeds into every backend module** and balloons the per-page PR size. | Strictly lazy: a page that doesn't surface backend errors doesn't force its backend module to refactor. Set a per-PR ceiling (e.g. 400 LoC backend changes); split if exceeded. |
| **IT translations diverge in vocabulary** across pages (same concept worded differently because different developers wrote them). | Phase 6 explicitly budgets a vocabulary-alignment pass. Maintain a short glossary in `frontend-admin/src/locales/GLOSSARY.md` updated during extraction. |
| **Italian text overflow** breaks layouts. | Phase 6 screenshot pass. Avoid fixed-width buttons in shared chrome; prefer `min-width` + auto. |
| **Hard-coded strings sneak back in** after Phase 7 lint exists. | Lint rule + PR review. Allowlist must be tight — every new entry needs justification. |
| **SDK release coupling** — Phase 1 needs an SDK cut before backend can bump. | Phase 1 starts with the SDK PR; bundle the SDK release into the same workstream rather than letting it become a queue. |
| **Detector picks the wrong language** before login (cookie absent, browser is IT) and the user sees IT before having a chance to set EN. | Acceptable: matches the cookie-based pattern in `frontend-client`. The picker fixes it permanently after first login. |

## Open questions

1. **Where does the user preferences page live today** in `frontend-admin/src/pages/user/`? Phase 5 confirms when started.
2. **Does Huma v2's error formatter cleanly support adding a top-level `errorCode` field**, or do we fall back to `ErrorDetail`? Decided in the Phase 2 setup PR.
3. **Migration step for the `language` backfill** — is there a registered migration runner in the user module, or do we add a one-shot `init()` check? Confirmed in Phase 1.
4. **Do any addon repos (post-SDK split, see [[project_sdk_split_extractions]]) define their own admin pages** that the lint rule would also need to cover? If yes, extend Phase 7 lint to those repos.

## Sequencing summary

```
Phase 0  ──┐
           ├─► Phase 1 (backend user.language)  ──┐
           │                                       ├─► Phase 3 (i18n bootstrap) ──┐
           └─► Phase 2 setup (error-code shell) ──┘                                │
                                                                                   │
                                Phase 4 (string extraction, per-module PRs)  ◄────┘
                                  │   │   │   ...   │
                                  ▼   ▼   ▼   ...   ▼
                                Phase 5 (picker UI)  ◄── lands once preferences page is in Phase 4 batch
                                  │
                                  ▼
                                Phase 6 (IT completion pass)
                                  │
                                  ▼
                                Phase 7 (CI guardrails — turned on last so they don't block extraction PRs)
```

Phases 0–3 are linear. Phase 4 is the long tail and can be parallelized across contributors once 3 lands. Phase 7 is intentionally last so the lint rule doesn't flag every page mid-extraction.
