import { useState, useCallback } from 'react';
import { Modal, Form, Button, Spinner, Alert } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import { useQuickPromptAIModelMutation } from '../../../store/api/aiModelsApi';
import type { AIModelConfig } from '../../../types/aiModels';

interface QuickPromptModalProps {
  model: AIModelConfig | null;
  onHide: () => void;
}

const QuickPromptModal: React.FC<QuickPromptModalProps> = ({
  model,
  onHide
}) => {
  const { t } = useTranslation();
  const [prompt, setPrompt] = useState('');
  const [response, setResponse] = useState('');
  const [timeMs, setTimeMs] = useState<number | null>(null);
  const [error, setError] = useState('');
  const [sendPrompt, { isLoading }] = useQuickPromptAIModelMutation();

  const handleEnter = () => {
    setPrompt('');
    setResponse('');
    setTimeMs(null);
    setError('');
  };

  const handleSend = useCallback(async () => {
    if (!model || !prompt.trim()) return;
    setError('');
    setResponse('');
    setTimeMs(null);
    try {
      const result = await sendPrompt({
        uuid: model.uuid,
        prompt: prompt.trim()
      }).unwrap();
      setResponse(result.response);
      setTimeMs(result.timeMs);
    } catch {
      setError(t('aiModels.quickPrompt.error'));
    }
  }, [sendPrompt, model, prompt, t]);

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && e.ctrlKey) {
      handleSend();
    }
  };

  return (
    <Modal show={!!model} onHide={onHide} onEnter={handleEnter} size="lg">
      <Modal.Header closeButton>
        <Modal.Title className="fs-6">
          {t('aiModels.quickPrompt.title', { name: model?.name ?? '' })}
          <span className="text-muted ms-2 small">
            {t('aiModels.quickPrompt.modelNameSuffix', {
              modelName: model?.modelName ?? ''
            })}
          </span>
        </Modal.Title>
      </Modal.Header>
      <Modal.Body>
        <Form.Group className="mb-3">
          <Form.Label className="small">
            {t('aiModels.quickPrompt.promptLabel')}
          </Form.Label>
          <Form.Control
            as="textarea"
            rows={3}
            size="sm"
            value={prompt}
            onChange={e => setPrompt(e.target.value)}
            onKeyDown={handleKeyDown}
            placeholder={t('aiModels.quickPrompt.promptPlaceholder')}
            disabled={isLoading}
          />
        </Form.Group>

        <div className="d-flex justify-content-between align-items-center mb-3">
          <Button
            size="sm"
            variant="primary"
            onClick={handleSend}
            disabled={isLoading || !prompt.trim()}
          >
            {isLoading ? (
              <>
                <Spinner size="sm" className="me-1" />{' '}
                {t('aiModels.quickPrompt.sending')}
              </>
            ) : (
              t('aiModels.quickPrompt.send')
            )}
          </Button>
          {timeMs !== null && (
            <span className="text-muted small">
              {t('aiModels.quickPrompt.latencyMs', { ms: timeMs })}
            </span>
          )}
        </div>

        {error && (
          <Alert variant="danger" className="small">
            {error}
          </Alert>
        )}

        {response && (
          <div className="border rounded p-2 bg-body-tertiary">
            <pre
              className="mb-0 small"
              style={{ whiteSpace: 'pre-wrap', wordBreak: 'break-word' }}
            >
              {response}
            </pre>
          </div>
        )}
      </Modal.Body>
      <Modal.Footer>
        <Button variant="secondary" size="sm" onClick={onHide}>
          {t('aiModels.quickPrompt.close')}
        </Button>
      </Modal.Footer>
    </Modal>
  );
};

export default QuickPromptModal;
