import { useState } from 'react';
import { Form, InputGroup, Button } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { faEye, faEyeSlash } from '@fortawesome/free-solid-svg-icons';
import { useTranslation } from 'react-i18next';
import type { ConfigField } from 'store/api/moduleApi';
import { isFieldVisible } from './configModel';
import { translateConfigField } from 'helpers/configLabel';

/**
 * Mirrors Go's time.ParseDuration: an optional sign, then one or more
 * decimal-with-unit segments (ns, us/µs, ms, s, m, h). A bare zero is also
 * valid. The previous `^\d+[smh]$` rejected 1h30m, 500ms and 1.5h — all of
 * which the backend accepts, so the console was stricter than the contract.
 */
const DURATION_RE =
  /^[+-]?0$|^[+-]?((\d+(\.\d*)?|\.\d+)(ns|us|µs|μs|ms|s|m|h))+$/;

/** Compiles a schema-declared pattern, or null when it is not usable. */
const safeRegExp = (pattern?: string): RegExp | null => {
  if (!pattern) return null;
  try {
    return new RegExp(pattern);
  } catch {
    // The backend validator rejects an uncompilable pattern; if one still
    // reaches here, skipping the check beats throwing inside a render.
    return null;
  }
};

export interface ModuleConfigFieldsProps {
  schema: ConfigField[];
  configValues: Record<string, string>;
  secretValues: Record<string, string>;
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
  onConfigChange: (key: string, value: string) => void;
  onSecretChange: (key: string, value: string) => void;
}

/**
 * Dynamic form renderer for a backend module's `configSchema`. Its one
 * consumer is the admin module detail page's config section — the
 * first-install wizard only reads a `smtpConfigured` boolean and never
 * renders a schema. Handles all seven backend field types: string, bool,
 * int, duration, secret, enum, stringList.
 */
const ModuleConfigFields: React.FC<ModuleConfigFieldsProps> = ({
  schema,
  configValues,
  secretValues,
  secretStatus,
  includeKeys,
  moduleName,
  onConfigChange,
  onSecretChange
}) => {
  const { t } = useTranslation();
  const [revealedSecrets, setRevealedSecrets] = useState<
    Record<string, boolean>
  >({});

  const toggleReveal = (key: string) => {
    setRevealedSecrets(prev => ({ ...prev, [key]: !prev[key] }));
  };

  const selected = includeKeys
    ? includeKeys
        .map(key => schema.find(f => f.key === key))
        .filter((f): f is ConfigField => Boolean(f))
    : schema;
  const fields = selected.filter(f => isFieldVisible(f, configValues, schema));

  return (
    <>
      {fields.map(field => {
        const key = field.key;
        const label = translateConfigField(t, moduleName, field, 'label');
        const desc = translateConfigField(t, moduleName, field, 'desc');

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
              <InputGroup size="sm">
                <Form.Control
                  id={`cfg-${key}`}
                  type={revealed ? 'text' : 'password'}
                  placeholder={
                    alreadySet
                      ? t('adminModules.configFields.secretKeepPlaceholder')
                      : t('adminModules.configFields.secretEnterPlaceholder')
                  }
                  value={secretValues[key] || ''}
                  onChange={e => onSecretChange(key, e.target.value)}
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
              </InputGroup>
              {desc && <Form.Text className="text-muted">{desc}</Form.Text>}
            </Form.Group>
          );
        }

        if (field.type === 'bool') {
          // Mirror the enum branch: when no value is stored, fall back
          // to the schema default so the switch reflects what the
          // backend will actually enforce. Without this, default-true
          // toggles render OFF until the user explicitly saves a value
          // — admins read it as "disabled" and act on a wrong premise.
          const stored = configValues[key];
          const effective =
            stored !== undefined && stored !== ''
              ? stored
              : field.default || 'false';
          return (
            <Form.Group key={key} className="mb-3">
              <Form.Check
                id={`cfg-${key}`}
                type="switch"
                label={label}
                checked={effective === 'true'}
                onChange={e =>
                  onConfigChange(key, e.target.checked ? 'true' : 'false')
                }
              />
              {desc && <Form.Text className="text-muted">{desc}</Form.Text>}
            </Form.Group>
          );
        }

        if (field.type === 'enum') {
          const enumValue = configValues[key] ?? field.default ?? '';
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
                value={enumValue}
                onChange={e => onConfigChange(key, e.target.value)}
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
              {desc && <Form.Text className="text-muted">{desc}</Form.Text>}
            </Form.Group>
          );
        }

        const value = configValues[key] || '';
        const isEmpty = field.required && !value;
        const isDurationInvalid =
          field.type === 'duration' && value !== '' && !DURATION_RE.test(value);
        const numeric = field.type === 'int' ? Number(value) : NaN;
        const isBelowMin =
          field.min !== undefined && value !== '' && numeric < field.min;
        const isAboveMax =
          field.max !== undefined && value !== '' && numeric > field.max;
        const patternRe = safeRegExp(field.pattern);
        const isPatternInvalid =
          patternRe !== null && value !== '' && !patternRe.test(value);
        const isInvalid =
          isEmpty ||
          isDurationInvalid ||
          isBelowMin ||
          isAboveMax ||
          isPatternInvalid;
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
                value={value}
                onChange={e => onConfigChange(key, e.target.value)}
                isInvalid={isInvalid}
              />
            ) : (
              <Form.Control
                id={`cfg-${key}`}
                type={field.type === 'int' ? 'number' : 'text'}
                size="sm"
                placeholder={field.placeholder || field.default || ''}
                value={value}
                onChange={e => onConfigChange(key, e.target.value)}
                isInvalid={isInvalid}
              />
            )}
            {isEmpty && (
              <Form.Control.Feedback type="invalid">
                {t('adminModules.configFields.requiredFeedback')}
              </Form.Control.Feedback>
            )}
            {isDurationInvalid && (
              <Form.Control.Feedback type="invalid">
                {t('adminModules.configFields.durationFeedback')}
              </Form.Control.Feedback>
            )}
            {isBelowMin && (
              <Form.Control.Feedback type="invalid">
                {t('adminModules.configFields.minFeedback', { min: field.min })}
              </Form.Control.Feedback>
            )}
            {isAboveMax && (
              <Form.Control.Feedback type="invalid">
                {t('adminModules.configFields.maxFeedback', { max: field.max })}
              </Form.Control.Feedback>
            )}
            {isPatternInvalid && (
              <Form.Control.Feedback type="invalid">
                {t('adminModules.configFields.patternFeedback')}
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
