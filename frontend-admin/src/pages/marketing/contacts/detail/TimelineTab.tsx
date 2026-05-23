// TimelineTab — Phase 2 contact-detail tab that renders the activity
// log for the person, with a "Log activity" button that opens the
// ManualActivityModal.
//
// Phase 4 PR-4 additions:
//   - Per-row "Correct" button (hidden on corrected_by rows themselves
//     so operators can't correct a correction).
//   - Strike-through rendering on rows superseded by a corrected_by
//     entry. We detect supersede in O(N) by indexing the visible page
//     of activities by uuid and walking the kind==corrected_by rows'
//     refs.correctsActivityUuid.
//   - Per-row "↻ corrected" badge that toggles the CorrectionsExpander
//     under the row when clicked.
//
// Pagination: simple "Load more" button bumping skip — the timeline
// is bounded per-person and operators rarely scroll past the top 100
// rows. A virtualised infinite-scroll list is a follow-up if a real
// tenant hits the limit.

import { Fragment, useMemo, useState } from 'react';
import { Badge, Button, Table } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import { useListPersonActivitiesQuery } from 'store/api/marketingApi';
import type { Activity } from 'types/marketing';
import ManualActivityModal from './ManualActivityModal';
import CorrectActivityModal from './CorrectActivityModal';
import CorrectionsExpander from './CorrectionsExpander';

interface TimelineTabProps {
  personId: string;
}

const TimelineTab: React.FC<TimelineTabProps> = ({ personId }) => {
  const { t } = useTranslation();
  const [showModal, setShowModal] = useState(false);
  const [limit, setLimit] = useState(50);
  const [correctTarget, setCorrectTarget] = useState<Activity | null>(null);
  const [expandedUuid, setExpandedUuid] = useState<string | null>(null);

  const { data, isLoading, error } = useListPersonActivitiesQuery({
    personId,
    limit
  });

  // Set of activity uuids that have at least one corrected_by row in
  // the current page. The corrections-list fetch is per-row; this
  // local index just decides whether to render the strike-through +
  // badge cheaply.
  const correctedUuids = useMemo(() => {
    const out = new Set<string>();
    for (const a of data?.items ?? []) {
      if (a.kind === 'corrected_by' && a.refs?.correctsActivityUuid) {
        out.add(a.refs.correctsActivityUuid);
      }
    }
    return out;
  }, [data]);

  return (
    <>
      <div className="d-flex justify-content-between align-items-center mb-3">
        <p className="text-muted mb-0">{t('marketing.timeline.description')}</p>
        <Button
          size="sm"
          variant="outline-primary"
          onClick={() => setShowModal(true)}
        >
          {t('marketing.timeline.logActivity')}
        </Button>
      </div>

      {isLoading && (
        <p className="text-muted">{t('marketing.timeline.loading')}</p>
      )}
      {error && (
        <p className="text-danger">{t('marketing.timeline.loadError')}</p>
      )}

      {!isLoading && !error && (!data?.items || data.items.length === 0) && (
        <p className="text-muted mb-0">{t('marketing.timeline.empty')}</p>
      )}

      {data?.items && data.items.length > 0 && (
        <>
          <Table size="sm" responsive className="mb-2">
            <thead>
              <tr>
                <th style={{ width: 140 }}>
                  {t('marketing.timeline.colWhen')}
                </th>
                <th style={{ width: 160 }}>
                  {t('marketing.timeline.colKind')}
                </th>
                <th>{t('marketing.timeline.colPayload')}</th>
                <th style={{ width: 100 }}>
                  {t('marketing.timeline.colSource')}
                </th>
                <th style={{ width: 90 }}></th>
              </tr>
            </thead>
            <tbody>
              {data.items.map(a => {
                const isSuperseded = correctedUuids.has(a.uuid);
                const isCorrection = a.kind === 'corrected_by';
                const isExpanded = expandedUuid === a.uuid;
                const cellStyle = isSuperseded
                  ? { textDecoration: 'line-through' as const }
                  : undefined;

                return (
                  <Fragment key={a.uuid}>
                    <tr>
                      <td style={cellStyle}>
                        <small className="text-muted">
                          {new Date(a.occurredAt).toLocaleString()}
                        </small>
                      </td>
                      <td>
                        <Badge bg={isCorrection ? 'warning' : 'secondary'} pill>
                          {a.kind}
                        </Badge>{' '}
                        {isSuperseded && (
                          <button
                            type="button"
                            className="btn btn-link btn-sm p-0 ms-1 text-warning"
                            onClick={() =>
                              setExpandedUuid(isExpanded ? null : a.uuid)
                            }
                            title={t('marketing.corrections.badge.title')}
                          >
                            ↻ {t('marketing.corrections.badge.label')}
                          </button>
                        )}
                      </td>
                      <td style={cellStyle}>
                        {a.payload && Object.keys(a.payload).length > 0 ? (
                          <small className="font-monospace">
                            {JSON.stringify(a.payload)}
                          </small>
                        ) : (
                          <small className="text-muted">—</small>
                        )}
                      </td>
                      <td>
                        <small className="text-muted">{a.source}</small>
                      </td>
                      <td className="text-end">
                        {!isCorrection && (
                          <Button
                            size="sm"
                            variant="link"
                            className="p-0"
                            onClick={() => setCorrectTarget(a)}
                          >
                            {t('marketing.corrections.rowAction')}
                          </Button>
                        )}
                      </td>
                    </tr>
                    {isSuperseded && isExpanded && (
                      <tr className="table-light">
                        <td colSpan={5}>
                          <CorrectionsExpander activityId={a.uuid} />
                        </td>
                      </tr>
                    )}
                  </Fragment>
                );
              })}
            </tbody>
          </Table>

          {data.meta.count >= limit && (
            <div className="d-flex justify-content-center">
              <Button
                size="sm"
                variant="link"
                onClick={() => setLimit(limit + 50)}
              >
                {t('marketing.timeline.loadMore')}
              </Button>
            </div>
          )}
        </>
      )}

      <ManualActivityModal
        personId={personId}
        show={showModal}
        onHide={() => setShowModal(false)}
      />

      {correctTarget && (
        <CorrectActivityModal
          activity={correctTarget}
          onClose={() => setCorrectTarget(null)}
        />
      )}
    </>
  );
};

export default TimelineTab;
