// Conflict-review queue page — Phase 3. Lists every row the importer
// pipeline parked in marketing_conflict_reviews and opens
// ReviewResolverModal for per-row resolution.
//
// URL-synced filters: ?status=pending|resolved|dismissed,
// ?targetKind=person|organization, ?importJobUuid=<uuid>. The page
// is reachable from the sidebar (NavItem "Reviews") and deep-linked
// from the imports list's badge column.

import { useMemo, useState } from 'react';
import { Card, Badge, Button, Spinner } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import { useSearchParams } from 'react-router';
import type { ColumnDef } from '@tanstack/react-table';

import useAdvanceTable from 'hooks/ui/useAdvanceTable';
import AdvanceTableProvider from 'providers/AdvanceTableProvider';
import AdvanceTable from 'components/common/advance-table/AdvanceTable';
import AdvanceTableFooter from 'components/common/advance-table/AdvanceTableFooter';

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
import ReviewsTableHeader from './ReviewsTableHeader';

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
    { skip: !selectedUuid }
  );

  const updateParam = (key: string, value: string | null) => {
    const next = new URLSearchParams(searchParams);
    if (value === null || value === '') next.delete(key);
    else next.set(key, value);
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

  const columns = useMemo<ColumnDef<ConflictReview>[]>(
    () => [
      {
        id: 'createdAt',
        accessorKey: 'createdAt',
        header: t('marketing.reviews.col.created'),
        cell: ({ getValue }) => (
          <small className="text-muted">
            {new Date(getValue() as string).toLocaleString()}
          </small>
        ),
        meta: { headerProps: { className: 'text-900' } }
      },
      {
        id: 'targetKind',
        accessorFn: row => `${row.targetKind} ${row.existingUuid}`,
        header: t('marketing.reviews.col.targetKind'),
        cell: ({ row }) => {
          const r = row.original;
          return (
            <>
              <Badge bg="light" text="dark">
                {r.targetKind}
              </Badge>
              <div className="text-muted fs-10">
                <code>{r.existingUuid.slice(0, 8)}</code>
              </div>
            </>
          );
        },
        meta: { headerProps: { className: 'text-900' } }
      },
      {
        id: 'conflicts',
        accessorFn: row =>
          `${row.conflicts.length} ${row.conflicts
            .map(c => c.field)
            .join(' ')}`,
        header: t('marketing.reviews.col.conflicts'),
        cell: ({ row }) => {
          const r = row.original;
          return (
            <>
              <span className="fw-medium">{r.conflicts.length}</span>{' '}
              <span className="text-muted fs-10">
                ({r.conflicts.map(c => c.field).join(', ')})
              </span>
            </>
          );
        },
        meta: { headerProps: { className: 'text-900' } }
      },
      {
        id: 'importJob',
        accessorKey: 'importJobUuid',
        header: t('marketing.reviews.col.importJob'),
        cell: ({ getValue }) => (
          <code className="fs-10">{String(getValue()).slice(0, 8)}</code>
        ),
        meta: { headerProps: { className: 'text-900' } }
      },
      {
        id: 'status',
        accessorKey: 'status',
        header: t('marketing.reviews.col.status'),
        cell: ({ getValue }) => {
          const s = getValue() as ConflictReviewStatus;
          return <Badge bg={statusVariant[s]}>{s}</Badge>;
        },
        meta: { headerProps: { className: 'text-900' } }
      },
      {
        id: 'actions',
        enableSorting: false,
        header: () => (
          <span className="text-end d-block">
            {t('marketing.reviews.col.actions')}
          </span>
        ),
        cell: ({ row }) => {
          const r = row.original;
          return (
            <div className="text-end">
              <Button
                size="sm"
                variant={
                  r.status === 'pending' ? 'primary' : 'outline-secondary'
                }
                onClick={() => openModal(r)}
              >
                {r.status === 'pending'
                  ? t('marketing.reviews.action.resolve')
                  : t('marketing.reviews.action.view')}
              </Button>
            </div>
          );
        },
        meta: { headerProps: { className: 'text-900 text-end' } }
      }
    ],
    [t] // openModal closes over searchParams; that's fine since we don't
    // memoise on it (re-creating once per render is cheaper than the diff).
  );

  const rows = data?.items ?? [];
  const table = useAdvanceTable<ConflictReview>({
    data: rows,
    columns,
    sortable: true,
    pagination: true,
    perPage: 25
  });

  return (
    <>
      <div className="mb-3 d-flex justify-content-between align-items-center">
        <div>
          <h3 className="fw-normal mb-1">{t('marketing.reviews.title')}</h3>
          <p className="fs-10 text-muted mb-0">
            {t('marketing.reviews.subtitle')}
          </p>
        </div>
      </div>

      <Card>
        <Card.Body className="p-0">
          {isLoading ? (
            <div className="p-4 text-center text-muted">
              <Spinner animation="border" size="sm" className="me-2" />
              {t('marketing.reviews.loading')}
            </div>
          ) : (
            // ReviewsTableHeader uses AdvanceTableSearchBox which requires
            // the AdvanceTableProvider context — wrap unconditionally so
            // operators can adjust filters even when the current set returns
            // an empty list.
            <AdvanceTableProvider {...table}>
              <ReviewsTableHeader onRefresh={() => refetch()} />
              {!rows.length ? (
                <div className="p-4 text-center text-muted">
                  {t('marketing.reviews.empty')}
                </div>
              ) : (
                <>
                  <AdvanceTable
                    headerClassName="bg-body-tertiary align-middle"
                    rowClassName="align-middle"
                    tableProps={{
                      size: 'sm',
                      className: 'fs-10 mb-0 overflow-hidden'
                    }}
                  />
                  <div className="px-x1 py-2 border-top border-200">
                    <AdvanceTableFooter
                      rowInfo
                      navButtons
                      rowsPerPageSelection
                      rowsPerPageOptions={[10, 25, 50, 100]}
                    />
                  </div>
                </>
              )}
            </AdvanceTableProvider>
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
