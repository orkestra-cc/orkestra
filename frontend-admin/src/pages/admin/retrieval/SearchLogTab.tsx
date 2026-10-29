import { useMemo, useState } from 'react';
import { Alert, Button, Col, Form, Row, Spinner } from 'react-bootstrap';
import DatePicker from 'react-datepicker';
import { Controller, useForm, type Resolver } from 'react-hook-form';
import { yupResolver } from '@hookform/resolvers/yup';
import * as yup from 'yup';
import { faClockRotateLeft, faFilter } from '@fortawesome/free-solid-svg-icons';
import type {
  CellContext,
  ColumnDef,
  OnChangeFn,
  PaginationState
} from '@tanstack/react-table';
import { useTranslation } from 'react-i18next';
import CustomDateInput from 'components/common/CustomDateInput';
import EmptyState from 'components/common/EmptyState';
import IconButton from 'components/common/IconButton';
import SubtleBadge, { type BadgeColor } from 'components/common/SubtleBadge';
import AdvanceTable from 'components/common/advance-table/AdvanceTable';
import AdvanceTableFooter from 'components/common/advance-table/AdvanceTableFooter';
import { formatDateTime } from 'helpers/dateFormat';
import useAdvanceTable from 'hooks/ui/useAdvanceTable';
import AdvanceTableProvider from 'providers/AdvanceTableProvider';
import {
  useListRetrievalSearchLogQuery,
  type RetrievalSearchLogEntry,
  type RetrievalSearchOutcome
} from 'store/api/retrievalApi';
import RetrievalLoadError from './RetrievalLoadError';
import { isReauthCancelled } from './retrievalErrors';

// One page of the log; the server pages by offset, up to MAX_OFFSET
// (handlers.RetrievalSearchLogInput), so the last reachable page starts there.
const PAGE_SIZE = 50;
const MAX_OFFSET = 10000;
const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

const OUTCOME_COLOR: Record<RetrievalSearchOutcome, BadgeColor> = {
  ok: 'success',
  degraded: 'warning',
  error: 'danger'
};

interface Filters {
  userUuid: string;
  since: Date | null;
  until: Date | null;
}

interface AppliedFilters {
  userUuid?: string;
  since?: string;
  until?: string;
}

// A picked day is a whole local day: `since` from its first instant,
// `until` through its last.
const startOfDay = (d: Date) =>
  new Date(d.getFullYear(), d.getMonth(), d.getDate());
const endOfDay = (d: Date) =>
  new Date(d.getFullYear(), d.getMonth(), d.getDate(), 23, 59, 59, 999);

const toApplied = ({ userUuid, since, until }: Filters): AppliedFilters => ({
  userUuid: userUuid.trim() || undefined,
  since: since ? startOfDay(since).toISOString() : undefined,
  until: until ? endOfDay(until).toISOString() : undefined
});

const Detail = ({
  label,
  children
}: {
  label: string;
  children: React.ReactNode;
}) => (
  <div className="mb-2">
    <dt className="fw-semibold text-700">{label}</dt>
    <dd className="mb-0 text-break">{children}</dd>
  </div>
);

// DetailsCell expands one row in place (precedent: the attributes cell of
// pages/admin/modules/logging/LogPreviewPanel.tsx). It owns its open state,
// so toggling it never rebuilds the memoised columns — which would remount
// every row and drop the focus on the toggle.
const DetailsCell = ({ entry }: { entry: RetrievalSearchLogEntry }) => {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const date = formatDateTime(entry.createdAt);
  const none = (
    <span className="text-600">
      {t('adminRetrieval.searchLog.details.none')}
    </span>
  );
  return (
    <div>
      <Button
        variant="link"
        size="sm"
        className="p-0"
        aria-expanded={open}
        aria-label={t(
          open
            ? 'adminRetrieval.searchLog.details.hideAria'
            : 'adminRetrieval.searchLog.details.showAria',
          { date }
        )}
        onClick={() => setOpen(v => !v)}
      >
        {t(
          open
            ? 'adminRetrieval.searchLog.details.hide'
            : 'adminRetrieval.searchLog.details.show'
        )}
      </Button>
      {open && (
        <dl className="mb-0 mt-2 fs-10 text-wrap">
          <Detail label={t('adminRetrieval.searchLog.details.mode')}>
            {t(`adminRetrieval.diagnostics.modes.${entry.mode}`, {
              defaultValue: entry.mode
            })}
          </Detail>
          <Detail label={t('adminRetrieval.searchLog.details.principals')}>
            {entry.principals.length === 0 ? (
              none
            ) : (
              <span className="d-flex flex-wrap gap-1">
                {entry.principals.map(p => (
                  <SubtleBadge key={p} pill bg="secondary">
                    <span className="font-monospace">{p}</span>
                  </SubtleBadge>
                ))}
              </span>
            )}
          </Detail>
          <Detail label={t('adminRetrieval.searchLog.details.returned')}>
            {entry.returnedSourceIds.length === 0 ? (
              none
            ) : (
              <span className="font-monospace">
                {entry.returnedSourceIds.join(', ')}
              </span>
            )}
          </Detail>
          <Detail label={t('adminRetrieval.searchLog.details.subjects')}>
            {entry.subjects.length === 0 ? (
              none
            ) : (
              <span className="font-monospace">
                {entry.subjects.join(', ')}
              </span>
            )}
          </Detail>
          <Detail label={t('adminRetrieval.searchLog.details.filter')}>
            {/* filterText verbatim, never `filter` re-serialized: the
                browser rounds integers beyond 2^53. */}
            {!entry.filterText ? (
              none
            ) : (
              <pre className="font-monospace fs-11 bg-body-tertiary border rounded p-2 mb-0 text-wrap text-break">
                {entry.filterText}
              </pre>
            )}
          </Detail>
        </dl>
      )}
    </div>
  );
};

// SearchLogTab pages through the active organization's audited searches
// (retrieval.searchlog.admin + MFA + fresh step-up). Every page view is
// itself a read the backend audits, so filters apply on submit rather than
// per keystroke, and nothing is cached. The backend leaves out rows whose
// post-write erasure re-check has not confirmed them, from the page and
// the total alike: the client shows the page as served. A dismissed
// step-up leaves the log unloaded with a way to verify again.
const SearchLogTab = () => {
  const { t, i18n } = useTranslation();
  const [applied, setApplied] = useState<AppliedFilters>({});
  const [pageIndex, setPageIndex] = useState(0);

  // currentData, not data: while another page or other filters load, data
  // would still hold the previous rows under the new page's range. An
  // audit view shows only rows that answer the request on screen.
  const {
    currentData: data,
    isFetching,
    error,
    refetch
  } = useListRetrievalSearchLogQuery({
    ...applied,
    limit: PAGE_SIZE,
    offset: pageIndex * PAGE_SIZE
  });

  const schema = useMemo(
    () =>
      yup.object({
        userUuid: yup
          .string()
          .trim()
          .default('')
          .test(
            'uuid',
            t('adminRetrieval.searchLog.filters.invalidUuid'),
            v => !v || UUID.test(v)
          ),
        since: yup.date().nullable().default(null),
        until: yup
          .date()
          .nullable()
          .default(null)
          .test(
            'after-since',
            t('adminRetrieval.searchLog.filters.untilBeforeSince'),
            (v, ctx) => {
              const since = ctx.parent.since as Date | null;
              return !v || !since || startOfDay(v) >= startOfDay(since);
            }
          )
      }),
    [t]
  );
  const {
    control,
    register,
    handleSubmit,
    formState: { errors }
  } = useForm<Filters>({
    resolver: yupResolver(schema) as unknown as Resolver<Filters>,
    defaultValues: { userUuid: '', since: null, until: null }
  });

  const onApply = (values: Filters) => {
    setApplied(toApplied(values));
    setPageIndex(0);
  };

  const columns = useMemo<ColumnDef<RetrievalSearchLogEntry>[]>(
    () => [
      {
        id: 'createdAt',
        header: t('adminRetrieval.searchLog.columns.createdAt'),
        meta: { headerProps: { className: 'text-900' } },
        cell: ({
          row: { original }
        }: CellContext<RetrievalSearchLogEntry, unknown>) =>
          formatDateTime(original.createdAt)
      },
      {
        id: 'user',
        header: t('adminRetrieval.searchLog.columns.user'),
        meta: { headerProps: { className: 'text-900' } },
        cell: ({
          row: { original }
        }: CellContext<RetrievalSearchLogEntry, unknown>) => (
          <span className="font-monospace">{original.userUuid}</span>
        )
      },
      {
        id: 'corpus',
        header: t('adminRetrieval.searchLog.columns.corpus'),
        meta: { headerProps: { className: 'text-900' } },
        cell: ({
          row: { original }
        }: CellContext<RetrievalSearchLogEntry, unknown>) => original.corpus
      },
      {
        id: 'query',
        header: t('adminRetrieval.searchLog.columns.query'),
        meta: {
          headerProps: { className: 'text-900' },
          cellProps: { className: 'text-wrap text-break' }
        },
        cell: ({
          row: { original }
        }: CellContext<RetrievalSearchLogEntry, unknown>) => original.query
      },
      {
        id: 'returned',
        header: t('adminRetrieval.searchLog.columns.returned'),
        meta: {
          headerProps: { className: 'text-900 text-end' },
          cellProps: { className: 'text-end' }
        },
        cell: ({
          row: { original }
        }: CellContext<RetrievalSearchLogEntry, unknown>) =>
          original.hitCount.toLocaleString(i18n.language)
      },
      {
        id: 'subjects',
        header: t('adminRetrieval.searchLog.columns.subjects'),
        meta: {
          headerProps: { className: 'text-900 text-end' },
          cellProps: { className: 'text-end' }
        },
        cell: ({
          row: { original }
        }: CellContext<RetrievalSearchLogEntry, unknown>) =>
          original.subjects.length.toLocaleString(i18n.language)
      },
      {
        id: 'duration',
        header: t('adminRetrieval.searchLog.columns.duration'),
        meta: {
          headerProps: { className: 'text-900 text-end' },
          cellProps: { className: 'text-end text-nowrap' }
        },
        cell: ({
          row: { original }
        }: CellContext<RetrievalSearchLogEntry, unknown>) =>
          t('adminRetrieval.searchLog.durationMs', {
            ms: original.durationMs.toLocaleString(i18n.language)
          })
      },
      {
        id: 'outcome',
        header: t('adminRetrieval.searchLog.columns.outcome'),
        meta: { headerProps: { className: 'text-900' } },
        cell: ({
          row: { original }
        }: CellContext<RetrievalSearchLogEntry, unknown>) => (
          <div className="d-flex flex-wrap gap-1">
            <SubtleBadge
              pill
              bg={OUTCOME_COLOR[original.outcome] ?? 'secondary'}
            >
              {t(`adminRetrieval.searchLog.outcome.${original.outcome}`, {
                defaultValue: original.outcome
              })}
            </SubtleBadge>
            {original.degraded && original.outcome !== 'degraded' && (
              <SubtleBadge pill bg="warning">
                {t('adminRetrieval.searchLog.outcome.degraded')}
              </SubtleBadge>
            )}
            {original.errorKind && (
              <div className="w-100 fs-11 text-600">
                {t(`adminRetrieval.searchLog.errorKind.${original.errorKind}`, {
                  defaultValue: original.errorKind
                })}
              </div>
            )}
          </div>
        )
      },
      {
        id: 'details',
        header: t('adminRetrieval.searchLog.columns.details'),
        meta: { headerProps: { className: 'text-900' } },
        cell: ({
          row: { original }
        }: CellContext<RetrievalSearchLogEntry, unknown>) => (
          <DetailsCell entry={original} />
        )
      }
    ],
    [t, i18n.language]
  );

  const pagination: PaginationState = { pageIndex, pageSize: PAGE_SIZE };
  const onPaginationChange: OnChangeFn<PaginationState> = updater => {
    const next = typeof updater === 'function' ? updater(pagination) : updater;
    setPageIndex(next.pageIndex);
  };

  const table = useAdvanceTable({
    data: data?.items ?? [],
    columns,
    pagination: true,
    perPage: PAGE_SIZE,
    // Server-side paging (precedent: hooks/ui/useUserTable.tsx): the table
    // holds one page, rowCount is the server's total.
    manualPagination: true,
    rowCount: data?.total ?? 0,
    pageCount: Math.min(
      Math.ceil((data?.total ?? 0) / PAGE_SIZE),
      MAX_OFFSET / PAGE_SIZE + 1
    ),
    state: { pagination },
    onPaginationChange,
    getRowId: row => row.id
  });

  const dateField = (name: 'since' | 'until') => (
    <Form.Group as={Col} sm={6} lg={3} controlId={`retrieval-log-${name}`}>
      <Form.Label>{t(`adminRetrieval.searchLog.filters.${name}`)}</Form.Label>
      <Controller
        control={control}
        name={name}
        render={({ field }) => (
          <DatePicker
            selected={field.value}
            onChange={(d: Date | null) => field.onChange(d)}
            onBlur={field.onBlur}
            dateFormat="yyyy-MM-dd"
            placeholderText={t(
              'adminRetrieval.searchLog.filters.datePlaceholder'
            )}
            isClearable
            customInput={
              <CustomDateInput
                isInvalid={!!errors[name]}
                errorMessage={errors[name]?.message}
              />
            }
          />
        )}
      />
    </Form.Group>
  );

  const stepUpDismissed = isReauthCancelled(error);

  return (
    <>
      <Alert variant="info" className="fs-10">
        {t('adminRetrieval.searchLog.audited')}
      </Alert>
      <Form noValidate onSubmit={handleSubmit(onApply)} className="mb-3">
        <Row className="g-3 align-items-end">
          <Form.Group as={Col} lg={4} controlId="retrieval-log-user">
            <Form.Label>
              {t('adminRetrieval.searchLog.filters.userUuid')}
            </Form.Label>
            <Form.Control
              isInvalid={!!errors.userUuid}
              autoComplete="off"
              {...register('userUuid')}
            />
            <Form.Control.Feedback type="invalid">
              {errors.userUuid?.message}
            </Form.Control.Feedback>
          </Form.Group>
          {dateField('since')}
          {dateField('until')}
          <Col xs="auto" lg={2}>
            <IconButton
              variant="orkestra-primary"
              type="submit"
              icon={faFilter}
              disabled={isFetching}
            >
              {t('adminRetrieval.actions.apply')}
            </IconButton>
          </Col>
        </Row>
      </Form>

      {!data && isFetching ? (
        <Spinner animation="border" size="sm" />
      ) : stepUpDismissed ? (
        <Alert
          variant="info"
          className="d-flex flex-wrap align-items-center justify-content-between gap-2 fs-10 mb-0"
        >
          <span>{t('adminRetrieval.searchLog.stepUpNeeded')}</span>
          <Button
            variant="orkestra-default"
            size="sm"
            onClick={() => refetch()}
          >
            {t('adminRetrieval.actions.verify')}
          </Button>
        </Alert>
      ) : error || !data ? (
        <RetrievalLoadError onRetry={() => refetch()} />
      ) : data.items.length === 0 ? (
        <EmptyState
          icon={faClockRotateLeft}
          message={t('adminRetrieval.searchLog.empty')}
        />
      ) : (
        <AdvanceTableProvider {...table}>
          <AdvanceTable
            headerClassName="text-nowrap align-middle"
            rowClassName="align-middle"
            tableProps={{
              size: 'sm',
              striped: true,
              className: 'fs-10 mb-0 overflow-hidden'
            }}
          />
          <div className="mt-3">
            <AdvanceTableFooter rowInfo navButtons />
          </div>
        </AdvanceTableProvider>
      )}
    </>
  );
};

export default SearchLogTab;
