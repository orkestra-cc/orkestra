---
name: frontend-design
description: Use when creating or modifying ANY UI in frontend-admin/ (the Tier-1 operator console) — pages, components, tables, forms, modals, cards, KPI/stat rows, dashboards, charts, tabs. Also use when a request implies admin UI without naming it — "create a feature", "add a screen", "let operators/admins manage X", or full-stack work whose UI lands in frontend-admin. Invoke at the START of such a feature (while planning routes and pages), not when you reach the JSX. NOT for frontend-client/ — that SPA has its own stack and conventions.
---

# Frontend design — frontend-admin

Mandatory for all UI work in `frontend-admin/` (the Tier-1 operator console).

**Scope**: `frontend-admin/` only. The sibling `frontend-client/` SPA is out of scope — it has its own design system, primitives, and patterns; consult that project's own `CLAUDE.md` instead.

## Pre-flight contract (before any JSX)

Post this filled-in block in your response BEFORE writing any UI code:

```
Pre-flight:
- Production precedent: <src/pages/... page with the same shape — opened with Read this session>
- Reference read:       <src/reference/... file(s) from the cheat-sheet — opened with Read this session>
- Primitives:           <components/common/ components you will reuse>
```

Rules for filling it in:

1. Every file listed must be one you actually opened with Read **in this session** — not one you remember. Skill snippets and memory go stale; the files are current.
2. No production precedent in mind? `grep -rl <keyword> src/pages/` first. Most admin surfaces already exist in some form — **tenants, compliance, modules, users** are the four to check first.
3. Your case isn't in the cheat-sheet? `find src/reference -name "*.tsx" | xargs grep -l <keyword>` before writing anything.
4. If a line would be empty, stop and fill it before continuing.

**Precedence when sources disagree:** on **layout/visuals**, a recent production page beats this skill and the reference demos — the design system evolves faster than this file. The **stack mandates** (RTK Query, AdvanceTable, react-hook-form + yup, `t()` i18n, URL-synced tabs, path aliases) beat any page precedent — some shipped pages predate a mandate; an existing page that violates one is not a license to do the same.

## Reference cheat-sheet

| You're building…                | Read this reference file first                                                                  | Production primitive to use                                                                   |
| ------------------------------- | ----------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| **KPI / stat summary row**      | `src/reference/components/ui/StatCards.tsx`                                                     | `components/common/StatCard` + `SectionCard` — the ERP-style tile (4px accent border, faded 3× icon, big value; attention flag = diagonal corner ribbon via the `badge` prop). Live precedents: `src/pages/admin/tenants/index.tsx`, `src/pages/admin/compliance/` |
| **Any data table**              | `src/reference/components/tables/Tables.tsx`                                                    | `components/common/advance-table/AdvanceTable` + `useAdvanceTable` + `AdvanceTableProvider`    |
| **Form (validated)**            | `src/reference/components/forms/FormValidation.tsx`, `FormLayout.tsx`                           | `react-hook-form` + `yup` + React Bootstrap `Form.*`                                          |
| **Wizard / multi-step form**    | `src/reference/components/forms/WizardForms.tsx`                                                | `components/wizard/`                                                                          |
| **Select / multi-select**       | `src/reference/components/forms/Select.tsx`, `AdvanceSelect.tsx`                                | `components/common/MultiSelect`                                                               |
| **Date picker**                 | `src/reference/components/forms/DatePicker.tsx`                                                 | `components/common/CustomDateInput`                                                           |
| **File upload / dropzone**      | (search `OrkestraDropzone` in reference)                                                        | `components/common/OrkestraDropzone`                                                          |
| **Modal**                       | `src/reference/components/ui/Modals.tsx`                                                        | React Bootstrap `Modal` (no wrapper needed)                                                   |
| **Card / card with dropdown**   | `src/reference/components/ui/Cards.tsx`                                                         | `components/common/OrkestraCardHeader`, `OrkestraCardBody`, `CardDropdown`                    |
| **Page layout with header**     | `src/reference/pages/Starter.tsx`                                                               | `components/common/PageHeader`                                                                |
| **Dashboard widgets**           | Any file under `src/reference/dashboards/`                                                      | Existing widgets in `components/dashboards/`                                                  |
| **Chart**                       | `src/reference/charts/echarts/`                                                                 | `components/common/ReactEchart` (ECharts only — Chart.js/D3 were removed)                     |
| **Badge / status pill**         | (search `SubtleBadge` in reference)                                                             | `components/common/SubtleBadge`                                                               |
| **Icon button / action button** | `src/reference/components/tables/Tables.tsx` (shows both)                                       | `components/common/IconButton`, `components/common/ActionButton`                              |
| **Avatar / user identity**      | (search `Avatar` in reference)                                                                  | For user identities ALWAYS `components/common/UserAvatar` (handles `avatarSource` + initials fallback); raw `Avatar` only for non-user images |
| **Tabs**                        | See the `url-tabs` skill — tabs MUST sync with URL search params                                | `react-bootstrap/Tabs` + `useSearchParams` (never `useState`)                                 |
| **Full feature (kanban, chat, calendar, email, support-desk, social, events)** | The matching folder under `src/reference/app-examples/` | Copy and adapt — full implementations already exist                                           |

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

These snippets are orientation only — they drift as the codebase evolves. The pre-flight reads (reference file + production precedent) are the source of truth; the snippets exist so that even a rushed change lands on the right primitive.

### Data table (the most-missed pattern)

```typescript
import AdvanceTable from 'components/common/advance-table/AdvanceTable';
import AdvanceTableFooter from 'components/common/advance-table/AdvanceTableFooter';
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

Keys are dot-separated and namespaced by feature: `<module>.<page>.<element>`. Add every key to **both** `src/locales/en.json` and `it.json` — the EN/IT parity test fails otherwise.

Some shipped pages hard-code strings; they predate this rule. Do not copy that — this is a stack mandate, and page precedent does not override it.

## Adding a new module (full feature)

If the request is to add a new feature module (not just a page), use the canonical scaffold:

1. Read `frontend-admin/src/modules/_template/README.md`.
2. Copy `_template/api.ts`, `_template/types.ts`, `_template/pages/ExamplePage.tsx`.
3. Add tag types to `src/store/api/baseApi.ts`.
4. Create `src/modules/<name>.tsx` with routes wrapped in `<ModuleGate>` + `<ProtectedRoute>` + `<Suspense>`.
5. Register in `src/modules/index.ts`.
6. The backend module's `NavItems()` adds the sidebar entry — do not hardcode nav on the frontend.

## Rationalizations that have caused rejected work

| Thought | Reality |
| --- | --- |
| "The snippet in this skill is enough — no need to open the reference" | Snippets drift. The pre-flight block requires files opened this session. |
| "The shipped page skips `t()` / uses a raw table, so I can too" | Layout: page precedent wins. Stack mandates: this skill wins, always. |
| "I'll deal with the frontend patterns when I get to the JSX" | By then the API shapes and routes are designed around the wrong UI. Do the pre-flight while planning the feature. |
| "This page is too simple to need a precedent" | Simple pages are exactly where bespoke stat cards and raw tables sneak in. |

## DO NOT

- ❌ Use TanStack Query / React Query / axios — this project uses **RTK Query** exclusively for server state.
- ❌ Build a raw `<table>` for a production list — use `AdvanceTable`.
- ❌ Build a bespoke KPI/stat card — use `StatCard` (+ `SectionCard` for the section shell).
- ❌ Build a chart with Chart.js or D3 — they were removed; use ECharts via `ReactEchart`.
- ❌ Use CSS modules, styled-components, Tailwind, or inline color/spacing styles.
- ❌ Use generic fonts (Inter, Roboto, Arial). The theme provides the font.
- ❌ Hardcode user-visible strings — go through `t()` (i18n).
- ❌ Hardcode sidebar entries on the frontend for production features — `NavItems()` is on the backend.
- ❌ Use `useState` for active tab — sync with URL (`useSearchParams`). See `url-tabs` skill.
- ❌ Import from `src/modules/_template/` at runtime — it is a scaffold only.
- ❌ Use class components or `.js` (non-TS) files.
- ❌ Add `@/` prefix to imports or use long relative climbs — use bare path aliases.

## DO

- ✅ Fill in the pre-flight block (production precedent + reference + primitives) BEFORE writing code.
- ✅ Reuse from `components/common/` first; promote to common only on second use.
- ✅ Wrap form state with `react-hook-form` + `yup`.
- ✅ Wrap server state with an RTK Query slice extending `baseApi`.
- ✅ Lazy-load route components in module manifests (`React.lazy()`).
- ✅ Co-locate page-only sub-components next to the page.
- ✅ Treat `src/reference/` as the Orkestra-owned design-reference library: copy from it, and when you build a reusable primitive, put it in `components/common/` and add a live showcase under `src/reference/<subfolder>/` (register in `src/routes/referenceRoutes.tsx` + `src/reference/navigation/referenceRoutes.ts`), e.g. `reference/components/ui/StatCards.tsx` → `components/common/StatCard` + `SectionCard`.
- ✅ Read `frontend-admin/CLAUDE.md` ("Component reuse hierarchy") when in doubt — it is the source of truth over this skill.
