import { useState, useCallback, useRef, useEffect } from 'react';
import {
  Row,
  Col,
  Card,
  Button,
  Form,
  Spinner,
  Badge,
  Accordion
} from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import { useRAGStream } from '../../../hooks/useRAGStream';
import { useListAIModelsQuery } from '../../../store/api/aiModelsApi';
import type { SourceRef, QueryMeta } from '../../../types/rag';

interface Message {
  role: 'user' | 'assistant';
  content: string;
  sources?: SourceRef[];
  metadata?: QueryMeta;
}

const GraphRAG: React.FC = () => {
  const { t } = useTranslation();
  const [question, setQuestion] = useState('');
  const [isoFilter, setIsoFilter] = useState('');
  const [selectedModel, setSelectedModel] = useState('');
  const [messages, setMessages] = useState<Message[]>([]);

  const { data: modelsData } = useListAIModelsQuery({ type: 'llm' });
  const llmModels = modelsData?.models ?? [];
  const messagesEndRef = useRef<HTMLDivElement>(null);

  const {
    streamQuery,
    isStreaming,
    answer: streamingAnswer,
    sources: streamingSources,
    metadata: streamingMetadata,
    error: streamingError
  } = useRAGStream();

  // Track whether we have an active streaming assistant message
  const [streamingMsgIndex, setStreamingMsgIndex] = useState<number | null>(
    null
  );

  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [messages, streamingAnswer]);

  // Update the streaming assistant message as tokens arrive
  useEffect(() => {
    if (streamingMsgIndex === null) return;

    setMessages(prev => {
      const updated = [...prev];
      if (updated[streamingMsgIndex]?.role === 'assistant') {
        updated[streamingMsgIndex] = {
          ...updated[streamingMsgIndex],
          content: streamingAnswer,
          sources:
            streamingSources.length > 0
              ? streamingSources
              : updated[streamingMsgIndex].sources,
          metadata: streamingMetadata || updated[streamingMsgIndex].metadata
        };
      }
      return updated;
    });
  }, [streamingAnswer, streamingSources, streamingMetadata, streamingMsgIndex]);

  // Handle stream completion or error
  useEffect(() => {
    if (streamingMsgIndex === null) return;
    if (isStreaming) return;

    if (streamingError) {
      setMessages(prev => {
        const updated = [...prev];
        if (updated[streamingMsgIndex]?.role === 'assistant') {
          updated[streamingMsgIndex] = {
            ...updated[streamingMsgIndex],
            content:
              updated[streamingMsgIndex].content ||
              t('graph.rag.chat.errorFallback')
          };
        }
        return updated;
      });
    }
    setStreamingMsgIndex(null);
  }, [isStreaming, streamingError, streamingMsgIndex, t]);

  const handleSubmit = useCallback(async () => {
    if (!question.trim() || isStreaming) return;

    const q = question.trim();
    setQuestion('');

    // Add user message + empty assistant message placeholder
    setMessages(prev => {
      const newMessages = [
        ...prev,
        { role: 'user' as const, content: q },
        { role: 'assistant' as const, content: '' }
      ];
      setStreamingMsgIndex(newMessages.length - 1);
      return newMessages;
    });

    streamQuery({
      question: q,
      isoStandard: isoFilter || undefined,
      modelUuid: selectedModel || undefined
    });
  }, [question, isoFilter, selectedModel, isStreaming, streamQuery]);

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      handleSubmit();
    }
  };

  return (
    <>
      <Row className="g-3 mb-3">
        <Col>
          <div className="d-flex align-items-center justify-content-between">
            <h5 className="mb-0">{t('graph.rag.pageTitle')}</h5>
            <div className="d-flex gap-2 align-items-center">
              <Form.Label className="mb-0 small text-muted">
                {t('graph.rag.toolbar.modelLabel')}
              </Form.Label>
              <Form.Select
                size="sm"
                value={selectedModel}
                onChange={e => setSelectedModel(e.target.value)}
                style={{ width: 200 }}
              >
                <option value="">{t('graph.rag.toolbar.defaultLLM')}</option>
                {llmModels.map(m => (
                  <option key={m.uuid} value={m.uuid}>
                    {m.name}
                    {m.isDefault ? t('graph.rag.toolbar.defaultSuffix') : ''}
                  </option>
                ))}
              </Form.Select>
              <Form.Label className="mb-0 small text-muted">
                {t('graph.rag.toolbar.isoLabel')}
              </Form.Label>
              <Form.Control
                size="sm"
                value={isoFilter}
                onChange={e => setIsoFilter(e.target.value)}
                placeholder={t('graph.rag.toolbar.isoPlaceholder')}
                style={{ width: 100 }}
              />
              <Button
                size="sm"
                variant="outline-secondary"
                onClick={() => {
                  setMessages([]);
                  setStreamingMsgIndex(null);
                }}
              >
                {t('graph.rag.toolbar.clearButton')}
              </Button>
            </div>
          </div>
        </Col>
      </Row>

      <Row className="g-3 mb-3">
        <Col>
          <Card
            style={{
              height: 'calc(100vh - 280px)',
              display: 'flex',
              flexDirection: 'column'
            }}
          >
            {/* Messages */}
            <Card.Body className="flex-grow-1 overflow-auto p-3">
              {messages.length === 0 ? (
                <div className="text-center text-muted py-5">
                  <p className="mb-1">{t('graph.rag.chat.emptyTitle')}</p>
                  <small>{t('graph.rag.chat.emptyExample')}</small>
                </div>
              ) : (
                messages.map((msg, i) => (
                  <div
                    key={i}
                    className={`mb-3 d-flex ${
                      msg.role === 'user'
                        ? 'justify-content-end'
                        : 'justify-content-start'
                    }`}
                  >
                    <div
                      className={`p-3 rounded-3 ${
                        msg.role === 'user'
                          ? 'bg-primary text-white'
                          : 'bg-body-tertiary border'
                      }`}
                      style={{ maxWidth: '80%' }}
                    >
                      {/* Assistant content or streaming indicator */}
                      {msg.role === 'assistant' &&
                      !msg.content &&
                      i === streamingMsgIndex ? (
                        <div>
                          <Spinner size="sm" className="me-2" />
                          {streamingSources.length > 0
                            ? t('graph.rag.chat.generating')
                            : t('graph.rag.chat.searching')}
                        </div>
                      ) : (
                        <div style={{ whiteSpace: 'pre-wrap' }}>
                          {msg.content}
                        </div>
                      )}

                      {/* Streaming cursor */}
                      {i === streamingMsgIndex &&
                        isStreaming &&
                        msg.content && (
                          <span
                            className="d-inline-block bg-dark"
                            style={{
                              width: 2,
                              height: '1em',
                              animation: 'blink 1s infinite',
                              verticalAlign: 'text-bottom'
                            }}
                          />
                        )}

                      {/* Sources */}
                      {msg.sources && msg.sources.length > 0 && (
                        <Accordion className="mt-2" flush>
                          <Accordion.Item eventKey="0">
                            <Accordion.Header>
                              <small>
                                {t(
                                  msg.sources.length === 1
                                    ? 'graph.rag.chat.sourcesOne'
                                    : 'graph.rag.chat.sourcesOther',
                                  { count: msg.sources.length }
                                )}
                              </small>
                            </Accordion.Header>
                            <Accordion.Body className="p-2">
                              {msg.sources.map((src, j) => (
                                <div
                                  key={j}
                                  className="border-bottom pb-2 mb-2 small"
                                >
                                  <div className="d-flex gap-1 mb-1 flex-wrap">
                                    <Badge bg="primary">
                                      {src.documentTitle}
                                    </Badge>
                                    {src.isoStandard && (
                                      <Badge bg="info">{src.isoStandard}</Badge>
                                    )}
                                    {src.fullPath && (
                                      <Badge bg="secondary">
                                        {src.fullPath}
                                      </Badge>
                                    )}
                                    {src.requirementLevel && (
                                      <Badge bg="warning" text="dark">
                                        {src.requirementLevel}
                                      </Badge>
                                    )}
                                    <Badge bg="success">
                                      {t('graph.rag.chat.scorePercent', {
                                        percent: (src.score * 100).toFixed(0)
                                      })}
                                    </Badge>
                                  </div>
                                  <div
                                    className="text-muted"
                                    style={{
                                      maxHeight: 80,
                                      overflow: 'hidden',
                                      textOverflow: 'ellipsis'
                                    }}
                                  >
                                    {src.chunkText}
                                  </div>
                                </div>
                              ))}
                            </Accordion.Body>
                          </Accordion.Item>
                        </Accordion>
                      )}

                      {/* Timing metadata */}
                      {msg.metadata && (
                        <div className="mt-2 d-flex gap-2 flex-wrap">
                          <small className="text-muted">
                            {t('graph.rag.metadata.embed', {
                              ms: msg.metadata.embeddingTimeMs
                            })}
                          </small>
                          <small className="text-muted">
                            {t('graph.rag.metadata.search', {
                              ms: msg.metadata.searchTimeMs
                            })}
                          </small>
                          <small className="text-muted">
                            {t('graph.rag.metadata.llm', {
                              ms: msg.metadata.llmTimeMs
                            })}
                          </small>
                          <small className="text-muted">
                            {t('graph.rag.metadata.total', {
                              ms: msg.metadata.totalTimeMs
                            })}
                          </small>
                          <small className="text-muted">
                            {t('graph.rag.metadata.chunks', {
                              count: msg.metadata.chunksRetrieved
                            })}
                          </small>
                        </div>
                      )}
                    </div>
                  </div>
                ))
              )}
              <div ref={messagesEndRef} />
            </Card.Body>

            {/* Input */}
            <Card.Footer className="p-2">
              <div className="d-flex gap-2">
                <Form.Control
                  as="textarea"
                  rows={1}
                  size="sm"
                  value={question}
                  onChange={e => setQuestion(e.target.value)}
                  onKeyDown={handleKeyDown}
                  placeholder={t('graph.rag.chat.inputPlaceholder')}
                  disabled={isStreaming}
                  style={{ resize: 'none' }}
                />
                <Button
                  variant="primary"
                  size="sm"
                  onClick={handleSubmit}
                  disabled={isStreaming || !question.trim()}
                  style={{ whiteSpace: 'nowrap' }}
                >
                  {isStreaming ? (
                    <Spinner size="sm" />
                  ) : (
                    t('graph.rag.chat.send')
                  )}
                </Button>
              </div>
            </Card.Footer>
          </Card>
        </Col>
      </Row>

      {/* Blinking cursor animation */}
      <style>{`
        @keyframes blink {
          0%, 50% { opacity: 1; }
          51%, 100% { opacity: 0; }
        }
      `}</style>
    </>
  );
};

export default GraphRAG;
