import { Alert, Button } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';

// RetrievalLoadError is the retryable error state of an /admin/retrieval tab
// (precedent: pages/admin/llm/LlmLoadError.tsx).
const RetrievalLoadError = ({ onRetry }: { onRetry: () => void }) => {
  const { t } = useTranslation();
  return (
    <Alert
      variant="danger"
      className="d-flex flex-wrap align-items-center justify-content-between gap-2 fs-10 mb-0"
    >
      <span>{t('adminRetrieval.loadError')}</span>
      <Button variant="orkestra-default" size="sm" onClick={onRetry}>
        {t('adminRetrieval.actions.retry')}
      </Button>
    </Alert>
  );
};

export default RetrievalLoadError;
