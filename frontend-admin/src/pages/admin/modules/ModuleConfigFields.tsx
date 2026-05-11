import { useState } from 'react';
import { Form, InputGroup, Button } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { faEye, faEyeSlash } from '@fortawesome/free-solid-svg-icons';
import type { ConfigField } from 'store/api/moduleApi';

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
  onConfigChange: (key: string, value: string) => void;
  onSecretChange: (key: string, value: string) => void;
}

/**
 * Dynamic form renderer for a backend module's `configSchema`. Shared by
 * the admin modules page (edit an arbitrary module) and the first-install
 * onboarding wizard (configure SMTP before any user exists). Handles all
 * four backend field types: string, int, bool, secret.
 */
const ModuleConfigFields: React.FC<ModuleConfigFieldsProps> = ({
  schema,
  configValues,
  secretValues,
  secretStatus,
  includeKeys,
  onConfigChange,
  onSecretChange,
}) => {
  const [revealedSecrets, setRevealedSecrets] = useState<Record<string, boolean>>({});

  const toggleReveal = (key: string) => {
    setRevealedSecrets((prev) => ({ ...prev, [key]: !prev[key] }));
  };

  const fields = includeKeys
    ? (includeKeys
        .map((key) => schema.find((f) => f.key === key))
        .filter((f): f is ConfigField => Boolean(f)))
    : schema;

  return (
    <>
      {fields.map((field) => {
        const key = field.key;

        if (field.type === 'secret') {
          const alreadySet = Boolean(secretStatus?.[key]);
          const revealed = revealedSecrets[key] || false;
          return (
            <Form.Group key={key} className="mb-3">
              <Form.Label className="fs-10 fw-semibold">
                {field.label}
                {alreadySet && (
                  <span className="badge badge-subtle-success ms-2 fs-11">Set</span>
                )}
              </Form.Label>
              <InputGroup size="sm">
                <Form.Control
                  type={revealed ? 'text' : 'password'}
                  placeholder={alreadySet ? 'Leave empty to keep current' : 'Enter value'}
                  value={secretValues[key] || ''}
                  onChange={(e) => onSecretChange(key, e.target.value)}
                />
                <Button
                  variant="outline-secondary"
                  onClick={() => toggleReveal(key)}
                  title={revealed ? 'Hide' : 'Show'}
                >
                  <FontAwesomeIcon icon={revealed ? faEyeSlash : faEye} />
                </Button>
              </InputGroup>
              {field.description && (
                <Form.Text className="text-muted">{field.description}</Form.Text>
              )}
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
          const effective = stored !== undefined && stored !== '' ? stored : (field.default || 'false');
          return (
            <Form.Group key={key} className="mb-3">
              <Form.Check
                type="switch"
                label={field.label}
                checked={effective === 'true'}
                onChange={(e) => onConfigChange(key, e.target.checked ? 'true' : 'false')}
              />
              {field.description && (
                <Form.Text className="text-muted">{field.description}</Form.Text>
              )}
            </Form.Group>
          );
        }

        if (field.type === 'enum') {
          const enumValue = configValues[key] ?? field.default ?? '';
          const options = field.options ?? [];
          return (
            <Form.Group key={key} className="mb-3">
              <Form.Label className="fs-10 fw-semibold">
                {field.label}
                {field.required && <span className="text-danger ms-1">*</span>}
              </Form.Label>
              <Form.Select
                size="sm"
                value={enumValue}
                onChange={(e) => onConfigChange(key, e.target.value)}
              >
                {!field.required && <option value="">—</option>}
                {options.map((opt) => (
                  <option key={opt} value={opt}>
                    {opt}
                  </option>
                ))}
              </Form.Select>
              {field.description && (
                <Form.Text className="text-muted">{field.description}</Form.Text>
              )}
            </Form.Group>
          );
        }

        const value = configValues[key] || '';
        const isEmpty = field.required && !value;
        const isDurationInvalid = field.type === 'duration' && value !== '' && !/^\d+[smh]$/.test(value);
        const isStringList = field.type === 'stringList';

        return (
          <Form.Group key={key} className="mb-3">
            <Form.Label className="fs-10 fw-semibold">
              {field.label}
              {field.required && <span className="text-danger ms-1">*</span>}
            </Form.Label>
            {isStringList ? (
              <Form.Control
                as="textarea"
                rows={2}
                size="sm"
                placeholder={field.default || 'comma,separated,values'}
                value={value}
                onChange={(e) => onConfigChange(key, e.target.value)}
                isInvalid={isEmpty}
              />
            ) : (
              <Form.Control
                type={field.type === 'int' ? 'number' : 'text'}
                size="sm"
                placeholder={field.default || ''}
                value={value}
                onChange={(e) => onConfigChange(key, e.target.value)}
                isInvalid={isEmpty || isDurationInvalid}
              />
            )}
            {isEmpty && (
              <Form.Control.Feedback type="invalid">
                This field is required.
              </Form.Control.Feedback>
            )}
            {isDurationInvalid && (
              <Form.Control.Feedback type="invalid">
                Enter a valid duration (e.g. 30s, 5m, 1h).
              </Form.Control.Feedback>
            )}
            {field.envVar && (
              <Form.Text className="text-muted">
                Env: <code>{field.envVar}</code>
                {field.description ? ` — ${field.description}` : ''}
              </Form.Text>
            )}
          </Form.Group>
        );
      })}
    </>
  );
};

export default ModuleConfigFields;
