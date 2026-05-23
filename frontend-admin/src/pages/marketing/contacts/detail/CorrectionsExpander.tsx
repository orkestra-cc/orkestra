// Phase 4 PR-4 — inline expander that renders the chain of
// corrected_by entries pointing at one original activity, fetched
// from GET /v1/marketing/activities/{id}/corrections.
//
// Mounted conditionally inside the Timeline row's collapsible region
// — the parent Timeline tab decides when to render; this component
// owns the fetch + rendering.

import { useTranslation } from 'react-i18next';
import { useListActivityCorrectionsQuery } from 'store/api/marketingApi';

interface CorrectionsExpanderProps {
  activityId: string;
}

const CorrectionsExpander: React.FC<CorrectionsExpanderProps> = ({
  activityId
}) => {
  const { t } = useTranslation();
  const { data, isLoading } = useListActivityCorrectionsQuery(activityId);

  if (isLoading) {
    return (
      <div className="small text-muted">
        {t('marketing.corrections.expander.loading')}
      </div>
    );
  }
  if (!data?.items?.length) {
    return (
      <div className="small text-muted">
        {t('marketing.corrections.expander.empty')}
      </div>
    );
  }

  return (
    <ol className="small mb-0 ps-3">
      {data.items.map(entry => (
        <li key={entry.correctingActivityUuid} className="mb-1">
          <span className="text-muted">
            {new Date(entry.recordedAt).toLocaleString()}
          </span>
          {entry.recordedBy ? (
            <code className="ms-2">{entry.recordedBy.slice(0, 8)}</code>
          ) : null}
          {entry.reason ? (
            <span className="ms-2">— {entry.reason}</span>
          ) : (
            <span className="ms-2 text-muted">
              {t('marketing.corrections.expander.noReason')}
            </span>
          )}
        </li>
      ))}
    </ol>
  );
};

export default CorrectionsExpander;
