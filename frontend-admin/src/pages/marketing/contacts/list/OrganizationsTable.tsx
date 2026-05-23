// Organizations tab of /marketing/contacts — sibling to PersonsTable,
// same canonical AdvanceTable stack so search/sort/pagination behave
// identically across the two tabs.

import { useMemo } from 'react';
import { Badge, Spinner } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import type { ColumnDef } from '@tanstack/react-table';

import useAdvanceTable from 'hooks/ui/useAdvanceTable';
import AdvanceTableProvider from 'providers/AdvanceTableProvider';
import AdvanceTable from 'components/common/advance-table/AdvanceTable';
import AdvanceTableSearchBox from 'components/common/advance-table/AdvanceTableSearchBox';
import AdvanceTableFooter from 'components/common/advance-table/AdvanceTableFooter';
import ExportCsvButton from 'components/common/advance-table/ExportCsvButton';
import { formatDateForCSV } from 'utils/csvExport';

import { useListMarketingOrgsQuery } from 'store/api/marketingApi';
import type { Organization } from 'types/marketing';

import ContactAvatar from './ContactAvatar';
import { primaryEmail } from './helpers';

const OrganizationsTable = () => {
  const { t } = useTranslation();
  const { data, isLoading } = useListMarketingOrgsQuery(undefined);

  const columns = useMemo<ColumnDef<Organization>[]>(
    () => [
      {
        id: 'legalName',
        // Search projection: include displayName so operators can find
        // an org by either label.
        accessorFn: row =>
          [row.legalName, row.displayName].filter(Boolean).join(' '),
        header: t('marketing.contacts.list.colLegalName'),
        cell: ({ row }) => {
          const o = row.original;
          return (
            <div className="d-flex align-items-center gap-2">
              <ContactAvatar
                email={primaryEmail(o)}
                name={o.displayName || o.legalName}
              />
              <div>
                <span className="fw-medium">{o.legalName}</span>
                {o.displayName && o.displayName !== o.legalName && (
                  <div className="text-muted fs-10">{o.displayName}</div>
                )}
              </div>
            </div>
          );
        },
        meta: { headerProps: { className: 'text-900' } }
      },
      {
        id: 'kind',
        accessorKey: 'kind',
        header: t('marketing.contacts.list.colKind'),
        cell: ({ getValue }) => (
          <Badge bg="light" text="dark">
            {String(getValue() ?? '—')}
          </Badge>
        ),
        meta: { headerProps: { className: 'text-900' } }
      },
      {
        id: 'vat',
        accessorFn: row => row.vat ?? '',
        header: t('marketing.contacts.list.colVAT'),
        cell: ({ getValue }) =>
          (getValue() as string) || <span className="text-400">—</span>,
        meta: { headerProps: { className: 'text-900' } }
      },
      {
        id: 'email',
        accessorFn: row => primaryEmail(row),
        header: t('marketing.contacts.list.colEmail'),
        cell: ({ getValue }) =>
          (getValue() as string) || <span className="text-400">—</span>,
        meta: { headerProps: { className: 'text-900' } }
      },
      {
        id: 'updatedAt',
        accessorKey: 'updatedAt',
        header: t('marketing.contacts.list.colUpdated'),
        cell: ({ getValue }) => (
          <small className="text-muted">
            {new Date(getValue() as string).toLocaleDateString()}
          </small>
        ),
        meta: { headerProps: { className: 'text-900' } }
      }
    ],
    [t]
  );

  const orgs = data?.items ?? [];
  const table = useAdvanceTable<Organization>({
    data: orgs,
    columns,
    sortable: true,
    pagination: true,
    perPage: 25
  });

  if (isLoading) {
    return (
      <div className="p-4 text-center text-muted">
        <Spinner animation="border" size="sm" className="me-2" />
        {t('marketing.contacts.list.loading')}
      </div>
    );
  }

  if (!orgs.length) {
    return (
      <div className="p-4 text-center text-muted">
        {t('marketing.contacts.list.emptyOrganizations')}
      </div>
    );
  }

  return (
    <AdvanceTableProvider {...table}>
      <div className="d-flex flex-wrap justify-content-between align-items-center gap-2 px-x1 py-2 border-bottom border-200">
        <div className="flex-grow-1" style={{ maxWidth: 360 }}>
          <AdvanceTableSearchBox
            placeholder={t('marketing.contacts.list.searchPlaceholder')}
          />
        </div>
        <ExportCsvButton<Organization>
          filename="marketing_organizations"
          buildRow={o => ({
            UUID: o.uuid,
            LegalName: o.legalName,
            DisplayName: o.displayName ?? '',
            Kind: o.kind,
            VAT: o.vat ?? '',
            TaxCode: o.taxCode ?? '',
            Email: primaryEmail(o),
            Website: o.website ?? '',
            CreatedAt: formatDateForCSV(o.createdAt),
            UpdatedAt: formatDateForCSV(o.updatedAt)
          })}
        />
      </div>
      <AdvanceTable
        headerClassName="bg-body-tertiary align-middle"
        rowClassName="align-middle white-space-nowrap"
        tableProps={{
          size: 'sm',
          className: 'fs-10 mb-0 overflow-hidden'
        }}
      />
      <div className="px-x1 py-2 border-top border-200">
        <AdvanceTableFooter
          rowInfo
          navButtons
          rowsPerPageSelection
          rowsPerPageOptions={[10, 25, 50, 100]}
        />
      </div>
    </AdvanceTableProvider>
  );
};

export default OrganizationsTable;
