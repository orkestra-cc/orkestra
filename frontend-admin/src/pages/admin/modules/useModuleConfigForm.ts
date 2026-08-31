import { useMemo } from 'react';
import { useForm, type Resolver, type UseFormReturn } from 'react-hook-form';
import { yupResolver } from '@hookform/resolvers/yup';
import * as yup from 'yup';
import type { ConfigField } from 'store/api/moduleApi';
import { isFieldVisible } from './configModel';
import { expandSchema, rosterKeyOf } from './recordList/expandSchema';

/**
 * Mirrors the backend's `utils.ParseDuration` — NOT Go's `time.ParseDuration`,
 * which is only the first of its two alternatives:
 *
 *  1. everything `time.ParseDuration` takes — an optional sign, then one or
 *     more decimal-with-unit segments (ns, us/µs, ms, s, m, h); a bare zero
 *     is valid; and
 *  2. a bare `<number>d` for days.
 *
 * The second alternative is deliberately NOT part of the first: `utils.ParseDuration`
 * special-cases only a bare `<number>d` and leaves compound forms like `1d12h`
 * unsupported, so a value either parses exactly or is rejected. Keeping `d` out
 * of the segment group is what preserves that asymmetry here — see the
 * `1d12h` case in useModuleConfigForm.test.ts.
 *
 * This runs BEFORE the schema-declared `pattern`, so a unit this regex rejects
 * can never be saved no matter what the backend declares. Omitting `d` made
 * `sessionAbsoluteTTL: 89d` — the documented maximum, declared valid by auth's
 * own `Pattern: "^[0-9]+(s|m|h|d)$"` — unsavable from /admin/modules (ADR-0017).
 *
 * Kept identical to the copy in ModuleConfigFields — both are the same
 * contract, and the backend is the authority on it.
 */
const DURATION_RE =
  /^[+-]?0$|^[+-]?((\d+(\.\d*)?|\.\d+)(ns|us|µs|μs|ms|s|m|h))+$|^[+-]?(\d+(\.\d*)?|\.\d+)d$/;

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

/**
 * Values keyed by the backend's own config key (`ConfigField.key`) — the
 * currency of the whole model layer: the API payload, `dependsOn`, group
 * membership, i18n keys and `configCompleteness` are all keyed this way.
 */
export type ConfigValues = Record<string, string>;

/**
 * Values keyed by the name react-hook-form registers the field under (see
 * `buildFieldNames`). This is what `form.getValues()`, `useWatch`,
 * `formState.dirtyFields` and `formState.errors` speak — and *only* them.
 * Re-key with `toSchemaValues` before handing them to anything in
 * `configModel.ts`.
 */
export type ConfigFormValues = Record<string, string>;

/**
 * Maps each config field key to the name react-hook-form registers it under.
 *
 * RHF reads "." in a field name as a path separator, so registering
 * `email.smtp.host` verbatim writes the edit to {email:{smtp:{host}}} while
 * every consumer here reads the flat key — the edit is then invisible to
 * dirty tracking and to the save diff. That made `/admin/modules/notification`
 * (11 of 11 keys dotted) and `/admin/modules/tenant` (2 of 2) impossible to
 * configure: the field showed the typed value and Save never enabled.
 *
 * The sanitisation target is `\w` rather than "just strip the dots" because
 * RHF's own `isKey` (`/^\w*$/`) is the test that decides whether a name is
 * treated as one literal property or parsed as a path — and its path parser
 * also splits on `[`, `]`, `,` and strips quotes and `|`. Matching `isKey`
 * exactly is what makes the flat read/write guaranteed rather than merely
 * likely for whatever key a fork's addon declares.
 *
 * Sanitising is not enough on its own: `a.b` and `a_b` collapse onto the same
 * name, so uniqueness is enforced with a numeric suffix. The result is a pure,
 * deterministic function of the schema and its declaration order, so any two
 * callers deriving it from the same schema agree — which is why the argument
 * is optional everywhere below and passing the memoised instance is only a
 * performance choice, never a correctness one.
 *
 * The schema key stays the source of truth everywhere else. This mapping is a
 * form-layer detail and must never reach the wire.
 */
export const buildFieldNames = (schema: ConfigField[]): Map<string, string> => {
  const names = new Map<string, string>();
  const taken = new Set<string>();
  for (const field of schema) {
    // The `|| 'field'` guards exactly one case: an **empty** key. The replace
    // is char-for-char, so punctuation never vanishes — `'...'` becomes
    // `'___'`, not `''` (pinned by a test). An empty key would otherwise
    // yield `name === ''`, which `register('')` happily accepts as a field
    // that renders and can never be dirtied. Do not delete this as dead.
    const base = field.key.replace(/\W/g, '_') || 'field';
    let name = base;
    for (let n = 2; taken.has(name); n++) name = `${base}_${n}`;
    taken.add(name);
    names.set(field.key, name);
  }
  return names;
};

/**
 * Resolves one field's register name, refusing to guess.
 *
 * Every lookup goes through here rather than `fieldNames.get(key) ?? key`.
 * That fallback is unreachable today — every caller derives the map from the
 * very schema it is iterating — but it degrades to the **raw dotted key**,
 * which is precisely the bug this module exists to prevent: a caller that
 * ever pairs a `fieldNames` with a different `schema` would silently hand
 * `email.smtp.host` back to react-hook-form, with no error, no type error and
 * no failing test. Same reasoning that keeps `configModel.ts` from taking an
 * optional mapping parameter: a silently-wrong default is worse than a loud
 * failure. This throws, naming the key, in every environment — the state is
 * structurally impossible, and if it ever happens the whole form is broken
 * anyway, so a precise message beats a mysterious dead Save button.
 */
export const fieldNameOf = (
  fieldNames: ReadonlyMap<string, string>,
  key: string
): string => {
  const name = fieldNames.get(key);
  if (name === undefined) {
    throw new Error(
      `useModuleConfigForm: no react-hook-form name registered for config key "${key}" — ` +
        'the fieldNames map was built from a different schema than the one being rendered'
    );
  }
  return name;
};

/**
 * Re-keys form values (register names) back to schema keys.
 *
 * Translating once at this boundary is why `isFieldVisible`, `visibleFields`
 * and `configCompleteness` keep working in schema keys and need no mapping
 * argument — `configCompleteness` is handed the backend's `configValues`
 * directly, so a shared "sometimes name-keyed, sometimes not" signature there
 * would be a standing invitation to the same class of bug.
 *
 * A name the form holds no value for yields `''`, which `isFieldVisible`
 * already treats exactly like an absent key (both fall back to the target's
 * declared default). A key with no *name* is a different thing entirely and
 * throws — see `fieldNameOf`.
 */
export const toSchemaValues = (
  schema: ConfigField[],
  values: ConfigFormValues,
  fieldNames: ReadonlyMap<string, string> = buildFieldNames(schema)
): ConfigValues => {
  const out: ConfigValues = {};
  for (const field of schema) {
    out[field.key] = values[fieldNameOf(fieldNames, field.key)] ?? '';
  }
  return out;
};

/**
 * Seeds the form. A stored value wins over the schema default; a secret always
 * starts empty because the backend never sends secret values to the client —
 * only whether one exists. An empty secret field means "keep what is stored".
 *
 * A stored empty string is not the same as nothing stored: `UpdateConfig`
 * writes `configValues[key] || ''` when an operator clears a field, so `''`
 * is a real, persisted value. Falling back to the schema default in that case
 * would render a value the database does not hold, and because the form
 * wouldn't be dirty, the mismatch would never get corrected by a save. Only a
 * genuinely absent key falls back to the default — mirrored with `??`, same
 * as the `enum` branch in `ModuleConfigFields`.
 *
 * `bool` is the deliberate exception: a switch has no blank state, so a
 * stored `''` still collapses to the default, matching the `bool` branch in
 * `ModuleConfigFields`.
 *
 * `configValues` comes off the wire and is keyed by the schema key; the result
 * seeds react-hook-form and is therefore keyed by the register name.
 */
export const buildDefaults = (
  schema: ConfigField[],
  configValues: ConfigValues | undefined,
  fieldNames: ReadonlyMap<string, string> = buildFieldNames(schema)
): ConfigFormValues => {
  const stored = configValues ?? {};
  const out: ConfigFormValues = {};
  for (const f of schema) {
    const name = fieldNameOf(fieldNames, f.key);
    if (f.type === 'secret') {
      out[name] = '';
      continue;
    }
    const v = stored[f.key];
    if (f.type === 'bool') {
      out[name] = v !== undefined && v !== '' ? v : f.default || '';
      continue;
    }
    out[name] = v ?? f.default ?? '';
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
  schema: ConfigField[],
  fieldNames: ReadonlyMap<string, string> = buildFieldNames(schema),
  secretStatus?: Record<string, boolean>
): yup.ObjectSchema<Record<string, unknown>> => {
  const shape: Record<string, yup.StringSchema> = {};

  for (const field of schema) {
    let rule = yup.string();

    rule = rule.test(
      'orkestra-field',
      'invalid',
      function validateField(value) {
        // `this.parent` is the register-name-keyed form object, so it has to
        // be re-keyed before `isFieldVisible` can resolve a `dependsOn`
        // target.
        //
        // This is O(n²) in field count — n rules each re-keying n fields — and
        // it runs on every keystroke, since `mode: 'onChange'` validates the
        // whole object. Benchmarked on this repo: **63 fields (auth, the
        // largest module in the base) ≈ 0.8 ms per pass, ~5% of a 16.7 ms
        // frame** — imperceptible, and the reason this stays as it is. But it
        // is a real share of the pass rather than noise beside the yup rules,
        // and it scales roughly quadratically: **250 fields ≈ 9.6 ms, ~58% of
        // a frame**, which is felt as input lag. A fork addon that large
        // should re-key once per pass instead of once per rule — see the
        // staleness constraint below for why that cache is not trivial.
        //
        // Not cached, and NOT because a cache would miss — `this.parent` is one
        // stable reference across every rule in a pass, so a WeakMap would hit
        // n−1 times immediately. The reason is staleness: RHF mutates its
        // `_formValues` in place and yup hands back that same reference when
        // the object is unchanged, so an identity-keyed cache would happily
        // serve a re-key computed from *last* keystroke's values — silently
        // resolving `dependsOn` against stale state, which is a correctness
        // bug traded for 0.47 ms. Any cache here has to be invalidated by
        // something other than object identity.
        const values = toSchemaValues(
          schema,
          (this.parent ?? {}) as ConfigFormValues,
          fieldNames
        );
        if (!isFieldVisible(field, values, schema)) return true;

        const raw = (value ?? '').trim();

        // A stored secret is never echoed back — the input deliberately
        // resets to '' after save — so an empty required secret with
        // secretStatus true is *configured*, not missing. Same predicate as
        // `unfilledRequiredKeys`; diverging here paints "required" in red
        // next to the "Set" badge driven by the very same map.
        const storedSecret =
          field.type === 'secret' && Boolean(secretStatus?.[field.key]);
        if (field.required && raw === '' && !storedSecret) {
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

    shape[fieldNameOf(fieldNames, field.key)] = rule;
  }

  return yup.object(shape);
};

/**
 * The payload to send: changed non-secret keys plus non-empty secrets.
 *
 * A hidden field is excluded in both directions — its edit is not written back,
 * and its stored value is left alone. Switching an OAuth provider off must not
 * discard its client secret.
 *
 * `values` and `defaults` are register-name-keyed (they come straight from
 * `form.getValues()` and `buildDefaults`); the returned payload is keyed by
 * the schema key, which is what the backend stores and what the API contract
 * `{ name, environment, config?, secrets? }` requires.
 */
export const collectDiff = (
  schema: ConfigField[],
  values: ConfigFormValues,
  defaults: ConfigFormValues,
  fieldNames: ReadonlyMap<string, string> = buildFieldNames(schema)
): { config: ConfigValues; secrets: ConfigValues } => {
  const config: ConfigValues = {};
  const secrets: ConfigValues = {};

  // Re-keyed once up front so the visibility check, the comparison and the
  // payload below all speak the schema key.
  const current = toSchemaValues(schema, values, fieldNames);
  const baseline = toSchemaValues(schema, defaults, fieldNames);

  for (const field of schema) {
    if (!isFieldVisible(field, current, schema)) continue;
    const next = current[field.key] ?? '';
    if (field.type === 'secret') {
      if (next.trim() !== '') secrets[field.key] = next;
      continue;
    }
    if (next !== (baseline[field.key] ?? '')) config[field.key] = next;
  }

  return { config, secrets };
};

/**
 * Record-list slugs added in the UI and not yet saved, keyed by field. They
 * have to reach schema expansion or a freshly added element would have no
 * fields to fill in until after a save — which it cannot reach, because the
 * save is what carries them.
 */
export type PendingCreates = Readonly<Record<string, string[]>>;

/**
 * Module-scope and stable by reference, for the same reason EMPTY_SCHEMA is:
 * an inline `{}` default would mint a new object every render and rebuild the
 * expanded schema, the register-name map and the yup object on every
 * keystroke.
 */
export const EMPTY_CREATES: PendingCreates = {};

/** configValues with each field's roster extended by its pending creates. */
const withPendingCreates = (
  configValues: ConfigValues | undefined,
  pendingCreates: PendingCreates
): ConfigValues | undefined => {
  const fields = Object.keys(pendingCreates).filter(
    f => pendingCreates[f].length > 0
  );
  if (fields.length === 0) return configValues;
  const out: ConfigValues = { ...(configValues ?? {}) };
  for (const field of fields) {
    const key = rosterKeyOf(field);
    const stored = (out[key] ?? '')
      .split(',')
      .map(s => s.trim())
      .filter(Boolean);
    out[key] = [
      ...stored,
      ...pendingCreates[field].filter(s => !stored.includes(s))
    ].join(',');
  }
  return out;
};

/** Concatenation deduped by `key`, first occurrence winning. */
const unionByKey = (a: ConfigField[], b: ConfigField[]): ConfigField[] => {
  const seen = new Set<string>();
  const out: ConfigField[] = [];
  for (const f of [...a, ...b]) {
    if (seen.has(f.key)) continue;
    seen.add(f.key);
    out.push(f);
  }
  return out;
};

export interface ModuleConfigForm {
  form: UseFormReturn<ConfigFormValues>;
  defaults: ConfigFormValues;
  /**
   * The declared schema with every `recordList` replaced by its elements'
   * concrete fields (`expandSchema`). This — not the declared schema — is
   * what the form machine is built from: defaults, the yup object, the save
   * diff and the dirty/error tallies all iterate it, so the seven-type leaf
   * renderer and the resolver never learn an eighth case.
   *
   * Layout still iterates the DECLARED schema, which is where the record-list
   * container renders. The two are different lists on purpose.
   */
  expandedSchema: ConfigField[];
  /**
   * Schema key → react-hook-form register name, derived from the schema
   * exactly once per form. Every consumer that touches form state — the
   * field renderer's `register`/`Controller`/`errors` lookups, the
   * controller's dirty/error tallies, `collectDiff` — threads this same
   * instance rather than deriving its own, so the mapping is built once no
   * matter how many panels render.
   */
  fieldNames: ReadonlyMap<string, string>;
}

/**
 * One form instance for an entire module. The rail selects which slice is
 * rendered; it never remounts the form, so unsaved edits survive moving between
 * sections and the save bar can report them in aggregate.
 */
export const useModuleConfigForm = (
  schema: ConfigField[],
  configValues: ConfigValues | undefined,
  pendingCreates: PendingCreates = EMPTY_CREATES,
  secretStatus?: Record<string, boolean>
): ModuleConfigForm => {
  // A record list's membership is dynamic, so the schema the form is built
  // from has to be recomputed when it moves — both when the server's roster
  // changes and when the operator adds an element that has not been saved
  // yet. Memoised on the identities that decide the result.
  const rosterValues = useMemo(
    () => withPendingCreates(configValues, pendingCreates),
    [configValues, pendingCreates]
  );
  const expandedSchema = useMemo(
    () => expandSchema(schema, rosterValues),
    [schema, rosterValues]
  );
  // Names cover the declared schema AND the expanded leaves. The record-list
  // container holds no value, but the layout iterates the declared schema and
  // `fieldNameOf` throws rather than guessing — so the container needs a name
  // even though nothing ever registers it.
  const fieldNames = useMemo(
    () => buildFieldNames(unionByKey(schema, expandedSchema)),
    [schema, expandedSchema]
  );
  const defaults = buildDefaults(expandedSchema, configValues, fieldNames);
  // yupResolver validates the whole values object on every call, and
  // mode: 'onChange' calls it per keystroke — on a large module (auth's 63
  // fields) rebuilding the yup object from scratch on every render is pure
  // waste on top of that inherent per-keystroke validation cost. Rebuild
  // only when the schema itself changes.
  const resolver = useMemo(
    () =>
      yupResolver(
        buildYupSchema(expandedSchema, fieldNames, secretStatus)
      ) as unknown as Resolver<ConfigFormValues>,
    [expandedSchema, fieldNames, secretStatus]
  );
  const form = useForm<ConfigFormValues>({
    defaultValues: defaults,
    resolver,
    mode: 'onChange'
  });
  return { form, defaults, fieldNames, expandedSchema };
};
