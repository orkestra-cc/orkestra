# Developer nav section — exposing Falcon demos in the sidebar

**Status:** ✅ Shipped 2026-05-13 (commit `7d023df feat(frontend-admin): add Developer nav realm for Falcon demos`). Legacy nav exports retargeted at `paths.ref*` paths in `0ed1e7c` (2026-05-14).
**Owner:** Salvatore
**Drafted:** 2026-05-12
**Scope:** `frontend-admin/` (Tier-1 operator console). No backend changes.

---

## Goal

Add a collapsible **Developer** group to the operator sidebar that exposes the ~250 Falcon demo pages that already live under `src/reference/` but aren't currently reachable from the menu. Mirror Falcon's original sidebar layout (Dashboards / App Examples / Pages / Components / Utilities / Documentation). The group is **dev-only** — visible when the reference routes themselves are loaded.

## Why this is the right shape

Three constraints decided the approach:

1. **Reference routes are dev-gated.** `src/routes/referenceRoutes.tsx:221,1202` skip registration when `import.meta.env.PROD && !import.meta.env.VITE_ENABLE_REFERENCE`. A backend nav contribution can't gate on the *frontend bundle's* build flags, so it would leak prod links that 404.
2. **The Falcon demo pages have no backend module.** They're a template library, not a feature. Hand-rolling a backend `dev-nav` module to declare nav items for pages the backend doesn't own is the wrong tool.
3. **The data we need already exists.** `src/reference/navigation/referenceRoutes.ts` is a leftover Falcon nav config with `dashboardRoutes`, `appRoutes`, `pagesRoutes`, `modulesRoutes` (= Components), `documentationRoutes`, etc. — already shaped correctly, already imported by `NavbarTopDropDownMenus.tsx`. It just has the wrong paths (production paths instead of `paths.ref*`) and stale `roles: ['developer']` gates.

The plan refactors that existing file rather than duplicating it.

The frontend CLAUDE.md rule **"Don't hardcode sidebar entries — navigation comes from the backend"** stands for production features. This is the documented exception for dev-only template pages.

## File-level changes

### 1. `src/reference/navigation/referenceRoutes.ts` — refactor in place

**What's wrong with it today:**
- Items use production path constants (`paths.analytics`, `paths.calendar`, …). Those paths either don't exist or point to real production pages, not the Falcon demos under `/reference/*`.
- Items carry `roles: ['developer']` — meant for the old role-filtered nav. In the new dev-gated render path, this field is dead weight.
- Section coverage is incomplete vs. Falcon upstream — missing Utilities, missing some Pages variants.

**Refactor:**
- Replace every `to:` value with the matching `paths.ref*` constant from `src/routes/paths.ts` (292 constants available — verified covers every demo).
- Drop the `roles` field from items and groups. Gating moves to the build-time `import.meta.env.DEV || VITE_ENABLE_REFERENCE` check at the consumer.
- Add a new exported group `utilitiesRoutes: RouteGroup` covering Background, Borders, Colors, Display, Flex, Grid, Position, Sizing, Spacing, Typography, etc. (paths exist as `paths.refUtilities*`).
- Ensure `documentationRoutes` covers Getting Started, Configuration, Styling, Dark Mode, Plugins, FAQ, Design File, Migration, Changelog.
- Add a new export:

  ```ts
  export const developerRealm: NavRealm = {
    key: 'developer',
    label: 'Developer',
    sections: [
      { label: 'Dashboards',     children: dashboardRoutes.children },
      { label: 'App Examples',   children: appRoutes.children },
      { label: 'Pages',          children: pagesRoutes.children },
      { label: 'Components',     children: modulesRoutes.children },
      { label: 'Utilities',      children: utilitiesRoutes.children },
      { label: 'Documentation',  children: documentationRoutes.children },
    ],
  };
  ```

  `NavRealm` is the type already exported by `src/store/api/navigationApi.ts:34` — using it means the data merges into the existing realms render path with zero new types.

**Backwards-compat:**
- Existing named exports (`dashboardRoutes`, `appRoutes`, `pagesRoutes`, `modulesRoutes`, `documentationRoutes`) keep the same names so `NavbarTopDropDownMenus.tsx` (the combo-nav top dropdown consumer) keeps compiling.
- The five other exports (`testRoutes`, `referenceRoutes`, `developmentRoutes`, `operatorRoutes`, `managerRoutes`, `adminRoutes`, `superAdminRoutes`) are unused at runtime. Audit and remove any with zero importers; leave the rest.

### 2. `src/components/navbar/vertical/NavbarVertical.tsx` — append the realm

Around lines 156–183, in the `!isLoading && !isError` branch, change:

```ts
{realms.length > 0
  ? realms.map(realm => ( … ))
  : filteredNavigation.map(…)}
```

to:

```ts
const showDeveloperRealm =
  import.meta.env.DEV || !!import.meta.env.VITE_ENABLE_REFERENCE;
const renderedRealms = showDeveloperRealm
  ? [...realms, developerRealm]
  : realms;

{renderedRealms.length > 0
  ? renderedRealms.map(realm => ( … ))
  : filteredNavigation.map(…)}
```

Add the import `import { developerRealm } from 'reference/navigation/referenceRoutes';` at the top.

Three reasons for this shape:

- **Identical render path.** Uses the same `NavbarLabel` → `NavbarSectionLabel` → `NavbarVerticalMenu` chain that backend realms use. No visual drift between Developer and Administration realms; collapse/expand behaviour comes for free.
- **Same gate as the routes.** `DEV || VITE_ENABLE_REFERENCE` exactly mirrors `src/routes/referenceRoutes.tsx:221`. Nav and routes can never get out of sync — either both render or neither does.
- **Survives the auth gate.** The realm is added inside the `!isLoading && !isError` branch, after the early `if (!isAuthenticated) return null` at line 118. An unauthenticated user never sees the Developer realm flash.

### 3. `src/components/navbar/top/NavbarTopDropDownMenus.tsx` — no edit, verify only

The combo-nav top dropdowns consume `dashboardRoutes`, `appRoutes`, `pagesRoutes`, `modulesRoutes`, `documentationRoutes`. Because step 1 keeps those exports and only fixes their `to:` paths, the top dropdowns automatically start pointing at the correct `/reference/*` routes. Verify by switching to combo nav style in `/admin/settings` and clicking through a sample item per dropdown.

### 4. `frontend-admin/CLAUDE.md` — document the exception

In the "How navigation works" section, append:

> **Dev-only exception — Developer realm.** When `import.meta.env.DEV` is true (or `VITE_ENABLE_REFERENCE` is set), `NavbarVertical` appends a hardcoded `Developer` realm from `src/reference/navigation/referenceRoutes.ts` pointing to the dev-only `/reference/*` routes. This is the **only** place sidebar entries are hardcoded in the frontend. Do not extend this pattern for production features — declare those in the backend module's `NavItems()`.

Update the "Don't" list entry from "Don't hardcode sidebar entries. Navigation comes from the backend." to "Don't hardcode sidebar entries **for production features** — navigation comes from the backend. The dev-only Developer realm is a documented exception."

## Realm contents (concrete)

```
Developer  (realm, key: "developer")
├─ Dashboards
│   └─ Dashboards (chart-pie)
│      ├─ Default                  → paths.refDashboardsDefault
│      ├─ Analytics                → paths.refDashboardsAnalytics
│      ├─ CRM                      → paths.refDashboardsCrm
│      ├─ E-commerce               → paths.refDashboardsEcommerce (if exists)
│      ├─ LMS                      → paths.refDashboardsLms (if exists)
│      ├─ Project Management       → paths.refDashboardsProjectManagement
│      ├─ SaaS                     → paths.refDashboardsSaas
│      ├─ Support Desk             → paths.refDashboardsSupportDesk
│      └─ Default w/ Query         → paths.refDashboardsDefaultWithQuery (if exists)
├─ App Examples
│   ├─ Calendar                    → paths.refAppCalendar
│   ├─ Chat                        → paths.refAppChat
│   ├─ Kanban                      → paths.refAppKanban
│   ├─ Email ▸ Inbox / Compose / Detail
│   ├─ Events ▸ Create / List / Detail
│   ├─ Social ▸ Feed / Activity Log / Notifications / Followers
│   └─ Support Desk ▸ Table / Card / Contacts / Contact Details
│                    / Tickets Preview / Quick Links / Reports
├─ Pages
│   ├─ Starter                     → paths.refStarter
│   ├─ Landing                     → paths.refLanding
│   ├─ Pricing ▸ Default / Alt
│   ├─ FAQ ▸ Basic / Alt / Accordion
│   └─ Miscellaneous ▸ Associations / Invite People / Privacy Policy
├─ Components
│   ├─ Alerts, Accordion, Animated Icons, Avatar, Backgrounds,
│   │   Badges, Breadcrumbs, Buttons, Calendar, Cards,
│   │   Carousel ▸ Bootstrap / Slick, Collapse, Cookie Notice,
│   │   Count Up, Draggable, Dropdowns, List Group, Modals,
│   │   Offcanvas, Images, Figures, Hoverbox, Lightbox,
│   │   Progress Bar, Pagination, Placeholder, Popovers,
│   │   Scrollspy, Search, Spinners, Timeline, Toasts,
│   │   Tooltips, Treeview, Typed Text, Videos
│   ├─ Forms ▸ Form Control / Input Group / Select / Checks /
│   │           Range / Form Layout / Advance Select / Date Picker /
│   │           Editor / Emoji Picker / File Uploader / Input Mask /
│   │           Range Slider / Rating / Floating Labels / Wizard /
│   │           Validation
│   ├─ Tables ▸ Basic / Advance (TanStack)
│   ├─ Navigation ▸ Navs / Tabs / Navbars (Vertical / Top / Double Top / Combo)
│   ├─ Charts ▸ How To Use / Line / Bar / Candlestick / Geo Map /
│   │            Scatter / Pie / Radar / Heatmap
│   ├─ Icons ▸ FontAwesome / React Icons
│   ├─ Maps ▸ Google / Leaflet
│   └─ Widgets (gallery page)
├─ Utilities
│   └─ Background, Borders, Colors, Colored Links, Display,
│       Visibility, Stretched Link, Float, Position, Spacing,
│       Sizing, Text Truncation, Typography, Vertical Align,
│       Flex, Grid, Scrollbar
└─ Documentation
    └─ Getting Started, Configuration, Styling, Dark Mode,
        Plugins, FAQ, Design File, Migration, Changelog
```

For every leaf item we keep the icon Falcon originally used (FontAwesome class string already present in the legacy `referenceRoutes.ts`).

## Verification checklist

1. `cd frontend-admin && npm run typecheck` — must pass. `developerRealm` must satisfy `NavRealm` from `store/api/navigationApi.ts`.
2. `npm run dev`, log in as administrator:
   - Developer realm renders below Administration / Business / Tools.
   - Each section header (`Dashboards`, `App Examples`, …) renders with the same styling as backend section labels.
   - Expand `Components → Forms`, click `Validation` → lands on `/reference/components/forms/validation` and renders.
   - Click one item per top-level section to confirm no 404 / no path typo.
3. Switch the navbar style to combo in `/admin/settings`, confirm the top dropdowns (Dashboard / App / Pages / Modules / Documentation) still populate. Click one item each — should land on the same `/reference/*` routes the sidebar uses.
4. `npm run build` (production, no env flag) → preview → confirm Developer realm is **absent** and bundle size doesn't pick up `referenceRoutes.ts` page imports (chunk inspection: should already be excluded by the existing PROD gate in `routes/referenceRoutes.tsx`).
5. `VITE_ENABLE_REFERENCE=1 npm run build && npm run preview` → Developer realm **present**, links work — matches the route opt-in.
6. Log out → Developer realm gone (auth gate).
7. As non-administrator role (developer, manager, operator), confirm the Developer realm renders identically — it's not role-filtered. (Acceptable for dev builds; if we want role gating later, that's a follow-up.)

## What is explicitly out of scope

- No new demo pages — every link points to a Falcon page that already lives under `src/reference/`.
- No backend changes — no module, no `/v1/navigation` change, no `Realm`/`Section` field additions.
- No `paths.ts` changes — every `ref*` constant we reference already exists (verified: 292 `ref*` constants present).
- No role-based filtering of the Developer realm. In dev builds anyone with an admin login sees it.
- No mobile/responsive tweaks. The realm uses the same render primitives as backend realms and inherits their behaviour.
- No removal of the still-imported legacy exports (`dashboardRoutes` et al.) — only their `to:` paths are corrected.

## Rollback

Single commit. Revert with `git revert <sha>`. The legacy `referenceRoutes.ts` is the only behaviour-bearing change; reverting restores the prior (broken-paths) state, which nobody depends on for production.

## Risks & mitigations

| Risk | Mitigation |
|------|------------|
| A `paths.ref*` constant doesn't exist for a Falcon demo I want to expose | Audit `paths.ts` line-by-line during implementation; for any missing demo, either skip the item or add the path constant + the route in `routes/referenceRoutes.tsx`. The plan defaults to skip. |
| `NavbarTopDropDownMenus.tsx` regresses because path changes break a dropdown link | Step 3 verification clicks through every top dropdown. Also: pre-change those dropdown links are already broken (point to production paths), so the change can only improve things. |
| Future contributor copies the Developer-realm pattern for a production feature | CLAUDE.md edit (step 4) explicitly calls this out as the only allowed exception. |
| Bundle bloat in production | Reference routes are PROD-gated in `routes/referenceRoutes.tsx`; the realm data file is small (~250 lines of nav config, no page imports). Verify in step 4 of the checklist. |
| User confusion about Developer items appearing only in dev | Add a small `<small className="text-muted">Dev only</small>` subtitle under the realm label, or gate by `VITE_ENABLE_REFERENCE` in staging too so QA sees them. **Decision deferred to implementation.** |

## Estimated size

~300 lines net (most of it `to:` rewrites + the Utilities group + the `developerRealm` export). ~30 lines edited in `NavbarVertical.tsx` and CLAUDE.md. One commit, one PR.

## Open questions

1. Should the Developer realm be visible in **staging** builds too (via `VITE_ENABLE_REFERENCE=1` in the staging Dockerfile/compose)? Useful for QA to access design references; harmless because routes are also opt-in. Default: yes.
2. Drop or keep the existing unused exports (`testRoutes`, `developmentRoutes`, `operatorRoutes`, `managerRoutes`, `adminRoutes`, `superAdminRoutes`)? Default: drop the ones with zero importers after grep audit.
3. Should the `Developer` realm sit at the **top** or **bottom** of the sidebar? Default: bottom (after backend realms) so it doesn't push real work down.
