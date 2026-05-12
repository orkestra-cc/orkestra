import { useCallback, useEffect, useMemo, useState } from 'react';
import { useBlocker } from 'react-router';
import { Alert, Button, Card, Modal, Nav, Spinner } from 'react-bootstrap';
import { OrkestraCardHeader } from 'components/common';
import type {
  ModuleConfig,
  EnvironmentConfigResponse
} from 'store/api/moduleApi';
import {
  useGetModuleEnvironmentQuery,
  useUpdateModuleEnvironmentMutation
} from 'store/api/moduleApi';
import ModuleConfigFields from '../ModuleConfigFields';
import { bucketByGroup } from '../utils';

interface ModuleConfigSectionProps {
  module: ModuleConfig;
  selectedEnvironment: string;
}

const ModuleConfigSection: React.FC<ModuleConfigSectionProps> = ({
  module: mod,
  selectedEnvironment
}) => {
  const { data: envConfig, isLoading: envLoading } =
    useGetModuleEnvironmentQuery(
      { name: mod.moduleName, environment: selectedEnvironment },
      { skip: !mod.availableEnvironments?.length }
    );

  const [updateEnv, { isLoading: saving }] =
    useUpdateModuleEnvironmentMutation();

  const [configValues, setConfigValues] = useState<Record<string, string>>({});
  const [secretValues, setSecretValues] = useState<Record<string, string>>({});
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState(false);
  const [activeTab, setActiveTab] = useState('');

  // Track the initial loaded values for dirty detection.
  const [loadedValues, setLoadedValues] = useState<Record<string, string>>({});

  // Reset form when environment data loads or changes.
  const resetForm = useCallback(
    (data: EnvironmentConfigResponse | undefined) => {
      if (data) {
        setConfigValues({ ...(data.configValues ?? {}) });
        setLoadedValues({ ...(data.configValues ?? {}) });
      } else {
        setConfigValues({ ...(mod.configValues ?? {}) });
        setLoadedValues({ ...(mod.configValues ?? {}) });
      }
      setSecretValues({});
      setError(null);
      setSuccess(false);
    },
    [mod.configValues]
  );

  useEffect(() => {
    resetForm(envConfig);
  }, [envConfig, resetForm]);

  const schema = mod.configSchema ?? [];
  const groupBuckets = useMemo(() => bucketByGroup(schema), [schema]);
  const showTabs = groupBuckets.length >= 2;
  const currentTab = activeTab || groupBuckets[0]?.group || '';

  const secretStatus = envConfig?.secretStatus ?? mod.secretStatus ?? {};

  // Dirty detection
  const isDirty = useMemo(() => {
    const hasSecrets = Object.values(secretValues).some(v => v.trim() !== '');
    if (hasSecrets) return true;
    for (const field of schema) {
      if (field.type === 'secret') continue;
      if ((configValues[field.key] || '') !== (loadedValues[field.key] || ''))
        return true;
    }
    return false;
  }, [configValues, loadedValues, secretValues, schema]);

  const handleSave = async () => {
    setError(null);
    setSuccess(false);

    try {
      const changedConfig: Record<string, string> = {};
      for (const field of schema) {
        if (field.type === 'secret') continue;
        if (
          (configValues[field.key] || '') !== (loadedValues[field.key] || '')
        ) {
          changedConfig[field.key] = configValues[field.key] || '';
        }
      }

      const newSecrets: Record<string, string> = {};
      for (const [key, value] of Object.entries(secretValues)) {
        if (value.trim()) newSecrets[key] = value;
      }

      if (
        Object.keys(changedConfig).length === 0 &&
        Object.keys(newSecrets).length === 0
      ) {
        return;
      }

      await updateEnv({
        name: mod.moduleName,
        environment: selectedEnvironment,
        config:
          Object.keys(changedConfig).length > 0 ? changedConfig : undefined,
        secrets: Object.keys(newSecrets).length > 0 ? newSecrets : undefined
      }).unwrap();

      setSuccess(true);
      setSecretValues({});
      setTimeout(() => setSuccess(false), 3000);
    } catch (err: unknown) {
      const message =
        err && typeof err === 'object' && 'data' in err
          ? String(
              (err as { data: { detail?: string } }).data?.detail ||
                'Update failed'
            )
          : 'Update failed';
      setError(message);
    }
  };

  const handleDiscard = () => {
    resetForm(envConfig);
  };

  // Block navigation when there are unsaved changes.
  const blocker = useBlocker(isDirty);

  if (schema.length === 0) {
    return (
      <Card className="mb-3">
        <OrkestraCardHeader title="Configuration" light={false} />
        <Card.Body className="text-muted text-center py-4 fs-10">
          This module has no configurable settings.
        </Card.Body>
      </Card>
    );
  }

  const renderFields = (keys?: string[]) => (
    <ModuleConfigFields
      schema={schema}
      includeKeys={keys}
      configValues={configValues}
      secretValues={secretValues}
      secretStatus={secretStatus}
      onConfigChange={(key, value) =>
        setConfigValues(prev => ({ ...prev, [key]: value }))
      }
      onSecretChange={(key, value) =>
        setSecretValues(prev => ({ ...prev, [key]: value }))
      }
    />
  );

  return (
    <>
      {blocker.state === 'blocked' && (
        <Modal show centered onHide={() => blocker.reset()}>
          <Modal.Header closeButton>
            <Modal.Title className="fs-8">Unsaved Changes</Modal.Title>
          </Modal.Header>
          <Modal.Body className="fs-10">
            You have unsaved configuration changes. Are you sure you want to
            leave?
          </Modal.Body>
          <Modal.Footer>
            <Button
              variant="secondary"
              size="sm"
              onClick={() => blocker.reset()}
            >
              Stay
            </Button>
            <Button
              variant="danger"
              size="sm"
              onClick={() => blocker.proceed()}
            >
              Leave
            </Button>
          </Modal.Footer>
        </Modal>
      )}

      <Card className="mb-3">
        <OrkestraCardHeader
          title="Configuration"
          light={false}
          endEl={
            envLoading ? <Spinner animation="border" size="sm" /> : undefined
          }
        />
        <Card.Body>
          {error && (
            <Alert
              variant="danger"
              className="fs-10"
              dismissible
              onClose={() => setError(null)}
            >
              {error}
            </Alert>
          )}
          {success && (
            <Alert variant="success" className="fs-10">
              Configuration saved successfully.
            </Alert>
          )}

          {showTabs ? (
            <>
              <Nav
                variant="tabs"
                activeKey={currentTab}
                onSelect={k => setActiveTab(k || '')}
                className="mb-3"
              >
                {groupBuckets.map(({ group }) => (
                  <Nav.Item key={group}>
                    <Nav.Link eventKey={group}>{group}</Nav.Link>
                  </Nav.Item>
                ))}
              </Nav>
              {groupBuckets.map(({ group, keys }) =>
                currentTab === group ? (
                  <div key={group}>{renderFields(keys)}</div>
                ) : null
              )}
            </>
          ) : (
            renderFields()
          )}

          <div className="d-flex justify-content-end gap-2 mt-3 pt-3 border-top">
            {isDirty && (
              <Button
                variant="outline-secondary"
                size="sm"
                onClick={handleDiscard}
              >
                Discard
              </Button>
            )}
            <Button
              variant="primary"
              size="sm"
              onClick={handleSave}
              disabled={saving || !isDirty}
            >
              {saving ? (
                <Spinner animation="border" size="sm" />
              ) : (
                'Save Changes'
              )}
            </Button>
          </div>
        </Card.Body>
      </Card>
    </>
  );
};

export default ModuleConfigSection;
