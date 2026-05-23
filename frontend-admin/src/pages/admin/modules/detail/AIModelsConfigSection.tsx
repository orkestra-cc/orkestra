import { useCallback, useEffect, useMemo, useState } from 'react';
import { useBlocker, useSearchParams } from 'react-router';
import {
  Alert,
  Badge,
  Button,
  Card,
  Form,
  Modal,
  Nav,
  Spinner,
  Table
} from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import { OrkestraCardHeader } from 'components/common';
import type {
  ModuleConfig,
  EnvironmentConfigResponse
} from 'store/api/moduleApi';
import {
  useGetModuleEnvironmentQuery,
  useUpdateModuleEnvironmentMutation
} from 'store/api/moduleApi';
import {
  useListAIModelsQuery,
  useDeleteAIModelMutation,
  useSetDefaultAIModelMutation,
  useTestAIModelMutation,
  useUpdateAIModelMutation
} from 'store/api/aiModelsApi';
import type { AIModelConfig, Provider } from 'types/aiModels';
import { PROVIDER_INFO } from 'types/aiModels';
import ModuleConfigFields from '../ModuleConfigFields';
import ModelFormModal from 'pages/ai/models/ModelFormModal';

interface AIModelsConfigSectionProps {
  module: ModuleConfig;
  selectedEnvironment: string;
}

const PROVIDERS: Provider[] = ['ollama', 'openai', 'anthropic', 'gemini'];

const PROVIDER_CONFIG_KEYS: Record<Provider, string[]> = {
  ollama: ['ollamaBaseURL'],
  openai: ['openaiKey'],
  anthropic: ['anthropicKey'],
  gemini: ['geminiKey']
};

const AIModelsConfigSection: React.FC<AIModelsConfigSectionProps> = ({
  module: mod,
  selectedEnvironment
}) => {
  const { t } = useTranslation();
  // --- URL-synced tab ---
  const [searchParams, setSearchParams] = useSearchParams();
  const activeTab = (searchParams.get('tab') as Provider) || 'ollama';
  const handleTabSelect = (key: string | null) => {
    if (!key) return;
    setSearchParams(
      prev => {
        prev.set('tab', key);
        return prev;
      },
      { replace: true }
    );
  };

  // --- Module config (API keys / base URLs) ---
  const { data: envConfig, isLoading: envLoading } =
    useGetModuleEnvironmentQuery(
      { name: mod.moduleName, environment: selectedEnvironment },
      { skip: !mod.availableEnvironments?.length }
    );

  const [updateEnv, { isLoading: saving }] =
    useUpdateModuleEnvironmentMutation();

  const [configValues, setConfigValues] = useState<Record<string, string>>({});
  const [secretValues, setSecretValues] = useState<Record<string, string>>({});
  const [loadedValues, setLoadedValues] = useState<Record<string, string>>({});
  const [configError, setConfigError] = useState<string | null>(null);
  const [configSuccess, setConfigSuccess] = useState(false);

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
      setConfigError(null);
      setConfigSuccess(false);
    },
    [mod.configValues]
  );

  useEffect(() => {
    resetForm(envConfig);
  }, [envConfig, resetForm]);

  const schema = mod.configSchema ?? [];
  const secretStatus = envConfig?.secretStatus ?? mod.secretStatus ?? {};

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

  const handleSaveConfig = async () => {
    setConfigError(null);
    setConfigSuccess(false);
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
      )
        return;

      await updateEnv({
        name: mod.moduleName,
        environment: selectedEnvironment,
        config:
          Object.keys(changedConfig).length > 0 ? changedConfig : undefined,
        secrets: Object.keys(newSecrets).length > 0 ? newSecrets : undefined
      }).unwrap();

      setConfigSuccess(true);
      setSecretValues({});
      setTimeout(() => setConfigSuccess(false), 3000);
    } catch (err: unknown) {
      const fallback = t('adminModules.aiModelsSection.updateFailed');
      const message =
        err && typeof err === 'object' && 'data' in err
          ? String(
              (err as { data: { detail?: string } }).data?.detail || fallback
            )
          : fallback;
      setConfigError(message);
    }
  };

  const handleDiscardConfig = () => resetForm(envConfig);

  const blocker = useBlocker(isDirty);

  // --- Models for active provider ---
  const { data: modelsData, isLoading: modelsLoading } = useListAIModelsQuery({
    provider: activeTab
  });
  const models = modelsData?.models ?? [];

  const [deleteModel] = useDeleteAIModelMutation();
  const [setDefaultModel] = useSetDefaultAIModelMutation();
  const [testModel] = useTestAIModelMutation();
  const [updateModel] = useUpdateAIModelMutation();

  const [testResults, setTestResults] = useState<
    Record<string, { status: string; message: string }>
  >({});
  const [showFormModal, setShowFormModal] = useState(false);
  const [editingModel, setEditingModel] = useState<AIModelConfig | null>(null);

  const handleTest = useCallback(
    async (uuid: string) => {
      setTestResults(prev => ({
        ...prev,
        [uuid]: {
          status: 'testing',
          message: t('adminModules.aiModelsSection.testing')
        }
      }));
      try {
        const result = await testModel(uuid).unwrap();
        setTestResults(prev => ({ ...prev, [uuid]: result }));
      } catch {
        setTestResults(prev => ({
          ...prev,
          [uuid]: {
            status: 'error',
            message: t('adminModules.aiModelsSection.testFailed')
          }
        }));
      }
    },
    [testModel, t]
  );

  const handleToggleActive = useCallback(
    async (model: AIModelConfig) => {
      try {
        await updateModel({
          uuid: model.uuid,
          body: { isActive: !model.isActive }
        }).unwrap();
      } catch {
        // handled by RTK Query
      }
    },
    [updateModel]
  );

  const handleSetDefault = useCallback(
    async (uuid: string) => {
      try {
        await setDefaultModel(uuid).unwrap();
      } catch {
        // handled by RTK Query
      }
    },
    [setDefaultModel]
  );

  const handleDelete = useCallback(
    async (uuid: string) => {
      if (!confirm(t('adminModules.aiModelsSection.deleteConfirm'))) return;
      try {
        await deleteModel(uuid).unwrap();
      } catch {
        // handled by RTK Query
      }
    },
    [deleteModel, t]
  );

  const openCreate = () => {
    setEditingModel(null);
    setShowFormModal(true);
  };

  const openEdit = (model: AIModelConfig) => {
    setEditingModel(model);
    setShowFormModal(true);
  };

  const closeFormModal = () => {
    setShowFormModal(false);
    setEditingModel(null);
  };

  const getDetailsText = (model: AIModelConfig): string => {
    if (model.modelType === 'embedding' && model.dimensions) {
      return t('adminModules.aiModelsSection.detailsDimensions', {
        dimensions: model.dimensions
      });
    }
    if (model.modelType === 'llm') {
      const parts: string[] = [];
      if (model.temperature !== undefined)
        parts.push(
          t('adminModules.aiModelsSection.detailsTemp', {
            value: model.temperature
          })
        );
      if (model.maxTokens !== undefined)
        parts.push(
          t('adminModules.aiModelsSection.detailsMax', {
            value: model.maxTokens
          })
        );
      return parts.join(', ');
    }
    return '';
  };

  const getStatusBadge = (model: AIModelConfig) => {
    const testResult = testResults[model.uuid];
    if (testResult) {
      if (testResult.status === 'testing') {
        return (
          <Badge bg="secondary">
            <Spinner size="sm" />
          </Badge>
        );
      }
      return (
        <Badge bg={testResult.status === 'ok' ? 'success' : 'danger'}>
          {testResult.message}
        </Badge>
      );
    }
    if (model.lastTestStatus === 'ok') {
      return (
        <Badge bg="success" className="bg-opacity-25 text-body">
          {t('adminModules.aiModelsSection.testedOk')}
        </Badge>
      );
    }
    if (model.lastTestStatus === 'error') {
      return (
        <Badge bg="danger" className="bg-opacity-25 text-body">
          {t('adminModules.aiModelsSection.testedFailed')}
        </Badge>
      );
    }
    return null;
  };

  return (
    <>
      {/* Unsaved changes blocker */}
      {blocker.state === 'blocked' && (
        <Modal show centered onHide={() => blocker.reset()}>
          <Modal.Header closeButton>
            <Modal.Title className="fs-8">
              {t('adminModules.aiModelsSection.unsavedTitle')}
            </Modal.Title>
          </Modal.Header>
          <Modal.Body className="fs-10">
            {t('adminModules.aiModelsSection.unsavedBody')}
          </Modal.Body>
          <Modal.Footer>
            <Button
              variant="secondary"
              size="sm"
              onClick={() => blocker.reset()}
            >
              {t('adminModules.aiModelsSection.unsavedStay')}
            </Button>
            <Button
              variant="danger"
              size="sm"
              onClick={() => blocker.proceed()}
            >
              {t('adminModules.aiModelsSection.unsavedLeave')}
            </Button>
          </Modal.Footer>
        </Modal>
      )}

      <Card className="mb-3">
        <OrkestraCardHeader
          title={t('adminModules.aiModelsSection.title')}
          light={false}
          endEl={
            envLoading ? <Spinner animation="border" size="sm" /> : undefined
          }
        />
        <Card.Body>
          {/* Provider tabs */}
          <Nav
            variant="tabs"
            activeKey={activeTab}
            onSelect={handleTabSelect}
            className="mb-3"
          >
            {PROVIDERS.map(provider => (
              <Nav.Item key={provider}>
                <Nav.Link eventKey={provider}>
                  {PROVIDER_INFO[provider].label}
                </Nav.Link>
              </Nav.Item>
            ))}
          </Nav>

          {/* Config alerts */}
          {configError && (
            <Alert
              variant="danger"
              className="fs-10"
              dismissible
              onClose={() => setConfigError(null)}
            >
              {configError}
            </Alert>
          )}
          {configSuccess && (
            <Alert variant="success" className="fs-10">
              {t('adminModules.aiModelsSection.savedToast')}
            </Alert>
          )}

          {/* Provider config fields */}
          <div className="mb-3">
            <h6 className="fs-10 text-uppercase text-600 mb-2">
              {t('adminModules.aiModelsSection.providerSettingsHeading')}
            </h6>
            <ModuleConfigFields
              schema={schema}
              includeKeys={PROVIDER_CONFIG_KEYS[activeTab]}
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
            <div className="d-flex justify-content-end gap-2">
              {isDirty && (
                <Button
                  variant="outline-secondary"
                  size="sm"
                  onClick={handleDiscardConfig}
                >
                  {t('adminModules.aiModelsSection.discard')}
                </Button>
              )}
              <Button
                variant="primary"
                size="sm"
                onClick={handleSaveConfig}
                disabled={saving || !isDirty}
              >
                {saving ? (
                  <Spinner animation="border" size="sm" />
                ) : (
                  t('adminModules.aiModelsSection.save')
                )}
              </Button>
            </div>
          </div>

          {/* Models table */}
          <div className="border-top pt-3">
            <div className="d-flex align-items-center justify-content-between mb-2">
              <h6 className="fs-10 text-uppercase text-600 mb-0">
                {t('adminModules.aiModelsSection.modelsHeading')}
              </h6>
              <Button size="sm" variant="primary" onClick={openCreate}>
                {t('adminModules.aiModelsSection.addModel')}
              </Button>
            </div>

            {modelsLoading ? (
              <div className="text-center py-3">
                <Spinner size="sm" />
              </div>
            ) : models.length === 0 ? (
              <p className="text-muted fs-10 mb-0">
                {t('adminModules.aiModelsSection.emptyModels', {
                  provider: PROVIDER_INFO[activeTab].label
                })}
              </p>
            ) : (
              <Table size="sm" hover responsive className="mb-0 fs-10">
                <thead>
                  <tr>
                    <th>{t('adminModules.aiModelsSection.colName')}</th>
                    <th>{t('adminModules.aiModelsSection.colModel')}</th>
                    <th>{t('adminModules.aiModelsSection.colType')}</th>
                    <th>{t('adminModules.aiModelsSection.colDetails')}</th>
                    <th>{t('adminModules.aiModelsSection.colStatus')}</th>
                    <th></th>
                  </tr>
                </thead>
                <tbody>
                  {models.map(m => (
                    <tr key={m.uuid}>
                      <td className="fw-semibold">
                        {m.name}
                        {m.isDefault && (
                          <Badge bg="success" className="ms-1">
                            {t('adminModules.aiModelsSection.defaultBadge')}
                          </Badge>
                        )}
                      </td>
                      <td>
                        <code className="small">{m.modelName}</code>
                      </td>
                      <td>
                        <Badge bg="secondary">{m.modelType}</Badge>
                      </td>
                      <td className="text-muted">{getDetailsText(m)}</td>
                      <td>
                        {getStatusBadge(m) || (
                          <Form.Check
                            type="switch"
                            id={`toggle-active-${m.uuid}`}
                            checked={m.isActive}
                            onChange={() => handleToggleActive(m)}
                            label={
                              m.isActive
                                ? t('adminModules.aiModelsSection.active')
                                : t('adminModules.aiModelsSection.inactive')
                            }
                            className="mb-0"
                          />
                        )}
                      </td>
                      <td>
                        <div className="d-flex gap-1 flex-nowrap">
                          <Button
                            variant="outline-primary"
                            size="sm"
                            onClick={() => handleTest(m.uuid)}
                          >
                            {t('adminModules.aiModelsSection.test')}
                          </Button>
                          <Button
                            variant="outline-secondary"
                            size="sm"
                            onClick={() => openEdit(m)}
                          >
                            {t('adminModules.aiModelsSection.edit')}
                          </Button>
                          {!m.isDefault && (
                            <Button
                              variant="outline-success"
                              size="sm"
                              onClick={() => handleSetDefault(m.uuid)}
                            >
                              {t('adminModules.aiModelsSection.makeDefault')}
                            </Button>
                          )}
                          <Button
                            variant="outline-danger"
                            size="sm"
                            onClick={() => handleDelete(m.uuid)}
                          >
                            {t('adminModules.aiModelsSection.delete')}
                          </Button>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </Table>
            )}
          </div>
        </Card.Body>
      </Card>

      {/* Model create/edit modal */}
      <ModelFormModal
        show={showFormModal}
        onHide={closeFormModal}
        editingModel={editingModel}
        defaultProvider={activeTab}
      />
    </>
  );
};

export default AIModelsConfigSection;
