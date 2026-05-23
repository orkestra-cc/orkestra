---
name: frontend-design
description: Professional React frontend developer for the Orkestra Tier-1 operator console (frontend-admin/). Creates production-grade components following Orkestra design system patterns. Use PROACTIVELY whenever creating, modifying, or scaffolding any page, component, table, form, modal, card, or UI element inside frontend-admin/ — even when not explicitly asked. Trigger on any request that touches frontend-admin/src/pages/, frontend-admin/src/components/, or visual UI in frontend-admin. DO NOT use for frontend-client/ — that SPA has its own stack and conventions.
---

This skill creates professional React frontend components that follow Orkestra's established patterns. It is **mandatory** for any UI work in `frontend-admin/` (the Tier-1 operator console).

**Scope**: this skill applies **only** to `frontend-admin/`. The sibling `frontend-client/` SPA is out of scope — it has its own design system, component primitives, and patterns. If the request is about `frontend-client/`, stop and consult that project's own `CLAUDE.md` instead.

## STEP 0 — Mandatory pre-flight (do this before writing any JSX)

Before producing a single line of UI code, you MUST:

1. **Read `frontend-admin/CLAUDE.md`** — specifically the "Component reuse hierarchy" section. This is the source of truth, not this skill.
2. **Read the relevant reference file** based on what you're building (see "Reference cheat-sheet" below). Copy patterns from there — never reinvent.
3. **Confirm the production primitive exists** in `src/components/common/` (e.g. `AdvanceTable`, `PageHeader`, `IconButton`, `OrkestraComponentCard`). If it exists, you MUST use it.

If you skip Step 0 you will write code that diverges from the codebase and the user will reject the change. This has happened before — do not repeat it.

## Reference cheat-sheet (READ BEFORE BUILDING)

| You're building…                | Read this reference file first                                                                  | Production primitive to use                                                                   |
| ------------------------------- | ----------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| **Any data table**              | `src/reference/components/tables/Tables.tsx`                                                    | `components/common/advance-table/AdvanceTable` + `useAdvanceTable` + `AdvanceTableProvider`    |
| **Form (validated)**            | `src/reference/components/forms/FormValidation.tsx`, `FormLayout.tsx`                           | `react-hook-form` + `yup` + React Bootstrap `Form.*`                                          |
| **Wizard / multi-step form**    | `src/reference/components/forms/WizardForms.tsx`                                                | `components/wizard/`                                                                          |
| **Select / multi-select**       | `src/reference/components/forms/Select.tsx`, `AdvanceSelect.tsx`                                | `components/common/MultiSelect`                                                               |
| **Date picker**                 | `src/reference/components/forms/DatePicker.tsx`                                                 | `components/common/CustomDateInput`                                                           |
| **File upload / dropzone**      | (search `OrkestraDropzone` in reference)                                                        | `components/common/OrkestraDropzone`                                                          |
| **Modal**                       | `src/reference/components/ui/Modals.tsx`                                                        | React Bootstrap `Modal` (no wrapper needed)                                                   |
| **Card / card with dropdown**   | `src/reference/components/ui/Cards.tsx`                                                         | `components/common/OrkestraCardHeader`, `OrkestraCardBody`, `CardDropdown`                          |
| **Page layout with header**     | `src/reference/pages/Starter.tsx`                                                               | `components/common/PageHeader`                                                                |
| **Dashboard widgets**           | Any file under `src/reference/dashboards/`                                                      | Existing widgets in `components/dashboards/`                                                  |
| **Chart**                       | `src/reference/charts/echarts/`                                                                 | `components/common/ReactEchart` (ECharts only — Chart.js/D3 were removed)                     |
| **Badge / status pill**         | (search `SubtleBadge` in reference)                                                             | `components/common/SubtleBadge`                                                               |
| **Icon button / action button** | `src/reference/components/tables/Tables.tsx` (shows both)                                       | `components/common/IconButton`, `components/common/ActionButton`                              |
| **Avatar**                      | (search `Avatar` in reference)                                                                  | `components/common/Avatar`                                                                    |
| **Tabs**                        | See the `url-tabs` skill — tabs MUST sync with URL search params                                | `react-bootstrap/Tabs` + `useSearchParams` (never `useState`)                                 |
| **Full feature (kanban, chat, calendar, email, support-desk, social, events)** | The matching folder under `src/reference/app-examples/` | Copy and adapt — full implementations already exist                                           |

**If your case isn't in this table**, run `find src/reference -name "*.tsx" | xargs grep -l <keyword>` before writing anything.

## Technology stack (ENFORCED)

| Layer              | MUST use                                                          | MUST NOT use                                                       |
| ------------------ | ----------------------------------------------------------------- | ------------------------------------------------------------------ |
| Framework          | React 19 + TypeScript 5.9 (strict)                                | Class components, JS files                                         |
| UI kit             | React Bootstrap 2.10 + Bootstrap 5.3 + Orkestra SCSS              | MUI, Chakra, Tailwind, styled-components, CSS modules              |
| **Server state**   | **RTK Query** (slices in `src/store/api/`, extend `baseApi`)      | **TanStack Query / React Query, axios, raw `fetch` for app data**  |
| Client state       | Redux Toolkit slices, React `useState`, `AppProvider` context     | Zustand, Jotai, Recoil                                             |
| Forms              | `react-hook-form` + `yup` (`@hookform/resolvers/yup`)             | Formik, manual `useState` form handling                            |
| Tables             | TanStack Table v8 wrapped by `AdvanceTable`                       | Raw `<table>` for anything beyond static demos                     |
| Charts             | ECharts via `echarts-for-react` / `ReactEchart`                   | Chart.js, D3 (removed from the project)                            |
| Routing            | React Router 7.7                                                  | Hash routing, custom routers                                       |
| Tabs               | `useSearchParams` from react-router (see `url-tabs` skill)        | `useState` for active tab                                          |

Note: button variants are named `variant="falcon-primary"` etc. — that's the **Bootstrap variant string** the theme registers. The design system itself is **Orkestra-branded** (`OrkestraComponentCard`, `OrkestraDropzone`, `OrkestraLightBox`, ...).

## Path aliases (no `@/` prefix)

Imports use **bare aliases** declared in `tsconfig.json` + `vite.config.js`:

```typescript
import Avatar from 'components/common/Avatar';            // ✅
import PageHeader from 'components/common/PageHeader';     // ✅
import AdvanceTable from 'components/common/advance-table/AdvanceTable';

import Avatar from '@/components/common/Avatar';           // ❌ wrong — no @/
import Avatar from '../../../components/common/Avatar';    // ❌ wrong — no relative climbs
```

Available aliases: `App`, `components`, `pages`, `layouts`, `providers`, `hooks`, `helpers`, `data`, `assets`, `routes`, `store`, `config`, `reference`, `types`, `utils`, `widgets`, `features`, `demos`, `docs`, `reducers`, `test`.

## Canonical patterns

### Data table (the most-missed pattern)

```typescript
import AdvanceTable from 'components/common/advance-table/AdvanceTable';
import AdvanceTableFooter from 'components/common/advance-table/AdvanceTableFooter';
import AdvanceTablePagination from 'components/common/advance-table/AdvanceTablePagination';
import AdvanceTableSearchBox from 'components/common/advance-table/AdvanceTableSearchBox';
import useAdvanceTable from 'hooks/ui/useAdvanceTable';
import AdvanceTableProvider from 'providers/AdvanceTableProvider';
import { ColumnDef } from '@tanstack/react-table';

interface Row { id: string; name: string; email: string; }

const columns: ColumnDef<Row>[] = [
  { accessorKey: 'name', header: 'Name', meta: { headerProps: { className: 'text-900' } } },
  { accessorKey: 'email', header: 'Email' },
];

const MyTable: React.FC<{ rows: Row[] }> = ({ rows }) => {
  const table = useAdvanceTable({
    data: rows,
    columns,
    selection: true,
    sortable: true,
    pagination: true,
    perPage: 10,
  });

  return (
    <AdvanceTableProvider {...table}>
      <Row className="mb-3 g-2">
        <Col xs="auto"><AdvanceTableSearchBox /></Col>
      </Row>
      <AdvanceTable
        headerClassName="bg-200 text-nowrap align-middle"
        rowClassName="align-middle white-space-nowrap"
        tableProps={{ size: 'sm', striped: true, className: 'fs-10 mb-0 overflow-hidden' }}
      />
      <div className="mt-3">
        <AdvanceTableFooter rowsPerPageSelection rowInfo navButtons />
      </div>
    </AdvanceTableProvider>
  );
};
```

**Never** write raw `<Table>` markup for production lists. The single exception: a 3-row static info table inside a Card.

### Page with header

```typescript
import PageHeader from 'components/common/PageHeader';

const MyPage: React.FC = () => (
  <>
    <PageHeader title="Page Title" description="Optional description" className="mb-3" />
    <Row className="g-3">
      <Col lg={8}>
        <Card><Card.Body>Content</Card.Body></Card>
      </Col>
    </Row>
  </>
);
```

### Form with validation

```typescript
import { useForm } from 'react-hook-form';
import { yupResolver } from '@hookform/resolvers/yup';
import * as yup from 'yup';

const schema = yup.object({
  email: yup.string().email().required(),
  name: yup.string().required(),
});

type FormData = yup.InferType<typeof schema>;

const MyForm: React.FC = () => {
  const { register, handleSubmit, formState: { errors } } = useForm<FormData>({
    resolver: yupResolver(schema),
  });

  return (
    <Form onSubmit={handleSubmit((data) => { /* … */ })}>
      <Form.Group className="mb-3">
        <Form.Label>Email</Form.Label>
        <Form.Control type="email" isInvalid={!!errors.email} {...register('email')} />
        <Form.Control.Feedback type="invalid">{errors.email?.message}</Form.Control.Feedback>
      </Form.Group>
      <Button variant="falcon-primary" type="submit">Submit</Button>
    </Form>
  );
};
```

### Data fetching (RTK Query — NOT TanStack Query)

```typescript
// src/store/api/widgetsApi.ts
import { baseApi } from 'store/api/baseApi';
import type { Widget } from 'types/widget';

export const widgetsApi = baseApi.injectEndpoints({
  endpoints: (build) => ({
    listWidgets: build.query<Widget[], void>({
      query: () => '/v1/widgets',
      providesTags: ['Widget'],
    }),
  }),
});

export const { useListWidgetsQuery } = widgetsApi;
```

Then in the page:

```typescript
import { useListWidgetsQuery } from 'store/api/widgetsApi';

const WidgetsPage: React.FC = () => {
  const { data, isLoading, error } = useListWidgetsQuery();
  // …
};
```

New cache tag types must first be added to `baseApi.ts`'s `tagTypes` array.

### Theme / dark mode

```typescript
import { useAppContext } from 'providers/AppProvider';

const { config: { isDark, isRTL } } = useAppContext();
```

Every component must render correctly in both light and dark mode. Use `text-900`, `bg-body-tertiary`, `border` utilities — never hex codes.

### Internationalization

Strings shown to users **must** go through `react-i18next`'s `t()`. Never hard-code English/Italian copy in JSX.

```typescript
import { useTranslation } from 'react-i18next';

const { t } = useTranslation();
return <h1>{t('marketing.contacts.title')}</h1>;
```

Keys are dot-separated and namespaced by feature: `<module>.<page>.<element>`.

## Adding a new module (full feature)

If the request is to add a new feature module (not just a page), use the canonical scaffold:

1. Read `frontend-admin/src/modules/_template/README.md`.
2. Copy `_template/api.ts`, `_template/types.ts`, `_template/pages/ExamplePage.tsx`.
3. Add tag types to `src/store/api/baseApi.ts`.
4. Create `src/modules/<name>.tsx` with routes wrapped in `<ModuleGate>` + `<ProtectedRoute>` + `<Suspense>`.
5. Register in `src/modules/index.ts`.
6. The backend module's `NavItems()` adds the sidebar entry — do not hardcode nav on the frontend.

## DO NOT

- ❌ Use TanStack Query / React Query / axios — this project uses **RTK Query** exclusively for server state.
- ❌ Build a raw `<table>` for a production list — use `AdvanceTable`.
- ❌ Build a chart with Chart.js or D3 — they were removed; use ECharts via `ReactEchart`.
- ❌ Use CSS modules, styled-components, Tailwind, or inline color/spacing styles.
- ❌ Use generic fonts (Inter, Roboto, Arial). The theme provides the font.
- ❌ Hardcode user-visible strings — go through `t()` (i18n).
- ❌ Hardcode sidebar entries on the frontend for production features — `NavItems()` is on the backend.
- ❌ Use `useState` for active tab — sync with URL (`useSearchParams`). See `url-tabs` skill.
- ❌ Move or edit anything under `src/reference/` — it is read-only template material.
- ❌ Import from `src/modules/_template/` at runtime — it is a scaffold only.
- ❌ Use class components or `.js` (non-TS) files.
- ❌ Add `@/` prefix to imports or use long relative climbs — use bare path aliases.

## DO

- ✅ Read the relevant `src/reference/...` file BEFORE writing code.
- ✅ Reuse from `components/common/` first; promote to common only on second use.
- ✅ Wrap form state with `react-hook-form` + `yup`.
- ✅ Wrap server state with an RTK Query slice extending `baseApi`.
- ✅ Pass `credentials: 'include'`-equivalent — already handled by `baseApi`; do not bypass it.
- ✅ Lazy-load route components in module manifests (`React.lazy()`).
- ✅ Co-locate page-only sub-components next to the page.
- ✅ Run `npm run typecheck` and `npm run lint` mentally — strict mode catches the rest.
