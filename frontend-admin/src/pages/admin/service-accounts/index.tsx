import { useMemo, useState } from 'react';
import { Alert, Card } from 'react-bootstrap';
import { Link } from 'react-router';
import { useTranslation } from 'react-i18next';
import { ColumnDef } from '@tanstack/react-table';
import PageHeader from 'components/common/PageHeader';
import SubtleBadge from 'components/common/SubtleBadge';
import IconButton from 'components/common/IconButton';
import AdvanceTable from 'components/common/advance-table/AdvanceTable';
import AdvanceTableFooter from 'components/common/advance-table/AdvanceTableFooter';
import useAdvanceTable from 'hooks/ui/useAdvanceTable';
import AdvanceTableProvider from 'providers/AdvanceTableProvider';
import { formatDate } from 'helpers/dateFormat';
import { useListServiceAccountsQuery } from 'store/api/serviceAccountApi';
import type { ServiceAccount } from 'types/serviceAccounts';
import CreateServiceAccountModal from './CreateServiceAccountModal';

// ServiceAccountsPage — ADR-0014 client-credentials identities admin
// surface. The API returns the whole array (no server pagination), so the
// table is client-side sortable + paginated like admin/users' UserTable.
// Detail route (name link target) lands in Task 5.
const ServiceAccountsPage: React.FC = () => {
  const { t } = useTranslation();
  const { data, isLoading, error } = useListServiceAccountsQuery();
  const [showCreate, setShowCreate] = useState(false);

  const columns = useMemo<ColumnDef<ServiceAccount>[]>(
    () => [
      {
        accessorKey: 'name',
        header: t('adminServiceAccounts.columns.name'),
        cell: ({ row: { original } }) => (
          <Link
            to={`/admin/service-accounts/${original.id}`}
            className="fw-semibold text-primary text-decoration-none"
          >
            {original.name}
          </Link>
        )
      },
      {
        accessorKey: 'email',
        header: t('adminServiceAccounts.columns.email')
      },
      {
        accessorKey: 'isActive',
        header: t('adminServiceAccounts.columns.status'),
        cell: ({ row: { original } }) => (
          <SubtleBadge bg={original.isActive ? 'success' : 'secondary'}>
            {original.isActive
              ? t('adminServiceAccounts.status.active')
              : t('adminServiceAccounts.status.inactive')}
          </SubtleBadge>
        )
      },
      {
        accessorKey: 'activeCredentials',
        header: t('adminServiceAccounts.columns.activeCredentials')
      },
      {
        accessorKey: 'createdAt',
        header: t('adminServiceAccounts.columns.createdAt'),
        cell: ({ row: { original } }) => formatDate(original.createdAt)
      }
    ],
    [t]
  );

  const table = useAdvanceTable({
    data: data ?? [],
    columns,
    sortable: true,
    pagination: true,
    perPage: 10
  });

  if (isLoading) {
    return (
      <Card>
        <Card.Body>{t('adminServiceAccounts.loading')}</Card.Body>
      </Card>
    );
  }

  if (error || !data) {
    return (
      <Alert variant="danger">{t('adminServiceAccounts.loadFailed')}</Alert>
    );
  }

  return (
    <>
      <PageHeader
        title={t('adminServiceAccounts.title')}
        description={t('adminServiceAccounts.description')}
        className="mb-3"
      >
        <IconButton
          variant="orkestra-default"
          size="sm"
          icon="plus"
          transform="shrink-3"
          iconAlign="middle"
          className="mt-3"
          onClick={() => setShowCreate(true)}
        >
          {t('adminServiceAccounts.newButton')}
        </IconButton>
      </PageHeader>

      <Card className="shadow-none border">
        {data.length === 0 ? (
          <Card.Body className="text-muted text-center py-4">
            {t('adminServiceAccounts.empty')}
          </Card.Body>
        ) : (
          <AdvanceTableProvider {...table}>
            <Card.Body className="p-0">
              <AdvanceTable
                headerClassName="text-nowrap align-middle"
                rowClassName="align-middle"
                tableProps={{
                  size: 'sm',
                  className: 'fs-10 mb-0 overflow-hidden'
                }}
              />
            </Card.Body>
            <Card.Footer>
              <AdvanceTableFooter navButtons rowInfo rowsPerPageSelection />
            </Card.Footer>
          </AdvanceTableProvider>
        )}
      </Card>

      <CreateServiceAccountModal
        show={showCreate}
        onHide={() => setShowCreate(false)}
      />
    </>
  );
};

export default ServiceAccountsPage;
