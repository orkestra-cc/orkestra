import { useState, useCallback } from 'react';
import { Modal, Row, Col, Form, Button, Spinner } from 'react-bootstrap';
import {
  useCreateAIModelMutation,
  useUpdateAIModelMutation,
  useFetchAIProviderModelsMutation,
} from '../../../store/api/aiModelsApi';
import type {
  AIModelConfig,
  CreateAIModelRequest,
  UpdateAIModelRequest,
  Provider,
  AvailableModel,
} from '../../../types/aiModels';

interface ModelFormModalProps {
  show: boolean;
  onHide: () => void;
  editingModel: AIModelConfig | null;
  defaultProvider?: Provider;
}

const emptyForm: CreateAIModelRequest = {
  name: '',
  provider: 'openai',
  modelType: 'embedding',
  modelName: '',
  baseUrl: '',
  apiKey: '',
  dimensions: 768,
  temperature: 0.1,
  maxTokens: 2048,
};

const PROVIDER_HELP: Record<Provider, string> = {
  ollama: 'Leave empty to use global Ollama config',
  openai: 'For llama.cpp, vLLM, LocalAI, etc. Leave empty for OpenAI cloud.',
  anthropic: 'Leave empty to use Anthropic cloud API.',
  gemini: 'Leave empty to use Google Gemini cloud API.',
};

const PROVIDER_PLACEHOLDER: Record<Provider, string> = {
  ollama: 'http://host:11434',
  openai: 'http://host:8080/v1',
  anthropic: 'https://api.anthropic.com',
  gemini: 'https://generativelanguage.googleapis.com',
};

const showBaseUrlField = (provider: Provider): boolean => {
  return provider === 'ollama' || provider === 'openai';
};

const isCloudProvider = (provider: Provider): boolean => {
  return provider === 'anthropic' || provider === 'gemini';
};

const ModelFormModal: React.FC<ModelFormModalProps> = ({ show, onHide, editingModel, defaultProvider }) => {
  const [form, setForm] = useState<CreateAIModelRequest>({ ...emptyForm });
  const [isActive, setIsActive] = useState(true);
  const [availableModels, setAvailableModels] = useState<AvailableModel[]>([]);

  const [createModel, { isLoading: creating }] = useCreateAIModelMutation();
  const [updateModel, { isLoading: updating }] = useUpdateAIModelMutation();
  const [fetchProviderModels, { isLoading: fetching }] = useFetchAIProviderModelsMutation();

  const isEditing = !!editingModel;
  const saving = creating || updating;

  // Reset form when modal opens
  const handleEnter = () => {
    if (editingModel) {
      setForm({
        name: editingModel.name,
        provider: editingModel.provider,
        modelType: editingModel.modelType,
        modelName: editingModel.modelName,
        baseUrl: editingModel.baseUrl || '',
        apiKey: '',
        dimensions: editingModel.dimensions || 768,
        temperature: editingModel.temperature || 0.1,
        maxTokens: editingModel.maxTokens || 2048,
      });
      setIsActive(editingModel.isActive);
    } else {
      setForm({ ...emptyForm, provider: defaultProvider || emptyForm.provider });
      setIsActive(true);
    }
    setAvailableModels([]);
  };

  const updateForm = (field: string, value: unknown) => {
    setForm(prev => ({ ...prev, [field]: value }));
  };

  const fetchModelsForProvider = useCallback(async (provider: string, baseUrl: string, apiKey: string, modelType: string) => {
    try {
      const result = await fetchProviderModels({
        provider,
        baseUrl,
        apiKey: apiKey || undefined,
        modelType,
      }).unwrap();
      setAvailableModels(result.models ?? []);
      if (result.models?.length) {
        setForm(prev => prev.modelName ? prev : { ...prev, modelName: result.models[0].id });
      }
    } catch {
      setAvailableModels([]);
    }
  }, [fetchProviderModels]);

  const handleProviderChange = (provider: Provider) => {
    setForm(prev => {
      const updates: Partial<CreateAIModelRequest> = { provider, baseUrl: '', apiKey: '', modelName: '' };
      if (provider === 'anthropic' && prev.modelType === 'embedding') {
        updates.modelType = 'llm';
      }
      return { ...prev, ...updates };
    });
    setAvailableModels([]);
  };

  const handleFetchModels = useCallback(async () => {
    // Cloud providers need an API key, local providers need a base URL
    if (isCloudProvider(form.provider) && !form.apiKey) return;
    if (!isCloudProvider(form.provider) && !form.baseUrl) return;
    await fetchModelsForProvider(form.provider, form.baseUrl || '', form.apiKey || '', form.modelType);
  }, [fetchModelsForProvider, form.provider, form.baseUrl, form.apiKey, form.modelType]);

  const handleSave = useCallback(async () => {
    try {
      if (editingModel) {
        const body: UpdateAIModelRequest = {};
        if (form.name !== editingModel.name) body.name = form.name;
        if (form.baseUrl !== (editingModel.baseUrl || '')) body.baseUrl = form.baseUrl;
        if (form.apiKey) body.apiKey = form.apiKey;
        if (form.dimensions !== editingModel.dimensions) body.dimensions = form.dimensions;
        if (form.temperature !== editingModel.temperature) body.temperature = form.temperature;
        if (form.maxTokens !== editingModel.maxTokens) body.maxTokens = form.maxTokens;
        if (isActive !== editingModel.isActive) body.isActive = isActive;
        await updateModel({ uuid: editingModel.uuid, body }).unwrap();
      } else {
        await createModel(form).unwrap();
      }
      onHide();
    } catch {
      // Handled by RTK Query
    }
  }, [createModel, updateModel, editingModel, form, isActive, onHide]);

  return (
    <Modal show={show} onHide={onHide} onEnter={handleEnter} size="lg">
      <Modal.Header closeButton>
        <Modal.Title>{isEditing ? 'Edit Model' : 'Add AI Model'}</Modal.Title>
      </Modal.Header>
      <Modal.Body>
        <Row className="g-3">
          <Col md={6}>
            <Form.Group>
              <Form.Label className="small">Display Name</Form.Label>
              <Form.Control
                size="sm"
                value={form.name}
                onChange={e => updateForm('name', e.target.value)}
                placeholder="My Embedding Model"
              />
            </Form.Group>
          </Col>
          <Col md={3}>
            <Form.Group>
              <Form.Label className="small">Provider</Form.Label>
              <Form.Select
                size="sm"
                value={form.provider}
                onChange={e => handleProviderChange(e.target.value as Provider)}
                disabled={isEditing}
              >
                <option value="openai">OpenAI / Compatible</option>
                <option value="ollama">Ollama</option>
                <option value="anthropic">Anthropic (Claude)</option>
                <option value="gemini">Google (Gemini)</option>
              </Form.Select>
            </Form.Group>
          </Col>
          <Col md={3}>
            <Form.Group>
              <Form.Label className="small">Type</Form.Label>
              <Form.Select
                size="sm"
                value={form.modelType}
                onChange={e => {
                  updateForm('modelType', e.target.value);
                  setAvailableModels([]);
                  updateForm('modelName', '');
                }}
                disabled={isEditing}
              >
                <option value="embedding" disabled={form.provider === 'anthropic'}>
                  Embedding
                </option>
                <option value="llm">LLM</option>
              </Form.Select>
            </Form.Group>
          </Col>

          {showBaseUrlField(form.provider) && (
            <Col md={6}>
              <Form.Group>
                <Form.Label className="small">Base URL</Form.Label>
                <div className="d-flex gap-2">
                  <Form.Control
                    size="sm"
                    value={form.baseUrl}
                    onChange={e => updateForm('baseUrl', e.target.value)}
                    placeholder={PROVIDER_PLACEHOLDER[form.provider]}
                  />
                  <Button
                    size="sm"
                    variant="outline-secondary"
                    onClick={handleFetchModels}
                    disabled={fetching || !form.baseUrl}
                    style={{ whiteSpace: 'nowrap' }}
                  >
                    {fetching ? <Spinner size="sm" /> : 'Fetch Models'}
                  </Button>
                </div>
                <Form.Text className="text-muted">
                  {PROVIDER_HELP[form.provider]}
                </Form.Text>
              </Form.Group>
            </Col>
          )}

          <Col md={6}>
            <Form.Group>
              <Form.Label className="small">Model Name</Form.Label>
              {availableModels.length > 0 ? (
                <div className="d-flex gap-2">
                  <Form.Select
                    size="sm"
                    value={form.modelName}
                    onChange={e => updateForm('modelName', e.target.value)}
                    disabled={isEditing}
                  >
                    <option value="">Select a model...</option>
                    {availableModels
                      .filter(m => {
                        if (!m.capabilities) return true;
                        const caps = m.capabilities.split(',');
                        if (form.modelType === 'embedding') return caps.includes('embedContent');
                        return caps.includes('generateContent');
                      })
                      .map(m => (
                      <option key={m.id} value={m.id}>
                        {m.id}{m.ownedBy ? ` (${m.ownedBy})` : ''}
                      </option>
                    ))}
                  </Form.Select>
                  {!isEditing && isCloudProvider(form.provider) && (
                    <Button
                      size="sm"
                      variant="outline-secondary"
                      onClick={handleFetchModels}
                      disabled={fetching || !form.apiKey}
                      style={{ whiteSpace: 'nowrap' }}
                    >
                      {fetching ? <Spinner size="sm" /> : 'Refresh'}
                    </Button>
                  )}
                </div>
              ) : (
                <div className="d-flex gap-2">
                  <Form.Control
                    size="sm"
                    value={form.modelName}
                    onChange={e => updateForm('modelName', e.target.value)}
                    disabled={isEditing}
                    placeholder={
                      showBaseUrlField(form.provider)
                        ? 'Enter model name or click Fetch Models'
                        : fetching ? 'Loading models...' : 'Enter API key first, then Fetch Models'
                    }
                  />
                  {!isEditing && isCloudProvider(form.provider) && (
                    <Button
                      size="sm"
                      variant="outline-secondary"
                      onClick={handleFetchModels}
                      disabled={fetching || !form.apiKey}
                      style={{ whiteSpace: 'nowrap' }}
                    >
                      {fetching ? <Spinner size="sm" /> : 'Fetch Models'}
                    </Button>
                  )}
                </div>
              )}
              {showBaseUrlField(form.provider) && (
                <Form.Text className="text-muted">
                  Enter a base URL and click "Fetch Models" to see available models
                </Form.Text>
              )}
            </Form.Group>
          </Col>

          <Col md={6}>
            <Form.Group>
              <Form.Label className="small">API Key</Form.Label>
              <Form.Control
                size="sm"
                type="password"
                value={form.apiKey}
                onChange={e => updateForm('apiKey', e.target.value)}
                placeholder={isEditing ? '(unchanged)' : 'sk-... or leave empty for local'}
              />
              <Form.Text className="text-muted">
                {form.provider === 'ollama'
                  ? 'Not needed for local Ollama instances.'
                  : form.provider === 'anthropic'
                    ? 'Required for Anthropic API access.'
                    : form.provider === 'gemini'
                      ? 'Required for Google Gemini API access.'
                      : 'Required for OpenAI cloud. Not needed for local servers (llama.cpp, Ollama).'}
              </Form.Text>
            </Form.Group>
          </Col>

          {form.modelType === 'embedding' && (
            <Col md={6}>
              <Form.Group>
                <Form.Label className="small">Dimensions</Form.Label>
                <Form.Control
                  size="sm"
                  type="number"
                  value={form.dimensions}
                  onChange={e => updateForm('dimensions', parseInt(e.target.value) || 0)}
                />
                <Form.Text className="text-muted">Vector size output by the model</Form.Text>
              </Form.Group>
            </Col>
          )}

          {form.modelType === 'llm' && (
            <>
              <Col md={3}>
                <Form.Group>
                  <Form.Label className="small">Temperature</Form.Label>
                  <Form.Control
                    size="sm"
                    type="number"
                    step="0.1"
                    min="0"
                    max="2"
                    value={form.temperature}
                    onChange={e => updateForm('temperature', parseFloat(e.target.value) || 0)}
                  />
                </Form.Group>
              </Col>
              <Col md={3}>
                <Form.Group>
                  <Form.Label className="small">Max Tokens</Form.Label>
                  <Form.Control
                    size="sm"
                    type="number"
                    value={form.maxTokens}
                    onChange={e => updateForm('maxTokens', parseInt(e.target.value) || 0)}
                  />
                </Form.Group>
              </Col>
            </>
          )}
          {isEditing && (
            <Col md={6}>
              <Form.Group>
                <Form.Label className="small">Status</Form.Label>
                <Form.Check
                  type="switch"
                  id="model-is-active"
                  checked={isActive}
                  onChange={e => setIsActive(e.target.checked)}
                  label={isActive ? 'Active' : 'Inactive'}
                />
              </Form.Group>
            </Col>
          )}
        </Row>
      </Modal.Body>
      <Modal.Footer>
        <Button variant="secondary" size="sm" onClick={onHide}>
          Cancel
        </Button>
        <Button
          variant="primary"
          size="sm"
          onClick={handleSave}
          disabled={saving || !form.name || (!isEditing && !form.modelName)}
        >
          {saving ? <Spinner size="sm" /> : isEditing ? 'Save Changes' : 'Create'}
        </Button>
      </Modal.Footer>
    </Modal>
  );
};

export default ModelFormModal;
