// Imports list table — built on the canonical AdvanceTable stack so
// operators get global search, sort, and pagination over the audit log.
// Status filter is a Dropdown above the table (stable keys → setColumnFilters).

import { useMemo, useState } from 'react';
import { Badge, Button, Dropdown, Spinner } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { Link } from 'react-router';
import { Trans, useTranslation } from 'react-i18next';
import type { ColumnDef } from '@tanstack/react-table';

import useAdvanceTable from 'hooks/ui/useAdvanceTable';
import AdvanceTableProvider, {
  useAdvanceTableContext
} from 'providers/AdvanceTableProvider';
import AdvanceTable from 'components/common/advance-table/AdvanceTable';
import AdvanceTableSearchBox from 'components/common/advance-table/AdvanceTableSearchBox';
import AdvanceTableFooter from 'components/common/advance-table/AdvanceTableFooter';
import ExportCsvButton from 'components/common/advance-table/ExportCsvButton';
import { formatDateForCSV } from 'utils/csvExport';

import { useListMarketingImportsQuery } from 'store/api/marketingApi';
import type { ImportJob, ImportJobStatus } from 'types/marketing';

const statusVariant: Record<ImportJobStatus, string> = {
  queued: 'secondary',
  running: 'info',
  paused_for_review: 'warning',
  done: 'success',
  failed: 'danger'
};

// Stable filter keys so the switch is locale-independent (avoids the
// translated-label trap called out in feedback_imports_filter_keys).
type StatusFilterKey = 'all' | ImportJobStatus;

const STATUS_FILTER_KEYS: StatusFilterKey[] = [
  'all',
  'queued',
  'running',
  'paused_for_review',
  'done',
  'failed'
];

const StatusFilterDropdown = () => {
  const { t } = useTranslation();
  const { setColumnFilters } = useAdvanceTableContext();
  const [selected, setSelected] = useState<StatusFilterKey>('all');

  const label = (key: StatusFilterKey) =>
    key === 'all'
      ? t('marketing.imports.list.statusFilter.all')
      : t(`marketing.imports.list.statusFilter.${key}`);

  const choose = (key: StatusFilterKey) => {
    setSelected(key);
    if (key === 'all') setColumnFilters([]);
    else setColumnFilters([{ id: 'status', value: key }]);
  };

  return (
    <Dropdown className="font-sans-serif">
      <Dropdown.Toggle
        variant="orkestra-default"
        size="sm"
        className="text-600"
      >
        <FontAwesomeIcon icon="filter" transform="shrink-4" className="me-2" />
        <span className="d-none d-sm-inline-block">{label(selected)}</span>
      </Dropdown.Toggle>
      <Dropdown.Menu className="border py-2">
        {STATUS_FILTER_KEYS.map(key => (
          <Dropdown.Item
            key={key}
            onClick={() => choose(key)}
            className={selected === key ? 'active' : ''}
          >
            {label(key)}
            {selected === key && (
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
  );
};

interface Props {
  onRefresh: () => void;
}

const ImportsTable = ({ onRefresh }: Props) => {
  const { t } = useTranslation();
  const { data, isLoading } = useListMarketingImportsQuery(undefined);

  const columns = useMemo<ColumnDef<ImportJob>[]>(
    () => [
      {
        id: 'source',
        accessorFn: row =>
          [row.sourceName ?? '', row.uuid].filter(Boolean).join(' '),
        header: t('marketing.imports.list.colSource'),
        cell: ({ row }) => {
          const j = row.original;
          return (
            <>
              <div className="fw-medium">
                {j.sourceName || (
                  <span className="text-muted">
                    {t('marketing.imports.list.unnamed')}
                  </span>
                )}
              </div>
              <div className="text-muted fs-10">
                <code>{j.uuid.slice(0, 8)}</code>
              </div>
            </>
          );
        },
        meta: { headerProps: { className: 'text-900' } }
      },
      {
        id: 'importer',
        accessorKey: 'importer',
        header: t('marketing.imports.list.colAdapter'),
        cell: ({ getValue }) => (
          <Badge bg="light" text="dark">
            {String(getValue() ?? '')}
          </Badge>
        ),
        meta: { headerProps: { className: 'text-900' } }
      },
      {
        id: 'status',
        accessorKey: 'status',
        header: t('marketing.imports.list.colStatus'),
        // Status filter dropdown uses exact-match equality on the raw key.
        filterFn: 'equals',
        cell: ({ row }) => {
          const j = row.original;
          return (
            <>
              <Badge bg={statusVariant[j.status]}>{j.status}</Badge>
              {j.error && (
                <div className="text-danger fs-10 mt-1">{j.error}</div>
              )}
            </>
          );
        },
        meta: { headerProps: { className: 'text-900' } }
      },
      {
        id: 'reviews',
        accessorFn: row => row.conflictReviewUuids?.length ?? 0,
        header: t('marketing.imports.list.colReviews'),
        cell: ({ row }) => {
          const j = row.original;
          if (!j.conflictReviewUuids?.length) {
            return <span className="text-muted">—</span>;
          }
          return (
            <Link
              to={`/marketing/reviews?importJobUuid=${j.uuid}`}
              className="text-decoration-none"
            >
              <Badge bg="warning" text="dark">
                {t('marketing.imports.list.reviewsBadge', {
                  count: j.conflictReviewUuids.length
                })}
              </Badge>
            </Link>
          );
        },
        meta: { headerProps: { className: 'text-900' } }
      },
      {
        id: 'rowsRead',
        accessorFn: row => row.stats.rowsRead,
        header: t('marketing.imports.list.colRows'),
        cell: ({ row }) => {
          const s = row.original.stats;
          return (
            <>
              {s.rowsRead}
              {s.rowsFailed ? (
                <span className="text-danger fs-10">
                  {t('marketing.imports.list.rowsFailedSuffix', {
                    count: s.rowsFailed
                  })}
                </span>
              ) : null}
              {s.conflictsSkipped ? (
                <span className="text-warning fs-10">
                  {t('marketing.imports.list.conflictsSuffix', {
                    count: s.conflictsSkipped
                  })}
                </span>
              ) : null}
            </>
          );
        },
        meta: { headerProps: { className: 'text-900' } }
      },
      {
        id: 'createdAt',
        accessorKey: 'createdAt',
        header: t('marketing.imports.list.colCreated'),
        cell: ({ getValue }) => (
          <small className="text-muted">
            {new Date(getValue() as string).toLocaleString()}
          </small>
        ),
        meta: { headerProps: { className: 'text-900' } }
      },
      {
        id: 'createdMerged',
        // Not sortable — composite of multiple counts.
        accessorFn: row =>
          `${row.stats.orgsCreated ?? 0} ${row.stats.orgsMerged ?? 0} ${
            row.stats.personsCreated ?? 0
          } ${row.stats.personsMerged ?? 0}`,
        header: t('marketing.imports.list.colCreatedMerged'),
        enableSorting: false,
        cell: ({ row }) => {
          const s = row.original.stats;
          return (
            <>
              <small>
                {t('marketing.imports.list.orgsLine', {
                  created: s.orgsCreated ?? 0,
                  merged: s.orgsMerged ?? 0
                })}
              </small>
              <br />
              <small>
                {t('marketing.imports.list.personsLine', {
                  created: s.personsCreated ?? 0,
                  merged: s.personsMerged ?? 0
                })}
              </small>
            </>
          );
        },
        meta: { headerProps: { className: 'text-900' } }
      }
    ],
    [t]
  );

  const jobs = data?.items ?? [];
  const table = useAdvanceTable<ImportJob>({
    data: jobs,
    columns,
    sortable: true,
    pagination: true,
    perPage: 25
  });

  if (isLoading) {
    return (
      <div className="p-4 text-center text-muted">
        <Spinner animation="border" size="sm" className="me-2" />
        {t('marketing.imports.list.loading')}
      </div>
    );
  }

  if (!jobs.length) {
    return (
      <div className="p-4 text-center text-muted">
        <Trans
          i18nKey="marketing.imports.list.empty"
          components={{ strong: <strong /> }}
        />
      </div>
    );
  }

  return (
    <AdvanceTableProvider {...table}>
      <div className="d-flex flex-wrap justify-content-between align-items-center gap-2 px-x1 py-2 border-bottom border-200">
        <div className="flex-grow-1" style={{ maxWidth: 360 }}>
          <AdvanceTableSearchBox
            placeholder={t('marketing.imports.list.searchPlaceholder')}
          />
        </div>
        <div className="d-flex align-items-center gap-2">
          <StatusFilterDropdown />
          <ExportCsvButton<ImportJob>
            filename="marketing_imports"
            buildRow={j => ({
              UUID: j.uuid,
              SourceName: j.sourceName ?? '',
              Importer: j.importer,
              Status: j.status,
              RowsRead: j.stats.rowsRead,
              RowsFailed: j.stats.rowsFailed ?? 0,
              ConflictsSkipped: j.stats.conflictsSkipped ?? 0,
              OrgsCreated: j.stats.orgsCreated ?? 0,
              OrgsMerged: j.stats.orgsMerged ?? 0,
              PersonsCreated: j.stats.personsCreated ?? 0,
              PersonsMerged: j.stats.personsMerged ?? 0,
              MembershipsLinked: j.stats.membershipsLinked ?? 0,
              EngagementEmitted: j.stats.engagementEmitted ?? 0,
              Error: j.error ?? '',
              CreatedAt: formatDateForCSV(j.createdAt),
              StartedAt: formatDateForCSV(j.startedAt),
              CompletedAt: formatDateForCSV(j.completedAt),
              CreatedBy: j.createdBy ?? ''
            })}
          />
          <Button variant="outline-secondary" size="sm" onClick={onRefresh}>
            {t('marketing.imports.list.refresh')}
          </Button>
        </div>
      </div>
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
    </AdvanceTableProvider>
  );
};

export default ImportsTable;
