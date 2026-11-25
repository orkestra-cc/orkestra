import { Alert, Button } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';

// LlmLoadError is the retryable error state of an /admin/llm list.
const LlmLoadError = ({ onRetry }: { onRetry: () => void }) => {
  const { t } = useTranslation();
  return (
    <Alert
      variant="danger"
      className="d-flex flex-wrap align-items-center justify-content-between gap-2 fs-10 mb-0"
    >
      <span>{t('adminLlm.loadError')}</span>
      <Button variant="orkestra-default" size="sm" onClick={onRetry}>
        {t('adminLlm.actions.retry')}
      </Button>
    </Alert>
  );
};

export default LlmLoadError;
