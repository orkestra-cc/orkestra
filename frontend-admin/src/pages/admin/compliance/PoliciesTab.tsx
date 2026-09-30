import { Spinner } from 'react-bootstrap';
import { Link, useNavigate } from 'react-router';
import {
  faCopy,
  faPlus,
  faScaleBalanced
} from '@fortawesome/free-solid-svg-icons';
import type { CellContext, ColumnDef } from '@tanstack/react-table';
import { useTranslation } from 'react-i18next';
import IconButton from 'components/common/IconButton';
import SubtleBadge from 'components/common/SubtleBadge';
import { byTimestamp } from 'components/common/advance-table/sorting';
import {
  useListCompliancePoliciesQuery,
  type CompliancePolicyView
} from 'store/api/complianceApi';
import ComplianceEmptyState from './ComplianceEmptyState';
import ComplianceTable from './ComplianceTable';
import { formatDateTime } from './complianceFormat';

// PoliciesTab is the compliance policy catalog (spec §8): the platform policy
// and the tenant policies, flagged when less restrictive than the platform or
// past their review date. Editing happens on the detail page.
const PoliciesTab = () => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { data, isLoading } = useListCompliancePoliciesQuery();
  const items = data?.items ?? [];

  const columns: ColumnDef<CompliancePolicyView>[] = [
    {
      accessorKey: 'name',
      header: t('adminCompliance.policies.columns.name'),
      meta: { headerProps: { className: 'text-900' } },
      cell: ({
        row: { original }
      }: CellContext<CompliancePolicyView, unknown>) => (
        <>
          <Link
            to={`/admin/compliance/policies/${encodeURIComponent(original.uuid)}`}
            className="fw-semibold"
          >
            {original.name}
          </Link>
          {original.isPlatformDefault && (
            <SubtleBadge pill bg="primary" className="ms-2">
              {t('adminCompliance.policies.badges.platform')}
            </SubtleBadge>
          )}
          {original.lessRestrictiveFields.length > 0 && (
            <SubtleBadge pill bg="warning" className="ms-2">
              {t('adminCompliance.policies.badges.lessRestrictive')}
            </SubtleBadge>
          )}
          {original.reviewOverdue && (
            <SubtleBadge pill bg="danger" className="ms-2">
              {t('adminCompliance.policies.badges.reviewOverdue')}
            </SubtleBadge>
          )}
        </>
      )
    },
    {
      accessorKey: 'version',
      header: t('adminCompliance.policies.columns.version'),
      meta: { headerProps: { className: 'text-900' } }
    },
    {
      accessorKey: 'assignedTenants',
      header: t('adminCompliance.policies.columns.tenants'),
      meta: { headerProps: { className: 'text-900' } }
    },
    {
      id: 'ipAddress',
      accessorFn: p =>
        t(`adminCompliance.modes.ipAddress.${p.logContent.ipAddress}`),
      header: t('adminCompliance.policies.columns.ip'),
      meta: { headerProps: { className: 'text-900' } }
    },
    {
      id: 'updatedAt',
      accessorFn: p => formatDateTime(p.updatedAt),
      sortingFn: byTimestamp<CompliancePolicyView>(p => p.updatedAt),
      header: t('adminCompliance.policies.columns.updated'),
      meta: { headerProps: { className: 'text-900' } }
    },
    {
      id: 'actions',
      header: t('adminCompliance.policies.columns.actions'),
      enableSorting: false,
      meta: {
        headerProps: { className: 'text-end text-900' },
        cellProps: { className: 'text-end' }
      },
      cell: ({
        row: { original }
      }: CellContext<CompliancePolicyView, unknown>) => (
        <IconButton
          size="sm"
          variant="outline-secondary"
          icon={faCopy}
          onClick={() =>
            navigate(
              `/admin/compliance/policies/new?from=${encodeURIComponent(original.uuid)}`
            )
          }
        >
          {t('adminCompliance.policies.duplicate')}
        </IconButton>
      )
    }
  ];

  return (
    <>
      <div className="d-flex justify-content-end mb-3">
        <IconButton
          size="sm"
          variant="orkestra-primary"
          icon={faPlus}
          onClick={() => navigate('/admin/compliance/policies/new')}
        >
          {t('adminCompliance.policies.new')}
        </IconButton>
      </div>
      {isLoading ? (
        <Spinner animation="border" size="sm" className="mt-2" />
      ) : items.length === 0 ? (
        <ComplianceEmptyState
          icon={faScaleBalanced}
          message={t('adminCompliance.policies.emptyMessage')}
          hint={t('adminCompliance.policies.emptyHint')}
        />
      ) : (
        <ComplianceTable
          data={items}
          columns={columns}
          searchPlaceholder={t('adminCompliance.policies.searchPlaceholder')}
        />
      )}
    </>
  );
};

export default PoliciesTab;
