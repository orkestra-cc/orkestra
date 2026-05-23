import { useState, useCallback } from 'react';
import {
  Card,
  Button,
  Form,
  Table,
  Badge,
  Spinner,
  Alert
} from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import {
  useListAIModelsQuery,
  useDeleteAIModelMutation,
  useSetDefaultAIModelMutation,
  useTestAIModelMutation,
  useUpdateAIModelMutation
} from '../../../store/api/aiModelsApi';
import type { AIModelConfig, Provider } from '../../../types/aiModels';
import { PROVIDER_INFO } from '../../../types/aiModels';
import ModelFormModal from './ModelFormModal';
import QuickPromptModal from './QuickPromptModal';

const ModelsTable: React.FC = () => {
  const { t } = useTranslation();
  const [filterType, setFilterType] = useState<string>('');
  const [filterProvider, setFilterProvider] = useState<string>('');
  const [filterCategory, setFilterCategory] = useState<string>('');
  const [showFormModal, setShowFormModal] = useState(false);
  const [editingModel, setEditingModel] = useState<AIModelConfig | null>(null);
  const [promptModel, setPromptModel] = useState<AIModelConfig | null>(null);
  const [testResults, setTestResults] = useState<
    Record<string, { status: string; message: string }>
  >({});

  const queryParams: { type?: string; provider?: string; category?: string } =
    {};
  if (filterType) queryParams.type = filterType;
  if (filterProvider) queryParams.provider = filterProvider;
  if (filterCategory) queryParams.category = filterCategory;

  const { data, isLoading } = useListAIModelsQuery(
    Object.keys(queryParams).length > 0 ? queryParams : undefined
  );
  const [deleteModel] = useDeleteAIModelMutation();
  const [setDefaultModel] = useSetDefaultAIModelMutation();
  const [testModel] = useTestAIModelMutation();
  const [updateModel] = useUpdateAIModelMutation();

  const models = data?.models ?? [];

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

  const handleDelete = useCallback(
    async (uuid: string) => {
      if (!confirm(t('aiModels.table.deleteConfirm'))) return;
      try {
        await deleteModel(uuid).unwrap();
      } catch {
        // Handled by RTK Query
      }
    },
    [deleteModel, t]
  );

  const handleTest = useCallback(
    async (uuid: string) => {
      setTestResults(prev => ({
        ...prev,
        [uuid]: { status: 'testing', message: t('aiModels.table.testing') }
      }));
      try {
        const result = await testModel(uuid).unwrap();
        setTestResults(prev => ({ ...prev, [uuid]: result }));
      } catch {
        setTestResults(prev => ({
          ...prev,
          [uuid]: { status: 'error', message: t('aiModels.table.testFailed') }
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
        // Handled by RTK Query
      }
    },
    [updateModel]
  );

  const handleSetDefault = useCallback(
    async (uuid: string) => {
      try {
        await setDefaultModel(uuid).unwrap();
      } catch {
        // Handled by RTK Query
      }
    },
    [setDefaultModel]
  );

  const getProviderBadgeColor = (provider: Provider): string => {
    return PROVIDER_INFO[provider]?.color ?? 'secondary';
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

    // Show last test status if available
    if (model.lastTestStatus === 'ok') {
      return (
        <Badge bg="success" className="bg-opacity-25 text-body">
          {t('aiModels.table.testedOk')}
        </Badge>
      );
    }
    if (model.lastTestStatus === 'error') {
      return (
        <Badge bg="danger" className="bg-opacity-25 text-body">
          {t('aiModels.table.testedFailed')}
        </Badge>
      );
    }

    return (
      <Badge
        bg={model.isActive ? 'success' : 'secondary'}
        className="bg-opacity-25 text-body"
      >
        {model.isActive
          ? t('aiModels.table.statusActive')
          : t('aiModels.table.statusInactive')}
      </Badge>
    );
  };

  const getDetailsText = (model: AIModelConfig): string => {
    if (model.modelType === 'embedding' && model.dimensions) {
      return t('aiModels.table.detailsDimensions', {
        dimensions: model.dimensions
      });
    }
    if (model.modelType === 'llm') {
      const parts: string[] = [];
      if (model.temperature !== undefined)
        parts.push(
          t('aiModels.table.detailsTemp', { value: model.temperature })
        );
      if (model.maxTokens !== undefined)
        parts.push(t('aiModels.table.detailsMax', { value: model.maxTokens }));
      return parts.join(', ');
    }
    return '';
  };

  return (
    <>
      <Card>
        <Card.Header className="border-bottom border-200">
          <div className="d-flex align-items-center justify-content-between flex-wrap gap-2">
            <div className="d-flex gap-2 flex-wrap">
              <Form.Select
                size="sm"
                value={filterType}
                onChange={e => setFilterType(e.target.value)}
                style={{ width: 140 }}
              >
                <option value="">{t('aiModels.table.filterTypeAll')}</option>
                <option value="embedding">
                  {t('aiModels.table.filterTypeEmbedding')}
                </option>
                <option value="llm">{t('aiModels.table.filterTypeLlm')}</option>
              </Form.Select>
              <Form.Select
                size="sm"
                value={filterProvider}
                onChange={e => setFilterProvider(e.target.value)}
                style={{ width: 180 }}
              >
                <option value="">
                  {t('aiModels.table.filterProviderAll')}
                </option>
                <option value="ollama">
                  {t('aiModels.table.filterProviderOllama')}
                </option>
                <option value="openai">
                  {t('aiModels.table.filterProviderOpenai')}
                </option>
                <option value="anthropic">
                  {t('aiModels.table.filterProviderAnthropic')}
                </option>
                <option value="gemini">
                  {t('aiModels.table.filterProviderGemini')}
                </option>
              </Form.Select>
              <Form.Select
                size="sm"
                value={filterCategory}
                onChange={e => setFilterCategory(e.target.value)}
                style={{ width: 140 }}
              >
                <option value="">
                  {t('aiModels.table.filterCategoryAll')}
                </option>
                <option value="local">
                  {t('aiModels.table.filterCategoryLocal')}
                </option>
                <option value="cloud">
                  {t('aiModels.table.filterCategoryCloud')}
                </option>
              </Form.Select>
            </div>
            <Button size="sm" variant="primary" onClick={openCreate}>
              {t('aiModels.table.addModel')}
            </Button>
          </div>
        </Card.Header>
        <Card.Body className="p-0">
          {isLoading ? (
            <div className="text-center p-3">
              <Spinner size="sm" />
            </div>
          ) : models.length === 0 ? (
            <Alert variant="info" className="m-3 mb-0">
              {t('aiModels.table.empty')}
            </Alert>
          ) : (
            <Table size="sm" hover responsive className="mb-0">
              <thead>
                <tr>
                  <th>{t('aiModels.table.colName')}</th>
                  <th>{t('aiModels.table.colProvider')}</th>
                  <th>{t('aiModels.table.colType')}</th>
                  <th>{t('aiModels.table.colModel')}</th>
                  <th>{t('aiModels.table.colBaseUrl')}</th>
                  <th>{t('aiModels.table.colDetails')}</th>
                  <th>{t('aiModels.table.colStatus')}</th>
                  <th></th>
                </tr>
              </thead>
              <tbody>
                {models.map(m => (
                  <tr key={m.uuid}>
                    <td className="fw-semibold">
                      {m.name}
                      {m.isDefault && (
                        <Badge bg="success" className="ms-2">
                          {t('aiModels.table.defaultBadge')}
                        </Badge>
                      )}
                    </td>
                    <td>
                      <Badge bg={getProviderBadgeColor(m.provider)}>
                        {PROVIDER_INFO[m.provider]?.label ?? m.provider}
                      </Badge>
                    </td>
                    <td>
                      <Badge bg="secondary">{m.modelType}</Badge>
                    </td>
                    <td>
                      <code className="small">{m.modelName}</code>
                    </td>
                    <td
                      className="small text-muted"
                      style={{
                        maxWidth: 200,
                        overflow: 'hidden',
                        textOverflow: 'ellipsis',
                        whiteSpace: 'nowrap'
                      }}
                    >
                      {m.baseUrl || t('aiModels.table.baseUrlDash')}
                    </td>
                    <td className="small text-muted">{getDetailsText(m)}</td>
                    <td>
                      {testResults[m.uuid] ? (
                        getStatusBadge(m)
                      ) : (
                        <Form.Check
                          type="switch"
                          id={`toggle-active-${m.uuid}`}
                          checked={m.isActive}
                          onChange={() => handleToggleActive(m)}
                          label={
                            m.isActive
                              ? t('aiModels.table.switchActive')
                              : t('aiModels.table.switchInactive')
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
                          {t('aiModels.table.test')}
                        </Button>
                        {m.modelType === 'llm' && (
                          <Button
                            variant="outline-info"
                            size="sm"
                            onClick={() => setPromptModel(m)}
                          >
                            {t('aiModels.table.prompt')}
                          </Button>
                        )}
                        <Button
                          variant="outline-secondary"
                          size="sm"
                          onClick={() => openEdit(m)}
                        >
                          {t('aiModels.table.edit')}
                        </Button>
                        {!m.isDefault && (
                          <Button
                            variant="outline-success"
                            size="sm"
                            onClick={() => handleSetDefault(m.uuid)}
                          >
                            {t('aiModels.table.makeDefault')}
                          </Button>
                        )}
                        <Button
                          variant="outline-danger"
                          size="sm"
                          onClick={() => handleDelete(m.uuid)}
                        >
                          {t('aiModels.table.delete')}
                        </Button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </Table>
          )}
        </Card.Body>
      </Card>

      <ModelFormModal
        show={showFormModal}
        onHide={closeFormModal}
        editingModel={editingModel}
      />

      <QuickPromptModal
        model={promptModel}
        onHide={() => setPromptModel(null)}
      />
    </>
  );
};

export default ModelsTable;
