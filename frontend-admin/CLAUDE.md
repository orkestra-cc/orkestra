# Frontend Admin — Operator Console (Tier-1)

_Path: `/frontend-admin`_  
_Parent: [../CLAUDE.md](../CLAUDE.md)_

[← Root](../CLAUDE.md) | [☰ Module Map](../CLAUDE.md#module-map) | [🚀 Quick Start](../CLAUDE.md#quick-start) | [Tier-2 client SPA](../frontend-client/CLAUDE.md)

React 19 + Vite 7 + TypeScript 5.9 operator console for Orkestra — the **Tier-1 admin dashboard** used by internal staff. Cookie-based auth with the Go backend (operator audience), dynamic navigation driven by `/v1/navigation`, per-module RTK Query slices, Orkestra design system + Bootstrap 5. Sibling to [`../frontend-client`](../frontend-client/CLAUDE.md), the Tier-2 customer-facing SPA — different audience, different cookie domain, different stack.

## Tech stack

| Layer       | Choice                                                                                                                       |
| ----------- | ---------------------------------------------------------------------------------------------------------------------------- |
| Framework   | React 19.1, React Router 7.7                                                                                                 |
| Build       | Vite 7 (dev server + production bundle)                                                                                      |
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
│   ├── pages/                     # Production pages, organized by backend module
│   │   ├── admin/                 # User management
│   │   ├── ai/                    # aimodels + rag + agents UI
│   │   ├── billing/               # Invoicing (customers, suppliers, invoices, dashboard, notifications)
│   │   ├── company/               # Business registry lookup
│   │   ├── graph/                 # Knowledge graph explorer
│   │   ├── operator/              # Operator profile
│   │   ├── sales/                 # Sales jobs, prospects, reports, settings, skills
│   │   └── user/                  # User settings
│   ├── modules/
│   │   ├── index.ts               # Module catalog — maps module names to manifests
│   │   ├── types.ts               # ModuleManifest interface
│   │   ├── useModuleApi.ts        # Hook to lazily inject API slices for enabled modules
│   │   ├── billing.tsx            # Billing module manifest (routes + API injection)
│   │   ├── company.tsx            # Company module manifest
│   │   ├── graph.tsx              # Graph module manifest
│   │   ├── aimodels.tsx           # AI Models module manifest
│   │   ├── rag.tsx                # RAG module manifest (API only, no routes)
│   │   ├── agents.tsx             # Agents module manifest
│   │   ├── sales.tsx              # Sales module manifest
│   │   ├── README.md              # Module conventions + backend ↔ frontend map
│   │   └── _template/             # Copy-paste scaffold for adding a new module
│   ├── components/
│   │   ├── common/                # 🎯 UI primitives (Avatar, UserAvatar, Card, Flex, IconButton, AdvanceTable, ...) — barrel exported
│   │   ├── authentication/        # Login forms, ProtectedRoute, OAuth callback handlers. SocialLoginForm renders buttons from the live backend list (`useGetOAuthProvidersQuery` → `GET /v1/auth/operator/providers`) so toggling a provider on `/admin/modules/auth` removes it from the login page within 30s — never hardcode the provider list here.
│   │   ├── dashboards/            # Reusable dashboard widgets
│   │   ├── navbar/                # Sidebar + top navigation
│   │   ├── wizard/                # Form wizard helpers
│   │   ├── errors/                # 404, 500 pages
│   │   └── notification/          # Toast and banner notifications
│   ├── reference/                 # 📚 Orkestra template library (READ-ONLY) — 7 example apps + 60+ samples
│   │   ├── app-examples/          # calendar, chat, email, events, kanban, social, support-desk
│   │   ├── components/            # UI showcase (forms, tables, navigation, media, etc.)
│   │   ├── charts/                # ECharts examples only (chartjs/d3js removed — unresolved imports)
│   │   ├── dashboards/            # 11 complete dashboard layouts
│   │   ├── pages/                 # Landing, FAQ, pricing, miscellaneous templates
│   │   └── utilities/             # Bootstrap utility-class examples
│   ├── hooks/                     # Custom hooks (useRoleBasedNavigation, useRAGStream, useSettings, useAuth*)
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
├── vitest.config.ts               # Vitest config — happy-dom env, react-router-dom alias
└── package.json
```

## Path aliases

The project uses **bare path aliases** (no `@/` prefix). They are declared in both `tsconfig.json` and `vite.config.js`:

```ts
import Avatar from 'components/common/Avatar'; // not '@/components/common/Avatar'
import { useRoleBasedNavigation } from 'hooks/useRoleBasedNavigation';
import BillingDashboard from 'pages/billing/dashboard';
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

**Dev-only exception — Developer realm.** When `import.meta.env.DEV` is true (or `VITE_ENABLE_REFERENCE` is set), `NavbarVertical` appends a hardcoded `Developer` realm from `src/reference/navigation/referenceRoutes.ts` (`developerRealm` export) pointing at the dev-only `/reference/*` routes registered by `src/routes/referenceRoutes.tsx`. The gate matches the one on the routes themselves, so nav and routes stay in lockstep. This is the **only** place sidebar entries are hardcoded in the frontend — do not extend the pattern to production features.

**Operator reorder.** `/admin/modules/navigation` (admin-only) renders the full unfiltered tree from `GET /v1/admin/navigation` and lets operators drag-to-reorder items within a parent, sections within a realm, and realm cards themselves. Persisted overrides are PATCHed back per-parent; mutations invalidate both the `NavigationAdmin` and the public `Navigation` RTK Query tags so the live sidebar reflects the new order without a page refresh. See [backend navigation docs](../backend/internal/core/navigation/CLAUDE.md) for the override semantics + self-heal behaviour.

## How data fetching works

All server state goes through **RTK Query**, not React Query / TanStack Query. Each backend module gets its own slice in `src/store/api/`:

```
src/store/api/
├── baseApi.ts          # createApi() with createBaseQuery + global tagTypes
├── authApi.ts          # core: auth endpoints
├── userApi.ts          # core: user endpoints
├── navigationApi.ts    # core: /v1/navigation
├── navigationAdminApi.ts # admin: /v1/admin/navigation tree + ordering overrides
├── billingApi.ts       # addon
├── companyApi.ts       # addon
├── salesApi.ts         # addon
├── ragApi.ts           # addon
├── agentsApi.ts        # addon
├── aiModelsApi.ts      # addon
├── graphApi.ts         # addon
├── documentsApi.ts     # addon
├── moduleApi.ts        # admin: /v1/admin/modules
├── observabilityApi.ts # admin: /v1/admin/observability/log-levels (ADR-0005 Phase F)
├── personalAgentApi.ts
├── managementApi.ts
├── communicationsApi.ts
└── dashboardApi.ts
```

All slices extend `baseApi` via `injectEndpoints`. To add a new tag type, declare it in `baseApi.ts`'s `tagTypes` array. Auth uses **cookies + Bearer token** — `credentials: 'include'` is set in the base query, and the access token from the auth slice is added to the `Authorization` header when present.

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
6. **Backend declares the sidebar entry** via its addon's `NavItems()` method. The link appears in the sidebar automatically once the user has the required role and the backend module is enabled.

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

## Testing

Vitest + React Testing Library + happy-dom + MSW. The infra lives in `src/test/`:

| File                   | Purpose                                                                                                                                                                 |
| ---------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `src/test/setup.ts`    | Vitest global setup — jest-dom matchers, MSW lifecycle (`onUnhandledRequest: 'error'` so missing stubs fail loud), `resetCapturedRequests()` between tests              |
| `src/test/server.ts`   | Single shared `setupServer(...defaultHandlers)` reused by every test file                                                                                               |
| `src/test/handlers.ts` | Default MSW handlers + per-endpoint request capture (`capturedRequests.billingStatsParams` etc.) for tests that need to assert outbound params                          |
| `src/test/render.tsx`  | `renderWithProviders(ui, { preloadedState, store, routerEntries })` — wraps in a fresh non-persisted Redux store + `MemoryRouter`. Returns `{ store, ...renderResult }` |

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

- `vitest.config.ts` aliases `react-router-dom` → `react-router`. Without this, v7's dual-package layout creates separate `Router` context instances at test time and components mixing the two imports lose their context.
- `environment: 'happy-dom'` (not jsdom) — jsdom + MSW v2 + Node fetch trip over `RequestInit: Expected signal to be an instance of AbortSignal`.

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
- `billing.invoices.received.import.errorImporting` — the toast shown when the SDI XML import fails on the received-invoices page.
- `errors.auth.email_in_use` — the user-facing message for the `auth.email_in_use` error code returned by `POST /v1/users`.

See [`../docs/plans/frontend-admin-i18n.md`](../docs/plans/frontend-admin-i18n.md) for the rollout plan and phase status.

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
- Don't move things out of `src/reference/` — it's a read-only template library. Copy from it.
- Don't import from `src/modules/_template/` at runtime. It's a scaffold, not runtime code.
- Don't add new top-level directories under `src/`. The current layout is stable.

## Related

- [Backend module system](../backend/CLAUDE.md) — how to add the backend half of a new module
- [Backend addons](../backend/internal/addons/) — match the names of frontend module folders
- [Module template](src/modules/_template/README.md) — the LLM scaffolding entry point
- [Module conventions](src/modules/README.md) — backend ↔ frontend mapping
