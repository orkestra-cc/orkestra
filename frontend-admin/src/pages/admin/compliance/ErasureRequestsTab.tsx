import { Spinner } from 'react-bootstrap';
import { faUserSlash } from '@fortawesome/free-solid-svg-icons';
import type { CellContext, ColumnDef } from '@tanstack/react-table';
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
import ComplianceSection from './ComplianceSection';
import ComplianceTable from './ComplianceTable';
import { erasureStatusColor, formatDateTime } from './complianceFormat';

// ErasureRequestsTab drives the GDPR right-to-erasure queue: operators execute
// a hard delete (step-up-gated on the backend, may be blocked by a legal hold)
// or reject the request. Destructive 401s are replayed by the global StepUpModal.
const ErasureRequestsTab = () => {
  const { data, isLoading } = useListErasureRequestsQuery();
  const [execute] = useExecuteErasureRequestMutation();
  const [reject] = useRejectErasureRequestMutation();

  const onExecute = async (id: string) => {
    try {
      await execute({ id, mode: 'hard_delete' }).unwrap();
      toast.success('Erasure executed');
    } catch {
      toast.error('Execute failed (or blocked by a legal hold)');
    }
  };
  const onReject = async (id: string) => {
    try {
      await reject({ id }).unwrap();
      toast.success('Request rejected');
    } catch {
      toast.error('Reject failed');
    }
  };

  const columns: ColumnDef<ErasureRequest>[] = [
    {
      accessorKey: 'userUuid',
      header: 'Subject',
      meta: { headerProps: { className: 'text-900' } },
      cell: ({ row: { original } }: CellContext<ErasureRequest, unknown>) => (
        <span className="font-monospace small">{original.userUuid}</span>
      )
    },
    {
      accessorKey: 'reason',
      header: 'Reason',
      meta: { headerProps: { className: 'text-900' } },
      cell: ({ row: { original } }: CellContext<ErasureRequest, unknown>) =>
        original.reason || '—'
    },
    {
      accessorKey: 'status',
      header: 'Status',
      meta: { headerProps: { className: 'text-900' } },
      cell: ({ row: { original } }: CellContext<ErasureRequest, unknown>) => (
        <SubtleBadge pill bg={erasureStatusColor(original.status)}>
          {original.status}
        </SubtleBadge>
      )
    },
    {
      accessorKey: 'requestedAt',
      header: 'Requested',
      meta: { headerProps: { className: 'text-900' } },
      cell: ({ row: { original } }: CellContext<ErasureRequest, unknown>) =>
        formatDateTime(original.requestedAt)
    },
    {
      id: 'actions',
      header: 'Actions',
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
            Execute
          </IconButton>
          <IconButton
            size="sm"
            variant="outline-secondary"
            icon="ban"
            onClick={() => onReject(original.uuid)}
          >
            Reject
          </IconButton>
        </>
      )
    }
  ];

  const items = data?.items ?? [];

  return (
    <ComplianceSection
      icon={faUserSlash}
      iconColor="warning"
      title="Erasure Requests"
    >
      {isLoading ? (
        <Spinner animation="border" size="sm" className="mt-2" />
      ) : items.length === 0 ? (
        <ComplianceEmptyState
          icon={faUserSlash}
          message="No pending erasure requests."
          hint="Data-subject erasure requests awaiting review will appear here."
        />
      ) : (
        <ComplianceTable
          data={items}
          columns={columns}
          searchPlaceholder="Search by subject or reason"
        />
      )}
    </ComplianceSection>
  );
};

export default ErasureRequestsTab;
