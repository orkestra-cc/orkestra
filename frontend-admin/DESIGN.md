---
name: Orkestra Operator Console
description: Calm, clinical, high-contrast Tier-1 admin console — The Control Room
colors:
  primary: "#2c7be5"
  link-ink: "#2569c3"
  code-pink: "#c22e64"
  secondary: "#68707c"
  success: "#00d27a"
  info: "#27bcfd"
  warning: "#f5803e"
  danger: "#e63757"
  white: "#ffffff"
  gray-100: "#f9fafb"
  gray-200: "#f2f4f6"
  gray-300: "#dfe3e8"
  gray-400: "#bcc3cc"
  gray-500: "#98a1ac"
  gray-600: "#68707c"
  gray-700: "#454c58"
  gray-800: "#333a45"
  gray-900: "#252c36"
  gray-1000: "#1a202a"
  gray-1100: "#0f141c"
  dark-body-bg: "#0b1727"
  dark-body-color: "#9da9bb"
  dark-card-bg: "#121e2d"
typography:
  display:
    fontFamily: "Poppins, -apple-system, BlinkMacSystemFont, sans-serif"
    fontSize: "2.488rem"
    fontWeight: 500
  heading:
    fontFamily: "Poppins, -apple-system, BlinkMacSystemFont, sans-serif"
    fontWeight: 500
  body:
    fontFamily: "'Open Sans', -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif"
    fontSize: "1rem"
    fontWeight: 400
  label:
    fontFamily: "'Open Sans', -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif"
    fontSize: "0.833rem"
    fontWeight: 400
rounded:
  base: "0.25rem"
  lg: "0.375rem"
spacing:
  card-y: "1.25rem"
  table-cell-y: "0.5rem"
  table-cell-x: "0.75rem"
components:
  button-primary:
    backgroundColor: "#286fce"
    textColor: "{colors.white}"
    rounded: "{rounded.base}"
  button-orkestra:
    backgroundColor: "{colors.white}"
    textColor: "{colors.primary}"
    rounded: "{rounded.base}"
  card:
    backgroundColor: "{colors.white}"
    rounded: "{rounded.lg}"
    padding: "1.25rem"
  table-header:
    backgroundColor: "{colors.gray-300}"
    textColor: "{colors.gray-900}"
  badge-subtle-primary:
    backgroundColor: "#d9e7fa"
    textColor: "#215cac"
    rounded: "{rounded.base}"
---

# Design System: Orkestra Operator Console

Scope: `frontend-admin/` only — the Tier-1 operator console. The sibling `frontend-client/` SPA has its own design system and is not governed by this file. Implementation lives in the Orkestra SCSS theme (`src/assets/scss/theme/`, CSS variables prefixed `--orkestra-`); the live showcase library is `src/reference/` and the production primitives are `src/components/common/`. When this file and a recent production page disagree on a value, the theme SCSS is the source of truth — update this file, don't fork it.

## Overview

**Creative North Star: "The Control Room"**

A calm operations floor. Surfaces are quiet, neutral grays — a near-white canvas carrying white cards — and every signal on them is legible at a glance: body text holds an 8.9:1 contrast on white, headings are near-black, and color appears only where something acts or needs attention. The system's personality is **calm, clinical, precise**: the interface recedes so the operator's data comes forward. Nothing decorates; everything either informs or acts.

The confirmed anti-reference is the console's own past: Falcon's blue-tinted gray ramp (lavender page background `#edf2f9`, washed-out slate text `#5e6e82`). The 2026-08 retune replaced it with a neutral-cool scale — a whisper of cool tint in the GitHub-light register, chosen against a Datadog-inspired reference — and that neutrality is now an invariant, not a phase.

**Key Characteristics:**
- Neutral-cool "Cool Graphite" gray ramp; never blue-cast, never fully achromatic.
- High contrast as a feature: 8.9:1 body text, headings at `gray-900`.
- Color is semantic — Orkestra Blue means "you can act here"; status colors mean status.
- Operate-mode density: compact tables (`fs-10`, 8px vertical cell padding), information-first layouts.
- Ambient, discreet elevation: soft shadows separate, tonal layers structure.
- A permanently frozen dark theme, pixel-identical since 2026-08-05 apart from changes explicitly declared both-modes (see the Frozen Dark Rule).

## Colors

A neutral graphite field where the only saturated voices are the action color and the four status colors.

### Primary
- **Orkestra Blue** (#2c7be5): the color of action. Primary buttons, active nav states, focused inputs, selected rows. It never appears as decoration — if it's blue, the operator can act on it.
- **Link Ink** (#2569c3, `shade-color($primary, 15%)`): the text color of links and link-buttons — the same hue, darkened to hold ≥4.5:1 on every surface links sit on (white 5.40:1, gray-100 5.17:1, gray-200 4.90:1). Fills and states stay Orkestra Blue; only running link text uses the ink.

### Secondary
- **Secondary Gray** (#68707c, = `gray-600`): secondary text, muted actions, `btn-secondary`. A member of the ramp doing double duty, not a separate hue.

### Status accents
- **Success Green** (#00d27a): confirmations, healthy states, positive deltas.
- **Info Cyan** (#27bcfd): informational badges and callouts.
- **Warning Amber** (#f5803e): degraded states, caution. (Registered as `warning`/"yellow" in the theme map; visually amber-orange.)
- **Danger Crimson** (#e63757): errors, destructive actions, failing health.

Subtle variants exist for all of these (`*-bg-subtle` ≈ 82% tint for chips/badges, `*-text-emphasis` ≈ 20–35% shade for their text) — use them via `SubtleBadge` and the Bootstrap subtle utilities rather than recomputing tints.

### Neutral
The **Cool Graphite** ramp — 11 steps, neutral-cool with a whisper of blue, high-contrast by design. Each step has a fixed job:

- **gray-100** (#f9fafb): soft surfaces — striped table rows, hover fills, `bg-body-tertiary`.
- **gray-200** (#f2f4f6): the page canvas (`body` background).
- **gray-300** (#dfe3e8): borders, dividers, the table header band.
- **gray-400** (#bcc3cc): input borders, disabled states.
- **gray-500** (#98a1ac): placeholders, muted icons.
- **gray-600** (#68707c): secondary text (5.4:1 on white).
- **gray-700** (#454c58): body text (8.9:1 on white).
- **gray-800** (#333a45): medium-emphasis text.
- **gray-900** (#252c36): headings.
- **gray-1000** (#1a202a): near-black emphasis.
- **gray-1100** (#0f141c): theme black (`$dark`).

### Dark theme (frozen)
Dark mode keeps the pre-retune Falcon palette as literals: canvas **#0b1727**, cards **#121e2d** (2.9% tint of the canvas), body text **#9da9bb**, with the old blue-tinted ramp inverted for its grays.

**The Frozen Dark Rule.** The dark theme is pixel-frozen and permanently decoupled from the light ramp. Never derive a dark value from `$gray-*` or the light tokens; dark values are literals (see `_variables-dark.scss` and `root/_dark.scss`, which carry the freeze notes inline). Any future light retune must freeze its dark twins the same way. Dark moves only for a change **declared both-modes**; on 2026-08-07 that was `$body-secondary-color-dark` (`#d8e2ef` → `#8494a8`, the de-emphasis fix below) and `$headings-color-dark`, pinned to the literal it already resolved to. Both are hand-picked literals, not derivations.

**The Secondary-Is-Secondary Rule.** `--#{$prefix}secondary-color` — which `.text-muted` reads — must sit *below* body text, never above. It held `gray-900` until 2026-08-07 and so rendered every de-emphasized string (field help, group descriptions, `StatCard` captions and subtitles, muted table cells) at 14.07:1 against a body of 8.65:1: the whole de-emphasis layer ran backwards, and any hierarchy tuned against it was tuned against a lie. The trap is indirect — `$headings-color` used to resolve *through* this token, which is what forced it near-black. Headings now name their own ink (`$gray-900` / `#d8e2ef`), leaving secondary free to be secondary: `gray-600` (5.00:1 on a card, 4.54:1 on the canvas) and `#8494a8` in dark (5.43:1). De-emphasize with ink *or* with size, weight and case — but never invert the ramp to do it.

**The Acting Blue Rule.** Orkestra Blue appears only where the operator can act or is acting (buttons, links, active/selected/focused states). Large decorative washes of primary are off-system.

**The Utility-Class Rule.** Components never carry hex values. Color reaches JSX through Bootstrap/theme utilities (`text-900`, `bg-200`, `border-300`, `text-body-tertiary`) or CSS variables (`--orkestra-*`); charts read live values via `getColor()`. This is what keeps both themes and future retunes one-file changes.

## Typography

**Display Font:** Poppins (with system-ui fallbacks)
**Body Font:** Open Sans (with system-ui fallbacks)
**Mono Font:** SFMono-Regular, Menlo, Monaco, Consolas

**Character:** A pragmatic pairing — Poppins gives headings a rounded, geometric confidence at medium weight (500, never bold-by-default); Open Sans keeps running text and data neutral and highly legible at small sizes. The scale is modular with a 1.2 ratio from a 1rem base (`$type-scale: 1.2`, sizes exposed as `fs-11`…`fs-1`).

### Hierarchy
- **Display / h1** (500, 2.488rem = fs-4): page titles, rare.
- **Headline / h2** (500, 2.074rem = fs-5): section heads.
- **Title / h4–h5** (500, 1.44rem–1.2rem): card and panel titles. `.card-title` takes the theme-aware heading ink (`$card-title-color: var(--#{$prefix}heading-color)`), the same token a plain `<h5>` in an `OrkestraCardHeader` resolves to. It must never be pinned to a `$gray-*` literal: Bootstrap emits `--#{$prefix}card-title-color` as a **component-level** var with no dark counterpart, so the light `$headings-color` it used to hold painted every `<Card.Title>` `#252c36` on the `#121e2d` dark card — 1.1:1, a heading that was simply not on screen.
- **Body** (400, 1rem, Open Sans): default prose and form text, `gray-700`.
- **Label / dense UI** (400, 0.833rem = fs-10): table cells, badges, metadata — the workhorse size of every data surface.

**The fs-10 Table Rule.** Data tables run at `fs-10` (0.833rem): density is a feature of the Control Room, not a compromise. Don't bump table type up to body size to "improve readability" — contrast, not size, does that job here.

**The On-Scale Rule.** Every size on screen is a step of `$font-sizes` — there is no 14px and no 12px in this console. Bootstrap's own defaults are the usual leak (`$font-size-sm` 0.875rem behind `size="sm"` controls, `$small-font-size` 75% behind `.form-text`, and `.nav-link`'s `--#{$prefix}nav-link-font-size`, which is *declared but never defined* outside the pill/tab variants, so a bare `<Nav>` silently inherits 1rem). Fix those at the token or component-SCSS level so the whole console moves together; adding an `fs-*` class at the call site patches one page and hides the leak everywhere else.

## Layout

A vertical-navbar console: fixed sidebar (backend-driven navigation), content area on the `gray-200` canvas, content organized as white cards. Grid is Bootstrap 5's 12-column with breakpoints sm 576 / md 768 / lg 992 / xl 1200 / **xxl 1540** (widened for dense admin tables). Spacing uses Bootstrap utility steps (`mb-3`, `g-2`…); cards pad 1.25rem vertically. Density is deliberate: tables at 0.5rem vertical / 0.75rem horizontal cell padding (8px/12px), KPI rows as compact `StatCard` tiles, page headers via `PageHeader`. Layouts must hold at operator-realistic widths — wide tables scroll within their card, never the page.

## Elevation & Depth

Elevation is **ambient and discreet**: shadows separate surfaces from the canvas, they do not build a hierarchy of importance. Real structural depth comes from the tonal ramp — `gray-100` soft fills inside white cards, `gray-200` canvas beneath, `gray-300` borders between. One card level; no stacked shadow tiers.

### Shadow Vocabulary
- **Card / ambient** (`box-shadow: 0 7px 14px 0 rgba(50, 58, 70, 0.1), 0 3px 6px 0 rgba(0, 0, 0, 0.07)`): the default card float. Its first layer was deliberately neutralized from Falcon's blue-tinted `rgba(65,69,88)` — keep shadows tint-free.
- **Small** (`0 0.125rem 0.25rem rgba(0,0,0,0.075)`): dropdowns, popover-scale elements.
- **Large** (`0 1rem 4rem rgba(0,0,0,0.175)`): modals only.
- **Button ("orkestra")** (`0 0 0 1px rgba(43,45,80,0.1), 0 2px 5px 0 rgba(43,45,80,0.08), 0 1px 1.5px 0 rgba(0,0,0,0.07), 0 1px 2px 0 rgba(0,0,0,0.08)`): the crisp ring-plus-drop that makes white "falcon" buttons read as raised keys.

**The Ambient-Only Rule.** Don't invent intermediate shadow tiers or "lift" elements for emphasis. If something needs more weight, use tone (`gray-100` fill, `gray-300` border) or type, not a bigger shadow.

## Shapes

Restrained rounding: **0.25rem** default radius on controls and inputs, **0.375rem** on cards and containers, pill only where Bootstrap defaults it (badges-pill, avatars). Corners say "precision instrument", not "friendly app". Borders are 1px `gray-300` for dividers and structure; **controls step up to `gray-600`** — an input or a switch has nothing but its border to identify it, so it owes 3:1, which `gray-300`/`gray-400` never met. No decorative thick borders except the StatCard's 4px accent edge, which is semantic (it colors by status). No clipping tricks or asymmetric silhouettes — the one sanctioned diagonal is the StatCard corner ribbon.

## Components

Component philosophy: **quiet precision**. Clean white surfaces, discreet shadows, controlled density — a component never asks for attention it hasn't earned from its data. Reuse order is law: `src/reference/app-examples/` → `src/reference/components/` → `components/common/` → `components/dashboards/` → raw React Bootstrap. Build new only when all four miss, and register a showcase under `src/reference/` when you do.

### Buttons
- **Shape:** subtly rounded (0.25rem).
- **Primary** (`variant="primary"`): white text on #286fce (`shade-color($primary, 10%)`, 4.94:1 — the AA-passing button shade of Orkestra Blue) — reserved for the one main action of a view. Hover/active step darker (#2569c3 / #2362b7).
- **Orkestra family** (`variant="orkestra-primary"`, `orkestra-default`, `orkestra-danger`, …, compiled as `.btn-orkestra-*` by the `$theme-orkestra-btn-colors` loop in `_buttons.scss`): the console's signature button — white surface (`--orkestra-btn-orkestra-background`), colored text, the crisp orkestra shadow ring; hover deepens the text color (−17% shift) and the shadow, background stays put. Use for secondary and toolbar actions. **There is no `falcon-*` variant** — the family was renamed with the theme and nothing in `assets/scss/` defines `.btn-falcon-*`, so `variant="falcon-default"` emits a dead class and the button loses its surface, border and shadow ring entirely.
- **Hover / Focus:** color shifts and shadow steps, no size or position jumps.

### Cards / Containers
- **Corner Style:** 0.375rem.
- **Background:** white (`quaternary-bg`) on the `gray-200` canvas; dark mode #121e2d (frozen).
- **Shadow Strategy:** the ambient card shadow, one level, always.
- **Internal Padding:** 1.25rem vertical; headers via `OrkestraCardHeader`, bodies via `OrkestraCardBody` / `SectionCard`.
- **One card level inside a tabbed surface.** The `card-header-tabs` strip already names the pane it opens, so a tab pane carries no card of its own — a second bordered card inside the tab body is double chrome repeating a label the strip just showed. Panes go straight to their toolbar, table, or form; a pane that genuinely holds two or more distinct sub-surfaces (`/user/security`'s Two-factor pane: authenticator app + passkeys) uses `SectionCard` for each, never one wrapper around the whole pane.

### Data tables (AdvanceTable)
- Always `AdvanceTable` + `useAdvanceTable` + `AdvanceTableProvider` — never raw `<table>` for production lists.
- **Header band + ink:** the theme owns both, painted on `th` via `--orkestra-table-head-bg` and `--orkestra-gray-900` — light `gray-300` band with `gray-900` ink, dark the frozen literals `#232e3c` / `#d8e2ef`. Call sites pass no color classes on headers (`text-900` in column meta is redundant but harmless); `text-nowrap` stays.
- **Body:** `fs-10`, striped `gray-100` rows, 0.5rem/0.75rem cell padding, `align-middle`.
- **Footer:** `AdvanceTableFooter` with rows-per-page, row info, nav buttons.

### StatCard (signature)
The ERP-style KPI tile (`components/common/StatCard` + `SectionCard`; showcase at `reference/components/ui/StatCards.tsx`): a 4px **left accent edge**, a faded 2×-scale icon, one big value, compact label. The icon is a **category marker and stays quieter than the datum it labels** — 2× at 50% opacity, anchored to the card's bottom-right corner (the ribbon owns the top-right). Anchor it to the *card*, never to the text above it: a row mixing tiles that drill down with tiles that don't otherwise scatters its icons across three heights. At the old 3×/75%, a 48px mark in a saturated status hue was the loudest thing in an Operate tile (recalibrated 2026-08-21; geometry, so both modes move together). **The edge is neutral (`gray-300`) at rest and takes a hue only through the `accent` prop, which a call site passes when the metric's current state earns it** — a non-zero pending counter, a degraded health check — never as category identity; a GDPR page reporting perfect health must not frame its zeros in crimson (2026-08-07, replacing the old always-on four-side frame). `color` tints only the faded icon. When accented, the edge owes 3:1 against the canvas — in **light** it uses the `-text-emphasis` step of its hue, because the raw `success` and `info` read 1.81:1 and 1.96:1 there and the encoding simply is not there; dark keeps the raw hues, already at 8–9:1 (`.stat-card` / `.stat-card-accent-*` in `_stat-card.scss`). Attention states add the **corner ribbon** — a 45° band tucked into the top-right corner (78px box, uppercase 0.55rem/700 white text on a status color). This is the console's one expressive flourish; keep it earned (real attention states only, via the `badge` prop). The tile's title and value render as `.h6`/`.h3` **divs**, not heading tags — KPI data never joins the page outline. A tile whose metric has a page behind it carries that link in the **`footer` slot**, pinned by `mt-auto` to the bottom of the text column so a row of tiles shares one link line instead of a ragged one; omit it when there is nothing to drill into, and never point it at a list the counted rows are provably absent from (the slot's absence is what pushed one dashboard into a hand-rolled copy of this tile until 2026-08-21).

**`StatCardPair`** is the two-metric sibling (`components/common/StatCardPair`): one tile carrying a pair of figures read *against each other* — issued vs received, granted vs revoked — split by a `border-300` divider, each half with its own small icon, value and drill-down. Two *unrelated* metrics are two StatCards, not a pair. It shares this tile's language (accent edge, `.h6`/`.h3` divs, muted label register) and drops the large icon: each half has one, and a third would be the tallest thing in the card, which under `h-100` sets the height of every tile beside it. Its one sanctioned divergence — **the corner ribbon is card-level**, so a two-metric card would need two overlapping in one corner; a half flags with an inline pill, and a metric that needs the ribbon's weight needs its own StatCard.

### Inputs / Fields
- **Style:** white field, 1px `gray-600` border, 0.25rem radius. The border is the *only* thing identifying a white field on a white card, so it carries the 3:1 non-text floor rather than the `gray-400` it used to (1.29:1 light / 1.60:1 dark — a field with no perceptible edge). `gray-600` is redefined per theme, so one token clears both. Default-size controls carry body-size text — forms stay at 1rem even where tables are fs-10. `size="sm"` controls (the dense-form register used across the admin surfaces) sit one step down at **fs-10**, the same step as the `.form-label` above them, with help and validation text at **fs-11**. Both are theme tokens (`$input-font-size-sm`, `$form-text-font-size`), not call-site classes: never spell a font size on a `Form.Label`, `Form.Control` or `Form.Text`.
- **Focus:** Orkestra Blue border with the standard Bootstrap focus ring (primary at 25% alpha). Focus must always be visible — WCAG 2.2 focus-appearance is a hard requirement. Enforced at the token layer (2026-08-07, both-modes): `$btn-focus-width` is **0.25rem** (Bootstrap paints it on `:focus-visible`, so mouse clicks stay quiet), the orkestra button family re-adds the ring on top of its own shadow (`mixins/_button.scss`), and no call site may suppress the ring with `shadow-none`/`box-shadow: none` on a focusable control — that is how the whole console went keyboard-blind.
- **Switches:** rem-sized at **2.75rem × 1.5rem** (`_forms.scss`), not em-sized — inside an fs-10 table an em switch shrank to 27×13px, under the WCAG 2.2 24px target floor. Every instant-effect switch names its subject (`aria-label` with the row's module/user name), never a bare "switch".
- **Error / Disabled:** `danger` border + feedback text via react-hook-form/yup; disabled fields drop to `gray-400` borders and muted text. A disabled *button* gets a real neutral treatment — `gray-200` fill, `gray-700` label, full opacity — never the enabled fill at `opacity: .5`, which reads as an unreadable ghost in light and as a still-clickable button in dark.
- **Placeholders** are text and owe 4.5:1: `gray-600`, not the `gray-400` that made every search box read as an empty field.

### Badges / Status pills
`SubtleBadge` only: subtle tinted background (≈82% tint of the status color) with dark emphasized text of the same hue — status is readable in both themes without shouting. No solid-fill status badges in data surfaces. The light emphasis shades sit at 45% (`success`/`info`/`warning`), deepened from 35/35/30% where the three chips this console shows most — `running`, `production`, `toggleable` — measured 3.96/4.11/4.17:1 on their own bg-subtle. And status colors mean status: a required dependency in a healthy module is not `warning`.

### Navigation
Vertical sidebar rendered from the backend (`/v1/navigation`) — never hardcoded. In **light** the rail and the top bar are real surfaces, not overlays: white glass (`--#{$prefix}bg-navbar-glass: rgba(#fff, .97)`) with a 1px `gray-300` hairline (`--#{$prefix}navbar-hairline-color`, painted as an inset shadow so it never shifts layout). Dark pins the hairline to `transparent` and keeps its own navy glass — the frozen dark chrome stays edge-less. Nav links and realm labels sit on the type ramp at **fs-10** (both-modes, 2026-08-07); realm labels ink `gray-600` in light (dark pins its frozen `#5e6e82`), section sub-labels one step down at fs-11 `text-600`. Realm/section grouping per the navigation module.

**Active-item ink is `--#{$prefix}primary-text-emphasis`, not raw Orkestra Blue.** Blue-on-blue-subtle is the pairing every selected-state defaults to, and it measures 3.30:1 in light / 3.42:1 in dark against inactive siblings at 8.65:1 / 7.06:1 — the selected item ends up the *hardest* thing in its own list to read, which is the one thing a navigation list must never do. The emphasis token is theme-aware (deepened blue in light, light tint in dark) and lands 5.25:1 / 8.44:1. Selection also carries a non-chromatic mark — the module config rail uses a 3px inset left bar, the tab bar a 2px underline — so it survives greyscale and low vision. Fills, focus rings and hovers stay Orkestra Blue; only the *label of a selected item* takes the emphasis ink.

## Do's and Don'ts

### Do:
- **Do** reach color through utilities and `--orkestra-*` variables — `text-900`, `bg-200`, `border-300` — and through `getColor()` in charts.
- **Do** keep tables dense: `fs-10`, 0.5rem vertical padding, `gray-300` header band, striped `gray-100` rows.
- **Do** use the falcon/orkestra white-shadowed button family for secondary actions and keep solid Orkestra Blue for the single primary action of a view.
- **Do** verify every change in both themes; dark must remain pixel-identical unless a change is explicitly declared both-modes (like the 2026-08 density change).
- **Do** meet WCAG 2.2 AA: the ramp's contrast ratios (8.9:1 body, 5.4:1 secondary) are the floor, not the ceiling; focus states visible; target sizes respected.

### Don't:
- **Don't** derive dark-theme values from the light ramp — dark is frozen literals, permanently (The Frozen Dark Rule).
- **Don't** reintroduce blue-tinted grays, tinted shadows, or the old Falcon values (#edf2f9, #5e6e82, rgba(65,69,88,…)) anywhere in light mode.
- **Don't** hardcode hex colors, inline color/spacing styles, or generic fonts in components — the theme provides all three.
- **Don't** build bespoke primitives where one exists: no raw `<table>` (AdvanceTable), no hand-rolled KPI tiles (StatCard), no solid status badges (SubtleBadge), no Chart.js/D3 (ECharts via `ReactEchart`).
- **Don't** use Orkestra Blue decoratively or add new saturated hues to the system — the palette is closed: one action color (with its darker Link Ink), four status colors, graphite, and the single utilitarian code accent (#c22e64, AA on white).
