import type { ConfigField } from 'store/api/moduleApi';

/**
 * Bucket a config schema into ordered groups. Preserves declaration order of
 * both groups (first field of each group sets the tab order) and keys within
 * each group. Fields with an empty `group` land in a trailing "General" bucket.
 */
export const bucketByGroup = (
  schema: ConfigField[] | null | undefined
): { group: string; keys: string[] }[] => {
  if (!schema || schema.length === 0) return [];
  const order: string[] = [];
  const buckets = new Map<string, string[]>();
  for (const field of schema) {
    const g = field.group || '';
    if (!buckets.has(g)) {
      buckets.set(g, []);
      order.push(g);
    }
    buckets.get(g)!.push(field.key);
  }
  const result: { group: string; keys: string[] }[] = [];
  for (const g of order) {
    if (g === '') continue;
    result.push({ group: g, keys: buckets.get(g)! });
  }
  if (buckets.has('')) {
    result.push({ group: 'General', keys: buckets.get('')! });
  }
  return result;
};

/**
 * Compute config completeness: how many required fields have non-empty values.
 */
export const configCompleteness = (
  schema: ConfigField[] | null | undefined,
  configValues: Record<string, string> | null | undefined,
  secretStatus: Record<string, boolean> | null | undefined
): { filled: number; total: number } => {
  if (!schema) return { filled: 0, total: 0 };
  const required = schema.filter((f) => f.required);
  const cv = configValues ?? {};
  const ss = secretStatus ?? {};
  let filled = 0;
  for (const field of required) {
    if (field.type === 'secret') {
      if (ss[field.key]) filled++;
    } else if (cv[field.key]) {
      filled++;
    }
  }
  return { filled, total: required.length };
};
