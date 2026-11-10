import { useEffect, useState } from 'react';
import {
  Button,
  Form,
  OverlayTrigger,
  Spinner,
  Tooltip
} from 'react-bootstrap';
import { useSearchParams } from 'react-router';
import { faUserSlash } from '@fortawesome/free-solid-svg-icons';
import type { CellContext, ColumnDef } from '@tanstack/react-table';
import { useTranslation } from 'react-i18next';
import { toast } from 'react-toastify';
import { useAuth } from 'hooks/auth/useAuthRTK';
import IconButton from 'components/common/IconButton';
import SubtleBadge from 'components/common/SubtleBadge';
import { byTimestamp } from 'components/common/advance-table/sorting';
import {
  useApproveErasureRequestMutation,
  useCloseErasureRequestMutation,
  useLazyListErasureRequestsQuery,
  useOpenErasureRequestMutation,
  useExecuteErasureRequestMutation,
  useRejectErasureRequestMutation,
  type ErasureListStatus,
  type ErasureRequest
} from 'store/api/complianceApi';
import EmptyState from 'components/common/EmptyState';
import SearchablePagedTable from 'components/common/advance-table/SearchablePagedTable';
import { erasureStatusColor, formatDateTime } from './complianceFormat';
import ReasonModal from './policies/ReasonModal';
import ApproveErasureModal from './ApproveErasureModal';
import ExecuteErasureModal from './ExecuteErasureModal';
import ErasureReceiptModal from './ErasureReceiptModal';
import OpenErasureRequestModal from './OpenErasureRequestModal';
import { erasureErrorMessage } from './erasureErrors';
import { useOpenErasureRequests } from './useOpenErasureRequests';

// ErasureRequestsTab drives the GDPR right-to-erasure queue: operators execute
// a hard delete (step-up-gated on the backend, may be blocked by a legal hold)
// or reject the request. A request some producer has not finished stays
// incomplete (ADR-0023 D12): the operator retries it, or, when a producer is no
// longer installed, closes it as partial with a mandatory note. The list
// refreshes while the retry worker may change a request (useOpenErasureRequests).
// Destructive 401s are replayed by the global StepUpModal.
const STATUSES: ErasureListStatus[] = [
  'open',
  'completed',
  'rejected',
  'closed_partial',
  'all'
];

const ErasureRequestsTab = () => {
  const { t } = useTranslation();
  const { currentUser } = useAuth();
  const me = currentUser?.id;
  const [searchParams, setSearchParams] = useSearchParams();
  const rawStatus = searchParams.get('status') as ErasureListStatus | null;
  const status: ErasureListStatus =
    rawStatus && STATUSES.includes(rawStatus) ? rawStatus : 'open';
  const setStatus = (next: ErasureListStatus) =>
    setSearchParams(
      prev => {
        const sp = new URLSearchParams(prev);
        if (next === 'open') sp.delete('status');
        else sp.set('status', next);
        return sp;
      },
      { replace: true }
    );

  const { data, isLoading } = useOpenErasureRequests({ status });
  const [fetchMore, { isFetching: loadingMore }] =
    useLazyListErasureRequestsQuery();
  // Pages after the first, appended client-side; dropped whenever the first
  // page is refetched (its data changed) or the filter changes, so a stale
  // tail never sits under a fresh head.
  const [more, setMore] = useState<{
    items: ErasureRequest[];
    next?: string;
  } | null>(null);
  useEffect(() => setMore(null), [status, data]);
  const onLoadMore = async () => {
    const cursor = more ? more.next : data?.next;
    if (!cursor) return;
    try {
      const page = await fetchMore({ status, after: cursor }).unwrap();
      setMore(prev => ({
        items: [...(prev?.items ?? []), ...page.items],
        next: page.next
      }));
    } catch (err) {
      toast.error(
        erasureErrorMessage(err, t, t('adminCompliance.erasure.loadMoreError'))
      );
    }
  };
  const next = more ? more.next : data?.next;

  const [execute] = useExecuteErasureRequestMutation();
  const [reject, { isLoading: rejecting }] = useRejectErasureRequestMutation();
  const [rejectTarget, setRejectTarget] = useState<ErasureRequest | null>(null);
  const [close, { isLoading: closing }] = useCloseErasureRequestMutation();
  const [openRequest, { isLoading: opening }] = useOpenErasureRequestMutation();
  const [approve, { isLoading: approving }] =
    useApproveErasureRequestMutation();
  const [closeTarget, setCloseTarget] = useState<ErasureRequest | null>(null);
  const [approveTarget, setApproveTarget] = useState<ErasureRequest | null>(
    null
  );
  const [executeTarget, setExecuteTarget] = useState<ErasureRequest | null>(
    null
  );
  const [showOpen, setShowOpen] = useState(false);
  const [receiptId, setReceiptId] = useState<string | null>(null);

  const onExecute = async (id: string, mode?: string) => {
    try {
      const result = await execute({ id, mode }).unwrap();
      if (result.status === 'incomplete') {
        toast.warning(t('adminCompliance.erasure.executeIncomplete'));
      } else {
        toast.success(t('adminCompliance.erasure.executeSuccess'));
      }
    } catch (err: unknown) {
      const msg = erasureErrorMessage(
        err,
        t,
        t('adminCompliance.erasure.executeError')
      );
      if (
        (err as { data?: { code?: string } })?.data?.code ===
        'compliance.erasure_request_busy'
      ) {
        toast.warning(msg);
        return;
      }
      toast.error(msg);
    }
  };
  const onReject = async (note: string) => {
    if (!rejectTarget) return;
    try {
      await reject({ id: rejectTarget.uuid, note }).unwrap();
      toast.success(t('adminCompliance.erasure.rejectSuccess'));
      setRejectTarget(null);
    } catch (err) {
      toast.error(
        erasureErrorMessage(err, t, t('adminCompliance.erasure.rejectError'))
      );
    }
  };

  const onClose = async (note: string) => {
    if (!closeTarget) return;
    try {
      await close({ id: closeTarget.uuid, note }).unwrap();
      toast.success(t('adminCompliance.erasure.closeSuccess'));
      setCloseTarget(null);
    } catch (err) {
      toast.error(
        erasureErrorMessage(err, t, t('adminCompliance.erasure.closeError'))
      );
    }
  };

  const onOpen = async (v: {
    userUuid: string;
    mode: string;
    reason: string;
  }) => {
    try {
      await openRequest(v).unwrap();
      toast.success(t('adminCompliance.erasure.open.success'));
      setShowOpen(false);
    } catch (err) {
      toast.error(
        erasureErrorMessage(err, t, t('adminCompliance.erasure.open.error'))
      );
    }
  };

  const onApprove = async (v: { mode: string; note: string }) => {
    if (!approveTarget) return;
    try {
      await approve({ id: approveTarget.uuid, ...v }).unwrap();
      toast.success(t('adminCompliance.erasure.approve.success'));
      setApproveTarget(null);
    } catch (err) {
      toast.error(
        erasureErrorMessage(err, t, t('adminCompliance.erasure.approve.error'))
      );
    }
  };

  const columns: ColumnDef<ErasureRequest>[] = [
    {
      accessorKey: 'userUuid',
      header: t('adminCompliance.erasure.columns.subject'),
      meta: { headerProps: { className: 'text-900' } },
      cell: ({ row: { original } }: CellContext<ErasureRequest, unknown>) => (
        <>
          <span className="font-monospace small">{original.userUuid}</span>
          {(original.source === 'self_service' ||
            original.source === 'retention' ||
            original.source === 'admin') && (
            <div className="text-muted fs-11">
              {t(`adminCompliance.erasure.source.${original.source}`)}
            </div>
          )}
        </>
      )
    },
    {
      accessorKey: 'reason',
      header: t('adminCompliance.erasure.columns.reason'),
      meta: { headerProps: { className: 'text-900' } },
      cell: ({ row: { original } }: CellContext<ErasureRequest, unknown>) =>
        original.reason || '—'
    },
    {
      accessorKey: 'status',
      header: t('adminCompliance.erasure.columns.status'),
      meta: { headerProps: { className: 'text-900' } },
      cell: ({ row: { original } }: CellContext<ErasureRequest, unknown>) => (
        <>
          <SubtleBadge pill bg={erasureStatusColor(original.status)}>
            {t(`adminCompliance.status.${original.status}`, {
              defaultValue: original.status
            })}
          </SubtleBadge>
          {original.status === 'incomplete' && (
            <div className="text-muted fs-11 mt-1">
              <div>
                {t('adminCompliance.erasure.progress', {
                  done: (original.progress ?? []).filter(
                    p => p.outcome === 'done'
                  ).length,
                  total: (original.progress ?? []).length
                })}
              </div>
              <div>
                {original.nextAttemptAt
                  ? t('adminCompliance.erasure.nextRetry', {
                      date: formatDateTime(original.nextAttemptAt)
                    })
                  : t('adminCompliance.erasure.retriesExhausted')}
              </div>
              {(original.lastError === 'legal_hold_active' ||
                original.lastError === 'run_failed' ||
                original.lastError === 'retention_minimum') && (
                <div className="text-warning">
                  {t(`adminCompliance.erasure.lastError.${original.lastError}`)}
                </div>
              )}
            </div>
          )}
        </>
      )
    },
    {
      id: 'approvals',
      header: t('adminCompliance.erasure.columns.approvals'),
      enableSorting: false,
      meta: { headerProps: { className: 'text-900' } },
      cell: ({ row: { original } }: CellContext<ErasureRequest, unknown>) =>
        `${(original.approvals ?? []).length}/${original.requiredApprovals || 1}`
    },
    {
      id: 'requestedAt',
      // Formatted accessor + timestamp comparator — see byTimestamp.
      accessorFn: r => formatDateTime(r.requestedAt),
      sortingFn: byTimestamp<ErasureRequest>(r => r.requestedAt),
      header: t('adminCompliance.erasure.columns.requested'),
      meta: { headerProps: { className: 'text-900' } },
      cell: ({ row: { original } }: CellContext<ErasureRequest, unknown>) =>
        formatDateTime(original.requestedAt)
    },
    {
      id: 'actions',
      header: t('adminCompliance.erasure.columns.actions'),
      enableSorting: false,
      meta: {
        headerProps: { className: 'text-end text-900' },
        cellProps: { className: 'text-end' }
      },
      cell: ({ row: { original } }: CellContext<ErasureRequest, unknown>) =>
        original.status === 'incomplete' ? (
          <>
            <IconButton
              size="sm"
              variant="outline-danger"
              icon="redo"
              className="me-2"
              onClick={() =>
                onExecute(original.uuid, original.mode ?? 'hard_delete')
              }
            >
              {t('adminCompliance.erasure.retry')}
            </IconButton>
            {original.closable && (
              <IconButton
                size="sm"
                variant="outline-secondary"
                icon="times"
                onClick={() => setCloseTarget(original)}
              >
                {t('adminCompliance.erasure.close')}
              </IconButton>
            )}
          </>
        ) : original.status === 'pending' ? (
          <PendingActions
            request={original}
            me={me}
            onApprove={() => setApproveTarget(original)}
            onExecute={() => setExecuteTarget(original)}
            onReject={() => setRejectTarget(original)}
          />
        ) : original.status === 'completed' ||
          original.status === 'closed_partial' ? (
          <IconButton
            size="sm"
            variant="outline-secondary"
            icon="file-alt"
            onClick={() => setReceiptId(original.uuid)}
          >
            {t('adminCompliance.erasure.receipt.open')}
          </IconButton>
        ) : null
    }
  ];

  const first = data?.items ?? [];
  const seen = new Set(first.map(r => r.uuid));
  const items = [
    ...first,
    ...(more?.items ?? []).filter(r => !seen.has(r.uuid))
  ];

  // No SectionCard shell: the pane sits under the page's card-header-tabs,
  // and the active tab already names this section.
  return (
    <>
      <div className="d-flex align-items-center justify-content-between gap-2 mb-3">
        <Form.Select
          size="sm"
          className="w-auto"
          aria-label={t('adminCompliance.erasure.filter.label')}
          value={status}
          onChange={e => setStatus(e.target.value as ErasureListStatus)}
        >
          {STATUSES.map(s => (
            <option key={s} value={s}>
              {t(`adminCompliance.erasure.filter.${s}`)}
            </option>
          ))}
        </Form.Select>
        <Button
          size="sm"
          variant="orkestra-primary"
          onClick={() => setShowOpen(true)}
        >
          {t('adminCompliance.erasure.open.button')}
        </Button>
      </div>
      {isLoading ? (
        <Spinner animation="border" size="sm" className="mt-2" />
      ) : items.length === 0 ? (
        <EmptyState
          icon={faUserSlash}
          message={t(
            status === 'open'
              ? 'adminCompliance.erasure.emptyMessage'
              : 'adminCompliance.erasure.emptyFiltered'
          )}
          hint={t('adminCompliance.erasure.emptyHint')}
        />
      ) : (
        <>
          <SearchablePagedTable
            data={items}
            columns={columns}
            searchPlaceholder={t('adminCompliance.erasure.searchPlaceholder')}
          />
          {next && (
            <div className="text-center mt-2">
              <Button
                size="sm"
                variant="orkestra-default"
                disabled={loadingMore}
                onClick={onLoadMore}
              >
                {t('adminCompliance.erasure.loadMore')}
              </Button>
            </div>
          )}
        </>
      )}
      <ReasonModal
        show={closeTarget !== null}
        title={t('adminCompliance.erasure.closeTitle')}
        body={t('adminCompliance.erasure.closeBody', {
          keys: (closeTarget?.progress ?? [])
            .filter(p => p.outcome !== 'done')
            .map(p => p.key)
            .join(', ')
        })}
        confirmLabel={t('adminCompliance.erasure.close')}
        busy={closing}
        onCancel={() => setCloseTarget(null)}
        onConfirm={onClose}
      />
      <ReasonModal
        show={rejectTarget !== null}
        title={t('adminCompliance.erasure.rejectTitle')}
        body={t('adminCompliance.erasure.rejectBody')}
        confirmLabel={t('adminCompliance.erasure.reject')}
        busy={rejecting}
        onCancel={() => setRejectTarget(null)}
        onConfirm={onReject}
      />
      <OpenErasureRequestModal
        show={showOpen}
        busy={opening}
        onCancel={() => setShowOpen(false)}
        onSubmit={onOpen}
      />
      <ApproveErasureModal
        show={approveTarget !== null}
        busy={approving}
        lockedMode={approveTarget?.mode || undefined}
        onCancel={() => setApproveTarget(null)}
        onConfirm={onApprove}
      />
      <ExecuteErasureModal
        show={executeTarget !== null}
        busy={false}
        lockedMode={executeTarget?.mode || undefined}
        onCancel={() => setExecuteTarget(null)}
        onConfirm={async mode => {
          const target = executeTarget;
          setExecuteTarget(null);
          if (target) await onExecute(target.uuid, mode);
        }}
      />
      <ErasureReceiptModal
        requestId={receiptId}
        onClose={() => setReceiptId(null)}
      />
    </>
  );
};

// PendingActions: approve, execute and reject for a request awaiting review.
// Execute is shown disabled with a k/N tooltip while the approvals plus the
// operator's own would stay below the requirement; the backend decides anyway.
const PendingActions = ({
  request,
  me,
  onApprove,
  onExecute,
  onReject
}: {
  request: ErasureRequest;
  me?: string;
  onApprove: () => void;
  onExecute: () => void;
  onReject: () => void;
}) => {
  const { t } = useTranslation();
  const need = request.requiredApprovals || 1;
  const by = new Set((request.approvals ?? []).map(a => a.by));
  const have = by.size;
  const effective = me ? new Set([...by, me]).size : have;
  const blocked = effective < need;
  const execute = (
    <IconButton
      size="sm"
      variant="outline-danger"
      icon="trash-alt"
      className="me-2"
      disabled={blocked}
      onClick={onExecute}
    >
      {t('adminCompliance.erasure.execute')}
    </IconButton>
  );
  return (
    <>
      {need > 1 && !(me && by.has(me)) && (
        <IconButton
          size="sm"
          variant="outline-primary"
          icon="check"
          className="me-2"
          onClick={onApprove}
        >
          {t('adminCompliance.erasure.approve.button')}
        </IconButton>
      )}
      {blocked ? (
        <OverlayTrigger
          overlay={
            <Tooltip id={`exec-${request.uuid}`}>
              {t('adminCompliance.erasure.approvalsNeeded', {
                have: effective,
                need
              })}
            </Tooltip>
          }
        >
          <span className="d-inline-block">{execute}</span>
        </OverlayTrigger>
      ) : (
        execute
      )}
      <IconButton
        size="sm"
        variant="outline-secondary"
        icon="ban"
        onClick={onReject}
      >
        {t('adminCompliance.erasure.reject')}
      </IconButton>
    </>
  );
};

export default ErasureRequestsTab;
