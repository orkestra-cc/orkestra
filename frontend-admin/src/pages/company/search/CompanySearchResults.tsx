import { Link } from 'react-router';
import { Card, Alert } from 'react-bootstrap';
import AdvanceTableProvider from 'providers/AdvanceTableProvider';
import AdvanceTable from 'components/common/advance-table/AdvanceTable';
import AdvanceTableFooter from 'components/common/advance-table/AdvanceTableFooter';
import useAdvanceTable from 'hooks/ui/useAdvanceTable';
import SubtleBadge from 'components/common/SubtleBadge';
import type { BadgeColor } from 'components/common/SubtleBadge';
import type { CompanyLookup, CompanySearchResult } from 'types/company';
import { ACTIVITY_STATUS_COLORS, ACTIVITY_STATUS_LABELS } from 'types/company';

interface CompanySearchResultsProps {
  result: CompanySearchResult;
}

const columns = [
  {
    accessorKey: 'companyName',
    header: 'Azienda',
    meta: {
      headerProps: { className: 'ps-2 text-900', style: { height: '46px' } },
      cellProps: { className: 'py-2 white-space-nowrap pe-3 pe-xxl-4 ps-2' },
    },
    cell: ({ row: { original } }: { row: { original: CompanyLookup } }) => (
      <div>
        <Link to={`/company/lookup/${original.uuid}`} className="fw-semibold text-900">
          <h6 className="mb-0 text-primary">{original.companyName}</h6>
        </Link>
        <small className="text-muted font-monospace">{original.vatCode}</small>
      </div>
    ),
  },
  {
    accessorKey: 'taxCode',
    header: 'Codice Fiscale',
    meta: {
      headerProps: { className: 'text-900' },
      cellProps: { className: 'py-2 pe-4' },
    },
    cell: ({ row: { original } }: { row: { original: CompanyLookup } }) => (
      <span className="font-monospace text-900">{original.taxCode}</span>
    ),
  },
  {
    accessorKey: 'address.town',
    header: 'Sede',
    meta: {
      headerProps: { className: 'text-900' },
      cellProps: { className: 'py-2 pe-4' },
    },
    cell: ({ row: { original } }: { row: { original: CompanyLookup } }) => (
      <div>
        <div className="text-900">{original.address.town}</div>
        {original.address.province && (
          <small className="text-muted">({original.address.province})</small>
        )}
      </div>
    ),
  },
  {
    accessorKey: 'activityStatus',
    header: 'Stato',
    meta: {
      headerProps: { className: 'text-900' },
      cellProps: { className: 'fs-9 pe-4' },
    },
    cell: ({ row: { original } }: { row: { original: CompanyLookup } }) => {
      const color = (ACTIVITY_STATUS_COLORS[original.activityStatus] || 'secondary') as BadgeColor;
      const label = ACTIVITY_STATUS_LABELS[original.activityStatus] || original.activityStatus;
      return <SubtleBadge bg={color}>{label}</SubtleBadge>;
    },
  },
  {
    accessorKey: 'sdiCode',
    header: 'Codice SDI',
    meta: {
      headerProps: { className: 'text-900' },
      cellProps: { className: 'py-2 pe-4' },
    },
    cell: ({ row: { original } }: { row: { original: CompanyLookup } }) =>
      original.sdiCode ? (
        <span className="font-monospace text-900">{original.sdiCode}</span>
      ) : (
        <span className="text-muted">-</span>
      ),
  },
];

const CompanySearchResults = ({ result }: CompanySearchResultsProps) => {
  const { companies, totalResults, dryRun } = result;

  const table = useAdvanceTable({
    columns,
    data: companies,
    sortable: true,
    pagination: true,
    perPage: 10,
  });

  if (dryRun) {
    return (
      <Card>
        <Card.Body>
          <Alert variant="info" className="mb-0">
            <strong>Dry Run:</strong> Trovate{' '}
            <strong>{totalResults ?? 0}</strong> aziende corrispondenti ai
            filtri. Disattiva &quot;Dry Run&quot; per visualizzare i risultati.
          </Alert>
        </Card.Body>
      </Card>
    );
  }

  if (companies.length === 0) {
    return (
      <Card>
        <Card.Body>
          <Alert variant="warning" className="mb-0">
            Nessuna azienda trovata con i filtri selezionati.
          </Alert>
        </Card.Body>
      </Card>
    );
  }

  return (
    <AdvanceTableProvider {...table}>
      <Card>
        <Card.Header className="border-bottom border-200 px-0">
          <div className="d-flex align-items-center px-3">
            <h6 className="mb-0">Risultati</h6>
            {totalResults != null && (
              <SubtleBadge bg="primary" className="ms-2">
                {totalResults} trovate
              </SubtleBadge>
            )}
          </div>
        </Card.Header>
        <Card.Body className="p-0">
          <AdvanceTable
            headerClassName="bg-body-tertiary align-middle"
            bodyClassName=""
            rowClassName="btn-reveal-trigger align-middle"
            tableProps={{
              size: 'sm',
              className: 'fs-10 mb-0 overflow-hidden',
            }}
          />
        </Card.Body>
        <Card.Footer>
          <AdvanceTableFooter
            viewAllBtn={false}
            navButtons={true}
            className=""
            rowInfo={true}
            rowsPerPageSelection={true}
          />
        </Card.Footer>
      </Card>
    </AdvanceTableProvider>
  );
};

export default CompanySearchResults;
