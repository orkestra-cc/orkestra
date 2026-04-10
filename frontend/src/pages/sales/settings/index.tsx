import { useState, useEffect } from 'react';
import { Row, Col, Card, Form, Button, Spinner, Alert, Nav, Table, Badge } from 'react-bootstrap';
import { Link } from 'react-router-dom';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { faCog, faSave, faExternalLinkAlt, faRobot, faMagic, faScroll, faUndo, faArrowLeft, faPen } from '@fortawesome/free-solid-svg-icons';
import Background from 'components/common/Background';
import greetingsBg from 'assets/img/illustrations/ticket-greetings-bg.png';
import {
  useGetSalesSettingsQuery,
  useUpdateSalesSettingsMutation,
  useListSalesPromptsQuery,
  useGetSalesPromptQuery,
  useUpdateSalesPromptMutation,
  useResetSalesPromptMutation,
} from '../../../store/api/salesApi';
import type { SalesPromptConfig } from '../../../store/api/salesApi';
import { useListAIModelsQuery } from '../../../store/api/aiModelsApi';

// ─── Prompt Table ───

function PromptTable({ prompts, onEdit }: { prompts: SalesPromptConfig[]; onEdit: (id: string) => void }) {
  if (prompts.length === 0) {
    return <div className="text-center text-muted py-4">No prompts found</div>;
  }

  return (
    <Table hover responsive className="mb-0">
      <thead>
        <tr>
          <th>Name</th>
          <th>Description</th>
          <th>Status</th>
          <th>Size</th>
          <th>Updated</th>
          <th></th>
        </tr>
      </thead>
      <tbody>
        {prompts.map(p => (
          <tr key={p.uuid} style={{ cursor: 'pointer' }} onClick={() => onEdit(p.uuid)}>
            <td>
              <strong>{p.displayName}</strong>
              <br />
              <small className="text-muted font-monospace">{p.name}</small>
            </td>
            <td><small className="text-muted">{p.description}</small></td>
            <td>
              {p.isCustom
                ? <Badge bg="warning">Customized</Badge>
                : <Badge bg="secondary">Default</Badge>
              }
            </td>
            <td><small>{p.content?.length || 0} chars</small></td>
            <td><small>{new Date(p.updatedAt).toLocaleDateString()}</small></td>
            <td>
              <Button variant="outline-primary" size="sm" onClick={e => { e.stopPropagation(); onEdit(p.uuid); }}>
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

const CATEGORY_ICONS: Record<string, any> = { agents: faRobot, skills: faMagic };
const CATEGORY_COLORS: Record<string, string> = { agents: 'primary', skills: 'info' };

function PromptEditor({ promptId, onBack }: { promptId: string; onBack: () => void }) {
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
    return <Card><Card.Body className="text-center py-5"><Spinner /></Card.Body></Card>;
  }

  const handleSave = async () => {
    setSaved(false);
    await updatePrompt({ uuid: prompt.uuid, content, displayName, description }).unwrap();
    setSaved(true);
    setDirty(false);
    setTimeout(() => setSaved(false), 3000);
  };

  const handleReset = async () => {
    if (!window.confirm('Reset this prompt to the built-in default? Your custom edits will be lost.')) return;
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
                    <FontAwesomeIcon icon={CATEGORY_ICONS[prompt.category] || faScroll} className="text-primary" />
                    {prompt.displayName}
                  </h5>
                  <small className="text-muted">{prompt.category}/{prompt.name}</small>
                </div>
              </div>
              <div className="d-flex align-items-center gap-2">
                {prompt.isCustom && <Badge bg="warning">Customized</Badge>}
                {dirty && <Badge bg="secondary">Unsaved changes</Badge>}
                {saved && <Alert variant="success" className="mb-0 py-1 px-3 d-inline">Saved!</Alert>}
                <Button variant="outline-secondary" size="sm" onClick={handleReset} disabled={resetting || !prompt.isCustom}>
                  {resetting ? <Spinner size="sm" /> : <FontAwesomeIcon icon={faUndo} className="me-1" />}
                  Reset Default
                </Button>
                <Button variant="primary" size="sm" onClick={handleSave} disabled={saving || !dirty}>
                  {saving ? <Spinner size="sm" /> : <FontAwesomeIcon icon={faSave} className="me-1" />}
                  Save
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
              <h6 className="mb-0">Prompt Template</h6>
              <small className="text-muted">{content.length} chars | {content.split('\n').length} lines</small>
            </Card.Header>
            <Card.Body className="p-0">
              <Form.Control
                as="textarea"
                value={content}
                onChange={e => { setContent(e.target.value); setDirty(true); }}
                style={{
                  fontFamily: 'monospace',
                  fontSize: '0.85rem',
                  minHeight: '60vh',
                  border: 'none',
                  borderRadius: 0,
                  resize: 'vertical',
                }}
                className="p-3"
              />
            </Card.Body>
          </Card>
        </Col>

        <Col lg={4}>
          <Card className="mb-3">
            <Card.Header><h6 className="mb-0">Prompt Info</h6></Card.Header>
            <Card.Body>
              <Form.Group className="mb-3">
                <Form.Label className="fw-semibold">Display Name</Form.Label>
                <Form.Control
                  value={displayName}
                  onChange={e => { setDisplayName(e.target.value); setDirty(true); }}
                />
              </Form.Group>
              <Form.Group className="mb-3">
                <Form.Label className="fw-semibold">Description</Form.Label>
                <Form.Control
                  as="textarea"
                  rows={2}
                  value={description}
                  onChange={e => { setDescription(e.target.value); setDirty(true); }}
                />
              </Form.Group>
              <dl className="mb-0 small">
                <dt>Category</dt>
                <dd><Badge bg={CATEGORY_COLORS[prompt.category] || 'secondary'}>{prompt.category}</Badge></dd>
                <dt>Internal Name</dt>
                <dd className="font-monospace">{prompt.name}</dd>
                <dt>Last Updated</dt>
                <dd>{new Date(prompt.updatedAt).toLocaleString()}</dd>
              </dl>
            </Card.Body>
          </Card>

          <Card>
            <Card.Header><h6 className="mb-0">Template Variables</h6></Card.Header>
            <Card.Body>
              {templateVars.length > 0 ? (
                <div className="d-flex flex-wrap gap-1">
                  {templateVars.map(v => (
                    <Badge key={v} bg="body-tertiary" text="dark" className="font-monospace">
                      {'{{.'}{v}{'}}'}
                    </Badge>
                  ))}
                </div>
              ) : (
                <small className="text-muted">No template variables detected</small>
              )}
              <hr />
              <small className="text-muted">
                Available variables: <code>URL</code>, <code>Locale</code>, <code>CompanyName</code>,{' '}
                <code>Industry</code>, <code>Description</code>, <code>RawText</code>,{' '}
                <code>TechStack</code>, <code>TeamMembers</code>, <code>SocialLinks</code>,{' '}
                <code>ContactInfo</code>, <code>AboutText</code>, <code>RegistryData</code>,{' '}
                <code>Context</code>
              </small>
            </Card.Body>
          </Card>
        </Col>
      </Row>
    </>
  );
}

// ─── Prompt List Tab ───

function PromptListTab({ category, icon, subtitle }: { category: string; icon: any; subtitle: string }) {
  const [editingId, setEditingId] = useState<string | null>(null);
  const { data, isLoading } = useListSalesPromptsQuery({});
  const prompts = (data?.prompts || []).filter(p => p.category === category);

  if (editingId) {
    return <PromptEditor promptId={editingId} onBack={() => setEditingId(null)} />;
  }

  return (
    <Row className="g-3 mb-3">
      <Col lg={12}>
        <Card>
          <Card.Header>
            <h5 className="mb-0">
              <FontAwesomeIcon icon={icon} className={`text-${category === 'agents' ? 'primary' : 'info'} me-2`} />
              {category === 'agents' ? 'Agent Prompts' : 'Skill Prompts'}
              <small className="text-muted fw-normal ms-2">— {subtitle}</small>
            </h5>
          </Card.Header>
          <Card.Body className="p-0">
            {isLoading ? (
              <div className="text-center py-5"><Spinner /></div>
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
  const { data: settings, isLoading: settingsLoading } = useGetSalesSettingsQuery();
  const { data: modelsData, isLoading: modelsLoading } = useListAIModelsQuery({ type: 'llm' });
  const [updateSettings, { isLoading: saving }] = useUpdateSalesSettingsMutation();

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
    await updateSettings({ modelUuid, temperature, maxTokens, locale, batchMode }).unwrap();
    setSaved(true);
    setTimeout(() => setSaved(false), 3000);
  };

  const selectedModel = models.find((m: any) => m.uuid === modelUuid);
  const defaultModel = models.find((m: any) => m.isDefault);

  if (settingsLoading || modelsLoading) {
    return <div className="text-center py-5"><Spinner /></div>;
  }

  return (
    <Row className="g-3 mb-3">
      <Col lg={8}>
        <Card>
          <Card.Header><h5 className="mb-0">LLM Configuration</h5></Card.Header>
          <Card.Body>
            <Form>
              <Form.Group className="mb-4">
                <Form.Label className="fw-semibold">AI Model</Form.Label>
                <Form.Select value={modelUuid} onChange={e => setModelUuid(e.target.value)}>
                  <option value="">
                    System Default{defaultModel ? ` (${defaultModel.name} — ${defaultModel.modelName})` : ''}
                  </option>
                  {models.map((m: any) => (
                    <option key={m.uuid} value={m.uuid}>
                      {m.name} — {m.modelName}
                      {m.provider !== 'ollama' ? ` (${m.provider})` : ''}
                      {m.isDefault ? ' ★' : ''}
                      {!m.isActive ? ' [disabled]' : ''}
                    </option>
                  ))}
                </Form.Select>
                <Form.Text className="text-muted">
                  Select which LLM to use for prospect analysis and skills.{' '}
                  <Link to="/ai/models">
                    Manage models <FontAwesomeIcon icon={faExternalLinkAlt} size="xs" />
                  </Link>
                </Form.Text>
              </Form.Group>

              <Form.Group className="mb-4">
                <Form.Label className="fw-semibold">
                  Temperature: {temperature > 0 ? temperature.toFixed(2) : 'Model default'}
                </Form.Label>
                <Form.Range
                  min={0} max={1} step={0.05}
                  value={temperature}
                  onChange={e => setTemperature(parseFloat(e.target.value))}
                />
                <Form.Text className="text-muted">
                  0 = use model default. Lower = more focused/deterministic. Higher = more creative.
                </Form.Text>
              </Form.Group>

              <Form.Group className="mb-4">
                <Form.Label className="fw-semibold">Max Output Tokens</Form.Label>
                <Form.Control
                  type="number" min={0} max={128000} step={256}
                  value={maxTokens}
                  onChange={e => setMaxTokens(parseInt(e.target.value) || 0)}
                  placeholder="0 = model default (4096)"
                />
                <Form.Text className="text-muted">
                  Maximum tokens in LLM responses. 0 uses the default (4096).
                </Form.Text>
              </Form.Group>

              <Form.Group className="mb-4">
                <Form.Label className="fw-semibold">Default Locale</Form.Label>
                <Form.Select value={locale} onChange={e => setLocale(e.target.value)}>
                  <option value="">System default (Italian)</option>
                  <option value="it">Italian</option>
                  <option value="en">English</option>
                </Form.Select>
                <Form.Text className="text-muted">
                  Controls prompt language and cultural adaptation for outreach, proposals, and qualification.
                </Form.Text>
              </Form.Group>

              <Form.Group className="mb-4">
                <Form.Check
                  type="switch"
                  id="batchMode"
                  label="Batch Mode (50% cost savings)"
                  checked={batchMode}
                  onChange={e => setBatchMode(e.target.checked)}
                />
                <Form.Text className="text-muted">
                  Submit prospect analysis as a batch job via the provider's batch API.
                  Results typically arrive within 1 hour. Only works with cloud LLMs
                  (OpenAI, Anthropic, Gemini). Local models always use standard mode.
                </Form.Text>
              </Form.Group>

              <div className="d-flex align-items-center gap-3">
                <Button variant="primary" onClick={handleSave} disabled={saving}>
                  {saving ? <Spinner size="sm" className="me-1" /> : <FontAwesomeIcon icon={faSave} className="me-1" />}
                  Save Settings
                </Button>
                {saved && <Alert variant="success" className="mb-0 py-1 px-3">Saved!</Alert>}
              </div>
            </Form>
          </Card.Body>
        </Card>
      </Col>

      <Col lg={4}>
        <Card>
          <Card.Header><h5 className="mb-0">Active Configuration</h5></Card.Header>
          <Card.Body>
            <dl className="mb-0">
              <dt>Model</dt>
              <dd className="text-muted">
                {selectedModel
                  ? `${selectedModel.name} (${selectedModel.modelName})`
                  : defaultModel
                    ? `${defaultModel.name} (${defaultModel.modelName}) — system default`
                    : 'No LLM configured'}
              </dd>
              <dt>Provider</dt>
              <dd className="text-muted">
                {(selectedModel || defaultModel)?.provider || '—'}
                {(selectedModel || defaultModel)?.providerCategory === 'local' ? ' (local)' : ' (cloud)'}
              </dd>
              <dt>Temperature</dt>
              <dd className="text-muted">
                {temperature > 0 ? temperature.toFixed(2) : `Model default (${(selectedModel || defaultModel)?.temperature || 0.1})`}
              </dd>
              <dt>Max Tokens</dt>
              <dd className="text-muted">
                {maxTokens > 0 ? maxTokens : `Default (${(selectedModel || defaultModel)?.maxTokens || 4096})`}
              </dd>
              <dt>Locale</dt>
              <dd className="text-muted">{locale || 'Italian (default)'}</dd>
              <dt>Batch Mode</dt>
              <dd className="text-muted">{batchMode ? 'Enabled (50% savings)' : 'Disabled (real-time)'}</dd>
            </dl>
          </Card.Body>
        </Card>
      </Col>
    </Row>
  );
}

// ─── Main Settings Page ───

const SalesSettingsPage = () => {
  const [activeTab, setActiveTab] = useState('llm');

  return (
    <>
      <Row className="g-3 mb-3">
        <Col xxl={12}>
          <Card className="bg-body-tertiary dark__bg-opacity-50 shadow-none h-100">
            <Background image={greetingsBg} className="bg-card d-none d-sm-block" />
            <Card.Header className="d-flex align-items-center z-1 p-0">
              <div className="bg-primary rounded-circle p-3 ms-3">
                <FontAwesomeIcon icon={faCog} className="text-white" size="2x" />
              </div>
              <div className="ms-3">
                <h6 className="mb-1 text-primary">Sales Intelligence</h6>
                <h4 className="mb-0 text-primary fw-bold">Settings</h4>
              </div>
            </Card.Header>
          </Card>
        </Col>
      </Row>

      <Nav variant="tabs" activeKey={activeTab} onSelect={k => setActiveTab(k || 'llm')} className="mb-3">
        <Nav.Item>
          <Nav.Link eventKey="llm">
            <FontAwesomeIcon icon={faCog} className="me-1" /> LLM Configuration
          </Nav.Link>
        </Nav.Item>
        <Nav.Item>
          <Nav.Link eventKey="agents">
            <FontAwesomeIcon icon={faRobot} className="me-1" /> Agent Prompts
          </Nav.Link>
        </Nav.Item>
        <Nav.Item>
          <Nav.Link eventKey="skills">
            <FontAwesomeIcon icon={faMagic} className="me-1" /> Skill Prompts
          </Nav.Link>
        </Nav.Item>
      </Nav>

      {activeTab === 'llm' && <LLMConfigTab />}
      {activeTab === 'agents' && (
        <PromptListTab category="agents" icon={faRobot} subtitle="Used during the 5-agent parallel prospect analysis" />
      )}
      {activeTab === 'skills' && (
        <PromptListTab category="skills" icon={faMagic} subtitle="Used by individual skill endpoints" />
      )}
    </>
  );
};

export default SalesSettingsPage;
