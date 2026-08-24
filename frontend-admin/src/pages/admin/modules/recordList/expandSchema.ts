import type { ConfigField, FieldCondition } from 'store/api/moduleApi';

/** The SDK-owned key segments. Mirrors `recordlist_keys.go`. */
export const ROSTER_SUFFIX = '__items';
export const LABEL_SUFFIX = '__label';

export const rosterKeyOf = (field: string): string =>
  `${field}.${ROSTER_SUFFIX}`;

export const labelKeyOf = (field: string, slug: string): string =>
  `${field}.${slug}.${LABEL_SUFFIX}`;

export const itemKeyOf = (field: string, slug: string, sub: string): string =>
  `${field}.${slug}.${sub}`;

/**
 * The list's membership, in stored order. An absent roster is an empty list,
 * not an error — a module can declare a record list long before an operator
 * puts anything in it.
 */
export const rosterOf = (
  values: Record<string, string> | undefined,
  field: string
): string[] => {
  const raw = values?.[rosterKeyOf(field)]?.trim();
  if (!raw) return [];
  return raw
    .split(',')
    .map(s => s.trim())
    .filter(Boolean);
};

/**
 * One element's sub-fields as ordinary concrete fields, label first.
 *
 * `dependsOn` is rewritten onto this element. A sub-field's condition names a
 * *sibling sub-key* (`provider`), because that is the only thing an element
 * can reference — but once expanded, the values map is keyed by the full
 * `<field>.<slug>.<sub>` key. Leaving the condition alone would resolve it
 * against a key that never exists, so every conditional field inside every
 * element would be hidden forever, silently and with no error anywhere.
 */
export const expandElement = (
  field: ConfigField,
  slug: string
): ConfigField[] => {
  const rewrite = (c: FieldCondition): FieldCondition => ({
    ...c,
    key: itemKeyOf(field.key, slug, c.key)
  });
  return [
    {
      key: labelKeyOf(field.key, slug),
      label: 'Name',
      description: '',
      type: 'string',
      required: true,
      default: '',
      envVar: ''
    },
    ...(field.items ?? []).map(item => ({
      description: '',
      default: '',
      ...item,
      envVar: '',
      key: itemKeyOf(field.key, slug, item.key),
      dependsOn: item.dependsOn?.map(rewrite)
    }))
  ];
};

/**
 * The declared schema with every record list replaced by its elements'
 * concrete fields.
 *
 * The form machine — register names, defaults, the yup object, the save diff
 * — is built from a static schema. A record list is dynamic, so it is handed
 * this *expanded* schema instead, in which each element's sub-fields are
 * ordinary fields keyed `<field>.<slug>.<sub>`. The seven-type leaf renderer
 * therefore never learns an eighth case, and neither does the validator.
 *
 * The record-list field itself does NOT appear in the result: it holds no
 * value of its own, so registering it would create a form field with nothing
 * behind it. Layout still iterates the *declared* schema, which is where the
 * container renders.
 */
export const expandSchema = (
  schema: ConfigField[],
  values: Record<string, string> | undefined
): ConfigField[] =>
  schema.flatMap(field =>
    field.type === 'recordList'
      ? rosterOf(values, field.key).flatMap(slug => expandElement(field, slug))
      : [field]
  );
