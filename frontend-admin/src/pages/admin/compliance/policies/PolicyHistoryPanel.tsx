import { Spinner } from 'react-bootstrap';
import { faClockRotateLeft } from '@fortawesome/free-solid-svg-icons';
import type { CellContext, ColumnDef } from '@tanstack/react-table';
import { useTranslation } from 'react-i18next';
import SubtleBadge from 'components/common/SubtleBadge';
import { byTimestamp } from 'components/common/advance-table/sorting';
import {
  useListPolicyVersionsQuery,
  type PolicyVersion
} from 'store/api/complianceApi';
import EmptyState from 'components/common/EmptyState';
import SearchablePagedTable from 'components/common/advance-table/SearchablePagedTable';
import { formatDateTime } from '../complianceFormat';

// PolicyHistoryPanel lists the immutable versions: author, approver (four
// eyes) and reason of every change.
const PolicyHistoryPanel = ({ policyId }: { policyId: string }) => {
  const { t } = useTranslation();
  const { data, isLoading } = useListPolicyVersionsQuery(policyId);
  const mono = (v?: string) =>
    v ? <span className="font-monospace small">{v}</span> : '—';
  const columns: ColumnDef<PolicyVersion>[] = [
    {
      accessorKey: 'version',
      header: t('adminCompliance.history.columns.version'),
      meta: { headerProps: { className: 'text-900' } }
    },
    {
      accessorKey: 'changeKind',
      header: t('adminCompliance.history.columns.kind'),
      meta: { headerProps: { className: 'text-900' } },
      cell: ({ row: { original } }: CellContext<PolicyVersion, unknown>) => (
        <SubtleBadge
          pill
          bg={original.changeKind === 'delete' ? 'danger' : 'info'}
        >
          {t(`adminCompliance.history.kinds.${original.changeKind}`)}
        </SubtleBadge>
      )
    },
    {
      accessorKey: 'changedBy',
      header: t('adminCompliance.history.columns.changedBy'),
      meta: { headerProps: { className: 'text-900' } },
      cell: ({ row: { original } }: CellContext<PolicyVersion, unknown>) =>
        mono(original.changedBy)
    },
    {
      accessorKey: 'approvedBy',
      header: t('adminCompliance.history.columns.approvedBy'),
      meta: { headerProps: { className: 'text-900' } },
      cell: ({ row: { original } }: CellContext<PolicyVersion, unknown>) =>
        mono(original.approvedBy)
    },
    {
      id: 'changedAt',
      accessorFn: v => formatDateTime(v.changedAt),
      sortingFn: byTimestamp<PolicyVersion>(v => v.changedAt),
      header: t('adminCompliance.history.columns.changedAt'),
      meta: { headerProps: { className: 'text-900' } }
    },
    {
      accessorKey: 'reason',
      header: t('adminCompliance.history.columns.reason'),
      meta: { headerProps: { className: 'text-900' } }
    }
  ];
  if (isLoading) return <Spinner animation="border" size="sm" />;
  const items = data?.items ?? [];
  return items.length === 0 ? (
    <EmptyState
      icon={faClockRotateLeft}
      message={t('adminCompliance.history.emptyMessage')}
    />
  ) : (
    <SearchablePagedTable
      data={items}
      columns={columns}
      searchPlaceholder={t('adminCompliance.history.searchPlaceholder')}
    />
  );
};

export default PolicyHistoryPanel;
