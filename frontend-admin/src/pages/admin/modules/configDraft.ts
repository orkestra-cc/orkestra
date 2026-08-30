import type { ConfigFormValues } from './useModuleConfigForm';

/**
 * One dirty field captured by "Reload & review", re-applied on top of the
 * fresh baseline once it lands.
 *
 * `key` is the backend's **schema key** (`email.profiles.primary.host`), not
 * the react-hook-form register name. That distinction is the whole point of
 * this module: `buildFieldNames` sanitises `\W` to `_` and resolves the
 * collisions that creates with a numeric suffix, assigned in schema *and
 * roster* order. So a name like `email_profiles_a_b_c` denotes whichever
 * colliding field came first in the roster the map was built from — and a
 * reload is exactly when that roster can change. A draft keyed by name would
 * then re-apply the operator's value, or their typed secret, to a
 * *different* field of a *different* element, silently. The schema key is
 * immutable, so it identifies the field the operator actually edited no
 * matter how the roster moved.
 */
export interface DraftEntry {
  key: string;
  value: string;
  secret: boolean;
}

/**
 * The draft to carry across a reload: only fields react-hook-form marks
 * dirty, re-keyed from register name to schema key.
 *
 * Never the whole form — that would turn the other operator's changes into
 * "local edits" pointing back at the old values. Fields of elements created
 * in this session are excluded because the reload discards pending creates,
 * so their values must not come back as orphan edits. A secret typed and
 * then cleared is not a change: there is nothing to re-send. A name with no
 * schema key (nothing in the current mapping produced it) is skipped rather
 * than guessed at.
 *
 * `dirtyFields` is `form.formState.dirtyFields` — flat, because register
 * names are flat (`buildFieldNames`), so each entry is a plain boolean.
 */
export const captureDraftFromDirty = (
  dirtyFields: Readonly<Record<string, unknown>>,
  values: ConfigFormValues,
  fieldNames: ReadonlyMap<string, string>,
  secretNames: ReadonlySet<string>,
  createdLeafNames: ReadonlySet<string>
): DraftEntry[] => {
  // Register names are unique by construction, so the reverse mapping is a
  // function. Built once per capture rather than searched per entry.
  const schemaKeyOf = new Map<string, string>();
  for (const [key, name] of fieldNames) schemaKeyOf.set(name, key);

  const out: DraftEntry[] = [];
  for (const name of Object.keys(dirtyFields)) {
    if (!dirtyFields[name]) continue;
    if (createdLeafNames.has(name)) continue;
    const key = schemaKeyOf.get(name);
    if (key === undefined) continue;
    const value = String(values[name] ?? '');
    const secret = secretNames.has(name);
    if (secret && value === '') continue;
    out.push({ key, value, secret });
  }
  return out;
};

/**
 * Resolves a captured draft against the roster the reload brought back.
 *
 * Each entry is looked up by its schema key in the NEW mapping: a key with
 * no name belongs to an element another operator removed, which is the
 * "dropped edit" case — now decided by identity rather than by a name that
 * happened to disappear. A non-secret edit is re-applied only while it still
 * differs from the new baseline (an intentional clear to `''` against a
 * non-empty baseline included); an edit the other writer already made is no
 * longer a change. A secret's baseline is always `''` — it is never echoed —
 * so a typed secret is always a change.
 */
export const resolveDraft = (
  entries: readonly DraftEntry[],
  fieldNames: ReadonlyMap<string, string>,
  defaults: ConfigFormValues
): { apply: Array<[string, string]>; dropped: number } => {
  const apply: Array<[string, string]> = [];
  let dropped = 0;
  for (const { key, value, secret } of entries) {
    const name = fieldNames.get(key);
    if (name === undefined) {
      dropped += 1;
      continue;
    }
    if (secret || value !== (defaults[name] ?? '')) apply.push([name, value]);
  }
  return { apply, dropped };
};
