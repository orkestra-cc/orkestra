# Module Template

This directory is a **scaffold for adding a new feature module** to the Orkestra frontend. It is meant to be **copied**, not imported. Replace `_template` with your module name and update the placeholder strings.

It is the canonical example for an LLM agent (or a human) asked to "add a new module" to the frontend. Read this file before making any other changes.

## When to use this template

Use it when:

- A new backend addon module has been created in `backend/internal/addons/<name>/`, and you want to expose its routes in the React app.
- You are scaffolding the React side of a feature whose backend isn't built yet — this is fine, the API slice will return errors until the backend exists.

Do **not** use it for:

- Pure UI experiments — those go in `src/reference/` (the Orkestra template library).
- Cross-cutting components used by many modules — those go in `src/components/common/` and are exported via the barrel.

## How the existing frontend is wired

Before scaffolding a new module, understand the conventions already in place:

| Concern                                      | Where it lives                                                                                                                                   | Example                                          |
| -------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------ |
| Page components for module `<name>`          | `src/pages/<name>/<feature>/index.tsx`                                                                                                           | `src/pages/billing/dashboard/index.tsx`          |
| Sub-page components co-located with the page | Same directory as the page                                                                                                                       | `src/pages/billing/dashboard/RecentInvoices.tsx` |
| RTK Query slice for module `<name>`          | `src/store/api/<name>Api.ts` (single file per module)                                                                                            | `src/store/api/billingApi.ts`                    |
| Cache tag types                              | Added to the `tagTypes` array in `src/store/api/baseApi.ts`                                                                                      | `'Invoice', 'Customer', 'Supplier'`              |
| Type definitions                             | `src/types/<name>.ts`                                                                                                                            | `src/types/company.ts`                           |
| Module manifest                              | `src/modules/<name>.tsx` — declares routes + lazy API injection                                                                                  | `src/modules/billing.tsx`                        |
| Module catalog                               | Manifest registered in `src/modules/index.ts`                                                                                                    | `billing: billingManifest`                       |
| Backend nav entry                            | `NavItems()` method in the backend module's `module.go` — the React app reads the merged list from `/v1/navigation` via `useRoleBasedNavigation` | `backend/internal/addons/billing/module.go`      |

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

If you need a richer page (calendar, kanban, chat, email client), look at `src/reference/app-examples/` first — they are full Orkestra template implementations you can copy and adapt.

### 6. Create a module manifest

Create `frontend/src/modules/widgets.tsx`:

```tsx
import { Suspense, lazy } from 'react';
import type { ModuleManifest } from './types';
import ProtectedRoute from 'components/authentication/ProtectedRoute';
import ModuleGate from 'components/common/ModuleGate';
import OrkestraLoader from 'components/common/OrkestraLoader';

const WidgetList = lazy(() => import('pages/widgets/list'));
const WidgetDetail = lazy(() => import('pages/widgets/detail'));

export const widgetsManifest: ModuleManifest = {
  name: 'widgets',
  routes: () => [
    {
      path: 'widgets',
      element: (
        <ModuleGate module="widgets">
          <ProtectedRoute
            requiredPermissions={[
              ['super_admin', 'administrator', 'developer', 'operator']
            ]}
          >
            <Suspense key="widget-list" fallback={<OrkestraLoader />}>
              <WidgetList />
            </Suspense>
          </ProtectedRoute>
        </ModuleGate>
      )
    },
    {
      path: 'widgets/:id',
      element: (
        <ModuleGate module="widgets">
          <ProtectedRoute
            requiredPermissions={[
              ['super_admin', 'administrator', 'developer', 'operator']
            ]}
          >
            <Suspense key="widget-detail" fallback={<OrkestraLoader />}>
              <WidgetDetail />
            </Suspense>
          </ProtectedRoute>
        </ModuleGate>
      )
    }
  ],
  injectApi: () => import('store/api/widgetsApi'),
  injectI18n: async () => ({
    en: (await import('pages/widgets/locales/en.json')).default,
    it: (await import('pages/widgets/locales/it.json')).default
  })
};
```

Then register it in `frontend/src/modules/index.ts`:

```ts
import { widgetsManifest } from './widgets';

export const moduleCatalog: Record<string, ModuleManifest> = {
  // ...existing modules...
  widgets: widgetsManifest
};
```

### 6.5. Add translations (ADR-0007)

An addon **never** edits the core `src/locales/{en,it}.json`, `src/i18n-types.d.ts`, or `src/locales/parity.test.ts`. It ships its own translations as a dedicated i18next **namespace named after the module** (here `widgets`). Concretely:

1. **Bundle files** — create `src/pages/widgets/locales/en.json` and `it.json` (copy the scaffold's `_template/locales/`). Ship **every** supported language (`en` + `it` today) so a runtime `changeLanguage` finds the namespace already registered.
2. **Register them** — add the `injectI18n` field to the manifest (shown in step 6). A boot hook (`useModuleI18nInjection`) registers each catalogued module's bundle under its namespace, ungated by auth/enabled-state.
3. **Type augmentation** — copy `_template/i18n.d.ts` to `src/pages/widgets/i18n.d.ts`, replacing `widgets` with your module name. This makes `t('widgets:list.title')` typed without touching the core types.
4. **Consume** — bind the namespace in your pages:

   ```tsx
   const { t } = useTranslation('widgets');
   return <h1>{t('list.title')}</h1>; // or t('widgets:list.title')
   ```

5. **Parity test (recommended)** — copy `_template/parity.example.ts` to `src/pages/widgets/locales/parity.test.ts`. It reuses `locales/parityCheck.ts` to guard EN/IT parity for your namespace only.

Rules: the namespace equals the manifest `name` (so addons never collide); never write keys into the core `translation` namespace; adding a brand-new **language** (e.g. `fr`) is a core change to `src/i18n.ts`, not an addon operation.

### 7. Verify

Run `npm run typecheck` and `npm run build` from `frontend/`. Boot the backend with the widgets module enabled (e.g. `MODULES=widgets` in your env file). Log in, and the "Widgets" entry should appear in the sidebar automatically because the navigation comes from the backend.

## Files in this scaffold

| File                         | Purpose                                                                                                                 |
| ---------------------------- | ----------------------------------------------------------------------------------------------------------------------- |
| `api.ts`                     | Example RTK Query slice extending `baseApi` — copy to `src/store/api/<name>Api.ts`                                      |
| `locales/{en,it}.json`       | Example translation bundles for the addon's i18next namespace — copy to `src/pages/<name>/locales/` (ADR-0007)          |
| `i18n.d.ts`                  | Example type augmentation for the namespace — copy to `src/pages/<name>/i18n.d.ts`, rename `widgets`                    |
| `parity.example.ts`          | Example EN/IT parity test — copy to `src/pages/<name>/locales/parity.test.ts`                                           |
| `pages/ExamplePage.tsx`      | Example page component using `react-bootstrap` and `components/common` — copy to `src/pages/<name>/<feature>/index.tsx` |
| `components/ExampleCard.tsx` | Example sub-component — co-locate in the page directory after copying                                                   |
| `routes.example.tsx`         | Example lazy-route definitions — pattern for the manifest file                                                          |
| `types.ts`                   | Example shared types — copy to `src/types/<name>.ts`                                                                    |
| `README.md`                  | This file                                                                                                               |

Nothing in `_template` is imported by the running app. Vite ignores files that no `import` statement references, so this directory has zero runtime cost.
