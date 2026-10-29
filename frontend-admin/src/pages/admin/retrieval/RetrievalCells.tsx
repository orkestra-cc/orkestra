import { useTranslation } from 'react-i18next';
import SubtleBadge, { type BadgeColor } from 'components/common/SubtleBadge';
import { formatDateTime } from 'helpers/dateFormat';
import type {
  RetrievalBacklog,
  RetrievalEngineStatus
} from 'store/api/retrievalApi';

// Cells and status helpers shared by the Corpora and Indexing tabs.

// The engine tile's status edge: earned only by a state that stops search.
export const engineAccent = (
  status: RetrievalEngineStatus['status']
): BadgeColor | undefined =>
  status === 'unavailable'
    ? 'danger'
    : status === 'not_configured'
      ? 'warning'
      : undefined;

// OwnerCell names the module that registers a corpus and whether it runs:
// a stopped owner is why a backlog does not drain.
export const OwnerCell = ({
  owner,
  running
}: {
  owner?: string;
  running: boolean;
}) => {
  const { t } = useTranslation();
  if (!owner) return <span className="text-600">—</span>;
  return (
    <span className="d-inline-flex align-items-center gap-2">
      <span className="font-monospace">{owner}</span>
      <SubtleBadge pill bg={running ? 'success' : 'secondary'}>
        {t(
          running
            ? 'adminRetrieval.corpora.ownerRunning'
            : 'adminRetrieval.corpora.ownerStopped'
        )}
      </SubtleBadge>
    </span>
  );
};

// BacklogCell shows the pending outbox keys and the least recently touched
// one's time, so an operator can tell a queue that drains from one that
// is stuck.
export const BacklogCell = ({ backlog }: { backlog: RetrievalBacklog }) => {
  const { t, i18n } = useTranslation();
  return (
    <div>
      <span className="text-900">
        {backlog.pending.toLocaleString(i18n.language)}
      </span>
      {backlog.pending > 0 && backlog.oldestAt && (
        <div className="fs-11 text-600">
          {t('adminRetrieval.corpora.outboxOldest', {
            date: formatDateTime(backlog.oldestAt)
          })}
        </div>
      )}
    </div>
  );
};
