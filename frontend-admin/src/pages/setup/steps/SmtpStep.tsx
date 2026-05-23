import { useState, useEffect, FormEvent } from 'react';
import { Alert, Button, Form, Spinner } from 'react-bootstrap';
import { Trans, useTranslation } from 'react-i18next';
import {
  useGetModuleQuery,
  useUpdateModuleMutation
} from 'store/api/moduleApi';
import ModuleConfigFields from 'pages/admin/modules/ModuleConfigFields';

interface SmtpStepProps {
  onNext: () => void;
  onSkip: () => void;
}

// The subset of notification-module fields the wizard asks about. We keep
// app-identity fields (app.name, app.support_email) out of the wizard
// because they have sensible defaults and the admin UI handles them later.
const SMTP_FIELD_KEYS = [
  'email.provider',
  'email.from_address',
  'email.from_name',
  'email.reply_to',
  'email.smtp.host',
  'email.smtp.port',
  'email.smtp.username',
  'email.smtp.password',
  'email.smtp.tls_mode'
];

/**
 * Third step of the setup wizard: configure the notification module's SMTP
 * settings. Reuses the shared ModuleConfigFields component so the field
 * rendering stays identical to /admin/modules. Skippable — if the operator
 * chooses to configure SMTP later, the notification module stays in noop
 * mode and auth mail logs to stdout.
 */
const SmtpStep = ({ onNext, onSkip }: SmtpStepProps) => {
  const { t } = useTranslation();
  const { data: mod, isLoading: isLoadingModule } =
    useGetModuleQuery('notification');
  const [updateModule, { isLoading: isSaving }] = useUpdateModuleMutation();

  const [configValues, setConfigValues] = useState<Record<string, string>>({});
  const [secretValues, setSecretValues] = useState<Record<string, string>>({});
  const [error, setError] = useState<string | null>(null);

  // Seed the form from the backend's current values (env-var bootstrapped
  // defaults for a fresh install) as soon as the module fetch completes.
  useEffect(() => {
    if (mod) {
      setConfigValues({ ...mod.configValues });
      setSecretValues({});
    }
  }, [mod]);

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault();
    setError(null);

    if (!mod) return;

    // Minimal sanity check: if the operator picked a non-noop provider, they
    // must give us at least a host and a from address. Otherwise nothing
    // will actually send.
    const provider = (configValues['email.provider'] || '').trim();
    if (provider && provider !== 'noop') {
      if (!configValues['email.smtp.host']) {
        setError(t('setup.smtp.errorHostRequired'));
        return;
      }
      if (!configValues['email.from_address']) {
        setError(t('setup.smtp.errorFromRequired'));
        return;
      }
    }

    try {
      // Collect changed plain values relative to what the backend reported.
      const changedConfig: Record<string, string> = {};
      for (const key of SMTP_FIELD_KEYS) {
        const field = mod.configSchema.find(f => f.key === key);
        if (!field || field.type === 'secret') continue;
        if (configValues[key] !== mod.configValues[key]) {
          changedConfig[key] = configValues[key] || '';
        }
      }

      const newSecrets: Record<string, string> = {};
      for (const [key, value] of Object.entries(secretValues)) {
        if (value.trim()) newSecrets[key] = value;
      }

      const body: {
        enabled?: boolean;
        config?: Record<string, string>;
        secrets?: Record<string, string>;
      } = {};
      if (Object.keys(changedConfig).length > 0) body.config = changedConfig;
      if (Object.keys(newSecrets).length > 0) body.secrets = newSecrets;

      if (Object.keys(body).length > 0) {
        await updateModule({ name: 'notification', ...body }).unwrap();
      }

      onNext();
    } catch (err: unknown) {
      const anyErr = err as { data?: { detail?: string } };
      setError(anyErr?.data?.detail || t('setup.smtp.errorGeneric'));
    }
  };

  if (isLoadingModule || !mod) {
    return (
      <div className="text-center py-4">
        <Spinner animation="border" />
      </div>
    );
  }

  return (
    <Form onSubmit={handleSubmit} noValidate>
      <div className="mb-4">
        <h5 className="mb-1">{t('setup.smtp.title')}</h5>
        <p className="text-muted fs-10 mb-0">
          <Trans i18nKey="setup.smtp.intro" components={{ code: <code /> }} />
        </p>
      </div>

      {error && (
        <Alert
          variant="danger"
          className="mb-3"
          onClose={() => setError(null)}
          dismissible
        >
          {error}
        </Alert>
      )}

      <Alert variant="warning" className="fs-10 mb-3">
        <Trans
          i18nKey="setup.smtp.noopNotice"
          components={{ strong: <strong />, code: <code /> }}
        />
      </Alert>

      <ModuleConfigFields
        schema={mod.configSchema}
        configValues={configValues}
        secretValues={secretValues}
        secretStatus={mod.secretStatus}
        includeKeys={SMTP_FIELD_KEYS}
        onConfigChange={(key, value) =>
          setConfigValues(prev => ({ ...prev, [key]: value }))
        }
        onSecretChange={(key, value) =>
          setSecretValues(prev => ({ ...prev, [key]: value }))
        }
      />

      <div className="d-flex justify-content-between">
        <Button
          variant="outline-secondary"
          onClick={onSkip}
          disabled={isSaving}
        >
          {t('setup.smtp.skip')}
        </Button>
        <Button type="submit" variant="primary" disabled={isSaving}>
          {isSaving ? (
            <>
              <Spinner animation="border" size="sm" className="me-2" />
              {t('setup.smtp.submitting')}
            </>
          ) : (
            t('setup.smtp.submit')
          )}
        </Button>
      </div>
    </Form>
  );
};

export default SmtpStep;
