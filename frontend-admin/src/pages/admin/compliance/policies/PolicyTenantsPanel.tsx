import { Link } from 'react-router';
import { faBuilding } from '@fortawesome/free-solid-svg-icons';
import type { CellContext, ColumnDef } from '@tanstack/react-table';
import { useTranslation } from 'react-i18next';
import SubtleBadge from 'components/common/SubtleBadge';
import { byTimestamp } from 'components/common/advance-table/sorting';
import type { PolicyAssignmentView } from 'store/api/complianceApi';
import ComplianceEmptyState from '../ComplianceEmptyState';
import ComplianceTable from '../ComplianceTable';
import { formatDateTime } from '../complianceFormat';

// PolicyTenantsPanel lists the tenants under the policy, linking each to its
// detail page (clients for Tier-2, internal tenants for Tier-1).
const PolicyTenantsPanel = ({
  assignments
}: {
  assignments: PolicyAssignmentView[];
}) => {
  const { t } = useTranslation();
  const columns: ColumnDef<PolicyAssignmentView>[] = [
    {
      id: 'tenant',
      accessorFn: a => a.tenantName || a.tenantId,
      header: t('adminCompliance.tenantsPanel.columns.tenant'),
      meta: { headerProps: { className: 'text-900' } },
      cell: ({
        row: { original }
      }: CellContext<PolicyAssignmentView, unknown>) => (
        <Link
          to={`/admin/${original.tenantKind === 'external' ? 'clients' : 'internal-tenants'}/${encodeURIComponent(original.tenantId)}`}
        >
          {original.tenantName || original.tenantId}
        </Link>
      )
    },
    {
      accessorKey: 'tenantKind',
      header: t('adminCompliance.tenantsPanel.columns.kind'),
      meta: { headerProps: { className: 'text-900' } },
      cell: ({
        row: { original }
      }: CellContext<PolicyAssignmentView, unknown>) => (
        <SubtleBadge
          pill
          bg={original.tenantKind === 'external' ? 'info' : 'secondary'}
        >
          {t(`adminCompliance.tenantsPanel.kinds.${original.tenantKind}`)}
        </SubtleBadge>
      )
    },
    {
      accessorKey: 'assignedBy',
      header: t('adminCompliance.tenantsPanel.columns.assignedBy'),
      meta: { headerProps: { className: 'text-900' } },
      cell: ({
        row: { original }
      }: CellContext<PolicyAssignmentView, unknown>) => (
        <span className="font-monospace small">{original.assignedBy}</span>
      )
    },
    {
      id: 'assignedAt',
      accessorFn: a => formatDateTime(a.assignedAt),
      sortingFn: byTimestamp<PolicyAssignmentView>(a => a.assignedAt),
      header: t('adminCompliance.tenantsPanel.columns.assignedAt'),
      meta: { headerProps: { className: 'text-900' } }
    },
    {
      accessorKey: 'reason',
      header: t('adminCompliance.tenantsPanel.columns.reason'),
      meta: { headerProps: { className: 'text-900' } }
    }
  ];
  return assignments.length === 0 ? (
    <ComplianceEmptyState
      icon={faBuilding}
      message={t('adminCompliance.tenantsPanel.emptyMessage')}
    />
  ) : (
    <ComplianceTable
      data={assignments}
      columns={columns}
      searchPlaceholder={t('adminCompliance.tenantsPanel.searchPlaceholder')}
    />
  );
};

export default PolicyTenantsPanel;
