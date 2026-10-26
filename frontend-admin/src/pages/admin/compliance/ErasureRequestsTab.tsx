import { useState } from 'react';
import { Spinner } from 'react-bootstrap';
import { faUserSlash } from '@fortawesome/free-solid-svg-icons';
import type { CellContext, ColumnDef } from '@tanstack/react-table';
import { useTranslation } from 'react-i18next';
import { toast } from 'react-toastify';
import IconButton from 'components/common/IconButton';
import SubtleBadge from 'components/common/SubtleBadge';
import { byTimestamp } from 'components/common/advance-table/sorting';
import {
  useCloseErasureRequestMutation,
  useExecuteErasureRequestMutation,
  useListErasureRequestsQuery,
  useRejectErasureRequestMutation,
  type ErasureRequest
} from 'store/api/complianceApi';
import EmptyState from 'components/common/EmptyState';
import SearchablePagedTable from 'components/common/advance-table/SearchablePagedTable';
import { erasureStatusColor, formatDateTime } from './complianceFormat';
import ReasonModal from './policies/ReasonModal';

// ErasureRequestsTab drives the GDPR right-to-erasure queue: operators execute
// a hard delete (step-up-gated on the backend, may be blocked by a legal hold)
// or reject the request. A request some producer has not finished stays
// incomplete (ADR-0023 D12): the operator retries it, or, when a producer is no
// longer installed, closes it as partial with a mandatory note. Destructive 401s are replayed by the global StepUpModal.
const ErasureRequestsTab = () => {
  const { t } = useTranslation();
  const { data, isLoading } = useListErasureRequestsQuery();
  const [execute] = useExecuteErasureRequestMutation();
  const [reject] = useRejectErasureRequestMutation();
  const [close, { isLoading: closing }] = useCloseErasureRequestMutation();
  const [closeTarget, setCloseTarget] = useState<ErasureRequest | null>(null);

  const onExecute = async (id: string, mode: string) => {
    try {
      const result = await execute({ id, mode }).unwrap();
      if (result.status === 'incomplete') {
        toast.warning(t('adminCompliance.erasure.executeIncomplete'));
      } else {
        toast.success(t('adminCompliance.erasure.executeSuccess'));
      }
    } catch (err: unknown) {
      const e = err as { data?: { code?: string } };
      if (e?.data?.code === 'compliance.erasure_request_busy') {
        toast.warning(t('adminCompliance.erasure.busy'));
        return;
      }
      toast.error(t('adminCompliance.erasure.executeError'));
    }
  };
  const onReject = async (id: string) => {
    try {
      await reject({ id }).unwrap();
      toast.success(t('adminCompliance.erasure.rejectSuccess'));
    } catch {
      toast.error(t('adminCompliance.erasure.rejectError'));
    }
  };

  const onClose = async (note: string) => {
    if (!closeTarget) return;
    try {
      await close({ id: closeTarget.uuid, note }).unwrap();
      toast.success(t('adminCompliance.erasure.closeSuccess'));
      setCloseTarget(null);
    } catch {
      toast.error(t('adminCompliance.erasure.closeError'));
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
            original.source === 'retention') && (
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
                original.lastError === 'run_failed') && (
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
        ) : (
          <>
            <IconButton
              size="sm"
              variant="outline-danger"
              icon="trash-alt"
              className="me-2"
              onClick={() => onExecute(original.uuid, 'hard_delete')}
            >
              {t('adminCompliance.erasure.execute')}
            </IconButton>
            <IconButton
              size="sm"
              variant="outline-secondary"
              icon="ban"
              onClick={() => onReject(original.uuid)}
            >
              {t('adminCompliance.erasure.reject')}
            </IconButton>
          </>
        )
    }
  ];

  const items = data?.items ?? [];

  // No SectionCard shell: the pane sits under the page's card-header-tabs,
  // and the active tab already names this section.
  return isLoading ? (
    <Spinner animation="border" size="sm" className="mt-2" />
  ) : items.length === 0 ? (
    <EmptyState
      icon={faUserSlash}
      message={t('adminCompliance.erasure.emptyMessage')}
      hint={t('adminCompliance.erasure.emptyHint')}
    />
  ) : (
    <>
      <SearchablePagedTable
        data={items}
        columns={columns}
        searchPlaceholder={t('adminCompliance.erasure.searchPlaceholder')}
      />
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
    </>
  );
};

export default ErasureRequestsTab;
