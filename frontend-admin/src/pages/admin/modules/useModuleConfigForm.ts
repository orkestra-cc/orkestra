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
