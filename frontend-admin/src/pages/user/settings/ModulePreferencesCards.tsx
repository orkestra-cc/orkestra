// One card per module that declares per-user preferences
// (module.HasUserPreferences): the declared fields rendered with the same
// generic form the admin module config uses, saved to
// PUT /v1/me/preferences/{module} with PUT semantics (the whole map). A module
// therefore ships no settings component of its own; it declares fields.
import { useEffect } from 'react';
import { Alert, Button, Card, Form, Spinner } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import { toast } from 'react-toastify';
import ModuleConfigFields from 'pages/admin/modules/ModuleConfigFields';
import {
  toSchemaValues,
  useModuleConfigForm
} from 'pages/admin/modules/useModuleConfigForm';
import {
  useGetMyPreferencesAllQuery,
  usePutMyPreferencesMutation,
  type ModulePreferences
} from 'store/api/userApi';

// The RTK error envelope: {status, data: {detail|message}} for a backend
// refusal (a 422 lists every invalid field in `detail`), an Error otherwise.
function extractError(err: unknown, unknownLabel: string): string {
  if (err && typeof err === 'object' && 'data' in err) {
    const data = (err as { data?: unknown }).data;
    if (data && typeof data === 'object') {
      const d = data as { detail?: string; message?: string };
      if (d.detail) return d.detail;
      if (d.message) return d.message;
    }
  }
  if (err instanceof Error && err.message) return err.message;
  return unknownLabel;
}

const ModulePreferencesCard: React.FC<{ prefs: ModulePreferences }> = ({
  prefs
}) => {
  const { t } = useTranslation();
  const [put, { isLoading: saving }] = usePutMyPreferencesMutation();
  const { form, defaults, expandedSchema, fieldNames } = useModuleConfigForm(
    prefs.fields,
    prefs.values
  );

  // A save answers the new stored values (RTK refetches the cache entry):
  // re-anchor the form on them so dirty tracking restarts from what the
  // server holds. Keyed on the server's values object, not on `defaults`,
  // which useModuleConfigForm rebuilds on every render — resetting on that
  // would loop.
  const { reset } = form;
  useEffect(() => {
    reset(defaults);
  }, [prefs.values, reset]);

  const onSubmit = form.handleSubmit(async values => {
    try {
      await put({
        module: prefs.module,
        values: toSchemaValues(expandedSchema, values, fieldNames)
      }).unwrap();
      toast.success(t('settings.modulePreferences.saved'));
    } catch (err) {
      toast.error(
        t('settings.modulePreferences.saveFailed', {
          error: extractError(err, t('settings.modulePreferences.unknownError'))
        })
      );
    }
  });

  return (
    <Card className="mb-3" data-testid={`module-preferences-${prefs.module}`}>
      <Card.Header className="bg-body-tertiary d-flex align-items-center justify-content-between">
        <h5 className="mb-0">
          {t(`${prefs.module}:displayName`, { defaultValue: prefs.module })}
        </h5>
        {prefs.updatedAt && (
          <small className="text-body-tertiary">
            {t('settings.modulePreferences.lastSaved', {
              when: new Date(prefs.updatedAt).toLocaleString()
            })}
          </small>
        )}
      </Card.Header>
      <Card.Body>
        <Form onSubmit={onSubmit} noValidate>
          <ModuleConfigFields
            schema={prefs.fields}
            control={form.control}
            register={form.register}
            fieldNames={fieldNames}
            moduleName={prefs.module}
          />
          <div className="d-flex justify-content-end mt-3">
            <Button
              type="submit"
              size="sm"
              disabled={saving || !form.formState.isDirty}
            >
              {saving
                ? t('settings.modulePreferences.saving')
                : t('settings.modulePreferences.save')}
            </Button>
          </div>
        </Form>
      </Card.Body>
    </Card>
  );
};

/** The settings-page host: a card per module with declared preferences. */
const ModulePreferencesCards: React.FC = () => {
  const { t } = useTranslation();
  const { data, isLoading, error } = useGetMyPreferencesAllQuery();

  if (isLoading) {
    return (
      <Card className="mb-3">
        <Card.Body className="text-center py-4">
          <Spinner animation="border" size="sm" role="status" />
        </Card.Body>
      </Card>
    );
  }
  // A 403 (no user.self: guest) or any other failure hides the block; the
  // rest of the settings page does not depend on it.
  if (error || !data) return null;
  if (data.modules.length === 0) return null;

  return (
    <>
      <Alert variant="light" className="border mb-3 py-2">
        <strong>{t('settings.modulePreferences.title')}</strong>
        <div className="fs-10 text-body-secondary">
          {t('settings.modulePreferences.description')}
        </div>
      </Alert>
      {data.modules.map(m => (
        <ModulePreferencesCard key={m.module} prefs={m} />
      ))}
    </>
  );
};

export default ModulePreferencesCards;
