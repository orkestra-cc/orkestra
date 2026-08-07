import { Spinner } from 'react-bootstrap';
import { faUserSlash } from '@fortawesome/free-solid-svg-icons';
import type { CellContext, ColumnDef } from '@tanstack/react-table';
import { useTranslation } from 'react-i18next';
import { toast } from 'react-toastify';
import IconButton from 'components/common/IconButton';
import SubtleBadge from 'components/common/SubtleBadge';
import {
  useExecuteErasureRequestMutation,
  useListErasureRequestsQuery,
  useRejectErasureRequestMutation,
  type ErasureRequest
} from 'store/api/complianceApi';
import ComplianceEmptyState from './ComplianceEmptyState';
import ComplianceTable from './ComplianceTable';
import { erasureStatusColor, formatDateTime } from './complianceFormat';

// ErasureRequestsTab drives the GDPR right-to-erasure queue: operators execute
// a hard delete (step-up-gated on the backend, may be blocked by a legal hold)
// or reject the request. Destructive 401s are replayed by the global StepUpModal.
const ErasureRequestsTab = () => {
  const { t } = useTranslation();
  const { data, isLoading } = useListErasureRequestsQuery();
  const [execute] = useExecuteErasureRequestMutation();
  const [reject] = useRejectErasureRequestMutation();

  const onExecute = async (id: string) => {
    try {
      await execute({ id, mode: 'hard_delete' }).unwrap();
      toast.success(t('adminCompliance.erasure.executeSuccess'));
    } catch {
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

  const columns: ColumnDef<ErasureRequest>[] = [
    {
      accessorKey: 'userUuid',
      header: t('adminCompliance.erasure.columns.subject'),
      meta: { headerProps: { className: 'text-900' } },
      cell: ({ row: { original } }: CellContext<ErasureRequest, unknown>) => (
        <span className="font-monospace small">{original.userUuid}</span>
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
        <SubtleBadge pill bg={erasureStatusColor(original.status)}>
          {t(`adminCompliance.status.${original.status}`, {
            defaultValue: original.status
          })}
        </SubtleBadge>
      )
    },
    {
      accessorKey: 'requestedAt',
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
      cell: ({ row: { original } }: CellContext<ErasureRequest, unknown>) => (
        <>
          <IconButton
            size="sm"
            variant="outline-danger"
            icon="trash-alt"
            className="me-2"
            onClick={() => onExecute(original.uuid)}
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
    <ComplianceEmptyState
      icon={faUserSlash}
      message={t('adminCompliance.erasure.emptyMessage')}
      hint={t('adminCompliance.erasure.emptyHint')}
    />
  ) : (
    <ComplianceTable
      data={items}
      columns={columns}
      searchPlaceholder={t('adminCompliance.erasure.searchPlaceholder')}
    />
  );
};

export default ErasureRequestsTab;
