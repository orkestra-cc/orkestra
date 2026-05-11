import { useState, useCallback, useRef, useEffect } from 'react';
import { Row, Col, Card, Button, Form, Spinner, Badge, Accordion } from 'react-bootstrap';
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
    error: streamingError,
  } = useRAGStream();

  // Track whether we have an active streaming assistant message
  const [streamingMsgIndex, setStreamingMsgIndex] = useState<number | null>(null);

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
          sources: streamingSources.length > 0 ? streamingSources : updated[streamingMsgIndex].sources,
          metadata: streamingMetadata || updated[streamingMsgIndex].metadata,
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
            content: updated[streamingMsgIndex].content || 'An error occurred while processing your question. Please try again.',
          };
        }
        return updated;
      });
    }
    setStreamingMsgIndex(null);
  }, [isStreaming, streamingError, streamingMsgIndex]);

  const handleSubmit = useCallback(async () => {
    if (!question.trim() || isStreaming) return;

    const q = question.trim();
    setQuestion('');

    // Add user message + empty assistant message placeholder
    setMessages(prev => {
      const newMessages = [
        ...prev,
        { role: 'user' as const, content: q },
        { role: 'assistant' as const, content: '' },
      ];
      setStreamingMsgIndex(newMessages.length - 1);
      return newMessages;
    });

    streamQuery({
      question: q,
      isoStandard: isoFilter || undefined,
      modelUuid: selectedModel || undefined,
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
            <h5 className="mb-0">RAG Query</h5>
            <div className="d-flex gap-2 align-items-center">
              <Form.Label className="mb-0 small text-muted">Model:</Form.Label>
              <Form.Select
                size="sm"
                value={selectedModel}
                onChange={e => setSelectedModel(e.target.value)}
                style={{ width: 200 }}
              >
                <option value="">Default LLM</option>
                {llmModels.map(m => (
                  <option key={m.uuid} value={m.uuid}>
                    {m.name}{m.isDefault ? ' (default)' : ''}
                  </option>
                ))}
              </Form.Select>
              <Form.Label className="mb-0 small text-muted">ISO:</Form.Label>
              <Form.Control
                size="sm"
                value={isoFilter}
                onChange={e => setIsoFilter(e.target.value)}
                placeholder="All"
                style={{ width: 100 }}
              />
              <Button size="sm" variant="outline-secondary" onClick={() => { setMessages([]); setStreamingMsgIndex(null); }}>
                Clear
              </Button>
            </div>
          </div>
        </Col>
      </Row>

      <Row className="g-3 mb-3">
        <Col>
          <Card style={{ height: 'calc(100vh - 280px)', display: 'flex', flexDirection: 'column' }}>
            {/* Messages */}
            <Card.Body className="flex-grow-1 overflow-auto p-3">
              {messages.length === 0 ? (
                <div className="text-center text-muted py-5">
                  <p className="mb-1">Ask questions about your ISO norms and documents.</p>
                  <small>Example: "What are the requirements for risk assessment in ISO 27001?"</small>
                </div>
              ) : (
                messages.map((msg, i) => (
                  <div key={i} className={`mb-3 d-flex ${msg.role === 'user' ? 'justify-content-end' : 'justify-content-start'}`}>
                    <div
                      className={`p-3 rounded-3 ${msg.role === 'user' ? 'bg-primary text-white' : 'bg-body-tertiary border'}`}
                      style={{ maxWidth: '80%' }}
                    >
                      {/* Assistant content or streaming indicator */}
                      {msg.role === 'assistant' && !msg.content && i === streamingMsgIndex ? (
                        <div>
                          <Spinner size="sm" className="me-2" />
                          {streamingSources.length > 0 ? 'Generating answer...' : 'Searching...'}
                        </div>
                      ) : (
                        <div style={{ whiteSpace: 'pre-wrap' }}>{msg.content}</div>
                      )}

                      {/* Streaming cursor */}
                      {i === streamingMsgIndex && isStreaming && msg.content && (
                        <span className="d-inline-block bg-dark" style={{ width: 2, height: '1em', animation: 'blink 1s infinite', verticalAlign: 'text-bottom' }} />
                      )}

                      {/* Sources */}
                      {msg.sources && msg.sources.length > 0 && (
                        <Accordion className="mt-2" flush>
                          <Accordion.Item eventKey="0">
                            <Accordion.Header>
                              <small>{msg.sources.length} source{msg.sources.length > 1 ? 's' : ''}</small>
                            </Accordion.Header>
                            <Accordion.Body className="p-2">
                              {msg.sources.map((src, j) => (
                                <div key={j} className="border-bottom pb-2 mb-2 small">
                                  <div className="d-flex gap-1 mb-1 flex-wrap">
                                    <Badge bg="primary">{src.documentTitle}</Badge>
                                    {src.isoStandard && <Badge bg="info">{src.isoStandard}</Badge>}
                                    {src.fullPath && <Badge bg="secondary">{src.fullPath}</Badge>}
                                    {src.requirementLevel && <Badge bg="warning" text="dark">{src.requirementLevel}</Badge>}
                                    <Badge bg="success">{(src.score * 100).toFixed(0)}%</Badge>
                                  </div>
                                  <div className="text-muted" style={{ maxHeight: 80, overflow: 'hidden', textOverflow: 'ellipsis' }}>
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
                          <small className="text-muted">embed: {msg.metadata.embeddingTimeMs}ms</small>
                          <small className="text-muted">search: {msg.metadata.searchTimeMs}ms</small>
                          <small className="text-muted">llm: {msg.metadata.llmTimeMs}ms</small>
                          <small className="text-muted">total: {msg.metadata.totalTimeMs}ms</small>
                          <small className="text-muted">chunks: {msg.metadata.chunksRetrieved}</small>
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
                  placeholder="Ask a question about your ISO documents..."
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
                  {isStreaming ? <Spinner size="sm" /> : 'Send'}
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
