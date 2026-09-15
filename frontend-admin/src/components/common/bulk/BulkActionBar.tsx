import type { ReactNode } from 'react';
import { Button } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';

interface BulkActionBarProps {
  /** Number of rows currently selected on the page. */
  count: number;
  /**
   * Rows that fell out of the selection because they left the current
   * page between fetches (`useInboxSelection`'s `droppedCount`) — a bulk
   * action never silently loses track of what it was about to do.
   */
  droppedCount: number;
  /** Action buttons for the host list (e.g. Approve/Reject) — built and
   * wired by the caller, which knows its own labels and permissions. */
  actions: ReactNode;
  onAcknowledgeDropped: () => void;
}

/**
 * Selection action bar for a polled, server-paginated queue — purely
 * presentational, no network call lives here. Shows how many rows are
 * selected, the caller's action buttons, and — when `droppedCount > 0` —
 * the notice that some selected rows left the page. Shared by the forms
 * registrations queue (`pages/forms/inbox`) and the crm contact-intake
 * queue (`pages/crm/reviews`); pair it with `useInboxSelection`.
 */
const BulkActionBar = ({
  count,
  droppedCount,
  actions,
  onAcknowledgeDropped
}: BulkActionBarProps) => {
  const { t } = useTranslation();

  if (count === 0 && droppedCount === 0) return null;

  return (
    <div className="d-flex flex-column flex-md-row align-items-md-center justify-content-between gap-2 border rounded p-2 mb-3 bg-body-tertiary">
      <div className="d-flex align-items-center gap-3">
        <span className="fw-semibold text-900">
          {t('bulk.selection.count', { count })}
        </span>
        {count > 0 && <div className="d-flex gap-2">{actions}</div>}
      </div>
      {droppedCount > 0 && (
        <div className="d-flex align-items-center gap-2">
          <span className="text-warning-emphasis">
            {t('bulk.selection.dropped', { count: droppedCount })}
          </span>
          <Button
            size="sm"
            variant="outline-secondary"
            onClick={onAcknowledgeDropped}
          >
            {t('bulk.selection.acknowledgeDropped')}
          </Button>
        </div>
      )}
    </div>
  );
};

export default BulkActionBar;
