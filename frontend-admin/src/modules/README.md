# `src/modules/` — Module Templates and Conventions

This directory holds **scaffolds and conventions** for adding new feature modules to the Orkestra frontend. It does **not** contain runtime code that the app imports.

## Why this directory exists

The actual feature code lives in three places under `src/`:

- `src/pages/<name>/` — page components, organized by backend module name (`billing`, `sales`, `company`, ...)
- `src/store/api/<name>Api.ts` — RTK Query slice for the module
- `src/types/<name>.ts` — shared type definitions

That layout is **already aligned** with the backend addon structure in `backend/internal/addons/<name>/`. Adding a new module means creating those three things plus a route registration in `src/routes/index.tsx`.

`src/modules/_template/` is a **copy-paste scaffold** that demonstrates the full pattern in one place, so an LLM agent or a new contributor can read one directory and understand the whole convention without spelunking through five different folders.

## Backend ↔ frontend module map

The frontend folder names match the backend addon names. When the backend ships a module, the frontend directory of the same name is where its UI lives.

| Backend addon (`backend/internal/addons/`) | Frontend pages (`frontend/src/pages/`) | API slice (`frontend/src/store/api/`) |
|---|---|---|
| `billing` | `billing/{customers,suppliers,invoices,dashboard,notifications}` | `billingApi.ts` |
| `documents` | `(used internally by billing)` | `documentsApi.ts` |
| `company` | `company/{lookup,search}` | `companyApi.ts` |
| `graph` | `graph/` | `graphApi.ts` |
| `aimodels` | `ai/` | `aiModelsApi.ts` |
| `rag` | `ai/` | `ragApi.ts` |
| `agents` | `ai/` | `agentsApi.ts` |
| `sales` | `sales/{jobs,prospect,reports,settings,skills}` | `salesApi.ts` |
| core: `auth` | `(handled by AuthProvider + components/authentication/)` | `authApi.ts` |
| core: `user` | `user/`, `admin/` | `userApi.ts`, `managementApi.ts` |
| core: `navigation` | `(handled by useRoleBasedNavigation hook)` | `navigationApi.ts` |

## Where to look for what

When asked to build something new, consult these directories in order:

1. **`src/modules/_template/`** — first stop for "how do I add a module?" Read its README before doing anything else.
2. **`src/components/common/`** — UI primitives (Avatar, Card, Flex, IconButton, PageHeader, AdvanceTable, etc.). Use these before reaching for raw Bootstrap.
3. **`src/reference/`** — Orkestra template library with full example apps (calendar, chat, email, kanban, social, support-desk) and 60+ component samples. When asked for a calendar or kanban, copy from here rather than reinventing.
4. **`src/pages/<existing-module>/`** — for "build a page like X" requests, find an existing page in the same module category and follow its conventions.

## Navigation is backend-driven

The sidebar menu is **not** defined in the frontend. The React app fetches it from `/v1/navigation` (via the `navigationApi` RTK Query slice and the `useRoleBasedNavigation` hook). Each backend module declares its own `NavItems()` in its `module.go`, and the navigation core module aggregates them filtered by the user's role.

This means: **to add a sidebar entry for a new page, edit the backend module — not the frontend.** The frontend only needs to register the route component in `src/routes/index.tsx` so the path resolves when the user clicks the link.
