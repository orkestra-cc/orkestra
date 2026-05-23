import { Card, Table, Spinner, Badge } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { faFileInvoice, faFileImport } from '@fortawesome/free-solid-svg-icons';
import { useTranslation } from 'react-i18next';
import OrkestraCardHeader from 'components/common/OrkestraCardHeader';
import { Link } from 'react-router';
import { useGetInvoicesQuery } from 'store/api/billingApi';
import {
  formatCurrency,
  formatItalianDate,
  INVOICE_STATUS_LABELS
} from 'types/billing';
import type { InvoiceSummary, InvoiceStatus } from 'types/billing';
import { lastYearRange } from './dateRanges';

const getStatusBadge = (status: InvoiceStatus) => {
  const variants: Record<InvoiceStatus, string> = {
    draft: 'secondary',
    pending: 'warning',
    sent: 'info',
    delivered: 'primary',
    rejected: 'danger',
    accepted: 'success',
    paid: 'success',
    cancelled: 'secondary'
  };
  return variants[status] || 'secondary';
};

const RecentInvoices = () => {
  const { t } = useTranslation();
  const lastYear = lastYearRange();
  const { data: issuedData, isLoading: issuedLoading } = useGetInvoicesQuery({
    direction: 'issued',
    pageSize: 5,
    fromDate: lastYear.fromDate,
    toDate: lastYear.toDate
  });

  const { data: receivedData, isLoading: receivedLoading } =
    useGetInvoicesQuery({
      direction: 'received',
      pageSize: 5,
      fromDate: lastYear.fromDate,
      toDate: lastYear.toDate
    });

  const isLoading = issuedLoading || receivedLoading;

  if (isLoading) {
    return (
      <Card className="h-100">
        <OrkestraCardHeader
          title={t('billing.dashboard.recentInvoices.title')}
          titleTag="h6"
          light
        />
        <Card.Body
          className="d-flex align-items-center justify-content-center"
          style={{ minHeight: 250 }}
        >
          <Spinner animation="border" />
        </Card.Body>
      </Card>
    );
  }

  // Combine and sort invoices by date
  const allInvoices: (InvoiceSummary & { direction: 'issued' | 'received' })[] =
    [
      ...(issuedData?.invoices || []).map(inv => ({
        ...inv,
        direction: 'issued' as const
      })),
      ...(receivedData?.invoices || []).map(inv => ({
        ...inv,
        direction: 'received' as const
      }))
    ]
      .sort(
        (a, b) =>
          new Date(b.createdAt).getTime() - new Date(a.createdAt).getTime()
      )
      .slice(0, 8);

  return (
    <Card className="h-100">
      <OrkestraCardHeader
        title={t('billing.dashboard.recentInvoices.title')}
        titleTag="h6"
        light
        endEl={
          <div className="d-flex gap-2">
            <Link
              to="/billing/invoices/issued"
              className="fs-10 text-decoration-none"
            >
              {t('billing.dashboard.recentInvoices.linkIssued')}
            </Link>
            <span className="text-body-tertiary">|</span>
            <Link
              to="/billing/invoices/received"
              className="fs-10 text-decoration-none"
            >
              {t('billing.dashboard.recentInvoices.linkReceived')}
            </Link>
          </div>
        }
      />
      <Card.Body className="p-0">
        {allInvoices.length === 0 ? (
          <div className="text-center py-5 text-body-tertiary">
            <FontAwesomeIcon
              icon={faFileInvoice}
              className="fs-3 mb-2 d-block"
            />
            <span className="fs-10">
              {t('billing.dashboard.recentInvoices.empty')}
            </span>
          </div>
        ) : (
          <Table responsive className="fs-10 mb-0">
            <thead className="bg-body-tertiary">
              <tr>
                <th className="border-0 ps-x1">
                  {t('billing.dashboard.recentInvoices.colType')}
                </th>
                <th className="border-0">
                  {t('billing.dashboard.recentInvoices.colNumber')}
                </th>
                <th className="border-0">
                  {t('billing.dashboard.recentInvoices.colCustomer')}
                </th>
                <th className="border-0">
                  {t('billing.dashboard.recentInvoices.colDate')}
                </th>
                <th className="border-0 text-end">
                  {t('billing.dashboard.recentInvoices.colAmount')}
                </th>
                <th className="border-0 text-center">
                  {t('billing.dashboard.recentInvoices.colStatus')}
                </th>
              </tr>
            </thead>
            <tbody>
              {allInvoices.map(invoice => (
                <tr key={invoice.id} className="align-middle">
                  <td className="ps-x1">
                    <FontAwesomeIcon
                      icon={
                        invoice.direction === 'issued'
                          ? faFileInvoice
                          : faFileImport
                      }
                      className={
                        invoice.direction === 'issued'
                          ? 'text-primary'
                          : 'text-success'
                      }
                      title={
                        invoice.direction === 'issued'
                          ? t('billing.dashboard.recentInvoices.typeIssued')
                          : t('billing.dashboard.recentInvoices.typeReceived')
                      }
                    />
                  </td>
                  <td>
                    <Link
                      to={`/billing/invoices/${invoice.direction}/${invoice.id}`}
                      className="fw-medium"
                    >
                      {invoice.number}
                    </Link>
                  </td>
                  <td className="text-truncate" style={{ maxWidth: 150 }}>
                    {invoice.partyName}
                  </td>
                  <td className="text-body-tertiary">
                    {formatItalianDate(invoice.date)}
                  </td>
                  <td className="text-end fw-medium">
                    {formatCurrency(invoice.totalAmount)}
                  </td>
                  <td className="text-center">
                    <Badge
                      bg={getStatusBadge(invoice.status)}
                      className="text-uppercase fs-11"
                    >
                      {INVOICE_STATUS_LABELS[invoice.status]}
                    </Badge>
                  </td>
                </tr>
              ))}
            </tbody>
          </Table>
        )}
      </Card.Body>
    </Card>
  );
};

export default RecentInvoices;
