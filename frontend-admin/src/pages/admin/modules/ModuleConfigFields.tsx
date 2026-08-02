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
import type { ConfigFormValues } from './useModuleConfigForm';

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
 * Handles all seven backend field types: string, bool, int, duration, secret,
 * enum, stringList.
 */
const ModuleConfigFields: React.FC<ModuleConfigFieldsProps> = ({
  schema,
  control,
  register,
  secretStatus,
  includeKeys,
  moduleName
}) => {
  const { t } = useTranslation();
  const [revealedSecrets, setRevealedSecrets] = useState<
    Record<string, boolean>
  >({});
  // Whole-form watch: a field's visibility can depend on any other field in
  // the schema (dependsOn), not just ones this particular panel renders.
  const values = useWatch({ control }) as ConfigFormValues;
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
        const label = translateConfigField(t, moduleName, field, 'label');
        const desc = translateConfigField(t, moduleName, field, 'desc');
        const fieldError = errors[key]?.message as string | undefined;

        if (field.type === 'secret') {
          const alreadySet = Boolean(secretStatus?.[key]);
          const revealed = revealedSecrets[key] || false;
          return (
            <Form.Group key={key} className="mb-3">
              <Form.Label className="fs-10 fw-semibold" htmlFor={`cfg-${key}`}>
                {label}
                {alreadySet && (
                  <span className="badge badge-subtle-success ms-2 fs-11">
                    {t('adminModules.configFields.secretSetBadge')}
                  </span>
                )}
              </Form.Label>
              <InputGroup size="sm" hasValidation>
                <Form.Control
                  id={`cfg-${key}`}
                  type={revealed ? 'text' : 'password'}
                  placeholder={
                    alreadySet
                      ? t('adminModules.configFields.secretKeepPlaceholder')
                      : t('adminModules.configFields.secretEnterPlaceholder')
                  }
                  isInvalid={Boolean(fieldError)}
                  {...register(key)}
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
                  <Form.Control.Feedback type="invalid">
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
                name={key}
                control={control}
                render={({ field: rhfField }) => (
                  <Form.Check
                    id={`cfg-${key}`}
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
              <Form.Label className="fs-10 fw-semibold" htmlFor={`cfg-${key}`}>
                {label}
                {field.required && <span className="text-danger ms-1">*</span>}
              </Form.Label>
              <Form.Select
                id={`cfg-${key}`}
                size="sm"
                isInvalid={Boolean(fieldError)}
                {...register(key)}
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
                <Form.Control.Feedback type="invalid">
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
            <Form.Label className="fs-10 fw-semibold" htmlFor={`cfg-${key}`}>
              {label}
              {field.required && <span className="text-danger ms-1">*</span>}
            </Form.Label>
            {isStringList ? (
              <Form.Control
                id={`cfg-${key}`}
                as="textarea"
                rows={2}
                size="sm"
                placeholder={
                  field.placeholder ||
                  field.default ||
                  t('adminModules.configFields.stringListPlaceholder')
                }
                isInvalid={Boolean(fieldError)}
                {...register(key)}
              />
            ) : (
              <Form.Control
                id={`cfg-${key}`}
                type={field.type === 'int' ? 'number' : 'text'}
                size="sm"
                placeholder={field.placeholder || field.default || ''}
                isInvalid={Boolean(fieldError)}
                {...register(key)}
              />
            )}
            {fieldError && (
              <Form.Control.Feedback type="invalid">
                {feedbackForCode(t, fieldError)}
              </Form.Control.Feedback>
            )}
            {field.envVar && (
              <Form.Text className="text-muted">
                {t('adminModules.configFields.envPrefix')}
                <code>{field.envVar}</code>
                {desc ? ` — ${desc}` : ''}
              </Form.Text>
            )}
          </Form.Group>
        );
      })}
    </>
  );
};

export default ModuleConfigFields;
