import { useState, useCallback, useRef, useEffect } from 'react';
import { useParams } from 'react-router-dom';
import Markdown from 'react-markdown';
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
} from '@fortawesome/free-solid-svg-icons';
import classNames from 'classnames';
import dayjs from 'dayjs';
import relativeTime from 'dayjs/plugin/relativeTime';

import {
  useGetProjectQuery,
  useAgentQueryMutation,
  useListConversationsQuery,
  useGetConversationQuery,
  useCreateConversationMutation,
  useDeleteConversationMutation,
} from '../../../store/api/agentsApi';
import type {
  AgentMessage,
  AgentSource,
  PersonaType,
} from '../../../types/agents';
import { PERSONA_LABELS, PERSONA_DESCRIPTIONS } from '../../../types/agents';

dayjs.extend(relativeTime);

// ---------------------------------------------------------------------------
// Sub-components
// ---------------------------------------------------------------------------

interface MessageBubbleProps {
  message: AgentMessage;
  isLoading?: boolean;
}

function MessageBubble({ message, isLoading }: MessageBubbleProps) {
  const isUser = message.role === 'user';

  return (
    <div
      className={classNames('d-flex mb-3', {
        'justify-content-end': isUser,
        'justify-content-start': !isUser,
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
            {isUser ? 'You' : 'Assistant'}
            {message.createdAt && (
              <span className="ms-2">{dayjs(message.createdAt).fromNow()}</span>
            )}
          </small>
          {isUser && (
            <FontAwesomeIcon
              icon={faUser}
              className="text-primary"
              size="sm"
            />
          )}
        </div>

        <div
          className={classNames('p-3 rounded-3', {
            'bg-primary text-white': isUser,
            'bg-200': !isUser,
          })}
        >
          {isLoading ? (
            <div className="d-flex align-items-center gap-2">
              <Spinner size="sm" animation="border" />
              <span className="text-muted">Thinking...</span>
            </div>
          ) : (
            isUser ? (
              <p className="mb-0 white-space-pre-line">{message.content}</p>
            ) : (
              <div className="mb-0 agent-markdown">
                <Markdown>{message.content}</Markdown>
              </div>
            )
          )}
        </div>

        {/* Metadata badge */}
        {!isUser && message.metadata?.totalTimeMs && (
          <div className="mt-1 d-flex gap-3 flex-wrap">
            <small className="text-muted">
              <FontAwesomeIcon icon={faClock} className="me-1" size="xs" />
              {(message.metadata.totalTimeMs / 1000).toFixed(1)}s
            </small>
            {message.metadata.totalTokens ? (
              <small className="text-muted">
                {message.metadata.totalTokens} tokens
                <span className="ms-1 text-muted-50">
                  ({message.metadata.inputTokens} in / {message.metadata.outputTokens} out)
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
                  {message.sources.length} source
                  {message.sources.length > 1 ? 's' : ''}
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
                        bg={source.score >= 0.8 ? 'success' : source.score >= 0.5 ? 'warning' : 'secondary'}
                        className="ms-2"
                      >
                        {(source.score * 100).toFixed(0)}%
                      </Badge>
                    </div>
                    <small className="text-muted d-block mt-1 font-monospace" style={{ fontSize: '0.75rem' }}>
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
  conversations: { uuid: string; title?: string; persona: string; updatedAt: string }[];
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
  isDeleting,
}: ConversationSidebarProps) {
  return (
    <ListGroup variant="flush" className="overflow-auto" style={{ maxHeight: '100%' }}>
      {conversations.length === 0 && (
        <div className="text-center text-muted py-4">
          <FontAwesomeIcon icon={faComments} className="mb-2" size="2x" />
          <p className="small mb-0">No conversations yet</p>
        </div>
      )}
      {conversations.map((conv) => (
        <ListGroup.Item
          key={conv.uuid}
          action
          active={conv.uuid === activeId}
          onClick={() => onSelect(conv.uuid)}
          className="d-flex justify-content-between align-items-start py-2 px-3"
        >
          <div className="text-truncate me-2">
            <div className="fw-semibold small text-truncate">
              {conv.title || 'Untitled conversation'}
            </div>
            <small className={classNames({ 'text-white-50': conv.uuid === activeId, 'text-muted': conv.uuid !== activeId })}>
              {PERSONA_LABELS[conv.persona as PersonaType] ?? conv.persona}
              {' \u00b7 '}
              {dayjs(conv.updatedAt).fromNow()}
            </small>
          </div>
          <Button
            variant="link"
            size="sm"
            className={classNames('p-0 flex-shrink-0', {
              'text-white-50': conv.uuid === activeId,
              'text-danger': conv.uuid !== activeId,
            })}
            onClick={(e) => {
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
// Main component
// ---------------------------------------------------------------------------

const AgentChat: React.FC = () => {
  const { uuid: projectUuid } = useParams<{ uuid: string }>();

  // State
  const [activeConversationId, setActiveConversationId] = useState<string | null>(null);
  const [persona, setPersona] = useState<PersonaType>('developer');
  const [inputValue, setInputValue] = useState('');
  const [sidebarOpen, setSidebarOpen] = useState(true);
  const [localMessages, setLocalMessages] = useState<AgentMessage[]>([]);
  const [isWaitingForResponse, setIsWaitingForResponse] = useState(false);

  // Refs
  const messagesEndRef = useRef<HTMLDivElement>(null);
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  // RTK Query hooks
  const { data: project, isLoading: projectLoading } = useGetProjectQuery(projectUuid!, {
    skip: !projectUuid,
  });

  const { data: conversationsData } = useListConversationsQuery(
    { projectUuid: projectUuid!, limit: 50 },
    { skip: !projectUuid },
  );

  const { data: activeConversation } = useGetConversationQuery(activeConversationId!, {
    skip: !activeConversationId,
  });

  const [agentQuery] = useAgentQueryMutation();
  const [createConversation] = useCreateConversationMutation();
  const [deleteConversation, { isLoading: isDeleting }] = useDeleteConversationMutation();

  // Derive the displayed messages from active conversation or local state
  const displayedMessages: AgentMessage[] = isWaitingForResponse
    ? localMessages
    : activeConversation?.messages ?? localMessages;

  // Auto-scroll to bottom when messages change
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

  const handleNewConversation = useCallback(async () => {
    if (!projectUuid) return;
    try {
      const conv = await createConversation({ projectUuid, persona }).unwrap();
      setActiveConversationId(conv.uuid);
      setLocalMessages([]);
      setIsWaitingForResponse(false);
    } catch {
      // Error handled by baseApi toast
    }
  }, [projectUuid, persona, createConversation]);

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
    [deleteConversation, activeConversationId],
  );

  const handleSend = useCallback(async () => {
    const question = inputValue.trim();
    if (!question || !projectUuid || isWaitingForResponse) return;

    const now = new Date().toISOString();

    const userMessage: AgentMessage = {
      role: 'user',
      content: question,
      createdAt: now,
    };

    const placeholderAssistant: AgentMessage = {
      role: 'assistant',
      content: '',
      createdAt: now,
    };

    // Build optimistic message list
    const previousMessages = activeConversation?.messages ?? localMessages;
    const optimistic = [...previousMessages, userMessage, placeholderAssistant];

    setLocalMessages(optimistic);
    setIsWaitingForResponse(true);
    setInputValue('');

    try {
      const response = await agentQuery({
        projectUuid,
        body: {
          question,
          persona,
          conversationId: activeConversationId ?? undefined,
        },
      }).unwrap();

      // Update the placeholder with the real response
      const assistantMessage: AgentMessage = {
        role: 'assistant',
        content: response.answer,
        sources: response.sources,
        metadata: response.metadata,
        createdAt: new Date().toISOString(),
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
    projectUuid,
    isWaitingForResponse,
    activeConversation?.messages,
    localMessages,
    agentQuery,
    persona,
    activeConversationId,
  ]);

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
      if (e.key === 'Enter' && !e.shiftKey) {
        e.preventDefault();
        handleSend();
      }
    },
    [handleSend],
  );

  // Loading state
  if (projectLoading) {
    return (
      <div className="d-flex justify-content-center align-items-center" style={{ height: 'calc(100vh - 200px)' }}>
        <Spinner animation="border" />
      </div>
    );
  }

  const conversations = conversationsData?.conversations ?? [];

  return (
    <Card style={{ height: 'calc(100vh - 200px)' }} className="overflow-hidden">
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
          <h6 className="mb-0">{project?.name ?? 'Agent Chat'}</h6>
        </div>
        <div className="d-flex align-items-center gap-2">
          <Dropdown>
            <Dropdown.Toggle variant="falcon-default" size="sm">
              <FontAwesomeIcon icon={faUser} className="me-1" />
              {PERSONA_LABELS[persona]}
            </Dropdown.Toggle>
            <Dropdown.Menu>
              {(Object.keys(PERSONA_LABELS) as PersonaType[]).map((key) => (
                <Dropdown.Item
                  key={key}
                  active={key === persona}
                  onClick={() => setPersona(key)}
                >
                  <span className="fw-semibold">{PERSONA_LABELS[key]}</span>
                  <br />
                  <small className="text-muted">{PERSONA_DESCRIPTIONS[key]}</small>
                </Dropdown.Item>
              ))}
            </Dropdown.Menu>
          </Dropdown>
          <Button variant="primary" size="sm" onClick={handleNewConversation}>
            <FontAwesomeIcon icon={faPlus} className="me-1" />
            New Conversation
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
                Conversations
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
                <FontAwesomeIcon icon={faRobot} size="3x" className="mb-3 text-300" />
                <p className="mb-1">No messages yet</p>
                <small>Ask a question to get started</small>
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
                  onChange={(e) => setInputValue(e.target.value)}
                  onKeyDown={handleKeyDown}
                  placeholder="Type a message... (Shift+Enter for newline)"
                  disabled={isWaitingForResponse}
                  style={{ resize: 'none', maxHeight: 120, overflowY: 'auto' }}
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
  );
};

export default AgentChat;
