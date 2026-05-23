// ScoresTab — Phase 2 contact-detail tab. Lists every score
// snapshot for the person across profiles. Each row expands into a
// breakdown drawer showing which activities contributed.

import { useState } from 'react';
import { Badge, Button, Table } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import {
  useListPersonScoresQuery,
  useListScoreProfilesQuery
} from 'store/api/marketingApi';
import BreakdownDrawer from './BreakdownDrawer';

interface ScoresTabProps {
  personId: string;
}

const ScoresTab: React.FC<ScoresTabProps> = ({ personId }) => {
  const { t } = useTranslation();
  const [snapshotId, setSnapshotId] = useState<string | null>(null);

  const { data, isLoading, error } = useListPersonScoresQuery(personId);
  const { data: profiles } = useListScoreProfilesQuery(undefined);

  const profilesByUUID = Object.fromEntries(
    (profiles?.items ?? []).map(p => [p.uuid, p])
  );

  if (isLoading) {
    return <p className="text-muted">{t('marketing.scores.loading')}</p>;
  }
  if (error) {
    return <p className="text-danger">{t('marketing.scores.loadError')}</p>;
  }

  if (!data?.items?.length) {
    return <p className="text-muted mb-0">{t('marketing.scores.empty')}</p>;
  }

  // Sort by value desc so the highest-ranking profile shows first.
  const sorted = [...data.items].sort((a, b) => b.value - a.value);

  return (
    <>
      <p className="text-muted">{t('marketing.scores.description')}</p>
      <Table size="sm" responsive>
        <thead>
          <tr>
            <th>{t('marketing.scores.colProfile')}</th>
            <th style={{ width: 100 }}>{t('marketing.scores.colValue')}</th>
            <th style={{ width: 130 }}>
              {t('marketing.scores.colActivityCount')}
            </th>
            <th style={{ width: 120 }}>{t('marketing.scores.colState')}</th>
            <th style={{ width: 90 }}></th>
          </tr>
        </thead>
        <tbody>
          {sorted.map(s => (
            <tr key={s.uuid}>
              <td>
                {profilesByUUID[s.profileUuid]?.name ??
                  s.profileUuid.slice(0, 8)}{' '}
                <small className="text-muted">v{s.profileVersion}</small>
              </td>
              <td>
                <strong>{s.value.toFixed(2)}</strong>
              </td>
              <td>
                <small className="text-muted">{s.activityCount}</small>
              </td>
              <td>
                {!s.applicable && (
                  <Badge bg="light" text="dark" className="me-1">
                    {t('marketing.scores.badgeNotApplicable')}
                  </Badge>
                )}
                {s.stale && (
                  <Badge bg="warning" text="dark">
                    {t('marketing.scores.badgeStale')}
                  </Badge>
                )}
                {s.applicable && !s.stale && (
                  <Badge bg="success">{t('marketing.scores.badgeFresh')}</Badge>
                )}
              </td>
              <td>
                <Button
                  size="sm"
                  variant="link"
                  onClick={() => setSnapshotId(s.uuid)}
                >
                  {t('marketing.scores.viewBreakdown')}
                </Button>
              </td>
            </tr>
          ))}
        </tbody>
      </Table>

      <BreakdownDrawer
        snapshotId={snapshotId}
        onHide={() => setSnapshotId(null)}
      />
    </>
  );
};

export default ScoresTab;
