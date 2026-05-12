# Frontend Admin — Operator Console (Tier-1)

*Path: `/frontend-admin`*  
*Parent: [../CLAUDE.md](../CLAUDE.md)*

[← Root](../CLAUDE.md) | [☰ Module Map](../CLAUDE.md#module-map) | [🚀 Quick Start](../CLAUDE.md#quick-start) | [Tier-2 client SPA](../frontend-client/CLAUDE.md)

React 19 + Vite 7 + TypeScript 5.9 operator console for Orkestra — the **Tier-1 admin dashboard** used by internal staff. Cookie-based auth with the Go backend (operator audience), dynamic navigation driven by `/v1/navigation`, per-module RTK Query slices, Falcon design system + Bootstrap 5. Sibling to [`../frontend-client`](../frontend-client/CLAUDE.md), the Tier-2 customer-facing SPA — different audience, different cookie domain, different stack.

## Tech stack

| Layer | Choice |
|---|---|
| Framework | React 19.1, React Router 7.7 |
| Build | Vite 7 (dev server + production bundle) |
| Language | TypeScript 5.9 strict mode |
| State | Redux Toolkit 2.9 + RTK Query (server state lives in RTK Query, not React Query) |
| UI kit | React Bootstrap 2.10 + Bootstrap 5.3 + Falcon SCSS theme |
| Forms | React Hook Form + Yup |
| Charts | ECharts (lazy-loaded chunks). Chart.js + D3 reference samples were removed — use `echarts-for-react` for any new chart work. |
| Calendar | FullCalendar |
| Maps | Google Maps + Leaflet |
| Tables | TanStack Table v8 |
| Drag & Drop | dnd-kit |
| Auth | Cookie sessions + Bearer access tokens (RS256 JWT issued by backend) |

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
│   │   ├── referenceRoutes.tsx    # Falcon template routes (dev-only, gated by import.meta.env.DEV)
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
│   │   ├── common/                # 🎯 UI primitives (Avatar, Card, Flex, IconButton, AdvanceTable, ...) — barrel exported
│   │   ├── authentication/        # Login forms, ProtectedRoute, OAuth callback handlers
│   │   ├── dashboards/            # Reusable dashboard widgets
│   │   ├── navbar/                # Sidebar + top navigation
│   │   ├── wizard/                # Form wizard helpers
│   │   ├── errors/                # 404, 500 pages
│   │   └── notification/          # Toast and banner notifications
│   ├── reference/                 # 📚 Falcon template library (READ-ONLY) — 7 example apps + 60+ samples
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
import Avatar from 'components/common/Avatar';     // not '@/components/common/Avatar'
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

## How data fetching works

All server state goes through **RTK Query**, not React Query / TanStack Query. Each backend module gets its own slice in `src/store/api/`:

```
src/store/api/
├── baseApi.ts          # createApi() with createBaseQuery + global tagTypes
├── authApi.ts          # core: auth endpoints
├── userApi.ts          # core: user endpoints
├── navigationApi.ts    # core: /v1/navigation
├── billingApi.ts       # addon
├── companyApi.ts       # addon
├── salesApi.ts         # addon
├── ragApi.ts           # addon
├── agentsApi.ts        # addon
├── aiModelsApi.ts      # addon
├── graphApi.ts         # addon
├── documentsApi.ts     # addon
├── moduleApi.ts        # admin: /v1/admin/modules
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

1. **`src/reference/app-examples/`** — full Falcon implementations of common apps (calendar, chat, email, kanban, social, support-desk, events). Copy and adapt — don't reinvent.
2. **`src/reference/components/`** — 60+ Falcon component samples (forms, tables, navigation, media, charts).
3. **`src/components/common/`** — UI primitives that the app's pages already use (Avatar, Card, Flex, IconButton, PageHeader, AdvanceTable, FalconDropzone, ...).
4. **`src/components/dashboards/`** — reusable dashboard widgets (WeeklySales, ActiveUsers, ...).
5. **`react-bootstrap`** — raw primitives for layout (Row, Col, Card, Button, Form).

Only build a new component if none of the above fits. New components used by exactly one page live next to that page (`src/pages/<module>/<feature>/MyHelper.tsx`). Promote to `components/common/` only when a second page needs it.

## State management

| Concern | Where it lives |
|---|---|
| Server state (cached responses) | RTK Query (`src/store/api/`) |
| Auth user + tokens | Redux slice (`src/store/slices/authSlice.ts`) |
| Kanban board state | Redux slice (`src/store/slices/kanbanSlice.ts`) |
| Theme, navbar config, RTL | `AppProvider` context |
| Form local state | React Hook Form |
| Component local state | `useState` |

Persisted state is opt-in via `redux-persist` — only user preferences are persisted, never tokens.

## Build & dev

```bash
npm run dev               # Vite dev server (port 5173 inside container, mapped to host)
npm run dev:staging       # Dev with staging mode flags
npm run build             # tsc + vite build (production)
npm run build:staging     # Staging build
npm run preview           # Serve built bundle locally
npm run typecheck         # tsc --noEmit (CI-safe)
npm run test              # Vitest single-pass run
npm run test:watch        # Vitest watch mode
```

The `tsc` step in `build` enforces strict mode — TypeScript errors fail the build.

## Testing

Vitest + React Testing Library + happy-dom + MSW. The infra lives in `src/test/`:

| File | Purpose |
|---|---|
| `src/test/setup.ts` | Vitest global setup — jest-dom matchers, MSW lifecycle (`onUnhandledRequest: 'error'` so missing stubs fail loud), `resetCapturedRequests()` between tests |
| `src/test/server.ts` | Single shared `setupServer(...defaultHandlers)` reused by every test file |
| `src/test/handlers.ts` | Default MSW handlers + per-endpoint request capture (`capturedRequests.billingStatsParams` etc.) for tests that need to assert outbound params |
| `src/test/render.tsx` | `renderWithProviders(ui, { preloadedState, store, routerEntries })` — wraps in a fresh non-persisted Redux store + `MemoryRouter`. Returns `{ store, ...renderResult }` |

**Default pattern**: real component, real Redux store, real RTK Query, MSW for HTTP. Mock hooks (`vi.mock`) only when testing branching logic of a hook's *consumer* (e.g. `ProtectedRoute` mocking `useAuth`), not when testing data flow.

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
