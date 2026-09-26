import { useTranslation } from 'react-i18next';
import { Alert, Button } from 'react-bootstrap';

export interface BulkRowOutcome {
  id: string;
  ok: boolean;
  status?: string;
  error_code?: string;
}

interface Props {
  results: BulkRowOutcome[];
  onRetryFailed: (ids: string[]) => void;
  onDismiss: () => void;
  /**
   * Translates a backend row `error_code` into operator copy. Each host
   * owns its own map in its own i18n namespace (`forms.*` codes in forms,
   * `crm.*` codes in crm — see their `bulkErrors.ts`). Return `undefined`
   * for a code you do not know: the generic core fallback is used, so an
   * unmapped or missing code never surfaces a raw string — the operator
   * always reads a translated reason.
   */
  errorLabel: (code: string | undefined) => string | undefined;
}

/**
 * PERSISTENT summary of a bulk action. Deliberately not a toast: a toast
 * fades on its own, and the operator who just moved 40 rows must be able to
 * read at leisure which ones did not go through and why.
 */
const BulkResultSummary = ({
  results,
  onRetryFailed,
  onDismiss,
  errorLabel
}: Props) => {
  const { t } = useTranslation();
  const failed = results.filter(r => !r.ok);
  const okCount = results.length - failed.length;
  if (results.length === 0) return null;

  const errorMessage = (code?: string) =>
    errorLabel(code) ?? t('bulk.errors.generic');

  return (
    <Alert
      variant={failed.length === 0 ? 'success' : 'warning'}
      role="status"
      dismissible
      onClose={onDismiss}
      className="mb-3"
    >
      <div className="fw-semibold mb-1">
        {t('bulk.summary', { ok: okCount, failed: failed.length })}
      </div>
      {failed.length > 0 && (
        <>
          <ul className="mb-2 ps-3">
            {failed.map(row => (
              <li key={row.id}>
                <code>{row.id}</code> — {errorMessage(row.error_code)}
              </li>
            ))}
          </ul>
          <Button
            size="sm"
            variant="outline-secondary"
            onClick={() => onRetryFailed(failed.map(r => r.id))}
          >
            {t('bulk.retryFailed')}
          </Button>
        </>
      )}
    </Alert>
  );
};

export default BulkResultSummary;
