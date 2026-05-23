// BreakdownDrawer — slide-in panel showing the per-activity score
// breakdown for a single snapshot. Rendered as a bootstrap Modal
// docked right via the `dialogClassName` end-positioning utility;
// React-Bootstrap's Offcanvas would also work but we already have
// Modal styling across the rest of marketing.

import { Badge, Modal, Table } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import { useGetScoreSnapshotQuery } from 'store/api/marketingApi';

interface BreakdownDrawerProps {
  snapshotId: string | null;
  onHide: () => void;
}

const BreakdownDrawer: React.FC<BreakdownDrawerProps> = ({
  snapshotId,
  onHide
}) => {
  const { t } = useTranslation();
  const { data, isLoading, error } = useGetScoreSnapshotQuery(
    snapshotId ?? '',
    { skip: !snapshotId }
  );

  return (
    <Modal show={!!snapshotId} onHide={onHide} size="lg" centered>
      <Modal.Header closeButton>
        <Modal.Title>
          {t('marketing.scores.breakdown.title')}{' '}
          {data && (
            <small className="text-muted">
              · {data.value.toFixed(2)} {t('marketing.scores.breakdown.points')}
            </small>
          )}
        </Modal.Title>
      </Modal.Header>
      <Modal.Body>
        {isLoading && (
          <p className="text-muted">
            {t('marketing.scores.breakdown.loading')}
          </p>
        )}
        {error && (
          <p className="text-danger">
            {t('marketing.scores.breakdown.loadError')}
          </p>
        )}
        {data && !data.breakdown?.length && (
          <p className="text-muted">{t('marketing.scores.breakdown.empty')}</p>
        )}
        {data && data.breakdown && data.breakdown.length > 0 && (
          <Table size="sm" responsive>
            <thead>
              <tr>
                <th>{t('marketing.scores.breakdown.colWhen')}</th>
                <th>{t('marketing.scores.breakdown.colKind')}</th>
                <th>{t('marketing.scores.breakdown.colRule')}</th>
                <th>{t('marketing.scores.breakdown.colRaw')}</th>
                <th>{t('marketing.scores.breakdown.colDecay')}</th>
                <th>{t('marketing.scores.breakdown.colContribution')}</th>
              </tr>
            </thead>
            <tbody>
              {data.breakdown.map((b, i) => (
                <tr key={`${b.activityUuid}-${i}`}>
                  <td>
                    <small className="text-muted">
                      {b.activityUuid ? (
                        new Date(b.occurredAt).toLocaleString()
                      ) : (
                        <em>{t('marketing.scores.breakdown.aggregateRow')}</em>
                      )}
                    </small>
                  </td>
                  <td>
                    <Badge
                      bg={
                        b.activityKind === 'aggregate' ? 'light' : 'secondary'
                      }
                      text={b.activityKind === 'aggregate' ? 'dark' : undefined}
                      pill
                    >
                      {b.activityKind}
                    </Badge>
                  </td>
                  <td>
                    <small>#{b.ruleIndex}</small>
                  </td>
                  <td>{b.rawPoints.toFixed(2)}</td>
                  <td>
                    <small>×{b.appliedDecay.toFixed(3)}</small>
                  </td>
                  <td>
                    <strong>{b.pointsContributed.toFixed(2)}</strong>
                  </td>
                </tr>
              ))}
            </tbody>
          </Table>
        )}
      </Modal.Body>
    </Modal>
  );
};

export default BreakdownDrawer;
