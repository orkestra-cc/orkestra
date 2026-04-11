import { useMemo, useState } from 'react';
import { Alert, Button, Form, Modal, Spinner, Tab, Tabs } from 'react-bootstrap';
import { FalconCloseButton } from 'components/common';
import type { ConfigField, ModuleConfig } from 'store/api/moduleApi';
import { useUpdateModuleMutation } from 'store/api/moduleApi';
import ModuleConfigFields from './ModuleConfigFields';

// Bucket a schema into ordered groups. Preserves declaration order of both
// groups (first field of each group sets the tab order) and keys within each
// group. Fields with an empty `group` land in a trailing "General" bucket so
// modules can mix grouped and ungrouped fields without losing either.
const bucketByGroup = (
  schema: ConfigField[]
): { group: string; keys: string[] }[] => {
  const order: string[] = [];
  const buckets = new Map<string, string[]>();
  for (const field of schema) {
    const g = field.group || '';
    if (!buckets.has(g)) {
      buckets.set(g, []);
      order.push(g);
    }
    buckets.get(g)!.push(field.key);
  }
  // Move the ungrouped bucket to the end and label it "General".
  const result: { group: string; keys: string[] }[] = [];
  for (const g of order) {
    if (g === '') continue;
    result.push({ group: g, keys: buckets.get(g)! });
  }
  if (buckets.has('')) {
    result.push({ group: 'General', keys: buckets.get('')! });
  }
  return result;
};

interface ModuleConfigModalProps {
  module: ModuleConfig | null;
  show: boolean;
  onHide: () => void;
}

const ModuleConfigModal: React.FC<ModuleConfigModalProps> = ({
  module: mod,
  show,
  onHide,
}) => {
  const [updateModule, { isLoading }] = useUpdateModuleMutation();
  const [configValues, setConfigValues] = useState<Record<string, string>>({});
  const [secretValues, setSecretValues] = useState<Record<string, string>>({});
  const [enabled, setEnabled] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState(false);

  // Reset form when module changes
  const handleShow = () => {
    if (mod) {
      setConfigValues({ ...mod.configValues });
      setSecretValues({});
      setEnabled(mod.enabled);
      setError(null);
      setSuccess(false);
    }
  };

  const handleSave = async () => {
    if (!mod) return;
    setError(null);
    setSuccess(false);

    try {
      const body: {
        enabled?: boolean;
        config?: Record<string, string>;
        secrets?: Record<string, string>;
      } = {};

      if (enabled !== mod.enabled) {
        body.enabled = enabled;
      }

      // Collect changed config values (non-secret fields only)
      const changedConfig: Record<string, string> = {};
      for (const field of mod.configSchema) {
        if (field.type === 'secret') continue;
        if (configValues[field.key] !== mod.configValues[field.key]) {
          changedConfig[field.key] = configValues[field.key] || '';
        }
      }
      if (Object.keys(changedConfig).length > 0) {
        body.config = changedConfig;
      }

      // Collect non-empty secret values
      const newSecrets: Record<string, string> = {};
      for (const [key, value] of Object.entries(secretValues)) {
        if (value.trim()) {
          newSecrets[key] = value;
        }
      }
      if (Object.keys(newSecrets).length > 0) {
        body.secrets = newSecrets;
      }

      if (Object.keys(body).length === 0) {
        onHide();
        return;
      }

      await updateModule({ name: mod.moduleName, ...body }).unwrap();
      setSuccess(true);
      setTimeout(onHide, 800);
    } catch (err: unknown) {
      const message =
        err && typeof err === 'object' && 'data' in err
          ? String((err as { data: { detail?: string } }).data?.detail || 'Update failed')
          : 'Update failed';
      setError(message);
    }
  };

  const schema = mod?.configSchema ?? [];
  const groupBuckets = useMemo(() => bucketByGroup(schema), [schema]);
  const showTabs = groupBuckets.length >= 2;
  const [activeTab, setActiveTab] = useState<string>('');

  if (!mod) return null;

  const isCore = mod.category === 'core';
  const hasSchema = schema.length > 0;
  const currentTab = activeTab || groupBuckets[0]?.group || '';

  return (
    <Modal show={show} onHide={onHide} onShow={handleShow} size="lg">
      <Modal.Header>
        <Modal.Title className="fs-8">
          {mod.displayName}
          <span className="text-muted fs-10 ms-2">({mod.moduleName})</span>
        </Modal.Title>
        <FalconCloseButton onClick={onHide} />
      </Modal.Header>
      <Modal.Body>
        {error && <Alert variant="danger" className="fs-10">{error}</Alert>}
        {success && <Alert variant="success" className="fs-10">Module updated successfully</Alert>}

        <Form.Group className="mb-4">
          <Form.Check
            type="switch"
            id="module-enabled"
            label={enabled ? 'Enabled' : 'Disabled'}
            checked={enabled}
            disabled={isCore}
            onChange={(e) => setEnabled(e.target.checked)}
          />
          {isCore && (
            <Form.Text className="text-muted">Core modules cannot be disabled</Form.Text>
          )}
        </Form.Group>

        {mod.status === 'failed' && mod.error && (
          <Alert variant="danger" className="fs-10">
            <strong>Init Error:</strong> {mod.error}
          </Alert>
        )}

        {hasSchema && (
          <>
            <h6 className="fs-9 border-bottom pb-2 mb-3">Configuration</h6>
            {showTabs ? (
              <Tabs
                id={`module-config-tabs-${mod.moduleName}`}
                activeKey={currentTab}
                onSelect={(k) => setActiveTab(k || '')}
                className="mb-3"
              >
                {groupBuckets.map(({ group, keys }) => (
                  <Tab eventKey={group} title={group} key={group}>
                    <div className="pt-3">
                      <ModuleConfigFields
                        schema={mod.configSchema}
                        includeKeys={keys}
                        configValues={configValues}
                        secretValues={secretValues}
                        secretStatus={mod.secretStatus}
                        onConfigChange={(key, value) =>
                          setConfigValues((prev) => ({ ...prev, [key]: value }))
                        }
                        onSecretChange={(key, value) =>
                          setSecretValues((prev) => ({ ...prev, [key]: value }))
                        }
                      />
                    </div>
                  </Tab>
                ))}
              </Tabs>
            ) : (
              <ModuleConfigFields
                schema={mod.configSchema}
                configValues={configValues}
                secretValues={secretValues}
                secretStatus={mod.secretStatus}
                onConfigChange={(key, value) =>
                  setConfigValues((prev) => ({ ...prev, [key]: value }))
                }
                onSecretChange={(key, value) =>
                  setSecretValues((prev) => ({ ...prev, [key]: value }))
                }
              />
            )}
          </>
        )}

        {mod.dependsOn && mod.dependsOn.length > 0 && (
          <div className="mt-3 fs-10 text-muted">
            <strong>Dependencies:</strong> {mod.dependsOn.join(', ')}
          </div>
        )}
      </Modal.Body>
      <Modal.Footer>
        <Button variant="secondary" size="sm" onClick={onHide}>
          Cancel
        </Button>
        <Button variant="primary" size="sm" onClick={handleSave} disabled={isLoading}>
          {isLoading ? <Spinner animation="border" size="sm" /> : 'Save Changes'}
        </Button>
      </Modal.Footer>
    </Modal>
  );
};

export default ModuleConfigModal;
