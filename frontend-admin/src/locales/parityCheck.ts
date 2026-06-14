// Reusable locale-parity primitives, shared by the core `parity.test.ts`
// and by every addon's own parity test (ADR-0007). Pure — no test runner
// coupling: each test imports these and runs its own assertions, so an addon
// validates its bundles without touching the core locale files.

/**
 * Flattens a nested locale object into dot-paths so two locales can be
 * compared key-for-key. `app.name` → "app.name". Every leaf (string, number,
 * plural variant) is terminal; arrays are treated as leaves; objects recurse.
 */
export function flatten(
  obj: unknown,
  prefix = '',
  out: Set<string> = new Set()
): Set<string> {
  if (obj === null || typeof obj !== 'object') {
    out.add(prefix);
    return out;
  }
  if (Array.isArray(obj)) {
    out.add(prefix);
    return out;
  }
  for (const [k, v] of Object.entries(obj)) {
    const path = prefix ? `${prefix}.${k}` : k;
    flatten(v, path, out);
  }
  return out;
}

/**
 * Keys present in exactly one of the two locales. A non-empty result means a
 * translation was added to one file but not the matching entry in the other,
 * so production would silently fall back to the key path or the fallbackLng.
 */
export function keyDiff(
  a: unknown,
  b: unknown
): { onlyInA: string[]; onlyInB: string[] } {
  const aKeys = flatten(a);
  const bKeys = flatten(b);
  return {
    onlyInA: [...aKeys].filter(k => !bKeys.has(k)).sort(),
    onlyInB: [...bKeys].filter(k => !aKeys.has(k)).sort()
  };
}

/**
 * Keys whose value is an empty/whitespace-only string — a leaked TODO or an
 * untranslated entry. `locale` labels each hit for the assertion message.
 */
export function emptyValues(
  obj: unknown,
  locale: string,
  prefix = '',
  out: Array<{ locale: string; key: string }> = []
): Array<{ locale: string; key: string }> {
  if (typeof obj === 'string') {
    if (obj.trim() === '') out.push({ locale, key: prefix });
    return out;
  }
  if (obj === null || typeof obj !== 'object' || Array.isArray(obj)) return out;
  for (const [k, v] of Object.entries(obj)) {
    emptyValues(v, locale, prefix ? `${prefix}.${k}` : k, out);
  }
  return out;
}
