# Module Config UX — Phase 2 (frontend model layer) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Teach the operator console to read the group tree, conditional visibility, and
validation metadata that phase 1 added to the API — with **no visual change** to any page.

**Architecture:** Three pure, separately-testable units — a model layer (`configModel.ts`:
group tree, field visibility, completeness), an i18n resolver (`helpers/configLabel.ts`,
twinned with the existing `helpers/navLabel.ts`), and declarative validation inside the
existing `ModuleConfigFields`. Each is wired into the *current* UI as it lands, so nothing
ships as dead code. The form stays on `useState`; the react-hook-form migration belongs to
phase 3, alongside the cross-group save bar that is what justifies it (parent spec §4.3).

**Tech Stack:** React 19, TypeScript 5.9 strict, RTK Query, react-i18next, Vitest +
React Testing Library + MSW.

**Parent spec:** [`module-config-ux.md`](module-config-ux.md) §2.3, §2.4, §2.5, §4.

```
Pre-flight:
- Production precedent: src/pages/admin/modules/ModuleConfigFields.tsx,
                        src/pages/admin/modules/detail/ModuleConfigSection.tsx,
                        src/pages/admin/modules/detail/ModuleDashboardCards.tsx,
                        src/pages/admin/navigation/index.tsx
- Reference read:       src/reference/components/navigation/Navs.tsx
- Primitives:           react-bootstrap Form.* (Control / Select / Check / Control.Feedback),
                        OrkestraCardHeader, SubtleBadge, StatCard
```

## Global Constraints

- **No visual change.** No core module declares `configGroups` or `dependsOn` yet, so every
  page must render byte-identically to today. Any task that changes what an operator sees
  with current API data has failed.
- Bare path aliases only (`store/api/moduleApi`, `helpers/configLabel`) — never `@/`, never
  a relative climb like `../../../`.
- Server state stays in RTK Query. No new fetching layer.
- Every user-visible string goes through `t()`. Every new key must be added to **both**
  `src/locales/en.json` and `src/locales/it.json` or the parity test fails.
- TypeScript strict: `npm run typecheck` must pass. `npm run lint` runs with
  `--max-warnings 0`.
- Vitest exits non-zero on an **unhandled MSW request** even when every assertion passes.
  Any test that renders a page must stub every endpoint that page touches.
- Tests live next to the unit under test, matching the existing convention
  (`src/modules/useModuleI18n.test.tsx`, `src/i18n.test.ts`).

---

### Task 1: API types + `configModel.ts`

**Files:**
- Modify: `frontend-admin/src/store/api/moduleApi.ts:5-22` (`ConfigField`), `:31-52` (`ModuleConfig`)
- Create: `frontend-admin/src/pages/admin/modules/configModel.ts`
- Test: `frontend-admin/src/pages/admin/modules/configModel.test.ts`

**Interfaces:**
- Consumes: the phase-1 API shape — `GET /v1/admin/modules/{name}` now returns
  `configGroups?: ConfigGroup[]` alongside `configSchema`, and each `ConfigField` may carry
  `advanced`, `dependsOn`, `min`, `max`, `pattern`, `placeholder`, `helpUrl`.
- Produces:
  - `interface FieldCondition { key: string; in: string[] }`
  - `interface ConfigGroup { key: string; label: string; description?: string; icon?: string; parent?: string; order?: number }`
  - `interface GroupNode { key: string; label: string; description?: string; icon?: string; fieldKeys: string[]; children: GroupNode[] }`
  - `buildGroupTree(schema, groups): GroupNode[]`
  - `isFieldVisible(field, values, schema): boolean`
  - `visibleFields(schema, values): ConfigField[]`
  - `configCompleteness(schema, values, secretStatus): { filled: number; total: number }`

- [ ] **Step 1: Extend the API types**

In `frontend-admin/src/store/api/moduleApi.ts`, add above `ConfigField`:

```ts
/**
 * Gates a field's visibility on the value of another field of the SAME module.
 * AND across a field's `dependsOn` array, OR within one condition's `in` list.
 *
 * Matching is type-aware, resolved against the *referenced* field's `type` —
 * this mirrors the contract documented on the backend's `FieldCondition`, and
 * the two implementations must not drift:
 *   - `bool` target: both sides are read as booleans, where `true` / `1` / `yes`
 *     (case-insensitive, trimmed) are true. So `in: ['true']` matches a stored `'1'`.
 *   - any other type: case-insensitive, whitespace-trimmed exact string match.
 */
export interface FieldCondition {
  key: string;
  in: string[];
}

/** One section of the settings rail. Presentation-only; never persisted. */
export interface ConfigGroup {
  key: string;
  label: string;
  description?: string;
  icon?: string;
  parent?: string;
  order?: number;
}
```

Extend `ConfigField` (keep every existing member unchanged):

```ts
  options?: string[];
  advanced?: boolean;
  dependsOn?: FieldCondition[];
  min?: number;
  max?: number;
  pattern?: string;
  placeholder?: string;
  helpUrl?: string;
```

Add to `ModuleConfig`, directly below `configSchema`:

```ts
  configSchema: ConfigField[];
  configGroups?: ConfigGroup[];
```

- [ ] **Step 2: Write the failing tests**

Create `frontend-admin/src/pages/admin/modules/configModel.test.ts`:

```ts
import { describe, it, expect } from 'vitest';
import type { ConfigField, ConfigGroup } from 'store/api/moduleApi';
import {
  buildGroupTree,
  isFieldVisible,
  visibleFields,
  configCompleteness
} from './configModel';

const field = (over: Partial<ConfigField> & { key: string }): ConfigField => ({
  label: over.key,
  description: '',
  type: 'string',
  required: false,
  default: '',
  envVar: '',
  ...over
});

describe('buildGroupTree — legacy modules (no declared groups)', () => {
  // Every module in the tree today is in this state: `group` holds a display
  // label and no `configGroups` is returned. The tree must reproduce exactly
  // what the old bucketByGroup produced, or the tabs visibly change.
  it('buckets by declaration order and trails ungrouped fields under General', () => {
    const schema = [
      field({ key: 'a', group: 'Google' }),
      field({ key: 'b' }),
      field({ key: 'c', group: 'Google' }),
      field({ key: 'd', group: 'Apple' })
    ];
    const tree = buildGroupTree(schema, undefined);
    expect(tree.map(n => n.key)).toEqual(['Google', 'Apple', 'General']);
    expect(tree.map(n => n.label)).toEqual(['Google', 'Apple', 'General']);
    expect(tree[0].fieldKeys).toEqual(['a', 'c']);
    expect(tree[2].fieldKeys).toEqual(['b']);
    expect(tree.every(n => n.children.length === 0)).toBe(true);
  });

  it('returns an empty tree for an empty schema', () => {
    expect(buildGroupTree([], undefined)).toEqual([]);
  });
});

describe('buildGroupTree — declared groups', () => {
  const groups: ConfigGroup[] = [
    { key: 'oauth.google', label: 'Google', parent: 'oauth', order: 2 },
    { key: 'oauth', label: 'OAuth Providers', order: 1 },
    { key: 'password', label: 'Password Policy', order: 3 }
  ];
  const schema = [
    field({ key: 'googleId', group: 'oauth.google' }),
    field({ key: 'googleOn', group: 'oauth' }),
    field({ key: 'minLen', group: 'password' })
  ];

  it('nests children under their parent and sorts by order', () => {
    const tree = buildGroupTree(schema, groups);
    expect(tree.map(n => n.key)).toEqual(['oauth', 'password']);
    expect(tree[0].children.map(n => n.key)).toEqual(['oauth.google']);
    expect(tree[0].fieldKeys).toEqual(['googleOn']);
    expect(tree[0].children[0].fieldKeys).toEqual(['googleId']);
  });

  it('keeps an ungrouped field reachable instead of dropping it', () => {
    // The backend validator rejects this, but the UI must never make a field
    // unreachable if a non-compliant module slips through.
    const tree = buildGroupTree([...schema, field({ key: 'orphan' })], groups);
    const general = tree.find(n => n.key === 'General');
    expect(general?.fieldKeys).toEqual(['orphan']);
  });

  it('ignores a child whose parent was never declared', () => {
    const tree = buildGroupTree(
      [field({ key: 'x', group: 'ghostchild' })],
      [{ key: 'ghostchild', label: 'Ghost child', parent: 'nowhere' }]
    );
    // Promoted to top level rather than lost.
    expect(tree.map(n => n.key)).toEqual(['ghostchild']);
    expect(tree[0].fieldKeys).toEqual(['x']);
  });
});

describe('isFieldVisible', () => {
  const schema = [
    field({ key: 'googleOn', type: 'bool', default: 'false' }),
    field({ key: 'provider', type: 'enum', default: 'noop', options: ['noop', 'smtp'] }),
    field({ key: 'clientId', dependsOn: [{ key: 'googleOn', in: ['true'] }] }),
    field({ key: 'smtpHost', dependsOn: [{ key: 'provider', in: ['smtp'] }] })
  ];

  it('is visible when the field declares no condition', () => {
    expect(isFieldVisible(schema[0], {}, schema)).toBe(true);
  });

  it('reads a bool target through bool semantics, not string equality', () => {
    // The backend seeds the raw env string, so "1" is what an operator who set
    // GOOGLE_ENABLED=1 actually has stored. Exact string match would hide the
    // field forever.
    const f = schema[2];
    expect(isFieldVisible(f, { googleOn: 'true' }, schema)).toBe(true);
    expect(isFieldVisible(f, { googleOn: '1' }, schema)).toBe(true);
    expect(isFieldVisible(f, { googleOn: 'YES' }, schema)).toBe(true);
    expect(isFieldVisible(f, { googleOn: 'false' }, schema)).toBe(false);
    expect(isFieldVisible(f, { googleOn: '' }, schema)).toBe(false);
  });

  it("falls back to the target's default when no value is stored", () => {
    const s = [
      field({ key: 'on', type: 'bool', default: 'true' }),
      field({ key: 'dep', dependsOn: [{ key: 'on', in: ['true'] }] })
    ];
    expect(isFieldVisible(s[1], {}, s)).toBe(true);
  });

  it('matches non-bool targets case-insensitively and trimmed', () => {
    const f = schema[3];
    expect(isFieldVisible(f, { provider: 'smtp' }, schema)).toBe(true);
    expect(isFieldVisible(f, { provider: ' SMTP ' }, schema)).toBe(true);
    expect(isFieldVisible(f, { provider: 'noop' }, schema)).toBe(false);
  });

  it('ANDs across conditions and ORs within one condition', () => {
    const s = [
      field({ key: 'a', type: 'bool', default: 'false' }),
      field({ key: 'b', type: 'enum', default: '', options: ['x', 'y', 'z'] }),
      field({
        key: 'dep',
        dependsOn: [
          { key: 'a', in: ['true'] },
          { key: 'b', in: ['x', 'y'] }
        ]
      })
    ];
    expect(isFieldVisible(s[2], { a: 'true', b: 'y' }, s)).toBe(true);
    expect(isFieldVisible(s[2], { a: 'true', b: 'z' }, s)).toBe(false);
    expect(isFieldVisible(s[2], { a: 'false', b: 'x' }, s)).toBe(false);
  });

  it('treats a condition on an unknown field as unsatisfied', () => {
    const f = field({ key: 'dep', dependsOn: [{ key: 'nope', in: ['true'] }] });
    expect(isFieldVisible(f, {}, [f])).toBe(false);
  });
});

describe('configCompleteness', () => {
  it('counts only required fields that are visible', () => {
    // Without this, `auth` reports a fraction that counts credentials belonging
    // to switched-off OAuth providers — a number that is simply false.
    const schema = [
      field({ key: 'googleOn', type: 'bool', default: 'false' }),
      field({
        key: 'clientId',
        required: true,
        dependsOn: [{ key: 'googleOn', in: ['true'] }]
      }),
      field({ key: 'always', required: true })
    ];
    expect(configCompleteness(schema, { always: 'set' }, {})).toEqual({
      filled: 1,
      total: 1
    });
    expect(
      configCompleteness(schema, { googleOn: 'true', always: 'set' }, {})
    ).toEqual({ filled: 1, total: 2 });
  });

  it('counts a stored secret as filled', () => {
    const schema = [field({ key: 's', type: 'secret', required: true })];
    expect(configCompleteness(schema, {}, { s: true })).toEqual({
      filled: 1,
      total: 1
    });
    expect(configCompleteness(schema, {}, { s: false })).toEqual({
      filled: 0,
      total: 1
    });
  });

  it('returns zeroes for a null schema', () => {
    expect(configCompleteness(null, {}, {})).toEqual({ filled: 0, total: 0 });
  });
});

describe('visibleFields', () => {
  it('drops hidden fields and preserves schema order', () => {
    const schema = [
      field({ key: 'on', type: 'bool', default: 'false' }),
      field({ key: 'hidden', dependsOn: [{ key: 'on', in: ['true'] }] }),
      field({ key: 'shown' })
    ];
    expect(visibleFields(schema, {}).map(f => f.key)).toEqual(['on', 'shown']);
    expect(visibleFields(schema, { on: 'true' }).map(f => f.key)).toEqual([
      'on',
      'hidden',
      'shown'
    ]);
  });
});
```

- [ ] **Step 3: Run the tests to verify they fail**

```bash
cd frontend-admin && npx vitest run src/pages/admin/modules/configModel.test.ts
```

Expected: FAIL — `Failed to resolve import "./configModel"`.

- [ ] **Step 4: Write `configModel.ts`**

Create `frontend-admin/src/pages/admin/modules/configModel.ts`:

```ts
import type { ConfigField, ConfigGroup } from 'store/api/moduleApi';

/** A node of the settings rail: one group, its own fields, and its children. */
export interface GroupNode {
  key: string;
  label: string;
  description?: string;
  icon?: string;
  fieldKeys: string[];
  children: GroupNode[];
}

const GENERAL = 'General';

/**
 * Values the backend's `parseBool` treats as true. Kept in lockstep with
 * `parseBool` in `pkg/sdk/module/config_unmarshal.go` — the config document
 * stores the raw env string, so an operator who set FOO=1 has "1" persisted,
 * and string equality against "true" would hide the dependent field forever.
 */
const TRUTHY = new Set(['true', '1', 'yes']);

const asBool = (raw: string): boolean =>
  TRUTHY.has(raw.trim().toLowerCase());

const normalize = (raw: string): string => raw.trim().toLowerCase();

/**
 * Builds the group tree for a module's settings surface.
 *
 * Two shapes, both supported on purpose:
 *
 *  - **Declared groups** (`groups` non-empty): nodes come from the declaration,
 *    ordered by `order` then declaration order, nested through `parent`, and
 *    fields are attached by `field.group === node.key`.
 *  - **Legacy** (`groups` empty or absent): `field.group` is a display label,
 *    so flat nodes are synthesized from the distinct labels in declaration
 *    order. This is the state of every module in the tree today and of every
 *    un-migrated fork addon, and it must reproduce the previous
 *    `bucketByGroup` output exactly or the rendered tabs change.
 *
 * A field whose group resolves to nothing lands in a trailing `General` node
 * rather than being dropped: the backend validator rejects that case, but a UI
 * that silently loses a field is worse than one with an extra bucket.
 */
export const buildGroupTree = (
  schema: ConfigField[] | null | undefined,
  groups: ConfigGroup[] | null | undefined
): GroupNode[] => {
  if (!schema || schema.length === 0) return [];

  const fieldsFor = (predicate: (f: ConfigField) => boolean): string[] =>
    schema.filter(predicate).map(f => f.key);

  if (!groups || groups.length === 0) {
    const order: string[] = [];
    const seen = new Set<string>();
    for (const f of schema) {
      const g = f.group || '';
      if (g && !seen.has(g)) {
        seen.add(g);
        order.push(g);
      }
    }
    const nodes: GroupNode[] = order.map(label => ({
      key: label,
      label,
      fieldKeys: fieldsFor(f => (f.group || '') === label),
      children: []
    }));
    const ungrouped = fieldsFor(f => !f.group);
    if (ungrouped.length > 0) {
      nodes.push({
        key: GENERAL,
        label: GENERAL,
        fieldKeys: ungrouped,
        children: []
      });
    }
    return nodes;
  }

  const declared = new Map<string, ConfigGroup>();
  for (const g of groups) {
    if (g.key && !declared.has(g.key)) declared.set(g.key, g);
  }

  const nodeOf = (g: ConfigGroup): GroupNode => ({
    key: g.key,
    label: g.label,
    description: g.description,
    icon: g.icon,
    fieldKeys: fieldsFor(f => f.group === g.key),
    children: []
  });

  const nodes = new Map<string, GroupNode>();
  const ordered = [...declared.values()].sort((a, b) => {
    const byOrder = (a.order ?? 0) - (b.order ?? 0);
    if (byOrder !== 0) return byOrder;
    return groups.indexOf(a) - groups.indexOf(b);
  });
  for (const g of ordered) nodes.set(g.key, nodeOf(g));

  const roots: GroupNode[] = [];
  for (const g of ordered) {
    const node = nodes.get(g.key)!;
    // A child whose parent was never declared is promoted rather than lost.
    const parent = g.parent ? nodes.get(g.parent) : undefined;
    if (parent) parent.children.push(node);
    else roots.push(node);
  }

  const orphans = fieldsFor(f => !f.group || !declared.has(f.group));
  if (orphans.length > 0) {
    roots.push({
      key: GENERAL,
      label: GENERAL,
      fieldKeys: orphans,
      children: []
    });
  }

  return roots;
};

/**
 * Whether a field should be rendered, given the module's current values.
 * AND across the field's conditions, OR within one condition's `in` list.
 * See `FieldCondition` in `store/api/moduleApi` for the matching contract this
 * implements — it is shared with the Go side and the two must not drift.
 */
export const isFieldVisible = (
  field: ConfigField,
  values: Record<string, string>,
  schema: ConfigField[]
): boolean => {
  if (!field.dependsOn || field.dependsOn.length === 0) return true;
  return field.dependsOn.every(cond => {
    const target = schema.find(f => f.key === cond.key);
    if (!target) return false;
    const stored = values[cond.key];
    const raw =
      stored !== undefined && stored !== '' ? stored : target.default || '';
    if (target.type === 'bool') {
      const actual = asBool(raw);
      return cond.in.some(v => asBool(v) === actual);
    }
    const actual = normalize(raw);
    return cond.in.some(v => normalize(v) === actual);
  });
};

/** The subset of the schema that is currently visible, in schema order. */
export const visibleFields = (
  schema: ConfigField[] | null | undefined,
  values: Record<string, string>
): ConfigField[] => {
  if (!schema) return [];
  return schema.filter(f => isFieldVisible(f, values, schema));
};

/**
 * How many required fields are filled. Counts **visible** fields only —
 * including hidden ones reports a fraction that counts, for example,
 * credentials belonging to a switched-off OAuth provider.
 */
export const configCompleteness = (
  schema: ConfigField[] | null | undefined,
  configValues: Record<string, string> | null | undefined,
  secretStatus: Record<string, boolean> | null | undefined
): { filled: number; total: number } => {
  if (!schema) return { filled: 0, total: 0 };
  const cv = configValues ?? {};
  const ss = secretStatus ?? {};
  const required = visibleFields(schema, cv).filter(f => f.required);
  let filled = 0;
  for (const f of required) {
    if (f.type === 'secret') {
      if (ss[f.key]) filled++;
    } else if (cv[f.key]) {
      filled++;
    }
  }
  return { filled, total: required.length };
};
```

- [ ] **Step 5: Run the tests to verify they pass**

```bash
cd frontend-admin && npx vitest run src/pages/admin/modules/configModel.test.ts
```

Expected: PASS (all describes).

- [ ] **Step 6: Typecheck and lint**

```bash
cd frontend-admin && npm run typecheck && npm run lint
```

Expected: both clean.

- [ ] **Step 7: Commit**

```bash
git add frontend-admin/src/store/api/moduleApi.ts \
        frontend-admin/src/pages/admin/modules/configModel.ts \
        frontend-admin/src/pages/admin/modules/configModel.test.ts
git commit -m "feat(admin): model the module config group tree and field visibility

Phase 1 taught the API to serve a group tree and per-field conditions; the
console could not read either. This adds the pure model layer: the tree
builder, the visibility predicate, and a completeness count that no longer
includes fields the operator cannot see.

buildGroupTree supports both shapes deliberately. Every module today returns
no configGroups and uses \`group\` as a display label, so the legacy path has
to reproduce the previous bucketing exactly — the tabs would visibly change
otherwise — while the declared path builds the real tree.

Bool conditions are matched through bool semantics rather than string
equality, matching the contract documented on the Go side. The config
document stores the raw env string, so an operator who set FOO=1 has \"1\"
persisted; comparing it to \"true\" would hide the dependent field forever."
```

---

### Task 2: wire the model into the existing UI

Replaces `bucketByGroup` with `buildGroupTree` and makes hidden fields disappear from both
the form and the completeness count. With today's API data the rendered output is
identical — that is the point, and the tests assert it.

**Files:**
- Modify: `frontend-admin/src/pages/admin/modules/detail/ModuleConfigSection.tsx:15,67-69,228-250`
- Modify: `frontend-admin/src/pages/admin/modules/detail/ModuleDashboardCards.tsx:12,47-51`
- Modify: `frontend-admin/src/pages/admin/modules/ModuleConfigFields.tsx` (skip hidden fields)
- Delete: `frontend-admin/src/pages/admin/modules/utils.ts`
- Delete: `frontend-admin/src/pages/admin/modules/ModuleConfigModal.tsx`
- Test: `frontend-admin/src/pages/admin/modules/detail/ModuleConfigSection.test.tsx`

**Interfaces:**
- Consumes: `buildGroupTree`, `visibleFields`, `configCompleteness` from Task 1.
- Produces: `ModuleConfigFields` gains a `values` prop used for visibility filtering.

- [ ] **Step 1: Confirm the dead file really is dead before deleting it**

`ModuleConfigModal.tsx` is the only other consumer of `bucketByGroup`, and nothing imports
it — the module detail page superseded it. The parent spec schedules its deletion, and it
has to happen here because `utils.ts` goes away with it.

```bash
cd frontend-admin && grep -rn "ModuleConfigModal" src/ --include=*.ts --include=*.tsx | grep -v "^src/pages/admin/modules/ModuleConfigModal.tsx"
```

Expected: **no output**. If anything is printed, stop and report — the file is live and the
plan is wrong.

- [ ] **Step 2: Write the failing test**

Create `frontend-admin/src/pages/admin/modules/detail/ModuleConfigSection.test.tsx`:

```tsx
import { describe, it, expect } from 'vitest';
import { screen } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { renderWithProviders } from 'test/render';
import { server } from 'test/server';
import type { ConfigField, ModuleConfig } from 'store/api/moduleApi';
import ModuleConfigSection from './ModuleConfigSection';

const field = (over: Partial<ConfigField> & { key: string }): ConfigField => ({
  label: over.key,
  description: '',
  type: 'string',
  required: false,
  default: '',
  envVar: '',
  ...over
});

const moduleWith = (
  schema: ConfigField[],
  extra: Partial<ModuleConfig> = {}
): ModuleConfig =>
  ({
    moduleName: 'demo',
    displayName: 'Demo',
    description: '',
    category: 'toggleable',
    enabled: true,
    status: 'running',
    needsRestart: false,
    configValues: {},
    secretStatus: {},
    configSchema: schema,
    dependsOn: [],
    providedServices: [],
    requiredServices: [],
    optionalServices: [],
    activeEnvironment: 'production',
    availableEnvironments: [],
    createdAt: '',
    updatedAt: '',
    ...extra
  }) as ModuleConfig;

describe('ModuleConfigSection', () => {
  it('renders legacy group labels as tabs, unchanged', () => {
    // Today's shape: no configGroups, `group` is a display label. This is the
    // regression guard for "no visual change".
    const mod = moduleWith([
      field({ key: 'a', label: 'Alpha', group: 'Google' }),
      field({ key: 'b', label: 'Beta', group: 'Apple' })
    ]);
    renderWithProviders(
      <ModuleConfigSection module={mod} selectedEnvironment="production" />
    );
    expect(screen.getByRole('tab', { name: 'Google' })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: 'Apple' })).toBeInTheDocument();
    expect(screen.getByText('Alpha')).toBeInTheDocument();
  });

  it('hides a field whose condition is unmet and shows it once met', () => {
    const schema = [
      field({ key: 'on', label: 'Enabled', type: 'bool', default: 'false' }),
      field({
        key: 'secretish',
        label: 'Client ID',
        dependsOn: [{ key: 'on', in: ['true'] }]
      })
    ];
    const { rerender } = renderWithProviders(
      <ModuleConfigSection
        module={moduleWith(schema)}
        selectedEnvironment="production"
      />
    );
    expect(screen.queryByText('Client ID')).not.toBeInTheDocument();

    rerender(
      <ModuleConfigSection
        module={moduleWith(schema, { configValues: { on: 'true' } })}
        selectedEnvironment="production"
      />
    );
    expect(screen.getByText('Client ID')).toBeInTheDocument();
  });

  it('renders a declared group tree', () => {
    const mod = moduleWith(
      [
        field({ key: 'x', label: 'Toggle', group: 'oauth' }),
        field({ key: 'y', label: 'Client ID', group: 'oauth.google' })
      ],
      {
        configGroups: [
          { key: 'oauth', label: 'OAuth Providers', order: 1 },
          { key: 'oauth.google', label: 'Google', parent: 'oauth', order: 2 }
        ]
      }
    );
    renderWithProviders(
      <ModuleConfigSection module={mod} selectedEnvironment="production" />
    );
    expect(
      screen.getByRole('tab', { name: 'OAuth Providers' })
    ).toBeInTheDocument();
  });
});
```

**MSW note:** `ModuleConfigSection` calls `useGetModuleEnvironmentQuery`, skipped unless
`availableEnvironments` is non-empty — the fixtures above leave it empty, so no request
fires. If you find any request escaping, add the handler rather than letting the suite fail
with an unrelated error:

```ts
server.use(
  http.get('*/v1/admin/modules/:name/environments/:env', () =>
    HttpResponse.json({
      environment: 'production',
      configValues: {},
      secretStatus: {},
      updatedAt: ''
    })
  )
);
```

- [ ] **Step 3: Run the test to verify it fails**

```bash
cd frontend-admin && npx vitest run src/pages/admin/modules/detail/ModuleConfigSection.test.tsx
```

Expected: the visibility and declared-group tests FAIL; the legacy-tabs test may already
pass against the current implementation.

- [ ] **Step 4: Filter hidden fields in `ModuleConfigFields`**

**No new prop.** The component already receives `configValues` — the same map
`ModuleConfigSection` keeps its edits in — which is exactly what `dependsOn` evaluates
against. A field whose condition is unmet is not rendered, and because it is not rendered
it is never validated as required and never enters the save diff.

In `frontend-admin/src/pages/admin/modules/ModuleConfigFields.tsx`, replace the `fields`
computation:

```ts
  const selected = includeKeys
    ? includeKeys
        .map(key => schema.find(f => f.key === key))
        .filter((f): f is ConfigField => Boolean(f))
    : schema;
  const fields = selected.filter(f => isFieldVisible(f, configValues, schema));
```

with `import { isFieldVisible } from './configModel';` added at the top.

- [ ] **Step 5: Switch `ModuleConfigSection` to the tree**

In `frontend-admin/src/pages/admin/modules/detail/ModuleConfigSection.tsx`:

- replace `import { bucketByGroup } from '../utils';` with
  `import { buildGroupTree, isFieldVisible } from '../configModel';`
- replace the bucket computation:

```ts
  const schema = mod.configSchema ?? [];
  const groupTree = useMemo(
    () => buildGroupTree(schema, mod.configGroups),
    [schema, mod.configGroups]
  );
  const showTabs = groupTree.length >= 2;
  const currentTab = activeTab || groupTree[0]?.key || '';
```

- render tabs from `groupTree` (top level only in this phase — nesting arrives with the
  rail in phase 3), replacing both `groupBuckets.map` blocks:

```tsx
              {groupTree.map(node => (
                <Nav.Item key={node.key}>
                  <Nav.Link eventKey={node.key}>{node.label}</Nav.Link>
                </Nav.Item>
              ))}
```

```tsx
              {groupTree.map(node =>
                currentTab === node.key ? (
                  <div key={node.key}>{renderFields(node.fieldKeys)}</div>
                ) : null
              )}
```

- pass the values through in `renderFields`, adding `values={configValues}` to the
  `<ModuleConfigFields …>` element.
- make the dirty check skip hidden fields, so a field the operator cannot see can never
  mark the form dirty. In the `isDirty` memo and in `handleSave`'s `changedConfig` loop,
  replace `for (const field of schema)` with:

```ts
    for (const field of schema) {
      if (field.type === 'secret') continue;
      if (!isFieldVisible(field, configValues, schema)) continue;
```

- [ ] **Step 6: Point the completeness card at the new helper**

In `frontend-admin/src/pages/admin/modules/detail/ModuleDashboardCards.tsx`, change the
import from `'../utils'` to `'../configModel'`. The call site is unchanged — the signature
matches.

- [ ] **Step 7: Delete the superseded files**

```bash
git rm frontend-admin/src/pages/admin/modules/utils.ts \
       frontend-admin/src/pages/admin/modules/ModuleConfigModal.tsx
cd frontend-admin && grep -rn "from '../utils'\|from './utils'\|ModuleConfigModal" src/pages/admin/modules/
```

Expected: no output.

- [ ] **Step 8: Run the full frontend gate**

```bash
cd frontend-admin && npm run typecheck && npm run lint && npm run test
```

Expected: all clean, all tests pass. Watch specifically for an MSW
`onUnhandledRequest: 'error'` failure — it presents as a test error unrelated to any
assertion.

- [ ] **Step 9: Commit**

```bash
git add -A frontend-admin/src/pages/admin/modules/
git commit -m "feat(admin): render module config from the group tree

Swaps bucketByGroup for buildGroupTree and filters the form by each field's
dependsOn. With today's API data — no module declares groups or conditions —
the rendered output is byte-identical, which the legacy-tabs test guards.

A hidden field is now skipped for dirty-detection and for the save diff too,
not just for rendering: a field the operator cannot see must never mark the
form dirty or be written back.

Deletes ModuleConfigModal, which nothing has imported since the module detail
page superseded it, and utils.ts along with it — the modal was its only other
consumer."
```

---

### Task 3: `helpers/configLabel.ts` — i18n resolution

**Files:**
- Create: `frontend-admin/src/helpers/configLabel.ts`
- Test: `frontend-admin/src/helpers/configLabel.test.ts`
- Modify: `frontend-admin/src/pages/admin/modules/ModuleConfigFields.tsx` (labels + descriptions)
- Modify: `frontend-admin/src/pages/admin/modules/detail/ModuleConfigSection.tsx` (group labels)

**Interfaces:**
- Consumes: `ConfigField`, `ConfigGroup` from `store/api/moduleApi`; `GroupNode` from `configModel`.
- Produces:
  - `translateConfigField(t, moduleName, field, part: 'label' | 'desc'): string`
  - `translateConfigGroup(t, moduleName, group: { key: string; label: string }): string`

**Why a helper and not inline `t()`:** the core modules keep their strings in the
monolithic `translation` bundle while a fork's addon ships its own namespace named after
the module (ADR-0007). One resolver keeps that split out of every call site — exactly what
`helpers/navLabel.ts` already does for nav labels.

- [ ] **Step 1: Write the failing test**

Create `frontend-admin/src/helpers/configLabel.test.ts`:

```ts
import { describe, it, expect, beforeAll } from 'vitest';
import i18n from '../i18n';
import type { ConfigField } from 'store/api/moduleApi';
import { translateConfigField, translateConfigGroup } from './configLabel';

const field: ConfigField = {
  key: 'passwordMinLength',
  label: 'Minimum length',
  description: 'Shortest accepted password.',
  type: 'int',
  required: false,
  default: '',
  envVar: ''
};

describe('translateConfigField', () => {
  beforeAll(async () => {
    await i18n.changeLanguage('en');
    // A fork addon's own namespace (ADR-0007).
    i18n.addResourceBundle(
      'en',
      'billing',
      { config: { fields: { passwordMinLength: { label: 'From addon ns' } } } },
      true,
      true
    );
    // The core bundle.
    i18n.addResourceBundle(
      'en',
      'translation',
      {
        moduleConfig: {
          auth: {
            fields: { passwordMinLength: { label: 'From core bundle' } }
          }
        }
      },
      true,
      true
    );
  });

  it("prefers the module's own namespace when it has the key", () => {
    expect(translateConfigField(i18n.t, 'billing', field, 'label')).toBe(
      'From addon ns'
    );
  });

  it('falls back to the core bundle for a core module', () => {
    expect(translateConfigField(i18n.t, 'auth', field, 'label')).toBe(
      'From core bundle'
    );
  });

  it('falls back to the literal from the backend when neither has the key', () => {
    // An un-migrated fork addon must keep showing English, never a raw key.
    expect(translateConfigField(i18n.t, 'unknownmod', field, 'label')).toBe(
      'Minimum length'
    );
    expect(translateConfigField(i18n.t, 'unknownmod', field, 'desc')).toBe(
      'Shortest accepted password.'
    );
  });
});

describe('translateConfigGroup', () => {
  it('falls back to the declared label', () => {
    expect(
      translateConfigGroup(i18n.t, 'unknownmod', {
        key: 'oauth',
        label: 'OAuth Providers'
      })
    ).toBe('OAuth Providers');
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
cd frontend-admin && npx vitest run src/helpers/configLabel.test.ts
```

Expected: FAIL — `Failed to resolve import "./configLabel"`.

- [ ] **Step 3: Write the helper**

Create `frontend-admin/src/helpers/configLabel.ts`:

```ts
import type { TFunction } from 'i18next';
import i18n from '../i18n';
import type { ConfigField } from 'store/api/moduleApi';

/**
 * Resolves the display string for a module config field or group.
 *
 * The key is derived from the backend's already-stable `key`, so the schema
 * carries no redundant i18n field and `label` stays the literal that always
 * works. Resolution order, mirroring `helpers/navLabel.ts`:
 *
 *   1. `<moduleName>:config.fields.<key>.label` — a fork addon's own namespace (ADR-0007)
 *   2. `moduleConfig.<moduleName>.fields.<key>.label` — the core bundle
 *   3. the literal `label` the backend sent
 *
 * Step 3 is what keeps an un-migrated addon showing English instead of a raw
 * key path.
 */
const resolve = (
  t: TFunction,
  moduleName: string,
  suffix: string,
  fallback: string
): string => {
  if (moduleName && i18n.hasResourceBundle(i18n.language, moduleName)) {
    const scoped = t(`${moduleName}:${suffix}`, { defaultValue: '' });
    if (scoped) return scoped;
  }
  return t(`moduleConfig.${moduleName}.${suffix}`, { defaultValue: fallback });
};

export const translateConfigField = (
  t: TFunction,
  moduleName: string,
  field: ConfigField,
  part: 'label' | 'desc'
): string =>
  resolve(
    t,
    moduleName,
    `config.fields.${field.key}.${part}`,
    part === 'label' ? field.label : field.description || ''
  );

export const translateConfigGroup = (
  t: TFunction,
  moduleName: string,
  group: { key: string; label: string }
): string =>
  resolve(t, moduleName, `config.groups.${group.key}.label`, group.label);
```

- [ ] **Step 4: Run the test to verify it passes**

```bash
cd frontend-admin && npx vitest run src/helpers/configLabel.test.ts
```

Expected: PASS (4 tests).

- [ ] **Step 5: Wire it into the two render sites**

`ModuleConfigFields` needs the module name to resolve against. Add a prop:

```ts
  /** Owning module — selects the i18n namespace the labels resolve against. */
  moduleName: string;
```

Replace every bare `{field.label}` with
`{translateConfigField(t, moduleName, field, 'label')}` and every
`{field.description}` render with the `'desc'` variant, keeping the existing
`{field.description && …}` guards but testing the resolved string instead.

In `ModuleConfigSection`, pass `moduleName={mod.moduleName}` to `ModuleConfigFields`, and
render tab labels as
`{translateConfigGroup(t, mod.moduleName, node)}`.

- [ ] **Step 6: Run the full frontend gate**

```bash
cd frontend-admin && npm run typecheck && npm run lint && npm run test
```

Expected: all clean. The `ModuleConfigSection` tests from Task 2 still pass — no
`moduleConfig.*` keys exist yet, so every label resolves to the literal, unchanged.

- [ ] **Step 7: Commit**

```bash
git add frontend-admin/src/helpers/configLabel.ts \
        frontend-admin/src/helpers/configLabel.test.ts \
        frontend-admin/src/pages/admin/modules/
git commit -m "feat(admin): resolve module config labels through i18n

Config labels came straight from the Go schema, so an Italian operator saw a
half-translated page: chrome in Italian, every field label in English.

The key is derived from the backend's stable field key rather than declared,
so the schema carries no redundant i18n field and \`label\` stays the literal
fallback. Resolution mirrors helpers/navLabel.ts: the module's own namespace
first (ADR-0007), then the core bundle, then the literal — which is what keeps
an un-migrated fork addon showing English instead of a raw key path.

No key files change here: with no moduleConfig.* entries yet every label
resolves to the same literal it does today."
```

---

### Task 4: declarative validation

**Files:**
- Modify: `frontend-admin/src/pages/admin/modules/ModuleConfigFields.tsx`
- Modify: `frontend-admin/src/locales/en.json`, `frontend-admin/src/locales/it.json`
- Test: `frontend-admin/src/pages/admin/modules/ModuleConfigFields.test.tsx`

**Interfaces:**
- Consumes: `ConfigField.min` / `max` / `pattern` / `placeholder` from Task 1.
- Produces: nothing later tasks depend on.

**The bug this closes:** the current duration check is `/^\d+[smh]$/`, while the backend
parses durations with Go's `time.ParseDuration`. `1h30m`, `500ms` and `1.5h` are accepted
by the server and rejected by the UI — the client is stricter than the contract.

- [ ] **Step 1: Add the locale keys**

To `src/locales/en.json` under `adminModules.configFields`:

```json
      "minFeedback": "Minimum is {{min}}",
      "maxFeedback": "Maximum is {{max}}",
      "patternFeedback": "Value does not match the required format",
      "durationFeedback": "Use a Go duration — e.g. 30s, 15m, 1h30m, 500ms"
```

and the matching `it.json` entries:

```json
      "minFeedback": "Il minimo è {{min}}",
      "maxFeedback": "Il massimo è {{max}}",
      "patternFeedback": "Il valore non corrisponde al formato richiesto",
      "durationFeedback": "Usa una durata Go — es. 30s, 15m, 1h30m, 500ms"
```

`durationFeedback` already exists in both files — replace its value, do not add a second
entry.

- [ ] **Step 2: Write the failing test**

Create `frontend-admin/src/pages/admin/modules/ModuleConfigFields.test.tsx`:

```tsx
import { describe, it, expect } from 'vitest';
import { screen } from '@testing-library/react';
import { renderWithProviders } from 'test/render';
import type { ConfigField } from 'store/api/moduleApi';
import ModuleConfigFields from './ModuleConfigFields';

const field = (over: Partial<ConfigField> & { key: string }): ConfigField => ({
  label: over.key,
  description: '',
  type: 'string',
  required: false,
  default: '',
  envVar: '',
  ...over
});

const render = (schema: ConfigField[], values: Record<string, string>) =>
  renderWithProviders(
    <ModuleConfigFields
      schema={schema}
      moduleName="demo"
      configValues={values}
      secretValues={{}}
      onConfigChange={() => {}}
      onSecretChange={() => {}}
    />
  );

describe('duration validation', () => {
  const schema = [field({ key: 'ttl', label: 'TTL', type: 'duration' })];

  it.each(['30s', '15m', '1h', '1h30m', '500ms', '1.5h', '0'])(
    'accepts %s — the backend does',
    value => {
      render(schema, { ttl: value });
      expect(screen.getByLabelText('TTL')).not.toHaveClass('is-invalid');
    }
  );

  it.each(['30 s', 'abc', '15x', ''])('rejects %s', value => {
    if (value === '') return; // empty is "unset", not invalid
    render(schema, { ttl: value });
    expect(screen.getByLabelText('TTL')).toHaveClass('is-invalid');
  });
});

describe('min / max validation', () => {
  const schema = [
    field({ key: 'len', label: 'Length', type: 'int', min: 8, max: 128 })
  ];

  it('flags a value below min and names the bound', () => {
    render(schema, { len: '6' });
    expect(screen.getByText('Minimum is 8')).toBeInTheDocument();
  });

  it('flags a value above max', () => {
    render(schema, { len: '999' });
    expect(screen.getByText('Maximum is 128')).toBeInTheDocument();
  });

  it('accepts a value inside the range', () => {
    render(schema, { len: '12' });
    expect(screen.queryByText(/Minimum is/)).not.toBeInTheDocument();
    expect(screen.queryByText(/Maximum is/)).not.toBeInTheDocument();
  });
});

describe('pattern validation', () => {
  it('flags a value that does not match', () => {
    render([field({ key: 'code', label: 'Code', pattern: '^[a-z]+$' })], {
      code: 'ABC'
    });
    expect(
      screen.getByText('Value does not match the required format')
    ).toBeInTheDocument();
  });

  it('ignores an uncompilable pattern rather than throwing', () => {
    // The backend validator rejects these, but a bad regex reaching the render
    // path must degrade to "no pattern check", never crash the page.
    expect(() =>
      render([field({ key: 'code', label: 'Code', pattern: '([' })], {
        code: 'anything'
      })
    ).not.toThrow();
  });
});

describe('placeholder', () => {
  it('prefers the declared placeholder over the default', () => {
    render(
      [
        field({
          key: 'host',
          label: 'Host',
          default: 'localhost',
          placeholder: 'smtp.example.com'
        })
      ],
      {}
    );
    expect(screen.getByPlaceholderText('smtp.example.com')).toBeInTheDocument();
  });
});
```

- [ ] **Step 3: Run the test to verify it fails**

```bash
cd frontend-admin && npx vitest run src/pages/admin/modules/ModuleConfigFields.test.tsx
```

Expected: FAIL — the min/max/pattern messages do not exist, and `1h30m` is currently marked
invalid.

- [ ] **Step 4: Implement the validators**

In `ModuleConfigFields.tsx`, add above the component:

```ts
/**
 * Mirrors Go's time.ParseDuration: an optional sign, then one or more
 * decimal-with-unit segments (ns, us/µs, ms, s, m, h). A bare zero is also
 * valid. The previous `^\d+[smh]$` rejected 1h30m, 500ms and 1.5h — all of
 * which the backend accepts, so the console was stricter than the contract.
 */
const DURATION_RE =
  /^[+-]?0$|^[+-]?((\d+(\.\d*)?|\.\d+)(ns|us|µs|μs|ms|s|m|h))+$/;

/** Compiles a schema-declared pattern, or null when it is not usable. */
const safeRegExp = (pattern?: string): RegExp | null => {
  if (!pattern) return null;
  try {
    return new RegExp(pattern);
  } catch {
    // The backend validator rejects an uncompilable pattern; if one still
    // reaches here, skipping the check beats throwing inside a render.
    return null;
  }
};
```

and inside the string/int branch, replace the single `isDurationInvalid` computation with:

```ts
        const value = configValues[key] || '';
        const isEmpty = field.required && !value;
        const isDurationInvalid =
          field.type === 'duration' && value !== '' && !DURATION_RE.test(value);
        const numeric = field.type === 'int' ? Number(value) : NaN;
        const isBelowMin =
          field.min !== undefined && value !== '' && numeric < field.min;
        const isAboveMax =
          field.max !== undefined && value !== '' && numeric > field.max;
        const patternRe = safeRegExp(field.pattern);
        const isPatternInvalid =
          patternRe !== null && value !== '' && !patternRe.test(value);
        const isInvalid =
          isEmpty ||
          isDurationInvalid ||
          isBelowMin ||
          isAboveMax ||
          isPatternInvalid;
```

Use `isInvalid` for both `Form.Control` variants' `isInvalid` prop, use
`field.placeholder || field.default || ''` for the placeholder, and add the three feedback
blocks alongside the existing two:

```tsx
            {isBelowMin && (
              <Form.Control.Feedback type="invalid">
                {t('adminModules.configFields.minFeedback', { min: field.min })}
              </Form.Control.Feedback>
            )}
            {isAboveMax && (
              <Form.Control.Feedback type="invalid">
                {t('adminModules.configFields.maxFeedback', { max: field.max })}
              </Form.Control.Feedback>
            )}
            {isPatternInvalid && (
              <Form.Control.Feedback type="invalid">
                {t('adminModules.configFields.patternFeedback')}
              </Form.Control.Feedback>
            )}
```

The test addresses controls with `getByLabelText`, and today **nothing is wired**: the
labels render as `<Form.Label className="fs-10 fw-semibold">` with no `htmlFor` and the
controls carry no `id`. Add `id={\`cfg-${key}\`}` to every `Form.Control` / `Form.Select`
in the string, int, duration, stringList and enum branches, and `htmlFor={\`cfg-${key}\`}`
to their labels. This is an accessibility fix in its own right — the labels are currently
not programmatically associated with their inputs.

- [ ] **Step 5: Run the tests to verify they pass**

```bash
cd frontend-admin && npx vitest run src/pages/admin/modules/ModuleConfigFields.test.tsx
```

Expected: PASS.

- [ ] **Step 6: Run the full frontend gate**

```bash
cd frontend-admin && npm run typecheck && npm run lint && npm run test
```

Expected: all clean, including the EN/IT parity test over the four new keys.

- [ ] **Step 7: Commit**

```bash
git add frontend-admin/src/pages/admin/modules/ModuleConfigFields.tsx \
        frontend-admin/src/pages/admin/modules/ModuleConfigFields.test.tsx \
        frontend-admin/src/locales/en.json frontend-admin/src/locales/it.json
git commit -m "fix(admin): stop the console rejecting durations the backend accepts

The duration check was ^\\d+[smh]\$ while the backend parses with Go's
time.ParseDuration, so 1h30m, 500ms and 1.5h were refused by the form and
accepted by the server — the client was stricter than the contract it renders.

Adds the min/max/pattern checks the schema now declares, so a bad value is
caught while typing instead of coming back as a failed save. An uncompilable
pattern degrades to no check rather than throwing inside a render: the backend
validator rejects those, but a render path is the wrong place to find out."
```

---

## Phase exit criteria

- `npm run typecheck`, `npm run lint`, `npm run test` all clean in `frontend-admin`.
- `/admin/modules/:name` renders identically to before for every core module — same tabs,
  same fields, same completeness figure. The legacy-tabs test is the automated guard; a
  visual check against the running stack is the confirmation.
- `bucketByGroup`, `utils.ts` and `ModuleConfigModal.tsx` are gone, with no dangling import.
- No `moduleConfig.*` locale key exists yet — every label still resolves to the literal the
  backend sends. Those keys arrive with the module migrations in phases 4 and 5.

Update [`module-config-ux.md`](module-config-ux.md) §8: set phase 2 to ✅.
