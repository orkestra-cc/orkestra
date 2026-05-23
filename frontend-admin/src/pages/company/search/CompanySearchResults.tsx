import { useMemo } from 'react';
import { Link } from 'react-router';
import { Card, Alert } from 'react-bootstrap';
import { Trans, useTranslation } from 'react-i18next';
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

const CompanySearchResults = ({ result }: CompanySearchResultsProps) => {
  const { t } = useTranslation();
  const { companies, totalResults, dryRun } = result;

  const dash = t('company.search.results.dash');

  const columns = useMemo(
    () => [
      {
        accessorKey: 'companyName',
        header: t('company.search.results.colCompany'),
        meta: {
          headerProps: {
            className: 'ps-2 text-900',
            style: { height: '46px' }
          },
          cellProps: { className: 'py-2 white-space-nowrap pe-3 pe-xxl-4 ps-2' }
        },
        cell: ({ row: { original } }: { row: { original: CompanyLookup } }) => (
          <div>
            <Link
              to={`/company/lookup/${original.uuid}`}
              className="fw-semibold text-900"
            >
              <h6 className="mb-0 text-primary">{original.companyName}</h6>
            </Link>
            <small className="text-muted font-monospace">
              {original.vatCode}
            </small>
          </div>
        )
      },
      {
        accessorKey: 'taxCode',
        header: t('company.search.results.colTaxCode'),
        meta: {
          headerProps: { className: 'text-900' },
          cellProps: { className: 'py-2 pe-4' }
        },
        cell: ({ row: { original } }: { row: { original: CompanyLookup } }) => (
          <span className="font-monospace text-900">{original.taxCode}</span>
        )
      },
      {
        accessorKey: 'address.town',
        header: t('company.search.results.colTown'),
        meta: {
          headerProps: { className: 'text-900' },
          cellProps: { className: 'py-2 pe-4' }
        },
        cell: ({ row: { original } }: { row: { original: CompanyLookup } }) => (
          <div>
            <div className="text-900">{original.address.town}</div>
            {original.address.province && (
              <small className="text-muted">
                ({original.address.province})
              </small>
            )}
          </div>
        )
      },
      {
        accessorKey: 'activityStatus',
        header: t('company.search.results.colStatus'),
        meta: {
          headerProps: { className: 'text-900' },
          cellProps: { className: 'fs-9 pe-4' }
        },
        cell: ({ row: { original } }: { row: { original: CompanyLookup } }) => {
          const color = (ACTIVITY_STATUS_COLORS[original.activityStatus] ||
            'secondary') as BadgeColor;
          const label =
            ACTIVITY_STATUS_LABELS[original.activityStatus] ||
            original.activityStatus;
          return <SubtleBadge bg={color}>{label}</SubtleBadge>;
        }
      },
      {
        accessorKey: 'sdiCode',
        header: t('company.search.results.colSdiCode'),
        meta: {
          headerProps: { className: 'text-900' },
          cellProps: { className: 'py-2 pe-4' }
        },
        cell: ({ row: { original } }: { row: { original: CompanyLookup } }) =>
          original.sdiCode ? (
            <span className="font-monospace text-900">{original.sdiCode}</span>
          ) : (
            <span className="text-muted">{dash}</span>
          )
      }
    ],
    [t, dash]
  );

  const table = useAdvanceTable({
    columns,
    data: companies,
    sortable: true,
    pagination: true,
    perPage: 10
  });

  if (dryRun) {
    return (
      <Card>
        <Card.Body>
          <Alert variant="info" className="mb-0">
            <Trans
              i18nKey="company.search.results.dryRunIntro"
              values={{ count: totalResults ?? 0 }}
              components={{ strong: <strong /> }}
            />
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
            {t('company.search.results.empty')}
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
            <h6 className="mb-0">{t('company.search.results.title')}</h6>
            {totalResults != null && (
              <SubtleBadge bg="primary" className="ms-2">
                {t('company.search.results.countBadge', {
                  count: totalResults
                })}
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
              className: 'fs-10 mb-0 overflow-hidden'
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
