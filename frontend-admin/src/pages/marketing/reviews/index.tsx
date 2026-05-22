// Conflict-review queue page — Phase 3. Lists every row the importer
// pipeline parked in marketing_conflict_reviews and opens
// ReviewResolverModal for per-row resolution.
//
// URL-synced filters: ?status=pending|resolved|dismissed,
// ?targetKind=person|organization, ?importJobUuid=<uuid>. The page
// is reachable from the sidebar (NavItem "Reviews") and deep-linked
// from the imports list's badge column.

import { useState } from 'react';
import { Card, Form, Table, Badge, Button } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import { useSearchParams } from 'react-router';
import {
  useListConflictReviewsQuery,
  useGetConflictReviewQuery
} from 'store/api/marketingApi';
import type {
  ConflictReview,
  ConflictReviewStatus,
  ConflictTargetKind
} from 'types/marketing';
import ReviewResolverModal from './ReviewResolverModal';

const statusVariant: Record<ConflictReviewStatus, string> = {
  pending: 'warning',
  resolved: 'success',
  dismissed: 'secondary'
};

const ReviewsPage: React.FC = () => {
  const { t } = useTranslation();
  const [searchParams, setSearchParams] = useSearchParams();

  const status =
    (searchParams.get('status') as ConflictReviewStatus) || 'pending';
  const targetKind =
    (searchParams.get('targetKind') as ConflictTargetKind) || undefined;
  const importJobUuid = searchParams.get('importJobUuid') || undefined;
  const openReviewUuid = searchParams.get('reviewUuid') || undefined;

  const { data, isLoading, refetch } = useListConflictReviewsQuery({
    status,
    targetKind,
    importJobUuid,
    limit: 50
  });

  const [selectedUuid, setSelectedUuid] = useState<string | undefined>(
    openReviewUuid
  );
  const { data: selectedReview } = useGetConflictReviewQuery(
    selectedUuid as string,
    {
      skip: !selectedUuid
    }
  );

  const updateParam = (key: string, value: string | null) => {
    const next = new URLSearchParams(searchParams);
    if (value === null || value === '') {
      next.delete(key);
    } else {
      next.set(key, value);
    }
    setSearchParams(next, { replace: true });
  };

  const openModal = (review: ConflictReview) => {
    setSelectedUuid(review.uuid);
    updateParam('reviewUuid', review.uuid);
  };

  const closeModal = () => {
    setSelectedUuid(undefined);
    updateParam('reviewUuid', null);
  };

  return (
    <>
      <div className="mb-3 d-flex justify-content-between align-items-center">
        <div>
          <h3 className="fw-normal mb-1">{t('marketing.reviews.title')}</h3>
          <p className="fs-10 text-muted mb-0">
            {t('marketing.reviews.subtitle')}
          </p>
        </div>
        <Button variant="outline-secondary" size="sm" onClick={() => refetch()}>
          {t('marketing.reviews.refresh')}
        </Button>
      </div>

      <Card className="mb-3">
        <Card.Body className="d-flex gap-3 flex-wrap align-items-end">
          <Form.Group style={{ minWidth: 160 }}>
            <Form.Label className="fs-10 mb-1">
              {t('marketing.reviews.filter.status')}
            </Form.Label>
            <Form.Select
              size="sm"
              value={status}
              onChange={e => updateParam('status', e.target.value)}
            >
              <option value="pending">
                {t('marketing.reviews.status.pending')}
              </option>
              <option value="resolved">
                {t('marketing.reviews.status.resolved')}
              </option>
              <option value="dismissed">
                {t('marketing.reviews.status.dismissed')}
              </option>
            </Form.Select>
          </Form.Group>
          <Form.Group style={{ minWidth: 160 }}>
            <Form.Label className="fs-10 mb-1">
              {t('marketing.reviews.filter.targetKind')}
            </Form.Label>
            <Form.Select
              size="sm"
              value={targetKind ?? ''}
              onChange={e => updateParam('targetKind', e.target.value || null)}
            >
              <option value="">{t('marketing.reviews.filter.all')}</option>
              <option value="person">
                {t('marketing.reviews.target.person')}
              </option>
              <option value="organization">
                {t('marketing.reviews.target.organization')}
              </option>
            </Form.Select>
          </Form.Group>
          <Form.Group className="flex-grow-1" style={{ minWidth: 240 }}>
            <Form.Label className="fs-10 mb-1">
              {t('marketing.reviews.filter.importJob')}
            </Form.Label>
            <Form.Control
              size="sm"
              type="text"
              placeholder={t('marketing.reviews.filter.importJobPlaceholder')}
              value={importJobUuid ?? ''}
              onChange={e =>
                updateParam('importJobUuid', e.target.value || null)
              }
            />
          </Form.Group>
        </Card.Body>
      </Card>

      <Card>
        <Card.Body className="p-0">
          {isLoading ? (
            <div className="p-3 text-muted">
              {t('marketing.reviews.loading')}
            </div>
          ) : !data?.items?.length ? (
            <div className="p-3 text-muted">{t('marketing.reviews.empty')}</div>
          ) : (
            <Table responsive hover className="mb-0">
              <thead className="bg-200">
                <tr>
                  <th>{t('marketing.reviews.col.created')}</th>
                  <th>{t('marketing.reviews.col.targetKind')}</th>
                  <th>{t('marketing.reviews.col.conflicts')}</th>
                  <th>{t('marketing.reviews.col.importJob')}</th>
                  <th>{t('marketing.reviews.col.status')}</th>
                  <th className="text-end">
                    {t('marketing.reviews.col.actions')}
                  </th>
                </tr>
              </thead>
              <tbody>
                {data.items.map(r => (
                  <tr key={r.uuid}>
                    <td>
                      <small className="text-muted">
                        {new Date(r.createdAt).toLocaleString()}
                      </small>
                    </td>
                    <td>
                      <Badge bg="light" text="dark">
                        {r.targetKind}
                      </Badge>
                      <div className="text-muted fs-10">
                        <code>{r.existingUuid.slice(0, 8)}</code>
                      </div>
                    </td>
                    <td>
                      <span className="fw-medium">{r.conflicts.length}</span>{' '}
                      <span className="text-muted fs-10">
                        ({r.conflicts.map(c => c.field).join(', ')})
                      </span>
                    </td>
                    <td>
                      <code className="fs-10">
                        {r.importJobUuid.slice(0, 8)}
                      </code>
                    </td>
                    <td>
                      <Badge bg={statusVariant[r.status]}>{r.status}</Badge>
                    </td>
                    <td className="text-end">
                      <Button
                        size="sm"
                        variant={
                          r.status === 'pending'
                            ? 'primary'
                            : 'outline-secondary'
                        }
                        onClick={() => openModal(r)}
                      >
                        {r.status === 'pending'
                          ? t('marketing.reviews.action.resolve')
                          : t('marketing.reviews.action.view')}
                      </Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </Table>
          )}
        </Card.Body>
      </Card>

      {selectedReview && (
        <ReviewResolverModal review={selectedReview} onClose={closeModal} />
      )}
    </>
  );
};

export default ReviewsPage;
