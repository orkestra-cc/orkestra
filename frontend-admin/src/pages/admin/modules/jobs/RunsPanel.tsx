import { useState } from 'react';
import { Alert, Button, Collapse, Form, Spinner } from 'react-bootstrap';
import type { ColumnDef } from '@tanstack/react-table';
import { useTranslation } from 'react-i18next';
import { toast } from 'react-toastify';
import AdvanceTable from 'components/common/advance-table/AdvanceTable';
import SubtleBadge, { type BadgeColor } from 'components/common/SubtleBadge';
import { formatDateTime } from 'helpers/dateFormat';
import { resolveErrorMessage } from 'helpers/errorMessage';
import useAdvanceTable from 'hooks/ui/useAdvanceTable';
import AdvanceTableProvider from 'providers/AdvanceTableProvider';
import {
  useLazyListJobRunsQuery,
  useListJobRunsQuery
} from 'store/api/jobsApi';
import { JOB_RUN_STATUSES, type JobRun, type JobRunStatus } from 'types/jobs';
import { formatDuration } from './format';
import { errorBody } from './useJobActions';

const PAGE_SIZE = 25;
const POLL_MS = 15000;

const statusColor: Record<JobRunStatus, BadgeColor> = {
  running: 'info',
  succeeded: 'success',
  failed: 'danger',
  timed_out: 'danger',
  abandoned: 'warning',
  interrupted: 'warning',
  skipped: 'secondary'
};

const displayValue = (value: unknown): string =>
  typeof value === 'string' ? value : JSON.stringify(value);

const RunDetails = ({ run }: { run: JobRun }) => {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const details = Object.entries(run.details ?? {});
  if (!run.error && details.length === 0) {
    return <span className="text-600">—</span>;
  }
  return (
    <div>
      <Button
        variant="link"
        size="sm"
        className="p-0"
        aria-expanded={open}
        onClick={() => setOpen(v => !v)}
      >
        {t(open ? 'adminJobs.runs.hideDetails' : 'adminJobs.runs.showDetails')}
      </Button>
      <Collapse in={open}>
        <div>
          {open && run.error && (
            <pre className="bg-body-tertiary border rounded p-2 mt-2 mb-2 fs-11 text-wrap text-break">
              {run.error}
            </pre>
          )}
          {open && details.length > 0 && (
            <dl className="mb-0 mt-2">
              {details.map(([name, value]) => (
                <div key={name} className="d-flex gap-2">
                  <dt className="fw-semibold">{name}</dt>
                  <dd className="mb-0 text-break">{displayValue(value)}</dd>
                </div>
              ))}
              {run.droppedDetails ? (
                <div className="text-600 fs-11">
                  {t('adminJobs.runs.dropped', { count: run.droppedDetails })}
                </div>
              ) : null}
            </dl>
          )}
        </div>
      </Collapse>
    </div>
  );
};

const RunsList = ({
  jobKey,
  status
}: {
  jobKey: string;
  status?: JobRunStatus;
}) => {
  const { t } = useTranslation();
  const first = useListJobRunsQuery(
    { key: jobKey, status, limit: PAGE_SIZE },
    { pollingInterval: POLL_MS }
  );
  const [loadPage, more] = useLazyListJobRunsQuery();
  // Later pages are held here; the first page stays an RTK Query
  // subscription, so polling and run-now invalidation refresh it.
  const [extra, setExtra] = useState<JobRun[]>([]);
  const [tail, setTail] = useState<{ cursor?: string } | null>(null);

  const nextCursor = tail ? tail.cursor : first.data?.nextCursor;
  const seen = new Set<string>();
  const rows = [...(first.data?.runs ?? []), ...extra].filter(run => {
    if (seen.has(run.id)) return false;
    seen.add(run.id);
    return true;
  });

  const onLoadMore = async () => {
    if (!nextCursor) return;
    try {
      const page = await loadPage({
        key: jobKey,
        status,
        cursor: nextCursor,
        limit: PAGE_SIZE
      }).unwrap();
      setExtra(current => [...current, ...page.runs]);
      setTail({ cursor: page.nextCursor });
    } catch (e) {
      toast.error(
        resolveErrorMessage(errorBody(e), t('adminJobs.errors.generic'))
      );
    }
  };

  const columns: ColumnDef<JobRun>[] = [
    {
      id: 'startedAt',
      header: t('adminJobs.runs.columns.started'),
      enableSorting: false,
      meta: {
        headerProps: { className: 'text-900' },
        cellProps: { className: 'text-nowrap' }
      },
      cell: ({ row: { original: run } }) => (
        <span data-testid="job-run-row">{formatDateTime(run.startedAt)}</span>
      )
    },
    {
      id: 'status',
      header: t('adminJobs.runs.columns.status'),
      enableSorting: false,
      meta: { headerProps: { className: 'text-900' } },
      cell: ({ row: { original: run } }) => (
        <SubtleBadge bg={statusColor[run.status]}>
          {t(`adminJobs.runs.status.${run.status}`)}
        </SubtleBadge>
      )
    },
    {
      id: 'trigger',
      header: t('adminJobs.runs.columns.trigger'),
      enableSorting: false,
      meta: { headerProps: { className: 'text-900' } },
      cell: ({ row: { original: run } }) =>
        t(`adminJobs.runs.trigger.${run.trigger}`)
    },
    {
      id: 'attempt',
      header: t('adminJobs.runs.columns.attempt'),
      enableSorting: false,
      meta: { headerProps: { className: 'text-900' } },
      cell: ({ row: { original: run } }) => run.attempt
    },
    {
      id: 'duration',
      header: t('adminJobs.runs.columns.duration'),
      enableSorting: false,
      meta: {
        headerProps: { className: 'text-900' },
        cellProps: { className: 'text-nowrap' }
      },
      cell: ({ row: { original: run } }) =>
        run.status === 'running' ? '—' : formatDuration(run.durationMs)
    },
    {
      id: 'details',
      header: t('adminJobs.runs.columns.details'),
      enableSorting: false,
      meta: { headerProps: { className: 'text-900' } },
      cell: ({ row: { original: run } }) => <RunDetails run={run} />
    }
  ];

  const table = useAdvanceTable({
    data: rows,
    columns,
    pagination: false,
    getRowId: run => run.id
  });

  if (first.isLoading) {
    return (
      <div className="text-center py-4" role="status">
        <Spinner animation="border" size="sm" className="me-2" />
        <span className="fs-10">{t('adminJobs.runs.loading')}</span>
      </div>
    );
  }
  if (first.error) {
    return (
      <Alert variant="danger" className="fs-10 mb-0">
        {resolveErrorMessage(
          errorBody(first.error),
          t('adminJobs.runs.loadFailed')
        )}
      </Alert>
    );
  }
  if (rows.length === 0) {
    return (
      <p className="text-600 fs-10 mb-0" role="status">
        {t('adminJobs.runs.empty')}
      </p>
    );
  }
  return (
    <>
      <AdvanceTableProvider {...table}>
        <AdvanceTable
          headerClassName="text-nowrap align-middle"
          rowClassName="align-middle"
          tableProps={{ size: 'sm', className: 'fs-10 mb-0' }}
        />
      </AdvanceTableProvider>
      {nextCursor && (
        <div className="text-center mt-3">
          <Button
            variant="orkestra-default"
            size="sm"
            disabled={more.isFetching}
            onClick={onLoadMore}
          >
            {more.isFetching && (
              <Spinner animation="border" size="sm" className="me-2" />
            )}
            {t('adminJobs.runs.loadMore')}
          </Button>
        </div>
      )}
    </>
  );
};

const RunsPanel = ({ jobKey }: { jobKey: string }) => {
  const { t } = useTranslation();
  const [status, setStatus] = useState<JobRunStatus | ''>('');
  return (
    <div>
      <Form.Group className="mb-3" controlId="job-runs-status">
        <Form.Label className="fs-10">
          {t('adminJobs.runs.filterLabel')}
        </Form.Label>
        <Form.Select
          size="sm"
          value={status}
          aria-label={t('adminJobs.runs.filterAria')}
          onChange={event => setStatus(event.target.value as JobRunStatus | '')}
        >
          <option value="">{t('adminJobs.runs.allStatuses')}</option>
          {JOB_RUN_STATUSES.map(s => (
            <option key={s} value={s}>
              {t(`adminJobs.runs.status.${s}`)}
            </option>
          ))}
        </Form.Select>
      </Form.Group>
      {/* Keyed so a filter or job change drops the loaded extra pages. */}
      <RunsList
        key={`${jobKey}:${status}`}
        jobKey={jobKey}
        status={status || undefined}
      />
    </div>
  );
};

export default RunsPanel;
