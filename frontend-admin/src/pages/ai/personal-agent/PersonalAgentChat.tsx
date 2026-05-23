import { useState, useCallback, useRef, useEffect } from 'react';
import Markdown from 'react-markdown';
import { useTranslation } from 'react-i18next';
import {
  Card,
  Row,
  Col,
  Button,
  Form,
  Spinner,
  Badge,
  Accordion,
  ListGroup,
  Dropdown,
  Offcanvas
} from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import {
  faPaperPlane,
  faPlus,
  faComments,
  faTrash,
  faBars,
  faRobot,
  faUser,
  faChevronLeft,
  faFileAlt,
  faClock,
  faCog
} from '@fortawesome/free-solid-svg-icons';
import classNames from 'classnames';
import dayjs from 'dayjs';
import relativeTime from 'dayjs/plugin/relativeTime';

import {
  useGetPersonalAgentQuery,
  usePersonalAgentQueryMutation,
  useListPersonalConversationsQuery,
  useGetPersonalConversationQuery,
  useDeletePersonalConversationMutation,
  useAddPersonalDocumentsMutation,
  useRemovePersonalDocumentsMutation,
  useUpdatePersonalSettingsMutation
} from '../../../store/api/personalAgentApi';
import { useListDocumentsQuery } from '../../../store/api/ragApi';
import type {
  AgentMessage,
  AgentSource,
  PersonaType,
  AgentSettings
} from '../../../types/agents';
import { PERSONA_LABELS, PERSONA_DESCRIPTIONS } from '../../../types/agents';
import type { RagDocument } from '../../../types/rag';

dayjs.extend(relativeTime);

// ---------------------------------------------------------------------------
// Sub-components
// ---------------------------------------------------------------------------

interface MessageBubbleProps {
  message: AgentMessage;
  isLoading?: boolean;
}

function MessageBubble({ message, isLoading }: MessageBubbleProps) {
  const { t } = useTranslation();
  const isUser = message.role === 'user';

  return (
    <div
      className={classNames('d-flex mb-3', {
        'justify-content-end': isUser,
        'justify-content-start': !isUser
      })}
    >
      <div style={{ maxWidth: '75%' }}>
        <div className="d-flex align-items-center gap-2 mb-1">
          {!isUser && (
            <FontAwesomeIcon
              icon={faRobot}
              className="text-secondary"
              size="sm"
            />
          )}
          <small className="text-muted">
            {isUser
              ? t('aiAgents.chat.messageBubble.user')
              : t('aiAgents.chat.messageBubble.assistant')}
            {message.createdAt && (
              <span className="ms-2">{dayjs(message.createdAt).fromNow()}</span>
            )}
          </small>
          {isUser && (
            <FontAwesomeIcon icon={faUser} className="text-primary" size="sm" />
          )}
        </div>

        <div
          className={classNames('p-3 rounded-3', {
            'bg-primary text-white': isUser,
            'bg-200': !isUser
          })}
        >
          {isLoading ? (
            <div className="d-flex align-items-center gap-2">
              <Spinner size="sm" animation="border" />
              <span className="text-muted">
                {t('aiAgents.chat.messageBubble.thinking')}
              </span>
            </div>
          ) : isUser ? (
            <p className="mb-0 white-space-pre-line">{message.content}</p>
          ) : (
            <div className="mb-0 agent-markdown">
              <Markdown>{message.content}</Markdown>
            </div>
          )}
        </div>

        {/* Metadata badges */}
        {!isUser && message.metadata?.totalTimeMs && (
          <div className="mt-1 d-flex gap-3 flex-wrap">
            <small className="text-muted">
              <FontAwesomeIcon icon={faClock} className="me-1" size="xs" />
              {t('aiAgents.chat.messageBubble.latencySeconds', {
                seconds: (message.metadata.totalTimeMs / 1000).toFixed(1)
              })}
            </small>
            {message.metadata.totalTokens ? (
              <small className="text-muted">
                {t('aiAgents.chat.messageBubble.tokens', {
                  count: message.metadata.totalTokens
                })}
                <span className="ms-1 text-muted-50">
                  {' '}
                  {t('aiAgents.chat.messageBubble.tokensBreakdown', {
                    input: message.metadata.inputTokens,
                    output: message.metadata.outputTokens
                  })}
                </span>
              </small>
            ) : null}
            {message.metadata.modelUsed && (
              <small className="text-muted">{message.metadata.modelUsed}</small>
            )}
          </div>
        )}

        {/* Source citations */}
        {!isUser && message.sources && message.sources.length > 0 && (
          <Accordion className="mt-2">
            <Accordion.Item eventKey="0">
              <Accordion.Header>
                <small>
                  <FontAwesomeIcon icon={faFileAlt} className="me-1" />
                  {t(
                    message.sources.length === 1
                      ? 'aiAgents.chat.messageBubble.sources_one'
                      : 'aiAgents.chat.messageBubble.sources_other',
                    { count: message.sources.length }
                  )}
                </small>
              </Accordion.Header>
              <Accordion.Body className="p-2">
                {message.sources.map((source: AgentSource, idx: number) => (
                  <div
                    key={`${source.documentUuid}-${idx}`}
                    className="border-bottom pb-2 mb-2 last-child-mb-0"
                  >
                    <div className="d-flex justify-content-between align-items-start">
                      <div>
                        <span className="fw-semibold small">
                          {source.documentTitle}
                        </span>
                        <br />
                        <small className="text-muted">{source.fullPath}</small>
                      </div>
                      <Badge
                        bg={
                          source.score >= 0.8
                            ? 'success'
                            : source.score >= 0.5
                              ? 'warning'
                              : 'secondary'
                        }
                        className="ms-2"
                      >
                        {(source.score * 100).toFixed(0)}%
                      </Badge>
                    </div>
                    <small
                      className="text-muted d-block mt-1 font-monospace"
                      style={{ fontSize: '0.75rem' }}
                    >
                      {source.chunkText.length > 200
                        ? `${source.chunkText.slice(0, 200)}...`
                        : source.chunkText}
                    </small>
                  </div>
                ))}
              </Accordion.Body>
            </Accordion.Item>
          </Accordion>
        )}
      </div>
    </div>
  );
}

interface ConversationSidebarProps {
  conversations: {
    uuid: string;
    title?: string;
    persona: string;
    updatedAt: string;
  }[];
  activeId: string | null;
  onSelect: (uuid: string) => void;
  onDelete: (uuid: string) => void;
  isDeleting: boolean;
}

function ConversationSidebar({
  conversations,
  activeId,
  onSelect,
  onDelete,
  isDeleting
}: ConversationSidebarProps) {
  const { t } = useTranslation();
  return (
    <ListGroup
      variant="flush"
      className="overflow-auto"
      style={{ maxHeight: '100%' }}
    >
      {conversations.length === 0 && (
        <div className="text-center text-muted py-4">
          <FontAwesomeIcon icon={faComments} className="mb-2" size="2x" />
          <p className="small mb-0">{t('aiAgents.chat.sidebarEmpty')}</p>
        </div>
      )}
      {conversations.map(conv => (
        <ListGroup.Item
          key={conv.uuid}
          action
          active={conv.uuid === activeId}
          onClick={() => onSelect(conv.uuid)}
          className="d-flex justify-content-between align-items-start py-2 px-3"
        >
          <div className="text-truncate me-2">
            <div className="fw-semibold small text-truncate">
              {conv.title || t('aiAgents.chat.untitled')}
            </div>
            <small
              className={classNames({
                'text-white-50': conv.uuid === activeId,
                'text-muted': conv.uuid !== activeId
              })}
            >
              {t(`aiAgents.chat.personaLabels.${conv.persona}`, {
                defaultValue:
                  PERSONA_LABELS[conv.persona as PersonaType] ?? conv.persona
              })}
              {' \u00b7 '}
              {dayjs(conv.updatedAt).fromNow()}
            </small>
          </div>
          <Button
            variant="link"
            size="sm"
            className={classNames('p-0 flex-shrink-0', {
              'text-white-50': conv.uuid === activeId,
              'text-danger': conv.uuid !== activeId
            })}
            onClick={e => {
              e.stopPropagation();
              onDelete(conv.uuid);
            }}
            disabled={isDeleting}
          >
            <FontAwesomeIcon icon={faTrash} size="sm" />
          </Button>
        </ListGroup.Item>
      ))}
    </ListGroup>
  );
}

// ---------------------------------------------------------------------------
// Document Picker
// ---------------------------------------------------------------------------

interface DocumentPickerProps {
  show: boolean;
  onHide: () => void;
  documents: RagDocument[];
  selectedUuids: string[];
  onAdd: (uuids: string[]) => void;
  onRemove: (uuids: string[]) => void;
}

function DocumentPicker({
  show,
  onHide,
  documents,
  selectedUuids,
  onAdd,
  onRemove
}: DocumentPickerProps) {
  const { t } = useTranslation();
  const handleToggle = (doc: RagDocument) => {
    if (selectedUuids.includes(doc.uuid)) {
      onRemove([doc.uuid]);
    } else {
      onAdd([doc.uuid]);
    }
  };

  return (
    <Offcanvas show={show} onHide={onHide} placement="end">
      <Offcanvas.Header closeButton>
        <Offcanvas.Title>
          <FontAwesomeIcon icon={faFileAlt} className="me-2" />
          {t('aiAgents.personal.documentsTitle')}
        </Offcanvas.Title>
      </Offcanvas.Header>
      <Offcanvas.Body>
        {documents.length === 0 && (
          <p className="text-muted text-center py-4">
            {t('aiAgents.personal.documentsEmpty')}
          </p>
        )}
        <ListGroup variant="flush">
          {documents.map(doc => (
            <ListGroup.Item
              key={doc.uuid}
              className="d-flex align-items-start gap-2 px-0"
            >
              <Form.Check
                type="checkbox"
                checked={selectedUuids.includes(doc.uuid)}
                onChange={() => handleToggle(doc)}
                className="mt-1"
              />
              <div className="flex-1">
                <div className="fw-semibold small">{doc.title}</div>
                <div className="d-flex gap-1 flex-wrap mt-1">
                  {doc.isoStandard && (
                    <Badge bg="info" className="fw-normal">
                      {doc.isoStandard}
                    </Badge>
                  )}
                  {doc.documentCategory && (
                    <Badge bg="secondary" className="fw-normal">
                      {doc.documentCategory}
                    </Badge>
                  )}
                  <Badge bg="light" text="dark" className="fw-normal">
                    {t('aiAgents.personal.chunkCount', {
                      count: doc.chunkCount
                    })}
                  </Badge>
                </div>
              </div>
            </ListGroup.Item>
          ))}
        </ListGroup>
      </Offcanvas.Body>
    </Offcanvas>
  );
}

// ---------------------------------------------------------------------------
// Settings Offcanvas
// ---------------------------------------------------------------------------

const TEMPERATURE_OPTIONS: {
  value: AgentSettings['temperature'];
  labelKey: string;
}[] = [
  { value: 'precise', labelKey: 'aiAgents.personal.temperaturePrecise' },
  { value: 'balanced', labelKey: 'aiAgents.personal.temperatureBalanced' },
  { value: 'creative', labelKey: 'aiAgents.personal.temperatureCreative' }
];

// English/Italiano are proper-noun language names — left as literals
// (matches the convention used in the Agent Settings form).
const LANGUAGE_OPTIONS = [
  { value: 'en', label: 'English' },
  { value: 'it', label: 'Italiano' }
];

interface SettingsOffcanvasProps {
  show: boolean;
  onHide: () => void;
  settings: AgentSettings;
  onSave: (settings: Partial<AgentSettings>) => void;
}

function SettingsOffcanvas({
  show,
  onHide,
  settings,
  onSave
}: SettingsOffcanvasProps) {
  const { t } = useTranslation();
  const [language, setLanguage] = useState(settings.language ?? 'en');
  const [temperature, setTemperature] = useState<AgentSettings['temperature']>(
    settings.temperature ?? 'balanced'
  );
  const [skepticism, setSkepticism] = useState(settings.skepticism ?? 3);
  const [literalism, setLiteralism] = useState(settings.literalism ?? 3);
  const [empathy, setEmpathy] = useState(settings.empathy ?? 3);

  // Sync local state when the settings prop changes (e.g. after a save round-trip).
  useEffect(() => {
    setLanguage(settings.language ?? 'en');
    setTemperature(settings.temperature ?? 'balanced');
    setSkepticism(settings.skepticism ?? 3);
    setLiteralism(settings.literalism ?? 3);
    setEmpathy(settings.empathy ?? 3);
  }, [settings]);

  const handleSave = () => {
    onSave({ language, temperature, skepticism, literalism, empathy });
    onHide();
  };

  return (
    <Offcanvas show={show} onHide={onHide} placement="end">
      <Offcanvas.Header closeButton>
        <Offcanvas.Title>
          <FontAwesomeIcon icon={faCog} className="me-2" />
          {t('aiAgents.personal.settingsTitle')}
        </Offcanvas.Title>
      </Offcanvas.Header>
      <Offcanvas.Body>
        {/* Language */}
        <Form.Group className="mb-4">
          <Form.Label className="fw-semibold">
            {t('aiAgents.personal.settingsLanguage')}
          </Form.Label>
          <Form.Select
            value={language}
            onChange={e => setLanguage(e.target.value)}
          >
            {LANGUAGE_OPTIONS.map(opt => (
              <option key={opt.value} value={opt.value}>
                {opt.label}
              </option>
            ))}
          </Form.Select>
        </Form.Group>

        {/* Temperature */}
        <Form.Group className="mb-4">
          <Form.Label className="fw-semibold">
            {t('aiAgents.personal.settingsTemperature')}
          </Form.Label>
          <div className="d-flex gap-3">
            {TEMPERATURE_OPTIONS.map(opt => (
              <Form.Check
                key={opt.value}
                type="radio"
                label={t(opt.labelKey)}
                name="temperature"
                checked={temperature === opt.value}
                onChange={() => setTemperature(opt.value)}
              />
            ))}
          </div>
        </Form.Group>

        {/* Skepticism */}
        <Form.Group className="mb-3">
          <Form.Label className="fw-semibold">
            {t('aiAgents.personal.settingsSkepticism')}{' '}
            <Badge bg="secondary">{skepticism}</Badge>
          </Form.Label>
          <Form.Range
            min={1}
            max={5}
            value={skepticism}
            onChange={e => setSkepticism(Number(e.target.value))}
          />
        </Form.Group>

        {/* Literalism */}
        <Form.Group className="mb-3">
          <Form.Label className="fw-semibold">
            {t('aiAgents.personal.settingsLiteralism')}{' '}
            <Badge bg="secondary">{literalism}</Badge>
          </Form.Label>
          <Form.Range
            min={1}
            max={5}
            value={literalism}
            onChange={e => setLiteralism(Number(e.target.value))}
          />
        </Form.Group>

        {/* Empathy */}
        <Form.Group className="mb-4">
          <Form.Label className="fw-semibold">
            {t('aiAgents.personal.settingsEmpathy')}{' '}
            <Badge bg="secondary">{empathy}</Badge>
          </Form.Label>
          <Form.Range
            min={1}
            max={5}
            value={empathy}
            onChange={e => setEmpathy(Number(e.target.value))}
          />
        </Form.Group>

        <Button variant="primary" className="w-100" onClick={handleSave}>
          {t('aiAgents.personal.settingsSave')}
        </Button>
      </Offcanvas.Body>
    </Offcanvas>
  );
}

// ---------------------------------------------------------------------------
// Main component
// ---------------------------------------------------------------------------

const PersonalAgentChat: React.FC = () => {
  const { t } = useTranslation();
  // Auto-provision the personal agent project on mount
  const {
    data: personalProject,
    isLoading: projectLoading,
    isError: projectError
  } = useGetPersonalAgentQuery();

  // State
  const [activeConversationId, setActiveConversationId] = useState<
    string | null
  >(null);
  const [persona, setPersona] = useState<PersonaType>('developer');
  const [inputValue, setInputValue] = useState('');
  const [sidebarOpen, setSidebarOpen] = useState(true);
  const [localMessages, setLocalMessages] = useState<AgentMessage[]>([]);
  const [isWaitingForResponse, setIsWaitingForResponse] = useState(false);
  const [showDocuments, setShowDocuments] = useState(false);
  const [showSettings, setShowSettings] = useState(false);

  // Refs
  const messagesEndRef = useRef<HTMLDivElement>(null);
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  // RTK Query hooks
  const { data: conversationsData } = useListPersonalConversationsQuery(
    { limit: 50 },
    { skip: !personalProject }
  );

  const { data: activeConversation } = useGetPersonalConversationQuery(
    activeConversationId!,
    { skip: !activeConversationId }
  );

  const { data: documentsData } = useListDocumentsQuery(
    { status: 'completed' },
    { skip: !personalProject }
  );

  const [agentQuery] = usePersonalAgentQueryMutation();
  const [deleteConversation, { isLoading: isDeleting }] =
    useDeletePersonalConversationMutation();
  const [addDocuments] = useAddPersonalDocumentsMutation();
  const [removeDocuments] = useRemovePersonalDocumentsMutation();
  const [updateSettings] = useUpdatePersonalSettingsMutation();

  // Derived values
  const conversations = conversationsData?.conversations ?? [];
  const documents = documentsData?.documents ?? [];
  const selectedDocUuids = personalProject?.documentUuids ?? [];
  const currentSettings: AgentSettings = personalProject?.settings ?? {};

  // Pulled into a temp to dodge the prettier-version-drift loop on
  // `a?.b ?? c` inside a ternary tail (see project_precommit_prettier_drift).
  const conversationMessages: AgentMessage[] =
    activeConversation?.messages ?? localMessages;
  const displayedMessages: AgentMessage[] = isWaitingForResponse
    ? localMessages
    : conversationMessages;

  // Auto-scroll to bottom when messages change.
  // Necessary because new messages arrive outside the render cycle (via mutation responses).
  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [displayedMessages.length, isWaitingForResponse]);

  // Sync persona from active conversation
  useEffect(() => {
    if (activeConversation?.persona) {
      const p = activeConversation.persona as PersonaType;
      if (p in PERSONA_LABELS) {
        setPersona(p);
      }
    }
  }, [activeConversation?.persona]);

  // Handlers

  const handleNewConversation = useCallback(() => {
    setActiveConversationId(null);
    setLocalMessages([]);
    setIsWaitingForResponse(false);
  }, []);

  const handleSelectConversation = useCallback((uuid: string) => {
    setActiveConversationId(uuid);
    setLocalMessages([]);
    setIsWaitingForResponse(false);
  }, []);

  const handleDeleteConversation = useCallback(
    async (uuid: string) => {
      try {
        await deleteConversation(uuid).unwrap();
        if (activeConversationId === uuid) {
          setActiveConversationId(null);
          setLocalMessages([]);
        }
      } catch {
        // Error handled by baseApi toast
      }
    },
    [deleteConversation, activeConversationId]
  );

  const handleSend = useCallback(async () => {
    const question = inputValue.trim();
    if (!question || !personalProject || isWaitingForResponse) return;

    const now = new Date().toISOString();

    const userMessage: AgentMessage = {
      role: 'user',
      content: question,
      createdAt: now
    };

    const placeholderAssistant: AgentMessage = {
      role: 'assistant',
      content: '',
      createdAt: now
    };

    // Build optimistic message list
    const previousMessages = activeConversation?.messages ?? localMessages;
    const optimistic = [...previousMessages, userMessage, placeholderAssistant];

    setLocalMessages(optimistic);
    setIsWaitingForResponse(true);
    setInputValue('');

    try {
      const response = await agentQuery({
        question,
        persona,
        conversationId: activeConversationId ?? undefined
      }).unwrap();

      // Replace placeholder with real response
      const assistantMessage: AgentMessage = {
        role: 'assistant',
        content: response.answer,
        sources: response.sources,
        metadata: response.metadata,
        createdAt: new Date().toISOString()
      };

      setLocalMessages([...previousMessages, userMessage, assistantMessage]);

      // If this was the first message, set the conversation id
      if (!activeConversationId && response.conversationId) {
        setActiveConversationId(response.conversationId);
      }
    } catch {
      // Remove the placeholder assistant message on error
      setLocalMessages([...previousMessages, userMessage]);
    } finally {
      setIsWaitingForResponse(false);
      textareaRef.current?.focus();
    }
  }, [
    inputValue,
    personalProject,
    isWaitingForResponse,
    activeConversation?.messages,
    localMessages,
    agentQuery,
    persona,
    activeConversationId
  ]);

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
      if (e.key === 'Enter' && !e.shiftKey) {
        e.preventDefault();
        handleSend();
      }
    },
    [handleSend]
  );

  const handleAddDocuments = useCallback(
    async (uuids: string[]) => {
      try {
        await addDocuments({ documentUuids: uuids }).unwrap();
      } catch {
        // Error handled by baseApi toast
      }
    },
    [addDocuments]
  );

  const handleRemoveDocuments = useCallback(
    async (uuids: string[]) => {
      try {
        await removeDocuments({ documentUuids: uuids }).unwrap();
      } catch {
        // Error handled by baseApi toast
      }
    },
    [removeDocuments]
  );

  const handleSaveSettings = useCallback(
    async (patch: Partial<AgentSettings>) => {
      try {
        await updateSettings(patch).unwrap();
      } catch {
        // Error handled by baseApi toast
      }
    },
    [updateSettings]
  );

  // Loading state
  if (projectLoading) {
    return (
      <div
        className="d-flex justify-content-center align-items-center"
        style={{ height: 'calc(100vh - 200px)' }}
      >
        <div className="text-center">
          <Spinner animation="border" className="mb-3" />
          <p className="text-muted mb-0">{t('aiAgents.personal.loading')}</p>
        </div>
      </div>
    );
  }

  if (projectError) {
    return (
      <div
        className="d-flex justify-content-center align-items-center"
        style={{ height: 'calc(100vh - 200px)' }}
      >
        <div className="text-center text-danger">
          <FontAwesomeIcon icon={faRobot} size="3x" className="mb-3" />
          <p className="mb-0">{t('aiAgents.personal.errorLoad')}</p>
        </div>
      </div>
    );
  }

  return (
    <>
      <Card
        style={{ height: 'calc(100vh - 200px)' }}
        className="overflow-hidden"
      >
        {/* Header */}
        <Card.Header className="bg-body-tertiary d-flex align-items-center justify-content-between py-2 px-3">
          <div className="d-flex align-items-center gap-2">
            <Button
              variant="link"
              size="sm"
              className="p-0 text-secondary"
              onClick={() => setSidebarOpen(!sidebarOpen)}
            >
              <FontAwesomeIcon icon={sidebarOpen ? faChevronLeft : faBars} />
            </Button>
            <FontAwesomeIcon icon={faRobot} className="text-primary" />
            <h6 className="mb-0">{t('aiAgents.personal.header')}</h6>
          </div>
          <div className="d-flex align-items-center gap-2">
            <Dropdown>
              <Dropdown.Toggle variant="orkestra-default" size="sm">
                <FontAwesomeIcon icon={faUser} className="me-1" />
                {t(`aiAgents.chat.personaLabels.${persona}`, {
                  defaultValue: PERSONA_LABELS[persona]
                })}
              </Dropdown.Toggle>
              <Dropdown.Menu>
                {(Object.keys(PERSONA_LABELS) as PersonaType[]).map(key => (
                  <Dropdown.Item
                    key={key}
                    active={key === persona}
                    onClick={() => setPersona(key)}
                  >
                    <span className="fw-semibold">
                      {t(`aiAgents.chat.personaLabels.${key}`, {
                        defaultValue: PERSONA_LABELS[key]
                      })}
                    </span>
                    <br />
                    <small className="text-muted">
                      {t(`aiAgents.chat.personaDescriptions.${key}`, {
                        defaultValue: PERSONA_DESCRIPTIONS[key]
                      })}
                    </small>
                  </Dropdown.Item>
                ))}
              </Dropdown.Menu>
            </Dropdown>
            <Button
              variant="orkestra-default"
              size="sm"
              onClick={() => setShowSettings(true)}
              title={t('aiAgents.personal.buttonSettingsTitle')}
            >
              <FontAwesomeIcon icon={faCog} />
            </Button>
            <Button
              variant="orkestra-default"
              size="sm"
              onClick={() => setShowDocuments(true)}
              title={t('aiAgents.personal.buttonDocumentsTitle')}
            >
              <FontAwesomeIcon icon={faFileAlt} />
            </Button>
            <Button variant="primary" size="sm" onClick={handleNewConversation}>
              <FontAwesomeIcon icon={faPlus} className="me-1" />
              {t('aiAgents.chat.newConversation')}
            </Button>
          </div>
        </Card.Header>

        {/* Body: sidebar + chat area */}
        <Card.Body className="p-0 d-flex overflow-hidden">
          {/* Sidebar */}
          {sidebarOpen && (
            <div
              className="border-end bg-body-tertiary flex-shrink-0"
              style={{ width: 280, minWidth: 280 }}
            >
              <div className="p-2 border-bottom">
                <small className="fw-semibold text-muted text-uppercase">
                  {t('aiAgents.chat.conversationsHeading')}
                </small>
                <Badge bg="secondary" className="ms-2">
                  {conversations.length}
                </Badge>
              </div>
              <ConversationSidebar
                conversations={conversations}
                activeId={activeConversationId}
                onSelect={handleSelectConversation}
                onDelete={handleDeleteConversation}
                isDeleting={isDeleting}
              />
            </div>
          )}

          {/* Main chat area */}
          <div className="flex-1 d-flex flex-column overflow-hidden">
            {/* Messages */}
            <div className="flex-1 overflow-auto p-3">
              {displayedMessages.length === 0 && (
                <div className="text-center text-muted py-5">
                  <FontAwesomeIcon
                    icon={faRobot}
                    size="3x"
                    className="mb-3 text-300"
                  />
                  <p className="mb-1">{t('aiAgents.chat.emptyTitle')}</p>
                  <small>{t('aiAgents.chat.emptySubtitle')}</small>
                </div>
              )}

              {displayedMessages.map((msg, idx) => (
                <MessageBubble
                  key={`${msg.role}-${idx}`}
                  message={msg}
                  isLoading={
                    isWaitingForResponse &&
                    idx === displayedMessages.length - 1 &&
                    msg.role === 'assistant' &&
                    !msg.content
                  }
                />
              ))}
              <div ref={messagesEndRef} />
            </div>

            {/* Input footer */}
            <div className="border-top p-3 bg-body-tertiary">
              <Row className="g-2 align-items-end">
                <Col>
                  <Form.Control
                    as="textarea"
                    ref={textareaRef}
                    rows={1}
                    value={inputValue}
                    onChange={e => setInputValue(e.target.value)}
                    onKeyDown={handleKeyDown}
                    placeholder={t('aiAgents.chat.inputPlaceholder')}
                    disabled={isWaitingForResponse}
                    style={{
                      resize: 'none',
                      maxHeight: 120,
                      overflowY: 'auto'
                    }}
                  />
                </Col>
                <Col xs="auto">
                  <Button
                    variant="primary"
                    onClick={handleSend}
                    disabled={!inputValue.trim() || isWaitingForResponse}
                  >
                    {isWaitingForResponse ? (
                      <Spinner size="sm" animation="border" />
                    ) : (
                      <FontAwesomeIcon icon={faPaperPlane} />
                    )}
                  </Button>
                </Col>
              </Row>
            </div>
          </div>
        </Card.Body>
      </Card>

      {/* Document Picker Offcanvas */}
      <DocumentPicker
        show={showDocuments}
        onHide={() => setShowDocuments(false)}
        documents={documents}
        selectedUuids={selectedDocUuids}
        onAdd={handleAddDocuments}
        onRemove={handleRemoveDocuments}
      />

      {/* Settings Offcanvas */}
      <SettingsOffcanvas
        show={showSettings}
        onHide={() => setShowSettings(false)}
        settings={currentSettings}
        onSave={handleSaveSettings}
      />
    </>
  );
};

export default PersonalAgentChat;
