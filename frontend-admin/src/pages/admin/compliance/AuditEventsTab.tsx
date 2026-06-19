import { Spinner } from 'react-bootstrap';
import { faClipboardList } from '@fortawesome/free-solid-svg-icons';
import type { CellContext, ColumnDef } from '@tanstack/react-table';
import SubtleBadge from 'components/common/SubtleBadge';
import {
  useListAuditEventsQuery,
  type AuditEvent
} from 'store/api/complianceApi';
import ComplianceEmptyState from './ComplianceEmptyState';
import SectionCard from 'components/common/SectionCard';
import ComplianceTable from './ComplianceTable';
import { formatDateTime, outcomeColor } from './complianceFormat';

// AuditEventsTab renders the immutable audit trail (latest 50 events). Read-only.
const AuditEventsTab = () => {
  const { data, isLoading } = useListAuditEventsQuery({ limit: 50 });

  const columns: ColumnDef<AuditEvent>[] = [
    {
      accessorKey: 'timestamp',
      header: 'Time',
      meta: { headerProps: { className: 'text-900' } },
      cell: ({ row: { original } }: CellContext<AuditEvent, unknown>) =>
        formatDateTime(original.timestamp)
    },
    {
      accessorKey: 'action',
      header: 'Action',
      meta: { headerProps: { className: 'text-900' } },
      cell: ({ row: { original } }: CellContext<AuditEvent, unknown>) => (
        <span className="font-monospace small">{original.action}</span>
      )
    },
    {
      id: 'actor',
      header: 'Actor',
      meta: { headerProps: { className: 'text-900' } },
      cell: ({ row: { original } }: CellContext<AuditEvent, unknown>) => (
        <span className="small">
          {original.actorEmail || original.actorUserId || original.actorType}
        </span>
      )
    },
    {
      id: 'resource',
      header: 'Resource',
      meta: { headerProps: { className: 'text-900' } },
      cell: ({ row: { original } }: CellContext<AuditEvent, unknown>) => (
        <span className="small">
          {original.resourceType}
          {original.resourceId ? `/${original.resourceId}` : ''}
        </span>
      )
    },
    {
      accessorKey: 'outcome',
      header: 'Outcome',
      meta: { headerProps: { className: 'text-900' } },
      cell: ({ row: { original } }: CellContext<AuditEvent, unknown>) => (
        <SubtleBadge pill bg={outcomeColor(original.outcome)}>
          {original.outcome}
        </SubtleBadge>
      )
    }
  ];

  const items = data?.items ?? [];

  return (
    <SectionCard
      icon={faClipboardList}
      iconColor="primary"
      title="Audit Events"
    >
      {isLoading ? (
        <Spinner animation="border" size="sm" className="mt-2" />
      ) : items.length === 0 ? (
        <ComplianceEmptyState
          icon={faClipboardList}
          message="No audit events."
          hint="Security-relevant actions across the platform are recorded here."
        />
      ) : (
        <ComplianceTable
          data={items}
          columns={columns}
          searchPlaceholder="Search by action, actor or resource"
        />
      )}
    </SectionCard>
  );
};

export default AuditEventsTab;
