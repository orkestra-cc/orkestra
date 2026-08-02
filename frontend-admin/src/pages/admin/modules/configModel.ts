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
