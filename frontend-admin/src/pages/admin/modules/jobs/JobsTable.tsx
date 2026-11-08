import { useMemo } from 'react';
import { Button, Form } from 'react-bootstrap';
import {
  faClockRotateLeft,
  faPenToSquare,
  faPlay
} from '@fortawesome/free-solid-svg-icons';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import type { ColumnDef } from '@tanstack/react-table';
import { useTranslation } from 'react-i18next';
import AdvanceTable from 'components/common/advance-table/AdvanceTable';
import SubtleBadge from 'components/common/SubtleBadge';
import { formatDateTime } from 'helpers/dateFormat';
import useAdvanceTable from 'hooks/ui/useAdvanceTable';
import AdvanceTableProvider from 'providers/AdvanceTableProvider';
import type { Job } from 'types/jobs';
import {
  describeSchedule,
  formatDuration,
  formatRelative,
  jobStatus
} from './format';
import type { JobActions } from './useJobActions';

export type JobDrawerTab = 'overview' | 'schedule' | 'runs';

interface JobsTableProps {
  jobs: Job[];
  actions: JobActions;
  onOpen: (key: string, tab: JobDrawerTab) => void;
}

/** The per-row controls, shared with the drawer header. */
export const JobRowActions = ({
  job,
  actions,
  onEdit
}: {
  job: Job;
  actions: JobActions;
  onEdit: () => void;
}) => {
  const { t } = useTranslation();
  if (!actions.canManage) return null;
  const busy = actions.busyKey === job.key;
  const manual = job.schedule.kind === 'manual';
  return (
    <div className="d-flex align-items-center justify-content-end gap-2">
      <Form.Check
        type="switch"
        role="switch"
        id={`job-enabled-${job.key}`}
        className="mb-0"
        checked={job.enabled}
        disabled={busy}
        aria-label={t('adminJobs.actions.enableAria', { title: job.title })}
        onChange={() => actions.toggle(job)}
      />
      <Button
        variant="orkestra-default"
        size="sm"
        disabled={manual}
        title={t('adminJobs.actions.editSchedule')}
        aria-label={t('adminJobs.actions.editScheduleAria', {
          title: job.title
        })}
        onClick={onEdit}
      >
        <FontAwesomeIcon icon={faPenToSquare} />
      </Button>
      <Button
        variant="orkestra-default"
        size="sm"
        disabled={busy || !job.moduleStarted}
        title={t('adminJobs.actions.runNow')}
        aria-label={t('adminJobs.actions.runNowAria', { title: job.title })}
        onClick={() => actions.runNow(job)}
      >
        <FontAwesomeIcon icon={faPlay} />
      </Button>
      {job.overridden && (
        <Button
          variant="orkestra-default"
          size="sm"
          disabled={busy}
          title={t('adminJobs.actions.reset')}
          aria-label={t('adminJobs.actions.resetAria', { title: job.title })}
          onClick={() => actions.reset(job)}
        >
          <FontAwesomeIcon icon={faClockRotateLeft} />
        </Button>
      )}
    </div>
  );
};

const JobsTable = ({ jobs, actions, onOpen }: JobsTableProps) => {
  const { t, i18n } = useTranslation();

  const columns = useMemo<ColumnDef<Job>[]>(
    () => [
      {
        accessorKey: 'title',
        header: t('adminJobs.columns.job'),
        meta: { headerProps: { className: 'text-900' } },
        cell: ({ row: { original: job } }) => (
          <div>
            <Button
              variant="link"
              className="p-0 fw-semibold text-start"
              onClick={() => onOpen(job.key, 'overview')}
            >
              {job.title}
            </Button>
            <div className="text-600 fs-11">
              <code>{job.key}</code>
            </div>
          </div>
        )
      },
      {
        accessorKey: 'module',
        header: t('adminJobs.columns.module'),
        meta: { headerProps: { className: 'text-900' } }
      },
      {
        id: 'schedule',
        header: t('adminJobs.columns.schedule'),
        enableSorting: false,
        meta: { headerProps: { className: 'text-900' } },
        cell: ({ row: { original: job } }) => (
          <div>
            <span>{describeSchedule(t, job.schedule, job.timezone)}</span>
            {job.overridden && (
              <SubtleBadge bg="info" className="ms-2">
                {t('adminJobs.overridden')}
              </SubtleBadge>
            )}
          </div>
        )
      },
      {
        id: 'status',
        header: t('adminJobs.columns.status'),
        enableSorting: false,
        meta: { headerProps: { className: 'text-900' } },
        cell: ({ row: { original: job } }) => {
          const status = jobStatus(job);
          return (
            <SubtleBadge bg={status.color}>
              {t(`adminJobs.status.${status.key}`)}
            </SubtleBadge>
          );
        }
      },
      {
        id: 'lastRun',
        header: t('adminJobs.columns.lastRun'),
        enableSorting: false,
        meta: {
          headerProps: { className: 'text-900' },
          cellProps: { className: 'text-nowrap' }
        },
        cell: ({ row: { original: job } }) =>
          job.lastRun ? (
            <span title={formatDateTime(job.lastRun.startedAt)}>
              {formatRelative(job.lastRun.startedAt, i18n.language)}
              <span className="text-600">
                {' · '}
                {formatDuration(job.lastRun.durationMs)}
              </span>
            </span>
          ) : (
            '—'
          )
      },
      {
        id: 'nextRun',
        header: t('adminJobs.columns.nextRun'),
        enableSorting: false,
        meta: {
          headerProps: { className: 'text-900' },
          cellProps: { className: 'text-nowrap' }
        },
        cell: ({ row: { original: job } }) =>
          job.enabled && job.moduleStarted && job.nextRunAt ? (
            <span title={formatDateTime(job.nextRunAt)}>
              {formatRelative(job.nextRunAt, i18n.language)}
            </span>
          ) : (
            '—'
          )
      },
      ...(actions.canManage
        ? [
            {
              id: 'actions',
              header: t('adminJobs.columns.actions'),
              enableSorting: false,
              meta: {
                headerProps: { className: 'text-900 text-end' },
                cellProps: { className: 'text-end' }
              },
              cell: ({ row: { original: job } }) => (
                <JobRowActions
                  job={job}
                  actions={actions}
                  onEdit={() => onOpen(job.key, 'schedule')}
                />
              )
            } satisfies ColumnDef<Job>
          ]
        : [])
    ],
    [actions, i18n.language, onOpen, t]
  );

  const table = useAdvanceTable({
    data: jobs,
    columns,
    sortable: true,
    pagination: false,
    getRowId: job => job.key
  });

  return (
    <AdvanceTableProvider {...table}>
      <AdvanceTable
        headerClassName="text-nowrap align-middle"
        rowClassName="align-middle"
        tableProps={{ striped: true, className: 'fs-10 mb-0 overflow-hidden' }}
      />
    </AdvanceTableProvider>
  );
};

export default JobsTable;
