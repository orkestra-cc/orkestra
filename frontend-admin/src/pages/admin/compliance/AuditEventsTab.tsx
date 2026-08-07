import { Spinner } from 'react-bootstrap';
import { faClipboardList } from '@fortawesome/free-solid-svg-icons';
import type { CellContext, ColumnDef } from '@tanstack/react-table';
import { useTranslation } from 'react-i18next';
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
  const { t } = useTranslation();
  const { data, isLoading } = useListAuditEventsQuery({ limit: 50 });

  const columns: ColumnDef<AuditEvent>[] = [
    {
      accessorKey: 'timestamp',
      header: t('adminCompliance.audit.columns.time'),
      meta: { headerProps: { className: 'text-900' } },
      cell: ({ row: { original } }: CellContext<AuditEvent, unknown>) =>
        formatDateTime(original.timestamp)
    },
    {
      accessorKey: 'action',
      header: t('adminCompliance.audit.columns.action'),
      meta: { headerProps: { className: 'text-900' } },
      cell: ({ row: { original } }: CellContext<AuditEvent, unknown>) => (
        <span className="font-monospace small">{original.action}</span>
      )
    },
    {
      id: 'actor',
      header: t('adminCompliance.audit.columns.actor'),
      meta: { headerProps: { className: 'text-900' } },
      cell: ({ row: { original } }: CellContext<AuditEvent, unknown>) => (
        <span className="small">
          {original.actorEmail || original.actorUserId || original.actorType}
        </span>
      )
    },
    {
      id: 'resource',
      header: t('adminCompliance.audit.columns.resource'),
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
      header: t('adminCompliance.audit.columns.outcome'),
      meta: { headerProps: { className: 'text-900' } },
      cell: ({ row: { original } }: CellContext<AuditEvent, unknown>) => (
        <SubtleBadge pill bg={outcomeColor(original.outcome)}>
          {t(`adminCompliance.status.${original.outcome}`, {
            defaultValue: original.outcome
          })}
        </SubtleBadge>
      )
    }
  ];

  const items = data?.items ?? [];

  return (
    <SectionCard
      icon={faClipboardList}
      iconColor="primary"
      title={t('adminCompliance.audit.sectionTitle')}
    >
      {isLoading ? (
        <Spinner animation="border" size="sm" className="mt-2" />
      ) : items.length === 0 ? (
        <ComplianceEmptyState
          icon={faClipboardList}
          message={t('adminCompliance.audit.emptyMessage')}
          hint={t('adminCompliance.audit.emptyHint')}
        />
      ) : (
        <ComplianceTable
          data={items}
          columns={columns}
          searchPlaceholder={t('adminCompliance.audit.searchPlaceholder')}
        />
      )}
    </SectionCard>
  );
};

export default AuditEventsTab;
