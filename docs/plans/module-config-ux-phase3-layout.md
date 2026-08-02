# Module Config UX — Phase 3 (master-detail layout) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the horizontal-tab config card at `/admin/modules/:name` with a
master-detail settings surface whose rail navigates the whole module page, backed by one
form whose unsaved changes aggregate across every group.

**Architecture:** One `react-hook-form` instance for the entire module, mounted in
`detail/index.tsx`. The rail only chooses which slice of it is visible, so switching
section is not a navigation and never trips `useBlocker`. A sticky save bar reads
`formState.dirtyFields` to report changes and validation errors across *all* groups, each
error linking back to the section that owns it.

**Tech Stack:** React 19, TypeScript 5.9 strict, react-hook-form 7.76 + yup 1.6 +
`@hookform/resolvers` 5.4, React Bootstrap 2.10, RTK Query, react-i18next, Vitest + RTL +
MSW.

**Parent spec:** [`module-config-ux.md`](module-config-ux.md) §4.1–4.5.
**Builds on:** [phase 2](module-config-ux-phase2-model.md) (merged) — `configModel.ts`
(`buildGroupTree`, `isFieldVisible`, `visibleFields`, `configCompleteness`) and
`helpers/configLabel.ts` (`translateConfigField`, `translateConfigGroup`).

## Global Constraints

- **Phase 3 must land before phase 4.** Today only a group tree's *top-level* `fieldKeys`
  render. Migrating `auth` first would leave the 19 fields under `oauth.google` /
  `oauth.apple` / `oauth.github` / `oauth.discord` unreachable in the UI while
  `configCompleteness` still counts them as required. This plan is what makes nested
  groups reachable.
- **Degradation is mandatory, not optional.** `configGroups` absent, or a tree with fewer
  than 2 top-level nodes → **no rail, the flat form as today**. Every module currently
  served takes this path, and so does every un-migrated fork addon. A regression here is
  a regression on 100% of live installs.
- Server state stays in RTK Query. No new fetching layer.
- Bare path aliases (`store/api/moduleApi`, `helpers/configLabel`); never `@/`, never long
  relative climbs.
- Every user-visible string goes through `t()`, with the key in **both**
  `src/locales/en.json` and `it.json` or the parity test fails. Do **not** add any
  `moduleConfig.*` key — those belong to phases 4–5.
- Active section syncs to `?section=` via `useSearchParams` (the `url-tabs` mandate) —
  never `useState`.
- TypeScript strict; `npm run lint` runs `--max-warnings 0`.
- Vitest exits non-zero on an unhandled MSW request even when every assertion passes.
- **No sticky-positioned element exists anywhere in `pages/` today.** There is no in-repo
  precedent to copy; the rail and save bar introduce the pattern. Both must work in light
  and dark mode and must not overlap content at narrow widths.

```
Pre-flight:
- Production precedent: src/pages/admin/navigation/index.tsx (master-detail Row/Col lg=8|4),
                        src/pages/admin/modules/detail/{index,ModuleConfigSection,
                        ModuleDashboardCards,ModuleDependencyCard,ModuleEnvironmentSwitcher}.tsx
- Reference read:       src/reference/components/navigation/Navs.tsx (Nav flex-column, pills)
- Primitives:           StatCard, SectionCard, OrkestraCardHeader, SubtleBadge, PageHeader,
                        react-bootstrap Nav/Form/Alert/Collapse/Button
```

## Scope decision recorded

The chosen mockup showed a rail entry for "Health & logs". **There is no per-module log
surface in the product** — health is already a `StatCard` in the KPI row, and the runtime
log-level admin lives at `/admin/observability/log-levels`, not per module. Inventing one
would widen the phase. Health stays inside Overview; the rail's non-config sections are
**Overview, Dependencies, Environments**.

---

### Task 1: `useModuleConfigForm` — one form for the whole module

**Files:**
- Create: `frontend-admin/src/pages/admin/modules/useModuleConfigForm.ts`
- Test: `frontend-admin/src/pages/admin/modules/useModuleConfigForm.test.ts`

**Interfaces:**
- Consumes: `ConfigField` from `store/api/moduleApi`; `isFieldVisible` from `./configModel`.
- Produces:
  - `buildYupSchema(schema: ConfigField[]): yup.ObjectSchema<Record<string, unknown>>`
  - `buildDefaults(schema, configValues): Record<string, string>`
  - `collectDiff(schema, values, defaults): { config: Record<string,string>; secrets: Record<string,string> }`
  - `useModuleConfigForm(schema, configValues): { form, defaults }` — positional
    arguments; `form` is the `UseFormReturn<ConfigFormValues>` and `defaults` is what the
    form was seeded with. Callers derive the diff themselves with
    `collectDiff(schema, form.watch(), defaults)`; the hook deliberately does not compute
    it, so a consumer that only needs the form does not re-run the diff on every keystroke.

Validation and diffing are extracted as **pure functions tested without a DOM**; the hook
is a thin wrapper. That keeps the load-bearing logic reviewable and lets Task 3's save bar
consume `collectDiff` directly.

- [ ] **Step 1: Write the failing test**

Create `frontend-admin/src/pages/admin/modules/useModuleConfigForm.test.ts`:

```ts
import { describe, it, expect } from 'vitest';
import type { ConfigField } from 'store/api/moduleApi';
import { buildYupSchema, buildDefaults, collectDiff } from './useModuleConfigForm';

const field = (over: Partial<ConfigField> & { key: string }): ConfigField => ({
  label: over.key,
  description: '',
  type: 'string',
  required: false,
  default: '',
  envVar: '',
  ...over
});

describe('buildDefaults', () => {
  it('prefers the stored value over the schema default', () => {
    const schema = [field({ key: 'a', default: 'fallback' })];
    expect(buildDefaults(schema, { a: 'stored' })).toEqual({ a: 'stored' });
  });

  it('falls back to the schema default when nothing is stored', () => {
    const schema = [field({ key: 'a', default: 'fallback' })];
    expect(buildDefaults(schema, {})).toEqual({ a: 'fallback' });
  });

  it('seeds secrets as empty regardless of stored state', () => {
    // A stored secret is never sent to the client — only secretStatus is. An
    // empty field means "keep what is stored"; pre-filling anything here would
    // either leak or overwrite with a placeholder.
    const schema = [field({ key: 's', type: 'secret', default: 'nope' })];
    expect(buildDefaults(schema, { s: 'nope' })).toEqual({ s: '' });
  });
});

describe('buildYupSchema', () => {
  const validate = async (schema: ConfigField[], values: Record<string, string>) => {
    try {
      await buildYupSchema(schema).validate(values, { abortEarly: false });
      return [] as string[];
    } catch (err) {
      return (err as { errors: string[] }).errors;
    }
  };

  it('accepts a value inside declared bounds', async () => {
    const schema = [field({ key: 'n', type: 'int', min: 8, max: 128 })];
    expect(await validate(schema, { n: '12' })).toEqual([]);
  });

  it('rejects below min and above max', async () => {
    const schema = [field({ key: 'n', type: 'int', min: 8, max: 128 })];
    expect((await validate(schema, { n: '4' })).length).toBe(1);
    expect((await validate(schema, { n: '999' })).length).toBe(1);
  });

  it('rejects a required field left empty', async () => {
    const schema = [field({ key: 'r', required: true })];
    expect((await validate(schema, { r: '' })).length).toBe(1);
  });

  it('does NOT require a required field that is hidden', async () => {
    // The operator cannot see it, so demanding it makes the form unsavable
    // with no visible cause.
    const schema = [
      field({ key: 'on', type: 'bool', default: 'false' }),
      field({
        key: 'hidden',
        required: true,
        dependsOn: [{ key: 'on', in: ['true'] }]
      })
    ];
    expect(await validate(schema, { on: 'false', hidden: '' })).toEqual([]);
    expect((await validate(schema, { on: 'true', hidden: '' })).length).toBe(1);
  });

  it('rejects a value failing a declared pattern', async () => {
    const schema = [field({ key: 'p', pattern: '^[a-z]+$' })];
    expect((await validate(schema, { p: 'ABC' })).length).toBe(1);
    expect(await validate(schema, { p: 'abc' })).toEqual([]);
  });

  it('ignores an uncompilable pattern rather than throwing', async () => {
    const schema = [field({ key: 'p', pattern: '([' })];
    expect(await validate(schema, { p: 'anything' })).toEqual([]);
  });

  it('accepts every duration Go accepts', async () => {
    const schema = [field({ key: 'd', type: 'duration' })];
    for (const ok of ['30s', '1h30m', '500ms', '1.5h', '-5m', '0']) {
      expect(await validate(schema, { d: ok })).toEqual([]);
    }
    for (const bad of ['30 s', '15x', '1H', 'h']) {
      expect((await validate(schema, { d: bad })).length).toBe(1);
    }
  });

  it('treats an empty optional value as valid', async () => {
    const schema = [field({ key: 'n', type: 'int', min: 8 })];
    expect(await validate(schema, { n: '' })).toEqual([]);
  });
});

describe('collectDiff', () => {
  const schema = [
    field({ key: 'a' }),
    field({ key: 'b' }),
    field({ key: 's', type: 'secret' }),
    field({ key: 'on', type: 'bool', default: 'false' }),
    field({ key: 'hidden', dependsOn: [{ key: 'on', in: ['true'] }] })
  ];
  const defaults = { a: 'one', b: 'two', s: '', on: 'false', hidden: 'stale' };

  it('sends only changed non-secret keys', () => {
    const { config } = collectDiff(
      schema,
      { ...defaults, a: 'changed' },
      defaults
    );
    expect(config).toEqual({ a: 'changed' });
  });

  it('sends a secret only when non-empty', () => {
    expect(collectDiff(schema, { ...defaults, s: '' }, defaults).secrets).toEqual({});
    expect(
      collectDiff(schema, { ...defaults, s: 'new' }, defaults).secrets
    ).toEqual({ s: 'new' });
  });

  it('never sends a hidden field, even when its value differs', () => {
    // Switching a provider off must not write back an edit to a field the
    // operator can no longer see — nor discard what is already stored.
    const { config } = collectDiff(
      schema,
      { ...defaults, hidden: 'edited' },
      defaults
    );
    expect(config).toEqual({});
  });

  it('sends a field that became visible and was then edited', () => {
    const values = { ...defaults, on: 'true', hidden: 'edited' };
    const { config } = collectDiff(schema, values, defaults);
    expect(config).toEqual({ on: 'true', hidden: 'edited' });
  });

  it('returns empty objects when nothing changed', () => {
    const { config, secrets } = collectDiff(schema, defaults, defaults);
    expect(config).toEqual({});
    expect(secrets).toEqual({});
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
cd frontend-admin && npx vitest run src/pages/admin/modules/useModuleConfigForm.test.ts
```

Expected: FAIL — `Failed to resolve import "./useModuleConfigForm"`.

- [ ] **Step 3: Write the module**

Create `frontend-admin/src/pages/admin/modules/useModuleConfigForm.ts`:

```ts
import { useForm, type UseFormReturn } from 'react-hook-form';
import { yupResolver } from '@hookform/resolvers/yup';
import * as yup from 'yup';
import type { ConfigField } from 'store/api/moduleApi';
import { isFieldVisible } from './configModel';

/**
 * Mirrors Go's time.ParseDuration: an optional sign, then one or more
 * decimal-with-unit segments (ns, us/µs, ms, s, m, h). A bare zero is valid.
 * Kept identical to the copy in ModuleConfigFields — both are the same
 * contract, and the backend is the authority on it.
 */
const DURATION_RE =
  /^[+-]?0$|^[+-]?((\d+(\.\d*)?|\.\d+)(ns|us|µs|μs|ms|s|m|h))+$/;

/** Compiles a schema-declared pattern, or null when it is not usable. */
const safeRegExp = (pattern?: string): RegExp | null => {
  if (!pattern) return null;
  try {
    return new RegExp(pattern);
  } catch {
    // The backend validator rejects an uncompilable pattern; if one reaches
    // here, skipping the check beats throwing inside a resolver.
    return null;
  }
};

export type ConfigFormValues = Record<string, string>;

/**
 * Seeds the form. A stored value wins over the schema default; a secret always
 * starts empty because the backend never sends secret values to the client —
 * only whether one exists. An empty secret field means "keep what is stored".
 */
export const buildDefaults = (
  schema: ConfigField[],
  configValues: Record<string, string> | undefined
): ConfigFormValues => {
  const stored = configValues ?? {};
  const out: ConfigFormValues = {};
  for (const f of schema) {
    if (f.type === 'secret') {
      out[f.key] = '';
      continue;
    }
    const v = stored[f.key];
    out[f.key] = v !== undefined && v !== '' ? v : f.default || '';
  }
  return out;
};

/**
 * Builds the validation schema from the backend's own field metadata.
 *
 * Every rule is conditional on the field being visible: a hidden field is not
 * validated at all. Validating one would make the form unsavable with no
 * visible cause — the operator cannot reach the control the error belongs to.
 */
export const buildYupSchema = (
  schema: ConfigField[]
): yup.ObjectSchema<Record<string, unknown>> => {
  const shape: Record<string, yup.StringSchema> = {};

  for (const field of schema) {
    let rule = yup.string();

    rule = rule.test(
      'orkestra-field',
      'invalid',
      function validateField(value) {
        const values = (this.parent ?? {}) as ConfigFormValues;
        if (!isFieldVisible(field, values, schema)) return true;

        const raw = (value ?? '').trim();

        if (field.required && raw === '') {
          return this.createError({ message: 'required' });
        }
        if (raw === '') return true;

        if (field.type === 'duration' && !DURATION_RE.test(raw)) {
          return this.createError({ message: 'duration' });
        }
        if (field.type === 'int') {
          const n = Number(raw);
          if (Number.isNaN(n)) {
            return this.createError({ message: 'notANumber' });
          }
          if (field.min !== undefined && n < field.min) {
            return this.createError({ message: `min:${field.min}` });
          }
          if (field.max !== undefined && n > field.max) {
            return this.createError({ message: `max:${field.max}` });
          }
        }
        const re = safeRegExp(field.pattern);
        if (re && !re.test(raw)) {
          return this.createError({ message: 'pattern' });
        }
        return true;
      }
    );

    shape[field.key] = rule;
  }

  return yup.object(shape);
};

/**
 * The payload to send: changed non-secret keys plus non-empty secrets.
 *
 * A hidden field is excluded in both directions — its edit is not written back,
 * and its stored value is left alone. Switching an OAuth provider off must not
 * discard its client secret.
 */
export const collectDiff = (
  schema: ConfigField[],
  values: ConfigFormValues,
  defaults: ConfigFormValues
): { config: Record<string, string>; secrets: Record<string, string> } => {
  const config: Record<string, string> = {};
  const secrets: Record<string, string> = {};

  for (const field of schema) {
    if (!isFieldVisible(field, values, schema)) continue;
    const next = values[field.key] ?? '';
    if (field.type === 'secret') {
      if (next.trim() !== '') secrets[field.key] = next;
      continue;
    }
    if (next !== (defaults[field.key] ?? '')) config[field.key] = next;
  }

  return { config, secrets };
};

export interface ModuleConfigForm {
  form: UseFormReturn<ConfigFormValues>;
  defaults: ConfigFormValues;
}

/**
 * One form instance for an entire module. The rail selects which slice is
 * rendered; it never remounts the form, so unsaved edits survive moving between
 * sections and the save bar can report them in aggregate.
 */
export const useModuleConfigForm = (
  schema: ConfigField[],
  configValues: Record<string, string> | undefined
): ModuleConfigForm => {
  const defaults = buildDefaults(schema, configValues);
  const form = useForm<ConfigFormValues>({
    defaultValues: defaults,
    resolver: yupResolver(buildYupSchema(schema)) as never,
    mode: 'onChange'
  });
  return { form, defaults };
};
```

- [ ] **Step 4: Run the tests to verify they pass**

```bash
cd frontend-admin && npx vitest run src/pages/admin/modules/useModuleConfigForm.test.ts
```

Expected: PASS.

- [ ] **Step 5: Typecheck and lint**

```bash
cd frontend-admin && npm run typecheck && npm run lint
```

- [ ] **Step 6: Commit**

```bash
git add frontend-admin/src/pages/admin/modules/useModuleConfigForm.ts \
        frontend-admin/src/pages/admin/modules/useModuleConfigForm.test.ts
git commit -m "feat(admin): build the module config form from backend metadata

One react-hook-form instance per module, with the validation schema generated
from the fields the backend declares rather than hand-written per module.

The three load-bearing rules live in pure functions so they are testable
without a DOM: a hidden field is never validated (validating one makes the
form unsavable with no visible cause), a hidden field never enters the diff in
either direction (switching a provider off must not discard its stored client
secret), and a secret always seeds empty because the backend sends only
whether one exists, never the value."
```

---

### Task 2: the rail and the panel

Replaces `ModuleConfigSection`'s horizontal tabs with a vertical rail plus a panel,
rendering nested groups. Still inside the existing card — the full-page rail is Task 4.

**Files:**
- Create: `frontend-admin/src/pages/admin/modules/detail/ModuleConfigRail.tsx`
- Create: `frontend-admin/src/pages/admin/modules/detail/ModuleConfigPanel.tsx`
- Modify: `frontend-admin/src/pages/admin/modules/detail/ModuleConfigSection.tsx`
- Modify: `frontend-admin/src/locales/en.json`, `it.json`
- Test: `frontend-admin/src/pages/admin/modules/detail/ModuleConfigSection.test.tsx` (extend)

**Interfaces:**
- Consumes: `GroupNode` / `buildGroupTree` from `../configModel`; `translateConfigGroup`
  from `helpers/configLabel`.
- Produces:
  - `ModuleConfigRail({ tree, moduleName, activeKey, onSelect, statusFor })` where
    `onSelect: (key: string) => void` and
    `statusFor: (node: GroupNode) => { unfilled: number }` — the count of that node's
    **visible** required fields still empty, driving the warning badge. Pass
    `() => ({ unfilled: 0 })` from any caller that does not compute it.
  - `ModuleConfigPanel({ node, moduleName, schema, ...fieldProps })`
  - `flattenTree(tree: GroupNode[]): GroupNode[]` exported from `configModel.ts` —
    depth-first, parents before children; used to resolve `?section=` and to find the
    first selectable node.

- [ ] **Step 1: Add the locale keys**

`en.json`, under `adminModules.detail`:

```json
      "rail": {
        "overview": "Overview",
        "configuration": "Configuration",
        "module": "Module",
        "dependencies": "Dependencies",
        "environments": "Environments",
        "advancedToggle_one": "Advanced ({{count}})",
        "advancedToggle_other": "Advanced ({{count}})",
        "settingsCount_one": "{{count}} setting",
        "settingsCount_other": "{{count}} settings",
        "incomplete": "{{count}} to fill"
      }
```

`it.json`, same path:

```json
      "rail": {
        "overview": "Panoramica",
        "configuration": "Configurazione",
        "module": "Modulo",
        "dependencies": "Dipendenze",
        "environments": "Ambienti",
        "advancedToggle_one": "Avanzate ({{count}})",
        "advancedToggle_other": "Avanzate ({{count}})",
        "settingsCount_one": "{{count}} impostazione",
        "settingsCount_other": "{{count}} impostazioni",
        "incomplete": "{{count}} da compilare"
      }
```

- [ ] **Step 2: Export `flattenTree` from `configModel.ts`**

```ts
/**
 * Depth-first flattening, parents before their children. Used to resolve a
 * `?section=` value against the tree and to pick the first selectable node.
 */
export const flattenTree = (tree: GroupNode[]): GroupNode[] => {
  const out: GroupNode[] = [];
  const walk = (nodes: GroupNode[]) => {
    for (const n of nodes) {
      out.push(n);
      walk(n.children);
    }
  };
  walk(tree);
  return out;
};
```

Add a test to `configModel.test.ts`:

```ts
describe('flattenTree', () => {
  it('returns parents before children, depth-first', () => {
    const schema = [
      field({ key: 'a', group: 'oauth' }),
      field({ key: 'b', group: 'oauth.google' }),
      field({ key: 'c', group: 'password' })
    ];
    const groups = [
      { key: 'oauth', label: 'OAuth', order: 1 },
      { key: 'oauth.google', label: 'Google', parent: 'oauth', order: 2 },
      { key: 'password', label: 'Password', order: 3 }
    ];
    expect(flattenTree(buildGroupTree(schema, groups)).map(n => n.key)).toEqual([
      'oauth',
      'oauth.google',
      'password'
    ]);
  });
});
```

- [ ] **Step 3: Write the failing component test**

Append to `ModuleConfigSection.test.tsx`:

```tsx
  it('renders a vertical rail with nested groups and switches panel on click', async () => {
    const user = userEvent.setup();
    const mod = moduleWith(
      [
        field({ key: 'toggle', label: 'Enable Google', group: 'oauth' }),
        field({ key: 'clientId', label: 'Client ID', group: 'oauth.google' }),
        field({ key: 'minLen', label: 'Minimum length', group: 'password' })
      ],
      {
        configGroups: [
          { key: 'oauth', label: 'OAuth Providers', order: 1 },
          { key: 'oauth.google', label: 'Google', parent: 'oauth', order: 2 },
          { key: 'password', label: 'Password Policy', order: 3 }
        ]
      }
    );
    renderWithProviders(
      <ModuleConfigSection module={mod} selectedEnvironment="production" />
    );

    // Every group, including the nested child, is reachable from the rail.
    expect(screen.getByRole('button', { name: 'OAuth Providers' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Google' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Password Policy' })).toBeInTheDocument();

    // First node selected by default.
    expect(screen.getByText('Enable Google')).toBeInTheDocument();
    expect(screen.queryByText('Client ID')).not.toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: 'Google' }));
    expect(screen.getByText('Client ID')).toBeInTheDocument();
    expect(screen.queryByText('Enable Google')).not.toBeInTheDocument();
  });

  it('collapses advanced fields behind a toggle', async () => {
    const user = userEvent.setup();
    const mod = moduleWith(
      [
        field({ key: 'plain', label: 'Plain', group: 'g1' }),
        field({ key: 'rare', label: 'Rare', group: 'g1', advanced: true })
      ],
      {
        configGroups: [
          { key: 'g1', label: 'Group One', order: 1 },
          { key: 'g2', label: 'Group Two', order: 2 }
        ]
      }
    );
    renderWithProviders(
      <ModuleConfigSection module={mod} selectedEnvironment="production" />
    );
    expect(screen.getByText('Plain')).toBeInTheDocument();
    expect(screen.queryByText('Rare')).not.toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: /Advanced \(1\)/ }));
    expect(screen.getByText('Rare')).toBeInTheDocument();
  });

  it('keeps the flat form when the module declares no groups', () => {
    // The degradation path — every module served today, and every un-migrated
    // fork addon, takes it.
    const mod = moduleWith([
      field({ key: 'a', label: 'Alpha', group: 'Google' }),
      field({ key: 'b', label: 'Beta', group: 'Apple' })
    ]);
    renderWithProviders(
      <ModuleConfigSection module={mod} selectedEnvironment="production" />
    );
    expect(screen.getByRole('button', { name: 'Google' })).toBeInTheDocument();
    expect(screen.getByText('Alpha')).toBeInTheDocument();
  });
```

Add `import userEvent from '@testing-library/user-event';` to the file's imports.

- [ ] **Step 4: Run the test to verify it fails**

```bash
cd frontend-admin && npx vitest run src/pages/admin/modules/detail/ModuleConfigSection.test.tsx
```

Expected: the nested-rail and advanced-toggle tests FAIL; the degradation test passes.

- [ ] **Step 5: Write `ModuleConfigRail.tsx`**

A `Nav` in `flex-column`, one entry per node, children indented one level. Each entry is a
`Nav.Link` carrying the node's translated label and, when the group has unfilled required
fields, a `SubtleBadge bg="warning"` with the count from `statusFor(node)`.

Requirements to hold:
- entries render as `role="button"` — do **not** add `role="tablist"`. Phase 2 removed a
  half-built tablist (no `tabpanel`, no `aria-controls`, dead arrow keys) and the
  regression test in `ModuleConfigSection.test.tsx` guards it. A correct tablist needs the
  panel wired too; if you want one, wire `id` / `aria-controls` / `role="tabpanel"` /
  `aria-labelledby` and roving `tabIndex` together, or leave buttons alone.
- every entry must be reachable by sequential Tab.
- the active entry gets `active` and `aria-current="true"`.

- [ ] **Step 6: Write `ModuleConfigPanel.tsx`**

Renders one group: translated label as the heading, translated description beneath it when
present, then that node's fields via `ModuleConfigFields` with `includeKeys={node.fieldKeys}`.

Fields carrying `advanced: true` are **excluded from the main list** and rendered inside a
`<Collapse>` under a toggle button labelled `rail.advancedToggle` with the count. The
toggle only renders when the group actually has advanced fields, and the count must be of
*visible* advanced fields (a hidden one must not inflate it).

- [ ] **Step 7: Rewire `ModuleConfigSection.tsx`**

Replace the tab `Nav` + per-tab body with `<Row>` → `<Col md={4} lg={3}>` rail,
`<Col>` panel. Keep the existing `showTabs`-equivalent decision: fewer than 2 top-level
nodes → render the flat form exactly as today, no rail, no panel.

The active node stays local `useState` in this task; Task 4 lifts it to `?section=`.

- [ ] **Step 8: Run the tests and the full gate**

```bash
cd frontend-admin && npx vitest run src/pages/admin/modules/ && npm run typecheck && npm run lint && npm run test
```

- [ ] **Step 9: Commit**

```bash
git add frontend-admin/src/pages/admin/modules/ frontend-admin/src/locales/
git commit -m "feat(admin): navigate module config through a vertical rail

Horizontal tabs cannot express the group tree: a nested group renders as a
sibling of its parent, and 11 of them wrap. Worse, since phase 2 only rendered
top-level fieldKeys, a nested group's fields were unreachable entirely — which
is what blocks migrating auth.

The rail renders the tree at any depth and the panel renders one node at a
time, with rarely-touched fields collapsed behind an Advanced toggle whose
count excludes fields hidden by their own conditions.

A module declaring no groups still renders the flat form, unchanged. That is
every module served today and every un-migrated fork addon."
```

---

### Task 3: the sticky save bar and the cross-group save model

**Files:**
- Create: `frontend-admin/src/pages/admin/modules/detail/ModuleSaveBar.tsx`
- Modify: `frontend-admin/src/pages/admin/modules/detail/ModuleConfigSection.tsx`
- Modify: `frontend-admin/src/pages/admin/modules/ModuleConfigFields.tsx` (RHF registration)
- Modify: `frontend-admin/src/locales/en.json`, `it.json`
- Create: `frontend-admin/src/assets/scss/theme/_module-save-bar.scss`, registered with
  `@import 'theme/module-save-bar';` appended to `frontend-admin/src/assets/scss/theme.scss`
  (that file already ends with `@import 'theme/stat-card';` — follow it exactly)
- Test: `ModuleConfigSection.test.tsx` (extend)

**Interfaces:**
- Consumes: `useModuleConfigForm`, `collectDiff` from Task 1; `GroupNode` from `configModel`.
- Produces: `ModuleSaveBar({ dirtyCount, perGroup, errors, saving, onDiscard, onSave })`.

- [ ] **Step 1: Add the locale keys**

`en.json` under `adminModules.detail`:

```json
      "saveBar": {
        "changes_one": "{{count}} unsaved change",
        "changes_other": "{{count}} unsaved changes",
        "perGroup": "{{group}} ({{count}})",
        "errors_one": "{{count}} field needs attention",
        "errors_other": "{{count}} fields need attention",
        "goToError": "Go to {{group}}"
      }
```

`it.json`:

```json
      "saveBar": {
        "changes_one": "{{count}} modifica non salvata",
        "changes_other": "{{count}} modifiche non salvate",
        "perGroup": "{{group}} ({{count}})",
        "errors_one": "{{count}} campo da correggere",
        "errors_other": "{{count}} campi da correggere",
        "goToError": "Vai a {{group}}"
      }
```

Also add the four validation messages the resolver emits as codes (`required`,
`duration`, `min:N`, `max:N`, `pattern`, `notANumber`) — reuse the existing
`adminModules.configFields.*Feedback` keys rather than adding parallel ones.

- [ ] **Step 2: Write the failing test**

```tsx
  it('accumulates unsaved changes across two different groups', async () => {
    const user = userEvent.setup();
    const mod = moduleWith(
      [
        field({ key: 'a', label: 'Alpha', group: 'g1' }),
        field({ key: 'b', label: 'Beta', group: 'g2' })
      ],
      {
        configGroups: [
          { key: 'g1', label: 'Group One', order: 1 },
          { key: 'g2', label: 'Group Two', order: 2 }
        ]
      }
    );
    renderWithProviders(
      <ModuleConfigSection module={mod} selectedEnvironment="production" />
    );

    await user.type(screen.getByLabelText('Alpha'), 'x');
    await user.click(screen.getByRole('button', { name: 'Group Two' }));
    await user.type(screen.getByLabelText('Beta'), 'y');

    // One bar, both groups counted — this is what the per-card form could not do.
    expect(await screen.findByText(/2 unsaved changes/)).toBeInTheDocument();
    expect(screen.getByText(/Group One \(1\)/)).toBeInTheDocument();
    expect(screen.getByText(/Group Two \(1\)/)).toBeInTheDocument();
  });

  it('surfaces an error from a section that is not on screen', async () => {
    const user = userEvent.setup();
    const mod = moduleWith(
      [
        field({ key: 'n', label: 'Count', type: 'int', min: 8, group: 'g1' }),
        field({ key: 'b', label: 'Beta', group: 'g2' })
      ],
      {
        configGroups: [
          { key: 'g1', label: 'Group One', order: 1 },
          { key: 'g2', label: 'Group Two', order: 2 }
        ]
      }
    );
    renderWithProviders(
      <ModuleConfigSection module={mod} selectedEnvironment="production" />
    );

    await user.clear(screen.getByLabelText('Count'));
    await user.type(screen.getByLabelText('Count'), '3');
    await user.click(screen.getByRole('button', { name: 'Group Two' }));

    // The bad value is in a section the operator is no longer looking at.
    // Without this, save fails with no indication of where.
    const link = await screen.findByRole('button', { name: /Go to Group One/ });
    await user.click(link);
    expect(screen.getByLabelText('Count')).toBeInTheDocument();
  });
```

- [ ] **Step 3: Run to verify it fails**

```bash
cd frontend-admin && npx vitest run src/pages/admin/modules/detail/ModuleConfigSection.test.tsx
```

- [ ] **Step 4: Migrate `ModuleConfigFields` to RHF registration**

Replace the `configValues` / `secretValues` / `onConfigChange` / `onSecretChange` props
with a `control` from the form plus the existing `schema` / `includeKeys` / `moduleName` /
`secretStatus`. Each control uses RHF's `register` (or `Controller` for the bool switch),
and shows `fieldState.error` translated through the existing `adminModules.configFields.*`
keys, mapping the resolver's message codes (`required`, `duration`, `min:N`, `max:N`,
`pattern`, `notANumber`).

Keep everything phase 2 established: labels/descriptions through
`translateConfigField(t, moduleName, field, …)`, guards on the **resolved** description,
`id={`cfg-${key}`}` + `htmlFor` on all six branches, `isFieldVisible` filtering.

- [ ] **Step 5: Write `ModuleSaveBar.tsx`**

A bar pinned to the bottom of the config surface. Contents: the aggregated change count,
a per-group breakdown, any validation errors with a button per offending group that calls
`onSelect(groupKey)`, then Discard and Save.

Styling: add `.module-save-bar { position: sticky; bottom: 0; z-index: 5; }` to the app's
SCSS, using theme variables for background and border so it works in light and dark mode —
**no hex codes, no inline colour styles**. Verify it does not cover the last form field at
narrow widths.

- [ ] **Step 6: Wire the save model in `ModuleConfigSection`**

`handleSubmit` → `collectDiff` → the existing `updateEnv` mutation, unchanged payload
shape. On success, `form.reset(currentValues)` so the bar clears without a refetch.
`useBlocker` now takes `formState.isDirty`.

- [ ] **Step 7: Full gate**

```bash
cd frontend-admin && npm run typecheck && npm run lint && npm run test
```

- [ ] **Step 8: Commit**

```bash
git add frontend-admin/src/pages/admin/modules/ frontend-admin/src/locales/ frontend-admin/src/assets/
git commit -m "feat(admin): save module config across groups from one sticky bar

Editing two groups meant two round-trips, because each tab owned its own
useState form. One react-hook-form instance now spans the module: the rail
picks the visible slice, the bar reports every unsaved change with a per-group
breakdown, and one save sends them all.

Validation errors in a section the operator is not looking at get a button
that navigates there. Without it a failed save gives no indication of where
the bad value is.

The payload is unchanged — changed non-secret keys plus non-empty secrets —
so the behaviour that fixed UpdateConfig wiping secrets is untouched."
```

---

### Task 4: full-page rail, URL sync, and the Overview section

**Files:**
- Modify: `frontend-admin/src/pages/admin/modules/detail/index.tsx`
- Rename: `ModuleDashboardCards.tsx` → `ModuleOverviewPanel.tsx`
- Modify: `ModuleConfigRail.tsx` (non-config sections)
- Test: Create `frontend-admin/src/pages/admin/modules/detail/index.test.tsx`

**Interfaces:**
- Consumes: everything from Tasks 1–3.
- Produces: the two-column page. `?section=` is the single source of truth for the active
  entry.

- [ ] **Step 1: Write the failing test**

Create `frontend-admin/src/pages/admin/modules/detail/index.test.tsx`:

```tsx
import { describe, it, expect, beforeEach } from 'vitest';
import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { Route, Routes, useLocation } from 'react-router';
import { renderWithProviders } from 'test/render';
import { server } from 'test/server';
import type { ConfigField, ModuleConfig } from 'store/api/moduleApi';
import ModuleDetailPage from './index';

const field = (over: Partial<ConfigField> & { key: string }): ConfigField => ({
  label: over.key,
  description: '',
  type: 'string',
  required: false,
  default: '',
  envVar: '',
  ...over
});

const demoModule: ModuleConfig = {
  moduleName: 'demo',
  displayName: 'Demo',
  description: '',
  category: 'toggleable',
  enabled: true,
  status: 'running',
  needsRestart: false,
  configValues: {},
  secretStatus: {},
  configSchema: [
    field({ key: 'toggle', label: 'Enable Google', group: 'oauth' }),
    field({ key: 'clientId', label: 'Client ID', group: 'oauth.google' }),
    field({ key: 'minLen', label: 'Minimum length', group: 'password' })
  ],
  configGroups: [
    { key: 'oauth', label: 'OAuth Providers', order: 1 },
    { key: 'oauth.google', label: 'Google', parent: 'oauth', order: 2 },
    { key: 'password', label: 'Password Policy', order: 3 }
  ],
  dependsOn: [],
  providedServices: [],
  requiredServices: [],
  optionalServices: [],
  activeEnvironment: 'production',
  availableEnvironments: ['production', 'sandbox'],
  createdAt: '',
  updatedAt: ''
} as ModuleConfig;

// All four endpoints the page touches. MSW runs with
// onUnhandledRequest: 'error', so a missing stub fails the suite with an
// error that looks unrelated to any assertion.
const stubAll = (mod: ModuleConfig = demoModule) => {
  server.use(
    http.get('*/v1/admin/modules', () => HttpResponse.json({ modules: [mod] })),
    http.get('*/v1/admin/modules/health', () =>
      HttpResponse.json({ modules: [{ moduleName: 'demo', status: 'healthy' }] })
    ),
    http.get('*/v1/admin/modules/:name', () => HttpResponse.json(mod)),
    http.get('*/v1/admin/modules/:name/environments/:env', () =>
      HttpResponse.json({
        environment: 'production',
        configValues: {},
        secretStatus: {},
        updatedAt: ''
      })
    )
  );
};

// renderWithProviders wraps in a MemoryRouter, so the URL lives in memory and
// `window.location` never changes. This probe is how the tests read it.
let currentSearch = '';
const LocationProbe = () => {
  currentSearch = useLocation().search;
  return null;
};

const renderAt = (search: string) =>
  renderWithProviders(
    <>
      <LocationProbe />
      <Routes>
        <Route path="/admin/modules/:moduleName" element={<ModuleDetailPage />} />
      </Routes>
    </>,
    { routerEntries: [`/admin/modules/demo${search}`] }
  );

describe('ModuleDetailPage sections', () => {
  beforeEach(() => stubAll());

  it('opens the section named in ?section= on load', async () => {
    renderAt('?section=password');
    expect(await screen.findByText('Minimum length')).toBeInTheDocument();
    expect(screen.queryByText('Client ID')).not.toBeInTheDocument();
  });

  it('reaches a nested group from the rail and reflects it in the URL', async () => {
    const user = userEvent.setup();
    renderAt('');
    await user.click(await screen.findByRole('button', { name: 'Google' }));
    expect(screen.getByText('Client ID')).toBeInTheDocument();
    expect(currentSearch).toContain('section=oauth.google');
  });

  it('falls back to Overview when ?section= names an unknown key', async () => {
    // A stale bookmark or a renamed group must not render an empty page.
    renderAt('?section=this-group-was-renamed');
    expect(await screen.findByRole('button', { name: /Overview/ })).toHaveAttribute(
      'aria-current',
      'true'
    );
  });

  it('keeps an unsaved edit when moving between sections', async () => {
    // Same route, one shared form — switching section is not a navigation, so
    // the edit survives and useBlocker must stay quiet. This is the whole
    // point of one form per module.
    const user = userEvent.setup();
    renderAt('?section=oauth.google');
    await user.type(await screen.findByLabelText('Client ID'), 'abc123');

    await user.click(screen.getByRole('button', { name: 'Password Policy' }));
    expect(screen.getByText('Minimum length')).toBeInTheDocument();
    expect(screen.queryByText(/unsaved changes\?/i)).not.toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: 'Google' }));
    expect(screen.getByLabelText('Client ID')).toHaveValue('abc123');
  });

  it('renders the stacked page unchanged when the module declares no groups', async () => {
    // The degradation path — every module served today.
    stubAll({ ...demoModule, configGroups: undefined } as ModuleConfig);
    renderAt('');
    expect(await screen.findByText('Enable Google')).toBeInTheDocument();
    expect(
      screen.queryByRole('button', { name: /Overview/ })
    ).not.toBeInTheDocument();
  });
});
```

**Two test-infra facts that will bite otherwise.** `renderWithProviders` (verified in
`src/test/render.tsx:59-65`) accepts `routerEntries` and wraps in a plain `MemoryRouter`:

1. It is **not a data router**, so `useBlocker` throws. `ModuleConfigSection.test.tsx`
   already works around this with a local `vi.mock('react-router')` that spreads
   `importOriginal()` and overrides only `useBlocker`. Once the form (and therefore the
   blocker) moves into `detail/index.tsx` in this task, this file needs the same mock.
   Copy that pattern rather than inventing a second one, and keep spreading the original —
   `useLocation`, `Routes` and `Route` above all come from the same module.
2. The URL lives in memory, so `window.location.search` never changes. That is what
   `LocationProbe` is for; do not assert on `window.location`.

- [ ] **Step 2: Restructure `detail/index.tsx`**

`<Row>` → rail `<Col md={4} lg={3}>` / content `<Col>`. Rail sections in order:
`Overview`, then a `Configuration` caption with the group tree, then a `Module` caption
with `Dependencies` and `Environments` (the latter only when the module has more than one
environment). Content renders the matching panel.

`ModuleDetailHeader` stays above the two columns.

- [ ] **Step 3: `?section=` wiring**

```ts
const [searchParams, setSearchParams] = useSearchParams();
const requested = searchParams.get('section') ?? '';
const selectable = ['overview', ...flattenTree(groupTree).map(n => n.key),
                    'dependencies', 'environments'];
const active = selectable.includes(requested) ? requested : 'overview';
```

Selecting an entry calls `setSearchParams(prev => …, { replace: true })` — `replace` so a
settings tour does not fill the browser history.

- [ ] **Step 4: Rename `ModuleDashboardCards` → `ModuleOverviewPanel`**

`git mv` to preserve history. Update the import. The KPI row is unchanged — health stays
here; there is no separate health section (see "Scope decision recorded").

- [ ] **Step 5: Degradation**

Fewer than 2 top-level config nodes → **no rail**: render today's stacked page
(header → KPIs → environment switcher → config card → dependencies). This is the path
every module currently takes; a regression here hits every live install.

- [ ] **Step 6: Full gate**

```bash
cd frontend-admin && npm run typecheck && npm run lint && npm run test
```

- [ ] **Step 7: Commit**

```bash
git add -A frontend-admin/src/pages/admin/modules/
git commit -m "feat(admin): make the rail navigate the whole module page

The rail now spans Overview, the configuration groups, Dependencies and
Environments rather than only the config card, so a long group no longer
scrolls its own navigation off the top of the page.

The active entry lives in ?section=, sanitised against the tree, so a section
is linkable and survives a reload. Switching section is not a navigation — the
form is shared — so unsaved edits persist and the blocker stays quiet.

A module with fewer than two top-level groups keeps the stacked page it has
today."
```

---

### Task 5: documentation

**Files:**
- Modify: `frontend-admin/CLAUDE.md`
- Modify: `docs/plans/module-config-ux.md` (§8 phase 3 → ✅)
- Modify: `backend/pkg/sdk/CLAUDE.md` (the `Advanced` flag is now honoured)

- [ ] **Step 1: `frontend-admin/CLAUDE.md`**

Extend the "Module config labels" section with the rail: how a module's `ConfigGroups()`
becomes the settings rail, that nesting is rendered at any depth, that `Advanced: true`
collapses a field, and that declaring fewer than two top-level groups keeps the flat form.

- [ ] **Step 2: `backend/pkg/sdk/CLAUDE.md`**

Note in the `module/` section that `ConfigField.Advanced` and `ConfigField.DependsOn` are
now honoured by the operator console, so an addon declaring them gets the behaviour
without frontend work.

- [ ] **Step 3: Commit**

```bash
git add frontend-admin/CLAUDE.md backend/pkg/sdk/CLAUDE.md docs/plans/module-config-ux.md
git commit -m "docs: describe the module settings rail for addon authors

An addon author declaring ConfigGroups() needs to know what the console does
with it — nesting at any depth, Advanced collapsing a field, and fewer than
two top-level groups keeping the flat form."
```

---

## Phase exit criteria

- `npm run typecheck`, `npm run lint`, `npm run test` clean.
- A module declaring **no** `configGroups` renders exactly as it does today — no rail, same
  stacked page. This is every module currently served; verify against the running stack,
  not only in tests.
- A module declaring a **nested** tree renders every group at every depth, and each group's
  fields are reachable. This is the gate that unblocks phase 4.
- Editing fields in two different groups produces one save.
- `?section=` round-trips: linkable, reload-safe, and an unknown value falls back rather
  than blanking the page.

Update [`module-config-ux.md`](module-config-ux.md) §8: set phase 3 to ✅.
