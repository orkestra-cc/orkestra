import { useMemo } from 'react';
import { useForm, type Resolver, type UseFormReturn } from 'react-hook-form';
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
    // A key of nothing but punctuation would sanitise to '' — still a usable
    // RHF name, but an unreadable one in the DOM and in devtools.
    const base = field.key.replace(/\W/g, '_') || 'field';
    let name = base;
    for (let n = 2; taken.has(name); n++) name = `${base}_${n}`;
    taken.add(name);
    names.set(field.key, name);
  }
  return names;
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
 * A missing name yields `''`, which `isFieldVisible` already treats exactly
 * like an absent key (both fall back to the target's declared default).
 */
export const toSchemaValues = (
  schema: ConfigField[],
  values: ConfigFormValues,
  fieldNames: ReadonlyMap<string, string> = buildFieldNames(schema)
): ConfigValues => {
  const out: ConfigValues = {};
  for (const field of schema) {
    out[field.key] = values[fieldNames.get(field.key) ?? field.key] ?? '';
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
    const name = fieldNames.get(f.key) ?? f.key;
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
  fieldNames: ReadonlyMap<string, string> = buildFieldNames(schema)
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
        // target. Deliberately per-test rather than cached across the pass:
        // even auth's 62 fields make this a few thousand map lookups per
        // keystroke, which is noise next to the 62 yup rules already running,
        // and a cache keyed on object identity would have to outlive the
        // validation pass to pay for itself.
        const values = toSchemaValues(
          schema,
          (this.parent ?? {}) as ConfigFormValues,
          fieldNames
        );
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

    shape[fieldNames.get(field.key) ?? field.key] = rule;
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

export interface ModuleConfigForm {
  form: UseFormReturn<ConfigFormValues>;
  defaults: ConfigFormValues;
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
  configValues: ConfigValues | undefined
): ModuleConfigForm => {
  // Memoised on the same `[schema]` identity the resolver keys off — see the
  // EMPTY_SCHEMA constant in useModuleConfigController for why a `?? []`
  // fallback anywhere upstream would quietly defeat both.
  const fieldNames = useMemo(() => buildFieldNames(schema), [schema]);
  const defaults = buildDefaults(schema, configValues, fieldNames);
  // yupResolver validates the whole values object on every call, and
  // mode: 'onChange' calls it per keystroke — on a large module (auth's 62
  // fields) rebuilding the yup object from scratch on every render is pure
  // waste on top of that inherent per-keystroke validation cost. Rebuild
  // only when the schema itself changes.
  const resolver = useMemo(
    () =>
      yupResolver(
        buildYupSchema(schema, fieldNames)
      ) as unknown as Resolver<ConfigFormValues>,
    [schema, fieldNames]
  );
  const form = useForm<ConfigFormValues>({
    defaultValues: defaults,
    resolver,
    mode: 'onChange'
  });
  return { form, defaults, fieldNames };
};
