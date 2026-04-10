# Module Template

This directory is a **scaffold for adding a new feature module** to the Orkestra frontend. It is meant to be **copied**, not imported. Replace `_template` with your module name and update the placeholder strings.

It is the canonical example for an LLM agent (or a human) asked to "add a new module" to the frontend. Read this file before making any other changes.

## When to use this template

Use it when:

- A new backend addon module has been created in `backend/internal/addons/<name>/`, and you want to expose its routes in the React app.
- You are scaffolding the React side of a feature whose backend isn't built yet — this is fine, the API slice will return errors until the backend exists.

Do **not** use it for:

- Pure UI experiments — those go in `src/reference/` (the Falcon template library).
- Cross-cutting components used by many modules — those go in `src/components/common/` and are exported via the barrel.

## How the existing frontend is wired

Before scaffolding a new module, understand the conventions already in place:

| Concern | Where it lives | Example |
|---|---|---|
| Page components for module `<name>` | `src/pages/<name>/<feature>/index.tsx` | `src/pages/billing/dashboard/index.tsx` |
| Sub-page components co-located with the page | Same directory as the page | `src/pages/billing/dashboard/RecentInvoices.tsx` |
| RTK Query slice for module `<name>` | `src/store/api/<name>Api.ts` (single file per module) | `src/store/api/billingApi.ts` |
| Cache tag types | Added to the `tagTypes` array in `src/store/api/baseApi.ts` | `'Invoice', 'Customer', 'Supplier'` |
| Type definitions | `src/types/<name>.ts` | `src/types/company.ts` |
| Lazy-loaded routes | `lazy(() => import('pages/<name>/<feature>'))` registered in `src/routes/index.tsx` | line 205: `() => import('pages/billing/dashboard')` |
| Backend nav entry | `NavItems()` method in the backend module's `module.go` — the React app reads the merged list from `/v1/navigation` via `useRoleBasedNavigation` | `backend/internal/addons/billing/module.go` |

The frontend does **not** define its own navigation. It renders whatever the backend reports. So the link in the sidebar appears the moment the backend module declares a `NavItem` and the user has the required role.

## Step-by-step: scaffolding a new module called `widgets`

The goal is to add a "Widgets" module with a list page and a detail page.

### 1. Backend prerequisites

Create the backend addon (see `backend/CLAUDE.md` for details). The backend module's `NavItems()` should declare the menu entries that will appear in the sidebar:

```go
func (m *WidgetsModule) NavItems() []module.NavItemSpec {
    return []module.NavItemSpec{
        {Group: "Operations", Name: "Widgets", Icon: "cube", Path: "/widgets", MinRole: "operator", Active: true},
    }
}
```

### 2. Add cache tags to `baseApi.ts`

Open `frontend/src/store/api/baseApi.ts` and add your tag types to the `tagTypes` array:

```ts
tagTypes: [
  // ...existing tags...
  'Widget',
  'WidgetStats',
],
```

### 3. Create the API slice

Copy `_template/api.ts` to `frontend/src/store/api/widgetsApi.ts` and rename the symbols. The slice extends `baseApi` via `injectEndpoints`, which is the convention used by every other module slice (`companyApi.ts`, `billingApi.ts`, etc.).

### 4. Create the type definitions

Create `frontend/src/types/widgets.ts` with the request/response shapes returned by the backend handlers.

### 5. Create the pages

Create `frontend/src/pages/widgets/list/index.tsx` and `frontend/src/pages/widgets/detail/index.tsx`. Use the components in `src/components/common/` (Avatar, Card, AdvanceTable, Flex, IconButton, PageHeader, etc.) as building blocks. Use `react-bootstrap` primitives for layout. Co-locate any sub-components in the same directory.

If you need a richer page (calendar, kanban, chat, email client), look at `src/reference/app-examples/` first — they are full Falcon template implementations you can copy and adapt.

### 6. Register the routes

In `frontend/src/routes/index.tsx`, add lazy imports near the existing module imports:

```ts
const WidgetList = lazy(() => import('pages/widgets/list'));
const WidgetDetail = lazy(() => import('pages/widgets/detail'));
```

Then add `RouteObject` entries inside the protected `MainLayout` children:

```ts
{ path: '/widgets', element: <WidgetList /> },
{ path: '/widgets/:id', element: <WidgetDetail /> },
```

### 7. Verify

Run `npm run typecheck` and `npm run build` from `frontend/`. Boot the backend with the widgets module enabled (e.g. `MODULES=widgets` in your env file). Log in, and the "Widgets" entry should appear in the sidebar automatically because the navigation comes from the backend.

## Files in this scaffold

| File | Purpose |
|---|---|
| `api.ts` | Example RTK Query slice extending `baseApi` — copy to `src/store/api/<name>Api.ts` |
| `pages/ExamplePage.tsx` | Example page component using `react-bootstrap` and `components/common` — copy to `src/pages/<name>/<feature>/index.tsx` |
| `components/ExampleCard.tsx` | Example sub-component — co-locate in the page directory after copying |
| `routes.example.tsx` | Example lazy-route definitions — pattern to add to `src/routes/index.tsx` |
| `types.ts` | Example shared types — copy to `src/types/<name>.ts` |
| `README.md` | This file |

Nothing in `_template` is imported by the running app. Vite ignores files that no `import` statement references, so this directory has zero runtime cost.
