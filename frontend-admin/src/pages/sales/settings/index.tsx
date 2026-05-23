import { useState, useEffect } from 'react';
import {
  Row,
  Col,
  Card,
  Form,
  Button,
  Spinner,
  Alert,
  Nav,
  Table,
  Badge
} from 'react-bootstrap';
import { Link, useSearchParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import {
  faCog,
  faSave,
  faExternalLinkAlt,
  faRobot,
  faMagic,
  faScroll,
  faUndo,
  faArrowLeft,
  faPen
} from '@fortawesome/free-solid-svg-icons';
import Background from 'components/common/Background';
import greetingsBg from 'assets/img/illustrations/ticket-greetings-bg.png';
import {
  useGetSalesSettingsQuery,
  useUpdateSalesSettingsMutation,
  useListSalesPromptsQuery,
  useGetSalesPromptQuery,
  useUpdateSalesPromptMutation,
  useResetSalesPromptMutation
} from '../../../store/api/salesApi';
import type { SalesPromptConfig } from '../../../store/api/salesApi';
import { useListAIModelsQuery } from '../../../store/api/aiModelsApi';

// ─── Prompt Table ───

function PromptTable({
  prompts,
  onEdit
}: {
  prompts: SalesPromptConfig[];
  onEdit: (id: string) => void;
}) {
  const { t } = useTranslation();

  if (prompts.length === 0) {
    return (
      <div className="text-center text-muted py-4">
        {t('sales.settings.promptList.empty')}
      </div>
    );
  }

  return (
    <Table hover responsive className="mb-0">
      <thead>
        <tr>
          <th>{t('sales.settings.promptList.colName')}</th>
          <th>{t('sales.settings.promptList.colDescription')}</th>
          <th>{t('sales.settings.promptList.colStatus')}</th>
          <th>{t('sales.settings.promptList.colSize')}</th>
          <th>{t('sales.settings.promptList.colUpdated')}</th>
          <th></th>
        </tr>
      </thead>
      <tbody>
        {prompts.map(p => (
          <tr
            key={p.uuid}
            style={{ cursor: 'pointer' }}
            onClick={() => onEdit(p.uuid)}
          >
            <td>
              <strong>{p.displayName}</strong>
              <br />
              <small className="text-muted font-monospace">{p.name}</small>
            </td>
            <td>
              <small className="text-muted">{p.description}</small>
            </td>
            <td>
              {p.isCustom ? (
                <Badge bg="warning">
                  {t('sales.settings.promptList.badgeCustomized')}
                </Badge>
              ) : (
                <Badge bg="secondary">
                  {t('sales.settings.promptList.badgeDefault')}
                </Badge>
              )}
            </td>
            <td>
              <small>
                {t('sales.settings.promptList.sizeChars', {
                  count: p.content?.length || 0
                })}
              </small>
            </td>
            <td>
              <small>{new Date(p.updatedAt).toLocaleDateString()}</small>
            </td>
            <td>
              <Button
                variant="outline-primary"
                size="sm"
                onClick={e => {
                  e.stopPropagation();
                  onEdit(p.uuid);
                }}
              >
                <FontAwesomeIcon icon={faPen} />
              </Button>
            </td>
          </tr>
        ))}
      </tbody>
    </Table>
  );
}

// ─── Prompt Editor ───

const CATEGORY_ICONS: Record<string, any> = {
  agents: faRobot,
  skills: faMagic
};
const CATEGORY_COLORS: Record<string, string> = {
  agents: 'primary',
  skills: 'info'
};

function PromptEditor({
  promptId,
  onBack
}: {
  promptId: string;
  onBack: () => void;
}) {
  const { t } = useTranslation();
  const { data: prompt, isLoading } = useGetSalesPromptQuery(promptId);
  const [updatePrompt, { isLoading: saving }] = useUpdateSalesPromptMutation();
  const [resetPrompt, { isLoading: resetting }] = useResetSalesPromptMutation();

  const [content, setContent] = useState('');
  const [displayName, setDisplayName] = useState('');
  const [description, setDescription] = useState('');
  const [saved, setSaved] = useState(false);
  const [dirty, setDirty] = useState(false);

  useEffect(() => {
    if (prompt) {
      setContent(prompt.content);
      setDisplayName(prompt.displayName);
      setDescription(prompt.description);
      setDirty(false);
    }
  }, [prompt]);

  if (isLoading || !prompt) {
    return (
      <Card>
        <Card.Body className="text-center py-5">
          <Spinner />
        </Card.Body>
      </Card>
    );
  }

  const handleSave = async () => {
    setSaved(false);
    await updatePrompt({
      uuid: prompt.uuid,
      content,
      displayName,
      description
    }).unwrap();
    setSaved(true);
    setDirty(false);
    setTimeout(() => setSaved(false), 3000);
  };

  const handleReset = async () => {
    if (!window.confirm(t('sales.settings.promptEditor.resetConfirm'))) return;
    const result = await resetPrompt(prompt.uuid).unwrap();
    setContent(result.content);
    setDirty(false);
  };

  const templateVars = (content.match(/\{\{\.(\w+)\}\}/g) || [])
    .map(v => v.replace(/\{\{\.|}\}/g, ''))
    .filter((v, i, a) => a.indexOf(v) === i);

  return (
    <>
      <Row className="g-3 mb-3">
        <Col xxl={12}>
          <Card>
            <Card.Header className="d-flex justify-content-between align-items-center">
              <div className="d-flex align-items-center gap-2">
                <Button variant="outline-secondary" size="sm" onClick={onBack}>
                  <FontAwesomeIcon icon={faArrowLeft} />
                </Button>
                <div>
                  <h5 className="mb-0 d-flex align-items-center gap-2">
                    <FontAwesomeIcon
                      icon={CATEGORY_ICONS[prompt.category] || faScroll}
                      className="text-primary"
                    />
                    {prompt.displayName}
                  </h5>
                  <small className="text-muted">
                    {prompt.category}/{prompt.name}
                  </small>
                </div>
              </div>
              <div className="d-flex align-items-center gap-2">
                {prompt.isCustom && (
                  <Badge bg="warning">
                    {t('sales.settings.promptList.badgeCustomized')}
                  </Badge>
                )}
                {dirty && (
                  <Badge bg="secondary">
                    {t('sales.settings.promptEditor.unsavedChanges')}
                  </Badge>
                )}
                {saved && (
                  <Alert variant="success" className="mb-0 py-1 px-3 d-inline">
                    {t('sales.settings.promptEditor.savedToast')}
                  </Alert>
                )}
                <Button
                  variant="outline-secondary"
                  size="sm"
                  onClick={handleReset}
                  disabled={resetting || !prompt.isCustom}
                >
                  {resetting ? (
                    <Spinner size="sm" />
                  ) : (
                    <FontAwesomeIcon icon={faUndo} className="me-1" />
                  )}
                  {t('sales.settings.promptEditor.resetButton')}
                </Button>
                <Button
                  variant="primary"
                  size="sm"
                  onClick={handleSave}
                  disabled={saving || !dirty}
                >
                  {saving ? (
                    <Spinner size="sm" />
                  ) : (
                    <FontAwesomeIcon icon={faSave} className="me-1" />
                  )}
                  {t('sales.settings.promptEditor.saveButton')}
                </Button>
              </div>
            </Card.Header>
          </Card>
        </Col>
      </Row>

      <Row className="g-3 mb-3">
        <Col lg={8}>
          <Card>
            <Card.Header className="d-flex justify-content-between align-items-center">
              <h6 className="mb-0">{t('sales.settings.promptTemplate')}</h6>
              <small className="text-muted">
                {t('sales.settings.promptEditor.promptStats', {
                  chars: content.length,
                  lines: content.split('\n').length
                })}
              </small>
            </Card.Header>
            <Card.Body className="p-0">
              <Form.Control
                as="textarea"
                value={content}
                onChange={e => {
                  setContent(e.target.value);
                  setDirty(true);
                }}
                style={{
                  fontFamily: 'monospace',
                  fontSize: '0.85rem',
                  minHeight: '60vh',
                  border: 'none',
                  borderRadius: 0,
                  resize: 'vertical'
                }}
                className="p-3"
              />
            </Card.Body>
          </Card>
        </Col>

        <Col lg={4}>
          <Card className="mb-3">
            <Card.Header>
              <h6 className="mb-0">{t('sales.settings.promptInfo')}</h6>
            </Card.Header>
            <Card.Body>
              <Form.Group className="mb-3">
                <Form.Label className="fw-semibold">
                  {t('sales.settings.promptEditor.displayNameLabel')}
                </Form.Label>
                <Form.Control
                  value={displayName}
                  onChange={e => {
                    setDisplayName(e.target.value);
                    setDirty(true);
                  }}
                />
              </Form.Group>
              <Form.Group className="mb-3">
                <Form.Label className="fw-semibold">
                  {t('sales.settings.promptEditor.descriptionLabel')}
                </Form.Label>
                <Form.Control
                  as="textarea"
                  rows={2}
                  value={description}
                  onChange={e => {
                    setDescription(e.target.value);
                    setDirty(true);
                  }}
                />
              </Form.Group>
              <dl className="mb-0 small">
                <dt>{t('sales.settings.promptEditor.categoryLabel')}</dt>
                <dd>
                  <Badge bg={CATEGORY_COLORS[prompt.category] || 'secondary'}>
                    {prompt.category}
                  </Badge>
                </dd>
                <dt>{t('sales.settings.promptEditor.internalNameLabel')}</dt>
                <dd className="font-monospace">{prompt.name}</dd>
                <dt>{t('sales.settings.promptEditor.lastUpdatedLabel')}</dt>
                <dd>{new Date(prompt.updatedAt).toLocaleString()}</dd>
              </dl>
            </Card.Body>
          </Card>

          <Card>
            <Card.Header>
              <h6 className="mb-0">{t('sales.settings.templateVariables')}</h6>
            </Card.Header>
            <Card.Body>
              {templateVars.length > 0 ? (
                <div className="d-flex flex-wrap gap-1">
                  {templateVars.map(v => (
                    <Badge
                      key={v}
                      bg="body-tertiary"
                      text="dark"
                      className="font-monospace"
                    >
                      {'{{.'}
                      {v}
                      {'}}'}
                    </Badge>
                  ))}
                </div>
              ) : (
                <small className="text-muted">
                  {t('sales.settings.promptEditor.noTemplateVars')}
                </small>
              )}
              <hr />
              <small className="text-muted">
                {t('sales.settings.promptEditor.availableVarsHeading')}{' '}
                <code>URL</code>, <code>Locale</code>, <code>CompanyName</code>,{' '}
                <code>Industry</code>, <code>Description</code>,{' '}
                <code>RawText</code>, <code>TechStack</code>,{' '}
                <code>TeamMembers</code>, <code>SocialLinks</code>,{' '}
                <code>ContactInfo</code>, <code>AboutText</code>,{' '}
                <code>RegistryData</code>, <code>Context</code>
              </small>
            </Card.Body>
          </Card>
        </Col>
      </Row>
    </>
  );
}

// ─── Prompt List Tab ───

function PromptListTab({
  category,
  icon,
  subtitle
}: {
  category: string;
  icon: any;
  subtitle: string;
}) {
  const { t } = useTranslation();
  const [editingId, setEditingId] = useState<string | null>(null);
  const { data, isLoading } = useListSalesPromptsQuery({});
  const prompts = (data?.prompts || []).filter(p => p.category === category);

  if (editingId) {
    return (
      <PromptEditor promptId={editingId} onBack={() => setEditingId(null)} />
    );
  }

  return (
    <Row className="g-3 mb-3">
      <Col lg={12}>
        <Card>
          <Card.Header>
            <h5 className="mb-0">
              <FontAwesomeIcon
                icon={icon}
                className={`text-${
                  category === 'agents' ? 'primary' : 'info'
                } me-2`}
              />
              {category === 'agents'
                ? t('sales.settings.promptList.agentsTitle')
                : t('sales.settings.promptList.skillsTitle')}
              <small className="text-muted fw-normal ms-2">— {subtitle}</small>
            </h5>
          </Card.Header>
          <Card.Body className="p-0">
            {isLoading ? (
              <div className="text-center py-5">
                <Spinner />
              </div>
            ) : (
              <PromptTable prompts={prompts} onEdit={setEditingId} />
            )}
          </Card.Body>
        </Card>
      </Col>
    </Row>
  );
}

// ─── LLM Configuration Tab ───

function LLMConfigTab() {
  const { t } = useTranslation();
  const { data: settings, isLoading: settingsLoading } =
    useGetSalesSettingsQuery();
  const { data: modelsData, isLoading: modelsLoading } = useListAIModelsQuery({
    type: 'llm'
  });
  const [updateSettings, { isLoading: saving }] =
    useUpdateSalesSettingsMutation();

  const [modelUuid, setModelUuid] = useState('');
  const [temperature, setTemperature] = useState(0);
  const [maxTokens, setMaxTokens] = useState(0);
  const [locale, setLocale] = useState('');
  const [batchMode, setBatchMode] = useState(false);
  const [saved, setSaved] = useState(false);

  const models = modelsData?.models || [];

  useEffect(() => {
    if (settings) {
      setModelUuid(settings.modelUuid || '');
      setTemperature(settings.temperature || 0);
      setMaxTokens(settings.maxTokens || 0);
      setLocale(settings.locale || '');
      setBatchMode(settings.batchMode || false);
    }
  }, [settings]);

  const handleSave = async () => {
    setSaved(false);
    await updateSettings({
      modelUuid,
      temperature,
      maxTokens,
      locale,
      batchMode
    }).unwrap();
    setSaved(true);
    setTimeout(() => setSaved(false), 3000);
  };

  const selectedModel = models.find((m: any) => m.uuid === modelUuid);
  const defaultModel = models.find((m: any) => m.isDefault);

  if (settingsLoading || modelsLoading) {
    return (
      <div className="text-center py-5">
        <Spinner />
      </div>
    );
  }

  return (
    <Row className="g-3 mb-3">
      <Col lg={8}>
        <Card>
          <Card.Header>
            <h5 className="mb-0">{t('sales.settings.llm.title')}</h5>
          </Card.Header>
          <Card.Body>
            <Form>
              <Form.Group className="mb-4">
                <Form.Label className="fw-semibold">
                  {t('sales.settings.llm.modelLabel')}
                </Form.Label>
                <Form.Select
                  value={modelUuid}
                  onChange={e => setModelUuid(e.target.value)}
                >
                  <option value="">
                    {t('sales.settings.llm.modelDefaultOption')}
                    {defaultModel
                      ? t('sales.settings.llm.modelDefaultSuffix', {
                          name: defaultModel.name,
                          modelName: defaultModel.modelName
                        })
                      : ''}
                  </option>
                  {models.map((m: any) => (
                    <option key={m.uuid} value={m.uuid}>
                      {m.name} — {m.modelName}
                      {m.provider !== 'ollama' ? ` (${m.provider})` : ''}
                      {m.isDefault
                        ? t('sales.settings.llm.modelDefaultStar')
                        : ''}
                      {!m.isActive
                        ? t('sales.settings.llm.modelDisabledSuffix')
                        : ''}
                    </option>
                  ))}
                </Form.Select>
                <Form.Text className="text-muted">
                  {t('sales.settings.llm.modelHelpIntro')}{' '}
                  <Link to="/admin/modules/aimodels">
                    {t('sales.settings.llm.manageModelsLink')}{' '}
                    <FontAwesomeIcon icon={faExternalLinkAlt} size="xs" />
                  </Link>
                </Form.Text>
              </Form.Group>

              <Form.Group className="mb-4">
                <Form.Label className="fw-semibold">
                  {t('sales.settings.llm.temperatureLabel', {
                    value:
                      temperature > 0
                        ? temperature.toFixed(2)
                        : t('sales.settings.llm.temperatureModelDefault')
                  })}
                </Form.Label>
                <Form.Range
                  min={0}
                  max={1}
                  step={0.05}
                  value={temperature}
                  onChange={e => setTemperature(parseFloat(e.target.value))}
                />
                <Form.Text className="text-muted">
                  {t('sales.settings.llm.temperatureHelp')}
                </Form.Text>
              </Form.Group>

              <Form.Group className="mb-4">
                <Form.Label className="fw-semibold">
                  {t('sales.settings.llm.maxTokensLabel')}
                </Form.Label>
                <Form.Control
                  type="number"
                  min={0}
                  max={128000}
                  step={256}
                  value={maxTokens}
                  onChange={e => setMaxTokens(parseInt(e.target.value) || 0)}
                  placeholder={t('sales.settings.llm.maxTokensPlaceholder')}
                />
                <Form.Text className="text-muted">
                  {t('sales.settings.llm.maxTokensHelp')}
                </Form.Text>
              </Form.Group>

              <Form.Group className="mb-4">
                <Form.Label className="fw-semibold">
                  {t('sales.settings.llm.localeLabel')}
                </Form.Label>
                <Form.Select
                  value={locale}
                  onChange={e => setLocale(e.target.value)}
                >
                  <option value="">
                    {t('sales.settings.llm.localeSystemDefault')}
                  </option>
                  <option value="it">
                    {t('sales.settings.llm.localeItalian')}
                  </option>
                  <option value="en">
                    {t('sales.settings.llm.localeEnglish')}
                  </option>
                </Form.Select>
                <Form.Text className="text-muted">
                  {t('sales.settings.llm.localeHelp')}
                </Form.Text>
              </Form.Group>

              <Form.Group className="mb-4">
                <Form.Check
                  type="switch"
                  id="batchMode"
                  label={t('sales.settings.llm.batchModeLabel')}
                  checked={batchMode}
                  onChange={e => setBatchMode(e.target.checked)}
                />
                <Form.Text className="text-muted">
                  {t('sales.settings.llm.batchModeHelp')}
                </Form.Text>
              </Form.Group>

              <div className="d-flex align-items-center gap-3">
                <Button
                  variant="primary"
                  onClick={handleSave}
                  disabled={saving}
                >
                  {saving ? (
                    <Spinner size="sm" className="me-1" />
                  ) : (
                    <FontAwesomeIcon icon={faSave} className="me-1" />
                  )}
                  {t('sales.settings.llm.saveButton')}
                </Button>
                {saved && (
                  <Alert variant="success" className="mb-0 py-1 px-3">
                    {t('sales.settings.llm.savedToast')}
                  </Alert>
                )}
              </div>
            </Form>
          </Card.Body>
        </Card>
      </Col>

      <Col lg={4}>
        <Card>
          <Card.Header>
            <h5 className="mb-0">{t('sales.settings.activeConfiguration')}</h5>
          </Card.Header>
          <Card.Body>
            <dl className="mb-0">
              <dt>{t('sales.settings.activeConfig.modelLabel')}</dt>
              <dd className="text-muted">
                {selectedModel
                  ? t('sales.settings.activeConfig.modelSelected', {
                      name: selectedModel.name,
                      modelName: selectedModel.modelName
                    })
                  : defaultModel
                    ? `${t('sales.settings.activeConfig.modelSelected', {
                        name: defaultModel.name,
                        modelName: defaultModel.modelName
                      })}${t(
                        'sales.settings.activeConfig.modelSystemDefaultSuffix'
                      )}`
                    : t('sales.settings.activeConfig.modelNone')}
              </dd>
              <dt>{t('sales.settings.activeConfig.providerLabel')}</dt>
              <dd className="text-muted">
                {(selectedModel || defaultModel)?.provider ||
                  t('sales.settings.activeConfig.providerDash')}
                {(selectedModel || defaultModel)?.providerCategory === 'local'
                  ? t('sales.settings.activeConfig.providerLocal')
                  : t('sales.settings.activeConfig.providerCloud')}
              </dd>
              <dt>{t('sales.settings.activeConfig.temperatureLabel')}</dt>
              <dd className="text-muted">
                {temperature > 0
                  ? temperature.toFixed(2)
                  : t('sales.settings.activeConfig.temperatureModelDefault', {
                      value: (selectedModel || defaultModel)?.temperature || 0.1
                    })}
              </dd>
              <dt>{t('sales.settings.activeConfig.maxTokensLabel')}</dt>
              <dd className="text-muted">
                {maxTokens > 0
                  ? maxTokens
                  : t('sales.settings.activeConfig.maxTokensDefault', {
                      value: (selectedModel || defaultModel)?.maxTokens || 4096
                    })}
              </dd>
              <dt>{t('sales.settings.activeConfig.localeLabel')}</dt>
              <dd className="text-muted">
                {locale || t('sales.settings.activeConfig.localeDefault')}
              </dd>
              <dt>{t('sales.settings.activeConfig.batchModeLabel')}</dt>
              <dd className="text-muted">
                {batchMode
                  ? t('sales.settings.activeConfig.batchModeEnabled')
                  : t('sales.settings.activeConfig.batchModeDisabled')}
              </dd>
            </dl>
          </Card.Body>
        </Card>
      </Col>
    </Row>
  );
}

// ─── Main Settings Page ───

const SalesSettingsPage = () => {
  const { t } = useTranslation();
  const [searchParams, setSearchParams] = useSearchParams();
  const activeTab = searchParams.get('tab') || 'llm';

  return (
    <>
      <Row className="g-3 mb-3">
        <Col xxl={12}>
          <Card className="bg-body-tertiary dark__bg-opacity-50 shadow-none h-100">
            <Background
              image={greetingsBg}
              className="bg-card d-none d-sm-block"
            />
            <Card.Header className="d-flex align-items-center z-1 p-0">
              <div className="bg-primary rounded-circle p-3 ms-3">
                <FontAwesomeIcon
                  icon={faCog}
                  className="text-white"
                  size="2x"
                />
              </div>
              <div className="ms-3">
                <h6 className="mb-1 text-primary">{t('sales.kicker')}</h6>
                <h4 className="mb-0 text-primary fw-bold">
                  {t('sales.settings.title')}
                </h4>
              </div>
            </Card.Header>
          </Card>
        </Col>
      </Row>

      <Nav
        variant="tabs"
        activeKey={activeTab}
        onSelect={k => {
          if (!k) return;
          setSearchParams(
            prev => {
              prev.set('tab', k);
              return prev;
            },
            { replace: true }
          );
        }}
        className="mb-3"
      >
        <Nav.Item>
          <Nav.Link eventKey="llm">
            <FontAwesomeIcon icon={faCog} className="me-1" />{' '}
            {t('sales.settings.tabs.llm')}
          </Nav.Link>
        </Nav.Item>
        <Nav.Item>
          <Nav.Link eventKey="agents">
            <FontAwesomeIcon icon={faRobot} className="me-1" />{' '}
            {t('sales.settings.tabs.agents')}
          </Nav.Link>
        </Nav.Item>
        <Nav.Item>
          <Nav.Link eventKey="skills">
            <FontAwesomeIcon icon={faMagic} className="me-1" />{' '}
            {t('sales.settings.tabs.skills')}
          </Nav.Link>
        </Nav.Item>
      </Nav>

      {activeTab === 'llm' && <LLMConfigTab />}
      {activeTab === 'agents' && (
        <PromptListTab
          category="agents"
          icon={faRobot}
          subtitle={t('sales.settings.promptList.agentsSubtitle')}
        />
      )}
      {activeTab === 'skills' && (
        <PromptListTab
          category="skills"
          icon={faMagic}
          subtitle={t('sales.settings.promptList.skillsSubtitle')}
        />
      )}
    </>
  );
};

export default SalesSettingsPage;
