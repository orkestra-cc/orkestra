# Frontend Admin — Operator Console (Tier-1)

_Path: `/frontend-admin`_  
_Parent: [../CLAUDE.md](../CLAUDE.md)_

[← Root](../CLAUDE.md) | [☰ Module Map](../CLAUDE.md#module-map) | [🚀 Quick Start](../CLAUDE.md#quick-start) | [Tier-2 client SPA](../frontend-client/CLAUDE.md)

React 19 + Vite 8 + TypeScript 5.9 operator console for Orkestra — the **Tier-1 admin dashboard** used by internal staff. Cookie-based auth with the Go backend (operator audience), dynamic navigation driven by `/v1/navigation`, per-module RTK Query slices, Orkestra design system + Bootstrap 5. Sibling to [`../frontend-client`](../frontend-client/CLAUDE.md), the Tier-2 customer-facing SPA — different audience, different cookie domain, different stack.

> **Before writing any UI code here, invoke the `orkestra-frontend-admin` skill** (`.claude/skills/orkestra-frontend-admin/SKILL.md`). It enforces the reference-first workflow: read the matching `src/reference/*.tsx` showcase and a production-page precedent before any JSX.

The console's **visual design authority** is [`DESIGN.md`](DESIGN.md) — normative token frontmatter (Cool Graphite ramp, Orkestra Blue, type scale) plus named rules such as the frozen-dark invariant — with its machine-readable sidecar `.impeccable/design.json` (rendered component snippets). Both are generated from the SCSS theme and `src/reference/` via `/impeccable document`; keep them in sync with `src/assets/scss/theme/` when the theme changes.

## Tech stack

| Layer       | Choice                                                                                                                       |
| ----------- | ---------------------------------------------------------------------------------------------------------------------------- |
| Framework   | React 19, React Router 8 (single `react-router` package — `react-router-dom` no longer exists)                               |
| Build       | Vite 8 — rolldown (dev server + production bundle)                                                                           |
| Language    | TypeScript 5.9 strict mode                                                                                                   |
| State       | Redux Toolkit 2.9 + RTK Query (server state lives in RTK Query, not React Query)                                             |
| UI kit      | React Bootstrap 2.10 + Bootstrap 5.3 + Orkestra SCSS theme                                                                   |
| Forms       | React Hook Form + Yup                                                                                                        |
| Charts      | ECharts (lazy-loaded chunks). Chart.js + D3 reference samples were removed — use `echarts-for-react` for any new chart work. |
| Calendar    | FullCalendar                                                                                                                 |
| Maps        | Google Maps + Leaflet                                                                                                        |
| Tables      | TanStack Table v8                                                                                                            |
| Drag & Drop | dnd-kit                                                                                                                      |
| Auth        | Cookie sessions + Bearer access tokens (RS256 JWT issued by backend)                                                         |

## Directory layout

```
frontend-admin/
├── src/
│   ├── App.tsx                    # Root component
│   ├── index.tsx                  # Entry point
│   ├── config.ts                  # App config, theme defaults
│   ├── routes/
│   │   ├── createRouter.ts        # Router factory — assembles core + module + reference routes
│   │   ├── coreRoutes.tsx         # Auth, admin, user/operator routes (always loaded)
│   │   ├── referenceRoutes.tsx    # Orkestra template routes (dev-only, gated by import.meta.env.DEV)
│   │   └── paths.ts               # Path constants
│   ├── layouts/                   # 9 layouts: MainLayout, VerticalNavLayout, TopNavLayout, ComboNavLayout, AuthLayouts...
│   ├── providers/                 # AppProvider, AuthProvider, KanbanProvider, ChatProvider, EmailProvider
│   ├── store/                     # Redux store + RTK Query slices
│   │   ├── index.ts               # Store configuration
│   │   ├── ReduxProvider.tsx      # Provider with redux-persist
│   │   ├── hooks.ts               # Typed useAppSelector / useAppDispatch
│   │   ├── slices/                # Redux slices (auth, kanban)
│   │   └── api/                   # RTK Query slices — one per backend module
│   ├── pages/                     # Production pages (core only — ADR-0006)
│   │   ├── admin/                 # Module admin, users, clients, tenants, roles, navigation, observability
│   │   ├── operator/              # Operator profile
│   │   ├── setup/                 # First-install wizard
│   │   └── user/                  # User settings + security center
│   │   # (a fork's addon pages live alongside these, e.g. pages/billing/)
│   ├── modules/
│   │   ├── index.ts               # Module catalog — EMPTY moduleCatalog (a fork registers its own)
│   │   ├── types.ts               # ModuleManifest interface
│   │   ├── useModuleApi.ts        # Hook to lazily inject API slices for enabled modules
│   │   ├── README.md              # Module conventions + backend ↔ frontend map
│   │   └── _template/             # Copy-paste scaffold for adding a new module
│   ├── components/
│   │   ├── common/                # 🎯 UI primitives (Avatar, UserAvatar, Card, Flex, IconButton, AdvanceTable, ...) — barrel exported
│   │   ├── authentication/        # Login forms, ProtectedRoute, OAuth callback handlers. SocialLoginForm renders buttons from the live backend list (`useGetOAuthProvidersQuery` → `GET /v1/auth/operator/providers`) so toggling a provider on `/admin/modules/auth` removes it from the login page within 30s — never hardcode the provider list here. ProtectedRoute stashes the attempted URL in `location.state.from`; every login-completion path (password, MFA, OAuth-via-`sessionStorage`) returns the user there through `utils/returnTo.ts`'s `sanitizeReturnTo` (open-redirect + auth-loop guard, default `DEFAULT_POST_LOGIN` = `/user/dashboard` — the old `/dashboard/analytics` was a dev-only reference demo that 404s in production builds) — never hardcode the post-login destination. The OAuth landing is `SocialAuthCallback`, bound to the backend's CLOSED callback contract (`?success=true&provider=<google|apple|github|discord>`, `?success=false&error=<allowlisted>`, MFA continuation in the **fragment** `#requiresMfa=true&mfaToken=&webauthnAvailable=<true|false>`; parser in `utils/oauthCallbackParams.ts` matches each shape on its EXACT key set and cardinality — an unknown provider, a half-formed fragment, an extra or duplicated key, a fragment next to a query outcome or a query next to a fragment is the generic failure; error codes → `auth.social.callback.errors.*`, raw URL text is never rendered). It parses the URL once on first render, then in its FIRST effect takes-and-deletes the return target and scrubs the URL (`navigate(pathname, {replace:true})`) before its first await — that effect must stay a passive `useEffect`, because React commits layout effects child-first and a `useLayoutEffect` here runs before the Router's own history subscription is live, so the scrub is silently dropped; it force-fetches `getSession` and navigates only after the refresh-cookie session is confirmed (signed-out → login error, 503 → retry), and renders the MFA challenge through `MfaVerifyPanel` from component memory — never `location.state` (the password path's `LoginMfaVerify` page still reads router state, which never travels in a URL). The OAuth return target is a `{target, createdAt}` record under `oauth_return_to`, written by `stashOAuthReturnTo` and taken by `takeOAuthReturnTo` — a destructive read that must run in an effect, never during render — honoured only within 10 minutes and after `sanitizeReturnTo`. `socialAuthUtils` persists nothing else: the signed state is bound to the browser by the backend's HttpOnly `orkestra_oauth_state` cookie, so `oauth_state` / `oauth_provider` are no longer written and the client-side callback helper is gone. `MfaVerifyPanel` is a `react-hook-form` + `yup` form with `orkestra-*` buttons — the shape every new auth form follows. The unauthenticated password surface is policy-gated (PR 3): `EmailPasswordForm` (now `react-hook-form` + `yup`) renders only when `/policy`'s `passwordLoginEnabled` is literally `true` — a served `false` OR `null` hides it (the shared predicate is `passwordUiVisible` in `store/api/authApi.ts`; only a TRANSPORT failure falls open, via the queryFn fallback) — except under `passwordLoginBreakGlassEffective`, which renders a labelled emergency form with the forgot-password and register CTAs hidden. `RegisterForm` and `ForgotPasswordForm` render only the disabled alert in that state, break-glass included. `SocialLoginForm` reports its resolved provider count through `onProvidersResolved` (success only, never on a query error) so `Login` can show the no-sign-in-method alert without a second query — an outage renders the retryable provider error instead, never "no method".
│   │   ├── dashboards/            # Reusable dashboard widgets
│   │   ├── navbar/                # Sidebar + top navigation
│   │   ├── wizard/                # Form wizard helpers
│   │   ├── errors/                # 404, 500 pages
│   │   └── notification/          # Toast and banner notifications
│   ├── reference/                 # 📚 Orkestra design-reference library (editable, Orkestra-owned) — 7 example apps + 60+ samples + our own showcases
│   │   ├── app-examples/          # calendar, chat, email, events, kanban, social, support-desk
│   │   ├── components/            # UI showcase (forms, tables, navigation, media, etc.)
│   │   ├── charts/                # ECharts examples only (chartjs/d3js removed — unresolved imports)
│   │   ├── dashboards/            # 11 complete dashboard layouts
│   │   ├── pages/                 # Landing, FAQ, pricing, miscellaneous templates
│   │   └── utilities/             # Bootstrap utility-class examples
│   ├── hooks/                     # Custom hooks (useRoleBasedNavigation, useSettings, useAuth*)
│   ├── helpers/                   # Pure utility functions
│   ├── types/                     # Shared TypeScript types per backend module
│   ├── data/                      # Static data, mock APIs, lookups
│   ├── docs/                      # Component docs (separate from src/reference/)
│   ├── test/                      # Test infra: MSW server, renderWithProviders, default handlers
│   └── assets/                    # Images, SCSS, fonts
├── public/                        # Static files served as-is
├── Dockerfile                     # Multi-stage: builder (node:24-alpine) → production (nginx:alpine)
├── tsconfig.json                  # Path aliases declared here AND in vite.config.js
├── vite.config.js                 # Vite config with manualChunks for vendor splitting
├── vitest.config.ts               # Vitest config — happy-dom env
└── package.json
```

## Path aliases

The project uses **bare path aliases** (no `@/` prefix). They are declared in both `tsconfig.json` and `vite.config.js`:

```ts
import Avatar from 'components/common/Avatar'; // not '@/components/common/Avatar'
import { useRoleBasedNavigation } from 'hooks/useRoleBasedNavigation';
import UsersPage from 'pages/admin/users';
```

Available aliases: `App`, `components`, `pages`, `layouts`, `providers`, `hooks`, `helpers`, `data`, `assets`, `routes`, `store`, `config`, `reference`, `types`, `utils`, `widgets`, `features`, `demos`, `docs`, `reducers`, `test`.

## How navigation works

Navigation is **backend-driven**. The React app does not define its own menu — it fetches the menu the user is allowed to see from `/v1/navigation` and renders it.

```
backend module.go NavItems()
  → backend navigation core module aggregates all enabled modules,
    filters by module-enabled + tenant kind (Tier) + system role (MinRole)
    → /v1/navigation returns { groups[], realms[], tenantKind, userRole }
      → frontend navigationApi (RTK Query) caches the response per role+tenantKind
        → useRoleBasedNavigation hook exposes realms + legacy groups to layouts
          → NavbarVertical renders realm → section → items, falls back
            to flat groups[] when realms are empty
```

The response carries **two shapes** for a transition window:

- `groups[]` — legacy flat `label + children` (v1, still populated for any consumer that hasn't migrated).
- `realms[]` — nested `realm.key → sections → items` (v2). Realm keys are `personal | platform | business | shared`, with canonical labels `My workspace | Administration | Business | Tools` — `platform` is the admin-only realm (gated `MinRole=administrator` at every item), `business` is the operator's day-to-day work surface for managing external clients, revenue, etc.

Each `NavItemSpec` a backend module declares carries `Realm`, `Section`, and `Tier` (`"internal" | "external" | ""`). `Tier="internal"` items are filtered out for callers acting in an external tenant and vice versa, so external Tier-2 admins never see operator-only routes in the menu even if their role would otherwise grant access.

This means:

- **Adding a sidebar entry** → edit the backend module's `NavItems()` — set `Realm`, `Section`, `Tier`, not the legacy `Group`. The frontend picks it up on the next `/v1/navigation` fetch.
- **Disabling a module on the backend** → its sidebar entry disappears automatically, and `ModuleGate` redirects to 404 if the URL is accessed directly.
- **The frontend route is declared in the module manifest** → `src/modules/<name>.tsx` defines routes, registered via `src/modules/index.ts`.

**Dev-only exception — Developer realm.** When `import.meta.env.DEV` is true (or `VITE_ENABLE_REFERENCE` is set) **and** the signed-in user's role is `developer` (or `super_admin`, which outranks it), `NavbarVertical` appends a hardcoded `Developer` realm from `src/reference/navigation/referenceRoutes.ts` (`developerRealm` export) pointing at the dev-only `/reference/*` routes registered by `src/routes/referenceRoutes.tsx`. The build-time gate matches the one on the routes themselves, so nav and routes stay in lockstep; the role check on top keeps the menu entry out of every other role's sidebar. Note this is a **cosmetic menu restriction only** — the `/reference/*` routes themselves are still gated solely by build mode, so a non-developer can reach them by URL. This is the **only** place sidebar entries are hardcoded in the frontend — do not extend the pattern to production features.

**Operator reorder + visibility audit.** `/admin/modules/navigation` (admin-only) renders the full unfiltered tree from `GET /v1/admin/navigation` and lets operators drag-to-reorder items within a parent, sections within a realm, and realm cards themselves. Persisted overrides are PATCHed back per-parent; mutations invalidate both the `NavigationAdmin` and the public `Navigation` RTK Query tags so the live sidebar reflects the new order without a page refresh. The same page audits visibility: a tenant-kind switch + "Show role matrix" overlay tri-state chips (per role × tenant kind, colour-coded by the server-computed `visibility` truth table — green visible, gray role-below-min, amber hidden by module/config/tier/parent), the detail panel shows the full role×tenant grid + config-gate state, and "View as" previews the sidebar as any role/tenant persona (reorder disabled in preview). All visibility comes from the backend — the SPA never recomputes it. See [backend navigation docs](../backend/internal/core/navigation/CLAUDE.md) for the override semantics + self-heal behaviour and the visibility truth table.

## How data fetching works

All server state goes through **RTK Query**, not React Query / TanStack Query. Each backend module gets its own slice in `src/store/api/`:

```
src/store/api/
├── baseApi.ts          # createApi() with createBaseQuery + global tagTypes
├── authApi.ts          # core: auth endpoints
├── userApi.ts          # core: user endpoints
├── tenantApi.ts        # core: orgs + memberships
├── mfaApi.ts           # core: MFA factors
├── deviceTrustApi.ts   # core: trusted devices
├── navigationApi.ts    # core: /v1/navigation
├── navigationAdminApi.ts # admin: /v1/admin/navigation tree + ordering overrides
├── moduleApi.ts        # admin: /v1/admin/modules
├── observabilityApi.ts # admin: /v1/admin/observability/log-levels (ADR-0005 Phase F)
├── setupApi.ts         # core: first-install wizard
├── managementApi.ts    # template demo (events/kanban/support)
├── communicationsApi.ts # template demo (chat/email)
└── dashboardApi.ts     # template demo (dashboard analytics)
```

A fork's addon slice (e.g. `billingApi.ts`) sits alongside these. ADR-0006 removed all addon slices from the base.

All slices extend `baseApi` via `injectEndpoints`. To add a new tag type, declare it in `baseApi.ts`'s `tagTypes` array. Auth uses **cookies + Bearer token** — `credentials: 'include'` is set in the base query, and the access token from the auth slice is added to the `Authorization` header while it is unexpired. The backend's `RequireAuth` is **bearer-only** (ADR-0020, #317): it never reads the refresh cookie, so `baseQueryWithRetry` owns the whole refresh lifecycle in three layers — (1) **proactive**: any non-auth request whose token expires within `PROACTIVE_REFRESH_SKEW_MS` (30 s — kept strictly below the backend's 60 s `MinAccessTokenTTL`; at or above it a floor-length token would refresh on every request) first awaits `performRefresh`; (2) **reactive**: a 401 on a non-auth endpoint runs the same `performRefresh` and retries once; (3) `performRefresh` itself is serialised — one in-flight promise per tab, a Web Lock across tabs, one `409 refresh_rotation_raced` retry, and 503 = "keep the token, try later". `refreshOnce`'s fetch is itself bounded by `REFRESH_FETCH_TIMEOUT_MS` (10 s, via `AbortSignal.timeout`) so a `/refresh-cookie` that accepts the connection and never answers can't stall the outbound request behind it forever; a timeout, and any other network-level throw, is treated exactly like the 503 case (`{ ok: false, retry: true }`), never as a bare failure — the pre-existing `catch` used to collapse both into one outcome the 401 branch reads as a real negative answer and signs the user out, which would have turned a slow network into an unwanted logout. The SPA rotates only at `POST /v1/auth/operator/refresh-cookie`; `GET /v1/auth/session` mints without rotating. Never add another caller of `/refresh-cookie` or `/refresh` — the legacy `utils/apiClient.ts`, `hooks/redux/useAuth.ts`, and `authSlice.refreshSession` were exactly that and have been deleted (#317 follow-up).

## How first-install setup and tenant selection work

A fresh installation is gated by `SetupGate` (`src/pages/setup/SetupGate.tsx`), mounted in `App.tsx` outside the auth gate so an uninitialized system never leaks any other route. It subscribes to `GET /v1/setup/status` (the `Setup` RTK Query tag) and force-redirects anything outside `/setup` while the authoritative `phase !== 'complete'`. A `503 setup.status_unavailable` — the backend failing closed when it cannot read the real phase — renders a neutral, auto-retrying "unavailable" screen; it never falls through to the wizard and never infers a phase from a cached value.

`SetupWizard.tsx` has four steps — Welcome, Administrator, Organization, Done — and is resumable: the starting step is derived from the authoritative `phase` (`tenant_required` resumes directly at the Organization step, skipping Welcome/Administrator) until the operator manually advances, at which point local `manualStep` wins. A refresh, a lost response, or a backend restart all land back on the correct step.

**The Organization step (`OrgStep.tsx`) is the finalizer**, and creating the initial Tier-1 organization there is mandatory (no skip). It drives a resumable, authenticated finalize saga (`POST /v1/setup/finalize` — 200 / 202 / 403 / 409) whose lock and recovery states `OrgStep` renders directly, mirroring `backend/internal/shared/setup/service.go`'s `evaluateAccessDetailed`:

- **Not authenticated** — the restored-session probe comes back empty → a sign-in prompt that returns here after login.
- **Access probe unavailable** — the finalization-access query errors, or returns a shape with neither `canFinalize` nor `canClaimRecovery` (unreachable per the backend's contract, but handled fail-closed) → a neutral, retryable warning.
- **`bound_to_another_admin`** — another administrator already holds the finalization lease; only "Switch account" / "Log out" are offered, no form.
- **`recovery_requires_super_admin`** — the bound administrator is no longer usable and the caller isn't an active `super_admin`; same locked shape, different copy.
- **`canClaimRecovery: true`** — an active `super_admin` may claim an abandoned setup: the form renders normally with a recovery warning banner on top.
- Once submitted, local `phaseState` covers the saga's own async states: `submitting` (form locked), `inProgress` (a 202 — an _identical_ concurrent request already holds the lease; this is a success, not an error, and the identical payload is auto-retried on the server's `Retry-After`), `refreshingSession` (see the re-mint paragraph below), `done`.

**The setup-status cache is invalidated explicitly at both phase boundaries**, never left to its own TTL — `getSetupStatus` is cached 300s precisely because the underlying phase only flips twice per deployment and both transitions invalidate it themselves: `createInitialAdmin` invalidates `['Setup', 'Auth', 'User', 'Navigation']` on success (`admin_required` → `tenant_required`), and `finalizeSetup`'s `onQueryStarted` invalidates `['Setup', 'Membership', 'Org', 'Navigation', ...tenant-scoped tags]` on a terminal 200 (`tenant_required` → `complete`) — but only _after_ the session re-mint below succeeds.

**A successful finalization must re-mint the session before any tenant-scoped request goes out, and `finalizeSetup` enforces this itself rather than trusting the caller.** The administrator's access token was minted before the Tier-1 tenant and its owner membership existed, so the token's `mbr` claim doesn't carry the new membership — an `X-Tenant-ID` request sent with that stale token would be rejected outright by the backend's tenant-resolution middleware (`resolveCurrentTenant`, `backend/internal/shared/middleware/auth.go`), which only ever picks among memberships already present on the token. `finalizeSetup`'s `onQueryStarted` therefore force-refetches `GET /v1/auth/session` (whose queryFn dispatches `setAccessToken` before resolving) and confirms a fresh `accessToken` landed **before** invalidating anything. If the re-mint comes back empty, nothing is invalidated and `OrgStep` surfaces the `refreshingSession` state instead — retry resubmits the identical captured payload, and the backend saga is idempotent on it, so nothing is created twice. Never invalidate `Membership`/`Org`/tenant-scoped tags off the raw 200 — a tenant-scoped refetch racing the old token is a guaranteed 403.

**Tenant selection: a stored choice wins over the JWT fallback, and neither grants membership.** `state.tenant.currentOrgId` (rehydrated from `localStorage['orkestra.currentOrgId']` and re-validated against the live membership list on every `setMemberships` — see `tenantSlice.ts`) is what `baseApi.ts`'s `baseQueryWithRetry` stamps as `X-Tenant-ID` on every tenant-scoped request (operator-admin impersonation, when active, takes precedence over it). The backend's own `resolveCurrentTenant` checks that header before it ever falls back to the JWT's own fallback tenant (`TenantFallbackID`, wire claim `dtid` — see `backend/internal/core/auth/CLAUDE.md`), so as long as the frontend has a valid stored selection, the server-side fallback claim never runs. But **neither path grants membership**: both are only choosing which of the token's _already-present_ memberships to scope the request to — `resolveCurrentTenant` matches the requested/fallback tenant ID against `claims.Memberships` and fails closed (no tenant resolved) on a miss, it never creates one. Don't re-derive this selection logic anywhere else in the frontend — `tenantSlice.ts`'s `setMemberships` reducer (stored → owned → first membership) is the one place it lives.

## Adding a new feature module

This is the **canonical workflow** for an LLM agent or contributor asked to add a new module:

1. **Read `src/modules/_template/README.md`** first. It walks through the full pattern with a worked example (`widgets`).
2. **Copy the scaffold files**:
   - `_template/api.ts` → `src/store/api/<name>Api.ts`
   - `_template/types.ts` → `src/types/<name>.ts`
   - `_template/pages/ExamplePage.tsx` → `src/pages/<name>/list/index.tsx` (and adapt)
   - `_template/components/ExampleCard.tsx` → co-locate next to your page
3. **Add cache tag types** to `src/store/api/baseApi.ts` `tagTypes` array.
4. **Create a module manifest** — `src/modules/<name>.tsx` with routes wrapped in `<ModuleGate>` + `<ProtectedRoute>` + `<Suspense>`, and an `injectApi` function that dynamically imports the API slice.
5. **Register the manifest** in `src/modules/index.ts` — add it to the `moduleCatalog` record.
6. **Backend declares the sidebar entry** via its module's `NavItems()` method. The link appears in the sidebar automatically once the user has the required role and the backend module is enabled.

`src/modules/_template/` is the **single source of truth** for the convention. If you change the pattern, update `_template/` so future scaffolds pick up the change.

## Component reuse hierarchy

When asked to build a UI, look for an existing solution in this order:

1. **`src/reference/app-examples/`** — full Orkestra implementations of common apps (calendar, chat, email, kanban, social, support-desk, events). Copy and adapt — don't reinvent.
2. **`src/reference/components/`** — 60+ Orkestra component samples (forms, tables, navigation, media, charts).
3. **`src/components/common/`** — UI primitives that the app's pages already use (Avatar, UserAvatar, Card, Flex, IconButton, PageHeader, AdvanceTable, OrkestraDropzone, ...). For user identities ALWAYS prefer `<UserAvatar user={...}>` over raw `<Avatar src={url}>` — UserAvatar handles the backend's `avatarSource` semantics (resolved URL when present, initials over a deterministic per-user color from `helpers/avatarColor.ts` otherwise).
4. **`src/components/dashboards/`** — reusable dashboard widgets (WeeklySales, ActiveUsers, ...).
5. **`react-bootstrap`** — raw primitives for layout (Row, Col, Card, Button, Form).

Only build a new component if none of the above fits. New components used by exactly one page live next to that page (`src/pages/<module>/<feature>/MyHelper.tsx`). Promote to `components/common/` only when a second page needs it.

## State management

| Concern                         | Where it lives                                  |
| ------------------------------- | ----------------------------------------------- |
| Server state (cached responses) | RTK Query (`src/store/api/`)                    |
| Auth user + tokens              | Redux slice (`src/store/slices/authSlice.ts`)   |
| Kanban board state              | Redux slice (`src/store/slices/kanbanSlice.ts`) |
| Theme, navbar config, RTL       | `AppProvider` context                           |
| Form local state                | React Hook Form                                 |
| Component local state           | `useState`                                      |

Persisted state is opt-in via `redux-persist` — only user preferences are persisted, never tokens.

## Build & dev

```bash
npm run dev               # Vite dev server (port 5173 inside container, mapped to host)
npm run dev:staging       # Dev with staging mode flags
npm run build             # tsc + vite build (production)
npm run build:staging     # Staging build
npm run preview           # Serve built bundle locally
npm run typecheck         # tsc --noEmit (CI-safe)
npm run lint              # eslint src/ --max-warnings 0
npm run test              # Vitest single-pass run
npm run test:watch        # Vitest watch mode
```

The `tsc` step in `build` enforces strict mode — TypeScript errors fail the build.

**Dev-server file watcher** — `vite.config.js`'s `resolveWatchOptions()` enables chokidar stat-polling (300 ms) only when `/proc/version` shows a WSL kernel, where inotify events from a `/mnt/c` bind mount never arrive; everywhere else the default event watcher runs, because polling this tree (~1.4k files) kept idle containers at a double-digit CPU share and ~1 GB RSS. `CHOKIDAR_USEPOLLING=true|false` (passed through by the dev/staging compose files from `docker/.env`) overrides the auto-detect in either direction. Don't hardcode `usePolling` back into the config.

## Testing

Vitest + React Testing Library + happy-dom + MSW. The infra lives in `src/test/`:

| File                     | Purpose                                                                                                                                                                                                                                                                                                 |
| ------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `src/test/setup.ts`      | Vitest global setup — jest-dom matchers, MSW lifecycle (`onUnhandledRequest: 'error'` so missing stubs fail loud), `resetCapturedRequests()` between tests. `./webStorage` **must stay its first import** — see that row                                                                                |
| `src/test/webStorage.ts` | Restores `localStorage` / `sessionStorage` on Node ≥ 25 — see the gotcha below. Guarded, so it no-ops on the pinned Node 24. Imported for its side effect, and first: ES imports are hoisted, so a plain statement in `setup.ts` would run after every other import had had its chance to touch storage |
| `src/test/server.ts`     | Single shared `setupServer(...defaultHandlers)` reused by every test file                                                                                                                                                                                                                               |
| `src/test/handlers.ts`   | Default MSW handlers + per-endpoint request capture (`capturedRequests.billingStatsParams` etc.) for tests that need to assert outbound params                                                                                                                                                          |
| `src/test/render.tsx`    | `renderWithProviders(ui, { preloadedState, store, routerEntries })` — wraps in a fresh non-persisted Redux store + `MemoryRouter`. Returns `{ store, ...renderResult }`                                                                                                                                 |

**Default pattern**: real component, real Redux store, real RTK Query, MSW for HTTP. Mock hooks (`vi.mock`) only when testing branching logic of a hook's _consumer_ (e.g. `ProtectedRoute` mocking `useAuth`), not when testing data flow.

```tsx
import { renderWithProviders } from 'test/render';
import { server } from 'test/server';
import { http, HttpResponse } from 'msw';

server.use(http.get('*/v1/whatever', () => HttpResponse.json({ ... })));
const { store } = renderWithProviders(<MyComponent />);
expect(await screen.findByText(...)).toBeInTheDocument();
expect(store.getState().auth.accessToken).toBe('...');
```

**Configuration gotchas:**

- `environment: 'happy-dom'` (not jsdom) — jsdom + MSW v2 + Node fetch trip over `RequestInit: Expected signal to be an instance of AbortSignal`.
- **Web Storage is undefined on Node ≥ 25 without a shim.** Node 25 added global `localStorage` / `sessionStorage`, defined on `globalThis` as own accessors that evaluate to `undefined` (with an `ExperimentalWarning`) unless the process was started with `--localstorage-file`. Those own properties pre-empt the ones the happy-dom environment would install, so both globals — and `window.localStorage`, which resolves to the same binding here — read back undefined even though happy-dom implements Web Storage. `src/test/webStorage.ts` puts them back. The repo pins Node 24 (`.mise.toml`), so CI never saw this; a contributor on a newer Node did, as three unrelated red suites. Note the failure is not always legible: code that reads storage inside a reducer surfaces as a _timeout_ on an unrelated assertion, not as a `TypeError`.

## Runtime config

The SPA reads `window.__ORKESTRA_CONFIG__` from `/config.js` (a classic `<script>` loaded before the main bundle in `index.html`). `src/config/environment.ts` consumes that object and falls back to `import.meta.env.VITE_*` only when a key is missing. One published image works in dev / staging / prod because the URLs live in `/config.js`, not in the compiled bundle.

| Path                       | Tracked?          | Who writes it                                                                         |
| -------------------------- | ----------------- | ------------------------------------------------------------------------------------- |
| `public/config.example.js` | ✅ yes            | Source-controlled template with dev defaults. Don't edit per-environment values here. |
| `public/config.js`         | ❌ **gitignored** | Each environment regenerates it at container start.                                   |

Who writes `public/config.js` at runtime:

| Environment                                    | Generator                                                                                                                                                                                  |
| ---------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Dev / staging (bind-mounted `npm run dev`)     | `command:` step in `docker/docker-compose.dev.yml` / `docker-compose.staging.yml` — writes from `VITE_*` env vars before Vite boots.                                                       |
| Prod / SKU profiles (nginx image)              | `/docker-entrypoint.d/10-write-config.sh` baked into `frontend-admin/Dockerfile` — writes from `ORKESTRA_*` env vars before nginx forks.                                                   |
| `npm run dev` directly on the host (no Docker) | You. Run `cp public/config.example.js public/config.js` once; edit values if you need non-default URLs.                                                                                    |
| CI `npm run build`                             | The Dockerfile's `RUN cp -n public/config.example.js public/config.js` seeds the build context so `dist/config.js` always exists. The runtime entrypoint overwrites it on container start. |

Adding a new field: declare it on `RuntimeConfig` in `src/config/environment.ts`, read it via the `config` singleton, and add the env-var fallback in **all three** generators (dev compose, staging compose, nginx entrypoint). Never reach for `import.meta.env.VITE_*` from new code — those bake at build time and defeat the point.

**`/config.js` must never be cached, and both serving paths now say so explicitly.** The file is rewritten on every container start, so a client holding a stale copy points at the wrong `apiUrl` until its cache expires. Production nginx sets `Cache-Control: no-store` (`Dockerfile`, `location = /config.js`); the dev server — which is what **dev and staging** run — gets the same header from the `orkestra-runtime-config` plugin in `vite.config.js`, which serves the file itself rather than letting Vite's public-dir middleware answer. Two reasons it works that way, both load-bearing:

- Vite's public-dir middleware (sirv) writes `Cache-Control: no-cache` **unconditionally into the response head**, so a middleware that only calls `res.setHeader` upstream of it is silently overridden. Short-circuiting the request is the only way to own the header — the same shape the `/health` plugin in that file uses.
- `no-cache` is not "don't cache", it is "cache but revalidate", and that is enough for a CDN to keep a copy. Cloudflare classifies `.js` as a static asset **by extension**, so it cached `/config.js` (while leaving `/index.html` and `/src/*.tsx` as `DYNAMIC`) and replaced the origin header with its own default `max-age=14400`. Staging served a 4-hour-stale runtime config after a deploy because of it.

**If a middleware edit to `vite.config.js` looks inert, restart the container.** Vite watches its own config and logs `server restarted` on save, but that reload cannot be relied on to replace a middleware a previous `configureServer` registered: here three consecutive reloads logged a clean restart while the old handler kept answering, and only `docker restart` made `no-store` appear. It is not deterministic — the client SPA picked the equivalent edit up from a plain reload — so treat the restart as the way to _rule the reload out_, not as a step that is always needed.

If a stale config reappears at an edge, diff origin against edge before touching this code — `curl -sI http://127.0.0.1:${FRONTEND_PORT}/config.js` vs `curl -sI https://<host>/config.js` — a `cf-cache-status` other than `DYNAMIC`/`BYPASS` means the CDN, not the app, owns the header, and the fix is a cache rule on the zone.

## Application version

The version string rendered in the footer (`src/components/footer/Footer.tsx` reads it from `src/config.ts`) and embedded in the dev-server `/health` response is derived from the git tag, not `package.json#version`. The chain:

1. `vite.config.js` calls `resolveAppVersion()` at config-evaluation time.
2. It tries `GITHUB_REF_NAME` (set by CI on tag pushes) → `ORKESTRA_VERSION` (host-side override) → `git describe --tags --always --dirty` → `"dev"` fallback.
3. The resolved value is injected as `__APP_VERSION__` via Vite's `define` — esbuild does a textual identifier substitution at build/dev-serve time.
4. `src/config.ts` reads `__APP_VERSION__` through a `typeof` guard, so a misconfigured build degrades to `"dev"` in the footer instead of crashing the SPA.

`package.json#version` is kept in lockstep cosmetically by the release workflow but is **not** consulted at runtime — never trust it for what's actually deployed.

**Containerised runs**: dev/staging/prod containers have no git binary and no `.git` mounted, so the host-side `ORKESTRA_VERSION` env var (or `--build-arg` on the production builder) is the only path. `orkestra.sh` auto-exports it from `git describe` on every invocation; CI passes `--build-arg ORKESTRA_VERSION=${{ github.ref_name }}` on tag pushes. See `docker/CLAUDE.md` for the env-var-flow table.

## Internationalization (i18n)

User-visible strings live in `src/locales/<lng>.json` and are rendered through `react-i18next`'s `t()`, never hard-coded in JSX. The app ships with `en` (default) and `it`; the user's choice is persisted on `user.language` and synced into `i18n` on auth state changes. Translation keys are **dot-separated and namespaced by feature**, mirroring the route tree where possible: `<module-or-area>.<page>.<element>`. Backend error codes translate via a flat `errors.<code>` namespace so handlers can stay UI-agnostic.

Examples:

- `nav.adminModules` — the sidebar entry for `/admin/modules`.
- `adminUsers.bulk.deleteConfirm` — the confirm copy on the bulk-delete modal in `/admin/users`.
- `errors.auth.email_in_use` — the user-facing message for the `auth.email_in_use` error code returned by `POST /v1/users`.

See [`../docs/archive/frontend-admin-i18n.md`](../docs/archive/frontend-admin-i18n.md) for the rollout plan and phase status (shipped; archived).

**Addon translations (ADR-0007).** Core strings live in the monolithic `translation` namespace (`src/locales/{en,it}.json`). A fork's addon **must not** edit those files — it ships its own bundles under `src/pages/<name>/locales/{en,it}.json`, registered at boot as a dedicated i18next namespace named after the module via the manifest's `injectI18n` seam (mirrors `injectApi`; the `useModuleI18nInjection` hook in `App.tsx` does the registration, ungated by auth/enabled-state). Consume with `useTranslation('<name>')` / `t('<name>:key')`. Type augmentation and the EN/IT parity test live in the addon's own files (parity primitives are shared via `src/locales/parityCheck.ts`). See [`src/modules/_template/README.md`](src/modules/_template/README.md) step 6.5 for the author recipe.

**Module config labels.** The `/admin/modules/<name>` settings form resolves every field and group label through `helpers/configLabel.ts`, so an addon translates its backend `configSchema` without the Go side shipping a redundant i18n field. Keys are derived from the backend's own stable `key`:

| Target            | Key                                       |
| ----------------- | ----------------------------------------- |
| Field label       | `<module>:config.fields.<fieldKey>.label` |
| Field description | `<module>:config.fields.<fieldKey>.desc`  |
| Group label       | `<module>:config.groups.<groupKey>.label` |
| Group description | `<module>:config.groups.<groupKey>.desc`  |

Resolution order per string (mirrors `helpers/navLabel.ts`): the addon's own namespace → the core bundle's `moduleConfig.<module>.{fields,groups}.…` → the literal `label` / `description` the backend sent. A key that is present but empty (`""`) counts as absent, so a blank translation can never blank a label, and an un-migrated addon keeps showing the backend's English rather than a raw key path. Core modules use the middle tier; an addon uses the first and **never** writes `moduleConfig.*` into the core bundle.

Five changes here break a fork's addon pages on sync: `ModuleConfigFields` now takes a **required `moduleName` prop** (it selects the namespace above); `ModuleConfigFields` **and** `detail/ModuleConfigPanel` also both take a **required `fieldNames` prop** (pass `controller.fieldNames` — see "Config keys vs form field names" below for why registering by the raw config key silently breaks saving); `src/pages/admin/modules/utils.ts` was deleted — its `bucketByGroup` is superseded by `buildGroupTree` and its `configCompleteness` moved verbatim in name, both now living in `src/pages/admin/modules/configModel.ts`, with `configCompleteness` counting only fields currently visible under their `dependsOn` conditions; `ModuleConfigSection` now takes a **required `controller` prop** (the shared `useModuleConfigController` instance, which `detail/index.tsx` owns — the component no longer creates its own) and **no longer takes `selectedEnvironment`**, since the environment is now the controller's business; and `detail/ModuleDashboardCards.tsx` was **renamed to `detail/ModuleOverviewPanel.tsx`** — a fork that touched that file gets a merge conflict on sync rather than a silent break, so resolve it by moving the edits onto the new name.

`ModuleConfigFields` was later migrated onto react-hook-form (`useModuleConfigForm` in `src/pages/admin/modules/useModuleConfigForm.ts`): its props are now `control`/`register` from a single module-wide form instance, not `configValues`/`secretValues`/`onConfigChange`/`onSecretChange`. `/admin/modules/<name>` is one react-hook-form form spanning every rail group — the rail only selects which slice is _rendered_, edits in an off-screen group stay live — with a single sticky `ModuleSaveBar` (`src/pages/admin/modules/detail/ModuleSaveBar.tsx`) reporting the aggregate unsaved-change count, a per-group breakdown, and a "Go to `<group>`" button per group carrying a validation error, however far the operator has navigated from it.

**Config keys vs form field names.** react-hook-form reads `.` in a field `name` as a **path separator** (and `[`/`]`/`,`/quotes as index syntax), so a backend key like `email.smtp.host` registered verbatim writes the operator's edit to `{email:{smtp:{host}}}` while every consumer reads the flat key — the edit then never reaches dirty tracking or the save diff, which made `/admin/modules/notification` (11 of 11 keys dotted) and `/admin/modules/tenant` (2 of 2) impossible to configure at all. `buildFieldNames(schema)` (`useModuleConfigForm.ts`) is the bijection that fixes it: schema key → a `\w`-only register name, unique by construction (`a.b` and `a_b` both sanitise to `a_b`, so collisions get a numeric suffix), derived once per form and threaded down as a **required `fieldNames` prop on both `ModuleConfigFields` and `detail/ModuleConfigPanel`** — a fork that renders either directly must pass `controller.fieldNames`. **The schema key stays the source of truth for everything else**: the `{ name, environment, config?, secrets? }` payload, `dependsOn`, `GroupNode.fieldKeys`, the i18n keys above, and `configCompleteness`. Only `register`/`Controller`/`formState.errors`/`dirtyFields`/`form.trigger` speak register names; `toSchemaValues` re-keys back at the boundary, which is why nothing in `configModel.ts` takes a mapping argument. **Never hand a raw `field.key` to a react-hook-form API**, and never let a register name reach the wire.

**Settings rail.** Declaring `ConfigGroups()` (SDK `module.ConfigGroup`: `Key`/`Label`/`Description`/`Icon`/`Parent`/`Order`) is what upgrades `/admin/modules/<name>` from one config card into a full master-detail page: a vertical rail navigating `Overview`, then the declared groups, then `Dependencies`/`Environments`, all synced to `?section=`. `Parent` nests groups to any depth — `buildGroupTree` (`configModel.ts`) builds the tree and `ModuleConfigRail` (`detail/ModuleConfigRail.tsx`) renders every level, indenting children under their parent.

Two named predicates in `configModel.ts` decide how much of that a module actually gets. `hasPageRail` — declared `configGroups` **and** at least 2 top-level (root) nodes in the resulting tree — decides whether the whole page promotes to the rail layout above. `hasCardRail` is looser (at least 2 top-level nodes **or** any declared `configGroups` at all, including the pre-existing legacy heuristic of ≥2 distinct `field.group` string labels with no declared metadata) and only decides whether `ModuleConfigSection`'s own config card shows a small internal tab rail. A module that clears `hasCardRail` but not `hasPageRail` — every module in the base except `auth`, which is the only one declaring a group tree today — keeps the previous stacked page (header, KPIs, environment switcher, one config card, dependencies): the card gets its own mini rail if the legacy check finds ≥2 buckets, otherwise one flat form with every field on screen. Declaring fewer than two top-level groups — including declaring none — is enough on its own to keep this path; that is a supported end state, not a gap.

A field with `Advanced: true` is pulled out of its group's main field list and rendered behind an "Advanced (N)" toggle (`detail/ModuleConfigPanel.tsx`, shared by both the card and the page layout). Both N and whether the toggle renders at all count only fields currently visible under their own `DependsOn` — a group whose sole advanced field is hidden by an unmet condition shows no toggle.

A declared group that owns **no fields of its own** — a pure container that exists only to nest its children — is a legitimate shape, and `ModuleConfigPanel` renders it as a table of contents of its children (each entry moves the rail there) rather than a bare heading. No module in the base declares one today: `auth`'s `oauth` nests the four providers but also owns 11 fields of its own (the eight provider toggles, the two signup toggles, and the auto-link toggle), so it takes the ordinary form branch. The container branch is there for addons that do, and is exercised by test fixtures. A _leaf_ group can land in the "heading over an empty body" state for a different reason: it owns fields, but every one of them is currently hidden by an unmet `DependsOn` (a provider's credentials before either of its enable toggles is on) — `ModuleConfigPanel` renders an honest empty state instead of silently producing nothing, since it has no children to link to. Because that state is a dead end otherwise, the empty state names the node's parent group and offers a button back to it (`adminModules.detail.rail.emptyUntilDependencyIn` + `rail.goToParent`, falling back to the parentless `rail.emptyUntilDependency`) — the options the fields wait on almost always live one level up. The sticky save bar (`detail/index.tsx`'s `showSaveBar`) is gated on the active node owning fields that are **currently visible**, not merely declared — `useModuleConfigController` exposes a module-wide `visibleKeys` set for exactly this check — so neither a container panel nor an all-hidden leaf ever carries a permanently-disabled Save.

Two behaviours the form model owes the operator, both in `useModuleConfigController`. **Stored values are validated once when the form is seeded**, not only when a field is touched: `mode: 'onChange'` never validates on mount, so a value the backend already holds in violation of its own `required`/`min`/`max`/`pattern` would render clean until someone typed in that exact field — the re-seed effect's `form.trigger()` is what keeps it red on arrival. And **switching environment asks first when edits are pending**: the switcher changes the query arg behind the whole form, which re-seeds it and discards every unsaved edit across every group. `useBlocker` cannot see that (nothing navigates), so `detail/index.tsx` holds the requested environment until the operator confirms.

The rail also carries an ambient completeness signal: `useModuleConfigController` exposes `unfilledByGroup` (node key → count of that node's **visible** required fields still empty, live against the form; a typed-but-unsaved secret counts as filled), and both rail callers feed it to `ModuleConfigRail`'s `statusFor`, which renders the amber "{{count}} to fill" badge (`adminModules.detail.rail.incomplete`). `Required` on a `ConfigField` composes with `DependsOn` as **required-when-visible** — the yup rule, this badge, and `configCompleteness` all skip fields hidden by an unmet condition, so marking a conditionally-revealed field required (e.g. `email.smtp.host`) never flags a module whose feature is switched off.

**An `enum` never silently rewrites a stored value.** A `<select>` that renders only its declared `options` drops any other stored value on the floor: the browser resolves the missing option to an empty selection, the operator sees a blank (or the first option) while the backend still holds the real value, and the next save writes the displayed one back. That happens whenever a field's domain changes under a value that already exists — a free-text field converted to `FieldEnum` (`notification`'s `email.provider` was exactly this), or an `Options` list that loses an entry. `ModuleConfigFields`' enum branch therefore renders the current value as its own extra `<option>` when it falls outside `options`, so the select shows the truth and a save that doesn't touch the field preserves it. The orphan option composes with the non-required empty placeholder rather than replacing it, and no orphan is added when the stored value is a declared option.

**Record lists (`recordList`).** A backend field of type `recordList` is not a leaf and does not register with react-hook-form — it has no value of its own. `useModuleConfigForm` builds the form machine (defaults, the yup object, the save diff, the dirty/error tallies) from an **expanded** schema instead: `expandSchema` (`src/pages/admin/modules/recordList/expandSchema.ts`) replaces each list with one concrete field per element per sub-field, keyed `<field>.<slug>.<sub>`, so the seven-type leaf renderer and the resolver never learn an eighth case. Layout keeps iterating the **declared** schema, which is where `RecordListField` renders — the two are different lists on purpose, and `fieldNames` is built from the union so `fieldNameOf` resolves either. An item's `dependsOn` names a _sibling sub-key_, so `expandElement` rewrites it onto the element it belongs to; leaving it alone hides every conditional field inside every element, silently.

Three behaviours the container owes the operator. The **slug is minted once**, from the name typed at creation, and never moves again — `mintSlug` mirrors `recordlist_slug.go` exactly, because the preview shown while typing must be the key the backend actually mints. A **removal is staged, not applied**: the card stays on screen, muted, with an Undo, and Save is where the destruction is armed — the confirmation names the elements, because deleting one drops its stored keys and its encrypted secrets for good. And a **membership change counts as dirty** even though it moves no form field, or a removal-only save would leave the save bar hidden and `useBlocker` silent.

Membership travels as explicit intent (`buildSavePayload` → `recordLists: [{field, create, remove}]`), never inferred from which keys the payload carries, and the `revision` rides along **only when something is being removed** — sending it on a pure add would turn another operator's compatible concurrent add into a needless 409. The three intents reach `ModuleConfigFields` through `RecordListContext` rather than as props: the renderer sits three layers below the controller that owns the state, and a missing provider is a legitimate state (the standalone renderer tests), so consumers read `undefined` and render read-only rather than throwing.

**Two different 409s reach the save, told apart by the body `code`.** `module.config_revision_stale` (`CONFIG_REVISION_STALE` in `store/api/moduleApi.ts`) means the config document itself lost its compare-and-swap; a codeless 409 means the record-list roster moved. Only the first is latched: `useModuleConfigController` sets `conflict`, which disables Save and puts a **Reload & review** button in `ModuleSaveBar`. Nothing is auto-retried — a retry would re-send a typed secret and re-decide the change against a state the operator never saw — so Save stays disabled until a reload has _succeeded_. `reloadAndReview` refetches the profile and re-applies only the fields react-hook-form marks dirty (an intentional clear to `''` included, unsent secrets kept in memory only), so untouched fields adopt the other writer's values instead of being reverted to the operator's stale baseline; pending record-list membership is discarded on every successful reload, including when the refetch returns the **same `configValues` reference** — RTK Query's structural sharing keeps it whenever the payload is deep-equal (an activation, another profile's write, or a secrets-only save to this one), and the re-seed effect is keyed on exactly that reference, so it never runs. The captured draft is cleared on that same identity test, not on `revision`: the two disagree precisely in the secrets-only case, and keying on `revision` left the draft alive to be re-applied on top of the operator's next successful save (a typed secret unconditionally). A failed refetch leaves the conflict latched. The reload also refreshes the module snapshot — `getModule.initiate(name, {forceRefetch: true})`, **awaited**, its subscription released in a `finally` — because an activation is one of the things that produces this very conflict, and the `live` badge / `activeEnvironment` / status would otherwise still show the world the operator was reviewing against; the latch lifts only once BOTH halves have landed, so Save can never re-enable over a stale badge, and a failed module refetch is an ordinary reload failure (draft cleared, `reloadFailed`, conflict kept). Only the `LIST` tag is still invalidated fire-and-forget — the list page is not what is being reviewed. The captured draft is keyed by the **schema key**, never the react-hook-form register name (`configDraft.ts`): `buildFieldNames` resolves sanitisation collisions with a numeric suffix assigned in schema _and roster_ order, so a reload that changes the roster can hand the same name to a different element's field — and a name-keyed draft would then re-apply a value, or a typed secret, to the wrong one, silently. That is why the re-seed effect is keyed on `configSource` — the baseline it actually seeds from — and no longer on `mod.configValues`, whose fresh reference would reset the form under the draft. A dirty field whose record-list element the other operator removed cannot be re-applied (there is no such field any more); those entries are counted and reported through `adminModules.detail.configCard.reloadDroppedEdits` instead of disappearing silently.

That only works because `updateModuleEnvironment` no longer invalidates its tags on a **failed** write. RTK Query runs `invalidatesTags` for a rejected mutation too (`isRejectedWithValue`), so every failed save used to refetch the environment behind the operator's back, and the controller's re-seed effect — which cannot tell that baseline from a deliberate one — reset the form and cleared the error banner explaining the failure. The unsaved edits, typed secrets included, vanished silently. A write that failed changed nothing on the server, so there is nothing to invalidate.

**Which environment is on screen lives in the URL**, as `?env=` beside `?section=` — it decides what every field on the page is bound to, so a link that omits it points at a different page than the sender was looking at. An unknown or stale value falls back to the module's active environment rather than binding the form to a profile that no longer exists. Two semantics are kept visually distinct because conflating them is a production-outage-shaped mistake: the button group answers _which profile you are viewing_, while the profile actually serving traffic carries a `live` badge that never moves with selection, and `detail/index.tsx` renders a persistent warning strip on **every** section whenever the two disagree (the page header that used to carry that signal scrolls away on a long panel). Promotion (`setAsActive`) is behind its own confirm modal — it used to fire immediately while merely _viewing_ another profile raised one.

## Conventions

- **Cookie auth** — every fetch goes through RTK Query's `baseApi` which sets `credentials: 'include'`. Never call `fetch` directly with custom auth headers.
- **No inline styles** for colors / spacing — use Bootstrap utility classes or SCSS variables.
- **Co-locate** sub-components, hooks, and helpers next to the page that uses them. Promote to shared only on second use.
- **Lazy-load route components** — every route in module manifests uses `React.lazy()` so each module ships its own chunk. All module routes are wrapped in `<ModuleGate>` to gate rendering based on backend module state.
- **Type imports** must come from `src/types/<module>.ts`, not be inlined in the slice.
- **Cache tags** must be declared in `baseApi.ts` before being used in a slice — TypeScript will reject otherwise.

## Don't

- Don't invent a parallel data-fetching layer (axios, custom fetch helpers). Every endpoint goes through an RTK Query slice that extends `baseApi`.
- Don't hardcode sidebar entries **for production features** — navigation comes from the backend. The dev-only Developer realm (see "How navigation works") is the single documented exception.
- `src/reference/` is an Orkestra-owned design-reference library, **not** read-only falcon material — copy from it, and add new Orkestra showcases under `src/reference/<subfolder>/` (register them in `src/routes/referenceRoutes.tsx` + `src/reference/navigation/referenceRoutes.ts`). Promote a pattern to `components/common/` once a production page consumes it; keep the live showcase in `reference/` (e.g. `reference/components/ui/StatCards.tsx` documents `components/common/StatCard` + `SectionCard`).
- Don't import from `src/modules/_template/` at runtime. It's a scaffold, not runtime code.
- Don't add new top-level directories under `src/`. The current layout is stable.

## Related

- [Backend module system](../backend/CLAUDE.md) — how to add the backend half of a new module
- [Backend core modules](../backend/internal/core/) — a fork's addon page folders match its backend `internal/addons/<name>/` module names
- [Module template](src/modules/_template/README.md) — the LLM scaffolding entry point
- [Module conventions](src/modules/README.md) — backend ↔ frontend mapping
