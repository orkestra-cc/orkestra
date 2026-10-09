import type { PolicyInput } from 'store/api/complianceApi';

export interface DiffRow {
  field: string;
  before: string;
  after: string;
}

const EMPTY = '—';

// flatten turns a policy into dotted paths (logContent.ipAddress,
// retention.admin_access, …) with printable values; arrays are joined.
const flatten = (
  value: unknown,
  prefix: string,
  out: Record<string, string>
) => {
  if (value === null || value === undefined || value === '') {
    if (prefix) out[prefix] = EMPTY;
    return;
  }
  if (Array.isArray(value)) {
    out[prefix] = value.length
      ? value
          .map(v => (typeof v === 'object' ? JSON.stringify(v) : String(v)))
          .join(', ')
      : EMPTY;
    return;
  }
  if (typeof value === 'object') {
    for (const [k, v] of Object.entries(value)) {
      flatten(v, prefix ? `${prefix}.${k}` : k, out);
    }
    return;
  }
  out[prefix] = String(value);
};

const editable = (p?: PolicyInput) =>
  p
    ? {
        name: p.name,
        description: p.description,
        logContent: p.logContent,
        retention: p.retention,
        accountability: p.accountability,
        sinks: p.sinks ?? null
      }
    : {};

// diffPolicies lists the editable fields that differ between two versions of
// a policy (before undefined = a new policy), sorted by path.
export function diffPolicies(
  before: PolicyInput | undefined,
  after: PolicyInput
): DiffRow[] {
  const b: Record<string, string> = {};
  const a: Record<string, string> = {};
  flatten(editable(before), '', b);
  flatten(editable(after), '', a);
  return [...new Set([...Object.keys(b), ...Object.keys(a)])]
    .sort()
    .map(field => ({
      field,
      before: b[field] ?? EMPTY,
      after: a[field] ?? EMPTY
    }))
    .filter(r => r.before !== r.after);
}
