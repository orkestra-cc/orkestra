import { useState } from 'react';
import { Form, InputGroup, Button } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { faEye, faEyeSlash } from '@fortawesome/free-solid-svg-icons';
import { useTranslation } from 'react-i18next';
import type { TFunction } from 'i18next';
import {
  Controller,
  useFormState,
  useWatch,
  type Control,
  type UseFormRegister
} from 'react-hook-form';
import type { ConfigField } from 'store/api/moduleApi';
import { isFieldVisible } from './configModel';
import { translateConfigField } from 'helpers/configLabel';
import { RecordListField } from './recordList/RecordListField';
import { expandElement, labelKeyOf } from './recordList/expandSchema';
import { useRecordListEditing } from './recordList/RecordListContext';
import {
  fieldNameOf,
  toSchemaValues,
  type ConfigFormValues
} from './useModuleConfigForm';

/**
 * Translates a resolver error code (see `buildYupSchema` in
 * `useModuleConfigForm.ts`) through the existing `adminModules.configFields.*`
 * feedback keys instead of adding a parallel set of English strings. The
 * resolver's single `.test()` rule per field means at most one code is ever
 * active at once, so this returns one message, not several.
 *
 * `notANumber` (an `int` field holding a non-numeric string) reuses
 * `patternFeedback` — "Value does not match the required format" reads fine
 * for that case too, and the brief calls for reuse over a parallel key.
 */
const feedbackForCode = (t: TFunction, code: string): string => {
  if (code === 'required')
    return t('adminModules.configFields.requiredFeedback');
  if (code === 'duration')
    return t('adminModules.configFields.durationFeedback');
  if (code === 'pattern' || code === 'notANumber') {
    return t('adminModules.configFields.patternFeedback');
  }
  if (code.startsWith('min:')) {
    return t('adminModules.configFields.minFeedback', { min: code.slice(4) });
  }
  if (code.startsWith('max:')) {
    return t('adminModules.configFields.maxFeedback', { max: code.slice(4) });
  }
  return code;
};

export interface ModuleConfigFieldsProps {
  schema: ConfigField[];
  /** The single module-wide react-hook-form instance (Task 1's `useModuleConfigForm`). */
  control: Control<ConfigFormValues>;
  register: UseFormRegister<ConfigFormValues>;
  /**
   * Schema key → react-hook-form register name (`buildFieldNames`). Every
   * `register`/`Controller`/`errors` lookup below goes through it, because a
   * key like `email.smtp.host` handed to RHF verbatim is read as a path and
   * nests the operator's edit out of sight of dirty tracking and the save
   * diff. Everything else here — `includeKeys`, `dependsOn`, `secretStatus`,
   * the i18n lookups — still keys by the schema key.
   */
  fieldNames: ReadonlyMap<string, string>;
  /**
   * Map of secret key → whether that secret is already stored on the backend.
   * Controls the "Set" badge and the placeholder hint for password inputs.
   */
  secretStatus?: Record<string, boolean>;
  /**
   * Optional allow-list of field keys to render. When provided, only these
   * fields are shown and in this order. Falls back to the full schema order.
   */
  includeKeys?: string[];
  /** Owning module — selects the i18n namespace the labels resolve against. */
  moduleName: string;
}

/**
 * Dynamic form renderer for a backend module's `configSchema`, registered
 * against the module-wide react-hook-form instance rather than owning its
 * own state — so an edit here survives the rail moving to a different group.
 * Its consumers are the admin module detail page's config section (directly,
 * for the flat/legacy layout) and `ModuleConfigPanel` (one group at a time).
 * Handles all seven leaf field types: string, bool, int, duration, secret,
 * enum, stringList. The eighth, `recordList`, is not a leaf — it delegates to
 * `RecordListField`, which calls back into this component for each element's
 * own concrete fields.
 *
 * Field labels below carry **no typography classes on purpose**. The theme
 * already puts `.form-label` on the scale (`$form-label-font-size: fs-10`,
 * weight 500), and a `bool` field's label is a `.form-check-label` that
 * `<Form.Check label=…>` renders for us — there is no prop to hand it the same
 * classes. Spelling `fs-10 fw-semibold` on the other three types therefore
 * bought a redundant size and a *divergent* weight (600 vs the switches' 500),
 * visible as two label weights side by side in one panel. Leave them bare so
 * every field type resolves to the same rule.
 */
const ModuleConfigFields: React.FC<ModuleConfigFieldsProps> = ({
  schema,
  control,
  register,
  fieldNames,
  secretStatus,
  includeKeys,
  moduleName
}) => {
  const { t } = useTranslation();
  const recordList = useRecordListEditing();
  const [revealedSecrets, setRevealedSecrets] = useState<
    Record<string, boolean>
  >({});
  // Whole-form watch: a field's visibility can depend on any other field in
  // the schema (dependsOn), not just ones this particular panel renders.
  // Re-keyed to schema keys because `dependsOn` targets are schema keys.
  const watched = useWatch({ control }) as ConfigFormValues;
  const values = toSchemaValues(schema, watched, fieldNames);
  const { errors } = useFormState({ control });

  const toggleReveal = (key: string) => {
    setRevealedSecrets(prev => ({ ...prev, [key]: !prev[key] }));
  };

  const selected = includeKeys
    ? includeKeys
        .map(key => schema.find(f => f.key === key))
        .filter((f): f is ConfigField => Boolean(f))
    : schema;
  const fields = selected.filter(f => isFieldVisible(f, values, schema));

  return (
    <>
      {fields.map(field => {
        const key = field.key;

        // A record list holds no value of its own, so it never registers.
        // The container owns the card chrome and the membership intents; each
        // element's body comes back through THIS renderer, one element's
        // concrete fields at a time — which is why the seven leaf branches
        // below never learn an eighth case.
        if (field.type === 'recordList') {
          const roster = recordList?.rosterFor(key) ?? [];
          return (
            <RecordListField
              key={key}
              field={field}
              moduleName={moduleName}
              roster={roster}
              labels={Object.fromEntries(
                roster.map(slug => [
                  slug,
                  watched[
                    fieldNameOf(fieldNames, labelKeyOf(key, slug))
                  ] as string
                ])
              )}
              staged={recordList?.stagedFor(key) ?? []}
              onCreate={(slug, label) => recordList?.create(key, slug, label)}
              onStageRemove={slug => recordList?.stageRemove(key, slug)}
              onUndoRemove={slug => recordList?.undoRemove(key, slug)}
              renderElement={slug => (
                <ModuleConfigFields
                  schema={expandElement(field, slug)}
                  moduleName={moduleName}
                  control={control}
                  register={register}
                  fieldNames={fieldNames}
                  secretStatus={secretStatus}
                />
              )}
            />
          );
        }

        // Everything RHF touches — register, Controller, the errors lookup,
        // and the DOM id/name that mirror them — goes through `name`.
        // Everything else stays on `key`.
        const name = fieldNameOf(fieldNames, key);
        const label = translateConfigField(t, moduleName, field, 'label');
        const desc = translateConfigField(t, moduleName, field, 'desc');
        const fieldError = errors[name]?.message as string | undefined;

        if (field.type === 'secret') {
          const alreadySet = Boolean(secretStatus?.[key]);
          const revealed = revealedSecrets[key] || false;
          return (
            <Form.Group key={key} className="mb-3">
              <Form.Label htmlFor={`cfg-${name}`}>
                {label}
                {alreadySet && (
                  <span className="badge badge-subtle-success ms-2 fs-11">
                    {t('adminModules.configFields.secretSetBadge')}
                  </span>
                )}
              </Form.Label>
              <InputGroup size="sm" hasValidation>
                <Form.Control
                  id={`cfg-${name}`}
                  type={revealed ? 'text' : 'password'}
                  placeholder={
                    alreadySet
                      ? t('adminModules.configFields.secretKeepPlaceholder')
                      : t('adminModules.configFields.secretEnterPlaceholder')
                  }
                  isInvalid={Boolean(fieldError)}
                  aria-invalid={fieldError ? true : undefined}
                  aria-describedby={
                    fieldError ? `cfg-${name}-feedback` : undefined
                  }
                  {...register(name)}
                />
                <Button
                  variant="outline-secondary"
                  onClick={() => toggleReveal(key)}
                  title={
                    revealed
                      ? t('adminModules.configFields.secretHide')
                      : t('adminModules.configFields.secretShow')
                  }
                >
                  <FontAwesomeIcon icon={revealed ? faEyeSlash : faEye} />
                </Button>
                {fieldError && (
                  <Form.Control.Feedback
                    type="invalid"
                    id={`cfg-${name}-feedback`}
                  >
                    {feedbackForCode(t, fieldError)}
                  </Form.Control.Feedback>
                )}
              </InputGroup>
              {desc && <Form.Text className="text-muted">{desc}</Form.Text>}
            </Form.Group>
          );
        }

        if (field.type === 'bool') {
          return (
            <Form.Group key={key} className="mb-3">
              <Controller
                name={name}
                control={control}
                render={({ field: rhfField }) => (
                  <Form.Check
                    id={`cfg-${name}`}
                    type="switch"
                    label={label}
                    name={rhfField.name}
                    checked={rhfField.value === 'true'}
                    onChange={e =>
                      rhfField.onChange(e.target.checked ? 'true' : 'false')
                    }
                    onBlur={rhfField.onBlur}
                    ref={rhfField.ref}
                  />
                )}
              />
              {desc && <Form.Text className="text-muted">{desc}</Form.Text>}
            </Form.Group>
          );
        }

        if (field.type === 'enum') {
          const options = field.options ?? [];
          return (
            <Form.Group key={key} className="mb-3">
              <Form.Label htmlFor={`cfg-${name}`}>
                {label}
                {field.required && (
                  <span className="text-danger ms-1" aria-hidden="true">
                    *
                  </span>
                )}
              </Form.Label>
              <Form.Select
                id={`cfg-${name}`}
                size="sm"
                isInvalid={Boolean(fieldError)}
                aria-invalid={fieldError ? true : undefined}
                aria-describedby={
                  fieldError ? `cfg-${name}-feedback` : undefined
                }
                aria-required={field.required || undefined}
                {...register(name)}
              >
                {!field.required && (
                  <option value="">
                    {t('adminModules.configFields.enumNonePlaceholder')}
                  </option>
                )}
                {options.map(opt => (
                  <option key={opt} value={opt}>
                    {opt}
                  </option>
                ))}
              </Form.Select>
              {fieldError && (
                <Form.Control.Feedback
                  type="invalid"
                  id={`cfg-${name}-feedback`}
                >
                  {feedbackForCode(t, fieldError)}
                </Form.Control.Feedback>
              )}
              {desc && <Form.Text className="text-muted">{desc}</Form.Text>}
            </Form.Group>
          );
        }

        const isStringList = field.type === 'stringList';

        return (
          <Form.Group key={key} className="mb-3">
            <Form.Label htmlFor={`cfg-${name}`}>
              {label}
              {field.required && (
                <span className="text-danger ms-1" aria-hidden="true">
                  *
                </span>
              )}
            </Form.Label>
            {isStringList ? (
              <Form.Control
                id={`cfg-${name}`}
                as="textarea"
                rows={2}
                size="sm"
                placeholder={
                  field.placeholder ||
                  field.default ||
                  t('adminModules.configFields.stringListPlaceholder')
                }
                isInvalid={Boolean(fieldError)}
                aria-invalid={fieldError ? true : undefined}
                aria-describedby={
                  fieldError ? `cfg-${name}-feedback` : undefined
                }
                aria-required={field.required || undefined}
                {...register(name)}
              />
            ) : (
              <Form.Control
                id={`cfg-${name}`}
                type={field.type === 'int' ? 'number' : 'text'}
                size="sm"
                placeholder={field.placeholder || field.default || ''}
                isInvalid={Boolean(fieldError)}
                aria-invalid={fieldError ? true : undefined}
                aria-describedby={
                  fieldError ? `cfg-${name}-feedback` : undefined
                }
                aria-required={field.required || undefined}
                {...register(name)}
              />
            )}
            {fieldError && (
              <Form.Control.Feedback type="invalid" id={`cfg-${name}-feedback`}>
                {feedbackForCode(t, fieldError)}
              </Form.Control.Feedback>
            )}
            {/* `desc` used to render only *inside* the `field.envVar` guard,
                so a documented field that declares no env var showed no help
                at all — while `secret`/`bool`/`enum` rendered theirs
                unconditionally. The description is the operator-facing half
                and comes first; the env var is the deployment detail and
                trails it. */}
            {(desc || field.envVar) && (
              <Form.Text className="text-muted">
                {desc}
                {desc && field.envVar && ' — '}
                {field.envVar && (
                  <>
                    {t('adminModules.configFields.envPrefix')}
                    <code>{field.envVar}</code>
                  </>
                )}
              </Form.Text>
            )}
          </Form.Group>
        );
      })}
    </>
  );
};

export default ModuleConfigFields;
