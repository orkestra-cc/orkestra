import { useState, useCallback, useRef, useEffect } from 'react';
import { Card, Button, Form, Collapse, Dropdown, Badge } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';

const HISTORY_KEY = 'orkestra:cypher-history';
const MAX_HISTORY = 20;

interface CypherEditorProps {
  onExecute: (cypher: string, params?: Record<string, unknown>) => void;
  isLoading?: boolean;
  readOnly?: boolean;
  onReadOnlyChange?: (readOnly: boolean) => void;
  defaultValue?: string;
  externalQuery?: string;
}

function loadHistory(): string[] {
  try {
    const raw = localStorage.getItem(HISTORY_KEY);
    if (!raw) return [];
    const parsed = JSON.parse(raw);
    return Array.isArray(parsed) ? parsed : [];
  } catch {
    return [];
  }
}

function saveHistory(history: string[]) {
  localStorage.setItem(HISTORY_KEY, JSON.stringify(history));
}

function pushToHistory(query: string) {
  const trimmed = query.trim();
  if (!trimmed) return;
  const history = loadHistory().filter(q => q !== trimmed);
  history.unshift(trimmed);
  saveHistory(history.slice(0, MAX_HISTORY));
}

const CypherEditor = ({
  onExecute,
  isLoading = false,
  readOnly = false,
  onReadOnlyChange,
  defaultValue = '',
  externalQuery
}: CypherEditorProps) => {
  const { t } = useTranslation();
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const [query, setQuery] = useState(defaultValue);
  const [showParams, setShowParams] = useState(false);
  const [paramsText, setParamsText] = useState('{}');
  const [paramsError, setParamsError] = useState('');
  const [history, setHistory] = useState<string[]>(loadHistory);

  const handleExecute = useCallback(() => {
    const cypher = query.trim();
    if (!cypher || isLoading) return;

    let params: Record<string, unknown> | undefined;
    if (showParams && paramsText.trim() !== '{}' && paramsText.trim() !== '') {
      try {
        params = JSON.parse(paramsText);
        setParamsError('');
      } catch {
        setParamsError(t('graph.cypher.errorInvalidJson'));
        return;
      }
    }

    pushToHistory(cypher);
    setHistory(loadHistory());
    onExecute(cypher, params);
  }, [query, isLoading, showParams, paramsText, onExecute, t]);

  const handleClear = useCallback(() => {
    setQuery('');
    textareaRef.current?.focus();
  }, []);

  const handleLoadFromHistory = useCallback((q: string) => {
    setQuery(q);
    textareaRef.current?.focus();
  }, []);

  // Ctrl+Enter / Cmd+Enter to execute
  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') {
        e.preventDefault();
        handleExecute();
      }
    },
    [handleExecute]
  );

  // Sync textarea when a query is triggered externally (e.g., sidebar click)
  useEffect(() => {
    if (externalQuery !== undefined && externalQuery !== '') {
      setQuery(externalQuery);
    }
  }, [externalQuery]);

  // Auto-resize textarea based on content
  useEffect(() => {
    const el = textareaRef.current;
    if (el) {
      el.style.height = 'auto';
      const newHeight = Math.max(120, Math.min(el.scrollHeight, 300));
      el.style.height = `${newHeight}px`;
    }
  }, [query]);

  return (
    <Card className="mb-0">
      <Card.Header className="bg-body-tertiary py-2 d-flex align-items-center justify-content-between">
        <h6 className="mb-0">{t('graph.cypher.cardTitle')}</h6>
        <span className="text-muted fs-10 d-none d-md-inline">
          {t('graph.cypher.ctrlEnterHint')}
        </span>
      </Card.Header>
      <Card.Body className="p-3">
        {/* Query Textarea */}
        <Form.Control
          ref={textareaRef}
          as="textarea"
          value={query}
          onChange={e => setQuery(e.target.value)}
          onKeyDown={handleKeyDown}
          className="font-monospace"
          style={{
            fontSize: '0.875rem',
            lineHeight: 1.6,
            minHeight: 120,
            maxHeight: 300,
            resize: 'none',
            backgroundColor: '#0d1117',
            color: '#e6edf3',
            border: '1px solid #30363d'
          }}
          placeholder={t('graph.cypher.queryPlaceholder')}
          spellCheck={false}
        />

        {/* Toolbar */}
        <div className="d-flex align-items-center gap-2 mt-2 flex-wrap">
          <Button
            variant="primary"
            size="sm"
            onClick={handleExecute}
            disabled={isLoading || !query.trim()}
          >
            {isLoading ? (
              <>
                <span
                  className="spinner-border spinner-border-sm me-1"
                  role="status"
                />
                {t('graph.cypher.running')}
              </>
            ) : (
              <>
                {'\u25B6'} {t('graph.cypher.execute')}
              </>
            )}
          </Button>

          <Form.Check
            type="switch"
            id="cypher-readonly-toggle"
            label={t('graph.cypher.readOnly')}
            className="mb-0 ms-1"
            checked={readOnly}
            onChange={e => onReadOnlyChange?.(e.target.checked)}
          />

          <Button variant="outline-secondary" size="sm" onClick={handleClear}>
            {t('graph.cypher.clear')}
          </Button>

          <Button
            variant="outline-secondary"
            size="sm"
            onClick={() => setShowParams(p => !p)}
            className="d-flex align-items-center gap-1"
          >
            {t('graph.cypher.parameters')}
            {showParams &&
              paramsText.trim() !== '{}' &&
              paramsText.trim() !== '' && (
                <Badge bg="info" pill className="ms-1">
                  {t('graph.cypher.paramsSetBadge')}
                </Badge>
              )}
          </Button>

          {history.length > 0 && (
            <Dropdown>
              <Dropdown.Toggle
                variant="outline-secondary"
                size="sm"
                id="cypher-history-dropdown"
              >
                {t('graph.cypher.history')}
                <Badge bg="secondary" pill className="ms-1">
                  {history.length}
                </Badge>
              </Dropdown.Toggle>
              <Dropdown.Menu
                style={{ maxHeight: 300, overflowY: 'auto', minWidth: 350 }}
              >
                {history.map((q, idx) => (
                  <Dropdown.Item
                    key={idx}
                    onClick={() => handleLoadFromHistory(q)}
                    className="font-monospace"
                    style={{ fontSize: '0.75rem', whiteSpace: 'pre-wrap' }}
                  >
                    {q.length > 80 ? q.slice(0, 80) + '...' : q}
                  </Dropdown.Item>
                ))}
              </Dropdown.Menu>
            </Dropdown>
          )}
        </div>

        {/* Collapsible Parameters Section */}
        <Collapse in={showParams}>
          <div>
            <div className="mt-2">
              <Form.Label className="fs-10 mb-1">
                {t('graph.cypher.paramsLabel')}
              </Form.Label>
              <Form.Control
                as="textarea"
                rows={3}
                value={paramsText}
                onChange={e => {
                  setParamsText(e.target.value);
                  setParamsError('');
                }}
                className="font-monospace"
                style={{ fontSize: '0.8rem' }}
                placeholder={t('graph.cypher.paramsPlaceholder')}
              />
              {paramsError && (
                <Form.Text className="text-danger">{paramsError}</Form.Text>
              )}
            </div>
          </div>
        </Collapse>
      </Card.Body>
    </Card>
  );
};

export default CypherEditor;
