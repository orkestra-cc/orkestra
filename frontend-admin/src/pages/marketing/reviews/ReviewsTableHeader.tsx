// Header bar for the Reviews table — search box (AdvanceTable global
// filter) on the left, URL-synced server-side filters on the right.
// The URL params remain the source of truth for the fetch query in
// the parent page; this component only reads/writes them.

import { Button, Dropdown, Form } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { useTranslation } from 'react-i18next';
import { useSearchParams } from 'react-router';
import AdvanceTableSearchBox from 'components/common/advance-table/AdvanceTableSearchBox';
import ExportCsvButton from 'components/common/advance-table/ExportCsvButton';
import { formatDateForCSV } from 'utils/csvExport';
import type {
  ConflictReview,
  ConflictReviewStatus,
  ConflictTargetKind
} from 'types/marketing';

const STATUSES: ConflictReviewStatus[] = ['pending', 'resolved', 'dismissed'];
const TARGET_KINDS: ConflictTargetKind[] = ['person', 'organization'];

interface Props {
  onRefresh: () => void;
}

const ReviewsTableHeader = ({ onRefresh }: Props) => {
  const { t } = useTranslation();
  const [searchParams, setSearchParams] = useSearchParams();

  const status =
    (searchParams.get('status') as ConflictReviewStatus) || 'pending';
  const targetKind =
    (searchParams.get('targetKind') as ConflictTargetKind | null) || '';
  const importJobUuid = searchParams.get('importJobUuid') || '';

  const updateParam = (key: string, value: string | null) => {
    const next = new URLSearchParams(searchParams);
    if (value === null || value === '') next.delete(key);
    else next.set(key, value);
    setSearchParams(next, { replace: true });
  };

  const statusLabel = t(`marketing.reviews.status.${status}`);
  const targetLabel = targetKind
    ? t(`marketing.reviews.target.${targetKind}`)
    : t('marketing.reviews.filter.all');

  return (
    <div className="d-flex flex-wrap justify-content-between align-items-center gap-2 px-x1 py-2 border-bottom border-200">
      <div className="flex-grow-1" style={{ maxWidth: 360 }}>
        <AdvanceTableSearchBox
          placeholder={t('marketing.reviews.searchPlaceholder')}
        />
      </div>
      <div className="d-flex align-items-center gap-2 flex-wrap">
        <Dropdown className="font-sans-serif">
          <Dropdown.Toggle
            variant="orkestra-default"
            size="sm"
            className="text-600"
          >
            <FontAwesomeIcon
              icon="filter"
              transform="shrink-4"
              className="me-2"
            />
            <span className="d-none d-sm-inline-block">
              {t('marketing.reviews.filter.status')}: {statusLabel}
            </span>
          </Dropdown.Toggle>
          <Dropdown.Menu className="border py-2">
            {STATUSES.map(s => (
              <Dropdown.Item
                key={s}
                onClick={() => updateParam('status', s)}
                className={status === s ? 'active' : ''}
              >
                {t(`marketing.reviews.status.${s}`)}
                {status === s && (
                  <FontAwesomeIcon
                    icon="check"
                    transform="down-4 shrink-4"
                    className="ms-2"
                  />
                )}
              </Dropdown.Item>
            ))}
          </Dropdown.Menu>
        </Dropdown>

        <Dropdown className="font-sans-serif">
          <Dropdown.Toggle
            variant="orkestra-default"
            size="sm"
            className="text-600"
          >
            <span className="d-none d-sm-inline-block">
              {t('marketing.reviews.filter.targetKind')}: {targetLabel}
            </span>
          </Dropdown.Toggle>
          <Dropdown.Menu className="border py-2">
            <Dropdown.Item
              onClick={() => updateParam('targetKind', null)}
              className={!targetKind ? 'active' : ''}
            >
              {t('marketing.reviews.filter.all')}
            </Dropdown.Item>
            {TARGET_KINDS.map(k => (
              <Dropdown.Item
                key={k}
                onClick={() => updateParam('targetKind', k)}
                className={targetKind === k ? 'active' : ''}
              >
                {t(`marketing.reviews.target.${k}`)}
              </Dropdown.Item>
            ))}
          </Dropdown.Menu>
        </Dropdown>

        <Form.Control
          size="sm"
          type="text"
          placeholder={t('marketing.reviews.filter.importJobPlaceholder')}
          value={importJobUuid}
          onChange={e => updateParam('importJobUuid', e.target.value || null)}
          style={{ width: 220 }}
          aria-label={t('marketing.reviews.filter.importJob')}
        />

        <ExportCsvButton<ConflictReview>
          filename="marketing_reviews"
          buildRow={r => ({
            UUID: r.uuid,
            ImportJobUUID: r.importJobUuid,
            TargetKind: r.targetKind,
            ExistingUUID: r.existingUuid,
            Status: r.status,
            ConflictFields: r.conflicts.map(c => c.field).join('; '),
            ConflictCount: r.conflicts.length,
            ResolutionAction: r.resolution?.action ?? '',
            ResolvedBy: r.resolvedBy ?? '',
            ResolvedAt: formatDateForCSV(r.resolvedAt),
            CreatedAt: formatDateForCSV(r.createdAt),
            UpdatedAt: formatDateForCSV(r.updatedAt)
          })}
        />

        <Button variant="outline-secondary" size="sm" onClick={onRefresh}>
          {t('marketing.reviews.refresh')}
        </Button>
      </div>
    </div>
  );
};

export default ReviewsTableHeader;
