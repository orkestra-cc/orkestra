import type {
  ConfigField,
  ConfigGroup,
  FieldCondition
} from 'store/api/moduleApi';

/** A node of the settings rail: one group, its own fields, and its children. */
export interface GroupNode {
  key: string;
  label: string;
  description?: string;
  icon?: string;
  fieldKeys: string[];
  children: GroupNode[];
  /**
   * The declared parent this node nests under, when it has one — key plus
   * literal label, which is exactly `translateConfigGroup`'s input, so a
   * panel can name and link back to its parent without being handed the
   * whole tree. Absent for roots, for legacy (undeclared-group) nodes, and
   * for a child whose declared parent does not exist (it is promoted to a
   * root instead of being lost).
   */
  parent?: { key: string; label: string };
}

const GENERAL = 'General';

/**
 * Values the backend's `parseBool` treats as true. Kept in lockstep with
 * `parseBool` in `pkg/sdk/module/config_unmarshal.go` — the config document
 * stores the raw env string, so an operator who set FOO=1 has "1" persisted,
 * and string equality against "true" would hide the dependent field forever.
 */
const TRUTHY = new Set(['true', '1', 'yes']);

const asBool = (raw: string): boolean => TRUTHY.has(raw.trim().toLowerCase());

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
    if (parent) {
      node.parent = { key: parent.key, label: parent.label };
      parent.children.push(node);
    } else roots.push(node);
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
 * Whether `ModuleConfigSection`'s own card-internal rail should show: either
 * the module explicitly declared `configGroups` (even a lone top-level entry
 * opts in — it can still have nested children the legacy flat-bucket case
 * never has), or the legacy heuristic found ≥2 distinct `field.group` labels
 * to bucket by. This is the permissive, card-scoped formula — see
 * `hasPageRail` for why the full-page rail needs a stricter one.
 */
export const hasCardRail = (
  groupTree: GroupNode[],
  configGroups: ConfigGroup[] | null | undefined
): boolean => groupTree.length >= 2 || Boolean(configGroups?.length);

/**
 * Whether the full-page rail (Overview / configuration tree / Dependencies /
 * Environments) should replace today's stacked page. Stricter than
 * `hasCardRail` on purpose: it requires an *explicit* declared
 * `configGroups`, not just the legacy heuristic finding ≥2 distinct
 * `field.group` labels. A legacy module still gets `hasCardRail`'s smaller
 * card-internal rail — it just isn't promoted to the whole-page framing
 * (Overview/Dependencies/Environments intermixed with a tree that has no
 * real declared hierarchy). `auth` is the only module in the base that
 * declares `configGroups` today; for every other one this is `false` no
 * matter how many legacy buckets their fields happen to land in.
 */
export const hasPageRail = (
  groupTree: GroupNode[],
  configGroups: ConfigGroup[] | null | undefined
): boolean => Boolean(configGroups?.length) && groupTree.length >= 2;

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

/**
 * Whether a field should be rendered, given the module's current values.
 * How the field's conditions combine is chosen by `dependsOnMatch` — AND by
 * default (`''`/`'all'`), OR when `'any'`; OR within one condition's `in`
 * list either way. See `FieldCondition` in `store/api/moduleApi` for the
 * matching contract this implements — it is shared with the Go side and the
 * two must not drift.
 *
 * `values` is keyed by the backend's schema key, always — that is the one
 * keying this whole module speaks, and it is what lets the same predicate
 * serve `configCompleteness` (handed the backend's `configValues` directly)
 * and the live form. react-hook-form values are keyed by register name
 * instead (`buildFieldNames`, because RHF parses "." as a path separator);
 * a form-side caller re-keys with `toSchemaValues` before calling in here.
 */
export const isFieldVisible = (
  field: ConfigField,
  values: Record<string, string>,
  schema: ConfigField[]
): boolean => {
  if (!field.dependsOn || field.dependsOn.length === 0) return true;
  const satisfied = (cond: FieldCondition): boolean => {
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
  };
  return field.dependsOnMatch === 'any'
    ? field.dependsOn.some(satisfied)
    : field.dependsOn.every(satisfied);
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
  // A record list is excluded from both completeness measures. It holds no
  // value of its own, so `cv[f.key]` is always empty and a list marked
  // required would read as permanently unfilled — an amber badge no operator
  // could ever clear. Arity is the list's own concern (`min`/`max`), not this
  // one's.
  const required = visibleFields(schema, cv).filter(
    f => f.required && f.type !== 'recordList'
  );
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

/**
 * The visible required fields currently holding nothing — the per-field
 * complement of `configCompleteness`, feeding the rail's per-group "to fill"
 * badge. `values` may be the live form values (re-keyed to schema keys) or
 * the backend's `configValues`. A secret counts as filled when the backend
 * already holds one (`secretStatus`) or the caller's values carry a fresh
 * plaintext — typed but not yet saved is still addressed.
 */
export const unfilledRequiredKeys = (
  schema: ConfigField[] | null | undefined,
  values: Record<string, string>,
  secretStatus: Record<string, boolean> | null | undefined
): Set<string> => {
  const out = new Set<string>();
  if (!schema) return out;
  const ss = secretStatus ?? {};
  for (const f of visibleFields(schema, values)) {
    // See configCompleteness: a record list has no value to be "filled".
    if (!f.required || f.type === 'recordList') continue;
    const typed = (values[f.key] ?? '').trim() !== '';
    const filled = f.type === 'secret' ? Boolean(ss[f.key]) || typed : typed;
    if (!filled) out.add(f.key);
  }
  return out;
};
